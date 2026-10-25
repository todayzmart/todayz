-- admin queries, generated into package admindb (gen/sqlc/admindb).
-- Only the work package that builds internal/admin edits this folder (D-50).

------------------------------------------------------------------------------
-- customers (A-06)
------------------------------------------------------------------------------

-- name: CustomerByPhoneHash :one
SELECT u.id, u.name, u.status, u.created_at, pi.phone_enc,
  (SELECT count(*) FROM strikes s
   WHERE s.subject_type = 'customer' AND s.subject_id = u.id AND s.reset_at IS NULL)::int AS active_strikes
FROM phone_identities pi JOIN users u ON u.id = pi.user_id
WHERE pi.phone_hash = @phone_hash;

-- name: GetCustomer :one
SELECT u.id, u.name, u.status, u.preferred_lang, u.created_at, u.version, pi.phone_enc,
  (SELECT count(*) FROM strikes s
   WHERE s.subject_type = 'customer' AND s.subject_id = u.id AND s.reset_at IS NULL)::int AS active_strikes,
  (au.user_id IS NOT NULL AND au.disabled_at IS NULL)::bool AS is_admin,
  (cp.user_id IS NOT NULL)::bool AS is_customer
FROM users u
  LEFT JOIN phone_identities pi ON pi.user_id = u.id
  LEFT JOIN admin_users au ON au.user_id = u.id
  LEFT JOIN customer_profiles cp ON cp.user_id = u.id
WHERE u.id = @id;

