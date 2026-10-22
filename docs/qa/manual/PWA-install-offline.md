# PWA Install, offline and updates

The three apps are installable PWAs. This file checks the customer install prompt rule (after the 2nd order, not before), the offline banner and cached data, the update toast, the forced update from `min_client_version_*`, and the service worker's cache rules. The offline POS outbox is in MQ-03 and STOCK.

- **Covers:** R-17.4; 06 customer and business globals; SC-CU, SC-BU.
- **Screens:** C-03, C-05, C-07, C-12, C-20; B-03, B-05; A-02.
- **Devices:** P360 Android Chrome (real phone for M.01 to M.04 if possible), P390 iOS Safari, D1280.
- **Time:** about 1 hour.

## Preconditions

1. `make reset`. A fresh browser profile for each app (DevTools → Application → Clear site data).
2. C18 has 2 past collected orders; C19 has 1; C10 has none in this session's history if you use a new number `+91 90000 00001` (C01).

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PWA-M.01 ★ | As C01 (new) browse, sign in and place and collect one order. | No install prompt at any point. | 06 global SC-CU | | |
| PWA-M.02 ★ | Place and collect a second order as C01. Return to Home. | The app's own install prompt shows after the 2nd order (not the browser's mini-infobar earlier). It shows on C-12 right after the second order is placed, and only once. | 06 global | ▢ | |
| PWA-M.03 | Tap Install. Open the installed app from the home screen. | Opens standalone (no address bar), at Home, signed in, with the TodayZ icon and name. | SC-CU | ▢ | |
| PWA-M.04 | On iOS Safari (P390) do the same as C19 after its 2nd order. | iOS has no install event: the app shows "Add to Home Screen" instructions instead, or nothing; record which. | SC-CU | | |
| PWA-M.05 ★ | With Home and a search for "atta" loaded once, set DevTools → Offline. Reload Home, then open the "atta" search. | An offline banner shows. Cached shops and results show, marked "may be out of date". No browser dinosaur page. | 06 global | ▢ | |
| PWA-M.06 | Offline, open a shop you never opened and tap Place order on a cart. | A clear offline message for the uncached shop; Place order is disabled or refused with a retry. No order is queued silently. | 06 global | | |
| PWA-M.07 | Go back online. | The banner goes away; data refreshes without a manual reload. | 06 global | | |
| PWA-M.08 | Offline while C-12 tracks an accepted order. K22 marks it ready. Go online. | C-12 shows "Ready" within a few seconds of reconnecting (SSE replay). | BK-35 | ▢ | |
| PWA-M.09 ★ | Keep the customer app open. Rebuild and restart the customer app (`infra/local/proc.sh restart customer`, or edit any string and let Vite rebuild in prod-preview mode). | An update toast "A new version is ready" (record the copy) with a reload action. The app does not reload by itself while a cart or checkout is open. | 06 global | ▢ | |
| PWA-M.10 | Tap the toast's reload. | The new version loads; the cart is kept. | 06 global | | |
| PWA-M.11 | `set min_client_version_customer = 99.0.0` (SA48 or AD47). Reload the customer app. | "Update available", then the app reloads. If the running build is still lower (it is), the message stays and blocks ordering; record the behaviour. Restore `0.0.0`. | `R-17.4` | ▢ | |
| PWA-M.12 | Repeat M.11 with `min_client_version_business` and `min_client_version_admin`. | Same "Update available" on the business app and the admin console. Restore both to `0.0.0`. | `R-17.4` | | |
| PWA-M.13 | DevTools → Application → Cache Storage on the customer app after browsing. Look for API responses with personal data (`/v1/me`, `/v1/me/orders`, wallet). | Only static assets and public discover data are cached. No `/v1/me…` response in any cache. | SC-CU `R-16.1` | | |
| PWA-M.14 | Sign out of the customer app. Go offline. Reopen it. | No previous user's orders, wallet or addresses visible. | `R-16.1` | | |
| PWA-M.15 | Install the business app on D1280 (Chrome install icon). Go offline. Open Sell. | The POS opens from cache and sells offline (MQ-03 covers the outbox). The sync indicator shows offline. | SC-BU B-05 | ▢ | |

## Cleanup

1. `restore defaults`: `min_client_version_customer`, `min_client_version_business`, `min_client_version_admin` all `0.0.0`.
2. Uninstall the installed apps. Clear site data. `make reset`.
