# 12 · Decision log

The numbered files (01–11) are the source of truth. This log records why they say what they say. Each entry settles one finding from a plan review. If a numbered file disagrees with this log, the numbered file is wrong: fix it, and add an entry here.

## Review 1 (2026-10-22, Reality Checker, 60 findings)

### Money and stock

**D-01 · Stock math.**
- Superseded in part by D-61: `picked_up` is now `collected`.
- On hand = the sum of `stock_movements`.
- Reservations are not stock movements. They live only in `stock_reservations`, and `store_items.reserved` is a cached counter. Available = on hand − reserved.
- Accepting an order creates reservations. It does not change on hand.
- An online order writes a `sale_online` movement only at `picked_up` / `delivered`, and that movement consumes the reservation.
- Movement source enum, complete:
  - `opening`, `restock`, `supplier_bill`, `import`
  - `count_correction`, `damage`, `expired`
  - `sale_pos`, `sale_online`, `pos_reversal`

**D-02 · Credit minimum basis.**
- **Eligible amount** = the items total after offers and discounts, before credits, without the delivery fee.
- Credits can be used only when the eligible amount is ₹100 or more (inclusive). Copy says "₹100 or more".
- `orders` gains `items_after_offers_paise`. Invariant 5 checks that column against `credit_min_order_value_paise`.

**D-03 · Setting units.**
- Every setting holding money ends in `_paise`. Percentages are basis points and end in `_bp`. Credits are whole numbers (1 credit = ₹1).
- `platform_settings` gains a `unit` column: `paise | bp | credits | minutes | hours | days | count | meters | bool | text`.
- Keys are renamed to match, for example `credit_min_order_value_paise = 10000` and `ref_qualifying_min_order_paise = 10000`.

**D-04 · Order code storage.** `orders` stores two things:
- `code_enc`: AES-256-GCM with `ORDER_CODE_ENC_KEY`. The owner's GET decrypts it.
- `code_hmac`: HMAC with `ORDER_CODE_HMAC_KEY`, used for lookup at the counter.

The shop never receives the plain code from the API. It only types what the customer shows.

**D-05 · Two-person approval.**
- Any credit adjustment above `admin_adjust_dual_approval_above_paise` (50000, i.e. ₹500) creates a pending `admin_adjustment_requests` row. This applies to super_admin too.
- Only `finance` and `super_admin` can request or approve. The approver must be a different person from the requester.
- The `admin` role cannot adjust credits.
- Table owned by W3-01.

**D-06 · Cashier permissions.**
- Cashiers can: sell at POS, count stock (`count_correction`), view items and orders, hand over orders (enter the pickup code), and create voice drafts.
- Cashiers cannot: restock, record damage, change prices, give manual discounts, or reverse sales.
- Voice confirm checks permission per line:
  - `set_price` needs `item.price.edit` (manager or above).
  - `restock` needs `stock.restock` (manager or above).
  - `count` needs `stock.count` (cashier or above).
- Lines the user may not confirm are shown disabled with "Ask a manager".

**D-07 · POS customer linking.**
- `pos_sales.link_method` is one of `bill_qr | counter_code | phone_unverified`.
- A phone-only link only adds the customer to the shop's own customer list. It earns no cashback, allows no credit spending, and does not qualify a referral.
- Only `bill_qr` and `counter_code` touch the ledger.

**D-08 · Reversing a linked POS sale.** Reversal is allowed within `pos_reversal_window_hours` (24) and creates a `reversal` ledger entry:
- **Credits spent on the sale** go back to the lots they came from (the consumption rows record which). Any part of a lot that has expired since then posts to `platform_expired`.
- **Cashback earned on the sale** is clawed back up to the customer's available (not held) balance. The wallet never goes negative. The store absorbs any shortfall, because it funded the cashback, and the platform loses nothing.
- **Store credit account:** the receivable for the reversed credits is reversed too.
- **Referral the sale qualified:** if the reward has not posted, the qualification is revoked. If it has posted, the reward stays and a risk flag is raised.
- Stock: a `pos_reversal` movement puts the quantities back.

**D-09 · Payout amount.**
- Payout amount = store credit balance − unpaid payouts.
- A business can have at most one open payout (`pending` or `held`), enforced by a unique partial index.
- If the open payout is still unpaid when a new monthly statement closes, the payout's amount is recomputed. No second payout row is created.

**D-27 · Credit lots.**
- Every credit into `customer_wallet` creates a lot: cashback, referral reward, and positive admin adjustments.
- A hold consumes lots FIFO and marks those portions as held. A released hold goes back to the same lots; no new lot is created.
- A lot that expires while held does not expire. When the hold is released after the lot's expiry, that portion posts to `platform_expired` immediately.
- Invariant 3: the sum over lots of (remaining + held) = wallet balance + held balance.

**D-28 · Partial accept.** The customer confirms the new totals (`awaiting_customer`), and the screen shows them.
- Superseded in part by D-94: the delivery minimum and the free-above threshold use the item subtotal, not the eligible amount.
- Held credits are re-capped against the new eligible amount.
- If the new eligible amount is under ₹100, all held credits are released and the order uses no credits.
- The delivery minimum is checked only at placement. After a partial accept, the delivery fee is recomputed on the new eligible amount, and the free-delivery threshold is applied again.

