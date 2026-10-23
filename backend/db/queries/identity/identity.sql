-- identity queries, generated into package identitydb (gen/sqlc/identitydb).
-- Only W1-01 (internal/identity) edits this folder (D-50). Every time the code
-- compares is a parameter from the application clock, never now().

------------------------------------------------------------------------------
-- OTP (01 §2; D-15)
------------------------------------------------------------------------------

-- name: LockKey :exec
-- Serialises the OTP limit checks of one phone or one IP across instances
-- until the transaction ends.
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: OtpSendsByPhoneSince :one
-- oldest and latest are @since when there is no row (sends = 0).
SELECT count(*)::int AS sends,
  COALESCE(min(created_at), @since::timestamptz)::timestamptz AS oldest,
  COALESCE(max(created_at), @since::timestamptz)::timestamptz AS latest
FROM otp_requests WHERE phone_hash = @phone_hash AND created_at > @since::timestamptz;

-- name: OtpSendsByIPSince :one
SELECT count(*)::int AS sends, COALESCE(min(created_at), @since::timestamptz)::timestamptz AS oldest
FROM otp_requests WHERE ip_hash = @ip_hash AND created_at > @since::timestamptz;

-- name: InsertOtpRequest :exec
INSERT INTO otp_requests (id, phone_hash, phone_enc, purpose, app, code_hash, expires_at, ip_hash, created_at)
VALUES (@id, @phone_hash, @phone_enc, @purpose, @app, @code_hash, @expires_at, sqlc.narg(ip_hash), @created_at);

-- name: GetOtpRequestForUpdate :one
SELECT id, phone_hash, phone_enc, purpose, app, code_hash, expires_at, attempts, consumed_at
FROM otp_requests WHERE id = @id FOR UPDATE;

-- name: RecordOtpAttempt :one
UPDATE otp_requests SET attempts = attempts + 1 WHERE id = @id RETURNING attempts;

-- name: ConsumeOtpRequest :exec
UPDATE otp_requests SET consumed_at = @now::timestamptz WHERE id = @id AND consumed_at IS NULL;

-- name: PurgeOtpRequests :execrows
DELETE FROM otp_requests WHERE created_at < @before;

------------------------------------------------------------------------------
-- Users and phone identities
------------------------------------------------------------------------------

-- name: GetUserByPhoneHash :one
SELECT u.id, u.status
FROM phone_identities p JOIN users u ON u.id = p.user_id
WHERE p.phone_hash = @phone_hash;

-- name: InsertUser :exec
INSERT INTO users (id, created_at, updated_at) VALUES (@id, @now, @now);

-- name: InsertPhoneIdentity :exec
INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at, created_at)
VALUES (@id, @user_id, @phone_enc, @phone_hash, @now, @now);

-- name: GetMe :one
SELECT u.id, u.name, u.preferred_lang, u.status, u.created_at, u.version, p.phone_enc
FROM users u JOIN phone_identities p ON p.user_id = u.id
WHERE u.id = @id;

-- name: UpdateMe :one
UPDATE users SET
  name = CASE WHEN @set_name::bool THEN sqlc.narg(name)::text ELSE name END,
  preferred_lang = COALESCE(sqlc.narg(preferred_lang)::text, preferred_lang),
  version = version + 1
WHERE id = @id AND version = @version AND status = 'active'
RETURNING version;

------------------------------------------------------------------------------
-- Customer profiles and admins
------------------------------------------------------------------------------

-- name: InsertCustomerProfile :execrows
-- No conflict target: a taken referral code also inserts nothing, and the
-- caller retries with a new code unless the profile now exists.
INSERT INTO customer_profiles (user_id, referral_code, created_at, updated_at)
VALUES (@user_id, @referral_code, @now, @now)
ON CONFLICT DO NOTHING;

-- name: GetCustomerProfile :one
SELECT user_id, referral_code, limited_until FROM customer_profiles WHERE user_id = @user_id;

-- name: GetAdminUser :one
SELECT user_id, roles, totp_enabled_at, disabled_at FROM admin_users WHERE user_id = @user_id;

------------------------------------------------------------------------------
-- Devices and sessions (02 §3.1)
------------------------------------------------------------------------------

-- name: UpsertDevice :one
INSERT INTO devices (id, user_id, device_key, last_seen_at, created_at, updated_at)
VALUES (@id, @user_id, @device_key, @now, @now, @now)
ON CONFLICT (user_id, device_key) DO UPDATE
  SET last_seen_at = EXCLUDED.last_seen_at, version = devices.version + 1
RETURNING id;

-- name: InsertSession :exec
INSERT INTO sessions (id, user_id, app, family_id, refresh_hash, expires_at, device_id,
  user_agent, ip_hash, last_used_at, totp_verified_at, created_at)
VALUES (@id, @user_id, @app, @family_id, @refresh_hash, @expires_at, sqlc.narg(device_id),
  sqlc.narg(user_agent), sqlc.narg(ip_hash), @now, sqlc.narg(totp_verified_at), @now);

