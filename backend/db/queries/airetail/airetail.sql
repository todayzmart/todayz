-- airetail queries (W3-11 retailer AI: supplier bill, shelf photo, review
-- reply, banner copy), generated into package airetaildb (gen/sqlc/airetaildb).
-- Only the work package that builds internal/ai/{billscan,shelfscan,replydraft,
-- bannercopy,insighttext} edits this folder (D-50).

-- name: MatchStoreItems :many
-- The best store item for each line the model read off a supplier bill
-- (AI-R2): pg_trgm word similarity of the line's words against the item's
-- name, brand, variant and pack, at least min_score. A line with no item
-- that close is left out (the person picks the item), and so is a line whose
-- two best items score the same: the server never guesses between them.
WITH scored AS (
  SELECT n.ord::int AS ord, si.id,
         coalesce(si.local_name, p.name)::text AS name, p.variant,
         p.pack_size::text AS pack_size, p.unit, si.on_hand,
         coalesce(si.mrp_paise, p.mrp_paise) AS mrp_paise,
         word_similarity(lower(n.name), lower(concat_ws(' ', coalesce(si.local_name, p.name), b.name, p.variant, p.pack_size::text, p.unit)))::float8 AS score
  FROM unnest(@names::text[]) WITH ORDINALITY AS n(name, ord)
  JOIN store_items si ON si.store_id = @store_id
  JOIN products p ON p.id = si.product_id
  LEFT JOIN brands b ON b.id = p.brand_id
  WHERE word_similarity(lower(n.name), lower(concat_ws(' ', coalesce(si.local_name, p.name), b.name, p.variant, p.pack_size::text, p.unit))) >= @min_score::float8
), ranked AS (
  SELECT scored.*,
         row_number() OVER (PARTITION BY scored.ord ORDER BY scored.score DESC, scored.id) AS rn,
         lead(scored.score) OVER (PARTITION BY scored.ord ORDER BY scored.score DESC, scored.id) AS next_score
  FROM scored
)
SELECT ranked.ord, ranked.id, ranked.name, ranked.variant, ranked.pack_size, ranked.unit,
       ranked.on_hand, ranked.mrp_paise, ranked.score
FROM ranked
WHERE ranked.rn = 1 AND (ranked.next_score IS NULL OR ranked.next_score < ranked.score - 1e-9)
ORDER BY ranked.ord;

-- name: StoreItemsByIDs :many
-- The named items of the store as a bill review shows them now; an id of
-- another store is missing.
SELECT si.id, coalesce(si.local_name, p.name)::text AS name, p.variant,
       p.pack_size::text AS pack_size, p.unit, si.on_hand,
       coalesce(si.mrp_paise, p.mrp_paise) AS mrp_paise
FROM store_items si JOIN products p ON p.id = si.product_id
WHERE si.store_id = @store_id AND si.id = ANY(@ids::uuid[]);

-- name: MatchProducts :many
-- The best active master product for each product the model saw on a shelf
-- (AI-R3), as SearchProducts matches (name, variant and brand), at least
-- min_score; a tie between the two best is left out, as for a bill line.
-- listed is true when the store already has the product.
WITH scored AS (
  SELECT n.ord::int AS ord, p.id, p.name, b.name AS brand_name,
         p.variant, p.pack_size::text AS pack_size, p.unit, p.mrp_paise,
         EXISTS (SELECT 1 FROM store_items si WHERE si.product_id = p.id AND si.store_id = @store_id)::bool AS listed,
         word_similarity(lower(n.name), lower(concat_ws(' ', b.name, p.name, p.variant, p.pack_size::text, p.unit)))::float8 AS score
  FROM unnest(@names::text[]) WITH ORDINALITY AS n(name, ord)
  CROSS JOIN products p
  LEFT JOIN brands b ON b.id = p.brand_id
  WHERE p.status = 'active'
    AND word_similarity(lower(n.name), lower(concat_ws(' ', b.name, p.name, p.variant, p.pack_size::text, p.unit))) >= @min_score::float8
), ranked AS (
  SELECT scored.*,
         row_number() OVER (PARTITION BY scored.ord ORDER BY scored.score DESC, scored.id) AS rn,
         lead(scored.score) OVER (PARTITION BY scored.ord ORDER BY scored.score DESC, scored.id) AS next_score
  FROM scored
)
SELECT ranked.ord, ranked.id, ranked.name, ranked.brand_name, ranked.variant, ranked.pack_size,
       ranked.unit, ranked.mrp_paise, ranked.listed, ranked.score
FROM ranked
WHERE ranked.rn = 1 AND (ranked.next_score IS NULL OR ranked.next_score < ranked.score - 1e-9)
ORDER BY ranked.ord;

