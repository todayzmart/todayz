# W1-02 UI kit: QA report

Package `@todayz/ui` in `shared/ui`. Branch `worktree-agent-a3e2b313da198d5ba`: built at ac36e9f from `main` d402367, rebased by the lead onto `main` 55549c0 (lockfile re-merged with no new package versions, D-88; after the rebase the frozen offline install, `@todayz/i18n` tests and `make check-ui` pass).

## What was built

- **Components (32 folders, every card in the WP list):**
  - Inputs and actions: Button and IconButton (all variants), TextField, SearchField, Chip, ChipGroup and RadiusChips, QuantityStepper, OtpInput, Tabs, Segmented, FilterButton, LanguagePicker.
  - Status: StatusPill (with `orderStatusTone`), StockTag.
  - Commerce: ProductCard, StoreCard, CategoryTile and CategoryRow, CartBar, PickupCode, OrderTimeline, WalletCard, PosCart.
  - Map: StoreMap, in a separate entry `@todayz/ui/map`. MapLibre with a Protomaps style built from the `map-*` tokens; pins, clusters (supercluster) and the radius ring; a static renderer when WebGL is missing; fully offline.
  - Dashboard: KpiTile and KpiStrip, DataTable (TanStack Table), DemandInsights.
  - Navigation: Sidebar, BottomNav.
  - Feedback and overlays: Banner, Sheet, Dialog (both on a native `<dialog>`), Toast and ToastProvider, Skeleton, EmptyState.
- **Storybook 10:** 154 stories covering every state. Each component with a design card also has a `DesignReference` story copying the card's markup and synthetic names.
- **`src/styles.css`:** only what the design cards do not draw (focus, loading, overlays, sorting, tabs, map overlay); tokens only.
- **Make steps:** `mk/45-ui.mk` adds `check-ui` (lint, CSS lint, types, unit tests with coverage, Storybook build) to `make check`, and `e2e-ui-visual` (Storybook build, design comparison, Chromium axe) to `make e2e`, following 08-testing.
- **Docs:** `shared/ui/CLAUDE.md` and `e2e/scenarios/UI.md` (SC-UI-01…10).

## Test counts

| Suite | Count | Result |
|---|---|---|
| Unit (Vitest + Testing Library, happy-dom), 36 files | 312 tests, including 154 story renders each checked with axe | pass |
| Coverage (v8, `src/`, stories and tests excluded) | statements 98.34%, branches 97.02%, functions 100%, lines 100% (gate 90% each) | pass |
| Token and style checks (`src/test/styles.test.ts`) | undeclared vars; colour literals or dark-theme queries in styles.css; hex anywhere in src | pass |
| Design comparison (Playwright, Chromium) | 23 design cards vs their stories, 0.1% pixel tolerance | 23/23 |
| Axe in Chromium with real tokens (contrast included) | 154 stories | 154/154, with 2 design-owned CartBar findings excepted |
| QA screenshots | 13 | pass |
| Lint (eslint jsx-a11y strict), stylelint, tsc | — | clean |
| `make check-ui` | — | pass |

The design comparison skips the five screen cards (AdminConsole, BusinessOverview, BusinessPOS, CustomerScreens, Cover); those are screens and belong to the app WPs.

## Screenshots

`docs/qa/W1-02/screens/` at 2x, synthetic data with the names from the design previews: `storybook-gallery.png`, `productcard.png`, `storecard.png`, `cartbar.png`, `pickupcode.png`, `ordertimeline.png`, `kpistrip.png`, `demandinsights.png`, `poscart.png`, `sidebar.png`, `bottomnav.png`, `storemap.png`, `storemap-full.png` (MapLibre WebGL, offline, clusters, pins and the radius ring).

Refresh with `TZ_QA_DIR=docs/qa/W1-02/screens pnpm --filter @todayz/ui visual screens.spec.ts` after building Storybook.

## Bugs found and fixed while verifying

