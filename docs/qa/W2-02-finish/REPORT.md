# W2-02 finish, orders and booking: QA report

This finishes the parts of W2-02 that were deferred: partial accept, the no-show order limit, auto-accept and the timer reconcile sweep. It covers the backend, the customer app and the business app.

- Branch: `claude/wp-w2-02-finish-7bedce`, based on 0e61d1e.
- Nothing was pushed or merged, and nothing touched a cloud account.
- Delivery (BK-23 to BK-27) and returns (D-69) were not started, as the brief asked.

## What was built

- **Partial accept, BK-08/09/10 (839d0ee, 9813c82, fd14ad1).**
  - Accepting fewer of some lines moves the order to `awaiting_customer` until `partial_confirm_by`.
  - The totals are recomputed on the accepted quantities:
    - offers scale pro rata;
    - credits are re-capped through `credits.Recap` (D-28);
    - the delivery fee is recomputed.
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
  - It finds orders past `accept_by`, `partial_confirm_by` or `hold_until` (pickup holds only), with 2 minutes of grace, and applies the timer's outcome through the same code the timers use.
  - Each order runs in its own transaction, so a late or duplicate run changes nothing (D-108).
  - Paging is keyset, 100 per page and at most 500 per walk per run.
  - The summary counts expired, noop and failed. A failed order is logged with its kind and order id only.
  - The runbook documents the sweep.
- **Screenshots (f68c2f3).**
  - The customer cases were added to `customer/e2e/transactions.shots.spec.ts`.
  - A new `business/e2e/orders.shots.spec.ts`, modelled on `sell.spec.ts`, covers the business side.
  - Both fake `/v1/**` with `page.route`; no stack is used.

No migration was added: the next free number in the 20201–20299 block is still 20203. Every number comes from `platform_settings` through `settings.Cache`, except the sweep grace (see Questions). Money is integer paise throughout.

## Evidence

| Check | Result |
|---|---|
| `make gen` | exit 0 |
| `gofmt -l`, `go vet ./...` | clean |
| `golangci-lint run ./...` (`.tools/bin`) | 0 issues |
| settings lint (`go run ./internal/devtools/settingslint .`) | clean |
| `go test -race -p 2 ./...` (shared test Postgres, `TODAYZ_TEST_PG_URL_FILE`) | ok in every package. `internal/platform/realtime` keeps its own container (`MainOwnServer`), so it ran separately with `DOCKER_HOST` set as `make check` does: ok |
| New Go tests | `TestBK22AutoAccept`, `TestBK21ReconcileSweep`, the partial tests in `orders_partial_http_test.go`, and BK-16 in `orders_http_test.go` |
| Contract (`make check-contract`: redocly lint, api-client lint/types/tests, hooks, `TestContractSmoke`) | exit 0. Redocly: valid, with 10 warnings, none in the orders files (unused components and existing examples). api-client: 42 tests passed. `TestContractSmoke` and `TestContractProblemCheckFires`: ok |
| customer lint, typecheck, unit tests | clean; 124 passed |
| business lint, lint:css, typecheck, unit tests | clean; 362 passed |
| `@todayz/i18n` tests | 197 passed |
| customer `transactions.shots.spec.ts` (built with `build:e2e`, run under `vite preview`) | 7 passed |
| business `orders.shots.spec.ts` (same setup) | 2 passed |

## Screenshots

These are in `docs/qa/W2-02-finish/`, at 390 px unless marked otherwise.

| File | Shows |
|---|---|
| `w2-02-accept-part-card.png` | B-04 desktop: a New card with one short line and one out-of-stock line. Accept is disabled; the hint and Accept part are shown |
| `w2-02-accept-part-sheet.png` | B-04 phone: the Accept part sheet, prefilled as 2, 1 and 0 |
| `w2-02-partial-confirm.png` | C-12: "Raju Kirana changed your order", with the items total, credits used, new total, countdown, Take it and Cancel |
| `w2-02-limit-banner.png` | C-11: the C-21 banner with its end date |

