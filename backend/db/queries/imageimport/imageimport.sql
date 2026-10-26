-- Stock photo import (D-252, `todayz images import`): products without an
-- image get a draft image from Open Food Facts, Open Beauty Facts or Open
-- Products Facts, which an admin approves in the image queue.

-- name: ImportCandidates :many
-- Products in the catalogue (active or pending review) with no image (or
-- only their kind's photo, D-253), no
-- photo waiting for review and no earlier import (an import an admin turned
-- down is not tried again), in id order after `after`.
SELECT p.id, p.name, p.barcodes, br.name AS brand_name, c.slug AS category_slug,
  coalesce(pc.slug, '')::text AS parent_slug
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN categories pc ON pc.id = c.parent_id
LEFT JOIN brands br ON br.id = p.brand_id
WHERE p.status IN ('active', 'pending_review')
  AND NOT EXISTS (SELECT 1 FROM product_images own WHERE own.id = p.image_id AND own.source <> 'type')
  AND NOT EXISTS (
    SELECT 1 FROM product_images pi
    WHERE pi.product_id = p.id
      AND (pi.status IN ('uploading', 'pending') OR pi.source IN ('off', 'obf', 'opf')))
  AND (sqlc.narg(after)::uuid IS NULL OR p.id > sqlc.narg(after)::uuid)
ORDER BY p.id
LIMIT @lim;

-- name: InsertImportedImage :execrows
-- A pending imported image, unless the product gained an image or a waiting
-- photo since it was listed (0 rows then).
INSERT INTO product_images (id, product_id, source, status, object_key, content_type, size_bytes,
  source_ref, source_url, page_url, licence, attribution, match, created_at, updated_at)
SELECT @id, p.id, @source, 'pending', @object_key, @content_type, @size_bytes,
  @source_ref, @source_url, @page_url, @licence, @attribution, @match, @now::timestamptz, @now::timestamptz
FROM products p
WHERE p.id = @product_id
  AND NOT EXISTS (SELECT 1 FROM product_images own WHERE own.id = p.image_id AND own.source <> 'type')
  AND NOT EXISTS (
    SELECT 1 FROM product_images pi
    WHERE pi.product_id = p.id AND pi.status IN ('uploading', 'pending'));

-- Item-type photos (D-253, `todayz images types`): one free-licence photo
-- per kind of item, from Wikimedia Commons, which an admin picks in A-08.

-- name: TypeKinds :many
-- Kinds of item (category, item_type_key) with a product that has no image,
-- and no type photo waiting or picked, most products first. A kind whose
-- candidates were all turned down is searched again; its turned-down files
-- are skipped (TypeKindSources).
SELECT p.category_id, item_type_key(p.name, br.name)::text AS type_key, c.slug AS category_slug,
  coalesce(pc.slug, '')::text AS parent_slug, count(*)::int AS products
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN categories pc ON pc.id = c.parent_id
LEFT JOIN brands br ON br.id = p.brand_id
WHERE p.status IN ('active', 'pending_review') AND p.image_id IS NULL
  AND item_type_key(p.name, br.name) IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM type_photos tp
    WHERE tp.category_id = p.category_id AND tp.type_key = item_type_key(p.name, br.name)
      AND tp.status IN ('pending', 'approved'))
GROUP BY p.category_id, item_type_key(p.name, br.name), c.slug, pc.slug
ORDER BY count(*) DESC, 2, 1
LIMIT @lim;

-- name: TypeKindSources :many
-- Commons files a kind already has in any state, so a turned-down file is
-- not offered again.
SELECT source_ref FROM type_photos WHERE category_id = @category_id AND type_key = @type_key::text;

-- name: ClearPendingTypePhotos :many
-- `images types --again`: drops the candidates nobody has decided on, of
-- kinds with no picked photo, so those kinds are searched again. Turned-down
-- rows stay, so their files still do not come back.
DELETE FROM type_photos tp
WHERE tp.status = 'pending'
  AND NOT EXISTS (SELECT 1 FROM type_photos a
    WHERE a.category_id = tp.category_id AND a.type_key = tp.type_key AND a.status = 'approved')
RETURNING tp.object_key;

-- name: InsertTypePhoto :execrows
INSERT INTO type_photos (id, category_id, type_key, rank, object_key, content_type, size_bytes,
  source_ref, source_url, page_url, licence, attribution, created_at, updated_at)
VALUES (@id, @category_id, @type_key, @rank, @object_key, @content_type, @size_bytes,
  @source_ref, @source_url, @page_url, @licence, @attribution, @now, @now)
ON CONFLICT (category_id, type_key, source_ref) DO NOTHING;

-- name: ApprovedTypePhotos :many
SELECT id, category_id, type_key, public_key FROM type_photos WHERE status = 'approved' ORDER BY id;

-- name: TypeKindProducts :many
-- As admin's TypeKindProducts: products of a kind that may take its photo.
SELECT p.id FROM products p LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.category_id = @category_id AND item_type_key(p.name, b.name) = @type_key::text
  AND p.status IN ('active', 'pending_review') AND p.image_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM product_images x WHERE x.product_id = p.id AND x.type_photo_id = @type_photo_id)
ORDER BY p.id
FOR UPDATE OF p;

-- name: InsertTypeImages :exec
INSERT INTO product_images (id, product_id, source, status, object_key, content_type, size_bytes, public_key,
  source_url, page_url, licence, attribution, type_photo_id, decided_at, created_at, updated_at)
SELECT u.id, u.product_id, 'type', 'approved', t.object_key, t.content_type, t.size_bytes, t.public_key,
  t.source_url, t.page_url, t.licence, t.attribution, t.id, @now::timestamptz, @now::timestamptz, @now::timestamptz
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@product_ids::uuid[]) AS product_id) u
JOIN type_photos t ON t.id = @type_photo_id AND t.status = 'approved';

-- name: SetTypeProductImages :exec
UPDATE products p SET image_id = u.id, image_key = @image_key::text, updated_at = @now::timestamptz, version = version + 1
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@product_ids::uuid[]) AS product_id) u
WHERE p.id = u.product_id;
