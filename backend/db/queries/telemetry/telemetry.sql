-- telemetry queries, generated into package telemetrydb (gen/sqlc/telemetrydb).
-- Only the work package that builds internal/telemetry edits this folder (D-50).

-- name: InsertEvent :execrows
-- One event from POST /v1/events (the handler inserts a batch in one transaction). An empty geo
-- cell, app version or platform is stored as NULL. A repeated event id (a retried batch) is skipped.
INSERT INTO events (id, name, occurred_at, app, user_id, anon_id, session_id, props, geo_cell, app_version, platform)
VALUES (@id, @name, @occurred_at, @app, @user_id, @anon_id, @session_id, @props, NULLIF(@geo_cell::text, ''),
        NULLIF(@app_version::text, ''), NULLIF(@platform::text, ''))
ON CONFLICT (id, occurred_at) DO NOTHING;

-- name: SlowQuery :one
-- Backs GET /v1/test/slow-sql: pg_sleep for the given seconds (local and test only).
SELECT pg_sleep(@seconds::float8) IS NOT NULL AS slept;
