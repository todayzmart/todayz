-- W3-07 analytics rollups (05 §3, 03 §10): the rollup tables the jobs in
-- internal/jobs/rollups.go fill, and the v_metric_* views that hold the metric
-- definitions (05 §3 "Metric definitions"). The rollup queries
-- (db/queries/rollups), dashboards 07–11 and the admin Pulse all read these, so
-- their numbers agree.
--
-- Days are IST calendar days. Orders count on the day they were placed and POS
-- sales on the day they were sold; rollup_today recomputes today and yesterday
-- every 5 minutes and rollup_day recomputes the last 3 days at 00:30, so a day is
-- final once its orders have ended (`final` marks a day computed after it closed).
--
-- The views use no functions of our own: a view's caller needs EXECUTE on every
-- function it calls, and grafana_ro only has pg_read_all_data. Nothing here says
-- `now()`: anything relative to the time (live stores, stale stock) is computed
-- by the rollup query at the injected clock's time and stored.

-- +goose Up

-- One row per online order with what the metrics need.
CREATE VIEW v_metric_orders AS
SELECT o.id AS order_id,
       o.store_id,
       o.customer_id,
       o.placed_at,
       (o.placed_at AT TIME ZONE 'Asia/Kolkata')::date AS placed_date,
       o.status,
       o.status_reason,
       o.fulfilment,
       o.total_paise,
       o.credits_used,
       o.status IN ('collected', 'delivered') AS completed,
       o.accepted_at IS NOT NULL AS accepted,
       CASE WHEN o.accepted_at IS NOT NULL THEN date_part('epoch', o.accepted_at - o.placed_at)::int END AS accept_seconds
FROM orders o;

-- Completed (not reversed) POS sales. Linked: bill QR or counter code, link
-- verified. Only linked sales count as POS GMV; the rest are "shop sales recorded".
CREATE VIEW v_metric_pos_sales AS
SELECT s.id AS sale_id,
       s.store_id,
       s.customer_id,
       s.sold_at,
       (s.sold_at AT TIME ZONE 'Asia/Kolkata')::date AS sold_date,
       s.total_paise,
       (s.link_method IN ('bill_qr', 'counter_code') AND s.link_status = 'linked') AS linked,
       s.device_seq IS NOT NULL AS offline
FROM pos_sales s
WHERE s.status = 'completed';

-- A transaction: a completed online order or a linked POS sale (transacting
-- customer, customer cohorts, conversions).
CREATE VIEW v_metric_transactions AS
SELECT o.customer_id, o.store_id, o.placed_at AS at, o.placed_date AS date, 'order'::text AS kind, o.total_paise
FROM v_metric_orders o
WHERE o.completed
UNION ALL
SELECT p.customer_id, p.store_id, p.sold_at, p.sold_date, 'pos'::text, p.total_paise
FROM v_metric_pos_sales p
WHERE p.linked AND p.customer_id IS NOT NULL;

-- App opens. A customer is the signed-in user, else the device's anon id; a
-- business open counts for its store, or every store of the business when the
-- event names only the business.
CREATE VIEW v_metric_app_opens AS
SELECT e.occurred_at,
       (e.occurred_at AT TIME ZONE 'Asia/Kolkata')::date AS date,
       e.app,
       coalesce(e.user_id, e.anon_id) AS actor_id,
       e.user_id,
       e.store_id,
       e.business_id
FROM events e
WHERE e.name IN ('app_opened', 'biz_app_opened');

-- Store activity: a staff app open or a POS sale, at a store of a verified
-- business (05 §3 "Active store"). One row per activity, not per day; group it.
CREATE VIEW v_metric_store_activity AS
SELECT s.id AS store_id, a.occurred_at AS at, a.date
FROM v_metric_app_opens a
JOIN stores s ON s.id = a.store_id OR (a.store_id IS NULL AND s.business_id = a.business_id)
JOIN businesses b ON b.id = s.business_id AND b.status = 'verified'
WHERE a.app = 'business'
UNION ALL
SELECT p.store_id, p.sold_at, p.sold_date
FROM v_metric_pos_sales p
JOIN stores s ON s.id = p.store_id
JOIN businesses b ON b.id = s.business_id AND b.status = 'verified';

-- Supply per store as it is now: listed items and the latest stock update.
-- "Live" (active, at least 50 listed items, stock updated within
-- stock_stale_after_hours, 05 §3) is computed from this by the rollup at its time.
CREATE VIEW v_metric_store_supply AS
SELECT s.id AS store_id,
       s.business_id,
       s.name,
       coalesce(s.city, '') AS city,
       s.status,
       s.live_at,
       b.status = 'verified' AS verified,
       b.verified_at,
       (s.accepts_pickup OR s.accepts_delivery) AS bookable,
       count(i.id) FILTER (WHERE i.is_listed)::int AS items_listed,
       count(i.id) FILTER (WHERE i.is_listed)::int >= 50 AS meets_item_floor,
       max(i.stock_updated_at) AS stock_updated_at
