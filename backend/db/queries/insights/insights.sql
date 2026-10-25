-- insights queries, generated into package insightsdb (gen/sqlc/insightsdb).
-- Only W3-03 (internal/insights) edits this folder (D-50).
--
-- Every bound comes from Go (clock.Clock), never now(): from/to are instants,
-- the IST day bounds of the window the caller picked. A "bill" is a completed
-- POS sale or a completed online order (collected or delivered), the same as
-- W3-07's pair rollup. Rollup tables (rollup_daily_search_demand,
-- rollup_item_pairs, rollup_daily_store) are read here, never written.

------------------------------------------------------------------------------
-- Demand insights facts (internal/insights/facts)
------------------------------------------------------------------------------

-- name: StoreItems :many
-- Every item of the store with what the kinds need. first_movement_at is the
-- item's first stock movement, or its creation when it has none.
SELECT si.id,
       si.product_id,
       coalesce(si.local_name, p.name)::text AS name,
       (trim_scale(p.pack_size)::text || ' ' || p.unit)::text AS size,
       si.price_paise,
       coalesce(si.mrp_paise, p.mrp_paise, 0)::bigint AS mrp_paise,
       si.on_hand,
       si.reserved,
       si.low_stock_at,
       si.is_listed,
       si.stock_updated_at,
       si.created_at,
       coalesce((SELECT min(m.created_at) FROM stock_movements m WHERE m.store_item_id = si.id), si.created_at)::timestamptz AS first_stock_at
FROM store_items si
JOIN products p ON p.id = si.product_id
WHERE si.store_id = @store_id::uuid
ORDER BY si.id;

-- name: ItemSales :many
-- Units and bills per item in [from_at, to_at).
WITH lines AS (
  SELECT l.store_item_id, l.quantity::bigint AS qty, l.sale_id AS bill_id, l.line_total_paise AS paise
  FROM pos_sale_lines l
  JOIN pos_sales s ON s.id = l.sale_id
  WHERE s.store_id = @store_id::uuid AND s.status = 'completed' AND l.store_item_id IS NOT NULL
    AND s.sold_at >= @from_at::timestamptz AND s.sold_at < @to_at::timestamptz
  UNION ALL
  SELECT oi.store_item_id, oi.quantity_accepted::bigint, oi.order_id, oi.line_total_paise
  FROM order_items oi
  JOIN orders o ON o.id = oi.order_id
  WHERE o.store_id = @store_id::uuid AND o.status IN ('collected', 'delivered') AND coalesce(oi.quantity_accepted, 0) > 0
    AND o.placed_at >= @from_at::timestamptz AND o.placed_at < @to_at::timestamptz
)
SELECT store_item_id::uuid AS store_item_id,
       sum(qty)::bigint AS units,
       count(DISTINCT bill_id)::int AS bills,
       sum(paise)::bigint AS revenue_paise
FROM lines
GROUP BY store_item_id;

