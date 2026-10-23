-- W1-05 catalogue and stock (03 §3, §11; 01 §4; 02 §3.3; 04 §2.6; D-01, D-34,
-- D-35, D-36, D-37, D-84, D-99).
-- categories (seeded below), brands, products (the master catalogue),
-- store_items, stock_batches, stock_movements (the ledger),
-- stock_movement_batches (the per-batch allocation) and catalogue_changes.
--
-- Plain uuid columns without a foreign key yet (D-35, 03 §13), each with its
-- index: products.created_by_store_id, store_items.store_id,
-- stock_movements.store_id, catalogue_changes.store_id (stores, Wave 1
-- constraints migration), stock_movements.actor_user_id (users) and
-- stock_movements.device_id (devices). businesses.category_id → categories is
-- also the Wave 1 constraints migration's (03 §13); this file does not add it.
--
-- Not here (deferred in the weekend slice, W1-05 report): product_review_queue
-- and the import tables.
--
-- Stock (03 stock rules 1 and 8): store_items.on_hand is a cache of
-- Σ stock_movements.delta, kept by trg_stock_movements_apply; a batch's
-- quantity is a cache of Σ stock_movement_batches.delta for that batch, kept
-- by trg_stock_movement_batches_apply (a batch is inserted at 0 and its first
-- allocation row brings it to its starting quantity). Both ledgers are
-- append-only. store_items.reserved is kept by W2-02's trigger on
-- stock_reservations (rule 2); nothing here writes it.

-- +goose Up

------------------------------------------------------------------------------
-- categories: a two-level tree, seeded below. names holds en, hi, kn, te, ta.
-- tint is one of the six design tints (design: "never invent a new hue"),
-- icon a Lucide icon name.
------------------------------------------------------------------------------
CREATE TABLE categories (
  id         uuid PRIMARY KEY,
  parent_id  uuid,
  slug       text NOT NULL,
  names      jsonb NOT NULL,
  tint       text NOT NULL,
  icon       text NOT NULL,
  sort       int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_categories_parent_id FOREIGN KEY (parent_id) REFERENCES categories (id),
  CONSTRAINT ck_categories_parent CHECK (parent_id IS NULL OR parent_id <> id),
  CONSTRAINT ck_categories_slug CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 64),
  CONSTRAINT ck_categories_names CHECK (jsonb_typeof(names) = 'object' AND jsonb_typeof(names -> 'en') = 'string'),
  CONSTRAINT ck_categories_tint CHECK (tint IN ('green','amber','rose','sky','lilac','peach')),
  CONSTRAINT ck_categories_icon CHECK (icon ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(icon) <= 40)
);
CREATE UNIQUE INDEX ux_categories_slug ON categories (slug);
CREATE INDEX ix_categories_parent_id ON categories (parent_id, sort) WHERE parent_id IS NOT NULL;
CREATE TRIGGER trg_categories_touch BEFORE UPDATE ON categories
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
COMMENT ON COLUMN categories.names IS
  'Display name per language (en, hi, kn, te, ta). The en names are final; hi, kn, te and ta were seeded as drafts by W1-05 and wait for a native speaker''s review.';

