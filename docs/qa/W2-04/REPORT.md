# W2-04 POS backend and sync: QA report

The Go backend for the counter: online sales, the offline outbox sync, bill QR claims, counter codes, receipts and purchases. Branch `claude/wp-w2-04-pos-421f24`, cut from `main`, with `main` merged back in at 0f2f4ed (W2-06 reviews, W2-07 offers, the shared test Postgres); not yet merged into `main`. Everything ran locally against PostGIS (testcontainers, then the shared test server); nothing touched a cloud account. Decisions are in D-130 and, for the lead's review fixes, D-131 (`docs/plan/12-decision-log.md`).

## What was built

- **Contract:** the pos, sync and receipts domains in `backend/api/paths|schemas|problems/`. The new problem codes are `claim_expired`, `claim_already_used`, `claim_mismatch`, `reversal_window_passed` (pos) and `resync_required` (sync, 410). It reuses `code_invalid`, `code_locked`, `price_changed`, `conflict`, `invalid_transition` and `validation_failed`.
- **Migration 20401:** `pos_sales`, `pos_sale_lines`, `bill_claims`, `counter_code_checks`, `counter_code_uses`, `receipt_tokens`, `sync_cursors`, `sync_entries`.
- **Policy:**
  - `pos.sell`, `pos.link_customer` and `pos.sync` for all staff;
  - `pos.discount` and `pos.reverse` for owner and manager;
  - `me.counter_code`, `me.bill_claim` and `me.purchases` for customers.
- **Online sale** (`internal/pos/sale.go`, `money.go`):
  - priced on the server through the Pricer seam;
  - a stale catalogued price or a wrong `expectedTotalPaise` is 409 `price_changed`;
  - custom lines are allowed;
  - round-off is to the nearest ₹1, half up (D-94);
  - each catalogued line is one `sale_pos` movement.
- **Reversal:** within `pos_reversal_window_hours`, by owner or manager only. It writes `pos_reversal` movements, then calls `Credits.ReverseSale` and `Referrals.Revoke`.
- **Sync** (`sync.go`):
  - idempotent by entry client id;
  - entries must arrive in `deviceSeq` order;
  - each entry is checked against its own role (sale, count, restock, damage, expired);
  - offline prices stand, and an oversell goes negative and flags `countNeeded` (02 §4.3);
  - the server cursor reads `catalogue_changes` and is 410 `resync_required` past 30 days.
- **Bill QR** (`claims.go`):
  - a 64-char HMAC token under `BILL_QR_SIGNING_KEY` (503 when the key is unset), valid for 120 s;
  - confirm takes `FOR UPDATE`: the first confirm wins and the second is `claim_already_used`;
  - credits are held at confirm and captured by the sale;
  - each change emits `bill_claim.updated` through the outbox.
- **Counter code** (`totp.go`):
  - TOTP, HMAC-SHA1, 60 s, ±1 step, 4 digits;
  - the seed is sealed under `DEVICE_SECRET_ENC_KEY`;
  - five tries per bill, then `code_locked`;
  - an offline code is verified at sync against the recorded device time, within `pos_offline_link_max_hours` and a 2-minute clock offset, once only;
  - more than `pos_offline_links_risk_per_day` offline links raise one store risk flag.
- **Rewards:** a sale linked by `bill_qr` or `counter_code` earns cashback through the Credits seam and qualifies a referral at `ref_qualifying_min_order_paise`. A `phone_unverified` link earns nothing and writes no ledger rows (D-07).
- **Receipts and purchases** (`receipts.go`):
  - a 128-bit receipt token per sale;
  - `GET /v1/public/receipts/{token}` returns no customer, phone, wallet or link method, sends `no-store` and expires after 30 days;
  - `GET /v1/me/purchases` merges orders and linked POS sales, newest first, with a keyset cursor (D-45, D-84).
