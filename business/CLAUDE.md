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
- `src/shell/Shell.tsx`: a `tz-side` sidebar (≥1024px, "Business" tag, Grow/Shop groups) and `tz-bnav` bottom tabs (<1024px: Overview, Orders, Sell, Stock, More). Below 1024px the content area gets `data-density="touch"` (44px controls) via `useNarrow()`. The sidebar shows neutral `.tz-count` badges (`useNavCounts`): Orders counts the live New list (`listKey(sid, 'new')`, refreshed by the store stream), Stock reads the Overview's `today` query. The Stock badge and the shop setup card at the sidebar foot (`overview/SetupCard.tsx`, hidden at 100%) stay hidden while the Overview runs on sample data (`SAMPLE_DATA` in `overview/sample.ts`), so made-up numbers never sit next to real orders; below 1024px that card sits under the Overview KPIs instead and the Overview top bar starts with the mark and store name.
- `src/shell/nav.ts`: each item says whether it is a tab, a sidebar entry or both. "More" lists the sidebar-only sections on phones.

## Conventions

- To build a section: add your route file(s) under `src/routes/<area>/` (e.g. `src/routes/orders/index.tsx`, `src/routes/orders/$orderId.tsx`). A static route outranks `/$section`, so your page replaces the placeholder. Edit nothing in `nav.ts` or `$section.tsx` (`src/app/routing.test.ts` proves the precedence). Overview (B-03) is the landing page at `/`.
- Tokens only; i18n keys in `shared/i18n/messages/business/`. Dev port 5302, Host `business.todayz.localhost`. Initial JS budget 250 KB gzip. Hidden sourcemaps, checked by `build` as in `customer/`. The offline POS (W1+) adds its own service worker strategy on top of the precache set up here.
- Smoke screenshots go to the git-ignored `test-results/screenshots/`; `TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones.

## Auth and gate (W1-08)

- The app is for signed-in staff. `src/shell/Gate.tsx` (root route) shows a skeleton while the refresh cookie is tried, sends a signed-out visitor to `/sign-in?next=<where they were going>`, and renders the sidebar `Shell` only when signed in. `/sign-in` is the only open route.
- `src/app/`: `session.ts` (`@todayz/auth` session for app `business`; token in memory only), `env.ts`, `labels.ts`, `next.ts` (`?next=` must be a path on this app), `update-context.tsx` (`useUpdateController`; B-05 Sell calls `useHoldUpdates(controller, saleOpen)` so the update toast waits), `shop.ts` (picker data and the remembered store).
- Routes: `/sign-in` (B-01), `/choose-shop` (picker: role shown, last store remembered, one store skips itself), `/invite/$token` (the invitation page, W2-10 finish). The picker uses the generated `listBusinesses` (`brandName`, `role`, `stores`); while the list fails it offers "Continue to overview".
- Sign out is a button at the foot of the sidebar (and of the page below 1024px); it clears the query cache.
- `src/test/render.tsx` renders `<App>` signed in by default (`signedIn: false` for the gate) with a fake `fetch`.
- Stack journeys: `e2e/sign-in.stack.spec.ts` via `playwright.stack.config.ts` against `make dev`: `pnpm --filter @todayz/business exec playwright test -c playwright.stack.config.ts`. The smoke spec answers the session refresh itself.

## Status

W0-05: shell, routing, i18n, PWA manifest, telemetry stub, size budget and smoke tests in place. Screens are placeholders.

W1-08: sign-in gate, B-01 sign-in and picker (picker data stubbed), offline banner, update toast, error boundary and SW precache check. B-03 Overview works behind it.

Offline first load: the gate shows `SessionPending` (offline notice and "Try again") while the session is `unknown` and the API cannot be reached.

W2-10 (weekend slice): B-04 Orders in `src/orders/` with routes `src/routes/orders/index.tsx` and `$orderId.tsx`.

- Tabs New, Accepted, Ready, Done, Missed and an empty Asks slot (B-20 comes with W3-16). New cards count down from the server's `serverNow`, show stock per line, Accept and Reject (D-60 reasons). While a line is short (`isShort`), Accept is disabled on the card and the detail page. Accept part (W2-02 finish, D-28) opens `AcceptPartSheet` (a stepper per line, prefilled with `canGive`, Send disabled at all-zero or nothing lowered) and sends every line to `/accept`; the hint is `orders.card.partHint`, or `orders.card.shortHint` when the stock covers nothing (`canAcceptPart` false, Accept part disabled too). An `awaiting_customer` order sits on the Accepted tab; its detail shows the changed quantities and a waiting note. Dispatch, delivery, returns, QR scan, B-02, B-13 and B-14 are deferred.
- `queries.ts`: list and order queries, the store stream (`@todayz/realtime`, polling fallback with a banner), `useOrderAction` (one Idempotency-Key per attempt, reused only after a network failure).
- `sound.ts` and `alerts.ts`: a Web Audio chime that repeats until the order is opened, accepted or rejected; "Allow sound" unlocks it. Orders already seen on the server at first load do not alert.
- `store.ts`: the current shop and the role come from the same `listBusinesses` answer the picker uses. Cancel on an accepted order is manager and above only (D-65).
- `CodeEntry.tsx`: the pickup code lives in component state only (never in a query key, URL, storage or log), is cleared after every answer, and shows tries left on `code_invalid` and the lock on `code_locked`.
- Stack journey: `e2e/orders.stack.spec.ts` (customer by API, shop in the browser). Passes on the seed (`docs/qa/stack-2026-10-24/`).

W2-12: B-07 Stock (`src/stock/`), B-08 Offers and banners (`src/offers/`), B-11 Reviews (`src/reviews/`) and B-10 Credits (`src/credits/`), each with its route under `src/routes/<area>/`. Stock, offers and reviews use the generated client; `stock/api.ts` (create product, import) and `credits/api.ts` are adapters marked `TODO(merge)`. Reviews, Credits and Offers still need `nav.ts` entries. Report: `docs/qa/W2-12/REPORT.md`.

W2-10 finish: B-02 onboarding (`src/onboarding/`, `/onboarding`), B-13 stores and staff (`src/stores/`, `/stores`, `/invite/$token`), B-14 settings (`src/settings/`, `/settings`) and the QR standee page (`src/standee/`, `/standee`). All use the generated client except the standee PDF (`credits/api.ts` adapter, `TODO(merge)` until `getQrStandee` is generated). Report: `docs/qa/W2-10-finish/REPORT.md`.

- Edits send `If-Match` (read first when the ETag is unknown; a 412 refetches). Create, invite, submit and document calls carry an `Idempotency-Key`. Roles are in `src/stores/perm.ts`; the server is the gate (D-200 to D-205).
- Documents: the file is checked in the browser, registered, then PUT to the presigned URL with only `Content-Type`. A failed PUT leaves a `pending` document (no confirm route in 04).

W2-11: B-05 Sell, the counter and offline POS. Modules: `src/pos/` (cart, checkout, money, links, history, the `PosApi` adapter on the generated client, `PosProvider` runtime, `Counter`, `SellPage`, the link, discount, add-product and sales sheets, `QrCode`), `src/sync/` (the outbox engine and the raw-IndexedDB `PosStorage`, D-170), `src/scanner/` (keyboard wedge and camera) and `src/receipt/`; route `src/routes/sell/index.tsx`; copy in `shared/i18n/messages/business/pos.en.json`. Decisions D-170 to D-179. Report: `docs/qa/W2-11/REPORT.md`.

- Every sale is saved to the device first (client ULID, device sequence), then sent by itself when online or by `syncStore` otherwise; the server's `(store_id, client_id)` idempotency makes a repeat safe. Refused sales stay in the list for a person to look at.
- Tests build the screen with a fake `PosApi` and `memoryStorage` (`PosProvider` takes `api`, `storage`, `now`); `user-event` is not installed, so `Counter.test.tsx` uses a small `fireEvent` shim.
- Screenshots: `e2e/sell.spec.ts` fakes `/v1/**` and writes to `docs/qa/W2-11/` with `TZ_QA_DIR=docs/qa/W2-11`. The preview config needs placeholder `VITE_TURNSTILE_SITE_KEY` and `VITE_API_URL` set in the shell (the bundle is already built with `build:e2e`), and the app only tries the session refresh when `localStorage` has `todayz.hasSession.business`.

W2-16: B-05 store sales. The Sell screen's "Store sales" button opens `src/pos/StoreSalesSheet.tsx`, the store's sales from the server (`PosApi.listSales` on the generated `listPosSales`, 20 a page, "Load more" on the cursor): time, total, units, payment, "Rung up offline", and the link (linked to a name, link pending, no customer, or "Customer link failed" with the reason, D-221). Online only; the device's outbox stays in `SalesSheet`. Report: `docs/qa/W2-16-pos-sales-list/REPORT.md`.
