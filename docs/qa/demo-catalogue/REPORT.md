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

## Image import and attribution

`todayz images import` uses the existing D-252 source lookup, image processing and pending review flow. Pending images were approved through the local admin Images view; the app publishes them through its normal review path. The latest status snapshot from `SELECT source,status,count(*) FROM product_images GROUP BY source,status ORDER BY source,status` was:

| Source | Approved | Pending | Rejected |
|---|---:|---:|---:|
| OFF | 398 | 0 | 0 |
| OBF | 146 | 0 | 0 |
| OPF | 27 | 0 | 0 |

This snapshot shows every queued image approved. The OFF import stopped after 137 successful rows when Open Food Facts returned HTTP 503. A later one-product retry also returned the rate-limit error (`tried: 1`, `found: 0`), so the remaining OFF candidates were left untouched. Retry after the source recovers. Validate required attribution fields with:

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
3. Run `todayz images import --source off --newest --limit N`, then repeat with `--source obf` and `--source opf` for their products. The importer paces requests according to source limits and can take a long time for hundreds of products.
4. In the local admin console, filter Images by source and approve matches in batches of no more than 50. Never bypass the normal approval path.

The demo command refuses non-local/test environments and `TODAYZ_NO_EGRESS`. Source products, photos and attributions are for local demos, not production (D-271).
