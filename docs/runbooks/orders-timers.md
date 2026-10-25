# Orders timers

Kinds `order_accept_timeout`, `order_pickup_no_show` and `order_partial_timeout` (owner W2-02, class `timer`, code in
`backend/internal/orders/timers.go`), their reconcile sweep `order_timer_reconcile`
(`backend/internal/orders/reconcile.go`) the daily `customer_strikes_recount` (`backend/internal/orders/strikes.go`) and the hourly `store_reliability` (`backend/internal/orders/reliability.go`), all declared in `backend/internal/jobs/orders.go`. The general
alert is [Job failing](jobs-job-failing.md) (`tz-jobs-failing-event`, critical for timers). The
sweep's own alert is `tz-order-reconcile-failed` (`infra/grafana/alerts/orders.yaml`, critical):
`tz_order_reconcile_failed_total{kind}` kept rising for 5 minutes, since a run that fails on some
orders still succeeds.

## What it means

`tz-order-reconcile-failed` fired: the `order_timer_reconcile` sweep failed to move some orders past their deadline for 5 minutes. Those orders stay `placed`, held or `awaiting_customer` after their time; customers and shops see them stuck. A timer kind failing outright is [Job failing](jobs-job-failing.md).

## First five minutes

1. Logs: the sweep's error line names the kind, the order and the error.
2. Count the stuck orders: `SELECT count(*) FROM orders WHERE sweep_failed_at IS NOT NULL` (read-only).
3. Open an [incident note](../ops/incident-template.md) and tell the owner and the lead. Do not edit `orders` by hand.

## What they do

| Kind | Enqueued by | Runs at | Outcome |
|---|---|---|---|
| `order_accept_timeout` | placing an order (same transaction) | `accept_by` | A still-`placed` order becomes `expired`, reason `not_accepted` (BK-06). The customer is told (`order_update`, variant `expired`). |
| `order_pickup_no_show` | accept, and each extension (same transaction) | `hold_until` | A held pickup order (`accepted`, `ready`) not collected becomes `expired`, reason `no_show` (BK-14). Its reservations are released, the customer gets one strike, and `noshow_limit_days` of the one-open-order limit once the strikes in `noshow_window_days` reach `noshow_strikes`. |
| `order_partial_timeout` | a partial accept (same transaction) | `partial_confirm_by` | A still-`awaiting_customer` order becomes `cancelled`, reason `partial_timeout` (BK-10). Nothing was reserved; its credit hold and offer redemptions are released. No strike for anyone. The customer is told (variant `generic`). |
| `order_timer_reconcile` | the schedule, every minute (one run per minute, one at a time) | — | Finds orders still `placed` past `accept_by`, `awaiting_customer` past `partial_confirm_by`, or held for pickup past `hold_until` (at the deadline itself, no grace), and applies the same outcome through the same code (BK-20, BK-21). Orders it never failed on first, oldest deadline first, then the ones it failed on, least recently failed first; at most 500 per deadline per run; the next minute takes the rest. Summary: `expired`, `noop`, `failed`. |
| `customer_strikes_recount` | the schedule, daily at 03:45 (class `maintenance`) | — | Recomputes every striked or limited customer's `strikes_count_30d` and `limited_until` from `strikes`, so a strike older than `noshow_window_days`, or reset, stops counting (01 §5.4). Each strike recounts its own customer at once; this run catches the ones that only aged out. A limit already past its end is cleared. Summary: `recounted` (rows changed). |
| `store_reliability` | the schedule, hourly at :20 (class `rollup`) | — | Recomputes each store's `accept_rate_30d`, `unaccepted_expiry_rate_30d` and `reliability_score` over the fixed 30-day window (D-85) and writes only those fields and `reliability_computed_at` (never `version`, the rating or the status). A store with nothing in the window goes back to NULL (ranking reads 1.0). Then raises the `store_strikes` risk flag for any store with `store_strike_risk_threshold` unreset store strikes in the window and no open flag; a flag cleared by support is not raised again from the same strikes, a newer strike raises it again. No automatic pause (D-67). An admin cancel with `strike: store` raises the flag at once; the score drops on the next run (BK-46). Summary: `stores` (rows changed), `flags` (raised). |

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
- The sweep and a timer that reach the same order serialise on its row lock; the second finds it
  moved on (`noop`). The sweep takes each order in its own transaction. An order it cannot
  apply is logged at error level with the kind and the order id, counted in `failed` and
  `tz_order_reconcile_failed_total{kind}`, marked `orders.sweep_failed_at` (the run's time), and
  tried again next minute behind every order that never failed, so orders that keep failing cannot
  hold newer ones past the 500 cap; the rest of the walk goes on. A run skips an order already
  marked at its own time.

Every expiry takes the order row lock first (`FOR UPDATE`), so a timer racing a shop's accept or a
hand-over (BK-19, BK-20) serialises with it: whichever commits first wins and the other sees the
new status.

## Diagnosis

1. **A-21 Jobs**: open the kind, then the latest failed run: the error, the attempt and the
   summary (`expired`, `noop`).
2. **The order**: read the `orders` row and its `order_events` (read-only): status,
   `accept_by` / `hold_until` / `partial_confirm_by`, and whether an `expired` or `cancelled` event is already there.
3. **Logs**: dashboard 15 *Job logs* with the run id; follow `trace_id`. Logs carry the order id
   only.

### Likely causes

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

## Mitigation

- Timer kinds are **never paused, run or retried by hand** (04 §2.7; A-21 refuses with
  `job_not_runnable`). Fix the cause; River's retries pick the run up.
- A run that exhausted its 25 attempts is discarded. The order stays in `placed` (or held, or
  `awaiting_customer`) past its deadline only until the next `order_timer_reconcile` run picks it
  up (within a minute of the deadline). If the sweep's `failed` count stays above 0
  (alert `tz-order-reconcile-failed`), its log line names the kind, the order and the error, and
  `SELECT id, status, sweep_failed_at FROM orders WHERE sweep_failed_at IS NOT NULL` (read-only)
  lists the orders it has failed on: fix the cause (often the same as the timer's) and the next
  minute applies it. `sweep_failed_at` stays set after; it only orders the sweep. Do not edit `orders` by hand (the guard
  trigger and the reservations must move together).
- Never edit `orders`, `order_events`, `stock_reservations`, `strikes` or `river_job` by hand.

## Escalation

**Level:** Critical: SEV2. SEV1 if many orders are stuck or stock stays reserved ([Severity levels](../ops/severity.md)).

- **W2-02** (owner) and the **lead** at once: this is a critical timer.
- The **support admin** if customers or shops saw an order stuck past its time, or a strike
  that should not be there.

## Follow-up

- A test with the order state that made the sweep fail.
- Support contacts customers and shops whose orders were stuck.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
