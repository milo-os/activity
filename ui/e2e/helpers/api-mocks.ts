import type { Page, Route } from '@playwright/test';
import type { ActivityPolicyList, ActivityPolicy, PolicyPreview, Event } from '../../src/types';

/**
 * API mock helpers for E2E tests
 * Provides reusable functions to mock ActivityPolicy and related API endpoints
 */

/**
 * Options for mocking API responses
 */
export interface MockApiOptions {
  /** Delay response in milliseconds (useful for testing loading states) */
  delay?: number;
  /** Return an error response */
  error?: {
    status: number;
    message: string;
  };
  /** Slice results by spec.limit and return a continue token while more remain. */
  paginate?: boolean;
}

/**
 * Mock GET /activitypolicies endpoint
 * @param page - Playwright page instance
 * @param policyList - Mock policy list data to return (optional)
 * @param options - Mock options (delay, error)
 */
export async function mockPolicyListAPI(
  page: Page,
  policyList?: ActivityPolicyList,
  options?: MockApiOptions
) {
  await page.route('**/activitypolicies', async (route: Route) => {
    // Only intercept GET requests
    if (route.request().method() !== 'GET') {
      return route.continue();
    }

    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    return route.fulfill({
      status: 200,
      json: policyList || { items: [] },
    });
  });
}

/**
 * Mock GET /activitypolicies/:name endpoint
 * @param page - Playwright page instance
 * @param name - Policy name to mock
 * @param policy - Mock policy data to return (optional)
 * @param options - Mock options (delay, error)
 */
export async function mockPolicyDetailAPI(
  page: Page,
  name: string,
  policy?: ActivityPolicy,
  options?: MockApiOptions
) {
  await page.route(`**/activitypolicies/${name}`, async (route: Route) => {
    // Only intercept GET requests
    if (route.request().method() !== 'GET') {
      return route.continue();
    }

    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    if (!policy) {
      return route.fulfill({
        status: 404,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: `activitypolicies "${name}" not found`,
          code: 404,
        },
      });
    }

    return route.fulfill({
      status: 200,
      json: policy,
    });
  });
}

/**
 * Mock POST /activitypolicies endpoint (create)
 * @param page - Playwright page instance
 * @param options - Mock options (delay, error)
 */
export async function mockPolicyCreateAPI(
  page: Page,
  options?: MockApiOptions
) {
  await page.route('**/activitypolicies', async (route: Route) => {
    // Only intercept POST requests
    if (route.request().method() !== 'POST') {
      return route.continue();
    }

    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    // Echo back the request body as the created resource
    const requestBody = route.request().postDataJSON();
    return route.fulfill({
      status: 201,
      json: {
        ...requestBody,
        metadata: {
          ...requestBody.metadata,
          resourceVersion: '1',
          creationTimestamp: new Date().toISOString(),
        },
        status: {
          conditions: [
            {
              type: 'Ready',
              status: 'True',
              lastTransitionTime: new Date().toISOString(),
              reason: 'AllRulesCompiled',
              message: 'All rules compiled successfully',
            },
          ],
          observedGeneration: 1,
        },
      },
    });
  });
}

/**
 * Mock PUT /activitypolicies/:name endpoint (update)
 * @param page - Playwright page instance
 * @param name - Policy name to mock
 * @param options - Mock options (delay, error)
 */
export async function mockPolicyUpdateAPI(
  page: Page,
  name: string,
  options?: MockApiOptions
) {
  await page.route(`**/activitypolicies/${name}`, async (route: Route) => {
    // Only intercept PUT requests
    if (route.request().method() !== 'PUT') {
      return route.continue();
    }

    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    // Echo back the request body as the updated resource
    const requestBody = route.request().postDataJSON();
    return route.fulfill({
      status: 200,
      json: {
        ...requestBody,
        metadata: {
          ...requestBody.metadata,
          resourceVersion: String(Number(requestBody.metadata.resourceVersion || '1') + 1),
        },
        status: {
          conditions: [
            {
              type: 'Ready',
              status: 'True',
              lastTransitionTime: new Date().toISOString(),
              reason: 'AllRulesCompiled',
              message: 'All rules compiled successfully',
            },
          ],
          observedGeneration: requestBody.metadata.generation || 1,
        },
      },
    });
  });
}

