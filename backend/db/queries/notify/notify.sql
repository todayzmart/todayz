-- notify queries, generated into package notifydb (gen/sqlc/notifydb).
-- Only W2-05 (internal/notify) edits this folder (D-50). Every time the code
-- compares or records is a parameter from the application clock, never now().

------------------------------------------------------------------------------
-- Inbox
------------------------------------------------------------------------------

-- name: InsertNotification :exec
INSERT INTO notifications (id, user_id, business_id, store_id, app, kind, params, ref_type, ref_id, created_at)
VALUES (@id, sqlc.narg(user_id), sqlc.narg(business_id), sqlc.narg(store_id), @app, @kind, @params,
  sqlc.narg(ref_type), sqlc.narg(ref_id), @created_at);

-- name: ListUserNotifications :many
-- Newest first, keyset after (before_at, before_id) when set.
SELECT id, store_id, kind, params, ref_type, ref_id, read_at, created_at
FROM notifications
WHERE user_id = @user_id AND app = @app
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: CountUserUnread :one
SELECT count(*) FROM notifications
WHERE user_id = @user_id AND app = @app AND read_at IS NULL;

-- name: MarkUserRead :exec
-- The listed rows, or every row when mark_all; rows of another user are not touched.
UPDATE notifications SET read_at = @now::timestamptz
WHERE user_id = @user_id AND app = @app AND read_at IS NULL
  AND (@mark_all::bool OR id = ANY(@ids::uuid[]));

-- name: ListBusinessNotifications :many
-- A member limited to some stores (store_ids not null) sees business-wide rows
-- and their stores' rows.
SELECT id, store_id, kind, params, ref_type, ref_id, read_at, created_at
FROM notifications
WHERE business_id = @business_id
  AND (sqlc.narg(store_ids)::uuid[] IS NULL OR store_id IS NULL OR store_id = ANY(sqlc.narg(store_ids)::uuid[]))
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: CountBusinessUnread :one
SELECT count(*) FROM notifications
WHERE business_id = @business_id AND read_at IS NULL
  AND (sqlc.narg(store_ids)::uuid[] IS NULL OR store_id IS NULL OR store_id = ANY(sqlc.narg(store_ids)::uuid[]));

-- name: MarkBusinessRead :exec
-- Read state is shared by the staff: one read_at per row.
UPDATE notifications SET read_at = @now::timestamptz
WHERE business_id = @business_id AND read_at IS NULL
  AND (sqlc.narg(store_ids)::uuid[] IS NULL OR store_id IS NULL OR store_id = ANY(sqlc.narg(store_ids)::uuid[]))
  AND (@mark_all::bool OR id = ANY(@ids::uuid[]));

-- name: StoreInBusiness :one
SELECT EXISTS (SELECT 1 FROM stores WHERE id = @id AND business_id = @business_id)::bool AS ok;

-- name: BusinessStores :many
-- Every store of the business: a business-wide inbox row is announced on each.
SELECT id FROM stores WHERE business_id = @business_id ORDER BY id;

------------------------------------------------------------------------------
-- Preferences
------------------------------------------------------------------------------

-- name: ListPrefs :many
SELECT kind, channel, enabled FROM notification_prefs
WHERE user_id = @user_id AND app = @app;

-- name: UpsertPref :exec
INSERT INTO notification_prefs (user_id, app, kind, channel, enabled, updated_at)
VALUES (@user_id, @app, @kind, @channel, @enabled, @now)
ON CONFLICT (user_id, app, kind, channel) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at;

-- name: PrefEnabled :one
-- No row means on.
SELECT COALESCE((SELECT enabled FROM notification_prefs
  WHERE user_id = @user_id AND app = @app AND kind = @kind AND channel = @channel), true)::bool AS enabled;

------------------------------------------------------------------------------
-- Deliveries
------------------------------------------------------------------------------

-- name: InsertDelivery :exec
INSERT INTO notification_deliveries (id, notification_id, channel, provider, lang, to_enc, created_at, updated_at)
VALUES (@id, @notification_id, @channel, @provider, @lang, @to_enc, @now, @now);

-- name: ClaimDelivery :one
-- queued → sending. A retry that finds the row past queued gets no row and
-- never calls the provider again: an outside message is sent at most once.
UPDATE notification_deliveries d SET status = 'sending', updated_at = @now
FROM notifications n
WHERE d.id = @id AND d.status = 'queued' AND n.id = d.notification_id
RETURNING d.id, d.channel, d.provider, d.lang, d.to_enc, n.kind, n.params;

-- name: FinishDelivery :exec
-- The phone is cleared once the delivery ends.
UPDATE notification_deliveries SET status = @status, provider_id = sqlc.narg(provider_id),
  error = sqlc.narg(error), cost_paise = @cost_paise, to_enc = NULL, updated_at = @now
WHERE id = @id AND status = 'sending';

-- name: GetDelivery :one
SELECT id, notification_id, channel, provider, provider_id, status, error, cost_paise, lang,
  (to_enc IS NOT NULL)::bool AS has_phone
FROM notification_deliveries WHERE id = @id;

------------------------------------------------------------------------------
-- "New order not seen" SMS (01 §15)
------------------------------------------------------------------------------

-- name: ClaimEscalation :one
-- Once per order: the second claim inserts nothing and returns no row.
INSERT INTO notify_order_escalations (order_id, store_id, notification_id, created_at)
VALUES (@order_id, @store_id, @notification_id, @now)
ON CONFLICT (order_id) DO NOTHING
RETURNING order_id;

-- name: EscalatedOrders :many
SELECT order_id FROM notify_order_escalations WHERE order_id = ANY(@order_ids::uuid[]);
