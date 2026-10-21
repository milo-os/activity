import { useState, useEffect, useCallback } from "react";
import { useNavigate, useSearchParams } from "@remix-run/react";
import {
  ActivityFeed,
  ActivityApiClient,
  type Activity,
  type ErrorFormatter,
  defaultResourceLinkResolver,
  defaultErrorFormatter,
} from "@datum-cloud/activity-ui";
import type { ActivityFeedFilters, TimeRange } from "@datum-cloud/activity-ui";
import { EventDetailModal } from "~/components/EventDetailModal";
import { AppLayout } from "~/components/AppLayout";
import {
  deserializeActivityState,
  serializeActivityState,
  type ActivityFeedUrlState,
} from "~/lib/url-state";

/**
 * Custom error formatter that adds organization-specific messaging
 */
const customErrorFormatter: ErrorFormatter = (error) => {
  // Get the default formatting first
  const defaultFormatted = defaultErrorFormatter(error);

  // Customize specific error types
  if (error.message.includes("403")) {
    return {
      message: "You don't have permission to view this activity feed. Contact your team admin to request access.",
      technical: defaultFormatted.technical,
    };
  }

  if (error.message.includes("404")) {
    return {
      message: "The Activity service is not available. Please check your cluster configuration.",
      technical: defaultFormatted.technical,
    };
  }

  // For all other errors, use the default formatter
  return defaultFormatted;
};

/**
 * Activity Feed page - displays human-readable activity stream.
 */
export default function ActivityFeedPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [client, setClient] = useState<ActivityApiClient | null>(null);
  const [selectedActivity, setSelectedActivity] = useState<Activity | null>(
    null
  );

  // Initialize state from URL or use defaults
  const urlState = deserializeActivityState(searchParams);
  const [initialFilters] = useState<ActivityFeedFilters>(() => ({
    changeSource: (urlState.changeSource as ActivityFeedFilters["changeSource"]) || "all",
    search: urlState.search,
    resourceKinds: urlState.resourceKinds,
    actorNames: urlState.actorNames,
    apiGroups: urlState.apiGroups,
    resourceNamespaces: urlState.resourceNamespaces,
    resourceName: urlState.resourceName,
  }));

  const [initialTimeRange] = useState<TimeRange>(() => ({
    start: urlState.startTime || "now-7d",
    end: urlState.endTime,
  }));

  // The example app doubles as a platform-operator UI, where probing
  // ActivityPolicies is allowed. E2E tests opt in via ?checkPolicies=true.
  const checkPolicies = searchParams.get("checkPolicies") === "true";

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

  const handleActivityClick = (activity: Activity) => {
    setSelectedActivity(activity);
  };

  // Update URL when filters or time range change
  const handleFiltersChange = useCallback(
    (filters: ActivityFeedFilters, timeRange: TimeRange) => {
      const newState: ActivityFeedUrlState = {
        changeSource: filters.changeSource,
        search: filters.search,
        resourceKinds: filters.resourceKinds,
        actorNames: filters.actorNames,
        apiGroups: filters.apiGroups,
        resourceNamespaces: filters.resourceNamespaces,
        resourceName: filters.resourceName,
        startTime: timeRange.start,
        endTime: timeRange.end,
      };

      const params = serializeActivityState(newState);
      // Use replace to avoid cluttering history
      setSearchParams(params, { replace: true });
    },
    [setSearchParams]
  );

  return (
    <AppLayout>
      {client && (
        <ActivityFeed
          client={client}
          onActivityClick={handleActivityClick}
          resourceLinkResolver={defaultResourceLinkResolver}
          onCreatePolicy={() => navigate("/policies")}
          checkPolicies={checkPolicies}
          initialTimeRange={initialTimeRange}
          initialFilters={initialFilters}
          onFiltersChange={handleFiltersChange}
          pageSize={30}
          showFilters={true}
          infiniteScroll={true}
          enableStreaming={true}
          errorFormatter={customErrorFormatter}
        />
      )}

      {selectedActivity && (
        <EventDetailModal
          title="Activity Details"
          data={selectedActivity}
          onClose={() => setSelectedActivity(null)}
        />
      )}
    </AppLayout>
  );
}
