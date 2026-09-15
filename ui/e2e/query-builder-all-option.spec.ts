import { test, expect, type Locator, type Page } from '@playwright/test';
import { mockAuditLogQueryAPI, mockAuditLogFacetQueryAPI } from './helpers/api-mocks';

/**
 * Each facet dropdown in SimpleQueryBuilder offers an "All" item and reads
 * "All" in its trigger whenever the filter is empty, so a user can tell at a
 * glance that nothing is selected and can clear a choice from the same list.
 *
 * Facet values come from mockAuditLogFacetQueryAPI's defaults.
 */
interface Dropdown {
  label: string;
  option: string;
  count: number;
}

const DROPDOWNS: Dropdown[] = [
  { label: 'Action', option: 'create', count: 12 },
  { label: 'Resource', option: 'deployments', count: 9 },
  { label: 'Namespace', option: 'default', count: 16 },
  { label: 'User', option: 'alice', count: 16 },
];

/**
 * The trigger reads "All" as a selected value, not as placeholder text.
 * datum-ui's Autocomplete renders an unresolved value as a muted placeholder
 * span, which is what the regression showed (the placeholder is also "All").
 */
async function expectReadsAll(button: Locator) {
  await expect(button).toHaveText('All');
  await expect(button.locator('span.text-muted-foreground')).toHaveCount(0);
}

/** The combobox trigger that sits under the given field label. */
function trigger(page: Page, label: string) {
  return page
    .locator('label', { hasText: new RegExp(`^${label}$`) })
    .locator('..')
    .getByRole('combobox');
}

test.beforeEach(async ({ page }) => {
  await mockAuditLogQueryAPI(page, []);
  await mockAuditLogFacetQueryAPI(page);
  await page.goto('/query-builder');
});

for (const dropdown of DROPDOWNS) {
  test(`${dropdown.label} dropdown offers All and reads All when unfiltered`, async ({ page }) => {
    const button = trigger(page, dropdown.label);
    await expect(button).toBeEnabled();
    await expectReadsAll(button);

    await button.click();
    const options = page.getByRole('option');
    await expect(options.first()).toHaveText('All');
    const choice = options.filter({ hasText: dropdown.option });
    await expect(choice).toBeVisible();
    await expect(choice).toContainText(String(dropdown.count));

    await choice.click();
    await expect(page.getByRole('option')).toHaveCount(0);
    await expect(button).toHaveText(`${dropdown.option} (${dropdown.count})`);

    await button.click();
    await page.getByRole('option', { name: 'All', exact: true }).click();
    await expect(page.getByRole('option')).toHaveCount(0);
    await expectReadsAll(button);
  });
}
