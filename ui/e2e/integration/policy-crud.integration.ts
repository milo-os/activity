import { test, expect } from '@playwright/test';
import {
  generateUniquePolicyName,
  cleanupPolicy,
  setupApiConnection,
  navigateToPolicies,
} from './helpers';

/**
 * Integration tests for ActivityPolicy CRUD operations
 *
 * These tests run against a REAL API backend and are NOT mocked.
 *
 * Prerequisites:
 * - Dev environment must be running (`task dev:setup`)
 * - Example app must be running at http://localhost:3000
 * - API server must be accessible at http://localhost:6443
 */

test.describe('Policy CRUD Integration Tests', () => {
  // Increase timeout for all tests since real API is slower
  test.setTimeout(60000);

  // Track created policies for cleanup
  const createdPolicies: string[] = [];

  test.beforeEach(async ({ page }) => {
    // Setup API connection before each test
    await setupApiConnection(page);
  });

  test.afterEach(async ({ page }) => {
    // Cleanup any policies created during tests
    for (const policyName of createdPolicies) {
      await cleanupPolicy(page, policyName);
    }
    createdPolicies.length = 0; // Clear the array
  });

  test('can create a new policy with audit rules', async ({ page }) => {
    const policyName = generateUniquePolicyName('create-test');
    createdPolicies.push(policyName);

    // Navigate to create policy page
    await navigateToPolicies(page);
    await page.getByRole('button', { name: /Create Policy/i }).click();
    await expect(page).toHaveURL(/\/policies\/new/);

    // Fill in policy name
    const nameInput = page.locator('input[name="name"], input#policy-name');
    await nameInput.fill(policyName);

    // Select API group
    // Find the API group dropdown/combobox
    const apiGroupButton = page.locator('button:has-text("Select API group")').first();
    await apiGroupButton.click();

    // Wait for dropdown to open and select an option
    // Use a simple built-in API group like "apps" or "core"
    await page.waitForTimeout(500); // Wait for dropdown animation
    const appsOption = page.locator('[role="option"]:has-text("apps")').first();
    await appsOption.click();

    // Select resource kind
    const kindButton = page.locator('button:has-text("Select resource kind")').first();
    await kindButton.click();
    await page.waitForTimeout(500);
    const deploymentOption = page.locator('[role="option"]:has-text("Deployment")').first();
    await deploymentOption.click();

    // Add an audit rule
    const addRuleButton = page.getByRole('button', { name: /Add Audit Rule/i });
    await addRuleButton.click();

    // Fill in match expression
    const matchInput = page.locator('input[name*="match"], textarea[name*="match"]').first();
    await matchInput.fill('audit.verb == "create"');

    // Fill in summary template
    const summaryInput = page.locator('input[name*="summary"], textarea[name*="summary"]').first();
    await summaryInput.fill('{{ actor }} created deployment {{ audit.objectRef.name }}');

    // Save the policy
    const saveButton = page.getByRole('button', { name: /Save Policy|Create Policy/i });
    await saveButton.click();

    // Wait for navigation to detail view
    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}`), {
      timeout: 15000,
    });

    // Verify policy appears in detail view
    await expect(page.locator('h1, h2').filter({ hasText: policyName })).toBeVisible();

    // Verify status indicator shows (may be Ready or Pending initially)
    const statusIndicator = page.locator('[class*="success-500"], [class*="badge-warning"], [data-status]').first();
    await expect(statusIndicator).toBeVisible({ timeout: 10000 });
  });

  test('can edit an existing policy and add rules', async ({ page }) => {
    const policyName = generateUniquePolicyName('edit-test');
    createdPolicies.push(policyName);

    // First, create a policy
    await navigateToPolicies(page);
    await page.getByRole('button', { name: /Create Policy/i }).click();

    const nameInput = page.locator('input[name="name"], input#policy-name');
    await nameInput.fill(policyName);

    // Select API group and kind
    const apiGroupButton = page.locator('button:has-text("Select API group")').first();
    await apiGroupButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("apps")').first().click();

    const kindButton = page.locator('button:has-text("Select resource kind")').first();
    await kindButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("Deployment")').first().click();

    // Add one audit rule
    const addRuleButton = page.getByRole('button', { name: /Add Audit Rule/i });
    await addRuleButton.click();

    const matchInput = page.locator('input[name*="match"], textarea[name*="match"]').first();
    await matchInput.fill('audit.verb == "create"');

    const summaryInput = page.locator('input[name*="summary"], textarea[name*="summary"]').first();
    await summaryInput.fill('Created deployment');

    // Save
    const saveButton = page.getByRole('button', { name: /Save Policy|Create Policy/i });
    await saveButton.click();

    // Wait for detail view
    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}`), {
      timeout: 15000,
    });

    // Now edit the policy
    const editButton = page.getByRole('button', { name: /Edit Policy/i });
    await editButton.click();

    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}/edit`));

    // Add another audit rule
    const addRuleButton2 = page.getByRole('button', { name: /Add Audit Rule/i });
    await addRuleButton2.click();

    // Fill in the second rule (find the last match/summary inputs)
    const matchInputs = page.locator('input[name*="match"], textarea[name*="match"]');
    const summaryInputs = page.locator('input[name*="summary"], textarea[name*="summary"]');

    const matchCount = await matchInputs.count();
    const summaryCount = await summaryInputs.count();

    await matchInputs.nth(matchCount - 1).fill('audit.verb == "delete"');
    await summaryInputs.nth(summaryCount - 1).fill('Deleted deployment');

    // Save changes
    const saveButton2 = page.getByRole('button', { name: /Save Policy/i });
    await saveButton2.click();

    // Wait for navigation back to detail view
    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}(?!/edit)`), {
      timeout: 15000,
    });

    // Verify we're on the detail page
    await expect(page.locator('h1, h2').filter({ hasText: policyName })).toBeVisible();
  });

  test('policy preview evaluates rules against real audit logs', async ({ page }) => {
    const policyName = generateUniquePolicyName('preview-test');
    createdPolicies.push(policyName);

    // Create a policy with a simple rule
    await navigateToPolicies(page);
    await page.getByRole('button', { name: /Create Policy/i }).click();

    const nameInput = page.locator('input[name="name"], input#policy-name');
    await nameInput.fill(policyName);

    // Select API group and kind
    const apiGroupButton = page.locator('button:has-text("Select API group")').first();
    await apiGroupButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("apps")').first().click();

    const kindButton = page.locator('button:has-text("Select resource kind")').first();
    await kindButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("Deployment")').first().click();

    // Add a simple audit rule that will match
    const addRuleButton = page.getByRole('button', { name: /Add Audit Rule/i });
    await addRuleButton.click();

    const matchInput = page.locator('input[name*="match"], textarea[name*="match"]').first();
    await matchInput.fill('true'); // Match everything

    const summaryInput = page.locator('input[name*="summary"], textarea[name*="summary"]').first();
    await summaryInput.fill('Test activity');

    // Save the policy
    const saveButton = page.getByRole('button', { name: /Save Policy|Create Policy/i });
    await saveButton.click();

    // Wait for detail view
    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}`), {
      timeout: 15000,
    });

    // Navigate to Preview tab
    const previewTab = page.getByRole('tab', { name: /Preview/i });
    await previewTab.click();

    // Wait for preview to load
    await page.waitForTimeout(2000);

    // Verify that either:
    // 1. Sample audit logs are shown OR
    // 2. A message indicating no audit logs are available
    const hasAuditLogs = await page.locator('text=/audit|sample|preview/i').isVisible({ timeout: 5000 });
    const hasEmptyState = await page.locator('text=/no audit logs|no data|no results/i').isVisible({ timeout: 5000 });

    expect(hasAuditLogs || hasEmptyState).toBeTruthy();

    // If audit logs exist, verify preview results section exists
    if (hasAuditLogs) {
      const resultsSection = page.locator('[role="region"], .preview-results, [data-testid*="preview"]').first();
      await expect(resultsSection).toBeVisible({ timeout: 5000 });
    }
  });

  test('can delete a policy', async ({ page }) => {
    const policyName = generateUniquePolicyName('delete-test');

    // Create a policy to delete
    await navigateToPolicies(page);
    await page.getByRole('button', { name: /Create Policy/i }).click();

    const nameInput = page.locator('input[name="name"], input#policy-name');
    await nameInput.fill(policyName);

    // Select API group and kind
    const apiGroupButton = page.locator('button:has-text("Select API group")').first();
    await apiGroupButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("apps")').first().click();

    const kindButton = page.locator('button:has-text("Select resource kind")').first();
    await kindButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("Deployment")').first().click();

    // Add a simple rule
    const addRuleButton = page.getByRole('button', { name: /Add Audit Rule/i });
    await addRuleButton.click();

    const matchInput = page.locator('input[name*="match"], textarea[name*="match"]').first();
    await matchInput.fill('true');

    const summaryInput = page.locator('input[name*="summary"], textarea[name*="summary"]').first();
    await summaryInput.fill('Test');

    // Save
    const saveButton = page.getByRole('button', { name: /Save Policy|Create Policy/i });
    await saveButton.click();

    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}`), {
      timeout: 15000,
    });

    // Navigate to edit view
    const editButton = page.getByRole('button', { name: /Edit Policy/i });
    await editButton.click();

    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}/edit`));

    // Wait for danger zone section
    await page.waitForSelector('text=Danger Zone', { timeout: 5000 });

    // Click delete button
    const deleteButton = page.getByRole('button', { name: /Delete Policy/i });
    await deleteButton.click();

    // Confirm deletion in dialog
    const confirmButton = page.getByRole('button', { name: /^Delete$/i }).last();
    await confirmButton.click();

    // Wait for navigation away
    await page.waitForURL(/\/policies(?:\/|$)/, { timeout: 15000 });

    // Verify policy is no longer in the list
    await navigateToPolicies(page);

    // Wait for list to load
    await page.waitForTimeout(2000);

    // Policy should not be visible
    const policyRow = page.locator(`tr:has-text("${policyName}")`);
    await expect(policyRow).not.toBeVisible({ timeout: 5000 });
  });

  test('policy list shows created policies', async ({ page }) => {
    const policyName = generateUniquePolicyName('list-test');
    createdPolicies.push(policyName);

    // Create a policy
    await navigateToPolicies(page);
    await page.getByRole('button', { name: /Create Policy/i }).click();

    const nameInput = page.locator('input[name="name"], input#policy-name');
    await nameInput.fill(policyName);

    // Select API group and kind
    const apiGroupButton = page.locator('button:has-text("Select API group")').first();
    await apiGroupButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("apps")').first().click();

    const kindButton = page.locator('button:has-text("Select resource kind")').first();
    await kindButton.click();
    await page.waitForTimeout(500);
    await page.locator('[role="option"]:has-text("Deployment")').first().click();

    // Add rule
    const addRuleButton = page.getByRole('button', { name: /Add Audit Rule/i });
    await addRuleButton.click();

    const matchInput = page.locator('input[name*="match"], textarea[name*="match"]').first();
    await matchInput.fill('true');

    const summaryInput = page.locator('input[name*="summary"], textarea[name*="summary"]').first();
    await summaryInput.fill('Test');

    // Save
    const saveButton = page.getByRole('button', { name: /Save Policy|Create Policy/i });
    await saveButton.click();

    await page.waitForURL(new RegExp(`/policies/${encodeURIComponent(policyName)}`), {
      timeout: 15000,
    });

    // Navigate back to list
    await navigateToPolicies(page);

    // Wait for policy list to load
    await page.waitForTimeout(2000);

    // Verify policy appears in the list
    // The policy will be grouped by API group (apps)
    const appsGroup = page.locator('button:has-text("apps")');
    await expect(appsGroup).toBeVisible({ timeout: 5000 });

    // Expand the apps group if it's collapsed
    const isExpanded = await appsGroup.getAttribute('aria-expanded');
    if (isExpanded === 'false') {
      await appsGroup.click();
      await page.waitForTimeout(500);
    }

    // Find the policy in the list
    const policyRow = page.locator(`tr:has-text("${policyName}")`);
    await expect(policyRow).toBeVisible({ timeout: 5000 });

    // Verify the row shows it's for Deployment resource
    await expect(policyRow.locator('text=Deployment')).toBeVisible();
  });
});
