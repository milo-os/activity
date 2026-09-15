import { test, expect } from '@playwright/test';
import { mockPolicy, mockPolicyWithError } from '../fixtures/policies';

/**
 * E2E tests for PolicyDetailView component
 * Tests the policy detail page including header, status, actions, and activity view
 */

test.describe('PolicyDetailView', () => {
  test.beforeEach(async ({ page }) => {
    // Mock the API call for the policy
    await page.route('**/activitypolicies/httpproxy-policy', async (route) => {
      await route.fulfill({ status: 200, json: mockPolicy });
    });

    // Navigate to the policy detail page
    await page.goto('/policies/httpproxy-policy');

    // Wait for the page to be ready
    await page.waitForTimeout(300);
  });

  test('displays policy header with kind and API group', async ({ page }) => {
    // Verify the policy kind is shown in the header
    const header = page.locator('h2').filter({ hasText: mockPolicy.spec.resource.kind });
    await expect(header).toBeVisible();

    // Verify API group is displayed
    const apiGroupText = page.locator('text=API Group: networking.datumapis.com');
    await expect(apiGroupText).toBeVisible();

    // Verify resource name is displayed
    const resourceText = page.locator('text=Resource: httpproxy-policy');
    await expect(resourceText).toBeVisible();
  });

  test('displays copy resource name button that copies to clipboard', async ({ page, context }) => {
    // Grant clipboard permissions
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    // Find the copy button (it's next to the resource name)
    const copyButton = page.getByRole('button', { name: 'Copy resource name' });
    await expect(copyButton).toBeVisible();

    // Click to copy
    await copyButton.click();

    // Wait for the checkmark icon to appear (indicates success)
    const checkIcon = page.locator('svg').filter({ has: page.locator('path') }).nth(0);
    await page.waitForTimeout(100);

    // Verify clipboard contains the policy name
    const clipboardText = await page.evaluate(() => navigator.clipboard.readText());
    expect(clipboardText).toBe('httpproxy-policy');
  });

  test('displays health status indicator (green dot for Ready)', async ({ page }) => {
    // Look for the success-toned status dot
    const statusDot = page.locator('span[class*="success-500"].w-2.h-2.rounded-full').first();
    await expect(statusDot).toBeVisible();

    // Hover over the dot to see the tooltip
    await statusDot.hover();
    await page.waitForTimeout(600); // Wait for tooltip delay

    // Verify tooltip shows success message (use first() to avoid strict mode violation)
    const tooltip = page.locator('text=All rules compiled successfully').first();
    await expect(tooltip).toBeVisible();
  });

  test('displays error banner for policies with error status', async ({ page }) => {
    // Override the route to return an error policy
    await page.route('**/activitypolicies/broken-policy', async (route) => {
      await route.fulfill({ status: 200, json: mockPolicyWithError });
    });

    // Navigate to the broken policy
    await page.goto('/policies/broken-policy');
    await page.waitForTimeout(300);

    // Verify the error banner is displayed
    const errorBanner = page.locator('[role="alert"]').filter({
      hasText: 'Policy Error:'
    });
    await expect(errorBanner).toBeVisible();

    // Verify error message is shown
    const errorMessage = page.locator('text=failed to compile');
    await expect(errorMessage).toBeVisible();
  });

  test('Edit Policy button navigates to edit view', async ({ page }) => {
    // Find the Edit Policy button
    const editButton = page.getByRole('button', { name: /Edit Policy/i });
    await expect(editButton).toBeVisible();

    // Click the button
    await editButton.click();

    // Verify navigation to edit route
    await expect(page).toHaveURL('/policies/httpproxy-policy/edit');
  });

  test('displays ActivityView with Activity/Events tabs', async ({ page }) => {
    // Mock the activity query response
    await page.route('**/activities/activity-query-*', async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          apiVersion: 'activity.miloapis.com/v1alpha1',
          kind: 'Activity',
          metadata: { name: 'activity-query-test' },
          status: { results: [], continue: null },
        },
      });
    });

    // Mock the events query response
    await page.route('**/events/event-query-*', async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          apiVersion: 'activity.miloapis.com/v1alpha1',
          kind: 'EventQuery',
          metadata: { name: 'event-query-test' },
          status: { results: [], continue: null },
        },
      });
    });

    // Look for the Activity tab
    const activityTab = page.getByRole('button', { name: 'Activity' });
    await expect(activityTab).toBeVisible();

    // Look for the Events tab
    const eventsTab = page.getByRole('button', { name: 'Events' });
    await expect(eventsTab).toBeVisible();

    // Click on Events tab
    await eventsTab.click();
    await page.waitForTimeout(200);

    // Verify Events tab is now active (has active styling)
    await expect(eventsTab).toHaveClass(/border-\[#BF9595\]/);
  });

  test('displays 404 error page for non-existent policy', async ({ page }) => {
    // Mock a 404 response
    await page.route('**/activitypolicies/non-existent-policy', async (route) => {
      await route.fulfill({
        status: 404,
        json: {
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          message: 'activitypolicies.activity.miloapis.com "non-existent-policy" not found',
          reason: 'NotFound',
          code: 404,
        },
      });
    });

    // Navigate to non-existent policy
    await page.goto('/policies/non-existent-policy');
    await page.waitForTimeout(500);

    // Verify error is displayed (ApiErrorAlert component uses role="alert")
    const errorAlert = page.locator('[role="alert"]');
    await expect(errorAlert.first()).toBeVisible();
  });
});
