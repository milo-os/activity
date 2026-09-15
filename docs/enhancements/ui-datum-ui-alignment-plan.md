# Align activity-ui with datum-ui — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `@datum-cloud/activity-ui` render only datum-ui components and semantic tokens, swap its time range dropdown for datum-ui's `DateTimeRangePicker`, and make feed pagination work when the host page scrolls, closing cloud-portal #1468, #1471, and #1472.

**Architecture:** The package keeps its public API and owns no theme. Local primitives under `ui/src/components/ui/` become datum-ui re-exports or thin adapters; the time range dropdown wraps datum-ui's picker and keeps the feed's relative preset keys so URL state is unchanged; `ActivityFeed` falls back to the viewport as its intersection-observer root. Three PRs in `milo-os/activity`, one minor release, then one cloud-portal PR.

**Tech Stack:** React 19, TypeScript 5, Rollup, pnpm, `@datum-cloud/datum-ui` 2.9.1, Playwright against `ui/example` (Remix + Vite) with `page.route` mocks.

**Spec:** `docs/enhancements/ui-datum-ui-alignment.md`

## Global Constraints

- `@datum-cloud/datum-ui` peer range `^2.9.0`; dev dependency `2.9.1`.
- Public API frozen: nothing exported from `ui/src/index.ts` is renamed or removed.
- The package never renders a `ThemeProvider`, never reads `prefers-color-scheme`, never sets `color-scheme`.
- No hard-coded palette classes (`bg-red-50`, `text-green-600`, `dark:` pairs) remain anywhere under `ui/src`.
- `@radix-ui/*` and `cmdk` peers are removed, except `@radix-ui/react-tooltip`, which the tooltip adapter keeps for a documented truncation reason.
- Release is a minor bump to `0.6.0`. staff-portal is not touched.
- Commit subjects: `type(ui): description`, lowercase, imperative, no attribution trailers.

All commands run from `~/Dev/datum/activity/ui` unless stated. Install with `pnpm install`. Build with `pnpm run build`. Typecheck with `pnpm run type-check`. Lint with `pnpm run lint`. E2E with `pnpm run test:e2e` (starts the example app on port 3000; stop any other server on that port first).

---

## PR 1 — Primitives on datum-ui, peer range, CI

Branch: `feat/ui-datum-ui-alignment` (already holds the RFC).

### Task 1: Move datum-ui to 2.9.1

**Files:**
- Modify: `ui/package.json` (`peerDependencies`, `devDependencies`)
- Modify: `ui/pnpm-lock.yaml` (regenerated)

**Interfaces:**
- Produces: a workspace where `@datum-cloud/datum-ui/picker`, `/popover`, `/command`, `/autocomplete`, `/multi-select` resolve.

- [ ] **Step 1: Change the ranges**

In `ui/package.json`:

```json
"peerDependencies": {
  "@datum-cloud/datum-ui": "^2.9.0",
```

```json
"devDependencies": {
  "@datum-cloud/datum-ui": "2.9.1",
```

- [ ] **Step 2: Install and confirm the resolved version**

Run: `pnpm install && node -p 'require("@datum-cloud/datum-ui/package.json").version'`
Expected: `2.9.1`

- [ ] **Step 3: Confirm the build still passes before touching code**

Run: `pnpm run type-check && pnpm run build`
Expected: both exit 0. If `type-check` fails, record every error in the task notes; the following tasks remove the code that causes them.

- [ ] **Step 4: Commit**

```bash
git add package.json pnpm-lock.yaml
git commit -m "chore(ui): require datum-ui 2.9"
```

### Task 2: Delete dead local primitives and repoint the last three imports

The public index already re-exports these from datum-ui, and no internal file imports the local copies: `alert`, `checkbox`, `input`, `label`, `select`, `separator`, `sheet`, `tabs`, `textarea`. Three files still import a local `button`, `card`, or `skeleton`.

**Files:**
- Delete: `ui/src/components/ui/alert.tsx`, `checkbox.tsx`, `input.tsx`, `label.tsx`, `select.tsx`, `separator.tsx`, `sheet.tsx`, `tabs.tsx`, `textarea.tsx`, `button.tsx`, `card.tsx`, `skeleton.tsx`
- Modify: `ui/src/components/VerbToggle.tsx` (button import)
- Modify: `ui/src/components/EventFeedItemSkeleton.tsx` (card and skeleton imports)

- [ ] **Step 1: Prove the nine are dead**

Run:
```bash
for b in alert checkbox input label select separator sheet tabs textarea; do
  echo "$b: $(grep -rlE "from ['\"](\.\.?/)+(components/)?ui/$b['\"]" src | grep -v "src/components/ui/$b.tsx" | wc -l)"
done
```
Expected: every line ends in `0`.

- [ ] **Step 2: Repoint the three live imports**

In `VerbToggle.tsx` replace the button import with:
```ts
import { Button } from '@datum-cloud/datum-ui/button';
```
In `EventFeedItemSkeleton.tsx` replace the card and skeleton imports with:
```ts
import { Card, CardContent } from '@datum-cloud/datum-ui/card';
import { Skeleton } from '@datum-cloud/datum-ui/skeleton';
```
Keep only the names each file actually uses.

- [ ] **Step 3: Delete the twelve files**

```bash
git rm src/components/ui/{alert,checkbox,input,label,select,separator,sheet,tabs,textarea,button,card,skeleton}.tsx
```

