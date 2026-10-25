-- rollups queries, generated into package rollupsdb (gen/sqlc/rollupsdb).
-- Only W3-07 (internal/rollups) edits this folder (D-50).
--
-- Every query takes the IST day and its bounds from Go (clock.Clock), never
-- now(): day is the IST date and [day_start, day_end) its instants. Go also
-- computes every cutoff (stale_before, fresh_after, week_from, …) from the
-- time "now" means for the day: the job's clock time, or the day's end for a
-- past day. The metric definitions live in the v_metric_* views (30701).

------------------------------------------------------------------------------
-- rollup_today / rollup_day
------------------------------------------------------------------------------

-- name: UpsertPlatformDay :exec
-- Retention: base = customers whose first customer-app open was 1, 7 or 30
-- days before the day; back = those of them who opened the app on the day.
WITH
o AS (
  SELECT count(*)::int AS placed,
         count(*) FILTER (WHERE accepted)::int AS accepted,
         count(*) FILTER (WHERE completed)::int AS completed,
         count(*) FILTER (WHERE status = 'collected')::int AS collected,
         count(*) FILTER (WHERE status = 'delivered')::int AS delivered,
         count(*) FILTER (WHERE status = 'rejected')::int AS rejected,
         count(*) FILTER (WHERE status = 'expired')::int AS expired,
         count(*) FILTER (WHERE status = 'expired' AND status_reason = 'not_accepted')::int AS expired_unaccepted,
         count(*) FILTER (WHERE status = 'expired' AND status_reason = 'no_show')::int AS no_show,
         count(*) FILTER (WHERE status = 'cancelled')::int AS cancelled,
         count(*) FILTER (WHERE status = 'delivery_failed')::int AS delivery_failed,
         count(*) FILTER (WHERE status IN ('placed', 'awaiting_customer', 'accepted', 'ready', 'out_for_delivery'))::int AS open,
         coalesce(sum(total_paise) FILTER (WHERE completed), 0)::bigint AS gmv
  FROM v_metric_orders
  WHERE placed_at >= @day_start::timestamptz AND placed_at < @day_end::timestamptz
),
p AS (
  SELECT count(*)::int AS sales,
         count(*) FILTER (WHERE linked)::int AS linked,
         coalesce(sum(total_paise) FILTER (WHERE linked), 0)::bigint AS gmv,
         coalesce(sum(total_paise) FILTER (WHERE NOT linked), 0)::bigint AS unlinked
  FROM v_metric_pos_sales
  WHERE sold_at >= @day_start::timestamptz AND sold_at < @day_end::timestamptz
),
opens AS (
  SELECT count(DISTINCT ao.actor_id) FILTER (WHERE ao.occurred_at >= @day_start::timestamptz)::int AS dau,
         count(DISTINCT ao.actor_id) FILTER (WHERE ao.occurred_at >= @week_from::timestamptz)::int AS wau,
         count(DISTINCT ao.actor_id)::int AS mau
  FROM v_metric_app_opens ao
  WHERE ao.app = 'customer' AND ao.occurred_at >= @month_from::timestamptz AND ao.occurred_at < @day_end::timestamptz
),
tx AS (
  SELECT count(DISTINCT customer_id)::int AS customers
  FROM v_metric_transactions
  WHERE at >= @day_start::timestamptz AND at < @day_end::timestamptz
),
newc AS (
  SELECT count(*)::int AS n FROM customer_profiles WHERE created_at >= @day_start::timestamptz AND created_at < @day_end::timestamptz
),
cr AS (
  SELECT
    (SELECT coalesce(sum(original), 0) FROM credit_lots WHERE earned_at >= @day_start::timestamptz AND earned_at < @day_end::timestamptz)::bigint AS issued,
    (SELECT coalesce(sum(-e.amount), 0)
     FROM ledger_entries e
     JOIN ledger_transactions t ON t.id = e.transaction_id AND t.kind = 'spend_capture'
     JOIN ledger_accounts a ON a.id = e.account_id AND a.kind IN ('customer_wallet', 'customer_held')
     WHERE e.amount < 0 AND e.created_at >= @day_start::timestamptz AND e.created_at < @day_end::timestamptz)::bigint AS spent,
    (SELECT coalesce(sum(-e.amount), 0)
     FROM ledger_entries e
     JOIN ledger_transactions t ON t.id = e.transaction_id AND t.kind = 'expiry'
     JOIN ledger_accounts a ON a.id = e.account_id AND a.kind = 'customer_wallet'
     WHERE e.amount < 0 AND e.created_at >= @day_start::timestamptz AND e.created_at < @day_end::timestamptz)::bigint AS expired
),
s AS (
  SELECT count(*)::int AS searches, count(*) FILTER (WHERE NOT has_results)::int AS zero
  FROM v_metric_searches
  WHERE created_at >= @day_start::timestamptz AND created_at < @day_end::timestamptz
),
views AS (
  SELECT count(*)::int AS n FROM events
  WHERE name = 'store_viewed' AND occurred_at >= @day_start::timestamptz AND occurred_at < @day_end::timestamptz
),
firsts AS (
  SELECT a.actor_id, a.date
  FROM v_metric_app_opens a
  WHERE a.app = 'customer' AND a.actor_id IS NOT NULL
    AND (a.date = @d1_day::date OR a.date = @d7_day::date OR a.date = @d30_day::date)
    AND NOT EXISTS (SELECT 1 FROM v_metric_app_opens b
                    WHERE b.app = 'customer' AND b.actor_id = a.actor_id AND b.date < a.date)
  GROUP BY a.actor_id, a.date
),
back AS (
  SELECT DISTINCT actor_id FROM v_metric_app_opens
  WHERE app = 'customer' AND occurred_at >= @day_start::timestamptz AND occurred_at < @day_end::timestamptz
),
ret AS (
  SELECT count(*) FILTER (WHERE f.date = @d1_day::date)::int AS d1_base,
         count(*) FILTER (WHERE f.date = @d1_day::date AND b.actor_id IS NOT NULL)::int AS d1_back,
         count(*) FILTER (WHERE f.date = @d7_day::date)::int AS d7_base,
         count(*) FILTER (WHERE f.date = @d7_day::date AND b.actor_id IS NOT NULL)::int AS d7_back,
         count(*) FILTER (WHERE f.date = @d30_day::date)::int AS d30_base,
         count(*) FILTER (WHERE f.date = @d30_day::date AND b.actor_id IS NOT NULL)::int AS d30_back
  FROM firsts f LEFT JOIN back b ON b.actor_id = f.actor_id
),
st AS (
  SELECT count(*) FILTER (WHERE verified)::int AS verified,
         count(*) FILTER (WHERE live_at >= @day_start::timestamptz AND live_at < @day_end::timestamptz AND verified)::int AS new_stores
  FROM v_metric_store_supply
),
act AS (
  SELECT count(DISTINCT store_id)::int AS active FROM v_metric_store_activity
  WHERE at >= @day_start::timestamptz AND at < @day_end::timestamptz
),
live AS (
  SELECT count(*)::int AS live
  FROM v_metric_store_supply sp
  WHERE sp.status = 'live' AND sp.verified AND sp.meets_item_floor
    AND sp.stock_updated_at >= @stale_before::timestamptz
    AND EXISTS (SELECT 1 FROM v_metric_store_activity a
                WHERE a.store_id = sp.store_id AND a.at >= @day_start::timestamptz AND a.at < @day_end::timestamptz)
)
INSERT INTO rollup_daily_platform (
  date, dau, wau, mau, new_customers, transacting_customers,
  orders_placed, orders_accepted, orders_completed, orders_collected, orders_delivered, orders_rejected,
  orders_expired, orders_expired_unaccepted, orders_no_show, orders_cancelled, orders_delivery_failed, orders_open,
  gmv_paise, pos_gmv_paise, pos_sales, pos_sales_linked, pos_unlinked_paise,
  credits_issued, credits_spent, credits_expired,
  searches, searches_zero, zero_result_rate_bp, store_views,
  ret_d1_base, ret_d1_back, ret_d7_base, ret_d7_back, ret_d30_base, ret_d30_back,
  verified_stores, active_stores, live_stores, new_stores, final, computed_at)
