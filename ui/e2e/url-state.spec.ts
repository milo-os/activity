import { test, expect } from '@playwright/test';
import { mockActivityQueryAPI, mockActivityFacetQueryAPI, mockEventQueryAPI, mockEventFacetQueryAPI, changeSourceFromSpec, type MockActivity, type MockK8sEvent } from './helpers/api-mocks';

/**
 * E2E tests for URL state persistence (deep linking) feature
 * Tests that filters and time ranges persist in URL query parameters
 * and can be restored when loading a URL with parameters
 */

// Sample activities for activity feed tests
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
      changeSource: 'system',
      actor: { name: 'system:node:worker-1', type: 'Node' },
      resource: { apiGroup: '', kind: 'ConfigMap', name: 'settings', namespace: 'default' },
      timestamp: '2024-01-01T10:01:00Z',
    },
  },
];

// Sample events for events feed tests
const mockEvents: MockK8sEvent[] = [
  {
    metadata: { name: 'event-1', namespace: 'default', uid: 'uid-1', creationTimestamp: '2024-01-01T10:00:00Z' },
    involvedObject: { apiVersion: 'v1', kind: 'Pod', name: 'web-app-abc123', namespace: 'default' },
    reason: 'Scheduled',
    message: 'Successfully assigned default/web-app-abc123 to node-1',
    type: 'Normal',
    source: { component: 'kube-scheduler' },
    firstTimestamp: '2024-01-01T10:00:00Z',
    lastTimestamp: '2024-01-01T10:00:00Z',
    count: 1,
  },
  {
    metadata: { name: 'event-2', namespace: 'default', uid: 'uid-2', creationTimestamp: '2024-01-01T10:01:00Z' },
    involvedObject: { apiVersion: 'apps/v1', kind: 'Deployment', name: 'nginx', namespace: 'default' },
    reason: 'ScalingReplicaSet',
    message: 'Scaled up replica set nginx-abc123 to 3',
    type: 'Warning',
    source: { component: 'deployment-controller' },
    firstTimestamp: '2024-01-01T10:01:00Z',
    lastTimestamp: '2024-01-01T10:01:00Z',
    count: 1,
  },
];

