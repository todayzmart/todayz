# W1-06 Places, discovery and search: QA report

This covers the backend only: `internal/search`, the `discover` contract, migration 10601 and the `search_performed` event. Branch `worktree-agent-abe9ea003e334811c`, built on W1-05's schema commit and rebased onto main 5c75d3b by the lead. Everything ran locally on testcontainers, with no network and no cloud. Search never calls a model.

## What was built

- **Places (10601):**
  - a `places` table with 3 cities and 104 curated localities with PIN codes (Bengaluru 43, Chennai 32, Hyderabad 29), `source = 'curated'`, with approximate centroids;
  - indexes: GiST on location, trigram on name, `text_pattern_ops` on pincode.
  - `GET /v1/places?q=` matches a PIN prefix or a name (trigram, typo-tolerant, LIKE metacharacters escaped).
  - `GET /v1/places/reverse` returns the nearest locality within 5 km, else the nearest city within 50 km, else 404.
- **Discover summary:** `GET /v1/discover/summary` returns stores nearby, open now, products in radius and pins (capped, with `pinsTruncated`). It counts only `live` stores of `verified` businesses. Clusters are deferred.
- **Store list:** `GET /v1/discover/stores` with open now, pickup, delivery and category filters, distance and a cursor.
  - Open-now goes through `OpenFunc`, wired to W1-04's `business.OpenAt`.
  - Delivery is offered only while `feature_delivery_enabled` is on.
- **Shop page:** `GET /v1/discover/stores/{slug}` and `/items` (with `q`, `category` and a cursor), with `confirmedAt` per item. A non-live or unverified store is 404.
- **Search:** `GET /v1/discover/search`.
  - **Matching:** a weighted tsvector (`simple` config, so Kannada works), 30 synonym rows, a trigram typo fallback, and an exact barcode match.
  - **Grouping:** by store, with up to 5 items per card.
  - **Ranking (01 §11):** w_text·text + w_dist·exp(−d/1500) + w_rel·reliability + w_rating·rating/5 + w_offer·offer.
    - Every weight comes from `platform_settings`.
    - A missing reliability counts as 1.0.
    - An unrated store uses a 4.0 prior.
    - `OfferLookup` returns false.
  - **Sorts:** relevance, nearest, cheapest.
  - **Filters:** radius, category, brand, `maxPricePaise`, open now, pickup, delivery, has offer.
- **D-101 on search:**
  - `summary {storesChecked, storesWithIt, minPricePaise}`, hidden when no store has it;
  - `confirmedAt` on every card, null once older than `stock_stale_after_hours`;
  - at most one badge per card: `lowest_price` (ties go to the nearer store, never an in-store-only store) or `closest`;
  - `deliveryFeePaise`;
  - the `pickup`, `brand` and `maxPricePaise` params;
  - `parse` is accepted, `chips` is always `[]` and `refineHint` is always false (TODO citing D-101).
- **Suggestions:** `GET /v1/discover/suggest` returns brands, categories and products by prefix or trigram. Suggestions are not logged.
- **Logging:**
  - `search_log` has daily partitions, no coordinates, and one row per first-page search, zero-result searches included.
  - `product_id` is set only on an exact D-99 match.
  - `search_performed` (with `product_id`, `chips` "none" and `source`) and `search_zero_results` carry neither the query text nor the location.

## Evidence (agent)