SELECT @on_date::date, opens.dau, opens.wau, opens.mau, newc.n, tx.customers,
       o.placed, o.accepted, o.completed, o.collected, o.delivered, o.rejected,
       o.expired, o.expired_unaccepted, o.no_show, o.cancelled, o.delivery_failed, o.open,
       o.gmv, p.gmv, p.sales, p.linked, p.unlinked,
       cr.issued, cr.spent, cr.expired,
       s.searches, s.zero, CASE WHEN s.searches > 0 THEN (s.zero * 10000 / s.searches)::int END, views.n,
       ret.d1_base, ret.d1_back, ret.d7_base, ret.d7_back, ret.d30_base, ret.d30_back,
       st.verified, act.active, live.live, st.new_stores, @final::bool, @computed_at::timestamptz
FROM o, p, opens, tx, newc, cr, s, views, ret, st, act, live
ON CONFLICT (date) DO UPDATE SET
  dau = excluded.dau, wau = excluded.wau, mau = excluded.mau, new_customers = excluded.new_customers,
  transacting_customers = excluded.transacting_customers,
  orders_placed = excluded.orders_placed, orders_accepted = excluded.orders_accepted,
  orders_completed = excluded.orders_completed, orders_collected = excluded.orders_collected,
  orders_delivered = excluded.orders_delivered, orders_rejected = excluded.orders_rejected,
  orders_expired = excluded.orders_expired, orders_expired_unaccepted = excluded.orders_expired_unaccepted,
  orders_no_show = excluded.orders_no_show, orders_cancelled = excluded.orders_cancelled,
  orders_delivery_failed = excluded.orders_delivery_failed, orders_open = excluded.orders_open,
  gmv_paise = excluded.gmv_paise, pos_gmv_paise = excluded.pos_gmv_paise, pos_sales = excluded.pos_sales,
  pos_sales_linked = excluded.pos_sales_linked, pos_unlinked_paise = excluded.pos_unlinked_paise,
  credits_issued = excluded.credits_issued, credits_spent = excluded.credits_spent,
  credits_expired = excluded.credits_expired, searches = excluded.searches, searches_zero = excluded.searches_zero,
  zero_result_rate_bp = excluded.zero_result_rate_bp, store_views = excluded.store_views,
  ret_d1_base = excluded.ret_d1_base, ret_d1_back = excluded.ret_d1_back,
  ret_d7_base = excluded.ret_d7_base, ret_d7_back = excluded.ret_d7_back,
  ret_d30_base = excluded.ret_d30_base, ret_d30_back = excluded.ret_d30_back,
  verified_stores = excluded.verified_stores, active_stores = excluded.active_stores,
  live_stores = excluded.live_stores, new_stores = excluded.new_stores,
  final = excluded.final, computed_at = excluded.computed_at;

