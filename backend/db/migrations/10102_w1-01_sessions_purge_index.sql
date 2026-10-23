-- W1-01 review: identity.purge deletes a session once it has been revoked or
-- has expired for SessionRetention (PurgeSessions: coalesce(revoked_at,
-- expires_at) < before). The expression index replaces the plain expires_at
-- index, which only that query used.

-- +goose Up
DROP INDEX IF EXISTS ix_sessions_expires;
CREATE INDEX ix_sessions_purge ON sessions ((coalesce(revoked_at, expires_at)));

-- +goose Down
DROP INDEX IF EXISTS ix_sessions_purge;
CREATE INDEX ix_sessions_expires ON sessions (expires_at);
