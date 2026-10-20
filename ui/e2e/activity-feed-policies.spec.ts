import { test, expect } from '@playwright/test';
import {
  mockActivityQueryAPI,
  mockActivityFacetQueryAPI,
  mockPolicyListAPI,
} from './helpers/api-mocks';

/**
 * The ActivityFeed only probes ActivityPolicies when the host opts in with
 * `checkPolicies`. Tenant clients cannot list the Platform-scope resource, so
 * the default render must not issue the request.
 */

const isPolicyListRequest = (url: string) =>
  new URL(url).pathname.endsWith('/activitypolicies');

test.describe('Activity Feed policy probe', () => {
  test('makes no activitypolicies request by default', async ({ page }) => {
    await mockActivityQueryAPI(page, []);
    await mockActivityFacetQueryAPI(page);

    const policyRequests: string[] = [];
    page.on('request', (request) => {
      if (isPolicyListRequest(request.url())) {
        policyRequests.push(request.url());
      }
    });

    await page.goto('/activity-feed');

    // Tenants fall through to the normal empty state.
    await expect(page.getByText('No activities found')).toBeVisible();
    await expect(
      page.getByText('Get started with activity logging')
    ).toHaveCount(0);

    // Give the mount effects time to settle before asserting the absence.
    await page.waitForTimeout(500);
    expect(policyRequests).toEqual([]);
  });

  test('probes activitypolicies and shows the No Policies state when opted in', async ({
    page,
  }) => {
    await mockActivityQueryAPI(page, []);
    await mockActivityFacetQueryAPI(page);
    await mockPolicyListAPI(page, {
      apiVersion: 'activity.miloapis.com/v1alpha1',
      kind: 'ActivityPolicyList',
      items: [],
    });

    const policyRequests: string[] = [];
    page.on('request', (request) => {
      if (isPolicyListRequest(request.url())) {
        policyRequests.push(request.url());
      }
    });

    await page.goto('/activity-feed?checkPolicies=true');

    await expect(
      page.getByText('Get started with activity logging')
    ).toBeVisible();
    await expect(page.getByText('No activities found')).toHaveCount(0);
    // React StrictMode double-invokes the mount effect in dev, so assert the
    // probe happened rather than pinning an exact count.
    expect(policyRequests.length).toBeGreaterThan(0);
  });
});
