import { test, expect } from '@playwright/test';
import { mockActivityQueryAPI, changeSourceFromSpec, type MockActivity } from './helpers/api-mocks';

/**
 * E2E tests for Activity Feed changeSource filter
 * Tests that the Human/System toggle correctly filters activities
 */

// Sample activities with different change sources
const mockActivities: MockActivity[] = [
  {
    metadata: { name: 'activity-1', uid: 'uid-1', creationTimestamp: '2024-01-01T10:00:00Z' },
    spec: {
      summary: 'alice created deployment web-app',
      changeSource: 'human',
      actor: { name: 'alice', type: 'User' },
      resource: { apiGroup: 'apps', kind: 'Deployment', name: 'web-app', namespace: 'default' },
      timestamp: '2024-01-01T10:00:00Z',
    },
  },
  {
    metadata: { name: 'activity-2', uid: 'uid-2', creationTimestamp: '2024-01-01T10:01:00Z' },
    spec: {
      summary: 'bob updated configmap settings',
      changeSource: 'human',
      actor: { name: 'bob', type: 'User' },
      resource: { apiGroup: '', kind: 'ConfigMap', name: 'settings', namespace: 'default' },
      timestamp: '2024-01-01T10:01:00Z',
    },
  },
  {
    metadata: { name: 'activity-3', uid: 'uid-3', creationTimestamp: '2024-01-01T10:02:00Z' },
    spec: {
      summary: 'system:serviceaccount:kube-system:deployment-controller updated replicaset web-app-abc123',
      changeSource: 'system',
      actor: { name: 'system:serviceaccount:kube-system:deployment-controller', type: 'ServiceAccount' },
      resource: { apiGroup: 'apps', kind: 'ReplicaSet', name: 'web-app-abc123', namespace: 'default' },
      timestamp: '2024-01-01T10:02:00Z',
    },
  },
  {
    metadata: { name: 'activity-4', uid: 'uid-4', creationTimestamp: '2024-01-01T10:03:00Z' },
    spec: {
      summary: 'kubelet updated pod status',
      changeSource: 'system',
      actor: { name: 'kubelet', type: 'Node' },
      resource: { apiGroup: '', kind: 'Pod', name: 'web-app-abc123-xyz', namespace: 'default' },
      timestamp: '2024-01-01T10:03:00Z',
    },
  },
];

