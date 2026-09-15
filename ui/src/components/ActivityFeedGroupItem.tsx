import { useState } from "react";
import type {
  ResourceLinkResolver,
  TenantLinkResolver,
  TenantRenderer,
} from "../types/activity";
import type { ActivityGroup } from "../lib/groupActivities";
import { getActionIconClasses, getTimelineIcon } from "../lib/verb";
import { ActivityFeedItem } from "./ActivityFeedItem";
import { TenantBadge } from "./TenantBadge";
import { Timestamp } from "./Timestamp";
import { Button } from "@datum-cloud/datum-ui/button";
import { cn } from "../lib/utils";

export interface ActivityFeedGroupItemProps {
  /** The group to render — a run of consecutive, same verb/kind/actor/tenant activities */
  group: ActivityGroup;
  resourceLinkResolver?: ResourceLinkResolver;
  tenantLinkResolver?: TenantLinkResolver;
  tenantRenderer?: TenantRenderer;
  onActorClick?: (actorName: string) => void;
  onActivityClick?: (activity: ActivityGroup["activities"][number]) => void;
  /** Whether this is the last group in the list (hides bottom border) */
  isLast?: boolean;
}

const KIND_PLURALS: Record<string, string> = {
  DNSRecordSet: "DNS records",
};

function pluralizeKind(kind: string, count: number): string {
  if (count === 1) return kind;
  return KIND_PLURALS[kind] ?? `${kind}s`;
}

const VERB_LABEL: Record<ActivityGroup["verb"], string> = {
  create: "created",
  update: "updated",
  delete: "deleted",
  other: "changed",
};

/**
 * ActivityFeedGroupItem renders a run of consecutive same verb/kind/actor
 * activities (see `groupActivities`) as a single collapsed row in the
 * "digest" timeline. A group of size 1 renders exactly like a normal
 * `ActivityFeedItem` — no visual change for ungrouped activities. Larger
 * groups render a synthesized summary ("{actor} created 12 DNS records")
 * that expands to reveal the individual activities underneath.
 */
export function ActivityFeedGroupItem({
  group,
  resourceLinkResolver,
  tenantLinkResolver,
  tenantRenderer,
  onActorClick,
  onActivityClick,
  isLast = false,
}: ActivityFeedGroupItemProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  if (group.count === 1) {
    return (
      <ActivityFeedItem
        activity={group.activities[0]}
        resourceLinkResolver={resourceLinkResolver}
        tenantLinkResolver={tenantLinkResolver}
        tenantRenderer={tenantRenderer}
        onActorClick={onActorClick}
        onActivityClick={onActivityClick}
        variant="timeline"
        density="compact"
        isLast={isLast}
      />
    );
  }

  const { container: iconBg, icon: iconColor } = getActionIconClasses(group.verb);
  const Icon = getTimelineIcon(group.verb);
  const actorVisible = group.actorDisplayName || group.actorName;
  const summary = `${actorVisible} ${VERB_LABEL[group.verb]} ${group.count} ${pluralizeKind(
    group.kind,
    group.count
  )}`;

  const toggleExpand = () => setIsExpanded((prev) => !prev);

  return (
    <div className={cn(!isLast && !isExpanded && "border-b border-border")}>
      <div
        className="flex items-center gap-3 py-1.5 cursor-pointer group"
        onClick={toggleExpand}
      >
        <div
          className={cn(
            "w-6 h-6 rounded-md shrink-0 flex items-center justify-center relative",
            iconBg,
            iconColor
          )}
        >
          <Icon size={14} strokeWidth={2} />
          <span className="absolute -bottom-1 -right-1 rounded-full bg-background border border-border px-1 text-[10px] font-semibold leading-tight text-foreground">
            {group.count}
          </span>
        </div>

        <div className="flex-1 min-w-0 text-xs text-foreground leading-normal">
          {summary}
        </div>

        {group.tenant && (
          <div className="shrink-0">
            {tenantRenderer ? (
              tenantRenderer(group.tenant)
            ) : (
              <TenantBadge
                tenant={group.tenant}
                tenantLinkResolver={tenantLinkResolver}
                size="compact"
              />
            )}
          </div>
        )}

        <span className="text-xs text-muted-foreground whitespace-nowrap shrink-0">
          <Timestamp value={group.latestTimestamp} />
        </span>

        <Button
          type="quaternary"
          theme="borderless"
          size="small"
          className="h-5 py-0 px-1 text-base text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
          onClick={toggleExpand}
          aria-expanded={isExpanded}
        >
          {isExpanded ? "−" : "+"}
        </Button>
      </div>

      {isExpanded && (
        <div className="pl-11 flex flex-col">
          {group.activities.map((activity, index) => (
            <ActivityFeedItem
              key={activity.metadata?.uid || activity.metadata?.name}
              activity={activity}
              resourceLinkResolver={resourceLinkResolver}
              tenantLinkResolver={tenantLinkResolver}
              tenantRenderer={tenantRenderer}
              onActorClick={onActorClick}
              onActivityClick={onActivityClick}
              compact
              density="compact"
              variant="timeline"
              isLast={index === group.activities.length - 1}
            />
          ))}
        </div>
      )}
    </div>
  );
}
