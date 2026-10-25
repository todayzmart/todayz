# 11 · AI features

The user asked (2026-10-22) for AI features wherever they are best, for retailers, consumers or admins. I picked these by one test: **does it remove real effort from a busy shopkeeper, a shopper or our ops team, and is it safe when the model is wrong?** Features that would move money, change stock, or give medical/financial advice without a human check are out.

## 1. Rules for every AI feature

1. **Draft, then confirm.** AI output is a proposal shown to a person. Nothing changes stock, prices, credits, orders, reviews or KYC until the person confirms. The confirm endpoint re-validates everything as if a human had typed it.
2. **Off until configured.** Each feature has one flag in `platform_settings` (D-25; names exactly as in [01-product-rules.md](01-product-rules.md) §17, D-61) and needs `ANTHROPIC_API_KEY` (and `SARVAM_API_KEY` for speech) in env. A feature works only when its flag is on and its key is set (D-19); otherwise the UI entry point is hidden, not broken. In `local` and `test` the mock LLM and STT (W0-07) count as configured, so every feature is testable without real keys.

   | Feature | Flag |
   |---|---|
   | AI-R1 Voice stock update | `ai_voice_stock_enabled` (and `feature_voice_enabled`) |
   | AI-R2 Supplier bill scan | `ai_billscan_enabled` |
   | AI-R3 Shelf photo to catalogue | `ai_shelfscan_enabled` |
   | AI-R4 Insight explanations | `ai_insighttext_enabled` |
   | AI-R5 Shop coach (AI-written cards; template text when off) | `ai_coach_enabled` |
   | AI-R6 Review reply drafts | `ai_replydraft_enabled` |
   | AI-R9 Banner maker and offer copy | `ai_bannercopy_enabled` |
   | AI-R10 Banner image safety check | `ai_imagecheck_enabled` |
   | AI-C1 List to basket | `ai_listparse_enabled` |
   | AI-C2 Multilingual search assist (model suggestions after the results, D-101) | `ai_querynorm_enabled` |
   | AI-C5 Voice search (speech only; needs `SARVAM_API_KEY`, not the model key, D-101) | `ai_voicesearch_enabled` |
   | AI-A1 Catalogue clean-up assistant | `ai_catalogue_enabled` |
   | AI-A2 Support triage and reply drafts | `ai_triage_enabled` |
   | AI-A3 Risk flag explanations | `ai_riskexplain_enabled` |

   AI-C3 substitutes are rule-based in v1 and have no AI flag.
3. **Models.** Code reads the model from settings and never hard-codes it (D-25): `ai_model_fast` (default `claude-haiku-5-5`: fast, cheap, multilingual) for every feature, and `ai_model_smart` (default `claude-sonnet-5-5`) only where the evaluation shows the fast model misses the bar (bill/invoice photos).
4. **Structured output only.** Every call uses tool/JSON schemas validated server-side. Free text is shown only where it is clearly labelled as a suggestion (review reply draft, weekly summary).
5. **Grounded.** Prompts get our data (store item candidates, rollup numbers), not open-ended knowledge. Any free-text output (insights, reply drafts, weekly summary) may only contain numbers from the backend input it was given; a validator rejects drafts that contain numbers not in the input. For reply drafts that input is the review text and the linked order facts.
6. **Privacy.** No phone numbers, addresses, names of customers, PAN/GST or codes are sent to the model. Customer text (search lists, support messages) is sent with a PII scrubber first.
7. **Cost guard.** Per-store and per-user daily call caps (`ai_daily_calls_store`, default 100; `ai_daily_calls_customer`, default 30), a separate per-store budget for background wording (`ai_daily_calls_store_background`, default 20: AI-R4 insight sentences and AI-R5 coach cards, which never take from the calls the owner starts and, when spent, show the template with no error, D-228), a global daily spend cap `ai_daily_cost_cap_paise` (super_admin, unit paise; default 50000, ₹500 a day, D-70; AI calls stop for the rest of the IST day when it is reached, and an alert fires at 80%). When a daily cap is hit, the call returns `429 ai_daily_cap_reached` with `extensions.scope` = `customer`, `store` or `platform` and `Retry-After` set to the next IST midnight. The screen shows "AI help is paused until tomorrow." and the entry point is disabled, with no retry button. There is no monthly AI budget; the daily cap is the only spend limit (D-70), and metrics `tz_ai_calls_total{feature,model,outcome}`, `tz_ai_tokens_total`, `tz_ai_cost_paise_total`, `tz_ai_latency_seconds` on dashboard 14 "AI quality and cost" (D-49).
8. **Evaluated.** Each feature has a fixture set in `backend/internal/ai/<feature>/eval/` and an offline eval command (`todayz ai eval <feature>`) with a pass bar. Prompt changes must not drop the score. Evals run against recorded fixtures in `make check`. Live-model evals (`make ai-eval`) are opt-in: they run only when the user has chosen to put their own `ANTHROPIC_API_KEY` (and `SARVAM_API_KEY` for speech) in `.env.local` (D-17). No default target needs them.
9. **Prompts are code.** `prompt.md` files next to the code, versioned; the prompt version is stored on every draft for later review.
10. **Feedback loop.** Every draft records accepted / edited / discarded per line; dashboard 14 shows acceptance rate per feature so weak features are visible.
11. **One drafts table.** Every AI draft is a row in `ai_drafts` (D-46): feature, actor, input ref, prompt version, model, output, per-line outcomes, cost, latency. W3-10 owns the table. The old `voice_drafts` table is folded into it as feature `voice_stock`. Two customer features are not drafts and write no row (D-101): AI-C5 voice search returns a transcript to the app only, and AI-C2 search assist returns suggestion chips; both store nothing (no audio, transcript or query), and the customer acts only by tapping (editing and running the search, or tapping a chip), so the person still confirms every effect. The cost guard (rule 7) counts both, and `tz_ai_calls_total` records their outcomes.

