import { test, expect } from '@playwright/test';
import { fillMonacoEditor, getMonacoEditorValue } from '../helpers/monaco';

/**
 * E2E tests for PolicyRuleList and PolicyRuleEditorDialog components
 * Tests creating, editing, and deleting rules in the policy editor
 */

test.describe('PolicyRuleList', () => {
  test.beforeEach(async ({ page }) => {
    // Mock API to prevent real requests
    await page.route('**/activitypolicies', async (route) => {
      await route.fulfill({
        status: 200,
        json: { items: [] },
      });
    });

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

    // Navigate to policy create page
    await page.goto('/policies/new');
  });

  test('displays Audit Rules and Event Rules tabs', async ({ page }) => {
    // Wait for page to load
    await page.waitForSelector('text=Audit Rules');
    await page.waitForSelector('text=Event Rules');

    // Verify both tabs exist (these are Radix Tabs within the rule list)
    const auditTab = page.getByRole('tab', { name: /Audit Rules/ });
    const eventTab = page.getByRole('tab', { name: /Event Rules/ });

    await expect(auditTab).toBeVisible();
    await expect(eventTab).toBeVisible();
  });

  test('tabs show rule count badges', async ({ page }) => {
    // Wait for tabs to load
    await page.waitForSelector('text=Audit Rules');

    // Initially should show 0 for both tabs
    const auditTab = page.getByRole('tab', { name: /Audit Rules/ });
    const eventTab = page.getByRole('tab', { name: /Event Rules/ });

    await expect(auditTab).toContainText('0');
    await expect(eventTab).toContainText('0');
  });

  test('empty state shows "No rules defined" message', async ({ page }) => {
    // Wait for the page to load
    await page.waitForSelector('text=Audit Rules');

    // Audit Rules tab should be active by default
    await expect(page.getByText(/No audit rules defined/)).toBeVisible();

    // Switch to Event Rules tab
    await page.getByRole('tab', { name: /Event Rules/ }).click();
    await page.waitForTimeout(200);
    await expect(page.getByText(/No event rules defined/)).toBeVisible();
  });

  test('Add Audit Rule button opens rule editor dialog', async ({ page }) => {
    // Wait for the page to load
    await page.waitForSelector('text=Add Audit Rule');

    // Click Add Audit Rule button
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Verify dialog opens
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText('Create Audit Rule')).toBeVisible();
  });

  test('Add Event Rule button opens rule editor dialog', async ({ page }) => {
    // Wait for the page to load
    await page.waitForSelector('text=Event Rules');

    // Switch to Event Rules tab
    await page.getByRole('tab', { name: /Event Rules/ }).click();
    await page.waitForTimeout(200);

    // Click Add Event Rule button
    await page.getByRole('button', { name: 'Add Event Rule' }).click();

    // Verify dialog opens
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText('Create Event Rule')).toBeVisible();
  });

  test('clicking existing rule opens edit dialog', async ({ page }) => {
    // Wait for the page to load
    await page.waitForSelector('text=Add Audit Rule');

    // Create a rule first
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Fill in the form (name is a regular input, match/summary are Monaco editors)
    await page.fill('#rule-name', 'test-rule');
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "create"');
    await fillMonacoEditor(page, 'cel-editor-summary', '{{ actor.name }} created a resource');

    // Save the rule
    await page.getByRole('button', { name: 'Create Rule' }).click();

    // Wait for dialog to close
    await expect(page.getByRole('dialog')).not.toBeVisible();

    // Now click the Edit button for the rule (find the button with aria-label="Edit rule")
    const editButton = page.getByRole('button', { name: 'Edit rule' }).first();
    await editButton.click();

    // Verify edit dialog opens with populated data
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText('Edit Audit Rule')).toBeVisible();
    await expect(page.locator('#rule-name')).toHaveValue('test-rule');

    // Check Monaco editor values
    const matchValue = await getMonacoEditorValue(page, 'cel-editor-match');
    expect(matchValue).toBe('audit.verb == "create"');
  });

  test('delete button removes rule', async ({ page }) => {
    // Wait for the page to load
    await page.waitForSelector('text=Add Audit Rule');

    // Create a rule first
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    await page.fill('#rule-name', 'delete-me');
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "delete"');
    await fillMonacoEditor(page, 'cel-editor-summary', 'Deleted a resource');

    await page.getByRole('button', { name: 'Create Rule' }).click();

    // Wait for dialog to close
    await expect(page.getByRole('dialog')).not.toBeVisible();

    // Verify rule is visible
    await expect(page.getByText('delete-me')).toBeVisible();

    // Click delete button (trash icon) - find button with aria-label="Delete rule"
    const deleteButton = page.getByRole('button', { name: 'Delete rule' });
    await deleteButton.click();

    // Verify rule is removed
    await expect(page.getByText('delete-me')).not.toBeVisible();
    await expect(page.getByText(/No audit rules defined/)).toBeVisible();
  });
});

