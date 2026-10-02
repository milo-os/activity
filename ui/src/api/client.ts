import type { AuditLogQuery, AuditLogQuerySpec } from '../types';
import type {
  Activity,
  ActivityList,
  ActivityListParams,
  ActivityQuery,
  ActivityQuerySpec,
  ActivityFacetQuery,
  ActivityFacetQuerySpec,
  AuditLogFacetsQuery,
  AuditLogFacetsQuerySpec,
  WatchEvent,
} from '../types/activity';
import type {
  ActivityPolicy,
  ActivityPolicySpec,
  ActivityPolicyList,
  PolicyPreview,
  PolicyPreviewSpec,
} from '../types/policy';
import { parseApiError, NetworkError } from '../lib/errors';
import type {
  K8sEvent,
  K8sEventList,
  K8sEventListParams,
  EventFacetQuery,
  EventFacetQuerySpec,
  EventQuery,
  EventQuerySpec,
} from '../types/k8s-event';
import type {
  ReindexJob,
  ReindexJobListResource,
  ReindexJobSpec,
} from '../types/reindex';

/**
 * API Group information from Kubernetes discovery
 */
export interface APIGroup {
  name: string;
  versions: { groupVersion: string; version: string }[];
  preferredVersion?: { groupVersion: string; version: string };
}

/**
 * API Resource information from Kubernetes discovery
 */
export interface APIResource {
  name: string;
  singularName: string;
  namespaced: boolean;
  kind: string;
  verbs: string[];
  shortNames?: string[];
  categories?: string[];
}

export interface ApiClientConfig {
  /**
   * Base URL of the Activity API server
   * Example: 'https://api.example.com'
   */
  baseUrl: string;

  /**
   * Optional bearer token for authentication
   */
  token?: string;

  /**
   * Custom fetch implementation (useful for testing)
   */
  fetch?: typeof fetch;

  /**
   * Optional response transformer for proxies that wrap API responses.
   * Called with the parsed JSON response, should return the unwrapped data.
   *
   * Example for a proxy that wraps responses in {code, data, ...}:
   * ```
   * responseTransformer: (response) => response.data
   * ```
   */
  responseTransformer?: (response: unknown) => unknown;
}

export class ActivityApiClient {
  private config: ApiClientConfig;

  constructor(config: ApiClientConfig) {
    this.config = {
      ...config,
      fetch: config.fetch || globalThis.fetch.bind(globalThis),
    };
  }

  /**
   * Create a new AuditLogQuery
   */
  async createQuery(
    name: string,
    spec: AuditLogQuerySpec
  ): Promise<AuditLogQuery> {
    const query: AuditLogQuery = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'AuditLogQuery',
      metadata: { name },
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/auditlogqueries',
      {
        method: 'POST',
        body: JSON.stringify(query),
      }
    );

