-- W2-06 reviews and ratings (01 §10; 03 §9 reviews, review_replies, review_reports; D-41, D-59, D-60).
--
-- One review per order or per linked POS sale. order_id and pos_sale_id are plain
-- uuids (D-35): orders and pos_sales are other work packages' tables, and the
-- Wave 2 constraints migration adds the foreign keys. A review is published at once;
-- the customer edits it until edited_until (created + review_edit_window_days, stored at
-- creation so a later settings change never reopens or closes an old review).
--
-- Moderation is a soft delete: hidden_at/hidden_by/hide_reason, cleared by a restore.
-- Every hide and restore is also an audit_log row (written by the service in the same
-- transaction). A hidden review is out of the public list, the store's list and the rating.
-- A reply is hidden the same way (deleted_* columns).
--
-- tags: all positive or all negative (04 §2.4 "ReviewTagPositive[] | ReviewTagNegative[]").
-- stores.rating_avg and rating_count have one writer, this package (D-59).

-- +goose Up

CREATE TABLE reviews (
  id          uuid PRIMARY KEY,
  store_id    uuid NOT NULL REFERENCES stores (id),
  customer_id uuid NOT NULL REFERENCES users (id),
  order_id    uuid,
  pos_sale_id uuid,
  stars       smallint NOT NULL,
  text        text,
  tags        text[] NOT NULL DEFAULT '{}',
  edited_until timestamptz NOT NULL,
  edited_at   timestamptz,
  hidden_at   timestamptz,
  hidden_by   uuid,
  hide_reason text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_reviews_subject CHECK ((order_id IS NULL) <> (pos_sale_id IS NULL)),
  CONSTRAINT ck_reviews_stars CHECK (stars BETWEEN 1 AND 5),
  CONSTRAINT ck_reviews_text CHECK (text IS NULL OR char_length(text) BETWEEN 1 AND 1000),
  CONSTRAINT ck_reviews_tags CHECK (
    tags <@ ARRAY['fresh_stock','fair_price','quick_service','friendly','stock_was_right','well_packed',
                  'item_missing','slow','rude','wrong_item','expired_item','overpriced']::text[]
    AND cardinality(tags) <= 6
    AND NOT (tags && ARRAY['fresh_stock','fair_price','quick_service','friendly','stock_was_right','well_packed']::text[]
             AND tags && ARRAY['item_missing','slow','rude','wrong_item','expired_item','overpriced']::text[])),
  CONSTRAINT ck_reviews_hidden CHECK ((hidden_at IS NULL) = (hidden_by IS NULL) AND (hidden_at IS NULL) = (hide_reason IS NULL))
);
CREATE UNIQUE INDEX uq_reviews_order ON reviews (order_id) WHERE order_id IS NOT NULL;
CREATE UNIQUE INDEX uq_reviews_pos_sale ON reviews (pos_sale_id) WHERE pos_sale_id IS NOT NULL;
CREATE INDEX ix_reviews_store ON reviews (store_id, created_at DESC, id DESC) WHERE hidden_at IS NULL;
CREATE INDEX ix_reviews_customer ON reviews (customer_id, created_at DESC);
CREATE INDEX ix_reviews_hidden ON reviews (hidden_at DESC) WHERE hidden_at IS NOT NULL;
CREATE TRIGGER trg_reviews_touch BEFORE UPDATE ON reviews
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE review_replies (
  review_id      uuid PRIMARY KEY REFERENCES reviews (id) ON DELETE CASCADE,
  author_user_id uuid NOT NULL REFERENCES users (id),
  text           text NOT NULL,
  edited_at      timestamptz,
  deleted_at     timestamptz,
  deleted_by     uuid,
  delete_reason  text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_review_replies_text CHECK (char_length(text) BETWEEN 1 AND 1000),
  CONSTRAINT ck_review_replies_deleted CHECK ((deleted_at IS NULL) = (deleted_by IS NULL) AND (deleted_at IS NULL) = (delete_reason IS NULL))
);
CREATE TRIGGER trg_review_replies_touch BEFORE UPDATE ON review_replies
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- A report from a customer (reporter_store_id null) or from a shop (the store that was reviewed).
-- One report per reporter per review; sending it again returns the first.
CREATE TABLE review_reports (
  id                uuid PRIMARY KEY,
  review_id         uuid NOT NULL REFERENCES reviews (id) ON DELETE CASCADE,
  reporter_user_id  uuid NOT NULL REFERENCES users (id),
  reporter_store_id uuid REFERENCES stores (id),
  reason            text NOT NULL,
  status            text NOT NULL DEFAULT 'open',
  resolution        text,
  resolution_note   text,
  resolved_by       uuid,
  resolved_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_review_reports_reason CHECK (char_length(reason) BETWEEN 1 AND 500),
  CONSTRAINT ck_review_reports_status CHECK (status IN ('open','resolved')),
  CONSTRAINT ck_review_reports_resolution CHECK (resolution IS NULL OR resolution IN ('dismissed','upheld')),
  CONSTRAINT ck_review_reports_note CHECK (resolution_note IS NULL OR char_length(resolution_note) <= 500),
  CONSTRAINT ck_review_reports_resolved CHECK ((status = 'resolved') = (resolved_at IS NOT NULL AND resolved_by IS NOT NULL AND resolution IS NOT NULL))
);
CREATE UNIQUE INDEX uq_review_reports_reporter ON review_reports (review_id, reporter_user_id);
CREATE INDEX ix_review_reports_status ON review_reports (status, created_at DESC, id DESC);

-- +goose Down

DROP TABLE review_reports;
DROP TABLE review_replies;
DROP TABLE reviews;
