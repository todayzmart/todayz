# business/

Business PWA with offline-first POS (business.todayz.in; locally https://business.todayz.localhost). Phone bottom tabs, desktop sidebar.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §4.1, §4.3; 06-screens.md (B-xx); 01-product-rules.md.

## Run

- `make dev`, then open https://business.todayz.localhost
- `pnpm --filter @todayz/business test` / `build`

## Rules for this folder

- Import only from `shared/*` and `design/`, never from a sibling app.
- Tokens only, no raw hex; Lucide icons; sentence case; light theme only.
- Every local write gets a client ULID and device sequence; the server is the real permission gate.
- All user-facing text through i18n keys in `shared/i18n/messages/business/<area>.en.json`.
- Initial JS ≤ 250 KB gzip (size-limit).

## Layout (W0-05)

Same structure as `customer/` (see its CLAUDE.md): `src/main.tsx`, `src/app/`, `src/shell/`, `src/routes/`, `e2e/smoke.spec.ts`.

- `src/routes/index.tsx`: `/` is B-03 Overview (`src/overview/`, sample data for now behind `queries.ts`); the D-84 redirect to `/orders` is gone. `src/routes/overview.tsx` replaces `/overview` with `/` (query kept), and `NavLink` points the Overview entry at `/` (exact match).
- `src/shell/Shell.tsx`: a `tz-side` sidebar (≥1024px, "Business" tag, Grow/Shop groups) and `tz-bnav` bottom tabs (<1024px: Overview, Orders, Sell, Stock, More). Below 1024px the content area gets `data-density="touch"` (44px controls) via `useNarrow()`.
- `src/shell/nav.ts`: each item says whether it is a tab, a sidebar entry or both. "More" lists the sidebar-only sections on phones.

## Conventions

- To build a section: add your route file(s) under `src/routes/<area>/` (e.g. `src/routes/orders/index.tsx`, `src/routes/orders/$orderId.tsx`). A static route outranks `/$section`, so your page replaces the placeholder. Edit nothing in `nav.ts` or `$section.tsx` (`src/app/routing.test.ts` proves the precedence). W3-03 moves the landing redirect when it builds Overview.
- Tokens only; i18n keys in `shared/i18n/messages/business/`. Dev port 5302, Host `business.todayz.localhost`. Initial JS budget 250 KB gzip. Hidden sourcemaps, checked by `build` as in `customer/`. The offline POS (W1+) adds its own service worker strategy on top of the precache set up here.
- Smoke screenshots go to the git-ignored `test-results/screenshots/`; `TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones.

## Status

W0-05: shell, routing, i18n, PWA manifest, telemetry stub, size budget and smoke tests in place. Screens are placeholders.
