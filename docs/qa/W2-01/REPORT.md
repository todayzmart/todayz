# W2-01 Credits ledger: QA report

The double-entry credits ledger in `backend/internal/credits`, with the `wallet` and `business-credits` API domains. It is wired into orders for hold, release, capture and cashback. Branch `claude/wp-w2-01-credits-1bb47b`. Everything ran locally on testcontainers; nothing touched a cloud account, and `make dev`, `make reset` and `make e2e` were not run.

## What was built

- **Migration `20101_w2-01_credits.sql`** creates these tables:
  - `ledger_accounts`, `account_balances`, `ledger_transactions`, `ledger_entries`;
  - `credit_holds`, `credit_lots`, `credit_lot_consumptions`.

  It also does the following:
  - A deferred constraint trigger refuses a transaction whose entries do not sum to 0.
  - The platform accounts are seeded: `platform_marketing`, `platform_expired`, `platform_adjustments` and `platform_payouts_cash`.
  - `todayz_app` gets INSERT/SELECT only on accounts, transactions, entries and consumptions. It may UPDATE the balance, hold and lot rows, and may not DELETE them.
- **The service (`service.go`)** implements Holder and Poster in the caller's transaction:
  - **Lock order:** the customer and store balance rows first, in one statement in id order; then the hold; then the lots. Platform rows are not locked up front, and their entries are written last.
  - **Holds** draw from lots FIFO. A release returns credits to the same lots, latest expiry first. A lot that expired while held goes straight to `platform_expired` (D-27).
  - **Capture** moves the hold to `store_credit`. Cashback is paid on eligible − credits × 100, floored to whole credits, and creates a `cashback` lot.
  - **Caps:**
    - per order (`credit_max_per_order`);
    - the ₹1 floor;
    - `credit_min_order_value_paise`;
    - `credit_orders_per_month` per customer in the IST month;
    - `store_credit_redemptions_per_day` per store in the IST day.

    Every number is read from `platform_settings`. A cap refusal writes a `risk_flags` row outside the caller's transaction, so the flag survives a rollback.
  - **Idempotency:** every operation is idempotent per ref (hold, recap, release, complete), and every posting per `IdempotencyKey`.
  - **Notices:** `credits_earned` on cashback; one `credits_expiring` per customer for the lots due within `credit_expiry_reminder_days`.
- **Jobs**, registered in `internal/jobs/credits.go`:
  - `credits_expire`: money class, hourly at :05;
  - `credits_expiry_reminder`: notify class, daily at 10:00;
  - `ledger_reconcile`: money class, daily at 03:30. It only reads, and records `tz_ledger_reconcile_drift{reason}`.

  Other metrics: `tz_credits_cap_refusals_total` and `tz_credits_expired_total`. The runbook is `docs/runbooks/credits-ledger.md`.
- **Reads:**
  - `GET /v1/me/wallet`;
  - `GET /v1/me/wallet/transactions`: plain-language lines with params for i18n;
  - `GET /v1/biz/{businessId}/credit-account`: manager role, with each store's balance and lines.
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
type Completion struct { Ref Ref; CustomerID, StoreID ids.ID; EligiblePaise int64 }
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
	IdempotencyKey string // ≤ 200 chars; a repeat posts nothing
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

var ErrInsufficient, ErrInvalid, ErrUnavailable, ErrHeld error

// *Service also: OnAccountDelete(ctx, userID) error (identity hook),
// ExpireDue, RemindExpiring, Reconcile (jobs), Wallet, WalletLines,
// CreditAccount (reads), Rules(ctx) (Rules, error).
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
- **Every end state** goes through `end()`, which releases.
- **`collect()`** completes: it captures, pays cashback, and writes `cashback_credits` on the order.
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
| `go test -p 2 ./cmd/todayz/` | ok, 67 pass. The two contract-smoke tests skip in a plain `go test`; they ran under `make check-contract` (below) |
| `make gen` | exit 0 |

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
| CR-19 | `TestAppendOnlyDB` | 42501 on UPDATE or DELETE of entries, transactions, balances, consumptions, lots and holds |
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
| CR-30 (builder) | `TestHoldReleaseCaptureDB`, `TestPostDB`, `TestForfeitOnDeleteDB`, the property runs | **Idempotent repeats.** A repeated hold returns the hold; release and complete return what was posted first; `Post` with the same key gives `Duplicate: true` and writes nothing; a repeated forfeiture posts once |

