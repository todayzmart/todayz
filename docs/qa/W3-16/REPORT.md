# W3-16 report: ask shops nearby, share cards, site strip, A-23 Demand, H3

Branch `claude/w3-16-asks-457dd8`, from merge base `1465e58`. Scenarios: [e2e/scenarios/ASK.md](../../../e2e/scenarios/ASK.md) (SC-ASK-01…28, where each one runs, plus ASK-29…41). Row: `docs/plan/07-work-packages.md:120`, D-101.

## What was built

**Backend**

- `internal/platform/geo/h3`: a pure-Go port of the H3 functions we need: `LatLngToCell`, `CellToLatLng`, `GridDisk`, and the string and int64 forms. It is checked against about 4,700 reference rows in `testdata/`. No new module (D-88).
- `search_log.geo_cell` is now the H3 res-8 cell of the search point. The `rollup_demand` job fills `insight_geo_cells` with the cell centre (`MissingGeoCells`).
- `internal/asks`:
  - Create (daily limit, one open ask, nearest-first recipients within the radius, up to `ask_max_stores`, skipping muted, blocked and low-yield shops).
  - The shop inbox, with the distance band only.
  - Reply (Yes or No, price rule, ready-by, one reply per shop under a race), report, and the customer list and detail.
  - The pricer for orders, `Demand` for insights, and the window-close and retention jobs.
  - The account-deletion hook.
  - Every number comes from `platform_settings`.
- `internal/share`:
  - Share cards: create with a hash key and a rate limit, resolve with `410` for a shop that is gone, and the PNG card render.
  - The site strip: the nightly proposal, k-floor, denylist, and approval with an audit entry.
  - The A-23 gaps read with its floor.
- Migration `31601_w3-16_asks.sql`:
  - New tables: asks, ask_recipients, ask_store_blocks, share_cards, site_demand_marquee, rollup_daily_ask_demand and share_card_creates.
  - `orders.ask_reply_id` with its foreign key, and the `orders_guard` trigger updated for it.
- Routes, the `policy/asks.go` golden, job kinds and the asks seed fixtures.
- Runbook: `docs/runbooks/asks-timers.md`.

**Contract** (`backend/api/{paths,schemas}/{asks,share,admin-demand}.yaml`, `problems/{asks,share}.yaml`), with two additions made while building the customer side (48b2e8e):

- `AskReply.storeId` (required Ulid) and `AskReply.listedPricePaise` (integer or null). The second is the item's price now, shown once the replied price ends ("Price may have changed").
- `ask_limit_reached` carries `extensions.limit`, so the app never hard-codes the daily number.

**Customer**

- **C-25 ask sheet**, reached from C-05 with no results (the real `AskNearbySlot`) and from C-08:
  - Quantity, need-by, Send.
  - A signed-out draft survives sign-in.
  - The limit and already-open problems.
- **C-26 ask page:**
  - Progress ("Asked 5 shops · 3 replied · best ₹255") and a waiting note.
  - Reply cards with "Ready soonest", the price and how long it is held, "Ask the shop for the price", and no Book for in-store only.
  - Close ask, and the closed-with-no-Yes state.
  - The `/asks` list is linked from Orders.
- **Book from a reply:** fills the cart with `askReplyId` and the asked quantity, and C-11 handles `ask_price_expired`.
- **C-08 Share:** a share card, then the system sheet, or a sheet with Copy link (WhatsApp first on Android only).
- **`/x/{code}`:**
  - A Cloudflare Pages Function serves the preview HTML (no distance, the name escaped) and falls through to the app on failure.
  - The app lands on C-08 with `ref=share`.
  - A gone shop gets a notice and a search box.
  - Vite has a dev middleware for it.

**Business:** B-20 Asks inbox is the Asks tab of B-04 (`/orders?tab=asks`).

- Open cards count down from `serverNow` and show the distance band only.
- The reply sheet handles price rules by role and an MRP cap, ready-by today in IST, and offers "Add it to your stock" for an unlisted item.
- Report with a reason.
- "Done" for today and "Show earlier asks".
- A muted note for owners and managers.
- Copy in en, hi, kn, te and ta.

**Admin:** A-23 Demand (`/demand`).