------------------------------------------------------------------------------
-- brands
------------------------------------------------------------------------------
CREATE TABLE brands (
  id         uuid PRIMARY KEY,
  name       text NOT NULL,
  slug       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_brands_name CHECK (char_length(name) BETWEEN 1 AND 80),
  CONSTRAINT ck_brands_slug CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 80)
);
CREATE UNIQUE INDEX ux_brands_slug ON brands (slug);
CREATE TRIGGER trg_brands_touch BEFORE UPDATE ON brands
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- products: the shared master catalogue (01 §5, D-37). search is generated
-- from the product's own columns ('simple' config: names are brand and
-- product words, often transliterated, so no stemming); the brand's name
-- cannot be in a generated column and is added to store_items.search.
------------------------------------------------------------------------------
CREATE TABLE products (
  id                  uuid PRIMARY KEY,
  name                text NOT NULL,
  brand_id            uuid,
  category_id         uuid NOT NULL,
  variant             text,
  pack_size           numeric(10,3) NOT NULL,
  unit                text NOT NULL,
  mrp_paise           bigint,
  image_key           text,
  barcodes            text[] NOT NULL DEFAULT '{}',
  search              tsvector GENERATED ALWAYS AS (
                        setweight(to_tsvector('simple'::regconfig, name), 'A') ||
                        setweight(to_tsvector('simple'::regconfig, coalesce(variant, '')), 'B')
                      ) STORED,
  status              text NOT NULL DEFAULT 'active',
  merged_into         uuid,
  created_by_store_id uuid,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             int NOT NULL DEFAULT 1,
  CONSTRAINT fk_products_brand_id FOREIGN KEY (brand_id) REFERENCES brands (id),
  CONSTRAINT fk_products_category_id FOREIGN KEY (category_id) REFERENCES categories (id),
  CONSTRAINT fk_products_merged_into FOREIGN KEY (merged_into) REFERENCES products (id),
  CONSTRAINT ck_products_name CHECK (char_length(name) BETWEEN 1 AND 120),
  CONSTRAINT ck_products_variant CHECK (variant IS NULL OR char_length(variant) BETWEEN 1 AND 80),
  CONSTRAINT ck_products_pack_size CHECK (pack_size > 0),
  CONSTRAINT ck_products_unit CHECK (unit IN ('g','kg','ml','l','pc','pack')),
  CONSTRAINT ck_products_mrp_paise CHECK (mrp_paise IS NULL OR mrp_paise > 0),
  CONSTRAINT ck_products_image_key CHECK (image_key IS NULL OR char_length(image_key) BETWEEN 1 AND 300),
  CONSTRAINT ck_products_barcodes CHECK (cardinality(barcodes) <= 10),
  CONSTRAINT ck_products_status CHECK (status IN ('active','pending_review','merged','retired')),
  CONSTRAINT ck_products_merged CHECK ((status = 'merged') = (merged_into IS NOT NULL)),
  CONSTRAINT ck_products_merged_self CHECK (merged_into IS NULL OR merged_into <> id)
);
CREATE INDEX ix_products_barcodes ON products USING gin (barcodes);
CREATE INDEX ix_products_name_trgm ON products USING gin (name gin_trgm_ops);
CREATE INDEX ix_products_search ON products USING gin (search);
CREATE INDEX ix_products_brand_id ON products (brand_id) WHERE brand_id IS NOT NULL;
CREATE INDEX ix_products_category_id ON products (category_id);
CREATE INDEX ix_products_merged_into ON products (merged_into) WHERE merged_into IS NOT NULL;
CREATE INDEX ix_products_created_by_store_id ON products (created_by_store_id)
  WHERE created_by_store_id IS NOT NULL;