test.describe('PolicyRuleEditorDialog', () => {
  test.beforeEach(async ({ page }) => {
    // Mock API
    await page.route('**/activitypolicies', async (route) => {
      await route.fulfill({
        status: 200,
        json: { items: [] },
      });
    });

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

    // Navigate to policy create page
    await page.goto('/policies/new');
  });

  test('dialog opens with empty form for new rule', async ({ page }) => {
    // Click Add Audit Rule
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Verify regular input fields are empty
    await expect(page.locator('#rule-name')).toHaveValue('');
    await expect(page.locator('#rule-description')).toHaveValue('');

    // Verify Monaco editors are empty
    const matchValue = await getMonacoEditorValue(page, 'cel-editor-match');
    const summaryValue = await getMonacoEditorValue(page, 'cel-editor-summary');
    expect(matchValue).toBe('');
    expect(summaryValue).toBe('');
  });

  test('dialog opens with populated form for existing rule', async ({ page }) => {
    // Create a rule first
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    await page.fill('#rule-name', 'existing-rule');
    await page.fill('#rule-description', 'A test rule');
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "update"');
    await fillMonacoEditor(page, 'cel-editor-summary', 'Updated something');

    await page.getByRole('button', { name: 'Create Rule' }).click();

    // Wait for dialog to close
    await expect(page.getByRole('dialog')).not.toBeVisible();

    // Click the Edit button for the rule
    const editButton = page.getByRole('button', { name: 'Edit rule' }).first();
    await editButton.click();

    // Wait for dialog to open
    await expect(page.getByRole('dialog')).toBeVisible();

    // Verify regular input fields are populated
    await expect(page.locator('#rule-name')).toHaveValue('existing-rule');
    await expect(page.locator('#rule-description')).toHaveValue('A test rule');

    // Verify Monaco editor values are populated
    const matchValue = await getMonacoEditorValue(page, 'cel-editor-match');
    const summaryValue = await getMonacoEditorValue(page, 'cel-editor-summary');
    expect(matchValue).toBe('audit.verb == "update"');
    expect(summaryValue).toBe('Updated something');
  });

  test('name field is required', async ({ page }) => {
    // Open dialog
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Fill only match and summary (skip name)
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "create"');
    await fillMonacoEditor(page, 'cel-editor-summary', 'Created something');

    // Try to save
    await page.getByRole('button', { name: 'Create Rule' }).click();

    // Verify error message appears
    await expect(page.getByText('Name is required')).toBeVisible();

    // Dialog should still be open
    await expect(page.getByRole('dialog')).toBeVisible();
  });

  test('match expression field is required', async ({ page }) => {
    // Open dialog
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Fill only name and summary (skip match)
    await page.fill('#rule-name', 'test-rule');
    await fillMonacoEditor(page, 'cel-editor-summary', 'Created something');

    // Try to save
    await page.getByRole('button', { name: 'Create Rule' }).click();

    // Verify error message appears
    await expect(page.getByText('Match expression is required')).toBeVisible();

    // Dialog should still be open
    await expect(page.getByRole('dialog')).toBeVisible();
  });

  test('summary template field is required', async ({ page }) => {
    // Open dialog
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Fill only name and match (skip summary)
    await page.fill('#rule-name', 'test-rule');
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "create"');

    // Try to save
    await page.getByRole('button', { name: 'Create Rule' }).click();

    // Verify error message appears
    await expect(page.getByText('Summary template is required')).toBeVisible();

    // Dialog should still be open
    await expect(page.getByRole('dialog')).toBeVisible();
  });

  test('CEL Help section expands to show available variables', async ({ page }) => {
    // Open dialog
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Wait for dialog to be fully loaded
    await page.waitForSelector('[data-testid="cel-editor-match"]');

    // Find the details element for CEL help (it's near the match editor)
    const celHelp = page.getByText('Available variables').locator('..');

    // Verify it's collapsed initially (content should not be visible)
    await expect(celHelp.locator('ul').first()).not.toBeVisible();

    // Click to expand
    await celHelp.getByText('Available variables').click();

    // Verify variables are visible (use .first() to handle multiple matches)
    await expect(celHelp.getByText(/^audit\.verb -/).first()).toBeVisible();
    await expect(celHelp.getByText(/user\.username/).first()).toBeVisible();
  });

  test('Template Help section expands to show available variables', async ({ page }) => {
    // Open dialog
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Find the summary section for template variables (use getByText for better matching)
    const templateHelp = page.getByText('Template variables').locator('..');

    // Verify it's collapsed initially (content should not be visible)
    await expect(templateHelp.locator('ul').first()).not.toBeVisible();

    // Click to expand
    await templateHelp.getByText('Template variables').click();

    // Wait for expansion
    await page.waitForTimeout(200);

    // Verify variables are visible (use .first() to handle multiple matches)
    await expect(templateHelp.getByText(/\{\{ actor \}\}/).first()).toBeVisible();
    await expect(templateHelp.getByText(/objectRef\.name/).first()).toBeVisible();
  });

  test('Save button creates new rule and closes dialog', async ({ page }) => {
    // Open dialog
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Fill in all required fields
    await page.fill('#rule-name', 'new-rule');
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "create"');
    await fillMonacoEditor(page, 'cel-editor-summary', '{{ actor.name }} created something');

    // Click Save
    await page.getByRole('button', { name: 'Create Rule' }).click();

    // Wait for dialog to close
    await expect(page.getByRole('dialog')).not.toBeVisible();

    // Verify rule appears in the list
    await expect(page.getByText('new-rule')).toBeVisible();

    // Verify count badge updated
    const auditTab = page.getByRole('tab', { name: /Audit Rules/ });
    await expect(auditTab).toContainText('1');
  });

  test('Cancel button closes dialog without saving', async ({ page }) => {
    // Open dialog
    await page.getByRole('button', { name: 'Add Audit Rule' }).click();

    // Fill in fields
    await page.fill('#rule-name', 'cancel-me');
    await fillMonacoEditor(page, 'cel-editor-match', 'audit.verb == "create"');
    await fillMonacoEditor(page, 'cel-editor-summary', 'This should not be saved');

    // Click Cancel
    await page.getByRole('button', { name: 'Cancel' }).click();

    // Wait for dialog to close
    await expect(page.getByRole('dialog')).not.toBeVisible();

    // Verify rule was NOT created
    await expect(page.getByText('cancel-me')).not.toBeVisible();
    await expect(page.getByText(/No audit rules defined/)).toBeVisible();
  });
});
