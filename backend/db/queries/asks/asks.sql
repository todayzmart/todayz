-- asks queries, generated into package asksdb (gen/sqlc/asksdb).
-- Only W3-16 (internal/asks) edits this folder (D-50). The customer side
-- filters on customer_id from the session; the shop side on store_id from the
-- path and never selects a customer column.

-- --- create ------------------------------------------------------------------

-- name: LockCustomerAsks :exec
-- Serialises one customer's creates, so the daily limit and the open-ask
-- check hold.
SELECT pg_advisory_xact_lock(hashtextextended('asks:' || sqlc.arg(customer_key)::text, 0));

-- name: CustomerStatus :one
SELECT status FROM users WHERE id = @id;

-- name: CountAsksSince :one
SELECT count(*)::int FROM asks WHERE customer_id = @customer_id AND created_at >= @since;

-- name: OpenAskFor :one
SELECT id FROM asks
WHERE customer_id = @customer_id AND status = 'open'
  AND coalesce(product_id::text, 'q:' || query_norm) = coalesce(sqlc.narg(product_id)::uuid::text, 'q:' || @query_norm::text);

-- name: RecentAskYes :many
-- The customer's latest asks since their latest completed online order, each
-- with whether any shop said Yes (the low-yield rule, 01 §11.1).
SELECT a.id,
       EXISTS (SELECT 1 FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer = 'yes')::bool AS had_yes
FROM asks a
WHERE a.customer_id = @customer_id
  AND a.created_at > coalesce((
    SELECT max(coalesce(o.collected_at, o.updated_at)) FROM orders o
    WHERE o.customer_id = @customer_id AND o.status IN ('collected', 'delivered')
  ), '-infinity'::timestamptz)
ORDER BY a.created_at DESC
LIMIT sqlc.arg(n)::int;

-- name: ProductForAsk :one
SELECT p.id, p.name, p.variant, p.category_id, p.status
FROM products p WHERE p.id = @id;

-- name: CityAt :one
-- The city of a point: a city whose bbox holds it, else the nearest city.
SELECT p.id FROM places p
WHERE p.kind = 'city'
ORDER BY (p.bbox IS NOT NULL AND ST_Covers(p.bbox, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)) DESC,
         p.location <-> ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography
LIMIT 1;

