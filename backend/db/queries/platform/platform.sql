-- Platform queries (W0-03), generated into package platformdb.

-- name: GetSetting :one
SELECT key, value, unit, min, max, version, updated_at
FROM platform_settings
WHERE key = $1;

-- name: CountOpenRiskFlags :one
SELECT count(*) FROM risk_flags WHERE status = 'open';

-- name: GetRiskFlag :one
SELECT id, subject_type, subject_id, rule, score, status, reviewed_at, created_at
FROM risk_flags
WHERE id = $1;