/**
 * Mock DELETE /activitypolicies/:name endpoint
 * @param page - Playwright page instance
 * @param name - Policy name to mock
 * @param options - Mock options (delay, error)
 */
export async function mockPolicyDeleteAPI(
  page: Page,
  name: string,
  options?: MockApiOptions
) {
  await page.route(`**/activitypolicies/${name}`, async (route: Route) => {
    // Only intercept DELETE requests
    if (route.request().method() !== 'DELETE') {
      return route.continue();
    }

    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    return route.fulfill({
      status: 200,
      json: {
        kind: 'Status',
        apiVersion: 'v1',
        status: 'Success',
        message: `activitypolicies "${name}" deleted`,
      },
    });
  });
}

/**
 * Mock POST /policypreviews endpoint
 * @param page - Playwright page instance
 * @param result - Mock preview result (optional)
 * @param options - Mock options (delay, error)
 */
export async function mockPolicyPreviewAPI(
  page: Page,
  result?: PolicyPreview,
  options?: MockApiOptions
) {
  await page.route('**/policypreviews', async (route: Route) => {
    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    const defaultResult: PolicyPreview = {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'PolicyPreview',
      spec: route.request().postDataJSON().spec,
      status: {
        results: [],
        activities: [],
      },
    };

    return route.fulfill({
      status: 200,
      json: result || defaultResult,
    });
  });
}

/**
 * Mock POST /auditlogqueries endpoint
 * @param page - Playwright page instance
 * @param events - Mock events to return (optional)
 * @param options - Mock options (delay, error)
 */
export async function mockAuditLogQueryAPI(
  page: Page,
  events?: Event[],
  options?: MockApiOptions
) {
  await page.route('**/auditlogqueries', async (route: Route) => {
    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    return route.fulfill({
      status: 200,
      json: {
        apiVersion: 'activity.miloapis.com/v1alpha1',
        kind: 'AuditLogQuery',
        spec: route.request().postDataJSON().spec,
        status: {
          results: events || [],
        },
      },
    });
  });
}

/**
 * Activity data for mocking activity queries
 */
export interface MockActivity {
  metadata: {
    name: string;
    uid?: string;
    creationTimestamp?: string;
  };
  spec: {
    summary: string;
    changeSource: 'human' | 'system';
    actor?: {
      name: string;
      type: string;
    };
    resource?: {
      apiGroup: string;
      kind: string;
      name: string;
      namespace?: string;
    };
    timestamp?: string;
  };
}

/**
 * Mock POST /activityqueries endpoint
 * @param page - Playwright page instance
 * @param activities - Mock activities to return (optional)
 * @param options - Mock options (delay, error)
 */
/**
 * Read the changeSource an ActivityQuery request asks for.
 * useActivityFeed sends it as a CEL clause in spec.filter
 * (`spec.changeSource == "human"`), never as a top-level field.
 */
export function changeSourceFromSpec(spec: { filter?: string } | undefined): string | undefined {
  return spec?.filter?.match(/spec\.changeSource == "([^"]+)"/)?.[1];
}

export async function mockActivityQueryAPI(
  page: Page,
  activities?: MockActivity[],
  options?: MockApiOptions
) {
  await page.route('**/activityqueries', async (route: Route) => {
    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    const request = route.request().postDataJSON();
    const changeSourceFilter = changeSourceFromSpec(request?.spec);

    // Filter activities by changeSource if specified
    let filteredActivities = activities || [];
    if (changeSourceFilter && activities) {
      filteredActivities = activities.filter(a => a.spec.changeSource === changeSourceFilter);
    }

    const limit: number = request?.spec?.limit ?? filteredActivities.length;
    const offset: number = options?.paginate && request?.spec?.continue
      ? Number(request.spec.continue)
      : 0;
    const page = options?.paginate ? filteredActivities.slice(offset, offset + limit) : filteredActivities;
    const nextOffset = offset + page.length;
    const hasMore = options?.paginate && nextOffset < filteredActivities.length;

    return route.fulfill({
      status: 200,
      json: {
        apiVersion: 'activity.miloapis.com/v1alpha1',
        kind: 'ActivityQuery',
        spec: request?.spec || {},
        status: {
          results: page,
          ...(hasMore ? { continue: String(nextOffset) } : {}),
        },
      },
    });
  });
}