-- name: UpsertStoreDays :execrows
-- One row per store of a verified business, and per store with orders or POS
-- sales that day. conversions = completed orders + linked POS sales.
WITH
o AS (
  SELECT store_id,
         count(*)::int AS orders,
         count(*) FILTER (WHERE accepted)::int AS accepted,
         count(*) FILTER (WHERE completed)::int AS completed,
         count(*) FILTER (WHERE status = 'rejected')::int AS rejected,
         count(*) FILTER (WHERE status = 'expired' AND status_reason = 'not_accepted')::int AS expired_unaccepted,
         count(*) FILTER (WHERE status = 'expired' AND status_reason = 'no_show')::int AS no_shows,
         count(*) FILTER (WHERE status = 'cancelled')::int AS cancelled,
         count(*) FILTER (WHERE status = 'delivery_failed')::int AS delivery_failed,
         coalesce(sum(total_paise) FILTER (WHERE completed), 0)::bigint AS gmv,
         avg(accept_seconds)::int AS avg_accept,
         (percentile_cont(0.5) WITHIN GROUP (ORDER BY accept_seconds))::int AS p50_accept,
         (percentile_cont(0.9) WITHIN GROUP (ORDER BY accept_seconds))::int AS p90_accept
  FROM v_metric_orders
  WHERE placed_at >= @day_start::timestamptz AND placed_at < @day_end::timestamptz
  GROUP BY store_id
),
p AS (
  SELECT store_id,
         count(*)::int AS sales,
         count(*) FILTER (WHERE linked)::int AS linked,
         coalesce(sum(total_paise) FILTER (WHERE linked), 0)::bigint AS gmv,
         coalesce(sum(total_paise) FILTER (WHERE NOT linked), 0)::bigint AS unlinked
  FROM v_metric_pos_sales
  WHERE sold_at >= @day_start::timestamptz AND sold_at < @day_end::timestamptz
  GROUP BY store_id
),
mv AS (
  SELECT store_id, count(*)::int AS n FROM stock_movements
  WHERE created_at >= @day_start::timestamptz AND created_at < @day_end::timestamptz
  GROUP BY store_id
),
fresh AS (
  SELECT si.store_id,
         count(*) FILTER (WHERE si.stock_updated_at IS NULL
                          OR si.stock_updated_at < @stale_before::timestamptz)::int AS stale,
         count(*) FILTER (WHERE si.stock_updated_at >= @fresh_after::timestamptz)::int AS fresh_24h
  FROM store_items si
  WHERE si.is_listed
  GROUP BY si.store_id
),
vw AS (
  SELECT store_id, count(*)::int AS n FROM events
  WHERE name = 'store_viewed' AND store_id IS NOT NULL AND occurred_at >= @day_start::timestamptz AND occurred_at < @day_end::timestamptz
  GROUP BY store_id
),
act AS (
  SELECT DISTINCT store_id FROM v_metric_store_activity WHERE at >= @day_start::timestamptz AND at < @day_end::timestamptz
)
INSERT INTO rollup_daily_store (
  date, store_id, orders, accepted, completed, rejected, expired_unaccepted, no_shows, cancelled, delivery_failed,
  gmv_paise, pos_sales, pos_sales_linked, pos_gmv_paise, pos_unlinked_paise,
  avg_accept_seconds, p50_accept_seconds, p90_accept_seconds,
  stock_updates, items_listed, items_stale, items_fresh_24h, views, searches_hit, conversions, active, live, computed_at)
