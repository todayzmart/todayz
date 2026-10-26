# 05 · Observability, analytics and "what to fix" dashboards

Goal (user, 2026-10-22): track latencies, RPS, customers, every server-side signal, errors, client-side performance, retention, usage, RDS and retailer-side behaviour **on dashboards that make it easy to pinpoint what to improve**.

The design principle: every dashboard answers one question, starts with the few numbers that matter, and drills down from "what is bad" to "where" to "why" (metric → trace → log → code, or KPI → segment → store/customer). The top dashboard ranks problems by **impact**, so the first screen always says what to fix today.

## 1. Stack

| Signal | Tool (local, waves 0–4) | Tool (cloud, wave 5) |
|---|---|---|
| Metrics | Prometheus-compatible via OTel → `grafana/otel-lgtm` container (Mimir) | Grafana Cloud Mimir (free tier to start) |
| Logs | slog JSON → OTel → Loki | Grafana Cloud Loki |
| Traces | OTel → Tempo | Grafana Cloud Tempo |
| Errors | No Sentry before Wave 5 (D-17). Errors are `level=error` log lines with stack and fingerprint in Loki, shown on the local Grafana (`grafana.todayz.localhost`) | Sentry SaaS from Wave 5 (errors, releases, source maps, user impact) |
| Database | `postgres_exporter` + `pg_stat_statements` + Grafana Postgres datasource (read-only role) | same, plus CloudWatch (RDS: CPU, memory, IOPS, connections, replica lag, storage) and CloudWatch Database Insights (standard mode, Enhanced Monitoring every 60 s; Postgres logs exported to CloudWatch) |
| Infra | Docker stats via cAdvisor | CloudWatch: ALB (RPS, 5xx, target latency), Beanstalk (instance health, CPU), plus Cloudflare analytics (edge requests, cache, WAF blocks) through the Cloudflare GraphQL datasource |
| Client performance (RUM) | Our `/v1/rum` endpoint → histograms in Mimir + raw sample to Loki | same |
| Product analytics | Our `events` table + rollups in Postgres → Grafana (Postgres datasource) and the admin **Pulse** | same |
| Uptime | k6/blackbox exporter probing local endpoints | Grafana Synthetic Monitoring from Mumbai + Cloudflare health checks |

Why our own product analytics instead of a SaaS: the search/demand data is TodayZ's core asset (sold to brands later), must stay in our database, and needs joins with orders, stores and geography. Postgres handles it at v1 scale; `events` is partitioned daily and rollups run every 5 minutes (today) and nightly (history). If volume passes ~5 M events/day, the ADR path is ClickHouse.

Third-party services before Wave 5 (D-17): SMS, push and Sentry always use mocks or local tools. Anthropic and Sarvam use the mocks by default; live calls happen only if the user puts their own keys in `.env.local`. Dashboards and alerts must work with the mocks alone.

Every dashboard is code: JSON in `infra/grafana/dashboards/*.json` (generated with Grafonnet or kept as exported JSON), provisioned into the local LGTM container by `docker-compose` and into Grafana Cloud by Terraform. Alert rules live in `infra/grafana/alerts/*.yaml`. Nothing is hand-clicked.

## 2. Instrumentation rules

### 2.1 Metric naming and labels

- Prefix `tz_`. Units in the name (`_seconds`, `_bytes`, `_total`, `_paise`).
- Allowed labels (bounded): `service` (`api|worker`), `route` (template, e.g. `/v1/stores/{id}/orders`), `method`, `status_class` (`2xx|4xx|5xx`), `code` (problem code, ~60 values), `app` (`customer|business|admin|site`), `role`, `kind` (a registered job kind from the `jobs.Define` registry, D-95, about 40 values), `class` (the 7 job classes), `queue` (River queue names from the registry), `channel`, `provider`, `outcome`, `feature` (one per AI feature in 11), `model` (the two model ids set in `ai_model_fast` and `ai_model_smart`), `source` (the stock movement source enum, D-01), `city` (≤ 20), `category` (≤ 30), `lang`, `platform` (`android|ios|desktop|other`), `conn` (`4g|3g|wifi|slow-2g|unknown`), `app_version` (last 5 kept).
- **Never** as labels: user id, store id, order id, phone, query text, IP. Store-level and customer-level numbers come from Postgres rollups, not metrics. A unit test scans registered metrics and fails on any label not in the allow-list.
- Histograms use native/exponential buckets (OTel exponential histogram) so p50/p95/p99 are accurate without bucket tuning.

### 2.2 Logs

- JSON, one line per event: `ts, level, msg, service, env, version, request_id, trace_id, span_id, route, method, status, duration_ms, user_kind, role, store_id (ok, not PII), err_code, err, fingerprint`. `env` is `local|test|staging|prod`; there is no `dev` environment (D-15).
- PII redaction handler: drops/masks keys `phone, otp, code, token, authorization, cookie, address, pan, gstin, upi, name`; values matching an Indian mobile regex are masked. Test: a log of a request containing all of them produces none of them.
- **An OTP is never logged, in any environment, including `local` and `test`** (D-15). Local sign-in uses the reserved numbers `+91 90000 000xx` with `000000`; for any other number the message shows in the mocks UI (`mocks.todayz.localhost`), never in a log. A test sends a real OTP through the local stack and asserts the code appears in no log line, span attribute or Loki stream.
- Levels: `info` for request summaries (sampled 100% for 4xx/5xx, 10% for 2xx at > 50 RPS), `warn` for business rule refusals that look unusual, `error` only for things someone should look at.
- Retention: local 7 days; cloud 30 days (Loki), audit lives in Postgres.

