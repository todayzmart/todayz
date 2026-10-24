# W2-08 / W2-09 finish: QA report

Customer PWA, pickup flow. Branch `claude/wp-w2-08-09-finish-1d3b0b`, merged with `main` 6787251 (offers, banners, reviews, referrals, business screens) on 2026-10-24. Everything ran locally; nothing touched a cloud account. No migrations and no decision-log entries (D-190 to D-199 unused).

## What was built

W2-08 (discovery):
- **Images:** `IMAGE_BASE_URL` (`app/env.ts`); item and product images use `<base>/<imageKey>` (`home/present.ts` `imageUrl`).
- **Product filter:** `searchDiscover` takes an optional `productId`. "Other nearby shops" on the product page sends it instead of matching names client-side.
- **Shop header:** shows the category name (`categoryName` and `categorySlug` added to `DiscoverStorePage`).
- **StoreMap card on Home:** lazy chunk, non-interactive, pins and radius chips, "Open full map". Tiles come from `home/tiles.ts` (one `.pmtiles` per city under `TILES_URL`); without tiles the static renderer is used.
- **C-04 full map `/map`:** map and list switch, tapped pin opens a sheet (distance, locality, open state, Directions, View shop), truncated-pins banner, no-place, loading and error states.
- **C-06 category page `/c/$slug`:** shops in the category with radius chips, load more, empty state with "widen", not-found state. Home links to it from the category filter note.

W2-09 (transactions):
- **Partial confirm on C-12** (`orders/PartialConfirm.tsx`): changed lines, new total, server-clock countdown, take or cancel.
- **Credits at checkout** (`cart/credits.ts`, `CreditsControl`): switch and amount slider from the quote's `creditsAllowed` and `creditsLimit`. A probe quote with a very large `creditsToUse` reads the limit, so quote calls double while credits are on. No numbers are hard-coded in copy.

## Evidence

Run after merging `main`.

| Check | Result |
|---|---|
| `make gen` | ok |
| `pnpm -C customer typecheck` | clean |
| `pnpm -C customer lint`, `lint:css` | clean, 0 warnings |
| `pnpm -C customer test` | 16 files, 116 tests passed |
| `pnpm -C customer build:e2e` | ok; precache-check: all 98 chunks in the manifest; dist-check: no sourceMappingURL |
| `pnpm -C customer size` | initial JS 137.36 kB gzip (limit 170); CSS 9.68 kB (limit 20) |
| Backend (before the merge, by the subagent) | `go vet`, golangci-lint clean; `make check-contract-lint`, `make contract-smoke` ok; `go test -race ./internal/search/` ok |

Screenshots at 390 px (2x) in `docs/qa/W2-08-09-finish/screens/`: `home-map-card.png`, `full-map-sheet.png`, `category-page.png`. They use faked discover responses and no tile file, so the map shows the static renderer (radius ring, pins, no streets). Partial confirm and credits are covered by tests only (30 cases in `transactions.test.tsx`); no screenshots.

## Adapters and the merge

There was no `TODO(merge)` adapter in the customer areas, so nothing was swapped to the generated client. The customer screens that use offers (C-03 offers near you), reviews (C-07) and referrals (C-16) are not part of this slice and are still to be built; their endpoints are in the contract (`listNearbyOffers`, `listDiscoverStoreReviews`, `getMyReferrals`). The wallet adapter stays until W2-01 merges.

## Open items (skipped, and why)

- **Delivery:** the backend answers 409 `store_not_bookable` and there is no addresses API (BK-23 to BK-25).
- **In store tab:** needs `/v1/me/purchases` (W2-04) and the wallet, not in the contract.
- **QR of the pickup code:** needs a QR library; none is in the workspace and no dependency was added.
- **Call shop:** no customer-facing shop phone in any response.
- **C-04 "Search this area":** `StoreMap` has no map-moved event (`shared/ui`, lead-owned).
- **C-06 products:** the search needs `q` and has no category filter, so the page lists shops only.

## Questions

1. ~~C-05 none confirmed~~ answered in D-190 and built (below).
2. Shop phone: may a customer see it, and from which response? (Unblocks "Call shop" on C-12 and the shop page.)
3. QR code: OK to add a small QR library to `customer/`?
4. Production tile layout: one India file or per-city files? `home/tiles.ts` assumes per-city names from `infra/local/tiles.sh`.
5. Credits copy ("30 credits on ₹100+") is not shown because no public settings endpoint exists. Add one, or keep it out?
6. `IMAGE_BASE_URL` assumes `<base>/<imageKey>`. Confirm the production key to URL form.

## Not verified

- No live MapLibre rendering with real tiles, and no stack e2e (barred in this session).
- The partial-accept backend is not built, so partial confirm is verified against mocks only.
- Pin names beyond the first 20 loaded shops show a generic "Shop" (pins carry no name).
- Go tests: `TestSearchAtScale` failed once on p95 under heavy load and passed on rerun; `go test -race ./cmd/todayz/...` timed out in `TestOrderCodeCollisionRedraws` under load. Neither was rerun after the merge.

## Files outside the owned paths

- Backend (small, for `productId` and the category name): `backend/api/paths/discover.yaml`, `backend/api/schemas/discover.yaml`, `backend/db/queries/search/search.sql`, `backend/internal/search/{search_run,shop,search_db_test}.go`. These landed inside commit 1be6880, whose message says only "Tests: no non-null assertions..." (a `git commit -a` swept them in; history left as is).
- `customer/vite.config.ts`: a `stripAssetSourceMapComments` plugin (the MapLibre worker ships a `sourceMappingURL` that dist-check rejects) and `mjs` in the Workbox precache globs. Precache grew to about 2.7 MiB; initial JS is unaffected because the map is lazy. Sibling apps that use `@todayz/ui/map` need the same.
- `shared/i18n/messages/customer/{home,orders,cart}.en.json`: new keys.
- New customer folders `src/map` and `src/category`, plus `customer/CLAUDE.md` status lines.

## Follow-up: C-05 "none confirmed" (D-190)

Built on `a0a4dae` + merge of `main`. In `search/SearchPage.tsx` a `NoneConfirmed` banner shows when the results are loaded, there is no further page, and no in-stock item in any result has `confirmedAt` (the server nulls it past `stock_stale_after_hours`). It says "None of the shops has confirmed yet." and offers "Search within {next} km" (the next radius chip up, which only changes the search). At 20 km there is no wider radius: the copy says so and the button is "Search again later", which re-runs the search. The shops are never asked from here; "Ask shops nearby" stays the customer's own action in `AskNearbySlot`, which is still an empty slot because the asks contract does not exist yet.

Reading used: the ask flow is not built, so "the ask has ended" cannot be checked; the state is the stale-results case of 01 §11.2 / ASK-12. The zero-results state keeps its own copy. Tests: 3 new in `search.test.tsx` (next radius sent in the request and no ask call; 20 km; hidden while any shop has confirmed). Run: customer typecheck, lint, vitest (119 passed), `shared/i18n` test (197 passed), `build:e2e`, size 137.46 kB. Screenshot: `screens/search-none-confirmed.png` (390 px). One unrelated flake seen once under load: `transactions.test.tsx` "moves when the live stream says the order changed"; it passed on rerun.
