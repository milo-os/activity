import { useState, useCallback, useMemo, useEffect, useRef } from 'react';
import type { K8sEvent, K8sEventType, EventQuerySpec } from '../types/k8s-event';
import type { WatchEvent } from '../types/activity';
import { extractEvent as extractEventFn } from '../types/k8s-event';
import { ActivityApiClient } from '../api/client';

// Debounce delay for filter changes (ms)
const FILTER_DEBOUNCE_MS = 300;

/**
 * Filter options for the events feed
 */
export interface EventsFeedFilters {
  /** Filter by event type (Normal/Warning/all) */
  eventType?: K8sEventType | 'all';
  /** Filter by namespaces (multi-select) */
  namespaces?: string[];
  /** Filter by involved object API group (multi-select) */
  involvedApiGroups?: string[];
  /** Filter by involved object kind (multi-select) */
  involvedKinds?: string[];
  /** Filter by involved object name */
  involvedName?: string;
  /** Filter by event reasons (multi-select) */
  reasons?: string[];
  /** Filter by source components (multi-select) */
  sourceComponents?: string[];
  /** Full-text search on event messages */
  search?: string;
}

/**
 * Time range for the events feed
 */
export interface TimeRange {
  /** Start of time range (RFC3339 or relative like "now-24h") */
  start: string;
  /** End of time range (RFC3339 or relative, default: now) */
  end?: string;
}

/**
 * Options for the useEventsFeed hook
 */
export interface UseEventsFeedOptions {
  /** API client instance */
  client: ActivityApiClient;
  /** Initial filter settings */
  initialFilters?: EventsFeedFilters;
  /** Initial time range */
  initialTimeRange?: TimeRange;
  /** Number of items per page (default: 50) */
  pageSize?: number;
  /** Namespace to filter events (optional) */
  namespace?: string;
  /** Enable real-time streaming (default: false) */
  enableStreaming?: boolean;
  /** Auto-start streaming when enabled (default: true) */
  autoStartStreaming?: boolean;
}

/**
 * Result returned by the useEventsFeed hook
 */
export interface UseEventsFeedResult {
  /** List of events */
  events: K8sEvent[];
  /** Whether the feed is loading */
  isLoading: boolean;
  /** Whether a filter/time change is pending (shows during debounce period) */
  isRefreshing: boolean;
  /** Error if any occurred */
  error: Error | null;
  /** Whether there are more events to load */
  hasMore: boolean;
  /** Current filter settings */
  filters: EventsFeedFilters;
  /** Current time range */
  timeRange: TimeRange;
  /** Execute/refresh the feed query */
  refresh: () => Promise<void>;
  /** Load more events (pagination) */
  loadMore: () => Promise<void>;
  /** Update filter settings */
  setFilters: (filters: EventsFeedFilters) => void;
  /** Update time range */
  setTimeRange: (timeRange: TimeRange) => void;
  /** Reset to initial state */
  reset: () => void;
  /** Whether streaming is currently active */
  isStreaming: boolean;
  /** Start streaming (when enableStreaming is true) */
  startStreaming: () => void;
  /** Stop streaming */
  stopStreaming: () => void;
  /** Number of new events received via streaming since last refresh */
  newEventsCount: number;
}

/**
 * Build field selector from filter options
 */
function buildFieldSelector(filters: EventsFeedFilters): string | undefined {
  const selectors: string[] = [];

  // Event type filter
  if (filters.eventType && filters.eventType !== 'all') {
    selectors.push(`type=${filters.eventType}`);
  }

  // Involved object API group filter (multi-select)
  if (filters.involvedApiGroups && filters.involvedApiGroups.length > 0) {
    if (filters.involvedApiGroups.length === 1) {
      selectors.push(`regarding.apiVersion=${filters.involvedApiGroups[0]}`);
    } else {
      // Kubernetes field selectors don't support OR, so we'll handle this client-side
      // Just use the first one for server-side filtering
      selectors.push(`regarding.apiVersion=${filters.involvedApiGroups[0]}`);
    }
  }

  // Involved object kind filter (multi-select)
  if (filters.involvedKinds && filters.involvedKinds.length > 0) {
    if (filters.involvedKinds.length === 1) {
      selectors.push(`regarding.kind=${filters.involvedKinds[0]}`);
    } else {
      // Kubernetes field selectors don't support OR, so we'll handle this client-side
      // Just use the first one for server-side filtering
      selectors.push(`regarding.kind=${filters.involvedKinds[0]}`);
    }
  }

  // Involved object name filter
  if (filters.involvedName) {
    selectors.push(`regarding.name=${filters.involvedName}`);
  }

  // Namespace filter (multi-select)
  if (filters.namespaces && filters.namespaces.length > 0) {
    if (filters.namespaces.length === 1) {
      selectors.push(`regarding.namespace=${filters.namespaces[0]}`);
    } else {
      // Use first namespace for server-side filter
      selectors.push(`regarding.namespace=${filters.namespaces[0]}`);
    }
  }

  // Reason filter (multi-select)
  if (filters.reasons && filters.reasons.length > 0) {
    if (filters.reasons.length === 1) {
      selectors.push(`reason=${filters.reasons[0]}`);
    } else {
      // Use first reason for server-side filter
      selectors.push(`reason=${filters.reasons[0]}`);
    }
  }

  // Source component filter (multi-select)
  if (filters.sourceComponents && filters.sourceComponents.length > 0) {
    if (filters.sourceComponents.length === 1) {
      selectors.push(`source.component=${filters.sourceComponents[0]}`);
    } else {
      // Use first component for server-side filter
      selectors.push(`source.component=${filters.sourceComponents[0]}`);
    }
  }

  return selectors.length > 0 ? selectors.join(',') : undefined;
}

