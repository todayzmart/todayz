# W2-02 Orders and booking: QA report

Branch `worktree-agent-a8a633d76272b610d`, rebased by the lead onto main a2ed6d1 and fast-forwarded. Commits e2332ee, 114538e, c2c334a, 0ea5ab4, 70df509, 022c643, f83ded9, ded483e, d818f53 (first hand-back), then a6ce916, 3cfa2fc, a5fef55, 2414eb1, 2eedcb3 (review fixes). Hashes in the agent sections below are from before the rebase.

## What was built
- **Contract** (`backend/api/paths|schemas|problems/orders.yaml`): 18 operations.
  - Customer:
    - `POST /v1/me/checkout/quote`;
    - `GET` and `POST /v1/me/orders`;
    - `GET /v1/me/orders/{orderId}`;
    - `cancel`, `confirm-partial`, `extend` and `new-code`.
  - Store (`/v1/biz/businesses/{bid}/stores/{sid}/orders…`):
    - list and get;
    - `accept`, `reject`, `ready`;
    - `dispatch`, `fail-delivery`, `confirm-return`;
    - `cancel`, `verify-code`.
  - Order problem codes are registered in `api/problems/orders.yaml`.
- **Schema** (`db/migrations/20201_w2-02_orders.sql`):
  - Tables: orders, order lines, order_events, stock reservations and strikes.
  - The guard trigger `trg_orders_guard` refuses any status move outside the state machine.
  - `ux_orders_store_code_active` keeps a store's open pickup codes unique.
- **Queries**: `db/queries/orders/orders.sql` (sqlc package `ordersdb`).
- **Service** (`internal/orders`):
  - **Quote and create.** A price or stock change after the quote is refused with `price_changed` (carrying the new quote) or `insufficient_stock` (carrying `storeItemId` and `available`). Create is idempotent.
  - **Transitions:**
    - shop side: accept, reject, ready, cancel and verify-code (hand-over), which is allowed from `accepted` or `ready`;
    - customer side: cancel, extend and new-code.
  - **Reads** for the customer and the store.
  - **The state machine** (`machine.go`) mirrors the DB guard.
  - **Cross-domain seams** go through small interfaces.
- **Pickup code:**
  - The code has 4 digits, drawn uniformly with crypto/rand. It is stored AES-GCM-sealed with an HMAC lookup key bound to the store.
  - It is returned only on the customer's own GET, while the order is `accepted` or `ready` and the code is not locked.
  - 5 wrong tries lock it. `new-code` replaces a locked code only.
- **Timers** (`internal/jobs/orders.go` and `internal/orders/timers.go`):
  - `order_accept_timeout` runs at `accept_by` and moves a `placed` order to `expired` with reason `not_accepted`.
  - `order_pickup_no_show` runs at `hold_until`. It moves the order to `expired` with reason `no_show`, releases the reservations, records one strike and applies the one-open-order limit when the strikes reach the threshold.
  - Both are enqueued in the same transaction as the move that starts them.
  - Both are idempotent. The no-show unique key includes the hold, so an extension enqueues its own timer.
  - A run before its deadline errors and River retries it.
- **Authz policy and golden**: `internal/platform/authz/policy/orders.go`.
- **Routes**: `cmd/todayz/routes_orders.go`.
- **Runbook**: `docs/runbooks/orders-timers.md`.
- **Notifications**: `order_update` with variants accepted, rejected, expired and ready. A shop cancel and a no-show send the generic text. A customer cancel and a collect send nothing.
- **Realtime**: an `outbox_events` row plus `pg_notify` for every transition.
- **Settings**: every configurable number is read through `settings.Cache`, including the accept window, the hold and extension, and `noshow_window_days`, `noshow_limit_days` and `noshow_strikes`. Money is integer paise.

## Evidence (agent)
All results are from `go test -race -count=1` against a testcontainers PostGIS database (colima), in `backend/cmd/todayz/orders_http_test.go`, through the real router.

