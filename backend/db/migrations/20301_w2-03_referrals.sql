-- W2-03 referrals (01 §8; D-07, D-08, D-13, D-26, D-32, D-72; 03 referrals,
-- referral_clicks).
--
-- referral_clicks: one row per open of a referral link or standee URL whose
-- code resolves (an unknown code writes nothing). The attribution cookie
-- carries the click id; the claim reads the click to find the referrer and
-- the time of first touch. ip and user agent are keyed hashes (PII_HASH_KEY),
-- never the values.
--
-- referrals: one row per new person (unique referee). The reward amounts are
-- a snapshot of the settings at claim time (RF-10). A side's reward is posted
-- once, with the idempotency key referral:<id>:referrer|referee; *_rewarded_at
-- says the side is done (its transaction id is null when the amount was 0).
-- status rewarded means both sides are done. device_hash and
-- referee_phone_hash are keyed hashes kept for the same-device and same-phone
-- rules after the referee's account is gone.
--
-- referral_shop_sales: the completed orders and linked sales that count
-- toward a new shop's qualification (ref_shop_qualifying_orders); the key makes
-- the count idempotent per order or sale.
--
-- standee_id and the *_transaction_id columns are plain uuids: qr_standees
-- (W2-10) and ledger_transactions (W2-01) are built in this wave, so their
-- FKs wait for the wave-2 constraints migration (D-35, D-79).

-- +goose Up

CREATE TABLE referral_clicks (
  id            uuid PRIMARY KEY,
  code          text NOT NULL,
  referrer_type text NOT NULL,
  referrer_id   uuid NOT NULL,
  standee_id    uuid,
  landing       text NOT NULL,
  ip_hash       bytea,
  ua_hash       bytea,
  device_hash   bytea,
  created_at    timestamptz NOT NULL,
  CONSTRAINT ck_referral_clicks_code CHECK (code ~ '^[A-HJKMNP-Z2-9]{6}$'),
  CONSTRAINT ck_referral_clicks_referrer_type CHECK (referrer_type IN ('customer','store','business')),
  CONSTRAINT ck_referral_clicks_landing CHECK (landing IN ('store','app')),
  CONSTRAINT ck_referral_clicks_standee CHECK (standee_id IS NULL OR referrer_type = 'store'),
  CONSTRAINT ck_referral_clicks_hashes CHECK ((ip_hash IS NULL OR octet_length(ip_hash) = 32)
    AND (ua_hash IS NULL OR octet_length(ua_hash) = 32)
    AND (device_hash IS NULL OR octet_length(device_hash) = 32))
);
CREATE INDEX ix_referral_clicks_referrer ON referral_clicks (referrer_type, referrer_id, created_at);
CREATE INDEX ix_referral_clicks_standee ON referral_clicks (standee_id, created_at) WHERE standee_id IS NOT NULL;
CREATE INDEX ix_referral_clicks_created ON referral_clicks USING brin (created_at);