To refresh them, build first, then run the specs with `TZ_QA_DIR=docs/qa/W2-02-finish`, a placeholder `VITE_TURNSTILE_SITE_KEY`, and an https `VITE_API_URL` (the preview config refuses anything else):

- customer: `pnpm exec playwright test -c playwright.config.ts transactions.shots.spec.ts -g "part of the order|order limit"`;
- business: `pnpm exec playwright test -c playwright.config.ts orders.shots.spec.ts`.

Running the whole customer spec with `TZ_QA_DIR` also writes the W2-09 shots into this folder, so filter with `-g` as above.

## Skipped

- **Item 5 of the brief:**
  - admin cancel through the admin API (D-83);
  - the `store_reliability` and `risk_flags` writes.

  Both are not built. They need the admin domain's routes and the reliability tables' owners, which is more than this pass could finish safely.

## Open items

1. No stock is held while an order is `awaiting_customer`, so `confirm-partial` can answer 409 `insufficient_stock` if the stock went in the meantime. This is the conservative reading of D-28.
2. There is no `cancelled` notice variant, so a partial timeout or decline sends no customer notice beyond the order's own status.
3. `strikes_count_30d` is never decremented; nothing ages strikes out yet.
4. The C-21 banner hides against the time the client fetched `/v1/me`; the server's clock is the real gate.
5. On the B-04 desktop card, the three buttons wrap: Reject and Accept part sit on the first row and Accept on the second (see the card screenshot). This is a layout nit for the design pass.
6. The business `smoke.spec.ts` failures noted in W2-11 were not looked at here.

## Questions (TODO(lead))

1. **Offers at partial accept:** they scale pro rata instead of asking the offers engine again. Asking again would count this order's own redemption against the per-customer limit. As a result, a minimum-bill offer stays, scaled, when the smaller bill drops under its minimum. Is that right? (`internal/orders/partial.go`, `billFor`.)
2. **Credits at partial accept:** they are re-capped when the shop accepts part, as 03 says, not at confirm, as 01 reads. Which is meant?
3. **Auto-accept "stays placed":** create already refuses a cart with a short line under the item lock (BK-02), so at an auto-accept store a short order is refused with 409 instead of being placed for the shop. The stays-placed branch is only a backstop. Should a short order at an auto-accept store be placed for the shop instead?
4. **Sweep grace:** the 2 minutes are a code constant (`reconcileGrace`), not a `platform_settings` row, because 01 §17 has no such number. Should it become a setting?
5. **Accept part rules:** the order needs more than one unit in total, and Send is disabled until something is lowered (a full accept goes through Accept). Is that the intended rule?
6. **Accept part retries:** the Idempotency-Key is reused only on a network-failure retry, as `useOrderAction` does for the other actions. Confirm this.

## Not verified

- Nothing ran against the full local stack (`make dev`, `make e2e` and the stack journeys were out of bounds). The screenshots use a faked API.
- Real-time behaviour (SSE) of the new statuses in the browser was not checked; the specs answer the stream with an empty body.
- The sweep was tested with the fake jobs runner (`RunScheduled`), not a live River worker.
- There are no load or concurrency tests beyond the lock-order tests in the Go suite.

## Files changed outside the orders, checkout, customer-orders and business-orders areas

- `backend/internal/jobs/credits.go`: the `Recap` adapter.
- `backend/internal/jobs/orders.go`: registers `order_timer_reconcile`.
- `backend/cmd/todayz/orders_http_test.go`, `orders_partial_http_test.go`, `orders_reconcile_http_test.go`.
- `docs/runbooks/orders-timers.md`.
- `shared/i18n/messages/business/orders.en.json`, `shared/i18n/messages/customer/orders.en.json`, `shared/i18n/messages/customer/checkout.en.json`.
- `business/CLAUDE.md`: the B-04 Accept part line.
- `customer/src/home/Home.tsx` and `customer/src/cart/CartPage.tsx`: the banner and the dated error text.
