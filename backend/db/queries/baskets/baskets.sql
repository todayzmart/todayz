-- baskets queries, generated into package basketsdb (gen/sqlc/basketsdb).
-- Only W2-14 (internal/baskets) edits this folder (D-50). Every read and
-- write filters on customer_id: another customer's basket is not found.

-- name: ListBaskets :many
SELECT b.id, b.name, b.updated_at, b.version,
       (SELECT count(*) FROM basket_lines l WHERE l.basket_id = b.id)::int AS line_count
FROM baskets b
WHERE b.customer_id = @customer_id AND b.deleted_at IS NULL
ORDER BY b.updated_at DESC, b.id DESC;

-- name: LockCustomerBaskets :exec
-- Serialises creates of one customer so the basket_max_saved count holds.
SELECT pg_advisory_xact_lock(hashtextextended('baskets:' || sqlc.arg(customer_key)::text, 0));

-- name: CountBaskets :one
SELECT count(*)::int FROM baskets WHERE customer_id = @customer_id AND deleted_at IS NULL;

-- name: InsertBasket :one
INSERT INTO baskets (id, customer_id, name, created_at, updated_at)
VALUES (@id, @customer_id, @name, @now, @now)
RETURNING id, name, version, last_split, updated_at;

-- name: GetBasket :one
SELECT id, name, version, last_split, updated_at
FROM baskets
WHERE id = @id AND customer_id = @customer_id AND deleted_at IS NULL;

-- name: LockBasket :one
SELECT id, name, version, last_split, updated_at
FROM baskets
WHERE id = @id AND customer_id = @customer_id AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateBasket :one
UPDATE baskets
SET name = @name, last_split = @last_split, version = version + 1, updated_at = @now
WHERE id = @id AND customer_id = @customer_id AND deleted_at IS NULL
RETURNING id, name, version, last_split, updated_at;

-- name: SoftDeleteBasket :execrows
UPDATE baskets
SET deleted_at = @now, version = version + 1, updated_at = @now
WHERE id = @id AND customer_id = @customer_id AND deleted_at IS NULL;

-- name: DeleteCustomerBaskets :exec
-- Account deletion (01 §16): the lines go by cascade.
DELETE FROM baskets WHERE customer_id = @customer_id;

-- name: DeleteLines :exec
DELETE FROM basket_lines WHERE basket_id = @basket_id;

-- name: InsertLine :exec
INSERT INTO basket_lines (id, basket_id, product_id, text, quantity, position, created_at)
VALUES (@id, @basket_id, sqlc.narg('product_id'), sqlc.narg('text'), @quantity, @position, @now);

-- name: ListLines :many
SELECT l.id, l.product_id, l.text, l.quantity, l.position,
       p.name AS product_name, p.variant, COALESCE(p.pack_size, 0)::float8 AS pack_size, p.unit -- 0 for a text line (LEFT JOIN)
FROM basket_lines l
LEFT JOIN products p ON p.id = l.product_id
WHERE l.basket_id = @basket_id
ORDER BY l.position;

-- name: ExistingProducts :many
-- The product ids among these that a line may name (any status: a basket
-- keeps a product the catalogue later hides; search simply finds none).
SELECT id FROM products WHERE id = ANY(@ids::uuid[]);
