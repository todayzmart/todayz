-- W3-05 admin console: the A-03 Leads tab works a lead through new → contacted
-- → closed (04 §2.7), and the A-05 Review tab sends a store back with a note
-- the owner sees (D-40). A send-back returns the store to draft; the note stays
-- until the owner submits again or an admin approves.

-- +goose Up
ALTER TABLE leads
  ADD COLUMN status            text NOT NULL DEFAULT 'new',
  ADD COLUMN status_changed_at timestamptz,
  ADD COLUMN status_changed_by uuid,
  ADD COLUMN version           int NOT NULL DEFAULT 1,
  ADD CONSTRAINT ck_leads_status CHECK (status IN ('new','contacted','closed')),
  ADD CONSTRAINT fk_leads_status_changed_by FOREIGN KEY (status_changed_by) REFERENCES users (id);
CREATE INDEX ix_leads_status ON leads (status, created_at DESC, id DESC);
CREATE INDEX ix_leads_status_changed_by ON leads (status_changed_by) WHERE status_changed_by IS NOT NULL;

ALTER TABLE stores
  ADD COLUMN review_note text,
  ADD CONSTRAINT ck_stores_review_note CHECK (review_note IS NULL OR char_length(review_note) BETWEEN 1 AND 500);
CREATE INDEX ix_stores_status_created ON stores (status, created_at DESC, id DESC);
CREATE INDEX ix_businesses_status_created ON businesses (status, created_at DESC, id DESC);

-- +goose Down
DROP INDEX ix_businesses_status_created;
DROP INDEX ix_stores_status_created;
ALTER TABLE stores DROP CONSTRAINT ck_stores_review_note, DROP COLUMN review_note;
DROP INDEX ix_leads_status_changed_by;
DROP INDEX ix_leads_status;
ALTER TABLE leads
  DROP CONSTRAINT fk_leads_status_changed_by,
  DROP CONSTRAINT ck_leads_status,
  DROP COLUMN version,
  DROP COLUMN status_changed_by,
  DROP COLUMN status_changed_at,
  DROP COLUMN status;
