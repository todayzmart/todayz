# W2-01 Credits ledger: QA report

The double-entry credits ledger in `backend/internal/credits`, with the `wallet` and `business-credits` API domains. It is wired into orders for hold, release, capture and cashback. Branch `claude/wp-w2-01-credits-1bb47b`. Everything ran locally on testcontainers; nothing touched a cloud account, and `make dev`, `make reset` and `make e2e` were not run.

## What was built

- **Migration `20101_w2-01_credits.sql`** creates these tables:
  - `ledger_accounts`, `account_balances`, `ledger_transactions`, `ledger_entries`;
  - `credit_holds`, `credit_lots`, `credit_lot_consumptions`.

  It also does the following:
  - A deferred constraint trigger refuses a transaction whose entries do not sum to 0.
  - The platform accounts are seeded: `platform_marketing`, `platform_expired`, `platform_adjustments` and `platform_payouts_cash`.
  - `todayz_app` gets INSERT/SELECT only on accounts, transactions, entries and consumptions, and may not DELETE balance, hold or lot rows.
- **Migration `20102_w2-01_ledger_grants.sql`** (D-123, CR-19): the two apply triggers become `SECURITY DEFINER`. `todayz_app` keeps only column grants on `account_balances` (INSERT `account_id, kind, updated_at`; UPDATE `updated_at`, which `SELECT … FOR UPDATE` needs) and on `credit_lots` (UPDATE `expired_at, reminded_at`). A balance or a lot's amounts move only through an entry or a consumption.
- **Migration `20202_w2-02_order_cashback_bp.sql`** (D-124): `orders.cashback_bp`, the store's rate snapshotted at placement. It is in orders' block because D-90 keeps a 201xx file off `orders` (see "Files changed outside the owned paths").
- **The service (`service.go`)** implements Holder and Poster in the caller's transaction:
  - **Lock order:** the customer and store balance rows first, in one statement in id order; then the hold; then the lots. `Hold` locks the store's row too, so the store's daily cap is counted under a lock (D-120). New accounts are created in (kind, owner) order, so two first uses never deadlock (D-121). Platform rows are not locked up front, and their entries are written last.
  - **Holds** draw from lots FIFO. A release returns credits to the same lots, latest expiry first. A lot that expired while held goes straight to `platform_expired` (D-27).
  - **Capture** moves the hold to `store_credit`. Cashback is paid on eligible − credits × 100, floored to whole credits, and creates a `cashback` lot. The rate is the completion's `CashbackBP` (an order's snapshot, D-124), or the store's rate now when it is nil (POS).
  - **Caps:**
    - per order (`credit_max_per_order`);
    - the ₹1 floor;
    - `credit_min_order_value_paise`;
    - `credit_orders_per_month` per customer in the IST month;
    - `store_credit_redemptions_per_day` per store in the IST day.

    Every number is read from `platform_settings`. A cap refusal writes a `risk_flags` row outside the caller's transaction, so the flag survives a rollback; at most one per subject, rule and IST day (D-126).
  - **Idempotency:** every operation is idempotent per ref (hold, recap, release, complete), and every posting per `IdempotencyKey`. A Poster key must start with its kind's prefix (`KeyPrefix`), and a repeat with other legs is `ErrKeyReused` (D-122).
  - **Notices:** `credits_earned` on cashback; one `credits_expiring` per customer for the lots due within `credit_expiry_reminder_days`.
- **Jobs**, registered in `internal/jobs/credits.go`:
  - `credits_expire`: money class, hourly at :05;
  - `credits_expiry_reminder`: notify class, daily at 10:00;
  - `ledger_reconcile`: money class, daily at 03:30. It only reads, and records `tz_ledger_reconcile_drift{reason}`.

  Other metrics: `tz_credits_cap_refusals_total` and `tz_credits_expired_total`. The runbook is `docs/runbooks/credits-ledger.md`.
