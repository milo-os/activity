import { useState, useEffect, useCallback } from "react";
import { useSearchParams } from "react-router";
import {
  EventsFeed,
  ActivityApiClient,
  type K8sEvent,
} from "@datum-cloud/activity-ui";
import type { EventsFeedFilters, TimeRange } from "@datum-cloud/activity-ui";
import { EventDetailModal } from "~/components/EventDetailModal";
import { AppLayout } from "~/components/AppLayout";
import {
  deserializeEventsState,
  serializeEventsState,
  type EventsFeedUrlState,
} from "~/lib/url-state";

/**
 * Events page - displays Kubernetes events with filtering and real-time updates.
 */
export default function EventsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [client, setClient] = useState<ActivityApiClient | null>(null);
  const [selectedEvent, setSelectedEvent] = useState<K8sEvent | null>(null);

  // Initialize state from URL or use defaults
  const urlState = deserializeEventsState(searchParams);
  const [initialFilters] = useState<EventsFeedFilters>(() => ({
    eventType: (urlState.eventType as EventsFeedFilters["eventType"]) || "all",
    search: urlState.search,
    involvedKinds: urlState.involvedKinds,
    reasons: urlState.reasons,
    namespaces: urlState.namespaces,
    sourceComponents: urlState.sourceComponents,
    involvedName: urlState.involvedName,
  }));

  const [initialTimeRange] = useState<TimeRange>(() => ({
    start: urlState.startTime || "now-24h",
    end: urlState.endTime,
  }));

  useEffect(() => {
    // Check if in production environment
    const isProduction = typeof window !== "undefined" &&
      window.location.hostname !== "localhost" &&
      window.location.hostname !== "127.0.0.1";

    if (isProduction) {
      // In production, use relative URLs (Gateway handles routing)
      setClient(new ActivityApiClient({ baseUrl: "" }));
    } else {
      // In development, check sessionStorage for connection info
      const apiUrl = sessionStorage.getItem("apiUrl") || "";
      const token = sessionStorage.getItem("token") || undefined;

      // If no URL configured, use the proxy (empty baseUrl means same origin)
      setClient(
        new ActivityApiClient({
          baseUrl: apiUrl || "",
          token,
        })
      );
    }
  }, []);

  const handleEventClick = (event: K8sEvent) => {
    setSelectedEvent(event);
  };

  // Update URL when filters or time range change
  const handleFiltersChange = useCallback(
    (filters: EventsFeedFilters, timeRange: TimeRange) => {
      const newState: EventsFeedUrlState = {
        eventType: filters.eventType,
        search: filters.search,
        involvedKinds: filters.involvedKinds,
        reasons: filters.reasons,
        namespaces: filters.namespaces,
        sourceComponents: filters.sourceComponents,
        involvedName: filters.involvedName,
        startTime: timeRange.start,
        endTime: timeRange.end,
      };

      const params = serializeEventsState(newState);
      // Use replace to avoid cluttering history
      setSearchParams(params, { replace: true });
    },
    [setSearchParams]
  );

  return (
    <AppLayout>
      {client && (
        <EventsFeed
          client={client}
          initialTimeRange={initialTimeRange}
          initialFilters={initialFilters}
          onFiltersChange={handleFiltersChange}
          pageSize={50}
          enableStreaming={true}
          showFilters={true}
          onEventClick={handleEventClick}
        />
      )}

      {selectedEvent && (
        <EventDetailModal
          title="Event Details"
          data={selectedEvent}
          onClose={() => setSelectedEvent(null)}
        />
      )}
    </AppLayout>
  );
}
