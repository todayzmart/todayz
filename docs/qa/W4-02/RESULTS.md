# W4-02 load results (local, 26 Oct 2026)

These numbers come from the local stack: one MacBook with 16 GB, Postgres capped at 2 CPU and 4 GB, and the API at
`GOMAXPROCS=2 GOMEMLIMIT=1800MiB`. The data is from `make load-data`: 1,000 shops around central Bengaluru, about
210 stocked items each on average, and 10,000 customers. These figures tell you the shape of the system. They are
not production capacity: staging re-runs the suite in wave 5. The criteria come from 08 §6, and the suite measures
them as `e2e/load/README.md` describes. No OTP, code, token or phone number appears here.

## Scenarios

| Scenario | Numbers | 08 §6 verdict |
|---|---|---|
| browse_peak (10 min, 57,212 requests) | errors 0%. reads p95 134.7 ms, p99 472 ms. search+suggest p95 285.5 ms, p99 905 ms | Pass on the suite thresholds (reads <200, search <300, errors <0.1%). Search alone is p95 590 ms, p99 1,571 ms: see open items |
| order_burst (550 orders) | writes p95 139.8 ms, p99 221.5 ms. timers late p99 1.04 s, 0 missed. reconcile clean (`checks/order_burst.sql`) | Pass |
| pos_rush (200 shops, 10 min) | writes p95 53.2 ms (sale 50.9, retry 6.8). sync batch of 20 p95 678 ms. 0 duplicates in 6,021 sales. retry returned the same sale 110/110 | Pass |
| otp_abuse | IP abuser: 20 accepted, then 281/281 blocked. hammered number: 5 accepted. code guessing: locked 12/12. legit users 90/90 | Pass |
| sse_fanout | 5,000 streams. 42,672/42,672 deliveries. delivery p95 6.8 ms. API RSS 65 to 264 to 69 MB after close | Pass |
| soak (planned 20 min at 30% of peak) | Aborted at about 13 min: the lead stopped it because the stack was needed for a demo. The stack was also recreated under the run at about 7 min. Over the clean window (09:25:54 to 09:30 UTC): API RSS 112 to 125 MB, goroutines 170 to 177, pool 24/24 idle between requests, Postgres 28 connections, 0 idle-in-transaction | Not run to completion. The window is too short to judge memory growth or connection leaks |

Per-endpoint p95 for browse_peak:
- categories 38 ms
- stores 53 ms
- store 52 ms
- store_items 171 ms
- store_items with a query 179 ms
- summary 170 ms
- stores by category 166 ms
- substitutes 275 ms
- suggest 163 ms
- search 590 ms

## Fixes (behaviour unchanged; `go test ./internal/search` passes)

All timings are warm, against 1,000 shops.

| Where | Change | Before | After |
|---|---|---|---|
| `backend/db/queries/search/search.sql:69` (StoresInRadius) | The category filter matches `product_id = ANY(ARRAY(products in the categories))` instead of a correlated EXISTS per item | 223 ms | 15 ms |
| `backend/db/queries/search/search.sql:176` (SearchItems) | Same change for the category arm of the match | mean 119 ms | mean 45 ms |
| `backend/db/migrations/40201_w4-02_store_items_listing_index.sql` | `ix_store_items_store_listing (store_id) INCLUDE (price_paise, on_hand, reserved, is_listed)` replaces `ix_store_items_store_id` | StoreItemsPage and CountListingsInRadius 92 ms | 6 ms (index-only scan) |

Before these fixes, browse_peak search p95 was 3,646 ms. That run also had 35.9% errors, which came from parameter
bugs in the scenario, not from the API.

## EXPLAIN on the top statements (pg_stat_statements, browse_peak plus background jobs)

**SearchItems (mean 45 ms, 282 s total)**
- After the fix, the plan is a GiST radius scan on stores, then the per-store store_items index, then a filter on
  the tsvector and category arms.
- I tried a rewrite that runs the match first and materialises it. It was slower: 95 ms against 52 ms at 5 km.
- The p99 tail under load is queueing on 2 CPUs, not a bad plan.

**SuggestProducts (mean 17 ms, 172 s total)**
- A sequential scan of about 3k products, with a trigram `%` test on each row.
- Cheap at this catalogue size. A GIN trigram index is worth adding once the catalogue reaches tens of thousands
  of products. Not changed.

**StoresInRadius 4.0 ms, StoreItemsPage 4.8 ms, CountListingsInRadius 5.0 ms, TypoProducts 4.3 ms, ItemPrices 1.2 ms**
- Index scans after the fixes.

**Nightly rollups: UpsertPlatformDay 3.2 s, UpsertBannerDays 2.0 s, UpsertStoreDays 450 ms**
- These are worker jobs, run once per day, not on the request path. Nothing to do now. Watch them as volume grows.

The post-soak pass of the top 30 was not repeatable: the database was recreated under the soak.

## Open items for the lead

1. **Search criterion.** 08 §6 says "search <300 ms". Read as all of search plus suggest, it passes (285.5 ms).
   Read as the search endpoint alone, it fails: 590 ms on this laptop. Please decide which reading applies, and
   whether the staging run is the one that counts.
2. **POS sync batch.** The p95 of 678 ms for a 20-sale batch is inside the overall pos_rush p95, but it is above
   400 ms if you read it on its own.
3. **Soak.** It needs a clean 20-minute local run (`make load-soak`) on a stack nobody else uses, and the 2-hour
   run in staging (wave 5).
4. **Generic limiter** (`TODO(lead)` in ratelimit.go:31).
   - Observed traffic:
     - browsing is about 18 requests per minute per user
     - shop polling is 8 to 15 per minute
     - POS is about 3 sales per minute per shop
   - The current rules (auth 60/min; me, biz, admin and public 300 to 1,200) leave wide headroom.
   - Suggestion: key by principal when signed in, and keep the IP-keyed groups (public, auth) generous because of
     CGNAT on Indian mobile networks.
   - The OTP rules held under abuse. No product rule was invented.
5. **Shared stack.** Moving the shared test clock and running bootstrap left future-dated `otp_requests` that
   triggered 429s. Removing those 105 local rows fixed it. Load runs need the stack to themselves.