- **Reads:**
  - `GET /v1/me/wallet`;
  - `GET /v1/me/wallet/transactions`: plain-language lines with params for i18n;
  - `GET /v1/biz/businesses/{businessId}/credit-account`: owner and manager, with each store's balance and lines. A manager limited to some stores sees only those (D-125).
- **The `OnAccountDelete` forfeiture (D-31):**
  - It posts one `forfeit_deletion` from `customer_wallet` to `platform_expired` and closes the lots.
  - A repeat is a no-op.
  - Held credits refuse the deletion with 409 `conflict`.
- **`Fake`:** an in-memory Holder and Poster with the same rules, for W2-03 and W2-04 unit tests. It refuses the same mismatched completion and held-credit deletion as the service.

## The interfaces (for W2-03 referrals and W2-04 POS)

```go
type RefType string                       // RefOrder "order", RefBillClaim "bill_claim"
type Ref struct { Type RefType; ID ids.ID }

type Limit string // "", not_available, below_credit_minimum, store_credit_cap,
                  // credit_monthly_cap, credit_max_exceeded, no_balance
func (l Limit) Blocks() bool // no credits at all may be used

type Allowance struct { Allowed, Max, Available int; Limit Limit }
type HoldRequest struct { Ref Ref; CustomerID, StoreID ids.ID; Credits int; EligiblePaise int64 }
type Completion struct { Ref Ref; CustomerID, StoreID ids.ID; EligiblePaise int64
                         CashbackBP *int } // 0–2000; nil = the store's rate now (POS); an order passes its snapshot (D-124)
type Completed struct { Captured, Cashback int }

// Holder: Hold, Recap, Release and Complete run in the caller's transaction
// and are idempotent per Ref.
type Holder interface {
	Allow(ctx context.Context, customerID, storeID ids.ID, requested int, eligiblePaise int64) (Allowance, error) // writes nothing
	Hold(ctx context.Context, req HoldRequest) (Allowance, error)   // all or none; refusal = Allowed 0 + Limit, nil error
	Recap(ctx context.Context, ref Ref, eligiblePaise int64) (int, error) // partial accept (D-28): credits still held
	Release(ctx context.Context, ref Ref) (int, error)               // whole hold back to its lots
	Complete(ctx context.Context, c Completion) (Completed, error)   // capture + cashback; repeat returns the first
}

type Account struct { Kind AccountKind; Owner ids.ID } // Owner: user id, store id, or zero for platform
type Leg struct { Account Account; Amount int64 }       // + increases the account
type Posting struct {
	Kind           TxKind // referral_reward, admin_adjustment or payout
	RefType        string // both or neither
	RefID          ids.ID
	IdempotencyKey string // ≤ 200 chars; KeyPrefix[Kind] + something (D-122); a repeat posts nothing
	CreatedBy      ids.ID
	Memo           string
	Legs           []Leg
}
type Posted struct { TransactionID ids.ID; Duplicate bool }

// Poster: legs sum to 0, non-zero, one per account; customer_held is never a
// leg; a wallet credit makes a lot, a debit consumes FIFO (ErrInsufficient);
// payout = store_credit down + platform_payouts_cash up; referral_reward is
// funded by platform_marketing; admin_adjustment is against platform_adjustments.
type Poster interface {
	Post(ctx context.Context, p Posting) (Posted, error)
}

var KeyPrefix = map[TxKind]string{TxReferralReward: "referral:", TxAdminAdjustment: "adjust:", TxPayout: "payout:"}

var ErrInsufficient, ErrInvalid, ErrUnavailable, ErrHeld error
var ErrKeyReused error // the key was posted before with other legs; nothing posted

// *Service also: OnAccountDelete(ctx, userID) error (identity hook),
// ExpireDue, RemindExpiring, Reconcile (jobs), Wallet, WalletLines,
// CreditAccount(ctx, businessID, stores []ids.ID /* nil = all */, limit, cursor) (reads),
// Rules(ctx) (Rules, error). credits.Register(r, s, authz, staff authz.StaffResolver).
```

