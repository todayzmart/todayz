# Ask shops nearby scenarios (SC-ASK)

Owner: W3-16 (`backend/internal/asks`, `backend/internal/share`, `customer/src/asks`, `customer/src/share`, `customer/functions/x/`, `business/src/asks`, `admin/src/demand`). ASK-01…ASK-28 are fixed in [08-testing.md](../../docs/plan/08-testing.md) §SC-ASK; these add to them from ASK-29. Format as in 08 §3: `ID · Given / When / Then · level` (U unit, I integration, E E2E, M manual).

## Where the fixed scenarios run (W3-16 code)

| ID | Covered by | Not covered yet |
|---|---|---|
| ASK-01 | `asks_db_test.go` TestCreateLimitsAndOneOpenAsk | the IST-midnight rollover with a test clock; paging with `limit=` |
| ASK-02 | `asks_db_test.go` TestSelectRecipients (order, radius, `ask_max_stores`), TestCreateSendsNearestFirstAndSkipsExcluded | the seed-book run at the QA point (E2E-21) |
| ASK-03 | TestCreateSendsNearestFirstAndSkipsExcluded, TestNoShopAnswersAreTheSame (same status, code, title, detail) | |
| ASK-04 | TestBand (the four bands at their edges), TestReplyPricerAndNoStockMovement (no `customer`, `phone`, `address`, `distanceM`, `userId` in the biz JSON) | push payload and log-line scan |
| ASK-05 | TestReadyByOK (need-by day rule), TestReplyPricerAndNoStockMovement (second reply → `409 invalid_transition`), TestOneReplyUnderARace | reply at `reply_by` with a test clock → `409 ask_closed` |
| ASK-06 | TestReplyPriceRule (cashier lock, MRP); `business/src/asks/asks.test.tsx` (cashier's Yes sends no price) | |
| ASK-07 | TestReplyPricerAndNoStockMovement (`stock_updated_at` set, no `stock_movements` row); B-20 "You have {n}" in `asks.test.tsx` | |
| ASK-08 | TestReplyPricerAndNoStockMovement (unit price, `validUntil`, max quantity); `customer/src/test/asks.test.tsx` ("Price may have changed · ₹60") | the full quote/order money matrix runs in W2-02's order tests behind the real pricer |
| ASK-13 | TestLowYield; TestCloseWindowRetentionAndDeletion (report → `ask_reported` risk flag, store block) | the order-lifts-the-cap path end to end |
| ASK-14 | TestCreateSendsNearestFirstAndSkipsExcluded (a muted store gets nothing); B-20 muted note in `asks.test.tsx` | the toggle itself is B-14 settings (W2-10) |
| ASK-17 | `share_db_test.go` TestCards; `render_test.go`; `customer/src/test/asks.test.tsx` (Pages Function HTML has no distance, escapes the name) | `share_opened` event row |
| ASK-18 | `share_db_test.go` TestGapsFloor; `admin/src/demand/demand.test.tsx` | |
| ASK-19 | `share_db_test.go` TestMarqueeFloorDenylistAndApproval; `admin/e2e/demand.spec.ts` | |
| ASK-20 | TestCloseWindowRetentionAndDeletion (`asks.Demand` counts the unanswered ask) | the search-then-ask "counts once" sum with the k ≥ 10 floor |
| ASK-21 | TestOneReplyUnderARace (two replies at once from one store) | one booking per reply is W2-02's `ask_reply_used` |
| ASK-22 | TestCloseWindowRetentionAndDeletion | |
| ASK-23 | `customer/src/test/asks.test.tsx` and `customer/e2e/asks.shots.spec.ts` (no Book on an in-store-only card) | the API `409 store_not_bookable` is W2-02's |
| ASK-24 | TestYesWithNoPriceBooksAtCataloguePrice | |
| ASK-25 | TestCards (`410` for a paused store), TestCardCreateLimit, TestCardCreateNeedsHashKey; `/x` gone page in `asks.shots.spec.ts` | |
| ASK-28 | TestCloseWindowRetentionAndDeletion (`OnAccountDelete`) | inside AU-18 |

ASK-09…ASK-12 (W1-06), ASK-15, ASK-16, ASK-26, ASK-27 (W3-12) are not W3-16 code.

## Added scenarios

| ID | Scenario | Lv | Where |
|---|---|---|---|
| ASK-29 | Given a search with no results (C-05), when the customer opens "Ask shops nearby", sets quantity 2 and Today and taps Send, then exactly one `POST /v1/me/asks` goes with `{query, quantity, needBy, lat, lng}` and a ULID `Idempotency-Key`, and C-26 opens with "Asked 5 shops · 0 replied" and "Waiting for shops. Most reply within {min} min." where {min} is the window the server set on the ask | U, E | `customer/src/test/asks.test.tsx`, `customer/e2e/asks.shots.spec.ts` |
| ASK-30 | Given C-08 for a listed product, when the customer asks from it, then the body carries `productId` (not the text) and the item name is fixed in the sheet | U, E | same |
| ASK-31 | Given a signed-out customer, when they fill the sheet and tap "Sign in to send", then no ask is sent, they go to C-10 with `next` set to the page, and after sign-in the sheet opens again with their quantity and need-by | U | `customer/src/test/asks.test.tsx` |
| ASK-32 | Given `429 ask_limit_reached` with `extensions.limit` 3, then the sheet says "You can send 3 asks a day. Try again tomorrow." (the number comes from the server); given `409 ask_already_open` with `extensions.askId`, then "See it" links to that ask | U | `customer/src/test/asks.test.tsx` |
| ASK-33 | Given an ask with three replies (a priced Yes, a Yes without a price that is ready first, an in-store-only Yes), then C-26 shows the best price in the progress line, "Ready soonest" only on the earliest ready-by, "Ask the shop for the price", and no Book on the in-store-only card; Book on the priced card puts the asked quantity and the `askReplyId` in the cart and opens C-11 | U, E | `customer/src/test/asks.test.tsx`, `customer/e2e/asks.shots.spec.ts` |
| ASK-34 | Given an ask that closed with no Yes, then C-26 says "No shop has it right now. We've told shops nearby people want it." and there is no "Close ask" | U, E | same |
| ASK-35 | Given C-08 in a browser with no system share sheet, when the customer taps Share, then one `POST /v1/public/share-cards` goes with the `storeItemId` and the sheet offers "Copy link" (WhatsApp first only on Android) | U, E | same |
| ASK-36 | Given `/x/{code}` for a live card, then the app lands on C-08 with `ref=share`; for a `410`, it shows "This shop isn't on TodayZ right now" with a search box that opens C-05 with the words | U, E | same |
| ASK-37 | Given a shop's B-20 inbox with an open ask, when an owner taps Reply, keeps Yes and sets ₹55 (prefilled with the listed ₹58), then one reply goes with `{answer:'yes', pricePaise:5500}` and a ULID `Idempotency-Key`, the card leaves the open list and shows under "Done today" with ₹55 | U, E | `business/src/asks/asks.test.tsx`, `business/e2e/asks.spec.ts` |
| ASK-38 | Given B-20 at 360 px, then the page has no horizontal scroll, and an ask for an item the shop does not list shows "Add it to your stock" in the reply sheet | E | `business/e2e/asks.spec.ts` |
| ASK-39 | Given a cashier, then B-20 shows no muted note (a cashier cannot read the store settings, so no settings call is made) and a Yes sends no price | U | `business/src/asks/asks.test.tsx` (the priceless Yes; the no-note half is the `canSeeAskSettings` gate, not asserted yet) |
| ASK-40 | Given A-23 Gaps for all cities, then a product not in the catalogue carries "Not in the catalogue", the city column shows, the City filter narrows the rows and Export CSV saves only the shown rows (cells that start with `=`, `+`, `-` or `@` are quoted as text) | U, E | `admin/src/demand/demand.test.tsx`, `admin/e2e/demand.spec.ts` |
| ASK-41 | Given A-23 Site strip as `catalogue`, when they untick one product and tap "Approve {n}", then one decide call goes with the kept product ids, `builtOn` and a ULID `Idempotency-Key`, and the card shows the approved chips; a `409` says "This changed while you were looking. Reload and check it again."; a `field_agent` never sees the tab and `?tab=strip` falls back to Gaps | U, E | same |