| Check | Result |
|---|---|
| Unit and golden: `TestRankGolden`, `TestBadges`, `TestSummary`, `TestNormalize`, `TestParsePack`, `TestTsquery`, `TestCursor`, `TestConfirmedAt`, `TestHoursOut` | pass |
| SC-SR, ASK and IN-28 (DB): `TestSearchSummaryASK09`, `TestHiddenStoresNeverShowSR01`, `TestOutOfStockAndInStoreOnlySR02SR05`, `TestSortsAndWeightsSR02SR04`, `TestBadgesAndModesASK10`, `TestConfirmedAtASK12`, `TestSearchLogProductIN28` | pass |
| Other DB tests: `TestMatching` (typo, synonym, Kannada, barcode, category, injection-looking input), `TestFilters`, `TestPagination`, `TestShopPage`, `TestDiscoverSummaryAndList`, `TestPlaces`, `TestSuggest` | pass |
| Index use (`TestSearchAtScale/indexes`) | the EXPLAIN of each query, on a 1,000-store city after ANALYZE, names the expected index: `ix_stores_location`, `ix_places_location`, `ix_places_name_trgm`, `ix_places_pincode`, `ix_products_name_trgm`, `ix_products_search`, `ix_products_barcodes`, `ix_store_items_store_id` |
| Latency (`TestSearchAtScale/latency`) | **p95 80.8 ms** (target < 300 ms), p50 19.6 ms, max 85 ms. Setup: the dev seed plus 1,000 synthetic live stores out to 12 km, 600 extra products and 30,188 listings; 14 queries × 2 radii × 2 rounds, plus the summary and the store list. |
| gofmt, vet, golangci-lint, gosec, settings lint | clean |
| `make check-contract` | the Schemathesis smoke failed on 4 cases; the lead fixed them (below) |

## Deferred

- All-India places (the India Post plus OSM import, 02 §6).
- The refine-chip parser and `refineHint` (D-101).
- Clusters.
- A real `OfferLookup` (W2).
- `search_log.geo_cell` (H3): there is no H3 library in go.mod, and W3-03 needs it (D-59, D-81).

## Not verified

- The curated coordinates against a survey source.
- Latency through HTTP and Caddy. The test calls the service directly.
- Planner choices on RDS statistics.
- Frontends: no app consumes these endpoints yet.

## Lead, after the merge (2026-10-23)

Rebased onto main 5c75d3b (W1-05 merged). Git skipped the duplicate schema commit dd7521f, and the 3 W1-06 commits applied cleanly. I fast-forwarded main to 304e938, then fixed the contract smoke in a1b30dd. Both causes were in lead-owned code:

1. **Undeclared query names.** The validator now refuses a query name the operation does not declare, with 422 `query.<name>: not allowed`. A name outside the field-token pattern is echoed as `<redacted>`. This matches how an unknown body property is already refused. Before the change I checked that every app calls the API through the generated clients, which send only declared params. `TestUndeclaredQueryRefused` covers it.
2. **Opaque cursors.** A cursor the server never issued is refused with 422 `query.cursor`, and that stays. `e2e/contract/schemathesis.toml` (loaded by `run.sh --config-file`) lets `positive_data_acceptance` accept 422 on the three public cursor lists only: `listDiscoverStores`, `listDiscoverStoreItems` and `searchDiscover`. Lists behind auth answer 401 first. A new public cursor list joins that file.

Verification on main after the fixes is recorded in the table below.

Decisions on the agent's questions (all accepted unless noted):

- **Ranking prior and card size:** a 4.0 prior for unrated stores, and 5 items per card.
- **Pack in the query:** a pack in the query is a hard filter.
- **D-99 match:** an ambiguous D-99 match logs a null `product_id`.
- **Card total and logging:**
  - a card's total is the pickup price when the store accepts pickup;
  - only the first page is logged.
- **Open stores and ranking:** closed stores are ranked by the 01 §11 formula with `open: false`, because 01 is the source of truth over 08. If shops or customers find this wrong after the demo, an open term goes into the formula as a setting, with a decision entry.
- **Half a point:** on the shop page, a half point (only `lat` or only `lng`) drops the distance instead of answering 422.
- **Constants, not settings:**
  - the reverse-geocode radii (5 km, 50 km);
  - the pins cap (200).

  These are not product rules in 01 §17.
- **Store-list category:** it filters on the business category.
- **Synonym translations:** the hi, kn, te and ta synonym rows need native review. This is a founder item.