CREATE TRIGGER trg_products_touch BEFORE UPDATE ON products
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- store_items: a store's copy of a master product with its own price, MRP,
-- listing and the cached stock counters. Available = on_hand − reserved.
-- version moves only on a PATCH of price or listing, never on a counter
-- change, so a sale does not break a manager's If-Match. stock_updated_at is
-- the freshness moment (01 §4): any movement, or a count with no change.
-- The selling price is never above the MRP (Legal Metrology; 01 §11.2 says
-- the same for an ask's price).
------------------------------------------------------------------------------
CREATE TABLE store_items (
  id               uuid PRIMARY KEY,
  store_id         uuid NOT NULL,
  product_id       uuid NOT NULL,
  local_name       text,
  price_paise      bigint NOT NULL,
  mrp_paise        bigint,
  on_hand          int NOT NULL DEFAULT 0,
  reserved         int NOT NULL DEFAULT 0,
  low_stock_at     int,
  is_listed        boolean NOT NULL DEFAULT true,
  stock_updated_at timestamptz,
  search           tsvector NOT NULL DEFAULT ''::tsvector,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  version          int NOT NULL DEFAULT 1,
  CONSTRAINT fk_store_items_product_id FOREIGN KEY (product_id) REFERENCES products (id),
  CONSTRAINT ck_store_items_local_name CHECK (local_name IS NULL OR char_length(local_name) BETWEEN 1 AND 120),
  CONSTRAINT ck_store_items_price_paise CHECK (price_paise > 0),
  CONSTRAINT ck_store_items_mrp_paise CHECK (mrp_paise IS NULL OR mrp_paise > 0),
  CONSTRAINT ck_store_items_price_mrp CHECK (mrp_paise IS NULL OR price_paise <= mrp_paise),
  CONSTRAINT ck_store_items_reserved CHECK (reserved >= 0),
  CONSTRAINT ck_store_items_low_stock_at CHECK (low_stock_at IS NULL OR low_stock_at >= 0)
);
CREATE UNIQUE INDEX ux_store_items_store_product ON store_items (store_id, product_id);
CREATE INDEX ix_store_items_store_id ON store_items (store_id) INCLUDE (price_paise, on_hand, reserved);
CREATE INDEX ix_store_items_search ON store_items USING gin (search);
CREATE INDEX ix_store_items_product_id ON store_items (product_id);
CREATE TRIGGER trg_store_items_touch BEFORE UPDATE ON store_items
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- stock_batches (D-99): created only by stock in that carries an expiry or a
-- cost and by a positive count (found_in_count, no expiry). quantity is the
-- remaining quantity; it changes only through stock_movement_batches (the
-- trigger below). Only cost_paise and expires_on are edited afterwards
-- (PATCH {S}/batches/{batchId}, which bumps version).
------------------------------------------------------------------------------
CREATE TABLE stock_batches (
  id                uuid PRIMARY KEY,
  store_item_id     uuid NOT NULL,
  received_quantity int NOT NULL,
  quantity          int NOT NULL DEFAULT 0,
  expires_on        date,
  received_at       timestamptz NOT NULL,
  cost_paise        bigint,
  found_in_count    boolean NOT NULL DEFAULT false,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  version           int NOT NULL DEFAULT 1,
  CONSTRAINT fk_stock_batches_store_item_id FOREIGN KEY (store_item_id) REFERENCES store_items (id),
  CONSTRAINT ck_stock_batches_received_quantity CHECK (received_quantity > 0),
  CONSTRAINT ck_stock_batches_quantity CHECK (quantity BETWEEN 0 AND received_quantity),
  CONSTRAINT ck_stock_batches_cost_paise CHECK (cost_paise IS NULL OR cost_paise >= 0),
  CONSTRAINT ck_stock_batches_found_in_count CHECK (NOT found_in_count OR expires_on IS NULL)
);
CREATE INDEX ix_stock_batches_item_expires ON stock_batches (store_item_id, expires_on) WHERE quantity > 0;
CREATE INDEX ix_stock_batches_store_item_id ON stock_batches (store_item_id, received_at);
CREATE TRIGGER trg_stock_batches_touch BEFORE UPDATE ON stock_batches
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- stock_movements: the append-only ledger (D-01, D-34; not partitioned in v1).
-- client_id is the device's id of the offline operation line (a replayed
-- sync never writes twice); server-originated movements leave it null.
------------------------------------------------------------------------------
CREATE TABLE stock_movements (
  id            uuid PRIMARY KEY,
  store_id      uuid NOT NULL,
  store_item_id uuid NOT NULL,
  delta         int NOT NULL,
  source        text NOT NULL,
  ref_type      text,
  ref_id        uuid,
  actor_user_id uuid,
  device_id     uuid,
  client_id     uuid,
  note          text,
  created_at    timestamptz NOT NULL,
  CONSTRAINT fk_stock_movements_store_item_id FOREIGN KEY (store_item_id) REFERENCES store_items (id),
  CONSTRAINT ck_stock_movements_delta CHECK (delta <> 0),
  CONSTRAINT ck_stock_movements_source CHECK (source IN ('opening','restock','supplier_bill','import',
    'count_correction','damage','expired','sale_pos','sale_online','pos_reversal')),
  CONSTRAINT ck_stock_movements_ref CHECK ((ref_type IS NULL) = (ref_id IS NULL)),
  CONSTRAINT ck_stock_movements_ref_type CHECK (ref_type IS NULL OR (ref_type ~ '^[a-z][a-z0-9_]*$' AND char_length(ref_type) <= 40)),
  CONSTRAINT ck_stock_movements_note CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 200)
);
CREATE INDEX ix_stock_movements_created_at ON stock_movements USING brin (created_at);
CREATE UNIQUE INDEX ux_stock_movements_store_client ON stock_movements (store_id, client_id);
CREATE INDEX ix_stock_movements_item_created ON stock_movements (store_item_id, created_at DESC);
CREATE INDEX ix_stock_movements_ref ON stock_movements (ref_type, ref_id) WHERE ref_id IS NOT NULL;
CREATE INDEX ix_stock_movements_store_id ON stock_movements (store_id, created_at DESC);
CREATE INDEX ix_stock_movements_actor_user_id ON stock_movements (actor_user_id) WHERE actor_user_id IS NOT NULL;
CREATE INDEX ix_stock_movements_device_id ON stock_movements (device_id) WHERE device_id IS NOT NULL;
CREATE TRIGGER trg_stock_movements_append_only BEFORE UPDATE OR DELETE ON stock_movements
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_stock_movements_no_truncate BEFORE TRUNCATE ON stock_movements
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
REVOKE UPDATE, DELETE, TRUNCATE ON stock_movements FROM todayz_app;

