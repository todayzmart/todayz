-- search queries, generated into package searchdb (gen/sqlc/searchdb).
-- Only the work package that builds internal/search edits this folder (D-50).
--
-- W1-06: places, discover and search (01 §11, 04 §2.3). Every customer-facing
-- query shows only `live` stores of `verified` businesses (D-40). The point is
-- always ST_SetSRID(ST_MakePoint(lng, lat), 4326)::geography, written out in
-- each query (not a CTE) so the planner can use ix_stores_location, which is
-- partial on status = 'live': every query names s.status = 'live' literally.

------------------------------------------------------------------------------
-- Places
------------------------------------------------------------------------------

-- name: PlacesByPincode :many
-- PIN autocomplete: places whose PIN starts with the typed digits.
SELECT p.id, p.kind, p.name, p.pincode, c.name AS city,
  ST_Y(p.location::geometry)::float8 AS lat, ST_X(p.location::geometry)::float8 AS lng
FROM places p LEFT JOIN places c ON c.id = p.city_id
WHERE p.pincode LIKE @prefix::text || '%'
ORDER BY p.pincode, p.kind, p.name, p.id
LIMIT @lim;

-- name: PlacesByName :many
-- Locality and city autocomplete: a name containing the text (pattern is the
-- escaped ILIKE pattern), or close to it (trigram, for typos); names that
-- start with it first. Cities match on their names in every language too.
SELECT p.id, p.kind, p.name, p.pincode, c.name AS city,
  ST_Y(p.location::geometry)::float8 AS lat, ST_X(p.location::geometry)::float8 AS lng
FROM places p LEFT JOIN places c ON c.id = p.city_id
WHERE p.name ILIKE @pattern::text
   OR p.name % @q::text
   OR (p.kind = 'city' AND EXISTS (SELECT 1 FROM jsonb_each_text(p.names) n WHERE n.value ILIKE @pattern::text))
ORDER BY (p.name ILIKE @prefix_pattern::text) DESC, similarity(p.name, @q::text) DESC, p.kind, p.name, p.id
LIMIT @lim;

