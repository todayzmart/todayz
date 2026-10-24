-- When the timers' reconcile sweep (order_timer_reconcile, BK-21) last failed
-- to apply this order's timer. The sweep walks orders that never failed first,
-- oldest deadline first, then the failed ones, least recently failed first,
-- and skips one that already failed in the same run, so orders that keep
-- failing cannot hold back newer due orders behind its per-run cap. NULL for
-- an order the sweep never failed on. Setting it bumps updated_at once (the
-- touch trigger); the orders code reads no updated_at and version is untouched.

-- +goose Up
ALTER TABLE orders ADD COLUMN sweep_failed_at timestamptz;

-- +goose Down
ALTER TABLE orders DROP COLUMN sweep_failed_at;
