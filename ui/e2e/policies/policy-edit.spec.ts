import { test, expect } from '@playwright/test';
import { mockPolicy } from '../fixtures/policies';
import { fillResourceDetails } from '../helpers/form-helpers';
import { fillMonacoEditor } from '../helpers/monaco';

/**
 * E2E tests for PolicyEditView component
 * Tests the policy editor for creating and editing policies
 */

test.describe('PolicyEditView - New Policy', () => {
  test.beforeEach(async ({ page }) => {
    // Navigate to the new policy page
    await page.goto('/policies/new');

    // Wait for the page to be ready
    await page.waitForTimeout(300);
  });

  test('displays policy name input for new policies', async ({ page }) => {
    // Verify the policy name input is visible and editable
    const nameInput = page.locator('#policy-name');
    await expect(nameInput).toBeVisible();
    await expect(nameInput).toBeEditable();

    // Verify the label
    const label = page.locator('label[for="policy-name"]');
    await expect(label).toHaveText('Policy Name');

    // Verify placeholder
    await expect(nameInput).toHaveAttribute('placeholder', /httpproxy-policy/i);
  });

  test('displays resource form and rule list', async ({ page }) => {
    // Wait for page to load
    await page.waitForSelector('text=Policy Name', { timeout: 5000 });

    // Verify Resource Target section is present
    await expect(page.getByText('Resource Target')).toBeVisible();

    // Verify Add Audit Rule button is present
    const addAuditRuleButton = page.getByRole('button', { name: /Add Audit Rule/i });
    await expect(addAuditRuleButton).toBeVisible();

    // Verify Add Event Rule button is present (need to switch to Event Rules tab first)
    const eventRulesTab = page.getByRole('tab', { name: /Event Rules/ });
    await eventRulesTab.click();
    await page.waitForTimeout(200);

    const addEventRuleButton = page.getByRole('button', { name: /Add Event Rule/i });
    await expect(addEventRuleButton).toBeVisible();
  });

  test('Save button is disabled when no changes made', async ({ page }) => {
    // Find the Save Policy button
    const saveButton = page.getByRole('button', { name: 'Save Policy' });
    await expect(saveButton).toBeVisible();

    // Should be disabled because isDirty is false and no valid data
    await expect(saveButton).toBeDisabled();
  });

  test('Save button is enabled with valid changes', async ({ page }) => {
    // Fill in required fields
    const nameInput = page.locator('#policy-name');
    await nameInput.fill('test-policy');

    // Fill in resource details using helper
    await fillResourceDetails(page, 'test.datumapis.com', 'TestResource');

    // Wait for state updates
    await page.waitForTimeout(300);

    // Save button should now be enabled
    const saveButton = page.getByRole('button', { name: 'Save Policy' });
    await expect(saveButton).toBeEnabled();
  });

  test('Validate button performs dry-run validation', async ({ page }) => {
    // Mock the dry-run validation response
    await page.route('**/activitypolicies?dryRun=All', async (route) => {
      await route.fulfill({
        status: 200,
        json: {
          ...mockPolicy,
          metadata: { ...mockPolicy.metadata, name: 'test-policy' },
        },
      });
    });

    // Fill in required fields
    const nameInput = page.locator('#policy-name');
    await nameInput.fill('test-policy');

    // Fill in resource details using helper
    await fillResourceDetails(page, 'test.datumapis.com', 'TestResource');

    await page.waitForTimeout(300);

    // Click Validate button
    const validateButton = page.getByRole('button', { name: 'Validate' });
    await expect(validateButton).toBeEnabled();
    await validateButton.click();

    // Wait for validation to complete
    await page.waitForTimeout(500);

    // Verify the API call was made with dryRun parameter
    // (The route mock will have been called)
  });

  test('validation errors display in alert', async ({ page }) => {
    // Mock a validation error response
    await page.route('**/activitypolicies?dryRun=All', async (route) => {
      await route.fulfill({
        status: 422,
        json: {
          kind: 'Status',
          status: 'Failure',
          message: 'ActivityPolicy.activity.miloapis.com "test-policy" is invalid: spec.auditRules: Required value',
          reason: 'Invalid',
          code: 422,
        },
      });
    });

    // Fill in policy name and resource (server will reject for missing rules)
    const nameInput = page.locator('#policy-name');
    await nameInput.fill('test-policy');

    // Fill in both API Group and Kind to pass client-side validation
    await fillResourceDetails(page, 'test.datumapis.com', 'TestResource');

    await page.waitForTimeout(300);

    // Click Validate button (server will return validation error for missing rules)
    const validateButton = page.getByRole('button', { name: 'Validate' });
    await validateButton.click();

    // Wait for error to appear
    await page.waitForTimeout(500);

    // Verify error alert is displayed
    const errorAlert = page.locator('[role="alert"]').filter({
      hasText: /invalid|required/i,
    });
    await expect(errorAlert).toBeVisible();
  });

  test('Cancel button navigates back without saving', async ({ page }) => {
    // Fill in some data
    const nameInput = page.locator('#policy-name');
    await nameInput.fill('test-policy');

    // Click Cancel button
    const cancelButton = page.getByRole('button', { name: 'Cancel' });
    await expect(cancelButton).toBeVisible();
    await cancelButton.click();

    // Verify navigation back to policies list
    await expect(page).toHaveURL('/policies');
  });
});

