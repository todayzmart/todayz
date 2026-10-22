-- support queries, generated into package supportdb (gen/sqlc/supportdb).
-- Only the work package that builds internal/support edits this folder (D-50).
-- sqlc needs at least one query per package: replace this placeholder with
-- the domain's first real query.

-- name: SupportPlaceholder :one
SELECT 1::int AS ok;