-- stock_movements_apply: on_hand += delta and the freshness moment moves to
-- the movement's time (the application clock, never now()). A movement whose
-- store_id is not its item's store is refused, so the per-store reads and the
-- delta feed can trust stock_movements.store_id.
-- +goose StatementBegin
CREATE FUNCTION stock_movements_apply() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  item_store uuid;
BEGIN
  UPDATE public.store_items si
     SET on_hand = si.on_hand + NEW.delta,
         stock_updated_at = GREATEST(COALESCE(si.stock_updated_at, NEW.created_at), NEW.created_at)
   WHERE si.id = NEW.store_item_id
  RETURNING si.store_id INTO item_store;
  IF item_store IS DISTINCT FROM NEW.store_id THEN
    RAISE EXCEPTION 'stock_movements: item % is not in store %', NEW.store_item_id, NEW.store_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_stock_movements_apply AFTER INSERT ON stock_movements
  FOR EACH ROW EXECUTE FUNCTION stock_movements_apply();

------------------------------------------------------------------------------
-- stock_movement_batches (D-99): the per-batch allocation of one movement.
-- Σ delta of a movement's rows is the part that came from (or went into)
-- batches; the rest is stock in no batch. Append-only.
------------------------------------------------------------------------------
CREATE TABLE stock_movement_batches (
  movement_id uuid NOT NULL,
  batch_id    uuid NOT NULL,
  delta       int NOT NULL,
  CONSTRAINT pk_stock_movement_batches PRIMARY KEY (movement_id, batch_id),
  CONSTRAINT fk_stock_movement_batches_movement_id FOREIGN KEY (movement_id) REFERENCES stock_movements (id),
  CONSTRAINT fk_stock_movement_batches_batch_id FOREIGN KEY (batch_id) REFERENCES stock_batches (id),
  CONSTRAINT ck_stock_movement_batches_delta CHECK (delta <> 0)
);
CREATE INDEX ix_stock_movement_batches_batch_id ON stock_movement_batches (batch_id);
CREATE TRIGGER trg_stock_movement_batches_append_only BEFORE UPDATE OR DELETE ON stock_movement_batches
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_stock_movement_batches_no_truncate BEFORE TRUNCATE ON stock_movement_batches
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
REVOKE UPDATE, DELETE, TRUNCATE ON stock_movement_batches FROM todayz_app;

-- stock_movement_batches_apply: the batch's remaining quantity moves by the
-- row's delta (ck_stock_batches_quantity keeps it within 0..received). The
-- row must belong to the movement's item, have the movement's sign, and the
-- movement's rows together may not exceed the movement.
-- +goose StatementBegin
CREATE FUNCTION stock_movement_batches_apply() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  batch_item uuid;
  mv_item    uuid;
  mv_delta   int;
  allocated  bigint;
