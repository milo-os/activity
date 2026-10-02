import { test, expect, type Page } from '@playwright/test';
import { mockActivityQueryAPI, mockActivityFacetQueryAPI } from './helpers/api-mocks';

// datum-ui's picker trigger is a combobox with no accessible name; it is the
// only combobox on the activity feed page. Its popover is a dialog whose day
// cells are named by full date ("Saturday, August 15th, 2026"), so days are
// matched by ordinal within the first (earlier) month grid.
const timeRangeTrigger = (page: Page) => page.getByRole('combobox').first();
const timeRangePopover = (page: Page) => page.getByRole('dialog').last();

test.beforeEach(async ({ page }) => {
  await mockActivityQueryAPI(page, []);
  await mockActivityFacetQueryAPI(page);
});

test('a preset writes its relative key to the URL', async ({ page }) => {
  await page.goto('/activity-feed');
  await timeRangeTrigger(page).click();
  await timeRangePopover(page).getByRole('button', { name: 'Last 24 hours' }).click();
  // The example app persists the feed time range as startTime/endTime.
  await expect(page).toHaveURL(/startTime=now-24h/);
  await expect(page).not.toHaveURL(/endTime=/);
});

test('a manual range writes ISO timestamps and cannot be inverted', async ({ page }) => {
  await page.goto('/activity-feed');
  await timeRangeTrigger(page).click();

  // Pick the later day first, then the earlier one. The range picker orders them.
  const calendar = timeRangePopover(page).getByRole('grid').first();
  await calendar.getByRole('button', { name: / 15th, / }).click();
  await calendar.getByRole('button', { name: / 10th, / }).click();
  await timeRangePopover(page).getByRole('button', { name: /apply/i }).click();

  const url = new URL(page.url());
  const start = new Date(url.searchParams.get('startTime')!);
  const end = new Date(url.searchParams.get('endTime')!);
  expect(start.getTime()).toBeLessThan(end.getTime());
  expect(url.searchParams.get('startTime')).toMatch(/^\d{4}-\d{2}-\d{2}T/);
});

test('a URL with a preset key highlights that preset', async ({ page }) => {
  await page.goto('/activity-feed?startTime=now-7d');
  await expect(timeRangeTrigger(page)).toContainText('Last 7 days');
});
