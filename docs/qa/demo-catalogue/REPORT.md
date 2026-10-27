# Local demo catalogue QA

Run date: 27 Oct 2026. This is local demo data only. Product data came from India-filtered Open Food Facts, Open Beauty Facts and Open Products Facts. Raw API pages are in git-ignored `.cache/{off,obf,opf}-demo/` and are not committed.

## Catalogue result

The catalogue currently contains 1,041 products added in the demo import window, spread over the built-in categories below. Each newly-added product was listed in three demo stores and stocked through restock movements. Re-running the importer is safe by EAN. The image goal is not yet met: 571 of the 1,041 have published photos.

| TodayZ category | Products | With published photo |
|---|---:|---:|
| Baby care | 1 | 1 |
| Beverages | 165 | 134 |
| Dairy and eggs | 92 | 21 |
| Household | 27 | 27 |
| Personal care | 146 | 146 |
| Snacks | 139 | 104 |
| Spices and oils | 190 | 44 |
| Staples | 281 | 94 |
| **Total** | **1,041** | **571** |

Still thin: baby care (1), household (27), dairy photos (21), spices and oils photos (44), and the individual rice/dal/spice product subgroups. OFF cached pages with front photos include 100 biscuit-tagged and 196 breakfast-cereal-tagged products; cached rice/legume/spice category coverage remains sparse. The broader TodayZ category rollups combine multiple source tags.

## Founder update: listed shop products

The local database has 1,130 distinct listed products. The before and after photo counts are the same because new Open Facts images and Commons type photos remain pending until admin approval or a founder pick; pending candidates are not customer-visible. This SQL snapshot reports 559 listed products without a published photo:

```sql
WITH listed AS (
  SELECT DISTINCT si.product_id, p.category_id, p.image_id
  FROM store_items si JOIN products p ON p.id = si.product_id
  WHERE si.is_listed
)
SELECT c.slug, count(*) AS listed,
       count(*) FILTER (WHERE l.image_id IS NULL) AS without_photo
FROM listed l JOIN categories c ON c.id = l.category_id
GROUP BY c.slug ORDER BY c.slug;
```

| TodayZ category | Listed | Before without photo | After without photo |
|---|---:|---:|---:|
| Baby care | 1 | 0 | 0 |
| Bakery | 2 | 2 | 2 |
| Beverages | 169 | 35 | 35 |
| Dairy and eggs | 101 | 80 | 80 |
| Electrical | 6 | 6 | 6 |
| Fruits and vegetables | 12 | 12 | 12 |
| Hair care | 1 | 1 | 1 |
| Household | 37 | 10 | 10 |
| Medicines | 1 | 1 | 1 |
| Paints | 3 | 3 | 3 |
| Personal care | 149 | 3 | 3 |
| Plumbing | 3 | 3 | 3 |
| Snacks | 145 | 41 | 41 |
| Spices and oils | 198 | 154 | 154 |
| Staples | 298 | 204 | 204 |
| Tools | 4 | 4 | 4 |
| **Total** | **1,130** | **559** | **559** |

The founder's note grouped hardware-related listings as 16; TodayZ reports those as electrical (6), plumbing (3), tools (4) and paints (3), totaling 16. The live query returns 204 staples without photos (one more than the update's 203); the total remains 559 because the other category counts sum to 7 here.

The follow-up source imports used `--listed-first --newest`. The Open Food Facts probe still returned HTTP 503. Open Beauty Facts tried 20 products (16 not found; four had no lookup key), and Open Products Facts tried 20 (none had a lookup key); neither added a product-image row. Category routing covers personal care, skin care, hair care, makeup and baby care through OBF, and household/electronics categories through OPF. The route prioritizes currently shop-listed products before the rest of the catalogue.

The D-253 Commons search was stopped on request after 226 kinds: 195 had candidates, 23 had no free-licence result, and 7 medicines were skipped by design. The run was interrupted during a thumbnail download; remaining eligible kinds were not searched. Candidates saved before the stop remain pending in `type_photos` for A-08. No candidate was approved or applied during this follow-up.

## Image import and attribution

`todayz images import` uses the existing D-252 source lookup, image processing and pending review flow. Pending images were approved through the local admin Images view; the app publishes them through its normal review path. The latest status snapshot from `SELECT source,status,count(*) FROM product_images GROUP BY source,status ORDER BY source,status` was:

| Source | Approved | Pending | Rejected |
|---|---:|---:|---:|
| OFF | 398 | 0 | 0 |
| OBF | 146 | 0 | 0 |
| OPF | 27 | 0 | 0 |

