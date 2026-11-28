import type { ActivityLink, ResourceRef, ResourceLinkResolver, ResourceLinkContext } from '../types/activity';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

/**
 * Returns true for links that point at a user: an iam User, or the actor
 * reference (`kind: user`) a policy template produced.
 */
function isUserLink(link: ActivityLink): boolean {
  const resource = link.resource;
  if (!resource) return false;
  return (
    (resource.apiGroup === 'iam.miloapis.com' && resource.kind === 'User') ||
    (!resource.apiGroup && resource.kind === 'user')
  );
}

/**
 * Returns the visible text for a link. User links show the server-provided
 * displayName (e.g. "Smith Nelson") when present; everything else shows the
 * marker exactly as it appears in the summary.
 */
function linkVisibleText(link: ActivityLink): string {
  return isUserLink(link) && link.displayName ? link.displayName : link.marker;
}

/**
 * Returns the hover text for a link, if any. For a user link shown by its
 * display name this is the original marker (the verified username or email);
 * for other links it is the resource UID when that adds information.
 */
function linkHoverText(link: ActivityLink): string | undefined {
  const visible = linkVisibleText(link);
  if (isUserLink(link)) {
    return visible !== link.marker ? link.marker : undefined;
  }
  const uid = link.resource?.uid;
  return uid && uid !== visible ? uid : undefined;
}

/** Wraps a rendered link in a tooltip showing its hover text, if any. */
function withHover(key: string, hoverText: string | undefined, node: JSX.Element): JSX.Element {
  if (!hoverText) return node;
  return (
    <Tooltip key={key}>
      <TooltipTrigger asChild>{node}</TooltipTrigger>
      <TooltipContent>
        <span className="font-mono text-xs">{hoverText}</span>
      </TooltipContent>
    </Tooltip>
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

    const key = `link-${i}`;
    const visibleText = linkVisibleText(range.link);
    const hoverText = linkHoverText(range.link);
    const userLink = isUserLink(range.link);
    const url = resourceLinkResolver ? resourceLinkResolver(range.link.resource, resourceLinkContext) : undefined;

    if (url) {
      result.push(
        withHover(
          key,
          hoverText,
          <a
            key={key}
            href={url}
            className="underline underline-offset-2 text-primary hover:text-primary/80"
            title={hoverText ? undefined : `${range.link.resource.kind}: ${range.link.resource.name}`}
            onClick={(e) => e.stopPropagation()}
          >
            {visibleText}
          </a>
        )
      );
    } else if (resourceLinkResolver || userLink) {
      // No href: plain text. User links are never rendered as buttons; they
      // only become links when a resolver returns an href for them.
      result.push(
        hoverText ? (
          withHover(
            key,
            hoverText,
            <span key={key} className="cursor-help">
              {visibleText}
            </span>
          )
        ) : (
          visibleText
        )
      );
    } else {
      // Fallback to button with onResourceClick handler for backward compatibility
      const handleClick = onResourceClick
        ? (e: React.MouseEvent) => {
            e.preventDefault();
            e.stopPropagation();
            onResourceClick(range.link.resource);
          }
        : undefined;

      result.push(
        withHover(
          key,
          hoverText,
          <button
            key={key}
            type="button"
            className="bg-transparent border-none p-0 cursor-pointer underline underline-offset-2 text-primary hover:text-primary/80"
            onClick={handleClick}
            title={hoverText ? undefined : `${range.link.resource.kind}: ${range.link.resource.name}`}
          >
            {visibleText}
          </button>
        )
      );
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
