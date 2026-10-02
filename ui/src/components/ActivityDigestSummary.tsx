import { useMemo } from "react";
import type { Activity } from "../types/activity";

export interface ActivityDigestSummaryProps {
  /** Activities currently loaded in the feed (the loaded page(s), not necessarily the full range) */
  activities: Activity[];
  /** Additional CSS class */
  className?: string;
}

/**
 * Thin stat strip summarizing the activities currently loaded in the feed —
 * e.g. "18 changes · 3 people · 1 system". Computed client-side over the
 * loaded/paginated activities; it is a quick-glance headline, not a
 * full-range aggregate (that would require a backend facet query).
 */
export function ActivityDigestSummary({ activities, className = "" }: ActivityDigestSummaryProps) {
  const stats = useMemo(() => {
    const humanActors = new Set<string>();
    const systemActors = new Set<string>();

    for (const activity of activities) {
      const { actor, changeSource } = activity.spec;
      if (changeSource === "human") {
        humanActors.add(actor.name);
      } else {
        systemActors.add(actor.name);
      }
    }

    return {
      total: activities.length,
      people: humanActors.size,
      systems: systemActors.size,
    };
  }, [activities]);

  if (stats.total === 0) return null;

  const parts: string[] = [`${stats.total} ${stats.total === 1 ? "change" : "changes"}`];
  if (stats.people > 0) parts.push(`${stats.people} ${stats.people === 1 ? "person" : "people"}`);
  if (stats.systems > 0) parts.push(`${stats.systems} ${stats.systems === 1 ? "system" : "systems"}`);

  return (
    <div className={`px-1 pt-4 pb-2 text-xs text-muted-foreground ${className}`}>
      {parts.join(" · ")}
    </div>
  );
}
