-- reviews queries, generated into package reviewsdb (gen/sqlc/reviewsdb).
-- Only W2-06 (internal/reviews) edits this folder (D-50). A hidden review is out of every
-- customer-facing read and out of the rating; the customer's own list is the one exception
-- (it says `hidden`). stores.rating_avg and rating_count are written only here (D-59).

-- name: GetOrderForReview :one
-- The one read of another domain's table: who the order belongs to and whether it is done.
SELECT id, store_id, customer_id, status FROM orders WHERE id = @id;

-- name: LockStoreReviews :exec
-- Serialises review writes of one store so the rating recompute sees every committed change.
SELECT pg_advisory_xact_lock(hashtextextended('reviews:' || sqlc.arg(store_key)::text, 0));

-- name: InsertReview :one
INSERT INTO reviews (id, store_id, customer_id, order_id, pos_sale_id, stars, text, tags, edited_until, created_at, updated_at)
VALUES (@id, @store_id, @customer_id, sqlc.narg('order_id'), sqlc.narg('pos_sale_id'), @stars, sqlc.narg('text'), @tags, @edited_until, @now, @now)
ON CONFLICT DO NOTHING
RETURNING id;

-- name: LockMyReview :one
SELECT id, store_id, stars, text, tags, edited_until, hidden_at
FROM reviews
WHERE id = @id AND customer_id = @customer_id
FOR UPDATE;

-- name: UpdateMyReview :exec
UPDATE reviews
SET stars = @stars, text = sqlc.narg('text'), tags = @tags, edited_at = @now, updated_at = @now
WHERE id = @id;

-- name: GetMyReview :one
SELECT r.id, r.store_id, s.name AS store_name, r.order_id, r.pos_sale_id, r.stars, r.text, r.tags,
       r.created_at, r.edited_at, r.edited_until, r.hidden_at,
       p.text AS reply_text, p.created_at AS reply_created_at, p.edited_at AS reply_edited_at, p.deleted_at AS reply_deleted_at
FROM reviews r
JOIN stores s ON s.id = r.store_id
LEFT JOIN review_replies p ON p.review_id = r.id
WHERE r.id = @id AND r.customer_id = @customer_id;

-- name: ListMyReviews :many
SELECT r.id, r.store_id, s.name AS store_name, r.order_id, r.pos_sale_id, r.stars, r.text, r.tags,
       r.created_at, r.edited_at, r.edited_until, r.hidden_at,
       p.text AS reply_text, p.created_at AS reply_created_at, p.edited_at AS reply_edited_at, p.deleted_at AS reply_deleted_at
