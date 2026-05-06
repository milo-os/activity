import { useState } from "react";
import type {
  Activity,
  ResourceLinkResolver,
  TenantLinkResolver,
  TenantRenderer,
} from "../types/activity";
import {
  ActivityFeedSummary,
  ResourceLinkClickHandler,
} from "./ActivityFeedSummary";
import { ActivityExpandedDetails } from "./ActivityExpandedDetails";
import { TenantBadge } from "./TenantBadge";
import { cn } from "../lib/utils";
import { Button } from "@datum-cloud/datum-ui/button";
import { Plus, Pencil, Trash2, Activity as ActivityIcon } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { TableCell, TableRow } from "@datum-cloud/datum-ui/table";
import { Timestamp } from "./Timestamp";

// Number of columns rendered for the feed variant. Used as the colSpan on
// the expanded-detail row so it stretches across the full width.
export const ACTIVITY_FEED_COLUMN_COUNT = 5;

export interface ActivityFeedItemProps {
  /** The activity to render */
  activity: Activity;
  /** Handler called when a resource link is clicked (deprecated: use resourceLinkResolver) */
  onResourceClick?: ResourceLinkClickHandler;
  /** Function that resolves resource references to URLs */
  resourceLinkResolver?: ResourceLinkResolver;
  /** Function that resolves tenant references to URLs */
  tenantLinkResolver?: TenantLinkResolver;
  /** Custom renderer for tenant badges (overrides default TenantBadge) */
  tenantRenderer?: TenantRenderer;
  /** Handler called when the actor name or avatar is clicked */
  onActorClick?: (actorName: string) => void;
  /** Handler called when the item is clicked */
  onActivityClick?: (activity: Activity) => void;
  /** Whether the item is selected */
  isSelected?: boolean;
  /** Additional CSS class */
  className?: string;
  /** Whether to show as compact (for resource detail tabs) */
  compact?: boolean;
  /** Whether this is a newly streamed activity */
  isNew?: boolean;
  /** Layout variant: 'feed' (default) or 'timeline' */
  variant?: "feed" | "timeline";
  /** Whether this is the last item in the list (hides bottom border, only used in timeline variant) */
  isLast?: boolean;
  /** Whether the item starts expanded */
  defaultExpanded?: boolean;
}

/**
 * Get avatar initials from actor name
 */
function getActorInitials(name: string): string {
  const parts = name.split(/[@\s.]+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].charAt(0).toUpperCase();
  return (parts[0].charAt(0) + parts[parts.length - 1].charAt(0)).toUpperCase();
}

/**
 * Get Tailwind classes for actor avatar based on actor type
 */
function getActorAvatarClasses(actorType: string, compact: boolean): string {
  const baseClasses = cn(
    "rounded-full flex items-center justify-center shrink-0 font-semibold",
    compact ? "w-5 h-5 text-1xs" : "w-6 h-6 text-1xs",
  );
  switch (actorType) {
    case "user":
      return cn(baseClasses, "bg-primary text-primary-foreground");
    case "controller":
      return cn(baseClasses, "bg-secondary text-secondary-foreground");
    case "machine account":
      return cn(baseClasses, "bg-muted text-muted-foreground");
    default:
      return cn(baseClasses, "bg-muted text-muted-foreground");
  }
}

/**
 * Extract verb from activity summary (e.g., "alice created HTTPProxy" -> "created")
 */
function extractVerb(summary: string): string {
  const words = summary.split(/\s+/);
  if (words.length >= 2) {
    return words[1].toLowerCase();
  }
  return "unknown";
}

/**
 * Normalize verb to a canonical form for coloring
 */
function normalizeVerb(verb: string): "create" | "update" | "delete" | "other" {
  const normalized = verb.toLowerCase();
  if (normalized.includes("create") || normalized.includes("add"))
    return "create";
  if (normalized.includes("delete") || normalized.includes("remove"))
    return "delete";
  if (
    normalized.includes("update") ||
    normalized.includes("patch") ||
    normalized.includes("modify") ||
    normalized.includes("change") ||
    normalized.includes("edit")
  )
    return "update";
  return "other";
}

/**
 * Get icon container + icon color classes based on verb
 */