- **Seams** (`seams.go`): Stock, Pricer, Redeemer, Credits and Referrals. The defaults are `NoOffers`, `NoRedeem`, `NoCredits` (`not_available`) and `NoReferrals`. Stock is the catalogue writer through `jobs.PosStock`.
- **Offers (W2-07, real):** `offersForPos` in `cmd/todayz/routes_pos.go` adapts `offers.Service`:
  - `Price` on `ChannelPOS`, as a walk-in, with no Fixed lines (a catalogued line already carries the catalogue price);
  - `Redeem(ctx, Ref{POSSaleID}, customer, quote)` runs inside the sale's transaction (`db.InTx` puts the tx in ctx), after the lines and before the claim bind; `offers.ErrUnavailable` becomes `pos.ErrOfferGone`, which the sale returns as `409 price_changed`;
  - a reversal calls `Release(Ref{POSSaleID})` in the reversal's transaction;
  - an offline sale at sync redeems nothing (its discounts are the device's, with no offer ids).
- **Reviews (W2-06):** `posForReviews` implements `reviews.SaleSource` through `pos.Service.SaleRef` (store, customer, link method only for a rewarding link, completed unless reversed). `routes_reviews.go` now passes it as `Sales`, so a POS sale can be reviewed.
- **Wiring:** `newPos` in `cmd/todayz/routes_pos.go` builds the service for `mountPos` and for reviews. It has a `posSeams` test hook that sets the fakes (credits, referrals, and the offers fakes in the older tests).

## Evidence

Runs were on this branch at 5e3a82b (first table) and 64b075b (second table), one heavy command at a time, Go tests with `-p 2`. The full `make check` was not run (lead's rule after the crash).

| Check | Result |
|---|---|
| gofmt | clean |
| `go vet ./...` | clean |
| golangci-lint (`internal/pos`, `internal/jobs`, `cmd/todayz`) | 0 issues |
| gosec (same packages) | 0 issues |
| settings lint | clean |
| `make gen` | exit 0 |
| `redocly lint` | valid; 10 warnings, none in pos, sync or receipts |
| `TestContractSmoke`, `TestContractProblemCheckFires` | PASS (smoke 11.7 s) |
| `go test -race -p 2 -run POS ./cmd/todayz/` (27 tests) plus `TestRoutesAreInTheContract`, `TestOneMountPerDomain` | ok, 39 s |
| `internal/pos` unit tests (7) | ok |
| `TestMigrationFileNames` | ok |

After merging `main` and wiring offers and reviews (64b075b), with `TODAYZ_TEST_PG_URL_FILE` set to the shared test server:

| Check | Result |
|---|---|
| `go vet`, golangci-lint, gosec, settings lint (`internal/pos`, `cmd/todayz`) | clean, 0 issues |
| `make gen` | exit 0 |
| `TestPOSOffersRedeemAndRelease`, `TestPOSSaleReview` | ok, 5.7 s |
| `go test -race -p 2 -run 'POS\|TestRoutesAreInTheContract\|TestOneMountPerDomain\|Offer\|Review' ./cmd/todayz/` | ok, 56 s |
| `internal/pos`, `internal/reviews`, `internal/offers` unit and DB tests | ok |
| `go test -race -p 2 ./cmd/todayz/` (whole package) | ok, 161 s |

Review round (D-131), at df6d08e, shared test server:

| Check | Result |
|---|---|
| golangci-lint, gosec, settings lint (`internal/pos`, `internal/jobs`, `cmd/todayz`) | clean, 0 issues |
| `make gen` | exit 0 |
| `go test -race -p 2 ./cmd/todayz/ ./internal/pos/ ./internal/jobs/` | ok (cmd/todayz 195 s); `internal/platform/migrate` ok at a6c7ba0 |
| `TestMigrationFileNames`, `TestRoutesAreInTheContract` | ok |
| `TestContractProblemCheckFires` | PASS |
| `TestContractSmoke` | FAIL, 136/137 operations pass. The one failure is not W2-04's: `GET /v1/discover/stores/{slug}/reviews` (W2-06, `api/paths/reviews.yaml`) answers 422 `query.cursor: not a cursor from this list` to a schema-valid long cursor in the coverage phase. This round changed no contract file. For the reviews owner: either give the cursor parameter a `pattern`/`maxLength` the server keeps, or treat an unknown cursor as allowed by the schema. |

## Review fixes (D-131)

The lead's W2-04 review: no blockers. Each item was landed on this branch with a test (`cmd/todayz/pos_review_http_test.go` unless named).

| Item | What changed | Test |
|---|---|---|
| MUST 1, code brute force | `counter_code_failures`: 5 per (store, phone) in 15 min lock the phone at the store for 15 min; 20 per cashier in an hour lock the cashier for an hour. Advisory locks: cashier, then phone. Offline wrong or reused codes count too (`source = 'sync'`); a locked phone fails the sync's remaining links | `TestPOSPhoneLockAcrossBills` (a new `saleClientId` is still locked; unlocks after 15 min), `TestPOSCashierCodeLock`, `TestPOSOfflineCodesHitTheLock` (7 wrong then the right code in one sync: all failed, 5 counted, locked online after) |
| MUST 2, `sold_at` | Clamped to at most `received_at` (and at least 72 h before it, 01 §17). An offline sale's reversal window runs from `received_at` | `TestPOSReversalWindowFromReceived` (`at` 20 h ahead, reversal after 24 h is `reversal_window_passed`; a sale 5 h behind reverses 23 h after it arrived), `TestPOS06DeviceClock` |
| MUST 3, guard tables | `REVOKE DELETE, TRUNCATE` on `counter_code_uses` and `counter_code_checks`; `UPDATE, DELETE, TRUNCATE` on `sync_entries`, `receipt_tokens` and `counter_code_failures`; `no_truncate` triggers; 20401 edited in place | `TestPOSGuardTablesAppendOnly` (`todayz_app` DELETE, TRUNCATE and UPDATE denied; the owner's TRUNCATE refused) |
| SHOULD 4, offline offer discount | Accepted as sold; counts with the offline links against `pos_offline_links_risk_per_day`; a cashier without `pos.discount` also raises `pos.offline_offer_without_discount_role` | `TestPOSOfflineOfferDiscount` |
| SHOULD 5, bill-QR retry | Same `clientId` under a new key returns the sale (replay), also when both arrive at once | `TestPOSBillQRConcurrentRetry` (3 rounds of two at once: both 200, one sale, one capture) |
| SHOULD 6, credit holds | A new QR cancels the bill's earlier claims and releases their holds; a confirmed claim past `expires_at` is `claim_expired` at bind; the bill's sale releases the others; timer `pos_bill_claim_expiry` + runbook; `Capture` must fail on a released hold (seams.go). The test fake records each Release and Capture and refuses a capture after a release | `TestPOSClaimHoldLifecycle` (two claims for one bill; late bind; plain sale; the sweep through the service and through the registered timer) |
| Nits | Manual discount past the offer's remainder is `422`, not clamped; a verified check binds for 2 min; `Stock.Reverse` errors are `409 invalid_transition`; the re-confirm comment fixed; receipt token on replay left open (question 17) | `TestPOSManualDiscountAndCheckAge` |

`TestPOSBillQRSale` now sells within the claim's 120 s, since a late bind is refused.

## Coverage of SC-POS (08-testing.md)

| Scenario | Where | Notes |
|---|---|---|
| POS-01, 02, 04, 05, 06, 08, 09, 11, 13, 14, 17 | `cmd/todayz/pos_http_test.go` | HTTP against a real database |
| POS-10 | `TestClaimTokenTamper` (unit) | amount, store, expiry, tag bit, key |
| POS-12 | `TestTOTPDrift` (unit), `TestPOSCodeDrift` (HTTP) | ±1 step passes, ±2 fails |
| POS-16 | `TestRoundOff`, `TestPriceBill` (unit) | D-94 boundaries, credits leave ≥ ₹1 |
| POS-22…27 | `pos_http_test.go` | offline code boundaries, risk flag, cursor, receipt expiry, purchases, ₹100 credit minimum |
| POS-28…34 (builder-written) | `pos_http_test.go` | online then sync, entry roles, item not found, tenancy, price changed, deviceSeq order, concurrent receipts |
| Bill-claim race | `TestPOS09ClaimRace` | one winner; credits held once |
| Sync property | `TestPOSSyncProperty` | random batches and replays; every entry applies once, stock ends the same |
| POS-03, 07, 15 | — | E2E with the POS frontend (W2-11) |
| POS-18…21 | — | the ledger side belongs to the credits and referrals domains. Here they are fakes; the tests only check that `ReverseSale` and `Revoke` are called |
| POS-35 | — | not written; the slot is free |
| Offers at the counter | `TestPOSOffersRedeemAndRelease` | a 10% offer prices the line and writes one redemption; a reversal releases it; an offline sale writes none; a flat offer with `maxRedemptions` 1 applies to the first sale only |
| Reviews of a sale | `TestPOSSaleReview` | a counter-code sale is reviewable once (`review_exists` after); `phone_unverified` is 404; a reversed sale is `review_not_eligible` |

Tests never print codes, tokens or phones. `noSecrets` captures the API's log output and asserts that no receipt token, claim token, counter code or phone reached it, in POS-01, 08, 11, 13, 17, 22, 25 and the bill QR test. POS-01 also checks that the receipt has `no-store` and that its body holds no phone and no `customer` field. That sale is unlinked; a receipt for a linked sale is not checked for PII separately, since the Receipt schema has no such fields.

## Not verified

- **Real seams:** the credits ledger and referrals run as fakes (offers are now real). Holds, captures, cashback and clawback are checked only as calls on the seam.
- **SSE delivery:** `bill_claim.updated` is checked as an `outbox_events` row. Delivery to a connected POS was not run.
- **Authenticated contract smoke:** the smoke runs unauthenticated, so `/v1/biz` and `/v1/me` return only 401s there. Their shapes are covered by the HTTP tests and the request validator.
- `make check`, `make e2e`, load and the abuse suite were not run.

## Open / Questions

Each one has the most conservative reading built, and the ones in code are marked `TODO(lead):`.

1. **How long a confirmed QR stays usable:** resolved by the review (D-131). A sale binds a claim only before its `expires_at`.
2. **Hold release:** resolved by the review (D-131). A new QR, the bill's sale and the `pos_bill_claim_expiry` timer release holds. Still open: the worker's Credits is the do-nothing default until W2-01 wires its ledger into `internal/jobs/pos.go` (`TODO(lead)`).
3. **`phone_unverified` sales:** should they link later, once the customer verifies that phone? Built: they never link.
4. **Realtime catalogue:** `bill_claim.updated` is not in `realtime.yaml` or the shared catalogue yet.
5. **Pricer:** resolved. W2-07's `offers.Service` is wired through `offersForPos`.
6. **Offline-link risk flag score** (`sync.go`, TODO). Built: 50.
7. **Counter-code lockout:** it now locks per phone and per cashier (D-131), but it still raises no risk flag. Should a phone or cashier lock flag the store?
8. **Device secret rotation** (`totp.go`, TODO). `DEVICE_SECRET_ENC_KEY` rotation and a customer's "reset my code" are not built.
9. **Fixed 30-day windows:** `changesKept` (cursor) and the receipt TTL are constants, not `platform_settings`. 01 lists no setting for them.
10. **Files outside the owned folders:** `cmd/todayz/pos_http_test.go` and the `posSeams` hook in `routes_pos.go` live in `cmd/todayz`.
11. **Counts in the sync property test:** they are absolute and depend on order, so the property covers sales, restocks, damage and expiries only.
12. **Offline sales and offer limits:** a synced sale carries the device's discounts with no offer ids, so it redeems nothing and does not count against `maxRedemptions` or a budget. Should the outbox entry carry offer ids so sync can redeem (and accept going over a limit)?
13. **Custom lines and minimum-bill offers:** custom lines (no `storeItemId`) are not sent to the pricer, so they do not count toward a minimum-bill threshold.
14. **Walk-in pricing:** the sale is priced with no customer, even when a customer is linked; the redemption records the linked customer. Per-customer offers therefore never apply at the counter. Should the sale price with the linked customer?
15. **An offer that runs out mid-sale:** `409 price_changed` with no extension members; the POS re-sends and gets the new prices. A dedicated code may read better at the counter.
16. **FKs to `pos_sales` and `bill_claims`:** not added here (see below).
17. **Receipt token on a replay** (open item from the review). A replayed sale (same `clientId`, the idempotent retry or the bill-QR retry) comes back without `receiptToken`: the token is shown once and stored only hashed. The POS keeps the first one; reissuing would need a new token row per replay.
18. **The 72 h `sold_at` clamp** (`sale.go`, TODO). 01 §17 lists "sold_at clamp for offline sales, 72 h" with no direction. Built: an offline `sold_at` is never more than 72 h before `received_at`, and never after it.
19. **`pos.offline_offer_without_discount_role` score** (`sync.go`, TODO). Built: 50, as for `pos.offline_links`.
20. **Plan text to update** (not W2-04's files): 03 §5 says that a reversal is allowed within the window "of `sold_at`". For an offline sale, the window now runs from `received_at`. The counter-code row in 01 §6 says "5 wrong tries per bill"; D-131 adds 5 per phone at a store in 15 minutes and 20 per cashier in an hour.
21. **Deadlocks between syncs:** a code check locks the cashier, then the phone. Two cashiers' syncs that touch the same phones in different orders can deadlock. Postgres aborts one of them (a 500), and the device retries with the same key. Should the sync sort its counter-code entries by phone first?

## The cross-package FKs

The lead asked for the FKs from `reviews.pos_sale_id` and `offer_redemptions.pos_sale_id` to `pos_sales` in a new migration in this block. They are not in this branch. Migrations run in file-name order, and any 204xx file sorts before `20601_w2-06_reviews.sql` and `20701_w2-07_offers.sql`, so on a fresh database it would run before those tables exist and fail. The plan puts these in the Wave 2 constraints migration that sorts after every Wave 2 file (03 §13, D-35, D-79). The SQL for it:

```sql
ALTER TABLE reviews
  ADD CONSTRAINT fk_reviews_pos_sale FOREIGN KEY (pos_sale_id) REFERENCES pos_sales(id);
ALTER TABLE offer_redemptions
  ADD CONSTRAINT fk_offer_redemptions_pos_sale FOREIGN KEY (pos_sale_id) REFERENCES pos_sales(id);
-- after W2-01 lands credit_holds:
-- ALTER TABLE credit_holds
--   ADD CONSTRAINT fk_credit_holds_bill_claim FOREIGN KEY (bill_claim_id) REFERENCES bill_claims(id);
```

The indexes they need already exist (`uq_reviews_pos_sale`, `ix_offer_redemptions_pos_sale`). No `ON DELETE` is needed: POS sales are never deleted, only reversed.

## Assumed seam interfaces (`internal/pos/seams.go`)

```go
type Pricer interface {
    Price(ctx, customerID, storeID ids.ID, lines []PriceLine) (Pricing{LineDiscounts []int64; BillDiscount int64; Offers []AppliedOffer}, error)
}
type Redeemer interface {
    Redeem(ctx, saleID, customerID ids.ID, applied []AppliedOffer) error // in the sale's tx; ErrOfferGone when one ran out
    Release(ctx, saleID ids.ID) error
}
type Credits interface {
    Allow(ctx, customerID, storeID ids.ID, requested int, eligiblePaise int64) (CreditsAllowance{Allowed int; Limit string}, error)
    Hold(ctx, customerID, storeID, claimID ids.ID, credits int) (int, error)
    Capture(ctx, claimID, saleID ids.ID) error
    Release(ctx, claimID ids.ID) error
    Cashback(ctx, Cashback{CustomerID, StoreID, SaleID ids.ID; Credits int}) (int, error)
    ReverseSale(ctx, saleID, customerID ids.ID) error
}
type Referrals interface {
    Qualify(ctx, customerID, saleID ids.ID, qualifyingPaise int64) error
    Revoke(ctx, customerID, saleID ids.ID) error
}
type Stock interface {
    Sell(ctx, tx, StockSale) (ids.ID, error)
    Reverse(ctx, tx, StockReversal) (ids.ID, error)
    Change(ctx, tx, StockChange) (StockResult, error)
}
```
