-- W2-04 point of sale and offline sync (03 §5; 01 §6, §7; 02 §4.3; 04 §2.6;
-- D-07, D-08, D-12, D-30, D-36, D-45, D-65, D-74, D-75, D-84, D-92, D-94).
-- pos_sales, pos_sale_lines, bill_claims, counter_code_checks,
-- counter_code_failures, counter_code_uses, receipt_tokens, sync_cursors and
-- sync_entries.
--
-- Foreign keys go to lower blocks only: users and devices (10101), stores
-- (10401), store_items and stock_movements (10501). reviews.pos_sale_id,
-- offer_redemptions.pos_sale_id and credit_holds.bill_claim_id point here from
-- later packages through the Wave 2 constraints migration (03 §13).
--
-- Additions to 03 §5, recorded in D-130:
--   * pos_sales.link_phone_hash / link_phone_enc: the phone a cashier typed
--     for a phone_unverified or counter_code link (PII_HASH_KEY / PII_ENC_KEY),
--     so the shop's customer list (W3-03) can show a walk-in without an
--     account. Never returned by a customer-facing or public API.
--   * pos_sales.bill_offer_discount_paise: the minimum-bill offer's amount, so
--     subtotal_paise = item_subtotal_paise − bill_offer_discount_paise.
--   * pos_sale_lines.offer_discount_paise, manual_discount_paise and
--     movement_id (the sale_pos movement a reversal puts back).
--   * counter_code_checks: the per-bill attempt counter (5 wrong tries lock
--     the bill, 01 §6) and the verified customer device a counter-code sale
--     links to.
--   * counter_code_failures: every wrong counter code, online or at sync,
--     for the per-phone and per-cashier locks (D-131).
--   * sync_entries: every outbox entry a sync applied, so a replayed entry
--     answers `duplicate` with what it made, even a count that wrote nothing.
--
-- Money on a sale (D-94, 01 §7), every amount in paise:
--   line_total          = unit × qty − offer discount (before manual discounts)
--   item_subtotal       = Σ line_total
--   subtotal            = item_subtotal − bill offer discount
--   discount            = Σ manual line discounts + manual bill discount
--   eligible            = subtotal − discount        (credit rules, cashback)
--   total               = eligible − credits × 100 + round_off, round_off −49…+50

-- +goose Up

