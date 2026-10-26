-- Backend risk fixes: the field photos sweep (W3-06) claims its rows and
-- deletes their objects outside the row-lock transaction, and a photo whose
-- object it could not delete waits out a backoff instead of blocking the
-- rest.
--
-- sweep_retry_at: the sweep does not take the row before it. The claim sets
-- it a lease ahead (so a second sweep skips rows the first is deleting, and a
-- sweep that died leaves them to a later run); a failed delete sets it to the
-- backoff. NULL for a row the sweep never took.
-- sweep_attempts: the deletes that failed, for the backoff and the log.

-- +goose Up
ALTER TABLE business_documents
  ADD COLUMN sweep_retry_at timestamptz,
  ADD COLUMN sweep_attempts int NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE business_documents
  DROP COLUMN IF EXISTS sweep_attempts,
  DROP COLUMN IF EXISTS sweep_retry_at;
