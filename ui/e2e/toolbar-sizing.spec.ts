import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  mockActivityQueryAPI,
  mockActivityFacetQueryAPI,
  mockEventQueryAPI,
  mockEventFacetQueryAPI,
  mockAuditLogQueryAPI,
  mockAuditLogFacetQueryAPI,
} from './helpers/api-mocks';

/**
 * Every control in the filter toolbars (and the feed header's Pause/Resume
 * button) renders at datum-ui's small-control height so the rows line up.
 */
const TARGET_HEIGHT = 36;
const TOLERANCE_PX = 1;

interface ToolbarPage {
  path: string;
  mock: (page: Page) => Promise<void>;
  /** Accessible name of the segmented source toggle, if the page has one */
  toggleName?: string;
  /** Placeholder of the search input, if the page has one */
  searchPlaceholder?: string;
  /** Toolbar buttons that stand in for a source toggle (audit logs) */
  extraButtons?: string[];
  /** Label of a text-mode filter in the Add Filters menu */
  textFilterLabel: string;
  hasPauseButton: boolean;
}

const PAGES: ToolbarPage[] = [
  {
    path: '/activity-feed',
    mock: async (page) => {
      await mockActivityQueryAPI(page, []);
      await mockActivityFacetQueryAPI(page);
    },
    toggleName: 'Filter by change source',
    searchPlaceholder: 'Search activities...',
    textFilterLabel: 'Resource Name',
    hasPauseButton: true,
  },
  {
    path: '/events',
    mock: async (page) => {
      await mockEventQueryAPI(page, []);
      await mockEventFacetQueryAPI(page);
    },
    toggleName: 'Filter by event type',
    searchPlaceholder: 'Search events...',
    textFilterLabel: 'Resource Name',
    hasPauseButton: true,
  },
  {
    path: '/audit-logs',
    mock: async (page) => {
      await mockAuditLogQueryAPI(page, []);
      await mockAuditLogFacetQueryAPI(page);
    },
    extraButtons: ['Actions', 'All Users'],
    textFilterLabel: 'Name',
    hasPauseButton: false,
  },
];

const addFiltersButton = (page: Page) => page.getByRole('button', { name: /(Add )?Filters/i });

/** Add a text-mode filter, give it a value, and close its popover so the chip persists. */
async function addTextFilterChip(page: Page, label: string, value: string) {
  await addFiltersButton(page).click();
  await page.getByRole('button', { name: label, exact: true }).click();
  const input = page.getByPlaceholder('Enter resource name...');
  await expect(input).toBeVisible({ timeout: 5000 });
  await input.fill(value);
  await page.keyboard.press('Escape');
  await expect(input).toBeHidden();
}

async function measure(entries: Array<[string, Locator]>) {
  const heights: Array<{ name: string; height: number }> = [];
  for (const [name, locator] of entries) {
    await expect(locator, name).toBeVisible();
    const box = await locator.boundingBox();
    expect(box, `${name} has a bounding box`).not.toBeNull();
    heights.push({ name, height: box!.height });
  }
  return heights;
}

for (const config of PAGES) {
  test(`${config.path}: toolbar controls share one ${TARGET_HEIGHT}px height`, async ({ page }) => {
    await config.mock(page);
    await page.goto(config.path);
    await expect(addFiltersButton(page)).toBeVisible({ timeout: 10000 });

    await addTextFilterChip(page, config.textFilterLabel, 'web-app');

    const entries: Array<[string, Locator]> = [];

    if (config.toggleName) {
      const toggle = page.getByRole('group', { name: config.toggleName });
      entries.push(['source toggle', toggle]);
      const segments = toggle.getByRole('button');
      const count = await segments.count();
      for (let i = 0; i < count; i++) {
        entries.push([`source toggle segment ${i + 1}`, segments.nth(i)]);
      }
    }
    for (const name of config.extraButtons ?? []) {
      entries.push([`${name} button`, page.getByRole('button', { name, exact: true })]);
    }
    if (config.searchPlaceholder) {
      entries.push(['search input', page.getByPlaceholder(config.searchPlaceholder)]);
    }
    entries.push(['Add Filters trigger', addFiltersButton(page)]);
    entries.push([
      'filter chip trigger',
      page.getByRole('button', { name: new RegExp(`^${config.textFilterLabel}:`) }),
    ]);
    entries.push([
      'filter chip remove button',
      page.getByRole('button', { name: `Clear ${config.textFilterLabel} filter` }),
    ]);
    entries.push(['time range trigger', page.getByRole('combobox').first()]);
    if (config.hasPauseButton) {
      entries.push(['Pause/Resume button', page.getByRole('button', { name: /^(Pause|Resume)$/ })]);
    }

    const heights = await measure(entries);

    for (const { name, height } of heights) {
      expect(Math.abs(height - TARGET_HEIGHT), `${name} is ${height}px`).toBeLessThanOrEqual(TOLERANCE_PX);
    }
    const values = heights.map((h) => h.height);
    expect(Math.max(...values) - Math.min(...values), JSON.stringify(heights)).toBeLessThanOrEqual(
      TOLERANCE_PX
    );
  });
}