SELECT @on_date::date, sp.store_id,
       coalesce(o.orders, 0), coalesce(o.accepted, 0), coalesce(o.completed, 0), coalesce(o.rejected, 0),
       coalesce(o.expired_unaccepted, 0), coalesce(o.no_shows, 0), coalesce(o.cancelled, 0), coalesce(o.delivery_failed, 0),
       coalesce(o.gmv, 0), coalesce(p.sales, 0), coalesce(p.linked, 0), coalesce(p.gmv, 0), coalesce(p.unlinked, 0),
       o.avg_accept, o.p50_accept, o.p90_accept,
       coalesce(mv.n, 0), sp.items_listed, coalesce(fresh.stale, 0), coalesce(fresh.fresh_24h, 0), coalesce(vw.n, 0),
       NULL, coalesce(o.completed, 0) + coalesce(p.linked, 0),
       act.store_id IS NOT NULL,
       act.store_id IS NOT NULL AND sp.status = 'live' AND sp.verified AND sp.meets_item_floor
         AND sp.stock_updated_at >= @stale_before::timestamptz,
       @computed_at::timestamptz
FROM v_metric_store_supply sp
LEFT JOIN o ON o.store_id = sp.store_id
LEFT JOIN p ON p.store_id = sp.store_id
LEFT JOIN mv ON mv.store_id = sp.store_id
LEFT JOIN fresh ON fresh.store_id = sp.store_id
LEFT JOIN vw ON vw.store_id = sp.store_id
LEFT JOIN act ON act.store_id = sp.store_id
WHERE sp.verified OR o.store_id IS NOT NULL OR p.store_id IS NOT NULL
ON CONFLICT ON CONSTRAINT pk_rollup_daily_store DO UPDATE SET
  orders = excluded.orders, accepted = excluded.accepted, completed = excluded.completed,
  rejected = excluded.rejected, expired_unaccepted = excluded.expired_unaccepted, no_shows = excluded.no_shows,
  cancelled = excluded.cancelled, delivery_failed = excluded.delivery_failed, gmv_paise = excluded.gmv_paise,
  pos_sales = excluded.pos_sales, pos_sales_linked = excluded.pos_sales_linked, pos_gmv_paise = excluded.pos_gmv_paise,
  pos_unlinked_paise = excluded.pos_unlinked_paise, avg_accept_seconds = excluded.avg_accept_seconds,
  p50_accept_seconds = excluded.p50_accept_seconds, p90_accept_seconds = excluded.p90_accept_seconds,
  stock_updates = excluded.stock_updates, items_listed = excluded.items_listed, items_stale = excluded.items_stale,
  items_fresh_24h = excluded.items_fresh_24h, views = excluded.views, searches_hit = excluded.searches_hit,
  conversions = excluded.conversions, active = excluded.active, live = excluded.live,
  computed_at = excluded.computed_at;

------------------------------------------------------------------------------
-- rollup_cohorts
------------------------------------------------------------------------------

