-- Admin order search and detail (A-07, W3-05). The pickup code and the address
-- are never read here.

-- name: AdminListOrders :many
SELECT o.id, o.display_number, o.store_id, st.name AS store_name, o.customer_id, u.name AS customer_name,
  o.fulfilment, o.status, o.status_reason, o.total_paise, o.placed_at
FROM orders o JOIN stores st ON st.id = o.store_id JOIN users u ON u.id = o.customer_id
WHERE (sqlc.narg(number)::bigint IS NULL OR o.number = sqlc.narg(number)::bigint)
  AND (sqlc.narg(store_id)::uuid IS NULL OR o.store_id = sqlc.narg(store_id)::uuid)
  AND (sqlc.narg(customer_id)::uuid IS NULL OR o.customer_id = sqlc.narg(customer_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status)::text)
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR o.placed_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR o.placed_at < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (o.placed_at, o.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY o.placed_at DESC, o.id DESC
LIMIT @lim;

-- name: AdminOrderNames :one
SELECT st.name AS store_name, u.name AS customer_name
FROM stores st, users u
WHERE st.id = @store_id AND u.id = @customer_id;

-- name: AdminOrderLines :many
SELECT id, product_snapshot, unit_price_paise, quantity_requested, quantity_accepted, line_total_paise
FROM order_items WHERE order_id = @order_id
ORDER BY created_at, id
LIMIT 200;

-- name: AdminOrderEvents :many
SELECT id, from_status, to_status, actor_kind, actor_id, reason, created_at
FROM order_events WHERE order_id = @order_id
ORDER BY created_at, id
LIMIT 200;

-- name: AdminOrderNotifications :many
SELECT id, app, kind, created_at, read_at
FROM notifications WHERE ref_type = 'order' AND ref_id = @order_id
ORDER BY created_at, id
LIMIT 100;

-- name: AdminOrderCreditHolds :many
SELECT id, amount, captured, released, status, created_at
FROM credit_holds WHERE order_id = @order_id
ORDER BY created_at, id
LIMIT 20;