**D-29 · What delivery and cashback are based on.**
- Superseded in part by D-94: the delivery minimum and the free-above threshold use the item subtotal; cashback is unchanged.
- The delivery minimum and the free-above threshold use the eligible amount.
- Credits never pay the delivery fee.
- Cashback base = eligible amount − credits used. The delivery fee is excluded.
- The credit cap per order applies to the eligible amount.

**D-30 · Credit caps.**
- Superseded in part by D-62: there are no caps on earning credits.
- "Day" is the IST calendar day. "Month" is the IST calendar month.
- When a cap is reached, the order can still be placed, without credits. The quote shows "Credits can't be used at this shop today" before the customer places it.
- Bill QR and counter-code claims count toward the customer's earn caps.
- The ₹100 credit minimum also applies at POS, on the bill after discounts.

**D-31 · Account deletion.**
- Deletion is blocked while the customer has an order that is not finished.
- On deletion, the wallet balance is forfeited: `customer_wallet → platform_expired` with kind `forfeit_deletion`.
- W1-01 owns the deletion flow and exposes `OnAccountDelete` / `CanDeleteAccount` hooks. W2-01 registers the forfeiture and W2-02 registers the active-order check, each with its own tests.

**D-32 · Referral attribution.**
- First touch wins, within a 7-day window from opening the link to signing up (`ref_attribution_window_days` = 7).
- The qualifying amount is the eligible amount minus credits used, and must be at least `ref_qualifying_min_order_paise`.

**D-33 · Stock sources.** Covered by D-01.

**D-34 · Partitioning.** `stock_movements` is not partitioned in v1. It has a BRIN index on `created_at` and a unique `(store_id, client_id)`. Partitioning moves to file 10.

### Permissions and auth

**D-10 · API path namespaces.**

| Prefix | Who |
|---|---|
| `/v1/discover/...` | Public, e.g. `/discover/stores/{slug}`, `/discover/stores/{slug}/items`, `/discover/stores/{slug}/reviews` |
| `/v1/me/...` | The signed-in customer |
| `/v1/biz/businesses/{bid}/...` and `/v1/biz/businesses/{bid}/stores/{sid}/...` | Business users |
| `/v1/admin/...` | Admins |
| `/v1/public/...` | Leads and receipts |
| `/v1/auth/...` | Auth |
| `/v1/rum`, `/v1/events` | Telemetry |

No method + path pair is defined twice. Business product endpoints become `/biz/businesses/{bid}/products` and `/biz/businesses/{bid}/products/search` (fixes D-37).

**D-12 · Counter code while the POS is offline.**
- The offline POS records the typed counter code and the device time with the sale.
- At sync, the server checks the code against the time step at the recorded time. It accepts only if all of these hold:
  - the sale syncs within `pos_offline_link_max_hours` (12);
  - the device clock was within 2 minutes of the server's at the device's last online check, which the device records;
  - the code has not been used before.
- If verification fails, the sale stays unlinked and the shop sees "Customer link failed".
- More than `pos_offline_links_risk_per_day` (20) offline links at one store in a day raises a risk flag.

**D-13 · Referrals from the same device.**
- A sign-up on a device already tied to the referrer, or to another account in the last 90 days, is recorded as `rejected_same_device` with no reward.
- 3 or more accounts on one device raises a risk flag.

**D-14 · Refresh cookies.**
- Each app has its own cookie: `__Secure-tz_rt_c` (customer), `__Secure-tz_rt_b` (business), `__Secure-tz_rt_a` (admin).
- Each cookie is host-only on the API host (no `Domain` attribute), with `Path=/v1/auth`, `HttpOnly`, `Secure` and `SameSite=Strict`. The apps and the API share a registrable domain, so requests between them are same-site.
- Locally the host is `api.todayz.localhost`.

**D-15 · OTP in local and test.**
- An OTP is never logged in any environment.
- In `local` and `test`, the reserved numbers `+91 90000 000xx` accept `000000`.
- Any other number goes through the mock SMS server, and the message shows in the mocks UI (`mocks.todayz.localhost`).
- There is no `dev` environment.

**D-18 · Key env vars.**

| Env var | Purpose |
|---|---|
| `JWT_SIGNING_KEYS` | JSON key set of Ed25519 keys with `kid` |
| `JWT_CURRENT_KID` | Which key in the set signs new tokens |
| `ORDER_CODE_ENC_KEY` | Order code (D-04) |
| `ORDER_CODE_HMAC_KEY` | Order code lookup (D-04) |
| `BILL_QR_SIGNING_KEY` | Signs bill QRs |
| `PII_ENC_KEY` | AES-256-GCM for phone, PAN, GSTIN, UPI ID and bank details |
| `PII_HASH_KEY` | HMAC for lookups |
| `DEVICE_SECRET_ENC_KEY` | Counter-code device secrets |
| `TOTP_ENC_KEY` | Admin TOTP secrets |

- `make setup` generates any missing key into `.env.local` and prints only "generated: NAME".
- In staging and prod, the keys live in Secrets Manager.

**D-19 · Provider keys.**
- Provider keys live in env only.
- `platform_settings` holds on/off flags only. A channel works only when its flag is on and its key is set.
- A-17 shows each key as `set` / `missing`, never the value.