    return this.parseJson(response);
  }

  /**
   * Execute a query and get results with automatic pagination
   */
  async *executeQueryPaginated(
    spec: AuditLogQuerySpec,
    options?: {
      maxPages?: number;
      queryNamePrefix?: string;
    }
  ): AsyncGenerator<AuditLogQuery> {
    let pageNum = 0;
    let currentSpec = { ...spec };
    const maxPages = options?.maxPages || 100;
    const namePrefix = options?.queryNamePrefix || 'query';

    while (pageNum < maxPages) {
      const queryName = `${namePrefix}-${Date.now()}-${pageNum}`;
      const result = await this.createQuery(queryName, currentSpec);

      yield result;

      // Check if there are more results
      if (!result.status?.continue) {
        break;
      }

      currentSpec = {
        ...currentSpec,
        continue: result.status.continue,
      };
      pageNum++;
    }
  }

  // ============================================
  // Activity API Methods
  // ============================================

  /**
   * Create an ActivityQuery to search historical activities.
   * This is the preferred method for loading activity history with filters.
   */
  async createActivityQuery(spec: ActivityQuerySpec): Promise<ActivityQuery> {
    const query: ActivityQuery = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'ActivityQuery',
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/activityqueries',
      {
        method: 'POST',
        body: JSON.stringify(query),
      }
    );

    return this.parseJson(response);
  }

  /**
   * List activities with optional filtering and pagination
   */
  async listActivities(params?: ActivityListParams): Promise<ActivityList> {
    const searchParams = new URLSearchParams();

    if (params?.filter) searchParams.set('filter', params.filter);
    if (params?.fieldSelector) searchParams.set('fieldSelector', params.fieldSelector);
    if (params?.labelSelector) searchParams.set('labelSelector', params.labelSelector);
    if (params?.search) searchParams.set('search', params.search);
    if (params?.start) searchParams.set('start', params.start);
    if (params?.end) searchParams.set('end', params.end);
    if (params?.limit) searchParams.set('limit', String(params.limit));
    if (params?.continue) searchParams.set('continue', params.continue);

    const queryString = searchParams.toString();
    const path = `/apis/activity.miloapis.com/v1alpha1/activities${queryString ? `?${queryString}` : ''}`;

    const response = await this.fetch(path);
    return this.parseJson(response);
  }

  /**
   * Get a specific activity by name
   */
  async getActivity(namespace: string, name: string): Promise<Activity> {
    const response = await this.fetch(
      `/apis/activity.miloapis.com/v1alpha1/namespaces/${namespace}/activities/${name}`
    );
    return this.parseJson(response);
  }

  /**
   * Query facets for filtering UI (autocomplete, distinct values)
   */
  async queryFacets(spec: ActivityFacetQuerySpec): Promise<ActivityFacetQuery> {
    const query: ActivityFacetQuery = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'ActivityFacetQuery',
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/activityfacetqueries',
      {
        method: 'POST',
        body: JSON.stringify(query),
      }
    );

    return this.parseJson(response);
  }

  /**
   * List activities with automatic pagination using async generator
   */
  async *listActivitiesPaginated(
    params?: ActivityListParams,
    options?: {
      maxPages?: number;
    }
  ): AsyncGenerator<ActivityList> {
    let currentParams = { ...params };
    const maxPages = options?.maxPages || 100;
    let pageNum = 0;

    while (pageNum < maxPages) {
      const result = await this.listActivities(currentParams);

      yield result;

      // Check if there are more results
      if (!result.metadata?.continue) {
        break;
      }

      currentParams = {
        ...currentParams,
        continue: result.metadata.continue,
      };
      pageNum++;
    }
  }

  /**
   * Watch activities in real-time using the Kubernetes watch API.
   * Returns an async generator that yields watch events as they arrive.
   *
   * @param params - Query parameters (filter, start, end, etc.)
   * @param options - Watch options
   * @returns AsyncGenerator of watch events and an abort function
   */
  watchActivities(
    params?: Omit<ActivityListParams, 'watch'>,
    options?: {
      /** Resource version to start watching from */
      resourceVersion?: string;
      /** Callback when an event is received */
      onEvent?: (event: WatchEvent<Activity>) => void;
      /** Callback when an error occurs */
      onError?: (error: Error) => void;
      /** Callback when the connection closes */
      onClose?: () => void;
    }
  ): { stop: () => void } {
    const abortController = new AbortController();

    // Build URL with watch=true
    const searchParams = new URLSearchParams();
    searchParams.set('watch', 'true');

    if (params?.filter) searchParams.set('filter', params.filter);
    if (params?.fieldSelector) searchParams.set('fieldSelector', params.fieldSelector);
    if (params?.labelSelector) searchParams.set('labelSelector', params.labelSelector);
    if (params?.search) searchParams.set('search', params.search);
    if (params?.start) searchParams.set('start', params.start);
    if (params?.end) searchParams.set('end', params.end);
    if (options?.resourceVersion) searchParams.set('resourceVersion', options.resourceVersion);

    const queryString = searchParams.toString();
    const path = `/apis/activity.miloapis.com/v1alpha1/activities?${queryString}`;
    const url = `${this.config.baseUrl}${path}`;

    const headers: Record<string, string> = {
      'Accept': 'application/json',
    };

    if (this.config.token) {
      headers['Authorization'] = `Bearer ${this.config.token}`;
    }

    // Start the watch connection
    const startWatch = async () => {
      try {
        const response = await this.config.fetch!(url, {
          headers,
          signal: abortController.signal,
        });

        if (!response.ok) {
          const error = await response.text();
          throw new Error(`Watch request failed: ${response.status} ${error}`);
        }

        if (!response.body) {
          throw new Error('Response body is not available');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        for (;;) {
          const { done, value } = await reader.read();

          if (done) {
            options?.onClose?.();
            break;
          }

          buffer += decoder.decode(value, { stream: true });

          // Process complete lines (newline-delimited JSON)
          const lines = buffer.split('\n');
          buffer = lines.pop() || ''; // Keep incomplete line in buffer

          for (const line of lines) {
            if (line.trim()) {
              try {
                const event = JSON.parse(line) as WatchEvent<Activity>;
                options?.onEvent?.(event);
              } catch (parseError) {
                console.warn('Failed to parse watch event:', parseError, line);
              }
            }
          }
        }
      } catch (error) {
        if ((error as Error).name === 'AbortError') {
          options?.onClose?.();
          return;
        }
        options?.onError?.(error as Error);
      }
    };

    // Start watching in background
    startWatch();

    return {
      stop: () => abortController.abort(),
    };
  }

  /**
   * Watch activities using an async generator pattern.
   * This is an alternative API that yields events as they arrive.
   *
   * @param params - Query parameters (filter, start, end, etc.)
   * @param resourceVersion - Resource version to start watching from
   * @returns AsyncGenerator of watch events
   */
  async *watchActivitiesGenerator(
    params?: Omit<ActivityListParams, 'watch'>,
    resourceVersion?: string
  ): AsyncGenerator<WatchEvent<Activity>> {
    const abortController = new AbortController();

    // Build URL with watch=true
    const searchParams = new URLSearchParams();
    searchParams.set('watch', 'true');

    if (params?.filter) searchParams.set('filter', params.filter);
    if (params?.fieldSelector) searchParams.set('fieldSelector', params.fieldSelector);
    if (params?.labelSelector) searchParams.set('labelSelector', params.labelSelector);
    if (params?.search) searchParams.set('search', params.search);
    if (params?.start) searchParams.set('start', params.start);
    if (params?.end) searchParams.set('end', params.end);
    if (resourceVersion) searchParams.set('resourceVersion', resourceVersion);

    const queryString = searchParams.toString();
    const path = `/apis/activity.miloapis.com/v1alpha1/activities?${queryString}`;
    const url = `${this.config.baseUrl}${path}`;

    const headers: Record<string, string> = {
      'Accept': 'application/json',
    };

    if (this.config.token) {
      headers['Authorization'] = `Bearer ${this.config.token}`;
    }

    try {
      const response = await this.config.fetch!(url, {
        headers,
        signal: abortController.signal,
      });

      if (!response.ok) {
        const error = await response.text();
        throw new Error(`Watch request failed: ${response.status} ${error}`);
      }

      if (!response.body) {
        throw new Error('Response body is not available');
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();

        if (done) {
          break;
        }

        buffer += decoder.decode(value, { stream: true });

        // Process complete lines (newline-delimited JSON)
        const lines = buffer.split('\n');
        buffer = lines.pop() || ''; // Keep incomplete line in buffer

        for (const line of lines) {
          if (line.trim()) {
            const event = JSON.parse(line) as WatchEvent<Activity>;
            yield event;
          }
        }
      }
    } finally {
      abortController.abort();
    }
  }

  // ============================================
  // ActivityPolicy API Methods
  // ============================================

  /**
   * List all ActivityPolicies
   */
  async listPolicies(): Promise<ActivityPolicyList> {
    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/activitypolicies'
    );
    return this.parseJson(response);
  }

  /**
   * Get a specific ActivityPolicy by name
   */
  async getPolicy(name: string): Promise<ActivityPolicy> {
    const response = await this.fetch(
      `/apis/activity.miloapis.com/v1alpha1/activitypolicies/${name}`
    );
    return this.parseJson(response);
  }

  /**
   * Create a new ActivityPolicy
   * @param name Policy name
   * @param spec Policy specification
   * @param dryRun If true, validate without persisting
   */
  async createPolicy(
    name: string,
    spec: ActivityPolicySpec,
    dryRun?: boolean
  ): Promise<ActivityPolicy> {
    const policy: ActivityPolicy = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'ActivityPolicy',
      metadata: { name },
      spec,
    };

    const searchParams = new URLSearchParams();
    if (dryRun) {
      searchParams.set('dryRun', 'All');
    }

    const queryString = searchParams.toString();
    const path = `/apis/activity.miloapis.com/v1alpha1/activitypolicies${queryString ? `?${queryString}` : ''}`;

    const response = await this.fetch(path, {
      method: 'POST',
      body: JSON.stringify(policy),
    });

    return this.parseJson(response);
  }

  /**
   * Update an existing ActivityPolicy
   * @param name Policy name
   * @param spec Policy specification
   * @param dryRun If true, validate without persisting
   * @param resourceVersion Optional resource version for optimistic concurrency
   */
  async updatePolicy(
    name: string,
    spec: ActivityPolicySpec,
    dryRun?: boolean,
    resourceVersion?: string
  ): Promise<ActivityPolicy> {
    const policy: ActivityPolicy = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'ActivityPolicy',
      metadata: {
        name,
        ...(resourceVersion ? { resourceVersion } : {}),
      },
      spec,
    };

    const searchParams = new URLSearchParams();
    if (dryRun) {
      searchParams.set('dryRun', 'All');
    }

    const queryString = searchParams.toString();
    const path = `/apis/activity.miloapis.com/v1alpha1/activitypolicies/${name}${queryString ? `?${queryString}` : ''}`;

    const response = await this.fetch(path, {
      method: 'PUT',
      body: JSON.stringify(policy),
    });

    return this.parseJson(response);
  }

  /**
   * Delete an ActivityPolicy by name
   */
  async deletePolicy(name: string): Promise<void> {
    await this.fetch(
      `/apis/activity.miloapis.com/v1alpha1/activitypolicies/${name}`,
      { method: 'DELETE' }
    );
  }

  // ============================================
  // API Discovery Methods
  // ============================================

  /**
   * Discover all API groups available in the cluster
   */
  async discoverAPIGroups(): Promise<{ groups: APIGroup[] }> {
    const response = await this.fetch('/apis');
    return this.parseJson(response);
  }

  /**
   * Discover resources for a specific API group
   */
  async discoverAPIResources(
    group: string,
    version?: string
  ): Promise<{ resources: APIResource[] }> {
    // If no version specified, try to get the preferred version first
    let apiVersion = version;
    if (!apiVersion) {
      try {
        const groupsResponse = await this.discoverAPIGroups();
        const groupInfo = groupsResponse.groups?.find((g) => g.name === group);
        apiVersion = groupInfo?.preferredVersion?.version || 'v1';
      } catch {
        apiVersion = 'v1';
      }
    }

    const response = await this.fetch(`/apis/${group}/${apiVersion}`);
    return this.parseJson(response);
  }

  // ============================================
  // Audit Log Facets API Methods
  // ============================================

  /**
   * Query facets from audit logs (API groups, resources, verbs, etc.)
   * This is an ephemeral resource that executes immediately and returns results.
   */
  async queryAuditLogFacets(spec: AuditLogFacetsQuerySpec): Promise<AuditLogFacetsQuery> {
    const query: AuditLogFacetsQuery = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'AuditLogFacetsQuery',
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/auditlogfacetsqueries',
      {
        method: 'POST',
        body: JSON.stringify(query),
      }
    );

    return this.parseJson(response);
  }

  /**
   * Get all API groups that have audit log data
   * Uses the AuditLogFacetsQuery API to discover API groups from actual audit logs.
   */
  async getAuditedAPIGroups(): Promise<string[]> {
    try {
      const result = await this.queryAuditLogFacets({
        timeRange: { start: 'now-30d' },
        facets: [{ field: 'objectRef.apiGroup', limit: 100 }],
      });
      const apiGroupFacet = result.status?.facets?.find(f => f.field === 'objectRef.apiGroup');
      return apiGroupFacet?.values?.map(v => v.value).filter(v => v) || [];
    } catch {
      return [];
    }
  }

  /**
   * Get resource types for an API group that have audit log data
   * Uses the AuditLogFacetsQuery API to discover resources from actual audit logs.
   */
  async getAuditedResources(apiGroup: string): Promise<string[]> {
    try {
      const result = await this.queryAuditLogFacets({
        timeRange: { start: 'now-30d' },
        filter: `objectRef.apiGroup == "${apiGroup}"`,
        facets: [{ field: 'objectRef.resource', limit: 100 }],
      });
      const resourceFacet = result.status?.facets?.find(f => f.field === 'objectRef.resource');
      return resourceFacet?.values?.map(v => v.value).filter(v => v) || [];
    } catch {
      return [];
    }
  }

  // ============================================
  // PolicyPreview API Methods
  // ============================================

  /**
   * Create a PolicyPreview to test a policy against sample input
   * This is a virtual resource that executes immediately and returns results
   */
  async createPolicyPreview(spec: PolicyPreviewSpec): Promise<PolicyPreview> {
    const preview: PolicyPreview = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'PolicyPreview',
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/policypreviews',
      {
        method: 'POST',
        body: JSON.stringify(preview),
      }
    );

    return this.parseJson(response);
  }

  // ============================================
  // Kubernetes Events API Methods
  // ============================================

  /**
   * List Kubernetes events with optional filtering and pagination
   */
  async listEvents(params?: K8sEventListParams): Promise<K8sEventList> {
    const searchParams = new URLSearchParams();

    if (params?.fieldSelector) searchParams.set('fieldSelector', params.fieldSelector);
    if (params?.labelSelector) searchParams.set('labelSelector', params.labelSelector);
    if (params?.limit) searchParams.set('limit', String(params.limit));
    if (params?.continue) searchParams.set('continue', params.continue);

    const queryString = searchParams.toString();

    // Use the standard Kubernetes events.k8s.io/v1 API for real-time events
    // For historical queries (up to 60 days), use createEventQuery() instead
    const basePath = params?.namespace
      ? `/apis/events.k8s.io/v1/namespaces/${params.namespace}/events`
      : '/apis/events.k8s.io/v1/events';

    const path = `${basePath}${queryString ? `?${queryString}` : ''}`;

    const response = await this.fetch(path);
    return this.parseJson(response);
  }

  /**
   * Query event facets for filtering UI (autocomplete, distinct values)
   */
  async queryEventFacets(spec: EventFacetQuerySpec): Promise<EventFacetQuery> {
    const query: EventFacetQuery = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'EventFacetQuery',
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/eventfacetqueries',
      {
        method: 'POST',
        body: JSON.stringify(query),
      }
    );

    return this.parseJson(response);
  }

  /**
   * Create an EventQuery to search historical events from ClickHouse.
   * Unlike the live events API (limited to 24 hours), EventQuery supports up to 60 days of history.
   *
   * Returns EventRecord objects with event data nested under the `.event` field.
   *
   * @param spec - Query specification with time range and filters
   * @returns EventQuery with results in status.results as EventRecord[]
   */
  async createEventQuery(spec: EventQuerySpec): Promise<EventQuery> {
    const query: EventQuery = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'EventQuery',
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/eventqueries',
      {
        method: 'POST',
        body: JSON.stringify(query),
      }
    );

    return this.parseJson(response);
  }

  /**
   * Watch events in real-time using the Kubernetes watch API.
   * Returns a stop function and provides callbacks for handling events.
   *
   * @param params - Query parameters (fieldSelector, namespace, etc.)
   * @param options - Watch options
   * @returns Object with stop function
   */
  watchEvents(
    params?: Omit<K8sEventListParams, 'watch'>,
    options?: {
      /** Resource version to start watching from */
      resourceVersion?: string;
      /** Callback when an event is received */
      onEvent?: (event: WatchEvent<K8sEvent>) => void;
      /** Callback when an error occurs */
      onError?: (error: Error) => void;
      /** Callback when the connection closes */
      onClose?: () => void;
    }
  ): { stop: () => void } {
    const abortController = new AbortController();

    // Build URL with watch=true
    const searchParams = new URLSearchParams();
    searchParams.set('watch', 'true');

    if (params?.fieldSelector) searchParams.set('fieldSelector', params.fieldSelector);
    if (params?.labelSelector) searchParams.set('labelSelector', params.labelSelector);
    if (options?.resourceVersion) searchParams.set('resourceVersion', options.resourceVersion);

    const queryString = searchParams.toString();

    // Use the standard Kubernetes events.k8s.io/v1 API for real-time watch
    // For historical queries (up to 60 days), use createEventQuery() instead
    const basePath = params?.namespace
      ? `/apis/events.k8s.io/v1/namespaces/${params.namespace}/events`
      : '/apis/events.k8s.io/v1/events';

    const path = `${basePath}?${queryString}`;
    const url = `${this.config.baseUrl}${path}`;

    const headers: Record<string, string> = {
      'Accept': 'application/json',
    };

    if (this.config.token) {
      headers['Authorization'] = `Bearer ${this.config.token}`;
    }

    // Start the watch connection
    const startWatch = async () => {
      try {
        const response = await this.config.fetch!(url, {
          headers,
          signal: abortController.signal,
        });

        if (!response.ok) {
          const error = await response.text();
          throw new Error(`Watch request failed: ${response.status} ${error}`);
        }

        if (!response.body) {
          throw new Error('Response body is not available');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        for (;;) {
          const { done, value } = await reader.read();

          if (done) {
            options?.onClose?.();
            break;
          }

          buffer += decoder.decode(value, { stream: true });

          // Process complete lines (newline-delimited JSON)
          const lines = buffer.split('\n');
          buffer = lines.pop() || ''; // Keep incomplete line in buffer

          for (const line of lines) {
            if (line.trim()) {
              try {
                const event = JSON.parse(line) as WatchEvent<K8sEvent>;
                options?.onEvent?.(event);
              } catch (parseError) {
                console.warn('Failed to parse watch event:', parseError, line);
              }
            }
          }
        }
      } catch (error) {
        if ((error as Error).name === 'AbortError') {
          options?.onClose?.();
          return;
        }
        options?.onError?.(error as Error);
      }
    };

    // Start watching in background
    startWatch();

    return {
      stop: () => abortController.abort(),
    };
  }

  // ReindexJob API Methods

  /**
   * List all ReindexJobs
   */
  async listReindexJobs(): Promise<ReindexJobListResource> {
    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/reindexjobs'
    );
    return this.parseJson(response);
  }

  /**
   * Get a specific ReindexJob by name
   */
  async getReindexJob(name: string): Promise<ReindexJob> {
    const response = await this.fetch(
      `/apis/activity.miloapis.com/v1alpha1/reindexjobs/${name}`
    );
    return this.parseJson(response);
  }

  /**
   * Create a new ReindexJob
   * @param name Job name
   * @param spec Job specification
   */
  async createReindexJob(
    name: string,
    spec: ReindexJobSpec
  ): Promise<ReindexJob> {
    const job: ReindexJob = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'ReindexJob',
      metadata: { name },
      spec,
    };

    const response = await this.fetch(
      '/apis/activity.miloapis.com/v1alpha1/reindexjobs',
      {
        method: 'POST',
        body: JSON.stringify(job),
      }
    );

    return this.parseJson(response);
  }

  /**
   * Delete a ReindexJob by name
   */
  async deleteReindexJob(name: string): Promise<void> {
    await this.fetch(
      `/apis/activity.miloapis.com/v1alpha1/reindexjobs/${name}`,
      { method: 'DELETE' }
    );
  }

  /**
   * Watch ReindexJobs in real-time using the Kubernetes watch API.
   * Returns a stop function and provides callbacks for handling events.
   *
   * @param options - Watch options
   * @returns Object with stop function
   */
  watchReindexJobs(
    options?: {
      /** Resource version to start watching from */
      resourceVersion?: string;
      /** Callback when an event is received */
      onEvent?: (event: WatchEvent<ReindexJob>) => void;
      /** Callback when an error occurs */
      onError?: (error: Error) => void;
      /** Callback when the connection closes */
      onClose?: () => void;
    }
  ): { stop: () => void } {
    const abortController = new AbortController();

    // Build URL with watch=true
    const searchParams = new URLSearchParams();
    searchParams.set('watch', 'true');

    if (options?.resourceVersion) searchParams.set('resourceVersion', options.resourceVersion);

    const queryString = searchParams.toString();
    const path = `/apis/activity.miloapis.com/v1alpha1/reindexjobs?${queryString}`;
    const url = `${this.config.baseUrl}${path}`;

    const headers: Record<string, string> = {
      'Accept': 'application/json',
    };

    if (this.config.token) {
      headers['Authorization'] = `Bearer ${this.config.token}`;
    }

    // Start the watch connection
    const startWatch = async () => {
      try {
        const response = await this.config.fetch!(url, {
          headers,
          signal: abortController.signal,
        });

        if (!response.ok) {
          const error = await response.text();
          throw new Error(`Watch request failed: ${response.status} ${error}`);
        }

        if (!response.body) {
          throw new Error('Response body is not available');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        for (;;) {
          const { done, value } = await reader.read();

          if (done) {
            options?.onClose?.();
            break;
          }

          buffer += decoder.decode(value, { stream: true });

          // Process complete lines (newline-delimited JSON)
          const lines = buffer.split('\n');
          buffer = lines.pop() || ''; // Keep incomplete line in buffer

          for (const line of lines) {
            if (line.trim()) {
              try {
                const event = JSON.parse(line) as WatchEvent<ReindexJob>;
                options?.onEvent?.(event);
              } catch (parseError) {
                console.warn('Failed to parse watch event:', parseError, line);
              }
            }
          }
        }
      } catch (error) {
        if ((error as Error).name === 'AbortError') {
          options?.onClose?.();
          return;
        }
        options?.onError?.(error as Error);
      }
    };

    // Start watching in background
    startWatch();

    return {
      stop: () => abortController.abort(),
    };
  }

  private async fetch(path: string, init?: RequestInit): Promise<Response> {
    const url = `${this.config.baseUrl}${path}`;
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(init?.headers as Record<string, string> || {}),
    };

    if (this.config.token) {
      headers['Authorization'] = `Bearer ${this.config.token}`;
    }

    try {
      const response = await this.config.fetch!(url, {
        ...init,
        headers,
      });

      if (!response.ok) {
        const errorBody = await response.text();
        throw parseApiError(response.status, errorBody);
      }

      return response;
    } catch (error) {
      // If it's already an ApiError, re-throw it
      if (error instanceof Error && error.name === 'ApiError') {
        throw error;
      }

      // Network errors (connection refused, DNS failure, CORS, etc.)
      // These are thrown by fetch as TypeError
      if (error instanceof TypeError) {
        throw new NetworkError(error.message);
      }

      // Re-throw other errors as-is
      throw error;
    }
  }

  /**
   * Parse JSON response and apply optional transformer.
   * Use this instead of response.json() to support proxy response unwrapping.
   */
  private async parseJson<T>(response: Response): Promise<T> {
    const json = await response.json();

    if (this.config.responseTransformer) {
      const transformed = this.config.responseTransformer(json);
      return transformed as T;
    }

    return json as T;
  }
}
