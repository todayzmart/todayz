-- orders queries (W2-02), generated into package ordersdb (gen/sqlc/ordersdb).
-- Only the work package that builds internal/orders edits this folder (D-50).
--
-- Times come from the injected clock (@now), never now(). Lock order: the
-- order row (FOR UPDATE) first. Holding or releasing stock then locks the
-- order's store_items by id and never the stores row; collecting goes
-- through the catalogue writer (stores row, then the item), one item at a
-- time in store_item_id order, and never pre-locks items, so no path takes
-- an item before the stores row.

------------------------------------------------------------------------------
-- Placing
------------------------------------------------------------------------------

-- name: StoreForBooking :one
-- The store as checkout needs it. A store is bookable while live and its
-- business verified (D-40, the same filter as discovery).
SELECT s.id, s.business_id, s.slug, s.name, s.address_line1, s.address_line2, s.locality,
       s.status, (b.status = 'verified')::bool AS business_verified,
       s.accepts_pickup, s.accepts_delivery, s.hours, s.closed_until, s.cashback_bp, s.auto_accept
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE s.id = @id;

-- name: ItemsForBooking :many
-- The named items of the store with the product fields a line shows. An id
-- of another store is simply missing from the result.
SELECT si.id, si.product_id, si.price_paise, si.mrp_paise, si.on_hand, si.reserved, si.is_listed,
       coalesce(si.local_name, p.name)::text AS name, p.variant, p.pack_size::float8 AS pack_size,
       p.unit, p.image_key, p.status AS product_status
FROM store_items si JOIN products p ON p.id = si.product_id
WHERE si.store_id = @store_id AND si.id = ANY(@ids::uuid[])
ORDER BY si.id;

-- name: LockCustomer :one
-- The customer's users row, held to the end of the transaction: an account
-- deletion (FOR NO KEY UPDATE on the same row) and a second order of the
-- same customer wait for this one, so the active-order checks hold.
SELECT u.status, cp.limited_until
FROM users u LEFT JOIN customer_profiles cp ON cp.user_id = u.id
WHERE u.id = @id
FOR NO KEY UPDATE OF u;

-- name: ActiveOrderCount :one
-- Orders of the customer that are not finished.
SELECT count(*)::int AS n FROM orders
WHERE customer_id = @customer_id
  AND status IN ('placed','awaiting_customer','accepted','ready','out_for_delivery');

-- name: ActiveOrderForAskReply :one
SELECT id FROM orders
WHERE ask_reply_id = @ask_reply_id AND status NOT IN ('cancelled','expired','rejected')
LIMIT 1;

-- name: CodeInUse :one
-- Whether a code's HMAC is taken among the store's open orders (D-04).
SELECT EXISTS (
  SELECT 1 FROM orders
  WHERE store_id = @store_id AND code_hmac = @code_hmac
    AND status IN ('placed','awaiting_customer','accepted','ready','out_for_delivery')
)::bool AS taken;

-- name: InsertOrder :one
INSERT INTO orders (id, customer_id, store_id, ask_reply_id, fulfilment, subtotal_paise, discount_paise,
  items_after_offers_paise, item_subtotal_paise, credits_used, delivery_fee_paise, total_paise,
  cashback_bp, code_enc, code_hmac, accept_by, placed_at)
VALUES (@id, @customer_id, @store_id, sqlc.narg(ask_reply_id), @fulfilment, @subtotal_paise, @discount_paise,
  @items_after_offers_paise, @item_subtotal_paise, @credits_used, @delivery_fee_paise, @total_paise,
  @cashback_bp, @code_enc, @code_hmac, @accept_by, @placed_at)
RETURNING version;

-- name: InsertOrderItem :exec
INSERT INTO order_items (id, order_id, store_item_id, product_snapshot, unit_price_paise,
  quantity_requested, line_total_paise, price_source, created_at)
VALUES (@id, @order_id, @store_item_id, @product_snapshot, @unit_price_paise,
  @quantity_requested, @line_total_paise, @price_source, @created_at);

-- name: InsertOrderEvent :exec
INSERT INTO order_events (id, order_id, from_status, to_status, actor_kind, actor_id, reason, meta, created_at)
VALUES (@id, @order_id, sqlc.narg(from_status), @to_status, @actor_kind, sqlc.narg(actor_id),
  sqlc.narg(reason), @meta, @created_at);

------------------------------------------------------------------------------
-- Reading one order
------------------------------------------------------------------------------

-- name: GetOrder :one
SELECT * FROM orders WHERE id = @id;

