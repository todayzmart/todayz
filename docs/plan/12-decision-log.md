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

## Jobs platform, AWS shape and graceful shutdown (founder, 2026-10-22)

**D-95 · One framework for every background job.**
- Every River worker is declared through `jobs.Define(jobs.Kind{...})` in `backend/internal/platform/jobs`. A test fails if a worker is registered anywhere else.
- A kind has a name, owner WP, description, class, trigger (an IST cron `Schedule`, or event-triggered), `Timeout`, `MaxAttempts`, `Queue`, `Concurrency`, `UniqueBy` (the period key, so a scheduled period runs at most once) and a `Runbook` (required for `timer` and `money`).
- Classes: `timer`, `money`, `notify`, `marketing`, `ai`, `rollup`, `maintenance`. `ai` kinds only write drafts. `marketing` and `notify` kinds read their caps and quiet rules from the settings 01 already defines.
- A handler returns `jobs.Result{Summary, Note}`.
- Tables: `job_runs` (monthly-partitioned, retention `job_runs_retention_days`, default 90, dropped through the owner function `drop_old_partitions(parent, before)`, 03 §9), `job_stats_hourly` (with `cancelled` and `yielded` counters; the 7-day success rate is `succeeded / (succeeded + failed + cancelled + discarded)`), `job_controls`. Scheduled, manual, retry, backfill and continuation runs are always logged. Event-kind successes are only counted; their failures, retries and discards are logged. This keeps the table small.
- `timer` kinds can never be paused or run by hand. `money` kinds need `super_admin`, step-up and a reason. Other kinds need `admin` or `super_admin` and a reason. Every pause, resume, run-now and retry writes an audit row.
- `jobs_watchdog` runs every 5 minutes and marks a scheduled, unpaused kind `missed` after 2× its interval with no succeeded run (daily kinds: 15 minutes of grace). It writes one `missed` row per missed period, never one per tick.
- Metrics `tz_job_runs_total`, `tz_job_duration_seconds`, `tz_job_lateness_seconds` (this subsumes the timer lateness), `tz_job_missed`; dashboard 15; alerts "Job failing" and "Cron missed", each with a runbook.
- Every job already in the plan becomes a registered kind in the WP that owned it.
- 8 admin endpoints and the A-21 Jobs screen. Codes `job_not_runnable`, `job_already_running`, `job_paused`.
- New WP W1-11 owns the framework, the tables, the watchdog, the metrics, the endpoints, dashboard 15 and the alerts. W1-07 keeps the River metrics only.
- A key that a plan change adds to 01 §17 is seeded in the same merge by a lead migration in the Wave 0 block (`000NN_w0-03_settings_<topic>.sql`), with its `keys.go` constant. `TestSettingsMatchProductRules` reads `platform_settings` inserts from every migration, not only 00002 (lead change at merge). `job_runs_retention_days` is seeded this way; W1-11 does not seed it.
- Lives in: 01 §13, §17; 02 §2.2, §2.3, §2.7, §3.3; 03 §9, §13; 04 §1.4, §2.7; 05 §3, §4 (dashboard 15), §5, §8; 06 A-21; 07 W1-11; 08 SC-JB-01…15; 11 (coach jobs); manual `JOBS-ops-console`.