## Files changed outside the owned paths

| File | Why |
|---|---|
| `internal/orders/seams.go` | `Credits` gains `Hold`, `Release`, `Complete` and `CreditsHold`; `NoCredits` implements them |
| `internal/orders/create.go` | hold after `InsertOrder`; map a refusal to the wallet problem or `price_changed` |
| `internal/orders/transitions.go` | release in `end()`; complete in `collect()` and write `cashback_credits`; the old TODO(lead) is removed |
| `internal/orders/quote.go` | one comment |
| `internal/jobs/orders.go` | the timers' orders service gets the real ledger |
| `cmd/todayz/routes_orders.go` | `Credits: jobs.OrdersCredits(creditsService(d))` |
| `api/paths/orders.yaml` | the four credit problem codes on create; quote description |
| `internal/platform/notice/notice.go` | `KindCreditsEarned` and `KindCreditsExpiring` |
| `internal/notify/catalogue.go`, `catalogue_test.go` | the two notice kinds' catalogue entries |
| `go.mod` | `pgregory.net/rapid` v1.2.0 becomes a direct requirement for the CR-13 property tests (already in `go.sum`; 08-testing names it). `go mod tidy` also moved `golang.org/x/text` from indirect to direct, because `internal/search` already imports it |

The owned paths are `internal/credits`, `internal/jobs/credits.go`, `cmd/todayz/routes_credits.go`, `api/{paths,schemas,problems}/{wallet,business-credits}.yaml`, `db/queries/credits`, `db/migrations/20101_*`, `platform/authz/policy/credits.go` with its golden file, and `docs/runbooks/credits-ledger.md`.

## Not reviewed

The brief asked for a `claude-reviewer` pass on this money package. The lead's later rule ("no sub-agents") made that impossible here, so **no independent review has been done**. The lead should review before merging, or give a follow-up the review.

## Questions (TODO(lead) in code where it applies)

1. **Cashback rate.** The rate is the store's `cashback_bp` at collect time, not at placement. If a shop changes its rate while an order is open, the new rate applies. The orders row does not snapshot it.
2. **Deletion with held credits.** It is refused with 409 `conflict` (`ErrHeld`). Orders already refuses a deletion with an unfinished order, so this only matters for a W2-04 bill-claim hold. Should a deletion release the hold first instead?
3. **Risk-flag volume.** Every refused hold at a cap writes one `risk_flags` row. A customer retrying at the cap writes several (`service.go`). Should flags be deduplicated per subject per day?
4. **Store-limited managers.** `credit-account` shows every store of the business to any manager. The plan does not say whether a manager limited to some stores should see only those (`handlers.go`).
5. **A short hold at create** (the balance dropped between quote and place) returns `price_changed` with a re-priced quote. There is no dedicated `no_balance` problem. Is that the intended UX?
6. **Complete without a hold** (an order with no credits) pays cashback only. Complete refuses with `ErrInvalid` when a hold exists for another customer or store.
7. **Partial accept is not built** in orders, so `Recap` is not wired. When BK-08–10 land, orders should call `Recap` in the accept path.
8. **Business-side notice.** The plan mentions a "credits earned" in-app line for the shop; only the customer's `credits_earned` notice is sent. Is a business notice wanted?
9. **The "Ledger invariant" Grafana alert** (05 §6) is infra's to add. It should fire on `tz_ledger_reconcile_drift > 0` or a log line with `event=ledger_invariant`.
10. **POS reversal** (`TxReversal`) has a kind and a wallet line text but no operation. W2-04 owns how a refunded POS sale reverses a capture.
11. **Hot platform rows.** Platform balance rows are updated in every cashback and expiry. They are written last to keep lock time short. Under load, `platform_marketing` and `platform_expired` could become contention points. Consider a sharded or append-only platform balance.
12. **i18n keys.** The wallet problem codes (`api/problems/wallet.yaml`) and line `params` need app translations (W2-05 and the apps).

No decision-log entries were added (D-120–D-129 are unused).