1. **The MapLibre worker never loaded in any build.** MapLibre 6 builds its worker URL at runtime from `import.meta.url`, which Vite cannot see, so the built map failed with "Worker failed to load". `loader.ts` now imports the worker with `?url` and calls `setWorkerUrl`; apps get this automatically.
2. **The DataTable body sat 1px low**, because Chromium keeps an sr-only `<caption>` in the table's flow. The table is now named with `aria-label`.
3. **StoreMap height.** It now keeps the design's 312:150 ratio by default; a `height` prop gives a fixed height.
4. **StoreMap focus.** The interactive canvas is focusable, so it is no longer `aria-hidden`; its name comes from `mapLabel`.
5. **StoreCard chevron position.** It is now a plain span, as in the design card.
6. **Story frames.** A `frame()` decorator now skips the design reference stories.
7. **StoreMap zoom reset.** A new radius now resets a cluster zoom; covered by a test.

## Choices beyond the spec

- BottomNav: the active tab uses ink, following the design CSS (the README text says brand).
- StatusPill: `delivery_failed` uses the rejected (red) tone.
- Map labels need glyph files, which are not available offline; road labels appear only when a glyphs URL is passed.
- DemandInsights: "Not useful" is an X icon button named by the prop label; `expired_on_shelf` can never be dismissed; tints and Lucide icons were chosen for the 7 undesigned kinds (CalendarX for expired on shelf).
- StockTag: stale stock gets no colour of its own; it shows the stale label.
- Toast: defaults to 4 s and at most 3 at once (both props); pauses while hovered or focused.
- SearchField: the wrapper is a `div` with a labelled input, so the clear button is not inside a label.
- Icons: the kit uses real Lucide icons; the comparison masks icons at the same boxes on both pages, and masks the map drawing.
- Axe in unit tests: the `region` rule is off because stories render fragments outside landmarks; contrast is checked only in Chromium.
- Vitest is capped at 2 workers (an uncapped run was killed for memory).
- The visual suite and Chromium axe run in `make e2e`, per 08-testing, not in `make check`.

## Changes outside `shared/ui`

`mk/45-ui.mk` (new step file; root Makefile untouched), `e2e/scenarios/UI.md`, `docs/qa/W1-02/screens/`, `pnpm-lock.yaml` (the `shared/ui` importer and its dependencies).

## Open questions

1. **CartBar contrast in the design CSS** (`@todayz/tokens` bundle.css): `.tz-cartbar-text small` (on-brand at 85% opacity) is 4.39:1 for 12px text and `.tz-cartbar-count` is 3.6:1; AA needs 4.5:1. The kit matches the design and `visual/a11y.spec.ts` excepts exactly these two nodes. Suggested design fix: opacity 1 for the `small` and a stronger count wash, then delete the exceptions.
2. **bundle.css colour literals:** a raw `#a3a3a3` (`.tz-map-legend .closed`), and `#fff` inline in the design previews.
3. Confirm `e2e/scenarios/UI.md` is the right place for SC-UI.
4. The visual suite takes about 40 s with no stack; add `e2e-ui-visual` to `CHECK_STEPS` if it should gate merges.

## Not verified

- Focus return to the opener after a Dialog or Sheet closes (native `<dialog>` behaviour; happy-dom does not model it).
- A real Protomaps `.pmtiles` basemap; only the self-contained fixture style was rendered.
- `make e2e` was not run; the two specs in its UI step were run directly and pass.

## Lead review (2026-10-23)

A fresh review found no merge blocker. Follow-up fixes, before the apps build on the affected components: Modal `close` sync, toasts under an open modal, toast action timing and `href`, `Button loading` focus, QuantityStepper focus and limit announcement, StockTag freshness and D-101 wording, SearchField `forwardRef`, backdrop drag-select, Modal children mounting while closed, plus StoreMap and OtpInput items and nits. The CartBar contrast (open question 1) is a design-token change and stays with the lead.
