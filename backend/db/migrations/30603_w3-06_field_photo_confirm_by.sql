-- W3-06 field agents: stale field photos (the lead's re-review of 88e4624).
--
-- business_documents.confirm_by is when a field photo's upload must be
-- confirmed (POST .../documents/{docId}/confirm): the upload URL's life plus
-- five minutes. Only the field routes set it; the owner routes have no
-- confirm step yet, so their rows keep it null and are never swept.
--
-- Past confirm_by a pending row no longer counts towards the 5-per-kind cap
-- on either route, and the hourly business_field_photos_sweep rejects it and
-- deletes its object.

-- +goose Up

ALTER TABLE business_documents ADD COLUMN confirm_by timestamptz;
CREATE INDEX ix_business_documents_confirm_by ON business_documents (confirm_by)
  WHERE status = 'pending' AND confirm_by IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ix_business_documents_confirm_by;
ALTER TABLE business_documents DROP COLUMN IF EXISTS confirm_by;
