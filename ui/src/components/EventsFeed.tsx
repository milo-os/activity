import { useEffect, useRef, useCallback } from "react";
import type { K8sEvent } from "../types/k8s-event";
import type {
  EffectiveTimeRangeCallback,
  ErrorFormatter,
} from "../types/activity";
import type {
  EventsFeedFilters as FilterState,
  TimeRange,
} from "../hooks/useEventsFeed";
import { useEventsFeed } from "../hooks/useEventsFeed";
import { EventFeedItem, EVENT_COLUMN_COUNT } from "./EventFeedItem";
import { EventsFeedFilters } from "./EventsFeedFilters";
import { ActivityApiClient } from "../api/client";
import { Button } from "@datum-cloud/datum-ui/button";
import { Card } from "@datum-cloud/datum-ui/card";
import { Badge } from "./ui/badge";
import { Skeleton } from "@datum-cloud/datum-ui/skeleton";
import { ApiErrorAlert } from "./ApiErrorAlert";
import { cn } from "../lib/utils";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@datum-cloud/datum-ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./ui/tooltip";

export interface EventsFeedProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Initial filter settings */
  initialFilters?: FilterState;
  /** Initial time range */
  initialTimeRange?: TimeRange;
  /** Number of items per page */
  pageSize?: number;
  /** Handler called when an event is clicked */
  onEventClick?: (event: K8sEvent) => void;
  /** Handler called when a resource name is clicked. If provided, resource names become clickable. */
  onResourceClick?: (resource: {
    kind: string;
    name: string;
    namespace?: string;
    uid?: string;
  }) => void;
  /** Whether to show in compact mode (for resource detail tabs) */
  compact?: boolean;
  /** Filter to a specific namespace */
  namespace?: string;
  /** Whether to show filters */
  showFilters?: boolean;
  /** Filters that should be locked and hidden from the UI (programmatically set by parent) */
  hiddenFilters?: Array<
    | "involvedKinds"
    | "reasons"
    | "namespaces"
    | "sourceComponents"
    | "involvedName"
    | "eventType"
  >;
  /** Additional CSS class */
  className?: string;
  /** Enable infinite scroll (default: true) */
  infiniteScroll?: boolean;
  /** Threshold in pixels for triggering load more (default: 200) */
  loadMoreThreshold?: number;
  /** Enable real-time streaming (default: false) */
  enableStreaming?: boolean;
  /** Callback invoked when the effective time range is resolved */
  onEffectiveTimeRangeChange?: EffectiveTimeRangeCallback;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
  /** Callback invoked when filters or time range change (useful for URL state management) */
  onFiltersChange?: (filters: FilterState, timeRange: TimeRange) => void;
  /** Layout variant: 'feed' (table, default) or 'timeline' (icon-list rows) */
  variant?: 'feed' | 'timeline';
}

/**
 * EventsFeed displays a chronological list of Kubernetes events with filtering and pagination.
 * Supports optional real-time streaming of new events.
 */
