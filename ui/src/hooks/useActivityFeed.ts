import { useState, useCallback, useMemo, useEffect, useRef } from 'react';
import type { Activity, ActivityListParams, ActivityQuerySpec, ChangeSource, WatchEvent, EffectiveTimeRange } from '../types/activity';
import { ActivityApiClient } from '../api/client';
import { StreamError } from '../lib/errors';

// Debounce delay for filter changes (ms)
const FILTER_DEBOUNCE_MS = 300;

/**
 * Filter options for the activity feed
 */
export interface ActivityFeedFilters {
  /** Filter by change source (human/system/all) */
  changeSource?: ChangeSource | 'all';
  /** Full-text search on summaries */
  search?: string;
  /** Filter to a specific resource UID */
  resourceUid?: string;
  /** Filter by resource kinds (multi-select) */
  resourceKinds?: string[];
  /** Filter by actor names (multi-select) */
  actorNames?: string[];
  /** Filter by API groups (multi-select) */
  apiGroups?: string[];
  /** Filter by resource name (partial match) */
  resourceName?: string;
  /** Filter by resource namespaces (multi-select) */
  resourceNamespaces?: string[];
  /** Filter by actions (multi-select) - prepared for future backend support */
  actions?: string[];
  /** Custom CEL filter expression */
  customFilter?: string;
}

/**
 * Time range for the activity feed
 */
export interface TimeRange {
  /** Start of time range (RFC3339 or relative like "now-24h") */
  start: string;
  /** End of time range (RFC3339 or relative, default: now) */
  end?: string;
}

/**
 * Options for the useActivityFeed hook
 */
export interface UseActivityFeedOptions {
  /** API client instance */
  client: ActivityApiClient;
  /** Initial filter settings */
  initialFilters?: ActivityFeedFilters;
  /** Initial time range */
  initialTimeRange?: TimeRange;
  /** Number of items per page (default: 30) */
  pageSize?: number;
  /** Enable real-time streaming (default: false) */
  enableStreaming?: boolean;
  /** Auto-start streaming when enabled (default: true) */
  autoStartStreaming?: boolean;
  /** Callback invoked when the effective time range is resolved */
  onEffectiveTimeRangeChange?: (timeRange: EffectiveTimeRange) => void;
}

/**
 * Result returned by the useActivityFeed hook
 */
export interface UseActivityFeedResult {
  /** List of activities */
  activities: Activity[];
  /** Whether the feed is loading */
  isLoading: boolean;
  /** Error if any occurred */
  error: Error | null;
  /** Watch stream error if any occurred */
  watchError: Error | null;
  /** Whether there are more activities to load */
  hasMore: boolean;
  /** Current filter settings */
  filters: ActivityFeedFilters;
  /** Current time range */
  timeRange: TimeRange;
  /** Execute/refresh the feed query */
  refresh: () => Promise<void>;
  /** Load more activities (pagination) */
  loadMore: () => Promise<void>;
  /** Update filter settings */
  setFilters: (filters: ActivityFeedFilters) => void;
  /** Update time range */
  setTimeRange: (timeRange: TimeRange) => void;
  /** Reset to initial state */
  reset: () => void;
  /** Total count if available */
  totalCount?: number;
  /** Whether streaming is currently active */
  isStreaming: boolean;
  /** Start streaming (when enableStreaming is true) */
  startStreaming: () => void;
  /** Stop streaming */
  stopStreaming: () => void;
  /** Number of new activities received via streaming since last refresh */
  newActivitiesCount: number;
  /** Effective time range after query resolution (undefined until first query completes) */
  effectiveTimeRange?: EffectiveTimeRange;
}

/**
 * Escape a value for safe interpolation into a CEL string literal.
 * Prevents injection via values containing backslashes or double quotes.
 */
