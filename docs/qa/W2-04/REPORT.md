# W2-04 POS backend and sync: QA report

The Go backend for the counter: online sales, the offline outbox sync, bill QR claims, counter codes, receipts and purchases. Branch `claude/wp-w2-04-pos-421f24`, cut from `main`, not yet merged. Everything ran locally against testcontainers PostGIS; nothing touched a cloud account. Decisions are in D-130 (`docs/plan/12-decision-log.md`).

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
- **Seams** (`seams.go`): Stock, Pricer, Credits and Referrals. The defaults are `NoOffers`, `NoCredits` (`not_available`) and `NoReferrals`. Stock is the catalogue writer through `jobs.PosStock`.
- **Wiring:** `cmd/todayz/routes_pos.go` mounts the three domains. It has a `posSeams` test hook that sets the fakes.

## Evidence

All runs were on this branch at 5e3a82b, one heavy command at a time, Go tests with `-p 2`. The full `make check` was not run (lead's rule after the crash).

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

Tests never print codes, tokens or phones. `noSecrets` captures the API's log output and asserts that no receipt token, claim token, counter code or phone reached it, in POS-01, 08, 11, 13, 17, 22, 25 and the bill QR test. POS-01 also checks that the receipt has `no-store` and that its body holds no phone and no `customer` field. That sale is unlinked; a receipt for a linked sale is not checked for PII separately, since the Receipt schema has no such fields.

## Not verified

- **Real seams:** the credits ledger, referrals and the Pricer (W2-07 offers) run as fakes. Holds, captures, cashback and clawback are checked only as calls on the seam.
- **SSE delivery:** `bill_claim.updated` is checked as an `outbox_events` row. Delivery to a connected POS was not run.
- **Authenticated contract smoke:** the smoke runs unauthenticated, so `/v1/biz` and `/v1/me` return only 401s there. Their shapes are covered by the HTTP tests and the request validator.
- `make check`, `make e2e`, load and the abuse suite were not run.

## Open / Questions

Each one has the most conservative reading built, and the ones in code are marked `TODO(lead):`.

1. **How long a confirmed QR stays usable** (`sale.go`, TODO). Built: once confirmed inside 120 s, the sale may use it after 120 s too. Should that have a cap?
2. **Hold release:** nothing in pos releases credits held by a claim that was confirmed but never used. Built: that sweep is assumed to live in the credits domain.
3. **`phone_unverified` sales:** should they link later, once the customer verifies that phone? Built: they never link.
4. **Realtime catalogue:** `bill_claim.updated` is not in `realtime.yaml` or the shared catalogue yet.
5. **Pricer:** does W2-07's Pricer match the assumed interface (below)?
6. **Offline-link risk flag score** (`sync.go`, TODO). Built: 50.
7. **Counter-code lockout:** it raises no risk flag. Should five wrong codes flag the store?
8. **Device secret rotation** (`totp.go`, TODO). `DEVICE_SECRET_ENC_KEY` rotation and a customer's "reset my code" are not built.
9. **Fixed 30-day windows:** `changesKept` (cursor) and the receipt TTL are constants, not `platform_settings`. 01 lists no setting for them.
10. **Files outside the owned folders:** `cmd/todayz/pos_http_test.go` and the `posSeams` hook in `routes_pos.go` live in `cmd/todayz`.
11. **Counts in the sync property test:** they are absolute and depend on order, so the property covers sales, restocks, damage and expiries only.

## Assumed seam interfaces (`internal/pos/seams.go`)

```go
type Pricer interface {
    Price(ctx, customerID, storeID ids.ID, lines []PriceLine) (Pricing{LineDiscounts []int64; BillDiscount int64; Offers []AppliedOffer}, error)
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