**D-96 · AWS shape for Wave 5 (single-instance RDS, as amended by the founder).**
- Access: the founder provides IAM access keys for Wave 5 only (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` in `.env.local`, never printed). Only Terraform and the lead's shell see them; the app uses instance profiles and never receives an `AWS_*` variable (`proc.sh` strips them, D-97). The first Terraform resource is an AWS Budgets alarm. CI deploys later use GitHub OIDC roles. Local-first is unchanged.
- Compute: Elastic Beanstalk, Docker on AL2023, Graviton. api 2–6 × t4g.small over 2 AZs, target tracking on `RequestCountPerTarget` (start at 1,000 per minute per target) plus CPU 60%, **traffic splitting** deploys (10% for 10 minutes, automatic rollback). worker 1–3 × t4g.small, step scaling on the age of the oldest available River job (above 30 s for 3 minutes; the worker publishes it with `cloudwatch:PutMetricData` into `TodayZ/Worker`, only in staging and prod, W5-04 owns it and W1-07 exposes the value), **immutable** deploys. Migrations run first (expand, migrate, contract).
- Load balancer: an ALB with HTTPS through ACM, Cloudflare IPs only, access logs to S3. Target group health check on `/readyz`: interval 5 s, timeout 4 s, unhealthy threshold 2, healthy threshold 2, so a draining instance leaves rotation inside the 15 s pre-stop (D-98).
- RDS prod: PostgreSQL 16 + PostGIS on **db.t4g.medium, Single-AZ**, gp3 100 GB autoscaling to 500 GB, PITR 14 days, deletion protection, a weekly cross-region snapshot, Database Insights (standard) and Enhanced Monitoring (60 s). No RDS Proxy. Upgrade to db.m7g.large when CPU stays above 60%.
- **Trade-off (founder's choice, to save the cost of a standby).** An AZ failure means a restore from PITR, an estimated 30–60 minutes of downtime. Moving to Multi-AZ is one Terraform flag (`multi_az = true`), applied in a maintenance window when the founder decides. A-22 shows "Multi-AZ: off" as a fact, not a warning. The first draft of this decision had Multi-AZ from launch; the amendment replaces it.
- Staging: the same shape, smaller and single-AZ (api and worker 1 × t4g.micro, RDS db.t4g.small), stoppable off-hours.
- Redis stays out of v1. Trigger to add ElastiCache Serverless (Valkey): rate-limit or cache queries above 20% of database load in Database Insights, or `LISTEN/NOTIFY` fan-out limits SSE.
- Observability: Grafana Cloud for app telemetry; CloudWatch added as a Grafana datasource through a read-only IAM role (W5-03). Not Amazon Managed Grafana (it needs IAM Identity Center, costs per editor and would duplicate the dashboards).
- Cost, to be re-checked with the AWS Pricing Calculator in Wave 5: prod about ₹13–17k a month (RDS about ₹4.5k, its storage about ₹2.5k, 3 × t4g.small about ₹3k, ALB about ₹2k, NAT about ₹3k, CloudWatch about ₹1–2k); staging about ₹4–6k. The AWS Budgets alarm is ₹30k a month for prod and staging together, alerting at 80% of actual spend and at 100% of forecast spend (it was ₹15k, which prod alone would now come close to).
- No earlier D-entry stated single-AZ, so nothing is tagged as superseded. The old 09 text ("Single-AZ at launch, Multi-AZ once there is revenue") is kept in spirit and expanded.
- Lives in: 02 §2.1, §2.3, §9.2, §10; 09 §2, §3, §5, §6; 10; 07 W1-10, W5-01…W5-04; 08 SC-GS-10.

**D-97 · A-22 System screen.**
- A read-only admin screen (`admin` and `super_admin`) shows Beanstalk health, version, instance count and the last 10 deploys; the ALB's requests per minute, 5xx rate and p95; RDS CPU, connections, free storage, Multi-AZ status ("off" at launch) and the top 10 SQL statements from `pg_stat_statements`; River queue depth and oldest job age; CloudWatch alarm states; and links to Grafana.
- One endpoint, `GET /v1/admin/system`, in `internal/ops`. It reads AWS through the API instance role with a read-only policy (`cloudwatch:GetMetricData`, `cloudwatch:DescribeAlarms`, `elasticbeanstalk:Describe*`, `rds:DescribeDBInstances`, `pi:GetResourceMetrics`, `pi:DescribeDimensionKeys`); actions that cannot be scoped to a resource use `Resource: "*"`. Results are cached 60 s per instance. Terraform checks the policy against an action allow-list (only `Describe*`, `Get*`, `List*` and the named reads), not a wildcard-resource rule.
- AWS keys stay out of the app. `internal/ops` builds an AWS client only when `APP_ENV` is `staging` or `prod`, with an explicit EC2 instance-role (IMDS) credential provider, never the SDK's default chain. W0-03b's `proc.sh` strips every `AWS_*` variable from app processes; only Terraform and the lead's shell see the founder's keys.
- Locally the AWS panels read "Not configured (local)" and make no AWS call, even with `AWS_ACCESS_KEY_ID` set in `.env.local`; the Postgres and River panels show real data.
- New WP W3-15 owns A-21, A-22 and `internal/ops`. It depends on W1-11 and W3-05's nav.
- Lives in: 02 §2.2, §3.3, §10; 04 §2.7; 06 A-22; 07 W3-15; 08 SC-OPS-01…09; 09 §3.6; manual `JOBS-ops-console`.

**D-98 · Graceful shutdown everywhere (founder, 2026-10-22).**
- Rule: no deploy, scale-in or restart cuts off an API request or a running job.
- API on SIGTERM: `/readyz` flips to 503 at once (`/healthz` stays 200); the server keeps serving for `SHUTDOWN_PRESTOP` (15 s deployed, 1 s locally); SSE streams get a final `event: reconnect` with `retry: 1000` and close; `srv.Shutdown` then waits up to `SHUTDOWN_DRAIN` = 75 s; the rest closes in order (background loops, DB pool, telemetry flush 5 s). `SHUTDOWN_DRAIN` must exceed `WriteTimeout` (60 s) and a test holds them together. The earlier 20 s drain was a bug. `BaseContext` is a background context that SIGTERM never cancels.
- Worker on SIGTERM: `/readyz` 503 and River `Stop`. A job `Timeout` is at most 60 s unless the kind is `Batched`; batched kinds work in batches of at most 60 s, checkpoint per batch and call `jobs.Yield(ctx)`. While draining, `Yield` enqueues a continuation and the run ends `yielded` (a new `job_runs` status). `queue.Drain(c, grace=90s, hardGrace=30s)` runs it: `DrainGrace` is 90 s, then `StopAndCancel` (30 s to return) as a last resort: logged at error level, counted in `tz_job_hard_cancelled_total{kind}`, alerted ("Job hard-cancelled at shutdown", warning, with a runbook), and the attempt is retried. Retries are safe because every job is idempotent and side effects carry an idempotency key. The continuation a yielding run enqueues is a `job_runs` row with `trigger` `continuation` and the unique key `<period>#<n>`, so River's uniqueness does not drop it while the original run is still `running`.
- Deploy timings: ALB deregistration delay 75 s; api container stop timeout 120 s; worker container stop timeout 180 s (this replaces the old "longer than 60 s" bullet). The ALB health check (5 s interval, 4 s timeout, 2 and 2 thresholds) takes a draining instance out of rotation inside the 15 s pre-stop; W5-04 also checks the Docker unit's stop timeout on the worker host, and if it is under 180 s sets it or adds an Auto Scaling termination lifecycle hook. Worker deploys are immutable, so River's leader election hands periodic scheduling over.
- Migrations still run first and are backward compatible, so draining instances keep working.
- Frontends (screen rule 06 G9): no forced reloads; the update toast lets the user choose; B-05 and C-11 defer the prompt while a sale or checkout is open; the service worker precaches every chunk of its own version, so an old tab keeps working after a Pages deploy.
- Local: `make stop` sends SIGTERM and waits for the same drain (bounded at 180 s for the worker) before SIGKILL. `proc.sh stop-all` stops `api` and `worker` first, waits for them, then stops the rest, mocks last, so a draining job can still reach the mocks.
- Ownership: the new W0-03b (lead follow-up, before the Wave 0 gate) owns `internal/platform/{lifecycle,httpx,queue,health}`, the telemetry flush, `cmd/todayz` and `proc.sh`: the API steps, the draining flag, the drain test, `queue.Drain`, the hard-cancel metric and the ordered `proc.sh` stop. It fixes the `queue.go` comment ("twice this") and the shutdown lines in `backend/CLAUDE.md`. Its tests use an in-test slow handler: `/v1/test/slow` is a Wave 1 route (W1-07), so Wave 0 does not depend on it. `Batched`, `Yield`, `yielded` and the 60 s cap belong to W1-11; SSE reconnect to W2-05; frontend deferral to W1-08, W2-11 and W2-09; ALB and container timeouts to W5.
- Lives in: 02 §2.8, §5; 04 `/readyz`; 05 §5, §8; 06 G9; 07 W0-03b, W1-11, W2-05, W5; 08 SC-GS-01…10; 09 §3.7; manual `JOBS-ops-console`.

## Demand insights v2 (founder, 2026-10-22)

**D-99 · Demand insights v2 (founder, 2026-10-22).**
- The B-03 card matches the design (`design/source/components/DemandInsights/`): title "Demand insights", the line "What sells, what sits, what is about to go to waste, and what people nearby want", tag "Last 7 days" (replaces "Past 7 days"), tinted icon square, label, a sentence with the product in bold, and a text action with an arrow. The metric chip is dropped (the design has none; the numbers sit in the sentence). "Check stock ›" is replaced by an action specific to the kind. Nothing is applied by the action: it opens the target screen prefilled.
- Four original kinds and their actions: selling fast → **Restock →** (quantity = `ceil(sold × insight_cover_days ÷ insight_period_days) − available`, never below 0, whole-number maths; sold = units over `insight_period_days`); sitting on the shelf → **Create offer →** (a % inside the backend's safe limits; AI-R4 may change only the % and the wording; the tied-up value at cost is shown when a cost is known: the latest batch with remaining above 0 that has a cost, a batch with no cost being skipped); going to waste → **Flash offer →** ("N items expire within D days: A, B, C + N more"); wanted nearby → **Add product →** (prefilled from the master match). "Wanted nearby" appends "No shop nearby has it." only when at least 10 other live verified stores of distinct businesses are inside the radius and none lists the product with stock above 0; otherwise only the search count shows (review fix M10). The response carries a boolean and never a count or a name of competing shops. The design README's "how many nearby shops do" is therefore not built (the design README now says so).
- Seven new deterministic kinds (plain SQL, no AI): missed sales, expired on shelf (**Mark expired →**, an `expired` movement per batch, manager and above), selling below cost (**Edit price →**), stock check due (**Count stock →**), bought together (**Create offer →**; buy-X-get-Y stays same-item only), regulars slipping away (**See customers →**, B-09 `segment=slipping`, no messaging in v1) and priced above nearby (off by default, Q-52). Owner and manager only, like the card. Thresholds are the new `insight_*` keys in 01 §17 and 11 §2.1.
- The card shows the top 4 firing rows by severity, at most one per kind. The severity ladder, with the 2-day rule, is in 01 §12 (04 §2.6 quotes it): `high`: expired on shelf, going to waste, selling below cost, selling fast with available ÷ pace of 2 days or less; `medium`: other selling fast, missed sales, stock check due, regulars slipping; `low`: sitting on the shelf, wanted nearby, bought together, priced above nearby. Inside a severity the kinds go in the 01 §12 table order, and rows of one kind go by money or units at stake. "All insights" opens an Insights tab in B-12 through `GET {S}/insights?all=1` (same endpoint, cursor paging). **Change from the first D-99 wording (review fix M3, lead decision):** the card shows only kinds that fire; if none fires it shows "Nothing needs you right now" with the setup checklist, and a kind below its minimum evidence shows its "what data it needs" line only in the B-12 tab, never a number and never on the card. The first wording put that line on the card.
- "Not useful" on a row: new `POST {S}/insights/dismiss` and table `insight_feedback` (unique `(store_id, kind, subject_ref)`). It quiets the row's subject, not the whole kind, for `coach_dismiss_quiet_days` (review fix M2): an item, a master product or `q:` query, a pair `a:b`, a batch id (both waste kinds) or the store. **Expired on shelf cannot be dismissed** (a food-safety row; it goes away when the batch is written off or reaches 0), and the API refuses it with `422 validation_failed`. `subjectRef` is validated per kind (`422 validation_failed`); the response is `200 { quietUntil }`; a repeat call keeps the existing `quiet_until`; "idempotent" means a natural upsert, with no `Idempotency-Key` (L11). The coach reads the rows with `coach_suggestions.status` for per-shop detector weights. This is the only new endpoint (the spec allowed none unless paging needed it; a dismissal cannot ride on a GET). It is one of the 295 method + path pairs 04 §4 counts (L1; the earlier "285 → 286" was against an older base). Round 3 swaps `GET/POST {S}/batches` for `GET {S}/batches` plus `PATCH {S}/batches/{batchId}`, so the count stays 295.
- Data: `search_log.product_id` is written by W1-06 when the query is an exact barcode or the normalised query equals a master product's name or one of its synonyms, otherwise null (no threshold, no setting; the old "ranking confidence threshold" is gone, review fix M7). `rollup_daily_search_demand.product_id` (nullable) is created in W3-07's own creating migration in its 307xx block (D-89), so "missed sales" counts searches for the product, not the query text. `rollup_item_pairs` (nightly, class `rollup` in the D-95 job registry, owner W3-07) holds per-store pair counts, `bills_total` and `lift_bp` for "bought together": every pair with `bills_both ≥ insight_pair_min_bills`, the top 100 per store by lift, with the share and lift tests applied at read time (round 3). Time at zero is derived from the movement ledger: a day counts when the item's closing stock for that whole IST day was 0 or below, and today never counts (review fix M6); no new history table. **Batches are used up as stock moves (review fix H1, owner W1-05):** 03 stock rule 8; a sale takes unexpired dated batches first (earliest `expires_on` first), then undated batches, then stock in no batch, and expired batches last (round 3), while `damage`, a downward count and the expired write-off keep the plain earliest-expiry-first order; the allocation saved in the new table `stock_movement_batches`, reversals and returns restore the source batches, a positive count creates a "found in count" batch, and "going to waste" and "expired on shelf" read the batch's remaining quantity, never the received quantity; without this "expired on shelf" would report stock that was already sold. `stock_batches` gains `received_quantity`, `found_in_count` and a partial index; `coach_suggestions.action_type` gains five new action types (`mark_expired`, `edit_price`, `check_cost`, `count_stock`, `see_customers`) so the coach and the card share one enum.
- Ownership (D-48 unchanged): W3-03 owns `internal/insights`, including `internal/insights/facts`, and the card and tab; W3-11 owns the coach and imports `facts` instead of recomputing any shared number. No file moves between WPs.
- k-anonymity: searches k ≥ 10 over the summed cells (D-81), `insight_missed_sales_min_searches` never below 10, "priced above nearby" and "No shop nearby has it." need at least 10 other live verified stores of distinct businesses (the shop's own business does not count) whatever the setting (D-71). Nothing names another shop, and no insight row carries a customer's phone (the B-09 "Slipping away" list shows phones masked as in R-16.2).
- **Open, for the founder (Q-52, traceability §6):** should "Priced above nearby" be on at launch? It compares a shop's price with the typical price of nearby shops. It is useful, but shops may dislike being compared. `insight_price_compare_enabled` is false until the founder decides.
- New 01 §17 keys (twelve; the lead seeds them, with min and max, in `00011_w0-03_settings_d99.sql`, the D-95 rule, and the same migration updates the description of `insight_period_days` to "Last 7 days"). Each is `key (unit, default, min, max)`: `insight_cover_days` (days, 7, 1, 30); `insight_missed_sales_min_zero_days` (days, 1, 1, 30); `insight_missed_sales_min_searches` (count, 10, 10, none); `insight_pair_min_bills` (count, 20, 5, 1000); `insight_pair_window_days` (days, 60, 14, 180); `insight_pair_min_share_bp` (bp, 2000, 500, 10000); `insight_pair_min_lift_pct` (count, 150, 100, 1000); `insight_regular_min_visits` (count, 3, 2, 20); `insight_regular_window_days` (days, 60, 14, 180); `insight_regular_lapse_days` (days, 21, 7, 90); `insight_regular_min_linked_customers` (count, 10, 1, none); `insight_price_compare_enabled` (bool, false). The first draft's `insight_stock_check_days` is dropped: "Stock check due" follows `stock_stale_after_hours` (review fix M4). `insight_pair_min_share_bp` has unit `bp`, so it is a super_admin-only key (D-64); `insight_pair_min_lift_pct` is a count key, so an admin can change it. `insight_missed_sales_min_searches` is never below 10 (01 §16).
- Review fixes (lead-approved, 2026-10-22), in short: H1 batches used up as stock moves (above); H2 "bought together" needs lift; M1 one share rule, written once in 01 §12 and copied word for word into 03 and 05: item A is the item of the pair with more bills (ties: the lower item id), the share is `bills_both ÷ bills_A`; M3 needs-data lines only in the tab (above); M4 stock check due fires at `stock_stale_after_hours`, the moment customers see "Stock not confirmed recently", and 01 §4 now says what refreshes `stock_updated_at` (any movement, and a count with no change); M5 missed sales counts searches, not people; M8 the prefill deep links are defined once in 06 "Insight deep links", built by W2-12, and W3-03 only links; M9 selling below cost compares the latest batch with remaining stock that has a cost, cost per sale unit as paid including GST, with the "Check the cost entry" case; L5 every window is whole IST calendar days ending yesterday; L6 the severity ladder is in 01 §12 and the 2-day rule is a fixed constant in the 01 §17 table; L8 regulars count distinct IST purchase days, exclude reversed POS sales and walk-ins without a phone, and need `insight_regular_min_linked_customers`; L9 missed sales only for `is_listed` items; L2 IN-01's wording scan covers the apps and the i18n bundles, not the docs.
- Choices beyond the spec: the min and max of every new key (the table above; `insight_pair_min_lift_pct` min 100 and `insight_regular_min_linked_customers` min 1 and default 10 were given; the other bounds are mine); stock in that carries an expiry or a cost creates a batch (others stay plain quantity); the per-batch allocation is a table, `stock_movement_batches`, not a jsonb column; `stock_batches` gets `received_quantity` and `found_in_count`; undated batches are used after dated ones, oldest received first (for a sale, after the unexpired dated ones and before stock in no batch and expired batches); a count that finds more adds a "found in count" batch with no expiry; the "Mark expired" write-off names its batches and does not follow the earliest-first order; the subject of a going-to-waste row is its earliest-expiring batch id and the server writes one `insight_feedback` row per batch in the row at that moment; the subject of wanted nearby is the master product id, or `q:` plus the normalised query when no product matches; a new action type `check_cost` ("Check cost →", opens the edit form of that batch) for the cost-at-or-above-MRP row, whose suggested price on the normal row is the cost rounded up to a whole rupee and never above the MRP, and the "Check the cost entry" row ranks last in its kind; the "2 days" in the severity ladder is a fixed constant rather than a key, and so are the 4 card rows, the 100 pairs kept per store and the 10-store floor (all in the 01 fixed-constants table); a shop's other stores do not count as "other" stores for the 10-store floor; the dismiss response is `200 { quietUntil }`; the pair rollup filters on the bill floor only: it stores every pair with `bills_both ≥ insight_pair_min_bills` and `lift_bp` and keeps the top 100 per store by lift, and reads apply the share and lift tests with the current settings (changing `insight_pair_min_share_bp` or `insight_pair_min_lift_pct` is immediate; changing `insight_pair_min_bills` or `insight_pair_window_days` waits for the nightly run or `todayz rollup`); the stock-check sentence reads "N hours" when `stock_stale_after_hours` is not a multiple of 24; the kind-level needs-data line shows only for a kind that has no row and is below its evidence; a regular is lapsed when the last visit date is on or before today minus `insight_regular_lapse_days`; inside a severity kinds follow the 01 §12 table order and only rows of one kind compare money or units at stake; the count-mode link carries at most 100 item ids; the earlier choices still stand: extra keys `insight_missed_sales_min_zero_days` (1), `insight_pair_window_days` (60) and `insight_regular_window_days` (60) so no threshold is hard-coded, the card shows 4 rows as a fixed number (the design's four) rather than a setting, and "bought together" renders "1 of N" as `round(bills_A ÷ bills_both)`. Seed rows that make every kind fire are in 08 §2 and the manual book README §7.
- **Round 3 (lead-approved, 2026-10-22), in short:** (1) **A sale skips expired batches** (stock rule 8): sale order is unexpired dated batches by earliest `expires_on`, undated batches oldest received first, stock in no batch (max(`on_hand`, 0) − Σ batch remaining), then expired batches by earliest expiry; "expired" is `expires_on` before today IST, the test Expired on shelf uses; the property test is "a sale uses an expired batch only when no unexpired stock remains". The invariant is Σ batch remaining ≤ max(`on_hand`, 0), because an offline sale can drive `on_hand` below 0. (2) **Correcting a batch:** `PATCH {S}/batches/{batchId}` changes `cost_paise` and `expires_on` only, `batches.manage` (manager and above), audited with before and after; an expiry change re-runs nothing, and the next insights read uses it; there is no `POST {S}/batches`, batches are created only by stock in and by a positive count; "Check cost →" opens this edit form. (3) **Cost source:** "Selling below cost" and the "₹ at cost" value read the latest batch with remaining above 0 that has a cost. (4) **Pair rollup:** bill floor only; share and lift apply at read time. (5) **Mark expired across batches:** prefilled earliest expiry first, each at min(batch remaining, available left), so the total never exceeds available. (6) **Missed sales:** a day at 0 means closing stock of 0 or below; the "Restock →" quantity uses the Selling fast formula. (7) **Seeded searches sit in named H3 res-8 cells** (W, S2, M), each at most 1.5 km from the shop it counts for and at least 2.5 km from the other, and the seed fails the build if a distance bound is broken. (8) **Seed shape:** an 11th phone-linked customer (H11) so `insight_regular_min_linked_customers` (10) is not on the boundary; J1, J20 and J21 are the only items with 5 or more sales in 7 days (64, 56, 35 units; 15.0 and 16.0 days of stock for the last two), so at `insight_cover_days` 14 only Tata Tea fires, and at `insight_pair_min_lift_pct` 100 only the milk and Parle-G pair joins Bread and Eggs. (9) The 01 §17 table keeps four columns (Key, Unit, Default, Rule); each bound is the last sentence of the Rule text, "Min 1, max 30." or "Min 10.". The bounds listed in the key list above stand.
- Lives in: 01 §4, §11, §12, §16, §17; 03 §3 (stock rules 5–8, `stock_batches`, `stock_movement_batches`), §9, §10, §11, §12; 04 §2.6 (insights, customers, movements), §2.11; 05 events and §3; 06 B-03, B-07, B-08, B-09, B-12, B-19 and "Insight deep links"; 07 W1-05, W1-06, W2-12, W3-03, W3-07, W3-11; 08 §2, §3.7 (IN-01…IN-28); 11 AI-R4 and §2.1; design `design/README.md` "Where the plan differs from the design"; manual book INSIGHT-demand-insights.md, README §6–§7, traceability.
- **Lead fix at merge (2026-10-22):** the lift key is `insight_pair_min_lift_pct` (count, 150, min 100, max 1000), not `insight_pair_min_lift_bp`: a `bp` key is a share capped at 10000 by `ck_platform_settings_bp`, and lift is a ratio above 1. It is a count key, so an admin can change it; `rollup_item_pairs.lift_bp` keeps lift × 10000 and a pair fires when `lift_bp ≥ insight_pair_min_lift_pct × 100`.