------------------------------------------------------------------------------
-- pos_sales: one row per bill. Idempotent by (store_id, client_id), the
-- device's ULID for the bill. A reversal marks the row (D-74); there is no
-- second row. sold_at is the device's time, received_at the server's.
------------------------------------------------------------------------------
CREATE TABLE pos_sales (
  id                        uuid PRIMARY KEY,
  store_id                  uuid NOT NULL,
  client_id                 uuid NOT NULL,
  device_id                 uuid NOT NULL,
  device_seq                bigint,
  cashier_user_id           uuid NOT NULL,
  customer_id               uuid,
  link_method               text,
  link_status               text,
  offline_link_code         text,
  offline_link_at           timestamptz,
  device_clock_skew_ms      int,
  link_phone_hash           bytea,
  link_phone_enc            bytea,
  item_subtotal_paise       bigint NOT NULL,
  bill_offer_discount_paise bigint NOT NULL DEFAULT 0,
  subtotal_paise            bigint NOT NULL,
  discount_paise            bigint NOT NULL DEFAULT 0,
  round_off_paise           int NOT NULL DEFAULT 0,
  credits_used              int NOT NULL DEFAULT 0,
  cashback_credits          int NOT NULL DEFAULT 0,
  total_paise               bigint NOT NULL,
  payment_method            text NOT NULL,
  status                    text NOT NULL DEFAULT 'completed',
  reversed_at               timestamptz,
  reversed_by               uuid,
  reversal_reason           text,
  sold_at                   timestamptz NOT NULL,
  received_at               timestamptz NOT NULL,
  created_at                timestamptz NOT NULL DEFAULT now(),
  updated_at                timestamptz NOT NULL DEFAULT now(),
  version                   int NOT NULL DEFAULT 1,
  CONSTRAINT fk_pos_sales_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT fk_pos_sales_cashier FOREIGN KEY (cashier_user_id) REFERENCES users (id),
  CONSTRAINT fk_pos_sales_customer FOREIGN KEY (customer_id) REFERENCES users (id),
  CONSTRAINT fk_pos_sales_reversed_by FOREIGN KEY (reversed_by) REFERENCES users (id),
  CONSTRAINT ck_pos_sales_device_seq CHECK (device_seq IS NULL OR device_seq > 0),
  CONSTRAINT ck_pos_sales_link_method CHECK (link_method IS NULL
    OR link_method IN ('bill_qr','counter_code','phone_unverified')),
  CONSTRAINT ck_pos_sales_link_status CHECK (
    (link_method IS NULL AND link_status IS NULL)
    OR (link_method IS NOT NULL AND link_status IN ('linked','pending_verification','failed'))),
  -- A linked sale names its customer; phone_unverified may not (a walk-in
  -- without an account); a failed link never does.
  CONSTRAINT ck_pos_sales_link_customer CHECK (
    (link_status IS DISTINCT FROM 'linked' OR link_method = 'phone_unverified' OR customer_id IS NOT NULL)
    AND (link_status IS DISTINCT FROM 'failed' OR customer_id IS NULL)
    AND (link_method IS NOT NULL OR customer_id IS NULL)),
  CONSTRAINT ck_pos_sales_offline_code CHECK (offline_link_code IS NULL OR offline_link_code ~ '^[0-9]{4}$'),
  CONSTRAINT ck_pos_sales_phone CHECK ((link_phone_hash IS NULL) = (link_phone_enc IS NULL)
    AND (link_phone_hash IS NULL OR octet_length(link_phone_hash) = 32)),
  CONSTRAINT ck_pos_sales_money CHECK (item_subtotal_paise >= 0 AND bill_offer_discount_paise >= 0
    AND subtotal_paise = item_subtotal_paise - bill_offer_discount_paise AND subtotal_paise >= 0
    AND discount_paise >= 0 AND discount_paise <= subtotal_paise
    AND credits_used >= 0 AND cashback_credits >= 0
    AND round_off_paise BETWEEN -49 AND 50
    AND total_paise = subtotal_paise - discount_paise - credits_used::bigint * 100 + round_off_paise
    AND total_paise >= 0),
  -- D-07, D-30: credits are spent only through a bill QR, and never take the
  -- bill below ₹1.
  CONSTRAINT ck_pos_sales_credits_link CHECK (credits_used = 0 OR (link_method = 'bill_qr'
    AND link_status = 'linked' AND subtotal_paise - discount_paise - credits_used::bigint * 100 >= 100)),
  -- Cashback and ledger effects only on bill_qr and counter_code (D-07).
  CONSTRAINT ck_pos_sales_cashback_link CHECK (cashback_credits = 0
    OR (link_method IN ('bill_qr','counter_code') AND link_status = 'linked')),
  CONSTRAINT ck_pos_sales_payment CHECK (payment_method IN ('cash','upi','card','other')),
  CONSTRAINT ck_pos_sales_status CHECK (status IN ('completed','reversed')),
  CONSTRAINT ck_pos_sales_reversed CHECK ((status = 'reversed') = (reversed_at IS NOT NULL)
    AND (reversed_at IS NULL) = (reversed_by IS NULL)
    AND (reversed_at IS NULL) = (reversal_reason IS NULL)),
  CONSTRAINT ck_pos_sales_reversal_reason CHECK (reversal_reason IS NULL OR char_length(reversal_reason) BETWEEN 1 AND 200)
);
CREATE UNIQUE INDEX ux_pos_sales_store_client ON pos_sales (store_id, client_id);
CREATE INDEX ix_pos_sales_store_sold ON pos_sales (store_id, sold_at DESC);
-- /v1/me/purchases (D-45, D-75): never phone_unverified.
CREATE INDEX ix_pos_sales_customer_sold ON pos_sales (customer_id, sold_at DESC)
  WHERE link_method IN ('bill_qr','counter_code');
-- D-12 risk count: offline counter-code links per store per IST day.
CREATE INDEX ix_pos_sales_offline_links ON pos_sales (store_id, received_at)
  WHERE offline_link_at IS NOT NULL;
