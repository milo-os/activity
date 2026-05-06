import { useState, useEffect, useRef, useCallback } from "react";
import { formatISO, subDays } from "date-fns";
import {
  AuditLogFilters,
  buildAuditLogCEL,
  type AuditLogFilterState,
  type TimeRange,
} from "./AuditLogFilters";
import { AuditLogFeedItem, AUDIT_LOG_COLUMN_COUNT } from "./AuditLogFeedItem";
import { useAuditLogQuery } from "../hooks/useAuditLogQuery";
import type { AuditLogQuerySpec, Event } from "../types";
import type { ActivityApiClient } from "../api/client";
import type { ErrorFormatter } from "../types/activity";
import { Card } from "@datum-cloud/datum-ui/card";
import { Button } from "@datum-cloud/datum-ui/button";
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
import { TooltipProvider } from "./ui/tooltip";

// Debounce delay for filter changes (ms)
const FILTER_DEBOUNCE_MS = 300;

// Default page size for infinite scroll
const DEFAULT_PAGE_SIZE = 100;

export interface AuditLogQueryComponentProps {
  client: ActivityApiClient;
  className?: string;
  onEventSelect?: (event: Event) => void;
  initialFilters?: AuditLogFilterState;
  initialTimeRange?: TimeRange;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
  /** Layout variant: 'feed' (table, default) or 'timeline' (icon-list rows) */
  variant?: 'feed' | 'timeline';
}

/**
 * Complete audit log query component with filter builder and results viewer
 */