## 2. Retailers (business app)

| ID | Feature | What it does | Why | v1? |
|---|---|---|---|---|
| AI-R1 | **Voice stock update** (W3-04, which depends on W3-10; drafts are `ai_drafts` rows with feature `voice_stock`) | Speak sales, restocks, counts, price changes; draft lines to confirm. Line kinds are `sale`, `restock`, `count`, `set_price` (D-65): `sale` records a quick POS sale with no customer (`sale_pos`) and cashiers may confirm it; "out of stock" is a `count` of 0 | Hands busy at the counter | v1 |
| AI-R2 | **Scan a supplier bill to restock** | Photo of a distributor invoice/bill → line items matched to store items with quantities, cost price and (optionally) expiry dates; unmatched lines offered as "create product". Confirm → restock movements + batches. | Restocking is the biggest data-entry chore; keeping stock fresh is what makes TodayZ work | v1 |
| AI-R3 | **Shelf photo to catalogue (onboarding)** | Photos of shelves → recognised products matched to the master catalogue → a checklist to add with price defaulting to MRP | Gets a new shop to 50+ listed items in minutes instead of an hour | v1 |
| AI-R4 | **Insight explanations and suggested actions** | The Demand insights card and tab get one plain sentence per row, for every kind (D-99), in the shop's language ("Amul butter 100 g: 6 sold a day, 2 days left. Order 32 to cover a week.") and a suggested offer % for slow items, within limits the backend computes. The numbers come only from the row's `facts`; the model changes wording and the suggested %, never a number, and the template text shows when AI is off or a number is ungrounded | Turns numbers into an action; works in Kannada/Hindi/etc. | v1 |
| AI-R5 | **Shop coach (background jobs)** | Scheduled jobs study each shop's numbers and post suggestion cards to a coach feed (B-19), see §2.1 | The user asked for background AI giving retailers suggestions: what to do, improve, highs and lows | v1 |
| AI-R9 | **Banner maker and offer copy** | From a chosen offer or items: draft 3 banner headlines/sub-lines in the shop's language and English, pick a tint and product photo for the template; the retailer picks and edits | Shops won't design banners themselves | v1 |
| AI-R10 | **Banner image safety check** | Uploaded banner images are checked (no explicit, violent, misleading or competitor-brand-abuse content; text legible); unsure → admin review queue | Banners publish without approval, so they need an automatic guard | v1 |
| AI-R6 | **Review reply drafts** | One-tap polite reply draft in the review's language; retailer edits and posts | Encourages replies, which builds trust | v1 |
| AI-R7 | **Ask about my shop** | Chat over the shop's own rollups ("Which items didn't sell this month?") with tool calls to fixed analytics queries (no free SQL) | Nice, but needs good analytics first | later |
| AI-R8 | Restock suggestions to the distributor | Auto-build a purchase list | Needs supplier integration | later |

### 2.1 Shop coach: how it works