/**
 * Client-side filtering for multi-value filters that can't be expressed in field selectors
 */
function matchesClientFilters(event: K8sEvent, filters: EventsFeedFilters): boolean {
  // Get regarding object (handles both new and deprecated field names)
  const regarding = event.regarding || event.involvedObject;

  // Check involved API groups (multi-select)
  if (filters.involvedApiGroups && filters.involvedApiGroups.length > 1) {
    if (!filters.involvedApiGroups.includes(regarding?.apiVersion || '')) {
      return false;
    }
  }

  // Check involved kinds (multi-select)
  if (filters.involvedKinds && filters.involvedKinds.length > 1) {
    if (!filters.involvedKinds.includes(regarding?.kind || '')) {
      return false;
    }
  }

  // Check namespaces (multi-select)
  if (filters.namespaces && filters.namespaces.length > 1) {
    if (!filters.namespaces.includes(regarding?.namespace || '')) {
      return false;
    }
  }

  // Check reasons (multi-select)
  if (filters.reasons && filters.reasons.length > 1) {
    if (!filters.reasons.includes(event.reason || '')) {
      return false;
    }
  }

  // Check source components (multi-select) - check both new and deprecated field names
  if (filters.sourceComponents && filters.sourceComponents.length > 1) {
    const reportingController = event.reportingController || event.source?.component;
    if (!filters.sourceComponents.includes(reportingController || '')) {
      return false;
    }
  }

  return true;
}

/**
 * React hook for fetching and managing the Kubernetes events feed
 */
