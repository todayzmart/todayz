-- Share cards and the site's search strip (W3-16; 01 §11.1, §11.3; D-101).

-- name: ItemForCard :one
-- A store item with what its card and preview show. available is false once
-- the store is not live or its business not verified (D-40).
SELECT si.id, si.price_paise, si.mrp_paise, si.product_id,
       coalesce(si.local_name, p.name || coalesce(' ' || p.variant, ''))::text AS item_name,
       p.mrp_paise AS product_mrp_paise,
       s.name AS store_name, s.slug AS store_slug, s.locality,
       (s.status = 'live' AND b.status = 'verified')::bool AS available
FROM store_items si
JOIN products p ON p.id = si.product_id
JOIN stores s ON s.id = si.store_id
JOIN businesses b ON b.id = s.business_id
WHERE si.id = @store_item_id;

-- name: CardByItem :one
SELECT id, code, image_key, price_paise_rendered FROM share_cards WHERE store_item_id = @store_item_id;

-- name: CardByCode :one
SELECT id, store_item_id, code, image_key, price_paise_rendered FROM share_cards WHERE code = @code;

-- name: InsertCard :execrows
INSERT INTO share_cards (id, store_item_id, code, created_at)
VALUES (@id, @store_item_id, @code, @now)
ON CONFLICT (store_item_id) DO NOTHING;

-- name: SetCardRendered :exec
UPDATE share_cards SET image_key = @image_key, price_paise_rendered = @price_paise, rendered_at = @now
WHERE id = @id;

-- name: CountCreates :one
SELECT count(*)::int FROM share_card_creates WHERE key_hash = @key_hash AND created_at > @since;

-- name: InsertCreate :exec
INSERT INTO share_card_creates (key_hash, created_at) VALUES (@key_hash, @now);

-- name: DeleteOldCreates :exec
DELETE FROM share_card_creates WHERE created_at < @before;

-- name: MarqueeCellsMissing :many
-- Cells of the window's product searches with no centre in insight_geo_cells.
SELECT DISTINCT l.geo_cell::bigint AS geo_cell FROM search_log l
WHERE l.created_at >= @since AND l.created_at < @until AND l.product_id IS NOT NULL AND l.geo_cell IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM insight_geo_cells c WHERE c.geo_cell = l.geo_cell);

-- name: InsertGeoCell :exec
INSERT INTO insight_geo_cells (geo_cell, centre)
VALUES (@geo_cell, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)
ON CONFLICT (geo_cell) DO NOTHING;

-- name: MarqueeCandidates :many
-- Per city, master products searched in the window by at least min_searchers
-- distinct signed-in customers (anonymous searches add to searches_total
-- only), after the category denylist (the product's category and its
-- parent), ranked, at most max_items per city. A cell's city is the city
-- whose bbox holds its centre, else the nearest city.
WITH s AS (
  SELECT l.product_id, l.user_id,
         (SELECT p.id FROM places p WHERE p.kind = 'city'
          ORDER BY (p.bbox IS NOT NULL AND ST_Covers(p.bbox, g.centre)) DESC, p.location <-> g.centre
          LIMIT 1) AS city_place_id
  FROM search_log l
  JOIN insight_geo_cells g ON g.geo_cell = l.geo_cell
  WHERE l.created_at >= @since AND l.created_at < @until AND l.product_id IS NOT NULL
), agg AS (
  SELECT s.city_place_id, s.product_id,
         count(DISTINCT s.user_id)::int AS searchers, count(*)::int AS searches_total
  FROM s
  JOIN products pr ON pr.id = s.product_id
  LEFT JOIN categories c1 ON c1.id = pr.category_id
  LEFT JOIN categories c2 ON c2.id = c1.parent_id
  WHERE s.city_place_id IS NOT NULL
    AND NOT (coalesce(c1.slug, '') = ANY(@denylist::text[]) OR coalesce(c2.slug, '') = ANY(@denylist::text[]))
  GROUP BY 1, 2
  HAVING count(DISTINCT s.user_id) >= sqlc.arg(min_searchers)::int
), ranked AS (
  SELECT agg.*, row_number() OVER (PARTITION BY city_place_id ORDER BY searchers DESC, searches_total DESC, product_id)::int AS rank
  FROM agg
)
SELECT city_place_id::uuid AS city_place_id, product_id::uuid AS product_id, searchers, searches_total, rank
FROM ranked WHERE rank <= sqlc.arg(max_items)::int
ORDER BY city_place_id, rank;