/**
 * Mock K8s event data for mocking event queries
 */
export interface MockK8sEvent {
  metadata: {
    name: string;
    namespace: string;
    uid?: string;
    creationTimestamp?: string;
  };
  involvedObject: {
    apiVersion: string;
    kind: string;
    name: string;
    namespace?: string;
  };
  reason: string;
  message: string;
  type: 'Normal' | 'Warning';
  source?: {
    component: string;
  };
  firstTimestamp?: string;
  lastTimestamp?: string;
  count?: number;
}

/**
 * Mock POST /eventqueries endpoint
 * @param page - Playwright page instance
 * @param events - Mock events to return (optional)
 * @param options - Mock options (delay, error)
 */
export async function mockEventQueryAPI(
  page: Page,
  events?: MockK8sEvent[],
  options?: MockApiOptions
) {
  await page.route('**/eventqueries', async (route: Route) => {
    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    const request = route.request().postDataJSON();

    return route.fulfill({
      status: 200,
      json: {
        apiVersion: 'activity.miloapis.com/v1alpha1',
        kind: 'EventQuery',
        spec: request?.spec || {},
        status: {
          results: events || [],
        },
      },
    });
  });
}

/**
 * Facet data for mocking facet queries
 */
export interface MockFacetValue {
  value: string;
  count: number;
}

export interface MockFacets {
  [field: string]: MockFacetValue[];
}

/**
 * Mock POST /eventfacetqueries endpoint
 * @param page - Playwright page instance
 * @param facets - Mock facets to return (optional)
 * @param options - Mock options (delay, error)
 */
export async function mockEventFacetQueryAPI(
  page: Page,
  facets?: MockFacets,
  options?: MockApiOptions
) {
  await page.route('**/eventfacetqueries', async (route: Route) => {
    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    const request = route.request().postDataJSON();
    const defaultFacets: MockFacets = {
      'regarding.kind': [
        { value: 'Pod', count: 45 },
        { value: 'Deployment', count: 23 },
        { value: 'Service', count: 12 },
        { value: 'ConfigMap', count: 8 },
      ],
      reason: [
        { value: 'Scheduled', count: 30 },
        { value: 'Pulled', count: 25 },
        { value: 'Created', count: 20 },
        { value: 'Started', count: 18 },
        { value: 'FailedScheduling', count: 5 },
      ],
      'regarding.namespace': [
        { value: 'default', count: 50 },
        { value: 'kube-system', count: 30 },
        { value: 'monitoring', count: 15 },
      ],
      'source.component': [
        { value: 'kubelet', count: 40 },
        { value: 'kube-scheduler', count: 25 },
        { value: 'deployment-controller', count: 15 },
      ],
    };

    return route.fulfill({
      status: 200,
      json: {
        apiVersion: 'activity.miloapis.com/v1alpha1',
        kind: 'EventFacetQuery',
        spec: request?.spec || {},
        status: {
          facets: facets || defaultFacets,
        },
      },
    });
  });
}

/**
 * Mock POST /activityfacetqueries endpoint
 * @param page - Playwright page instance
 * @param facets - Mock facets to return (optional)
 * @param options - Mock options (delay, error)
 */
export async function mockActivityFacetQueryAPI(
  page: Page,
  facets?: MockFacets,
  options?: MockApiOptions
) {
  await page.route('**/activityfacetqueries', async (route: Route) => {
    if (options?.delay) {
      await new Promise(resolve => setTimeout(resolve, options.delay));
    }

    if (options?.error) {
      return route.fulfill({
        status: options.error.status,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: options.error.message,
          code: options.error.status,
        },
      });
    }

    const request = route.request().postDataJSON();

    return route.fulfill({
      status: 200,
      json: {
        apiVersion: 'activity.miloapis.com/v1alpha1',
        kind: 'ActivityFacetQuery',
        spec: request?.spec || {},
        status: {
          facets: facets || {},
        },
      },
    });
  });
}