Runs as River periodic jobs per store, spread across the night so load is flat. Each is a registered kind of class `ai` (`coach_realtime_rules` uses no model but is still class `ai`, owner W3-11), declared through `jobs.Define` (D-95). `coach_daily` and `coach_weekly` are batched kinds (they work in batches of at most 60 s and yield on shutdown, D-98). An `ai` kind only writes drafts (`ai_drafts`, `coach_suggestions`); a person confirms. The job's `Summary` counts cards created and skipped, and A-21 shows it:

| Job | When (IST) | What it looks at | Example cards |
|---|---|---|---|
| `coach_daily` | 06:00 | yesterday vs the same weekday average: sales, orders, missed orders (expired/rejected), time to accept, stock-outs of top sellers, stale stock, expiring batches, new reviews, wanted-nearby searches | **High:** "Best Thursday in 6 weeks: ₹18,240 in sales, 31% above usual. Maggi and Amul milk led." **Low:** "3 orders expired yesterday because nobody accepted within 10 minutes (7–9 pm). Turn on auto-accept for in-stock items?" → action. **Do this:** "Amul butter 100 g has 2 days of stock left at the current pace. Restock 15." → action |
| `coach_weekly` | Mon 07:00 | week vs previous 4 weeks, cohort of repeat customers, offer results, rating trend, nearby benchmark (only with k ≥ 10 stores, D-71) | Weekly summary: highs, lows, 3 priorities for the week |
| `coach_realtime_rules` | every 15 min (rules only, no AI) | sudden events; the shop's 30-day unaccepted-expiry rate against `shop_expiry_penalty_threshold_bp` | "12 people near you searched for 'Dolo 650' in the last hour; you have it but stock was last updated 4 days ago." **Expiry-penalty nudge** (D-46): "Too many orders expired without an answer in the last 30 days, so your shop now shows lower in search. Turn on auto-accept for in-stock items?" → action |

"Near you" means within `insight_wanted_nearby_radius_m` (2000): analytics stores H3 resolution-8 cells, "wanted nearby" adds up the cells within that radius, then applies k ≥ 10 to the total (D-81). Every coach statement about other people, including counts of people or searches and benchmarks against nearby stores, needs k ≥ 10 (D-43, D-71), always; below that the detector does not fire.

Pipeline per store:

1. **Facts (SQL, deterministic):** a library of ~30 detectors in `internal/coach/detectors/` (D-48) computes candidate facts with numbers and a severity score (e.g. `missed_orders_peak_hour`, `top_seller_low_days_of_cover`, `slow_mover_21d`, `expiring_batch_value`, `stale_stock_share`, `rating_drop`, `offer_uplift`, `wanted_nearby`, `repeat_rate_change`, `best_day_record`, `accept_time_vs_benchmark`, `delivery_requests_but_off`, `setup_incomplete`). Where a detector covers a Demand insights kind (`top_seller_low_days_of_cover` = selling fast, `slow_mover_21d` = sitting on the shelf, `expiring_batch_value` = going to waste, `stale_stock_share` = stock check due, `wanted_nearby`, and the D-99 kinds missed sales, expired on shelf, selling below cost, bought together, regulars slipping away), it imports the fact query from `internal/insights/facts` (W3-03) and only adds severity, recency and wording, so the card and the coach never compute the same number twice (D-99). The coach does not duplicate "priced above nearby" while `insight_price_compare_enabled` is false.
2. **Choose:** the top 3–5 facts by severity × recency, not repeating a dismissed subject (an item, a pair, a batch or the store) for `coach_dismiss_quiet_days` (7), and at most `coach_max_cards_per_day` (5) new cards per store per day.
3. **Write (AI):** Haiku turns each fact into a card in the shop's language (title ≤ 60 chars, body ≤ 200 chars) using only the given numbers; the grounding validator rejects any number not in the facts; on failure, the rule-based template text is used instead. AI off → template text always.
4. **Act:** each card carries an `action_type` and a payload that opens the right screen prefilled (restock quantity, offer % within backend-computed safe limits, items for a flash offer). Nothing is applied automatically.
5. **Learn:** Done / Not useful / ignored feeds detector weights per shop (simple bandit-style score adjustment, no model training) and dashboard 14 shows acceptance rate per detector.

**Look and feel:** coach cards and Demand insights rows share one component (DemandInsights row in the design system, `design/source/components/DemandInsights/`): a tinted 32px icon square, a label, one sentence with the product name in bold, a text action with an arrow ("Restock →"), and a period tag ("Last 7 days") on the card. There is no metric chip (D-99, to match the design). This matches the old app's Demand insights card the user likes. The coach feed and cards live in `business/src/coach/` (W3-11) and are mounted in the `<CoachSlot/>` that W3-03 provides in the `business/src/overview` shell (D-48).

