import { useEffect, useCallback, useMemo } from 'react';
import type { Activity, ResourceLinkResolver, ErrorFormatter } from '../types/activity';
import type { ActivityFeedFilters } from '../hooks/useActivityFeed';
import { useActivityFeed } from '../hooks/useActivityFeed';
import { ActivityFeedItem } from './ActivityFeedItem';
import { ResourceLinkClickHandler } from './ActivityFeedSummary';
import { ActivityApiClient } from '../api/client';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardHeader, CardTitle, CardContent } from '@datum-cloud/datum-ui/card';
import { cn } from '../lib/utils';
import { ApiErrorAlert } from './ApiErrorAlert';

/**
 * Resource filter for searching by resource attributes
 */
export interface ResourceFilter {
  /** API group of the resource (e.g., 'networking.k8s.io') */
  apiGroup?: string;
  /** Kind of the resource (e.g., 'HTTPProxy') */
  kind?: string;
  /** Namespace of the resource */
  namespace?: string;
  /** Name of the resource (supports partial match) */
  name?: string;
  /** UID of the resource (exact match, takes precedence over other filters) */
  uid?: string;
}

export interface ResourceHistoryViewProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Resource filter - can filter by UID or by apiGroup/kind/namespace/name */
  resourceFilter: ResourceFilter;
  /** Start of time range (default: 'now-30d') */
  startTime?: string;
  /** Maximum number of events to load (default: 50) */
  limit?: number;
  /** Whether to show the header (default: true) */
  showHeader?: boolean;
  /** Reduced padding for embedding (default: false) */
  compact?: boolean;
  /** Handler called when an activity is clicked */
  onActivityClick?: (activity: Activity) => void;
  /** Handler called when a resource link is clicked (deprecated: use resourceLinkResolver) */
  onResourceClick?: ResourceLinkClickHandler;
  /** Function that resolves resource references to URLs */
  resourceLinkResolver?: ResourceLinkResolver;
  /** Handler called when the actor name or avatar is clicked */
  onActorClick?: (actorName: string) => void;
  /** Additional CSS class */
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

/**
 * Build a display title from the resource filter
 */
function buildHeaderTitle(filter: ResourceFilter): string {
  if (filter.uid) {
    return `Resource History (UID: ${filter.uid.substring(0, 8)}...)`;
  }

  const parts: string[] = [];
  if (filter.kind) {
    parts.push(filter.kind);
  }
  if (filter.name) {
    parts.push(filter.name);
  }
  if (filter.namespace) {
    parts.push(`in ${filter.namespace}`);
  }
  if (filter.apiGroup) {
    parts.push(`(${filter.apiGroup})`);
  }

  return parts.length > 0 ? parts.join(' ') : 'Resource History';
}

/**
 * ResourceHistoryView displays a resource's change history as a vertical timeline.
 * Shows who changed what, when, with expandable details.
 *
 * Can filter by:
 * - UID (exact match)
 * - API Group, Kind, Namespace, Name (combined filter)
 */
