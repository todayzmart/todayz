-- catalogue queries, generated into package cataloguedb (gen/sqlc/cataloguedb).
-- Only W1-05 (internal/catalogue) edits this folder (D-50). Every time the
-- code compares or records is a parameter from the application clock, never
-- now(). Pack sizes travel as float8 (at most 3 decimals, 03 §3).

------------------------------------------------------------------------------
-- Categories and brands
------------------------------------------------------------------------------

-- name: ListCategories :many
-- Top-level categories first, then children, each by sort.
SELECT * FROM categories
ORDER BY (parent_id IS NOT NULL), sort, slug;

-- name: CategoryExists :one
SELECT EXISTS (SELECT 1 FROM categories WHERE id = @id) AS ok;

-- name: InsertBrand :one
INSERT INTO brands (id, name, slug, created_at, updated_at)
VALUES (@id, @name, @slug, @now, @now)
ON CONFLICT (slug) DO UPDATE SET name = brands.name
RETURNING id;

------------------------------------------------------------------------------
-- Master products
------------------------------------------------------------------------------

-- name: InsertProduct :exec
INSERT INTO products (id, name, brand_id, category_id, variant, pack_size, unit, mrp_paise,
  image_key, barcodes, status, created_by_store_id, created_at, updated_at)
VALUES (@id, @name, sqlc.narg(brand_id), @category_id, sqlc.narg(variant), @pack_size::float8, @unit,
  sqlc.narg(mrp_paise), sqlc.narg(image_key), @barcodes::text[], @status, sqlc.narg(created_by_store_id), @now, @now);

-- name: GetProduct :one
SELECT p.id, p.name, p.brand_id, b.name AS brand_name, p.category_id, p.variant,
  p.pack_size::float8 AS pack_size, p.unit, p.mrp_paise, p.image_key, p.barcodes, p.status
FROM products p LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.id = @id;

-- name: SearchProducts :many
-- The master catalogue for "add an item" (D-37): active products only. A
-- barcode is an exact match; otherwise the words match the name, variant or
-- brand (pattern is the escaped ILIKE pattern of q), best trigram match first.
-- in_store is true when the store already lists the product.
SELECT p.id, p.name, p.brand_id, b.name AS brand_name, p.category_id, p.variant,
  p.pack_size::float8 AS pack_size, p.unit, p.mrp_paise, p.image_key, p.barcodes,
  EXISTS (SELECT 1 FROM store_items si WHERE si.product_id = p.id AND si.store_id = sqlc.narg(store_id)::uuid) AS in_store
FROM products p LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.status = 'active'
  AND (sqlc.narg(barcode)::text IS NULL OR p.barcodes @> ARRAY[sqlc.narg(barcode)::text])
  AND (sqlc.narg(q)::text IS NULL
       OR p.name ILIKE sqlc.narg(pattern)::text
       OR p.variant ILIKE sqlc.narg(pattern)::text
       OR b.name ILIKE sqlc.narg(pattern)::text
       OR p.search @@ plainto_tsquery('simple', sqlc.narg(q)::text))
ORDER BY similarity(p.name, coalesce(sqlc.narg(q)::text, '')) DESC, p.name, p.id
LIMIT @lim;

------------------------------------------------------------------------------
-- Store items
------------------------------------------------------------------------------

-- name: InsertStoreItem :one
INSERT INTO store_items (id, store_id, product_id, local_name, price_paise, mrp_paise,
  low_stock_at, is_listed, search, created_at, updated_at)
SELECT @id, @store_id, p.id, sqlc.narg(local_name), @price_paise, sqlc.narg(mrp_paise),
  sqlc.narg(low_stock_at), @is_listed,
  p.search || setweight(to_tsvector('simple'::regconfig, coalesce(b.name, '')), 'B')
           || setweight(to_tsvector('simple'::regconfig, coalesce(sqlc.narg(local_name)::text, '')), 'A'),
  @now, @now
FROM products p LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.id = @product_id
RETURNING id;

-- name: GetStoreItem :one
-- One store item with its product, for a store (a wrong store finds nothing).
SELECT si.id, si.store_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  si.on_hand, si.reserved, si.low_stock_at, si.is_listed, si.stock_updated_at,
  si.created_at, si.updated_at, si.version,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit, p.image_key,
  p.barcodes, p.category_id, p.brand_id, b.name AS brand_name, p.mrp_paise AS product_mrp_paise