**D-20 · Admin cancel.**
- `support`, `admin` and `super_admin` can cancel from any non-terminal state, with a reason from the admin-cancel list (D-60).
- Cancelling releases reservations and held credits. No strike is given unless the admin picks a side.
- It is audited and added to the transition table.

**D-21 · No-show applies to pickup only.**
- No-show expiry applies only to pickup orders.
- A delivery order still in `accepted` after `delivery_dispatch_timeout_min` becomes `expired` with reason `dispatch_timeout`, and the shop gets a strike.

**D-22 · `delivery_failed`.**
- It is reachable only from `out_for_delivery`.
- No stock movement was written (D-01), so the reservation is released when the shop confirms the goods came back. If they did not, the shop records `damage`.
- Held credits are released and no cashback is issued.

**D-23 · Order numbers.** One global sequence, shown as `TZ-` plus at least 6 digits, zero-padded (for example `TZ-024817`). There is no per-day reset.

**D-24 · Problem codes.**
- One registry: `backend/api/problems/<domain>.yaml`, bundled. Each entry has a code, HTTP status and i18n key.
- The canonical code for a refused state change is `invalid_transition`.
- W0-03 creates the common file, and each work package adds codes in its own domain file.

**D-38 · Permission matrix.** 02 holds the complete matrix: every action used in 04, with one column per role.
- Superseded in part by D-64: there are now seven admin roles.
- Business roles: owner, manager, cashier.
- Admin roles: field_agent, support, finance, admin, super_admin.

Business roles:
- **Owner:** everything.
- **Manager:** everything except managing owners, editing KYC or bank details, and deleting the business. Managers can view KYC masked.
- **Cashier:** D-06 only.
- POS reverse and manual discounts: manager and above.
- Customers view, banners, offers, coach, review reply, batches and bulk import: owner and manager.

Admin roles:
- **field_agent:** D-39.
- **support:** orders and users view, cancel, tickets, review moderation and restore, strike reset, risk view.
- **finance:** ledger, payouts, adjustment request and approve.
- **admin:** everything except credit adjustments, role management and money settings.
- **super_admin:** everything.

**D-39 · Field agents.**
- A field agent can onboard businesses.
- They can add and edit items for businesses they onboarded, until `field_agent_edit_window_days` (30) after that business is verified.
- Endpoints: `/v1/admin/field/businesses/{bid}/stores/{sid}/items`.

**D-40 · Verification.**
- KYC verification is at the business level (`businesses.status`).
- Each store has its own status: `draft | pending_review | live | paused | suspended`.
- A new store at a verified business needs a quick admin review (address and photos).
- Only `live` stores of `verified` businesses appear to customers.

**D-41 · Rules that were missing from 01.**
- Stores have `accepts_pickup` and `accepts_delivery`. With both off, the store is visible and browsable but cannot be booked, and shows "In-store only".
- Ordering outside opening hours, or while the shop is closed manually, returns 409 `store_closed`.
- Admins (support and above) can restore a hidden review and can reset a store's or customer's strikes, with a reason. Both are audited.

**D-44 · Encryption.** GSTIN and UPI ID are encrypted with `PII_ENC_KEY`, and a masked copy (last 4 characters) is kept for display.

### Product details

**D-25 · Settings keys.** File 01 lists every key with its unit and default.
- Feature flags: `feature_delivery_enabled`, `feature_bill_qr_enabled`, `feature_voice_enabled`, `feature_offers_enabled`, `feature_banners_enabled`.
- One `ai_<feature>_enabled` per AI feature in 11.
- Models: `ai_model_fast` (`claude-haiku-5-5`) and `ai_model_smart` (`claude-sonnet-5-5`). Code reads the model from settings and never hard-codes it.
- `ai_daily_cost_cap_paise`.
- Referral rewards: `ref_<kind>_referrer_credits` and `ref_<kind>_referee_credits` for each referral kind in 01.
- Minimum client versions: `min_client_version_customer`, `min_client_version_business`, `min_client_version_admin`.

**D-26 · Shop referrals and codes.**
- A shop's referral reward goes to the store credit account of the business's primary store (its first live store), and is paid out with payouts.
- Each code has one job:
  - The business referral code is for shop-to-shop referrals.
  - The store short code is the one in the QR standee URL.
  - Standee rows carry no code of their own. A scan is tracked with a `?s=<standee_id>` parameter.