- [ ] **Step 4: Verify**

Run: `pnpm run type-check && pnpm run lint && pnpm run build`
Expected: exit 0. A `Cannot find module './ui/button'` error means a consumer was missed; fix its import the same way.

- [ ] **Step 5: Commit**

```bash
git add -A src/components
git commit -m "refactor(ui): drop local primitives already served by datum-ui"
```

### Task 3: Rebuild add-filter-dropdown and filter-chip on datum-ui popover and command

These stay local because datum-ui has no filter chip, but they must stop importing Radix and cmdk directly.

**Files:**
- Modify: `ui/src/components/ui/add-filter-dropdown.tsx`
- Modify: `ui/src/components/ui/filter-chip.tsx`

**Interfaces:**
- Consumes: `Popover`, `PopoverTrigger`, `PopoverContent` from `@datum-cloud/datum-ui/popover`; `Command`, `CommandInput`, `CommandList`, `CommandEmpty`, `CommandGroup`, `CommandItem` from `@datum-cloud/datum-ui/command` (shadcn command surface).
- Produces: unchanged props on both components.

- [ ] **Step 1: Swap the imports in add-filter-dropdown**

Replace:
```ts
import * as Popover from '@radix-ui/react-popover';
```
with:
```ts
import { Popover, PopoverTrigger, PopoverContent } from '@datum-cloud/datum-ui/popover';
```
Then replace each JSX use: `<Popover.Root>` → `<Popover>`, `<Popover.Trigger asChild>` → `<PopoverTrigger asChild>`, `<Popover.Portal><Popover.Content …>` → `<PopoverContent …>` (datum-ui's content already portals). Remove the hand-written popover class string on `PopoverContent`; keep only `align`, `sideOffset`, and a width class.

- [ ] **Step 2: Swap the imports in filter-chip**

Do the same popover replacement, then replace the cmdk import:
```ts
import { Command as CommandPrimitive } from 'cmdk';
```
with:
```ts
import {
  Command,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from '@datum-cloud/datum-ui/command';
```
and map `CommandPrimitive` → `Command`, `CommandPrimitive.Input` → `CommandInput`, `.List` → `CommandList`, `.Empty` → `CommandEmpty`, `.Group` → `CommandGroup`, `.Item` → `CommandItem`. Delete the local class strings that restyled cmdk parts; datum-ui's command is already themed.

- [ ] **Step 3: Verify the filters still work in the example app**

Run: `pnpm run test:e2e -- e2e/activity-source-filter.spec.ts e2e/events-filters.spec.ts`
Expected: all pass. These specs exercise adding a filter chip and picking values.

- [ ] **Step 4: Typecheck, lint, build**

Run: `pnpm run type-check && pnpm run lint && pnpm run build`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add src/components/ui/add-filter-dropdown.tsx src/components/ui/filter-chip.tsx
git commit -m "refactor(ui): build filter chips on datum-ui popover and command"
```

### Task 4: Adapt combobox to datum-ui autocomplete and multi-combobox to multi-select

**Files:**
- Modify: `ui/src/components/ui/combobox.tsx` (full rewrite, same exports)
- Modify: `ui/src/components/ui/multi-combobox.tsx` (full rewrite, same exports)

**Interfaces:**
- Produces, unchanged: `Combobox`, `ComboboxProps { options, value, onValueChange, placeholder?, searchPlaceholder?, emptyMessage?, disabled?, loading?, className?, clearable?, showAllOption?, allOptionLabel? }`, `ComboboxOption { value, label, count? }`; `MultiCombobox`, `MultiComboboxProps { options, values, onValuesChange, placeholder?, searchPlaceholder?, emptyMessage?, disabled?, loading?, className?, maxDisplayed? }`, `MultiComboboxOption`.
- Consumes: `Autocomplete` from `@datum-cloud/datum-ui/autocomplete` with `options`, `value`, `onValueChange`, `placeholder`, `searchPlaceholder`, `emptyContent`, `disabled`, `loading`, `footer`, `renderOption` (it has no `clearable` prop); `MultiSelect` from `@datum-cloud/datum-ui/multi-select` with `options`, `value`, `onValueChange`, `placeholder`, `emptyContent`, `isLoading`, `maxCount`, `className`, `boxClassName` (it has no search placeholder or disabled prop).

- [ ] **Step 1: Rewrite combobox.tsx**

```tsx
import * as React from 'react';
import { Autocomplete } from '@datum-cloud/datum-ui/autocomplete';

export interface ComboboxOption {
  value: string;
  label: string;
  count?: number;
}

export interface ComboboxProps {
  options: ComboboxOption[];
  value: string;
  onValueChange: (value: string) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyMessage?: string;
  disabled?: boolean;
  loading?: boolean;
  className?: string;
  /** Allow clearing the selection */
  clearable?: boolean;
  /** Show "All" option at the top */
  showAllOption?: boolean;
  allOptionLabel?: string;
}

const ALL_VALUE = '';

export function Combobox({
  options,
  value,
  onValueChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  emptyMessage = 'No results',
  disabled,
  loading,
  className,
  clearable,
  showAllOption,
  allOptionLabel = 'All',
}: ComboboxProps) {
  const items = React.useMemo(() => {
    const base = options.map((o) => ({ value: o.value, label: o.label, count: o.count }));
    return showAllOption ? [{ value: ALL_VALUE, label: allOptionLabel }, ...base] : base;
  }, [options, showAllOption, allOptionLabel]);

  return (
    <Autocomplete
      className={className}
      options={items}
      value={value}
      onValueChange={(next) => onValueChange(next === ALL_VALUE ? '' : next)}
      placeholder={placeholder}
      searchPlaceholder={searchPlaceholder}
      emptyContent={emptyMessage}
      disabled={disabled}
      loading={loading}
      footer={
        clearable && value ? (
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground w-full px-2 py-1.5 text-left text-xs"
            onClick={() => onValueChange('')}
          >
            Clear selection
          </button>
        ) : undefined
      }
      renderOption={(option) => (
        <span className="flex w-full items-center justify-between gap-2">
          <span className="truncate">{option.label}</span>
          {'count' in option && option.count !== undefined && (
            <span className="text-muted-foreground text-xs">{option.count}</span>
          )}
        </span>
      )}
    />
  );
}
```

- [ ] **Step 2: Rewrite multi-combobox.tsx**

```tsx
import * as React from 'react';
import { MultiSelect } from '@datum-cloud/datum-ui/multi-select';

export interface MultiComboboxOption {
  value: string;
  label: string;
  count?: number;
}

export interface MultiComboboxProps {
  options: MultiComboboxOption[];
  values: string[];
  onValuesChange: (values: string[]) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyMessage?: string;
  disabled?: boolean;
  loading?: boolean;
  className?: string;
  /** Maximum number of selections to show in the trigger before collapsing */
  maxDisplayed?: number;
}

export function MultiCombobox({
  options,
  values,
  onValuesChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  emptyMessage = 'No results',
  disabled,
  loading,
  className,
  maxDisplayed = 3,
}: MultiComboboxProps) {
  const items = React.useMemo(
    () => options.map((o) => ({ value: o.value, label: o.label })),
    [options]
  );
  return (
    <MultiSelect
      className={className}
      // MultiSelect has no disabled prop; mirror the disabled look on its box.
      boxClassName={disabled ? 'pointer-events-none opacity-50' : undefined}
      options={items}
      value={values}
      onValueChange={onValuesChange}
      placeholder={placeholder}
      emptyContent={emptyMessage}
      isLoading={loading}
      maxCount={maxDisplayed}
    />
  );
}
```
`searchPlaceholder` has no counterpart on `MultiSelect`; keep it in `MultiComboboxProps` for API compatibility and leave it unused with a one-line comment.

- [ ] **Step 3: Verify the one consumer**

`SimpleQueryBuilder.tsx` uses `Combobox`. Run: `pnpm run type-check`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add src/components/ui/combobox.tsx src/components/ui/multi-combobox.tsx
git commit -m "refactor(ui): adapt combobox and multi-combobox to datum-ui"
```

### Task 5: Remove Radix and cmdk peers, keep only the tooltip peer

**Files:**
- Modify: `ui/package.json`
- Modify: `ui/rollup.config.mjs:42-52` (the `external` list)

- [ ] **Step 1: Prove nothing imports them**

Run: `grep -rn "@radix-ui/\|from 'cmdk'" src | grep -v "react-tooltip"`
Expected: no output. If a line prints, fix that file first (Tasks 3 and 4 cover the known ones).

- [ ] **Step 2: Edit package.json**

Remove from `peerDependencies`: `@radix-ui/react-checkbox`, `@radix-ui/react-dialog`, `@radix-ui/react-popover`, `@radix-ui/react-select`, `@radix-ui/react-separator`, `@radix-ui/react-tabs`, `cmdk`. Keep `@radix-ui/react-tooltip`.

- [ ] **Step 3: Keep the rollup external for tooltip only**

Replace `/^@radix-ui\//,` with `'@radix-ui/react-tooltip',` in the `external` array.

- [ ] **Step 4: Verify**

Run: `pnpm install && pnpm run type-check && pnpm run build && pnpm run test:e2e`
Expected: all exit 0.

- [ ] **Step 5: Commit**

```bash
git add package.json pnpm-lock.yaml rollup.config.mjs
git commit -m "chore(ui): drop radix and cmdk peers now served by datum-ui"
```

### Task 6: Add a UI job to CI

`testing.yaml` only runs Go tests today. Every later PR needs the UI checks to run on pull requests.

**Files:**
- Modify: `.github/workflows/testing.yaml` (repo root)

- [ ] **Step 1: Add the job**

Append under `jobs:`:
```yaml
  ui-tests:
    name: UI lint, typecheck, build, e2e
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: ui
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
        with:
          version: 10
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: pnpm
          cache-dependency-path: ui/pnpm-lock.yaml
      - run: pnpm install --frozen-lockfile
      - run: pnpm run lint
      - run: pnpm run type-check
      - run: pnpm run build
      - run: pnpm exec playwright install --with-deps chromium
      - run: pnpm run test:e2e
```
If the workflow's `on:` block only lists `push`, add `pull_request:` so PRs run it.

- [ ] **Step 2: Verify locally that the same commands pass**

Run: `pnpm run lint && pnpm run type-check && pnpm run build && pnpm run test:e2e`
Expected: exit 0.

- [ ] **Step 3: Commit and open PR 1**

```bash
git add .github/workflows/testing.yaml
git commit -m "ci(ui): run lint, typecheck, build and e2e on pull requests"
git push -u origin feat/ui-datum-ui-alignment
gh pr create --title "Build activity-ui primitives on datum-ui 2.9" --body "$(cat <<'BODY'
**Problem**
activity-ui carried its own copies of shadcn primitives next to datum-ui re-exports, imported Radix and cmdk directly, pinned a datum-ui peer that predates the picker family, and had no UI checks in CI.

**Solution**
Require datum-ui 2.9, delete the twelve local primitives the index already sources from datum-ui, rebuild the filter chips on datum-ui popover and command, adapt combobox and multi-combobox to autocomplete and multi-select, drop the Radix and cmdk peers except tooltip, and add a UI CI job. Public exports are unchanged.

Refs datum-cloud/cloud-portal#1468, datum-cloud/cloud-portal#1471, datum-cloud/cloud-portal#1472. Design: docs/enhancements/ui-datum-ui-alignment.md
BODY
)"
```

---

## PR 2 — Picker swap and feed pagination

Branch: `feat/ui-picker-and-pagination`, created from `feat/ui-datum-ui-alignment` after PR 1 merges (or stacked on it if not yet merged).

### Task 7: Extend the activity query mock to paginate

The pagination spec needs a mock that honours `limit` and `continue`.

**Files:**
- Modify: `ui/e2e/helpers/api-mocks.ts:407-455` (`mockActivityQueryAPI`)

**Interfaces:**
- Produces: `mockActivityQueryAPI(page, activities?, options?)` where `options.paginate?: boolean` makes the mock slice by `spec.limit` and return `status.continue` while more remain.

- [ ] **Step 1: Add the option**

Extend `MockApiOptions`:
```ts
export interface MockApiOptions {
  delay?: number;
  error?: { status: number; message: string };
  /** Slice results by spec.limit and return a continue token while more remain. */
  paginate?: boolean;
}
```

- [ ] **Step 2: Implement slicing**

Replace the final `route.fulfill` in `mockActivityQueryAPI` with:
```ts
    const limit: number = request?.spec?.limit ?? filteredActivities.length;
    const offset: number = options?.paginate && request?.spec?.continue
      ? Number(request.spec.continue)
      : 0;
    const page = options?.paginate ? filteredActivities.slice(offset, offset + limit) : filteredActivities;
    const nextOffset = offset + page.length;
    const hasMore = options?.paginate && nextOffset < filteredActivities.length;

    return route.fulfill({
      status: 200,
      json: {
        apiVersion: 'activity.miloapis.com/v1alpha1',
        kind: 'ActivityQuery',
        spec: request?.spec || {},
        status: {
          results: page,
          ...(hasMore ? { continue: String(nextOffset) } : {}),
        },
      },
    });
```

- [ ] **Step 3: Run the existing specs to confirm nothing changed by default**

Run: `pnpm run test:e2e`
Expected: all pass (the option is off unless set).

- [ ] **Step 4: Commit**

```bash
git add e2e/helpers/api-mocks.ts
git commit -m "test(ui): let the activity query mock paginate"
```

### Task 8: Fall back to the viewport as the infinite-scroll root

**Files:**
- Modify: `ui/src/components/ActivityFeed.tsx:217-240` (observer effect)
- Test: `ui/e2e/feed-pagination.spec.ts` (new)

**Interfaces:**
- Consumes: `mockActivityQueryAPI(page, activities, { paginate: true })` from Task 7.

- [ ] **Step 1: Write the failing spec**

Create `e2e/feed-pagination.spec.ts`:
```ts
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
  const activities = Array.from({ length: 45 }, (_, i) => activity(i));
  await mockActivityQueryAPI(page, activities, { paginate: true });
  await mockActivityFacetQueryAPI(page);

  await page.goto('/activity-feed');
  await expect(page.getByText('alice created deployment app-0')).toBeVisible();
  await expect(page.getByText('alice created deployment app-29')).toBeVisible();

  // The example page scrolls at the document level, not inside the feed.
  await page.mouse.wheel(0, 20000);

  await expect(page.getByText('alice created deployment app-44')).toBeVisible({ timeout: 10000 });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm run test:e2e -- e2e/feed-pagination.spec.ts`
Expected: FAIL on the `app-44` assertion (only the first 30 render).

- [ ] **Step 3: Implement the fallback**

In `ActivityFeed.tsx`, replace the observer effect with:
```tsx
  // Infinite scroll using Intersection Observer.
  // The list container is the root only when it actually scrolls. Host apps
  // such as cloud-portal scroll at the page level, so the container never
  // overflows; in that case observe against the viewport instead.
  useEffect(() => {
    if (!infiniteScroll || !hasMore || !loadMoreTriggerRef.current) return;

    const container = scrollContainerRef.current;
    const containerScrolls =
      !!container && container.scrollHeight > container.clientHeight + 1;

    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (entry.isIntersecting && !isLoadingRef.current) {
          loadMoreRef.current();
        }
      },
      {
        root: containerScrolls ? container : null,
        rootMargin: `${loadMoreThreshold}px`,
        threshold: 0,
      },
    );

    observer.observe(loadMoreTriggerRef.current);
    return () => observer.disconnect();
  }, [infiniteScroll, loadMoreThreshold, hasMore, activities.length]);
```
`activities.length` is added to the dependency list on purpose: after a page loads, the trigger moves further down the document and the observer must re-evaluate against the new position; without it the second observer callback never fires in the viewport case.

- [ ] **Step 4: Run the spec to see it pass**

Run: `pnpm run test:e2e -- e2e/feed-pagination.spec.ts`
Expected: PASS.

- [ ] **Step 5: Run the whole suite, typecheck, build**

Run: `pnpm run test:e2e && pnpm run type-check && pnpm run build`
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add src/components/ActivityFeed.tsx e2e/feed-pagination.spec.ts
git commit -m "fix(ui): observe the viewport when the feed does not scroll itself"
```

### Task 9: Rebuild TimeRangeDropdown on datum-ui's DateTimeRangePicker

**Files:**
- Modify: `ui/src/components/ui/time-range-dropdown.tsx` (full rewrite, same exports)
- Test: `ui/e2e/time-range.spec.ts` (new)

**Interfaces:**
- Produces, unchanged: `TimeRangeDropdown`, `TimeRangeDropdownProps { presets, selectedPreset, onPresetSelect, onCustomRangeApply, customStart?, customEnd?, disabled?, className?, displayLabel? }`, `TimeRangePreset { key, label }`.
- Consumes: `DateTimeRangePicker` and `PickerPreset` from `@datum-cloud/datum-ui/picker`. At runtime a preset click emits `{ from, to, preset: key }` (the `preset` field is not in the wrapper's declared type; read it defensively).
- Callers (`ActivityFeedFilters`, `AuditLogFilters`, `EventsFeedFilters`, `SimpleQueryBuilder`) pass `customStart`/`customEnd` in `datetime-local` format (`YYYY-MM-DDTHH:mm`) and expect `onCustomRangeApply(start, end)` in the same format. Keep that contract; convert at the boundary.

- [ ] **Step 1: Write the failing spec**

Create `e2e/time-range.spec.ts`:
```ts
import { test, expect } from '@playwright/test';
import { mockActivityQueryAPI, mockActivityFacetQueryAPI } from './helpers/api-mocks';

test.beforeEach(async ({ page }) => {
  await mockActivityQueryAPI(page, []);
  await mockActivityFacetQueryAPI(page);
});

test('a preset writes its relative key to the URL', async ({ page }) => {
  await page.goto('/activity-feed');
  await page.getByRole('combobox', { name: /time range/i }).click();
  await page.getByRole('button', { name: 'Last 24 hours' }).click();
  await expect(page).toHaveURL(/start=now-24h/);
  await expect(page).not.toHaveURL(/end=/);
});

test('a manual range writes ISO timestamps and cannot be inverted', async ({ page }) => {
  await page.goto('/activity-feed');
  await page.getByRole('combobox', { name: /time range/i }).click();

  // Pick the later day first, then the earlier one. The range picker orders them.
  const calendar = page.getByRole('grid').first();
  await calendar.getByRole('gridcell', { name: /^15$/ }).first().click();
  await calendar.getByRole('gridcell', { name: /^10$/ }).first().click();
  await page.getByRole('button', { name: /apply/i }).click();

  const url = new URL(page.url());
  const start = new Date(url.searchParams.get('start')!);
  const end = new Date(url.searchParams.get('end')!);
  expect(start.getTime()).toBeLessThan(end.getTime());
  expect(url.searchParams.get('start')).toMatch(/^\d{4}-\d{2}-\d{2}T/);
});

test('a URL with a preset key highlights that preset', async ({ page }) => {
  await page.goto('/activity-feed?start=now-7d');
  await expect(page.getByRole('combobox', { name: /time range/i })).toContainText('Last 7 days');
});
```
The accessible names above (`combobox` for the trigger, `grid`/`gridcell` for the calendar, `Apply`) come from datum-ui's picker. If a name differs on first run, read it from `pnpm exec playwright codegen http://localhost:3000/activity-feed` and adjust the locator, not the assertion.

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm run test:e2e -- e2e/time-range.spec.ts`
Expected: the second and third tests FAIL (the old dropdown has no calendar and no combobox trigger).

- [ ] **Step 3: Rewrite time-range-dropdown.tsx**

```tsx
import * as React from 'react';
import { DateTimeRangePicker, type PickerPreset } from '@datum-cloud/datum-ui/picker';

export interface TimeRangePreset {
  key: string;
  label: string;
}

export interface TimeRangeDropdownProps {
  /** Available time range presets, keyed by relative strings such as `now-24h`. */
  presets: TimeRangePreset[];
  /** Currently selected preset key, or 'custom' for custom range */
  selectedPreset: string;
  /** Handler when a preset is selected */
  onPresetSelect: (presetKey: string) => void;
  /** Handler when custom range is applied; values are `datetime-local` strings */
  onCustomRangeApply: (start: string, end: string) => void;
  /** Initial custom start value (datetime-local format) */
  customStart?: string;
  /** Initial custom end value (datetime-local format) */
  customEnd?: string;
  /** Whether the dropdown is disabled */
  disabled?: boolean;
  /** Additional class name */
  className?: string;
  /** Display label for the selected value */
  displayLabel?: string;
}

const RELATIVE_KEY = /^now-(\d+)([mhd])$/;

/** Resolve a relative key such as `now-7d` to a `{ from, to }` window ending now. */
export function relativeKeyToRange(key: string, now: Date = new Date()): { from: Date; to: Date } | null {
  const match = RELATIVE_KEY.exec(key);
  if (!match) return null;
  const amount = Number(match[1]);
  const unitMs = { m: 60_000, h: 3_600_000, d: 86_400_000 }[match[2] as 'm' | 'h' | 'd'];
  return { from: new Date(now.getTime() - amount * unitMs), to: now };
}

/** `YYYY-MM-DDTHH:mm` in local time, the format the filter components store. */
export function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function toPickerPresets(presets: TimeRangePreset[]): PickerPreset[] {
  return presets
    .filter((p) => RELATIVE_KEY.test(p.key))
    .map((p) => ({
      key: p.key,
      label: p.label,
      getRange: () => relativeKeyToRange(p.key)!,
    }));
}

/**
 * TimeRangeDropdown - relative presets plus an absolute range, on datum-ui's
 * DateTimeRangePicker. The emitted value carries `preset` when a preset was
 * clicked, which keeps `start=now-7d` style URL state intact.
 */
export function TimeRangeDropdown({
  presets,
  selectedPreset,
  onPresetSelect,
  onCustomRangeApply,
  customStart,
  customEnd,
  disabled = false,
  className,
  displayLabel,
}: TimeRangeDropdownProps) {
  const pickerPresets = React.useMemo(() => toPickerPresets(presets), [presets]);

  const value = React.useMemo(() => {
    if (selectedPreset !== 'custom') {
      const range = relativeKeyToRange(selectedPreset);
      return range
        ? { from: range.from.toISOString(), to: range.to.toISOString(), preset: selectedPreset }
        : null;
    }
    if (customStart && customEnd) {
      return { from: new Date(customStart).toISOString(), to: new Date(customEnd).toISOString() };
    }
    return null;
  }, [selectedPreset, customStart, customEnd]);

  const handleChange = (next: { from: string; to: string } | null) => {
    if (!next) return;
    const preset = (next as { preset?: string }).preset;
    if (preset) {
      onPresetSelect(preset);
      return;
    }
    onCustomRangeApply(toDatetimeLocal(next.from), toDatetimeLocal(next.to));
  };

  return (
    <DateTimeRangePicker
      className={className}
      value={value}
      onChange={handleChange}
      presets={pickerPresets}
      disableFuture
      clearable={false}
      disabled={disabled}
      placeholder={displayLabel ?? 'Select time range'}
      triggerLabel={displayLabel ? () => displayLabel : undefined}
    />
  );
}
```
`triggerLabel` is a render prop `(pendingValue: unknown) => ReactNode` on the wrapper, which is why it is wrapped in a function.

- [ ] **Step 4: Run the spec to see it pass**

Run: `pnpm run test:e2e -- e2e/time-range.spec.ts`
Expected: PASS.

- [ ] **Step 5: Confirm the four call sites need no change**

Run: `pnpm run type-check`
Expected: exit 0. The props are unchanged, so `ActivityFeedFilters`, `AuditLogFilters`, `EventsFeedFilters`, and `SimpleQueryBuilder` compile as they are.

- [ ] **Step 6: Run the full suite and build**

Run: `pnpm run test:e2e && pnpm run lint && pnpm run build`
Expected: exit 0.

- [ ] **Step 7: Commit and open PR 2**

```bash
git add src/components/ui/time-range-dropdown.tsx e2e/time-range.spec.ts
git commit -m "feat(ui): pick time ranges with datum-ui's range picker"
git push -u origin feat/ui-picker-and-pagination
gh pr create --base feat/ui-datum-ui-alignment --title "Use datum-ui's range picker and fix feed pagination" --body "$(cat <<'BODY'
**Problem**
The custom time range used native datetime-local inputs with no ordering check and a browser-drawn calendar icon that disappears in some themes. The feed's infinite scroll observed its own list container, which never scrolls when the host page is the scroller, so users saw one page with no way to load more.

**Solution**
TimeRangeDropdown wraps datum-ui's DateTimeRangePicker; relative presets keep their keys so URL state is unchanged. ActivityFeed observes the viewport when its container does not overflow. Playwright specs cover preset and manual round-trips and a second page load in an unconstrained layout.

Closes datum-cloud/cloud-portal#1471, datum-cloud/cloud-portal#1472, datum-cloud/cloud-portal#1468. Design: docs/enhancements/ui-datum-ui-alignment.md
BODY
)"
```
Set `--base main` instead if PR 1 has merged.

---

## PR 3 — Colour tokens across the package

Branch: `feat/ui-semantic-colours`, from the same base as PR 2. Tasks 10 to 12 can be split across workers by file group; each ends with the same verification.

**Mapping used by every task below:**

| Hard-coded | Replacement |
|---|---|
| `bg-green-50 dark:bg-green-950`, `bg-green-50/40 dark:bg-green-950/20` | `bg-success-100` |
| `text-green-600 dark:text-green-400`, `text-green-500` | `text-success-500` |
| `bg-green-500`, `bg-green-400` (status dots) | `bg-success-500` |
| `bg-red-50 dark:bg-red-950` | `bg-destructive/10` |
| `text-red-500 dark:text-red-400`, `text-red-600`, `text-red-700`, `text-red-900` | `text-destructive` |
| `bg-red-500 dark:bg-red-400` (status dots) | `bg-destructive` |
| `border-red-200 dark:border-red-800` | `border-destructive/30` |
| `bg-amber-50 dark:bg-amber-950`, `bg-yellow-*` | `bg-warning-100` |
| `text-amber-600 dark:text-amber-400`, `text-amber-700`, `text-amber-900` | `text-warning-500` |
| `bg-blue-50 dark:bg-blue-950` | `bg-info-100` |
| `text-blue-500 dark:text-blue-400` | `text-info-500` |
| `bg-slate-100 dark:bg-slate-800`, `bg-gray-100 dark:bg-gray-800` | `bg-muted` |
| `text-slate-500 dark:text-slate-400`, `text-gray-400`, `text-gray-500` | `text-muted-foreground` |
| `bg-white dark:bg-*` | `bg-card` or `bg-background` (card surfaces vs page) |
| `border-gray-* dark:border-*` | `border-border` |

Before the first replacement, confirm the scale names exist in the installed theme:
`grep -oE -- "--(success|warning|info)-[0-9]{3}" node_modules/@datum-cloud/datum-ui/dist/styles/themes/alpha.css | sort -u`. If `warning` or `info` scales are absent, use `text-warning`/`bg-warning/10` and `text-info`/`bg-info/10` tokens instead; the Alert and Badge variants stay as the primary path either way.

Wherever a block of classes paints a status *surface with a title*, prefer the component over classes: `<Alert variant="destructive" | "success" | "warning" | "info">` and `<Badge type="danger" | "success" | "warning" | "info">`.

### Task 10: Feed items and the alert primitive

**Files:**
- Modify: `ui/src/components/ActivityFeedItem.tsx:128-144, 345`
- Modify: `ui/src/components/AuditLogFeedItem.tsx` (34 occurrences)
- Modify: `ui/src/components/EventFeedItem.tsx` (24 occurrences)
- Modify: `ui/src/components/ActivityFeed.tsx:310-330` (streaming dots)
- Modify: `ui/src/components/ActivityExpandedDetails.tsx`, `AuditLogExpandedDetails.tsx`, `details.tsx`, `EventsFeed.tsx`
- Delete: `ui/src/components/ui/alert.tsx` if Task 2 left it (it is dead; the index exports datum-ui's Alert)

- [ ] **Step 1: Baseline the count**

Run: `grep -rnoE "(bg|text|border|ring|from|to|fill|stroke)-(white|black|gray|slate|zinc|neutral|stone|red|green|blue|yellow|amber|orange|emerald|purple|indigo|pink|cyan|teal|lime|sky)(-[0-9]{2,3})?(/[0-9]+)?" src | wc -l`
Expected: `206` (or whatever Tasks 2 and 3 left; record it).

- [ ] **Step 2: Replace in ActivityFeedItem**

The verb-colour map at lines 128 to 144 becomes:
```ts
      case 'create':
        return { container: 'bg-success-100', icon: 'text-success-500' };
      case 'update':
        return { container: 'bg-info-100', icon: 'text-info-500' };
      case 'delete':
        return { container: 'bg-destructive/10', icon: 'text-destructive' };
      default:
        return { container: 'bg-muted', icon: 'text-muted-foreground' };
