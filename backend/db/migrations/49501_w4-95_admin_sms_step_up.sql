-- D-264 (founder, 27 Oct): the admin authenticator (TOTP) is gone in every
-- environment. Admins sign in with the SMS OTP alone, and a † action needs a
-- fresh SMS step-up (POST /v1/auth/step-up/send, then /verify) to the admin's
-- own phone. This is the expand half of the change (docs/guide/deployment.md:
-- migrations run before the new API is released and only move forward).
--
-- sessions.step_up_at is the time of the sign-in's last SMS step-up; identity
-- stamps the family's live row at a good verify and refresh copies it to the
-- next row (as totp_verified_at was). A new nullable column without a default
-- is a catalogue change only.
--
-- The admin TOTP secrets and recovery codes are scrubbed now: nothing in the
-- new code reads them, and no secret should outlive the feature. The columns,
-- their checks, sessions.totp_verified_at and admin_recovery_codes are still
-- there because the API being replaced reads them until the new one is live;
-- a later release drops them (the contract half, D-264).

-- +goose Up
ALTER TABLE sessions ADD COLUMN step_up_at timestamptz;

UPDATE admin_users
   SET totp_secret_enc = NULL, totp_enabled_at = NULL, totp_last_step = NULL, totp_failures = 0,
       version = version + 1
 WHERE totp_secret_enc IS NOT NULL OR totp_enabled_at IS NOT NULL OR totp_last_step IS NOT NULL OR totp_failures <> 0;

DELETE FROM admin_recovery_codes;

-- +goose Down
-- The scrubbed secrets and recovery codes cannot come back (nothing reads
-- them any more); only the new column goes.
ALTER TABLE sessions DROP COLUMN IF EXISTS step_up_at;