export function ResourceHistoryView({
  client,
  resourceFilter,
  startTime = 'now-30d',
  limit = 50,
  showHeader = true,
  compact = false,
  onActivityClick,
  onResourceClick,
  resourceLinkResolver,
  onActorClick,
  className,
  errorFormatter,
}: ResourceHistoryViewProps) {
  // Build the activity feed filters from the resource filter
  const activityFilters = useMemo((): ActivityFeedFilters => {
    const filters: ActivityFeedFilters = {};

    // UID takes precedence if provided
    if (resourceFilter.uid) {
      filters.resourceUid = resourceFilter.uid;
    } else {
      // Use attribute-based filters
      if (resourceFilter.apiGroup) {
        filters.apiGroups = [resourceFilter.apiGroup];
      }
      if (resourceFilter.kind) {
        filters.resourceKinds = [resourceFilter.kind];
      }
      if (resourceFilter.namespace) {
        filters.resourceNamespaces = [resourceFilter.namespace];
      }
      if (resourceFilter.name) {
        filters.resourceName = resourceFilter.name;
      }
    }

    return filters;
  }, [resourceFilter]);

  // Create a stable key for the filter to detect changes
  const filterKey = useMemo(() => {
    return JSON.stringify(resourceFilter);
  }, [resourceFilter]);

  const {
    activities,
    isLoading,
    error,
    hasMore,
    refresh,
    loadMore,
  } = useActivityFeed({
    client,
    initialFilters: activityFilters,
    initialTimeRange: { start: startTime },
    pageSize: limit,
    enableStreaming: false,
  });

  // Auto-execute on mount and when filter changes
  useEffect(() => {
    refresh();
  }, [filterKey]); // eslint-disable-line react-hooks/exhaustive-deps

  // Handle load more click
  const handleLoadMore = useCallback(() => {
    loadMore();
  }, [loadMore]);

  // Build header title from filter
  const headerTitle = buildHeaderTitle(resourceFilter);

  // Check if we have any valid filter criteria
  const hasValidFilter = resourceFilter.uid ||
    resourceFilter.apiGroup ||
    resourceFilter.kind ||
    resourceFilter.namespace ||
    resourceFilter.name;

  return (
    <Card className={cn(compact ? 'p-0 shadow-none border-0' : '', className)}>
      {/* Header */}
      {showHeader && (
        <CardHeader className={cn(compact ? 'px-0 pt-0 pb-3' : 'pb-4')}>
          <CardTitle className="text-base font-semibold text-foreground flex items-center gap-2">
            <svg
              className="w-4 h-4 text-muted-foreground"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            {headerTitle}
          </CardTitle>
        </CardHeader>
      )}

      <CardContent className={cn(compact ? 'p-0' : '')}>
        {/* No filter provided */}
        {!hasValidFilter && (
          <div className="py-12 text-center text-muted-foreground">
            <svg
              className="w-12 h-12 mx-auto mb-3 text-muted-foreground/50"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
              />
            </svg>
            <p className="m-0 text-sm">No resource filter specified</p>
            <p className="text-xs text-muted-foreground mt-1 m-0">
              Provide at least one filter criterion to view resource history
            </p>
          </div>
        )}

        {/* Error Display */}
        {hasValidFilter && <ApiErrorAlert error={error} onRetry={refresh} className="mb-4" errorFormatter={errorFormatter} />}

        {/* Loading state (initial) */}
        {hasValidFilter && isLoading && activities.length === 0 && (
          <div className="flex items-center justify-center gap-3 py-12 text-muted-foreground text-sm">
            <div className="w-5 h-5 border-[3px] border-muted border-t-primary rounded-full animate-spin" />
            <span>Loading history...</span>
          </div>
        )}

        {/* Empty state */}
        {hasValidFilter && !isLoading && activities.length === 0 && !error && (
          <div className="py-12 text-center text-muted-foreground">
            <svg
              className="w-12 h-12 mx-auto mb-3 text-muted-foreground/50"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"
              />
            </svg>
            <p className="m-0 text-sm">No history found for this resource</p>
            <p className="text-xs text-muted-foreground mt-1 m-0">
              Changes will appear here once activity policies are configured
            </p>
          </div>
        )}

        {/* Timeline */}
        {hasValidFilter && activities.length > 0 && (
          <div className="relative">
            {activities.map((activity, index) => {
              const activityId = activity.metadata?.uid || activity.metadata?.name || String(index);
              return (
                <ActivityFeedItem
                  key={activityId}
                  activity={activity}
                  variant="timeline"
                  compact={compact}
                  isLast={index === activities.length - 1 && !hasMore}
                  onActivityClick={onActivityClick}
                  onResourceClick={onResourceClick}
                  resourceLinkResolver={resourceLinkResolver}
                  onActorClick={onActorClick}
                />
              );
            })}

            {/* Loading more indicator */}
            {isLoading && activities.length > 0 && (
              <div className={cn('relative', compact ? 'pl-8' : 'pl-10')}>
                <div className="flex items-center gap-3 py-4 text-muted-foreground text-sm">
                  <div className="w-4 h-4 border-2 border-muted border-t-primary rounded-full animate-spin" />
                  <span>Loading more...</span>
                </div>
              </div>
            )}

            {/* Load more button */}
            {hasMore && !isLoading && (
              <div className={cn('relative', compact ? 'pl-8' : 'pl-10')}>
                {/* Continue timeline track */}
                <div
                  className={cn(
                    'absolute w-0.5 bg-border',
                    compact ? 'left-[11px] top-0 h-4' : 'left-[15px] top-0 h-5'
                  )}
                />
                <Button
                  type="quaternary" theme="borderless"
                  size="small"
                  onClick={handleLoadMore}
                  className="text-muted-foreground hover:text-foreground mt-2"
                >
                  Load more history
                </Button>
              </div>
            )}
          </div>
        )}

        {/* Summary footer */}
        {hasValidFilter && activities.length > 0 && (
          <div className={cn(
            'text-xs text-muted-foreground mt-4 pt-3 border-t border-border',
            compact ? '' : ''
          )}>
            Showing {activities.length} event{activities.length !== 1 ? 's' : ''}
            {hasMore && ' (more available)'}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