function celEscape(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

/**
 * Build CEL filter expression from filter options for ActivityQuery.
 * All filters are expressed as CEL — the API no longer accepts dedicated filter fields.
 */
function buildCelFilter(filters: ActivityFeedFilters): string | undefined {
  const conditions: string[] = [];

  // Change source filter
  if (filters.changeSource && filters.changeSource !== 'all') {
    conditions.push(`spec.changeSource == "${celEscape(filters.changeSource)}"`);
  }

  // Resource UID filter (for resource-specific views)
  if (filters.resourceUid) {
    conditions.push(`spec.resource.uid == "${celEscape(filters.resourceUid)}"`);
  }

  // Resource kinds filter (multi-select)
  if (filters.resourceKinds && filters.resourceKinds.length > 0) {
    if (filters.resourceKinds.length === 1) {
      conditions.push(`spec.resource.kind == "${celEscape(filters.resourceKinds[0])}"`);
    } else {
      const kindConditions = filters.resourceKinds.map((k) => `spec.resource.kind == "${celEscape(k)}"`);
      conditions.push(`(${kindConditions.join(' || ')})`);
    }
  }

  // Actor names filter (multi-select)
  if (filters.actorNames && filters.actorNames.length > 0) {
    if (filters.actorNames.length === 1) {
      conditions.push(`spec.actor.name == "${celEscape(filters.actorNames[0])}"`);
    } else {
      const actorConditions = filters.actorNames.map((a) => `spec.actor.name == "${celEscape(a)}"`);
      conditions.push(`(${actorConditions.join(' || ')})`);
    }
  }

  // API groups filter (multi-select)
  if (filters.apiGroups && filters.apiGroups.length > 0) {
    if (filters.apiGroups.length === 1) {
      conditions.push(`spec.resource.apiGroup == "${celEscape(filters.apiGroups[0])}"`);
    } else {
      const groupConditions = filters.apiGroups.map((g) => `spec.resource.apiGroup == "${celEscape(g)}"`);
      conditions.push(`(${groupConditions.join(' || ')})`);
    }
  }

  // Resource name filter (partial match)
  if (filters.resourceName) {
    conditions.push(`spec.resource.name.contains("${celEscape(filters.resourceName)}")`);
  }

  // Resource namespaces filter (multi-select)
  if (filters.resourceNamespaces && filters.resourceNamespaces.length > 0) {
    if (filters.resourceNamespaces.length === 1) {
      conditions.push(`spec.resource.namespace == "${celEscape(filters.resourceNamespaces[0])}"`);
    } else {
      const nsConditions = filters.resourceNamespaces.map((ns) => `spec.resource.namespace == "${celEscape(ns)}"`);
      conditions.push(`(${nsConditions.join(' || ')})`);
    }
  }

  // Custom filter
  if (filters.customFilter) {
    conditions.push(filters.customFilter);
  }

  return conditions.length > 0 ? conditions.join(' && ') : undefined;
}

/**
 * React hook for fetching and managing the activity feed with optional real-time streaming
 */
export function useActivityFeed({
  client,
  initialFilters = {},
  initialTimeRange = { start: 'now-7d' },
  pageSize = 30,
  enableStreaming = false,
  autoStartStreaming = true,
  onEffectiveTimeRangeChange,
}: UseActivityFeedOptions): UseActivityFeedResult {
  const [activities, setActivities] = useState<Activity[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [watchError, setWatchError] = useState<Error | null>(null);
  const [continueCursor, setContinueCursor] = useState<string | undefined>();
  const [filters, setFilters] = useState<ActivityFeedFilters>(initialFilters);
  const [timeRange, setTimeRange] = useState<TimeRange>(initialTimeRange);
  const [isStreaming, setIsStreaming] = useState(false);
  // Tracks whether the user has explicitly paused streaming. The
  // auto-start and post-filter-restart effects below respect this flag so
  // they don't immediately re-open the watch the user just closed. Cleared
  // when the user clicks Resume.
  const [userPaused, setUserPaused] = useState(false);
  const [newActivitiesCount, setNewActivitiesCount] = useState(0);
  const [effectiveTimeRange, setEffectiveTimeRange] = useState<EffectiveTimeRange | undefined>();

  // Track the latest resource version for watch resume
  const resourceVersionRef = useRef<string | undefined>();
  // Track the watch stop function
  const watchStopRef = useRef<(() => void) | null>(null);
  // Track whether streaming should restart after filter change
  const shouldRestartStreamingRef = useRef(false);
  // Track if we've done the initial load
  const hasInitialLoadRef = useRef(false);
  // Debounce timer for filter changes
  const filterDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Build ActivityQuerySpec from current state
  const buildQuerySpec = useCallback(
    (cursor?: string): ActivityQuerySpec => {
      const spec: ActivityQuerySpec = {
        startTime: timeRange.start,
        endTime: timeRange.end || 'now',
        limit: pageSize,
      };

      // Add search
      if (filters.search) {
        spec.search = filters.search;
      }

      // Add CEL filter
      const celFilter = buildCelFilter(filters);
      if (celFilter) {
        spec.filter = celFilter;
      }

      // Add pagination cursor
      if (cursor) {
        spec.continue = cursor;
      }

      return spec;
    },
    [filters, timeRange, pageSize]
  );

  // Build field selector string for Watch API
  // Supported fields: spec.changeSource, spec.resource.*, spec.actor.*
  const buildFieldSelector = useCallback((): string | undefined => {
    const selectors: string[] = [];

    // changeSource filter (single value)
    if (filters.changeSource && filters.changeSource !== 'all') {
      selectors.push(`spec.changeSource=${filters.changeSource}`);
    }

    // Resource UID filter (single value)
    if (filters.resourceUid) {
      selectors.push(`spec.resource.uid=${filters.resourceUid}`);
    }

    // Single resource kind (multi-value requires client-side filtering)
    if (filters.resourceKinds && filters.resourceKinds.length === 1) {
      selectors.push(`spec.resource.kind=${filters.resourceKinds[0]}`);
    }

    // Single actor name (multi-value requires client-side filtering)
    if (filters.actorNames && filters.actorNames.length === 1) {
      selectors.push(`spec.actor.name=${filters.actorNames[0]}`);
    }

    // Single API group (multi-value requires client-side filtering)
    if (filters.apiGroups && filters.apiGroups.length === 1) {
      selectors.push(`spec.resource.apiGroup=${filters.apiGroups[0]}`);
    }

    // Single resource namespace (multi-value requires client-side filtering)
    if (filters.resourceNamespaces && filters.resourceNamespaces.length === 1) {
      selectors.push(`spec.resource.namespace=${filters.resourceNamespaces[0]}`);
    }

    return selectors.length > 0 ? selectors.join(',') : undefined;
  }, [filters]);

  // Build watch params with field selectors for server-side filtering
  const buildWatchParams = useCallback((): ActivityListParams => {
    return {
      start: timeRange.start,
      end: timeRange.end,
      fieldSelector: buildFieldSelector(),
    };
  }, [timeRange, buildFieldSelector]);

  // Handle incoming watch events with client-side filtering for multi-value scenarios
  // Single-value filters (changeSource, single resourceKind, etc.) are handled server-side via fieldSelector
  // Multi-value filters (multiple resourceKinds, actorNames, etc.) require client-side filtering
  const handleWatchEvent = useCallback((event: WatchEvent<Activity>) => {
    if (event.type === 'ERROR') {
      console.error('Watch error:', event.object);
      return;
    }

    if (event.type === 'BOOKMARK') {
      // Update resource version for resume capability
      if (event.object.metadata?.resourceVersion) {
        resourceVersionRef.current = event.object.metadata.resourceVersion;
      }
      return;
    }

    // Update resource version from the event
    if (event.object.metadata?.resourceVersion) {
      resourceVersionRef.current = event.object.metadata.resourceVersion;
    }

    const activity = event.object;
    const spec = activity.spec;

    // Client-side filtering for multi-value filters (field selectors only support single values)
    // Multi-value resourceKinds filter
    if (filters.resourceKinds && filters.resourceKinds.length > 1) {
      if (!spec?.resource?.kind || !filters.resourceKinds.includes(spec.resource.kind)) {
        return;
      }
    }

    // Multi-value actorNames filter
    if (filters.actorNames && filters.actorNames.length > 1) {
      if (!spec?.actor?.name || !filters.actorNames.includes(spec.actor.name)) {
        return;
      }
    }

    // Multi-value apiGroups filter
    if (filters.apiGroups && filters.apiGroups.length > 1) {
      if (!spec?.resource?.apiGroup || !filters.apiGroups.includes(spec.resource.apiGroup)) {
        return;
      }
    }

    // Multi-value resourceNamespaces filter
    if (filters.resourceNamespaces && filters.resourceNamespaces.length > 1) {
      if (!spec?.resource?.namespace || !filters.resourceNamespaces.includes(spec.resource.namespace)) {
        return;
      }
    }

    // Resource name partial match filter (field selectors don't support partial matches)
    if (filters.resourceName) {
      if (!spec?.resource?.name || !spec.resource.name.includes(filters.resourceName)) {
        return;
      }
    }

    if (event.type === 'ADDED') {
      // Prepend new activity to the list
      setActivities((prev) => {
        // Check for duplicates by name
        const exists = prev.some((a) => a.metadata?.name === event.object.metadata?.name);
        if (exists) {
          return prev;
        }
        return [event.object, ...prev];
      });
      setNewActivitiesCount((prev) => prev + 1);
    } else if (event.type === 'MODIFIED') {
      // Update existing activity
      setActivities((prev) =>
        prev.map((a) =>
          a.metadata?.name === event.object.metadata?.name ? event.object : a
        )
      );
    } else if (event.type === 'DELETED') {
      // Remove deleted activity
      setActivities((prev) =>
        prev.filter((a) => a.metadata?.name !== event.object.metadata?.name)
      );
    }
  }, [filters]);

  // Start watching for real-time updates
  const startStreaming = useCallback(() => {
    if (watchStopRef.current) {
      // Already watching
      return;
    }

    // Explicit start clears the user-paused flag so the auto-restart
    // effects can keep the stream alive after subsequent filter changes.
    setUserPaused(false);

    // Clear any previous watch error when starting a new stream
    setWatchError(null);

    const params = buildWatchParams();
    const { stop } = client.watchActivities(params, {
      resourceVersion: resourceVersionRef.current,
      onEvent: handleWatchEvent,
      onError: (err) => {
        console.error('Watch stream error:', err);
        // Wrap the error in a StreamError for user-friendly messaging
        const streamError = new StreamError(err.message, err);
        setWatchError(streamError);
        setIsStreaming(false);
        watchStopRef.current = null;
      },
      onClose: () => {
        setIsStreaming(false);
        watchStopRef.current = null;
      },
    });

    watchStopRef.current = stop;
    setIsStreaming(true);
    setNewActivitiesCount(0);
  }, [client, buildWatchParams, handleWatchEvent]);

  // Stop watching. Marks the user as paused so the auto-start effects
  // below don't immediately re-open the stream this call just closed.
  const stopStreaming = useCallback(() => {
    setUserPaused(true);
    setIsStreaming(false);
    if (watchStopRef.current) {
      watchStopRef.current();
      watchStopRef.current = null;
    }
  }, []);

  // Execute the feed query using ActivityQuery
  const refresh = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    setNewActivitiesCount(0);

    try {
      const spec = buildQuerySpec();
      const result = await client.createActivityQuery(spec);
      const activitiesArray = result.status?.results || [];

      setActivities(activitiesArray);
      setContinueCursor(result.status?.continue);

      // Capture and notify about effective time range
      if (result.status?.effectiveStartTime && result.status?.effectiveEndTime) {
        const newEffectiveTimeRange: EffectiveTimeRange = {
          startTime: result.status.effectiveStartTime,
          endTime: result.status.effectiveEndTime,
        };
        setEffectiveTimeRange(newEffectiveTimeRange);
        onEffectiveTimeRangeChange?.(newEffectiveTimeRange);
      }

      // Note: ActivityQuery doesn't return resourceVersion, so we'll get it from the watch
      hasInitialLoadRef.current = true;

      // Auto-restart streaming if it was active before filter change
      if (shouldRestartStreamingRef.current && enableStreaming) {
        shouldRestartStreamingRef.current = false;
        // Defer streaming start to next tick to ensure state is updated
        setTimeout(() => {
          if (watchStopRef.current === null) {
            // startStreaming will be called via the effect
          }
        }, 0);
      }
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      shouldRestartStreamingRef.current = false;
    } finally {
      setIsLoading(false);
    }
  }, [client, buildQuerySpec, enableStreaming, onEffectiveTimeRangeChange]);

  // Load more activities (pagination) using ActivityQuery
  const loadMore = useCallback(async () => {
    if (!continueCursor || isLoading) {
      return;
    }

    setIsLoading(true);
    setError(null);

    try {
      const spec = buildQuerySpec(continueCursor);
      const result = await client.createActivityQuery(spec);

      // Deduplicate before appending - use uid as primary key, fallback to name
      setActivities((prev) => {
        const existingUids = new Set(prev.map(a => a.metadata?.uid).filter(Boolean));
        const existingNames = new Set(prev.map(a => a.metadata?.name).filter(Boolean));

        const newActivities = (result.status?.results || []).filter(activity => {
          const uid = activity.metadata?.uid;
          const name = activity.metadata?.name;

          // Use uid if available (most reliable), otherwise fall back to name
          if (uid) {
            return !existingUids.has(uid);
          }
          if (name) {
            return !existingNames.has(name);
          }
          // If no uid or name, allow it through (shouldn't happen in practice)
          return true;
        });

        return [...prev, ...newActivities];
      });
      setContinueCursor(result.status?.continue);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setIsLoading(false);
    }
  }, [client, buildQuerySpec, continueCursor, isLoading]);

  // Update filters and reset pagination with debounced auto-refresh
  const updateFilters = useCallback((newFilters: ActivityFeedFilters) => {
    // Track if streaming was active so we can restart it
    if (isStreaming) {
      shouldRestartStreamingRef.current = true;
      stopStreaming();
    }

    // Set loading FIRST so skeleton loaders appear immediately
    setIsLoading(true);
    setFilters(newFilters);
    setActivities([]);
    setContinueCursor(undefined);
    resourceVersionRef.current = undefined;

    // Cancel any pending debounced refresh
    if (filterDebounceRef.current) {
      clearTimeout(filterDebounceRef.current);
    }

    // Debounce the refresh to avoid excessive API calls
    filterDebounceRef.current = setTimeout(() => {
      filterDebounceRef.current = null;
    }, FILTER_DEBOUNCE_MS);
  }, [stopStreaming, isStreaming]);

  // Update time range and reset pagination with auto-refresh
  const updateTimeRange = useCallback((newTimeRange: TimeRange) => {
    // Track if streaming was active so we can restart it
    if (isStreaming) {
      shouldRestartStreamingRef.current = true;
      stopStreaming();
    }

    // Set loading FIRST so skeleton loaders appear immediately
    setIsLoading(true);
    setTimeRange(newTimeRange);
    setActivities([]);
    setContinueCursor(undefined);
    resourceVersionRef.current = undefined;

    // Cancel any pending debounced refresh
    if (filterDebounceRef.current) {
      clearTimeout(filterDebounceRef.current);
    }

    // Debounce the refresh
    filterDebounceRef.current = setTimeout(() => {
      filterDebounceRef.current = null;
    }, FILTER_DEBOUNCE_MS);
  }, [stopStreaming, isStreaming]);

  // Reset to initial state
  const reset = useCallback(() => {
    stopStreaming();
    setActivities([]);
    setError(null);
    setWatchError(null);
    setContinueCursor(undefined);
    setFilters(initialFilters);
    setTimeRange(initialTimeRange);
    setNewActivitiesCount(0);
    resourceVersionRef.current = undefined;
  }, [initialFilters, initialTimeRange, stopStreaming]);

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
  }, [filters, timeRange]); // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-start streaming after initial load when enabled. Skipped when
  // the user has explicitly paused — otherwise clicking Pause would be
  // immediately undone by this effect on the next render.
  useEffect(() => {
    if (
      enableStreaming &&
      autoStartStreaming &&
      !userPaused &&
      activities.length > 0 &&
      !isStreaming &&
      !isLoading
    ) {
      startStreaming();
    }
  }, [enableStreaming, autoStartStreaming, userPaused, activities.length, isStreaming, isLoading, startStreaming]);

  // Restart streaming after filter change refresh completes. Also skipped
  // when the user has paused — a filter change shouldn't silently resume
  // a stream the user intentionally stopped.
  useEffect(() => {
    if (
      enableStreaming &&
      shouldRestartStreamingRef.current &&
      !userPaused &&
      activities.length > 0 &&
      !isStreaming &&
      !isLoading
    ) {
      shouldRestartStreamingRef.current = false;
      startStreaming();
    }
  }, [enableStreaming, userPaused, activities.length, isStreaming, isLoading, startStreaming]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (watchStopRef.current) {
        watchStopRef.current();
      }
      if (filterDebounceRef.current) {
        clearTimeout(filterDebounceRef.current);
      }
    };
  }, []);

  const hasMore = useMemo(() => !!continueCursor, [continueCursor]);

  return {
    activities,
    isLoading,
    error,
    watchError,
    hasMore,
    filters,
    timeRange,
    refresh,
    loadMore,
    setFilters: updateFilters,
    setTimeRange: updateTimeRange,
    reset,
    isStreaming,
    startStreaming,
    stopStreaming,
    newActivitiesCount,
    effectiveTimeRange,
  };
}
