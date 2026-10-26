-- Item-type photos (D-253): one free-licence photo per kind of item ("toor
-- dal", "toothpaste") in a category, shown on every product of that kind
-- that has no pack photo of its own.
--
-- item_type_key(name, brand) is the kind: the product name without its brand
-- and without the pack size that ends it ("Aashirvaad Urad dal 500 g" with
-- brand Aashirvaad is "urad dal"), lower-cased. The importer (`todayz images types`)
-- groups products by (category, kind), searches Wikimedia Commons for each
-- kind and stores up to three candidates per kind as `pending` type_photos in
-- the private bucket. An admin picks one in A-08: it is copied to the public
-- images bucket once, the other candidates are turned down, and every product
-- of that kind without an image gets a product_images row with source `type`
-- pointing at the shared copy. A pack photo approved later replaces it as any
-- image does.

-- +goose Up
CREATE FUNCTION item_type_key(name text, brand text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT nullif(lower(trim(regexp_replace(
    regexp_replace(
      CASE WHEN brand IS NOT NULL AND lower(name) LIKE lower(brand) || ' %'
        THEN substr(name, char_length(brand) + 2) ELSE name END,
      '\s+[0-9]+([.,][0-9]+)?\s*[[:alpha:]]*(\s.*)?$', ''),
    '\s+', ' ', 'g'))), '')
$$;

CREATE TABLE type_photos (
  id            uuid PRIMARY KEY,
  category_id   uuid NOT NULL REFERENCES categories (id),
  type_key      text NOT NULL,
  status        text NOT NULL DEFAULT 'pending',
  rank          smallint NOT NULL,
  object_key    text NOT NULL,
  content_type  text NOT NULL,
  size_bytes    int NOT NULL,
  public_key    text,
  source        text NOT NULL DEFAULT 'commons',
  source_ref    text NOT NULL,
  source_url    text NOT NULL,
  page_url      text NOT NULL,
  licence       text NOT NULL,
  attribution   text NOT NULL,
  decided_by    uuid REFERENCES users (id),
  decided_at    timestamptz,
  reject_reason text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_type_photos_status CHECK (status IN ('pending','approved','rejected')),
  CONSTRAINT ck_type_photos_source CHECK (source IN ('commons')),
  CONSTRAINT ck_type_photos_rank CHECK (rank BETWEEN 1 AND 5),
  CONSTRAINT ck_type_photos_content_type CHECK (content_type IN ('image/jpeg','image/png','image/webp')),
  CONSTRAINT ck_type_photos_size CHECK (size_bytes > 0),
  CONSTRAINT ck_type_photos_approved CHECK ((status = 'approved') = (public_key IS NOT NULL)),
  CONSTRAINT ck_type_photos_texts CHECK (
    char_length(type_key) BETWEEN 1 AND 120
    AND char_length(object_key) BETWEEN 1 AND 300
    AND (public_key IS NULL OR char_length(public_key) BETWEEN 1 AND 300)
    AND char_length(source_ref) BETWEEN 1 AND 240
    AND char_length(source_url) BETWEEN 1 AND 500
    AND char_length(page_url) BETWEEN 1 AND 500
    AND char_length(licence) BETWEEN 1 AND 40
    AND char_length(attribution) BETWEEN 1 AND 200
    AND (reject_reason IS NULL OR char_length(reject_reason) BETWEEN 1 AND 500))
);
-- One photo per kind at a time, and a file is a candidate for a kind once.
CREATE UNIQUE INDEX ux_type_photos_approved ON type_photos (category_id, type_key) WHERE status = 'approved';
CREATE UNIQUE INDEX ux_type_photos_ref ON type_photos (category_id, type_key, source_ref);
CREATE INDEX ix_type_photos_pending ON type_photos (type_key, category_id, rank) WHERE status = 'pending';
CREATE INDEX ix_type_photos_decided_by ON type_photos (decided_by) WHERE decided_by IS NOT NULL;
CREATE TRIGGER trg_type_photos_touch BEFORE UPDATE ON type_photos
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- A product showing its kind's photo has a product_images row with source
-- `type`, sharing the kind's files.
ALTER TABLE product_images ADD COLUMN type_photo_id uuid REFERENCES type_photos (id);
CREATE INDEX ix_product_images_type_photo_id ON product_images (type_photo_id) WHERE type_photo_id IS NOT NULL;
ALTER TABLE product_images DROP CONSTRAINT ck_product_images_source;
ALTER TABLE product_images ADD CONSTRAINT ck_product_images_source
  CHECK (source IN ('retailer','admin','off','obf','opf','type'));
ALTER TABLE product_images ADD CONSTRAINT ck_product_images_type
  CHECK ((source = 'type') = (type_photo_id IS NOT NULL)
    AND (source <> 'type' OR (page_url IS NOT NULL AND licence IS NOT NULL AND attribution IS NOT NULL)));

-- +goose Down
ALTER TABLE product_images DROP CONSTRAINT ck_product_images_type;
ALTER TABLE product_images DROP CONSTRAINT ck_product_images_source;
ALTER TABLE product_images ADD CONSTRAINT ck_product_images_source
  CHECK (source IN ('retailer','admin','off','obf','opf'));
DROP INDEX ix_product_images_type_photo_id;
ALTER TABLE product_images DROP COLUMN type_photo_id;
DROP TABLE type_photos;
DROP FUNCTION item_type_key(text, text);