export function EventsFeed({
  client,
  initialFilters = {},
  initialTimeRange = { start: "now-24h" },
  pageSize = 50,
  onEventClick,
  onResourceClick,
  compact = false,
  namespace,
  showFilters = true,
  hiddenFilters = [],
  className = "",
  enableStreaming = false,
  errorFormatter,
  onFiltersChange: onFiltersChangeProp,
  variant = 'feed',
}: EventsFeedProps) {
  // Merge namespace into initial filters if provided
  const mergedInitialFilters: FilterState = {
    ...initialFilters,
  };

  const {
    events,
    isLoading,
    isRefreshing,
    error,
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
    newEventsCount,
  } = useEventsFeed({
    client,
    initialFilters: mergedInitialFilters,
    initialTimeRange,
    pageSize,
    namespace,
    enableStreaming,
    autoStartStreaming: true,
  });

  const scrollContainerRef = useRef<HTMLDivElement>(null);

  // Store the latest loadMore function in a ref to avoid observer re-subscription
  const loadMoreRef = useRef(loadMore);

  // Auto-execute on mount
  useEffect(() => {
    refresh();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Update the ref whenever loadMore changes
  useEffect(() => {
    loadMoreRef.current = loadMore;
  }, [loadMore]);

  // EventsFeed uses manual "Load more" pagination for consistency with the
  // activity feed; the previous IntersectionObserver-driven infinite
  // scroll caused observer rebuild loops on isLoading toggles.

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

  // Build container classes - use flex layout to properly fill available space
  // flex-1 min-h-0 allows the Card to fill parent flex container and enable child scrolling
  const containerClasses = compact
    ? `flex-1 min-h-0 flex flex-col p-2 shadow-none border-border gap-0 ${className}`
    : `flex-1 min-h-0 flex flex-col p-3 gap-0 ${className}`;

  // Build list classes - use flex-1 min-h-0 for flex-based scrolling
  const listClasses = "flex-1 min-h-0 overflow-y-auto pr-2";

  return (
    <Card className={containerClasses}>
      {/* Header with streaming status — matches ActivityFeed: no border,
          tooltipped indicator, outlined Pause/Resume button. */}
      {enableStreaming && (
        <div className="flex items-center justify-between">
          <div className="flex items-center">
            {isStreaming ? (
              <TooltipProvider delayDuration={300}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="flex items-center gap-2">
                      <span className="relative flex h-2 w-2">
                        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 dark:bg-green-500 opacity-75" />
                        <span className="relative inline-flex rounded-full h-2 w-2 bg-green-500 dark:bg-green-400" />
                      </span>
                      <span className="text-xs text-muted-foreground">
                        Streaming events...
                      </span>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent className="text-xs">
                    <p>New events will appear automatically</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            ) : null}
            {newEventsCount > 0 ? (
              <Badge variant="secondary" className="text-xs ml-2">
                +{newEventsCount} new
              </Badge>
            ) : null}
          </div>
          <Button type="tertiary" theme="outline" size="small" onClick={handleStreamingToggle}>
            {isStreaming ? (
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
        <EventsFeedFilters
          client={client}
          filters={filters}
          timeRange={timeRange}
          onFiltersChange={handleFiltersChange}
          onTimeRangeChange={handleTimeRangeChange}
          disabled={isLoading}
          namespace={namespace}
          hiddenFilters={hiddenFilters}
        />
      )}

      {/* Error Display */}
      <ApiErrorAlert
        error={error}
        onRetry={refresh}
        className="mb-4"
        errorFormatter={errorFormatter}
      />

      {/* Event List */}
      <div className={listClasses} ref={scrollContainerRef}>
        <TooltipProvider delayDuration={200}>
          {variant === 'timeline' ? (
            <div>
              {(isLoading || isRefreshing) && events.length === 0
                ? Array.from({ length: 8 }).map((_, index) => (
                    <div
                      key={`sk-${index}`}
                      className={cn(
                        index < 7 && 'border-b border-border',
                        compact ? 'py-2' : 'py-3'
                      )}
                    >
                      <div className="flex items-center gap-3">
                        <Skeleton className="w-8 h-8 rounded-md shrink-0" />
                        <div className="flex-1 min-w-0 flex flex-col gap-1">
                          <Skeleton className="h-4 w-24" />
                          <Skeleton className="h-3 w-3/4" />
                        </div>
                        <Skeleton className="h-4 w-32 shrink-0" />
                        <Skeleton className="h-4 w-24 shrink-0" />
                        <Skeleton className="h-5 w-5 shrink-0" />
                      </div>
                    </div>
                  ))
                : null}
              {!isLoading && !isRefreshing && events.length === 0 ? (
                <div className="py-12 text-center text-muted-foreground">
                  <div>No events found</div>
                  <div className="text-sm mt-2">
                    Try adjusting your filters or time range
                  </div>
                </div>
              ) : null}
              {events.map((event, index) => (
                <EventFeedItem
                  key={event.metadata?.uid || event.metadata?.name}
                  event={event}
                  onEventClick={onEventClick}
                  onResourceClick={onResourceClick}
                  compact={compact}
                  isNew={enableStreaming && index < newEventsCount}
                  variant="timeline"
                  isLast={index === events.length - 1}
                />
              ))}
            </div>
          ) : (
            <Table className="w-full">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[90px]">Type</TableHead>
                  <TableHead className="w-[160px]">Reason</TableHead>
                  <TableHead>Note</TableHead>
                  <TableHead className="w-[220px]">Object</TableHead>
                  <TableHead className="w-[170px]">When</TableHead>
                  <TableHead className="w-10" aria-label="Expand" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {(isLoading || isRefreshing) && events.length === 0
                  ? Array.from({ length: 8 }).map((_, index) => (
                      <TableRow key={`sk-${index}`}>
                        <TableCell className="py-2">
                          <Skeleton className="h-5 w-14 rounded" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-24" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-3/4" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-32" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-24" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-6 w-6" />
                        </TableCell>
                      </TableRow>
                    ))
                  : null}
                {!isLoading && !isRefreshing && events.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={EVENT_COLUMN_COUNT}
                      className="text-center py-12 text-muted-foreground"
                    >
                      <div>No events found</div>
                      <div className="text-sm mt-2">
                        Try adjusting your filters or time range
                      </div>
                    </TableCell>
                  </TableRow>
                ) : null}
                {events.map((event, index) => (
                  <EventFeedItem
                    key={event.metadata?.uid || event.metadata?.name}
                    event={event}
                    onEventClick={onEventClick}
                    onResourceClick={onResourceClick}
                    compact={compact}
                    isNew={enableStreaming && index < newEventsCount}
                  />
                ))}
              </TableBody>
            </Table>
          )}
        </TooltipProvider>

        {/* Manual pagination footer */}
        {events.length > 0 ? (
          <div className="flex items-center justify-between gap-4 px-4 py-3 border-t border-border text-sm text-muted-foreground">
            <span>
              {events.length} {events.length === 1 ? "event" : "events"}
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
  );
}
