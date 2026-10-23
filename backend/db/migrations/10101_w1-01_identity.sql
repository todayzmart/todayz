-- W1-01 identity (03 §1; 02 §3.1; 04 §1.3; D-14, D-15, D-18, D-31, D-35, D-64).
-- users, phone_identities, otp_requests, devices, sessions, admin_users,
-- admin_recovery_codes, customer_profiles, addresses; then the D-35 foreign
-- keys from W0-03's platform_settings.updated_by and risk_flags.reviewed_by.
--
-- Times the code compares (OTP expiry and limits, session expiry) are written
-- from the application clock (clock.Clock, the test clock in local/test), so
-- every such column is set by the INSERT, never by a now() default.
-- Phones: *_enc is AES-256-GCM under PII_ENC_KEY, *_hash is HMAC-SHA256 under
-- PII_HASH_KEY (platform/pii). Codes and tokens are stored hashed only.

-- +goose Up

------------------------------------------------------------------------------
-- users: one per human (03 §1). Deletion anonymises the row (phase B, D-31).
------------------------------------------------------------------------------
CREATE TABLE users (
  id             uuid PRIMARY KEY,
  name           text,
  preferred_lang text NOT NULL DEFAULT 'en',
  status         text NOT NULL DEFAULT 'active',
  deleted_at     timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  version        int NOT NULL DEFAULT 1,
  CONSTRAINT ck_users_lang CHECK (preferred_lang IN ('en','hi','kn','te','ta')),
  CONSTRAINT ck_users_status CHECK (status IN ('active','blocked','deleted')),
  CONSTRAINT ck_users_deleted CHECK ((status = 'deleted') = (deleted_at IS NOT NULL)),
  CONSTRAINT ck_users_name CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 80)
);
CREATE TRIGGER trg_users_touch BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- phone_identities: the phone, encrypted, with a keyed hash for lookup.
-- One phone is one identity (a customer and a business user at once, 01 §2).
------------------------------------------------------------------------------
CREATE TABLE phone_identities (
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL,
  phone_enc   bytea NOT NULL,
  phone_hash  bytea NOT NULL,
  verified_at timestamptz NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_phone_identities_user_id FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT ck_phone_identities_hash CHECK (octet_length(phone_hash) = 32)
);
CREATE UNIQUE INDEX ux_phone_identities_phone_hash ON phone_identities (phone_hash);
CREATE UNIQUE INDEX ux_phone_identities_user_id ON phone_identities (user_id);

------------------------------------------------------------------------------
-- otp_requests (D-15): the code is stored as an HMAC bound to the request id,
-- never in clear and never logged. Limits (01 §2) are counted here, so they
-- hold across API instances: per phone and per IP per hour, resend gap.
-- Purged after 24 h by identity.purge.
------------------------------------------------------------------------------
CREATE TABLE otp_requests (
  id          uuid PRIMARY KEY,
  phone_hash  bytea NOT NULL,
  phone_enc   bytea NOT NULL,  -- the verify step creates the identity from it
  purpose     text NOT NULL,
  app         text NOT NULL,
  code_hash   bytea NOT NULL,
  expires_at  timestamptz NOT NULL,
  attempts    int NOT NULL DEFAULT 0,
  consumed_at timestamptz,
  ip_hash     bytea,
  created_at  timestamptz NOT NULL,
  CONSTRAINT ck_otp_requests_purpose CHECK (purpose IN ('login','step_up','owner_confirm')),
  CONSTRAINT ck_otp_requests_app CHECK (app IN ('customer','business','admin')),
  CONSTRAINT ck_otp_requests_attempts CHECK (attempts >= 0),
  CONSTRAINT ck_otp_requests_hashes CHECK (octet_length(phone_hash) = 32 AND octet_length(code_hash) = 32
    AND (ip_hash IS NULL OR octet_length(ip_hash) = 32))
);
CREATE INDEX ix_otp_requests_phone ON otp_requests (phone_hash, created_at DESC);
CREATE INDEX ix_otp_requests_ip ON otp_requests (ip_hash, created_at DESC) WHERE ip_hash IS NOT NULL;
CREATE INDEX ix_otp_requests_created ON otp_requests (created_at);

