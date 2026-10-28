-- offers queries, generated into package offersdb (gen/sqlc/offersdb).
-- Only W2-07 (internal/offers) edits this folder (D-50).

------------------------------------------------------------------------------
-- The shop's offers
------------------------------------------------------------------------------

-- name: InsertOffer :one
INSERT INTO offers (id, store_id, title, kind, value, buy_qty, get_qty, min_bill_paise, scope, store_item_ids, category_id,
                    channels, per_customer_limit, max_redemptions, starts_at, ends_at, status, source, batch_id, created_by,
                    created_at, updated_at)
VALUES (@id, @store_id, sqlc.narg('title'), @kind, @value, sqlc.narg('buy_qty'), sqlc.narg('get_qty'), sqlc.narg('min_bill_paise'),
        @scope, @store_item_ids::uuid[], sqlc.narg('category_id'), @channels, sqlc.narg('per_customer_limit'),
        sqlc.narg('max_redemptions'), @starts_at, sqlc.narg('ends_at'), @status, @source, sqlc.narg('batch_id'),
        sqlc.narg('created_by'), @now, @now)
RETURNING *;

-- name: GetOffer :one
SELECT * FROM offers WHERE id = @id AND store_id = @store_id;

-- name: LockOffer :one
SELECT * FROM offers WHERE id = @id AND store_id = @store_id FOR UPDATE;

-- name: ListOffers :many
-- A store's offers, newest first. status NULL lists every state.
SELECT * FROM offers
WHERE store_id = @store_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('after_id')::uuid IS NULL OR id < sqlc.narg('after_id')::uuid)
ORDER BY id DESC
LIMIT @lim;

-- name: UpdateOffer :one
UPDATE offers SET
  title = sqlc.narg('title'), value = @value, buy_qty = sqlc.narg('buy_qty'), get_qty = sqlc.narg('get_qty'),
  min_bill_paise = sqlc.narg('min_bill_paise'), scope = @scope, store_item_ids = @store_item_ids::uuid[],
  category_id = sqlc.narg('category_id'), channels = @channels, per_customer_limit = sqlc.narg('per_customer_limit'),
  max_redemptions = sqlc.narg('max_redemptions'), starts_at = @starts_at, ends_at = sqlc.narg('ends_at'),
  status = @status, version = version + 1, updated_at = @now
WHERE id = @id AND store_id = @store_id
RETURNING *;

-- name: ActiveFlashForBatch :one
SELECT id FROM offers WHERE batch_id = @batch_id AND status <> 'ended';

-- name: BatchForFlash :one
-- An expiring batch of one of the store's items, for a flash offer.
SELECT b.id, b.store_item_id, b.expires_on, b.quantity
FROM stock_batches b JOIN store_items si ON si.id = b.store_item_id
WHERE b.id = @batch_id AND si.store_id = @store_id;

-- name: ItemsOfStore :many
-- Which of these store items belong to the store (and their list price).
SELECT id, price_paise FROM store_items WHERE store_id = @store_id AND id = ANY(@ids::uuid[]);

-- name: CategoryExists :one
SELECT EXISTS (SELECT 1 FROM categories WHERE id = @id)::bool;

------------------------------------------------------------------------------
-- Pricing
------------------------------------------------------------------------------

-- name: PricingOffers :many
-- The offers that could apply to a cart of this store at time at: not paused or
-- ended, inside their dates. The channel, the redemption cap and the customer's
-- limit are applied by the engine (internal/offers).
SELECT * FROM offers
WHERE store_id = @store_id
  AND status IN ('scheduled', 'live')
  AND starts_at <= @at
  AND (ends_at IS NULL OR ends_at > @at)
ORDER BY id;

-- name: OffersOfStores :many
-- The same for several stores (the has-offer lookups and the feed).
SELECT * FROM offers
WHERE store_id = ANY(@store_ids::uuid[])
  AND status IN ('scheduled', 'live')
  AND starts_at <= @at
  AND (ends_at IS NULL OR ends_at > @at)
ORDER BY store_id, id;

-- name: ItemCategoryChains :many
-- For each store item: its product's category and all that category's
-- ancestors, so a category offer covers a sub-category's items.
WITH RECURSIVE chain AS (
  SELECT si.id AS store_item_id, p.category_id AS category_id
  FROM store_items si JOIN products p ON p.id = si.product_id
  WHERE si.id = ANY(@ids::uuid[])
  UNION
  SELECT ch.store_item_id, c.parent_id
  FROM chain ch JOIN categories c ON c.id = ch.category_id
  WHERE c.parent_id IS NOT NULL
)
SELECT store_item_id, array_agg(category_id)::uuid[] AS category_ids
FROM chain GROUP BY store_item_id;

-- name: CustomerUsage :many
-- How often the customer has used each offer (unreleased redemptions).
SELECT offer_id, count(*)::int AS used
FROM offer_redemptions
WHERE customer_id = @customer_id AND offer_id = ANY(@offer_ids::uuid[]) AND released_at IS NULL
GROUP BY offer_id;

------------------------------------------------------------------------------
-- Redemptions
------------------------------------------------------------------------------

