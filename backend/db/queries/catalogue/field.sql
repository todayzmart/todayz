-- Field agents (W3-06, A-04): a store's items newest first with their
-- product's review status, for the agent's item list.

-- name: ListFieldItems :many
SELECT si.id, si.store_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  si.on_hand, si.is_listed, si.created_at, si.updated_at,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit,
  p.status AS product_status, b.name AS brand_name, p.mrp_paise AS product_mrp_paise
FROM store_items si
JOIN products p ON p.id = si.product_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE si.store_id = @store_id
  AND (sqlc.narg(before_id)::uuid IS NULL OR si.id < sqlc.narg(before_id)::uuid)
ORDER BY si.id DESC
LIMIT @lim;

-- name: GetFieldItem :one
SELECT si.id, si.store_id, si.product_id, si.local_name, si.price_paise, si.mrp_paise,
  si.on_hand, si.is_listed, si.created_at, si.updated_at, si.version,
  p.name AS product_name, p.variant, p.pack_size::float8 AS pack_size, p.unit,
  p.status AS product_status, b.name AS brand_name, p.mrp_paise AS product_mrp_paise
FROM store_items si
JOIN products p ON p.id = si.product_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE si.id = @id AND si.store_id = @store_id;

-- name: BarcodeTaken :one
-- Whether a product that is not merged or retired already carries the barcode.
SELECT EXISTS (SELECT 1 FROM products WHERE barcodes @> ARRAY[@barcode::text] AND status IN ('active','pending_review'));