**Minimum evidence (fixes a weakness in the old card, which said "sold 1 unit this week … 100% of sales"):** a detector may only fire when it has enough data. Below the minimum, a Demand insights kind has no row on the B-03 card; the B-12 Insights tab says what it needs ("Record sales on TodayZ for a week to see what sells fast") instead of a misleading number, and a coach detector does not fire.

**Insight and coach settings (D-59, D-61).** Every threshold is a `platform_settings` key; detectors and copy read them and never hard-code them.

| Key | Default | Unit | Rule | Min | Max |
|---|---|---|---|---|---|
| `insight_no_sale_days` | 21 | days | Days without a sale for "sitting on the shelf" (`slow_mover`) | — | — |
| `insight_min_days_listed` | 21 | days | Days of listing history before "sitting on the shelf" (equal to the no-sale rule) | — | — |
| `insight_expiring_within_days` | 5 | days | "Going to waste": batch expiry window | — | — |
| `insight_wanted_nearby_radius_m` | 2000 | meters | Radius for "wanted nearby" and "near you" | — | — |
| `insight_min_searches` | 10 | count | Nearby searches before "wanted nearby" (k-anonymity, never below 10) | 10 | — |
| `insight_min_sales_lines` | 20 | count | Sales lines in the period before any share-of-sales claim | — | — |
| `insight_min_units_selling_fast` | 5 | count | Units sold before "selling fast" | — | — |
| `insight_min_events_for_percent` | 20 | count | Events behind any percentage shown | — | — |
| `insight_period_days` | 7 | days | Insight period ("Last 7 days") and the window for the average daily pace | — | — |
| `insight_cover_days` | 7 | days | "Selling fast": the restock quantity covers this many days at the current pace; a row fires below this many days of stock (D-99) | 1 | 30 |
| `insight_missed_sales_min_zero_days` | 1 | days | "Missed sales": whole IST days with closing stock of 0 or below in the period before it can fire | 1 | 30 |
| `insight_missed_sales_min_searches` | 10 | count | "Missed sales": nearby searches matching the product on those days (k-anonymity, never below 10) | 10 | — |
| `insight_pair_min_bills` | 20 | count | "Bought together": bills containing both items in the window | 5 | 1000 |
| `insight_pair_window_days` | 60 | days | "Bought together": bill window for the pair counts | 14 | 180 |
| `insight_pair_min_share_bp` | 2000 | bp | "Bought together": share of item A's bills (the item with more bills) that also contain B (super_admin-only, unit `bp`) | 500 | 10000 |
| `insight_pair_min_lift_pct` | 150 | count | "Bought together": minimum lift in percent, P(B given A) ÷ P(B) (150 = 1.5×, 100 = no lift needed) | 100 | 1000 |
| `insight_regular_min_visits` | 3 | count | "Regulars slipping away": visits (distinct IST purchase days) needed in the regular window | 2 | 20 |
| `insight_regular_window_days` | 60 | days | "Regulars slipping away": window in which visits are counted | 14 | 180 |
| `insight_regular_lapse_days` | 21 | days | "Regulars slipping away": no purchase for at least this many days | 7 | 90 |
| `insight_regular_min_linked_customers` | 10 | count | "Regulars slipping away": phone-linked customers who bought in the regular window before the kind shows anything | 1 | — |
| `insight_price_compare_enabled` | false | bool | "Priced above nearby" on or off (Q-52). Needs at least 10 other live verified stores of distinct businesses in the radius whatever the setting (D-71) | — | — |
| `coach_max_cards_per_day` | 5 | count | New coach cards per store per day | — | — |
| `coach_max_push_per_day` | 1 | count | Coach pushes per store per day | — | — |
| `coach_dismiss_quiet_days` | 7 | days | Days a dismissed subject stays quiet | — | — |

The names and defaults match [01-product-rules.md](01-product-rules.md) §17 exactly (D-61); if they ever differ, 01 wins.

Location in analytics is an H3 resolution-8 cell. Cards expire when no longer true (the detector re-checks before display).

## 3. Consumers (customer app)

