import { isToday, isYesterday, format } from 'date-fns';
import type { Activity, Tenant } from '../types/activity';
import { extractVerb, normalizeVerb, type NormalizedVerb } from './verb';

/**
 * A run of consecutive activities that share a verb, resource kind, actor,
 * and tenant — e.g. a burst of DNS records created by the same controller
 * in one reconcile. A group of size 1 wraps a single ungrouped activity.
 */
export interface ActivityGroup {
  key: string;
  verb: NormalizedVerb;
  kind: string;
  actorName: string;
  actorDisplayName?: string;
  tenant?: Tenant;
  activities: Activity[];
  count: number;
  /** Timestamp of the most recent activity in the group (activities are newest-first). */
  latestTimestamp?: string;
}

function tenantKey(tenant?: Tenant): string {
  if (!tenant) return '';
  return `${tenant.type}:${tenant.name}`;
}

function groupSignature(activity: Activity): string {
  const { spec } = activity;
  const verb = normalizeVerb(extractVerb(spec.summary));
  return [verb, spec.resource.kind, spec.actor.name, tenantKey(spec.tenant)].join('|');
}

/**
 * Group consecutive activities that share verb + resource kind + actor +
 * tenant. Activities are assumed to already be sorted newest-first (as
 * returned by `useActivityFeed`); only adjacent items are merged, so a
 * burst of matching activities collapses into one group without needing
 * any time-window heuristics.
 */
export function groupActivities(activities: Activity[]): ActivityGroup[] {
  const groups: ActivityGroup[] = [];

  for (const activity of activities) {
    const signature = groupSignature(activity);
    const last = groups[groups.length - 1];

    if (last && last.key === signature) {
      last.activities.push(activity);
      last.count += 1;
      continue;
    }

    const { spec } = activity;
    groups.push({
      key: signature,
      verb: normalizeVerb(extractVerb(spec.summary)),
      kind: spec.resource.kind,
      actorName: spec.actor.name,
      actorDisplayName: spec.actor.displayName,
      tenant: spec.tenant,
      activities: [activity],
      count: 1,
      latestTimestamp: activity.metadata?.creationTimestamp,
    });
  }

  return groups;
}

export interface ActivityDaySection {
  /** Day label, e.g. "Today", "Yesterday", or a formatted date */
  label: string;
  groups: ActivityGroup[];
}

function dayLabel(timestamp: string | undefined): string {
  if (!timestamp) return 'Unknown date';
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return 'Unknown date';
  if (isToday(date)) return 'Today';
  if (isYesterday(date)) return 'Yesterday';
  return format(date, 'MMMM d, yyyy');
}

/**
 * Bucket activities into calendar-day sections (Today / Yesterday / a
 * formatted date), then group consecutive matching activities within each
 * day. Bucketing by day first (rather than grouping across the whole list)
 * keeps a group from silently spanning a day boundary.
 */
export function groupActivitiesByDay(activities: Activity[]): ActivityDaySection[] {
  const sections: ActivityDaySection[] = [];

  let currentLabel: string | null = null;
  let currentBucket: Activity[] = [];

  const flush = () => {
    if (currentLabel !== null && currentBucket.length > 0) {
      sections.push({ label: currentLabel, groups: groupActivities(currentBucket) });
    }
  };

  for (const activity of activities) {
    const label = dayLabel(activity.metadata?.creationTimestamp);
    if (label !== currentLabel) {
      flush();
      currentLabel = label;
      currentBucket = [];
    }
    currentBucket.push(activity);
  }
  flush();

  return sections;
}