FROM store_items si
JOIN products p ON p.id = si.product_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE si.id = @id AND si.store_id = @store_id;

-- name: LookupStoreItem :one
-- The read interface for orders and POS: price, counters and listing of one
-- item of one store.
SELECT si.id, si.store_id, si.product_id, si.price_paise, si.mrp_paise, si.on_hand, si.reserved,
  si.is_listed, si.stock_updated_at, coalesce(si.local_name, p.name)::text AS name
FROM store_items si JOIN products p ON p.id = si.product_id
WHERE si.id = @id AND si.store_id = @store_id;

-- name: LockStoreItem :one
-- Locks the item row for a stock write (lock order: the store's catalogue
-- sequence first, then the item, then its batches by id).
SELECT id, store_id, on_hand, reserved, price_paise, is_listed
FROM store_items WHERE id = @id AND store_id = @store_id
FOR UPDATE;

-- name: StoreItemIDByProduct :one
SELECT id FROM store_items WHERE store_id = @store_id AND product_id = @product_id;

-- name: ListStoreItems :many
-- B-07: a store's items by product name, then id (keyset after_name/after_id).
-- Filters: q (escaped ILIKE pattern over the name, local name, variant and
-- brand), category (the category or its children), low stock (available at
-- or below the item's threshold, else low_default) and listed only.
SELECT si.id, si.store_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  si.on_hand, si.reserved, si.low_stock_at, si.is_listed, si.stock_updated_at,
  si.created_at, si.updated_at, si.version,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit, p.image_key,
  p.barcodes, p.category_id, p.brand_id, b.name AS brand_name, p.mrp_paise AS product_mrp_paise
FROM store_items si
JOIN products p ON p.id = si.product_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE si.store_id = @store_id
  AND (sqlc.narg(pattern)::text IS NULL
       OR p.name ILIKE sqlc.narg(pattern)::text
       OR si.local_name ILIKE sqlc.narg(pattern)::text
       OR p.variant ILIKE sqlc.narg(pattern)::text
       OR b.name ILIKE sqlc.narg(pattern)::text
       OR p.barcodes @> ARRAY[sqlc.narg(q)::text])
  AND (sqlc.narg(category_id)::uuid IS NULL
       OR p.category_id = sqlc.narg(category_id)::uuid
       OR p.category_id IN (SELECT c.id FROM categories c WHERE c.parent_id = sqlc.narg(category_id)::uuid))
  AND (NOT @low_only::bool OR si.on_hand - si.reserved <= coalesce(si.low_stock_at, @low_default::int))
  AND (NOT @listed_only::bool OR si.is_listed)
  AND (sqlc.narg(after_name)::text IS NULL
       OR (p.name, si.id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid))
ORDER BY p.name, si.id
LIMIT @lim;

-- name: UpdateStoreItem :one
-- Only the fields whose set_* flag is true change; a stale version matches no
-- row. Counters are never written here.
UPDATE store_items SET
  price_paise  = CASE WHEN @set_price::bool THEN @price_paise::bigint ELSE price_paise END,
  mrp_paise    = CASE WHEN @set_mrp::bool THEN sqlc.narg(mrp_paise)::bigint ELSE mrp_paise END,
  is_listed    = CASE WHEN @set_listed::bool THEN @is_listed::bool ELSE is_listed END,
  low_stock_at = CASE WHEN @set_low_stock::bool THEN sqlc.narg(low_stock_at)::int ELSE low_stock_at END,
  version = version + 1
WHERE id = @id AND store_id = @store_id AND version = @version
RETURNING id;

-- name: ConfirmStock :exec
-- A count with no change (01 §4): the freshness moment moves, no movement.
UPDATE store_items
   SET stock_updated_at = GREATEST(coalesce(stock_updated_at, @now::timestamptz), @now::timestamptz)
 WHERE id = @id;

------------------------------------------------------------------------------
-- Movements and batches
------------------------------------------------------------------------------

-- name: InsertMovement :one
INSERT INTO stock_movements (id, store_id, store_item_id, delta, source, ref_type, ref_id,
  actor_user_id, device_id, client_id, note, created_at)
VALUES (@id, @store_id, @store_item_id, @delta, @source, sqlc.narg(ref_type), sqlc.narg(ref_id),
  sqlc.narg(actor_user_id), sqlc.narg(device_id), sqlc.narg(client_id), sqlc.narg(note), @created_at)
RETURNING *;

-- name: InsertMovementBatch :exec
-- The trigger moves the batch's remaining quantity by delta.
INSERT INTO stock_movement_batches (movement_id, batch_id, delta) VALUES (@movement_id, @batch_id, @delta);

-- name: InsertBatch :exec
-- A new batch starts at 0; its first allocation row gives it its quantity.
INSERT INTO stock_batches (id, store_item_id, received_quantity, quantity, expires_on, received_at,
  cost_paise, found_in_count, created_at, updated_at)
VALUES (@id, @store_item_id, @received_quantity, 0, sqlc.narg(expires_on), @received_at,
  sqlc.narg(cost_paise), @found_in_count, @received_at, @received_at);

-- name: LockItemBatches :many
-- Every batch of the item with remaining stock, locked, in id order.
SELECT id, store_item_id, received_quantity, quantity, expires_on, received_at, cost_paise, found_in_count
FROM stock_batches
WHERE store_item_id = @store_item_id AND quantity > 0
ORDER BY id
FOR UPDATE;

-- name: LockBatchesByID :many
-- The named batches of one item (with or without remaining stock), locked.
SELECT id, store_item_id, received_quantity, quantity, expires_on, received_at, cost_paise, found_in_count
FROM stock_batches
WHERE store_item_id = @store_item_id AND id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: GetMovement :one
SELECT * FROM stock_movements WHERE id = @id AND store_id = @store_id;

-- name: MovementByClientID :one
SELECT * FROM stock_movements WHERE store_id = @store_id AND client_id = @client_id;

-- name: MovementAllocations :many
SELECT batch_id, delta FROM stock_movement_batches WHERE movement_id = @movement_id ORDER BY batch_id;

-- name: ReversedFrom :one
-- What has been put back already against a source movement: the total and
-- per batch (as jsonb {batchId: qty}).
SELECT coalesce(sum(m.delta), 0)::bigint AS total,
  coalesce((SELECT jsonb_object_agg(x.batch_id::text, x.qty) FROM (
     SELECT a.batch_id, sum(a.delta) AS qty
     FROM stock_movements r JOIN stock_movement_batches a ON a.movement_id = r.id
     WHERE r.source = 'pos_reversal' AND r.ref_type = 'stock_movement' AND r.ref_id = @source_id
     GROUP BY a.batch_id) x), '{}'::jsonb)::jsonb AS per_batch
FROM stock_movements m
WHERE m.source = 'pos_reversal' AND m.ref_type = 'stock_movement' AND m.ref_id = @source_id;

-- name: ItemHistory :many
-- An item's movements, newest first (keyset before_at/before_id), each with
-- its batch allocation as [{batchId, delta}].
SELECT m.id, m.delta, m.source, m.ref_type, m.ref_id, m.actor_user_id, m.note, m.created_at,
  coalesce((SELECT jsonb_agg(jsonb_build_object('batchId', a.batch_id, 'delta', a.delta) ORDER BY a.batch_id)
            FROM stock_movement_batches a WHERE a.movement_id = m.id), '[]'::jsonb)::jsonb AS batches
FROM stock_movements m
WHERE m.store_item_id = @store_item_id
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (m.created_at, m.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY m.created_at DESC, m.id DESC
LIMIT @lim;

-- name: ListStoreBatches :many
-- {S}/batches: batches with remaining stock (or all, with include_empty),
-- earliest expiry first, undated after dated, then oldest received.
SELECT b.id, b.store_item_id, b.received_quantity, b.quantity, b.expires_on, b.received_at,
  b.cost_paise, b.found_in_count, b.version, b.updated_at,
  coalesce(si.local_name, p.name)::text AS item_name, si.price_paise, coalesce(si.mrp_paise, p.mrp_paise) AS mrp_paise
FROM stock_batches b
JOIN store_items si ON si.id = b.store_item_id
JOIN products p ON p.id = si.product_id
WHERE si.store_id = @store_id
  AND (@include_empty::bool OR b.quantity > 0)
  AND (sqlc.narg(store_item_id)::uuid IS NULL OR b.store_item_id = sqlc.narg(store_item_id)::uuid)
  AND (sqlc.narg(expires_before)::date IS NULL OR b.expires_on < sqlc.narg(expires_before)::date)
ORDER BY b.expires_on NULLS LAST, b.received_at, b.id
LIMIT @lim;

-- name: GetStoreBatch :one
SELECT b.id, b.store_item_id, b.received_quantity, b.quantity, b.expires_on, b.received_at,
  b.cost_paise, b.found_in_count, b.version, b.updated_at,
  coalesce(si.local_name, p.name)::text AS item_name, si.price_paise, coalesce(si.mrp_paise, p.mrp_paise) AS mrp_paise
FROM stock_batches b
JOIN store_items si ON si.id = b.store_item_id
JOIN products p ON p.id = si.product_id
WHERE b.id = @id AND si.store_id = @store_id;

-- name: UpdateBatch :one
-- Cost and expiry only (D-99); a stale version matches no row. A found-in-
-- count batch keeps no expiry (ck_stock_batches_found_in_count).
UPDATE stock_batches b SET
  cost_paise = CASE WHEN @set_cost::bool THEN sqlc.narg(cost_paise)::bigint ELSE b.cost_paise END,
  expires_on = CASE WHEN @set_expires::bool THEN sqlc.narg(expires_on)::date ELSE b.expires_on END,
  version = b.version + 1
FROM store_items si
WHERE b.id = @id AND b.version = @version AND si.id = b.store_item_id AND si.store_id = @store_id
RETURNING b.id;

------------------------------------------------------------------------------
-- catalogue_changes (D-36)
------------------------------------------------------------------------------

-- name: RecordCatalogueChange :one
-- Takes the next seq from stores.catalogue_seq (the row lock is held to the
-- end of the transaction, so a store's changes commit in seq order).
WITH s AS (
  UPDATE stores SET catalogue_seq = catalogue_seq + 1 WHERE id = @store_id RETURNING catalogue_seq
)
INSERT INTO catalogue_changes (store_id, seq, entity, entity_id, op, created_at)
SELECT @store_id, s.catalogue_seq, @entity, @entity_id, @op, @now FROM s
RETURNING seq;

------------------------------------------------------------------------------
-- Reconcile (internal/jobs/catalogue.go)
------------------------------------------------------------------------------

-- name: OnHandDrift :many
-- Items whose cached on_hand differs from the sum of their movements.
SELECT si.id, si.store_id, si.on_hand, coalesce(m.total, 0)::int AS ledger
FROM store_items si
LEFT JOIN (SELECT store_item_id, sum(delta) AS total FROM stock_movements GROUP BY store_item_id) m
  ON m.store_item_id = si.id
WHERE si.on_hand <> coalesce(m.total, 0)
ORDER BY si.id
LIMIT @lim;

-- name: LockItemForRepair :one
SELECT on_hand FROM store_items WHERE id = @id FOR UPDATE;

-- name: ItemLedgerSum :one
SELECT coalesce(sum(delta), 0)::int AS total FROM stock_movements WHERE store_item_id = @store_item_id;

-- name: RepairOnHand :exec
UPDATE store_items SET on_hand = @on_hand WHERE id = @id;

-- name: BatchDrift :many
-- Batches whose remaining quantity differs from the sum of their allocation rows.
SELECT b.id, b.store_item_id, b.quantity, coalesce(a.total, 0)::int AS ledger
FROM stock_batches b
LEFT JOIN (SELECT batch_id, sum(delta) AS total FROM stock_movement_batches GROUP BY batch_id) a
  ON a.batch_id = b.id
WHERE b.quantity <> coalesce(a.total, 0)
ORDER BY b.id
LIMIT @lim;

-- name: BatchOverItems :many
-- Items whose batches hold more than max(on_hand, 0) (03 stock rule 8).
SELECT si.id, si.store_id, si.on_hand, sum(b.quantity)::int AS in_batches
FROM store_items si JOIN stock_batches b ON b.store_item_id = si.id AND b.quantity > 0
GROUP BY si.id, si.store_id, si.on_hand
HAVING sum(b.quantity) > GREATEST(si.on_hand, 0)
ORDER BY si.id
LIMIT @lim;