| ID | Feature | What it does | Why | v1? |
|---|---|---|---|---|
| AI-C1 | **List to basket** (D-110) | Type, paste or speak a list in any of the five languages or mixed ("1 kg atta, 2 Nandini milk, Dolo 650, maggi") → items parsed into basket lines (`POST /v1/me/ai/list-parse`) → a draft in the basket editor (C-22) that the customer confirms or edits line by line before anything is saved or searched. The model only parses. Finding shops is the basket search (01 §11.4, W2-14): non-AI, ranked by full cover, lines covered, total price and distance, with a two-shop split when no one has everything; it never calls a model | The core TodayZ promise ("is it in stock nearby?") for a whole list, not one item at a time; typing a list is faster than adding lines one by one | v1 |
| AI-C2 | **Multilingual search assist** | Queries in local script or transliteration ("doodh", "ಹಾಲು") are normalised to catalogue terms: the synonym table first, the model only when the table misses. Since D-101 the model never runs inside `GET /v1/discover/search`: search returns results from the synonym table and the deterministic parser (W1-06), with `refineHint` when the table missed or the parser found no chip in a query of at least `refine_ai_min_words` words. Then, after the results render and only for a signed-in customer, the app calls `POST /v1/me/ai/search-assist`, and the model proposes a cleaner query and chips (price cap, open now, pickup, delivery, offers, near me, a master-catalogue brand) shown as suggestions the customer taps to apply. A 3 s timeout; on any failure nothing shows and the results stay. No `ai_drafts` row (§1 rule 11); the output is only a query and filters, never stock, money or orders (01 §11.1) | Many users won't type English product names, or will type a whole sentence | v1 |
| AI-C5 | **Voice search** (D-101) | Mic in the customer search bar → speech to text (Sarvam) in the app language plus English, at most 15 s → the transcript goes back to the app only and fills the search box for the customer to edit; the search runs only when they tap search. One endpoint, no `ai_drafts` row: audio and transcript are never stored, and only the confirmed query is kept, when the search runs or an ask is sent (§1 rule 11). Hidden unless the flag is on, the speech key is set, the customer is signed in and online. The business app has no voice search in v1 | Many users would rather speak than type | v1, off by default |
| AI-C3 | **Substitutes when out of stock** | When an item is out of stock at the chosen shop, suggest same-category, similar-pack alternatives in that shop or the same product at a nearby shop. Rule-based first (category + pack size + brand); AI ranking later | Saves the order instead of losing it | v1 (rules), AI later |
| AI-C4 | Help assistant | Answers "where is my order", "how do credits work" from FAQs and the user's own order status (tool calls), hands off to a human ticket | Fewer tickets | later |
| — | Not doing | Medicine advice, prescription reading, health or financial recommendations | Regulatory and safety risk | never in v1 |

### 3.1 Consumer settings (D-101)

| Key | Default | Unit | Rule | Min | Max |
|---|---|---|---|---|---|
| `refine_ai_min_words` | 4 | count | Search sets `refineHint` (and the app may call AI-C2 search assist) only for a query of at least this many words with no chip, or when the synonym table missed | 2 | 20 |
| `ai_voicesearch_enabled` | false | bool | AI-C5 flag (§1) | — | — |

Both are in 01 §17; the lead seeds them, with min and max, in `00012_w0-03_settings_d101.sql` (D-95 rule).

## 4. Admins (admin console)

| ID | Feature | What it does | Why | v1? |
|---|---|---|---|---|
| AI-A1 | **Catalogue clean-up assistant** | In the product review queue: normalise name/brand/pack/unit/category from messy shop input, propose the duplicate to merge into with a reason, draft names in five languages for categories | The master catalogue quality decides search quality | v1 |
| AI-A2 | **Support triage and reply drafts** | Classify new tickets (category, priority, linked order guess), summarise long threads, draft a reply using the order timeline; agent edits and sends | Small team, many tickets | v1 |
| AI-A3 | **Risk flag explanations** | For each fraud flag, a short summary of the evidence ("Same device used by 6 new accounts that each placed one ₹100 order at the referrer's shop") | Faster, more consistent reviews | v1 |
| AI-A4 | **Ask Pulse** | Natural-language questions over the analytics views with fixed, read-only tool queries; shows the query and the chart | Founders get answers without Grafana | later |
| AI-A5 | KYC document check | Read PAN/GST certificate photos and compare with typed values | Needs document uploads and legal review | later |

## 5. Work packages (added to Wave 3)

