# W2-02 finish, orders and booking: QA report

This finishes the parts of W2-02 that were deferred: partial accept, the no-show order limit, auto-accept, the timer reconcile sweep, admin cancel with fault side, store strikes and the `store_reliability` job. It covers the backend, the customer app and the business app. The second pass (from "Admin cancel" down) follows the lead's answers and scope items A, B and C. The third pass covers the lead's code review (items 1 to 7, below under "Review") and Q-1, CR-02.

- Branch: `claude/wp-w2-02-finish-7bedce`, based on 0e61d1e.
- Nothing was pushed or merged, and nothing touched a cloud account.
- Delivery (BK-23 to BK-27) and returns (D-69) were not started, as the brief asked.

## What was built

- **Partial accept, BK-08/09/10 (839d0ee, 9813c82, fd14ad1).**
  - Accepting fewer of some lines moves the order to `awaiting_customer` until `partial_confirm_by`.
  - Every bill figure is recomputed on the accepted quantities (7967a47, lead answers 1 and 2):
    - offers are re-priced, not scaled: `offers.Reprice` runs the engine again over the offers this order holds, as they stood when it was placed, without counting their caps again, so a minimum-bill offer drops off when the new item subtotal is under its minimum;
    - while the order waits, `credits_used` and the total show the credits the cap would keep (`credits.Kept`, a preview; the ledger hold stays whole);
    - the customer's accept re-caps the hold through `credits.Recap` and writes `credits_used` and the total; decline and timeout release everything as before;
    - the delivery fee stays 0: pickup only, `TODO(lead)` at `partial.go` `billFor` (delivery-WP follow-up below).
  - No stock is held while the order waits.
  - `confirm-partial` holds the accepted quantities and starts the pickup hold. It answers 409 `insufficient_stock` if a line went in the meantime.
  - Declining records `declined_partial`.
  - `order_partial_timeout` cancels with `partial_timeout` and gives no strike.
  - Accepting nothing is 422 on `body.lines`.
  - Business (B-04): Accept part is on the card and on the detail page, and opens `AcceptPartSheet`:
    - one stepper per line, prefilled with what the stock covers;
    - Send is disabled when every line is zero or when nothing was lowered;
    - every line is sent to `/accept` with an Idempotency-Key.
  - Customer (C-12): the partial confirm shows the changed lines, the items total, the credits used, the delivery fee and the new total.
- **No-show order limit, BK-16 (b08d3a8, fd14ad1).**
  - While `limited_until` is in the future, quote and place refuse a second unfinished order with `ordering_suspended`, carrying an `until` extension.
  - Place checks again under the customer row lock.
  - Customer: the C-21 banner shows on Home and Checkout from `/v1/me` `activeOrderLimit`.
  - The cart, quote and place errors give the end date when the server sends one.
- **Auto-accept, BK-22 (9c4dfcf).**
  - At a store with `auto_accept` on, create locks the cart's items before pricing (lock order per D-131/D-132: customer, then store_items in id order, then the ledger).
  - The order is then accepted by the system in the same transaction:
    - stock is held;
    - the pickup hold starts (`pickup_hold_min` from settings);
    - a `system` placed→accepted event is written;
    - an `order_update` notice goes to the customer, and the shop still gets `new_order`.
  - No accept timer is enqueued for an auto-accepted order.
- **Timer reconcile sweep, BK-20/21 (d15e416).**
  - `order_timer_reconcile` runs every minute through the jobs registry: PerTick, concurrency 1, class timer, runbook `orders-timers.md`.
  - It finds orders past `accept_by`, `partial_confirm_by` or `hold_until` (pickup holds only) and applies the timer's outcome through the same code the timers use. It acts at the deadline with no grace (dbfac6b, lead answer 4); since it runs every minute, the lag is at most a minute.
  - Every transition refuses after its deadline: `TestDeadlinesRefuseOneSecondLate` covers a shop accepting 1 s after `accept_by` and a customer confirming 1 s after `partial_confirm_by`.
  - Each order runs in its own transaction, so a late or duplicate run changes nothing (D-108).
  - Paging is keyset, 100 per page and at most 500 per walk per run. Orders the sweep never failed on come first, oldest deadline first, then the failed ones, least recently failed first (64ae657, review 3).
  - The summary counts expired, noop and failed. A failed order is logged with its kind and order id only, counted in `tz_order_reconcile_failed_total{kind}`, and marked `orders.sweep_failed_at` (migration 20203).
  - The runbook documents the sweep.