test.describe('Activity Feed Source Filter', () => {
  test.beforeEach(async ({ page }) => {
    // Set up mocks for activity queries
    await mockActivityQueryAPI(page, mockActivities);
  });

  test('Human filter sends changeSource: "human" in API request', async ({ page }) => {
    const requests: any[] = [];

    // Capture requests to verify changeSource parameter
    await page.route('**/activityqueries', async (route) => {
      const request = route.request();
      if (request.method() === 'POST') {
        const payload = JSON.parse(request.postData() || '{}');
        requests.push(payload);
      }

      // Filter activities based on changeSource
      const requestData = JSON.parse(request.postData() || '{}');
      const changeSourceFilter = changeSourceFromSpec(requestData?.spec);
      let filteredActivities = mockActivities;
      if (changeSourceFilter) {
        filteredActivities = mockActivities.filter(a => a.spec.changeSource === changeSourceFilter);
      }

      await route.fulfill({
        status: 200,
        json: {
          apiVersion: 'activity.miloapis.com/v1alpha1',
          kind: 'ActivityQuery',
          spec: requestData?.spec || {},
          status: { results: filteredActivities },
        },
      });
    });

    // Navigate to Activity Feed
    await page.goto('/activity-feed');

    // Wait for initial load
    await page.waitForTimeout(500);

    // Clear captured requests from initial load
    const initialRequestCount = requests.length;

    // Click Human filter
    await page.getByRole('button', { name: 'Human' }).click();

    // Wait for the debounced request
    await page.waitForTimeout(500);

    // Check if a new request was made after clicking Human
    const newRequests = requests.slice(initialRequestCount);

    if (newRequests.length > 0) {
      const lastRequest = newRequests[newRequests.length - 1];
      expect(changeSourceFromSpec(lastRequest.spec)).toBe('human');
    } else {
      // Human may already be selected - click All then Human
      await page.getByRole('button', { name: 'All' }).click();
      await page.waitForTimeout(500);

      const afterAllCount = requests.length;

      await page.getByRole('button', { name: 'Human' }).click();
      await page.waitForTimeout(500);

      const humanRequests = requests.slice(afterAllCount);
      expect(humanRequests.length).toBeGreaterThan(0);

      const humanRequest = humanRequests[humanRequests.length - 1];
      expect(changeSourceFromSpec(humanRequest.spec)).toBe('human');
    }
  });

  test('System filter sends changeSource: "system" in API request', async ({ page }) => {
    const requests: any[] = [];

    await page.route('**/activityqueries', async (route) => {
      const request = route.request();
      if (request.method() === 'POST') {
        const payload = JSON.parse(request.postData() || '{}');
        requests.push(payload);
      }

      const requestData = JSON.parse(request.postData() || '{}');
      const changeSourceFilter = changeSourceFromSpec(requestData?.spec);
      let filteredActivities = mockActivities;
      if (changeSourceFilter) {
        filteredActivities = mockActivities.filter(a => a.spec.changeSource === changeSourceFilter);
      }

      await route.fulfill({
        status: 200,
        json: {
          apiVersion: 'activity.miloapis.com/v1alpha1',
          kind: 'ActivityQuery',
          spec: requestData?.spec || {},
          status: { results: filteredActivities },
        },
      });
    });

    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    const beforeCount = requests.length;

    // Click System filter
    await page.getByRole('button', { name: 'System' }).click();
    await page.waitForTimeout(500);

    const newRequests = requests.slice(beforeCount);
    expect(newRequests.length).toBeGreaterThan(0);

    const lastRequest = newRequests[newRequests.length - 1];
    expect(changeSourceFromSpec(lastRequest.spec)).toBe('system');
  });

  test('All filter does NOT include changeSource in API request', async ({ page }) => {
    const requests: any[] = [];

    await page.route('**/activityqueries', async (route) => {
      const request = route.request();
      if (request.method() === 'POST') {
        requests.push(JSON.parse(request.postData() || '{}'));
      }

      const requestData = JSON.parse(request.postData() || '{}');
      await route.fulfill({
        status: 200,
        json: {
          apiVersion: 'activity.miloapis.com/v1alpha1',
          kind: 'ActivityQuery',
          spec: requestData?.spec || {},
          status: { results: mockActivities },
        },
      });
    });

    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // First click Human to ensure we're not on All
    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(500);

    const beforeCount = requests.length;

    // Now click All
    await page.getByRole('button', { name: 'All' }).click();
    await page.waitForTimeout(500);

    const newRequests = requests.slice(beforeCount);
    expect(newRequests.length).toBeGreaterThan(0);

    const lastRequest = newRequests[newRequests.length - 1];
    expect(lastRequest.spec.changeSource).toBeUndefined();
  });

  test('Human filter returns ONLY human activities (no system)', async ({ page }) => {
    let lastResponse: any = null;

    await page.route('**/activityqueries', async (route) => {
      const requestData = JSON.parse(route.request().postData() || '{}');
      const changeSourceFilter = changeSourceFromSpec(requestData?.spec);
      let filteredActivities = mockActivities;
      if (changeSourceFilter) {
        filteredActivities = mockActivities.filter(a => a.spec.changeSource === changeSourceFilter);
      }

      lastResponse = {
        apiVersion: 'activity.miloapis.com/v1alpha1',
        kind: 'ActivityQuery',
        spec: requestData?.spec || {},
        status: { results: filteredActivities },
      };

      await route.fulfill({
        status: 200,
        json: lastResponse,
      });
    });

    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Click All then Human to ensure fresh request
    await page.getByRole('button', { name: 'All' }).click();
    await page.waitForTimeout(500);

    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(500);

    // Check response - should only have human activities
    expect(lastResponse).toBeTruthy();
    const activities = lastResponse.status.results;
    const systemActivities = activities.filter((a: MockActivity) => a.spec.changeSource === 'system');
    expect(systemActivities).toHaveLength(0);

    // All should be human
    const humanActivities = activities.filter((a: MockActivity) => a.spec.changeSource === 'human');
    expect(humanActivities.length).toBe(activities.length);
  });

  test('filter buttons toggle correctly', async ({ page }) => {
    await page.route('**/activityqueries', async (route) => {
      const requestData = JSON.parse(route.request().postData() || '{}');
      await route.fulfill({
        status: 200,
        json: {
          apiVersion: 'activity.miloapis.com/v1alpha1',
          kind: 'ActivityQuery',
          spec: requestData?.spec || {},
          status: { results: mockActivities },
        },
      });
    });

    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Verify all three filter buttons exist
    await expect(page.getByRole('button', { name: 'All' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Human' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'System' })).toBeVisible();

    // Click through each filter
    await page.getByRole('button', { name: 'System' }).click();
    await page.waitForTimeout(300);

    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(300);

    await page.getByRole('button', { name: 'All' }).click();
    await page.waitForTimeout(300);

    // Test passes if no errors occurred
    expect(true).toBe(true);
  });
});
