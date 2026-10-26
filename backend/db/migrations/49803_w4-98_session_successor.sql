-- Refresh lost-response recovery (D-239). A refresh that rotates a token now
-- records, on the rotated row, the row it inserted (successor_id) and 32
-- random bytes (successor_salt). The successor's token is HMAC-SHA256 under
-- PII_HASH_KEY of the rotated token and the salt, so a retry with the rotated
-- token inside RotationGrace, while the successor has never been presented,
-- is answered with that same successor token (the response the browser
-- lost). Only the successor's SHA-256 is stored (refresh_hash), as before:
-- neither column is a token, and the salt alone gives nothing without the
-- rotated token and the key.
--
-- No foreign key on successor_id: identity_purge deletes rows on their own
-- schedule, and a dangling id only means "no replay" (the successor is gone).
-- Rows rotated before this migration, and rows the admin TOTP flows rotate,
-- have no successor: a retry of theirs keeps the plain grace 401.

-- +goose Up
ALTER TABLE sessions
  ADD COLUMN successor_id uuid,
  ADD COLUMN successor_salt bytea,
  ADD CONSTRAINT ck_sessions_successor CHECK ((successor_id IS NULL) = (successor_salt IS NULL)),
  ADD CONSTRAINT ck_sessions_successor_salt CHECK (successor_salt IS NULL OR octet_length(successor_salt) = 32);

-- +goose Down
ALTER TABLE sessions
  DROP CONSTRAINT IF EXISTS ck_sessions_successor_salt,
  DROP CONSTRAINT IF EXISTS ck_sessions_successor,
  DROP COLUMN IF EXISTS successor_salt,
  DROP COLUMN IF EXISTS successor_id;