```
(keep whichever verb keys the file already uses; only the class strings change.) Line 345: `isNew && 'bg-success-100/60'`.

- [ ] **Step 3: Replace in the other files using the mapping table**

Work file by file. After each file: `grep -nE "(red|green|blue|amber|yellow|slate|gray|white)-" <file>` must print nothing.

- [ ] **Step 4: Visual check in both themes**

Run the example app (`cd example && pnpm run dev`), open `/activity-feed`, `/events`, `/audit-logs`; toggle the OS theme or add/remove the `dark` class on `<html>` in DevTools. Every create/update/delete tint and every status dot must be visible in both.

- [ ] **Step 5: Verify and commit**

Run: `pnpm run lint && pnpm run type-check && pnpm run build && pnpm run test:e2e`
```bash
git add src/components
git commit -m "style(ui): use theme tokens in feed items"
```

### Task 11: Policy, rule, and reindex views

**Files:**
- Modify: `ui/src/components/PolicyList.tsx` (14), `PolicyEditor.tsx` (10), `PolicyDetailView.tsx` (10), `PolicyEditView.tsx` (7), `PolicyRuleEditor.tsx` (7), `PolicyRuleListItem.tsx` (3), `ReindexJobDetailView.tsx` (8), `ReindexJobList.tsx` (1)

- [ ] **Step 1: Replace using the mapping table**

`PolicyList.tsx:330` status dot → `bg-success-500`; `:338-341` → `text-destructive`, `text-warning-500`, `text-muted-foreground`; `:358` and `:372` disabled rows → `bg-muted text-muted-foreground`. Apply the table to the rest.

- [ ] **Step 2: Visual check**

Open `/policies` and a policy detail and edit page in the example app in both themes.

- [ ] **Step 3: Verify and commit**

Run: `pnpm run lint && pnpm run type-check && pnpm run build && pnpm run test:e2e -- e2e/policies`
```bash
git add src/components
git commit -m "style(ui): use theme tokens in policy and reindex views"
```

### Task 12: Filter builders and the three remaining primitives

**Files:**
- Modify: `ui/src/components/FilterBuilderWithAutocomplete.tsx` (9), `FilterBuilder.tsx` (4), `ui/src/components/ui/select.tsx` (deleted in Task 2; skip if gone), `ui/src/components/ui/multi-combobox.tsx` (2), `combobox.tsx` (2), `add-filter-dropdown.tsx` (2), `sheet.tsx` (deleted in Task 2; skip if gone)

- [ ] **Step 1: Replace using the mapping table**

- [ ] **Step 2: Prove the package is clean**

Run: `grep -rnoE "(bg|text|border|ring|from|to|fill|stroke)-(white|black|gray|slate|zinc|neutral|stone|red|green|blue|yellow|amber|orange|emerald|purple|indigo|pink|cyan|teal|lime|sky)(-[0-9]{2,3})?(/[0-9]+)?" src | wc -l`
Expected: `0`.

Run: `grep -rn "dark:" src | wc -l`
Expected: `0`.

- [ ] **Step 3: Verify, commit, open PR 3**

Run: `pnpm run lint && pnpm run type-check && pnpm run build && pnpm run test:e2e`
```bash
git add src/components
git commit -m "style(ui): finish moving to theme tokens"
git push -u origin feat/ui-semantic-colours
gh pr create --base feat/ui-datum-ui-alignment --title "Move activity-ui colours to datum-ui theme tokens" --body "$(cat <<'BODY'
**Problem**
The package carried about two hundred hard-coded palette classes with hand-written dark variants, so its colours drifted from the host theme and every surface had to be maintained twice.

