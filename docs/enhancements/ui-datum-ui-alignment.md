---
status: proposed
---

# Align activity-ui with datum-ui

> Tracks [datum-cloud/cloud-portal#1471](https://github.com/datum-cloud/cloud-portal/issues/1471),
> [datum-cloud/cloud-portal#1472](https://github.com/datum-cloud/cloud-portal/issues/1472), and
> [datum-cloud/cloud-portal#1468](https://github.com/datum-cloud/cloud-portal/issues/1468).

## Table of Contents

- [Summary](#summary)
- [Problem](#problem)
- [Design](#design)
  - [Theme contract](#theme-contract)
  - [Primitive replacement](#primitive-replacement)
  - [Time range picker](#time-range-picker)
  - [Colour tokens](#colour-tokens)
  - [Feed pagination](#feed-pagination)
  - [Public API compatibility](#public-api-compatibility)
  - [Peer dependency range](#peer-dependency-range)
- [Verification](#verification)
- [Delivery](#delivery)
- [Alternatives](#alternatives)
- [Failure modes](#failure-modes)
- [Decisions](#decisions)
- [Open questions](#open-questions)

---

## Summary

`@datum-cloud/activity-ui` ships its own copies of twenty UI primitives, a hand-rolled time range dropdown built on native `datetime-local` inputs, and about two hundred hard-coded palette classes with hand-written `dark:` variants. This RFC replaces the primitives with datum-ui's, swaps the dropdown for datum-ui's `DateTimeRangePicker`, moves every colour to a semantic token, and fixes the feed's infinite scroll so it works when the host page is the scroller. The package keeps its public API and stays theme-agnostic: it renders under whatever datum-ui `ThemeProvider` the host app supplies.

## Problem

Three cloud-portal bugs share one root: the package draws its own UI instead of reusing datum-ui.

- **#1471.** The custom range's Apply button only checks that both fields are filled. Nothing stops a start after the end, and the query is sent as is.
- **#1472.** The custom range uses native `datetime-local` inputs, so the calendar icon is the browser's own indicator. Its rendering depends on the browser and the resolved `color-scheme`, and the reporter sees no icon in light mode. The package has no control over that icon at all.
- **#1468.** The feed's infinite scroll observes a trigger element with the feed's own list container as the observer root. That container only scrolls when a parent constrains its height. In cloud-portal the dashboard layout is the scroller, so the list grows to full height, the observer fires at most once, and users see one page with no way to load more.

Beyond the bugs, the package cannot follow the host theme reliably. Datum-ui components carry both themes in their tokens. Activity-ui carries 206 hard-coded classes such as `bg-red-50` and `text-green-600` across 24 files, each paired by hand with a `dark:` variant, and each a place where the package drifts from the host's palette.

Two apps consume the package. cloud-portal uses `ActivityFeed`, the filter serialisers, and the error formatter. staff-portal imports 28 symbols including the re-exported Tooltip primitives, `ActivityFeedFilters`, `PolicyEditor`, `PolicyList`, and `useActivityFeed`. cloud-portal is on datum-ui 2.8.0, staff-portal on 1.3.1, and the package declares a peer of `^0.8.0`, which predates the picker family. datum-ui's current release is 2.9.1.

## Design

### Theme contract

The package does not own a theme. It never renders a `ThemeProvider`, never reads `prefers-color-scheme`, and never sets `color-scheme`. The host app's datum-ui `ThemeProvider` applies the theme class and writes `color-scheme` to the root element; datum-ui's tokens do the rest. A component in this package is correct when it renders only datum-ui components and semantic token classes.

### Primitive replacement

Every module under `ui/src/components/ui/` becomes either a re-export of the datum-ui module of the same name or a small composition of datum-ui parts. Local export names are kept so importers inside and outside the package do not change.

| Local module | Importing files | Replacement |
|---|---|---|
| button, card, input, label, textarea, skeleton, separator, checkbox, tabs, sheet, alert | 34 down to 0 | already re-exported from `@datum-cloud/datum-ui/<name>` by the index; the local copies are dead and are deleted |
| badge, dialog | 16, 4 | existing adapters over datum-ui's Badge and compound Dialog; kept as they are |
| tooltip | 16 | kept as a local adapter on `@radix-ui/react-tooltip`; datum-ui's `Tooltip` wraps the trigger in an inline-flex span that breaks truncation in feed rows, which the file documents |
| select | 3 | re-export from `@datum-cloud/datum-ui/select` |
| combobox | 2 | wrapper around `@datum-cloud/datum-ui/autocomplete`; datum-ui exports no combobox subpath |
| multi-combobox | 1 | re-export from `@datum-cloud/datum-ui/multi-select` |
| add-filter-dropdown, filter-chip | 3 each | rebuilt on `@datum-cloud/datum-ui/popover` and `command`; stay local because datum-ui has no filter chip |
| time-range-dropdown | 5 | see [Time range picker](#time-range-picker) |

Direct imports of `@radix-ui/*` and `cmdk` disappear from the package except `@radix-ui/react-tooltip`, which the tooltip adapter keeps. The other Radix peers and `cmdk` are removed in the same release as the peer range change.

### Time range picker

`TimeRangeDropdown` keeps its props and becomes a wrapper around datum-ui's `DateTimeRangePicker` from `@datum-cloud/datum-ui/picker`.

- The feed's relative presets (`now-1h`, `now-24h`, `now-7d`, `now-30d`) become picker presets whose `getRange` resolves the window at click time. The preset `key` is the relative string, so the emitted value carries `preset: "now-7d"` and the filters component keeps writing `start=now-7d` to the URL exactly as today.
- A manual range emits absolute ISO timestamps, as today.
- The picker enforces `from <= to` and exposes `minDate` and `maxDate`; the wrapper passes `disableFuture`. This closes #1471.
- The picker draws its own trigger and calendar icon and renders no native `datetime-local` control, so the browser indicator is out of the picture. This closes #1472.
- `SimpleQueryBuilder`, `AuditLogFilters`, and `EventsFeedFilters` use the same wrapper.

### Colour tokens

All 206 hard-coded palette classes move to semantic tokens, and every `dark:` variant they carried is deleted.

| Meaning today | Replacement |
|---|---|
| create or success (`green-*`) | Badge `type="success"`, Alert `variant="success"`; bare text or borders use the theme's `--success-*` scale |
| delete or error (`red-*`) | Badge `type="danger"`, Alert `variant="destructive"`, `text-destructive`, `border-destructive` |
| update or warning (`amber-*`, `yellow-*`) | Badge `type="warning"`, Alert `variant="warning"`; bare colours use the theme's warning scale |
| informational (`blue-*`) | Badge `type="info"`, Alert `variant="info"`; bare colours use the theme's info scale |
| neutral surfaces (`gray-*`, `slate-*`, `white`) | `bg-muted`, `bg-card`, `bg-background`, `border-border`, `text-muted-foreground` |

Component variants come first. A bare status colour is only used where no datum-ui component carries the meaning, and each one is verified in the example app because the status scales live in datum-ui's theme stylesheet, not in the base tokens.

Files in scope: the feed items, expanded details, alert primitive, policy list, editor, detail and edit views, rule editor and list, reindex views, filter builders, events feed, and the three primitives that still carry a palette class. The whole package is in scope, not only the components cloud-portal renders.

### Feed pagination

`ActivityFeed` decides the intersection-observer root when the trigger mounts. If the list container's `scrollHeight` exceeds its `clientHeight`, the container is the root as today. Otherwise the root is `null`, which is the viewport, so a host page that scrolls itself still brings the trigger into view. The observer is rebuilt when `hasMore` changes, as today.

cloud-portal's `ResourceActivityFeed` wrapper additionally passes `infiniteScroll={false}`, which renders the package's existing "Load more" footer with a running count. That is the behaviour #1468 asks for and it does not depend on layout.

### Public API compatibility

Nothing exported from `ui/src/index.ts` is renamed or removed. Re-exported primitives keep their names and their prop shapes where datum-ui's match. Where datum-ui's props differ, the local module adapts them so existing call sites in staff-portal keep compiling. `TimeRangeDropdownProps` and `TimeRangePreset` are unchanged.

### Peer dependency range

`@datum-cloud/datum-ui` moves from `^0.8.0` to `^2.9.0`, and the package's development dependency moves to 2.9.1, so the build, the example app, and the Playwright suite run against what cloud-portal ships. The 2.9.x releases are additive (card layout variants, a settings nav, and picker triggers switching to `rounded-lg`). Radix and cmdk peers are removed, except `@radix-ui/react-tooltip`. Because the peer range moves, the release is a minor bump to 0.6.0.

The package supports one datum-ui major. Supporting 1.x as well would mean every change is verified twice and still leaves room for silent runtime and stylesheet drift between majors, which a typecheck cannot catch.

staff-portal is not touched. It stays on activity-ui 0.5.1 and datum-ui 1.3.1, and 0.6.0 is not installable there until it moves to datum-ui 2.x. That move is a separate migration: datum-ui 2.0 raised its TanStack Table and Motion peers to new majors, and staff-portal imports TanStack Table in 38 files, datum-ui's data table in 36, and Motion in 5. The frozen public API in this RFC is what makes that later bump a version change rather than a rewrite.

## Verification

- CI runs only Go tests today; a `ui-tests` job (lint, typecheck, build, Playwright) is added so every PR here is checked.
- The package has no unit tests; it has a Playwright suite against `ui/example` with route mocks. New specs cover: applying an inverted custom range is impossible; a preset round-trips through the URL as its relative key; a manual range round-trips as ISO; and a feed inside an unconstrained page loads a second page.
- Every changed view is checked in the example app in light and dark, since token mapping is the riskiest change.
- `rollup -c`, `tsc --noEmit`, and `eslint` pass against datum-ui 2.9.1.
- staff-portal's 28 imported symbols are checked against the package's public declarations so the API freeze holds, even though staff-portal does not take this release yet.
- cloud-portal is verified in the browser on the DNS zone activity tab and the org activity page after the bump.

## Delivery

Three pull requests in milo-os/activity, each independently reviewable and releasable:

1. Primitive re-exports, peer range change, and removal of Radix and cmdk imports.
2. `TimeRangeDropdown` on the picker, and the observer root fallback.
3. Colour tokens across the package.

Then a minor release, 0.6.0, through the manual publish workflow. Then one cloud-portal pull request that bumps activity-ui to 0.6.0 and datum-ui to 2.9.1, and sets `infiniteScroll={false}`, closing #1468, #1471, and #1472. staff-portal is left on 0.5.1 until its own datum-ui 2.x migration; nothing in its imports changes when it does bump.

## Alternatives

- **Patch the native inputs.** Add ordering validation and `min`/`max` to the existing dropdown and style the native indicator. Cheaper, but leaves the icon at the browser's mercy and keeps a second date picker implementation alive next to datum-ui's.
- **Fix only the components cloud-portal renders.** Smaller PR, but the policy and audit views keep drifting from the theme and staff-portal never benefits.
- **Set `color-scheme` in cloud-portal's stylesheet.** Rejected: datum-ui's `ThemeProvider` already writes `color-scheme` inline on the root element, so a stylesheet rule would be redundant and would fight the provider.

## Failure modes

- **A datum-ui prop shape differs from the local primitive.** Caught by the public-declaration check against staff-portal's imports; the local module adapts rather than the consumer.
- **staff-portal upgrades activity-ui before datum-ui.** The `^2.9.0` peer makes the install fail loudly instead of running with a mismatched major.
- **The viewport-root fallback fires once and stops.** The trigger stays intersecting after a load, so the observer only re-arms on `hasMore` changes. The cloud-portal footer is the belt-and-braces path; the Playwright pagination spec covers the fallback itself.
- **Preset round-tripping breaks URL state.** Covered by the URL specs; the preset key is the relative string on purpose.

## Decisions

- The package never owns theme; the host does.
- Public API is frozen for this change.
- Peer range is `^2.9.0`; the package and cloud-portal move to datum-ui 2.9.1, and staff-portal is not touched.
- Three PRs, one minor release.

## Open questions

- None at the time of writing.
