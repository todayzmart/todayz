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

- `src/routes/index.tsx`: `/` is B-03 Overview (`src/overview/`, reading `GET {S}/overview` and `GET {S}/insights` through `queries.ts`); the D-84 redirect to `/orders` is gone. `src/routes/overview.tsx` replaces `/overview` with `/` (query kept), and `NavLink` points the Overview entry at `/` (exact match).
- `src/shell/Shell.tsx`: a `tz-side` sidebar (≥1024px, "Business" tag, Grow/Shop groups; dark green since D-274, see below) and `tz-bnav` bottom tabs (<1024px: Overview, Orders, Sell, Stock, More). Below 1024px the content area gets `data-density="touch"` (44px controls) via `useNarrow()`. The sidebar shows neutral `.tz-count` badges (`useNavCounts`): Orders counts the live New list (`listKey(sid, 'new')`, refreshed by the store stream), Stock reads the Overview's `today` query (`sellOutSoon`). The Stock badge and the shop setup card at the sidebar foot (`overview/SetupCard.tsx`, hidden at 100%) show for owners and managers only; a cashier gets neither, and no overview call is made for them. Below 1024px that card sits under the Overview KPIs instead and the Overview top bar starts with the mark and store name.
- `src/shell/nav.ts`: each item says whether it is a tab, a sidebar entry or both. "More" lists the sidebar-only sections on phones.
- Bright skin (D-261): the shell root sets `data-skin="bright"`, so the page is grey `page` and cards are white with `shadow-card`. The rules live in `design/source/components/bundle.css`, not in this folder. Text directly on the page is `ink-2` at least (`tz-page-sub` for a subline), because `ink-3` fails contrast on the grey. `e2e/bright.shots.spec.ts` takes the phone and tablet shots of the main screens with `TZ_QA_DIR=…`, and runs axe when `TZ_AXE` points at `axe.min.js`. Below 640px (`usePhone`) the stock table is a list of rows, so nothing scrolls sideways on a phone.

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

W2-16, then D-268: Counter sales. B-04 Orders has two tabs (`orders.view.*`, a `Segmented` in `OrdersPage`): Online orders and Counter sales (`/orders?view=sales`, search params parsed in `src/orders/salesQueries.ts`). The Sell screen's "View sales" link opens it; `StoreSalesSheet` is gone, and the device's outbox stays in `SalesSheet` and shows on Counter sales as "Not synced yet" (through `usePosMaybe`).

- `src/orders/CounterSales.tsx`: IST range chips, filters (payment, status, link, offline, cashier), the stats (`getPosSalesSummary`), a `DataTable` on desktop and cards below 640px, "Load more" on the cursor, Export CSV (`exportPosSales`, downloaded as a Blob). A cashier gets the list only: no summary call, no cashier filter, no export.
- `src/orders/SaleDetail.tsx`: the bill panel (`?sale=<id>`, a side panel or a sheet): lines, money, cashier, device, times, link and why it failed, the reversal. Reverse is owner or manager inside the window (from `reversibleUntil` and the server's clock), with a reason and the key `reverse-<saleId>` (the device's own `engine.reverse` when the sale is in its records). The receipt shows only when this device has its token.
- `PosProvider` now always renders its context provider, so a fallback that is the same element as its children (Counter sales) keeps its state when the device store opens.
- The online-order chime does not play while Counter sales is open: the alert hook lives in the Online orders view.
- Tests: `src/orders/counterSales.test.tsx`; `e2e/sell.spec.ts` writes `counter-sales-*.png` with `TZ_QA_DIR=docs/qa/D-268-counter-sales` (repo-root relative). Report: `docs/qa/D-268-counter-sales/REPORT.md`.

D-270: no item goes below zero at the counter. `cart.ts` caps a line at the item's available (lowering always works); asking for more, or charging a bill a sync made short (`firstShortLine`), opens `src/pos/UpdateStockDialog.tsx` (on hand, blocked for online orders, available, "Counted now", the "includes the N units set aside" box only when N > 0). Save records a `count_correction` through the outbox and fits the line. The engine applies sales and counts to the stored catalogue at once, gives a refused sale's units back, and on an online `409 insufficient_stock` keeps the sale for sync, adopts the server's number and sets `short` (the dialog opens with "This sale went through"). Tiles and search results show "N blocked for online orders". `e2e/sell.spec.ts` writes `pos-stock-*.png` with `TZ_QA_DIR=docs/qa/D-270-pos-stock-cap`. Report: `docs/qa/D-270-pos-stock-cap/REPORT.md`.

Business i18n (25 Oct): all business keys in hi, kn, te and ta, approved by the founder without a native-speaker review (`docs/qa/business-i18n/REVIEW.md`). B-14 Settings has a Language section for every role (`src/settings/LanguageSection.tsx`); `App` loads one compiled catalogue per language on demand (`src/app/i18n.ts` `loadMessages`, `src/app/locale.ts` `useLocale`) and the choice is kept on the device (`todayz.lang`). `src/settings/language.test.tsx` fails when an English key has no reviewed, current hi/kn/te/ta translation, so new English keys need hi/kn/te/ta translations in the same change. Server-side `preferredLang` is not wired yet.