**Solution**
Every colour now comes from datum-ui components (Alert and Badge variants) or theme tokens, and the dark variants are gone. Verified in the example app in both themes.

Refs datum-cloud/cloud-portal#1472. Design: docs/enhancements/ui-datum-ui-alignment.md
BODY
)"
```

---

## Release and cloud-portal

### Task 13: Release activity-ui 0.6.0

Run after PRs 1 to 3 have merged to `main`.

- [ ] **Step 1: Confirm main is green**

Run: `gh run list --repo milo-os/activity --branch main --limit 3`
Expected: the latest `Execute Golang Tests` run, including the new `ui-tests` job, is `success`.

- [ ] **Step 2: Dispatch the release**

Run: `gh workflow run publish-ui-npm.yaml --repo milo-os/activity -f bump-type=minor`
Then: `gh run watch --repo milo-os/activity $(gh run list --repo milo-os/activity --workflow publish-ui-npm.yaml --limit 1 --json databaseId --jq '.[0].databaseId')`
Expected: run succeeds; `npm view @datum-cloud/activity-ui version` prints `0.6.0`.

### Task 14: Bump cloud-portal and switch embedded feeds to the Load more footer

Repository: cloud-portal, branch `chore/activity-ui-0.6`.

**Files:**
- Modify: `package.json` (`@datum-cloud/activity-ui` → `^0.6.0`, `@datum-cloud/datum-ui` → `^2.9.1`)
- Modify: `app/features/activity/resource-activity-feed.tsx:148` (`ActivityFeed` props)

- [ ] **Step 1: Bump and install**

Run: `bun add @datum-cloud/activity-ui@^0.6.0 @datum-cloud/datum-ui@^2.9.1`
Expected: lockfile updates; `bun run typecheck` exits 0.

- [ ] **Step 2: Pass infiniteScroll false**

In `resource-activity-feed.tsx`, add to the `<ActivityFeed>` props, before `{...feedProps}` so a caller can still override it:
```tsx
      infiniteScroll={false}