-- name: CandidateStores :many
-- Stores that may receive the ask, before the open-now check (done in Go on
-- hours): live, business verified, a location, not muted, no block for this
-- customer, under the store's daily limit; inside the radius, or the one
-- target store. tier 0 lists the product (any stock state; for a query ask, an
-- item matching the words), 1 lists its category, 2 the rest; nearest first.
SELECT s.id, s.slug, s.name, s.hours, s.closed_until, s.business_id,
       ST_Distance(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::int AS distance_m,
       (CASE
         WHEN sqlc.narg(product_id)::uuid IS NOT NULL AND EXISTS (
           SELECT 1 FROM store_items si WHERE si.store_id = s.id AND si.product_id = sqlc.narg(product_id)::uuid) THEN 0
         WHEN sqlc.narg(product_id)::uuid IS NULL AND EXISTS (
           SELECT 1 FROM store_items si WHERE si.store_id = s.id AND si.is_listed
             AND si.search @@ plainto_tsquery('simple', @query_norm::text)) THEN 0
         WHEN sqlc.narg(category_id)::uuid IS NOT NULL AND EXISTS (
           SELECT 1 FROM store_items si JOIN products p ON p.id = si.product_id
           WHERE si.store_id = s.id AND p.category_id = sqlc.narg(category_id)::uuid) THEN 1
         ELSE 2
       END)::int AS tier
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE s.status = 'live' AND b.status = 'verified' AND s.location IS NOT NULL AND NOT s.asks_muted
  AND NOT EXISTS (SELECT 1 FROM ask_store_blocks k WHERE k.store_id = s.id AND k.customer_id = @customer_id)
  AND (SELECT count(*) FROM ask_recipients r WHERE r.store_id = s.id AND r.sent_at >= @day_start) < sqlc.arg(store_daily_limit)::int
  AND (CASE WHEN sqlc.narg(target_store_id)::uuid IS NOT NULL THEN s.id = sqlc.narg(target_store_id)::uuid
            ELSE ST_DWithin(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, sqlc.arg(radius_m)::int) END)
ORDER BY tier, distance_m, s.id
LIMIT sqlc.arg(max_candidates)::int;

-- name: StoreBySlug :one
SELECT s.id FROM stores s WHERE s.slug = @slug;

-- name: InsertAsk :exec
INSERT INTO asks (id, customer_id, product_id, query_norm, quantity, need_by, geo_cell, city_place_id,
                  radius_m, target_store_id, recipients_count, low_yield, created_at)
VALUES (@id, @customer_id, sqlc.narg(product_id), @query_norm, @quantity, sqlc.narg(need_by), sqlc.narg(geo_cell),
        sqlc.narg(city_place_id), @radius_m, sqlc.narg(target_store_id), @recipients_count, @low_yield, @now);

-- name: InsertRecipient :exec
INSERT INTO ask_recipients (id, ask_id, store_id, sent_at, distance_m, distance_band, reply_by, store_item_id)
VALUES (@id, @ask_id, @store_id, @now, @distance_m, @distance_band, @reply_by, sqlc.narg(store_item_id));

-- name: StoreItemFor :one
SELECT si.id FROM store_items si WHERE si.store_id = @store_id AND si.product_id = @product_id;

-- --- customer reads ----------------------------------------------------------

-- name: ListMyAsks :many
SELECT a.id, a.product_id, a.query_norm, a.quantity, a.need_by, a.status, a.close_reason, a.recipients_count,
       a.created_at, a.closed_at, a.version, p.name AS product_name, p.variant AS product_variant,
       (SELECT count(*) FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer IS NOT NULL)::int AS replied,
       (SELECT count(*) FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer = 'yes')::int AS yes_count,
       coalesce((SELECT min(r.price_paise) FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer = 'yes'), 0)::bigint AS best_price_paise,
       coalesce((SELECT max(r.reply_by) FROM ask_recipients r WHERE r.ask_id = a.id), a.created_at)::timestamptz AS window_ends_at
FROM asks a LEFT JOIN products p ON p.id = a.product_id
WHERE a.customer_id = @customer_id
  AND (sqlc.narg(status)::text IS NULL OR a.status = sqlc.narg(status)::text)
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (a.created_at, a.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(lim)::int;

-- name: GetMyAsk :one
SELECT a.id, a.product_id, a.query_norm, a.quantity, a.need_by, a.status, a.close_reason, a.recipients_count,
       a.created_at, a.closed_at, a.version, p.name AS product_name, p.variant AS product_variant,
       (SELECT count(*) FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer IS NOT NULL)::int AS replied,
       (SELECT count(*) FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer = 'yes')::int AS yes_count,
       coalesce((SELECT min(r.price_paise) FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer = 'yes'), 0)::bigint AS best_price_paise,
       coalesce((SELECT max(r.reply_by) FROM ask_recipients r WHERE r.ask_id = a.id), a.created_at)::timestamptz AS window_ends_at
FROM asks a LEFT JOIN products p ON p.id = a.product_id
WHERE a.id = @id AND a.customer_id = @customer_id;

-- name: YesReplies :many
-- The Yes replies of one ask, with the shop and the order already placed
-- from the reply (if any). distance_m is shown to the customer only.
SELECT r.id, r.distance_m, r.price_paise, r.ready_by, r.replied_at, r.price_valid_until,
       coalesce(r.store_item_id, (SELECT si.id FROM store_items si JOIN asks a ON a.id = r.ask_id
                                  WHERE si.store_id = r.store_id AND si.product_id = a.product_id))::uuid AS store_item_id,
       s.slug, s.name, s.accepts_pickup, s.accepts_delivery,
       (s.status = 'live' AND b.status = 'verified')::bool AS store_bookable,
       (SELECT o.id FROM orders o WHERE o.ask_reply_id = r.id
          AND o.status NOT IN ('cancelled', 'expired', 'rejected') LIMIT 1)::uuid AS order_id
FROM ask_recipients r JOIN stores s ON s.id = r.store_id JOIN businesses b ON b.id = s.business_id
WHERE r.ask_id = @ask_id AND r.answer = 'yes'
ORDER BY r.replied_at, r.id;

-- name: CloseAsk :one
UPDATE asks SET status = 'closed', close_reason = @close_reason, closed_at = @now, version = version + 1
WHERE id = @id AND status = 'open'
RETURNING id, customer_id, version, recipients_count;

-- name: AskOwner :one
SELECT id, customer_id, status FROM asks WHERE id = @id;

-- name: AskYesCount :one
SELECT count(*)::int FROM ask_recipients WHERE ask_id = @ask_id AND answer = 'yes';

-- name: AskRecipientStores :many
SELECT store_id FROM ask_recipients WHERE ask_id = @ask_id;

-- --- shop side ---------------------------------------------------------------

-- name: ListStoreAsks :many
-- One store's asks. view: open (not replied, window open, ask open), replied,
-- missed (not replied and the window or the ask is over), all.
SELECT r.id AS reply_id, r.ask_id, r.distance_band, r.reply_by, r.answer, r.price_paise, r.ready_by, r.replied_at,
       r.sent_at, r.reported_at, a.product_id, a.query_norm, a.quantity, a.need_by, a.status AS ask_status,
       p.name AS product_name, p.variant AS product_variant, p.mrp_paise AS product_mrp_paise,
       si.id AS store_item_id, si.price_paise AS listed_price_paise, si.mrp_paise AS item_mrp_paise,
       coalesce(si.on_hand - si.reserved, 0)::int AS available, si.is_listed
FROM ask_recipients r
JOIN asks a ON a.id = r.ask_id
LEFT JOIN products p ON p.id = a.product_id
LEFT JOIN store_items si ON si.id = coalesce(r.store_item_id,
  (SELECT x.id FROM store_items x WHERE x.store_id = r.store_id AND x.product_id = a.product_id))
WHERE r.store_id = @store_id
  AND (sqlc.narg(ask_id)::uuid IS NULL OR r.ask_id = sqlc.narg(ask_id)::uuid)
  AND (CASE sqlc.arg(view)::text
         WHEN 'open' THEN r.answer IS NULL AND r.reply_by > @now AND a.status = 'open'
         WHEN 'replied' THEN r.answer IS NOT NULL
         WHEN 'missed' THEN r.answer IS NULL AND (r.reply_by <= @now OR a.status = 'closed')
         ELSE true END)
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (r.sent_at, r.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY r.sent_at DESC, r.id DESC
LIMIT sqlc.arg(lim)::int;

-- name: CountOpenStoreAsks :one
SELECT count(*)::int FROM ask_recipients r JOIN asks a ON a.id = r.ask_id
WHERE r.store_id = @store_id AND r.answer IS NULL AND r.reply_by > @now AND a.status = 'open';

-- name: LockRecipient :one
-- The store's row of one ask, locked for the reply or the report.
SELECT r.id, r.ask_id, r.store_id, r.answer, r.reply_by, r.reported_at, r.store_item_id, r.sent_at,
       a.customer_id, a.product_id, a.query_norm, a.quantity, a.need_by, a.status AS ask_status, a.created_at AS ask_created_at,
       p.name AS product_name, p.mrp_paise AS product_mrp_paise
FROM ask_recipients r JOIN asks a ON a.id = r.ask_id LEFT JOIN products p ON p.id = a.product_id
WHERE r.ask_id = @ask_id AND r.store_id = @store_id
FOR UPDATE OF r;

-- name: StoreItemPrice :one
SELECT si.id, si.price_paise, si.mrp_paise, si.is_listed, si.product_id
FROM store_items si WHERE si.store_id = @store_id AND si.product_id = @product_id;

-- name: StoreForAsk :one
SELECT s.id, s.name, s.business_id FROM stores s WHERE s.id = @id;

-- name: ConfirmItemStock :exec
-- A Yes on a listed item is a stock count with no change (01 §11.2, §4): the
-- freshness moment moves and no movement is written.
UPDATE store_items
   SET stock_updated_at = GREATEST(coalesce(stock_updated_at, @now::timestamptz), @now::timestamptz)
 WHERE id = @id AND store_id = @store_id;

-- name: ReplyRecipient :one
UPDATE ask_recipients
SET answer = @answer, price_paise = sqlc.narg(price_paise), ready_by = sqlc.narg(ready_by),
    store_item_id = coalesce(sqlc.narg(store_item_id), store_item_id),
    replied_at = @now, replied_by = @replied_by, price_valid_until = sqlc.narg(price_valid_until)
WHERE id = @id AND answer IS NULL
RETURNING id, answer, price_paise, ready_by, replied_at, price_valid_until, store_item_id;

-- name: BumpAsk :one
UPDATE asks SET version = version + 1 WHERE id = @id RETURNING version;

-- name: UnansweredOpen :one
-- Recipients of the ask still able to reply.
SELECT count(*)::int FROM ask_recipients WHERE ask_id = @ask_id AND answer IS NULL AND reply_by > @now;

-- name: AskAllReplied :one
SELECT NOT EXISTS (SELECT 1 FROM ask_recipients WHERE ask_id = @ask_id AND answer IS NULL)::bool;

-- name: ReportRecipient :one
UPDATE ask_recipients SET reported_at = @now, reported_by = @reported_by
WHERE id = @id AND reported_at IS NULL
RETURNING id;

-- name: UpsertBlock :exec
INSERT INTO ask_store_blocks (store_id, customer_id, ask_id, created_at)
VALUES (@store_id, @customer_id, @ask_id, @now)
ON CONFLICT (store_id, customer_id) DO UPDATE SET ask_id = EXCLUDED.ask_id, created_at = EXCLUDED.created_at;

-- name: InsertAskReportedFlag :exec
INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, details, created_at, updated_at)
VALUES (@id, 'user', @customer_id, 'ask_reported', @score, @details, @now, @now);

-- name: AttachStoreItem :exec
-- "Add it to your stock" after a Yes on an unlisted item: the reply points at
-- the new store item so the customer can book it.
UPDATE ask_recipients SET store_item_id = @store_item_id
WHERE ask_id = @ask_id AND store_id = @store_id AND answer = 'yes' AND store_item_id IS NULL;

-- --- booking (the Pricer) ----------------------------------------------------

-- name: ReplyPrice :one
SELECT coalesce(r.store_item_id, (SELECT si.id FROM store_items si
                WHERE si.store_id = r.store_id AND si.product_id = a.product_id))::uuid AS store_item_id,
       r.price_paise, r.price_valid_until, a.quantity
FROM ask_recipients r JOIN asks a ON a.id = r.ask_id
WHERE r.id = @id AND r.store_id = @store_id AND a.customer_id = @customer_id AND r.answer = 'yes';

-- --- timers and maintenance --------------------------------------------------

-- name: EndedOpenAsks :many
-- Open asks whose every recipient has replied or whose last window is over.
SELECT a.id,
       NOT EXISTS (SELECT 1 FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer IS NULL)::bool AS all_replied
FROM asks a
WHERE a.status = 'open'
  AND NOT EXISTS (SELECT 1 FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer IS NULL AND r.reply_by > @now)
ORDER BY a.created_at
LIMIT sqlc.arg(lim)::int;

-- name: DeleteOldAsks :execrows
DELETE FROM asks WHERE id IN (
  SELECT x.id FROM asks x WHERE x.created_at < @before ORDER BY x.created_at LIMIT sqlc.arg(lim)::int);

-- name: DeleteCustomerAsks :execrows
DELETE FROM asks WHERE customer_id = @customer_id;

-- name: DeleteCustomerBlocks :exec
DELETE FROM ask_store_blocks WHERE customer_id = @customer_id;

-- --- demand ------------------------------------------------------------------

-- name: RollupAskDemand :execrows
-- One IST day of closed asks per receiving store, product (or query) and
-- cell. An ask whose customer searched the same product (or query) in the
-- hour before counts in the search already (01 §11.1), so it is left out.
INSERT INTO rollup_daily_ask_demand (date, geo_cell, store_id, product_id, query_norm, asks, asks_without_yes)
SELECT @day::date, a.geo_cell, r.store_id, a.product_id, a.query_norm,
       count(*)::int,
       count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM ask_recipients y WHERE y.ask_id = a.id AND y.answer = 'yes'))::int