-- name: NearestPlace :one
-- Reverse geocode: the nearest place of a kind within max_m (KNN on the GiST
-- index, then the exact distance).
SELECT p.id, p.kind, p.name, p.pincode, c.name AS city,
  ST_Y(p.location::geometry)::float8 AS lat, ST_X(p.location::geometry)::float8 AS lng,
  ST_Distance(p.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::float8 AS distance_m
FROM places p LEFT JOIN places c ON c.id = p.city_id
WHERE p.kind = @kind::text
  AND ST_DWithin(p.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @max_m::float8)
ORDER BY p.location <-> ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, p.id
LIMIT 1;

------------------------------------------------------------------------------
-- Discover: stores in the radius
------------------------------------------------------------------------------

-- name: StoresInRadius :many
-- Live stores of verified businesses within radius_m, nearest first, with what
-- the summary pins, the store list and the open-now check need. lim is a
-- safety cap (the service filters open-now and offers after this).
SELECT s.id, s.slug, s.name, s.locality, s.hours, s.closed_until,
  s.accepts_pickup, s.accepts_delivery, s.delivery_fee_paise, s.delivery_free_above_paise,
  s.cashback_bp, (s.rating_avg IS NOT NULL)::bool AS has_rating, COALESCE(s.rating_avg, 0)::float8 AS rating_avg,
  s.rating_count, c.tint AS category_tint,
  ST_Y(s.location::geometry)::float8 AS lat, ST_X(s.location::geometry)::float8 AS lng,
  ST_Distance(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::float8 AS distance_m
FROM stores s
JOIN businesses b ON b.id = s.business_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE s.status = 'live' AND b.status = 'verified'
  AND ST_DWithin(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @radius_m::float8)
  AND (sqlc.narg(category_ids)::uuid[] IS NULL
       OR b.category_id = ANY(sqlc.narg(category_ids)::uuid[])
       OR EXISTS (SELECT 1 FROM store_items si JOIN products p ON p.id = si.product_id
                  WHERE si.store_id = s.id AND si.is_listed AND si.on_hand - si.reserved > 0
                    AND p.category_id = ANY(sqlc.narg(category_ids)::uuid[])))
ORDER BY distance_m, s.id
LIMIT @lim;

-- name: CountStoresInRadius :one
-- The summary line's N (D-101): live stores of verified businesses inside the
-- radius, open or closed, whatever the filters.
SELECT count(*)::int AS n
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE s.status = 'live' AND b.status = 'verified'
  AND ST_DWithin(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @radius_m::float8);

-- name: CountListingsInRadius :one
-- productsInRadius (04 §2.3): in-stock listings across live stores within the
-- radius (not distinct catalogue products).
SELECT count(*)::int AS n
FROM stores s
JOIN businesses b ON b.id = s.business_id
JOIN store_items si ON si.store_id = s.id
WHERE s.status = 'live' AND b.status = 'verified'
  AND ST_DWithin(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @radius_m::float8)
  AND si.is_listed AND si.on_hand - si.reserved > 0;

------------------------------------------------------------------------------
-- Shop page
------------------------------------------------------------------------------

-- name: StoreBySlug :one
-- The shop page: a live store of a verified business, else no row (404).
-- distance_m is null unless the request gave a point.
SELECT s.id, s.slug, s.name, s.locality, s.address_line1, s.address_line2, s.landmark, s.city, s.pincode,
  s.hours, s.closed_until, s.accepts_pickup, s.accepts_delivery,
  s.delivery_fee_paise, s.delivery_free_above_paise, s.delivery_min_order_paise, s.delivery_radius_m,
  s.cashback_bp, (s.rating_avg IS NOT NULL)::bool AS has_rating, COALESCE(s.rating_avg, 0)::float8 AS rating_avg,
  s.rating_count, c.tint AS category_tint, c.slug AS category_slug,
  COALESCE(NULLIF(c.names->>'en', ''), c.slug, '') AS category_name, -- '' when the business has no category (the Go code then sends null)
  ST_Y(s.location::geometry)::float8 AS lat, ST_X(s.location::geometry)::float8 AS lng,
  (sqlc.narg(lng)::float8 IS NOT NULL AND sqlc.narg(lat)::float8 IS NOT NULL)::bool AS has_distance,
  COALESCE(ST_Distance(s.location, ST_SetSRID(ST_MakePoint(sqlc.narg(lng)::float8, sqlc.narg(lat)::float8), 4326)::geography), 0)::float8 AS distance_m
FROM stores s
JOIN businesses b ON b.id = s.business_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE s.slug = @slug AND s.status = 'live' AND b.status = 'verified' AND s.location IS NOT NULL;

-- name: StoreItemsPage :many
-- The shop page catalogue: listed items of products that are not retired or
-- merged, in stock first, then by name. tsq (optional) filters by the item's
-- search vector, category_ids (optional) by category. lim/off page it.
SELECT si.id AS store_item_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  (si.on_hand - si.reserved > 0)::bool AS in_stock, si.stock_updated_at,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit, p.image_key,
  br.name AS brand_name
FROM store_items si
JOIN products p ON p.id = si.product_id
LEFT JOIN brands br ON br.id = p.brand_id
WHERE si.store_id = @store_id AND si.is_listed
  AND p.status IN ('active', 'pending_review')
  AND (sqlc.narg(tsq)::text IS NULL OR si.search @@ to_tsquery('simple', sqlc.narg(tsq)::text))
  AND (sqlc.narg(category_ids)::uuid[] IS NULL OR p.category_id = ANY(sqlc.narg(category_ids)::uuid[]))
ORDER BY (si.on_hand - si.reserved > 0) DESC, lower(COALESCE(si.local_name, p.name)), si.id
LIMIT @lim OFFSET @off;

------------------------------------------------------------------------------
-- Search
------------------------------------------------------------------------------

-- name: SearchItems :many
-- Filter first (01 §11): in stock, listed, live store of a verified business,
-- within the radius. An item matches when its search vector matches tsq (the
-- normalised words with their synonyms), or its product is one of typo_ids
-- (trigram on the product name), or its product is in match_category_ids (the
-- query names a category). brand_id, max_price and category_ids are the
-- request's filters. product_id (optional) limits it to one master product and
-- also lets that product match without the text. fts and sim feed the text
-- score in Go. lim is a safety cap.
SELECT si.id AS store_item_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  (si.on_hand - si.reserved)::int AS available, si.stock_updated_at,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit, p.image_key,
  p.category_id, p.brand_id, br.name AS brand_name,
  (si.search @@ to_tsquery('simple', @tsq::text))::bool AS fts,
  GREATEST(similarity(p.name, @q::text), similarity(COALESCE(si.local_name, ''), @q::text))::float8 AS sim,
  s.id AS store_id, s.slug AS store_slug, s.name AS store_name, s.locality AS store_locality,
  s.hours, s.closed_until, s.accepts_pickup, s.accepts_delivery,
  s.delivery_fee_paise, s.delivery_free_above_paise, s.cashback_bp,
  (s.rating_avg IS NOT NULL)::bool AS has_rating, COALESCE(s.rating_avg, 0)::float8 AS rating_avg, s.rating_count,
  c.tint AS category_tint,
  (s.reliability_score IS NOT NULL)::bool AS has_reliability, COALESCE(s.reliability_score, 0)::float8 AS reliability,
  ST_Distance(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::float8 AS distance_m
FROM stores s
JOIN businesses b ON b.id = s.business_id
JOIN store_items si ON si.store_id = s.id
JOIN products p ON p.id = si.product_id
LEFT JOIN brands br ON br.id = p.brand_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE s.status = 'live' AND b.status = 'verified'
  AND ST_DWithin(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @radius_m::float8)
  AND si.is_listed AND si.on_hand - si.reserved > 0
  AND p.status IN ('active', 'pending_review')
  AND (si.search @@ to_tsquery('simple', @tsq::text)
       OR si.product_id = ANY(@typo_ids::uuid[])
       OR p.category_id = ANY(@match_category_ids::uuid[])
       OR si.product_id = sqlc.narg(product_id)::uuid)
  AND (sqlc.narg(product_id)::uuid IS NULL OR si.product_id = sqlc.narg(product_id)::uuid)
  AND (sqlc.narg(brand_id)::uuid IS NULL OR p.brand_id = sqlc.narg(brand_id)::uuid)
  AND (sqlc.narg(max_price)::bigint IS NULL OR si.price_paise <= sqlc.narg(max_price)::bigint)
  AND (sqlc.narg(category_ids)::uuid[] IS NULL OR p.category_id = ANY(sqlc.narg(category_ids)::uuid[]))
LIMIT @lim;

-- name: TypoProducts :many
-- Typo tolerance: active master products whose name is close to q (pg_trgm %,
-- the extension's default threshold), best first.
SELECT id FROM products
WHERE status = 'active' AND name % @q::text
ORDER BY similarity(name, @q::text) DESC, id
LIMIT @lim;

-- name: ListCategories :many
SELECT id, parent_id, slug, names FROM categories ORDER BY sort, slug;

-- name: BrandBySlug :one
SELECT id FROM brands WHERE slug = @slug;

-- name: CatalogueKnows :one
-- Whether any active master product's name or variant holds the words of tsq (the
-- catalogue, not the stock nearby). Search asks it only for a query the synonym
-- table missed and that found no listing, to decide refineHint (D-238).
SELECT EXISTS (
  SELECT 1 FROM products WHERE status = 'active' AND search @@ to_tsquery('simple', @tsq::text)
)::bool AS known;

-- name: SynonymsFor :many
-- Synonym rows touching any of the phrases, either way round.
SELECT term, synonym, product_id FROM product_synonyms
WHERE term = ANY(@phrases::text[]) OR synonym = ANY(@phrases::text[])
ORDER BY term, synonym, id;

-- name: SynonymsOfTerms :many
-- Every synonym of the given terms (the second hop: dahi → curd → thayir).
SELECT term, synonym FROM product_synonyms
WHERE term = ANY(@terms::text[]) AND product_id IS NULL
ORDER BY term, synonym, id;

------------------------------------------------------------------------------
-- D-99: the search's product_id
------------------------------------------------------------------------------

-- name: ProductsByBarcode :many
SELECT id FROM products
WHERE status = 'active' AND barcodes @> ARRAY[@code::text]
ORDER BY id
LIMIT 2;

-- name: ProductsByWords :many
-- Candidates for the exact-name rule: active products whose name and variant
-- hold the words of one of the phrases (tsq, no prefixes). The service then
-- compares normalised names exactly.
SELECT id, name, variant, pack_size::float8 AS pack_size, unit FROM products
WHERE status = 'active' AND search @@ to_tsquery('simple', @tsq::text)
ORDER BY id
LIMIT @lim;

------------------------------------------------------------------------------
-- Suggestions
------------------------------------------------------------------------------

-- name: SuggestProducts :many
-- Active master products by name: prefix or contained word first, then typos.
SELECT p.id, p.name, p.variant, p.pack_size::float8 AS pack_size, p.unit, br.name AS brand_name
FROM products p LEFT JOIN brands br ON br.id = p.brand_id
WHERE p.status = 'active' AND (p.name ILIKE @pattern::text OR p.name % @q::text)
ORDER BY (p.name ILIKE @prefix_pattern::text) DESC, similarity(p.name, @q::text) DESC, p.name, p.id
LIMIT @lim;

-- name: SuggestBrands :many
SELECT id, name, slug FROM brands
WHERE name ILIKE @pattern::text OR name % @q::text
ORDER BY (name ILIKE @prefix_pattern::text) DESC, similarity(name, @q::text) DESC, name, id
LIMIT @lim;

------------------------------------------------------------------------------
-- Logging
------------------------------------------------------------------------------

-- name: InsertSearchLog :exec
-- One row per answered search, zero results too (01 §11). Never coordinates.
INSERT INTO search_log (id, created_at, query_norm, results_count, store_count, radius_m,
  geo_cell, category, product_id, user_id, source)
VALUES (@id, @created_at, @query_norm, @results_count, @store_count, @radius_m,
  sqlc.narg(geo_cell), sqlc.narg(category), @product_id, @user_id, @source);

-- name: InsertServerEvent :exec
-- A server-emitted product event (05 §2.6, app 'api'); props are validated
-- against the catalogue first.
INSERT INTO events (id, name, occurred_at, app, user_id, props)
VALUES (@id, @name, @occurred_at, 'api', @user_id, @props)
ON CONFLICT (id, occurred_at) DO NOTHING;

-- name: BasketProductItems :many
-- W2-14 basket search, the product lines: listings of these master products
-- under SearchItems's filter (in stock, listed, live store of a verified
-- business, within the radius). The columns are SearchItems's, in its order,
-- so Go reads both row types the same way; fts and sim are constant (a named
-- product is an exact match).
SELECT si.id AS store_item_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  (si.on_hand - si.reserved)::int AS available, si.stock_updated_at,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit, p.image_key,
  p.category_id, p.brand_id, br.name AS brand_name,
  true AS fts,
  1::float8 AS sim,
  s.id AS store_id, s.slug AS store_slug, s.name AS store_name, s.locality AS store_locality,
  s.hours, s.closed_until, s.accepts_pickup, s.accepts_delivery,
  s.delivery_fee_paise, s.delivery_free_above_paise, s.cashback_bp,
  (s.rating_avg IS NOT NULL)::bool AS has_rating, COALESCE(s.rating_avg, 0)::float8 AS rating_avg, s.rating_count,
  c.tint AS category_tint,
  (s.reliability_score IS NOT NULL)::bool AS has_reliability, COALESCE(s.reliability_score, 0)::float8 AS reliability,
  ST_Distance(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::float8 AS distance_m
FROM stores s
JOIN businesses b ON b.id = s.business_id
JOIN store_items si ON si.store_id = s.id
JOIN products p ON p.id = si.product_id
LEFT JOIN brands br ON br.id = p.brand_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE s.status = 'live' AND b.status = 'verified'
  AND ST_DWithin(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @radius_m::float8)
  AND si.is_listed AND si.on_hand - si.reserved > 0
  AND p.status IN ('active', 'pending_review')
  AND si.product_id = ANY(@product_ids::uuid[])
LIMIT @lim;

-- name: BasketProductNames :many
-- The names of a basket's product lines, for the search log (any status).
SELECT id, name FROM products WHERE id = ANY(@ids::uuid[]);

------------------------------------------------------------------------------
-- Substitutes (W3-12, AI-C3 rule-based, D-235)
------------------------------------------------------------------------------

-- name: SubstituteSource :one
-- The item substitutes are found for: a listed item of the store whose product
-- is not retired or merged, else no row (404).
SELECT si.product_id, p.category_id, p.brand_id, p.pack_size::float8 AS pack_size, p.unit
FROM store_items si
JOIN products p ON p.id = si.product_id
WHERE si.id = @store_item_id AND si.store_id = @store_id AND si.is_listed
  AND p.status IN ('active', 'pending_review');

-- name: SubstituteShopItems :many
-- The same shop's in-stock items of other products in the category, in one of
-- units, whose pack size in base units (g, ml, pc, pack; kg and l × 1000) is
-- between min_base and max_base. The columns are SearchItems's, in its order,
-- so Go reads them the same way. The store is the caller's live store.
SELECT si.id AS store_item_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  (si.on_hand - si.reserved)::int AS available, si.stock_updated_at,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit, p.image_key,
  p.category_id, p.brand_id, br.name AS brand_name,
  true AS fts,
  1::float8 AS sim,
  s.id AS store_id, s.slug AS store_slug, s.name AS store_name, s.locality AS store_locality,
  s.hours, s.closed_until, s.accepts_pickup, s.accepts_delivery,
  s.delivery_fee_paise, s.delivery_free_above_paise, s.cashback_bp,
  (s.rating_avg IS NOT NULL)::bool AS has_rating, COALESCE(s.rating_avg, 0)::float8 AS rating_avg, s.rating_count,
  c.tint AS category_tint,
  (s.reliability_score IS NOT NULL)::bool AS has_reliability, COALESCE(s.reliability_score, 0)::float8 AS reliability,
  ST_Distance(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::float8 AS distance_m
FROM stores s
JOIN businesses b ON b.id = s.business_id
JOIN store_items si ON si.store_id = s.id
JOIN products p ON p.id = si.product_id
LEFT JOIN brands br ON br.id = p.brand_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE s.id = @store_id AND s.status = 'live' AND b.status = 'verified'
  AND si.is_listed AND si.on_hand - si.reserved > 0
  AND p.status IN ('active', 'pending_review')
  AND p.category_id = @category_id AND p.id <> @product_id
  AND p.unit = ANY(@units::text[])
  AND (p.pack_size * CASE WHEN p.unit IN ('kg', 'l') THEN 1000 ELSE 1 END)::float8
      BETWEEN @min_base::float8 AND @max_base::float8
LIMIT @lim;