-- name: UpsertCohorts :execrows
-- Customer and store cohorts whose week starts on or after from_week, weeks
-- 0–12 up to the week holding day. ISO weeks start on Monday.
WITH
cust AS (
  SELECT customer_id AS id, date_trunc('week', min(date))::date AS cohort_week
  FROM v_metric_transactions
  GROUP BY customer_id
  HAVING date_trunc('week', min(date))::date >= @from_week::date
),
store AS (
  SELECT s.id, date_trunc('week', (b.verified_at AT TIME ZONE 'Asia/Kolkata')::date)::date AS cohort_week
  FROM stores s JOIN businesses b ON b.id = s.business_id
  WHERE b.status = 'verified' AND b.verified_at IS NOT NULL
    AND date_trunc('week', (b.verified_at AT TIME ZONE 'Asia/Kolkata')::date)::date >= @from_week::date
),
cust_active AS (
  SELECT DISTINCT a.user_id AS id, date_trunc('week', a.date)::date AS wk
  FROM v_metric_app_opens a
  WHERE a.app = 'customer' AND a.user_id IS NOT NULL AND a.date >= @from_week::date AND a.date <= @on_date::date
),
cust_tx AS (
  SELECT DISTINCT customer_id AS id, date_trunc('week', date)::date AS wk
  FROM v_metric_transactions WHERE date >= @from_week::date AND date <= @on_date::date
),
store_active AS (
  SELECT DISTINCT store_id AS id, date_trunc('week', date)::date AS wk
  FROM v_metric_store_activity WHERE date >= @from_week::date AND date <= @on_date::date
),
store_tx AS (
  SELECT DISTINCT store_id AS id, date_trunc('week', placed_date)::date AS wk
  FROM v_metric_orders WHERE completed AND placed_date >= @from_week::date AND placed_date <= @on_date::date
  UNION
  SELECT DISTINCT store_id, date_trunc('week', sold_date)::date
  FROM v_metric_pos_sales WHERE sold_date >= @from_week::date AND sold_date <= @on_date::date
),
weeks AS (SELECT n::smallint AS n FROM generate_series(0, 12) n),
rows AS (
  SELECT c.cohort_week, 'customer'::text AS kind, w.n AS week_n,
         count(*)::int AS size,
         count(*) FILTER (WHERE ca.id IS NOT NULL)::int AS active,
         count(*) FILTER (WHERE ct.id IS NOT NULL)::int AS ordering
  FROM cust c
  CROSS JOIN weeks w
  LEFT JOIN cust_active ca ON ca.id = c.id AND ca.wk = c.cohort_week + w.n * 7
  LEFT JOIN cust_tx ct ON ct.id = c.id AND ct.wk = c.cohort_week + w.n * 7
  WHERE c.cohort_week + w.n * 7 <= @on_date::date
  GROUP BY c.cohort_week, w.n
  UNION ALL
  SELECT c.cohort_week, 'store'::text, w.n,
         count(*)::int,
         count(*) FILTER (WHERE sa.id IS NOT NULL)::int,
         count(*) FILTER (WHERE st.id IS NOT NULL)::int
  FROM store c
  CROSS JOIN weeks w
  LEFT JOIN store_active sa ON sa.id = c.id AND sa.wk = c.cohort_week + w.n * 7
  LEFT JOIN store_tx st ON st.id = c.id AND st.wk = c.cohort_week + w.n * 7
  WHERE c.cohort_week + w.n * 7 <= @on_date::date
  GROUP BY c.cohort_week, w.n
)
INSERT INTO rollup_cohort_weekly (cohort_week, kind, week_n, cohort_size, active_count, ordering_count, computed_at)
SELECT cohort_week, kind, week_n, size, active, ordering, @computed_at::timestamptz FROM rows
ON CONFLICT ON CONSTRAINT pk_rollup_cohort_weekly DO UPDATE SET
  cohort_size = excluded.cohort_size, active_count = excluded.active_count,
  ordering_count = excluded.ordering_count, computed_at = excluded.computed_at;

------------------------------------------------------------------------------
-- rollup_demand
------------------------------------------------------------------------------

-- name: UpsertDemandDay :execrows
INSERT INTO rollup_daily_search_demand (date, geo_cell, query_norm, product_id, searches, zero_results, stores_with_stock)
SELECT @on_date::date, geo_cell, query_norm, product_id,
       count(*)::int, count(*) FILTER (WHERE NOT has_results)::int, coalesce(max(store_count), 0)::int