CREATE INDEX ix_pos_sales_cashier ON pos_sales (cashier_user_id);
CREATE INDEX ix_pos_sales_reversed_by ON pos_sales (reversed_by) WHERE reversed_by IS NOT NULL;
CREATE INDEX ix_pos_sales_phone ON pos_sales (store_id, link_phone_hash) WHERE link_phone_hash IS NOT NULL;
CREATE TRIGGER trg_pos_sales_touch BEFORE UPDATE ON pos_sales
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
REVOKE DELETE, TRUNCATE ON pos_sales FROM todayz_app;
CREATE TRIGGER trg_pos_sales_no_truncate BEFORE TRUNCATE ON pos_sales
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

------------------------------------------------------------------------------
-- pos_sale_lines: the lines as sold. client_id is the device's line ULID and
-- the sale_pos movement's client_id. A custom line has no store item and no
-- movement. Sold prices are never rewritten (02 §4.3).
------------------------------------------------------------------------------
CREATE TABLE pos_sale_lines (
  id                    uuid PRIMARY KEY,
  sale_id               uuid NOT NULL,
  client_id             uuid NOT NULL,
  store_item_id         uuid,
  name_snapshot         text NOT NULL,
  unit_price_paise      bigint NOT NULL,
  quantity              int NOT NULL,
  offer_discount_paise  bigint NOT NULL DEFAULT 0,
  manual_discount_paise bigint NOT NULL DEFAULT 0,
  line_total_paise      bigint NOT NULL,
  movement_id           uuid,
  created_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_pos_sale_lines_sale_id FOREIGN KEY (sale_id) REFERENCES pos_sales (id),
  CONSTRAINT fk_pos_sale_lines_store_item_id FOREIGN KEY (store_item_id) REFERENCES store_items (id),
  CONSTRAINT fk_pos_sale_lines_movement_id FOREIGN KEY (movement_id) REFERENCES stock_movements (id),
  CONSTRAINT ck_pos_sale_lines_name CHECK (char_length(name_snapshot) BETWEEN 1 AND 120),
  CONSTRAINT ck_pos_sale_lines_price CHECK (unit_price_paise > 0),
  CONSTRAINT ck_pos_sale_lines_quantity CHECK (quantity > 0),
  CONSTRAINT ck_pos_sale_lines_money CHECK (offer_discount_paise >= 0 AND manual_discount_paise >= 0
    AND line_total_paise = unit_price_paise * quantity - offer_discount_paise
    AND line_total_paise >= 0 AND manual_discount_paise <= line_total_paise),
  CONSTRAINT ck_pos_sale_lines_movement CHECK (movement_id IS NULL OR store_item_id IS NOT NULL)
);
CREATE UNIQUE INDEX ux_pos_sale_lines_sale_client ON pos_sale_lines (sale_id, client_id);
CREATE INDEX ix_pos_sale_lines_store_item_id ON pos_sale_lines (store_item_id) WHERE store_item_id IS NOT NULL;
CREATE INDEX ix_pos_sale_lines_movement_id ON pos_sale_lines (movement_id) WHERE movement_id IS NOT NULL;
REVOKE UPDATE, DELETE, TRUNCATE ON pos_sale_lines FROM todayz_app;
CREATE TRIGGER trg_pos_sale_lines_no_truncate BEFORE TRUNCATE ON pos_sale_lines
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