**D-35 · Foreign keys to tables built later.**
- A migration may add a foreign key only to a table from an earlier wave, or from the same work package.
- Columns that point at tables built later in the same wave, or in a later wave, are created as plain `uuid`. The FK constraint is added by a constraints migration owned by the work package that creates the target table (or by the wave's integration package).

**D-36 · POS sync cursor.**
- Catalogue sync uses `catalogue_changes` (`store_id`, `seq`, `entity`, `entity_id`, `op`), kept for 30 days.
- If a cursor is older than that, the server returns `resync_required`, and the device downloads a full snapshot.
- `outbox_events` (24 h) is only for SSE replay.

**D-42 · Search ranking.**
- Filter first: in stock, `live` store, within the radius.
- Then score = 0.45·text + 0.30·exp(−d / 1.5 km) + 0.10·reliability + 0.10·rating_norm + 0.05·has_offer. The weights are settings (`search_w_*`).
- Ties go to the nearer store. The customer can switch to "Nearest first".
- The `has_offer` filter is in the API and is a chip on C-05.

**D-43 · Coach privacy threshold.** The coach example now reads "12 people near you searched…". k ≥ 10 always applies.

**D-45 · Customers' in-store purchases.**
- Superseded in part by D-75: the "In store" tab is on C-13, not C-08.
- `GET /v1/me/purchases` lists online orders and linked POS sales together.
- C-08 gets an "In store" tab.
- Receipt link: `GET /v1/public/receipts/{token}`. The token is 128-bit and expires after 30 days. The page shows items, totals and the store, and no customer data. Screen C-24 Receipt, route `/r/{token}`.

**D-59 · Demand-insight settings.**
- `insight_no_sale_days` 21
- `insight_min_days_listed` 21 (equal to the no-sale rule)
- `insight_wanted_nearby_radius_m` 2000
- Every other insight threshold in 11 also gets a key.
- Location in analytics is an H3 resolution-8 cell, not "~100 m".
- The store rating has one writer, W2-06. The `store_reliability` job writes only reliability fields.

**D-60 · Reason lists and small items.**

| List | Values |
|---|---|
| Reject reasons | `out_of_stock`, `closing_soon`, `too_busy`, `cannot_deliver_address`, `price_changed`, `other` |
| Delivery-failed reasons | `customer_unreachable`, `address_not_found`, `customer_refused`, `other` |
| Admin cancel reasons | `customer_request`, `store_request`, `fraud_suspected`, `technical_issue`, `other` |
| Customer cancel reasons | `changed_mind`, `declined_partial` (used when they decline a partial accept), `too_slow`, `other` |
| Review tags, positive | `fresh_stock`, `fair_price`, `quick_service`, `friendly`, `stock_was_right`, `well_packed` |
| Review tags, negative | `item_missing`, `slow`, `rude`, `wrong_item`, `expired_item`, `overpriced` |

- The customer's full phone number is shown to the shop only while the order is `out_for_delivery`.
- All copy with numbers in it reads them from settings.
- Banner conversions: `banner_daily` (impressions, clicks, attributed orders). An order is attributed when it is at the same store within 24 h of a click.
- Admin TOTP recovery codes: `admin_recovery_codes(admin_id, code_hash, used_at)`, 10 per admin.

### Local first and secrets

**D-16 · Terraform.** Before Wave 5, the only Terraform steps are `terraform fmt -check`, `terraform validate`, tflint and checkov. The first `terraform plan` runs in Wave 5.

**D-17 · Real third-party services before Wave 5.** Default chosen; the user can change it.
- SMS, push and Sentry always use mocks or local tools before staging. Real SMS would reach real phones and needs DLT registration first. Errors go to local Grafana/Loki.
- Anthropic and Sarvam use mocks by default. If the user puts their own keys in `.env.local`, live AI evals and manual AI QA can run locally. That is the user's choice; the default suite never needs them.

**D-58 · Shared folder and Lighthouse.**
- All folders may depend on `shared/` and `design/`. When repos split, `shared` becomes a versioned package.
- Lighthouse CI writes to the local filesystem only.

### Ownership and order of work (file 07)

**D-11 · Wave gates.**
- The W2-13 gate runs only the E2E journeys whose dependencies are all in Waves 0–2. The E2E table in 08 has a "Gate" column.
- E2E-01, 08, 09, 11, 13 and 14, and the E2E part of CR-15, run at the Wave 3 gate (new W3-14).
- W4-01 runs all of them.

**D-46 · Work that had no owner.**

| Item | Owner |
|---|---|
| A-20 Banner moderation (queue and takedown) | W3-05 |
| Shop expiry-penalty nudge | Coach realtime rules, W3-11 |
| B-16 Inbox | W2-05 |
| `/overview` and `/customers` backends | W3-03 |
| `/public/leads` | W1-04 |
| `/test/clock` | W0-03 |
| AI endpoints and the `ai_drafts` table (feature, actor, input ref, prompt version, model, output, per-line outcomes, cost, latency) | W3-10. `voice_drafts` is folded into it as feature `voice_stock` |
| `admin_adjustment_requests` | W3-01 |
| `business_documents` (kind `pan | gst | shop_photo | storefront | license | other`, object key, status, reviewer) | The onboarding/KYC package |
| `admin_recovery_codes` | The admin auth package |

**D-47 · Admin screens.** W3-06 alone owns A-04. W3-08 alone owns A-14. W3-05 owns A-02, A-03, A-05…A-08 and A-20.

**D-48 · Overview and coach.**
- W3-03 owns `internal/insights` and the `business/src/overview` shell, including a `<CoachSlot/>` mount point.
- W3-11 owns `internal/coach/` (detectors move to `internal/coach/detectors`) and `business/src/coach/`.

**D-49 · Dashboards and the e2e folder.**
- Dashboard 13 is Cost.
- New dashboard 14, "AI quality and cost", is owned by W3-10.
- `e2e/` ownership:

| Folder | Owner |
|---|---|
| `e2e/journeys`, `e2e/fixtures` | W4-01 |
| `e2e/load` | W4-02 |
| `e2e/a11y`, `e2e/visual` | W4-04 |
| `e2e/mocks` | W0-07 |

**D-50 · Files that many packages edit.**
- **Permissions:** policy lives in `platform/authz/policy/<domain>.go`, with a golden table per domain. The lead generates `docs/generated/permissions.md`.
- **Events:** `shared/telemetry/events/<domain>.yaml`.
- **Jobs and seed:** W0 pre-creates one registry line and one no-op stub per known domain. Each package fills only its own stub.
- **Frontend routes:** each package owns its own `src/routes/<area>/` files. `routeTree.gen.ts` is git-ignored and generated.
- **Generated code** (`backend/gen`, `shared/api-client`, the bundled `openapi.yaml`): not committed. `make gen` produces it, and every build and check runs `make gen` first.
- **Makefile:** the root Makefile is owned by W0-01 and includes `mk/*.mk`. Each package adds its own `mk/<area>.mk`.
- **HTTP middleware:** W0-03 creates the chain in `platform/httpx` with a slot for every middleware. Each package fills only its own middleware file.

**D-51 · Folders.**
- W3-01 owns `business/src/statements/`.
- `business/src/standee/` is split: W2-10 owns the PDF and preview, W3-09 owns campaigns and analytics.
- W3-02 owns `admin-referrals.yaml` and `admin-risk.yaml`, with their backends and screens.

**D-52 · Wave 2 work that needed Wave 3.**
- W2-09 owns the `support_tickets` table, the customer create/list API and C-19. W3-08 owns the admin side.
- The standee PDF generator moves to W2-10.
- The `store_reliability` job is owned by W2-02. W1-06 ranking treats a missing value as 1.0.

**D-53 · Added dependencies.**
- W2-07 commits a pricing interface first (`internal/offers/pricing.go`, `Price(ctx, cart) Quote`). W2-02 and W2-04 build against it.
- W2-08 depends on W2-07.
- W2-09 depends on W2-05 and W2-06.
- W3-03 depends on W3-07.
- W3-04 depends on W3-10.

**D-54 · Scenario IDs.** W3-11 keeps SC-AI-10…40. W3-12 becomes SC-AI-41…55. W3-09's standee scenarios become SC-ST-01…04.

### Small consistency fixes

**D-55 · Editing the contract.** The bundled `openapi.yaml` is generated. Edit only your own domain's files in `backend/api/paths|schemas/`.

**D-56 · Make targets.** One canonical list, the same in 02, 09, CLAUDE.md and W0-01:
- `setup`, `dev`, `stop`, `logs`, `reset`, `gen`
- `check`, `e2e`, `load`, `security`, `ai-eval`
- `demo`, `env-check`, `release-candidate`

There is no `make seed`; use `make reset`.

**D-57 · Paths and hosts.**
- Tokens are at `design/source/tokens.json`.
- The package list in 02 now includes `banners`, `ai`, `coach`, `seed`.
- The Caddy hosts include `grafana.`, `mocks.`, `s3.`, `tiles.`, `img.`.
- Mailpit is removed.
- Wave 2 has 13 agents.

## Review 1 follow-ups (2026-10-22, questions raised while applying D-01…D-60)

**Names**

**D-61 · State and key names.**
- The pickup state is `collected`. Read D-01's `picked_up` as `collected`.
- The canonical settings key names are the ones in 01 §17. Every other file uses exactly those names, including the `ai_<pkg>_enabled` names and the `insight_*`, `coach_*` and `search_w_*` keys.

**D-62 · There are no earn caps.** D-30's sentence about earn caps is withdrawn, because 01 defines only spend caps. The cashback cap per bill limits earning.

**D-63 · Freshness in ranking.** Search ranking has no freshness term. Stale stock is handled by the coach and the expiry-rate penalty only.

**Roles and permissions**

**D-64 · Admin roles.** D-38 dropped two roles by mistake; they are back. There are 7 roles:

| Role | Can do |
|---|---|
| `field_agent` | Onboarding and items; their own stats at `/v1/admin/field/me/stats` |
| `support` | Orders and users view, cancel, tickets, review moderation and restore, strike reset, risk view, banner moderation |
| `catalogue` | Master catalogue review queue and merges |
| `analyst` | Read-only Pulse and dashboards |
| `finance` | Ledger, payouts (hold, unhold, mark paid, export), adjustment request and approve, masked bank details |
| `admin` | All operations except credit adjustments, role management, money settings, and payout mark-paid and hold |
| `super_admin` | Everything |

- KYC verification: `admin` and `super_admin`.
- Pulse: every admin role except `field_agent`.
- "Money settings" are keys with unit `paise`, `bp` or `credits`. Only `super_admin` can change them.

**D-65 · Cashiers and orders.** This replaces D-06 for orders. Cashiers handle orders day to day:
- They can: accept, reject, partial accept, mark ready, dispatch, mark delivered, mark delivery failed, and hand over (verify code).
- Manager and above only: cancelling an accepted order, POS reverse, manual discounts.
- POS round-off to the nearest ₹1 is automatic and allowed for cashiers.
- Voice line kinds are `sale`, `restock`, `count`, `set_price`:
  - `sale` records a quick POS sale with no customer (`sale_pos`) and is allowed for cashiers.
  - "Out of stock" is a `count` of 0.

**D-66 · Staff.** The owner manages all staff. A manager can add and remove cashiers only. There is no business-delete endpoint in v1; an owner asks support.

**Orders**

**D-67 · Strikes.**
- Strikes lower the `store_reliability` score.
- 5 or more strikes in 30 days (`store_strike_risk_threshold`) raises a risk flag. There is no automatic pause.
- Customer no-show strikes keep the rules already in 01.

**D-68 · Cancel reasons.**
- `ShopCancelReason`: `out_of_stock`, `customer_unreachable`, `closing_soon`, `other`.
- When a partial accept gets no answer in time, the order becomes `cancelled` with system reason `partial_timeout`, and nobody gets a strike.

**D-69 · Delivery-failed follow-through.**
- The reservation stays until the shop confirms the goods came back, or records `damage`. Cashiers ask a manager to record damage.
- The shop gets inbox reminders at 2 h and 24 h.
- After 72 h it is auto-confirmed as returned (`return_outcome = unconfirmed`), and a risk flag is raised.

**Credits**

**D-70 · AI cost cap.** `ai_daily_cost_cap_paise` defaults to 50000 (₹500 a day). An alert fires at 80%. The monthly budget wording is removed.

**D-71 · Coach privacy.** Every coach statement about other people, including benchmarks, needs k ≥ 10.

**D-72 · Primary store for rewards.** The primary store is the business's oldest store that is `live` when the reward posts. If the business has no live store, the reward waits as pending and posts when one goes live.

**D-73 · Two-person approval threshold.** The threshold compares the absolute value of the amount. Adjustments at or under it post immediately, with no request row.

**D-74 · POS reversal and clawback.** The reversal marks the original sale row; no separate reversal row is created. The clawback reads the customer's available balance after the spent credits are restored, in the same transaction.

**Customer screens**

**D-75 · Customer purchases.**
- `/me/purchases` lists online orders, plus POS sales linked by `bill_qr` or `counter_code`. Sales linked by phone only never show.
- The "In store" tab is on C-13, the Orders list. D-45 wrongly said C-08.

**D-76 · Cap messages.**
- At a store's daily cap: "Credits can't be used at this shop today".
- At the customer's monthly cap: "You've used this month's credit limit".

**Platform and ownership**

**D-77 · Banner attribution.** An order is attributed to a banner when the same customer orders at the same store within 24 h of a click. If they clicked more than one banner, the last click wins.

**D-78 · Risk flags table.** W0-03 creates the `risk_flags` table so that any package can raise a flag. W3-02 owns the rules and the screens.

**D-79 · Foreign keys within a wave.** Same-wave FK constraints are added by a constraints migration the lead writes at the end of each wave.

**D-80 · Test clock.** `/v1/test/clock` exists in `local` and `test`, for manual testing. It returns 404 in staging and prod builds.

**D-81 · Analytics location.** Analytics stores H3 resolution-8 cells. "Wanted nearby" adds up the cells within `insight_wanted_nearby_radius_m`, then applies k ≥ 10 to the total.

**D-82 · AI folders in 11.**
- W3-10 owns `internal/ai/{client,schema,scrub,guard,eval,prompts,drafts}`, `alerts/ai.yaml` and `docs/runbooks/ai-*`.
- W3-13 owns `admin/src/ai-assist/{catalogue,support,risk}`, mounted through an `<AssistSlot/>` in A-08, A-14 and A-13.
- W3-13 scenarios are SC-AI-56…65.

**D-83 · E2E-10 and E2E-12.** At the Wave 2 gate they drive the admin API. At the Wave 3 gate they run again through the UI.

**D-84 · Ownership not covered by D-01…D-60.**

| Item | Owner |
|---|---|
| `qr_standees` | W2-10 |
| `support_messages` | W2-09 |
| `catalogue_changes` | W1-05 |
| `business_documents` | W1-04 |
| Receipt tokens and `/me/purchases` API | W2-04 |
| C-24 screen and the "In store" tab | W2-09 |
| B-03 Overview and B-09 Customers | W3-03 (B-04 Orders is the business landing page until then) |
| CR-15 | W3-01 |
| `shared/ui` after Wave 1 | The lead |

**D-85 · Payout export.**
- The payout export (CSV) is the only place full bank account and UPI IDs appear, decrypted.
- Only `finance` and `super_admin` can export, with step-up. Every export is audited.
- Screens show masked values only.
- The delivery-failed timers (2 h, 24 h, 72 h) and the 30-day strike window are fixed constants in code, not settings.

**D-86 · Local stack images and ports (Wave 0 build).**
- Local Postgres runs on host port 5433, because 5432 is left free for a developer's own Postgres. The full port table is in the root `CLAUDE.md`.
- Which PostGIS image:
  - amd64 uses `postgis/postgis:16-3.4`.
  - arm64 uses `imresamu/postgis:16-3.4`, the same docker-postgis build published multi-arch; the official image is amd64 only.
  - `TODAYZ_POSTGIS_IMAGE` overrides the choice. The local stack and testcontainers use the same image.
- MinIO stopped publishing images, so the local R2 stand-in uses `pgsty/minio` with a pinned tag. These images are used locally only; staging and prod use RDS and R2.
- macOS ships GNU Make 3.81, which ignores `.SHELLFLAGS`. Recipes therefore run through `scripts/make-shell.sh` (bash `-euo pipefail`), and every canonical target stops at its first failing step. `scripts/test/make-fail-fast.sh` guards this.

**D-87 · Frontend shell details (Wave 0 build).**
- Active bottom-nav item: ink with a 2px icon stroke, as `design/source` `bundle.css` (`tz-bnav`) renders it. Only the active sidebar icon uses brand. The BottomNav README line saying "brand" is wrong; the CSS is the source of truth when they differ.
- `?locale=pseudo` (en-XA) works in non-production builds only. Playwright smoke runs on `--mode e2e` builds.
- Production builds use hidden sourcemaps. Shipped JS and CSS carry no `sourceMappingURL`, and deploy uploads the `.map` files to Sentry and keeps them out of the public bucket.
- The customer app holds `beforeinstallprompt` from the first visit; the prompt is shown only after the 2nd order (06).
- Playwright screenshots go to git-ignored `test-results/screenshots/`. `TZ_QA_DIR=docs/qa/<WP>` writes committed QA evidence.

**D-88 · Dependency age and security patches (Wave 0 build).**
- A new dependency version is adopted only when it has been released for at least 2 weeks.
- Exception: a patch release that fixes a vulnerability our code calls (govulncheck "called") or a Go toolchain point release may be adopted at once. Each exception is listed in the WP report.
- Go is pinned per module with a `toolchain` line in `go.mod` (`go1.26.10` from W0-03). `make setup` downloads it into the Go cache, so `make check` stays offline. The user's system Go is left alone.
- With the patched toolchain, govulncheck fails on any called standard-library vulnerability (`GOVULN_STDLIB=fail` by default).
- First exceptions (W0-03): `golang.org/x/net` v0.60.0 and Go 1.26.10.

**D-89 · Parallel-WP collision points in the backend (W0-03 review).**
- Migration versions come in per-WP blocks, `<wave><wp:2><seq:2>` (W2-03 owns `20301`–`20399`), so parallel WPs never pick the same number. Wave 0 keeps `00001`–`00099`. (goose first stayed in order here; D-90 replaces that with out-of-order migrations.)
- Routes, sqlc queries and partitioned tables follow D-50: one pre-created file or entry per domain (`mountX` stubs, `db/queries/<domain>/` with package `<domain>db`), and the tables to partition are listed in the owner-only `partition_policies` table (each WP inserts its own row in its migration; `ensure_partitions` reads it), not in a Go map.
- Idempotency is opt-in per route, fenced by a claim id, and stores response bodies encrypted under `PII_ENC_KEY`. A `Cache-Control: no-store` response (auth, OTP, tokens) is never stored.

**D-90 · goose runs with out-of-order migrations allowed.**
- Every environment runs goose with out-of-order migrations allowed (`WithAllowOutofOrder`, or the equivalent the code uses).
- Why: per-WP version blocks (D-89) mean a lower block can merge after a higher one in the same wave. This is safe because a WP never merges before the WPs it depends on, so anything a migration needs is already applied. WPs in one wave that do not depend on each other do not touch each other's tables.
- Locally, `make reset` remains the fix for a stale DB.
- Status: the W0-03 code does not set this option yet; goose still refuses a file numbered below the newest applied one. W0-04 adds the option with a test.


**D-91 · Refusal codes added from the QA book review.** Each is registered in `backend/api/problems/<domain>.yaml` and listed in 04 §1.4.
- `auth.yaml`: `otp_locked` 429, `otp_expired` 401, `account_blocked` 403, `step_up_required` 403 (admin TOTP older than 10 min).
- `orders.yaml`: `price_changed` 409 (fresh quote in `extensions`), `below_minimum` 409 (delivery minimum), `delivery_closed` 409 (`extensions.nextDeliveryWindow`; pickup still offered), `ordering_suspended` 409 (`extensions.until`), `extension_already_used` 409.
- `wallet.yaml`: `below_credit_minimum` 409, `credit_max_exceeded` 409, `credit_monthly_cap` 409, `store_credit_cap` 409. "Refused and flagged" (CR-11) means the matching code plus a `risk_flags` row; the order can still be placed without credits.
- `pos.yaml`: `claim_expired` 409, `claim_already_used` 409.
- `reviews.yaml`: `review_edit_window_passed` 409.
- `ai.yaml`: `ai_daily_cap_reached` 429, `extensions.scope` = `customer`, `store` or `platform`, `Retry-After` at the next IST midnight. Copy: "AI help is paused until tomorrow." The entry point is disabled, with no retry button.
- Status: documented in the plan; the handlers register the codes in their WPs.

**D-92 · New settings keys and the fixed-constants list.**
- New admin-editable, non-money keys (01 §17): `order_unseen_sms_after_min` (3), `pickup_hold_reminder_min` (10), `credit_expiry_reminder_days` (7), `review_edit_window_days` (7), `risk_code_lockouts_store_per_day` (10; precedent `pos_offline_links_risk_per_day` in D-12).
- Everything else that looked like a loose number is a fixed constant in code, listed in 01 §17 "Fixed constants".
- Why: numbers that protect security (code tries, bill QR and counter-code validity, step-up window, OTP limits), define a protocol (sync cursor, SSE poll, device clock tolerance, `sold_at` clamp, receipt token life) or feed an alert rule change through code review. Operational timings an operator may need to tune are settings.
- The 2 h / 24 h / 72 h failed-delivery values and the 30-day store-strike window stay constants (D-85). The customer no-show window is the setting `noshow_window_days`.

**D-93 · Other rules settled from the QA book review.**
- Q-03: `productsInRadius` counts in-stock listings (`store_products` with qty > 0) in `live` stores within the radius, not distinct products (04 §2.3).
- Q-07: `support` opens A-03 read-only; the action buttons are hidden and the API refuses them with 403 (06 §3, A-03).
- Q-13: timed responses and SSE order events carry `serverNow`; countdowns are computed from it, not the device clock (04 §1, 06 C-12, B-04).
- Q-14: the install prompt shows once, on C-12, right after the second order is placed; D-87 is unchanged (06 customer global).
- Q-18: no business notification for "credits expiring" in v1; WhatsApp, when enabled, carries the same events as SMS, with SMS as the fallback (01 §15).
- Q-20: seed admins in local and test are pre-enrolled with deterministic TOTP secrets, plus one unenrolled admin; the mocks UI has a "TOTP now" panel. The backend never reads under `e2e/`: the seed module reads the fixture path from `SEED_ADMINS_FILE`, which `infra/local/local.env` sets to `e2e/fixtures/admins.yaml`. With the variable unset it seeds no TOTP admins and logs the variable's name; it refuses to seed them unless `APP_ENV` is `local` or `test` (08 §2, 07 W0-07, W1-01).
- Q-21: the B-14 cashback example is computed from the store's current `cashback_bp`; at 0 it reads "Cashback is off…" (06 B-14).
- Q-26: SMS to reserved numbers still goes to the mock SMS server; only verification is short-circuited (01 §2, 08 §2).
- Q-28: `feature_delivery_enabled` off is a platform kill switch; a store with no pickup shows "In-store only" and booking returns `409 store_not_bookable`; in-progress delivery orders finish (01 §5).
- Q-32: the payout export CSV holds `pending` rows only, with fixed columns, stamps `exported_at`, and is sanitised against formula injection (04 §2.7, 03 §8).
- Q-33: admin adjustments are whole credits (fractions get `422 validation_failed`); dual approval applies above 500 credits, exclusive (08 CR-15).
- Q-34: Latin digits in every locale (`nu-latn`); native digits are later (02 §4.1, 07 W1-03, 10).
- Q-36: the grounding validator applies to every free-text AI output; for reply drafts the allowed numbers are those in the review text and order facts (11 §1 rule 5).
- Q-37: `GET /v1/test/slow?ms=` and `GET /v1/test/slow-sql?ms=` (both with `ms` capped at 30000) behind the test-clock gate, owner W1-07 (04 §2.9, 08 §2).
- Q-38: credit caps count when the hold is taken; a cancelled or expired order no longer counts (01 §7).
- Q-39: customer referral rewards post with the qualifying outcome; only shop rewards wait for the 10th order (D-72) (01 §8, 08 POS-21).
- Q-40a: a lot earned on day X (IST) is usable through day X+N and expires at 00:00 IST on X+N+1 (01 §7).
- Q-40c: hours `00:00`–`24:00` means open all day; B-14 has an "Open 24 hours" toggle (03 §2 `stores.hours`, 06 B-14).
- Q-40d: changing a timer setting never moves existing deadlines (01 §5.1).
- Q-40e: a shop may accept after closing time until `accept_by`; `store_closed` applies to placing only (01 §5.2).
- Q-40f: "first N orders" and `max_redemptions` are used up at placement and released on cancel or expiry (01 §12).
- Q-42: an item is low when quantity is at or below its threshold (01 §4).
- Q-43: accepting with too little stock returns `409 insufficient_stock`; an offline sale at an old cached price is accepted as recorded, with no risk flag in v1 (04 §2.6, 02 §4.3).
- Q-45: search weights keep unit `bp` and are super_admin-only on purpose (01 §17).
- Q-46: `verify-dist` moves to an allow-list (same-origin hashed bundles, and the Turnstile script on the lead-form page only); leads get admin list and status endpoints and a Leads tab on A-03 (02 §4.5, 04 §2.7, 06 A-03).
- Q-14, Q-35, Q-38 and Q-44 were confirmed by the founder.

**D-94 · ₹ thresholds and POS rounding (founder, 2026-10-22).**
- ₹ thresholds are inclusive. The delivery minimum, the free-delivery threshold and a minimum-bill offer are compared with the item subtotal: the items total after item-level offers, before the minimum-bill discount and before credits.
- Order of operations in a quote: item-level offers, minimum-bill offer, delivery fee, credits.
- Example, assuming free delivery on ₹500+: a ₹500 cart gets free delivery and the "₹50 off on ₹500+" offer, so it pays ₹450. A ₹520 cart with a ₹30 item offer has an item subtotal of ₹490: delivery is charged and the minimum-bill offer does not apply.
- This replaces "eligible amount" as the base for the delivery minimum and free delivery (D-28, D-29). Orders and POS sales store it as `item_subtotal_paise` (03). The eligible amount still drives the credit minimum, the credit cap and cashback.
- POS round-off is to the nearest ₹1 and half rounds up: ₹100.50 becomes ₹101. `round_off_paise` is between −49 and +50.
- Lives in: 01 §5.5, §7, §12; 03 §4 and §5; 04 quote row; 06 C-09, B-14; 08 POS-16.