- **W2-03 referrals** posts `TxReferralReward` with legs `platform_marketing −n` and `customer_wallet +n`, and a key such as `referral:<id>:<side>`.
- **W2-04 POS** uses `Ref{Type: RefBillClaim, ID: claimID}`:
  - `Hold` when the claim is made;
  - `Complete` when the sale completes;
  - `Release` when the claim expires. The line reads "Returned: the bill was not confirmed in time".

## The orders seam

`orders.Credits` (in `internal/orders/seams.go`) now has `Allow`, `Hold`, `Release` and `Complete`. The adapter is `jobs.OrdersCredits(*credits.Service)`, using `Ref{RefOrder, orderID}`. It is wired in two places:
- `cmd/todayz/routes_orders.go`, for the API;
- `internal/jobs/orders.go`, for the order timers.

What orders does with it:
- **Create:** holds after `InsertOrder` when `credits_used > 0`.
  - A blocking limit returns that limit's problem: `below_credit_minimum`, `credit_max_exceeded`, `credit_monthly_cap` or `store_credit_cap`.
  - A short hold (the balance changed after the quote) re-prices and returns `price_changed` with the fresh quote.
- **Create** also writes the store's `cashback_bp` on the order (D-124).
- **Every end state** goes through `end()`, which always releases; `Release` without an open hold does nothing (D-127).
- **`collect()`** completes with the order's own `cashback_bp`: it captures, pays cashback, and writes `cashback_credits` on the order. The seam is `Complete(ctx, orderID, customerID, storeID, itemsAfterOffersPaise, cashbackBP)`.
- **Recap is not wired**, because orders has no partial accept yet (BK-08–10 deferred).

`NoCredits` stays the default when no ledger is configured.

## Evidence

| Check | Result |
|---|---|
| `go test -race -p 2 ./internal/credits/` (testcontainers PostGIS) | ok, 23 tests, 29 s |
| CR-13 Fake property run (`TestFakeInvariantsProperty`) | 10,000 runs, invariants 1–8 hold |
| CR-13 database property run (`TestServiceInvariantsPropertyDB`) | 150 runs through the real service. After every step the reconcile finds no drift and no balance is below 0 |
| CR-12 (`TestConcurrentSpendDB`) | 50 goroutines, 25 holds and 25 debits of 30 each on a wallet of 100: exactly 3 succeed, the wallet ends at 10, and the reconcile is clean |
| `go test -p 2 ./internal/jobs/ ./internal/notify/` | ok |
| `go test -p 2 ./cmd/todayz/` | ok. The two contract-smoke tests skip in a plain `go test`; they ran under `make check-contract` (below) |
| `make gen` | exit 0 |

### After the review fixes (shared test server, `TODAYZ_TEST_PG_URL_FILE`)

| Check | Result |
|---|---|
| `go test -p 2 ./internal/credits/` | ok, 20 s (with `TestConcurrentStoreCapDB`, 3 of 3 runs) |
| `go test -p 2 ./internal/jobs/ ./internal/notify/ ./internal/platform/migrate/` | ok (migrate: block order, file names and up-down-up with 20102 and 20202) |
| `go test -p 2 ./cmd/todayz/ ./internal/referrals/ ./internal/reviews/` | ok, 47 s (referrals and reviews insert `orders` rows directly; the 0 default keeps them valid) |
| `go vet ./...` | ok |
| golangci-lint on credits, orders, jobs, cmd/todayz | 0 issues |
| settings lint | exit 0 (`maxCashbackBP = 2000` carries `//settings:ok`: the 0–20% range is a product bound, not a setting) |
| gofmt | clean |
| `make gen` | exit 0 |
| `make check-contract` | Redocly lint, `apigen check` and the api-client checks pass. **`TestContractSmoke` fails on two operations that are not W2-01's** and came in with main (W2-06 and W2-03): `GET /v1/discover/stores/{slug}/reviews` answers 422 to a schema-valid very long `cursor` (the contract allows a cursor the validator or handler refuses), and `GET /v1/public/ref/{code}` is a network error (its redirect goes to the customer app, which is not running under the smoke). 132 of 134 operations pass, including wallet and business-credits. Nothing W2-01 changed touches either path; listed under Questions for those WPs |

