import type { ActivityLink, ResourceRef, ResourceLinkResolver, ResourceLinkContext } from '../types/activity';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

/**
 * Returns the visible text for a link: prefer the server-provided
 * displayName (e.g. "Smith Nelson") and fall back to the original marker
 * (e.g. an email or UID baked into the summary by the policy template).
 */
function linkVisibleText(link: ActivityLink): string {
  return link.displayName && link.displayName.length > 0 ? link.displayName : link.marker;
}

/**
 * Returns true when the link carries hover-worthy detail beyond the
 * visible text (e.g. an email or UID that we want to surface but not
 * show inline).
 */
function linkHasHoverDetail(link: ActivityLink): boolean {
  if (link.email && link.email !== linkVisibleText(link)) return true;
  const uid = link.resource?.uid;
  if (uid && uid !== linkVisibleText(link)) return true;
  return false;
}

/** Renders the tooltip body shown when a link is hovered. */
function LinkHoverBody({ link }: { link: ActivityLink }) {
  return (
    <div className="flex flex-col gap-0.5 text-xs">
      {link.email ? <span className="font-mono">{link.email}</span> : null}
      {link.resource?.uid ? <span className="font-mono opacity-70">{link.resource.uid}</span> : null}
    </div>
  );
}

export interface ResourceLinkClickHandler {
  (resource: ResourceRef): void;
}

export interface ActivityFeedSummaryProps {
  /** The summary text to render */
  summary: string;
  /** Links within the summary to make clickable */
  links?: ActivityLink[];
  /** Handler called when a resource link is clicked (deprecated: use resourceLinkResolver) */
  onResourceClick?: ResourceLinkClickHandler;
  /** Function that resolves resource references to URLs (renders as <a> tags) */
  resourceLinkResolver?: ResourceLinkResolver;
  /** Context for resolving resource links (includes tenant information) */
  resourceLinkContext?: ResourceLinkContext;
  /** Additional CSS class */
  className?: string;
}

/**
 * Parse summary text and replace marker strings with clickable links
 */
function parseSummaryWithLinks(
  summary: string,
  links: ActivityLink[] | undefined,
  onResourceClick?: ResourceLinkClickHandler,
  resourceLinkResolver?: ResourceLinkResolver,
  resourceLinkContext?: ResourceLinkContext
): (string | JSX.Element)[] {
  if (!links || links.length === 0) {
    return [summary];
  }

  // Sort links by marker length (longest first) to avoid partial matches.
  // Skip empty markers — indexOf('') clamps to summary.length and would
  // produce an infinite loop below.
  const sortedLinks = [...links]
    .filter((l) => l.marker && l.marker.length > 0)
    .sort((a, b) => b.marker.length - a.marker.length);

  // Track positions that have been replaced
  interface ReplacedRange {
    start: number;
    end: number;
    link: ActivityLink;
  }

  const replacedRanges: ReplacedRange[] = [];

  // Find all marker positions
  for (const link of sortedLinks) {
    let searchStart = 0;
    let pos = summary.indexOf(link.marker, searchStart);

    while (pos !== -1) {
      const end = pos + link.marker.length;

      // Check if this range overlaps with any existing range
      const overlaps = replacedRanges.some(
        (range) => pos < range.end && end > range.start
      );

      if (!overlaps) {
        replacedRanges.push({ start: pos, end, link });
      }

      searchStart = pos + 1;
      pos = summary.indexOf(link.marker, searchStart);
    }
  }

  // Sort ranges by start position
  replacedRanges.sort((a, b) => a.start - b.start);

  // Build the result array
  const result: (string | JSX.Element)[] = [];
  let lastEnd = 0;

  for (let i = 0; i < replacedRanges.length; i++) {
    const range = replacedRanges[i];

    // Add text before this marker
    if (range.start > lastEnd) {
      result.push(summary.substring(lastEnd, range.start));
    }

    const visibleText = linkVisibleText(range.link);
    const showHover = linkHasHoverDetail(range.link);

    // If resourceLinkResolver is provided, render as <a> tag
    if (resourceLinkResolver) {
      const url = resourceLinkResolver(range.link.resource, resourceLinkContext);
      if (url) {
        const anchor = (
          <a
            key={`link-${i}`}
            href={url}
            className="underline underline-offset-2 text-primary hover:text-primary/80"
            title={showHover ? undefined : `${range.link.resource.kind}: ${range.link.resource.name}`}
            onClick={(e) => e.stopPropagation()}
          >
            {visibleText}
          </a>
        );

        if (showHover) {
          result.push(
            <Tooltip key={`link-${i}`}>
              <TooltipTrigger asChild>{anchor}</TooltipTrigger>
              <TooltipContent>
                <LinkHoverBody link={range.link} />
              </TooltipContent>
            </Tooltip>
          );
        } else {
          result.push(anchor);
        }
      } else if (showHover) {
        // Resolver opted out of linking but we still want to surface the
        // hover detail (email/UID) for user-typed references.
        result.push(
          <Tooltip key={`link-${i}`}>
            <TooltipTrigger asChild>
              <span className="underline decoration-dotted decoration-muted-foreground/60 underline-offset-2 cursor-help">
                {visibleText}
              </span>
            </TooltipTrigger>
            <TooltipContent>
              <LinkHoverBody link={range.link} />
            </TooltipContent>
          </Tooltip>
        );
      } else {
        // Resolver returned undefined and there's no hover detail, render plain text
        result.push(visibleText);
      }
    } else {
      // Fallback to button with onResourceClick handler for backward compatibility
      const handleClick = onResourceClick
        ? (e: React.MouseEvent) => {
            e.preventDefault();
            e.stopPropagation();
            onResourceClick(range.link.resource);
          }
        : undefined;

      const button = (
        <button
          key={`link-${i}`}
          type="button"
          className="bg-transparent border-none p-0 cursor-pointer underline underline-offset-2 text-primary hover:text-primary/80"
          onClick={handleClick}
          title={showHover ? undefined : `${range.link.resource.kind}: ${range.link.resource.name}`}
        >
          {visibleText}
        </button>
      );

      if (showHover) {
        result.push(
          <Tooltip key={`link-${i}`}>
            <TooltipTrigger asChild>{button}</TooltipTrigger>
            <TooltipContent>
              <LinkHoverBody link={range.link} />
            </TooltipContent>
          </Tooltip>
        );
      } else {
        result.push(button);
      }
    }

    lastEnd = range.end;
  }

  // Add any remaining text
  if (lastEnd < summary.length) {
    result.push(summary.substring(lastEnd));
  }

  return result;
}

/**
 * ActivityFeedSummary renders an activity summary with clickable resource links
 */
export function ActivityFeedSummary({
  summary,
  links,
  onResourceClick,
  resourceLinkResolver,
  resourceLinkContext,
  className = '',
}: ActivityFeedSummaryProps) {
  const parsedContent = parseSummaryWithLinks(summary, links, onResourceClick, resourceLinkResolver, resourceLinkContext);

  return (
    <span className={`text-xs text-foreground leading-normal break-words ${className}`}>
      {parsedContent}
    </span>
  );
}