export function useEventsFeed({
  client,
  initialFilters = {},
  initialTimeRange = { start: 'now-24h' },
  pageSize = 50,
  namespace,
  enableStreaming = false,
  autoStartStreaming = true,
}: UseEventsFeedOptions): UseEventsFeedResult {
  const [events, setEvents] = useState<K8sEvent[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [continueCursor, setContinueCursor] = useState<string | undefined>();
  const [filters, setFilters] = useState<EventsFeedFilters>(initialFilters);
  const [timeRange, setTimeRange] = useState<TimeRange>(initialTimeRange);
  const [isStreaming, setIsStreaming] = useState(false);
  // Tracks whether the user has explicitly paused streaming so the
  // auto-start / post-filter-restart effects don't immediately reopen the
  // stream the user just closed.
  const [userPaused, setUserPaused] = useState(false);
  const [newEventsCount, setNewEventsCount] = useState(0);

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

  // Build EventQuery spec from current state
  const buildQuerySpec = useCallback(
    (cursor?: string): EventQuerySpec => {
      const spec: EventQuerySpec = {
        startTime: timeRange.start,
        endTime: timeRange.end || 'now',
        limit: pageSize,
      };

      // Add namespace if specified
      if (namespace) {
        spec.namespace = namespace;
      }

      // Add field selector
      const fieldSelector = buildFieldSelector(filters);
      if (fieldSelector) {
        spec.fieldSelector = fieldSelector;
      }

      // Add pagination cursor
      if (cursor) {
        spec.continue = cursor;
      }

      return spec;
    },
    [filters, timeRange, pageSize, namespace]
  );

  // Handle incoming watch events
  const handleWatchEvent = useCallback((event: WatchEvent<K8sEvent>) => {
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

    // Apply client-side filtering
    if (!matchesClientFilters(event.object, filters)) {
      return;
    }

    if (event.type === 'ADDED') {
      // Prepend new event to the list
      setEvents((prev) => {
        // Check for duplicates by name
        const exists = prev.some((e) => e.metadata?.name === event.object.metadata?.name);
        if (exists) {
          return prev;
        }
        return [event.object, ...prev];
      });
      setNewEventsCount((prev) => prev + 1);
    } else if (event.type === 'MODIFIED') {
      // Update existing event
      setEvents((prev) =>
        prev.map((e) =>
          e.metadata?.name === event.object.metadata?.name ? event.object : e
        )
      );
    } else if (event.type === 'DELETED') {
      // Remove deleted event
      setEvents((prev) =>
        prev.filter((e) => e.metadata?.name !== event.object.metadata?.name)
      );
    }
  }, [filters]);

  // Build watch params (for live streaming, uses field selector without time range)
  const buildWatchParams = useCallback(() => {
    return {
      namespace,
      fieldSelector: buildFieldSelector(filters),
    };
  }, [filters, namespace]);

  // Start watching for real-time updates
  const startStreaming = useCallback(() => {
    if (watchStopRef.current) {
      // Already watching
      return;
    }

    // Explicit start clears the user-paused flag.
    setUserPaused(false);

    const params = buildWatchParams();
    const { stop } = client.watchEvents(params, {
      resourceVersion: resourceVersionRef.current,
      onEvent: handleWatchEvent,
      onError: (err) => {
        console.error('Watch stream error:', err);
        setError(err);
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
    setNewEventsCount(0);
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

  // Execute the feed query using EventQuery
  const refresh = useCallback(async () => {
    setIsLoading(true);
    setIsRefreshing(false);
    setError(null);
    setNewEventsCount(0);

    try {
      const spec = buildQuerySpec();
      const result = await client.createEventQuery(spec);

      // Extract K8sEvent objects from EventRecord results
      const events = (result.status?.results || []).map(extractEventFn);

      // Apply client-side filtering for multi-value filters
      const filteredEvents = events.filter(event => matchesClientFilters(event, filters));

      setEvents(filteredEvents);
      setContinueCursor(result.status?.continue);

      // Note: EventQuery doesn't return resourceVersion, so watch will start from latest
      hasInitialLoadRef.current = true;

      // Auto-restart streaming if it was active before filter change
      if (shouldRestartStreamingRef.current && enableStreaming) {
        shouldRestartStreamingRef.current = false;
      }
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      shouldRestartStreamingRef.current = false;
    } finally {
      setIsLoading(false);
    }
  }, [client, buildQuerySpec, filters, enableStreaming]);

  // Load more events (pagination) using EventQuery
  const loadMore = useCallback(async () => {
    if (!continueCursor || isLoading) {
      return;
    }

    setIsLoading(true);
    setError(null);

    try {
      const spec = buildQuerySpec(continueCursor);
      const result = await client.createEventQuery(spec);

      // Extract K8sEvent objects from EventRecord results
      const events = (result.status?.results || []).map(extractEventFn);

      // Apply client-side filtering
      const filteredEvents = events.filter(event => matchesClientFilters(event, filters));

      // Deduplicate before appending - use uid as primary key, fallback to name
      setEvents((prev) => {
        const existingUids = new Set(prev.map(e => e.metadata?.uid).filter(Boolean));
        const existingNames = new Set(prev.map(e => e.metadata?.name).filter(Boolean));

        const newEvents = filteredEvents.filter(event => {
          const uid = event.metadata?.uid;
          const name = event.metadata?.name;

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

        return [...prev, ...newEvents];
      });
      setContinueCursor(result.status?.continue);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setIsLoading(false);
    }
  }, [client, buildQuerySpec, continueCursor, isLoading, filters]);

  // Update filters and reset pagination with debounced auto-refresh
  const updateFilters = useCallback((newFilters: EventsFeedFilters) => {
    // Track if streaming was active so we can restart it
    if (isStreaming) {
      shouldRestartStreamingRef.current = true;
      stopStreaming();
    }

    // Set isRefreshing BEFORE clearing events to prevent empty state flash
    setIsRefreshing(true);
    setFilters(newFilters);
    setEvents([]);
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

    // Set isRefreshing BEFORE clearing events to prevent empty state flash
    setIsRefreshing(true);
    setTimeRange(newTimeRange);
    setEvents([]);
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
    setEvents([]);
    setError(null);
    setContinueCursor(undefined);
    setFilters(initialFilters);
    setTimeRange(initialTimeRange);
    setNewEventsCount(0);
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

  // Auto-start streaming after initial load when enabled. Skipped when the
  // user has explicitly paused — otherwise clicking Pause would be
  // immediately undone.
  useEffect(() => {
    if (
      enableStreaming &&
      autoStartStreaming &&
      !userPaused &&
      events.length > 0 &&
      !isStreaming &&
      !isLoading
    ) {
      startStreaming();
    }
  }, [enableStreaming, autoStartStreaming, userPaused, events.length, isStreaming, isLoading, startStreaming]);

  // Restart streaming after filter change refresh completes. Also skipped
  // when paused.
  useEffect(() => {
    if (
      enableStreaming &&
      shouldRestartStreamingRef.current &&
      !userPaused &&
      events.length > 0 &&
      !isStreaming &&
      !isLoading
    ) {
      shouldRestartStreamingRef.current = false;
      startStreaming();
    }
  }, [enableStreaming, userPaused, events.length, isStreaming, isLoading, startStreaming]);

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
    events,
    isLoading,
    isRefreshing,
    error,
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
    newEventsCount,
  };
}
