-- aiconsumer queries (W3-12 consumer AI: list parse, search assist, voice
-- search), generated into package aiconsumerdb (gen/sqlc/aiconsumerdb).
-- Only the work package that builds internal/ai/{listparse,querynorm,
-- voicesearch} edits this folder (D-50). Read-only: none of these features
-- writes anything but its ai_drafts row and the daily call counts.

-- name: BrandsNamedIn :many
-- The master-catalogue brands whose name is in the query (the brand refine
-- chip, 01 §11). The caller checks the word boundaries; longest names first,
-- so "Amul Gold" wins over "Amul".
SELECT name, slug FROM brands
WHERE char_length(name) >= 2
  AND strpos(lower(@query::text), lower(name)) > 0
ORDER BY char_length(name) DESC, slug
LIMIT 20;

-- name: BrandsBySlug :many
-- The brands of the slugs the model proposed (search assist): a slug not in
-- the master catalogue is never shown.
SELECT name, slug FROM brands WHERE slug = ANY(@slugs::text[]) ORDER BY slug;