| ID | Title | Agent | Owns | Depends | Deliverables | Tests | Scenarios |
|---|---|---|---|---|---|---|---|
| W3-10 | AI platform | AI Engineer | `backend/internal/ai/{client,schema,scrub,guard,eval,prompts,drafts}` only, not the whole `internal/ai/` (client, schemas, PII scrubber, cost guard, eval runner, prompt registry, drafts), `infra/grafana/alerts/ai.yaml`, `docs/runbooks/ai-*` (D-82), plus the `ai_drafts` migration, api `ai` base (shared draft endpoints: read a draft, record per-line outcomes, discard) and dashboard 14 (`infra/grafana/dashboards/14-ai-quality-cost.json`) | W1-07 | Anthropic client wrapper with metrics/spans/cost, tool-schema helpers, the number-grounding validator, caps, flags, the `ai_drafts` table, eval CLI, dashboard 14 "AI quality and cost", the AI cost alert at 80% of `ai_daily_cost_cap_paise` with its runbook | Unit tests for the scrubber and the validator; the cost cap test (calls stop at 100%, alert at 80%) | SC-AI-01…08 |
| W3-11 | Retailer AI and shop coach | AI Engineer + Data Engineer + Frontend Developer | `internal/ai/{billscan,shelfscan,insighttext,replydraft,bannercopy,imagecheck}`, `internal/coach/` (jobs, card writer, feed, realtime rules, `detectors/`), `business/src/{restock-scan,onboarding/shelf,insights/text,coach}` | W3-10, W3-03, W2-07, W2-12 | AI-R2…R6, R9, R10 with draft/confirm UIs; the coach detectors, jobs, realtime rules (including the expiry-penalty nudge), feed and B-19 screen, mounted in W3-03's `<CoachSlot/>`; 60 real-looking invoice fixtures (printed and handwritten, en/hi/kn), 40 shelf photos, insight-text fixtures | Evals: bill scan ≥ 90% correct lines on printed bills and ≥ 75% on handwritten bills; 0 writes without confirm; insight and coach text has 0 ungrounded numbers; every detector has golden SQL tests; coach respects `coach_max_cards_per_day` and `coach_max_push_per_day`; a dismissed topic stays quiet for `coach_dismiss_quiet_days`; no statement about other people below k ≥ 10 (D-71) | SC-AI-10…40 |
| W3-12 | Consumer AI | AI Engineer + Frontend Developer | `internal/ai/{listparse,querynorm,voicesearch}`, `customer/src/{list-parse,voice-search,search-assist}` | W3-10, W1-06, W2-08, W2-14 | AI-C1 list parse (typed, pasted or spoken text → a basket draft in W2-14's editor, lines confirmed by the customer; ranking and cart fill are the non-AI basket search of W2-14, D-110), AI-C2 search assist (`POST /v1/me/ai/search-assist` after the results render, 3 s timeout, nothing shown on failure, in W2-08's `<SearchAssistSlot/>`, D-101), AI-C5 voice search (one endpoint, no draft, no audio or transcript kept, the mic in W2-08's `<VoiceSearchSlot/>`, D-101), AI-C3 rule-based substitutes | Evals: 200 list fixtures across five languages (≥ 95% items parsed correctly); a test that confirming a parse saves and searches nothing (08 BSK-15); latency p95 < 2.5 s end to end; a recorded voice-search eval of 100 clips across the five languages (transcript and chips, run on recorded STT replies in `make check`) and 100 sentence queries for search assist (≥ 90% chips correct, 0 chips outside the allowed kinds); a test that no audio or transcript is stored (08 ASK-16, ASK-26) and that a search-assist failure leaves search unaffected (08 ASK-27) | SC-AI-41…55 |
| W3-13 | Admin AI | AI Engineer + Frontend Developer | `internal/ai/{catalogue,triage,riskexplain}`, `admin/src/ai-assist/{catalogue,support,risk}`, mounted through an `<AssistSlot/>` in A-08, A-14 and A-13 (D-82; W3-05, W3-08 and W3-02 own those screens and their slots) | W3-10, W3-05, W3-02, W3-08 | AI-A1…A3 | Evals: duplicate suggestion precision ≥ 90% on the labelled set; triage category accuracy ≥ 85% | SC-AI-56…65 |

AI endpoints: W3-10 owns the shared `ai` base; each feature package (W3-04, W3-11, W3-12, W3-13) adds its own endpoints in its own domain files under `backend/api/paths|schemas/` and writes its drafts to `ai_drafts` (D-46, D-55). W3-04 (voice, in [07-work-packages.md](07-work-packages.md)) depends on W3-10 (D-53).

Local development uses the mock LLM with recorded responses (W0-07) so tests are deterministic and free. Live-model evals and manual AI QA are opt-in: they run only when the user has chosen to put their own keys in `.env.local` (D-17). Nothing before cloud requires them.
