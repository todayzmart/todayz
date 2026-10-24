-- W2-07 offers (03 §9; 01 §5.5, §12; D-35, D-94).
--
-- offers: one row per offer. value is the whole percent for percent, paise per
-- unit for flat and paise off the bill for bill_min; buy_qty/get_qty are for
-- bxgy; min_bill_paise is for bill_min (compared inclusively with the item
-- subtotal, D-94). status is the shop's intent plus the schedule: `scheduled`
-- and `live` both price by their dates (internal/offers.Offer.Live), the
-- scheduling job only moves the label, so an offer expires on time even when
-- the job runs late. `title` is an optional custom name; the customer-facing
-- name is made from the terms when it is null.
--
-- redemptions counts the orders and sales that used the offer (used when the
-- order is placed, released on cancel or expiry, 01 §12). offer_redemptions has
-- one row per (offer, order or POS sale); order_id and pos_sale_id are plain
-- uuids until the Wave 2 constraints migration (D-35). released_at marks a
-- redemption given back; it no longer counts against any limit.

-- +goose Up

CREATE TABLE offers (
  id                 uuid PRIMARY KEY,
  store_id           uuid NOT NULL REFERENCES stores (id),
  title              text,
  kind               text NOT NULL,
  value              bigint NOT NULL DEFAULT 0,
  buy_qty            int,
  get_qty            int,
  min_bill_paise     bigint,
  scope              text NOT NULL DEFAULT 'store',
  store_item_ids     uuid[] NOT NULL DEFAULT '{}',
  category_id        uuid REFERENCES categories (id),
  channels           text NOT NULL DEFAULT 'both',
  per_customer_limit int,
  max_redemptions    int,
  redemptions        int NOT NULL DEFAULT 0,
  starts_at          timestamptz NOT NULL,
  ends_at            timestamptz,
  status             text NOT NULL DEFAULT 'live',
  source             text NOT NULL DEFAULT 'manual',
  batch_id           uuid REFERENCES stock_batches (id),
  version            int NOT NULL DEFAULT 1,
  created_by         uuid,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_offers_kind CHECK (kind IN ('percent', 'flat', 'bxgy', 'bill_min')),
  CONSTRAINT ck_offers_scope CHECK (scope IN ('store', 'items', 'category')),
  CONSTRAINT ck_offers_channels CHECK (channels IN ('online', 'pos', 'both')),
  CONSTRAINT ck_offers_status CHECK (status IN ('scheduled', 'live', 'paused', 'ended')),
  CONSTRAINT ck_offers_source CHECK (source IN ('manual', 'flash_expiry', 'coach_suggestion')),
  CONSTRAINT ck_offers_title CHECK (title IS NULL OR char_length(btrim(title)) BETWEEN 1 AND 40),
  CONSTRAINT ck_offers_dates CHECK (ends_at IS NULL OR ends_at > starts_at),
  CONSTRAINT ck_offers_limits CHECK (
    (per_customer_limit IS NULL OR per_customer_limit > 0)
    AND (max_redemptions IS NULL OR max_redemptions > 0)
    AND redemptions >= 0),
  CONSTRAINT ck_offers_terms CHECK (
    (kind = 'percent' AND value BETWEEN 1 AND 90 AND buy_qty IS NULL AND get_qty IS NULL AND min_bill_paise IS NULL)
    OR (kind = 'flat' AND value > 0 AND buy_qty IS NULL AND get_qty IS NULL AND min_bill_paise IS NULL)
    OR (kind = 'bxgy' AND value = 0 AND buy_qty BETWEEN 1 AND 20 AND get_qty BETWEEN 1 AND 20 AND min_bill_paise IS NULL)
    OR (kind = 'bill_min' AND value > 0 AND min_bill_paise > 0 AND buy_qty IS NULL AND get_qty IS NULL AND scope = 'store')),
  CONSTRAINT ck_offers_scope_terms CHECK (
    (scope = 'store' AND cardinality(store_item_ids) = 0 AND category_id IS NULL)
    OR (scope = 'items' AND cardinality(store_item_ids) BETWEEN 1 AND 50 AND category_id IS NULL)
    OR (scope = 'category' AND cardinality(store_item_ids) = 0 AND category_id IS NOT NULL)),
  CONSTRAINT ck_offers_batch CHECK (batch_id IS NULL OR source = 'flash_expiry')
);
-- The shop's list and the pricing read: a store's offers that are not over.
CREATE INDEX ix_offers_store_open ON offers (store_id, starts_at) WHERE status IN ('scheduled', 'live');
-- The scheduling job: what to start and what to end.
CREATE INDEX ix_offers_due ON offers (starts_at, ends_at) WHERE status IN ('scheduled', 'live');
CREATE INDEX ix_offers_store_item_ids ON offers USING gin (store_item_ids) WHERE status IN ('scheduled', 'live');
CREATE UNIQUE INDEX ux_offers_batch ON offers (batch_id) WHERE batch_id IS NOT NULL AND status <> 'ended';
CREATE TRIGGER trg_offers_touch BEFORE UPDATE ON offers
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE offer_redemptions (
  id            uuid PRIMARY KEY,
  offer_id      uuid NOT NULL REFERENCES offers (id),
  order_id      uuid,
  pos_sale_id   uuid,
  customer_id   uuid REFERENCES users (id),
  discount_paise bigint NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  released_at   timestamptz,
  CONSTRAINT ck_offer_redemptions_one_ref CHECK ((order_id IS NULL) <> (pos_sale_id IS NULL)),
  CONSTRAINT ck_offer_redemptions_discount CHECK (discount_paise > 0)
);
CREATE UNIQUE INDEX ux_offer_redemptions_order ON offer_redemptions (offer_id, order_id) WHERE order_id IS NOT NULL;
CREATE UNIQUE INDEX ux_offer_redemptions_pos_sale ON offer_redemptions (offer_id, pos_sale_id) WHERE pos_sale_id IS NOT NULL;
-- The per-customer limit: this customer's unreleased uses of an offer.
CREATE INDEX ix_offer_redemptions_customer ON offer_redemptions (customer_id, offer_id) WHERE released_at IS NULL AND customer_id IS NOT NULL;
CREATE INDEX ix_offer_redemptions_order ON offer_redemptions (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX ix_offer_redemptions_pos_sale ON offer_redemptions (pos_sale_id) WHERE pos_sale_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS offer_redemptions;
DROP TABLE IF EXISTS offers;