------------------------------------------------------------------------------
-- bill_claims: the bill QR (01 §6). The token is signed with
-- BILL_QR_SIGNING_KEY over (claim id, store, amount, expiry, nonce); only the
-- nonce is stored. A claim is made for the sale the POS is about to ring
-- (sale_client_id, amount = the bill after discounts) and is bound to the
-- sale when it is rung. One claim per bill: the first confirm wins, a new
-- claim for the bill cancels the earlier ones, and a claim not used by
-- expires_at expires (D-131; the hold of a cancelled or expired claim is
-- released).
------------------------------------------------------------------------------
CREATE TABLE bill_claims (
  id                uuid PRIMARY KEY,
  store_id          uuid NOT NULL,
  sale_client_id    uuid NOT NULL,
  pos_sale_id       uuid,
  amount_paise      bigint NOT NULL,
  token_nonce       bytea NOT NULL,
  expires_at        timestamptz NOT NULL,
  created_by        uuid NOT NULL,
  claimed_by        uuid,
  claimed_at        timestamptz,
  credits_requested int NOT NULL DEFAULT 0,
  credits_used      int NOT NULL DEFAULT 0,
  status            text NOT NULL DEFAULT 'open',
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  version           int NOT NULL DEFAULT 1,
  CONSTRAINT fk_bill_claims_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT fk_bill_claims_pos_sale_id FOREIGN KEY (pos_sale_id) REFERENCES pos_sales (id),
  CONSTRAINT fk_bill_claims_created_by FOREIGN KEY (created_by) REFERENCES users (id),
  CONSTRAINT fk_bill_claims_claimed_by FOREIGN KEY (claimed_by) REFERENCES users (id),
  CONSTRAINT ck_bill_claims_amount CHECK (amount_paise > 0),
  CONSTRAINT ck_bill_claims_nonce CHECK (octet_length(token_nonce) = 16),
  CONSTRAINT ck_bill_claims_status CHECK (status IN ('open','claimed','expired','cancelled')),
  -- An open claim has no customer; a claimed or used one has. A cancelled or
  -- expired claim keeps the customer who had confirmed it, if any.
  CONSTRAINT ck_bill_claims_claimed CHECK ((status <> 'open' OR claimed_by IS NULL)
    AND ((status <> 'claimed' AND pos_sale_id IS NULL) OR claimed_by IS NOT NULL)
    AND (pos_sale_id IS NULL OR status = 'claimed')
    AND (claimed_by IS NULL) = (claimed_at IS NULL)),
  CONSTRAINT ck_bill_claims_credits CHECK (credits_requested >= 0 AND credits_used >= 0
    AND credits_used <= credits_requested AND (claimed_by IS NOT NULL OR credits_requested = 0))
);
CREATE INDEX ix_bill_claims_store_sale_client ON bill_claims (store_id, sale_client_id);
CREATE UNIQUE INDEX ux_bill_claims_pos_sale ON bill_claims (pos_sale_id) WHERE pos_sale_id IS NOT NULL;
CREATE INDEX ix_bill_claims_open_expires ON bill_claims (expires_at) WHERE status IN ('open','claimed') AND pos_sale_id IS NULL;
CREATE INDEX ix_bill_claims_created_by ON bill_claims (created_by);
CREATE INDEX ix_bill_claims_claimed_by ON bill_claims (claimed_by) WHERE claimed_by IS NOT NULL;
CREATE TRIGGER trg_bill_claims_touch BEFORE UPDATE ON bill_claims
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
REVOKE DELETE, TRUNCATE ON bill_claims FROM todayz_app;
CREATE TRIGGER trg_bill_claims_no_truncate BEFORE TRUNCATE ON bill_claims
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

------------------------------------------------------------------------------
-- counter_code_checks: one per bill a cashier typed a counter code for
-- (POST {S}/counter-code/verify). 5 wrong tries lock that bill (01 §6). A
-- right code records the customer and the device whose code matched; the
-- sale with that client id links to them.
------------------------------------------------------------------------------
CREATE TABLE counter_code_checks (
  store_id           uuid NOT NULL,
  sale_client_id     uuid NOT NULL,
  attempts           smallint NOT NULL DEFAULT 0,
  locked_at          timestamptz,
  customer_id        uuid,
  customer_device_id uuid,
  time_step          bigint,
  verified_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pk_counter_code_checks PRIMARY KEY (store_id, sale_client_id),
  CONSTRAINT fk_counter_code_checks_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT fk_counter_code_checks_customer FOREIGN KEY (customer_id) REFERENCES users (id),
  CONSTRAINT fk_counter_code_checks_device FOREIGN KEY (customer_device_id) REFERENCES devices (id),
  CONSTRAINT ck_counter_code_checks_attempts CHECK (attempts >= 0),
  CONSTRAINT ck_counter_code_checks_verified CHECK ((verified_at IS NULL) = (customer_id IS NULL)
    AND (verified_at IS NULL) = (customer_device_id IS NULL)
    AND (verified_at IS NULL) = (time_step IS NULL))
);
CREATE INDEX ix_counter_code_checks_customer ON counter_code_checks (customer_id) WHERE customer_id IS NOT NULL;
CREATE INDEX ix_counter_code_checks_device ON counter_code_checks (customer_device_id) WHERE customer_device_id IS NOT NULL;
CREATE TRIGGER trg_counter_code_checks_touch BEFORE UPDATE ON counter_code_checks
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
REVOKE DELETE, TRUNCATE ON counter_code_checks FROM todayz_app;
CREATE TRIGGER trg_counter_code_checks_no_truncate BEFORE TRUNCATE ON counter_code_checks
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

