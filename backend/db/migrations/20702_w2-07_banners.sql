-- W2-07 banners (03 §9; 01 §12; D-46, D-77).
--
-- store_banners: a shop's banner. Either an uploaded image (image_key, a key in
-- the images bucket) or a template (template jsonb: tint, subline, product
-- image key); the headline (at most 40 characters) is always on the row. The
-- target says where a tap goes: an offer, a set of items, a category in the
-- shop or the shop page (target_id is the offer id, the category id, or null;
-- for `items` the ids are in target_item_ids).
--
-- status: pending_review (an upload the safety check could not clear, held
-- for the admin queue), live, scheduled (starts in the future), paused, ended,
-- removed (taken down by an admin, with removed_by and remove_reason).
-- moderation: auto_ok, needs_review, rejected. Only `live` banners whose
-- moderation is auto_ok show anywhere. The shop may have store_banners_max
-- live-or-scheduled banners; the count is checked in Go under the store's row
-- lock.
--
-- banner_daily is filled by the W3-07 rollup from the banner_impression and
-- banner_tapped events (D-77); W2-07 creates the table so the shop's analytics
-- and the rollup agree on one shape. banner_taps keeps the last tap per
-- (customer, banner) so the order attribution (same customer, same store,
-- within 24 h, last click wins) has something to read.

-- +goose Up

CREATE TABLE store_banners (
  id              uuid PRIMARY KEY,
  store_id        uuid NOT NULL REFERENCES stores (id),
  image_key       text,
  template        jsonb,
  headline        text NOT NULL,
  target_type     text NOT NULL,
  target_id       uuid,
  target_item_ids uuid[] NOT NULL DEFAULT '{}',
  starts_at       timestamptz NOT NULL,
  ends_at         timestamptz,
  status          text NOT NULL,
  moderation      text NOT NULL,
  removed_by      uuid,
  removed_at      timestamptz,
  remove_reason   text,
  version         int NOT NULL DEFAULT 1,
  created_by      uuid,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_store_banners_headline CHECK (char_length(btrim(headline)) BETWEEN 1 AND 40),
  CONSTRAINT ck_store_banners_image CHECK ((image_key IS NULL) <> (template IS NULL)),
  CONSTRAINT ck_store_banners_template CHECK (template IS NULL OR (jsonb_typeof(template) = 'object' AND octet_length(template::text) <= 2048)),
  CONSTRAINT ck_store_banners_target CHECK (
    (target_type = 'store' AND target_id IS NULL AND cardinality(target_item_ids) = 0)
    OR (target_type IN ('offer', 'category') AND target_id IS NOT NULL AND cardinality(target_item_ids) = 0)
    OR (target_type = 'items' AND target_id IS NULL AND cardinality(target_item_ids) BETWEEN 1 AND 20)),
  CONSTRAINT ck_store_banners_status CHECK (status IN ('pending_review', 'live', 'scheduled', 'paused', 'ended', 'removed')),
  CONSTRAINT ck_store_banners_moderation CHECK (moderation IN ('auto_ok', 'needs_review', 'rejected')),
  CONSTRAINT ck_store_banners_dates CHECK (ends_at IS NULL OR ends_at > starts_at),
  CONSTRAINT ck_store_banners_removed CHECK ((status = 'removed') = (removed_at IS NOT NULL))
);
CREATE INDEX ix_store_banners_store ON store_banners (store_id, created_at DESC);
CREATE INDEX ix_store_banners_active ON store_banners (store_id) WHERE status IN ('live', 'scheduled');
CREATE INDEX ix_store_banners_due ON store_banners (starts_at, ends_at) WHERE status IN ('live', 'scheduled');
CREATE INDEX ix_store_banners_review ON store_banners (created_at) WHERE moderation = 'needs_review';
CREATE TRIGGER trg_store_banners_touch BEFORE UPDATE ON store_banners
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE banner_daily (
  banner_id         uuid NOT NULL REFERENCES store_banners (id),
  date              date NOT NULL,
  impressions       bigint NOT NULL DEFAULT 0,
  clicks            bigint NOT NULL DEFAULT 0,
  attributed_orders bigint NOT NULL DEFAULT 0,
  CONSTRAINT pk_banner_daily PRIMARY KEY (banner_id, date),
  CONSTRAINT ck_banner_daily_counts CHECK (impressions >= 0 AND clicks >= 0 AND attributed_orders >= 0)
);

CREATE TABLE banner_taps (
  customer_id uuid NOT NULL REFERENCES users (id),
  banner_id   uuid NOT NULL REFERENCES store_banners (id),
  store_id    uuid NOT NULL REFERENCES stores (id),
  tapped_at   timestamptz NOT NULL,
  CONSTRAINT pk_banner_taps PRIMARY KEY (customer_id, banner_id)
);
-- Attribution reads a customer's recent taps at one store, newest first.
CREATE INDEX ix_banner_taps_customer_store ON banner_taps (customer_id, store_id, tapped_at DESC);

-- +goose Down
DROP TABLE IF EXISTS banner_taps;
DROP TABLE IF EXISTS banner_daily;
DROP TABLE IF EXISTS store_banners;