FROM v_metric_searches
WHERE created_at >= @day_start::timestamptz AND created_at < @day_end::timestamptz
GROUP BY geo_cell, query_norm, product_id
ON CONFLICT ON CONSTRAINT ux_rollup_daily_search_demand DO UPDATE SET
  searches = excluded.searches, zero_results = excluded.zero_results, stores_with_stock = excluded.stores_with_stock;

-- name: MissingGeoCells :many
-- Cells day d's demand uses that insight_geo_cells has no centre for yet.
SELECT DISTINCT d.geo_cell::bigint AS geo_cell
FROM rollup_daily_search_demand d
LEFT JOIN insight_geo_cells c ON c.geo_cell = d.geo_cell
WHERE d.date = @on_date::date AND d.geo_cell IS NOT NULL AND c.geo_cell IS NULL;

-- name: InsertGeoCell :exec
-- The centre of an H3 res-8 cell, computed in Go (platform/geo/h3).
INSERT INTO insight_geo_cells (geo_cell, centre)
VALUES (@geo_cell, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)
ON CONFLICT (geo_cell) DO NOTHING;

------------------------------------------------------------------------------
-- rollup_item_pairs
------------------------------------------------------------------------------

-- name: DeleteItemPairs :execrows
DELETE FROM rollup_item_pairs;

-- name: InsertItemPairs :execrows
-- Bills in [window_start, window_end): completed online orders (accepted
-- lines) and completed POS sales (catalogue lines); UNION keeps an item once
-- per bill. Item A has more bills (ties: the lower id). Keeps every pair with
-- at least min_bills joint bills, the top 100 per store by lift (01 §17 fixed).
WITH
bills AS (
  SELECT o.id AS bill, o.store_id, i.store_item_id AS item
  FROM orders o JOIN order_items i ON i.order_id = o.id
  WHERE o.status IN ('collected', 'delivered') AND i.quantity_accepted > 0
    AND o.placed_at >= @window_start::timestamptz AND o.placed_at < @window_end::timestamptz
  UNION
  SELECT s.id, s.store_id, l.store_item_id
  FROM pos_sales s JOIN pos_sale_lines l ON l.sale_id = s.id
  WHERE s.status = 'completed' AND l.store_item_id IS NOT NULL
    AND s.sold_at >= @window_start::timestamptz AND s.sold_at < @window_end::timestamptz
),
totals AS (SELECT store_id, count(DISTINCT bill)::int AS n FROM bills GROUP BY store_id),
items AS (SELECT store_id, item, count(*)::int AS n FROM bills GROUP BY store_id, item),
pairs AS (
  SELECT a.store_id, a.item AS i1, b.item AS i2, count(*)::int AS both_n
  FROM bills a JOIN bills b ON b.bill = a.bill AND b.store_id = a.store_id AND a.item < b.item
  GROUP BY a.store_id, a.item, b.item
  HAVING count(*) >= @min_bills::int
),
oriented AS (
  SELECT p.store_id,
         CASE WHEN n1.n >= n2.n THEN p.i1 ELSE p.i2 END AS item_a,
         CASE WHEN n1.n >= n2.n THEN p.i2 ELSE p.i1 END AS item_b,
         p.both_n,
         greatest(n1.n, n2.n) AS bills_a,
         least(n1.n, n2.n) AS bills_b,
         t.n AS bills_total
  FROM pairs p
  JOIN items n1 ON n1.store_id = p.store_id AND n1.item = p.i1
  JOIN items n2 ON n2.store_id = p.store_id AND n2.item = p.i2
  JOIN totals t ON t.store_id = p.store_id
),
ranked AS (
  SELECT o.*, (o.both_n::bigint * o.bills_total * 10000 / (o.bills_a::bigint * o.bills_b))::bigint AS lift_bp,
         row_number() OVER (PARTITION BY o.store_id
                            ORDER BY o.both_n::bigint * o.bills_total * 10000 / (o.bills_a::bigint * o.bills_b) DESC,
                                     o.both_n DESC, o.item_a, o.item_b) AS rn
  FROM oriented o
)
INSERT INTO rollup_item_pairs (store_id, store_item_a, store_item_b, computed_at, window_days,
                               bills_both, bills_a, bills_b, bills_total, lift_bp)
SELECT store_id, item_a, item_b, @computed_at::timestamptz, @window_days::int, both_n, bills_a, bills_b, bills_total, lift_bp
FROM ranked
WHERE rn <= 100;

------------------------------------------------------------------------------
-- rollup_banners
------------------------------------------------------------------------------