BEGIN
  SELECT m.store_item_id, m.delta INTO mv_item, mv_delta
    FROM public.stock_movements m WHERE m.id = NEW.movement_id;
  IF (NEW.delta > 0) <> (mv_delta > 0) THEN
    RAISE EXCEPTION 'stock_movement_batches: allocation sign differs from movement %', NEW.movement_id
      USING ERRCODE = 'check_violation';
  END IF;
  SELECT pg_catalog.sum(a.delta) INTO allocated
    FROM public.stock_movement_batches a WHERE a.movement_id = NEW.movement_id;
  IF pg_catalog.abs(allocated) > pg_catalog.abs(mv_delta) THEN
    RAISE EXCEPTION 'stock_movement_batches: allocation exceeds movement %', NEW.movement_id
      USING ERRCODE = 'check_violation';
  END IF;
  UPDATE public.stock_batches b SET quantity = b.quantity + NEW.delta
   WHERE b.id = NEW.batch_id
  RETURNING b.store_item_id INTO batch_item;
  IF batch_item IS DISTINCT FROM mv_item THEN
    RAISE EXCEPTION 'stock_movement_batches: batch % is not of the movement''s item', NEW.batch_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_stock_movement_batches_apply AFTER INSERT ON stock_movement_batches
  FOR EACH ROW EXECUTE FUNCTION stock_movement_batches_apply();

------------------------------------------------------------------------------
-- catalogue_changes (D-36, D-84): the offline POS delta cursor. seq comes from
-- stores.catalogue_seq with UPDATE … RETURNING in the writing transaction, so
-- per store the rows commit in seq order. Kept 30 days (a purge deletes old
-- rows, so it is not append-only). The feed endpoint is deferred; rows are
-- written from W1-05 on.
------------------------------------------------------------------------------
CREATE TABLE catalogue_changes (
  store_id   uuid NOT NULL,
  seq        bigint NOT NULL,
  entity     text NOT NULL,
  entity_id  uuid NOT NULL,
  op         text NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT pk_catalogue_changes PRIMARY KEY (store_id, seq),
  CONSTRAINT ck_catalogue_changes_seq CHECK (seq > 0),
  CONSTRAINT ck_catalogue_changes_entity CHECK (entity IN ('store_item','product','category','offer')),
  CONSTRAINT ck_catalogue_changes_op CHECK (op IN ('upsert','delete'))
);
CREATE INDEX ix_catalogue_changes_created_at ON catalogue_changes USING brin (created_at);

