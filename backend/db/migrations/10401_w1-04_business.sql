-- W1-04 businesses, stores, staff and KYC (03 §2, §9 leads; 01 §3; 02 §3.3;
-- 04 §2.5–2.6; D-26, D-35, D-40, D-41, D-44, D-46, D-66, D-101).
-- businesses, business_kyc, business_documents, stores, staff, leads.
--
-- Plain uuid columns without a foreign key yet (D-35, 03 §13): the users
-- columns (onboarded_by, verified_by, reviewed_by, staff.user_id,
-- staff.invited_by) get theirs from the Wave 1 constraints migration,
-- businesses.category_id from the same (categories is W1-05), and
-- businesses.referred_by_referral_id from W2-03. Each has its index.
--
-- Encryption (03 header, D-18, D-44): *_enc is AES-256-GCM under PII_ENC_KEY
-- (platform/pii), *_hash is HMAC-SHA256 under PII_HASH_KEY. Only the *_last4
-- copies of KYC values are ever shown.

-- +goose Up

------------------------------------------------------------------------------
-- businesses: KYC verification is per business (01 §3, D-40).
-- needs_info_fields / needs_info_note are what an admin asks for when moving
-- a business to needs_info (SC-BZ-04); the business app shows them. They are
-- not in 03 §2 (W1-04 report, Questions).
------------------------------------------------------------------------------
CREATE TABLE businesses (
  id                      uuid PRIMARY KEY,
  brand_name              text NOT NULL,
  legal_name              text,
  category_id             uuid,
  status                  text NOT NULL DEFAULT 'draft',
  onboarded_by            uuid,
  referral_code           text NOT NULL,
  referred_by_referral_id uuid,
  needs_info_fields       text[],
  needs_info_note         text,
  submitted_at            timestamptz,
  verified_at             timestamptz,
  verified_by             uuid,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  version                 int NOT NULL DEFAULT 1,
  CONSTRAINT ck_businesses_status CHECK (status IN ('draft','submitted','needs_info','verified','rejected','suspended')),
  CONSTRAINT ck_businesses_brand_name CHECK (char_length(brand_name) BETWEEN 1 AND 80),
  CONSTRAINT ck_businesses_legal_name CHECK (legal_name IS NULL OR char_length(legal_name) BETWEEN 1 AND 120),
  -- 6 characters, no ambiguous letters or digits (0 O 1 I L), as customer codes.
  CONSTRAINT ck_businesses_referral_code CHECK (referral_code ~ '^[A-HJKMNP-Z2-9]{6}$'),
  CONSTRAINT ck_businesses_needs_info_note CHECK (needs_info_note IS NULL OR char_length(needs_info_note) <= 500),
  CONSTRAINT ck_businesses_needs_info_fields CHECK (needs_info_fields IS NULL OR cardinality(needs_info_fields) <= 20)
);
CREATE UNIQUE INDEX ux_businesses_referral_code ON businesses (referral_code);
CREATE INDEX ix_businesses_category_id ON businesses (category_id) WHERE category_id IS NOT NULL;
CREATE INDEX ix_businesses_onboarded_by ON businesses (onboarded_by) WHERE onboarded_by IS NOT NULL;
CREATE INDEX ix_businesses_verified_by ON businesses (verified_by) WHERE verified_by IS NOT NULL;
CREATE INDEX ix_businesses_referred_by ON businesses (referred_by_referral_id)
  WHERE referred_by_referral_id IS NOT NULL;