-- name: BannerEventCounts :many
-- Impressions and taps by the banner id in the event's props (a ULID string;
-- Go converts it to the uuid).
SELECT (props ->> 'banner_id')::text AS banner,
       count(*) FILTER (WHERE name = 'banner_impression')::bigint AS impressions,
       count(*) FILTER (WHERE name = 'banner_tapped')::bigint AS clicks
FROM events
WHERE name IN ('banner_impression', 'banner_tapped') AND props ? 'banner_id'
  AND occurred_at >= @day_start::timestamptz AND occurred_at < @day_end::timestamptz
GROUP BY 1;

-- name: UpsertBannerDays :execrows
-- D-77: an order placed this day is attributed to the banner of the same store
-- the same customer tapped last, within 24 h before the order (fixed, 01 §17).
-- banner_taps keeps each customer's last tap per banner only, so a backfill of
-- an old day misses taps that were later repeated.
WITH
ev AS (
  SELECT u.banner_id, u.impressions, u.clicks
  FROM (SELECT unnest(@banner_ids::uuid[]) AS banner_id, unnest(@impressions::bigint[]) AS impressions,
               unnest(@clicks::bigint[]) AS clicks) u
  JOIN store_banners b ON b.id = u.banner_id
),
attr AS (
  SELECT t.banner_id, count(*)::bigint AS orders
  FROM orders o
  JOIN LATERAL (
    SELECT bt.banner_id FROM banner_taps bt
    WHERE bt.customer_id = o.customer_id AND bt.store_id = o.store_id
      AND bt.tapped_at <= o.placed_at AND bt.tapped_at > o.placed_at - interval '24 hours'
    ORDER BY bt.tapped_at DESC, bt.banner_id DESC
    LIMIT 1
  ) t ON true
  WHERE o.placed_at >= @day_start::timestamptz AND o.placed_at < @day_end::timestamptz
  GROUP BY t.banner_id
)
INSERT INTO banner_daily (banner_id, date, impressions, clicks, attributed_orders)
SELECT coalesce(ev.banner_id, attr.banner_id), @on_date::date, coalesce(ev.impressions, 0), coalesce(ev.clicks, 0),
       coalesce(attr.orders, 0)
FROM ev FULL JOIN attr ON attr.banner_id = ev.banner_id
ON CONFLICT ON CONSTRAINT pk_banner_daily DO UPDATE SET
  impressions = excluded.impressions, clicks = excluded.clicks, attributed_orders = excluded.attributed_orders;

------------------------------------------------------------------------------
-- Pulse (A-02)
------------------------------------------------------------------------------

-- name: PlatformDays :many
SELECT * FROM rollup_daily_platform WHERE date >= @from_day::date AND date <= @to_day::date ORDER BY date;

-- name: ActiveCustomersBetween :one
SELECT count(DISTINCT actor_id)::int FROM v_metric_app_opens
WHERE app = 'customer' AND occurred_at >= @start_at::timestamptz AND occurred_at < @end_at::timestamptz;

-- name: TransactingCustomersBetween :one
SELECT count(DISTINCT customer_id)::int FROM v_metric_transactions WHERE at >= @start_at::timestamptz AND at < @end_at::timestamptz;

-- name: ActiveStoresBetween :one
SELECT count(DISTINCT store_id)::int FROM rollup_daily_store
WHERE active AND date >= @from_day::date AND date <= @to_day::date;

-- name: OrderLeaks :many
SELECT status, status_reason, count(*)::int AS n
FROM v_metric_orders
WHERE placed_at >= @start_at::timestamptz AND placed_at < @end_at::timestamptz
  AND status IN ('rejected', 'expired', 'cancelled', 'delivery_failed')
GROUP BY status, status_reason
ORDER BY n DESC, status, status_reason
LIMIT 40;

-- name: CustomerCohorts :many
-- The last 12 customer cohorts up to to_day, newest first.
SELECT cohort_week, week_n, cohort_size, ordering_count
FROM rollup_cohort_weekly
WHERE kind = 'customer'
  AND cohort_week IN (SELECT DISTINCT c.cohort_week FROM rollup_cohort_weekly c
                      WHERE c.kind = 'customer' AND c.cohort_week <= @to_day::date
                      ORDER BY c.cohort_week DESC LIMIT 12)
ORDER BY cohort_week DESC, week_n;