CREATE TABLE referrals (
  id                      uuid PRIMARY KEY,
  kind                    text NOT NULL,
  referrer_type           text NOT NULL,
  referrer_id             uuid NOT NULL,
  referee_type            text NOT NULL,
  referee_id              uuid NOT NULL,
  code                    text NOT NULL,
  source                  text NOT NULL,
  click_id                uuid REFERENCES referral_clicks (id),
  standee_id              uuid,
  status                  text NOT NULL DEFAULT 'pending',
  reject_reason           text,
  device_hash             bytea,
  referee_phone_hash      bytea,
  referrer_credits        int NOT NULL,
  referee_credits         int NOT NULL,
  qualifying_ref_type     text,
  qualifying_ref_id       uuid,
  qualified_at            timestamptz,
  referrer_transaction_id uuid,
  referrer_rewarded_at    timestamptz,
  referee_transaction_id  uuid,
  referee_rewarded_at     timestamptz,
  created_at              timestamptz NOT NULL,
  updated_at              timestamptz NOT NULL,
  version                 int NOT NULL DEFAULT 1,
  CONSTRAINT ck_referrals_kind CHECK (
    (kind = 'retailer_customer' AND referrer_type = 'store' AND referee_type = 'customer')
    OR (kind = 'customer_customer' AND referrer_type = 'customer' AND referee_type = 'customer')
    OR (kind = 'any_shop' AND referrer_type IN ('customer','business') AND referee_type = 'business')),
  CONSTRAINT ck_referrals_code CHECK (code ~ '^[A-HJKMNP-Z2-9]{6}$'),
  CONSTRAINT ck_referrals_source CHECK (source IN ('qr_standee','link','code')),
  CONSTRAINT ck_referrals_standee CHECK ((source = 'qr_standee') = (standee_id IS NOT NULL)),
  CONSTRAINT ck_referrals_status CHECK (status IN ('pending','qualified','rewarded','rejected','rejected_same_device')),
  CONSTRAINT ck_referrals_reject CHECK ((status IN ('rejected','rejected_same_device')) = (reject_reason IS NOT NULL)
    AND (reject_reason IS NULL OR reject_reason ~ '^[a-z][a-z0-9_]{0,40}$')),
  CONSTRAINT ck_referrals_self CHECK (NOT (referrer_type = referee_type AND referrer_id = referee_id)),
  CONSTRAINT ck_referrals_hashes CHECK ((device_hash IS NULL OR octet_length(device_hash) = 32)
    AND (referee_phone_hash IS NULL OR octet_length(referee_phone_hash) = 32)),
  CONSTRAINT ck_referrals_credits CHECK (referrer_credits BETWEEN 0 AND 100000 AND referee_credits BETWEEN 0 AND 100000),
  CONSTRAINT ck_referrals_qualifying CHECK ((qualifying_ref_type IS NULL) = (qualifying_ref_id IS NULL)
    AND (qualifying_ref_type IS NULL OR qualifying_ref_type IN ('order','pos_sale'))
    AND ((status IN ('qualified','rewarded')) = (qualified_at IS NOT NULL))),
  CONSTRAINT ck_referrals_rewarded CHECK ((status = 'rewarded') = (referrer_rewarded_at IS NOT NULL AND referee_rewarded_at IS NOT NULL)
    AND (referrer_transaction_id IS NULL OR referrer_rewarded_at IS NOT NULL)
    AND (referee_transaction_id IS NULL OR referee_rewarded_at IS NOT NULL)
    AND (status IN ('qualified','rewarded') OR (referrer_rewarded_at IS NULL AND referee_rewarded_at IS NULL)))
);
CREATE UNIQUE INDEX ux_referrals_referee ON referrals (referee_type, referee_id);
CREATE INDEX ix_referrals_referrer ON referrals (referrer_type, referrer_id, status);
CREATE INDEX ix_referrals_device ON referrals (device_hash, created_at) WHERE device_hash IS NOT NULL;
CREATE INDEX ix_referrals_phone ON referrals (referee_phone_hash) WHERE referee_phone_hash IS NOT NULL;
CREATE INDEX ix_referrals_qualified ON referrals (qualified_at) WHERE status = 'qualified';
CREATE INDEX ix_referrals_shop_pending ON referrals (created_at) WHERE status = 'pending' AND kind = 'any_shop';
CREATE INDEX ix_referrals_standee ON referrals (standee_id) WHERE standee_id IS NOT NULL;

CREATE TABLE referral_shop_sales (
  referral_id uuid NOT NULL REFERENCES referrals (id),
  ref_type    text NOT NULL,
  ref_id      uuid NOT NULL,
  store_id    uuid NOT NULL REFERENCES stores (id),
  created_at  timestamptz NOT NULL,
  PRIMARY KEY (referral_id, ref_type, ref_id),
  CONSTRAINT ck_referral_shop_sales_ref_type CHECK (ref_type IN ('order','pos_sale'))
);

ALTER TABLE customer_profiles ADD CONSTRAINT fk_customer_profiles_referred_by
  FOREIGN KEY (referred_by_referral_id) REFERENCES referrals (id);
ALTER TABLE businesses ADD CONSTRAINT fk_businesses_referred_by
  FOREIGN KEY (referred_by_referral_id) REFERENCES referrals (id);

-- +goose Down
ALTER TABLE businesses DROP CONSTRAINT IF EXISTS fk_businesses_referred_by;
ALTER TABLE customer_profiles DROP CONSTRAINT IF EXISTS fk_customer_profiles_referred_by;
DROP TABLE IF EXISTS referral_shop_sales;
DROP TABLE IF EXISTS referrals;
DROP TABLE IF EXISTS referral_clicks;