FROM stores s
JOIN businesses b ON b.id = s.business_id
LEFT JOIN store_items i ON i.store_id = s.id
GROUP BY s.id, b.status, b.verified_at;

-- Searches (search_log) per day, with success (at least one result).
CREATE VIEW v_metric_searches AS
SELECT l.created_at,
       (l.created_at AT TIME ZONE 'Asia/Kolkata')::date AS date,
       l.query_norm,
       l.product_id,
       l.geo_cell,
       l.user_id,
       l.results_count > 0 AS has_results,
       l.store_count
FROM search_log l;

-- Daily platform totals (rollup_today, rollup_day).
CREATE TABLE rollup_daily_platform (
  date                      date PRIMARY KEY,
  dau                       int NOT NULL DEFAULT 0,
  wau                       int NOT NULL DEFAULT 0,
  mau                       int NOT NULL DEFAULT 0,
  new_customers             int NOT NULL DEFAULT 0,
  transacting_customers     int NOT NULL DEFAULT 0,
  orders_placed             int NOT NULL DEFAULT 0,
  orders_accepted           int NOT NULL DEFAULT 0,
  orders_completed          int NOT NULL DEFAULT 0,
  orders_collected          int NOT NULL DEFAULT 0,
  orders_delivered          int NOT NULL DEFAULT 0,
  orders_rejected           int NOT NULL DEFAULT 0,
  orders_expired            int NOT NULL DEFAULT 0,
  orders_expired_unaccepted int NOT NULL DEFAULT 0,
  orders_no_show            int NOT NULL DEFAULT 0,
  orders_cancelled          int NOT NULL DEFAULT 0,
  orders_delivery_failed    int NOT NULL DEFAULT 0,
  orders_open               int NOT NULL DEFAULT 0,
  gmv_paise                 bigint NOT NULL DEFAULT 0,
  pos_gmv_paise             bigint NOT NULL DEFAULT 0,
  pos_sales                 int NOT NULL DEFAULT 0,
  pos_sales_linked          int NOT NULL DEFAULT 0,
  pos_unlinked_paise        bigint NOT NULL DEFAULT 0,
  credits_issued            bigint NOT NULL DEFAULT 0,
  credits_spent             bigint NOT NULL DEFAULT 0,
  credits_expired           bigint NOT NULL DEFAULT 0,
  searches                  int NOT NULL DEFAULT 0,
  searches_zero             int NOT NULL DEFAULT 0,
  zero_result_rate_bp       int,
  store_views               int NOT NULL DEFAULT 0,
  -- App retention (05 §3): customers first seen N days before this day (base)
  -- and how many of them opened the app on this day (back), N = 1, 7, 30.
  ret_d1_base               int NOT NULL DEFAULT 0,
  ret_d1_back               int NOT NULL DEFAULT 0,
  ret_d7_base               int NOT NULL DEFAULT 0,
  ret_d7_back               int NOT NULL DEFAULT 0,
  ret_d30_base              int NOT NULL DEFAULT 0,
  ret_d30_back              int NOT NULL DEFAULT 0,
  verified_stores          int NOT NULL DEFAULT 0,
  active_stores             int NOT NULL DEFAULT 0,
  live_stores               int NOT NULL DEFAULT 0,
  new_stores                int NOT NULL DEFAULT 0,
  final                     boolean NOT NULL DEFAULT false,
  computed_at               timestamptz NOT NULL,
  CONSTRAINT ck_rollup_daily_platform_rate CHECK (zero_result_rate_bp IS NULL OR zero_result_rate_bp BETWEEN 0 AND 10000)
);

-- Daily per store. items_* and live are a snapshot at the time the row was
-- computed (store_items keeps no history), so a backfilled past day shows the
-- supply as of the backfill.
CREATE TABLE rollup_daily_store (
  date                date NOT NULL,
  store_id            uuid NOT NULL REFERENCES stores (id),
  orders              int NOT NULL DEFAULT 0,
  accepted            int NOT NULL DEFAULT 0,
  completed           int NOT NULL DEFAULT 0,
  rejected            int NOT NULL DEFAULT 0,
  expired_unaccepted  int NOT NULL DEFAULT 0,
  no_shows            int NOT NULL DEFAULT 0,
  cancelled           int NOT NULL DEFAULT 0,
  delivery_failed     int NOT NULL DEFAULT 0,
  gmv_paise           bigint NOT NULL DEFAULT 0,
  pos_sales           int NOT NULL DEFAULT 0,
  pos_sales_linked    int NOT NULL DEFAULT 0,
  pos_gmv_paise       bigint NOT NULL DEFAULT 0,
  pos_unlinked_paise  bigint NOT NULL DEFAULT 0,
  avg_accept_seconds  int,
  p50_accept_seconds  int,
  p90_accept_seconds  int,
  stock_updates       int NOT NULL DEFAULT 0,
  items_listed        int NOT NULL DEFAULT 0,
  items_stale         int NOT NULL DEFAULT 0,
  items_fresh_24h     int NOT NULL DEFAULT 0,
  views               int NOT NULL DEFAULT 0,
  searches_hit        int,  -- TODO(lead): search_log records no store ids; null until it does
  conversions         int NOT NULL DEFAULT 0,
  active              boolean NOT NULL DEFAULT false,
  live                boolean NOT NULL DEFAULT false,
  computed_at         timestamptz NOT NULL,
  CONSTRAINT pk_rollup_daily_store PRIMARY KEY (date, store_id)
);
CREATE INDEX ix_rollup_daily_store_store_date ON rollup_daily_store (store_id, date DESC);

