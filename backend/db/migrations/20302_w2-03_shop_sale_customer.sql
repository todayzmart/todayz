-- W2-03: a new shop qualifies on distinct customers, not purchases (D-168).
--
-- referral_shop_sales.customer_id is the buyer of each counted purchase; the
-- count is count(DISTINCT customer_id). Every counted purchase has a known
-- customer (unlinked sales are never counted), but rows written before this
-- migration carry none: orders are backfilled from the order, and a POS row
-- without a customer simply stops counting.

-- +goose Up

ALTER TABLE referral_shop_sales ADD COLUMN customer_id uuid REFERENCES users (id);

UPDATE referral_shop_sales s SET customer_id = o.customer_id
FROM orders o WHERE s.ref_type = 'order' AND o.id = s.ref_id;

CREATE INDEX ix_referral_shop_sales_customer ON referral_shop_sales (referral_id, customer_id);

-- +goose Down

DROP INDEX ix_referral_shop_sales_customer;
ALTER TABLE referral_shop_sales DROP COLUMN customer_id;