------------------------------------------------------------------------------
-- counter_code_failures (D-131): one row per wrong counter code, online or
-- at sync, at the server's time. 5 in 15 minutes for a phone at a store lock
-- that phone there for 15 minutes; 20 in an hour by a cashier (any store)
-- lock their code checks for an hour. The failure that reaches a limit
-- carries the lock's end, so a lock is "a row whose *_locked_until is still
-- ahead". phone_hash is NULL for a phone that is not a phone number.
------------------------------------------------------------------------------
CREATE TABLE counter_code_failures (
  id                   uuid PRIMARY KEY,
  store_id             uuid NOT NULL,
  cashier_user_id      uuid NOT NULL,
  phone_hash           bytea,
  source               text NOT NULL,
  phone_locked_until   timestamptz,
  cashier_locked_until timestamptz,
  failed_at            timestamptz NOT NULL,
  CONSTRAINT fk_counter_code_failures_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT fk_counter_code_failures_cashier FOREIGN KEY (cashier_user_id) REFERENCES users (id),
  CONSTRAINT ck_counter_code_failures_phone CHECK (phone_hash IS NULL OR octet_length(phone_hash) = 32),
  CONSTRAINT ck_counter_code_failures_source CHECK (source IN ('online', 'sync')),
  CONSTRAINT ck_counter_code_failures_locks CHECK ((phone_locked_until IS NULL OR (phone_hash IS NOT NULL AND phone_locked_until > failed_at))
    AND (cashier_locked_until IS NULL OR cashier_locked_until > failed_at))
);
CREATE INDEX ix_counter_code_failures_phone ON counter_code_failures (store_id, phone_hash, failed_at)
  WHERE phone_hash IS NOT NULL;
CREATE INDEX ix_counter_code_failures_cashier ON counter_code_failures (cashier_user_id, failed_at);
REVOKE UPDATE, DELETE, TRUNCATE ON counter_code_failures FROM todayz_app;
CREATE TRIGGER trg_counter_code_failures_no_truncate BEFORE TRUNCATE ON counter_code_failures
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

------------------------------------------------------------------------------
-- counter_code_uses (D-12): a counter code (device, 60 s step) is accepted
-- once, online or at sync. pos_sale_id is set when the sale is rung.
------------------------------------------------------------------------------
CREATE TABLE counter_code_uses (
  customer_device_id uuid NOT NULL,
  time_step          bigint NOT NULL,
  store_id           uuid NOT NULL,
  pos_sale_id        uuid,
  created_at         timestamptz NOT NULL,
  CONSTRAINT pk_counter_code_uses PRIMARY KEY (customer_device_id, time_step),
  CONSTRAINT fk_counter_code_uses_device FOREIGN KEY (customer_device_id) REFERENCES devices (id),
  CONSTRAINT fk_counter_code_uses_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT fk_counter_code_uses_pos_sale_id FOREIGN KEY (pos_sale_id) REFERENCES pos_sales (id),
  CONSTRAINT ck_counter_code_uses_step CHECK (time_step > 0)
);
CREATE INDEX ix_counter_code_uses_store_id ON counter_code_uses (store_id);
CREATE INDEX ix_counter_code_uses_pos_sale_id ON counter_code_uses (pos_sale_id) WHERE pos_sale_id IS NOT NULL;
REVOKE DELETE, TRUNCATE ON counter_code_uses FROM todayz_app;
CREATE TRIGGER trg_counter_code_uses_no_truncate BEFORE TRUNCATE ON counter_code_uses
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

