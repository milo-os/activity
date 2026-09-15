import { useState, useCallback } from 'react';
import type { AuditLogQuery, AuditLogQuerySpec, Event } from '../types';
import { ActivityApiClient } from '../api/client';

export interface UseAuditLogQueryOptions {
  client: ActivityApiClient;
  autoExecute?: boolean;
}

export interface UseAuditLogQueryResult {
  query: AuditLogQuery | null;
  events: Event[];
  isLoading: boolean;
  error: Error | null;
  hasMore: boolean;
  executeQuery: (spec: AuditLogQuerySpec) => Promise<void>;
  loadMore: () => Promise<void>;
  reset: () => void;
}

/**
 * React hook for executing audit log queries
 */
export function useAuditLogQuery({
  client,
}: UseAuditLogQueryOptions): UseAuditLogQueryResult {
  const [query, setQuery] = useState<AuditLogQuery | null>(null);
  const [events, setEvents] = useState<Event[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [currentSpec, setCurrentSpec] = useState<AuditLogQuerySpec | null>(null);

  const executeQuery = useCallback(
    async (spec: AuditLogQuerySpec) => {
      setIsLoading(true);
      setError(null);
      setCurrentSpec(spec);

      try {
        const queryName = `query-${Date.now()}`;
        const result = await client.createQuery(queryName, spec);

        console.log('[useAuditLogQuery] Query response:', {
          resultCount: result.status?.results?.length,
          continue: result.status?.continue,
          phase: result.status?.phase,
        });

        setQuery(result);
        setEvents(result.status?.results || []);
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setIsLoading(false);
      }
    },
    [client]
  );

  const loadMore = useCallback(async () => {
    if (!query?.status?.continue || !currentSpec || isLoading) {
      console.log('[useAuditLogQuery] loadMore skipped:', {
        hasContinue: !!query?.status?.continue,
        hasCurrentSpec: !!currentSpec,
        isLoading,
      });
      return;
    }

    console.log('[useAuditLogQuery] Loading more with continue:', query.status.continue);

    setIsLoading(true);
    setError(null);

    try {
      const nextSpec: AuditLogQuerySpec = {
        ...currentSpec,
        continue: query.status.continue,
      };

      const queryName = `query-${Date.now()}`;
      const result = await client.createQuery(queryName, nextSpec);

      console.log('[useAuditLogQuery] loadMore response:', {
        resultCount: result.status?.results?.length,
        continue: result.status?.continue,
        totalEvents: events.length + (result.status?.results?.length || 0),
      });

      setQuery(result);
      // Deduplicate before appending - use auditID as the unique key
      setEvents((prev) => {
        const existingAuditIds = new Set(prev.map(e => e.auditID).filter(Boolean));

        const newEvents = (result.status?.results || []).filter(event => {
          const auditID = event.auditID;
          if (auditID) {
            return !existingAuditIds.has(auditID);
          }
          // If no auditID, allow it through (shouldn't happen in practice)
          return true;
        });

        return [...prev, ...newEvents];
      });
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setIsLoading(false);
    }
  }, [client, query, currentSpec, isLoading, events.length]);

  const reset = useCallback(() => {
    setQuery(null);
    setEvents([]);
    setError(null);
    setCurrentSpec(null);
  }, []);

  return {
    query,
    events,
    isLoading,
    error,
    hasMore: !!query?.status?.continue,
    executeQuery,
    loadMore,
    reset,
  };
}
