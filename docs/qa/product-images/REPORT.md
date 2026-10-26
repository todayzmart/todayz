# Product images (D-252): report

Branch `claude/product-images-8828ec`, 26 Oct 2026. Products now have pictures. A shop can add a photo, an admin can upload one, and stock photos can be imported from the Open Food Facts family of databases. Each one is a draft until an admin approves it.

## What was built

| Area | What | Where |
|---|---|---|
| Schema | `product_images` (source, status, private draft key, public key, licence, attribution, match) plus `products.image_id` / `image_key` | migration `49601_w4-96_product_images.sql` |
| API | Shop photo: create, presigned PUT, confirm. Admin queue: list, approve, reject, bulk approve (≤ 50), upload, confirm. Credit on search items | `backend/api/paths/{catalogue,admin-catalogue}.yaml`, `internal/catalogue/photos.go`, `internal/admin/product_images.go`, `internal/search/credit.go` |
| Storage | Presigned PUT into the private bucket. On approval the object is copied to the public images bucket | `internal/platform/storage` |
| Importer | `todayz images import [--dry-run] [--limit N] [--source off\|obf\|opf] [--contact URL] [--report FILE]` | `cmd/todayz/images.go`, `internal/imageimport` |
| Business (B-07) | Optional photo when creating a product (camera or file, shrunk to 1024 px JPEG in the browser). The stock list shows the approved image, else the shop's photo while it waits, else the initials. The item sheet shows the reason a photo was turned down | `business/src/stock/photo.ts`, `Thumb.tsx`, `AddItem.tsx` |
| Customer | Search and the product page show the image. An imported photo carries "Photo: Open Food Facts contributors, CC BY-SA 3.0", linking to the source page and the licence | `customer/src/product/ProductPage.tsx` |
| Admin (A-08) | Images tab: the waiting image beside the current one, source filter, match, barcode, licence, credit, approve or reject (a shop sees the reason), bulk approve with failures listed. Admins upload or replace an image on the product card. Thumbnails in the product list | `admin/src/catalogue/ImagesTab.tsx`, `CataloguePage.tsx` |

## Licence and attribution

- **Sources.** Only Open Food Facts (`off`), Open Beauty Facts (`obf`) and Open Products Facts (`opf`). No retailer site is scraped. Their images are CC BY-SA 3.0, and the database names its contributors as the authors.
- **Stored per image.** `source`, `source_ref` (the barcode looked up), `source_url` (the image), `page_url` (the product page), `licence` ("CC BY-SA 3.0") and `attribution` ("Open Food Facts contributors", and so on).
- **Shown.** The customer product page puts a credit line under an imported photo, linking to the source page and to the licence deed. Search items carry the same credit. The admin queue shows the licence, the credit and the source page. Shop and admin photos carry no credit.
- **Copies.** Images are copied into our own private bucket, and approved ones into our public images bucket. We never hot-link the databases.
- **Etiquette.** The User-Agent is `TodayZ-ImageImport/<version> (https://todayz.in)`. Requests are paced at 4.2 s per product lookup and 6.5 s per search, under the published limits (100 product reads and 10 searches a minute). The importer stops on a 429 rather than retrying.
- **Not imported.** Medicines (category or parent `medicines`) stay on the placeholder and are listed in the report, because a stock photo can show the wrong strength or maker.
- **Drafts.** Every import is `pending` until an admin approves it; bulk approval (up to 50 a call) is allowed. A product that already had an import is never tried again, so a rejected image does not come back.

## Live trial (≤ 20 products)

`internal/imageimport/live_test.go`, run once with `TODAYZ_IMAGE_TRIAL_DIR` set, on a throwaway test database (`TODAYZ_TEST_PG_URL_FILE`) seeded like dev and the storage fake. It never ran against the local stack or the cloud.

| Outcome | Count | Products |
|---|---|---|
| Tried | 20 | |
| Found | 1 | Amul Butter 100 g: Open Food Facts, by barcode 8901262010016. I checked the copied image by eye: it is the right pack |
| Not found | 2 | Maggi 2-Minute Noodles 70 g, Coca-Cola 750 ml (by name and brand) |
| Medicines skipped | 1 | Dolo 650 strip |
| Nothing to look up | 14 | No barcode and no brand: wheat atta, toor dal, curd, paneer and other loose staples, plus 6 "QA pack" rows |
| Failed | 2 | Nandini Toned Milk 500 ml, Tata Tea Gold 250 g: `world.openfoodfacts.org` answered 503 |

What the trial shows:
- The dev seed has few barcoded or branded products, so the coverage is low. Real shop catalogues with barcodes should do much better; that is the number to watch on the full run.
- Open Food Facts answered 503 now and then. Such a product is reported as failed and the run carries on; a later run tries it again, because only a stored import blocks a retry.
- The name-and-brand search found nothing for two well-known products. It is strict on purpose: a wrong pack is worse than the initials. An admin sees "Name and brand: check closely" on those matches.

## Screenshots

Taken on the e2e builds with a faked API on a random local port (not the founder's stack). The pictures are drawn by the specs; no real photo is committed.

- `admin-a08-image-queue.png`: the queue with an imported image opened, its match, licence and credit, and two images chosen for bulk approval.
- `admin-a08-product-image.png`: a product card with its image and "Replace image"; the list shows a thumbnail and the initials.
- `business-b07-stock-photos-360.png`: the stock list on a phone with an approved image, a photo waiting for review and the initials.
- `business-b07-new-product-photo-360.png`: a new product with a photo picked.

The customer credit line is covered by `customer/src/test/product.test.tsx`, but has no screenshot.

Refresh with `TZ_QA_DIR=docs/qa/product-images pnpm exec playwright test e2e/images.spec.ts` (admin) or `e2e/photo.spec.ts` (business), after `pnpm run build:e2e`.

## Checks run (per package, no full `make check`)

- Backend: `go test` for `internal/catalogue`, `internal/admin`, `internal/imageimport`, `internal/search`, `internal/platform/storage` and `cmd/todayz` (images), on the shared test Postgres. golangci-lint and settingslint on the changed packages.
- Business: tsc, eslint, stylelint, and vitest for `src/stock` and `src/settings/language.test.tsx`. Playwright `e2e/photo.spec.ts`.
- Customer: tsc, eslint, stylelint, and all of vitest (207 tests).
- Admin: tsc, eslint, stylelint, and all of vitest (293 tests). Playwright `e2e/images.spec.ts`.

## What is left

1. **The full import** over the catalogue waits for the founder's go, and the lead runs it: `todayz images import --limit 20000 --report docs/qa/product-images/full-run.md`. Then bulk-approve in A-08, checking every "Name and brand" match by eye.
2. **A photo after creation.** A shop can add a photo only while creating a product. Adding or changing one later from the item sheet is not built.
3. **Admin uploads skip review.** An admin's own upload goes live when it is confirmed. This is intended, but there is no second pair of eyes.
4. **Translations.** The business `stock.photo.*` keys in hi, kn, te and ta are machine-written and marked reviewed, with a line in `docs/qa/business-i18n/REVIEW.md`. The customer `product.photoCredit` key is translated the same way. Neither has had a native-speaker review.
5. **Take-down.** An approved image is replaced by approving or uploading another. Taking an approved image down, back to the initials, is in the API (reject on the approved image, audited), but A-08 has no button for it yet, because the Images tab lists only pending images.
6. **Seed coverage.** Adding real barcodes to the dev seed would make the trial numbers meaningful.
