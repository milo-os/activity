import { useEffect, useRef, useCallback, useState } from "react";
import type {
  Activity,
  ResourceRef,
  ResourceLinkResolver,
  TenantLinkResolver,
  TenantRenderer,
  EffectiveTimeRangeCallback,
  ErrorFormatter,
} from "../types/activity";
import type {
  ActivityFeedFilters as FilterState,
  TimeRange,
} from "../hooks/useActivityFeed";
import { useActivityFeed } from "../hooks/useActivityFeed";
import {
  ActivityFeedItem,
  ACTIVITY_FEED_COLUMN_COUNT,
} from "./ActivityFeedItem";
import { ActivityFeedItemSkeleton } from "./ActivityFeedItemSkeleton";
import { Skeleton } from "@datum-cloud/datum-ui/skeleton";
import { ActivityFeedFilters } from "./ActivityFeedFilters";
import { ActivityApiClient } from "../api/client";
import { Button } from "@datum-cloud/datum-ui/button";
import { Card } from "@datum-cloud/datum-ui/card";
import { Badge } from "./ui/badge";
import { ApiErrorAlert } from "./ApiErrorAlert";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./ui/tooltip";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@datum-cloud/datum-ui/table";

export interface ActivityFeedProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Initial filter settings */
  initialFilters?: FilterState;
  /** Initial time range */
  initialTimeRange?: TimeRange;
  /** Number of items per page */
  pageSize?: number;
  /** Handler called when a resource link is clicked (deprecated: use resourceLinkResolver) */
  onResourceClick?: (resource: ResourceRef) => void;
  /** Function that resolves resource references to URLs */
  resourceLinkResolver?: ResourceLinkResolver;
  /** Function that resolves tenant references to URLs */
  tenantLinkResolver?: TenantLinkResolver;
  /** Custom renderer for tenant badges (overrides default TenantBadge) */
  tenantRenderer?: TenantRenderer;
  /** Handler called when an activity is clicked */
  onActivityClick?: (activity: Activity) => void;
  /** Whether to show in compact mode (for resource detail tabs) */
  compact?: boolean;
  /** Layout variant for activity items: 'feed' (default) or 'timeline' */
  variant?: "feed" | "timeline";
  /** Filter to a specific resource UID */
  resourceUid?: string;
  /** Whether to show filters */
  showFilters?: boolean;
  /** Filters that should be locked and hidden from the UI (programmatically set by parent) */
  hiddenFilters?: Array<
    | "resourceKinds"
    | "actorNames"
    | "apiGroups"
    | "resourceNamespaces"
    | "resourceName"
    | "changeSource"
  >;
  /** Additional CSS class */
  className?: string;
  /** Enable infinite scroll (default: true) */
  infiniteScroll?: boolean;
  /** Threshold in pixels for triggering load more (default: 200) */
  loadMoreThreshold?: number;
  /** Handler called when user wants to create a policy (for empty state) */
  onCreatePolicy?: () => void;
  /** Enable real-time streaming (default: false) */
  enableStreaming?: boolean;
  /** Callback invoked when the effective time range is resolved */
  onEffectiveTimeRangeChange?: EffectiveTimeRangeCallback;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
  /**
   * Maximum height for the scroll container (CSS value like '500px' or 'calc(100vh - 300px)').
   * By default, the component uses flex layout (flex-1 min-h-0) which adapts to parent container constraints.
   * Only set this if your parent container doesn't have proper height constraints.
   * Set to 'none' to explicitly disable any max-height constraint.
   */
  maxHeight?: string;
  /** Callback invoked when filters or time range change (useful for URL state management) */
  onFiltersChange?: (filters: FilterState, timeRange: TimeRange) => void;
}

/**
 * ActivityFeed displays a chronological list of activities with filtering and pagination.
 * Supports optional real-time streaming of new activities.
 */