-- name: LockOrder :one
SELECT * FROM orders WHERE id = @id FOR UPDATE;

-- name: OrderStore :one
SELECT id, business_id, slug, name, address_line1, address_line2, locality, cashback_bp
FROM stores WHERE id = @id;

-- name: OrderLines :many
-- The lines in placing order, with what is available now (B-04 shows it).
SELECT oi.id, oi.store_item_id, oi.product_snapshot, oi.unit_price_paise, oi.quantity_requested,
       oi.quantity_accepted, oi.line_total_paise, oi.price_source,
       (si.on_hand - si.reserved)::int AS available
FROM order_items oi JOIN store_items si ON si.id = oi.store_item_id
WHERE oi.order_id = @order_id
ORDER BY oi.created_at, oi.id;

-- name: OrderTimeline :many
SELECT from_status, to_status, actor_kind, reason, created_at
FROM order_events
WHERE order_id = @order_id
ORDER BY created_at, id;

-- name: CustomerForStore :one
-- The customer as a shop sees them: the name and the sealed phone (the
-- service masks it, D-60).
SELECT u.name, pi.phone_enc
FROM users u LEFT JOIN phone_identities pi ON pi.user_id = u.id
WHERE u.id = @id;

------------------------------------------------------------------------------
-- Transitions (the caller holds LockOrder; trg_orders_guard checks the pair)
------------------------------------------------------------------------------

-- name: SetAccepted :one
UPDATE orders SET status = 'accepted', accepted_at = @now, hold_until = @hold_until, version = version + 1
WHERE id = @id
RETURNING version;

-- name: AcceptAllLines :exec
UPDATE order_items SET quantity_accepted = quantity_requested WHERE order_id = @order_id;

-- name: SetPartial :one
-- A partial accept (BK-08): the order waits for the customer with the
-- totals recomputed on the accepted lines (D-28). Nothing is reserved yet.
UPDATE orders SET status = 'awaiting_customer', partial_confirm_by = @partial_confirm_by,
  subtotal_paise = @subtotal_paise, discount_paise = @discount_paise,
  items_after_offers_paise = @items_after_offers_paise, item_subtotal_paise = @item_subtotal_paise,
  credits_used = @credits_used, delivery_fee_paise = @delivery_fee_paise, total_paise = @total_paise,
  version = version + 1
WHERE id = @id
RETURNING version;

-- name: SetLineAccepted :exec
UPDATE order_items SET quantity_accepted = @quantity_accepted, line_total_paise = @line_total_paise
WHERE id = @id AND order_id = @order_id;

-- name: SetReady :one
UPDATE orders SET status = 'ready', version = version + 1 WHERE id = @id
RETURNING version;

-- name: SetEnded :one
-- rejected, expired or cancelled, with the reason and note (03 §4.2).
UPDATE orders SET status = @status, status_reason = @reason, status_note = sqlc.narg(note), version = version + 1
WHERE id = @id
RETURNING version;

-- name: SetCollected :one
UPDATE orders SET status = 'collected', collected_at = @now, payment_status = 'paid',
  payment_method = @payment_method, cashback_credits = @cashback_credits, version = version + 1
WHERE id = @id
RETURNING version;

-- name: SetExtended :one
UPDATE orders SET hold_until = @hold_until, extended_at = @now, version = version + 1
WHERE id = @id
RETURNING version;

-- name: CountWrongCode :one
-- One wrong code: the count goes up and, at the limit, the code locks.
UPDATE orders SET code_attempts = code_attempts + 1,
  code_locked_at = CASE WHEN code_attempts + 1 >= @lock_at::int THEN @now::timestamptz ELSE code_locked_at END,
  version = CASE WHEN code_attempts + 1 >= @lock_at::int THEN version + 1 ELSE version END
WHERE id = @id
RETURNING code_attempts, code_locked_at, version;

-- name: SetNewCode :one
UPDATE orders SET code_enc = @code_enc, code_hmac = @code_hmac, code_attempts = 0, code_locked_at = NULL,
  version = version + 1
WHERE id = @id
RETURNING version;

------------------------------------------------------------------------------
-- Stock reservations (03 §3; trg_stock_reservations_apply moves reserved)
------------------------------------------------------------------------------

-- name: LockItems :many
-- The order's items, locked in id order.
SELECT id, on_hand, reserved FROM store_items
WHERE id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: InsertReservation :exec
INSERT INTO stock_reservations (id, order_id, order_item_id, store_item_id, quantity, created_at, updated_at)
VALUES (@id, @order_id, @order_item_id, @store_item_id, @quantity, @now, @now);

