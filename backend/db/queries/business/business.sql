-- business queries, generated into package businessdb (gen/sqlc/businessdb).
-- Only W1-04 (internal/business) edits this folder (D-50). Every time the code
-- compares or records is a parameter from the application clock, never now().
-- Locations go in and out as (lat, lng) float8 pairs through PostGIS.

------------------------------------------------------------------------------
-- Locks
------------------------------------------------------------------------------

-- name: LockKey :exec
-- Serialises work on one key (a lead's details hash) until the transaction ends.
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

------------------------------------------------------------------------------
-- Businesses
------------------------------------------------------------------------------

-- name: InsertBusiness :one
INSERT INTO businesses (id, brand_name, legal_name, category_id, referral_code, created_at, updated_at)
VALUES (@id, @brand_name, sqlc.narg(legal_name), sqlc.narg(category_id), @referral_code, @now, @now)
RETURNING *;

-- name: GetBusiness :one
SELECT * FROM businesses WHERE id = @id;

-- name: UpdateBusiness :one
-- Only the fields whose set_* flag is true change; a stale version matches no row.
UPDATE businesses SET
  brand_name  = CASE WHEN @set_brand_name::bool THEN @brand_name::text ELSE brand_name END,
  legal_name  = CASE WHEN @set_legal_name::bool THEN sqlc.narg(legal_name)::text ELSE legal_name END,
  category_id = CASE WHEN @set_category_id::bool THEN sqlc.narg(category_id)::uuid ELSE category_id END,
  version = version + 1
WHERE id = @id AND version = @version
RETURNING *;

-- name: BusinessStatusForShare :one
-- The business's status, held until the transaction ends so an admin
-- rejection or suspension cannot slip in under a store or KYC change.
SELECT status FROM businesses WHERE id = @id FOR SHARE;

-- name: SubmitBusiness :one
-- draft or needs_info → submitted (01 §3); the request for information is cleared.
UPDATE businesses SET status = 'submitted', submitted_at = @now::timestamptz,
  needs_info_fields = NULL, needs_info_note = NULL, version = version + 1
WHERE id = @id AND status IN ('draft','needs_info')
RETURNING *;

-- name: ListMyBusinesses :many
-- The businesses the user is active staff of, with their role and store scope.
SELECT b.id, b.brand_name, b.status, s.id AS staff_id, s.role, s.store_ids
FROM staff s JOIN businesses b ON b.id = s.business_id
WHERE s.user_id = @user_id AND s.status = 'active'
ORDER BY b.created_at, b.id;

-- name: ListStoresForBusinesses :many
SELECT id, business_id, name, slug, status FROM stores
WHERE business_id = ANY(@business_ids::uuid[])
ORDER BY created_at, id;

-- name: ListMyInvitations :many
-- Pending invites to this user: by user id, or by the phone they signed in with.
-- An invite sent at or before @expired_before has lapsed and is not listed.
SELECT s.id, s.business_id, s.role, b.brand_name, s.created_at
FROM staff s JOIN businesses b ON b.id = s.business_id
WHERE s.status = 'invited' AND s.invited_at > @expired_before::timestamptz
  AND (s.user_id = @user_id
       OR (s.user_id IS NULL AND s.invite_phone_hash = (SELECT p.phone_hash FROM phone_identities p WHERE p.user_id = @user_id)))
ORDER BY s.created_at, s.id;

-- name: CountSubmitStores :many
-- The stores a submit checks (01 §3: store name, address and pin, contact mobile, hours).
SELECT id, name, address_line1, city, pincode, (location IS NOT NULL)::bool AS has_location,
  (phone_enc IS NOT NULL)::bool AS has_phone, hours
FROM stores WHERE business_id = @business_id ORDER BY created_at, id;

------------------------------------------------------------------------------
-- KYC
------------------------------------------------------------------------------

-- name: InsertKyc :exec
INSERT INTO business_kyc (business_id, created_at, updated_at) VALUES (@business_id, @now, @now)
ON CONFLICT (business_id) DO NOTHING;

-- name: GetKyc :one
SELECT * FROM business_kyc WHERE business_id = @business_id;

-- name: UpdateKyc :one
UPDATE business_kyc SET
  pan_enc = sqlc.narg(pan_enc), pan_last4 = sqlc.narg(pan_last4),
  gstin_enc = sqlc.narg(gstin_enc), gstin_last4 = sqlc.narg(gstin_last4),
  upi_id_enc = sqlc.narg(upi_id_enc), upi_id_last4 = sqlc.narg(upi_id_last4),
  bank_account_enc = sqlc.narg(bank_account_enc), bank_account_last4 = sqlc.narg(bank_account_last4),
  bank_ifsc = sqlc.narg(bank_ifsc), legal_name = sqlc.narg(legal_name),
  status = @status, verified_by = NULL, verified_at = NULL,
  version = version + 1
WHERE business_id = @business_id AND version = @version
RETURNING *;

------------------------------------------------------------------------------
-- Documents
------------------------------------------------------------------------------

-- name: InsertDocument :one
INSERT INTO business_documents (id, business_id, store_id, kind, object_key, content_type, size_bytes, created_at, updated_at)
VALUES (@id, @business_id, sqlc.narg(store_id), @kind, @object_key, @content_type, @size_bytes, @now, @now)
RETURNING *;

-- name: CountDocumentsOfKind :one
-- The documents of one kind a business holds that are not rejected (pending
-- ones count, so unconfirmed upload URLs cannot pile up).
SELECT count(*)::int FROM business_documents
WHERE business_id = @business_id AND kind = @kind AND status <> 'rejected';

-- name: ListDocuments :many
SELECT * FROM business_documents WHERE business_id = @business_id
ORDER BY created_at DESC, id DESC LIMIT @max_rows;

------------------------------------------------------------------------------
-- Stores
------------------------------------------------------------------------------

-- name: InsertStore :exec
INSERT INTO stores (id, business_id, slug, short_code, name, address_line1, address_line2, landmark,
  locality, city, pincode, location, phone_enc, hours, created_at, updated_at)
VALUES (@id, @business_id, @slug, @short_code, @name, sqlc.narg(address_line1), sqlc.narg(address_line2),
  sqlc.narg(landmark), sqlc.narg(locality), sqlc.narg(city), sqlc.narg(pincode),
  CASE WHEN sqlc.narg(lat)::float8 IS NULL THEN NULL
       ELSE ST_SetSRID(ST_MakePoint(sqlc.narg(lng)::float8, sqlc.narg(lat)::float8), 4326)::geography END,
  sqlc.narg(phone_enc), @hours, @now, @now);

-- name: GetStore :one
SELECT id, business_id, slug, short_code, name, address_line1, address_line2, landmark, locality, city,
  pincode, COALESCE(ST_Y(location::geometry), 0)::float8 AS lat, COALESCE(ST_X(location::geometry), 0)::float8 AS lng,
  (location IS NOT NULL)::bool AS has_location, phone_enc, hours, closed_until, status, live_at,
  accepts_pickup, accepts_delivery, asks_muted, delivery_radius_m, delivery_fee_paise,
  delivery_free_above_paise, delivery_min_order_paise, delivery_hours, cashback_bp, auto_accept,
  created_at, updated_at, version
FROM stores WHERE id = @id;

-- name: ListStores :many
SELECT id, business_id, slug, short_code, name, address_line1, address_line2, landmark, locality, city,
  pincode, COALESCE(ST_Y(location::geometry), 0)::float8 AS lat, COALESCE(ST_X(location::geometry), 0)::float8 AS lng,
  (location IS NOT NULL)::bool AS has_location, phone_enc, hours, closed_until, status, live_at,
  accepts_pickup, accepts_delivery, asks_muted, delivery_radius_m, delivery_fee_paise,
  delivery_free_above_paise, delivery_min_order_paise, delivery_hours, cashback_bp, auto_accept,
  created_at, updated_at, version
FROM stores WHERE business_id = @business_id ORDER BY created_at, id;

-- name: StoreBusiness :one
SELECT business_id FROM stores WHERE id = @id;

-- name: SlugTaken :one
SELECT EXISTS (SELECT 1 FROM stores WHERE slug = @slug)::bool;

-- name: UpdateStoreProfile :execrows
UPDATE stores SET
  name          = CASE WHEN @set_name::bool THEN @name::text ELSE name END,
  address_line1 = CASE WHEN @set_address_line1::bool THEN sqlc.narg(address_line1)::text ELSE address_line1 END,
  address_line2 = CASE WHEN @set_address_line2::bool THEN sqlc.narg(address_line2)::text ELSE address_line2 END,
  landmark      = CASE WHEN @set_landmark::bool THEN sqlc.narg(landmark)::text ELSE landmark END,
  locality      = CASE WHEN @set_locality::bool THEN sqlc.narg(locality)::text ELSE locality END,
  city          = CASE WHEN @set_city::bool THEN sqlc.narg(city)::text ELSE city END,
  pincode       = CASE WHEN @set_pincode::bool THEN sqlc.narg(pincode)::text ELSE pincode END,
  location      = CASE WHEN NOT @set_location::bool THEN location
                       WHEN sqlc.narg(lat)::float8 IS NULL THEN NULL
                       ELSE ST_SetSRID(ST_MakePoint(sqlc.narg(lng)::float8, sqlc.narg(lat)::float8), 4326)::geography END,
  phone_enc     = CASE WHEN @set_phone::bool THEN sqlc.narg(phone_enc)::bytea ELSE phone_enc END,
  -- A changed name, address, location or phone sends a store customers can
  -- see (or one that was approved and paused) back to review (D-40).
  status        = CASE WHEN @needs_review::bool AND status IN ('live','paused') THEN 'pending_review' ELSE status END,
  version = version + 1
WHERE id = @id AND business_id = @business_id AND version = @version;

-- name: SubmitStore :execrows
-- draft → pending_review (D-40).
UPDATE stores SET status = 'pending_review', version = version + 1
WHERE id = @id AND business_id = @business_id AND status = 'draft';

-- name: UpdateStoreSettings :execrows
UPDATE stores SET
  hours = @hours, closed_until = sqlc.narg(closed_until), accepts_pickup = @accepts_pickup,
  accepts_delivery = @accepts_delivery, asks_muted = @asks_muted, delivery_radius_m = @delivery_radius_m,
  delivery_fee_paise = @delivery_fee_paise, delivery_free_above_paise = sqlc.narg(delivery_free_above_paise),
  delivery_min_order_paise = @delivery_min_order_paise, delivery_hours = sqlc.narg(delivery_hours),
  cashback_bp = @cashback_bp, auto_accept = @auto_accept,
  version = version + 1
WHERE id = @id AND business_id = @business_id AND version = @version;

------------------------------------------------------------------------------
-- Staff
------------------------------------------------------------------------------

-- name: Membership :one
-- Active staff only (authz.StaffResolver).
SELECT role, store_ids FROM staff
WHERE user_id = @user_id AND business_id = @business_id AND status = 'active';

-- name: InsertStaff :one
-- invited_at is set for an invite and NULL for an owner made active at once.
INSERT INTO staff (id, business_id, user_id, role, store_ids, can_deliver, invited_by, status,
  invite_phone_hash, invite_phone_enc, invited_at, created_at, updated_at)
VALUES (@id, @business_id, sqlc.narg(user_id), @role, sqlc.narg(store_ids), @can_deliver, sqlc.narg(invited_by),
  @status, sqlc.narg(invite_phone_hash), sqlc.narg(invite_phone_enc), sqlc.narg(invited_at), @now, @now)
RETURNING *;

-- name: ListStaff :many
-- Everyone but removed rows, with the name and sealed phone of the account.
SELECT s.*, u.name AS user_name, p.phone_enc AS user_phone_enc
FROM staff s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN phone_identities p ON p.user_id = s.user_id
WHERE s.business_id = @business_id AND s.status <> 'removed'
ORDER BY s.created_at, s.id;

-- name: GetStaffForUpdate :one
SELECT * FROM staff WHERE id = @id AND business_id = @business_id FOR UPDATE;

-- name: GetStaffByUser :one
SELECT * FROM staff WHERE business_id = @business_id AND user_id = @user_id FOR UPDATE;

-- name: GetInviteByPhone :one
SELECT * FROM staff
WHERE business_id = @business_id AND invite_phone_hash = @phone_hash AND status = 'invited'
FOR UPDATE;

-- name: GetStaffView :one
SELECT s.*, u.name AS user_name, p.phone_enc AS user_phone_enc
FROM staff s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN phone_identities p ON p.user_id = s.user_id
WHERE s.id = @id AND s.business_id = @business_id;

-- name: ReinviteStaff :one
-- A removed or lapsed row taken up again by a new invite; the expiry clock restarts.
UPDATE staff SET user_id = COALESCE(user_id, sqlc.narg(user_id)::uuid), role = @role,
  store_ids = sqlc.narg(store_ids), can_deliver = @can_deliver, invited_by = @invited_by,
  status = 'invited', removed_at = NULL, invited_at = @now::timestamptz,
  invite_phone_hash = @invite_phone_hash, invite_phone_enc = @invite_phone_enc, version = version + 1
WHERE id = @id
RETURNING *;

-- name: UpdateStaff :one
UPDATE staff SET
  role        = CASE WHEN @set_role::bool THEN @role::text ELSE role END,
  store_ids   = CASE WHEN @set_store_ids::bool THEN sqlc.narg(store_ids)::uuid[] ELSE store_ids END,
  can_deliver = CASE WHEN @set_can_deliver::bool THEN @can_deliver::bool ELSE can_deliver END,
  version = version + 1
WHERE id = @id
RETURNING *;

-- name: RemoveStaff :exec
-- The invite phone goes with the row (a removed invite keeps no phone).
UPDATE staff SET status = 'removed', removed_at = @now::timestamptz,
  invite_phone_hash = NULL, invite_phone_enc = NULL, version = version + 1
WHERE id = @id;

-- name: AcceptInvite :one
UPDATE staff SET status = 'active', user_id = @user_id,
  invite_phone_hash = NULL, invite_phone_enc = NULL, version = version + 1
WHERE id = @id AND status = 'invited' AND invited_at > @expired_before::timestamptz
RETURNING *;

-- name: UserIDByPhoneHash :one
SELECT user_id FROM phone_identities WHERE phone_hash = @phone_hash;

-- name: PhoneHashByUser :one
SELECT phone_hash FROM phone_identities WHERE user_id = @user_id;

-- name: StoresOfBusiness :many
SELECT id FROM stores WHERE business_id = @business_id;

------------------------------------------------------------------------------
-- Leads (04 §2.5)
------------------------------------------------------------------------------

-- name: RecentLeadExists :one
SELECT EXISTS (SELECT 1 FROM leads WHERE details_hash = @details_hash AND created_at > @since::timestamptz)::bool;

-- name: InsertLead :exec
INSERT INTO leads (id, name, phone_enc, shop_name, city, source, details_hash, created_at)
VALUES (@id, @name, @phone_enc, @shop_name, @city, @source, @details_hash, @now);

------------------------------------------------------------------------------
-- Account deletion (W1-01 phase B asks before deleting an account)
------------------------------------------------------------------------------

-- name: RemoveInvitesForAccount :execrows
-- Account deletion: the pending invites bound to the user, or to the phone they
-- signed in with, are withdrawn and lose the phone. Running it twice is a no-op.
UPDATE staff SET status = 'removed', removed_at = @now::timestamptz,
  invite_phone_hash = NULL, invite_phone_enc = NULL, version = version + 1
WHERE status = 'invited'
  AND (user_id = @user_id OR (sqlc.narg(phone_hash)::bytea IS NOT NULL AND invite_phone_hash = sqlc.narg(phone_hash)::bytea));

-- name: RemoveMembershipsForAccount :execrows
-- Account deletion (D-107): the user's manager and cashier rows end with the
-- account. Owner rows are left alone: OwnsActiveBusiness refuses the deletion
-- of an active owner, and a rejected business's owner row stays as a record.
UPDATE staff SET status = 'removed', removed_at = @now::timestamptz, version = version + 1
WHERE user_id = @user_id AND role <> 'owner' AND status = 'active';

-- name: OwnsActiveBusiness :one
-- The user is the active owner of a business that is not rejected. A draft
-- counts: its stores, staff and documents would be left without an owner.
SELECT EXISTS (
  SELECT 1 FROM staff s JOIN businesses b ON b.id = s.business_id
  WHERE s.user_id = @user_id AND s.role = 'owner' AND s.status = 'active' AND b.status <> 'rejected'
)::bool;
