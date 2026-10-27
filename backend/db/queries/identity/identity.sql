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
-- The 01 §2 sign-in limits. oldest and latest are @since when there is no row
-- (sends = 0). Admin step-up codes have their own bucket (D-264,
-- AdminStepUpSendsSince) and are left out here.
SELECT count(*)::int AS sends,
  COALESCE(min(created_at), @since::timestamptz)::timestamptz AS oldest,
  COALESCE(max(created_at), @since::timestamptz)::timestamptz AS latest
FROM otp_requests WHERE phone_hash = @phone_hash AND created_at > @since::timestamptz
  AND NOT (purpose = 'step_up' AND app = 'admin');

-- name: OtpSendsByIPSince :one
SELECT count(*)::int AS sends, COALESCE(min(created_at), @since::timestamptz)::timestamptz AS oldest
FROM otp_requests WHERE ip_hash = @ip_hash AND created_at > @since::timestamptz
  AND NOT (purpose = 'step_up' AND app = 'admin');

-- name: AdminStepUpSendsSince :one
-- The admin step-up bucket of one number (D-264: admin_step_up_sends_per_hour
-- and the resend gap between step-up codes).
SELECT count(*)::int AS sends,
  COALESCE(min(created_at), @since::timestamptz)::timestamptz AS oldest,
  COALESCE(max(created_at), @since::timestamptz)::timestamptz AS latest
FROM otp_requests WHERE phone_hash = @phone_hash AND created_at > @since::timestamptz
  AND purpose = 'step_up' AND app = 'admin';

-- name: AdminSigninLockedUntil :one
-- The number's admin sign-in lock (D-264); no row when it has none.
SELECT locked_until FROM admin_signin_locks WHERE phone_hash = @phone_hash;

-- name: InsertAdminSigninFailure :exec
INSERT INTO admin_signin_failures (id, phone_hash, failed_at) VALUES (@id, @phone_hash, @failed_at);

-- name: CountAdminSigninFailuresSince :one
SELECT count(*)::int FROM admin_signin_failures
WHERE phone_hash = @phone_hash AND failed_at > @since::timestamptz;

-- name: LockAdminSignin :exec
-- A lock replaces one that has run out.
INSERT INTO admin_signin_locks (phone_hash, locked_at, locked_until) VALUES (@phone_hash, @now, @locked_until)
ON CONFLICT (phone_hash) DO UPDATE SET locked_at = EXCLUDED.locked_at, locked_until = EXCLUDED.locked_until;

-- name: ClearAdminSigninFailures :exec
DELETE FROM admin_signin_failures WHERE phone_hash = @phone_hash;

-- name: DeleteAdminSigninLock :one
-- An unlock (A-18). No row when the number had no lock.
DELETE FROM admin_signin_locks WHERE phone_hash = @phone_hash RETURNING locked_until;

-- name: PurgeAdminSigninFailures :execrows
DELETE FROM admin_signin_failures WHERE failed_at < @before;

-- name: PurgeAdminSigninLocks :execrows
DELETE FROM admin_signin_locks WHERE locked_until < @before;

-- name: InsertOtpRequest :exec
INSERT INTO otp_requests (id, phone_hash, phone_enc, purpose, app, code_hash, expires_at, ip_hash, created_at)
VALUES (@id, @phone_hash, @phone_enc, @purpose, @app, @code_hash, @expires_at, sqlc.narg(ip_hash), @created_at);

-- name: GetOtpRequestForUpdate :one
SELECT id, phone_hash, phone_enc, purpose, app, code_hash, expires_at, attempts, consumed_at
FROM otp_requests WHERE id = @id FOR UPDATE;