-- name: ClaimOffer :one
-- Counts one use against the offer if it is still usable at time at (the row
-- lock serialises the claims of one offer). No row: it ran out or was paused.
UPDATE offers SET redemptions = redemptions + 1
WHERE id = @id
  AND status IN ('scheduled', 'live')
  AND starts_at <= @at
  AND (ends_at IS NULL OR ends_at > @at)
  AND (max_redemptions IS NULL OR redemptions < max_redemptions)
RETURNING per_customer_limit, store_id;

-- name: CustomerUses :one
SELECT count(*)::int FROM offer_redemptions
WHERE offer_id = @offer_id AND customer_id = @customer_id AND released_at IS NULL;

-- name: InsertRedemption :exec
INSERT INTO offer_redemptions (id, offer_id, order_id, pos_sale_id, customer_id, discount_paise, created_at)
VALUES (@id, @offer_id, sqlc.narg('order_id'), sqlc.narg('pos_sale_id'), sqlc.narg('customer_id'), @discount_paise, @now);

-- name: RedemptionsOfRef :many
SELECT offer_id FROM offer_redemptions
WHERE (order_id = sqlc.narg('order_id') OR pos_sale_id = sqlc.narg('pos_sale_id')) AND released_at IS NULL;

-- name: ReleaseRedemptions :many
-- Gives back every unreleased redemption of an order or POS sale and returns
-- the offers whose counters must go down.
UPDATE offer_redemptions SET released_at = @now
WHERE (order_id = sqlc.narg('order_id') OR pos_sale_id = sqlc.narg('pos_sale_id')) AND released_at IS NULL
RETURNING offer_id;

-- name: GiveBackUse :exec
UPDATE offers SET redemptions = GREATEST(redemptions - 1, 0) WHERE id = @id;

------------------------------------------------------------------------------
-- Scheduling (internal/jobs/offers.go)
------------------------------------------------------------------------------

-- name: LockStoresWithDueOffers :exec
-- The stores rows of every offer Tidy is about to move, in id order, before
-- it touches an offer: the catalogue writers' lock order (stores row first),
-- so Tidy cannot wait in a cycle with an order or a POS sale that holds a
-- store and then redeems or releases an offer (D-280).
SELECT s.id FROM stores s
WHERE s.id IN (
  SELECT o.store_id FROM offers o
  WHERE (o.status = 'scheduled' AND o.starts_at <= @now AND (o.ends_at IS NULL OR o.ends_at > @now))
     OR (o.status IN ('scheduled', 'live') AND o.ends_at IS NOT NULL AND o.ends_at <= @now)
     OR (o.status IN ('scheduled', 'live') AND o.max_redemptions IS NOT NULL AND o.redemptions >= o.max_redemptions)
)
ORDER BY s.id
FOR NO KEY UPDATE;

-- name: StartDueOffers :many
UPDATE offers SET status = 'live', version = version + 1, updated_at = @now
WHERE status = 'scheduled' AND starts_at <= @now AND (ends_at IS NULL OR ends_at > @now)
RETURNING id, store_id;

-- name: EndDueOffers :many
UPDATE offers SET status = 'ended', version = version + 1, updated_at = @now
WHERE status IN ('scheduled', 'live') AND ends_at IS NOT NULL AND ends_at <= @now
RETURNING id, store_id;

-- name: EndSpentOffers :many
-- An offer whose redemptions are all used up is over.
UPDATE offers SET status = 'ended', version = version + 1, updated_at = @now
WHERE status IN ('scheduled', 'live') AND max_redemptions IS NOT NULL AND redemptions >= max_redemptions
RETURNING id, store_id;

------------------------------------------------------------------------------
-- catalogue_changes (D-36): the POS picks offers up by this cursor
------------------------------------------------------------------------------

-- name: RecordCatalogueChange :one
WITH s AS (
  UPDATE stores SET catalogue_seq = catalogue_seq + 1 WHERE id = @store_id RETURNING catalogue_seq
)
INSERT INTO catalogue_changes (store_id, seq, entity, entity_id, op, created_at)
SELECT @store_id, s.catalogue_seq, 'offer', @entity_id, @op, @now FROM s
RETURNING seq;

------------------------------------------------------------------------------
-- The "Offers near you" feed
------------------------------------------------------------------------------

-- name: FeedStores :many
-- Live stores of verified businesses inside the radius, nearest first. The
-- service keeps those with a usable offer or a live banner; lim is a safety cap.
SELECT s.id, s.slug, s.name, s.locality, c.tint AS category_tint,
  ST_Distance(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::float8 AS distance_m
FROM stores s
JOIN businesses b ON b.id = s.business_id
LEFT JOIN categories c ON c.id = b.category_id
WHERE s.status = 'live' AND b.status = 'verified'
  AND ST_DWithin(s.location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @radius_m::float8)
ORDER BY distance_m, s.id
LIMIT @lim;

-- name: ItemPrices :many
-- The store and list price of store items (the has-offer lookup and the
-- ranking of a flat offer against a percent one).
SELECT id, store_id, price_paise FROM store_items WHERE id = ANY(@ids::uuid[]);

------------------------------------------------------------------------------
-- Events
------------------------------------------------------------------------------

-- name: InsertServerEvent :exec
INSERT INTO events (id, name, occurred_at, app, user_id, props)
VALUES (@id, @name, @occurred_at, 'api', @user_id, @props)
ON CONFLICT (id, occurred_at) DO NOTHING;