- **Admin cancel with fault side, item 5 / A (91e0bb8).**
  - `POST /v1/admin/orders/{orderId}/cancel` (pulled forward from W3-05 for the Wave 2 gate, D-83) takes a reason and a fault side (`customer`, `store` or none).
  - Support can cancel from any non-terminal state; stock, credits and offers are released as on any other cancel.
  - Only the side at fault gets an `admin_cancel` strike (01 §5.2), and the cancel is audited in the same transaction.
- **Store strikes and `store_reliability`, A (91e0bb8).**
  - A store strike is recorded once per order. At `store_strike_risk_threshold` unreset store strikes in the fixed 30-day window, one `store_strikes` row goes into `risk_flags`. There is no automatic pause (D-67).
  - A flag cleared by support is not raised again from the same strikes; a newer strike raises it again.
  - The hourly `store_reliability` job writes only `accept_rate_30d`, `unaccepted_expiry_rate_30d`, `reliability_score` and `reliability_computed_at`, and re-raises missed flags. Store strikes lower the score on its next run (BK-46).
  - Tests: `TestAdminCancelHTTP`, `TestBK46StoreStrikesFlagAndReliability`, `TestBK46ThresholdIsASetting`.
- **Rolling 30-day customer strike count, B (4fc6a30).**
  - `strikes_count_30d` and the no-show limit are recomputed from `strikes` (unreset, inside `noshow_window_days`) on every strike and by the daily `customer_strikes_recount` job, so aged-out and reset strikes stop counting.
  - Fake-clock tests: `TestBK16StrikesRoll` (on day 31 the day-0 strike has aged out and the limit is lifted), `TestBK16StrikeResetLiftsLimit`, `TestBK16LimitEnds` (the limit ends after `noshow_limit_days`; on day 31 all three strikes have aged out and one more no-show does not limit again, 53456a4).
- **B-04 desktop card, C (bea4b4c).** Reject, Accept part and Accept stay on one row; the card and sheet screenshots were refreshed.
- **Screenshots (f68c2f3).**
  - The customer cases were added to `customer/e2e/transactions.shots.spec.ts`.
  - A new `business/e2e/orders.shots.spec.ts`, modelled on `sell.spec.ts`, covers the business side.
  - Both fake `/v1/**` with `page.route`; no stack is used.

One migration was added: `20203_w2-02_order_sweep_failed_at.sql` (a nullable `orders.sweep_failed_at`; Down drops it). The next free number in the 20201–20299 block is 20204. Every number comes from `platform_settings` through `settings.Cache`; the 30-day store window is fixed (01 §17 "Fixed", D-85). Money is integer paise throughout.

## Review (lead's code review, items 1 to 7) and Q-1