-- Taking a seq updates the stores row, and W1-04's trg_stores_touch would move
-- stores.updated_at on every stock change. ix_stores_pending_review orders the
-- admin store review queue by updated_at, so a pending store that sets up its
-- stock would lose its place. The trigger is recreated to skip an update that
-- moves only catalogue_seq (no W1-04 write changes catalogue_seq). Flagged to
-- the lead in the W1-05 report.
DROP TRIGGER trg_stores_touch ON stores;
CREATE TRIGGER trg_stores_touch BEFORE UPDATE ON stores
  FOR EACH ROW WHEN (OLD.catalogue_seq IS NOT DISTINCT FROM NEW.catalogue_seq)
  EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- Seeded categories (03 §3 "Seeded"; reference data, so here and not in the
-- dev seed, which refuses to run outside local and test). Six top-level
-- categories with the design's tints and icons, and their children with the
-- parent's tint. Fixed ids so the apps, the seed and tests can name them.
------------------------------------------------------------------------------
INSERT INTO categories (id, parent_id, slug, names, tint, icon, sort) VALUES
  ('0190c47e-0000-7000-8000-000000000100', NULL, 'grocery',
   '{"en":"Grocery","hi":"किराना","kn":"ದಿನಸಿ","te":"కిరాణా","ta":"மளிகை"}', 'green', 'shopping-bag', 10),
  ('0190c47e-0000-7000-8000-000000000200', NULL, 'pharmacy',
   '{"en":"Pharmacy","hi":"फ़ार्मेसी","kn":"ಔಷಧಾಲಯ","te":"ఫార్మసీ","ta":"மருந்தகம்"}', 'rose', 'pill', 20),
  ('0190c47e-0000-7000-8000-000000000300', NULL, 'beauty',
   '{"en":"Beauty","hi":"सौंदर्य","kn":"ಸೌಂದರ್ಯ","te":"అందం","ta":"அழகு"}', 'lilac', 'sparkles', 30),
  ('0190c47e-0000-7000-8000-000000000400', NULL, 'electronics',
   '{"en":"Electronics","hi":"इलेक्ट्रॉनिक्स","kn":"ಎಲೆಕ್ಟ್ರಾನಿಕ್ಸ್","te":"ఎలక్ట్రానిక్స్","ta":"மின்னணு"}', 'sky', 'smartphone', 40),
  ('0190c47e-0000-7000-8000-000000000500', NULL, 'stationery',
   '{"en":"Stationery","hi":"स्टेशनरी","kn":"ಲೇಖನ ಸಾಮಗ್ರಿ","te":"స్టేషనరీ","ta":"எழுதுபொருள்"}', 'amber', 'pen', 50),
  ('0190c47e-0000-7000-8000-000000000600', NULL, 'hardware',
   '{"en":"Hardware","hi":"हार्डवेयर","kn":"ಹಾರ್ಡ್‌ವೇರ್","te":"హార్డ్‌వేర్","ta":"வன்பொருள்"}', 'peach', 'wrench', 60);

