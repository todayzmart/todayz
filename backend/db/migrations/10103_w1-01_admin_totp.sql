-- W1-01 phase B: admin TOTP state (02 §3.2, D-60).
--
-- totp_last_step is the RFC 6238 time step of the last accepted code: a code
-- is accepted only for a later step, so a code seen once (shoulder-surfed,
-- replayed from a log) never works twice. totp_failures counts wrong TOTP and
-- recovery codes in a row; identity ends the admin's sessions at the limit
-- and resets it on success.
-- A recovery code is looked up by (admin_id, code_hash); the codes of one
-- admin are distinct, so the index is unique.

-- +goose Up
ALTER TABLE admin_users ADD COLUMN totp_last_step bigint;
ALTER TABLE admin_users ADD COLUMN totp_failures int NOT NULL DEFAULT 0;
ALTER TABLE admin_users ADD CONSTRAINT ck_admin_users_totp_failures CHECK (totp_failures >= 0);
CREATE UNIQUE INDEX ux_admin_recovery_codes_code ON admin_recovery_codes (admin_id, code_hash);

-- +goose Down
DROP INDEX IF EXISTS ux_admin_recovery_codes_code;
ALTER TABLE admin_users DROP CONSTRAINT IF EXISTS ck_admin_users_totp_failures;
ALTER TABLE admin_users DROP COLUMN IF EXISTS totp_failures;
ALTER TABLE admin_users DROP COLUMN IF EXISTS totp_last_step;
