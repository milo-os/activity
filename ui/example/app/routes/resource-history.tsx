import { useState, useEffect, useMemo, useCallback } from "react";
import { useSearchParams } from "@remix-run/react";
import {
  ResourceHistoryView,
  ActivityApiClient,
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  Input,
  Label,
  Button,
  Combobox,
  useFacets,
  type Activity,
  type ResourceFilter,
  type ComboboxOption,
  type ActivityFeedFilterState,
} from "@datum-cloud/activity-ui";
import { AppLayout } from "~/components/AppLayout";
import { EventDetailModal } from "~/components/EventDetailModal";

/**
 * Resource History page - displays change history for a specific resource.
 * Supports filtering by API Group, Kind, Namespace, Name, or UID.
 * Uses the Facets API for typeahead dropdowns with cascading filters.
 *
 * Deep linking supported via URL search params:
 * - ?uid=<resource-uid> - Search by UID (takes precedence)
 * - ?apiGroup=<group>&kind=<kind>&namespace=<ns>&name=<name> - Search by attributes
 */
export default function ResourceHistoryPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [client, setClient] = useState<ActivityApiClient | null>(null);
  const [selectedActivity, setSelectedActivity] = useState<Activity | null>(
    null,
  );

  // Read initial values from URL search params
  const initialApiGroup = searchParams.get("apiGroup") || "";
  const initialKind = searchParams.get("kind") || "";
  const initialNamespace = searchParams.get("namespace") || "";
  const initialName = searchParams.get("name") || "";
  const initialUid = searchParams.get("uid") || "";

  // Form state - initialized from URL params
  const [apiGroup, setApiGroup] = useState(initialApiGroup);
  const [kind, setKind] = useState(initialKind);
  const [namespace, setNamespace] = useState(initialNamespace);
  const [name, setName] = useState(initialName);
  const [uid, setUid] = useState(initialUid);

  // Build filter from URL params if present
  const filterFromParams = useMemo((): ResourceFilter | null => {
    if (initialUid) {
      return { uid: initialUid };
    }
    if (initialApiGroup || initialKind || initialNamespace || initialName) {
      const filter: ResourceFilter = {};
      if (initialApiGroup) filter.apiGroup = initialApiGroup;
      if (initialKind) filter.kind = initialKind;
      if (initialNamespace) filter.namespace = initialNamespace;
      if (initialName) filter.name = initialName;
      return filter;
    }
    return null;
  }, [initialApiGroup, initialKind, initialNamespace, initialName, initialUid]);

  // Submitted filter - initialized from URL params
  const [submittedFilter, setSubmittedFilter] = useState<ResourceFilter | null>(
    filterFromParams,
  );

  // Sync submitted filter when URL params change (e.g., browser back/forward)
  useEffect(() => {
    setSubmittedFilter(filterFromParams);
    // Also sync form state
    setApiGroup(initialApiGroup);
    setKind(initialKind);
    setNamespace(initialNamespace);
    setName(initialName);
    setUid(initialUid);
  }, [
    filterFromParams,
    initialApiGroup,
    initialKind,
    initialNamespace,
    initialName,
    initialUid,
  ]);

  useEffect(() => {
    // Check if in production environment
    const isProduction =
      typeof window !== "undefined" &&
      window.location.hostname !== "localhost" &&
      window.location.hostname !== "127.0.0.1";

    if (isProduction) {
      setClient(new ActivityApiClient({ baseUrl: "" }));
    } else {
      const apiUrl = sessionStorage.getItem("apiUrl") || "";
      const token = sessionStorage.getItem("token") || undefined;
      setClient(
        new ActivityApiClient({
          baseUrl: apiUrl || "",
          token,
        }),
      );
    }
  }, []);

  // Build filter state from current form selections for cascading dropdowns
  const currentFilters = useMemo((): ActivityFeedFilterState => {
    const filters: ActivityFeedFilterState = {};
    if (apiGroup) filters.apiGroups = [apiGroup];
    if (kind) filters.resourceKinds = [kind];
    if (namespace) filters.resourceNamespaces = [namespace];
    if (name) filters.resourceName = name;
    return filters;
  }, [apiGroup, kind, namespace, name]);

  // Fetch facets for typeahead dropdowns - filtered by current selections
  const {
    resourceKinds,
    apiGroups,
    resourceNamespaces,
    isLoading: facetsLoading,
  } = useFacets(
    client!,
    { start: "now-30d" },
    currentFilters, // Pass current selections to filter facet results
  );

  // Convert facets to combobox options
  const apiGroupOptions: ComboboxOption[] = useMemo(
    () =>
      apiGroups
        .filter((f) => f.value)
        .map((f) => ({
          value: f.value,
          label: f.value,
          count: f.count,
        })),
    [apiGroups],
  );

  const kindOptions: ComboboxOption[] = useMemo(
    () =>
      resourceKinds
        .filter((f) => f.value)
        .map((f) => ({
          value: f.value,
          label: f.value,
          count: f.count,
        })),
    [resourceKinds],
  );

  const namespaceOptions: ComboboxOption[] = useMemo(
    () =>
      resourceNamespaces
        .filter((f) => f.value)
        .map((f) => ({
          value: f.value,
          label: f.value,
          count: f.count,
        })),
    [resourceNamespaces],
  );

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      const filter: ResourceFilter = {};
      const params = new URLSearchParams();

      if (uid.trim()) {
        filter.uid = uid.trim();
        params.set("uid", uid.trim());
      } else {
        if (apiGroup) {
          filter.apiGroup = apiGroup;
          params.set("apiGroup", apiGroup);
        }
        if (kind) {
          filter.kind = kind;
          params.set("kind", kind);
        }
        if (namespace) {
          filter.namespace = namespace;
          params.set("namespace", namespace);
        }
        if (name.trim()) {
          filter.name = name.trim();
          params.set("name", name.trim());
        }
      }

      // Only submit if we have at least one filter
      if (Object.keys(filter).length > 0) {
        setSubmittedFilter(filter);
        setSearchParams(params, { replace: false });
      }
    },
    [uid, apiGroup, kind, namespace, name, setSearchParams],
  );

  const handleActivityClick = useCallback((activity: Activity) => {
    setSelectedActivity(activity);
  }, []);

  const handleReset = useCallback(() => {
    setSubmittedFilter(null);
    setApiGroup("");
    setKind("");
    setNamespace("");
    setName("");
    setUid("");
    // Clear URL params
    setSearchParams({}, { replace: true });
  }, [setSearchParams]);

  const hasFormData = apiGroup || kind || namespace || name || uid;
  const isUidMode = !!uid;
  const isAttributeMode = !!(apiGroup || kind || namespace || name);

  return (
    <AppLayout>
      {!submittedFilter ? (
        <Card className="max-w-2xl mx-auto">
          <CardHeader>
            <CardTitle>Resource History</CardTitle>
            <CardDescription>
              Search for a resource to view its change history over time
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-6">
              {/* Resource Attributes Section */}
              <div className="space-y-4">
                <h3 className="text-sm font-semibold text-foreground">
                  Search by Resource Attributes
                </h3>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="api-group">API Group</Label>
                    <Combobox
                      options={apiGroupOptions}
                      value={apiGroup}
                      onValueChange={setApiGroup}
                      placeholder="Select API group..."
                      searchPlaceholder="Search API groups..."
                      emptyMessage="No API groups found"
                      disabled={isUidMode}
                      loading={facetsLoading && !client}
                      clearable
                      showAllOption={false}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="kind">Kind</Label>
                    <Combobox
                      options={kindOptions}
                      value={kind}
                      onValueChange={setKind}
                      placeholder="Select kind..."
                      searchPlaceholder="Search kinds..."
                      emptyMessage="No kinds found"
                      disabled={isUidMode}
                      loading={facetsLoading && !client}
                      clearable
                      showAllOption={false}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="namespace">Namespace</Label>
                    <Combobox
                      options={namespaceOptions}
                      value={namespace}
                      onValueChange={setNamespace}
                      placeholder="Select namespace..."
                      searchPlaceholder="Search namespaces..."
                      emptyMessage="No namespaces found"
                      disabled={isUidMode}
                      loading={facetsLoading && !client}
                      clearable
                      showAllOption={false}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="name">
                      Name{" "}
                      <span className="font-normal text-muted-foreground text-xs">
                        (partial match)
                      </span>
                    </Label>
                    <Input
                      id="name"
                      type="text"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder="e.g., api-gateway"
                      disabled={isUidMode}
                    />
                  </div>
                </div>
              </div>

              {/* Divider */}
              <div className="relative">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-border" />
                </div>
                <div className="relative flex justify-center text-xs uppercase">
                  <span className="bg-background px-2 text-muted-foreground">
                    or
                  </span>
                </div>
              </div>

              {/* UID Section */}
              <div className="space-y-4">
                <h3 className="text-sm font-semibold text-foreground">
                  Search by Resource UID
                </h3>
                <div className="space-y-2">
                  <Label htmlFor="uid">Resource UID</Label>
                  <Input
                    id="uid"
                    type="text"
                    value={uid}
                    onChange={(e) => setUid(e.target.value)}
                    placeholder="e.g., 550e8400-e29b-41d4-a716-446655440000"
                    className="font-mono"
                    disabled={isAttributeMode}
                  />
                  <p className="text-xs text-muted-foreground">
                    UID provides exact match. When specified, other filters are
                    ignored.
                  </p>
                </div>
              </div>

              <Button type="submit" disabled={!hasFormData} className="w-full">
                View History
              </Button>
            </form>

            <div className="mt-8 pt-6 border-t">
              <h3 className="text-sm font-semibold text-foreground mb-3">
                Tips
              </h3>
              <ul className="space-y-2 text-sm text-muted-foreground list-disc list-inside">
                <li>
                  Dropdowns <strong>filter automatically</strong> based on other
                  selections
                </li>
                <li>
                  <strong>Name</strong> supports partial matching (e.g., "api"
                  matches "api-gateway")
                </li>
                <li>
                  Combine filters to narrow down results (e.g., Kind +
                  Namespace)
                </li>
                <li>
                  Find a resource's UID with:{" "}
                  <code className="px-1 py-0.5 bg-muted rounded text-xs">
                    kubectl get &lt;kind&gt; &lt;name&gt; -o jsonpath='
                    {"{.metadata.uid}"}'
                  </code>
                </li>
              </ul>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-lg font-semibold text-foreground">
                Resource History
              </h1>
              <p className="text-sm text-muted-foreground">
                {submittedFilter.uid ? (
                  <span className="font-mono">UID: {submittedFilter.uid}</span>
                ) : (
                  <span>
                    {[
                      submittedFilter.kind,
                      submittedFilter.name,
                      submittedFilter.namespace &&
                        `in ${submittedFilter.namespace}`,
                      submittedFilter.apiGroup &&
                        `(${submittedFilter.apiGroup})`,
                    ]
                      .filter(Boolean)
                      .join(" ")}
                  </span>
                )}
              </p>
            </div>
            <Button variant="outline" onClick={handleReset}>
              New Search
            </Button>
          </div>

          {client && (
            <ResourceHistoryView
              client={client}
              resourceFilter={submittedFilter}
              startTime="now-30d"
              limit={50}
              showHeader={false}
              compact={false}
              onActivityClick={handleActivityClick}
            />
          )}
        </div>
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
