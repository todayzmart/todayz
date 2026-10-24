# Orders timers

Kinds `order_accept_timeout`, `order_pickup_no_show` and `order_partial_timeout` (owner W2-02, class `timer`, code in
`backend/internal/orders/timers.go`, declared in `backend/internal/jobs/orders.go`). The general
alert is [Job failing](jobs-job-failing.md) (`tz-jobs-failing-event`, critical for timers).

## What they do

| Kind | Enqueued by | Runs at | Outcome |
|---|---|---|---|
| `order_accept_timeout` | placing an order (same transaction) | `accept_by` | A still-`placed` order becomes `expired`, reason `not_accepted` (BK-06). The customer is told (`order_update`, variant `expired`). |
| `order_pickup_no_show` | accept, and each extension (same transaction) | `hold_until` | A held pickup order (`accepted`, `ready`) not collected becomes `expired`, reason `no_show` (BK-14). Its reservations are released, the customer gets one strike, and `noshow_limit_days` of the one-open-order limit once the strikes in `noshow_window_days` reach `noshow_strikes`. |
| `order_partial_timeout` | a partial accept (same transaction) | `partial_confirm_by` | A still-`awaiting_customer` order becomes `cancelled`, reason `partial_timeout` (BK-10). Nothing was reserved; its credit hold and offer redemptions are released. No strike for anyone. The customer is told (variant `generic`). |

The job arguments are the order id and, for the no-show timer, the hold it was set for. Never a
code, a phone or a customer id.

All three are idempotent (BK-21):

- A run for an order that has moved on (accepted, rejected, cancelled, collected, already
  expired) changes nothing and records `noop: 1`.
- A second run of the same timer is a no-op. The strike is unique per order, so a no-show is
  never counted twice.
- A no-show run for a hold that was extended since does nothing; the extension enqueued its
  own timer (its unique key includes the new hold).
- A late run still applies the right outcome from the order's current state.
- A run *before* its deadline (queue and clock disagree) fails with "timer ran before its
  deadline" and River retries it with backoff (up to 25 attempts). It changes nothing.

Every expiry takes the order row lock first (`FOR UPDATE`), so a timer racing a shop's accept or a
hand-over (BK-19, BK-20) serialises with it: whichever commits first wins and the other sees the
new status.

## First three checks

1. **A-21 Jobs**: open the kind, then the latest failed run: the error, the attempt and the
   summary (`expired`, `noop`).
2. **The order**: read the `orders` row and its `order_events` (read-only): status,
   `accept_by` / `hold_until` / `partial_confirm_by`, and whether an `expired` or `cancelled` event is already there.
3. **Logs**: dashboard 15 *Job logs* with the run id; follow `trace_id`. Logs carry the order id
   only.

## Likely causes

- **"timer ran before its deadline" on every attempt**: the worker's clock and the database
  disagree, or (local/test only) the test clock was moved back. It clears once the deadline
  passes.
- **503 "orders: not configured"**: the worker started without the database, settings, the
  insert-only queue client, or the order-code keys. Check `make env-check` names
  (`ORDER_CODE_ENC_KEY`, `ORDER_CODE_HMAC_KEY`, `PII_ENC_KEY`) and the worker's boot log.
- **A setting cannot be read** (`noshow_window_days`, `noshow_limit_days`,
  `noshow_strikes`): fix the row in `platform_settings` (A-20).
- **A trigger refused the move** (`trg_orders_guard`): the order is in a state the timer should
  have treated as a no-op. That is a bug; tell the owner with the order id.

## Fix

- Timer kinds are **never paused, run or retried by hand** (04 §2.7; A-21 refuses with
  `job_not_runnable`). Fix the cause; River's retries pick the run up.
- A run that exhausted its 25 attempts is discarded and nothing re-drives it. The order stays
  in `placed` (or held) past its deadline: the shop cannot accept or hand it over (deadlines are
  strict) and the customer can no longer cancel it.
  TODO(lead): there is no reconcile sweep yet that re-expires such orders. Until one exists, tell
  the owner with the order ids; do not edit `orders` by hand (the guard trigger and the
  reservations must move together).
- Never edit `orders`, `order_events`, `stock_reservations`, `strikes` or `river_job` by hand.

## Who to tell

- **W2-02** (owner) and the **lead** at once: this is a critical timer.
- The **support admin** if customers or shops saw an order stuck past its time, or a strike
  that should not be there.
