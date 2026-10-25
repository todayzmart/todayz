-- Field agents (W3-06; A-04, A-15, 01 §14, D-39). Same package as business.sql
-- (businessdb). businesses.onboarded_by is the agent who made the business.
-- The A-15 numbers read stores, store_items (catalogue) and orders (orders)
-- without writing them.

------------------------------------------------------------------------------
-- Businesses an agent onboards
------------------------------------------------------------------------------

-- name: InsertFieldBusiness :one
INSERT INTO businesses (id, brand_name, legal_name, category_id, referral_code, onboarded_by, created_at, updated_at)
VALUES (@id, @brand_name, sqlc.narg(legal_name), sqlc.narg(category_id), @referral_code, @onboarded_by, @now, @now)
RETURNING *;

-- name: ListFieldBusinesses :many
-- Newest first (ids are time-ordered ULIDs). agent_id narrows to one agent;
-- without it every business an agent onboarded (an admin's view).
SELECT * FROM businesses
WHERE onboarded_by IS NOT NULL
  AND (sqlc.narg(agent_id)::uuid IS NULL OR onboarded_by = sqlc.narg(agent_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(before_id)::uuid IS NULL OR id < sqlc.narg(before_id)::uuid)
ORDER BY id DESC
LIMIT @lim;

-- name: FieldStoreSummaries :many
-- The stores of some businesses with how many items each lists.
SELECT s.id, s.business_id, s.name, s.status, s.locality,
  (SELECT count(*) FROM store_items si WHERE si.store_id = s.id)::int AS item_count
FROM stores s
WHERE s.business_id = ANY(@business_ids::uuid[])
ORDER BY s.created_at, s.id;

-- name: FieldPhotoCounts :many
-- Shop photos (A-04) that are stored and not rejected, per business: a photo
-- whose upload was never confirmed is not counted.
SELECT business_id, count(*)::int AS photos FROM business_documents
WHERE business_id = ANY(@business_ids::uuid[]) AND kind IN ('shop_photo','storefront') AND status IN ('uploaded','accepted')
GROUP BY business_id;

-- name: CountFieldDocumentsOfKind :one
-- The cap on one kind (5) for the field routes: every row but the rejected
-- ones and the pending ones whose upload URL has expired unconfirmed.
SELECT count(*)::int FROM business_documents
WHERE business_id = @business_id AND kind = @kind AND status <> 'rejected'
  AND (status <> 'pending' OR created_at >= @stale_before);

-- name: FieldDocumentForUpdate :one
SELECT * FROM business_documents WHERE id = @id AND business_id = @business_id FOR UPDATE;

-- name: MarkDocumentUploaded :one
UPDATE business_documents SET status = 'uploaded', updated_at = @now::timestamptz
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: FieldOwners :many
-- The owner row of some businesses (invited until the owner confirms), with
-- the sealed phone it shows: the invite phone, or the account's once active.
SELECT s.id, s.business_id, s.status, s.user_id, s.invite_phone_enc, p.phone_enc AS user_phone_enc
FROM staff s
LEFT JOIN phone_identities p ON p.user_id = s.user_id
WHERE s.business_id = ANY(@business_ids::uuid[]) AND s.role = 'owner' AND s.status <> 'removed';

-- name: FieldOwnerForUpdate :one
SELECT * FROM staff WHERE business_id = @business_id AND role = 'owner' AND status <> 'removed' FOR UPDATE;

-- name: ReplaceOwnerInvite :exec
-- A new owner number before the owner confirmed (the invite clock restarts).
UPDATE staff SET invite_phone_hash = @invite_phone_hash, invite_phone_enc = @invite_phone_enc,
  invited_at = @now::timestamptz, version = version + 1
WHERE id = @id AND status = 'invited';

-- name: ActivateOwner :one
-- The owner confirmed with the code: the row becomes theirs.
UPDATE staff SET status = 'active', user_id = @user_id,
  invite_phone_hash = NULL, invite_phone_enc = NULL, version = version + 1
WHERE id = @id AND status = 'invited'
RETURNING *;

------------------------------------------------------------------------------
-- Owner codes
------------------------------------------------------------------------------

-- name: InsertOwnerCode :exec
INSERT INTO field_owner_codes (id, business_id, phone_hash, code_hash, expires_at, sent_by, created_at)
VALUES (@id, @business_id, @phone_hash, @code_hash, @expires_at, @sent_by, @now);

-- name: OwnerCodeSendsByPhone :one
-- The codes sent to one number since a time, owner codes and sign-in codes
-- together (01 §2 limits sends per number): how many, the oldest and the latest.
SELECT count(*)::int AS sends,
  COALESCE(min(created_at), 'epoch'::timestamptz)::timestamptz AS oldest,
  COALESCE(max(created_at), 'epoch'::timestamptz)::timestamptz AS latest
FROM (
  SELECT c.created_at FROM field_owner_codes c WHERE c.phone_hash = sqlc.arg(phone_hash)::bytea AND c.created_at >= sqlc.arg(since)::timestamptz
  UNION ALL
  SELECT o.created_at FROM otp_requests o WHERE o.phone_hash = sqlc.arg(phone_hash)::bytea AND o.created_at >= sqlc.arg(since)::timestamptz
) sends;

-- name: OwnerCodeSendsByAgent :one
-- The owner codes one agent sent since a time: how many and the oldest.
SELECT count(*)::int AS sends, COALESCE(min(created_at), 'epoch'::timestamptz)::timestamptz AS oldest
FROM field_owner_codes WHERE sent_by = @sent_by AND created_at >= @since;

-- name: AccountStatusByPhone :one
-- users.status of the account that holds a number (none when the number is new).
SELECT u.status FROM phone_identities p JOIN users u ON u.id = p.user_id WHERE p.phone_hash = @phone_hash;

-- name: LatestOwnerCodeForUpdate :one
SELECT * FROM field_owner_codes
WHERE business_id = @business_id AND consumed_at IS NULL
ORDER BY created_at DESC, id DESC LIMIT 1
FOR UPDATE;

-- name: RecordOwnerCodeAttempt :one
UPDATE field_owner_codes SET attempts = attempts + 1 WHERE id = @id RETURNING attempts;

-- name: ConsumeOwnerCode :exec
UPDATE field_owner_codes SET consumed_at = @now::timestamptz WHERE id = @id;

-- name: EndOwnerCodes :exec
-- A changed owner number ends every code still open for the business.
UPDATE field_owner_codes SET expires_at = LEAST(expires_at, @now::timestamptz)
WHERE business_id = @business_id AND consumed_at IS NULL;

------------------------------------------------------------------------------
-- Items an agent adds
------------------------------------------------------------------------------

-- name: FieldItemAddedBy :one
-- The agent who added a store item from the field routes (none for an item
-- the shop added itself).
SELECT agent_id FROM field_item_adds WHERE store_item_id = @store_item_id;

-- name: InsertFieldItemAdd :exec
INSERT INTO field_item_adds (store_item_id, business_id, store_id, agent_id, created_at)
VALUES (@store_item_id, @business_id, @store_id, @agent_id, @now);

------------------------------------------------------------------------------
-- A-15 numbers, per agent
------------------------------------------------------------------------------

-- name: FieldAgents :many
-- Every admin user with the field_agent role, by name.
SELECT a.user_id, u.name, a.disabled_at
FROM admin_users a JOIN users u ON u.id = a.user_id
WHERE 'field_agent' = ANY(a.roles)
ORDER BY u.name NULLS LAST, a.user_id;

-- name: UserName :one
SELECT name FROM users WHERE id = @id;

-- name: FieldStatusCounts :many
SELECT onboarded_by AS agent_id, status, count(*)::int AS n FROM businesses
WHERE onboarded_by = ANY(@agents::uuid[])
GROUP BY onboarded_by, status;

-- name: FieldStoresLive :many
SELECT b.onboarded_by AS agent_id, count(*)::int AS n
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE b.onboarded_by = ANY(@agents::uuid[]) AND s.status = 'live'
GROUP BY b.onboarded_by;

-- name: FieldItemsAdded :many
SELECT agent_id, count(*)::int AS n FROM field_item_adds
WHERE agent_id = ANY(@agents::uuid[])
GROUP BY agent_id;

-- name: FieldInWindow :many
-- Businesses an agent may still edit (D-39): onboarding, or verified after
-- verified_after (now minus field_agent_edit_window_days).
SELECT onboarded_by AS agent_id, count(*)::int AS n FROM businesses
WHERE onboarded_by = ANY(@agents::uuid[])
  AND (status IN ('draft','submitted','needs_info')
       OR (status = 'verified' AND verified_at >= @verified_after::timestamptz))
GROUP BY onboarded_by;

-- name: FieldFirstOrders :many
-- Verified businesses, and how many of them have a collected or delivered order.
SELECT b.onboarded_by AS agent_id, count(*)::int AS verified,
  count(*) FILTER (WHERE EXISTS (
    SELECT 1 FROM orders o JOIN stores s ON s.id = o.store_id
    WHERE s.business_id = b.id AND o.status IN ('collected','delivered')))::int AS with_order
FROM businesses b
WHERE b.onboarded_by = ANY(@agents::uuid[]) AND b.status = 'verified'
GROUP BY b.onboarded_by;

-- name: FieldWeekly :many
-- Businesses onboarded and verified and items added per IST week (Monday)
-- since a time.
SELECT agent_id, (date_trunc('week', at AT TIME ZONE 'Asia/Kolkata'))::date AS week_start, kind, count(*)::int AS n
FROM (
  SELECT onboarded_by AS agent_id, created_at AS at, 'onboarded'::text AS kind FROM businesses
  WHERE onboarded_by = ANY(@agents::uuid[]) AND created_at >= @since::timestamptz
  UNION ALL
  SELECT onboarded_by, verified_at, 'verified' FROM businesses
  WHERE onboarded_by = ANY(@agents::uuid[]) AND verified_at >= @since::timestamptz
  UNION ALL
  SELECT agent_id, created_at, 'items' FROM field_item_adds
  WHERE agent_id = ANY(@agents::uuid[]) AND created_at >= @since::timestamptz
) t
GROUP BY agent_id, week_start, kind;