FROM asks a JOIN ask_recipients r ON r.ask_id = a.id
WHERE a.created_at >= @from_at AND a.created_at < @to_at AND a.status = 'closed'
  AND NOT EXISTS (
    SELECT 1 FROM search_log l
    WHERE l.user_id = a.customer_id AND l.created_at >= a.created_at - interval '1 hour' AND l.created_at <= a.created_at
      AND (CASE WHEN a.product_id IS NOT NULL THEN l.product_id = a.product_id ELSE l.query_norm = a.query_norm END))
GROUP BY a.geo_cell, r.store_id, a.product_id, a.query_norm
ON CONFLICT ON CONSTRAINT ux_rollup_daily_ask_demand
DO UPDATE SET asks = EXCLUDED.asks, asks_without_yes = EXCLUDED.asks_without_yes;

-- name: DeleteAskDemandDay :exec
DELETE FROM rollup_daily_ask_demand WHERE date = @day::date;

-- name: AskDemandCells :many
-- Cells of the day's ask demand missing from insight_geo_cells.
SELECT DISTINCT d.geo_cell::bigint AS geo_cell FROM rollup_daily_ask_demand d
WHERE d.date = @day::date AND d.geo_cell IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM insight_geo_cells c WHERE c.geo_cell = d.geo_cell);