### Area checks

Per the lead's rule, I ran these instead of the full `make check`:

| Check | Result |
|---|---|
| `gofmt -l` on the changed packages | clean |
| `go vet ./...` | ok |
| golangci-lint on credits, jobs, orders, notify, notice, authz, cmd/todayz | 0 issues. The last run fixed an errorlint finding (`errors.Is` in `fake_test.go`) and a gosec G101 false positive (the runbook path constant is now `ledgerRunbook`) |
| settings lint (`go run ./internal/devtools/settingslint .`) | exit 0 |
| authz policy tests | ok |
| `make check-contract` | exit 0. Redocly lint: valid, with 10 warnings (unused components and a media-type example, none in wallet or business-credits). `apigen check`: ok. api-client: eslint and 42 tests pass. Hooks: 13 tests ok. `TestContractSmoke` (Schemathesis examples and coverage): ok, with 44 problem codes registered |

A fix from the final runs: in `cmd/todayz`, the identity deletion tests build their dependencies without Settings, so `OnAccountDelete` returned "not configured" and the deletion answered 500. The deletion forfeiture now needs only the database (`Service.dbReady`), because it reads no setting.

Not run here: the full `make check`, `make e2e`, gosec and govulncheck across the whole module. The lead runs `make check` on main after merging.

## Scenarios

| Scenario | Where | Notes |
|---|---|---|
| CR-01 | `TestHoldReleaseCaptureDB` | Available 70, held 30 |
| CR-02 | `TestAllowLimitsDB`, `TestAllow` | |
| CR-03 | `TestAllow` (unit) | Total ₹70 is the orders quote, which already uses `Allow` |
| CR-04 | `TestHoldReleaseCaptureDB`, `TestWalletLineText` | "Spent at Lakshmi Traders · order TZ-4821" |
| CR-05 | `TestHoldReleaseCaptureDB`, `TestWalletLineText` | "Returned: order TZ-… was cancelled / expired" |
| CR-06 | `TestCashbackDB`, `TestCashback` | 24 credits; store −24 |
| CR-07 | `TestCashbackDB` | no transaction written |
| CR-08 | `TestExpiryAndReminderDB` | partial lot expires; reminder sent once |
| CR-09 | `TestExpiryAndReminderDB`, `TestTakeFIFO` | |
| CR-10 | `TestCapsDB` | the quote shows the limit; risk flag on the user |
| CR-11 | `TestCapsDB` | risk flag on the store survives the caller's rollback |
| CR-12 | `TestConcurrentSpendDB` | |
| CR-13 | `TestFakeInvariantsProperty`, `TestServiceInvariantsPropertyDB` | |
| CR-14 | `TestReconcileDB` | `cache_drift` and `lots_vs_balances` = 1; nothing fixed. The alert rule itself is infra's (Questions) |
| CR-15 | — | **W3-01** (admin adjustments). `Post` with `TxAdminAdjustment` is the primitive it builds on |
| CR-16 | `TestCashbackDB` | |
| CR-17 | `TestRecapDB` | Service only; orders has no partial accept yet |
| CR-18 | `TestBillClaimDB` | |
| CR-19 | `TestAppendOnlyDB` | 42501 on UPDATE or DELETE of entries and transactions; DELETE of balances, consumptions, lots and holds; and, since 20102 (D-123), a direct UPDATE of a balance or its kind, an INSERT with a balance, and an UPDATE of a lot's `remaining`, `held` or `original` |
| CR-20 | — | E2E: language texts come with the apps. The API returns English `text` plus `params` (shop name, order number, reason, percent) for i18n |
| CR-21 | `TestAllowLimitsDB` | 9999 refused; 10000 and 10001 allowed |
| CR-22 | `TestAllowLimitsDB`, `TestAllowNeverBelowOneRupee` | cap raised to 500 → 149 on ₹150 |
| CR-23 | `TestHoldReleaseCaptureDB` | 13 credits |
| CR-24 | `TestExpiryWhileHeldDB` | both the collected and the cancelled branch |
| CR-25 | `TestHoldReleaseCaptureDB`, `TestRecapDB` | no new lot; cashback creates one |
| CR-26 | `TestCapsDB` | 23:59:59 / 00:00:00 IST, for the day and the month |
| CR-27 | — | **W3-01** (two-person approval roles) |
| CR-28 | `TestForfeitOnDeleteDB` | one posting of 83; repeat is a no-op; held → `ErrHeld` |
| CR-29 (builder) | `TestConcurrentSpendDB`, the property runs | **A hold racing a debit for the same credits.** Holds (orders) and debits (Poster) both lock the wallet row first. The loser sees the winner's balance: a hold comes back short, a debit returns `ErrInsufficient`. Neither ever overdraws |
| Store cap race (review, D-120) | `TestConcurrentStoreCapDB` | 12 customers hold at one store at once with `store_credit_redemptions_per_day` at 11: 11 held, exactly 1 `store_credit_cap`. With the store lock removed it holds 12 (checked by mutation) |
| Cashback snapshot (D-124) | `TestCollectPaysPlacedCashbackHTTP`, `TestCashbackDB` | placed at 5%, store moved to 10%, collected at 5% (`bp:500`); a completion's own rate beats the store's both ways; −1 and 2001 are `ErrInvalid`. Without the rate passed through the adapter the HTTP test fails (checked by mutation) |
| Store-limited read (D-125) | `TestCreditAccountHTTP`, `TestReadsDB` | owner and full manager see both stores, a manager of s2 sees s2; cashier 403; other business 403/404 |
| CR-30 (builder) | `TestHoldReleaseCaptureDB`, `TestPostDB`, `TestForfeitOnDeleteDB`, the property runs | **Idempotent repeats.** A repeated hold returns the hold; release and complete return what was posted first; `Post` with the same key gives `Duplicate: true` and writes nothing; a repeated forfeiture posts once |