-- name: ZeroResultQueries :many
-- k ≥ 10 (01 §16, fixed): only queries searched without a result at least k times.
SELECT query_norm, sum(zero_results)::int AS n
FROM rollup_daily_search_demand
WHERE date >= @from_day::date AND date <= @to_day::date
GROUP BY query_norm
HAVING sum(zero_results) >= @k::int
ORDER BY n DESC, query_norm
LIMIT 20;

-- name: StoresNeedingAttention :many
-- not_accepting: ≥ 3 orders expired unaccepted in the last 2 h (fixed, the
-- "Store not accepting" alert); strikes: ≥ strike_threshold unreset store
-- strikes in 30 days (fixed window); stale_stock: a live store whose stock was
-- not updated since stale_before (as of minus stock_stale_after_hours).
WITH
exp2h AS (
  SELECT o.store_id, count(*)::int AS n FROM orders o
  WHERE o.status = 'expired' AND o.status_reason = 'not_accepted'
    AND o.updated_at >= @recent_from::timestamptz
  GROUP BY o.store_id
),
expp AS (
  SELECT vo.store_id, count(*)::int AS n FROM v_metric_orders vo
  WHERE vo.status = 'expired' AND vo.status_reason = 'not_accepted' AND vo.placed_at >= @start_at::timestamptz AND vo.placed_at < @end_at::timestamptz
  GROUP BY vo.store_id
),
str AS (
  SELECT sk.subject_id AS store_id, count(*)::int AS n FROM strikes sk
  WHERE sk.subject_type = 'store' AND sk.reset_at IS NULL AND sk.created_at >= @strikes_from::timestamptz
  GROUP BY sk.subject_id
),
c AS (
  SELECT s.id, s.name, coalesce(s.city, '') AS city, s.accept_rate_30d, sp.stock_updated_at,
         coalesce(e2.n, 0) AS exp2h, coalesce(ep.n, 0) AS exp_period, coalesce(st.n, 0) AS strikes,
         CASE WHEN coalesce(e2.n, 0) >= 3 THEN 'not_accepting'
              WHEN coalesce(st.n, 0) >= @strike_threshold::int THEN 'strikes'
              WHEN s.status = 'live' AND (sp.stock_updated_at IS NULL
                   OR sp.stock_updated_at < @stale_before::timestamptz) THEN 'stale_stock'
         END AS reason
  FROM stores s
  JOIN v_metric_store_supply sp ON sp.store_id = s.id
  LEFT JOIN exp2h e2 ON e2.store_id = s.id
  LEFT JOIN expp ep ON ep.store_id = s.id
  LEFT JOIN str st ON st.store_id = s.id
  WHERE sp.verified
)
SELECT id, name, city, reason::text AS reason, exp_period::int AS expired_unaccepted, strikes::int AS strikes_30d,
       (accept_rate_30d IS NOT NULL)::bool AS has_accept_rate,
       coalesce(round(accept_rate_30d * 10000), 0)::int AS accept_rate_bp, stock_updated_at
FROM c
WHERE reason IS NOT NULL
ORDER BY CASE reason WHEN 'not_accepting' THEN 0 WHEN 'strikes' THEN 1 ELSE 2 END, exp2h DESC, strikes DESC, id
LIMIT 20;

-- name: LatestComputedAt :one
SELECT max(computed_at)::timestamptz FROM rollup_daily_platform WHERE date = @on_date::date;

-- name: MapStores :many
SELECT s.id, s.name, coalesce(s.city, '') AS city, s.status,
       ST_Y(s.location::geometry)::float8 AS lat, ST_X(s.location::geometry)::float8 AS lng,
       coalesce(r.live, false)::bool AS live
FROM stores s
LEFT JOIN rollup_daily_store r ON r.store_id = s.id AND r.date = @on_date::date
WHERE s.location IS NOT NULL AND s.status IN ('pending_review', 'live', 'paused', 'suspended')
ORDER BY s.id
LIMIT 5000;

-- name: MapDemand :many
-- k ≥ 10 (01 §16): a cell with fewer than k searches in the period is left out.
SELECT geo_cell::bigint AS geo_cell, sum(searches)::int AS searches, sum(zero_results)::int AS zero_results
FROM rollup_daily_search_demand
WHERE date >= @from_day::date AND date <= @to_day::date AND geo_cell IS NOT NULL
GROUP BY geo_cell
HAVING sum(searches) >= @k::int
ORDER BY searches DESC, geo_cell
LIMIT 5000;
