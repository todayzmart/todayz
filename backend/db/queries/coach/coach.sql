-- coach queries, generated into package coachdb (gen/sqlc/coachdb).
-- Only W3-11 (internal/coach) edits this folder (D-50). Every timestamp and
-- day comes from the caller's injected clock.

-- Stores ---------------------------------------------------------------------

-- name: LiveStoresAfter :many
-- One batch of the stores the coach runs for: live stores of verified
-- businesses, in id order after the checkpoint.
SELECT s.id, s.business_id, s.name
FROM stores s
JOIN businesses b ON b.id = s.business_id
WHERE s.status = 'live' AND b.status = 'verified' AND s.id > sqlc.arg(after)::uuid
ORDER BY s.id
LIMIT sqlc.arg(lim);

-- name: StoreProfile :one
-- What the store-level detectors and the card writer need about one store:
-- the owner's language for the card text, the order switches, the stored
-- 30-day reliability rates, and how much of the setup is done.
SELECT s.id, s.business_id, s.name, s.status, s.auto_accept, s.accepts_delivery,
       s.unaccepted_expiry_rate_30d, s.rating_avg, s.rating_count,
       (s.location IS NOT NULL)::bool AS has_location,
       (s.hours <> '{}'::jsonb)::bool AS has_hours,
       COALESCE((SELECT u.preferred_lang FROM staff st JOIN users u ON u.id = st.user_id
                 WHERE st.business_id = s.business_id AND st.role = 'owner' AND st.status = 'active'
                 LIMIT 1), 'en')::text AS lang,
       (SELECT count(*) FROM store_items si WHERE si.store_id = s.id)::bigint AS items
FROM stores s
WHERE s.id = sqlc.arg(store_id);

-- Detector facts -------------------------------------------------------------

-- name: UnacceptedExpiriesByHour :many
-- Orders that expired because nobody accepted them, placed in [from, to),
-- counted per IST hour of placing (missed_orders_peak_hour).
SELECT extract(hour FROM (o.placed_at AT TIME ZONE 'Asia/Kolkata'))::int AS hour, count(*)::bigint AS n
FROM orders o
WHERE o.store_id = sqlc.arg(store_id) AND o.status = 'expired' AND o.status_reason = 'not_accepted'
  AND o.placed_at >= sqlc.arg(from_at) AND o.placed_at < sqlc.arg(to_at)
GROUP BY 1
ORDER BY 1;

-- name: OrderOutcomes :one
-- Orders placed in [from, to) and how many of them expired unanswered
-- (the expiry-penalty nudge reads the same 30-day rate the ranking uses).
SELECT count(*)::bigint AS orders,
       count(*) FILTER (WHERE o.status = 'expired' AND o.status_reason = 'not_accepted')::bigint AS unaccepted
FROM orders o
WHERE o.store_id = sqlc.arg(store_id) AND o.placed_at >= sqlc.arg(from_at) AND o.placed_at < sqlc.arg(to_at);

-- name: DailySales :many
-- The store's sales (online GMV plus POS) per IST day in [from_day, to_day],
-- from W3-07's rollup (best_day_record, weekly_sales_change).
SELECT r.date, (r.gmv_paise + r.pos_gmv_paise)::bigint AS sales_paise,
       (r.completed + r.pos_sales)::bigint AS bills,
       r.p50_accept_seconds
FROM rollup_daily_store r
WHERE r.store_id = sqlc.arg(store_id) AND r.date BETWEEN sqlc.arg(from_day)::date AND sqlc.arg(to_day)::date
ORDER BY r.date;

-- name: TopItemsOn :many
-- The store items that sold the most in [from, to) (online and POS), for
-- naming what led a record day.
SELECT x.store_item_id, COALESCE(si.local_name, p.name)::text AS name, sum(x.qty)::bigint AS qty
FROM (
  SELECT oi.store_item_id, COALESCE(oi.quantity_accepted, oi.quantity_requested)::bigint AS qty
  FROM order_items oi JOIN orders o ON o.id = oi.order_id
  WHERE o.store_id = sqlc.arg(store_id) AND o.status IN ('collected', 'delivered')
    AND o.placed_at >= sqlc.arg(from_at) AND o.placed_at < sqlc.arg(to_at)
  UNION ALL
  SELECT pl.store_item_id, pl.quantity::bigint AS qty
  FROM pos_sale_lines pl JOIN pos_sales ps ON ps.id = pl.sale_id
  WHERE ps.store_id = sqlc.arg(store_id) AND ps.status = 'completed'
    AND ps.sold_at >= sqlc.arg(from_at) AND ps.sold_at < sqlc.arg(to_at) AND pl.store_item_id IS NOT NULL
) x
JOIN store_items si ON si.id = x.store_item_id
JOIN products p ON p.id = si.product_id
WHERE x.qty > 0
GROUP BY x.store_item_id, si.local_name, p.name
ORDER BY qty DESC, name
LIMIT sqlc.arg(lim);