### 2.3 Traces

- OTel spans for: HTTP server, every SQL query (pgx tracer, statement name from sqlc, not parameters), River jobs (linked to the enqueuing trace), outgoing HTTP (SMS, push, STT, LLM, R2), SSE publish.
- Browsers send `traceparent` on API calls (the `@todayz/telemetry` fetch wrapper), so a slow screen in RUM links to the backend trace.
- Sampling: 100% of errors and of requests slower than 1 s (tail-based in the collector), 10% of the rest; 100% locally.
- Exemplars on latency histograms link a p99 point in Grafana straight to a trace.

### 2.4 Errors (local: Loki and Grafana; Wave 5: Sentry)

What is captured:

- Backend: panics, 5xx, failed jobs after final retry, ledger invariant violations (always `fatal`).
- Frontends: unhandled errors, rejected promises, React error boundaries, failed chunk loads, service-worker errors.

Where it goes:

- **Local and test (waves 0–4):** Sentry is off; there is no Sentry test project (D-17). The backend writes each error as a `level=error` log line with stack, `err_code`, `trace_id` and a stable `fingerprint`. Frontend errors go in the RUM batch to `POST /v1/rum` and the API logs them the same way with `app` and `app_version`. Grafana groups them by fingerprint (Loki), which is what dashboard 00 panel 2 reads.
- **Wave 5 (staging, prod):** the same capture points also report to Sentry SaaS; source maps uploaded per release; `release = app@gitsha`, `environment`. Each Sentry issue links to a Grafana Explore query for the same `trace_id`.

In both: user context is the user id (ULID) and role only (no phone). Breadcrumbs and logged URLs scrub codes and tokens.

### 2.5 RUM (client side)

`@todayz/telemetry` collects, per page load and route change:

- Web vitals: LCP, INP, CLS, FCP, TTFB (with attribution: LCP element, INP target).
- Route render time (router start → data ready → painted), API call timings per route template (from the client's view, including network), long tasks > 200 ms, JS heap (Chrome), offline/online transitions, service-worker update events.
- Dimensions: app, route template, platform, conn type, effective RTT bucket, device memory bucket, app_version, city (from the saved location, coarse), lang.
- Sent with `sendBeacon` in batches to `POST /v1/rum`; the API turns them into histograms (`tz_rum_lcp_seconds{app,route,platform,conn}` …) and keeps a 1% raw sample in Loki for drill-down.

Lab data: Lighthouse CI runs in `make e2e` and writes its reports to the local filesystem only (D-58). Nothing is uploaded to a Lighthouse server or any third party. RUM is the field data; Lighthouse is the pre-release check.

### 2.6 Product events

The catalogue is split per domain (D-50): `shared/telemetry/events/<domain>.yaml` (for example `orders.yaml`, `pos.yaml`, `stock.yaml`, `ai.yaml`). Each work package edits only its own domain's file. The generator merges them into the TS emitter types and the Go validator; an event name defined in two files fails the build. Events are emitted from the client for UI intent and **from the server for facts** (an order is placed when the server says so, not when a button is tapped). Each event: name, required props, owner WP. Reason props use the enums in D-60 (reject, delivery-failed, admin-cancel and customer-cancel reasons); free text is never an event prop.

Core catalogue (v1):

| Area | Events |
|---|---|
| App lifecycle | `app_opened` (cold/warm, source: direct/push/qr/referral/share), `session_started`, `pwa_installed`, `push_permission` (granted/denied), `lang_changed` |
| Location and discovery | `location_set` (gps/pin/manual), `radius_changed`, `map_opened`, `category_opened`, `store_viewed`, `product_viewed` |
| Search | `search_performed` (query_norm, results, stores, radius, category, sort: relevance/nearest, has_offer, latency_ms, product_id when the query matches a master product, chips: the refine-chip kinds applied, source: typed/voice, D-101) [server], `search_result_clicked` (position, badge), `search_zero_results` [server], `refine_chip_removed` (kind) (D-101), `voice_search_used` (lang, seconds, outcome: searched/edited_then_searched/abandoned/cap_reached/failed; never audio, transcript or query text, so an abandoned voice search leaves no text) (D-101), `search_assist_used` (outcome: shown/tapped/empty/timeout/failed/cap_reached, latency_ms; never the query or the suggestion text) (D-101) |
| Asks (D-101) | `ask_created` (target: nearby/one_shop, has_product, quantity, need_by, recipients, low_yield) [server], `ask_replied` (answer: yes/no, seconds_to_reply, priced, listed, by_role) [server], `ask_closed` (reason: all_replied/window_ended/customer_closed, yes_count, recipients) [server], `ask_booked` (minutes_since_reply, price_source: ask_reply/catalogue) [server], `ask_price_expired` [server], `ask_reported` [server]. Never a customer id in props; `geo_cell` and the city only |
| Share (D-101) | `share_created` (surface: result_card/product_page, channel: whatsapp/system/copy) , `share_opened` [server, from `ref=share`]; no PII, no store item price |
| Cart and checkout | `cart_item_added`, `cart_store_switched`, `checkout_started`, `login_prompted`, `login_completed`, `checkout_quote_failed` (reason), `credits_applied` |
| Orders [server] | `order_placed`, `order_accepted` (seconds_to_accept, partial), `order_rejected` (reason), `order_expired` (reason: not_accepted/no_show/dispatch_timeout), `order_cancelled` (by: customer/shop/admin/system, reason; the shop uses `ShopCancelReason` and the system uses `partial_timeout`, D-68), `order_delivery_failed` (reason), `order_return_confirmed` (outcome: returned/damaged/unconfirmed, hours_after_failure; `unconfirmed` is the 72 h auto-confirm, D-69), `order_partial_confirmed` (accepted/declined, credits_released), `order_extended`, `order_ready`, `order_collected` (minutes_from_accept), `order_delivered`, `code_failed`, `code_locked` |
| POS [server] | `pos_sale` (lines, total, linked, link_method: bill_qr/counter_code/phone_unverified, offline), `pos_reversed`, `bill_qr_shown`, `bill_claimed`, `counter_code_used`, `offline_link_failed` (reason), `sync_completed` (items, lag_s) |
| Stock [server] | `stock_updated` (source: the movement source enum from D-01, `opening`/`restock`/`supplier_bill`/`import`/`count_correction`/`damage`/`expired`/`sale_pos`/`sale_online`/`pos_reversal`; via: pos/voice/bill_scan/manual/bulk_import), `item_listed`, `product_created_pending`, `bulk_import` (rows, errors). Reservations are not stock movements and emit no `stock_updated`. |
| AI drafts [server] | `ai_draft_created` (feature, model, lang, lines, matched_ratio, latency_ms), `ai_draft_confirmed` (feature, accepted_lines, edited_lines, blocked_lines), `ai_draft_discarded` (feature). Voice stock update is feature `voice_stock` (D-46). |
| Money [server] | `credits_earned` (source), `credits_spent`, `credits_expired`, `referral_signup`, `referral_qualified`, `payout_marked_paid` |
| Business engagement | `biz_app_opened`, `insight_viewed` (kind, surface: card or tab), `insight_action` (kind, action type: restock/create_offer/flash_offer/add_product/mark_expired/edit_price/check_cost/count_stock/see_customers), `insight_dismissed` (kind, reason: not_useful; never for `expired_on_shelf`, which cannot be dismissed), `coach_card_shown` (detector), `coach_card_action` (detector, done/dismissed), `banner_created`, `banner_impression` [server; counted exactly into `banner_daily`, raw event sampled], `banner_tapped` [server; feeds attribution], `banner_removed` (by: moderation), `offer_redeemed` [server], `offer_created`, `review_replied`, `settings_changed` (key) |
| Onboarding [server] | `business_created` (by: self/agent), `business_submitted`, `business_verified` (hours_to_verify), `first_product_listed`, `first_order_received` |
| Notifications [server] | `notification_sent` (channel, kind), `notification_opened` |
| Support | `ticket_opened` (category), `ticket_resolved` (hours) |

Identity: `anon_id` (random, first-party, localStorage) before sign-in, merged to `user_id` at login. Location in events and rollups is an H3 resolution-8 cell, never coordinates (D-59). Consent: analytics events are first-party and pseudonymous; the privacy notice explains it.

## 3. Rollups and derived metrics (Postgres)

River periodic jobs. Every row is a registered kind (`jobs.Define`, class `rollup` unless noted; W3-07 registers its rollups through W1-11's interface, D-95). Each one is idempotent per period (`UniqueBy` the period key); backfill command `todayz rollup --from --to`, and the same runs appear on A-21 with trigger `backfill`:

| Job | Every | Writes | Key definitions | Owner |
|---|---|---|---|---|
| `rollup_today` | 5 min | today rows in `rollup_daily_platform`, `rollup_daily_store` | | W3-07 |
| `rollup_day` | 00:30 IST | final yesterday rows | | W3-07 |
| `rollup_cohorts` | daily | `rollup_cohort_weekly` | customer cohort = week of first `order_collected` or `order_delivered` or linked POS sale; store cohort = week verified | W3-07 |
| `rollup_demand` | hourly | `rollup_daily_search_demand` | demand is keyed by H3 resolution-8 cell (D-59) and, when the search carries a `product_id` (written by W1-06 when the query is an exact barcode or equals a master product's name or synonym; D-99), by `product_id` (used by the "missed sales" insight). The table and its `product_id` column are created in W3-07's own migration (307xx block, D-89) | W3-07 |
| `rollup_item_pairs` | 02:00 IST (nightly) | `rollup_item_pairs` | per store, for the last `insight_pair_window_days` whole IST days ending yesterday: bills containing both items of a pair, bills containing each item, and the store's total bills (`bills_total`). A bill is a completed online order or a POS sale; an item counts once per bill. Item A is the item of the pair with more bills in the window (ties: the lower item id) and B is the other; the share is `bills_both ÷ bills_A`. A pair fires only when `bills_both ≥ insight_pair_min_bills`, the share is at least `insight_pair_min_share_bp`, and the lift, `(bills_both × bills_total) ÷ (bills_A × bills_B)`, is at least `insight_pair_min_lift_pct` (default 150, 1.5×; 100 means no lift needed). The job stores the lift as `lift_bp`, lift × 10000 (21428 for 2.14×), and a pair fires when `lift_bp ≥ insight_pair_min_lift_pct × 100`. The job filters on the bill floor only: it stores every pair with `bills_both ≥ insight_pair_min_bills` and keeps the top 100 per store by lift (`lift_bp` is stored). The share and lift tests run when insights are read, so changing either setting takes effect at once; changing `insight_pair_min_bills` or `insight_pair_window_days` waits for the next nightly run or `todayz rollup`. Registered in the D-95 job registry as a kind of class `rollup`. Feeds the "bought together" insight and the matching coach detector (D-99) | W3-07 |
| `rollup_banners` | 5 min (today), 00:30 IST (final) | `banner_daily` (banner, day, impressions, clicks, attributed orders) | an order is attributed to a banner when the same customer orders at the same store within 24 h of a click on that banner; if they clicked more than one banner, the last click wins and only that banner gets the order (D-60, D-77) | W3-07 |
| `store_reliability` | hourly | the reliability fields only: `stores.accept_rate_30d`, `unaccepted_expiry_rate_30d` and the store's strikes in the last 30 days | feeds ranking; store strikes lower the score (D-67); W1-06 treats a missing value as 1.0 (D-52). It never writes the rating. At `store_strike_risk_threshold` (5) strikes in 30 days it raises a `risk_flags` row (rule `store_strikes`); there is no automatic pause (D-67) | W2-02 |
| `risk_scan` | 5 min | `risk_flags` rows (rules `orders.code_lockouts`, `pos.bill_claim_spread`, `pos.reversal_ratio`, `referral.self_like`, `identity.otp_ip_fanout`, `identity.otp_failed_phone`) | each rule reads its `risk_*` setting each run; a flag is raised once per subject and rule per IST day (lockouts, bill claims, OTP), per window (reversal ratio) or ever (self-like referral). It never blocks a person or moves money (D-78, D-225) | W3-02 |
| `store_rating` | hourly | `stores.rating_avg`, `stores.rating_count` | Bayesian rating; the only writer of the store rating (D-59) | W2-06 |
| `ledger_reconcile` | nightly | alert on drift | invariants §6 of data model | W2-01 |
| `rollup_ask_demand` | 01:30 IST (nightly) | `rollup_daily_ask_demand` | per receiving store, per cell and product (or query): asks and asks that got no Yes; an ask whose customer searched the same product in the hour before is left out of the demand sums (01 §11.2, D-101) | W3-16 |
| `site_marquee_build` | 03:00 IST (nightly) | `site_demand_marquee` (status `proposed`) | per city, master products from `search_performed` with a `product_id` in the last `site_marquee_window_days`, at least `site_marquee_min_searchers` distinct signed-in `user_id`s (`anon_id`-only events add to the totals, never to this floor, D-101), denylisted categories dropped, top `site_marquee_max_items` by searchers (01 §11.3, D-101) | W3-16 |

Definitions (written once, used everywhere; in `docs/plan/05` and as SQL views `v_metric_*`):

- **Linked POS sale**: a POS sale with `link_method` `bill_qr` or `counter_code`. A `phone_unverified` sale only adds the customer to the shop's list; it counts as "shop sales recorded", not as a linked sale or a transacting customer (D-07).
- **Active customer (DAU/WAU/MAU)**: a signed-in or anonymous customer with `app_opened` that day/7d/30d. **Transacting customer**: completed an order or linked POS sale in the period.
- **Active store**: verified store with ≥ 1 staff app open or POS sale that day. **Live store**: active and ≥ 50 listed items with stock updated in the last 72 h.
- **GMV**: Σ total of collected + delivered orders (online GMV) and Σ linked POS sales (POS GMV, reported separately; unlinked POS sales are "shop sales recorded").
- **Order success rate**: (collected + delivered) / placed. Broken into the leaks: rejected, expired-unaccepted, cancelled by customer (including `declined_partial`), partial timeout (`cancelled` by the system with `partial_timeout`, D-68), no-show (pickup only), dispatch timeout (delivery only, D-21), cancelled by shop (by `ShopCancelReason`, D-68), cancelled by admin, delivery failed.
- **Search success rate**: searches with ≥ 1 result in radius / searches. **Search-to-order conversion**: sessions with search and an order within 24 h / sessions with search.
- **Retention**: D1/D7/D30 (returned with `app_opened` on day N ± 0 after first open); W1–W12 transacting retention by cohort; store W1–W12 retention (active store).
- **Time to accept**: placed → accepted, p50/p90 per store.
- **Freshness**: share of listed items updated in the last 24 h / 72 h.

## 4. Dashboards

Folder `TodayZ` in Grafana. Each dashboard has the standard top row: time range, `env`, `app`/`service`, and a text panel "How to read this / runbook link".

### 00 · What to fix today (the pinpoint dashboard)

One screen, refreshed every minute, ranked lists only:

1. **Slowest endpoints by impact**: table of route × method with RPS, p50, p95, p99, error %, and **impact score = RPS × max(0, p95 − budget)** (budget 200 ms reads / 400 ms writes), sorted desc. Row click → 02 API drill-down filtered to the route, with an exemplar trace.
2. **Top errors by users affected**: error log lines grouped by `fingerprint` (Loki) locally, Sentry issues (Sentry datasource) from Wave 5, plus API `code` 5xx counts; sorted by distinct users; new-since-last-release flag.
3. **Slowest SQL**: top 10 from `pg_stat_statements` by total time (calls × mean), with mean, rows, cache hit; link to the query name in code.
4. **Worst client screens**: route × app with p75 LCP/INP above budget, sorted by sessions × excess.
5. **Biggest funnel leak today vs 7-day baseline**: each leak step (search zero-result, checkout quote fail, unaccepted expiry, no-show, cancel) with today's rate, baseline, and Δ in lost orders.
6. **Stores hurting customers**: top 10 stores by expired-unaccepted orders + rejections today, with avg time to accept (links to the admin store page).
7. **Zero-result searches nearby**: top queries with no results, by city (product/supply gaps).
8. **Jobs and notifications failing**: job kinds with failures, missed schedules or backlog (row click → 15 · Jobs filtered to the kind); notification channels with failure rate > 2%.
9. **SLO burn**: error budget remaining for each SLO (§6).

### 01 · Service overview (golden signals)

- RPS total and by app (stacked), 2xx/4xx/5xx rate, error ratio vs SLO line, p50/p95/p99 latency, saturation (Go: goroutines, GC pause, heap; process CPU/mem; DB pool in use/idle/wait), instance count, deploy annotations (every release marks the graphs).
- Edge (cloud): Cloudflare requests, cache hit ratio, WAF/rate-limit blocks, origin latency.

### 02 · API endpoints (RED by route)

- Variable `route`. Table of all routes: RPS, error %, p50/p95/p99, apdex. Heatmap of latency for the selected route. Error codes breakdown (`code` label). Status by `role`/`app`. Exemplars → Tempo. Logs panel (Loki) filtered to the route and `level>=warn`.
- Latency breakdown per route from traces (Tempo TraceQL metrics): time in DB, in outbound HTTP, in handler.

### 03 · Database (RDS / Postgres)

- Connections (by state), pool wait time, transactions/s, commits vs rollbacks, deadlocks, lock waits, longest running query, replication lag (later), cache hit ratio, temp files, table/index bloat estimate, sequential scans on big tables, dead tuples and autovacuum activity, slowest queries table (mean, p95 via `pg_stat_statements` + `pg_stat_monitor` if allowed), top tables by size/growth, partitions present for `events`/`audit_log` (missing future partition = alert; `stock_movements` is not partitioned in v1, D-34, so watch its size and BRIN index instead).
- Cloud only: CloudWatch CPU, freeable memory, read/write IOPS and latency, disk queue, free storage, burst balance, Database Insights top waits.

### 04 · Jobs, realtime, notifications, integrations

- River: queue depth and age of the oldest available job, per queue (W1-07). Job-level numbers (runs, failures, retries, **timer lateness**: actual run − scheduled time, p99 must stay < 10 s since late expiries mean wrong holds) live on **15 · Jobs**; the panel here links to it.
- SSE: open connections by app, events published/s, delivery lag (publish → client ack, sampled), reconnects/min.
- Notifications: sent/delivered/failed by channel and kind, provider latency, SMS cost per day (₹), push permission rate, open rate.
- Integrations: STT and LLM latency, errors, tokens and cost per day (AI detail lives on 14); SMS provider errors; R2 errors. Locally every provider is a mock (D-17); the panels show the mock's numbers.

### 05 · Customer app experience (RUM)

- p75 LCP/INP/CLS/TTFB by route (table with budget colour), trend by app version (did the last release regress?), split by platform, connection type and city, JS errors per 1k sessions, chunk-load failures, service-worker update adoption, offline sessions, API latency as seen by clients vs server (difference = network/edge), top slow API calls from the client's view.

### 06 · Business app and POS experience

- Same RUM panels for business.todayz.in, plus POS specifics: scan-to-line latency, sale save time, **offline outbox size and sync lag** (device → server) distribution, devices offline > 1 h, sync conflicts by type, `resync_required` responses, POS sales offline vs online, offline customer links accepted vs failed (D-12), order alert latency (order placed → business device received, p95), audio-alert enablement rate, camera scanner usage vs keyboard scanner.

### 07 · Customer funnel and usage

- Funnel (selectable window): app opened → location set → search/browse → store viewed → product viewed → added to cart → checkout started → login (if needed) → order placed → accepted → collected/delivered. Conversion % per step and Δ vs previous period; split by city, category, new vs returning, entry source (QR standee, referral, push, direct).
- Search: searches/min, zero-result rate, top queries, top zero-result queries, result click position, search latency p95, radius chosen distribution.
- Asks (D-101): ask funnel per city (asks created → at least one Yes → booked from a reply) and reply rate (replies ÷ recipients, Yes share, median time to reply), asks closed with no reply, asks per store per day against `ask_daily_limit_store`; shares created and opened; voice searches, search assist outcomes (with timeouts and failures) and refine chips removed. From `events` and `asks`, never per customer.
- Usage: DAU/WAU/MAU, DAU/MAU stickiness, sessions per user, session length, feature usage (map, radius chips, bill QR scan, wallet, referrals, reviews), language mix, PWA install rate, push opt-in rate.

### 08 · Marketplace health (orders)

- Orders placed/hour, GMV (online and linked POS), AOV, items/order, pickup vs delivery share, credits used per order.
- **Order outcome breakdown** (stacked 100%): collected, delivered, rejected (by reason, D-60), expired-unaccepted, cancelled by customer, partial timeout (D-68), no-show, dispatch timeout, cancelled by shop (by reason, D-68), cancelled by admin, delivery failed (by reason, D-60). Trend lines for each leak.
- Time to accept distribution, time from accept to collect, extension usage, partial acceptance rate and customer acceptance of partials, code failures/lockouts, auto-accept adoption.
- Supply: verified stores, live stores, in-store-only stores (pickup and delivery both off, D-41), listed items, freshness % (24 h / 72 h), stores with stale stock, coverage map (stores per H3 resolution-8 cell vs searches per cell → under-supplied areas).
- Credits blocked by caps, by cap kind (D-30, D-76): store daily cap (quotes that showed "Credits can't be used at this shop today") and customer monthly cap (quotes that showed "You've used this month's credit limit").
- Delivery-failed returns (D-69): open returns waiting for the shop, returns confirmed as `returned` / `damaged`, and returns auto-confirmed after 72 h as `unconfirmed` (each one also raises a risk flag).

### 09 · Growth and retention

- New customers/day by source; referral signups and qualification rate; cost of referral rewards (credits from `platform_marketing`) per qualified customer.
- Cohort heatmaps: customer W0–W12 transacting retention; D1/D7/D30 app retention; store W0–W12 active retention.
- Resurrected and churned users (active last 30 d → none in 30 d).
- Credits economy: issued (cashback, referral, adjustment), spent, expired, outstanding liability (Σ customer wallets + held), breakage rate.

### 10 · Retailer health (supply side)

- Store funnel: created → submitted → verified → first product → 50 products → first order → active in week 4. Time between steps, drop-off by step, by field agent and by self-serve.
- Active stores daily/weekly, POS usage (stores with POS sales, sales/day, share linked by `bill_qr` / `counter_code` / `phone_unverified`), stock-update activity (by movement source from D-01: `opening`, `restock`, `supplier_bill`, `import`, `count_correction`, `damage`, `expired`, `sale_pos`, `sale_online`, `pos_reversal`; and by entry route: POS, voice, bill scan, manual, bulk import), freshness distribution, acceptance rate distribution, unaccepted-expiry rate distribution (stores above `shop_expiry_penalty_threshold_bp` flagged), store strikes in 30 days (stores at `store_strike_risk_threshold` flagged, D-67), median time to accept, rating distribution, review reply rate.
- Engagement with features: Demand insights viewed, actions taken and "Not useful" per kind (D-99), offers created and their uplift (orders on offer items vs before), banners (impressions, clicks, attributed orders from `banner_daily`), voice update usage and confirmation/edit rate, cashback % distribution and its link to repeat orders.
- **Store table** (Postgres, sortable): store, city, category, orders 7 d, success %, avg accept time, freshness %, rating, POS sales 7 d, last active; filter "needs attention" (low freshness, high expiry, no activity 7 d) → feeds field agents' follow-up list.
- Field agents: shops onboarded/verified/live per agent per week.

### 11 · Money and payouts

- Store credit balances (sum, top 20), negative balances, statements by status this month, payouts paid (₹) and pending, holds by reason, ledger reconcile status (must be "OK"), manual adjustments by admin, two-person approval requests pending and their age (D-05), POS reversals with credits returned and cashback clawed back (D-08), wallet forfeits on account deletion (D-31).

### 12 · Security and abuse

- OTP sends/verifies, verify success rate, OTP send rate by IP /24 and by phone prefix (anomalies), Turnstile challenge/fail rates, 401/403 rates by route, refresh-token reuse detections (should be ~0), admin logins and step-ups, admin sensitive actions per hour, WAF blocks, rate-limit hits, risk flags opened by rule (including `store_strikes` from D-67 and unconfirmed delivery-failed returns from D-69), referral rejections by reason (including `rejected_same_device`, D-13), bill-claim failures, code lockouts by store, offline counter-code links per store per day against `pos_offline_links_risk_per_day` and offline link failures (D-12). OTP panels count sends and verifies; they never show an OTP.

### 13 · Cost

- Daily ₹ estimate: SMS, WhatsApp, STT, LLM tokens (total only; per-feature detail on 14), R2 storage/egress, AWS (from Cost Explorer later), Grafana/Sentry usage vs free-tier limits (cloud only).

### 14 · AI quality and cost (owner W3-10)

Answers "which AI feature is weak or expensive?" (D-49, D-46). Sources: the `tz_ai_*` metrics and the `ai_drafts` table.

- Metrics (W3-10's client wrapper emits them for every call): `tz_ai_calls_total{feature,model,outcome}`, `tz_ai_tokens_total{feature,model}`, `tz_ai_cost_paise_total{feature,model}`, `tz_ai_latency_seconds{feature,model}`.
- Cost: ₹ per day per feature and model, against `ai_daily_cost_cap_paise` (default 50000, ₹500 a day, D-70) with an 80% line; calls refused by the per-store and per-customer caps (`ai_daily_calls_store`, `ai_daily_calls_customer`) and by the daily cost cap. There is no monthly AI budget (D-70).
- Speed and failures: p50/p95 latency per feature, error and timeout rate, schema-validation and number-grounding rejections.
- Quality, from `ai_drafts` (feature, actor, input ref, prompt version, model, output, per-line outcomes, cost, latency): per feature, the share of lines accepted / edited / discarded, and drafts discarded whole; split by prompt version so a prompt change that lowers acceptance shows at once. Voice stock update is feature `voice_stock`.
- Coach: cards shown, Done, Not useful and ignored, per detector.
- Lines blocked by permission (cashier confirming a `set_price` or `restock` line, D-06, D-65), per feature. Voice line kinds are `sale`, `restock`, `count`, `set_price` (D-65).
- Locally the numbers come from the mock LLM unless the user has put their own key in `.env.local` (D-17).

### 15 · Jobs (owner W1-11, D-95)

Answers "did every background job run, on time, and what did it do?". Sources: the `tz_job_*` metrics (§8) and the `job_runs` and `job_stats_hourly` tables (Postgres datasource).

- Top row: kinds failing now, kinds missed now (`tz_job_missed`), jobs paused, oldest available job age, hard-cancelled at shutdown (24 h).
- Table of every kind: class, owner, last run status and time, next run, 7-day success rate (`succeeded / (succeeded + failed + cancelled + discarded)`; `skipped` and `yielded` are excluded), average and maximum duration, paused. Row click → run history for the kind with each run's summary counts.
- Per kind (variable `kind`): runs by status over time, duration p50/p95/max, lateness p99, attempts and retries, the summary counts over time (for example `sent`, `skipped`).
- Timer class: lateness p99 against the 10 s target and the 30 s alert line.
- Logs panel (Loki) filtered by `kind` and `run_id`; exemplars → Tempo by `trace_id`.
- Links to A-21 for controls. Grafana is read-only for jobs; pausing and running happen only on A-21 (audited).

Dashboard ownership: W1-07 owns 01–04, W3-07 owns 00 and 05–13, W3-10 owns 14, W1-11 owns 15.

### Admin console Pulse (inside the product, not Grafana)

For founders and ops without Grafana: GMV, orders, success rate, active customers, active/live stores, new stores, D7 retention, top zero-result searches, stores needing attention, a city map with stores and demand. Same SQL views as dashboards 07–11 so numbers match. All settings-based numbers shown in Pulse and in alerts read `platform_settings`; none are hard-coded.

### Retailer analytics (inside the business app)

Each store sees its own: sales by hour/day, online vs POS, top items, orders funnel (received → accepted → collected), avg time to accept vs nearby median (anonymised benchmark, k ≥ 5 stores; coach statements that use a benchmark need k ≥ 10, D-71), repeat customers, ratings, Demand insights (the Insights tab of B-12 lists every kind; the same facts feed the coach, D-99). This is the retailer-facing slice of the same rollups.

## 5. Alerts

Routing: local → console + a local Grafana contact point (no paging). Cloud → `critical` to phone push/SMS (Grafana OnCall or simple SMS webhook) + email; `warning` to email/Slack-equivalent (WhatsApp group webhook if chosen). Every alert has a runbook in `docs/runbooks/<alert>.md` with six sections: What it means, First five minutes (dashboard links), Diagnosis, Mitigation, Escalation, Follow-up (D-236). `make check` runs `check-runbooks` (`scripts/ops/runbooks_check.py`), which fails on a missing runbook, section, dashboard or link.

| Alert | Condition | Severity |
|---|---|---|
| API availability SLO fast burn | 5xx ratio burn rate > 14.4× over 1 h and 5 m | critical |
| API availability SLO slow burn | burn rate > 6× over 6 h and 30 m | warning |
| API latency | p95 reads > 400 ms or writes > 800 ms for 10 min | warning |
| Order timers late | `tz_job_lateness_seconds` p99 > 30 s for 5 min on the `timer` class kinds (the rule's `kind` list is generated from the registry) | critical |
| Job backlog | oldest available job > 2 min | warning |
| Job failing (D-95) | 3 consecutive failed runs of a scheduled kind, or more than 20% of an event kind's runs failing over 15 min. Owner W1-11, `alerts/jobs.yaml` | critical for the `timer` and `money` classes, warning otherwise |
| Cron missed (D-95) | `tz_job_missed{kind}` = 1: a scheduled, unpaused kind had no succeeded run within 2× its interval (daily kinds: 15 min grace). Owner W1-11, `alerts/jobs.yaml` | critical for `money`, warning otherwise |
| Job hard-cancelled at shutdown (D-98) | `tz_job_hard_cancelled_total` > 0 in 10 min: a job did not finish inside the drain and was cancelled; it is retried. Owner W1-11, `alerts/jobs.yaml` | warning |
| Ledger invariant | reconcile drift ≠ 0 or invariant error logged | critical |
| DB connections | pool wait p95 > 50 ms or connections > 80% max | warning |
| DB CPU / storage | CPU > 80% 15 min; free storage < 20% | warning |
| Missing partition | next month's partition absent 7 days before | warning |
| Idempotency completion failed | `tz_idempotency_complete_failures` > 0 in 10 min (the key stays claimed, so a retry gets 409 until the 3-minute lease runs out and the same request can claim it again; owner W1-07) | warning |
| Order alerts slow | order alert latency p95 > 10 s for 10 min | critical |
| SSE down | open business connections drop > 50% in 5 min (business hours) | warning |
| SMS failures | OTP SMS failure > 5% for 10 min | critical |
| OTP abuse | sends > 5× baseline in 10 min or one /24 > 50/h | warning |
| Refresh token reuse | > 5 in 10 min | warning |
| Store not accepting | a store has ≥ 3 expired-unaccepted orders in 2 h | ops warning (to support queue, not paging) |
| Zero-result spike | zero-result rate > 40% in a city for 1 h | ops info |
| Client errors | JS error sessions > 2% for 30 min, or new release error rate 2× previous | warning |
| RUM regression | p75 LCP on customer `/` or `/search` > 3 s for 1 h | warning |
| POS sync lag | ≥ 5 devices with outbox > 1 h old during business hours | warning |
| AI cost | AI spend today (IST day) ≥ 80% of `ai_daily_cost_cap_paise` (D-70); at 100% AI calls stop for the rest of the day | warning (owner W3-10) |
| Store strikes | a store reaches `store_strike_risk_threshold` (5) strikes in 30 days; a risk flag is raised, no automatic pause (D-67) | ops warning (to the risk queue, not paging) |
| Unconfirmed returns | a delivery-failed return is still unconfirmed after 72 h and is auto-confirmed (D-69) | ops info (risk flag raised) |
| Certificate / uptime (cloud) | synthetic check fails 2 of 3 locations | critical |

## 6. SLOs

| SLO | Target (30 days) | SLI |
|---|---|---|
| API availability | 99.5% | non-5xx / all requests (excluding 429) |
| API latency | 95% of reads < 200 ms, writes < 400 ms | histogram |
| Order placement success | 99.5% of valid checkouts create an order | `POST /orders` 2xx / attempts without 4xx business errors |
| Order alert delivery | 99% within 5 s | placed → business device ack |
| Timer accuracy | 99.9% of expiries within 30 s of due | `tz_job_lateness_seconds` histogram, `timer` class |
| POS sync | 99% of offline sales synced within 2 min of coming online | sync lag |
| Customer LCP | p75 < 2.5 s | RUM |

## 7. What "done" means for observability in every WP

- New endpoints get RED metrics automatically from middleware (verify they show on 02 with the route template).
- New jobs register through `jobs.Define` (a `kind` with a class and owner) and appear on 15 and A-21; timers report lateness; a `timer` or `money` kind ships with its runbook.
- New business facts emit the catalogued server event; new screens emit RUM with a stable route template.
- Any new external call is wrapped with the integration client helper (metrics, spans, cost counter). Any new AI feature records each draft in `ai_drafts` and appears on 14.
- New events go in the WP's own `shared/telemetry/events/<domain>.yaml`.
- If the WP adds a failure mode a human must react to, it adds an alert and a runbook.
- The QA agent checks the dashboards show the WP's data after running its scenarios locally (screenshot attached).

## 8. Jobs (D-95, D-98)

Every background job is a registered kind (02 §2.7). The jobs middleware in `internal/platform/jobs` (W1-11) emits the metrics, writes the run row and logs; a handler does none of it by hand.

**Metrics** (the `kind` and `class` labels come from the registry and are in the 2.1 allow-list):

| Metric | Labels | Meaning |
|---|---|---|
| `tz_job_runs_total` | `kind`, `class`, `status` | Attempts by outcome: `succeeded`, `failed`, `cancelled`, `skipped`, `discarded`, `missed`, `yielded` |
| `tz_job_duration_seconds` | `kind` | Run duration (exponential histogram) |
| `tz_job_lateness_seconds` | `kind` | Actual start − scheduled time. Replaces the earlier timer lateness metric |
| `tz_job_missed` | `kind` | Gauge, 1 while the watchdog sees the kind as missed |
| `tz_job_hard_cancelled_total` | `kind` | Attempts cancelled by `StopAndCancel` at shutdown (D-98) |
| River queue depth, age of the oldest available job | `queue` (a bounded label: the queue names in the registry) | W1-07 emits the River numbers; W1-11 adds the rest |

**Logs.** Each run logs its start and finish with `kind`, `run_id` and `trace_id`. A-21's run detail links to Grafana Explore: Loki by `run_id`, Tempo by `trace_id`. Errors stored on a run are scrubbed (no phone, no token, at most 2 kB); the full log line is scrubbed the same way.

**Watchdog.** `jobs_watchdog` (class `maintenance`, every 5 min) writes one `missed` run row per missed period (never one per tick) and sets `tz_job_missed{kind}` to 1 for a scheduled, unpaused kind with no succeeded run within 2× its interval. Daily kinds get 15 min of grace after the expected time. It resets the gauge to 0 on the next success.

**Dashboard and alerts.** Dashboard 15 (§4) and `infra/grafana/alerts/jobs.yaml` (§5: Job failing, Cron missed, Job hard-cancelled at shutdown). Each alert has a runbook in `docs/runbooks/<alert>.md`; every `timer` and `money` kind also has its own runbook, required by the registry.

**Local.** Under the test clock (`/v1/test/clock`), `POST {"advance":"1h"}` runs due scheduled kinds, so dashboard 15 and A-21 have data. `make demo` runs jobs too.
