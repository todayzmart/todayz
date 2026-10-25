-- voice queries, generated into package voicedb (gen/sqlc/voicedb).
-- Only the work package that builds internal/voice edits this folder (D-50).

-- name: Candidates :many
-- The store's items the model may match (02 §8 step 3): the most sold in the
-- window first, then by name. Unlisted items are candidates too (a count or
-- a restock of an item the shop hid is still real stock).
SELECT si.id, coalesce(si.local_name, p.name)::text AS name, p.variant,
       p.pack_size::text AS pack_size, p.unit,
       coalesce(s.sold, 0)::bigint AS sold
FROM store_items si
JOIN products p ON p.id = si.product_id
LEFT JOIN (
  SELECT sm.store_item_id, sum(-sm.delta)::bigint AS sold
  FROM stock_movements sm
  WHERE sm.store_id = @store_id AND sm.source IN ('sale_pos', 'sale_online')
    AND sm.created_at >= @since::timestamptz
  GROUP BY sm.store_item_id
) s ON s.store_item_id = si.id
WHERE si.store_id = @store_id
ORDER BY coalesce(s.sold, 0) DESC, name, si.id
LIMIT @max_rows::int;

-- name: ItemsByIDs :many
-- The named items of the store as the review screen shows them now; an id of
-- another store is missing.
SELECT si.id, coalesce(si.local_name, p.name)::text AS name, si.price_paise, si.on_hand
FROM store_items si JOIN products p ON p.id = si.product_id
WHERE si.store_id = @store_id AND si.id = ANY(@ids::uuid[]);

-- name: DropOldTranscripts :execrows
-- Voice transcripts are kept 90 days for quality review (02 §8 step 6): after
-- that the transcript and each line's heard words are removed from the draft;
-- the lines, outcomes and costs stay.
UPDATE ai_drafts SET
  output = jsonb_set(output - 'transcript', '{lines}', coalesce((
    SELECT jsonb_agg(l || '{"heard": ""}'::jsonb ORDER BY o)
    FROM jsonb_array_elements(output -> 'lines') WITH ORDINALITY AS x(l, o)
  ), '[]'::jsonb)),
  updated_at = @now::timestamptz
WHERE feature = 'voice_stock' AND created_at < @before::timestamptz
  AND output IS NOT NULL AND output ? 'transcript';