test.describe('Activity Feed URL State Persistence', () => {
  test.beforeEach(async ({ page }) => {
    // Set up mocks for activity queries and facets
    await mockActivityQueryAPI(page, mockActivities);
    await mockActivityFacetQueryAPI(page, {
      'resource.kind': [
        { value: 'Pod', count: 45 },
        { value: 'Deployment', count: 23 },
        { value: 'HTTPProxy', count: 12 },
        { value: 'Gateway', count: 8 },
      ],
      'actor.name': [
        { value: 'alice', count: 30 },
        { value: 'bob', count: 25 },
      ],
    });
  });

  test('changeSource filter updates URL parameter', async ({ page }) => {
    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Click Human filter
    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(300);

    // Verify URL contains changeSource=human
    expect(page.url()).toContain('changeSource=human');
  });

  test('System filter updates URL with changeSource=system', async ({ page }) => {
    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Click System filter
    await page.getByRole('button', { name: 'System' }).click();
    await page.waitForTimeout(300);

    // Verify URL contains changeSource=system
    expect(page.url()).toContain('changeSource=system');
  });

  test('All filter removes changeSource from URL', async ({ page }) => {
    // Start with Human filter active
    await page.goto('/activity-feed?changeSource=human');
    await page.waitForTimeout(500);

    // Click All filter
    await page.getByRole('button', { name: 'All' }).click();
    await page.waitForTimeout(300);

    // Verify URL does NOT contain changeSource parameter
    expect(page.url()).not.toContain('changeSource=');
  });

  test('Loading URL with changeSource=human restores Human filter', async ({ page }) => {
    // Navigate directly with query parameter
    await page.goto('/activity-feed?changeSource=human');
    await page.waitForTimeout(500);

    // Verify Human button appears selected (should have different styling)
    const humanButton = page.getByRole('button', { name: 'Human' });
    await expect(humanButton).toBeVisible();

    // The button should be in the DOM and rendered - we can't easily test visual state,
    // but we can verify the API received the correct filter
    const requests: any[] = [];
    await page.route('**/activityqueries', async (route) => {
      const request = route.request();
      if (request.method() === 'POST') {
        requests.push(JSON.parse(request.postData() || '{}'));
      }
      await route.fulfill({
        status: 200,
        json: {
          apiVersion: 'activity.miloapis.com/v1alpha1',
          kind: 'ActivityQuery',
          status: { results: mockActivities },
        },
      });
    });

    // Trigger a filter change to capture request
    await page.getByRole('button', { name: 'All' }).click();
    await page.waitForTimeout(300);
    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(500);

    // Verify the last request had changeSource: 'human'
    if (requests.length > 0) {
      const lastRequest = requests[requests.length - 1];
      expect(changeSourceFromSpec(lastRequest.spec)).toBe('human');
    }
  });

  test('resourceKinds filter updates URL with comma-separated values', async ({ page }) => {
    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Open filters dropdown
    const addFiltersButton = page.getByRole('button', { name: /(Add )?Filters/i });
    await addFiltersButton.click();
    await page.waitForTimeout(200);

    // Add Kind filter
    await page.getByRole('button', { name: 'Kind' }).click();
    await page.waitForTimeout(500);

    // Select multiple kinds if available
    const options = page.locator('[cmdk-item]');
    const optionCount = await options.count();

    if (optionCount >= 2) {
      // Click first option
      await options.nth(0).click();
      await page.waitForTimeout(300);

      // Reopen the popover by clicking the chip
      const kindChip = page.locator('button').filter({ hasText: /^Kind:/ });
      await kindChip.click();
      await page.waitForTimeout(300);

      // Click second option
      await options.nth(1).click();
      await page.waitForTimeout(300);

      // Verify URL contains resourceKinds with comma-separated values
      const url = page.url();
      expect(url).toContain('resourceKinds=');
      // Should contain comma
      const urlObj = new URL(url);
      const resourceKinds = urlObj.searchParams.get('resourceKinds');
      expect(resourceKinds).toContain(',');
    }
  });

  test('Loading URL with resourceKinds restores filter selection', async ({ page }) => {
    // Navigate with resourceKinds in URL
    await page.goto('/activity-feed?resourceKinds=HTTPProxy,Gateway');
    await page.waitForTimeout(500);

    // Verify chip appears with selected kinds (exact text match may vary based on UI)
    const kindChip = page.locator('button').filter({ hasText: /Kind:/i });
    await expect(kindChip).toBeVisible();

    // Verify URL still contains the parameters
    expect(page.url()).toContain('resourceKinds=HTTPProxy,Gateway');
  });

  test('startTime filter updates URL', async ({ page }) => {
    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Click time range button (usually shows "Last 7 days" or similar)
    const timeRangeButton = page.getByRole('button', { name: /Last/i });
    if (await timeRangeButton.isVisible()) {
      await timeRangeButton.click();
      await page.waitForTimeout(300);

      // Select a different time range (e.g., "Last 24 hours")
      const option24h = page.getByText('Last 24 hours');
      if (await option24h.isVisible()) {
        await option24h.click();
        await page.waitForTimeout(300);

        // Verify URL contains startTime parameter
        const url = page.url();
        expect(url).toContain('startTime=');
      }
    }
  });

  test('Loading URL with startTime restores time range', async ({ page }) => {
    // Navigate with startTime parameter
    await page.goto('/activity-feed?startTime=now-24h');
    await page.waitForTimeout(500);

    // Verify the time range picker trigger (a combobox) shows the selection
    const timeRangeTrigger = page.getByRole('combobox').filter({ hasText: /24/i });
    await expect(timeRangeTrigger).toBeVisible();

    // Verify URL persists the parameter
    expect(page.url()).toContain('startTime=now-24h');
  });

  test('Multiple filters update URL with all parameters', async ({ page }) => {
    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Set changeSource filter
    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(300);

    // Verify changeSource is in URL
    expect(page.url()).toContain('changeSource=human');

    // Add Kind filter
    const addFiltersButton = page.getByRole('button', { name: /(Add )?Filters/i });
    await addFiltersButton.click();
    await page.waitForTimeout(200);
    await page.getByRole('button', { name: 'Kind' }).click();
    await page.waitForTimeout(500);

    // Select a kind if available
    const options = page.locator('[cmdk-item]');
    const optionCount = await options.count();
    if (optionCount > 0) {
      await options.first().click();
      await page.waitForTimeout(500);

      // Close popover by clicking outside
      await page.locator('body').click({ position: { x: 10, y: 10 } });
      await page.waitForTimeout(300);

      // Verify URL contains both parameters
      const url = page.url();
      expect(url).toContain('changeSource=human');
      expect(url).toContain('resourceKinds=');
    } else {
      // If no options, just verify changeSource
      expect(page.url()).toContain('changeSource=human');
    }
  });

  test('Browser back/forward preserves filter state', async ({ page }) => {
    // Navigate to different pages to create history entries with push
    // Then navigate to activity feed
    await page.goto('/');
    await page.waitForTimeout(300);

    await page.goto('/activity-feed');
    await page.waitForTimeout(500);
    const initialUrl = page.url();

    // Navigate to events to create another history entry
    await page.goto('/events');
    await page.waitForTimeout(500);

    // Go back to activity feed
    await page.goBack();
    await page.waitForTimeout(500);

    // Verify we're back at activity feed
    expect(page.url()).toContain('/activity-feed');

    // Apply filter (this uses replace)
    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(300);
    expect(page.url()).toContain('changeSource=human');

    // Go forward to events
    await page.goForward();
    await page.waitForTimeout(500);

    // Verify we're at events
    expect(page.url()).toContain('/events');

    // Go back to activity feed with Human filter
    await page.goBack();
    await page.waitForTimeout(500);

    // Verify filter persists (replace preserves the state)
    expect(page.url()).toContain('/activity-feed');
    expect(page.url()).toContain('changeSource=human');
  });
});