-- Weekly cohorts (rollup_cohorts). Customer: the IST week (Monday) of the first
-- transaction; active = opened the app, ordering = transacted, in week n.
-- Store: the week its business was verified; active = an active day, ordering =
-- a completed order or a POS sale, in week n.
CREATE TABLE rollup_cohort_weekly (
  cohort_week    date NOT NULL,
  kind           text NOT NULL,
  week_n         smallint NOT NULL,
  cohort_size    int NOT NULL,
  active_count   int NOT NULL DEFAULT 0,
  ordering_count int NOT NULL DEFAULT 0,
  computed_at    timestamptz NOT NULL,
  CONSTRAINT pk_rollup_cohort_weekly PRIMARY KEY (cohort_week, kind, week_n),
  CONSTRAINT ck_rollup_cohort_weekly_kind CHECK (kind IN ('customer', 'store')),
  CONSTRAINT ck_rollup_cohort_weekly_week CHECK (week_n BETWEEN 0 AND 12),
  CONSTRAINT ck_rollup_cohort_weekly_monday CHECK (extract(isodow FROM cohort_week) = 1)
);

-- Search demand per IST day, H3 resolution-8 cell and normalised query
-- (rollup_demand, hourly). geo_cell is null for searches logged without a cell.
-- stores_with_stock is the most stores any of those searches found.
CREATE TABLE rollup_daily_search_demand (
  date              date NOT NULL,
  geo_cell          bigint,
  query_norm        text NOT NULL,
  product_id        uuid,
  searches          int NOT NULL,
  zero_results      int NOT NULL,
  stores_with_stock int NOT NULL DEFAULT 0,
  CONSTRAINT ux_rollup_daily_search_demand UNIQUE NULLS NOT DISTINCT (date, geo_cell, query_norm, product_id),
  CONSTRAINT ck_rollup_daily_search_demand_counts CHECK (searches > 0 AND zero_results BETWEEN 0 AND searches)
);
CREATE INDEX ix_rollup_daily_search_demand_cell_date ON rollup_daily_search_demand (geo_cell, date);
CREATE INDEX ix_rollup_daily_search_demand_product ON rollup_daily_search_demand (product_id, geo_cell, date)
  WHERE product_id IS NOT NULL;

-- "Bought together" pairs (rollup_item_pairs, 02:00 IST; 01 §12 pair rule, D-99).
CREATE TABLE rollup_item_pairs (
  store_id     uuid NOT NULL REFERENCES stores (id),
  store_item_a uuid NOT NULL REFERENCES store_items (id),
  store_item_b uuid NOT NULL REFERENCES store_items (id),
  computed_at  timestamptz NOT NULL,
  window_days  int NOT NULL,
  bills_both   int NOT NULL,
  bills_a      int NOT NULL,
  bills_b      int NOT NULL,
  bills_total  int NOT NULL,
  lift_bp      bigint NOT NULL,
  CONSTRAINT pk_rollup_item_pairs PRIMARY KEY (store_id, store_item_a, store_item_b),
  CONSTRAINT ck_rollup_item_pairs_distinct CHECK (store_item_a <> store_item_b),
  CONSTRAINT ck_rollup_item_pairs_counts CHECK (bills_both > 0 AND bills_both <= bills_b AND bills_b <= bills_a
    AND bills_a <= bills_total AND lift_bp >= 0)
);
CREATE INDEX ix_rollup_item_pairs_store_lift ON rollup_item_pairs (store_id, lift_bp DESC);

-- Rollups read orders by time across all stores and order events by status.
CREATE INDEX ix_orders_placed_at ON orders USING brin (placed_at);
CREATE INDEX ix_pos_sales_sold_at ON pos_sales USING brin (sold_at);

-- +goose Down
DROP INDEX IF EXISTS ix_pos_sales_sold_at;
DROP INDEX IF EXISTS ix_orders_placed_at;
DROP TABLE IF EXISTS rollup_item_pairs;
DROP TABLE IF EXISTS rollup_daily_search_demand;
DROP TABLE IF EXISTS rollup_cohort_weekly;
DROP TABLE IF EXISTS rollup_daily_store;
DROP TABLE IF EXISTS rollup_daily_platform;
DROP VIEW IF EXISTS v_metric_searches;
DROP VIEW IF EXISTS v_metric_store_supply;
DROP VIEW IF EXISTS v_metric_store_activity;
DROP VIEW IF EXISTS v_metric_app_opens;
DROP VIEW IF EXISTS v_metric_transactions;
DROP VIEW IF EXISTS v_metric_pos_sales;
DROP VIEW IF EXISTS v_metric_orders;