-- name: HeldReservations :many
SELECT id, order_item_id, store_item_id, quantity FROM stock_reservations
WHERE order_id = @order_id AND status = 'held'
ORDER BY store_item_id, id;

-- name: ReleaseReservations :execrows
UPDATE stock_reservations SET status = 'released', released_reason = @reason
WHERE order_id = @order_id AND status = 'held';

-- name: ConsumeReservation :exec
UPDATE stock_reservations SET status = 'consumed', consumed_movement_id = @movement_id
WHERE id = @id AND status = 'held';

------------------------------------------------------------------------------
-- Lists
------------------------------------------------------------------------------

-- name: ListCustomerOrders :many
-- The customer's orders, newest first, keyset on (placed_at, id).
SELECT o.id, o.display_number, o.status, o.status_reason, o.fulfilment, o.total_paise, o.placed_at,
       o.accept_by, o.hold_until, o.version,
       s.id AS store_id, s.slug AS store_slug, s.name AS store_name, s.address_line1, s.address_line2,
       s.locality AS store_locality,
       (SELECT count(*) FROM order_items oi WHERE oi.order_id = o.id)::int AS item_count
FROM orders o JOIN stores s ON s.id = o.store_id
WHERE o.customer_id = @customer_id
  AND o.status = ANY(@statuses::text[])
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (o.placed_at, o.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY o.placed_at DESC, o.id DESC
LIMIT @lim;

-- name: ListStoreOrders :many
-- A store's orders for one B-04 tab, newest first. settled, when set, keeps
-- only delivery_failed orders whose return is (true) or is not (false)
-- confirmed; other statuses pass.
SELECT o.id, o.display_number, o.status, o.status_reason, o.fulfilment, o.total_paise, o.placed_at,
       o.accept_by, o.hold_until, o.version, o.seen_at, u.name AS customer_name,
       (SELECT count(*) FROM order_items oi WHERE oi.order_id = o.id)::int AS item_count
FROM orders o JOIN users u ON u.id = o.customer_id
WHERE o.store_id = @store_id
  AND o.status = ANY(@statuses::text[])
  AND (sqlc.narg(settled)::bool IS NULL OR o.status <> 'delivery_failed'
       OR (o.return_confirmed_at IS NOT NULL) = sqlc.narg(settled)::bool)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (o.placed_at, o.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY o.placed_at DESC, o.id DESC
LIMIT @lim;

-- name: MarkSeen :exec
-- A staff member listed or opened these new orders (B-04; the W2-05 "not
-- seen" escalation reads seen_at). version is not bumped: nothing the
-- customer sees changed.
UPDATE orders SET seen_at = @now
WHERE store_id = @store_id AND id = ANY(@ids::uuid[]) AND status = 'placed' AND seen_at IS NULL;

-- name: UnseenOrders :many
-- New orders no staff member has listed or opened (notice.UnseenOrders),
-- oldest first, keyset on (placed_at, id).
SELECT id, store_id, placed_at FROM orders
WHERE status = 'placed' AND seen_at IS NULL AND placed_at <= @created_before
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (placed_at, id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY placed_at, id
LIMIT @lim;

------------------------------------------------------------------------------
-- Strikes and the no-show limit (01 §5.4)
------------------------------------------------------------------------------

-- name: InsertStrike :execrows
INSERT INTO strikes (id, subject_type, subject_id, order_id, reason, created_at)
VALUES (@id, @subject_type, @subject_id, @order_id, @reason, @created_at)
ON CONFLICT (order_id, subject_type) WHERE order_id IS NOT NULL DO NOTHING;

-- name: CountActiveStrikes :one
SELECT count(*)::int AS n FROM strikes
WHERE subject_type = @subject_type AND subject_id = @subject_id AND reset_at IS NULL
  AND reason = @reason AND created_at > @since;

-- name: SetCustomerStrikes :execrows
-- The 30-day count, and the limit when it is reached (never shortened).
UPDATE customer_profiles SET strikes_count_30d = @strikes,
  limited_until = CASE
    WHEN sqlc.narg(limited_until)::timestamptz IS NULL THEN limited_until
    WHEN limited_until IS NULL OR limited_until < sqlc.narg(limited_until)::timestamptz THEN sqlc.narg(limited_until)::timestamptz
    ELSE limited_until END,
  version = version + 1
WHERE user_id = @user_id;