-- name: RatingWindows :one
-- Visible reviews' stars in the recent window [recent_from, to) and the
-- earlier window [earlier_from, recent_from) (rating_drop).
SELECT count(*) FILTER (WHERE r.created_at >= sqlc.arg(recent_from))::bigint AS recent_n,
       COALESCE(sum(r.stars) FILTER (WHERE r.created_at >= sqlc.arg(recent_from)), 0)::bigint AS recent_stars,
       count(*) FILTER (WHERE r.created_at < sqlc.arg(recent_from))::bigint AS earlier_n,
       COALESCE(sum(r.stars) FILTER (WHERE r.created_at < sqlc.arg(recent_from)), 0)::bigint AS earlier_stars
FROM reviews r
WHERE r.store_id = sqlc.arg(store_id) AND r.hidden_at IS NULL
  AND r.created_at >= sqlc.arg(earlier_from) AND r.created_at < sqlc.arg(to_at);

-- name: UnansweredReviews :one
-- Visible reviews with text in [from, to) that have no live reply.
SELECT count(*)::bigint AS n, COALESCE(min(r.stars), 0)::int AS lowest
FROM reviews r
LEFT JOIN review_replies rr ON rr.review_id = r.id AND rr.deleted_at IS NULL
WHERE r.store_id = sqlc.arg(store_id) AND r.hidden_at IS NULL AND r.text IS NOT NULL
  AND rr.review_id IS NULL AND r.created_at >= sqlc.arg(from_at) AND r.created_at < sqlc.arg(to_at);

-- name: NearbyAcceptMedian :one
-- The median of the other live stores' median accept time on one day within
-- radius_m of the store, and how many distinct businesses that is (D-71: a
-- coach statement needs at least 10).
SELECT count(DISTINCT o.business_id)::bigint AS businesses,
       COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY r.p50_accept_seconds), 0)::bigint AS median_seconds
FROM stores me
JOIN stores o ON o.id <> me.id AND o.status = 'live' AND o.business_id <> me.business_id
  AND o.location IS NOT NULL AND me.location IS NOT NULL
  AND ST_DWithin(o.location, me.location, sqlc.arg(radius_m)::float8)
JOIN rollup_daily_store r ON r.store_id = o.id AND r.date = sqlc.arg(day)::date AND r.p50_accept_seconds IS NOT NULL
WHERE me.id = sqlc.arg(store_id);

-- Cards ----------------------------------------------------------------------