------------------------------------------------------------------------------
-- devices: one per (user, client device id). Push and the counter-code secret
-- come with their WPs (W2-08 push, W2-04 counter codes; D-13 same-device).
------------------------------------------------------------------------------
CREATE TABLE devices (
  id                 uuid PRIMARY KEY,
  user_id            uuid NOT NULL,
  device_key         text NOT NULL,  -- the client's own ULID (deviceId), not a secret
  kind               text NOT NULL DEFAULT 'web',
  push_endpoint      text,
  push_keys          bytea,          -- encrypted (PII_ENC_KEY)
  counter_secret_enc bytea,          -- DEVICE_SECRET_ENC_KEY
  last_seen_at       timestamptz NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  version            int NOT NULL DEFAULT 1,
  CONSTRAINT fk_devices_user_id FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT ck_devices_kind CHECK (kind IN ('web')),
  CONSTRAINT ck_devices_key CHECK (device_key ~ '^[0-9A-HJKMNP-TV-Z]{26}$')
);
CREATE UNIQUE INDEX ux_devices_user_key ON devices (user_id, device_key);
CREATE TRIGGER trg_devices_touch BEFORE UPDATE ON devices
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- sessions (02 §3.1): one row per refresh token. A sign-in starts a family
-- (family_id = the first row's id, the access token's `sid`); each refresh
-- revokes its row as 'rotated' and inserts the next. Presenting a rotated
-- token again revokes the whole family as 'reuse' (theft detection, AU-07).
------------------------------------------------------------------------------
CREATE TABLE sessions (
  id               uuid PRIMARY KEY,
  user_id          uuid NOT NULL,
  app              text NOT NULL,
  family_id        uuid NOT NULL,
  refresh_hash     bytea NOT NULL,  -- SHA-256 of the 256-bit token
  expires_at       timestamptz NOT NULL,
  revoked_at       timestamptz,
  revoked_reason   text,
  device_id        uuid,
  user_agent       text,
  ip_hash          bytea,
  last_used_at     timestamptz NOT NULL,
  totp_verified_at timestamptz,     -- admin: last TOTP (amr otp+totp, step-up); phase B writes it
  created_at       timestamptz NOT NULL,
  CONSTRAINT fk_sessions_user_id FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT fk_sessions_device_id FOREIGN KEY (device_id) REFERENCES devices (id),
  CONSTRAINT ck_sessions_app CHECK (app IN ('customer','business','admin')),
  CONSTRAINT ck_sessions_hash CHECK (octet_length(refresh_hash) = 32),
  CONSTRAINT ck_sessions_revoked CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),
  CONSTRAINT ck_sessions_reason CHECK (revoked_reason IS NULL OR revoked_reason IN
    ('rotated','reuse','logout','logout_all','blocked','deleted','admin')),
  CONSTRAINT ck_sessions_user_agent CHECK (user_agent IS NULL OR char_length(user_agent) <= 256)
);
CREATE UNIQUE INDEX ux_sessions_refresh_hash ON sessions (refresh_hash);
CREATE INDEX ix_sessions_family ON sessions (family_id);
CREATE INDEX ix_sessions_user_live ON sessions (user_id, app) WHERE revoked_at IS NULL;
CREATE INDEX ix_sessions_device_id ON sessions (device_id) WHERE device_id IS NOT NULL;
CREATE INDEX ix_sessions_expires ON sessions (expires_at);

------------------------------------------------------------------------------
-- admin_users (D-64) and admin_recovery_codes (D-60). TOTP enrolment, step-up
-- and recovery codes are W1-01 phase B; the tables exist now so the admin
-- sign-in check and the seed have them.
------------------------------------------------------------------------------
CREATE TABLE admin_users (
  user_id         uuid PRIMARY KEY,
  roles           text[] NOT NULL,
  totp_secret_enc bytea,  -- TOTP_ENC_KEY
  totp_enabled_at timestamptz,
  created_by      uuid,
  disabled_at     timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         int NOT NULL DEFAULT 1,
  CONSTRAINT fk_admin_users_user_id FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT fk_admin_users_created_by FOREIGN KEY (created_by) REFERENCES users (id),
  CONSTRAINT ck_admin_users_roles CHECK (cardinality(roles) >= 1 AND roles <@ ARRAY[
    'field_agent','support','catalogue','analyst','finance','admin','super_admin']::text[]),
  CONSTRAINT ck_admin_users_totp CHECK ((totp_enabled_at IS NULL) OR (totp_secret_enc IS NOT NULL))
);
CREATE INDEX ix_admin_users_created_by ON admin_users (created_by) WHERE created_by IS NOT NULL;
CREATE TRIGGER trg_admin_users_touch BEFORE UPDATE ON admin_users
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE admin_recovery_codes (
  id         uuid PRIMARY KEY,
  admin_id   uuid NOT NULL,
  code_hash  bytea NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_admin_recovery_codes_admin_id FOREIGN KEY (admin_id) REFERENCES admin_users (user_id) ON DELETE CASCADE,
  CONSTRAINT ck_admin_recovery_codes_hash CHECK (octet_length(code_hash) = 32)
);
CREATE INDEX ix_admin_recovery_codes_admin_id ON admin_recovery_codes (admin_id);

