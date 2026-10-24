-- The store's cashback rate, snapshotted on the order when it is placed
-- (D-124, W2-01 at the founder's call). Collect pays this rate, not the
-- store's rate at collect time, so the customer gets the cashback the shop
-- showed when they booked. W2-01 writes it in orders' own block because a
-- 201xx file may not alter orders (D-90).
--
-- The default 0 is for a writer that does not know the column yet (an older
-- API during a deploy, a test fixture): such an order pays no cashback rather
-- than a rate nobody showed. InsertOrder always sets it. Orders that already
-- exist take their store's current rate; the backfill's UPDATE bumps their
-- updated_at once (the touch trigger); the lead accepted that (the orders
-- code reads no updated_at).

-- +goose Up
ALTER TABLE orders ADD COLUMN cashback_bp int NOT NULL DEFAULT 0;
UPDATE orders o SET cashback_bp = s.cashback_bp FROM stores s WHERE s.id = o.store_id;
ALTER TABLE orders ADD CONSTRAINT ck_orders_cashback_bp CHECK (cashback_bp BETWEEN 0 AND 2000);

-- +goose Down
ALTER TABLE orders DROP CONSTRAINT ck_orders_cashback_bp;
ALTER TABLE orders DROP COLUMN cashback_bp;