-- name: DeleteProposedBuild :exec
-- A rerun of the same night replaces its own proposal, never a decided one.
DELETE FROM site_demand_marquee m
WHERE m.built_on = @built_on AND m.status = 'proposed'
  AND NOT EXISTS (SELECT 1 FROM site_demand_marquee d
                  WHERE d.city_place_id = m.city_place_id AND d.built_on = m.built_on AND d.status <> 'proposed');

-- name: DecidedCities :many
SELECT DISTINCT city_place_id FROM site_demand_marquee WHERE built_on = @built_on AND status <> 'proposed';

-- name: InsertMarquee :exec
INSERT INTO site_demand_marquee (id, city_place_id, product_id, searchers, searches_total, rank, built_on)
VALUES (@id, @city_place_id, @product_id, @searchers, @searches_total, @rank, @built_on)
ON CONFLICT (city_place_id, built_on, product_id) DO NOTHING;

-- name: ApprovedMarquee :many
-- The latest approved list of the city whose name slugifies to city_slug.
WITH c AS (
  SELECT p.id FROM places p
  WHERE p.kind = 'city' AND trim(BOTH '-' FROM regexp_replace(lower(p.name), '[^a-z0-9]+', '-', 'g')) = @city_slug::text
  ORDER BY p.id LIMIT 1
), b AS (
  SELECT max(m.built_on) AS built_on FROM site_demand_marquee m JOIN c ON c.id = m.city_place_id
  WHERE m.status = 'approved'
)
SELECT (pr.name || coalesce(' ' || pr.variant, ''))::text AS product_name
FROM site_demand_marquee m
JOIN c ON c.id = m.city_place_id
JOIN b ON b.built_on = m.built_on
JOIN products pr ON pr.id = m.product_id
WHERE m.status = 'approved'
ORDER BY m.rank
LIMIT 50;

-- name: MarqueeAdminRows :many
-- Per city, every row of its latest build and of its latest approved build.
WITH latest AS (
  SELECT city_place_id, max(built_on) AS built_on FROM site_demand_marquee GROUP BY 1
), appr AS (
  SELECT city_place_id, max(built_on) AS built_on FROM site_demand_marquee WHERE status = 'approved' GROUP BY 1
)
SELECT m.city_place_id, c.name AS city_name,
       trim(BOTH '-' FROM regexp_replace(lower(c.name), '[^a-z0-9]+', '-', 'g'))::text AS city_slug,
       m.built_on, m.product_id, (pr.name || coalesce(' ' || pr.variant, ''))::text AS product_name,
       (cat.names ->> 'en')::text AS category_name,
       m.searchers, m.searches_total, m.rank, m.status, m.decided_at,
       (m.built_on = l.built_on)::bool AS is_latest,
       (a.built_on IS NOT NULL AND m.built_on = a.built_on)::bool AS is_approved
FROM site_demand_marquee m
JOIN latest l ON l.city_place_id = m.city_place_id
LEFT JOIN appr a ON a.city_place_id = m.city_place_id
JOIN places c ON c.id = m.city_place_id
JOIN products pr ON pr.id = m.product_id
LEFT JOIN categories cat ON cat.id = pr.category_id
WHERE (m.built_on = l.built_on OR m.built_on = a.built_on)
  AND (sqlc.narg(city_place_id)::uuid IS NULL OR m.city_place_id = sqlc.narg(city_place_id)::uuid)
ORDER BY c.name, m.city_place_id, m.built_on DESC, m.rank;

-- name: LatestBuildOn :one
SELECT max(built_on)::date FROM site_demand_marquee WHERE city_place_id = @city_place_id;

-- name: LockBuild :many
SELECT product_id, status FROM site_demand_marquee
WHERE city_place_id = @city_place_id AND built_on = @built_on
ORDER BY product_id
FOR UPDATE;

-- name: DecideBuild :execrows
UPDATE site_demand_marquee
SET status = CASE WHEN product_id = ANY(@approve_ids::uuid[]) THEN 'approved' ELSE 'rejected' END,
    decided_by = @decided_by, decided_at = @now
WHERE city_place_id = @city_place_id AND built_on = @built_on AND status = 'proposed';