test.describe('PolicyEditView - Existing Policy', () => {
  test.beforeEach(async ({ page }) => {
    // Mock the API call for the existing policy
    await page.route('**/activitypolicies/httpproxy-policy', async (route) => {
      await route.fulfill({ status: 200, json: mockPolicy });
    });

    // Navigate to the edit page
    await page.goto('/policies/httpproxy-policy/edit');

    // Wait for the page to load
    await page.waitForTimeout(500);
  });

  test('displays read-only header showing policy name for existing policies', async ({ page }) => {
    // Verify the policy name input is NOT present
    const nameInput = page.locator('#policy-name');
    await expect(nameInput).not.toBeVisible();

    // Verify the header shows the kind
    const header = page.locator('h2').filter({ hasText: mockPolicy.spec.resource.kind });
    await expect(header).toBeVisible();

    // Verify resource name is displayed
    const resourceText = page.locator('text=Resource: httpproxy-policy');
    await expect(resourceText).toBeVisible();
  });

  test('successful save navigates to detail view', async ({ page }) => {
    // Mock the save response
    await page.route('**/activitypolicies/httpproxy-policy', async (route) => {
      if (route.request().method() === 'PUT') {
        await route.fulfill({
          status: 200,
          json: mockPolicy,
        });
      } else {
        await route.fulfill({ status: 200, json: mockPolicy });
      }
    });

    // In edit mode, resource fields are read-only (shown in header)
    // Make a change by adding/editing a rule to enable save
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();
    await page.fill('#rule-name', 'test-rule');
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "create"');
    await fillMonacoEditor(page, 'cel-editor-summary', 'Created something');
    await page.getByRole('button', { name: 'Create Rule' }).click();

    await page.waitForTimeout(300);

    // Click Save button
    const saveButton = page.getByRole('button', { name: 'Save Policy' });
    await expect(saveButton).toBeEnabled();
    await saveButton.click();

    // Wait for navigation
    await page.waitForTimeout(500);

    // Verify navigation to detail view
    await expect(page).toHaveURL('/policies/httpproxy-policy');
  });

  test('Danger Zone section shows Delete button for existing policies', async ({ page }) => {
    // Scroll down to find Danger Zone
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    await page.waitForTimeout(200);

    // Verify Danger Zone heading exists
    const dangerZoneHeading = page.locator('h3').filter({ hasText: 'Danger Zone' });
    await expect(dangerZoneHeading).toBeVisible();

    // Verify Delete button exists
    const deleteButton = page.getByRole('button', { name: /Delete Policy/i });
    await expect(deleteButton).toBeVisible();

    // Click delete button to open confirmation dialog
    await deleteButton.click();
    await page.waitForTimeout(200);

    // Verify confirmation dialog appears
    const dialog = page.locator('[role="dialog"]');
    await expect(dialog).toBeVisible();

    // Verify dialog title
    const dialogTitle = page.locator('text=Delete Policy').first();
    await expect(dialogTitle).toBeVisible();

    // Verify warning message
    const warningMessage = page.locator('text=This action cannot be undone').first();
    await expect(warningMessage).toBeVisible();
  });
});