FROM reviews r
JOIN stores s ON s.id = r.store_id
LEFT JOIN review_replies p ON p.review_id = r.id
WHERE r.customer_id = @customer_id
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (r.created_at, r.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim;

-- name: GetLiveStoreBySlug :one
-- Only a live store of a verified business has a public page (SR-01).
SELECT s.id, s.rating_avg, s.rating_count
FROM stores s JOIN businesses b ON b.id = s.business_id
WHERE s.slug = @slug AND s.status = 'live' AND b.status = 'verified';

-- name: GetStoreRating :one
SELECT rating_avg, rating_count FROM stores WHERE id = @id;

-- name: ListStoreReviews :many
-- Visible reviews of one store with the reviewer's name and the visible reply. @reporting_store is
-- the store whose business app asks (it fills `reported`); the zero uuid for the public page.
SELECT r.id, r.stars, r.text, r.tags, u.name AS customer_name, r.created_at, r.edited_at,
       p.text AS reply_text, p.created_at AS reply_created_at, p.edited_at AS reply_edited_at,
       EXISTS (SELECT 1 FROM review_reports x WHERE x.review_id = r.id AND x.reporter_store_id = @reporting_store) AS reported
FROM reviews r
JOIN users u ON u.id = r.customer_id
LEFT JOIN review_replies p ON p.review_id = r.id AND p.deleted_at IS NULL
WHERE r.store_id = @store_id AND r.hidden_at IS NULL
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (r.created_at, r.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim;

-- name: GetReviewForReport :one
-- A review a reporter may see: visible. customer_id lets the service refuse a report of your own review.
SELECT id, store_id, customer_id FROM reviews WHERE id = @id AND hidden_at IS NULL;

-- name: InsertReviewReport :one
INSERT INTO review_reports (id, review_id, reporter_user_id, reporter_store_id, reason, status, created_at)
VALUES (@id, @review_id, @reporter_user_id, sqlc.narg('reporter_store_id'), @reason, 'open', @now)
ON CONFLICT (review_id, reporter_user_id) DO NOTHING
RETURNING id, review_id, status, created_at;

-- name: GetReviewReportByReporter :one
SELECT id, review_id, status, created_at FROM review_reports WHERE review_id = @review_id AND reporter_user_id = @reporter_user_id;

-- name: GetStoreReviewForReply :one
-- A visible review of this store (the reply routes name the store in the path).
SELECT id FROM reviews WHERE id = @id AND store_id = @store_id AND hidden_at IS NULL;

-- name: InsertReply :one
INSERT INTO review_replies (review_id, author_user_id, text, created_at, updated_at)
VALUES (@review_id, @author_user_id, @text, @now, @now)
ON CONFLICT (review_id) DO NOTHING
RETURNING text, created_at, edited_at;

-- name: LockReply :one
SELECT text, created_at, edited_at, deleted_at FROM review_replies WHERE review_id = @review_id FOR UPDATE;

-- name: UpdateReply :one
UPDATE review_replies SET text = @text, edited_at = @now, updated_at = @now
WHERE review_id = @review_id
RETURNING text, created_at, edited_at;

-- name: LockReviewForModeration :one
SELECT id, store_id, hidden_at, hide_reason FROM reviews WHERE id = @id FOR UPDATE;

-- name: HideReview :exec
UPDATE reviews SET hidden_at = @now, hidden_by = @actor, hide_reason = @reason, updated_at = @now WHERE id = @id;

-- name: RestoreReview :exec
UPDATE reviews SET hidden_at = NULL, hidden_by = NULL, hide_reason = NULL, updated_at = @now WHERE id = @id;

-- name: HideReply :execrows
UPDATE review_replies SET deleted_at = @now, deleted_by = @actor, delete_reason = @reason, updated_at = @now
WHERE review_id = @review_id AND deleted_at IS NULL;

-- name: RestoreReply :execrows
UPDATE review_replies SET deleted_at = NULL, deleted_by = NULL, delete_reason = NULL, updated_at = @now
WHERE review_id = @review_id AND deleted_at IS NOT NULL;

-- name: UpholdOpenReports :execrows
-- Hiding a review closes its open reports as upheld.
UPDATE review_reports
SET status = 'resolved', resolution = 'upheld', resolution_note = 'review hidden', resolved_by = @actor, resolved_at = @now
WHERE review_id = @review_id AND status = 'open';

-- name: GetAdminReview :one
SELECT r.id, r.store_id, s.name AS store_name, r.customer_id, r.order_id, r.pos_sale_id, r.stars, r.text, r.tags,
       r.created_at, r.hidden_at, r.hide_reason,
       (SELECT count(*) FROM review_reports x WHERE x.review_id = r.id AND x.status = 'open')::int AS open_reports,
       p.text AS reply_text, p.created_at AS reply_created_at, p.deleted_at AS reply_deleted_at, p.delete_reason AS reply_delete_reason
FROM reviews r
JOIN stores s ON s.id = r.store_id
LEFT JOIN review_replies p ON p.review_id = r.id
WHERE r.id = @id;

-- name: ListAdminReviews :many
SELECT r.id, r.store_id, s.name AS store_name, r.customer_id, r.order_id, r.pos_sale_id, r.stars, r.text, r.tags,
       r.created_at, r.hidden_at, r.hide_reason,
       (SELECT count(*) FROM review_reports x WHERE x.review_id = r.id AND x.status = 'open')::int AS open_reports,
       p.text AS reply_text, p.created_at AS reply_created_at, p.deleted_at AS reply_deleted_at, p.delete_reason AS reply_delete_reason
FROM reviews r
JOIN stores s ON s.id = r.store_id
LEFT JOIN review_replies p ON p.review_id = r.id
WHERE (sqlc.narg('store_id')::uuid IS NULL OR r.store_id = sqlc.narg('store_id')::uuid)
  AND (@state::text = ''
       OR (@state::text = 'visible' AND r.hidden_at IS NULL)
       OR (@state::text = 'hidden' AND r.hidden_at IS NOT NULL)
       OR (@state::text = 'reported' AND r.hidden_at IS NULL
           AND EXISTS (SELECT 1 FROM review_reports x WHERE x.review_id = r.id AND x.status = 'open')))
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (r.created_at, r.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim;

-- name: ListAdminReportsOpen :many
-- The open queue, oldest first.
SELECT id, review_id, (reporter_store_id IS NOT NULL)::boolean AS from_store, reason, status, resolution, resolution_note, created_at, resolved_at
FROM review_reports
WHERE status = 'open'
  AND (sqlc.narg('after_at')::timestamptz IS NULL OR (created_at, id) > (sqlc.narg('after_at')::timestamptz, sqlc.narg('after_id')::uuid))
ORDER BY created_at, id
LIMIT @lim;

-- name: ListAdminReportsResolved :many
-- Resolved reports, newest first.
SELECT id, review_id, (reporter_store_id IS NOT NULL)::boolean AS from_store, reason, status, resolution, resolution_note, created_at, resolved_at
FROM review_reports
WHERE status = 'resolved'
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (created_at, id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: LockReport :one
SELECT id, review_id, status FROM review_reports WHERE id = @id FOR UPDATE;

-- name: ResolveReport :one
UPDATE review_reports
SET status = 'resolved', resolution = @resolution, resolution_note = sqlc.narg('note'), resolved_by = @actor, resolved_at = @now
WHERE id = @id
RETURNING id, review_id, (reporter_store_id IS NOT NULL)::boolean AS from_store, reason, status, resolution, resolution_note, created_at, resolved_at;

-- name: RecomputeStoreRating :execrows
-- Bayesian average (prior @prior_avg with weight @prior_weight, 01 §10) over visible reviews; NULL
-- with none. rating_avg is numeric(3,2).
WITH prior AS (
  SELECT sqlc.arg(prior_weight)::int::numeric AS w, sqlc.arg(prior_avg)::text::numeric AS a
), agg AS (
  SELECT count(*)::int AS n, COALESCE(sum(stars), 0)::numeric AS total
  FROM reviews WHERE store_id = @store_id AND hidden_at IS NULL
)
UPDATE stores s
SET rating_count = agg.n,
    rating_avg = CASE WHEN agg.n = 0 THEN NULL ELSE round((prior.w * prior.a + agg.total) / (prior.w + agg.n), 2) END
FROM agg, prior
WHERE s.id = @store_id;

-- name: RecomputeAllStoreRatings :execrows
-- The hourly heal: every store that has a review or a stale rating; rows already right are not touched.
WITH prior AS (
  SELECT sqlc.arg(prior_weight)::int::numeric AS w, sqlc.arg(prior_avg)::text::numeric AS a
), agg AS (
  SELECT s.id, count(r.id)::int AS n, COALESCE(sum(r.stars), 0)::numeric AS total
  FROM stores s LEFT JOIN reviews r ON r.store_id = s.id AND r.hidden_at IS NULL
  WHERE s.rating_count > 0 OR s.rating_avg IS NOT NULL OR EXISTS (SELECT 1 FROM reviews x WHERE x.store_id = s.id)
  GROUP BY s.id
), calc AS (
  SELECT agg.id, agg.n,
         CASE WHEN agg.n = 0 THEN NULL ELSE round((prior.w * prior.a + agg.total) / (prior.w + agg.n), 2) END AS avg
  FROM agg, prior
)
UPDATE stores s
SET rating_count = calc.n, rating_avg = calc.avg
FROM calc
WHERE s.id = calc.id AND (s.rating_count IS DISTINCT FROM calc.n OR s.rating_avg IS DISTINCT FROM calc.avg);