| SC | Test | Result |
|---|---|---|
| BK-01 | TestBK01PlacePickupOrder | PASS |
| BK-02 | TestBK02InsufficientStockAfterQuote | PASS |
| BK-03 | TestBK03PriceChangedAfterQuote | PASS |
| BK-04 | TestBK04IdempotentCreate | PASS |
| BK-05 | TestBK05AcceptAll (a partial accept answers 422) | PASS |
| BK-06 | TestBK06AcceptTimeout | PASS |
| BK-07 | TestBK07Reject | PASS |
| BK-08–10 | Partial accept | Deferred |
| BK-11 | TestBK11CollectWithCode | PASS |
| BK-12 | TestBK12WrongCodesAndLock (4 × 422 `code_invalid` with `attemptsLeft`; the 5th is 409 `code_locked`; the right code is then also refused; `new-code` unlocks; the old code no longer works) | PASS |
| BK-13 | TestBK13CodeAfterExpiry | PASS |
| BK-14 | TestBK14NoShow | PASS |
| BK-15 | TestBK15Extend | PASS |
| BK-16 | — | Deferred |
| BK-17 | TestBK17CustomerCancel | PASS |
| BK-18 | TestBK18ShopCancel | PASS |
| BK-19 | TestBK19RaceCancelVersusAccept (8 rounds; observed cancel-first 7 times, accept-first once; always ends `cancelled` with nothing held; see Open) | PASS |
| BK-20 | TestBK20RaceExpiryVersusVerify (hold ± 1 s, 4 rounds) | PASS |
| BK-21 | TestBK21TimerLateAndDuplicate | PASS |
| BK-22 | TestBK22AutoAcceptStoreStaysPlaced | PASS (the store stays `placed`; auto-accept itself is deferred) |
| BK-23–25 | Delivery | Deferred (the routes answer 409 or 404) |
| — | TestOrdersBlockAccountDeletion (409 `account_has_active_order`; 204 after a cancel) | PASS |
| — | TestOrdersTenancyHTTP (401, 403 and 404 across tenants; a customer token on store routes; a cashier cancel is 403; the delivery routes give 409 for the order's own store, 404 for another store) | PASS |
| — | TestOrderCodeNeverLeaks (logs, order_events meta, notifications, outbox_events, river_job args, responses and problem bodies) | PASS |
| — | TestOrderTransitionTable (11 × 11 × 4 = 484 cells; the observed moves equal the plan minus the deferred moves) | PASS |
| — | TestMachineMatchesTheGuard (CanMove agrees with `trg_orders_guard` for pickup and delivery) | PASS |
| — | Authz policy golden; TestRoutesAreInTheContract; TestContractOperationsAreMounted | PASS |

Checks:
- **Static checks:** go vet clean; golangci-lint 0 issues; gofmt clean; gosec 0 findings; govulncheck 0; settings lint 0.
- **Contract:** `check-contract` passes. Redocly finds the bundle valid. The api-client checks and `TestContractSmoke`/`TestContractProblemCheckFires` (Schemathesis examples and coverage) pass, with the main checkout's venv, under no-egress.
- **`make check`, run once:** exit 2. It stopped at check-backend on the known `internal/search TestSearchAtScale/latency` failure (p95 511 ms > 300 ms). Every other backend package passed.
- **Steps after check-backend, run by hand under no-egress:**
  - Pass: frontend, i18n, ui, realtime, mocks, ci and auth.
  - Fail: infra and tf-static, only because Terraform is not initialised in the worktree.

## Fixed during QA
- **Settings lint:** the code draw used the literal `10_000`, which equals the seeded default of `credit_min_order_value_paise`. The value is now `codeSpace = 10^codeDigits`, computed from the digit count, with no suppression comment (a259a3e).
- **Test-harness corrections** (no product change):
  - The quote request no longer sends `expectedTotalPaise`.
  - The session re-logs in and refreshes after a clock move (the access token TTL is 15 min).
  - Problem extensions are read from `extensions`.
  - River args are read through the envelope (`args->'a'`).
  - `code: null` counts as no code.
  - `code_invalid` is 422.
  - A 31 s advance avoids the OTP resend gap in the deletion test.
- **contract_test.go:** gofmt applied, and `unmountedOperations` emptied once the orders routes were mounted.

## Contract and behaviour changes
- **`code_invalid` is 422** (the auth problem code). `code_locked` is 409. Once a code is locked, the right code is refused with `code_locked` too.
- **The plain code appears only on the customer's own `GET /v1/me/orders/{id}`**, and only while the order is `accepted` or `ready` and not locked. Everywhere else it is `"code": null`. No action response carries it.
- **`new-code`** replaces a locked code only: 409 for an unlocked code or a closed order.
- **Hand-over (`verify-code`) is allowed from `accepted` or `ready`** (lead ruling; `ready` is optional, 01 §6).
- **Deadlines are strict** (`!now.Before(deadline)`). After `accept_by` the shop cannot accept, and after `hold_until` it cannot hand over, even before the timer has run.
- **A partial accept is 422** until BK-08–10 land.
- **These routes answer 409 `invalid_transition`** (404 for an order the store cannot see) until their features are built: `dispatch`, `fail-delivery`, `confirm-return` and `confirm-partial`.
- **Lock order:** the order row first (`FOR UPDATE`), then the stock rows. Timers take the same lock, so races serialise.
- **An early timer run** fails with "timer ran before its deadline" and River retries it (up to 25 attempts).
- **Notifications:** generic text for a shop cancel and a no-show; nothing for a customer cancel or a collect.
- **An active order blocks account deletion:** 409 `account_has_active_order`.

## Open (TODO(lead))
1. **BK-19:**
   - The cancel always wins. If the accept commits first, the customer's cancel still cancels the accepted order and releases its reservation. If the cancel commits first, the accept gets 409.
   - For a cancel to fail when an accept beats it, the customer cancel needs an If-Match/version precondition. This needs a product decision.
2. Cashback is 0 until credits and offers land.
3. `AskPricer` is nil (`ask_price_expired` is not wired).
4. A partial accept returns 422.
5. `strikes_count_30d` goes stale; nothing recomputes it.
6. No POS `catalogue_changes` rows are written for order stock moves.
7. Store reliability and risk flags are not updated.
8. **No reconcile sweep.** A timer discarded after 25 attempts leaves the order past its deadline, with no recovery but by hand. The runbook says to tell the owner.
9. `codeTries = 5` is a constant (01 §6), not a setting.
10. The `order_update` catalogue has no `cancelled` or `no_show` variant.
11. Credits are deferred (BK-06, BK-11, BK-14).

## Deferred
- Delivery (BK-23–25) and D-69 returns.
- Partial accept and confirm-partial (BK-08–10).
- store_reliability and risk_flags.
- Real credits and offers.
- Auto-accept (BK-22 covers only the store staying `placed`).
- BK-16.
- Admin cancel.
- The timer reconcile sweep.
- `jobs.EscalationSources` is not wired, as the brief instructs.

## Not verified
- **Timers in a real worker:** tests drove them through the platform jobs Fake runner and the insert-only queue client. A live River worker never ran them, and neither did `make dev`.
- **Push, SMS and WhatsApp delivery:** only the notification rows were checked.
- **Realtime fan-out to a connected client:** only the outbox rows and the notify path were checked.
- **make e2e, the UI, and Playwright journeys:** not run, per the brief.
- **`check-infra` and `check-tf-static` under make:** Terraform is not initialised in the worktree; W2-02 touches no infra.
- **The contract checks inside `make check` itself:** that run stopped at check-backend on the known search failure. `check-contract` passed separately, using the main checkout's Schemathesis venv.
- **Load and latency of the order routes:** not measured.

## Review fixes (second hand-back, 2026-10-24)

A claude-reviewer pass over the first hand-back found no blockers. The pickup code (uniform crypto/rand draw, AES-GCM, store-bound HMAC, no leaks), tenancy, the state machine, money and settings were clean. The lead asked for five fixes.

| Test (`backend/cmd/todayz/orders_race_test.go` unless noted) | Proves |
|---|---|
| TestVerifyCodeParallelWrongCodes | 20 parallel wrong codes on one order: exactly 4 × 422 `code_invalid` (attemptsLeft 4, 3, 2, 1 once each) and 16 × 409 `code_locked`; `code_attempts` = 5; `code_locked_at` set; the right code then gets 409; the order stays accepted with its stock held |
| TestOrderCodeCollisionRedraws | Scripted randomness (`orders.Deps.CodeRand`, test-only) covers both redraw paths: the pre-check finding the code in use, and a unique violation on `ux_orders_store_code_active` after the pre-check passed (only the savepoint rolls back). Each redraw logs once with its reason, never the code |
| TestDoubleAcceptParallel | Cashier and manager accept one order in parallel, 4 rounds: one 200, one 409 `invalid_transition`; reserved rises once by the order's quantities; one accepted event, one no-show job |
| TestReleaseVersusAcceptNoDeadlock | Cancelling accepted order P while accepting order Q on the same two items: both 200, no deadlock. With the fix disabled, the accept answered 500 (deadlock) |
| TestStoreInboxUsesIndex | The 3-status inbox tab over 20,001 orders uses the new `ix_orders_store_placed` with no Sort |
| TestBK05AcceptAll (`orders_http_test.go`, extended) | An accept above the asked quantity is 422 "more than the order asked for"; the order stays placed |

The fixes:
1. **Concurrency tests:** the first four rows.
2. **Inbox index:** `ix_orders_store_placed (store_id, placed_at DESC, id DESC)` in migration 20201, dropped in its Down.
3. **Lock order:**
   - The agent audited every multi-item catalogue path and found none that can deadlock with accept.
   - The audit did find a real deadlock in the release at an end state: one UPDATE of the reservations moved `store_items.reserved` in scan order, not id order. `end()` now locks the held items in id order first. The package doc states the lock order: order row, then the stores row (hand-over only), then store_items in id order.
   - The POS multi-line sale and bulk import must follow the same order when they land.
4. **Over-quantity accept:** invalid value (422), separate from "a partial accept is not available yet".
5. **One enqueuer:** the order timers build one insert client lazily on first run, instead of one at registration, so the API opens no extra client.

Also: a `TODO(risk)` at the code-lock site in `verify`.

## Lead, at merge (2026-10-24)

**Rebase.** The branch was cut before 818e8ca, so its `make check` hit the old `TestSearchAtScale` latency failure. I rebased it onto main a2ed6d1 (no conflicts). After the rebase that test passes, so the "known failure" in the agent's evidence no longer applies.

**Rulings (D-108, committed with this merge in 623924c):**
- **BK-19 (Open item 1):** the behaviour the agent found is the rule. Cancel and accept serialise on the order row. If the cancel commits first, the accept gets 409. If the accept commits first, the cancel cancels the accepted order. The order ends cancelled with its holds released once. `08-testing.md` BK-19 now says this.
- **Deadlines are strict, and hand-over is allowed from accepted or ready:** confirmed.
- **Pickup code:** the agent's rules stand. The code is shown only on the customer's own GET while accepted or ready and unlocked. Wrong tries answer 422 `code_invalid` with `attemptsLeft`, and the 5th answers 409 `code_locked`. `new-code` is for a locked code only. `codeTries = 5` stays a constant, as 01 §6 fixes it.
- **The lockout risk flag** is deferred to the risk WP. It counts `orders.code_locked_at` per store per IST day.
- **Order totals are not frozen**, and the deferred list stands as written.

**Lead verification in the worktree, at 2eedcb3:**

| Check | Result |
|---|---|
| `make gen` | exit 0 |
| gofmt, `go vet ./...`, golangci-lint | clean, 0 issues |
| `go test -race -count=1` orders, cmd/todayz, jobs, platform/migrate, settingslint | all pass (cmd/todayz 82.9 s) |
| `check-contract` under `scripts/no-egress.sh` (main checkout's Schemathesis venv) | pass: 13 hook tests, 39 problem codes, contract smoke |
| Diff review of the fix commits | scope only orders, jobs/orders.go, migration 20201 and tests; nothing touches secrets or infra |

**Follow-ups:**
- **Escalation for unseen orders.** `jobs.EscalationSources` is still not wired. The orders side (`Unseen`) exists. The business side (`StoreContacts`: the store's contact phone, else the owner's) is next, with a test.
- **W2-05's `registernotify`** also builds an eager insert client. Move it to the same lazy pattern when that area is next touched.
- **`TestStoreInboxUsesIndex`** hand-copies the sqlc query, so it can drift from the real one.
- **The stack journeys** (`customer/e2e/order.stack.spec.ts`, `business/e2e/orders.stack.spec.ts`) run after the W2-09 and W2-10 merges.
