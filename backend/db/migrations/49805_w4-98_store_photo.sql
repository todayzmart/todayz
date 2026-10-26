-- The shop's storefront photo on the customer cards and shop page (D-246,
-- founder 26 Oct). photo_key is the copy in the public images bucket of the
-- newest accepted storefront document (photo_document_id); the
-- store_photos_publish job keeps both in step with the documents.

-- +goose Up
ALTER TABLE stores
  ADD COLUMN photo_key text,
  ADD COLUMN photo_document_id uuid REFERENCES business_documents(id),
  ADD CONSTRAINT ck_stores_photo_key CHECK (photo_key IS NULL OR char_length(photo_key) BETWEEN 1 AND 512);
CREATE INDEX ix_stores_photo_document_id ON stores (photo_document_id) WHERE photo_document_id IS NOT NULL;

-- +goose Down
DROP INDEX ix_stores_photo_document_id;
ALTER TABLE stores
  DROP CONSTRAINT ck_stores_photo_key,
  DROP COLUMN photo_document_id,
  DROP COLUMN photo_key;
