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

### Latency regression on the real seed (fixed in 818e8ca)

With W1-05's full seed on main, `TestSearchAtScale/latency` failed: p95 523 ms against the 300 ms target (the agent's 81 ms had been on the stub seed). I sent it back to the agent; the target stayed.

**Cause.** The cause was the query plan, not the SQL or the indexes. pgx caches prepared statements, and after five runs Postgres switches each to its generic plan, made without the point, the radius or the optional filters. For `SearchItems`:
- the generic plan guessed 1 store in the radius (actually 186) and 11 products (actually about 3,600);
- it then probed `ux_store_items_store_product` once per (store, product) pair: about 670k probes and about 500 ms.

The plan made with the real arguments runs in about 60 ms. This was confirmed with `plan_cache_mode` forced each way and with `EXPLAIN (GENERIC_PLAN)`.

**Fix.** `search.ready()` wraps the querier in `planPerCall`. Its `Query` and `QueryRow` use `pgx.QueryExecModeCacheDescribe`:
- the discover reads run as unnamed statements and are planned per call;
- planning costs about 0.5 ms, and it is still one round trip;
- writes (search log, events) keep the default mode.

There is no new index and no migration. The perf test now times each query by name and prints `EXPLAIN (ANALYZE, BUFFERS)` of the worst two queries when p95 misses. It also guards the regression: the same statement runs 56 times, well past the switch.

| | p50 | p95 | max |
|---|---|---|---|
| Before (main 61e593c) | 107 ms | 517 ms | 539 ms |
| After (agent) | 22 ms | 78 ms | 89 ms |
| After (lead, in the worktree before the merge) | 24 ms | 74 ms | 80 ms |

Lead verification of 818e8ca, before the fast-forward:

| Check | Result |
|---|---|
| `make gen` | ok |
| `go vet ./...` | ok |
| golangci-lint | 0 issues |
| `go test -race ./internal/search/ ./cmd/todayz/` | ok |
| `TestSearchAtScale` (indexes, latency) | pass |

**Follow-ups:**
- Cost still grows with the listings inside the radius (about 31k at 5 km in the synthetic city). If real density goes far beyond that, filter on text first through the GIN `ix_store_items_search`. That needs the `OR p.category_id = ANY(…)` branch restructured.
- Any other package whose reads hang on a point plus optional `$n IS NULL OR …` filters should use the same per-call planning. Only `search` does today.

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