-- name: ProductsByIDs :many
-- Master products as a shelf review shows them now, with whether the store
-- already lists each.
SELECT p.id, p.name, b.name AS brand_name, p.variant, p.pack_size::text AS pack_size,
       p.unit, p.mrp_paise, p.status,
       EXISTS (SELECT 1 FROM store_items si WHERE si.product_id = p.id AND si.store_id = @store_id)::bool AS listed
FROM products p LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.id = ANY(@ids::uuid[]);

-- name: StoreLang :one
-- The shop's language for drafted text: the active owner's preferred
-- language (as the coach's cards), English when unset.
SELECT COALESCE((SELECT u.preferred_lang FROM stores s
                 JOIN staff st ON st.business_id = s.business_id AND st.role = 'owner' AND st.status = 'active'
                 JOIN users u ON u.id = st.user_id
                 WHERE s.id = @store_id LIMIT 1), 'en')::text AS lang;

-- name: ReviewForReply :one
-- One visible review of the store with whether it already has a reply. The
-- customer's name is never read: the draft sees only the stars, the tags and
-- the scrubbed text.
SELECT r.id, r.stars, r.text, r.tags,
       EXISTS (SELECT 1 FROM review_replies rr WHERE rr.review_id = r.id AND rr.deleted_at IS NULL)::bool AS replied
FROM reviews r
WHERE r.id = @id AND r.store_id = @store_id AND r.hidden_at IS NULL;

-- name: OfferForBanner :one
-- One offer of the store for banner copy (AI-R9).
SELECT o.id, o.title, o.kind, o.value, o.buy_qty, o.get_qty, o.min_bill_paise,
       o.scope, o.store_item_ids, o.status, o.ends_at
FROM offers o
WHERE o.id = @id AND o.store_id = @store_id;

-- name: ItemsForBanner :many
-- The store's items a banner may name, with their price and MRP.
SELECT si.id, coalesce(si.local_name, p.name)::text AS name, p.variant,
       p.pack_size::text AS pack_size, p.unit, si.price_paise,
       coalesce(si.mrp_paise, p.mrp_paise) AS mrp_paise, p.image_key
FROM store_items si JOIN products p ON p.id = si.product_id
WHERE si.store_id = @store_id AND si.id = ANY(@ids::uuid[])
ORDER BY name, si.id;

-- name: StoreName :one
SELECT name FROM stores WHERE id = @store_id;

-- name: InsightTextsForStore :many
-- The AI-R4 sentences kept for a store in one language; the caller keeps only
-- those whose facts_hash still matches the row.
SELECT kind, subject_ref, facts_hash, text
FROM insight_texts
WHERE store_id = @store_id AND lang = @lang;

-- name: UpsertInsightText :exec
INSERT INTO insight_texts (store_id, kind, subject_ref, lang, facts_hash, text, prompt_version, model, created_at)
VALUES (@store_id, @kind, @subject_ref, @lang, @facts_hash, @text, @prompt_version, @model, @now)
ON CONFLICT (store_id, kind, subject_ref, lang) DO UPDATE
SET facts_hash = EXCLUDED.facts_hash, text = EXCLUDED.text, prompt_version = EXCLUDED.prompt_version,
    model = EXCLUDED.model, created_at = EXCLUDED.created_at;

-- name: AiTextMisses :many
-- The negative cache of one background feature for a store in one language
-- (D-228): facts whose wording failed are not sent again while the hash holds.
SELECT ref, facts_hash
FROM ai_text_misses
WHERE feature = @feature AND store_id = @store_id AND lang = @lang;

-- name: AiTextMissed :one
-- Whether these facts of one ref already failed (the coach asks per card).
SELECT EXISTS (
  SELECT 1 FROM ai_text_misses
  WHERE feature = @feature AND store_id = @store_id AND ref = @ref AND lang = @lang AND facts_hash = @facts_hash
)::bool AS missed;

-- name: UpsertAiTextMiss :exec
INSERT INTO ai_text_misses (feature, store_id, ref, lang, facts_hash, reason, created_at)
VALUES (@feature, @store_id, @ref, @lang, @facts_hash, @reason, @now)
ON CONFLICT (feature, store_id, ref, lang) DO UPDATE
SET facts_hash = EXCLUDED.facts_hash, reason = EXCLUDED.reason, created_at = EXCLUDED.created_at;

-- name: LockStoreScans :exec
-- Serialises the start of a store's photo scans for the rest of the
-- transaction, so two creates cannot both pass the in-flight count.
SELECT pg_advisory_xact_lock(hashtextextended(@lock_key::text, 0));

-- name: ScansInFlight :one
-- A store's photo drafts still processing and not yet stale.
SELECT count(*)::int AS n
FROM ai_drafts
WHERE store_id = @store_id AND feature = ANY(@features::text[])
  AND status = 'processing' AND created_at > @since;