## Files changed outside the owned paths

| File | Why |
|---|---|
| `internal/orders/seams.go` | `Credits` gains `Hold`, `Release`, `Complete` and `CreditsHold`; `NoCredits` implements them |
| `internal/orders/create.go` | hold after `InsertOrder`; map a refusal to the wallet problem or `price_changed` |
| `internal/orders/transitions.go` | release in `end()` (always, D-127); complete in `collect()` with the order's `cashback_bp` and write `cashback_credits`; the old TODO(lead) is removed |
| `internal/orders/create.go` (again), `db/queries/orders/orders.sql` | `InsertOrder` writes `cashback_bp` from the store (D-124) |
| `db/migrations/20202_w2-02_order_cashback_bp.sql` | **in W2-02's block.** The lead asked for an `orders.cashback_bp` column in my block, but `TestMigrationBlockOrder` (D-90) refuses a 201xx file that alters `orders` (20201). The lead approved option A (the column in orders' block, editing the orders files, since W2-02 is closed). Orders that already exist are backfilled from their store's rate; there is no NULL fallback |
| `cmd/todayz/credits_http_test.go` (new) | `TestCreditAccountHTTP` (D-125) and `TestCollectPaysPlacedCashbackHTTP` (D-124) |
| `cmd/todayz/routes_credits.go` | owned; now passes `business.NewStaffResolver` for the store-limited read |
| `docs/plan/01-product-rules.md` §7, `docs/plan/03-data-model.md` (`orders`) | the order's snapshotted rate is what collection pays (D-124) |
| `docs/plan/12-decision-log.md` | D-120–D-127 |
| `internal/orders/quote.go` | one comment |
| `internal/jobs/orders.go` | the timers' orders service gets the real ledger |
| `cmd/todayz/routes_orders.go` | `Credits: jobs.OrdersCredits(creditsService(d))` |
| `api/paths/orders.yaml` | the four credit problem codes on create; quote description |
| `internal/platform/notice/notice.go` | `KindCreditsEarned` and `KindCreditsExpiring` |
| `internal/notify/catalogue.go`, `catalogue_test.go` | the two notice kinds' catalogue entries |
| `go.mod` | `pgregory.net/rapid` v1.2.0 becomes a direct requirement for the CR-13 property tests (already in `go.sum`; 08-testing names it). `go mod tidy` also moved `golang.org/x/text` from indirect to direct, because `internal/search` already imports it |