INSERT INTO categories (id, parent_id, slug, names, tint, icon, sort) VALUES
  ('0190c47e-0000-7000-8000-000000000101', '0190c47e-0000-7000-8000-000000000100', 'staples',
   '{"en":"Atta, rice and dal","hi":"आटा, चावल और दाल","kn":"ಹಿಟ್ಟು, ಅಕ್ಕಿ ಮತ್ತು ಬೇಳೆ","te":"పిండి, బియ్యం, పప్పు","ta":"மாவு, அரிசி மற்றும் பருப்பு"}', 'green', 'wheat', 10),
  ('0190c47e-0000-7000-8000-000000000102', '0190c47e-0000-7000-8000-000000000100', 'dairy-eggs',
   '{"en":"Dairy and eggs","hi":"दूध, डेयरी और अंडे","kn":"ಹಾಲು ಮತ್ತು ಮೊಟ್ಟೆ","te":"పాలు, గుడ్లు","ta":"பால் மற்றும் முட்டை"}', 'green', 'milk', 20),
  ('0190c47e-0000-7000-8000-000000000103', '0190c47e-0000-7000-8000-000000000100', 'fruits-vegetables',
   '{"en":"Fruits and vegetables","hi":"फल और सब्ज़ियाँ","kn":"ಹಣ್ಣು ಮತ್ತು ತರಕಾರಿ","te":"పండ్లు, కూరగాయలు","ta":"பழங்கள் மற்றும் காய்கறிகள்"}', 'green', 'carrot', 30),
  ('0190c47e-0000-7000-8000-000000000104', '0190c47e-0000-7000-8000-000000000100', 'snacks',
   '{"en":"Snacks","hi":"नमकीन और स्नैक्स","kn":"ತಿಂಡಿಗಳು","te":"చిరుతిళ్లు","ta":"தின்பண்டங்கள்"}', 'green', 'cookie', 40),
  ('0190c47e-0000-7000-8000-000000000105', '0190c47e-0000-7000-8000-000000000100', 'beverages',
   '{"en":"Beverages","hi":"पेय पदार्थ","kn":"ಪಾನೀಯಗಳು","te":"పానీయాలు","ta":"பானங்கள்"}', 'green', 'cup-soda', 50),
  ('0190c47e-0000-7000-8000-000000000106', '0190c47e-0000-7000-8000-000000000100', 'spices-oils',
   '{"en":"Spices and oils","hi":"मसाले और तेल","kn":"ಮಸಾಲೆ ಮತ್ತು ಎಣ್ಣೆ","te":"మసాలాలు, నూనెలు","ta":"மசாலா மற்றும் எண்ணெய்"}', 'green', 'flame', 60),
  ('0190c47e-0000-7000-8000-000000000107', '0190c47e-0000-7000-8000-000000000100', 'bakery',
   '{"en":"Bakery","hi":"बेकरी","kn":"ಬೇಕರಿ","te":"బేకరీ","ta":"பேக்கரி"}', 'green', 'croissant', 70),
  ('0190c47e-0000-7000-8000-000000000108', '0190c47e-0000-7000-8000-000000000100', 'household',
   '{"en":"Household","hi":"घरेलू सामान","kn":"ಮನೆ ಬಳಕೆ ವಸ್ತುಗಳು","te":"గృహోపకరణాలు","ta":"வீட்டு உபயோகப் பொருட்கள்"}', 'green', 'spray-can', 80),
  ('0190c47e-0000-7000-8000-000000000109', '0190c47e-0000-7000-8000-000000000100', 'personal-care',
   '{"en":"Personal care","hi":"व्यक्तिगत देखभाल","kn":"ವೈಯಕ್ತಿಕ ಆರೈಕೆ","te":"వ్యక్తిగత సంరక్షణ","ta":"தனிநபர் பராமரிப்பு"}', 'green', 'bath', 90),
  ('0190c47e-0000-7000-8000-000000000201', '0190c47e-0000-7000-8000-000000000200', 'medicines',
   '{"en":"Medicines","hi":"दवाइयाँ","kn":"ಔಷಧಿಗಳು","te":"మందులు","ta":"மருந்துகள்"}', 'rose', 'pill', 10),
  ('0190c47e-0000-7000-8000-000000000202', '0190c47e-0000-7000-8000-000000000200', 'health-wellness',
   '{"en":"Health and wellness","hi":"स्वास्थ्य","kn":"ಆರೋಗ್ಯ","te":"ఆరోగ్యం","ta":"ஆரோக்கியம்"}', 'rose', 'heart-pulse', 20),
  ('0190c47e-0000-7000-8000-000000000203', '0190c47e-0000-7000-8000-000000000200', 'baby-care',
   '{"en":"Baby care","hi":"शिशु देखभाल","kn":"ಮಗುವಿನ ಆರೈಕೆ","te":"శిశు సంరక్షణ","ta":"குழந்தை பராமரிப்பு"}', 'rose', 'baby', 30),
  ('0190c47e-0000-7000-8000-000000000301', '0190c47e-0000-7000-8000-000000000300', 'skin-care',
   '{"en":"Skin care","hi":"त्वचा की देखभाल","kn":"ಚರ್ಮದ ಆರೈಕೆ","te":"చర్మ సంరక్షణ","ta":"சரும பராமரிப்பு"}', 'lilac', 'droplets', 10),
  ('0190c47e-0000-7000-8000-000000000302', '0190c47e-0000-7000-8000-000000000300', 'hair-care',
   '{"en":"Hair care","hi":"बालों की देखभाल","kn":"ಕೂದಲಿನ ಆರೈಕೆ","te":"జుట్టు సంరక్షణ","ta":"முடி பராமரிப்பு"}', 'lilac', 'scissors', 20),
  ('0190c47e-0000-7000-8000-000000000303', '0190c47e-0000-7000-8000-000000000300', 'makeup',
   '{"en":"Makeup","hi":"मेकअप","kn":"ಮೇಕಪ್","te":"మేకప్","ta":"ஒப்பனை"}', 'lilac', 'palette', 30),
  ('0190c47e-0000-7000-8000-000000000401', '0190c47e-0000-7000-8000-000000000400', 'mobile-accessories',
   '{"en":"Mobile accessories","hi":"मोबाइल एक्सेसरीज़","kn":"ಮೊಬೈಲ್ ಪರಿಕರಗಳು","te":"మొబైల్ ఉపకరణాలు","ta":"கைபேசி துணைக்கருவிகள்"}', 'sky', 'cable', 10),
  ('0190c47e-0000-7000-8000-000000000402', '0190c47e-0000-7000-8000-000000000400', 'batteries-lighting',
   '{"en":"Batteries and lighting","hi":"बैटरी और लाइट","kn":"ಬ್ಯಾಟರಿ ಮತ್ತು ದೀಪಗಳು","te":"బ్యాటరీలు, లైట్లు","ta":"மின்கலங்கள் மற்றும் விளக்குகள்"}', 'sky', 'lightbulb', 20),
  ('0190c47e-0000-7000-8000-000000000403', '0190c47e-0000-7000-8000-000000000400', 'small-appliances',
   '{"en":"Small appliances","hi":"छोटे उपकरण","kn":"ಸಣ್ಣ ಉಪಕರಣಗಳು","te":"చిన్న ఉపకరణాలు","ta":"சிறு சாதனங்கள்"}', 'sky', 'plug', 30),
  ('0190c47e-0000-7000-8000-000000000501', '0190c47e-0000-7000-8000-000000000500', 'school-supplies',
   '{"en":"School supplies","hi":"स्कूल का सामान","kn":"ಶಾಲಾ ಸಾಮಗ್ರಿ","te":"పాఠశాల సామగ్రి","ta":"பள்ளிப் பொருட்கள்"}', 'amber', 'notebook-pen', 10),
  ('0190c47e-0000-7000-8000-000000000502', '0190c47e-0000-7000-8000-000000000500', 'office-supplies',
   '{"en":"Office supplies","hi":"ऑफ़िस का सामान","kn":"ಕಚೇರಿ ಸಾಮಗ್ರಿ","te":"కార్యాలయ సామగ్రి","ta":"அலுவலகப் பொருட்கள்"}', 'amber', 'paperclip', 20),
  ('0190c47e-0000-7000-8000-000000000601', '0190c47e-0000-7000-8000-000000000600', 'tools',
   '{"en":"Tools","hi":"औज़ार","kn":"ಸಲಕರಣೆಗಳು","te":"పనిముట్లు","ta":"கருவிகள்"}', 'peach', 'hammer', 10),
  ('0190c47e-0000-7000-8000-000000000602', '0190c47e-0000-7000-8000-000000000600', 'electrical',
   '{"en":"Electrical","hi":"बिजली का सामान","kn":"ವಿದ್ಯುತ್ ಸಾಮಗ್ರಿ","te":"విద్యుత్ సామగ్రి","ta":"மின் சாதனங்கள்"}', 'peach', 'zap', 20),
  ('0190c47e-0000-7000-8000-000000000603', '0190c47e-0000-7000-8000-000000000600', 'plumbing',
   '{"en":"Plumbing","hi":"प्लंबिंग का सामान","kn":"ಕೊಳಾಯಿ ಸಾಮಗ್ರಿ","te":"ప్లంబింగ్ సామగ్రి","ta":"குழாய் பொருட்கள்"}', 'peach', 'droplet', 30),
  ('0190c47e-0000-7000-8000-000000000604', '0190c47e-0000-7000-8000-000000000600', 'paints',
   '{"en":"Paints","hi":"पेंट","kn":"ಬಣ್ಣಗಳು","te":"రంగులు","ta":"வண்ணப்பூச்சுகள்"}', 'peach', 'paint-roller', 40);

-- +goose Down
DROP TRIGGER IF EXISTS trg_stores_touch ON stores;
CREATE TRIGGER trg_stores_touch BEFORE UPDATE ON stores
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
DROP TABLE IF EXISTS catalogue_changes;
DROP TRIGGER IF EXISTS trg_stock_movement_batches_apply ON stock_movement_batches;
DROP FUNCTION IF EXISTS stock_movement_batches_apply();
DROP TABLE IF EXISTS stock_movement_batches;
DROP TRIGGER IF EXISTS trg_stock_movements_apply ON stock_movements;
DROP FUNCTION IF EXISTS stock_movements_apply();
DROP TABLE IF EXISTS stock_movements;
DROP TABLE IF EXISTS stock_batches;
DROP TABLE IF EXISTS store_items;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS brands;
DROP TABLE IF EXISTS categories;