-- The admin verification queue (W1-10): submitted businesses, oldest first.
CREATE INDEX ix_businesses_status_submitted ON businesses (status, submitted_at);
CREATE TRIGGER trg_businesses_touch BEFORE UPDATE ON businesses
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- business_kyc: payout details (01 §3), one row per business. Every value is
-- sealed; the *_last4 copy is the only one an API shows (D-44).
------------------------------------------------------------------------------
CREATE TABLE business_kyc (
  business_id        uuid PRIMARY KEY,
  pan_enc            bytea,
  pan_last4          text,
  gstin_enc          bytea,
  gstin_last4        text,
  upi_id_enc         bytea,
  upi_id_last4       text,
  bank_account_enc   bytea,
  bank_account_last4 text,
  bank_ifsc          text,
  legal_name         text,
  status             text NOT NULL DEFAULT 'missing',
  verified_by        uuid,
  verified_at        timestamptz,
  notes              text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  version            int NOT NULL DEFAULT 1,
  CONSTRAINT fk_business_kyc_business_id FOREIGN KEY (business_id) REFERENCES businesses (id),
  CONSTRAINT ck_business_kyc_status CHECK (status IN ('missing','submitted','verified','rejected')),
  CONSTRAINT ck_business_kyc_pan CHECK ((pan_enc IS NULL) = (pan_last4 IS NULL)),
  CONSTRAINT ck_business_kyc_gstin CHECK ((gstin_enc IS NULL) = (gstin_last4 IS NULL)),
  CONSTRAINT ck_business_kyc_upi CHECK ((upi_id_enc IS NULL) = (upi_id_last4 IS NULL)),
  CONSTRAINT ck_business_kyc_bank CHECK ((bank_account_enc IS NULL) = (bank_account_last4 IS NULL)),
  CONSTRAINT ck_business_kyc_last4 CHECK (
    (pan_last4 IS NULL OR char_length(pan_last4) = 4)
    AND (gstin_last4 IS NULL OR char_length(gstin_last4) = 4)
    AND (upi_id_last4 IS NULL OR char_length(upi_id_last4) <= 4)
    AND (bank_account_last4 IS NULL OR char_length(bank_account_last4) = 4)),
  CONSTRAINT ck_business_kyc_ifsc CHECK (bank_ifsc IS NULL OR bank_ifsc ~ '^[A-Z]{4}0[A-Z0-9]{6}$'),
  CONSTRAINT ck_business_kyc_legal_name CHECK (legal_name IS NULL OR char_length(legal_name) BETWEEN 1 AND 120),
  CONSTRAINT ck_business_kyc_notes CHECK (notes IS NULL OR char_length(notes) <= 1000)
);
CREATE INDEX ix_business_kyc_verified_by ON business_kyc (verified_by) WHERE verified_by IS NOT NULL;
CREATE TRIGGER trg_business_kyc_touch BEFORE UPDATE ON business_kyc
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- stores (03 §2): per store status (D-40), fulfilment options (D-41) and the
-- settings, which live on the row (history in audit_log). hours is
-- {"weekly":{"mon":[{"open":"09:00","close":"21:00"}],…},"holidays":[…]};
-- internal/business validates it. delivery_hours NULL means the opening
-- hours. rating_* and the reliability columns have single writers (D-59).
------------------------------------------------------------------------------
CREATE TABLE stores (
  id                         uuid PRIMARY KEY,
  business_id                uuid NOT NULL,
  slug                       text NOT NULL,
  short_code                 text NOT NULL,
  name                       text NOT NULL,
  address_line1              text,
  address_line2              text,
  landmark                   text,
  locality                   text,
  city                       text,
  pincode                    text,
  location                   geography(Point, 4326),
  phone_enc                  bytea,
  hours                      jsonb NOT NULL DEFAULT '{}'::jsonb,
  closed_until               timestamptz,
  status                     text NOT NULL DEFAULT 'draft',
  live_at                    timestamptz,
  reviewed_by                uuid,
  reviewed_at                timestamptz,
  accepts_pickup             boolean NOT NULL DEFAULT true,
  accepts_delivery           boolean NOT NULL DEFAULT false,
  asks_muted                 boolean NOT NULL DEFAULT false,
  delivery_radius_m          int NOT NULL DEFAULT 2000,
  delivery_fee_paise         bigint NOT NULL DEFAULT 0,
  delivery_free_above_paise  bigint,
  delivery_min_order_paise   bigint NOT NULL DEFAULT 0,
  delivery_hours             jsonb,
  cashback_bp                int NOT NULL DEFAULT 0,
  auto_accept                boolean NOT NULL DEFAULT false,
  catalogue_seq              bigint NOT NULL DEFAULT 0,
  rating_avg                 numeric(3,2),
  rating_count               int NOT NULL DEFAULT 0,
  accept_rate_30d            numeric(5,4),
  unaccepted_expiry_rate_30d numeric(5,4),
  reliability_score          numeric(4,3),
  reliability_computed_at    timestamptz,
  created_at                 timestamptz NOT NULL DEFAULT now(),
  updated_at                 timestamptz NOT NULL DEFAULT now(),
  version                    int NOT NULL DEFAULT 1,
  CONSTRAINT fk_stores_business_id FOREIGN KEY (business_id) REFERENCES businesses (id),
  CONSTRAINT ck_stores_status CHECK (status IN ('draft','pending_review','live','paused','suspended')),
  CONSTRAINT ck_stores_slug CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 64),
  CONSTRAINT ck_stores_short_code CHECK (short_code ~ '^[A-HJKMNP-Z2-9]{6}$'),
  CONSTRAINT ck_stores_name CHECK (char_length(name) BETWEEN 1 AND 80),
  CONSTRAINT ck_stores_address CHECK (
    (address_line1 IS NULL OR char_length(address_line1) BETWEEN 1 AND 200)
    AND (address_line2 IS NULL OR char_length(address_line2) BETWEEN 1 AND 200)
    AND (landmark IS NULL OR char_length(landmark) BETWEEN 1 AND 120)
    AND (locality IS NULL OR char_length(locality) BETWEEN 1 AND 80)
    AND (city IS NULL OR char_length(city) BETWEEN 1 AND 80)),
  CONSTRAINT ck_stores_pincode CHECK (pincode IS NULL OR pincode ~ '^[1-9][0-9]{5}$'),
  CONSTRAINT ck_stores_hours CHECK (jsonb_typeof(hours) = 'object'),
  CONSTRAINT ck_stores_delivery_hours CHECK (delivery_hours IS NULL OR jsonb_typeof(delivery_hours) = 'object'),
  CONSTRAINT ck_stores_live CHECK (status <> 'live' OR live_at IS NOT NULL),
  CONSTRAINT ck_stores_delivery_radius CHECK (delivery_radius_m BETWEEN 100 AND 20000),
  CONSTRAINT ck_stores_delivery_money CHECK (delivery_fee_paise >= 0 AND delivery_min_order_paise >= 0
    AND (delivery_free_above_paise IS NULL OR delivery_free_above_paise > 0)),
  -- 0–20 % in 0.5 % steps (01 §7).
  CONSTRAINT ck_stores_cashback CHECK (cashback_bp BETWEEN 0 AND 2000 AND cashback_bp % 50 = 0),
  CONSTRAINT ck_stores_rating CHECK (rating_count >= 0 AND (rating_avg IS NULL OR rating_avg BETWEEN 0 AND 5)),
  CONSTRAINT ck_stores_rates CHECK ((accept_rate_30d IS NULL OR accept_rate_30d BETWEEN 0 AND 1)
    AND (unaccepted_expiry_rate_30d IS NULL OR unaccepted_expiry_rate_30d BETWEEN 0 AND 1)
    AND (reliability_score IS NULL OR reliability_score BETWEEN 0 AND 1)),
  CONSTRAINT ck_stores_catalogue_seq CHECK (catalogue_seq >= 0)
);
CREATE UNIQUE INDEX ux_stores_slug ON stores (slug);
CREATE UNIQUE INDEX ux_stores_short_code ON stores (short_code);
CREATE INDEX ix_stores_business_id ON stores (business_id);
CREATE INDEX ix_stores_reviewed_by ON stores (reviewed_by) WHERE reviewed_by IS NOT NULL;
-- 03 §11: nearby live stores (queries also join businesses.status = 'verified').
CREATE INDEX ix_stores_location ON stores USING gist (location) WHERE status = 'live';
-- The admin store review queue (D-40).
CREATE INDEX ix_stores_pending_review ON stores (updated_at) WHERE status = 'pending_review';
CREATE TRIGGER trg_stores_touch BEFORE UPDATE ON stores
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- business_documents: files in the private bucket (D-46). The row is made
-- 'pending' when the upload URL is issued, with the size the client declared
-- (the presigned PUT signs it); 'uploaded' once the object is confirmed; an
-- admin accepts or rejects it.
------------------------------------------------------------------------------
CREATE TABLE business_documents (
  id            uuid PRIMARY KEY,
  business_id   uuid NOT NULL,
  store_id      uuid,
  kind          text NOT NULL,
  object_key    text NOT NULL,
  content_type  text NOT NULL,
  size_bytes    bigint NOT NULL,
  status        text NOT NULL DEFAULT 'pending',
  reviewed_by   uuid,
  reviewed_at   timestamptz,
  reject_reason text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  version       int NOT NULL DEFAULT 1,
  CONSTRAINT fk_business_documents_business_id FOREIGN KEY (business_id) REFERENCES businesses (id),
  CONSTRAINT fk_business_documents_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT ck_business_documents_kind CHECK (kind IN ('pan','gst','shop_photo','storefront','license','other')),
  CONSTRAINT ck_business_documents_status CHECK (status IN ('pending','uploaded','accepted','rejected')),
  CONSTRAINT ck_business_documents_size CHECK (size_bytes > 0),
  CONSTRAINT ck_business_documents_content_type CHECK (content_type IN ('image/jpeg','image/png','image/webp','application/pdf')),
  CONSTRAINT ck_business_documents_object_key CHECK (char_length(object_key) BETWEEN 1 AND 512),
  CONSTRAINT ck_business_documents_reject_reason CHECK (reject_reason IS NULL OR char_length(reject_reason) <= 500)
);
CREATE UNIQUE INDEX ux_business_documents_object_key ON business_documents (object_key);
CREATE INDEX ix_business_documents_business_id ON business_documents (business_id, created_at DESC);
CREATE INDEX ix_business_documents_kind ON business_documents (business_id, kind) WHERE status <> 'rejected';
CREATE INDEX ix_business_documents_store_id ON business_documents (store_id) WHERE store_id IS NOT NULL;
CREATE INDEX ix_business_documents_reviewed_by ON business_documents (reviewed_by) WHERE reviewed_by IS NOT NULL;
CREATE TRIGGER trg_business_documents_touch BEFORE UPDATE ON business_documents
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- staff: a user's role in one business (02 §3.3, D-65, D-66). store_ids NULL
-- means every store. An invite is a row in status 'invited'; user_id is set
-- when the phone already has an account, else when the invitee accepts. The
-- invited phone is kept sealed (masked in the staff list) and hashed (to
-- find the invite and refuse a duplicate). invited_at starts the invite's
-- expiry clock (a re-invite restarts it). One live owner per business.
------------------------------------------------------------------------------
CREATE TABLE staff (
  id                uuid PRIMARY KEY,
  business_id       uuid NOT NULL,
  user_id           uuid,
  role              text NOT NULL,
  store_ids         uuid[],
  can_deliver       boolean NOT NULL DEFAULT false,
  invited_by        uuid,
  status            text NOT NULL DEFAULT 'invited',
  invite_phone_hash bytea,
  invite_phone_enc  bytea,
  invited_at        timestamptz,
  removed_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  version           int NOT NULL DEFAULT 1,
  CONSTRAINT fk_staff_business_id FOREIGN KEY (business_id) REFERENCES businesses (id),
  CONSTRAINT ck_staff_role CHECK (role IN ('owner','manager','cashier')),
  CONSTRAINT ck_staff_status CHECK (status IN ('invited','active','removed')),
  CONSTRAINT ck_staff_active_user CHECK (status <> 'active' OR user_id IS NOT NULL),
  CONSTRAINT ck_staff_invite CHECK (status <> 'invited' OR user_id IS NOT NULL OR invite_phone_hash IS NOT NULL),
  CONSTRAINT ck_staff_invited_at CHECK (status <> 'invited' OR invited_at IS NOT NULL),
  CONSTRAINT ck_staff_removed CHECK ((status = 'removed') = (removed_at IS NOT NULL)),
  CONSTRAINT ck_staff_owner_stores CHECK (role <> 'owner' OR store_ids IS NULL),
  CONSTRAINT ck_staff_store_ids CHECK (store_ids IS NULL OR cardinality(store_ids) BETWEEN 1 AND 100),
  CONSTRAINT ck_staff_phone CHECK ((invite_phone_hash IS NULL) = (invite_phone_enc IS NULL)
    AND (invite_phone_hash IS NULL OR octet_length(invite_phone_hash) = 32))
);
CREATE UNIQUE INDEX ux_staff_business_user ON staff (business_id, user_id);
CREATE UNIQUE INDEX ux_staff_business_invite_phone ON staff (business_id, invite_phone_hash)
  WHERE status = 'invited' AND invite_phone_hash IS NOT NULL;