-- name: LockCustomer :one
-- Lock order is the users row before its sessions (identity's refresh does the same).
-- is_admin and is_customer are read under the same lock, so a guard on them holds
-- for the rest of the transaction.
SELECT u.status,
  (au.user_id IS NOT NULL AND au.disabled_at IS NULL)::bool AS is_admin,
  (cp.user_id IS NOT NULL)::bool AS is_customer
FROM users u
  LEFT JOIN admin_users au ON au.user_id = u.id
  LEFT JOIN customer_profiles cp ON cp.user_id = u.id
WHERE u.id = @id
FOR UPDATE OF u;

-- name: SetCustomerStatus :exec
UPDATE users SET status = @status, updated_at = @now, version = version + 1 WHERE id = @id;

-- name: RevokeUserSessions :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = @reason::text
WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeUserSession :execrows
UPDATE sessions SET revoked_at = @now::timestamptz, revoked_reason = 'admin'
WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL AND expires_at > @now::timestamptz;

-- name: CustomerStrikes :many
SELECT id, reason, order_id, created_at, reset_at, reset_reason
FROM strikes
WHERE subject_type = 'customer' AND subject_id = @user_id
ORDER BY created_at DESC, id DESC
LIMIT 50;

-- name: ResetStrikes :many
UPDATE strikes SET reset_at = @now, reset_by = @by, reset_reason = @reason
WHERE subject_type = @subject_type AND subject_id = @subject_id AND reset_at IS NULL
RETURNING id;

-- name: LiveSessions :many
SELECT id, app, user_agent, created_at, last_used_at, expires_at
FROM sessions
WHERE user_id = @user_id AND revoked_at IS NULL AND expires_at > @now::timestamptz
ORDER BY last_used_at DESC, id DESC
LIMIT 50;

-- name: CustomerRecentOrders :many
SELECT o.id, o.display_number, o.store_id, st.name AS store_name, o.status, o.total_paise, o.placed_at
FROM orders o JOIN stores st ON st.id = o.store_id
WHERE o.customer_id = @customer_id
ORDER BY o.placed_at DESC, o.id DESC
LIMIT 20;

------------------------------------------------------------------------------
-- audit (A-19 and the side panels)
------------------------------------------------------------------------------

-- name: ListAudit :many
SELECT a.id, a.actor_kind, a.actor_id, u.name AS actor_name, a.action, a.subject_type, a.subject_id,
  a.before, a.after, a.created_at
FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
WHERE (sqlc.narg(actor_id)::uuid IS NULL OR a.actor_id = sqlc.narg(actor_id)::uuid)
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action)::text)
  AND (sqlc.narg(subject_type)::text IS NULL OR a.subject_type = sqlc.narg(subject_type)::text)
  AND (sqlc.narg(subject_id)::text IS NULL OR a.subject_id = sqlc.narg(subject_id)::text)
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR a.created_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR a.created_at < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (a.created_at, a.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT @lim;

------------------------------------------------------------------------------
-- platform settings (A-17)
------------------------------------------------------------------------------

-- name: ListSettings :many
SELECT ps.key, ps.value, ps.unit, ps.min, ps.max, ps.description, ps.updated_by, u.name AS updated_by_name,
  ps.updated_at, ps.version
FROM platform_settings ps LEFT JOIN users u ON u.id = ps.updated_by
ORDER BY ps.key;

-- name: GetSettingForUpdate :one
SELECT key, value, unit, min, max, version FROM platform_settings WHERE key = @key FOR UPDATE;

-- name: UpdateSetting :exec
-- touch_updated_at sets updated_at; the NOTIFY trigger reloads every settings cache.
UPDATE platform_settings SET value = @value, updated_by = @updated_by, version = version + 1
WHERE key = @key;

-- name: GetSetting :one
SELECT ps.key, ps.value, ps.unit, ps.min, ps.max, ps.description, ps.updated_by, u.name AS updated_by_name,
  ps.updated_at, ps.version
FROM platform_settings ps LEFT JOIN users u ON u.id = ps.updated_by
WHERE ps.key = @key;

-- name: SettingExists :one
SELECT EXISTS (SELECT 1 FROM platform_settings WHERE key = @key)::bool;

------------------------------------------------------------------------------
-- businesses (A-03)
------------------------------------------------------------------------------

-- name: ListBusinesses :many
-- The queue reads oldest submitted first in the UI by tab; the list itself is
-- newest first like every admin list. city is the first store's.
SELECT b.id, b.brand_name, b.legal_name, b.status, b.submitted_at, b.created_at,
  (SELECT count(*) FROM stores s WHERE s.business_id = b.id)::int AS store_count,
  COALESCE((SELECT s.city FROM stores s WHERE s.business_id = b.id ORDER BY s.created_at, s.id LIMIT 1), '')::text AS city
FROM businesses b
WHERE (sqlc.narg(status)::text IS NULL OR b.status = sqlc.narg(status)::text)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (b.created_at, b.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY b.created_at DESC, b.id DESC
LIMIT @lim;

-- name: GetBusiness :one
SELECT id, brand_name, legal_name, status, needs_info_fields, needs_info_note, submitted_at, verified_at, created_at
FROM businesses WHERE id = @id;

-- name: LockBusiness :one
SELECT status FROM businesses WHERE id = @id FOR UPDATE;

-- name: VerifyBusiness :exec
UPDATE businesses SET status = 'verified', verified_at = @now, verified_by = @by,
  needs_info_fields = NULL, needs_info_note = NULL, version = version + 1
WHERE id = @id;

-- name: GoLiveBusinessStores :many
-- A verify approves the business's stores already waiting for review; drafts stay drafts.
UPDATE stores SET status = 'live', live_at = COALESCE(live_at, @now), reviewed_by = @by, reviewed_at = @now,
  review_note = NULL, version = version + 1
WHERE business_id = @business_id AND status = 'pending_review'
RETURNING id;

-- name: RequestBusinessInfo :exec
UPDATE businesses SET status = 'needs_info', needs_info_fields = @fields::text[], needs_info_note = @note,
  version = version + 1
WHERE id = @id;

-- name: SetBusinessStatus :exec
UPDATE businesses SET status = @status, version = version + 1 WHERE id = @id;

-- name: BusinessKyc :one
SELECT pan_last4, gstin_last4, upi_id_last4, bank_account_last4, bank_ifsc, legal_name, status, notes, verified_at
FROM business_kyc WHERE business_id = @business_id;

-- name: LockBusinessKyc :one
SELECT status FROM business_kyc WHERE business_id = @business_id FOR UPDATE;

-- name: BusinessKycSealed :one
SELECT pan_enc, gstin_enc, upi_id_enc, bank_account_enc FROM business_kyc WHERE business_id = @business_id;

-- name: ReviewBusinessKyc :exec
UPDATE business_kyc SET status = @status, notes = sqlc.narg(notes),
  verified_by = CASE WHEN @status = 'verified' THEN @by::uuid END,
  verified_at = CASE WHEN @status = 'verified' THEN @now::timestamptz END,
  version = version + 1
WHERE business_id = @business_id;

-- name: BusinessStores :many
SELECT id, name, status, locality, city, live_at, created_at
FROM stores WHERE business_id = @business_id
ORDER BY created_at, id
LIMIT 100;

-- name: BusinessStaff :many
-- A joined member's phone is their identity's; an invite's is the one typed.
SELECT st.id, st.role, st.status, u.name, pi.phone_enc AS user_phone_enc, st.invite_phone_enc
FROM staff st
  LEFT JOIN users u ON u.id = st.user_id
  LEFT JOIN phone_identities pi ON pi.user_id = st.user_id
WHERE st.business_id = @business_id AND st.status <> 'removed'
ORDER BY st.created_at, st.id
LIMIT 100;

-- name: BusinessDocuments :many
SELECT id, kind, content_type, size_bytes, store_id, status, reject_reason, created_at, reviewed_at
FROM business_documents WHERE business_id = @business_id
ORDER BY created_at DESC, id DESC
LIMIT 100;

-- name: LockBusinessDocument :one
SELECT status FROM business_documents WHERE id = @id AND business_id = @business_id FOR UPDATE;

-- name: ReviewBusinessDocument :exec
UPDATE business_documents SET status = @status, reject_reason = sqlc.narg(reject_reason),
  reviewed_by = @by, reviewed_at = @now, version = version + 1
WHERE id = @id;

------------------------------------------------------------------------------
-- stores (A-05)
------------------------------------------------------------------------------

-- name: ListStores :many
SELECT s.id, s.name, s.business_id, b.brand_name AS business_name, b.status AS business_status, s.status,
  s.locality, s.city, s.closed_until, (s.reliability_score IS NOT NULL)::bool AS has_reliability,
  COALESCE(s.reliability_score, 0)::float8 AS reliability_score, s.created_at,
  (SELECT count(*) FROM strikes k
   WHERE k.subject_type = 'store' AND k.subject_id = s.id AND k.reset_at IS NULL)::int AS active_strikes
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE (sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status)::text)
  AND (sqlc.narg(city)::text IS NULL OR lower(s.city) = lower(sqlc.narg(city)::text))
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (s.created_at, s.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY s.created_at DESC, s.id DESC
LIMIT @lim;

-- name: GetStore :one
SELECT s.id, s.name, s.business_id, b.brand_name AS business_name, b.status AS business_status, s.status,
  s.address_line1, s.address_line2, s.landmark, s.locality, s.city, s.pincode,
  (s.location IS NOT NULL)::bool AS has_location,
  COALESCE(ST_Y(s.location::geometry), 0)::float8 AS lat, COALESCE(ST_X(s.location::geometry), 0)::float8 AS lng,
  s.hours, s.closed_until, s.review_note, s.live_at, s.reviewed_at,
  (s.rating_avg IS NOT NULL)::bool AS has_rating, COALESCE(s.rating_avg, 0)::float8 AS rating_avg, s.rating_count,
  (s.accept_rate_30d IS NOT NULL)::bool AS has_accept_rate, COALESCE(s.accept_rate_30d, 0)::float8 AS accept_rate_30d,
  (s.unaccepted_expiry_rate_30d IS NOT NULL)::bool AS has_expiry_rate,
  COALESCE(s.unaccepted_expiry_rate_30d, 0)::float8 AS unaccepted_expiry_rate_30d,
  (s.reliability_score IS NOT NULL)::bool AS has_reliability, COALESCE(s.reliability_score, 0)::float8 AS reliability_score,
  s.reliability_computed_at, s.created_at,
  (SELECT count(*) FROM strikes k
   WHERE k.subject_type = 'store' AND k.subject_id = s.id AND k.reset_at IS NULL)::int AS active_strikes
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE s.id = @id;

-- name: LockStore :one
-- Lock order is the business before its store (a verify moves stores).
SELECT s.status, s.closed_until, s.hours, b.status AS business_status, (s.location IS NOT NULL)::bool AS has_location,
  COALESCE(ST_Y(s.location::geometry), 0)::float8 AS lat, COALESCE(ST_X(s.location::geometry), 0)::float8 AS lng
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE s.id = @id
FOR UPDATE OF b, s;

-- name: ApproveStore :exec
UPDATE stores SET status = 'live', live_at = COALESCE(live_at, @now), reviewed_by = @by, reviewed_at = @now,
  review_note = NULL, version = version + 1
WHERE id = @id;

-- name: SendBackStore :exec
UPDATE stores SET status = 'draft', reviewed_by = @by, reviewed_at = @now, review_note = @note, version = version + 1
WHERE id = @id;

-- name: SetStorePin :exec
UPDATE stores SET location = ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, version = version + 1
WHERE id = @id;

-- name: SetStoreHours :exec
UPDATE stores SET hours = @hours, version = version + 1 WHERE id = @id;

-- name: SetStoreClosedUntil :exec
UPDATE stores SET closed_until = sqlc.narg(closed_until), version = version + 1 WHERE id = @id;

-- name: StoreStrikes :many
SELECT id, reason, order_id, created_at, reset_at, reset_reason
FROM strikes
WHERE subject_type = 'store' AND subject_id = @store_id
ORDER BY created_at DESC, id DESC
LIMIT 50;

------------------------------------------------------------------------------
-- leads (A-03 Leads tab)
------------------------------------------------------------------------------

-- name: ListLeads :many
SELECT id, name, phone_enc, shop_name, city, source, status, status_changed_at, created_at
FROM leads
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: LockLead :one
SELECT id, name, phone_enc, shop_name, city, source, status, status_changed_at, created_at
FROM leads WHERE id = @id FOR UPDATE;

-- name: SetLeadStatus :one
UPDATE leads SET status = @status, status_changed_at = @now, status_changed_by = @by, version = version + 1
WHERE id = @id
RETURNING id, name, phone_enc, shop_name, city, source, status, status_changed_at, created_at;

------------------------------------------------------------------------------
-- master catalogue (A-08)
------------------------------------------------------------------------------

-- name: ListProducts :many
-- Newest first. q is matched as an escaped ILIKE pattern over the name,
-- variant and brand; barcode is an exact match.
SELECT p.id, p.name, b.name AS brand_name, p.category_id, (c.names ->> 'en')::text AS category_name,
  p.variant, p.pack_size::float8 AS pack_size, p.unit, p.mrp_paise, p.barcodes, p.status, p.merged_into,
  (SELECT count(*) FROM store_items si WHERE si.product_id = p.id)::int AS store_count, p.created_at, p.updated_at
FROM products p JOIN categories c ON c.id = p.category_id LEFT JOIN brands b ON b.id = p.brand_id
WHERE (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status)::text)
  AND (sqlc.narg(category_id)::uuid IS NULL OR p.category_id = sqlc.narg(category_id)::uuid)
  AND (sqlc.narg(barcode)::text IS NULL OR p.barcodes @> ARRAY[sqlc.narg(barcode)::text])
  AND (sqlc.narg(pattern)::text IS NULL
       OR p.name ILIKE sqlc.narg(pattern)::text
       OR p.variant ILIKE sqlc.narg(pattern)::text
       OR b.name ILIKE sqlc.narg(pattern)::text)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (p.created_at, p.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT @lim;

-- name: GetProduct :one
SELECT p.id, p.name, b.name AS brand_name, p.category_id, (c.names ->> 'en')::text AS category_name,
  p.variant, p.pack_size::float8 AS pack_size, p.unit, p.mrp_paise, p.barcodes, p.status, p.merged_into,
  (SELECT count(*) FROM store_items si WHERE si.product_id = p.id)::int AS store_count, p.created_at, p.updated_at
FROM products p JOIN categories c ON c.id = p.category_id LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.id = @id;

-- name: LockProduct :one
SELECT id, name, category_id, variant, pack_size::float8 AS pack_size, unit, mrp_paise, barcodes, status, merged_into
FROM products WHERE id = @id FOR UPDATE;

-- name: LockProductBarcodes :exec
-- Serialises product writes that set barcodes, so the "barcode in use" check
-- below cannot race (barcodes carry no unique index).
SELECT pg_advisory_xact_lock(hashtext('admin.products.barcodes'));

-- name: BarcodeTaken :one
-- A barcode is taken by a product that is active or waiting for review.
SELECT EXISTS (
  SELECT 1 FROM products
  WHERE barcodes && @barcodes::text[] AND id <> @id AND status IN ('active', 'pending_review')
)::bool;

-- name: CategoryParent :one
SELECT parent_id FROM categories WHERE id = @id;

-- name: InsertProduct :exec
INSERT INTO products (id, name, category_id, variant, pack_size, unit, mrp_paise, barcodes, status, created_at, updated_at)
VALUES (@id, @name, @category_id, sqlc.narg(variant), @pack_size::float8, @unit, sqlc.narg(mrp_paise), @barcodes::text[],
  'active', @now, @now);

-- name: UpdateProduct :exec
UPDATE products SET name = @name, category_id = @category_id, variant = sqlc.narg(variant),
  pack_size = @pack_size::float8, unit = @unit, mrp_paise = sqlc.narg(mrp_paise), barcodes = @barcodes::text[],
  status = @status, updated_at = @now, version = version + 1
WHERE id = @id;

-- name: RefreshStoreItemSearch :exec
-- store_items.search copies the product's words at insert (catalogue's
-- InsertStoreItem); a master edit refreshes it the same way.
UPDATE store_items si
SET search = p.search || setweight(to_tsvector('simple'::regconfig, coalesce(b.name, '')), 'B')
                      || setweight(to_tsvector('simple'::regconfig, coalesce(si.local_name, '')), 'A')
FROM products p LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.id = @product_id AND si.product_id = p.id;

-- name: ListPendingProducts :many
-- The review queue, oldest first (keyset after_at/after_id).
SELECT p.id, p.name, b.name AS brand_name, p.category_id, (c.names ->> 'en')::text AS category_name,
  p.variant, p.pack_size::float8 AS pack_size, p.unit, p.mrp_paise, p.barcodes, p.status, p.merged_into,
  (SELECT count(*) FROM store_items si WHERE si.product_id = p.id)::int AS store_count, p.created_at, p.updated_at,
  p.created_by_store_id, s.name AS created_by_store_name
FROM products p JOIN categories c ON c.id = p.category_id LEFT JOIN brands b ON b.id = p.brand_id
LEFT JOIN stores s ON s.id = p.created_by_store_id
WHERE p.status = 'pending_review'
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (p.created_at, p.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY p.created_at, p.id
LIMIT @lim;

-- name: ProductSuggestions :many
-- Active products whose names look like name (pg_trgm), closest first.
SELECT p.id, p.name, b.name AS brand_name, p.category_id, (c.names ->> 'en')::text AS category_name,
  p.variant, p.pack_size::float8 AS pack_size, p.unit, p.mrp_paise, p.barcodes, p.status, p.merged_into,
  (SELECT count(*) FROM store_items si WHERE si.product_id = p.id)::int AS store_count, p.created_at, p.updated_at,
  similarity(p.name, @name::text)::float8 AS similarity
FROM products p JOIN categories c ON c.id = p.category_id LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.status = 'active' AND p.id <> @id AND p.name % @name::text
ORDER BY similarity(p.name, @name::text) DESC, p.id
LIMIT 5;

-- name: SetProductStatus :exec
UPDATE products SET status = @status, merged_into = sqlc.narg(merged_into), updated_at = @now, version = version + 1
WHERE id = @id;

-- name: ProductListed :one
SELECT EXISTS (SELECT 1 FROM store_items WHERE product_id = @product_id)::bool;

-- name: DropDuplicateSynonyms :execrows
-- Before a merge moves a product's synonyms, drop the ones the target
-- already has (the pair index would refuse them).
DELETE FROM product_synonyms f
WHERE f.product_id = @from_id
  AND EXISTS (SELECT 1 FROM product_synonyms t
              WHERE t.product_id = @into_id AND t.term = f.term AND t.synonym = f.synonym);

-- name: MoveSynonyms :execrows
UPDATE product_synonyms SET product_id = @into_id WHERE product_id = @from_id;

-- name: ListCategories :many
-- Top-level categories first, then children, each by sort.
SELECT c.id, c.parent_id, c.slug, c.names, c.tint, c.icon, c.sort, c.updated_at,
  (SELECT count(*) FROM products p WHERE p.category_id = c.id AND p.status = 'active')::int AS product_count
FROM categories c
ORDER BY (c.parent_id IS NOT NULL), c.sort, c.slug;

-- name: GetCategory :one
SELECT c.id, c.parent_id, c.slug, c.names, c.tint, c.icon, c.sort, c.updated_at,
  (SELECT count(*) FROM products p WHERE p.category_id = c.id AND p.status = 'active')::int AS product_count
FROM categories c WHERE c.id = @id;

-- name: LockCategory :one
SELECT id, names, tint, icon, sort FROM categories WHERE id = @id FOR UPDATE;

-- name: InsertCategory :exec
INSERT INTO categories (id, parent_id, slug, names, tint, icon, sort, created_at, updated_at)
VALUES (@id, sqlc.narg(parent_id), @slug, @names, @tint, @icon, @sort, @now, @now);

-- name: UpdateCategory :exec
UPDATE categories SET names = @names, tint = @tint, icon = @icon, sort = @sort, updated_at = @now
WHERE id = @id;

-- name: ListSynonyms :many
-- Newest first; prefix matches the start of the term or the synonym.
SELECT ps.id, ps.term, ps.synonym, ps.lang, ps.product_id, p.name AS product_name, ps.created_at
FROM product_synonyms ps LEFT JOIN products p ON p.id = ps.product_id
WHERE (sqlc.narg(prefix)::text IS NULL OR ps.term LIKE sqlc.narg(prefix)::text OR ps.synonym LIKE sqlc.narg(prefix)::text)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (ps.created_at, ps.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY ps.created_at DESC, ps.id DESC
LIMIT @lim;

-- name: GetSynonym :one
SELECT ps.id, ps.term, ps.synonym, ps.lang, ps.product_id, p.name AS product_name, ps.created_at
FROM product_synonyms ps LEFT JOIN products p ON p.id = ps.product_id
WHERE ps.id = @id;

-- name: ProductStatus :one
SELECT status FROM products WHERE id = @id;

-- name: InsertSynonym :exec
INSERT INTO product_synonyms (id, term, synonym, lang, product_id, created_at)
VALUES (@id, @term, @synonym, @lang, sqlc.narg(product_id), @now);

-- name: DeleteSynonym :one
DELETE FROM product_synonyms WHERE id = @id
RETURNING term, synonym, lang, product_id;

-- A-20 banner moderation (W3-05). The takedown goes through the banners
-- service (it owns the removal and the shop's event); the approve is here.

-- name: ListAdminBanners :many
-- review: waiting uploads, oldest first. live: cleared live, scheduled or paused,
-- newest first. removed: taken down, newest first. Keyset on (created_at, id).
SELECT sb.id, sb.store_id, s.name AS store_name, s.city AS store_city, sb.headline, sb.image_key, sb.template,
  sb.target_type, cardinality(sb.target_item_ids)::int AS target_item_count,
  COALESCE(CASE sb.target_type WHEN 'offer' THEN o.title WHEN 'category' THEN (c.names ->> 'en') END, '')::text AS target_label,
  sb.starts_at, sb.ends_at, sb.status, sb.moderation, sb.remove_reason, sb.removed_at, sb.created_at, sb.updated_at
FROM store_banners sb JOIN stores s ON s.id = sb.store_id
LEFT JOIN offers o ON sb.target_type = 'offer' AND o.id = sb.target_id
LEFT JOIN categories c ON sb.target_type = 'category' AND c.id = sb.target_id
WHERE CASE @state::text
    WHEN 'review' THEN sb.status = 'pending_review' AND sb.moderation = 'needs_review'
    WHEN 'live' THEN sb.status IN ('live', 'scheduled', 'paused') AND sb.moderation = 'auto_ok'
    ELSE sb.status = 'removed' END
  AND (sqlc.narg(store_id)::uuid IS NULL OR sb.store_id = sqlc.narg(store_id)::uuid)
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (@state::text = 'review' AND (sb.created_at, sb.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
       OR (@state::text <> 'review' AND (sb.created_at, sb.id) < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid)))
ORDER BY CASE WHEN @state::text = 'review' THEN sb.created_at END,
  CASE WHEN @state::text = 'review' THEN sb.id END,
  sb.created_at DESC, sb.id DESC
LIMIT @lim;

-- name: GetAdminBanner :one
SELECT sb.id, sb.store_id, s.name AS store_name, s.city AS store_city, sb.headline, sb.image_key, sb.template,
  sb.target_type, cardinality(sb.target_item_ids)::int AS target_item_count,
  COALESCE(CASE sb.target_type WHEN 'offer' THEN o.title WHEN 'category' THEN (c.names ->> 'en') END, '')::text AS target_label,
  sb.starts_at, sb.ends_at, sb.status, sb.moderation, sb.remove_reason, sb.removed_at, sb.created_at, sb.updated_at
FROM store_banners sb JOIN stores s ON s.id = sb.store_id
LEFT JOIN offers o ON sb.target_type = 'offer' AND o.id = sb.target_id
LEFT JOIN categories c ON sb.target_type = 'category' AND c.id = sb.target_id
WHERE sb.id = @id;

-- name: LockBanner :one
SELECT id, status, moderation, starts_at, ends_at FROM store_banners WHERE id = @id FOR UPDATE;

-- name: ClearBanner :execrows
-- A person cleared a held upload: it shows from now on, like an automatic pass.
UPDATE store_banners SET moderation = 'auto_ok', status = @status, version = version + 1, updated_at = @now
WHERE id = @id AND status = 'pending_review' AND moderation = 'needs_review';
