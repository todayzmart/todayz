# customer/

Customer PWA (app.todayz.in; locally https://app.todayz.localhost). Phone-first. React 19, Vite, TanStack Router/Query.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §4.1–4.2; 06-screens.md (C-xx); 01-product-rules.md.

## Run

- `make dev`, then open https://app.todayz.localhost
- `pnpm --filter @todayz/customer test` / `build`

## Rules for this folder

- Import only from `shared/*` and `design/`, never from a sibling app.
- Use tokens and `tz-` classes or `@todayz/ui`; no raw hex (Stylelint fails it). Light theme only.
- All user-facing text through i18n keys in `shared/i18n/messages/customer/<area>.en.json`.
- Own only your area's `src/routes/<area>/`; `routeTree.gen.ts` is generated and git-ignored.
- Initial JS ≤ 170 KB gzip (size-limit).

## Layout (W0-05)

- `src/main.tsx`: tokens CSS, holds back the browser install prompt (`src/pwa/install.ts`), locale, router, telemetry init (no-op locally; labels use the matched route id), render; registers the SW in production only.
- `src/app/`: `App.tsx` (IntlProvider → QueryClientProvider → RouterProvider), `router.ts` (QueryClient + router factory, used by tests too), `i18n.ts` (precompiled catalogues; `?locale=pseudo` loads en-XA in every mode except `production`).
- `src/pwa/install.ts`: `beforeinstallprompt` is prevented and stashed; `getDeferredPrompt()` hands it to the install banner, which shows after the 2nd order (06-screens.md).
- `src/shell/`: `Shell` (skip link, `main#main`, `tz-bnav` bottom nav), `nav.ts` (tabs + `PLACEHOLDER_SECTIONS`), `Icon`, `Placeholder` (SoonCard), `NotFound`, `shell.module.css`.
- `src/routes/`: `__root.tsx`, `index.tsx`, `$section.tsx` (placeholder for nav sections not built yet; unknown sections 404).
- `e2e/smoke.spec.ts`: Playwright against `vite preview` of a `--mode e2e` build on a random port. Screenshots go to the git-ignored `test-results/screenshots/`; `TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones.

## Conventions

- To build a section: add your route file(s) under `src/routes/<area>/` (e.g. `src/routes/search/index.tsx`). A static route outranks `/$section`, so `/search` renders your page and the placeholder stops being used. Edit nothing in `nav.ts` or `$section.tsx` (`src/app/routing.test.ts` proves the precedence). Keep the nav item; it is how users reach the section.
- Production builds use `sourcemap: 'hidden'`; `build` fails if shipped JS or CSS carries a `sourceMappingURL` (`@todayz/config/dist-check.mjs`).
- Icons: `lucide-react` through `shell/Icon` (stroke 1.5, aria-hidden). Text: `intl.formatMessage({ id })`, sentence case, no exclamation marks.
- CSS modules for layout, `tz-` classes for components, only `var(--token)` values (`src/test/tokens.test.ts` fails on an undeclared var).
- ICU messages are precompiled; the runtime parser is aliased away (`intlNoParserAlias`). Run `make gen` after editing a catalogue.
- Dev: port 5301 (strictPort), Host `app.todayz.localhost`, HMR over wss:443 via Caddy; `TODAYZ_HMR=direct` for http://127.0.0.1:5301.

## Auth, shell and onboarding (W1-08)

- `src/app/`: `session.ts` (the `@todayz/auth` session for app `customer`; token in memory only), `env.ts` (`VITE_API_URL`, `VITE_TURNSTILE_SITE_KEY`, defined by `vite.config.ts` through `@todayz/auth/build`; a production build fails without a real site key and an https API URL), `locale.ts` (language choice: stores it, loads the catalogue, PATCHes `preferredLang` when signed in), `place.ts` (C-02 location, this device only), `me.ts` (`patchMe`: PATCH /v1/me with If-Match), `labels.ts` (strings for the shared auth pieces), `next.ts` (`?next=` must be a same-app path; wraps `safeNext` from `@todayz/auth`), `update-context.tsx` (`useUpdateController`; C-11 checkout calls `useHoldUpdates(controller, open)` so the update toast waits).
- Routes: `/sign-in` (C-10), `/language` (C-01, `/` redirects first-run here), `/location` (C-02: GPS after an explainer, PIN code; locality search is `TODO(W1-06)`), `/account`.
- `src/test/render.tsx` renders `<App>` with a fake `fetch`; tests of routes live in `src/test/`, not in `src/routes/` (every file there is a route).
- `build` and `build:e2e` run `precache-check` (SC-GS-09): every chunk must be in the service worker's manifest.
- Stack journeys: `e2e/sign-in.stack.spec.ts` runs from `playwright.stack.config.ts` against `make dev`: `pnpm --filter @todayz/customer exec playwright test -c playwright.stack.config.ts`.

## Status

W0-05: shell, routing, i18n, PWA manifest, telemetry stub, size budget and smoke tests in place. Screens are placeholders.

W1-08: shell with sign-in (C-10), language (C-01), location (C-02), account, offline banner, update toast, error boundary and SW precache check. Account is real; Search, Orders and Wallet are still placeholders.

Offline first load: `/account` shows `SessionPending` (offline notice and "Try again") while the session is `unknown` and the API cannot be reached. Sign out clears the whole query cache.

W2-08 (weekend slice): C-03 Home (`routes/index.tsx`, `src/home/`), C-05 Search (`src/search/`), C-07 shop page `/s/$slug` (`src/store/`) and C-08 product detail `/s/$slug/i/$itemId` (`src/product/`). The cart's store id is the discover `storeId` (ULID); the slug is `CartStore.slug`. Freshness shows only when `confirmedAt` is set (the server nulls it past `stock_stale_after_hours`). The StoreMap card on Home (`home/StoreMapCard.tsx`, lazy; tiles from `home/tiles.ts`, one `.pmtiles` per city), the full map C-04 `/map` (`src/map/`) and the category page C-06 `/c/$slug` (`src/category/`) are built; C-04 has no "Search this area" (StoreMap has no map-moved event) and C-06 lists shops only. C-05 shows "None of the shops has confirmed yet." (D-190) when every listed in-stock result has `confirmedAt` null, with the next radius up (or "Search again later" at 20 km); it never asks the shops itself, and the ask flow stays the `AskNearbySlot` TODO(W3). Item and product images use `IMAGE_BASE_URL` (`app/env.ts`). Stack journey: `e2e/discover.stack.spec.ts`.

W2-09 (weekend slice, pickup only): C-09 Cart `/cart` (`src/cart/`, IndexedDB write-through, same exported API), C-11 Checkout `/checkout` (`src/checkout/`), C-12 Tracking `/orders/$orderId` and C-13 Orders `/orders` (`src/orders/`; SSE through `@todayz/realtime` with a polling fallback). The pickup code lives only in the order query cache and `PickupCode`, never in a URL, storage or log. Every discover screen (Home, Search, shop, product) shows `store/CartBarLink` when the cart has items. Credits at checkout (`cart/credits.ts`, `CreditsControl`) and the partial confirm on C-12 (`orders/PartialConfirm.tsx`) are built. Delivery, the In store tab, call shop and the QR code are deferred (see `docs/qa/W2-08-09-finish/REPORT.md` for why). Stack journey: `e2e/order.stack.spec.ts`.

W2-14 (basket search, non-AI): C-22 `/baskets`, `/baskets/device`, `/baskets/$basketId` (`src/basket/`). A saved basket autosaves with If-Match (a 412 reloads it); the signed-out basket lives on this device (`basket/device.ts`) and is offered for saving after sign-in. "Fill cart" uses `basket/fill.ts`; a split remembers the second shop, and C-12 shows `NextFromSplit` for it. `AddToBasket` sits on `store/ItemCard` (compact) and the product page; Home and Account link to `/baskets`. Stack journey: `e2e/basket.stack.spec.ts`; report `docs/qa/W2-14/REPORT.md`.

W2-15 (customer delivery): C-09 Pickup/Delivery switch with the shop's terms (`cart/Delivery.tsx`, `fulfilment.ts`, `deliveryRules.ts`), C-11 address picker, new-address form with pin, `delivery_closed` banner with "Switch to pickup" (`src/address/`), C-12 delivery state, C-18 Addresses card on Account (`address/SavedAddresses.tsx`). D-220: C-12 shows "Call shop" (`tel:`) only when the server sends `store.phone`, which it does only on the customer's GET of an open order. Journeys: `e2e/journeys/specs/14-e2e-05-delivery.spec.ts`; report `docs/qa/W2-15-customer-delivery/REPORT.md`.