CREATE UNIQUE INDEX ux_staff_business_owner ON staff (business_id)
  WHERE role = 'owner' AND status <> 'removed';
CREATE INDEX ix_staff_user_id ON staff (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX ix_staff_invited_by ON staff (invited_by) WHERE invited_by IS NOT NULL;
CREATE INDEX ix_staff_invite_phone ON staff (invite_phone_hash) WHERE status = 'invited';
CREATE TRIGGER trg_staff_touch BEFORE UPDATE ON staff
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- leads: the site's "list your shop" form (03 §9, 04 §2.5). details_hash is
-- an HMAC of the normalised details, so a resubmission within a day is one
-- lead (D-103). created_at comes from the application clock.
------------------------------------------------------------------------------
CREATE TABLE leads (
  id           uuid PRIMARY KEY,
  name         text NOT NULL,
  phone_enc    bytea NOT NULL,
  shop_name    text NOT NULL,
  city         text NOT NULL,
  source       text NOT NULL,
  details_hash bytea NOT NULL,
  created_at   timestamptz NOT NULL,
  CONSTRAINT ck_leads_name CHECK (char_length(name) BETWEEN 1 AND 80),
  CONSTRAINT ck_leads_shop_name CHECK (char_length(shop_name) BETWEEN 1 AND 120),
  CONSTRAINT ck_leads_city CHECK (char_length(city) BETWEEN 1 AND 80),
  CONSTRAINT ck_leads_source CHECK (char_length(source) BETWEEN 1 AND 64),
  CONSTRAINT ck_leads_details_hash CHECK (octet_length(details_hash) = 32)
);
CREATE INDEX ix_leads_details_hash ON leads (details_hash, created_at DESC);
CREATE INDEX ix_leads_created_at ON leads (created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS leads;
DROP TABLE IF EXISTS staff;
DROP TABLE IF EXISTS business_documents;
DROP TABLE IF EXISTS stores;
DROP TABLE IF EXISTS business_kyc;
DROP TABLE IF EXISTS businesses;
