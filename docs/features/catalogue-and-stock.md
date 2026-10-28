# Catalogue and stock

How products, shop prices and stock quantities are stored, changed and photographed.

> As built on main at d22219d7 (27 Oct 2026). Update this file in the same change as the code it describes.

## What users can do

| App | What |
|---|---|
| Shop app | Add items from the master catalogue or create new ones, set price and stock, add stock with a reason, see movement history, scan a supplier bill or shelf photo (AI drafts), count stock, set batches with expiry. Folders: [stock](../../business/src/stock/), [restock-scan](../../business/src/restock-scan/), [onboarding](../../business/src/onboarding/) (shelf scan). |
| Admin console | Approve pending products, edit categories and synonyms, upload product images and type photos. Folder: [admin/src/catalogue](../../admin/src/catalogue/). |
| Customer app | See products and photos on shop pages. Folders: [product](../../customer/src/product/), [category](../../customer/src/category/). |
| Backend jobs | `catalogue_reconcile` rechecks stock totals. |

## Rules

Source: [01-product-rules.md](../plan/01-product-rules.md) section 4.

- Master catalogue (shared products) plus a store catalogue (the shop's own price and stock).
- Every stock change is a movement with a source: opening, restock, supplier_bill, import, count_correction, damage, expired, sale_pos, sale_online, pos_reversal (D-33).
- Batches carry expiry (D-99). Stale and low stock use `stock_stale_after_hours` and `low_stock_default`.
- A shop may sell at any price, above the MRP too (D-245).
- Online stock is reserved at accept, not at placing (D-01).
- Stock does not go below zero (D-270): `catalogue.Writer` refuses a POS sale above available (`Sale.CapAvailable`) and damage or expiry above on hand (`Removal.CapOnHand`). A synced offline sale is never refused, so an offline race can still leave on hand below 0; every read (`items.go`, the sync changes) then shows on hand and available as 0 with `countNeeded: true` until a count.

## How it works

```mermaid
flowchart LR
    Shop[Shop edits stock] --> Move[stock_movements row]
    POS[POS sale] --> Move
    Order[Online order accepted] --> Res[stock_reservations]
    Move --> Items[store_items quantity]
    Res --> Items
    Items --> Rec[catalogue_reconcile job]
```

Each movement writes in the same transaction as the quantity change. Allocation across batches is in `alloc.go`. Product photos have three sources in order: the shop's photo, an admin upload, a stock photo of that kind of item (D-252, D-253). An admin picks each kind's photo from Commons candidates in A-08 (`todayz images types` finds them). The founder's picks are saved in `backend/internal/imageimport/data/approved-type-photos.csv`; `todayz images types --picks builtin` applies them to a new or reset database, with the same steps as A-08 and the system as the audited actor. It leaves alone a kind that already has a photo and a file that was turned down (D-265). The stock-image importer can prioritize shop-listed products with `--listed-first`; source routing sends personal care, skin care, hair care, makeup and baby care to Open Beauty Facts, household and electronics categories to Open Products Facts, and food categories to Open Food Facts. Every imported image remains pending for admin review.

## Demo catalogue

`todayz demo catalogue [--source off|obf|opf] [--category-tag TAG | --search-terms WORDS] [--limit N] [--dry-run] [--from DIR]` reads India-filtered Open Food Facts, Open Beauty Facts or Open Products Facts search JSON. Raw pages are cached under `.cache/<source>-demo/` (or a supplied directory). It maps supported source categories to existing TodayZ categories, keys products by EAN, and skips unmapped entries. Products are listed at up to three live demo stores, with inventory added through `catalogue.Writer` restock movements. Images use the D-252 path: run `todayz images import --source <source> --newest --limit N`, then approve pending images through the admin review API as a local admin. Image rows retain source references and their applicable licence and attribution. The command refuses non-local/test environments and `TODAYZ_NO_EGRESS`. This sample data is for local demos, not production.

The local-only helper `scripts/demo/enrich-catalogue.py` (D-273) cleans junk names, fills pack sizes and writes a realistic MRP and selling price. No retailer **product** image is used or kept: product photos come only from Open Food/Beauty/Products Facts or Wikimedia Commons, with licence and credit (D-271, D-252), and a listed product with neither keeps the initials placeholder. The Home category tiles are separate local-demo art (Blinkit/BigBasket/Amazon, D-273), not product photos. After the retailer rows were removed, 344 of the 1,130 listed products have no photo; importing and approving open or owned photos is the fix.

## Code

| Part | Where |
|---|---|
| Backend | `backend/internal/catalogue` ([folder](../../backend/internal/catalogue/)): `items.go`, `movements.go`, `batches.go`, `alloc.go`, `price.go`, `photos.go`, `field.go`. Image import: `backend/internal/imageimport` ([folder](../../backend/internal/imageimport/)). Local demo catalogue (D-271, D-273): `backend/internal/democatalogue` ([folder](../../backend/internal/democatalogue/)), `backend/internal/catalogue/demo.go`, `backend/cmd/todayz/demo_catalogue.go`, `scripts/demo/enrich-catalogue.py`. |
| API paths | [catalogue.yaml](../../backend/api/paths/catalogue.yaml), [admin-catalogue.yaml](../../backend/api/paths/admin-catalogue.yaml), [discover.yaml](../../backend/api/paths/discover.yaml) (substitutes) |
| Tables | `categories`, `brands`, `products`, `store_items`, `stock_batches`, `stock_movements`, `stock_movement_batches`, `catalogue_changes`, `product_images`, `type_photos`, `substitutes` |
| Frontend | folders above |

## Tests

Package tests plus `backend/cmd/todayz/*_http_test.go`. Reports: [W1-03](../qa/W1-03/REPORT.md), [W1-05](../qa/W1-05/REPORT.md), [product-images](../qa/product-images/).

## Status and known gaps

Live in code. The AI scan flows are drafts only (see [ai-features](ai-features.md)). Stock photos for Indic names depend on the kind mapping. 20 of the founder's 176 picks could not be saved (the files left the search results) and wait in A-08.

## Decisions

D-01 stock math. D-33 stock sources. D-99 batches. D-245 any price. D-252, D-253 and D-265 product images. D-271 local demo catalogue. D-270 no stock below zero. D-35 and D-79 foreign-key ordering. D-273 quick-commerce UI and realistic demo prices, with no retailer images.
