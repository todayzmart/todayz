-- W4-02 order_burst checks (08 §6: timers late < 10 s p99; ledger reconcile clean).
--   psql "$DATABASE_MIGRATE_URL" -v ON_ERROR_STOP=1 -v since=<run start, ISO 8601> -f checks/order_burst.sql
-- Prints counts and durations only. Exits non-zero (ERROR) when a pass criterion fails.

\set ON_ERROR_STOP 1
\pset footer off

-- Orders the run placed at the load stores, by status.
SELECT o.status, count(*) AS orders
FROM orders o JOIN stores s ON s.id = o.store_id
WHERE s.slug LIKE 'load-%' AND o.placed_at >= :'since'::timestamptz
GROUP BY o.status ORDER BY o.status;

-- How late the accept timer fired: the expiry event against accept_by.
CREATE TEMP VIEW w402_lateness AS
SELECT extract(epoch FROM (e.created_at - o.accept_by)) AS late_s
FROM orders o
JOIN stores s ON s.id = o.store_id
JOIN order_events e ON e.order_id = o.id AND e.to_status = 'expired'
WHERE s.slug LIKE 'load-%' AND o.placed_at >= :'since'::timestamptz
  AND o.status = 'expired' AND o.status_reason = 'not_accepted';

SELECT count(*) AS timers_fired,
       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY late_s)::numeric, 2) AS late_p50_s,
       round(percentile_cont(0.99) WITHIN GROUP (ORDER BY late_s)::numeric, 2) AS late_p99_s,
       round(max(late_s)::numeric, 2) AS late_max_s,
       round(min(late_s)::numeric, 2) AS late_min_s
FROM w402_lateness;

-- Timers that never fired: still placed 10 s after accept_by.
SELECT count(*) AS timers_missed
FROM orders o JOIN stores s ON s.id = o.store_id
WHERE s.slug LIKE 'load-%' AND o.placed_at >= :'since'::timestamptz
  AND o.status = 'placed' AND o.accept_by < now() - interval '10 seconds';

-- The credits ledger reconcile (backend/db/queries/credits/credits.sql, Reconcile; 03 §6).
CREATE TEMP VIEW w402_reconcile AS
SELECT
  (SELECT count(*) FROM (SELECT transaction_id FROM ledger_entries
     GROUP BY transaction_id HAVING sum(amount) <> 0) x)::bigint AS unbalanced,
  (SELECT count(*) FROM account_balances
     WHERE kind IN ('customer_wallet', 'customer_held') AND balance < 0)::bigint AS negative,
  (SELECT count(*) FROM (
     SELECT a.owner_id, sum(b.balance) AS bal FROM ledger_accounts a
     JOIN account_balances b ON b.account_id = a.id
     WHERE a.kind IN ('customer_wallet', 'customer_held') GROUP BY a.owner_id) w
   FULL JOIN (SELECT customer_id, sum(remaining + held) AS n FROM credit_lots GROUP BY customer_id) l
     ON l.customer_id = w.owner_id
   WHERE coalesce(w.bal, 0) <> coalesce(l.n, 0))::bigint AS lots_vs_balances,
  (SELECT count(*) FROM credit_holds h
   LEFT JOIN (SELECT hold_id, sum(held_delta) AS n FROM credit_lot_consumptions
     WHERE hold_id IS NOT NULL GROUP BY hold_id) c ON c.hold_id = h.id
   WHERE coalesce(c.n, 0) <> CASE WHEN h.status = 'held' THEN h.amount - h.released ELSE 0 END)::bigint AS holds,
  (SELECT count(*) FROM (
     SELECT a.owner_id, b.balance FROM ledger_accounts a JOIN account_balances b ON b.account_id = a.id
     WHERE a.kind = 'customer_held') w
   FULL JOIN (SELECT customer_id, sum(amount - released) AS n FROM credit_holds
     WHERE status = 'held' GROUP BY customer_id) h ON h.customer_id = w.owner_id
   WHERE coalesce(w.balance, 0) <> coalesce(h.n, 0))::bigint AS held_vs_holds,
  (SELECT count(*) FROM account_balances b
   LEFT JOIN (SELECT account_id, sum(amount) AS s FROM ledger_entries GROUP BY account_id) e
     ON e.account_id = b.account_id
   WHERE b.balance <> coalesce(e.s, 0))::bigint AS cache_drift,
  (SELECT count(*) FROM credit_lots l
   LEFT JOIN (SELECT lot_id, sum(remaining_delta) AS r, sum(held_delta) AS h
     FROM credit_lot_consumptions GROUP BY lot_id) c ON c.lot_id = l.id
   WHERE l.remaining <> l.original + coalesce(c.r, 0) OR l.held <> coalesce(c.h, 0))::bigint AS lot_drift;

SELECT * FROM w402_reconcile;

-- Ledger rows the run made (cashback on collected orders), so a clean reconcile is not empty.
SELECT count(*) AS ledger_entries_in_run FROM ledger_entries WHERE created_at >= :'since'::timestamptz;

DO $$
DECLARE
  p99 numeric;
  missed bigint;
  r record;
BEGIN
  SELECT percentile_cont(0.99) WITHIN GROUP (ORDER BY late_s) INTO p99 FROM w402_lateness;
  SELECT count(*) INTO missed FROM orders o JOIN stores s ON s.id = o.store_id
   WHERE s.slug LIKE 'load-%' AND o.status = 'placed' AND o.accept_by < now() - interval '10 seconds';
  SELECT * INTO r FROM w402_reconcile;
  IF p99 IS NULL THEN
    RAISE EXCEPTION 'order_burst: no accept timer fired in the run (FAIL: nothing measured)';
  END IF;
  IF p99 >= 10 OR missed > 0 THEN
    RAISE EXCEPTION 'order_burst: timers late p99 % s, % missed (FAIL: p99 < 10 s, none missed)', round(p99, 2), missed;
  END IF;
  IF r.unbalanced + r.negative + r.lots_vs_balances + r.holds + r.held_vs_holds + r.cache_drift + r.lot_drift > 0 THEN
    RAISE EXCEPTION 'order_burst: ledger reconcile not clean (FAIL)';
  END IF;
  RAISE NOTICE 'order_burst: PASS (timers p99 % s, none missed, reconcile clean)', round(p99, 2);
END
$$;