------------------------------------------------------------------------------
-- customer_profiles: created on the first customer-app sign-in.
-- referred_by_referral_id gets its FK from W2-03 (D-35). limited_until is the
-- no-show limit (01 §5.4, C-21), written by orders.
------------------------------------------------------------------------------
CREATE TABLE customer_profiles (
  user_id                 uuid PRIMARY KEY,
  referral_code           text NOT NULL,
  referred_by_referral_id uuid,
  strikes_count_30d       int NOT NULL DEFAULT 0,
  limited_until           timestamptz,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  version                 int NOT NULL DEFAULT 1,
  CONSTRAINT fk_customer_profiles_user_id FOREIGN KEY (user_id) REFERENCES users (id),
  -- 6 characters, no ambiguous letters or digits (0 O 1 I L).
  CONSTRAINT ck_customer_profiles_referral_code CHECK (referral_code ~ '^[A-HJKMNP-Z2-9]{6}$'),
  CONSTRAINT ck_customer_profiles_strikes CHECK (strikes_count_30d >= 0)
);
CREATE UNIQUE INDEX ux_customer_profiles_referral_code ON customer_profiles (referral_code);
CREATE INDEX ix_customer_profiles_referred_by ON customer_profiles (referred_by_referral_id)
  WHERE referred_by_referral_id IS NOT NULL;
CREATE TRIGGER trg_customer_profiles_touch BEFORE UPDATE ON customer_profiles
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- addresses (03 §1). The API for them comes with the customer delivery flow.
------------------------------------------------------------------------------
CREATE TABLE addresses (
  id         uuid PRIMARY KEY,
  user_id    uuid NOT NULL,
  label      text NOT NULL,
  line1      text NOT NULL,
  line2      text,
  landmark   text,
  pincode    text NOT NULL,
  location   geography(Point, 4326) NOT NULL,
  is_default boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version    int NOT NULL DEFAULT 1,
  CONSTRAINT fk_addresses_user_id FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT ck_addresses_pincode CHECK (pincode ~ '^[1-9][0-9]{5}$'),
  CONSTRAINT ck_addresses_label CHECK (char_length(label) BETWEEN 1 AND 40),
  CONSTRAINT ck_addresses_line1 CHECK (char_length(line1) BETWEEN 1 AND 200)
);
CREATE INDEX ix_addresses_user_id ON addresses (user_id);
CREATE UNIQUE INDEX ux_addresses_user_default ON addresses (user_id) WHERE is_default;
CREATE TRIGGER trg_addresses_touch BEFORE UPDATE ON addresses
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- D-35: W0-03's plain uuid columns that point at users (03 §13).
------------------------------------------------------------------------------
ALTER TABLE platform_settings
  ADD CONSTRAINT fk_platform_settings_updated_by FOREIGN KEY (updated_by) REFERENCES users (id);
CREATE INDEX ix_platform_settings_updated_by ON platform_settings (updated_by) WHERE updated_by IS NOT NULL;
ALTER TABLE risk_flags
  ADD CONSTRAINT fk_risk_flags_reviewed_by FOREIGN KEY (reviewed_by) REFERENCES users (id);
CREATE INDEX ix_risk_flags_reviewed_by ON risk_flags (reviewed_by) WHERE reviewed_by IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ix_risk_flags_reviewed_by;
ALTER TABLE risk_flags DROP CONSTRAINT IF EXISTS fk_risk_flags_reviewed_by;
DROP INDEX IF EXISTS ix_platform_settings_updated_by;
ALTER TABLE platform_settings DROP CONSTRAINT IF EXISTS fk_platform_settings_updated_by;
DROP TABLE IF EXISTS addresses;
DROP TABLE IF EXISTS customer_profiles;
DROP TABLE IF EXISTS admin_recovery_codes;
DROP TABLE IF EXISTS admin_users;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS otp_requests;
DROP TABLE IF EXISTS phone_identities;
DROP TABLE IF EXISTS users;