-- name: ItemDailyUnits :many
-- Units per item per IST day in [from_at, to_at) (the top items' sparkline).
WITH lines AS (
  SELECT l.store_item_id, l.quantity::bigint AS qty, s.sold_at AS at
  FROM pos_sale_lines l
  JOIN pos_sales s ON s.id = l.sale_id
  WHERE s.store_id = @store_id::uuid AND s.status = 'completed' AND l.store_item_id IS NOT NULL
    AND s.sold_at >= @from_at::timestamptz AND s.sold_at < @to_at::timestamptz
  UNION ALL
  SELECT oi.store_item_id, oi.quantity_accepted::bigint, o.placed_at
  FROM order_items oi
  JOIN orders o ON o.id = oi.order_id
  WHERE o.store_id = @store_id::uuid AND o.status IN ('collected', 'delivered') AND coalesce(oi.quantity_accepted, 0) > 0
    AND o.placed_at >= @from_at::timestamptz AND o.placed_at < @to_at::timestamptz
)
SELECT store_item_id::uuid AS store_item_id,
       (at AT TIME ZONE 'Asia/Kolkata')::date AS day,
       sum(qty)::bigint AS units
FROM lines
GROUP BY 1, 2;

-- name: LastSales :many
-- The last sale of each item before to_at.
WITH lines AS (
  SELECT l.store_item_id, s.sold_at AS at
  FROM pos_sale_lines l
  JOIN pos_sales s ON s.id = l.sale_id
  WHERE s.store_id = @store_id::uuid AND s.status = 'completed' AND l.store_item_id IS NOT NULL
    AND s.sold_at < @to_at::timestamptz
  UNION ALL
  SELECT oi.store_item_id, o.placed_at
  FROM order_items oi
  JOIN orders o ON o.id = oi.order_id
  WHERE o.store_id = @store_id::uuid AND o.status IN ('collected', 'delivered') AND coalesce(oi.quantity_accepted, 0) > 0
    AND o.placed_at < @to_at::timestamptz
)
SELECT store_item_id::uuid AS store_item_id, max(at)::timestamptz AS last_at
FROM lines
GROUP BY store_item_id;

-- name: OpenBatches :many
-- Batches with stock left, earliest expiry first (undated last).
SELECT b.id, b.store_item_id, b.quantity, b.expires_on, b.received_at, b.cost_paise
FROM stock_batches b
JOIN store_items si ON si.id = b.store_item_id
WHERE si.store_id = @store_id::uuid AND b.quantity > 0
ORDER BY b.expires_on NULLS LAST, b.received_at, b.id;

-- name: MovementDays :many
-- Stock movement deltas per item per IST day from from_at on, to rebuild the
-- closing stock of each day (on_hand minus every later delta).
SELECT m.store_item_id,
       (m.created_at AT TIME ZONE 'Asia/Kolkata')::date AS day,
       sum(m.delta)::bigint AS delta
FROM stock_movements m
WHERE m.store_id = @store_id::uuid AND m.created_at >= @from_at::timestamptz
GROUP BY 1, 2;

-- name: NearbyDemand :many
-- Searches per IST day and product (or normalised query when no product
-- matched) in the H3 cells whose centre is within radius_m of the store
-- (D-81), from W3-07's daily search demand rollup.
SELECT d.date AS day,
       d.product_id,
       d.query_norm,
       sum(d.searches)::bigint AS searches
FROM rollup_daily_search_demand d
JOIN insight_geo_cells c ON c.geo_cell = d.geo_cell
JOIN stores s ON s.id = @store_id
WHERE d.date >= @from_day::date AND d.date < @to_day::date
  AND ST_DWithin(c.centre, s.location, @radius_m::float8)
GROUP BY 1, 2, 3;

-- name: ProductNames :many
SELECT id, name, (trim_scale(pack_size)::text || ' ' || unit)::text AS size
FROM products WHERE id = ANY(@ids::uuid[]);

-- name: NearbyBusinesses :one
-- Distinct other businesses with a live store of a verified business within
-- radius_m of the store (the k of "No shop nearby has it" and the price
-- comparison). The store's own business never counts.
SELECT count(DISTINCT o.business_id)::int AS businesses
FROM stores s
JOIN stores o ON o.id <> s.id AND o.business_id <> s.business_id AND o.status = 'live'
JOIN businesses b ON b.id = o.business_id AND b.status = 'verified'
WHERE s.id = @store_id AND ST_DWithin(o.location, s.location, @radius_m::float8);

-- name: NearbyStocked :many
-- Of the given products, those that another nearby live verified store lists
-- with stock above 0.
SELECT DISTINCT oi.product_id
FROM stores s
JOIN stores o ON o.id <> s.id AND o.business_id <> s.business_id AND o.status = 'live'
JOIN businesses b ON b.id = o.business_id AND b.status = 'verified'
JOIN store_items oi ON oi.store_id = o.id AND oi.is_listed AND oi.on_hand - oi.reserved > 0
WHERE s.id = @store_id AND ST_DWithin(o.location, s.location, @radius_m::float8)
  AND oi.product_id = ANY(@product_ids::uuid[]);

-- name: NearbyMedianPrices :many
-- Median listed price of each of the store's listed products across the other
-- live verified stores within radius_m, only where those stores belong to at
-- least min_businesses distinct businesses (k ≥ 10, D-71).
SELECT mine.product_id,
       (percentile_cont(0.5) WITHIN GROUP (ORDER BY oi.price_paise))::bigint AS median_paise,
       count(DISTINCT o.business_id)::int AS businesses
FROM stores s
JOIN store_items mine ON mine.store_id = s.id AND mine.is_listed
JOIN stores o ON o.id <> s.id AND o.business_id <> s.business_id AND o.status = 'live'
JOIN businesses b ON b.id = o.business_id AND b.status = 'verified'
JOIN store_items oi ON oi.store_id = o.id AND oi.is_listed AND oi.product_id = mine.product_id
WHERE s.id = @store_id AND ST_DWithin(o.location, s.location, @radius_m::float8)
GROUP BY mine.product_id
HAVING count(DISTINCT o.business_id) >= @min_businesses::int;

-- name: StorePairs :many
-- The store's pairs from the latest nightly rollup (item A has the more bills).
SELECT store_item_a, store_item_b, bills_both, bills_a, bills_b, bills_total, lift_bp, window_days
FROM rollup_item_pairs
WHERE store_id = @store_id::uuid
ORDER BY lift_bp DESC, store_item_a, store_item_b;

-- name: CustomerVisits :many
-- One row per customer who bought at the store in [from_at, to_at): distinct
-- IST purchase days, the last one, bills and spend. A customer is the linked
-- account, else the phone of a phone-only POS link (D-07), verified or not;
-- a failed link, reversed POS sales
-- and walk-ins with no linked phone are not counted.
WITH tx AS (
  SELECT coalesce(s.customer_id::text, 'h:' || encode(s.link_phone_hash, 'hex')) AS ckey,
         s.customer_id, s.sold_at AS at, s.total_paise
  FROM pos_sales s
  WHERE s.store_id = @store_id::uuid AND s.status = 'completed'
    AND s.link_status IN ('linked', 'pending_verification')
    AND (s.customer_id IS NOT NULL OR s.link_phone_hash IS NOT NULL)
    AND s.sold_at >= @from_at::timestamptz AND s.sold_at < @to_at::timestamptz
  UNION ALL
  SELECT o.customer_id::text, o.customer_id, o.placed_at, o.total_paise
  FROM orders o
  WHERE o.store_id = @store_id::uuid AND o.status IN ('collected', 'delivered')
    AND o.placed_at >= @from_at::timestamptz AND o.placed_at < @to_at::timestamptz
)
SELECT ckey::text AS ckey,
       (array_agg(customer_id) FILTER (WHERE customer_id IS NOT NULL))[1]::uuid AS customer_id,
       count(DISTINCT (at AT TIME ZONE 'Asia/Kolkata')::date)::int AS visits,
       max((at AT TIME ZONE 'Asia/Kolkata')::date)::date AS last_day,
       max(at)::timestamptz AS last_at,
       count(*)::int AS bills,
       sum(total_paise)::bigint AS spend_paise
FROM tx
GROUP BY ckey;

-- name: QuietSubjects :many
SELECT kind, subject_ref FROM insight_feedback
WHERE store_id = @store_id::uuid AND quiet_until > @now::timestamptz;

-- name: UpsertFeedback :one
-- "Not useful": a subject still quiet keeps its quiet_until; a lapsed one is
-- renewed from now.
INSERT INTO insight_feedback (id, store_id, user_id, kind, subject_ref, quiet_until, created_at, updated_at)
VALUES (@id, @store_id, @user_id, @kind, @subject_ref, @quiet_until, @now, @now)
ON CONFLICT (store_id, kind, subject_ref) DO UPDATE SET
  quiet_until = CASE WHEN insight_feedback.quiet_until > excluded.created_at
                     THEN insight_feedback.quiet_until ELSE excluded.quiet_until END,
  user_id     = CASE WHEN insight_feedback.quiet_until > excluded.created_at
                     THEN insight_feedback.user_id ELSE excluded.user_id END,
  created_at  = CASE WHEN insight_feedback.quiet_until > excluded.created_at
                     THEN insight_feedback.created_at ELSE excluded.created_at END,
  updated_at  = excluded.updated_at
RETURNING quiet_until;

-- name: StoreItemExists :one
SELECT EXISTS (SELECT 1 FROM store_items WHERE id = @id AND store_id = @store_id)::bool;

-- name: StoreBatchItems :many
-- The batches among ids that belong to the store.
SELECT b.id, b.store_item_id
FROM stock_batches b
JOIN store_items si ON si.id = b.store_item_id
WHERE si.store_id = @store_id::uuid AND b.id = ANY(@ids::uuid[]);

-- name: ProductExists :one
SELECT EXISTS (SELECT 1 FROM products WHERE id = @id)::bool;

------------------------------------------------------------------------------
-- B-03 Overview
------------------------------------------------------------------------------

-- name: StoreHeader :one
SELECT s.name::text AS store_name, s.business_id, s.created_at
FROM stores s WHERE s.id = @store_id;

-- name: UserFirstName :one
SELECT coalesce(name, '')::text AS name FROM users WHERE id = @id;

-- name: SalesSummary :one
-- Sales (POS completed + online completed), bills, online orders placed in
-- [from_at, to_at).
SELECT
  (coalesce((SELECT sum(total_paise) FROM pos_sales
             WHERE store_id = @store_id::uuid AND status = 'completed'
               AND sold_at >= @from_at::timestamptz AND sold_at < @to_at::timestamptz), 0)
   + coalesce((SELECT sum(total_paise) FROM orders
               WHERE store_id = @store_id::uuid AND status IN ('collected', 'delivered')
                 AND placed_at >= @from_at::timestamptz AND placed_at < @to_at::timestamptz), 0))::bigint AS sales_paise,
  ((SELECT count(*) FROM pos_sales
    WHERE store_id = @store_id::uuid AND status = 'completed'
      AND sold_at >= @from_at::timestamptz AND sold_at < @to_at::timestamptz)
   + (SELECT count(*) FROM orders
      WHERE store_id = @store_id::uuid AND status IN ('collected', 'delivered')
        AND placed_at >= @from_at::timestamptz AND placed_at < @to_at::timestamptz))::int AS bills,
  (SELECT count(*) FROM orders
   WHERE store_id = @store_id::uuid AND status IN ('collected', 'delivered')
     AND placed_at >= @from_at::timestamptz AND placed_at < @to_at::timestamptz)::int AS online_bills,
  (SELECT count(*) FROM orders
   WHERE store_id = @store_id::uuid
     AND placed_at >= @from_at::timestamptz AND placed_at < @to_at::timestamptz)::int AS orders;

-- name: WaitingOrders :one
SELECT count(*)::int FROM orders WHERE store_id = @store_id::uuid AND status = 'placed';

-- name: SalesByHour :many
-- Sales per IST hour of [from_at, to_at).
WITH tx AS (
  SELECT sold_at AS at, total_paise FROM pos_sales
  WHERE store_id = @store_id::uuid AND status = 'completed'
    AND sold_at >= @from_at::timestamptz AND sold_at < @to_at::timestamptz
  UNION ALL
  SELECT placed_at, total_paise FROM orders
  WHERE store_id = @store_id::uuid AND status IN ('collected', 'delivered')
    AND placed_at >= @from_at::timestamptz AND placed_at < @to_at::timestamptz
)
SELECT extract(hour FROM at AT TIME ZONE 'Asia/Kolkata')::int AS hour, sum(total_paise)::bigint AS paise
FROM tx GROUP BY 1;

-- name: NewOrders :many
-- Orders waiting to be accepted, oldest first. The display number only, never
-- the pickup code.
SELECT o.id, o.display_number::text AS display_number, o.fulfilment, o.total_paise, o.accept_by, o.placed_at,
       coalesce(u.name, '')::text AS customer_name,
       coalesce((SELECT string_agg(
                   coalesce(oi.product_snapshot->>'name', '') ||
                   CASE WHEN oi.quantity_requested > 1 THEN ' ×' || oi.quantity_requested ELSE '' END,
                   ', ' ORDER BY oi.created_at, oi.id)
                 FROM order_items oi WHERE oi.order_id = o.id), '')::text AS summary
FROM orders o
LEFT JOIN users u ON u.id = o.customer_id
WHERE o.store_id = @store_id::uuid AND o.status = 'placed'
ORDER BY o.placed_at, o.id
LIMIT @lim;

-- name: LastStockChanges :many
-- The latest movement of each of the given items.
SELECT DISTINCT ON (store_item_id) store_item_id, source, created_at
FROM stock_movements
WHERE store_item_id = ANY(@ids::uuid[])
ORDER BY store_item_id, created_at DESC, id DESC;

-- name: SetupFacts :one
SELECT b.status::text AS business_status,
       (SELECT count(*) FROM store_items WHERE store_id = s.id AND is_listed)::int AS listed,
       (SELECT count(*) FROM staff st WHERE st.business_id = s.business_id AND st.status = 'active' AND st.role <> 'owner')::int AS staff,
       EXISTS (SELECT 1 FROM qr_standees q WHERE q.store_id = s.id)::bool AS has_standee,
       (s.hours <> '{}'::jsonb)::bool AS has_hours
FROM stores s
JOIN businesses b ON b.id = s.business_id
WHERE s.id = @store_id;

------------------------------------------------------------------------------
-- B-12 Analytics
------------------------------------------------------------------------------

-- name: DailySales :many
-- Online and POS sales per IST day in [from_at, to_at).
WITH tx AS (
  SELECT sold_at AS at, total_paise, false AS online FROM pos_sales
  WHERE store_id = @store_id::uuid AND status = 'completed'
    AND sold_at >= @from_at::timestamptz AND sold_at < @to_at::timestamptz
  UNION ALL
  SELECT placed_at, total_paise, true FROM orders
  WHERE store_id = @store_id::uuid AND status IN ('collected', 'delivered')
    AND placed_at >= @from_at::timestamptz AND placed_at < @to_at::timestamptz
)
SELECT (at AT TIME ZONE 'Asia/Kolkata')::date AS day,
       coalesce(sum(total_paise) FILTER (WHERE online), 0)::bigint AS online_paise,
       coalesce(sum(total_paise) FILTER (WHERE NOT online), 0)::bigint AS pos_paise,
       count(*) FILTER (WHERE online)::int AS online_bills,
       count(*) FILTER (WHERE NOT online)::int AS pos_bills
FROM tx GROUP BY 1 ORDER BY 1;

-- name: OrderFunnel :one
-- Orders placed in [from_at, to_at): received, accepted, completed and the
-- missed reasons; the store's median seconds to accept.
SELECT count(*)::int AS received,
       count(*) FILTER (WHERE accepted)::int AS accepted,
       count(*) FILTER (WHERE completed)::int AS completed,
       count(*) FILTER (WHERE status = 'rejected')::int AS rejected,
       count(*) FILTER (WHERE status = 'expired' AND status_reason = 'not_accepted')::int AS not_accepted,
       count(*) FILTER (WHERE status = 'expired' AND status_reason = 'no_show')::int AS no_show,
       count(*) FILTER (WHERE status = 'cancelled')::int AS cancelled,
       coalesce((percentile_cont(0.5) WITHIN GROUP (ORDER BY accept_seconds))::int, 0)::int AS p50_accept_seconds
FROM v_metric_orders
WHERE store_id = @store_id::uuid AND placed_at >= @from_at::timestamptz AND placed_at < @to_at::timestamptz;

-- name: NearbyAcceptMedian :one
-- Median of the daily p50 seconds to accept of other live verified stores
-- within radius_m, from W3-07's store rollup, with the number of distinct
-- businesses behind it (the B-12 benchmark needs k ≥ 5, D-71).
SELECT coalesce((percentile_cont(0.5) WITHIN GROUP (ORDER BY r.p50_accept_seconds))::int, 0)::int AS median_seconds,
       count(DISTINCT o.business_id)::int AS businesses
FROM stores s
JOIN stores o ON o.id <> s.id AND o.business_id <> s.business_id AND o.status = 'live'
JOIN businesses b ON b.id = o.business_id AND b.status = 'verified'
JOIN rollup_daily_store r ON r.store_id = o.id AND r.p50_accept_seconds IS NOT NULL
WHERE s.id = @store_id AND ST_DWithin(o.location, s.location, @radius_m::float8)
  AND r.date >= @from_day::date AND r.date < @to_day::date;

-- name: CategoryMix :many
-- Revenue per category of the items sold in [from_at, to_at).
WITH lines AS (
  SELECT l.store_item_id, l.line_total_paise AS paise
  FROM pos_sale_lines l
  JOIN pos_sales s ON s.id = l.sale_id
  WHERE s.store_id = @store_id::uuid AND s.status = 'completed' AND l.store_item_id IS NOT NULL
    AND s.sold_at >= @from_at::timestamptz AND s.sold_at < @to_at::timestamptz
  UNION ALL
  SELECT oi.store_item_id, oi.line_total_paise
  FROM order_items oi
  JOIN orders o ON o.id = oi.order_id
  WHERE o.store_id = @store_id::uuid AND o.status IN ('collected', 'delivered') AND coalesce(oi.quantity_accepted, 0) > 0
    AND o.placed_at >= @from_at::timestamptz AND o.placed_at < @to_at::timestamptz
)
SELECT coalesce(c.names ->> 'en', '')::text AS category, sum(lines.paise)::bigint AS paise
FROM lines
JOIN store_items si ON si.id = lines.store_item_id
JOIN products p ON p.id = si.product_id
LEFT JOIN categories c ON c.id = p.category_id
GROUP BY 1 ORDER BY 2 DESC, 1
LIMIT 8;

-- name: RatingsByWeek :many
-- Visible reviews per IST week (Monday) in [from_at, to_at).
SELECT date_trunc('week', created_at AT TIME ZONE 'Asia/Kolkata')::date AS week,
       count(*)::int AS reviews,
       avg(stars)::float8 AS avg_stars
FROM reviews
WHERE store_id = @store_id::uuid AND hidden_at IS NULL
  AND created_at >= @from_at::timestamptz AND created_at < @to_at::timestamptz
GROUP BY 1 ORDER BY 1;

------------------------------------------------------------------------------
-- B-09 Customers
------------------------------------------------------------------------------

-- name: CustomerNames :many
SELECT u.id, coalesce(u.name, '')::text AS name, pi.phone_enc
FROM users u
LEFT JOIN phone_identities pi ON pi.user_id = u.id
WHERE u.id = ANY(@ids::uuid[]);

-- name: PhoneLinks :many
-- The sealed phone of the latest phone-only POS link per phone hash.
SELECT DISTINCT ON (link_phone_hash) link_phone_hash, link_phone_enc
FROM pos_sales
WHERE store_id = @store_id::uuid AND link_phone_hash = ANY(@hashes::bytea[]) AND link_phone_enc IS NOT NULL
ORDER BY link_phone_hash, sold_at DESC;