W3-03: Demand insights v2 and the owner screens. Report: `docs/qa/W3-03/REPORT.md`.

- B-03 Overview (`src/overview/`): the KPI strip, sales by hour, new orders, the Demand insights card ("Not useful" dismisses with `POST {S}/insights/dismiss`) and an empty `<CoachSlot/>` for W3-11. `overview/StoreGate.tsx` is the store and role gate for Overview, Analytics and Customers; a cashier gets a note, and no call is made.
- B-12 Analytics (`src/analytics/`, `src/routes/analytics/`, `/analytics?tab=sales|insights&kind=`) and B-09 Customers (`src/customers/`, `src/routes/customers/`, `/customers?segment=all|top|slipping`).
- Insight actions are deep links that open a prefilled screen (`/stock?restock=…`, `/offers?offer=…`, `/stock?add=…`). They never change anything themselves.
- Test fakes: `src/test/insightsData.ts` (plain data, shared with the Playwright specs) and `src/test/insightsApi.ts` (handlers). The owner shell now calls `GET /overview`, so every spec that signs in an owner must answer it. `e2e/insights.spec.ts` writes the screenshots with `TZ_QA_DIR=docs/qa/W3-03`.

W3-04: B-06 Voice update in `src/voice/`, route `src/routes/voice/index.tsx` (`/voice`), on the generated `gen/voice/voice` client. The way in is `VoiceLink` on B-07 Stock, shown only while `getVoiceStockAvailability` answers (404 = off, and the screen shows the off state). Hold to talk (pointer or Space) records up to the server's `maxSeconds` with `recorder.ts` (MediaRecorder + a live waveform); the draft's lines (`lines.ts`: states, role checks, the confirm body) are confirmed with one Idempotency-Key per attempt; the server is the per-line gate (`ask_manager`). A cashier's confirm applies their lines and leaves the draft `ready` with the rest; `/voice?draft=<id>` reopens it, showing settled lines apart from the open ones (`splitDraft`). `e2e/voice.spec.ts` fakes the API and the microphone and writes to `docs/qa/W3-04/` with `TZ_QA_DIR=docs/qa/W3-04`. Report: `docs/qa/W3-04/REPORT.md`.

W3-08: B-15 Help (`src/help/`, routes `/help`, `/help/new`, `/help/$ticketId`). Every role gets the guide, the FAQs and how to reach support; owners and managers also get the Tickets tab (open with a category, store, order and up to 5 photos or PDFs; the thread with "TodayZ support"; reply; a closed ticket offers a new one). Files are checked in the browser, registered, then PUT with only `Content-Type`. The support phone shows only when `VITE_SUPPORT_PHONE` is set (`help/contact.ts`). Sidebar entry "Help" (`LifeBuoy`, Shop group, under More on phones; D-226). Playwright: `e2e/help.spec.ts`. Report: `docs/qa/W3-08/REPORT.md`.

D-274 (27 Oct): seller auto-onboarding and the dark green sidebar.

- B-02 (`src/onboarding/`, `/onboarding`) is a step flow with a dark green step rail (`onboarding.module.css`): business details, store (address and map pin), hours, category, photos (optional), payout details (optional), go live. `steps.ts` decides which steps are done and which open next. On a phone the rail is a dark header with the steps as a scrolling row; from 1024px it is a full-height column. The rail's "Go to dashboard" link shows only once the shop is live: a new signup has no shop to go to. The rail counts all seven steps ("3 of 7 steps done"), optional ones included, so it matches "Step n of 7". The last step submits, and the business and its complete stores go live at once; nothing waits for an approval. The page shows the calm note "Payouts start after our team checks your shop." until the shop is checked.
- `src/shell/Gate.tsx` renders `/onboarding` bare (`BARE`), without the shell, so B-02 draws its own frame. Its root sets `data-skin="bright"` itself, since the shell that normally sets it is not there (green primary buttons, no underlined button links).
- The shell sidebar uses the `rail` tokens (`rail`, `rail-raised`, `rail-line`, `on-rail`, `on-rail-2`, `rail-accent` in `design/source/tokens.json`; 12.6, 8.3 and 8.2:1 on `rail`). The founder asked for it over the white sidebar; the content area stays white. The focus ring on the rail is `rail-accent`. The shop setup card at the sidebar foot stays a white card with an ink link. `src/shell/sidebar.test.ts` checks the sidebar reads only rail tokens.
- B-10 Credits (`PayoutsCard`, `StatementsCard`) shows an info banner that payouts wait for the shop check while the business is verified and `checkStatus` is `unchecked`, and a statement's payout line reads `awaiting_check`. Tests: `src/onboarding/onboarding.test.tsx`, `src/shell/sidebar.test.ts`; screenshots from the built app with a faked API in `e2e/onboarding.shots.spec.ts` (`TZ_QA_DIR=docs/qa/seller-auto-onboarding` refreshes the committed ones).