function getActionIconClasses(verb: string): {
  container: string;
  icon: string;
} {
  const normalizedVerb = normalizeVerb(verb);
  switch (normalizedVerb) {
    case "create":
      return {
        container: "bg-blue-50 dark:bg-blue-950",
        icon: "text-blue-500 dark:text-blue-400",
      };
    case "update":
      return {
        container: "bg-green-50 dark:bg-green-950",
        icon: "text-green-600 dark:text-green-400",
      };
    case "delete":
      return {
        container: "bg-red-50 dark:bg-red-950",
        icon: "text-red-500 dark:text-red-400",
      };
    default:
      return {
        container: "bg-slate-100 dark:bg-slate-800",
        icon: "text-slate-500 dark:text-slate-400",
      };
  }
}

/**
 * Get the Lucide icon component for the timeline node based on verb
 */
function getTimelineIcon(verb: string): React.ElementType {
  const normalizedVerb = normalizeVerb(verb);
  switch (normalizedVerb) {
    case "create":
      return Plus;
    case "update":
      return Pencil;
    case "delete":
      return Trash2;
    default:
      return ActivityIcon;
  }
}

/**
 * ActivityFeedItem renders a single activity in the feed or timeline
 */
export function ActivityFeedItem({
  activity,
  onResourceClick,
  resourceLinkResolver,
  tenantLinkResolver,
  tenantRenderer,
  onActorClick,
  onActivityClick,
  isSelected = false,
  className = "",
  compact = false,
  isNew = false,
  variant = "feed",
  isLast = false,
  defaultExpanded = false,
}: ActivityFeedItemProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  const { spec, metadata } = activity;
  const { actor, summary, links, tenant } = spec;

  const handleActorClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onActorClick) {
      onActorClick(actor.name);
    }
  };

  const toggleExpand = (e: React.MouseEvent) => {
    e.stopPropagation();
    setIsExpanded(!isExpanded);
  };

  const timestamp = metadata?.creationTimestamp;
  const verb = extractVerb(summary);
  const isTimeline = variant === "timeline";

  // Timeline variant — flat list row with bottom border
  if (isTimeline) {
    const { container: iconBg, icon: iconColor } = getActionIconClasses(verb);
    const Icon = getTimelineIcon(verb);
    return (
      <div
        className={cn(
          !isLast && !isExpanded && "border-b border-border",
          className,
        )}
      >
        <div
          className={cn(
            "flex items-center gap-3 py-3 cursor-pointer group",
            isSelected && "bg-muted/40",
          )}
          onClick={toggleExpand}
        >
          {/* Action icon square */}
          <div
            className={cn(
              "w-8 h-8 rounded-md shrink-0 flex items-center justify-center",
              iconBg,
              iconColor,
            )}
          >
            <Icon size={16} strokeWidth={2} />
          </div>

          {/* Summary */}
          <div className="flex-1 min-w-0 text-sm text-foreground leading-snug">
            <ActivityFeedSummary
              summary={summary}
              links={links}
              onResourceClick={onResourceClick}
              resourceLinkResolver={resourceLinkResolver}
              resourceLinkContext={{ tenant }}
            />
          </div>

          {/* Tenant badge */}
          {tenant && (
            <div className="shrink-0">
              {tenantRenderer ? (
                tenantRenderer(tenant)
              ) : (
                <TenantBadge
                  tenant={tenant}
                  tenantLinkResolver={tenantLinkResolver}
                  size="compact"
                />
              )}
            </div>
          )}

          {/* Timestamp */}
          <span className="text-xs text-muted-foreground whitespace-nowrap shrink-0">
            <Timestamp value={timestamp} />
          </span>

          {/* Expand toggle */}
          <Button
            type="quaternary" theme="borderless"
            size="small"
            className="h-5 py-0 px-1 text-base text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
            onClick={toggleExpand}
            aria-expanded={isExpanded}
          >
            {isExpanded ? "−" : "+"}
          </Button>
        </div>

        {/* Expanded Details */}
        {isExpanded && (
          <ActivityExpandedDetails
            activity={activity}
            tenantLinkResolver={tenantLinkResolver}
            compact
          />
        )}
      </div>
    );
  }

  // Feed variant — flat-column table row. Columns: Actor | Summary |
  // Tenant | When | expand toggle. Renders an additional TableRow with a
  // single colSpan'd cell when expanded so the detail panel spans the full
  // table width. Tooltip primitives inherit the TooltipProvider mounted by
  // the parent ActivityFeed.
  const actorVisible = actor.displayName || actor.name;
  const actorInitialsSource = actor.displayName || actor.name;

  const avatar = (
    <div
      className={cn(
        getActorAvatarClasses(actor.type, compact),
        onActorClick && "cursor-pointer hover:opacity-80 transition-opacity",
      )}
      onClick={onActorClick ? handleActorClick : undefined}
    >
      {actor.type === "controller" ? (
        <span className="text-xs">⚙</span>
      ) : actor.type === "machine account" ? (
        <span className="text-xs">🤖</span>
      ) : (
        <span className="uppercase">
          {getActorInitials(actorInitialsSource)}
        </span>
      )}
    </div>
  );

  // Actor column = avatar only. The display name / email / UID are
  // surfaced via a hover tooltip on the avatar so the column stays narrow.
  const actorTooltipBody = (
    <div className="flex flex-col gap-0.5 text-xs">
      <span className="font-medium">{actorVisible}</span>
      {actor.email && actor.email !== actorVisible ? (
        <span className="font-mono">{actor.email}</span>
      ) : null}
      {actor.uid ? (
        <span className="font-mono opacity-70">{actor.uid}</span>
      ) : null}
    </div>
  );

  const actorCell = (
    <Tooltip>
      <TooltipTrigger asChild>{avatar}</TooltipTrigger>
      <TooltipContent>{actorTooltipBody}</TooltipContent>
    </Tooltip>
  );

  return (
    <>
      <TableRow
        data-state={isSelected ? "selected" : undefined}
        className={cn(
          "cursor-pointer",
          isNew && "bg-green-50/40 dark:bg-green-950/20",
          className,
        )}
        onClick={(e) => {
          // Row-level click toggles expansion. Inner interactive elements
          // (links, avatar, expand button) call e.stopPropagation() so
          // they don't double-fire. If a parent passed onActivityClick,
          // we still notify it so deep-link/select hooks keep working.
          toggleExpand(e);
          onActivityClick?.(activity);
        }}
        aria-expanded={isExpanded}
      >
        <TableCell className="py-2 align-middle">{actorCell}</TableCell>
        {/* Summary takes the remaining horizontal space; the auto-layout
            table gives this cell `width: 100%` worth of room and other
            columns size to their content. The inner div is the truncation
            box so its single line ellipsizes when the row gets crowded. */}
        <TableCell
          className="py-2 align-middle"
          style={{ width: "100%", maxWidth: 0, overflow: "hidden" }}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <div
                className="text-sm leading-snug"
                style={{
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                <ActivityFeedSummary
                  summary={summary}
                  links={links}
                  onResourceClick={onResourceClick}
                  resourceLinkResolver={resourceLinkResolver}
                  resourceLinkContext={{ tenant }}
                />
              </div>
            </TooltipTrigger>
            <TooltipContent className="max-w-[480px] whitespace-normal break-words">
              {summary}
            </TooltipContent>
          </Tooltip>
        </TableCell>
        <TableCell className="py-2 align-middle whitespace-nowrap">
          {tenant ? (
            tenantRenderer ? (
              tenantRenderer(tenant)
            ) : (
              <TenantBadge
                tenant={tenant}
                tenantLinkResolver={tenantLinkResolver}
              />
            )
          ) : null}
        </TableCell>
        <TableCell className="py-2 align-middle whitespace-nowrap text-sm text-muted-foreground">
          <Timestamp value={timestamp} />
        </TableCell>
        <TableCell className="py-2 align-middle w-10">
          <Button
            type="quaternary" theme="borderless"
            size="small"
            htmlType="button"
            className="h-6 w-6 p-0 text-base text-muted-foreground hover:text-foreground"
            onClick={toggleExpand}
            aria-expanded={isExpanded}
            aria-label={isExpanded ? "Collapse details" : "Expand details"}
          >
            {isExpanded ? "−" : "+"}
          </Button>
        </TableCell>
      </TableRow>
      {isExpanded ? (
        <TableRow className="bg-muted/30 hover:bg-muted/30">
          <TableCell colSpan={ACTIVITY_FEED_COLUMN_COUNT} className="p-0">
            <ActivityExpandedDetails
              activity={activity}
              tenantLinkResolver={tenantLinkResolver}
              compact
            />
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
