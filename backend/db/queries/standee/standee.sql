-- standee queries, generated into package standeedb (gen/sqlc/standeedb).
-- W2-10 (the PDF) and W3-09 (standee rows, counts, reorders) edit this folder.
--
-- A scan is a referral_clicks row with this standee_id (D-162, D-223); a
-- sign-up is a referral from such a click that was not rejected; qualified
-- is one whose first qualifying purchase is done.

-- name: LockStore :one
-- The store's short code, locked so two creates of one store count in turn.
SELECT short_code FROM stores WHERE id = @store_id FOR NO KEY UPDATE;

-- name: StoreShortCode :one
SELECT short_code FROM stores WHERE id = @store_id;

-- name: CountStandees :one
SELECT count(*)::int AS n FROM qr_standees WHERE store_id = @store_id;

-- name: InsertStandee :one
INSERT INTO qr_standees (id, store_id, campaign, created_by, created_at, updated_at)
VALUES (@id, @store_id, @campaign, @created_by, @now, @now)
RETURNING *;

-- name: ListStandees :many
SELECT q.id, q.store_id, q.campaign, q.printed_at, q.reorder_requested_at, q.created_at, q.version,
       c.scans, c.last_scan_at, r.sign_ups, r.qualified
FROM qr_standees q
CROSS JOIN LATERAL (
  SELECT count(*)::int AS scans, max(rc.created_at) AS last_scan_at
  FROM referral_clicks rc WHERE rc.standee_id = q.id
) c
CROSS JOIN LATERAL (
  SELECT count(*) FILTER (WHERE rf.status NOT IN ('rejected', 'rejected_same_device'))::int AS sign_ups,
         count(*) FILTER (WHERE rf.status IN ('qualified', 'rewarded'))::int AS qualified
  FROM referrals rf WHERE rf.standee_id = q.id
) r
WHERE q.store_id = @store_id
ORDER BY q.created_at DESC, q.id DESC;

-- name: GetStandee :one
SELECT q.id, q.store_id, q.campaign, q.printed_at, q.reorder_requested_at, q.created_at, q.version,
       c.scans, c.last_scan_at, r.sign_ups, r.qualified
FROM qr_standees q
CROSS JOIN LATERAL (
  SELECT count(*)::int AS scans, max(rc.created_at) AS last_scan_at
  FROM referral_clicks rc WHERE rc.standee_id = q.id
) c
CROSS JOIN LATERAL (
  SELECT count(*) FILTER (WHERE rf.status NOT IN ('rejected', 'rejected_same_device'))::int AS sign_ups,
         count(*) FILTER (WHERE rf.status IN ('qualified', 'rewarded'))::int AS qualified
  FROM referrals rf WHERE rf.standee_id = q.id
) r
WHERE q.id = @id AND q.store_id = @store_id;

-- name: LockStandee :one
SELECT id, reorder_requested_at FROM qr_standees
WHERE id = @id AND store_id = @store_id
FOR NO KEY UPDATE;

-- name: RequestReorder :exec
UPDATE qr_standees
SET reorder_requested_at = @now::timestamptz, reorder_requested_by = @requested_by, updated_at = @now::timestamptz, version = version + 1
WHERE id = @id AND store_id = @store_id AND reorder_requested_at IS NULL;

-- name: MarkStoreSheetPrinted :exec
-- The first Print of the store sheet (D-281); a later one keeps the first time.
UPDATE stores SET standee_printed_at = @now
WHERE id = @store_id AND standee_printed_at IS NULL;
