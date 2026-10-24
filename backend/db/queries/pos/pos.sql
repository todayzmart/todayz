-- pos queries (W2-04), generated into package posdb (gen/sqlc/posdb).
-- Only the work package that builds internal/pos edits this folder (D-50).
--
-- Times come from the injected clock (@now), never now(). Lock order inside
-- a sale's transaction: the bill claim row (FOR UPDATE) or the counter-code
-- check, then the pos_sales row, then (through the catalogue's stock writer)
-- the stores row and one store item at a time. A sync takes the stores row
-- through the writer on its first stock entry and keeps it to commit, so two
-- syncs of one store run one after the other.

------------------------------------------------------------------------------
-- Stores, items and people
------------------------------------------------------------------------------

-- name: StoreInfo :one
SELECT id, business_id, name, address_line1, locality, status, cashback_bp, catalogue_seq
FROM stores WHERE id = @id;

-- name: StoreItemsByIDs :many
-- The named items of the store; an id of another store is missing.
SELECT si.id, si.product_id, si.price_paise, coalesce(si.local_name, p.name)::text AS name
FROM store_items si JOIN products p ON p.id = si.product_id
WHERE si.store_id = @store_id AND si.id = ANY(@ids::uuid[])
ORDER BY si.id;

-- name: UserName :one
SELECT coalesce(name, '')::text AS name FROM users WHERE id = @id;

-- name: UserByPhoneHash :one
-- An active account with this phone (D-12: blocked or deleted never links).
SELECT u.id, coalesce(u.name, '')::text AS name
FROM phone_identities pi JOIN users u ON u.id = pi.user_id
WHERE pi.phone_hash = @phone_hash AND u.status = 'active' AND u.deleted_at IS NULL;

-- name: CounterDevices :many
-- The user's devices that have a counter-code secret.
SELECT id, counter_secret_enc FROM devices
WHERE user_id = @user_id AND counter_secret_enc IS NOT NULL
ORDER BY id;

-- name: SessionDevice :one
-- The device of the signed-in session family (the access token's sid), from
-- its newest row.
SELECT device_id FROM sessions
WHERE family_id = @family_id AND user_id = @user_id
ORDER BY created_at DESC
LIMIT 1;

-- name: LockDevice :one
SELECT id, user_id, counter_secret_enc FROM devices
WHERE id = @id
FOR UPDATE;

-- name: SetCounterSecret :exec
UPDATE devices SET counter_secret_enc = @secret_enc, version = version + 1
WHERE id = @id AND counter_secret_enc IS NULL;

------------------------------------------------------------------------------
-- Sales
------------------------------------------------------------------------------

-- name: InsertSale :one
-- A second insert with the same (store, client id) returns no row; the
-- caller then reads the first sale (idempotent by client id).
INSERT INTO pos_sales (
  id, store_id, client_id, device_id, device_seq, cashier_user_id, customer_id,
  link_method, link_status, offline_link_code, offline_link_at, device_clock_skew_ms,
  link_phone_hash, link_phone_enc, offline_offer_unmatched,
  item_subtotal_paise, bill_offer_discount_paise, subtotal_paise, discount_paise, round_off_paise,
  credits_used, cashback_credits, total_paise, payment_method, sold_at, received_at, created_at, updated_at
) VALUES (
  @id, @store_id, @client_id, @device_id, sqlc.narg(device_seq), @cashier_user_id, sqlc.narg(customer_id),
  sqlc.narg(link_method), sqlc.narg(link_status), sqlc.narg(offline_link_code), sqlc.narg(offline_link_at),
  sqlc.narg(device_clock_skew_ms), sqlc.narg(link_phone_hash), sqlc.narg(link_phone_enc), @offline_offer_unmatched,
  @item_subtotal_paise, @bill_offer_discount_paise, @subtotal_paise, @discount_paise, @round_off_paise,
  @credits_used, 0, @total_paise, @payment_method, @sold_at, @now, @now, @now
)
ON CONFLICT (store_id, client_id) DO NOTHING
RETURNING *;

-- name: SetSaleCashback :one
UPDATE pos_sales SET cashback_credits = @cashback_credits, version = version + 1, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: GetSaleByClient :one
SELECT * FROM pos_sales WHERE store_id = @store_id AND client_id = @client_id;

-- name: GetSale :one
SELECT * FROM pos_sales WHERE id = @id AND store_id = @store_id;

-- name: SaleRef :one
SELECT store_id, customer_id, link_method, link_status, status FROM pos_sales WHERE id = @id;

-- name: LockSale :one
SELECT * FROM pos_sales WHERE id = @id AND store_id = @store_id
FOR UPDATE;

-- name: ReverseSale :one
UPDATE pos_sales SET status = 'reversed', reversed_at = @now, reversed_by = @reversed_by,
  reversal_reason = @reason, version = version + 1, updated_at = @now
WHERE id = @id AND status = 'completed'
RETURNING *;

-- name: InsertSaleLine :exec
INSERT INTO pos_sale_lines (
  id, sale_id, client_id, store_item_id, name_snapshot, unit_price_paise, quantity,
  offer_discount_paise, manual_discount_paise, line_total_paise, movement_id, created_at
) VALUES (
  @id, @sale_id, @client_id, sqlc.narg(store_item_id), @name_snapshot, @unit_price_paise, @quantity,
  @offer_discount_paise, @manual_discount_paise, @line_total_paise, sqlc.narg(movement_id), @now
);

-- name: SaleLines :many
SELECT * FROM pos_sale_lines WHERE sale_id = @sale_id ORDER BY created_at, id;

------------------------------------------------------------------------------
-- Bill claims (bill QR)
------------------------------------------------------------------------------

-- name: InsertBillClaim :one
INSERT INTO bill_claims (id, store_id, sale_client_id, amount_paise, token_nonce, expires_at, created_by, created_at, updated_at)
VALUES (@id, @store_id, @sale_client_id, @amount_paise, @token_nonce, @expires_at, @created_by, @now, @now)
RETURNING *;

-- name: GetBillClaim :one
-- With the claiming customer's name for the POS ("claimed by Ananya").
SELECT bc.*, u.name AS customer_name
FROM bill_claims bc LEFT JOIN users u ON u.id = bc.claimed_by
WHERE bc.id = @id;

-- name: SaleBillClaim :one
-- The bill claim a sale bound (ux_bill_claims_pos_sale): its hold is what
-- the sale spent.
SELECT id FROM bill_claims WHERE pos_sale_id = @pos_sale_id;

-- name: LockBillClaim :one
SELECT * FROM bill_claims WHERE id = @id
FOR UPDATE;

-- name: ClaimBillClaim :one
UPDATE bill_claims SET status = 'claimed', claimed_by = @customer_id, claimed_at = @now,
  credits_requested = @credits_requested, credits_used = @credits_used, version = version + 1, updated_at = @now
WHERE id = @id AND status = 'open' AND claimed_by IS NULL
RETURNING *;

-- name: BindBillClaim :one
UPDATE bill_claims SET pos_sale_id = @pos_sale_id, version = version + 1, updated_at = @now
WHERE id = @id AND status = 'claimed' AND pos_sale_id IS NULL
RETURNING *;

-- name: CancelBillClaims :many
-- The bill's other live claims (a new claim for it, or its sale rung) are
-- cancelled; the caller releases the holds of those a customer confirmed.
UPDATE bill_claims SET status = 'cancelled', version = version + 1, updated_at = @now
WHERE store_id = @store_id AND sale_client_id = @sale_client_id AND id IS DISTINCT FROM sqlc.narg(keep_id)
  AND status IN ('open','claimed') AND pos_sale_id IS NULL
RETURNING id, claimed_by;

-- name: DueBillClaims :many
-- One page of the sweep: up to @lim live claims past their expiry after the
-- keyset (@after_at, @after_id), oldest first, so a claim left live does not
-- hold the page back; a claim a sale is binding right now is skipped and its
-- sale sees it expired.
SELECT id, claimed_by, expires_at FROM bill_claims
WHERE status IN ('open','claimed') AND pos_sale_id IS NULL AND expires_at <= @now
  AND (expires_at, id) > (@after_at::timestamptz, @after_id::uuid)
ORDER BY expires_at, id
LIMIT @lim
FOR UPDATE SKIP LOCKED;

-- name: ExpireBillClaim :execrows
UPDATE bill_claims SET status = 'expired', version = version + 1, updated_at = @now
WHERE id = @id AND status IN ('open','claimed') AND pos_sale_id IS NULL;

------------------------------------------------------------------------------
-- Counter codes
------------------------------------------------------------------------------

-- name: EnsureCounterCheck :exec
INSERT INTO counter_code_checks (store_id, sale_client_id, created_at, updated_at)
VALUES (@store_id, @sale_client_id, @now, @now)
ON CONFLICT (store_id, sale_client_id) DO NOTHING;

-- name: LockCounterCheck :one
SELECT * FROM counter_code_checks WHERE store_id = @store_id AND sale_client_id = @sale_client_id
FOR UPDATE;

-- name: GetCounterCheck :one
SELECT * FROM counter_code_checks WHERE store_id = @store_id AND sale_client_id = @sale_client_id;

-- name: FailCounterCheck :one
-- One wrong code; the @max_attempts-th locks the bill.
UPDATE counter_code_checks SET attempts = attempts + 1,
  locked_at = CASE WHEN attempts + 1 >= @max_attempts::int THEN @now::timestamptz ELSE NULL END,
  updated_at = @now
WHERE store_id = @store_id AND sale_client_id = @sale_client_id
RETURNING attempts, locked_at;

-- name: VerifyCounterCheck :exec
UPDATE counter_code_checks SET customer_id = @customer_id, customer_device_id = @customer_device_id,
  time_step = @time_step, verified_at = @now, updated_at = @now
WHERE store_id = @store_id AND sale_client_id = @sale_client_id;

-- name: LockCodeKey :exec
-- Serialises the failure count of one phone or cashier until commit.
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: CodeLocks :one
-- Whether a phone at a store, or a cashier, is locked, and their failures
-- in the windows: the phone at the store, the phone at any store, the
-- cashier (D-131).
SELECT
  EXISTS (SELECT 1 FROM counter_code_failures f
    WHERE f.store_id = @store_id AND f.phone_hash = sqlc.narg(phone_hash)
      AND f.failed_at > @phone_lock_since AND f.phone_locked_until > @now::timestamptz) AS phone_locked,
  EXISTS (SELECT 1 FROM counter_code_failures f
    WHERE f.cashier_user_id = @cashier_user_id
      AND f.failed_at > @cashier_lock_since AND f.cashier_locked_until > @now::timestamptz) AS cashier_locked,
  (SELECT count(*) FROM counter_code_failures f
    WHERE f.store_id = @store_id AND f.phone_hash = sqlc.narg(phone_hash)
      AND f.failed_at > @phone_window_since)::int AS phone_failures,
  (SELECT count(*) FROM counter_code_failures f
    WHERE f.phone_hash = sqlc.narg(phone_hash) AND f.failed_at > @phone_any_since)::int AS phone_any_failures,
  (SELECT count(*) FROM counter_code_failures f
    WHERE f.cashier_user_id = @cashier_user_id AND f.failed_at > @cashier_window_since)::int AS cashier_failures;

-- name: InsertCodeFailure :exec
INSERT INTO counter_code_failures (id, store_id, cashier_user_id, phone_hash, source,
  phone_locked_until, cashier_locked_until, failed_at)
VALUES (@id, @store_id, @cashier_user_id, sqlc.narg(phone_hash), @source,
  sqlc.narg(phone_locked_until), sqlc.narg(cashier_locked_until), @now);

-- name: InsertCodeUse :one
-- A (device, step) is accepted once (D-12): a second use returns no row.
INSERT INTO counter_code_uses (customer_device_id, time_step, store_id, pos_sale_id, created_at)
VALUES (@customer_device_id, @time_step, @store_id, sqlc.narg(pos_sale_id), @now)
ON CONFLICT (customer_device_id, time_step) DO NOTHING
RETURNING customer_device_id;

-- name: BindCodeUse :exec
UPDATE counter_code_uses SET pos_sale_id = @pos_sale_id
WHERE customer_device_id = @customer_device_id AND time_step = @time_step AND pos_sale_id IS NULL;

------------------------------------------------------------------------------
-- Receipts and purchases
------------------------------------------------------------------------------

-- name: InsertReceiptToken :exec
INSERT INTO receipt_tokens (id, token_hash, pos_sale_id, expires_at, created_at)
VALUES (@id, @token_hash, @pos_sale_id, @expires_at, @now);

-- name: ReceiptByHash :one
-- One lookup whether the token is unknown or expired (D-45).
SELECT ps.*, s.name AS store_name, s.address_line1, s.locality
FROM receipt_tokens rt
JOIN pos_sales ps ON ps.id = rt.pos_sale_id
JOIN stores s ON s.id = ps.store_id
WHERE rt.token_hash = @token_hash AND rt.expires_at > @now;

-- name: MyPurchases :many
-- Online orders and POS sales linked by bill_qr or counter_code, newest
-- first, keyset on (at, id) (D-45, D-75). phone_unverified never appears.
SELECT p.kind, p.id, p.store_id, p.store_name, p.status, p.total_paise, p.at, p.item_count
FROM (
  SELECT 'order'::text AS kind, o.id, o.store_id, s.name AS store_name, o.status, o.total_paise,
         o.placed_at AS at, (SELECT count(*) FROM order_items oi WHERE oi.order_id = o.id)::int AS item_count
  FROM orders o JOIN stores s ON s.id = o.store_id
  WHERE o.customer_id = @customer_id
  UNION ALL
  SELECT 'pos_sale'::text, ps.id, ps.store_id, s.name, ps.status, ps.total_paise,
         ps.sold_at, (SELECT count(*) FROM pos_sale_lines l WHERE l.sale_id = ps.id)::int
  FROM pos_sales ps JOIN stores s ON s.id = ps.store_id
  WHERE ps.customer_id = @customer_id AND ps.link_method IN ('bill_qr','counter_code')
    AND ps.link_status = 'linked'
) p
WHERE sqlc.narg(before_at)::timestamptz IS NULL
   OR (p.at, p.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid)
ORDER BY p.at DESC, p.id DESC
LIMIT @lim;

------------------------------------------------------------------------------
-- Sync
------------------------------------------------------------------------------

-- name: GetSyncCursor :one
SELECT * FROM sync_cursors WHERE device_id = @device_id AND store_id = @store_id;

-- name: UpsertSyncCursor :exec
INSERT INTO sync_cursors (device_id, store_id, last_catalogue_seq, last_synced_at)
VALUES (@device_id, @store_id, @seq, @now)
ON CONFLICT (device_id, store_id) DO UPDATE
SET last_catalogue_seq = EXCLUDED.last_catalogue_seq, last_synced_at = EXCLUDED.last_synced_at;

-- name: GetSyncEntry :one
SELECT * FROM sync_entries WHERE store_id = @store_id AND client_id = @client_id;

-- name: InsertSyncEntry :exec
INSERT INTO sync_entries (store_id, client_id, device_id, device_seq, kind, pos_sale_id, movement_id, applied_at)
VALUES (@store_id, @client_id, @device_id, @device_seq, @kind, @pos_sale_id, @movement_id, @now);

-- name: ChangeWindow :one
-- The store's oldest kept change, and whether the change at @cursor is still
-- kept and when it was made.
SELECT coalesce(min(c1.seq), 0)::bigint AS oldest_seq,
       EXISTS (SELECT 1 FROM catalogue_changes c2 WHERE c2.store_id = @store_id AND c2.seq = @cursor)::bool AS cursor_kept,
       coalesce((SELECT c3.created_at FROM catalogue_changes c3 WHERE c3.store_id = @store_id AND c3.seq = @cursor),
                'epoch'::timestamptz)::timestamptz AS cursor_at
FROM catalogue_changes c1 WHERE c1.store_id = @store_id;

-- name: ChangesAfter :many
-- The newest change per entity after @after (up to @upto), in seq order. An
-- entity whose newest change is past a page's last seq comes on a later page.
SELECT c.seq, c.entity, c.entity_id, c.op,
       si.id AS item_id, si.product_id, coalesce(si.local_name, p.name, '')::text AS item_name,
       si.price_paise, si.mrp_paise, si.on_hand, si.reserved, si.is_listed, si.version AS item_version
FROM (
  SELECT DISTINCT ON (cc.entity, cc.entity_id) cc.seq, cc.entity, cc.entity_id, cc.op
  FROM catalogue_changes cc
  WHERE cc.store_id = @store_id AND cc.seq > @after AND cc.seq <= @upto
  ORDER BY cc.entity, cc.entity_id, cc.seq DESC
) c
LEFT JOIN store_items si ON c.entity = 'store_item' AND c.op = 'upsert' AND si.id = c.entity_id AND si.store_id = @store_id
LEFT JOIN products p ON p.id = si.product_id
ORDER BY c.seq
LIMIT @lim;

------------------------------------------------------------------------------
-- Risk
------------------------------------------------------------------------------

-- name: OfflineRiskSalesSince :one
-- Offline sales that count toward pos_offline_links_risk_per_day, each once:
-- a counter code linked offline, or a device offer discount the offers
-- engine would not give (D-12, D-131).
SELECT
  count(*)::int AS n,
  count(*) FILTER (WHERE offline_link_at IS NOT NULL AND link_status = 'linked')::int AS links,
  count(*) FILTER (WHERE offline_offer_unmatched)::int AS offers
FROM pos_sales
WHERE store_id = @store_id AND received_at >= @since
  AND ((offline_link_at IS NOT NULL AND link_status = 'linked') OR offline_offer_unmatched);

-- name: InsertRiskFlagOnce :exec
-- One open flag per store, rule and IST day.
INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, details, created_at, updated_at)
SELECT @id, 'store', @store_id, @rule, @score, @details, @now, @now
WHERE NOT EXISTS (
  SELECT 1 FROM risk_flags rf
  WHERE rf.subject_type = 'store' AND rf.subject_id = @store_id AND rf.rule = @rule AND rf.created_at >= @since
);