This table is the `product_images` queue: existing Open Facts images are approved, and the follow-up imports added no new product-image rows. D-253 Commons candidates are in the separate `type_photos` queue and remain pending for A-08. The OFF import previously stopped after 137 successful rows when Open Food Facts returned HTTP 503; the follow-up probe also returned the rate-limit error (`tried: 1`, `found: 0`), so no further OFF requests were made. Retry after the source recovers. Validate required attribution fields with:

```sql
SELECT source,
       count(*) FILTER (WHERE source_url !~ '^https://images\\.' OR page_url !~ '^https://world\\.' OR licence IS NULL OR attribution IS NULL) AS invalid_rows
FROM product_images
WHERE source IN ('off','obf','opf')
GROUP BY source;
```

At the snapshot this returned zero invalid rows for all three sources. Their image hosts were `images.openfoodfacts.org`, `images.openbeautyfacts.org` and `images.openproductsfacts.org`; product pages use the matching `world.*facts.org` domains.

## Screenshots

Customer screenshots were captured at 390px and desktop widths. Product detail shows the source credit. Business captures show the catalogue picker search for “Parle” in responsive 390px mode and desktop mode.

| View | Phone | Desktop |
|---|---|---|
| Customer home | `customer-home-mobile.png` | `customer-home-desktop.png` |
| Staples category | `category-staples-mobile.png` | `category-staples-desktop.png` |
| Snacks category | `category-snacks-mobile.png` | `category-snacks-desktop.png` |
| Customer search (`milk`) | `search-milk-mobile.png` | `search-milk-desktop.png` |
| Product detail and photo credit | `product-photo-credit-mobile.png` | `product-photo-credit-desktop.png` |
| Business catalogue search (`Parle`) | `business-catalogue-search-mobile.png` | `business-catalogue-search-desktop.png` |

## Re-run after a local reset

1. Start and seed the local stack using the normal local workflow.
2. Fetch focused Open Facts pages, e.g. `todayz demo catalogue --source off --category-tag en:biscuits --limit 100`, then repeat for `en:breakfast-cereals`, `en:dairies`, `en:flours` and `en:vegetable-oils`. Use `--source obf` for personal care and `--source opf` for household. `--dry-run` previews cached/fetched products; raw pages are reused from `.cache/`.
3. Run `todayz images import --source off --newest --listed-first --limit N`, then repeat with `--source obf` and `--source opf` for their products. The importer paces requests according to source limits and can take a long time for hundreds of products.
4. In the local admin console, filter Images by source and approve matches in batches of no more than 50. Never bypass the normal approval path.
5. Run `todayz images types --limit 20000` for pending Commons type-photo candidates. Leave them pending for the founder's A-08 review; do not apply saved picks as part of this demo refresh.

The demo command refuses non-local/test environments and `TODAYZ_NO_EGRESS`. Source products, photos and attributions are for local demos, not production (D-271).

## Retailer images reverted (D-273, 27 Oct)

The founder changed D-273: no retailer image stays, not even as demo data. The quick-commerce layout, cards and product page stay in TodayZ green; the retailer photo pass is gone.

- **Removed from the repo**: `scripts/demo/retailer-images.py`, the 24 `customer/src/home/photos/sub-*.webp` files, and the retailer paragraph in `photos/SOURCES.md`. The six `cat-*.webp` files and `home/photos.ts` / `CategoryTiles.tsx` are back to main.
- **Removed from the local demo data**: 346 `product_images` rows (254 Flipkart, 90 Blinkit, 2 old synthetic rows) and their objects. No Blinkit, Flipkart, PharmEasy or Amazon image remains.
- **Effect**: 344 of the 1,130 listed products now have no photo and show the initials placeholder. The other listed products keep their Open Food/Beauty/Products Facts photos, with licence and credit. That is the accepted cost; the fix is to import and approve open or owned photos, not to re-add retailer ones.
- **Kept**: the quick-commerce theme, `ItemCard` (brand, MRP/saving, tag, green Add), the product page (cover photo, Highlights, About this item, Product details, sticky Add), `scripts/demo/enrich-catalogue.py` (names, packs, prices) and the Open Facts image import search.
- **Cover is back**: item tiles and the product page use `object-fit: cover` (D-272).

After a reset: run `todayz demo catalogue`, `todayz images import` for Open Facts with `--search-first --listed-first`, approve the pending images through admin review, and run `scripts/demo/enrich-catalogue.py` for names, packs and prices. Do not use a retailer fetcher.
