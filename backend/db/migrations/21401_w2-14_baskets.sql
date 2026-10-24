-- W2-14 saved baskets (D-110; 01 §11.4; 03 baskets, basket_lines).
--
-- A basket is private to its customer: every query filters on customer_id
-- from the session, and another customer's basket reads as 404. DELETE is a
-- soft delete (deleted_at); a deleted basket is hidden from every read and
-- from the basket_max_saved count. The account-deletion hook deletes the rows
-- (lines go by cascade). The limits basket_max_saved and basket_max_items are
-- checked in Go in the write transaction, not here.
--
-- last_split is the split the last basket search suggested, kept until the
-- basket is searched again (01 §11.4). Shape (internal/baskets.Split):
--   {"stores": [{"storeId", "storeSlug", "storeName", "lineIds": [...]}, ...],
--    "uncoveredLineIds": [...], "searchedAt": "<RFC 3339>"}
-- Line ids, not positions, so a reorder keeps it valid; PATCH that replaces
-- the lines without sending lastSplit clears it.

-- +goose Up

CREATE TABLE baskets (
  id          uuid PRIMARY KEY,
  customer_id uuid NOT NULL REFERENCES users (id),
  name        text NOT NULL,
  version     int NOT NULL DEFAULT 1,
  last_split  jsonb,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz,
  CONSTRAINT ck_baskets_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 40),
  CONSTRAINT ck_baskets_last_split CHECK (last_split IS NULL OR (jsonb_typeof(last_split) = 'object' AND octet_length(last_split::text) <= 8192))
);
CREATE INDEX ix_baskets_customer ON baskets (customer_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_baskets_touch BEFORE UPDATE ON baskets
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE basket_lines (
  id         uuid PRIMARY KEY,
  basket_id  uuid NOT NULL REFERENCES baskets (id) ON DELETE CASCADE,
  product_id uuid REFERENCES products (id),
  text       text,
  quantity   int NOT NULL,
  position   int NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_basket_lines_one_of CHECK ((product_id IS NULL) <> (text IS NULL)),
  CONSTRAINT ck_basket_lines_text CHECK (text IS NULL OR char_length(text) BETWEEN 1 AND 120),
  CONSTRAINT ck_basket_lines_quantity CHECK (quantity BETWEEN 1 AND 99),
  CONSTRAINT ck_basket_lines_position CHECK (position >= 0),
  CONSTRAINT uq_basket_lines_position UNIQUE (basket_id, position)
);
CREATE INDEX ix_basket_lines_product ON basket_lines (product_id) WHERE product_id IS NOT NULL;

-- Basket searches are logged as searches (01 §11.4): one row per line, source 'basket'.
ALTER TABLE search_log DROP CONSTRAINT ck_search_log_source;
ALTER TABLE search_log ADD CONSTRAINT ck_search_log_source CHECK (source IN ('typed', 'voice', 'basket'));

-- 01 §17 (D-110). TestSettingsMatchProductRules reads these inserts (D-95).
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('basket_max_saved', '10', 'count', 1, 50, 'Baskets. Saved baskets one customer can keep (422 basket_limit_reached beyond)'),
  ('basket_max_items', '30', 'count', 5, 100, 'Baskets. Lines in one basket (422 validation_failed on lines beyond)'),
  ('basket_split_max_shops', '2', 'count', 1, 2, 'Baskets. Stores in a suggested split when no store has everything; 1 turns splits off'),
  ('basket_results_max', '10', 'count', 3, 30, 'Baskets. Stores shown in basket search results');

-- +goose Down
DELETE FROM platform_settings WHERE key IN (
  'basket_max_saved',
  'basket_max_items',
  'basket_split_max_shops',
  'basket_results_max');
UPDATE search_log SET source = 'typed' WHERE source = 'basket';
ALTER TABLE search_log DROP CONSTRAINT ck_search_log_source;
ALTER TABLE search_log ADD CONSTRAINT ck_search_log_source CHECK (source IN ('typed', 'voice'));
DROP TABLE IF EXISTS basket_lines;
DROP TABLE IF EXISTS baskets;
