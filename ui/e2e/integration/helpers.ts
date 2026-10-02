import type { Page } from '@playwright/test';

/**
 * Generate a unique policy name for testing
 * Uses timestamp to ensure uniqueness across test runs
 */
export function generateUniquePolicyName(prefix: string = 'test-policy'): string {
  const timestamp = Date.now();
  return `${prefix}-${timestamp}`;
}

/**
 * Delete a policy via the UI
 * Navigates to the edit page and uses the danger zone delete button
 *
 * @param page - Playwright page object
 * @param name - Policy name to delete
 * @returns Promise that resolves when policy is deleted
 */
export async function cleanupPolicy(page: Page, name: string): Promise<void> {
  try {
    // Navigate to edit page
    await page.goto(`/policies/${encodeURIComponent(name)}/edit`, {
      timeout: 10000,
      waitUntil: 'networkidle',
    });

    // Wait for page to load
    await page.waitForSelector('text=Danger Zone', { timeout: 5000 });

    // Click delete button in danger zone
    const deleteButton = page.getByRole('button', { name: /Delete Policy/i });
    await deleteButton.click();

    // Confirm deletion in dialog
    // Look for confirmation button (could be "Delete" or "Confirm")
    const confirmButton = page.getByRole('button', { name: /^Delete$/i }).last();
    await confirmButton.click();

    // Wait for navigation away from the page
    await page.waitForURL(/\/policies(?:\/|$)/, { timeout: 10000 });
  } catch (error) {
    console.warn(`Failed to cleanup policy ${name}:`, error);
    // Don't throw - cleanup is best-effort
  }
}

/**
 * Wait for a policy to reach Ready status
 * Polls the policy detail view until status shows Ready
 *
 * @param page - Playwright page object
 * @param name - Policy name to check
 * @param timeoutMs - Maximum time to wait in milliseconds
 * @returns Promise that resolves when policy is ready, rejects on timeout
 */
export async function waitForPolicyReady(
  page: Page,
  name: string,
  timeoutMs: number = 30000
): Promise<void> {
  const startTime = Date.now();

  while (Date.now() - startTime < timeoutMs) {
    try {
      // Navigate to policy detail view
      await page.goto(`/policies/${encodeURIComponent(name)}`, {
        timeout: 10000,
        waitUntil: 'networkidle',
      });

      // Look for the ready status indicator (green checkmark or "Ready" badge)
      const readyIndicator = page.locator('[class*="success-500"], [data-status="ready"]').first();

      if (await readyIndicator.isVisible({ timeout: 2000 })) {
        return; // Policy is ready
      }

      // Wait a bit before retrying
      await page.waitForTimeout(1000);
    } catch (error) {
      // Continue polling
    }
  }

  throw new Error(`Policy ${name} did not reach Ready status within ${timeoutMs}ms`);
}

/**
 * Setup API connection for integration tests
 * Navigates to the home page and configures the API URL
 *
 * @param page - Playwright page object
 * @param apiUrl - API server URL (defaults to local dev cluster)
 * @param token - Optional bearer token
 */
export async function setupApiConnection(
  page: Page,
  apiUrl: string = 'http://localhost:6443',
  token?: string
): Promise<void> {
  // Navigate to home page
  await page.goto('/', { waitUntil: 'networkidle' });

  // Fill in API URL
  const apiUrlInput = page.locator('input#api-url');
  await apiUrlInput.fill(apiUrl);

  // Fill in token if provided
  if (token) {
    const tokenInput = page.locator('input#token');
    await tokenInput.fill(token);
  }

  // Click connect button
  const connectButton = page.getByRole('button', { name: /Connect to API/i });
  await connectButton.click();

  // Wait for navigation to activity feed
  await page.waitForURL(/\/activity-feed/, { timeout: 10000 });
}

/**
 * Navigate directly to the policies page
 * Assumes API connection is already configured in sessionStorage
 *
 * @param page - Playwright page object
 */
export async function navigateToPolicies(page: Page): Promise<void> {
  await page.goto('/policies', { waitUntil: 'networkidle' });
}

/**
 * Check if a policy exists in the list
 *
 * @param page - Playwright page object
 * @param policyName - Policy name to check
 * @returns Promise that resolves to true if policy exists, false otherwise
 */
export async function policyExists(page: Page, policyName: string): Promise<boolean> {
  await navigateToPolicies(page);

  try {
    // Wait for the policy list to load
    await page.waitForSelector('[data-testid="policy-list"]', { timeout: 5000 });

    // Look for the policy name in the list
    const policyRow = page.locator(`tr:has-text("${policyName}")`);
    return await policyRow.isVisible({ timeout: 2000 });
  } catch {
    return false;
  }
}