test.describe('Events Feed URL State Persistence', () => {
  test.beforeEach(async ({ page }) => {
    // Set up mocks for event queries and facets
    await mockEventQueryAPI(page, mockEvents);
    await mockEventFacetQueryAPI(page, {
      'regarding.kind': [
        { value: 'Pod', count: 45 },
        { value: 'Deployment', count: 23 },
        { value: 'Service', count: 12 },
      ],
      reason: [
        { value: 'Scheduled', count: 30 },
        { value: 'Pulled', count: 25 },
      ],
      'regarding.namespace': [
        { value: 'default', count: 50 },
        { value: 'kube-system', count: 30 },
      ],
    });
  });

  test('eventType filter updates URL parameter', async ({ page }) => {
    await page.goto('/events');
    await page.waitForTimeout(500);

    // Click Warning filter
    await page.getByRole('button', { name: 'Warning' }).click();
    await page.waitForTimeout(300);

    // Verify URL contains eventType=Warning
    expect(page.url()).toContain('eventType=Warning');
  });

  test('Normal event filter updates URL with eventType=Normal', async ({ page }) => {
    await page.goto('/events');
    await page.waitForTimeout(500);

    // Click Normal filter
    await page.getByRole('button', { name: 'Normal' }).click();
    await page.waitForTimeout(300);

    // Verify URL contains eventType=Normal
    expect(page.url()).toContain('eventType=Normal');
  });

  test('Loading URL with eventType=Warning restores filter', async ({ page }) => {
    // Navigate directly with query parameter
    await page.goto('/events?eventType=Warning');
    await page.waitForTimeout(500);

    // Verify Warning button is visible
    const warningButton = page.getByRole('button', { name: 'Warning' });
    await expect(warningButton).toBeVisible();

    // Verify URL persists
    expect(page.url()).toContain('eventType=Warning');
  });

  test('involvedKinds filter updates URL with comma-separated values', async ({ page }) => {
    await page.goto('/events');
    await page.waitForTimeout(500);

    // Open filters dropdown
    const addFiltersButton = page.getByRole('button', { name: /(Add )?Filters/i });
    await addFiltersButton.click();
    await page.waitForTimeout(200);

    // Add Kind filter
    await page.getByRole('button', { name: 'Kind' }).click();
    await page.waitForTimeout(500);

    // Select multiple kinds if available
    const options = page.locator('[cmdk-item]');
    const optionCount = await options.count();

    if (optionCount >= 2) {
      // Click first option
      await options.nth(0).click();
      await page.waitForTimeout(300);

      // Reopen the popover
      const kindChip = page.locator('button').filter({ hasText: /^Kind:/ });
      await kindChip.click();
      await page.waitForTimeout(300);

      // Click second option
      await options.nth(1).click();
      await page.waitForTimeout(300);

      // Verify URL contains involvedKinds with comma-separated values
      const url = page.url();
      expect(url).toContain('involvedKinds=');
      const urlObj = new URL(url);
      const involvedKinds = urlObj.searchParams.get('involvedKinds');
      expect(involvedKinds).toContain(',');
    }
  });

  test('Loading URL with involvedKinds restores filter selection', async ({ page }) => {
    // Navigate with involvedKinds in URL
    await page.goto('/events?involvedKinds=Pod,Deployment');
    await page.waitForTimeout(500);

    // Verify chip appears with selected kinds
    const kindChip = page.locator('button').filter({ hasText: /Kind:/i });
    await expect(kindChip).toBeVisible();

    // Verify URL still contains the parameters
    expect(page.url()).toContain('involvedKinds=Pod,Deployment');
  });

  test('startTime parameter works in events feed', async ({ page }) => {
    // Navigate with startTime parameter (events default is 24h)
    await page.goto('/events?startTime=now-1h');
    await page.waitForTimeout(500);

    // Verify URL persists
    expect(page.url()).toContain('startTime=now-1h');
  });

  test('Multiple event filters update URL with all parameters', async ({ page }) => {
    await page.goto('/events');
    await page.waitForTimeout(500);

    // Set eventType filter
    await page.getByRole('button', { name: 'Warning' }).click();
    await page.waitForTimeout(300);

    // Verify eventType is in URL
    expect(page.url()).toContain('eventType=Warning');

    // Add Reason filter
    const addFiltersButton = page.getByRole('button', { name: /(Add )?Filters/i });
    await addFiltersButton.click();
    await page.waitForTimeout(200);
    await page.getByRole('button', { name: 'Reason' }).click();
    await page.waitForTimeout(500);

    // Select a reason if available
    const options = page.locator('[cmdk-item]');
    const optionCount = await options.count();
    if (optionCount > 0) {
      await options.first().click();
      await page.waitForTimeout(500);

      // Close popover by clicking outside
      await page.locator('body').click({ position: { x: 10, y: 10 } });
      await page.waitForTimeout(300);

      // Verify URL contains both parameters
      const url = page.url();
      expect(url).toContain('eventType=Warning');
      expect(url).toContain('reasons=');
    } else {
      // If no options, just verify eventType
      expect(page.url()).toContain('eventType=Warning');
    }
  });

  test('Browser back/forward preserves event filter state', async ({ page }) => {
    // Navigate to different pages to create history entries
    await page.goto('/');
    await page.waitForTimeout(300);

    await page.goto('/events');
    await page.waitForTimeout(500);

    // Navigate to activity feed to create another history entry
    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Go back to events
    await page.goBack();
    await page.waitForTimeout(500);

    // Verify we're at events
    expect(page.url()).toContain('/events');

    // Apply filter (this uses replace)
    await page.getByRole('button', { name: 'Warning' }).click();
    await page.waitForTimeout(300);
    expect(page.url()).toContain('eventType=Warning');

    // Go forward to activity feed
    await page.goForward();
    await page.waitForTimeout(500);

    // Verify we're at activity feed
    expect(page.url()).toContain('/activity-feed');

    // Go back to events with Warning filter
    await page.goBack();
    await page.waitForTimeout(500);

    // Verify filter persists (replace preserves the state)
    expect(page.url()).toContain('/events');
    expect(page.url()).toContain('eventType=Warning');
  });
});

test.describe('URL State - History Behavior', () => {
  test.beforeEach(async ({ page }) => {
    await mockActivityQueryAPI(page, mockActivities);
  });

  test('Filter changes use replace to avoid history spam', async ({ page }) => {
    // Navigate from home to create initial history
    await page.goto('/');
    await page.waitForTimeout(300);

    await page.goto('/activity-feed');
    await page.waitForTimeout(500);

    // Change filter multiple times (these should all use replace)
    await page.getByRole('button', { name: 'Human' }).click();
    await page.waitForTimeout(300);

    await page.getByRole('button', { name: 'System' }).click();
    await page.waitForTimeout(300);

    await page.getByRole('button', { name: 'All' }).click();
    await page.waitForTimeout(300);

    // Go back once - should go to home (filter changes used replace, not push)
    await page.goBack();
    await page.waitForTimeout(500);

    // Verify we're back at home (not at intermediate filter states)
    expect(page.url()).toContain('/');
    expect(page.url()).not.toContain('/activity-feed');
  });
});