-- name: InsertCard :one
-- A new card. A rerun that finds the same live (store, detector, subject)
-- card writes nothing (the partial unique index) and returns no row.
INSERT INTO coach_suggestions (id, store_id, detector, subject_ref, kind, topic, title, body, numbers,
  action_type, action_payload, priority, status, generated_by, ai_draft_id, prompt_version, model, lang,
  job, day, valid_until, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(store_id), sqlc.arg(detector), sqlc.arg(subject_ref), sqlc.arg(kind),
  sqlc.arg(topic), sqlc.arg(title), sqlc.arg(body), sqlc.arg(numbers), sqlc.arg(action_type),
  sqlc.arg(action_payload), sqlc.arg(priority), 'new', sqlc.arg(generated_by), sqlc.narg(ai_draft_id),
  sqlc.narg(prompt_version), sqlc.narg(model), sqlc.arg(lang), sqlc.arg(job), sqlc.arg(day),
  sqlc.arg(valid_until), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (store_id, detector, subject_ref) WHERE status IN ('new', 'seen') DO NOTHING
RETURNING id;

-- name: LiveCardKeys :many
-- The store's live cards (detector and subject), so the chooser does not
-- write a card that is already showing.
SELECT detector, subject_ref
FROM coach_suggestions
WHERE store_id = sqlc.arg(store_id) AND status IN ('new', 'seen');

-- name: QuietSubjects :many
-- The detector subjects the shop dismissed that are still quiet at now.
SELECT DISTINCT detector, subject_ref
FROM coach_suggestions
WHERE store_id = sqlc.arg(store_id) AND status = 'dismissed' AND quiet_until > sqlc.arg(now);

-- name: CardsCreatedOn :one
-- New cards already written for the store on one IST day (coach_max_cards_per_day).
SELECT count(*)::bigint FROM coach_suggestions WHERE store_id = sqlc.arg(store_id) AND day = sqlc.arg(day)::date;

-- name: PushesSince :one
-- Cards that went out as a push since the start of the IST day (coach_max_push_per_day).
SELECT count(*)::bigint FROM coach_suggestions
WHERE store_id = sqlc.arg(store_id) AND pushed_at >= sqlc.arg(since);

-- name: MarkPushed :execrows
UPDATE coach_suggestions SET pushed_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND pushed_at IS NULL;

-- name: ListCards :many
-- The feed: the store's cards with one of the statuses, newest first.
SELECT * FROM coach_suggestions
WHERE store_id = sqlc.arg(store_id) AND status = ANY(sqlc.arg(statuses)::text[])
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: GetCard :one
SELECT * FROM coach_suggestions WHERE id = sqlc.arg(id) AND store_id = sqlc.arg(store_id);

-- name: MarkSeen :exec
UPDATE coach_suggestions SET status = 'seen', seen_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE store_id = sqlc.arg(store_id) AND id = ANY(sqlc.arg(ids)::uuid[]) AND status = 'new';

-- name: ActCard :one
-- Done or Not useful on a live card. A card already acted on, expired or
-- another store's returns no row.
UPDATE coach_suggestions
SET status = sqlc.arg(status), acted_at = sqlc.arg(now), acted_by = sqlc.arg(by),
    quiet_until = sqlc.narg(quiet_until), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND store_id = sqlc.arg(store_id) AND status IN ('new', 'seen')
RETURNING *;

-- name: ExpireDue :many
-- Closes the store's live cards whose valid_until has passed; returns their
-- detectors so the ignored ones count against the detector's weight.
UPDATE coach_suggestions SET status = 'expired', updated_at = sqlc.arg(now)
WHERE store_id = sqlc.arg(store_id) AND status IN ('new', 'seen') AND valid_until <= sqlc.arg(now)
RETURNING detector;

-- name: ExpireNotFiring :many
-- Closes the live cards of the given detectors whose subject no longer fires
-- (cards expire when they stop being true, 11 §2.1).
UPDATE coach_suggestions SET status = 'expired', updated_at = sqlc.arg(now)
WHERE store_id = sqlc.arg(store_id) AND status IN ('new', 'seen')
  AND detector = ANY(sqlc.arg(detectors)::text[])
  AND NOT ((detector || '|' || subject_ref) = ANY(sqlc.arg(firing)::text[]))
RETURNING detector;

-- Weights --------------------------------------------------------------------

-- name: Weights :many
SELECT detector, weight_bp FROM coach_weights WHERE store_id = sqlc.arg(store_id);

-- name: BumpWeight :exec
-- One feedback event on a detector: delta_bp moves the weight inside
-- 2500–20000 and the matching counter goes up by one.
INSERT INTO coach_weights (store_id, detector, weight_bp, shown, done, dismissed, ignored, updated_at)
VALUES (sqlc.arg(store_id), sqlc.arg(detector), LEAST(GREATEST(10000 + sqlc.arg(delta_bp)::int, 2500), 20000),
  sqlc.arg(shown)::int, sqlc.arg(done)::int, sqlc.arg(dismissed)::int, sqlc.arg(ignored)::int, sqlc.arg(now))
ON CONFLICT (store_id, detector) DO UPDATE
SET weight_bp = LEAST(GREATEST(coach_weights.weight_bp + sqlc.arg(delta_bp)::int, 2500), 20000),
    shown = coach_weights.shown + EXCLUDED.shown,
    done = coach_weights.done + EXCLUDED.done,
    dismissed = coach_weights.dismissed + EXCLUDED.dismissed,
    ignored = coach_weights.ignored + EXCLUDED.ignored,
    updated_at = EXCLUDED.updated_at;

-- Insight text (AI-R4) -------------------------------------------------------

-- name: InsightTexts :many
-- The stored sentences of a store's rows in one language.
SELECT kind, subject_ref, facts_hash, text FROM insight_texts
WHERE store_id = sqlc.arg(store_id) AND lang = sqlc.arg(lang);

-- name: PutInsightText :exec
INSERT INTO insight_texts (store_id, kind, subject_ref, lang, facts_hash, text, prompt_version, model, created_at)
VALUES (sqlc.arg(store_id), sqlc.arg(kind), sqlc.arg(subject_ref), sqlc.arg(lang), sqlc.arg(facts_hash),
  sqlc.arg(text), sqlc.arg(prompt_version), sqlc.arg(model), sqlc.arg(now))
ON CONFLICT (store_id, kind, subject_ref, lang) DO UPDATE
SET facts_hash = EXCLUDED.facts_hash, text = EXCLUDED.text, prompt_version = EXCLUDED.prompt_version,
    model = EXCLUDED.model, created_at = EXCLUDED.created_at;