------------------------------------------------------------------------------
-- receipt_tokens (D-45): a 128-bit random token per sale, stored as its
-- SHA-256. GET /v1/public/receipts/{token} shows items, totals and the store
-- for 30 days.
------------------------------------------------------------------------------
CREATE TABLE receipt_tokens (
  id          uuid PRIMARY KEY,
  token_hash  bytea NOT NULL,
  pos_sale_id uuid NOT NULL,
  expires_at  timestamptz NOT NULL,
  created_at  timestamptz NOT NULL,
  CONSTRAINT fk_receipt_tokens_pos_sale_id FOREIGN KEY (pos_sale_id) REFERENCES pos_sales (id),
  CONSTRAINT ck_receipt_tokens_hash CHECK (octet_length(token_hash) = 32),
  CONSTRAINT ck_receipt_tokens_expiry CHECK (expires_at > created_at)
);
CREATE UNIQUE INDEX ux_receipt_tokens_hash ON receipt_tokens (token_hash);
CREATE UNIQUE INDEX ux_receipt_tokens_pos_sale ON receipt_tokens (pos_sale_id);
REVOKE UPDATE, DELETE, TRUNCATE ON receipt_tokens FROM todayz_app;
CREATE TRIGGER trg_receipt_tokens_no_truncate BEFORE TRUNCATE ON receipt_tokens
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

------------------------------------------------------------------------------
-- sync_cursors (D-36): each POS device's catalogue cursor per store.
-- device_id is the signed-in device (sessions.device_id), or the session
-- family when the session has no device row.
------------------------------------------------------------------------------
CREATE TABLE sync_cursors (
  device_id          uuid NOT NULL,
  store_id           uuid NOT NULL,
  last_catalogue_seq bigint NOT NULL DEFAULT 0,
  last_synced_at     timestamptz NOT NULL,
  CONSTRAINT pk_sync_cursors PRIMARY KEY (device_id, store_id),
  CONSTRAINT fk_sync_cursors_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT ck_sync_cursors_seq CHECK (last_catalogue_seq >= 0)
);
CREATE INDEX ix_sync_cursors_store_id ON sync_cursors (store_id);

------------------------------------------------------------------------------
-- sync_entries (02 §4.3): each outbox entry a sync applied, by the device's
-- client id. A replay (the same client id again) answers `duplicate` with
-- the sale or movement it made; a count equal to on hand writes no movement,
-- so this row is what stops a replayed count from correcting stock twice.
-- Rejected entries are not stored: they can be sent again.
------------------------------------------------------------------------------
CREATE TABLE sync_entries (
  store_id    uuid NOT NULL,
  client_id   uuid NOT NULL,
  device_id   uuid NOT NULL,
  device_seq  bigint NOT NULL,
  kind        text NOT NULL,
  pos_sale_id uuid,
  movement_id uuid,
  applied_at  timestamptz NOT NULL,
  CONSTRAINT pk_sync_entries PRIMARY KEY (store_id, client_id),
  CONSTRAINT fk_sync_entries_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT fk_sync_entries_pos_sale_id FOREIGN KEY (pos_sale_id) REFERENCES pos_sales (id),
  CONSTRAINT fk_sync_entries_movement_id FOREIGN KEY (movement_id) REFERENCES stock_movements (id),
  CONSTRAINT ck_sync_entries_kind CHECK (kind IN ('sale', 'count', 'restock', 'damage', 'expired')),
  CONSTRAINT ck_sync_entries_seq CHECK (device_seq >= 1),
  CONSTRAINT ck_sync_entries_sale CHECK ((kind = 'sale') = (pos_sale_id IS NOT NULL))
);
CREATE INDEX ix_sync_entries_pos_sale_id ON sync_entries (pos_sale_id) WHERE pos_sale_id IS NOT NULL;
REVOKE UPDATE, DELETE, TRUNCATE ON sync_entries FROM todayz_app;
CREATE TRIGGER trg_sync_entries_no_truncate BEFORE TRUNCATE ON sync_entries
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

-- +goose Down
DROP TABLE IF EXISTS sync_entries;
DROP TABLE IF EXISTS sync_cursors;
DROP TABLE IF EXISTS receipt_tokens;
DROP TABLE IF EXISTS counter_code_uses;
DROP TABLE IF EXISTS counter_code_failures;
DROP TABLE IF EXISTS counter_code_checks;
DROP TABLE IF EXISTS bill_claims;
DROP TABLE IF EXISTS pos_sale_lines;
DROP TABLE IF EXISTS pos_sales;