export function ActivityFeed({
  client,
  initialFilters = { changeSource: "human" },
  initialTimeRange = { start: "now-7d" },
  pageSize = 30,
  onResourceClick,
  resourceLinkResolver,
  tenantLinkResolver,
  tenantRenderer,
  onActivityClick,
  compact = false,
  variant = "feed",
  resourceUid,
  showFilters = true,
  hiddenFilters = [],
  className = "",
  infiniteScroll = true,
  loadMoreThreshold = 200,
  onCreatePolicy,
  enableStreaming = false,
  onEffectiveTimeRangeChange,
  errorFormatter,
  maxHeight,
  onFiltersChange: onFiltersChangeProp,
}: ActivityFeedProps) {
  // Merge resourceUid into initial filters if provided
  const mergedInitialFilters: FilterState = {
    ...initialFilters,
    resourceUid: resourceUid || initialFilters.resourceUid,
  };

  const {
    activities,
    isLoading,
    error,
    watchError,
    hasMore,
    filters,
    timeRange,
    refresh,
    loadMore,
    setFilters,
    setTimeRange,
    isStreaming,
    startStreaming,
    stopStreaming,
    newActivitiesCount,
  } = useActivityFeed({
    client,
    initialFilters: mergedInitialFilters,
    initialTimeRange,
    pageSize,
    enableStreaming,
    autoStartStreaming: true,
    onEffectiveTimeRangeChange,
  });

  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const loadMoreTriggerRef = useRef<HTMLDivElement>(null);
  // Store the latest loadMore function in a ref to avoid observer re-subscription
  const loadMoreRef = useRef(loadMore);

  // Track whether policies exist in the system
  const [hasPolicies, setHasPolicies] = useState<boolean | null>(null);
  const [policiesLoading, setPoliciesLoading] = useState(true);

  // Check for policies on mount
  useEffect(() => {
    const checkPolicies = async () => {
      try {
        const policyList = await client.listPolicies();
        setHasPolicies((policyList.items?.length ?? 0) > 0);
      } catch {
        // If we can't check policies, assume they might exist
        setHasPolicies(true);
      } finally {
        setPoliciesLoading(false);
      }
    };
    checkPolicies();
  }, [client]);

  // Auto-execute on mount
  useEffect(() => {
    refresh();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Update the ref whenever loadMore changes
  useEffect(() => {
    loadMoreRef.current = loadMore;
  }, [loadMore]);

  // Mirror isLoading into a ref so the IntersectionObserver callback can
  // read the latest value without listing it as an effect dep. Listing
  // isLoading caused the observer to tear down and rebuild on every
  // isLoading toggle; the rebuilt observer fired immediately when the
  // trigger element was in the viewport, which in turn called loadMore()
  // and toggled isLoading again — a cycle that disabled the toolbar
  // repeatedly until items filled the viewport.
  //
  // hasMore *is* a dep, because the trigger element is conditionally
  // rendered (`{infiniteScroll && hasMore && <div ref={...} />}`). When
  // hasMore flips false→true after the initial fetch we need the effect
  // to re-run so the observer attaches to the now-mounted trigger.
  const isLoadingRef = useRef(isLoading);
  useEffect(() => {
    isLoadingRef.current = isLoading;
  }, [isLoading]);

  // Infinite scroll using Intersection Observer
  useEffect(() => {
    if (!infiniteScroll || !hasMore || !loadMoreTriggerRef.current) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (entry.isIntersecting && !isLoadingRef.current) {
          // Call through the ref to always use the latest function
          loadMoreRef.current();
        }
      },
      {
        root: scrollContainerRef.current,
        rootMargin: `${loadMoreThreshold}px`,
        threshold: 0,
      },
    );

    observer.observe(loadMoreTriggerRef.current);

    return () => {
      observer.disconnect();
    };
  }, [infiniteScroll, loadMoreThreshold, hasMore]);

  // Handle filter changes - refresh is automatic via the hook
  const handleFiltersChange = useCallback(
    (newFilters: FilterState) => {
      setFilters(newFilters);
      onFiltersChangeProp?.(newFilters, timeRange);
    },
    [setFilters, onFiltersChangeProp, timeRange],
  );

  // Handle time range changes - refresh is automatic via the hook
  const handleTimeRangeChange = useCallback(
    (newTimeRange: TimeRange) => {
      setTimeRange(newTimeRange);
      onFiltersChangeProp?.(filters, newTimeRange);
    },
    [setTimeRange, onFiltersChangeProp, filters],
  );

  // Handle manual load more click
  const handleLoadMoreClick = useCallback(() => {
    loadMore();
  }, [loadMore]);

  // Handle streaming toggle
  const handleStreamingToggle = useCallback(() => {
    if (isStreaming) {
      stopStreaming();
    } else {
      startStreaming();
    }
  }, [isStreaming, startStreaming, stopStreaming]);

  // Handle actor click - filter by actor name
  const handleActorClick = useCallback(
    (actorName: string) => {
      setFilters({
        ...filters,
        actorNames: [actorName],
      });
    },
    [filters, setFilters],
  );

  // Build container classes - use flex layout to properly fill available space
  // flex-1 min-h-0 allows the Card to fill parent flex container and enable child scrolling
  const containerClasses = compact
    ? `flex-1 min-h-0 flex flex-col p-3 shadow-none border-none gap-0 ${className}`
    : `flex-1 min-h-0 flex flex-col p-3 gap-0 ${className}`;

  // Build list classes - use flex-1 min-h-0 for flex-based scrolling
  // Parent containers must have proper height constraints (h-screen/h-full + overflow-hidden)
  const effectiveMaxHeight = maxHeight === "none" ? undefined : maxHeight;
  const listClasses = "flex-1 min-h-0 overflow-y-auto flex flex-col";

  return (
    <TooltipProvider delayDuration={200}>
      <Card className={containerClasses}>
        {/* Header with streaming status */}
        {enableStreaming && (
          <div className="flex items-center justify-between">
            <div className="flex items-center">
              {isStreaming && !watchError && (
                <TooltipProvider delayDuration={300}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="flex items-center gap-2">
                        <span className="relative flex h-2 w-2">
                          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 dark:bg-green-500 opacity-75"></span>
                          <span className="relative inline-flex rounded-full h-2 w-2 bg-green-500 dark:bg-green-400"></span>
                        </span>
                        <span className="text-xs text-muted-foreground">
                          Streaming activity...
                        </span>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent className="text-xs">
                      <p>New activities will appear automatically</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              )}
              {watchError && (
                <TooltipProvider delayDuration={300}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="flex items-center gap-2">
                        <span className="relative flex h-2 w-2">
                          <span className="relative inline-flex rounded-full h-2 w-2 bg-red-500 dark:bg-red-400"></span>
                        </span>
                        <span className="text-xs text-destructive">
                          Connection error
                        </span>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent className="text-xs">
                      <p>Stream connection lost</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              )}
              {newActivitiesCount > 0 && !watchError && (
                <Badge variant="secondary" className="text-xs">
                  +{newActivitiesCount} new
                </Badge>
              )}
            </div>
            <Button type="tertiary" theme="outline" size="small" onClick={handleStreamingToggle}>
              {watchError ? (
                <>
                  <svg
                    className="w-4 h-4 mr-1.5"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                    strokeWidth="2"
                  >
                    <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
                    <circle cx="12" cy="12" r="3" />
                  </svg>
                  Retry
                </>
              ) : isStreaming ? (
                <>
                  <svg
                    className="w-4 h-4 mr-1.5"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <rect x="6" y="4" width="4" height="16" />
                    <rect x="14" y="4" width="4" height="16" />
                  </svg>
                  Pause
                </>
              ) : (
                <>
                  <svg
                    className="w-4 h-4 mr-1.5"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <polygon points="5,3 19,12 5,21" fill="currentColor" />
                  </svg>
                  Resume
                </>
              )}
            </Button>
          </div>
        )}

        {/* Filters */}
        {showFilters && (
          <ActivityFeedFilters
            client={client}
            filters={filters}
            timeRange={timeRange}
            onFiltersChange={handleFiltersChange}
            onTimeRangeChange={handleTimeRangeChange}
            disabled={isLoading}
            hiddenFilters={hiddenFilters}
          />
        )}

        {/* Query Error Display */}
        <ApiErrorAlert
          error={error}
          onRetry={refresh}
          className="mb-4"
          errorFormatter={errorFormatter}
        />

        {/* Watch Stream Error Display */}
        <ApiErrorAlert
          error={watchError}
          onRetry={startStreaming}
          className="mb-4"
          errorFormatter={errorFormatter}
        />

        {/* No Policies Empty State */}
        {!policiesLoading && hasPolicies === false && (
          <div className="flex flex-col items-center py-12 px-8 text-center bg-muted border border-dashed border-border rounded-xl mb-4">
            <div className="flex justify-center mb-4 text-muted-foreground">
              <svg
                width="56"
                height="56"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2" />
                <rect x="9" y="3" width="6" height="4" rx="1" />
                <path d="M9 12h6" />
                <path d="M9 16h6" />
              </svg>
            </div>
            <h3 className="m-0 mb-2 text-lg font-semibold text-foreground leading-snug">
              Get started with activity logging
            </h3>
            <p className="m-0 mb-6 text-sm leading-relaxed text-muted-foreground max-w-[400px]">
              Activity policies define which resources to track and how to
              summarize changes. Create your first policy to start seeing
              activity logs here.
            </p>
            {onCreatePolicy && (
              <Button onClick={onCreatePolicy}>Create Policy</Button>
            )}
          </div>
        )}

        {/* Activity List */}
        <div
          className={listClasses}
          ref={scrollContainerRef}
          style={
            effectiveMaxHeight ? { maxHeight: effectiveMaxHeight } : undefined
          }
        >
          {/* Empty State - only show when not loading and we're using the
            feed variant. Empty state for the table case is rendered as an
            in-table message row below. */}
          {!isLoading &&
            activities.length === 0 &&
            hasPolicies !== false &&
            variant === "timeline" && (
              <div className="py-12 text-center text-muted-foreground">
                <p className="m-0">No activities found</p>
                <p className="text-sm text-muted-foreground mt-2 m-0">
                  Try adjusting your filters or time range
                </p>
              </div>
            )}

          {variant === "timeline" ? (
            <>
              {isLoading && activities.length === 0 && (
                <>
                  {Array.from({ length: 8 }).map((_, index) => (
                    <ActivityFeedItemSkeleton key={index} compact={compact} />
                  ))}
                </>
              )}
              {activities.map((activity, index) => (
                <ActivityFeedItem
                  key={activity.metadata?.uid || activity.metadata?.name}
                  activity={activity}
                  onResourceClick={onResourceClick}
                  resourceLinkResolver={resourceLinkResolver}
                  tenantLinkResolver={tenantLinkResolver}
                  tenantRenderer={tenantRenderer}
                  onActorClick={handleActorClick}
                  onActivityClick={onActivityClick}
                  compact={compact}
                  isNew={enableStreaming && index < newActivitiesCount}
                  variant={variant}
                  isLast={index === activities.length - 1}
                />
              ))}
            </>
          ) : (
            <Table className="w-full">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12" aria-label="Actor" />
                  <TableHead>Summary</TableHead>
                  <TableHead>Tenant</TableHead>
                  <TableHead className="w-[170px]">When</TableHead>
                  <TableHead className="w-10" aria-label="Expand" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading && activities.length === 0 && (
                  <>
                    {Array.from({ length: 8 }).map((_, index) => (
                      <TableRow key={`sk-${index}`}>
                        {/* Standard per-cell skeleton row, matching the
                            shape of the real columns: avatar, summary,
                            tenant, when, expand. */}
                        <TableCell className="py-2">
                          <Skeleton className="h-6 w-6 rounded-full" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-3/4" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-5 w-32 rounded-full" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-24" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-6 w-6" />
                        </TableCell>
                      </TableRow>
                    ))}
                  </>
                )}
                {!isLoading &&
                  activities.length === 0 &&
                  hasPolicies !== false && (
                    <TableRow>
                      <TableCell
                        colSpan={ACTIVITY_FEED_COLUMN_COUNT}
                        className="text-center py-12 text-muted-foreground"
                      >
                        <div>No activities found</div>
                        <div className="text-sm mt-2">
                          Try adjusting your filters or time range
                        </div>
                      </TableCell>
                    </TableRow>
                  )}
                {activities.map((activity, index) => (
                  <ActivityFeedItem
                    key={activity.metadata?.uid || activity.metadata?.name}
                    activity={activity}
                    onResourceClick={onResourceClick}
                    resourceLinkResolver={resourceLinkResolver}
                    tenantLinkResolver={tenantLinkResolver}
                    tenantRenderer={tenantRenderer}
                    onActorClick={handleActorClick}
                    onActivityClick={onActivityClick}
                    compact={compact}
                    isNew={enableStreaming && index < newActivitiesCount}
                    variant={variant}
                    isLast={index === activities.length - 1}
                  />
                ))}
              </TableBody>
            </Table>
          )}

          {/* Load More Trigger for Infinite Scroll */}
          {infiniteScroll && hasMore && (
            <div ref={loadMoreTriggerRef} className="h-px mt-4" />
          )}

          {/* Manual pagination footer: shown for both table and timeline
              variants when infinite scroll is disabled. Mirrors the look
              of other staff-portal data tables — count on the left,
              action on the right. */}
          {!infiniteScroll && activities.length > 0 ? (
            <div className="flex items-center justify-between gap-4 px-4 py-3 border-t border-border text-sm text-muted-foreground">
              <span>
                {activities.length}{" "}
                {activities.length === 1 ? "activity" : "activities"}
                {hasMore ? " so far" : ""}
              </span>
              {hasMore ? (
                <Button
                  type="tertiary" theme="outline"
                  size="small"
                  htmlType="button"
                  onClick={handleLoadMoreClick}
                  disabled={isLoading}
                >
                  {isLoading ? "Loading…" : "Load more"}
                </Button>
              ) : (
                <span>End of results</span>
              )}
            </div>
          ) : null}

        </div>
      </Card>
    </TooltipProvider>
  );
}