export function AuditLogQueryComponent({
  client,
  className = "",
  onEventSelect,
  initialFilters = {},
  initialTimeRange = {
    start: formatISO(subDays(new Date(), 1)),
    end: formatISO(new Date()),
  },
  errorFormatter,
  variant = 'feed',
}: AuditLogQueryComponentProps) {
  const [filters, setFilters] = useState<AuditLogFilterState>(initialFilters);
  const [timeRange, setTimeRange] = useState<TimeRange>(initialTimeRange);

  const { events, isLoading, error, hasMore, executeQuery, loadMore } =
    useAuditLogQuery({ client });

  const scrollContainerRef = useRef<HTMLDivElement>(null);
  // Store the latest loadMore function in a ref to avoid observer re-subscription
  const loadMoreRef = useRef(loadMore);
  const filterDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hasInitialLoadRef = useRef(false);

  // Build query spec from current filter state
  const buildQuerySpec = useCallback((): AuditLogQuerySpec => {
    const spec: AuditLogQuerySpec = {
      filter: buildAuditLogCEL(filters) || "",
      startTime: timeRange.start,
      endTime: timeRange.end,
      limit: DEFAULT_PAGE_SIZE,
    };
    return spec;
  }, [filters, timeRange]);

  // Execute query with current filters
  const refresh = useCallback(async () => {
    const spec = buildQuerySpec();
    await executeQuery(spec);
    hasInitialLoadRef.current = true;
  }, [buildQuerySpec, executeQuery]);

  // Handle filter changes with debounced auto-refresh
  const handleFiltersChange = useCallback((newFilters: AuditLogFilterState) => {
    setFilters(newFilters);

    // Cancel any pending debounced refresh
    if (filterDebounceRef.current) {
      clearTimeout(filterDebounceRef.current);
    }

    // Debounce the refresh to avoid excessive API calls
    filterDebounceRef.current = setTimeout(() => {
      filterDebounceRef.current = null;
    }, FILTER_DEBOUNCE_MS);
  }, []);

  // Handle time range changes with debounced auto-refresh
  const handleTimeRangeChange = useCallback((newTimeRange: TimeRange) => {
    setTimeRange(newTimeRange);

    // Cancel any pending debounced refresh
    if (filterDebounceRef.current) {
      clearTimeout(filterDebounceRef.current);
    }

    // Debounce the refresh
    filterDebounceRef.current = setTimeout(() => {
      filterDebounceRef.current = null;
    }, FILTER_DEBOUNCE_MS);
  }, []);

  // Auto-refresh when filters or time range change (debounced)
  useEffect(() => {
    // Skip the initial render - we'll handle that separately
    if (!hasInitialLoadRef.current) {
      return;
    }

    // Cancel any pending refresh
    if (filterDebounceRef.current) {
      clearTimeout(filterDebounceRef.current);
    }

    // Debounce the refresh
    filterDebounceRef.current = setTimeout(() => {
      filterDebounceRef.current = null;
      refresh();
    }, FILTER_DEBOUNCE_MS);

    return () => {
      if (filterDebounceRef.current) {
        clearTimeout(filterDebounceRef.current);
        filterDebounceRef.current = null;
      }
    };
  }, [filters, timeRange, refresh]);

  // Auto-execute on mount
  useEffect(() => {
    refresh();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Update the ref whenever loadMore changes
  useEffect(() => {
    loadMoreRef.current = loadMore;
  }, [loadMore]);

  // Audit log results use manual "Load more" pagination for consistency
  // with the activity feed; the previous IntersectionObserver-driven
  // infinite scroll caused observer rebuild loops on isLoading toggles.

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (filterDebounceRef.current) {
        clearTimeout(filterDebounceRef.current);
      }
    };
  }, []);

  return (
    <Card className={`flex-1 min-h-0 flex flex-col p-3 gap-0 ${className}`}>
      {/* Filters */}
      <AuditLogFilters
        client={client}
        filters={filters}
        timeRange={timeRange}
        onFiltersChange={handleFiltersChange}
        onTimeRangeChange={handleTimeRangeChange}
        disabled={isLoading}
      />

      {/* Error Display */}
      <ApiErrorAlert
        error={error}
        onRetry={refresh}
        className="mb-4"
        errorFormatter={errorFormatter}
      />

      {/* Event List with Infinite Scroll */}
      <div
        className="flex-1 min-h-0 overflow-y-auto pr-2"
        ref={scrollContainerRef}
      >
        <TooltipProvider delayDuration={200}>
          {variant === 'timeline' ? (
            <div>
              {isLoading && events.length === 0
                ? Array.from({ length: 8 }).map((_, index) => (
                    <div
                      key={`sk-${index}`}
                      className={cn(index < 7 && 'border-b border-border', 'py-3')}
                    >
                      <div className="flex items-center gap-3">
                        <Skeleton className="w-8 h-8 rounded-md shrink-0" />
                        <Skeleton className="h-4 flex-1 min-w-0" />
                        <Skeleton className="h-4 w-12 shrink-0" />
                        <Skeleton className="h-4 w-24 shrink-0" />
                        <Skeleton className="h-5 w-5 shrink-0" />
                      </div>
                    </div>
                  ))
                : null}
              {!isLoading && events.length === 0 && !error ? (
                <div className="py-12 text-center text-muted-foreground">
                  <div>No audit events found</div>
                  <div className="text-sm mt-2">
                    Try adjusting your filters or time range
                  </div>
                </div>
              ) : null}
              {events.map((event, index) => (
                <AuditLogFeedItem
                  key={event.auditID || `event-${index}`}
                  event={event}
                  onEventClick={onEventSelect}
                  variant="timeline"
                  isLast={index === events.length - 1}
                />
              ))}
            </div>
          ) : (
            <Table className="w-full">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[110px]">Verb</TableHead>
                  <TableHead>Summary</TableHead>
                  <TableHead className="w-[90px]">Status</TableHead>
                  <TableHead className="w-[170px]">When</TableHead>
                  <TableHead className="w-10" aria-label="Expand" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading && events.length === 0
                  ? Array.from({ length: 8 }).map((_, index) => (
                      <TableRow key={`sk-${index}`}>
                        <TableCell className="py-2">
                          <Skeleton className="h-5 w-16 rounded-full" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-3/4" />
                        </TableCell>
                        <TableCell className="py-2">
                          <Skeleton className="h-4 w-12" />
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
                {!isLoading && events.length === 0 && !error ? (
                  <TableRow>
                    <TableCell
                      colSpan={AUDIT_LOG_COLUMN_COUNT}
                      className="text-center py-12 text-muted-foreground"
                    >
                      <div>No audit events found</div>
                      <div className="text-sm mt-2">
                        Try adjusting your filters or time range
                      </div>
                    </TableCell>
                  </TableRow>
                ) : null}
                {events.map((event, index) => (
                  <AuditLogFeedItem
                    key={event.auditID || `event-${index}`}
                    event={event}
                    onEventClick={onEventSelect}
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
                onClick={() => loadMore()}
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