-- name: InsertAskGeoCell :exec
INSERT INTO insight_geo_cells (geo_cell, centre)
VALUES (@geo_cell, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)
ON CONFLICT (geo_cell) DO NOTHING;

-- name: StoreAskDemand :many
-- Unanswered asks a store received per day and product (or query).
SELECT d.date, d.product_id, (CASE WHEN d.product_id IS NULL THEN d.query_norm ELSE '' END)::text AS query_key,
       sum(d.asks_without_yes)::bigint AS asks
FROM rollup_daily_ask_demand d
WHERE d.store_id = @store_id AND d.date >= @from_day::date AND d.date <= @to_day::date AND d.asks_without_yes > 0
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3;

-- name: DemandGaps :many
-- Products (or queries) that at least min_customers distinct customers of a
-- city asked for in the window with no Yes (01 §11.1, A-23 Gaps).
SELECT a.city_place_id, c.name AS city_name, a.product_id, p.name AS product_name, p.variant AS product_variant,
       (CASE WHEN a.product_id IS NULL THEN a.query_norm ELSE '' END)::text AS query_key,
       count(DISTINCT a.customer_id)::int AS customers, count(*)::int AS asks,
       sum(a.recipients_count)::int AS shops_asked, max(a.created_at)::timestamptz AS last_asked_at
FROM asks a
JOIN places c ON c.id = a.city_place_id
LEFT JOIN products p ON p.id = a.product_id
WHERE a.status = 'closed' AND a.created_at >= @since
  AND (sqlc.narg(city_place_id)::uuid IS NULL OR a.city_place_id = sqlc.narg(city_place_id)::uuid)
  AND NOT EXISTS (SELECT 1 FROM ask_recipients r WHERE r.ask_id = a.id AND r.answer = 'yes')
GROUP BY 1, 2, 3, 4, 5, 6
HAVING count(DISTINCT a.customer_id) >= sqlc.arg(min_customers)::int
ORDER BY customers DESC, asks DESC, 6
LIMIT 500;

-- name: InsertServerEvent :exec
INSERT INTO events (id, name, occurred_at, app, user_id, props)
VALUES (@id, @name, @occurred_at, 'api', @user_id, @props)
ON CONFLICT (id, occurred_at) DO NOTHING;
