-- Product photos (D-252): a shop's photo of a product it created, an admin's
-- upload, and stock photos imported from Open Food Facts, Open Beauty Facts
-- and Open Products Facts.
--
-- product_images holds every candidate image. A draft lives in the private
-- bucket (object_key) and no customer sees it: a retailer or imported image
-- waits as 'pending' until an admin approves it; approval copies it into the
-- public images bucket (public_key) and points the product at it
-- (products.image_id, products.image_key). An admin upload is approved when
-- it is confirmed. The image it replaces becomes 'replaced'.
--
-- Imported images carry their source, licence and attribution (CC BY-SA),
-- which the customer product page shows as the credit.

-- +goose Up
CREATE TABLE product_images (
  id            uuid PRIMARY KEY,
  product_id    uuid NOT NULL REFERENCES products (id),
  source        text NOT NULL,
  status        text NOT NULL DEFAULT 'uploading',
  object_key    text NOT NULL,
  content_type  text NOT NULL,
  size_bytes    int,
  public_key    text,
  store_id      uuid REFERENCES stores (id),
  uploaded_by   uuid REFERENCES users (id),
  source_ref    text,
  source_url    text,
  page_url      text,
  licence       text,
  attribution   text,
  match         text,
  decided_by    uuid REFERENCES users (id),
  decided_at    timestamptz,
  reject_reason text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_product_images_source CHECK (source IN ('retailer','admin','off','obf','opf')),
  CONSTRAINT ck_product_images_status CHECK (status IN ('uploading','pending','approved','rejected','replaced')),
  CONSTRAINT ck_product_images_object_key CHECK (char_length(object_key) BETWEEN 1 AND 300),
  CONSTRAINT ck_product_images_content_type CHECK (content_type IN ('image/jpeg','image/png','image/webp')),
  CONSTRAINT ck_product_images_size CHECK (size_bytes IS NULL OR size_bytes > 0),
  CONSTRAINT ck_product_images_public_key CHECK (public_key IS NULL OR char_length(public_key) BETWEEN 1 AND 300),
  CONSTRAINT ck_product_images_approved CHECK ((status IN ('approved','replaced')) = (public_key IS NOT NULL)),
  CONSTRAINT ck_product_images_retailer CHECK (source <> 'retailer' OR store_id IS NOT NULL),
  -- An imported image always names where it came from and how it is licensed.
  CONSTRAINT ck_product_images_import CHECK (
    source NOT IN ('off','obf','opf')
    OR (source_url IS NOT NULL AND page_url IS NOT NULL AND licence IS NOT NULL AND attribution IS NOT NULL AND match IS NOT NULL)),
  CONSTRAINT ck_product_images_match CHECK (match IS NULL OR match IN ('barcode','name')),
  CONSTRAINT ck_product_images_texts CHECK (
    (source_ref IS NULL OR char_length(source_ref) BETWEEN 1 AND 64)
    AND (source_url IS NULL OR char_length(source_url) BETWEEN 1 AND 500)
    AND (page_url IS NULL OR char_length(page_url) BETWEEN 1 AND 500)
    AND (licence IS NULL OR char_length(licence) BETWEEN 1 AND 40)
    AND (attribution IS NULL OR char_length(attribution) BETWEEN 1 AND 200)
    AND (reject_reason IS NULL OR char_length(reject_reason) BETWEEN 1 AND 500))
);
CREATE INDEX ix_product_images_product_id ON product_images (product_id, created_at);
CREATE INDEX ix_product_images_pending ON product_images (created_at, id) WHERE status = 'pending';
CREATE INDEX ix_product_images_store_id ON product_images (store_id) WHERE store_id IS NOT NULL;
CREATE INDEX ix_product_images_uploaded_by ON product_images (uploaded_by) WHERE uploaded_by IS NOT NULL;
CREATE INDEX ix_product_images_decided_by ON product_images (decided_by) WHERE decided_by IS NOT NULL;
-- At most one approved image per product: the one products.image_id names.
CREATE UNIQUE INDEX ux_product_images_approved ON product_images (product_id) WHERE status = 'approved';
CREATE TRIGGER trg_product_images_touch BEFORE UPDATE ON product_images
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

ALTER TABLE products ADD COLUMN image_id uuid REFERENCES product_images (id);
CREATE INDEX ix_products_image_id ON products (image_id) WHERE image_id IS NOT NULL;

-- +goose Down
DROP INDEX ix_products_image_id;
ALTER TABLE products DROP COLUMN image_id;
DROP TABLE product_images;