The owned paths are `internal/credits`, `internal/jobs/credits.go`, `cmd/todayz/routes_credits.go`, `api/{paths,schemas,problems}/{wallet,business-credits}.yaml`, `db/queries/credits`, `db/migrations/201xx_*`, `platform/authz/policy/credits.go` with its golden file, and `docs/runbooks/credits-ledger.md`.

## Review

The lead's session did a read-only review ("merge with follow-ups"). Its fixes are in, each a decision-log entry:

| Item | Fix | Entry |
|---|---|---|
| 1. Store daily cap race | `Hold` locks the store row; concurrent test | D-120 |
| 2. Account first-use deadlock | accounts created in (kind, owner) order; lock-order comment fixed | D-121 |
| 3. Release in `end()` | unconditional | D-127 |
| 4. Poster keys | per-kind prefix; `ErrKeyReused` on a reused key with other legs | D-122 |
| 5. Grants | SECURITY DEFINER triggers and column grants (20102); the 20101 header no longer claims UPDATE is refused there | D-123 |
| 6. Cashback rate (Q1) | snapshotted on the order at placement | D-124 |
| 7. Store-limited managers (Q4) | the read shows only the member's stores; HTTP test | D-125 |
| 8. Q2, Q3, Q5, Q8 | conservative defaults; Q3 dedupe built (one flag per subject, rule and IST day) | D-126 |

No second independent review was done after these fixes.

## Questions (TODO(lead) in code where it applies)

1. ~~Cashback rate~~: resolved, snapshotted at placement in orders' block, approved by the lead (D-124).
2. ~~Deletion with held credits~~: resolved, refused with 409 (D-126).
3. ~~Risk-flag volume~~: resolved, one flag per subject, rule and IST day (D-126).
4. ~~Store-limited managers~~: resolved, they see only their stores (D-125).
5. ~~A short hold at create~~: resolved, `price_changed` stays (D-126).
6. **Complete without a hold** (an order with no credits) pays cashback only. Complete refuses with `ErrInvalid` when a hold exists for another customer or store.
7. **Partial accept is not built** in orders, so `Recap` is not wired. When BK-08–10 land, orders should call `Recap` in the accept path.
8. ~~Business-side notice~~: resolved, none yet (D-126).
9. **The "Ledger invariant" Grafana alert** (05 §6) is infra's to add. It should fire on `tz_ledger_reconcile_drift > 0` or a log line with `event=ledger_invariant`.
10. **POS reversal** (`TxReversal`) has a kind and a wallet line text but no operation. W2-04 owns how a refunded POS sale reverses a capture.
11. **Hot platform rows.** Platform balance rows are updated in every cashback and expiry. They are written last to keep lock time short. Under load, `platform_marketing` and `platform_expired` could become contention points. Consider a sharded or append-only platform balance.
12. **i18n keys.** The wallet problem codes (`api/problems/wallet.yaml`) and line `params` need app translations (W2-05 and the apps).
14. **Contract smoke on main (not W2-01).** See the evidence table: the reviews cursor 422 (W2-06) and the referral redirect (W2-03) fail `TestContractSmoke` here. The lead may want to see them on main before the next `make check`.
13. **Referrals on the real Poster.** W2-03 posts through its own `referrals.Ledger` seam (D-161). Wiring it to `credits.Poster` is a follow-up; its keys (`referral:<id>:<side>`) already satisfy D-122.

Decision-log entries: D-120–D-127.
