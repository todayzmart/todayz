-- banners queries, generated into package bannersdb (gen/sqlc/bannersdb).
-- Only W2-07 (internal/banners) edits this folder (D-50).

-- name: LockStore :one
-- Serialises the banner writes of one store so the store_banners_max count
-- holds under concurrent creates.
SELECT id FROM stores WHERE id = @id FOR UPDATE;

-- name: CountActiveBanners :one
-- Live or scheduled banners (the ones that count against store_banners_max).
SELECT count(*)::int FROM store_banners WHERE store_id = @store_id AND status IN ('live', 'scheduled');

-- name: InsertBanner :one
INSERT INTO store_banners (id, store_id, image_key, template, headline, target_type, target_id, target_item_ids,
                           starts_at, ends_at, status, moderation, created_by, created_at, updated_at)
VALUES (@id, @store_id, sqlc.narg('image_key'), sqlc.narg('template'), @headline, @target_type, sqlc.narg('target_id'),
        @target_item_ids::uuid[], @starts_at, sqlc.narg('ends_at'), @status, @moderation, sqlc.narg('created_by'), @now, @now)
RETURNING *;

-- name: GetBanner :one
SELECT * FROM store_banners WHERE id = @id AND store_id = @store_id;

-- name: LockBanner :one
SELECT * FROM store_banners WHERE id = @id AND store_id = @store_id FOR UPDATE;

-- name: ListBanners :many
SELECT * FROM store_banners
WHERE store_id = @store_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('after_id')::uuid IS NULL OR id < sqlc.narg('after_id')::uuid)
ORDER BY id DESC
LIMIT @lim;

-- name: UpdateBanner :one
UPDATE store_banners SET
  headline = @headline, template = sqlc.narg('template'), image_key = sqlc.narg('image_key'),
  target_type = @target_type, target_id = sqlc.narg('target_id'), target_item_ids = @target_item_ids::uuid[],
  starts_at = @starts_at, ends_at = sqlc.narg('ends_at'), status = @status, moderation = @moderation,
  version = version + 1, updated_at = @now
WHERE id = @id AND store_id = @store_id
RETURNING *;

-- name: RemoveBanner :one
-- An admin takedown (W3-05 calls this through the banners service).
UPDATE store_banners SET status = 'removed', removed_by = @removed_by, removed_at = @now, remove_reason = @reason,
  version = version + 1, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: BannerStats :many
SELECT banner_id, COALESCE(sum(impressions), 0)::bigint AS impressions, COALESCE(sum(clicks), 0)::bigint AS clicks,
       COALESCE(sum(attributed_orders), 0)::bigint AS attributed_orders
FROM banner_daily WHERE banner_id = ANY(@ids::uuid[]) GROUP BY banner_id;

-- name: OfferOfStore :one
SELECT id FROM offers WHERE id = @id AND store_id = @store_id AND status <> 'ended';

-- name: CategoryExists :one
SELECT EXISTS (SELECT 1 FROM categories WHERE id = @id)::bool;

-- name: ItemsOfStore :many
SELECT id FROM store_items WHERE store_id = @store_id AND id = ANY(@ids::uuid[]);

-- name: StoreForBanner :one
-- A banner's store when it is live and its business verified (the tap route).
SELECT s.id, s.slug, s.name
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE s.id = @id AND s.status = 'live' AND b.status = 'verified';

------------------------------------------------------------------------------
-- What customers see
------------------------------------------------------------------------------

-- name: LiveBannersForStores :many
-- Banners that show: live, cleared by moderation and inside their dates at time
-- at, in a stable order (the service rotates among them).
SELECT * FROM store_banners
WHERE store_id = ANY(@store_ids::uuid[])
  AND status IN ('live', 'scheduled')
  AND moderation = 'auto_ok'
  AND starts_at <= @at
  AND (ends_at IS NULL OR ends_at > @at)
ORDER BY store_id, id;

-- name: PublicBanner :one
-- One banner of a live store as customers may see it.
SELECT sb.* FROM store_banners sb
JOIN stores s ON s.id = sb.store_id
JOIN businesses b ON b.id = s.business_id
WHERE sb.id = @id AND s.status = 'live' AND b.status = 'verified'
  AND sb.status IN ('live', 'scheduled') AND sb.moderation = 'auto_ok'
  AND sb.starts_at <= @at AND (sb.ends_at IS NULL OR sb.ends_at > @at);

-- name: RecordTap :exec
-- The customer's last tap per banner (D-77: last click wins at attribution).
INSERT INTO banner_taps (customer_id, banner_id, store_id, tapped_at)
VALUES (@customer_id, @banner_id, @store_id, @tapped_at)
ON CONFLICT (customer_id, banner_id) DO UPDATE SET tapped_at = EXCLUDED.tapped_at;

------------------------------------------------------------------------------
-- Scheduling (internal/jobs/banners.go)
------------------------------------------------------------------------------

-- name: StartDueBanners :many
UPDATE store_banners SET status = 'live', version = version + 1, updated_at = @now
WHERE status = 'scheduled' AND moderation = 'auto_ok' AND starts_at <= @now AND (ends_at IS NULL OR ends_at > @now)
RETURNING id, store_id;

-- name: EndDueBanners :many
UPDATE store_banners SET status = 'ended', version = version + 1, updated_at = @now
WHERE status IN ('live', 'scheduled', 'pending_review') AND ends_at IS NOT NULL AND ends_at <= @now
RETURNING id, store_id;

-- name: InsertServerEvent :exec
INSERT INTO events (id, name, occurred_at, app, user_id, props)
VALUES (@id, @name, @occurred_at, 'api', @user_id, @props)
ON CONFLICT (id, occurred_at) DO NOTHING;
