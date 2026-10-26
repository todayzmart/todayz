# D-260: the bright skin in the business app

Founder, 26 Oct: "current customer UI is getting better, lets do the same for retailer too".

## What changed

- The business shell sets `data-skin="bright"`. The page is grey `page`, cards are white with `shadow-card`, primary buttons are `brand`, selected chips are `brand-soft`, the active bottom tab is `brand` with its bar, and page titles are Anek 800.
- `design/source/components/bundle.css` gets business-only rules, added at the end of the file. KPI tiles, the KPI strip, tables, POS tiles and the greeting card lift off the page. The sidebar's active item (≥1024px) is `brand-soft`.
- Text sitting directly on the grey is `ink-2`: the stock, offers and customers sublines use `tz-page-sub`, and the orders sound row moved off `ink-3`.
- POS: tiles fill their grid cells, so the columns are even. On a phone the search takes its own row, with Scan and Custom item below it; before, it was squashed to "Sea" at 360px. Tile size, density and the lack of photos are unchanged, and the offline counter is untouched.
- Tabs and the segmented control are shared with the customer app and keep their look.

## Screens

Phone 360×780 and tablet 768×1024: overview, Sell (4 items in the bill), stock, the stock item sheet with Change price open, orders, offers, customers and settings.

- `all-phone.png`, `all-tablet.png`: every screen in one image (the tablet one at half size).
- `before-all-phone.png`: the same phone screens before the skin.
- `<screen>-<phone|tablet>.png`: one shot each.

Re-run (the bundle must be built with `pnpm run build:e2e` first):

```
cd business
VITE_TURNSTILE_SITE_KEY=x VITE_API_URL=https://api.todayz.localhost TZ_QA_DIR=../docs/qa/D-260 TZ_AXE=<path to axe.min.js> pnpm exec playwright test e2e/bright.shots.spec.ts
```

## Checks

- Business: `typecheck`, `lint`, `lint:css`, `test` and `build` pass. `@todayz/tokens` tests pass.
- axe (WCAG 2.2 AA) is clean on all 16 shots except one skipped rule. The ink-3 sound row it caught on the grey page is fixed.

## Open

- **DataTable focus (predates the skin).** On a phone, the customers table's scrolling wrap (`tz-table-wrap`, `shared/ui` DataTable) is not keyboard-focusable (axe `scrollable-region-focusable`). The fix is `tabIndex={0}`, `role="region"` and a label on the wrap. It is a `shared/ui` change that admin uses too, so it is left for its own change, and the spec skips that one rule until then.
- **Visual baselines.** The business baselines in `e2e/visual/` will differ. They have not been refreshed, and wait for the founder's yes.
- **Desktop.** The sidebar rule (≥1024px) is in place but has no screenshot here.
