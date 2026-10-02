import { test, expect } from '@playwright/test';
import {
  mockPolicyList,
  mockEmptyPolicyList,
  mockPolicyWithError,
} from '../fixtures/policies';
import {
  mockPolicyListAPI,
  mockPolicyCreateAPI,
} from '../helpers/api-mocks';

/**
 * E2E tests for PolicyList component
 * Tests the policy list view including grouping, status indicators, and navigation
 */

test.describe('PolicyList', () => {
  test.beforeEach(async ({ page }) => {
    // Mock other API endpoints to prevent real requests
    await mockPolicyCreateAPI(page);

    await page.route('**/policypreviews', async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          status: {
            results: [],
            activities: [],
          },
        },
      });
    });

    await page.route('**/auditlogqueries', async (route) => {
      await route.fulfill({
        status: 200,
        json: { status: { results: [] } },
      });
    });
  });

  test('displays policies grouped by API group', async ({ page }) => {
    // Mock the API to return policies from different API groups
    await mockPolicyListAPI(page, mockPolicyList);

    // Navigate to policies page
    await page.goto('/policies');

    // Wait for the list to load
    await page.waitForSelector('text=networking.datumapis.com', { timeout: 5000 });

    // Verify API groups are displayed
    await expect(page.getByText('networking.datumapis.com')).toBeVisible();
    await expect(page.getByText('apps')).toBeVisible();

    // Verify group badges show correct policy counts
    const networkingGroup = page.locator('button:has-text("networking.datumapis.com")');
    await expect(networkingGroup).toContainText('2'); // HTTPProxy and Gateway

    const appsGroup = page.locator('button:has-text("apps")');
    await expect(appsGroup).toContainText('1'); // Deployment
  });

  test('shows audit and event rule counts per policy', async ({ page }) => {
    await mockPolicyListAPI(page, mockPolicyList);

    await page.goto('/policies');

    // Wait for the policies to load
    await page.waitForSelector('text=networking.datumapis.com');

    // Expand the networking.datumapis.com group (should be expanded by default)
    // Find the HTTPProxy policy row and verify rule counts
    const httpProxyRow = page.locator('tr', { has: page.locator('text=HTTPProxy') });

    // HTTPProxy has 2 audit rules and 1 event rule (based on fixtures)
    await expect(httpProxyRow.locator('td').nth(1)).toContainText('2');
    await expect(httpProxyRow.locator('td').nth(2)).toContainText('1');

    // Gateway has 2 audit rules and 0 event rules
    const gatewayRow = page.locator('tr', { has: page.locator('text=Gateway') });
    await expect(gatewayRow.locator('td').nth(1)).toContainText('2');
    await expect(gatewayRow.locator('td').nth(2)).toContainText('0');
  });

  test('shows ready status indicator for healthy policies', async ({ page }) => {
    await mockPolicyListAPI(page, mockPolicyList);

    await page.goto('/policies');

    // Wait for the policies to load
    await page.waitForSelector('text=networking.datumapis.com');

    // Find the HTTPProxy policy row
    const httpProxyRow = page.locator('tr', { has: page.locator('text=HTTPProxy') });

    // Verify the success-toned ready indicator is present (dot in the row)
    const readyIndicator = httpProxyRow.locator('div[class*="success-500"].w-2.h-2.rounded-full');
    await expect(readyIndicator).toBeVisible();
  });

  test('shows error status indicator for failed policies', async ({ page }) => {
    // Add the error policy to the list
    const listWithError = {
      ...mockPolicyList,
      items: [...mockPolicyList.items, mockPolicyWithError],
    };

    await mockPolicyListAPI(page, listWithError);

    await page.goto('/policies');

    // Wait for the policies to load
    await page.waitForSelector('text=test.datumapis.com');

    // Expand the test.datumapis.com group (may already be expanded by default)
    const brokenRow = page.locator('tr', { has: page.locator('text=BrokenResource') });

    // If row not visible, expand the group
    const isRowVisible = await brokenRow.isVisible().catch(() => false);
    if (!isRowVisible) {
      const testGroup = page.locator('button:has-text("test.datumapis.com")');
      await testGroup.click();
      await page.waitForTimeout(300);
    }

    // Verify error indicator (AlertTriangle icon with the destructive text token)
    const errorIndicator = brokenRow.locator('svg.text-destructive');
    await expect(errorIndicator.first()).toBeVisible();
  });

  test('clicking policy row navigates to detail view', async ({ page }) => {
    await mockPolicyListAPI(page, mockPolicyList);

    await page.goto('/policies');

    // Wait for the policies to load
    await page.waitForSelector('text=networking.datumapis.com');

    // Click the HTTPProxy policy row
    const httpProxyRow = page.locator('tr', { has: page.locator('text=HTTPProxy') });
    await httpProxyRow.click();

    // Verify navigation to detail view
    await expect(page).toHaveURL(/\/policies\/httpproxy-policy/);
  });

  test('Create Policy button navigates to /policies/new', async ({ page }) => {
    await mockPolicyListAPI(page, mockPolicyList);

    await page.goto('/policies');

    // Wait for the page to load
    await page.waitForSelector('text=Create Policy');

    // Click the Create Policy button
    await page.getByRole('button', { name: 'Create Policy' }).click();

    // Verify navigation to create page
    await expect(page).toHaveURL(/\/policies\/new/);
  });

  test('displays empty state when no policies exist', async ({ page }) => {
    await mockPolicyListAPI(page, mockEmptyPolicyList);

    await page.goto('/policies');

    // Wait for the empty state to render
    await page.waitForSelector('text=No policies found');

    // Verify empty state message
    await expect(page.getByText('No policies found')).toBeVisible();
    await expect(
      page.getByText(/Activity policies define how audit events/)
    ).toBeVisible();

    // Verify empty state action button
    await expect(
      page.getByRole('button', { name: 'Create your first policy' })
    ).toBeVisible();
  });

  test('displays loading skeleton while fetching', async ({ page }) => {
    // Mock with delay to capture loading state
    await mockPolicyListAPI(page, mockPolicyList, { delay: 1000 });

    await page.goto('/policies');

    // Verify skeleton is visible immediately
    // Look for skeleton placeholders (Skeleton components render with specific classes)
    const skeleton = page.locator('.animate-pulse').first();
    await expect(skeleton).toBeVisible({ timeout: 1000 });

    // Wait for actual content to load
    await page.waitForSelector('text=networking.datumapis.com', { timeout: 3000 });

    // Verify skeleton is gone
    await expect(skeleton).not.toBeVisible();
  });

  test('displays error with retry option on API failure', async ({ page }) => {
    // Mock with error
    await mockPolicyListAPI(page, undefined, {
      error: {
        status: 500,
        message: 'Internal server error',
      },
    });

    await page.goto('/policies');

    // Wait for error to display (ApiErrorAlert uses role="alert")
    // The error message is formatted as a friendly message like "The activity service hit a bump..."
    await page.waitForSelector('[role="alert"]', { timeout: 5000 });

    // Verify error alert is visible (the friendly message contains "bump" or similar)
    const errorAlert = page.locator('[role="alert"]');
    await expect(errorAlert).toBeVisible();

    // Verify retry button is present (in ApiErrorAlert - it's a button with title="Retry")
    const retryButton = errorAlert.getByRole('button', { name: /Retry/i });
    await expect(retryButton).toBeVisible();

    // Mock successful response for retry
    await mockPolicyListAPI(page, mockPolicyList);

    // Click retry button
    await retryButton.click();

    // Wait a bit for the refresh
    await page.waitForTimeout(500);

    // Verify policies load after retry
    await expect(page.getByText('networking.datumapis.com')).toBeVisible();
  });

  test('refresh button reloads policy list', async ({ page }) => {
    await mockPolicyListAPI(page, mockPolicyList);

    await page.goto('/policies');

    // Wait for initial load
    await page.waitForSelector('text=networking.datumapis.com');

    // Find and click refresh button (the one with title="Refresh policy list")
    const refreshButton = page.locator('button[title="Refresh policy list"]');
    await expect(refreshButton).toBeVisible();

    // Mock a different response for the refresh
    const refreshedList = {
      ...mockEmptyPolicyList,
    };
    await mockPolicyListAPI(page, refreshedList);

    // Click refresh
    await refreshButton.click();

    // Wait for empty state (since we mocked empty list)
    await page.waitForTimeout(500);
    await expect(page.getByText('No policies found')).toBeVisible();
  });

  test('groups are expanded by default', async ({ page }) => {
    await mockPolicyListAPI(page, mockPolicyList);

    await page.goto('/policies');

    // Wait for the policies to load
    await page.waitForSelector('text=networking.datumapis.com');

    // Verify that policy tables are visible (groups are expanded)
    await expect(page.getByText('HTTPProxy')).toBeVisible();
    await expect(page.getByText('Gateway')).toBeVisible();
    await expect(page.getByText('Deployment')).toBeVisible();
  });

  test('clicking group header toggles expansion', async ({ page }) => {
    await mockPolicyListAPI(page, mockPolicyList);

    await page.goto('/policies');

    // Wait for the policies to load
    await page.waitForSelector('text=networking.datumapis.com');

    // Verify group is expanded (policies visible)
    await expect(page.getByText('HTTPProxy')).toBeVisible();

    // Click the group header to collapse
    const networkingGroup = page.locator('button:has-text("networking.datumapis.com")');
    await networkingGroup.click();

    // Wait for collapse animation
    await page.waitForTimeout(300);

    // Verify policies are hidden
    await expect(page.getByText('HTTPProxy')).not.toBeVisible();

    // Click again to expand
    await networkingGroup.click();

    // Wait for expand animation
    await page.waitForTimeout(300);

    // Verify policies are visible again
    await expect(page.getByText('HTTPProxy')).toBeVisible();
  });
});
