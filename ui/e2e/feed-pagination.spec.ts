import { test, expect } from '@playwright/test';
import { mockActivityQueryAPI, mockActivityFacetQueryAPI, type MockActivity } from './helpers/api-mocks';

function activity(i: number): MockActivity {
  const ts = new Date(Date.UTC(2024, 0, 1, 10, i)).toISOString();
  return {
    metadata: { name: `activity-${i}`, uid: `uid-${i}`, creationTimestamp: ts },
    spec: {
      summary: `alice created deployment app-${i}`,
      changeSource: 'human',
      actor: { name: 'alice', type: 'User' },
      resource: { apiGroup: 'apps', kind: 'Deployment', name: `app-${i}`, namespace: 'default' },
      timestamp: ts,
    },
  };
}

test('loads a second page when the host page is the scroller', async ({ page }) => {
  // Three pages of 30: the second page loads on the first intersection either
  // way, so only a third page proves the observer keeps firing.
  const activities = Array.from({ length: 75 }, (_, i) => activity(i));
  await mockActivityQueryAPI(page, activities, { paginate: true });
  await mockActivityFacetQueryAPI(page);

  await page.goto('/activity-feed');

  // The example app constrains the feed to the viewport so the list scrolls
  // itself. Hosts such as cloud-portal scroll at the document level instead;
  // lift every height and overflow constraint so this page behaves the same.
  await page.addStyleTag({
    content: `
      html, body { height: auto !important; overflow: visible !important; }
      .overflow-hidden, .overflow-y-auto { overflow: visible !important; }
      .min-h-0 { min-height: auto !important; }
    `,
  });

  await expect(page.getByText('alice created deployment app-0')).toBeVisible();
  await expect(page.getByText('alice created deployment app-29')).toBeVisible();
  expect(
    await page.evaluate(() => document.documentElement.scrollHeight > window.innerHeight)
  ).toBe(true);

  // Scroll the document, as a user would, until the last page has rendered.
  await expect(async () => {
    await page.mouse.wheel(0, 20000);
    await expect(page.getByText('alice created deployment app-74')).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10000 });
});
