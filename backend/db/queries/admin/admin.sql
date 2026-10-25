-- admin queries, generated into package admindb (gen/sqlc/admindb).
-- Only the work package that builds internal/admin edits this folder (D-50).

------------------------------------------------------------------------------
-- customers (A-06)
------------------------------------------------------------------------------

-- name: CustomerByPhoneHash :one
SELECT u.id, u.name, u.status, u.created_at, pi.phone_enc,
  (SELECT count(*) FROM strikes s
   WHERE s.subject_type = 'customer' AND s.subject_id = u.id AND s.reset_at IS NULL)::int AS active_strikes
FROM phone_identities pi JOIN users u ON u.id = pi.user_id
WHERE pi.phone_hash = @phone_hash;

-- name: GetCustomer :one
SELECT u.id, u.name, u.status, u.preferred_lang, u.created_at, u.version, pi.phone_enc,
  (SELECT count(*) FROM strikes s
   WHERE s.subject_type = 'customer' AND s.subject_id = u.id AND s.reset_at IS NULL)::int AS active_strikes,
  (au.user_id IS NOT NULL AND au.disabled_at IS NULL)::bool AS is_admin
FROM users u
  LEFT JOIN phone_identities pi ON pi.user_id = u.id
  LEFT JOIN admin_users au ON au.user_id = u.id
WHERE u.id = @id;

-- name: LockCustomer :one
-- Lock order is the users row before its sessions (identity's refresh does the same).
SELECT u.status FROM users u WHERE u.id = @id FOR UPDATE;

-- name: SetCustomerStatus :exec
UPDATE users SET status = @status, updated_at = @now, version = version + 1 WHERE id = @id;

-- name: RevokeUserSessions :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = @reason::text
WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeUserSession :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = 'admin'
WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL AND expires_at > @now::timestamptz;

-- name: CustomerStrikes :many
SELECT id, reason, order_id, created_at, reset_at, reset_reason
FROM strikes
WHERE subject_type = 'customer' AND subject_id = @user_id
ORDER BY created_at DESC, id DESC
LIMIT 50;

-- name: ResetStrikes :many
UPDATE strikes SET reset_at = @now, reset_by = @by, reset_reason = @reason
WHERE subject_type = @subject_type AND subject_id = @subject_id AND reset_at IS NULL
RETURNING id;

-- name: LiveSessions :many
SELECT id, app, user_agent, created_at, last_used_at, expires_at
FROM sessions
WHERE user_id = @user_id AND revoked_at IS NULL AND expires_at > @now::timestamptz
ORDER BY last_used_at DESC, id DESC
LIMIT 50;

-- name: CustomerRecentOrders :many
SELECT o.id, o.display_number, o.store_id, st.name AS store_name, o.status, o.total_paise, o.placed_at
FROM orders o JOIN stores st ON st.id = o.store_id
WHERE o.customer_id = @customer_id
ORDER BY o.placed_at DESC, o.id DESC
LIMIT 20;

------------------------------------------------------------------------------
-- audit (A-19 and the side panels)
------------------------------------------------------------------------------

-- name: ListAudit :many
SELECT a.id, a.actor_kind, a.actor_id, u.name AS actor_name, a.action, a.subject_type, a.subject_id,
  a.before, a.after, a.created_at
FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
WHERE (sqlc.narg(actor_id)::uuid IS NULL OR a.actor_id = sqlc.narg(actor_id)::uuid)
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action)::text)
  AND (sqlc.narg(subject_type)::text IS NULL OR a.subject_type = sqlc.narg(subject_type)::text)
  AND (sqlc.narg(subject_id)::text IS NULL OR a.subject_id = sqlc.narg(subject_id)::text)
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR a.created_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR a.created_at < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (a.created_at, a.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @lim;

------------------------------------------------------------------------------
-- platform settings (A-17)
------------------------------------------------------------------------------

-- name: ListSettings :many
SELECT ps.key, ps.value, ps.unit, ps.min, ps.max, ps.description, ps.updated_by, u.name AS updated_by_name,
  ps.updated_at, ps.version
FROM platform_settings ps LEFT JOIN users u ON u.id = ps.updated_by
ORDER BY ps.key;

-- name: GetSettingForUpdate :one
SELECT key, value, unit, min, max, version FROM platform_settings WHERE key = @key FOR UPDATE;

-- name: UpdateSetting :exec
-- touch_updated_at sets updated_at; the NOTIFY trigger reloads every settings cache.
UPDATE platform_settings SET value = @value, updated_by = @updated_by, version = version + 1
WHERE key = @key;

-- name: GetSetting :one
SELECT ps.key, ps.value, ps.unit, ps.min, ps.max, ps.description, ps.updated_by, u.name AS updated_by_name,
  ps.updated_at, ps.version
FROM platform_settings ps LEFT JOIN users u ON u.id = ps.updated_by
WHERE ps.key = @key;

-- name: SettingExists :one
SELECT EXISTS (SELECT 1 FROM platform_settings WHERE key = @key)::bool;
