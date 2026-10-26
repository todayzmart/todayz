-- Stock photo import (D-252, `todayz images import`): products without an
-- image get a draft image from Open Food Facts, Open Beauty Facts or Open
-- Products Facts, which an admin approves in the image queue.

-- name: ImportCandidates :many
-- Products in the catalogue (active or pending review) with no image, no
-- photo waiting for review and no earlier import (an import an admin turned
-- down is not tried again), in id order after `after`.
SELECT p.id, p.name, p.barcodes, br.name AS brand_name, c.slug AS category_slug,
  coalesce(pc.slug, '')::text AS parent_slug
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN categories pc ON pc.id = c.parent_id
LEFT JOIN brands br ON br.id = p.brand_id
WHERE p.status IN ('active', 'pending_review') AND p.image_id IS NULL
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
WHERE p.id = @product_id AND p.image_id IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM product_images pi
    WHERE pi.product_id = p.id AND pi.status IN ('uploading', 'pending'));