| Item | What changed | Commit | Test |
|---|---|---|---|
| 1. Re-price, don't scale | `billFor` runs `offers.Reprice` on the kept lines; a minimum-bill offer under its minimum drops; a buy-X-get-Y line shrinks or drops with its groups | 7967a47, 6751b39 | `TestPartialAcceptRepricesOffers` (bill offer under its minimum), `TestPartialAcceptRepricesBuyXGetY` (6 kept as 4: two free become one; 3 kept as 2: none) |
| 2. Re-cap credits on confirm | Partial accept writes only a preview (`credits.Kept`); `confirm-partial` re-caps the hold (`credits.Recap`) | 7967a47 | `TestConfirmPartialRecapsCredits` |
| 3. Sweep starvation and failure metric | `orders.sweep_failed_at` (migration 20203). The Due* queries order never-failed orders first and skip an order already failed in the same run; a failure is counted in `tz_order_reconcile_failed_total{kind}`. Alert `tz-order-reconcile-failed` in the new `infra/grafana/alerts/orders.yaml`, modelled on `pos.yaml` (the precedent for a job that succeeds while its items fail); runbook updated | 64ae657 | `TestBK21ReconcileFailingOrderDoesNotBlockTheOthers`: the failing order does not stop the other two, the run succeeds with `failed: 1`, the next page order puts two newer orders before it, and once fixed the next run expires it |
| 4. Rounding | No proportional maths is left: every discount comes from the offers engine on the kept quantities, so nothing is scaled or rounded in `billFor` | 7967a47 | as item 1 |
| 5. `dispatch_by`, delivery fee | `TODO(lead)` at `b.deliveryFee = 0`; both listed below as delivery-WP follow-ups | 6751b39 | — |
| 6. Rolling count / `limited_until` | `CountActiveStrikes` is rolling (`created_at > since`), and so is `RecountCustomerStrikes`; the limit lifts after its days and the strikes leave the window | 53456a4 | `TestBK16LimitEnds` (extended), `TestBK16StrikesRoll` (day 31) |
| 7. Missing tests | ask-reply line and the bill-level offer under its minimum | 6751b39 | `TestPartialAcceptKeepsAskReplyPrice` (the replied ₹50 stays, no 10 % on it, the other line still gets it), `TestPartialAcceptRepricesOffers` |
| Q-1, CR-02 | Placing refuses credits it cannot use instead of clamping: over `credit_max_per_order` is 409 `credit_max_exceeded`, credits on a bill under the minimum is 409 `below_credit_minimum`, a wallet that no longer covers them is 409 `price_changed` with the current quote. The quote still clamps and reports `creditsLimit` (lead's answer to Q-1; decision-log D-216) | a9f1c32 | `TestCR02PlacingRefusesCreditsItCannotUse` |

Also 526750e: the settings lint flagged the literal 10000 (100 % in basis points) in `reliability.go`; it is now a named `bpWhole` constant with a `//settings:ok` reason.

### Delivery-WP follow-ups (review item 5)

- The reconcile sweep does not walk `dispatch_by`: no delivery state exists yet.
- The partial accept keeps the delivery fee at 0 (`TODO(lead)` in `billFor`); the delivery WP recomputes it on the kept lines.
- The D-69 72 h delivery-failed auto-confirm flag (see Skipped).

## Evidence

| Check | Result |
|---|---|
| All checks below were rerun on the final commit of this pass (53456a4 plus the docs commit) | |
| `make gen` | exit 0 |
| `gofmt -l`, `go vet ./...` | clean |
| `golangci-lint run ./...` (`.tools/bin`) | 0 issues |
| settings lint (`go run ./internal/devtools/settingslint .`) | clean |
| `go test -race -p 2 ./...` (shared test Postgres, `TODAYZ_TEST_PG_URL_FILE`) | ok in every package. `internal/platform/realtime` keeps its own container (`MainOwnServer`), so it ran separately with `DOCKER_HOST` set as `make check` does: ok |
| New Go tests | `TestBK22AutoAccept`, `TestBK21ReconcileSweep`, `TestBK21ReconcileFailingOrderDoesNotBlockTheOthers`, the partial tests in `orders_partial_http_test.go` (including `TestPartialAcceptRepricesOffers`, `TestPartialAcceptRepricesBuyXGetY`, `TestPartialAcceptKeepsAskReplyPrice` and `TestConfirmPartialRecapsCredits`), `TestCR02PlacingRefusesCreditsItCannotUse`, BK-16 in `orders_http_test.go` and `orders_strikes_http_test.go` (`TestBK16LimitEnds` now checks the limit lifts at day 31), `orders_admin_http_test.go`, `orders_deadline_http_test.go` |
| Contract (`make check-contract`: redocly lint, api-client lint/types/tests, hooks, `TestContractSmoke`) | exit 0. Redocly: valid, with 10 warnings, none in the orders files (unused components and existing examples). api-client: 42 tests passed. `TestContractSmoke` and `TestContractProblemCheckFires`: ok |
| customer lint, lint:css, typecheck, unit tests | clean; 124 passed |
| business lint, lint:css, typecheck, unit tests | clean; 362 passed |
| `@todayz/i18n` tests, `lint:messages` | 197 passed; clean |
| customer `transactions.shots.spec.ts` (second pass; no UI change since) (built with `build:e2e`, run under `vite preview`) | 7 passed |
| business `orders.shots.spec.ts` (same setup, second pass) | 2 passed |

## Screenshots

These are in `docs/qa/W2-02-finish/`, at 390 px unless marked otherwise.

| File | Shows |
|---|---|
| `w2-02-accept-part-card.png` | B-04 desktop: a New card with one short line and one out-of-stock line. Accept is disabled; the hint is shown, and Reject, Accept part and Accept sit on one row |
| `w2-02-accept-part-sheet.png` | B-04 phone: the Accept part sheet, prefilled as 2, 1 and 0 |
| `w2-02-partial-confirm.png` | C-12: "Raju Kirana changed your order", with the items total, credits used, new total, countdown, Take it and Cancel |
| `w2-02-limit-banner.png` | C-11: the C-21 banner with its end date |

To refresh them, build first, then run the specs with `TZ_QA_DIR=docs/qa/W2-02-finish`, a placeholder `VITE_TURNSTILE_SITE_KEY`, and an https `VITE_API_URL` (the preview config refuses anything else):

- customer: `pnpm exec playwright test -c playwright.config.ts transactions.shots.spec.ts -g "part of the order|order limit"`;
- business: `pnpm exec playwright test -c playwright.config.ts orders.shots.spec.ts`.

Running the whole customer spec with `TZ_QA_DIR` also writes the W2-09 shots into this folder, so filter with `-g` as above.

## Skipped

- **The D-69 risk flag on the 72 h delivery-failed auto-confirm.** That auto-confirm does not exist yet: no transition reaches `delivery_failed` (`internal/orders/seams.go`), and delivery (BK-23 to BK-27) and returns are out of scope here. The flag belongs with the delivery WP.

## Open items

1. No stock is held while an order is `awaiting_customer`, so `confirm-partial` can answer 409 `insufficient_stock` if the stock went in the meantime. The lead confirmed this is expected: reservations start at confirm.
2. There is no `cancelled` notice variant (the `order_update` catalogue belongs to W2-05), so a partial timeout or decline sends the general order notice only. The lead accepted this for now.
3. The C-21 banner hides against the time the client fetched `/v1/me`; the server's clock is the real gate.
4. The business `smoke.spec.ts` failures noted in W2-11 were not looked at here.

## Questions (TODO(lead))

The first pass's six questions are answered: re-price offers (1), re-cap credits at confirm with a preview while waiting (2), refuse a short line at every store (3), no sweep grace (4), and 5 and 6 are fine as built. Open now:

1. **Reliability formula** (`internal/orders/reliability.go`, `RecomputeStoreReliability`): 01 §5.4 and 05 §3 give the inputs but not the formula. As built, the score starts from the 30-day accept rate (1 with no answered order), loses the expiry rate as a factor above `shop_expiry_penalty_threshold_bp`, and loses half at `store_strike_risk_threshold` unreset store strikes, all of it at twice that. Confirm or give the formula.
2. **Store flag re-raise:** a `store_strikes` flag that support cleared is raised again only by a strike newer than the flag. Is that the rule?
3. **Dropped offer's redemption** (`partial.go`, `billFor`): when a re-priced offer drops off (say a minimum-bill offer under its minimum), its redemption stays counted until the order ends, because the offers engine has no partial release. Collected keeps it; any other end releases it. Is that acceptable, or should offers gain a partial release?
4. **`sweep_failed_at` in the data model:** 03-data-model.md does not list the new `orders.sweep_failed_at`. I did not edit the plan; add the column there or tell me to.

## Not verified

- Nothing ran against the full local stack (`make dev`, `make e2e` and the stack journeys were out of bounds). The screenshots use a faked API.
- Real-time behaviour (SSE) of the new statuses in the browser was not checked; the specs answer the stream with an empty body.
- The sweep was tested with the fake jobs runner (`RunScheduled`), not a live River worker.
- There are no load or concurrency tests beyond the lock-order tests in the Go suite.

## Files changed outside the orders, checkout, customer-orders and business-orders areas

- `backend/internal/offers/price.go` (W2-07): `Reprice`, the order's held offers re-run at its placing time.
- `backend/internal/credits/service.go` (W2-01): `Kept`, the cap preview (writes nothing).
- `backend/cmd/todayz/offers_seams.go`: the `Reprice` adapter.
- `backend/internal/jobs/credits.go`: the `Recap` and `Kept` adapters.
- `backend/internal/jobs/orders.go`: registers `order_timer_reconcile`, `store_reliability` and `customer_strikes_recount`.
- `backend/api/paths/admin-orders.yaml`, `backend/api/schemas/admin-orders.yaml`: the admin cancel operation.
- `backend/internal/platform/authz/policy/orders.go` and `testdata/orders.golden`: the admin cancel policy.
- `backend/cmd/todayz/routes_orders.go`: mounts the admin cancel.
- `backend/cmd/todayz/orders_http_test.go`, `orders_partial_http_test.go`, `orders_reconcile_http_test.go`, `orders_admin_http_test.go`, `orders_strikes_http_test.go`, `orders_deadline_http_test.go`.
- `customer/src/orders/PartialConfirm.tsx`: a comment only.
- `docs/runbooks/orders-timers.md`: the sweep order, `sweep_failed_at`, the metric and the alert.
- `infra/grafana/alerts/orders.yaml` (new): the `tz-order-reconcile-failed` alert.
- `backend/db/migrations/20203_w2-02_order_sweep_failed_at.sql` (new).
- `backend/cmd/todayz/orders_credits_http_test.go` (new): CR-02.
- `docs/plan/12-decision-log.md`: D-216 (quote informational, place refuses). Main already has D-214 and D-215; renumber at merge if another branch took D-216.
- `shared/i18n/messages/business/orders.en.json`, `shared/i18n/messages/customer/orders.en.json`, `shared/i18n/messages/customer/checkout.en.json`.
- `business/CLAUDE.md`: the B-04 Accept part line.
- `customer/src/home/Home.tsx` and `customer/src/cart/CartPage.tsx`: the banner and the dated error text.