- **Gaps:** every city or one; "Not in the catalogue"; the rule line from settings; CSV export with a formula guard.
- **Site strip** (catalogue and admins): leave products out, approve, see the approved chips; a stale proposal gives `409`.

## Changes after the lead's review of 828ee3c

- **M1, expired price** (7edbd4c, 3e8664c): a priced Yes holds until `price_valid_until`, exclusive. From then the quote is at the catalogue price, and placing with the reply id is `409 ask_price_expired` carrying that quote; the app drops the id and requotes. A Yes without a price has no expiry: it keeps the reply and the quantity cap and books at the catalogue price. Past the asked quantity the cart drops the reply (L9). Another customer's reply id gets the same 409 with the same extensions as an expired one.
- **M2** (1375808): `ask_recipients` stores a distance rounded to 500 m (CHECK in 31601); the band comes from the exact distance, which is never stored. A reply locks the ask row too, so a Yes cannot land on an ask being closed. The first-Yes push is claimed once (`first_yes_notified_at IS NULL`). Demand counts window-ended and all-replied asks only.
- **M3 and L4, public share reads** (e312bac): the card GET and a re-create take a token from a per-IP-HMAC bucket (429 past it). `share_opened` is recorded once per card, IP and IST day (`share_card_opens`, cleared by `asks_retention`). A stale image is redrawn by the `share_card_render` job (at most one queued per card in ten minutes) while the old image is served; a storage failure leaves the card row as it was and the job retries. A create with neither an IP nor a device id is refused with 429. **Caveat:** the Pages Function calls the API from Cloudflare, so every link-preview fetch arrives from Cloudflare's addresses: those reads share a few buckets, and `share_opened` undercounts previews (see TODO(lead)).
- **L5** (0d19236): the site strip checks product status and the denylist again when it is read, so an approved chip for a product since hidden or denied is not shown.
- **Events** (3e8664c): orders now tell asks of a booking or an expired-price refusal. `ask_booked` (with `price_source` and `minutes_since_reply`) and `ask_price_expired` are emitted after the order transaction commits. `ask_price_expired` counts only the customer's own priced, expired Yes, not another customer's id.
- **Audit** (999a878): `site_marquee` is an admin audit subject type (seen with `admin.marquee.approve`, keyed by the city). The A-23 strip card shows its history in the AuditPanel, refreshed after a decision.
- **Origins and `API_URL`** (e312bac, bbb32fd): the card's app and image origins come from config (`CustomerAppOrigin`, `ImagesOrigin`, `routes_asks.go`). Without `API_URL` the Pages Function logs an error and answers a plain 302 to `/x/{code}?app=1`, which it lets through to the app (the flag stops a redirect loop).
- **Indic names on the card image** (1ba7b89): not fixed with `golang.org/x/image`. Its opentype package draws glyph by glyph with no complex shaping (no conjuncts, no reordered vowel signs), so a Devanagari, Kannada, Telugu or Tamil name would come out wrong even with an Indic font. The card leaves off any name the Latin font cannot draw (typographic quotes and dashes are folded first) and shows the price; the link preview's title and description carry the real name. No module was added.
- **H3 vectors** (9eb2977): the lead's reference files already hold both poles, both sides of the antimeridian and the 12 res-0 pentagons. `TestGoldenVectorsNameTheEdges` now fails if a regenerated file drops one, and checks the disk sizes of all 12 pentagons (1, 6, 16 for k = 0, 1, 2). `grid_disk.tsv` has reference disks for 3 pentagons (base cells 4, 14 and 24 at res 8).
- **Customer copy** (7357d45): the 63 `asks.*` and 13 `share.*` keys in hi, kn, te and ta, approved under the founder's standing instruction; the row is in `docs/qa/W4-05-hindi-customer/REVIEW.md`.
- **New tests:** `cmd/todayz/orders_ask_http_test.go` (quantity above the ask is 422 on quote and create; the no-price Yes; another customer's id; two concurrent bookings on one reply; the `now == price_valid_until` boundary), the share GET that does not write on a storage failure (`TestStaleCardRenderIsAsync`), the reply-vs-close race and the concurrent first Yes, the Pages Function without `API_URL`, the Indic card fallback and the audit subject.

## Tests and results (all run on this branch, area checks only)

| Area | Command | Result |
|---|---|---|
| Go | `go test` for asks, share, seed, rollups, search, jobs, admin, platform/geo/h3, platform/authz/policy, cmd/todayz (DB tests on the shared test Postgres) | ok |
| Go lint | `golangci-lint run ./...`; settings lint | 0 issues; ok |
| Customer | lint, lint:css, typecheck, vitest, `build:e2e` | pass, 162/162 (19 files), built |
| Business | vitest | 468/468 (50 files) |
| Admin | typecheck, vitest | pass, 220/220 (25 files) |
| i18n | vitest; `lint:messages` | 198/198; customer 0 errors |
| Playwright (fake API) | `customer/e2e/asks.shots.spec.ts` (7), `business/e2e/asks.spec.ts` (2; one assertion now scoped to the open card, since the missed fixture has the same facts line and shows under "Done today" at some IST hours), `admin/e2e/demand.spec.ts` and `roles.spec.ts` (10) | pass |

I did not run the full `make check` or `make e2e`; the lead runs those after merge.

## Screenshots (`docs/qa/W3-16/`)

- **Customer:**
  - C-25: `customer-c25-search-zero.png`, `customer-c25-sheet.png`
  - C-26: `customer-c26-waiting.png`, `customer-c26-replies.png`, `customer-c26-replies-full.png`, `customer-c26-none.png`
  - C-08 share sheet: `customer-c08-share-sheet.png`
  - C-11 after an expired ask price (the new total at the current price): `customer-c11-ask-expired.png`
  - The `/x` gone page: `customer-x-gone.png`
- **Business:**
  - Desktop: `business-b20-inbox.png`, `business-b20-reply.png`, `business-b20-done.png`
  - 360 px: `business-b20-inbox-360.png`, `business-b20-reply-unlisted-360.png`
- **Admin:**
  - Gaps: `admin-a23-gaps.png`
  - Site strip: `admin-a23-strip-proposed.png`, `admin-a23-strip-approved.png`
- **Share card:** the rendered PNG is `share-card-sample.png`.

## Conservative readings (unclear rules, read the safe way)

**C-26 waiting note:** "Most reply within {min} min" uses `windowEndsAt − createdAt` from the ask (the server's `ask_reply_window_min`). The app never types the number.

**B-20:**

- The muted note shows for owners and managers only; cashiers cannot read store settings, so no settings call is made.
- A cashier's Yes sends no price, so the listed price stands.
- On an unlisted item the price is optional.
- "Done today" is the IST day of `sentAt`.
- Live updates come from the store stream plus a 30 s poll.

**A Yes with no price:** booking it charges the catalogue price. Booking more than was asked gives a 422 (`orders/quote.go`).

**A-23:**

- Gaps are fetched once for all cities and filtered in the browser. The API caps the list at 500 rows.
- The CSV is built in the browser.
- The strip has Approve only, with no Reject button: products are left out, not rejected.
- A role that cannot approve and opens `?tab=strip` gets the Gaps tab.

**Share copy on C-05 cards:** still plain or absent. Only C-08 has Share.

## TODO(lead)

- **Pages `API_URL`:** the lead sets it on the Pages project. Until then `/x` links log and redirect to the app, with no link preview.
- **Share reads from the Pages Function** (`backend/internal/share/cards.go`, `limitReads`): the function's fetches reach the API from Cloudflare's addresses, so they share IP-HMAC buckets and one `share_opened` per code per day. A fix is to have the function forward the visitor's `CF-Connecting-IP` in a header the API trusts only from Cloudflare (a shared secret or an mTLS origin rule). That is a trust and config decision, so I left it.

## Follow-ups

- **Storage:** `backend/internal/share/objstore.go` should merge with the business `presign.go` code into one `platform/storage` package, with the image origin set beside the R2 public host in config.
- **Card image in Indic scripts:** shaping Indic text needs a shaper (HarfBuzz or a Go port such as `go-text/typesetting`) plus Noto or Anek Indic fonts; until then the card shows the price without the name.
- **H3 disks:** reference `grid_disk` rows for the other 9 pentagons would need python h3 (network) to generate; the structural check covers them for now.

## Open points

- **Coverage gaps:**
  - ASK-20: the search-then-ask "counts once" sum with the k ≥ 10 floor has no explicit test.
  - ASK-23: the API `409 store_not_bookable` is W2-02's; here only the UI (no Book button) is covered.
  - ASK-14: only the muted exclusion is tested; the toggle itself is B-14.
  - ASK-05: reply at `reply_by` → `ask_closed` with a test clock.
  - ASK-39: the cashier's no-note half is not asserted.

  ASK.md lists them all.
- **Copy:**
  - Customer `asks` and `share` copy is in all five languages (approved, see above).
  - Business asks keys are in all five languages; `docs/qa/business-i18n/REVIEW.md` has the approval row for the 65 `asks.*` keys.
  - `product.share`, `product.share.copied` and `product.share.failed` in `customer/product.en.json` are now unused (C-08 uses `share.*`), and possibly `orders.asks.empty.*` too. I left them in place because that file is not mine.
- The business 360 px screenshot shows a curly-quote glyph from the font, not a layout bug.

## Edits outside my paths

- **admin:**
  - `src/shell/nav.ts`: `demand` added to BUILT_SECTIONS.
  - `src/audit/labels.ts` and `shared/i18n/messages/admin/audit.en.json`: the `marquee.decide` label.
  - `src/routes/demand/index.tsx`
  - `e2e/fake-api.ts`: empty answers for demand-gaps and site-marquee.
  - `e2e/roles.spec.ts`: READS entries for both.
- **backend `cmd/todayz`:** `routes.go`, `routes_asks.go`, `asks_seams.go`, `routes_test.go`, `routes_insights.go` (`asksForInsights`), `routes_orders.go` (`od.AskPricer`).
- **backend `internal/orders`:**
  - `quote.go`: a Yes without a price books at the catalogue price; a quantity above the ask gives 422; the replied price applies only while it holds; an expired price is 409 `ask_price_expired` with the fresh quote.
  - `seams.go`: `AskPrice.MaxQuantity` and the booking and expired-price notices to asks.
  - `create.go`: the ask notices after the order commits.
- **backend `internal/admin`:** `audit.go` and `admin_test.go` (the `site_marquee` subject view); `api/schemas/admin-audit.yaml` (the enum value); `cmd/todayz/admin_http_test.go`.
- **backend `internal/platform`:**
  - `geo/h3/*` (new).
  - `authz/policy/asks.go` and its golden.
- **backend `internal/rollups`:** `rollups.go`, `rollups_test.go` and `db/queries/rollups/rollups.sql` (`MissingGeoCells` and the cell centre).
- **backend `internal/search`:** `logging.go`, `search.go`, `search_db_test.go`, `basket_db_test.go` (the H3 `geo_cell`).
- **backend `internal/seed`:** `seed.go`, `asks.go`, `asks_test.go`, `seed_test.go`, `identity_test.go`, `insights.go` (comment).
- **backend `internal/jobs`:** `asks.go`, `jobs.go`, `jobs_test.go`.
- **backend `db`:** `queries/asks/{asks,share}.sql` and `sqlc.yaml`.
- **business:**
  - `src/orders/OrdersPage.tsx`, `src/orders/queries.ts` and `src/routes/orders/index.tsx` (the Asks tab).
  - `e2e/asks.spec.ts`
- **customer:**
  - `src/cart/cart.ts`, `src/cart/quote.ts` and `src/checkout/CheckoutPage.tsx` (`askReplyId`, `ask_price_expired`).
  - `src/orders/OrdersPage.tsx`: the asks link.
  - `src/product/ProductPage.tsx`: Share and Ask.
  - `src/search/slots.tsx`
  - `src/routes/asks/*` and `src/routes/x/$code.tsx`
  - `src/test/asks.test.tsx` and `e2e/asks.shots.spec.ts`
  - `tsconfig.json` and `vite.config.ts` (the `/x` dev middleware).
- **shared:**
  - `i18n/messages/{admin/demand,business/asks.*,customer/asks,customer/share}.json`
  - `telemetry/events/{asks,share}.yaml`
- **docs and e2e:** `docs/qa/business-i18n/REVIEW.md` and `docs/qa/W4-05-hindi-customer/REVIEW.md` (the approval rows), `docs/runbooks/asks-timers.md`, `e2e/scenarios/ASK.md`.