-- name: GetStepUpOtpRequestForUpdate :one
-- The read of a step-up code (account deletion, the admin step-up), taken
-- after the users row: only a step_up row is locked (a login row may be held
-- by a sign-in that waits for the user's row these paths hold).
SELECT id, phone_hash, phone_enc, purpose, app, code_hash, expires_at, attempts, consumed_at
FROM otp_requests WHERE id = @id AND purpose = 'step_up' FOR UPDATE;

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
SELECT u.id, u.name, u.preferred_lang, u.status, u.created_at, u.version, u.photo_key, p.phone_enc
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
SELECT user_id, roles, disabled_at FROM admin_users WHERE user_id = @user_id;

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
  user_agent, ip_hash, last_used_at, step_up_at, created_at)
VALUES (@id, @user_id, @app, @family_id, @refresh_hash, @expires_at, sqlc.narg(device_id),
  sqlc.narg(user_agent), sqlc.narg(ip_hash), @now, sqlc.narg(step_up_at), @now);

-- name: GetSessionUserByRefreshHash :one
-- The user of a refresh token, unlocked (a session's user never changes), so
-- refresh, logout and logout-all can lock the users row before the session row.
SELECT user_id FROM sessions WHERE refresh_hash = @refresh_hash;

-- name: GetSessionByRefreshHashForUpdate :one
SELECT s.id, s.user_id, s.app, s.family_id, s.expires_at, s.revoked_at, s.revoked_reason,
  s.device_id, s.step_up_at, s.successor_id, s.successor_salt, u.status AS user_status
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.refresh_hash = @refresh_hash
FOR UPDATE OF s;

-- name: RotateSession :execrows
-- A refresh revokes the presented row as rotated and records the row it
-- inserts and the salt its token was derived with (D-239), so a retry of the
-- presented token inside RotationGrace can be answered with the same token.
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = 'rotated',
  successor_id = @successor_id, successor_salt = @successor_salt
WHERE id = @id AND revoked_at IS NULL;

-- name: GetSessionForUpdate :one
-- A rotated row's successor, for the lost-response replay (D-239). Taken
-- after the users row and the rotated row (lock order: users, sessions).
SELECT id, user_id, app, family_id, refresh_hash, expires_at, revoked_at
FROM sessions WHERE id = @id
FOR UPDATE;

-- name: RevokeFamily :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = @reason::text
WHERE family_id = @family_id AND revoked_at IS NULL;

-- name: GetFamilyState :one
-- Is this sign-in (family) still live, and is its user active?
SELECT u.status,
  COALESCE(bool_or(s.revoked_at IS NULL AND s.expires_at > @now::timestamptz), false)::bool AS live,
  count(s.step_up_at) > 0 AS has_step_up,
  COALESCE(max(s.step_up_at), @now::timestamptz)::timestamptz AS step_up_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.family_id = @family_id AND s.user_id = @user_id AND s.app = @app
GROUP BY u.status;

-- name: PurgeSessions :execrows
-- Rows revoked (rotated, reused, logged out) or expired before @before,
-- whichever came first (ix_sessions_purge).
DELETE FROM sessions WHERE coalesce(revoked_at, expires_at) < @before::timestamptz;

------------------------------------------------------------------------------
-- Admin SMS step-up (D-264; 01 §2)
------------------------------------------------------------------------------

-- name: GetPhoneByUser :one
-- The admin's own number, where a step-up code goes: never from the client.
SELECT phone_hash, phone_enc FROM phone_identities WHERE user_id = @user_id;

-- name: SetFamilyStepUp :execrows
-- Marks the live row of an admin sign-in (family) as stepped up now; refresh
-- copies it to the next row. 0 rows: the sign-in is no longer live.
UPDATE sessions SET step_up_at = @now::timestamptz
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
-- The lock of every path that rotates or revokes a user's sessions: refresh
-- (its reuse branch revokes a family), logout, logout-all, the admin SMS
-- step-up (its lockout) and deletion. They run one at a time per
-- user, so no revocation misses a row a concurrent rotation inserts. It does
-- not conflict with the KEY SHARE lock a foreign key takes (no deadlock with
-- inserts that reference the user). Lock order: users, then admin_users,
-- then sessions.
SELECT status FROM users WHERE id = @id FOR NO KEY UPDATE;

-- name: LockUserShared :one
-- Sign-in's lock, after the unlocked lookup by phone: sign-ins run alongside
-- each other but not alongside a deletion (LockUser), and the status read
-- under it is current.
SELECT status FROM users WHERE id = @id FOR SHARE;

-- name: GetPhoneHashByUser :one
SELECT phone_hash FROM phone_identities WHERE user_id = @user_id;

-- name: AnonymiseUser :exec
UPDATE users SET status = 'deleted', deleted_at = @now::timestamptz, name = NULL, photo_key = NULL, version = version + 1
WHERE id = @id;

-- name: DeletePhoneIdentity :exec
DELETE FROM phone_identities WHERE user_id = @user_id;

-- name: ScrubDevices :exec
UPDATE devices SET push_endpoint = NULL, push_keys = NULL, counter_secret_enc = NULL, version = version + 1
WHERE user_id = @user_id;

-- name: ListDeviceIDs :many
SELECT id FROM devices WHERE user_id = @user_id ORDER BY id;

-- name: SetDeviceKey :exec
-- Deletion replaces the client's device id, which stays in the browser and
-- would link a new account on that browser to the deleted one.
UPDATE devices SET device_key = @device_key, version = version + 1 WHERE id = @id;

-- name: ScrubSessionPII :exec
UPDATE sessions SET user_agent = NULL, ip_hash = NULL
WHERE user_id = @user_id AND (user_agent IS NOT NULL OR ip_hash IS NOT NULL);

-- name: DeleteAddresses :exec
DELETE FROM addresses WHERE user_id = @user_id;

------------------------------------------------------------------------------
-- Profile photo (D-259)
------------------------------------------------------------------------------

-- name: CountRecentPhotoUploads :one
SELECT count(*)::int FROM user_photo_uploads WHERE user_id = @user_id AND created_at > @since::timestamptz;

-- name: InsertPhotoUpload :exec
INSERT INTO user_photo_uploads (id, user_id, object_key, content_type, size_bytes, created_at)
VALUES (@id, @user_id, @object_key, @content_type, @size_bytes, @now::timestamptz);

-- name: StalePhotoUploads :many
SELECT id, object_key FROM user_photo_uploads
WHERE user_id = @user_id AND status = 'uploading' AND created_at < @before::timestamptz
ORDER BY created_at
LIMIT 20;

-- name: ExpirePhotoUpload :exec
UPDATE user_photo_uploads SET status = 'expired' WHERE id = @id AND status = 'uploading';

-- name: LockPhotoUpload :one
SELECT id, object_key, content_type, size_bytes, status FROM user_photo_uploads
WHERE id = @id AND user_id = @user_id
FOR UPDATE;

-- name: MarkPhotoUploadDone :exec
UPDATE user_photo_uploads SET status = 'done', confirmed_at = @now::timestamptz WHERE id = @id;

-- name: LockUserPhoto :one
-- The user's current photo, with the row locked for the change.
SELECT photo_key, status FROM users WHERE id = @id FOR UPDATE;

-- name: SetUserPhoto :exec
UPDATE users SET photo_key = sqlc.narg(photo_key)::text, version = version + 1 WHERE id = @id;

-- name: UserPhotoObjects :many
-- Every object a user's photos may have left in the bucket: each upload's
-- file (<key>.upload), its cleaned copy (<key>.jpg) and the current photo.
SELECT object_key AS key FROM user_photo_uploads WHERE user_photo_uploads.user_id = @user_id::uuid
UNION
SELECT regexp_replace(object_key, '\.upload$', '.jpg') FROM user_photo_uploads WHERE user_photo_uploads.user_id = @user_id::uuid AND status = 'done'
UNION
SELECT photo_key FROM users WHERE users.id = @user_id::uuid AND photo_key IS NOT NULL;

-- name: DeletePhotoUploads :exec
DELETE FROM user_photo_uploads WHERE user_id = @user_id;