```
And in `ResourceActivityFeedProps`' doc block add one line under `pageSize`: `Embedded feeds page with an explicit "Load more" footer; pass \`feedProps={{ infiniteScroll: true }}\` to opt back in.`

- [ ] **Step 3: Verify**

Run: `bun run typecheck && bun run lint && bun test && bun run build`
Expected: exit 0.

Then start `bun run dev`, sign in to staging, and check in the browser:
- DNS zone → Activity: the footer shows a count and "Load more"; clicking it appends events.
- Org → Activity: the time range trigger opens datum-ui's picker; a preset writes `start=now-24h` to the URL; picking two calendar days in reverse order still yields start before end.
- Light and dark theme: the picker's calendar icon is visible in both.

- [ ] **Step 4: Commit and open the PR**

```bash
git add package.json bun.lock app/features/activity/resource-activity-feed.tsx
git commit -m "chore(activity): take activity-ui 0.6 and datum-ui 2.9"
git push -u origin chore/activity-ui-0.6
gh pr create --title "Fix Activity Log range picker and pagination" --label bug --body "$(cat <<'BODY'
**Problem**
The Activity Log's custom range accepted a start after the end, lost its calendar icon in light mode, and only ever showed the first thirty events because the feed's infinite scroll never fired when the dashboard page is the scroller.

**Solution**
Take activity-ui 0.6.0, which uses datum-ui's range picker and observes the viewport when its container does not scroll, move datum-ui to 2.9.1, and render embedded feeds with an explicit Load more footer.

Closes #1471
Closes #1472
Closes #1468
BODY
)"
```

---

## Orchestration map (for Orca)

| Wave | Task(s) | Worktree | Depends on |
|---|---|---|---|
| 1 | Tasks 1–6 (PR 1) | activity: `feat/ui-datum-ui-alignment` | — |
| 2a | Tasks 7–9 (PR 2) | activity: `feat/ui-picker-and-pagination` | wave 1 merged or stacked |
| 2b | Tasks 10–12 (PR 3) | activity: `feat/ui-semantic-colours` | wave 1 merged or stacked |
| 3 | Task 13 | — | PRs 1–3 merged |
| 4 | Task 14 | cloud-portal: `chore/activity-ui-0.6` | Task 13 |

Waves 2a and 2b both touch `ActivityFeed.tsx` (observer effect vs streaming-dot classes) in different regions; rebase 2b on 2a before merging to avoid a trivial conflict.