-- name: GetSessionByRefreshHashForUpdate :one
SELECT s.id, s.user_id, s.app, s.family_id, s.expires_at, s.revoked_at, s.revoked_reason,
  s.device_id, s.totp_verified_at, u.status AS user_status
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.refresh_hash = @refresh_hash
FOR UPDATE OF s;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = @reason::text
WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeFamily :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = @reason::text
WHERE family_id = @family_id AND revoked_at IS NULL;

-- name: GetFamilyState :one
-- Is this sign-in (family) still live, and is its user active?
SELECT u.status,
  COALESCE(bool_or(s.revoked_at IS NULL AND s.expires_at > @now::timestamptz), false)::bool AS live,
  count(s.totp_verified_at) > 0 AS has_totp,
  COALESCE(max(s.totp_verified_at), @now::timestamptz)::timestamptz AS totp_verified_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.family_id = @family_id AND s.user_id = @user_id AND s.app = @app
GROUP BY u.status;

-- name: PurgeSessions :execrows
-- Rows revoked (rotated, reused, logged out) or expired before @before,
-- whichever came first (ix_sessions_purge).
DELETE FROM sessions WHERE coalesce(revoked_at, expires_at) < @before::timestamptz;

------------------------------------------------------------------------------
-- Admin TOTP and recovery codes (W1-01 phase B; 02 §3.2, D-60)
------------------------------------------------------------------------------

-- name: GetAdminForUpdate :one
SELECT user_id, roles, totp_secret_enc, totp_enabled_at, disabled_at, totp_last_step, totp_failures
FROM admin_users WHERE user_id = @user_id FOR UPDATE;

-- name: SetPendingTotp :exec
-- A new, unconfirmed secret: totp_enabled_at stays NULL until a code verifies.
UPDATE admin_users SET totp_secret_enc = @secret_enc, totp_enabled_at = NULL,
  totp_last_step = NULL, totp_failures = 0, version = version + 1
WHERE user_id = @user_id AND totp_enabled_at IS NULL;

-- name: DeleteRecoveryCodes :exec
DELETE FROM admin_recovery_codes WHERE admin_id = @admin_id;

-- name: InsertRecoveryCode :exec
INSERT INTO admin_recovery_codes (id, admin_id, code_hash, created_at)
VALUES (@id, @admin_id, @code_hash, @now);

-- name: RecordTotpSuccess :exec
-- Confirms a pending enrolment too (totp_enabled_at is set once).
UPDATE admin_users SET totp_enabled_at = COALESCE(totp_enabled_at, @now::timestamptz),
  totp_last_step = COALESCE(sqlc.narg(step)::bigint, totp_last_step), totp_failures = 0
WHERE user_id = @user_id;

-- name: RecordTotpFailure :one
UPDATE admin_users SET totp_failures = totp_failures + 1 WHERE user_id = @user_id
RETURNING totp_failures;

-- name: ResetTotpFailures :exec
UPDATE admin_users SET totp_failures = 0 WHERE user_id = @user_id;

-- name: UseRecoveryCode :one
UPDATE admin_recovery_codes SET used_at = @now::timestamptz
WHERE admin_id = @admin_id AND code_hash = @code_hash AND used_at IS NULL
RETURNING id;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*)::int FROM admin_recovery_codes WHERE admin_id = @admin_id AND used_at IS NULL;

-- name: SetFamilyTotpVerified :execrows
-- Marks the live row of a sign-in (family) as TOTP-verified now; refresh
-- copies it to the next row. 0 rows: the sign-in is no longer live.
UPDATE sessions SET totp_verified_at = @now::timestamptz
WHERE family_id = @family_id AND user_id = @user_id AND app = 'admin'
  AND revoked_at IS NULL AND expires_at > @now::timestamptz;

------------------------------------------------------------------------------
-- Sign-out everywhere and account deletion (04 §2.1, §2.2; D-31)
------------------------------------------------------------------------------

-- name: RevokeUserApp :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = @reason::text
WHERE user_id = @user_id AND app = @app AND revoked_at IS NULL;

-- name: RevokeUserSessions :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = @reason::text
WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: LockUser :one
SELECT status FROM users WHERE id = @id FOR UPDATE;

-- name: GetPhoneHashByUser :one
SELECT phone_hash FROM phone_identities WHERE user_id = @user_id;

-- name: AnonymiseUser :exec
UPDATE users SET status = 'deleted', deleted_at = @now::timestamptz, name = NULL, version = version + 1
WHERE id = @id;

-- name: DeletePhoneIdentity :exec
DELETE FROM phone_identities WHERE user_id = @user_id;

-- name: ScrubDevices :exec
UPDATE devices SET push_endpoint = NULL, push_keys = NULL, counter_secret_enc = NULL, version = version + 1
WHERE user_id = @user_id;

-- name: DeleteAddresses :exec
DELETE FROM addresses WHERE user_id = @user_id;
