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
- Status: done in W0-04. `internal/platform/migrate` passes `goose.WithAllowOutofOrder(true)`; `TestOutOfOrderBlocks` applies a higher block first, then a lower one.


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
- The 2 h / 24 h / 72 h failed-delivery values and the 30-day store-strike window stay constants (D-85). (D-218 later made the 2 h and 24 h reminders settings; 72 h stays a constant.) The customer no-show window is the setting `noshow_window_days`.

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

## W0-04 review fixes (lead, 2026-10-22)

**D-100 · Contract and client review fixes (lead, 2026-10-22).**
- **Status codes (D1).** `validation_failed` is 422 (the registry, `common.yaml`, the client, the Schemathesis hooks and every plan line agree). Malformed JSON, trailing data, more than 10,000 JSON values, an array past its `maxItems` and a path two routers could read differently are `400 bad_request` with a generic detail that never repeats the client's bytes. The wrong media type stays 415, a body past the limit 413.
- **One router decides (D2, security H-1).** The validator runs after chi has picked the route and finds the operation by method and chi's route pattern; an unmounted path or method is chi's 404 or 405 (`Allow` lists mounted methods only) in every environment, so `/v1/test/clock` and `/version` are 404 where they are not mounted. Slot 8 `pathguard` answers 400 first for a raw path that is not the canonical encoding of its decoded form or carries `%2F`/`%2f` or `%5C`/`%5c`, so chi (raw path) and the validator (decoded path) always see one path. Fail closed both ways: a mounted route with no contract operation fails startup and `TestRoutesAreInTheContract`; a contract operation with no route is listed by the reverse test with an allowlist until its domain lands.
- **Order (D3).** `requestid → secheaders → recover → otel → clientip → accesslog → cors → pathguard → routepolicy → bodylimit → authn → ratelimit`, chi routes, then `validate → idempotency → authz` per route. Authn before validation: an unauthenticated call to `/v1/me`, `/v1/biz` or `/v1/admin` is 401 and never learns a field name. Validation before idempotency: a refused request is never stored or replayed.
- **Field paths (D4).** `fields` keys keep their location prefix (`body.items[2].quantity`, `query.limit`, `path.orderId`, `header.If-Match`); clients map `body.*` to form fields. At most 50 entries are listed, then `extensions.fieldsTruncated: true` (H-2).
- **Body limits (H-2).** The plan's limits stand: 1 MB for every JSON body, 10 MB on routes that declare `x-upload`. Inside the 1 MB the decoder caps the work: the body is buffered (at most 1 MB), pre-scanned for the 10,000-value budget, `maxItems` (also under `oneOf`/`anyOf`) and trailing data, then validated. Every request-body string carries `maxLength`, an enum, a const, `format: binary` or a length-bounding pattern, and every request-body array `maxItems` (apigen lint). A request body is `application/json` only; an `x-upload` operation takes any other media type and may not take JSON (apigen and `openapi.New` refuse any other contract). An upload body is checked for media type and size only and streamed to the handler once (M3, L10).
- **Policy from the spec (M-1).** Every unsafe operation under `/v1/me`, `/v1/biz`, `/v1/admin` states `x-idempotency: none|optional|required` (`none` is explicit, not implied); `x-money: true` requires `required`; `openapi.New` fails on any other value. The route's effective policy is the contract's merged with the code's, the stronger rule winning (higher idempotency, upload ORed); code weaker than the contract fails startup and the test. `/v1/biz/businesses` (the exact collection path, where a business is created) is an allowed prefix (M1). `x-open-object: true` is allowed only on an object without properties (security L-3). A `$ref` may not reach into another domain's schema file (L1).
- **Keyword ban (M-2).** The contract may not use `if`, `then`, `else`, `dependentRequired`, `dependentSchemas`, `contains`, `minContains`, `maxContains`, `patternProperties`, `propertyNames`, `unevaluatedProperties`, `unevaluatedItems` or `prefixItems`: the validator does not run them, so a rule written with them would be silently unenforced. The lint refuses them.
- **Strict handlers (M2).** Every generated `Register` builds its strict handler with `openapi.StrictOptions()`: a request or response error is a problem, never a bare string.
- **Client (M-3, L7, security L-6, L-4).** orval encodes path parameters; `request()` refuses a built path with a dot segment (`/../`, `/./`, `%2e`) or outside `/v1/` and the health paths; apps validate ids with the generated Zod schemas before calling. The client's fallback codes are registered ones only (`internal` for 5xx, `bad_request` below; `bad_gateway` is dropped). `Accept` is set only when the caller has not. `ApiError.toJSON()` is `{code, status, requestId}`. The MSW generator writes contract strings with `JSON.stringify`, defuses `*/` in comments and refuses an `operationId` that is not an identifier. An echoed unknown JSON key must match `[A-Za-z0-9_]{1,64}`, else it is reported as `body.<redacted>` (L4).
- **Redocly stays in `shared/api-client` (D5).** The rules let any folder depend on `shared/`; `make gen` passes every backend path explicitly (`TODAYZ_API_ROOT`, `TODAYZ_OPENAPI`, `TODAYZ_REDOCLY_CONFIG` to the bundler and the client generator, `TODAYZ_PROBLEMS_DIR` to the client generator, which writes the registry to the git-ignored `src/gen/problems.json`, and to the Schemathesis hooks), and nothing in `shared/` has a default into a sibling folder or reads `backend/` itself.
- **`make e2e` from a stopped stack (D6).** `e2e-contract` depends on `e2e-stack-up` (the idempotent `make dev`), because `e2e-stack`'s smoke ends with `make stop`. Only one stack runs per machine.
- **Toolchain and fuzzing (L8, L9, L11, security L-5).** uv and CPython are pinned to the patch (`UV_VERSION`, `CONTRACT_PYTHON` in `mk/25-contract.mk`) and installed into `.tools/` by `scripts/install-uv.sh` (SHA-256 per platform) and `uv python install`; no `brew install`. `e2e/contract/run.sh` refuses a base URL that is not `https?://<name>.todayz.localhost` or loopback. Hypothesis' database lives under the contract report dir; `.hypothesis/` is git-ignored. kin-openapi in `backend/tools` matches the runtime (v0.149.0). `make check` sets `TODAYZ_CHECK`, `TODAYZ_OAPI_CODEGEN` and `TODAYZ_REDOCLY`, so the codegen round trip never skips; `regen.test.ts` proves a removed operation disappears from the TypeScript client.
- **Migrations (L5, L6).** A migration may only touch tables created in its own block (by a lower number) or a lower block (`TestMigrationBlockOrder`, `CheckBlockOrder`; statement heads only, run-time DDL inside functions is out of scope by design). goose unwinds Down in reverse order of application, not of version; `TestOutOfOrderBlocks` says so.
- **Round 2 (lead review of the fixes, 2026-10-23).** Media types (M-A): a request body is `application/json` only unless the operation is `x-upload: true`, and then it is anything but JSON; apigen and `openapi.New` refuse the rest. `/v1/me` (M-B) is an authn prefix like `/v1/biz` and `/v1/admin`: an anonymous call is 401 before validation. Type arrays (M-C): the bounds and closed-object lints use `Types.Includes`, so `[string, "null"]` is linted, and the pre-scan walks `oneOf`/`anyOf` for `maxItems`. Error amplification (Low 2): kin-openapi v0.149.0 can only fail fast or collect every error, so the bound is measured and tested instead of capped (`TestErrorAmplificationBound`: a 1,022,459-byte body with 9,627 values and 4,800 unknown fields allocates about 10.8 MB and takes 17–20 ms on an Apple M-series, Go 1.26.10; the test fails above 200 MB or 2 s). An upload is granted by the contract only (Low 3): `Upload: true` in code on an operation without `x-upload` fails startup. The 04 §1.6 money operations are a list in apigen (Low 4, `moneyOperations`): an operation with one of those ids needs `x-money: true`; a domain that names its operation differently renames it in apigen and 04 §1.6 in the same change. A path may not end in `/` (Low 5), neither in the contract nor as a mounted pattern. A bounding pattern may not have a top-level alternation outside a group or end in an escaped `\$` (Low 6); `$dynamicRef`, `$dynamicAnchor` and `contentSchema` join the keyword ban (Low 7). `run.sh` refuses a URL with a backslash, whitespace or `@` (Low 8); `install-uv.sh` records the SHA-256 of the uv binary as well as the tarball's and compares the installed file on every setup (Low 9). `request()` resolves by the response's `Content-Type` (JSON parsed, `text/*` as text, else a Blob) and refuses a path with a backslash, tab, CR or LF (Low 10, 11). "An unknown field is 422 validation_failed" everywhere (Low 12). `shared/` never reads `backend/` (Low 13): `make gen` writes the problem registry into `shared/api-client/src/gen/problems.json` through `TODAYZ_PROBLEMS_DIR`, the test fails without it under `make check`. `CheckBlockOrder` also follows `REFERENCES` in CREATE and ALTER TABLE, `CREATE POLICY … ON`, and the FROM/JOIN tables of a view or materialized view (Low 14); what it still does not read (CTE statements, UPDATE/DELETE FROM and USING, subselects, `SELECT INTO`, `ATTACH PARTITION`, rules, locks, COPY, DO blocks and function bodies) is written in `blocks.go` and `db/migrations/README.md`.
- Lives in: 02 §2.4; 04 §1.4, §1.6; 08 PL-01; `backend/CLAUDE.md`, `backend/api/README.md`, `backend/db/migrations/README.md`, `e2e/CLAUDE.md`, `shared/api-client/CLAUDE.md`; `mk/25-contract.mk`.

## Ask shops nearby and search proof (founder, 2026-10-22)

**D-101 · Ask shops nearby, proof on results, voice and plain-language search, share cards, site sections (founder, 2026-10-22).** Inspired by an AI agent that phones local shops to confirm stock; TodayZ does it without calls: the shops answer in the business app. D-100 is W0-04's and is recorded separately.
- **Ask shops nearby (01 §11.2).** Entry points: a search with no in-stock result or none confirmed within `stock_stale_after_hours`, every product page, and "Ask this shop" on a store page. A signed-in customer (verified phone) sends at most `ask_daily_limit_customer` asks per IST day (`429 ask_limit_reached`) and has one open ask per product, or per normalised text, at a time (`409 ask_already_open`). The ask is a master product or free text (search normalisation), a quantity (default 1) and an optional "need by" (now, today, tomorrow); never the customer's name or phone. It goes to `live` stores of `verified` businesses within `ask_radius_m`, at most `ask_max_stores`, leaving out stores closed now, at `ask_daily_limit_store` asks today, muted, or that reported this customer; stores that list the product come first, then stores that list its category, then the rest, nearest first in each group. "Ask this shop" ignores the radius and keeps every exclusion (`409 ask_store_unavailable`); with no store left, `409 ask_no_shops`.
- **The shop's reply.** A push and a card in the new Asks inbox (B-20): item, quantity, "need by" and a distance band. One tap: **Yes** with the listed price prefilled (never above MRP, `422 price_above_mrp`) and an optional "ready by", or **No**, within `ask_reply_window_min`. A Yes on a listed item refreshes `stock_updated_at` with no movement (01 §4); "You have {n}" when stock is short; a reply never changes stock. A Yes on an unlisted item offers "Add it to your stock" (the normal add form, prefilled; nothing is created until the shop saves). Permissions (01 §3, 02 §3.3): `asks.view` and `asks.reply` for cashier and above (reply and report), `asks.manage` for owner and manager (mute, store setting `asks_muted`, audited). A second reply is `409 invalid_transition`; a reply after the window is `409 ask_closed`.
- **The customer side.** Live progress "Asked 8 shops · 3 replied · best ₹38" over the same SSE stream as orders (`ask.updated` on `/v1/me/stream`) with the same polling fallback; Yes cards with shop, distance, price, "ready by" and "Replied {n} min ago". **Book** opens the normal checkout with `askReplyId`: within `ask_price_valid_min` the line is priced at the replied price per unit, never stacked with an item offer, for at most the asked quantity (the quote carries `priceSource` and `validUntil`); validity is checked again at placement (`409 ask_price_expired` with the fresh quote, never a silent re-price); a partial accept keeps the replied unit price; after the validity, "Price may have changed" and the current price. A reply books at most one active order (`409 ask_reply_used`, a unique partial index on `orders.ask_reply_id`). The order records `ask_reply_id` and the line `price_source`; the reply reserves nothing.
- **Unanswered demand.** An ask with no Yes counts as one search for each store that received it in "Wanted nearby" and "Missed sales", under the same k ≥ 10 floors (D-99, D-81). Admin **Demand gaps** (A-23): products asked by at least `ask_gap_min_customers` distinct customers in a city in `ask_gap_window_days` with no Yes; counts only, a lead for the field team.
- **Abuse.** The usual rate limits; after `ask_low_yield_after_count` asks in a row with no Yes, a customer's asks go to at most `ask_low_yield_max_stores` stores until one of their online orders is `collected` or `delivered`. "Report this ask" (open or closed, while the ask is kept) raises a `risk_flags` row with the new rule `ask_reported` (the D-78 table, shown in A-13) and blocks that store from the customer's later asks; the customer gets the same response whatever the reason a store was left out, and a block is never revealed. Asks, with their replies and blocks, are kept `ask_retention_days` and deleted with the account.
- **Not in v1:** WhatsApp delivery of asks (`ask_whatsapp_enabled`, false; a Wave 5 option because each message costs money, Q-55). AI voice calls to shops: rejected (TRAI rules on automated calls, cost, shop goodwill, and they go around the shop's own app).
- **Proof on results (01 §11.1).** "Confirmed {time} ago" while `stock_updated_at` is within `stock_stale_after_hours` (an ask Yes reads the same); "Stock not confirmed recently" after that, unchanged. Badges are labels only and never change the ranking: at most one per card, each on at most one card of the first page, ties to the nearer store; **Closest**; **Lowest price** (the total for the chosen mode, with the delivery fee when delivering); **Ready soonest** only among ask replies in v1 (see the TODO below). "+₹20 delivery" shows on a delivering card. The **summary line** "Checked {N} shops nearby · {M} have it · from ₹{min}" counts live verified stores in the radius and the ones with it in stock, is hidden at M = 0 (the Ask button shows), and needs no k floor: it counts shops whose listings the customer already sees, not people, and 01 §16's floors are about people (checked).
- **Refine chips.** "under ₹200", "open now", "deliver by 7", "pickup", "on offer", "near me" or a master-catalogue brand become removable chips. A deterministic parser runs in all five languages and their digits (W1-06), and search never calls a model. When the synonym table missed, or the parser found nothing in a query of at least `refine_ai_min_words` words, search returns `refineHint`; then, after the results render and only for a signed-in customer, the app calls AI-C2 search assist (`ai_querynorm_enabled`, `POST /v1/me/ai/search-assist`, 3 s timeout, nothing shown on failure). Model output is only suggestion chips the customer taps to apply; it never touches stock, money or orders (11 §1 rule 1, §3).
- **Voice search (AI-C5, new; 11 §3).** A mic in the customer search bar under `ai_voicesearch_enabled` (false); speech to text through Sarvam (`SARVAM_API_URL`) in the app language plus English, at most 15 s (the existing fixed voice recording length, so no new key); one endpoint returns the transcript to the app only, it fills the box and the search runs only when the customer taps it; audio, transcript and `ai_drafts` rows are never stored, and only the confirmed query is kept, when the search runs (as a typed query) or an ask is sent; it counts against `ai_daily_calls_customer`. Hidden when offline, signed out, the flag is off or the key is unset; a provider failure hides it for the session. A recorded offline eval runs in `make check` (11 §5, W3-12).
- **Share (01 §11.1).** "Share" on a result card and the product page: on Android a small TodayZ sheet with WhatsApp first, then More (the system sheet) and Copy link; elsewhere the system sheet. Text "{item} at {shop}, ₹{price} · TodayZ" and `https://app.todayz.in/x/{code}`, which opens C-08 with `ref=share` and needs no sign-in. The preview shows item, shop, price and locality, never a distance; it is rendered once per store item into object storage (MinIO locally) and again on a price change. No reward; `ref=share` only records `share_created` and `share_opened`, with no PII. Cards exist only for a `live` store of a `verified` business (D-40): create and read return `410 share_card_unavailable` otherwise, and creation is limited per IP or `anon_id` by `share_card_create_per_hour`. Until W3-16's cards exist, W2-08's Share shares the plain product link.
- **Site sections (01 §11.3, S-01, S-07).** A rotating hero line "Just ask and find {item}" from a curated i18n list; a "What people near you are searching for" strip built nightly per city from master products with at least `site_marquee_min_searchers` distinct signed-in searchers in `site_marquee_window_days` (at most `site_marquee_max_items`, category denylist first), shown only after an admin approves it in A-23, with the curated list before that and on any error; a comparison section "Search engines, chatbots and directories vs TodayZ" (English source keys; other languages through the i18n process). The dot-grid background and black pill CTA are optional accents inside the design system.
- **Choices beyond the spec:** a fourth distance band, "over 5 km", because "Ask this shop" ignores the radius; "Ask this shop" keeps every exclusion, not only the store limit; stores that reported a customer get none of their later asks, held in a new table `ask_store_blocks`; the 20-asks threshold is a key, `ask_low_yield_after_count` (20, min 5, max 100), like the other thresholds in 01 §17, and the cap lifts when an online order is collected or delivered (a POS sale does not count); a customer gets one push per ask, at the first Yes (in-app for every Yes), and a shop gets push + in-app with no SMS (01 §15); a cashier sends the listed price and only owner and manager change it (the 01 §3 price rule, `item.price.edit`), a cashier's Yes on an unlisted item carries no price; the booking price is the replied price per unit with no offer stacking, for at most the asked quantity (review fixes below); a Yes without a price shows "Ask the shop for the price" and books at the catalogue price; an in-store-only store still gets asks and its Yes card shows "In-store only: visit the shop" with no Book button; a search followed by an ask for the same product by the same customer within one hour counts once; badge order when one card wins two is Lowest price, Closest, Ready soonest, and the badge not shown goes to no other card; with no mode chosen Lowest price uses each store's cheaper mode, credits never count and in-store-only stores never get it; "near me" sets Nearest first; customer voice search needs sign-in (the cap is per customer); the business app gets **no voice search in v1** (B-07 add-item search stays typed; AI-R1 voice update is unchanged); voice is a new feature AI-C5 split out of AI-C2 so it has its own flag, and AI-C2 now names search assist, its own endpoint called after the results render (review fixes below); our own Android share sheet because a web app cannot reorder the system sheet; share links are a short code `/x/{code}` (10 characters, base62, random; one per store item) served by a Cloudflare Pages Function `customer/functions/x/[code].ts` (Vite middleware locally) so link previews work for a client-rendered app, not the bare `/p/:storeItemId` route; the strip is built from `search_performed` events that carry a `product_id`, counting distinct signed-in users only (anonymous devices add to totals, never to the floor); the category denylist is a fixed constant (medicines and health, personal and intimate care, tobacco, adult); the visitor picks the strip's city (no IP location); the Demand gaps and strip floors are keys with a minimum of 10 that cannot go lower; strip approval is `catalogue`, `admin` and `super_admin` (`admin.marquee.approve`) and Demand gaps are readable by `field_agent`, `catalogue`, `analyst`, `admin` and `super_admin` (`admin.demand_gaps.view`); `refine_ai_min_words` (4, min 2, max 20) and `ai_voicesearch_enabled` sit in a new consumer settings table, 11 §3.1, because 11 §2.1 is the retailer insight and coach table; a new WP W3-16 rather than spreading the asks backend over existing WPs, with the extensions listed below.
- **TODOs for the founder (traceability §6), each with the plan's default:**
  - **Q-53:** is the replied price binding for `ask_price_valid_min` or only indicative, and does it stack with offers? The plan takes binding, with no stacking (the replied price replaces any item offer on that line), at most the asked quantity, and one active booking per reply.
  - **Q-54:** should shops ever pay for asks (a lead-generation fee)? The plan has no fee.
  - **Q-55:** should WhatsApp delivery of asks to shops be on at launch? `ask_whatsapp_enabled` stays false.
  - **Q-56:** "Ready soonest" on search results needs a pickup prep time or delivery ETA, which no store has (03 `stores`). Add a prep-time setting per store, or keep the badge to ask replies? The plan keeps it ask-only.
  - **Q-57:** may a cashier change the price in a reply? The plan says no (the 01 §3 price rule).
  - **Q-58 (for the lead):** a store that already answered replies again after the ask has closed: `409 invalid_transition` or `409 ask_closed`? 01 §11.2 and 04 name both codes but no order of checks. The book tests each alone (ASK-M.30 while the ask is open, ASK-M.48 after the window).
  - "Deliver by 7" sets only the Delivery chip: v1 has no delivery-time filter (user-visible; decide whether to build one).
- **New 01 §17 keys** (eighteen with the review fix's `share_card_create_per_hour`; the lead seeds them, with min and max, in `00012_w0-03_settings_d101.sql`, the D-95 rule). Each is `key (unit, default, min, max)`: `ask_daily_limit_customer` (count, 5, 1, 50); `ask_radius_m` (meters, 3000, 500, 10000); `ask_max_stores` (count, 8, 1, 20); `ask_daily_limit_store` (count, 40, 5, 200); `ask_reply_window_min` (minutes, 15, 5, 120); `ask_price_valid_min` (minutes, 60, 15, 240); `ask_low_yield_after_count` (count, 20, 5, 100); `ask_low_yield_max_stores` (count, 3, 1, 20); `ask_retention_days` (days, 90, 30, 365); `ask_whatsapp_enabled` (bool, false); `ask_gap_min_customers` (count, 10, 10, none); `ask_gap_window_days` (days, 7, 1, 30); `site_marquee_min_searchers` (count, 10, 10, none); `site_marquee_window_days` (days, 7, 1, 30); `site_marquee_max_items` (count, 20, 5, 50); `refine_ai_min_words` (count, 4, 2, 20); `share_card_create_per_hour` (count, 30, 5, 300); `ai_voicesearch_enabled` (bool, false, in the AI table). No `bp`, `paise` or credits key is added. New fixed constants: badges per card and cards per badge (1, 1), the four distance bands, one customer push per ask, the one-hour search and ask de-dupe, the share code shape, the strip's category denylist, and the search assist model timeout (3 s). New store setting: `asks_muted`.
- **API (04):** 295 → 307 method + path pairs (+12): me +4 (`/v1/me/asks` list and create, get, close), biz +3 (`{S}/asks`, reply, report), admin +3 (demand gaps, strip read, strip approve), public +3 (share card create and read, site strip), and, after the review fixes, me +1 for `POST /v1/me/ai/search-assist` and −2 because voice search is one endpoint (`POST /v1/me/ai/voice-search`) instead of the three draft pairs counted before. Mute is a field on `PATCH {S}/settings`. Nine new problem codes: `ask_limit_reached` (429), `ask_already_open`, `ask_no_shops`, `ask_store_unavailable`, `ask_closed` (409) and `price_above_mrp` (422) in `asks.yaml`; `ask_reply_used` and `ask_price_expired` (409) in `orders.yaml` (W2-02); `share_card_unavailable` (410) in `share.yaml`. Ask create and reply are idempotent (04 §1.6). New stream events `ask.updated` (me), `ask.created` and `ask.closed` (store).
- **Work packages (07):** new **W3-16 Ask shops nearby and search proof** (Backend Architect + Frontend Developer; `internal/asks`, `internal/share`, the customer, business and admin `asks`/`demand` folders, `customer/functions/x/`, migrations 31601–31699; depends on W1-06, W1-11, W2-02, W2-05, W2-08, W2-10, W2-12 and W3-05's nav; SC-ASK), so Wave 3 has 16 builders and the W3-14 gate waits for it. Extensions: W1-04 (`asks_muted`), W1-06 (summary, badges, `confirmedAt`, fee, the chip parser and new parameters, `product_id` and chips on `search_performed`), W1-09 (hero, S-07 island, comparison), W2-02 (`askReplyId`, the `AskPricer` hook, `order_items.price_source`, `ux_orders_ask_reply_active`, `ask_reply_used` and `ask_price_expired`), W2-05 (stream events and the two push templates), W2-08 (card proof, summary, chips, share with the plain-link fallback, mic, search-assist and ask slots), W2-10 (B-04 Asks tab, B-14 toggle), W3-03 (unanswered asks in the two demand kinds through `AskDemand`), W3-05 (the A-23 nav entry), W3-07 (the Asks panels on dashboard 07) and W3-12 (AI-C5 `internal/ai/voicesearch`, one endpoint, and AI-C2 search assist, its own endpoint).
- **After D-100 landed (rebase, 2026-10-23).** Ask create and ask reply are `x-idempotency: required` and not money (a reply's price becomes money only through the existing order operations), so D-101 adds no `x-money` operation. It removes one: `confirmVoiceSearchDraft` leaves the 04 §1.6 list and apigen's `moneyOperations` together (31 → 30 ids), because voice search no longer has a draft. The count is 307: 04 §2.11 said 295 when D-100 merged (D-99 had added `POST {S}/insights/dismiss` to 294), and the tables in 04 §2 enumerate 295 pairs before D-101 and 307 after; the "294" in apigen's comments and `mk/25-contract.mk` predated D-99 and now read 307 (`CONTRACT_EXPECT` stays 0 until the last domain WP merges).
- **Review fixes (lead, 2026-10-23).** A fresh review of the first commit found these; each is now in the files above.
  - **One booking per reply:** a reply books at most one active order; unique partial index `ux_orders_ask_reply_active (ask_reply_id) where ask_reply_id is not null and status not in ('cancelled','expired','rejected')`, owned by W2-02 because it creates the column; `409 ask_reply_used`; a race lets exactly one through. `delivery_failed` keeps the reply used (the goods may still be with the rider; conservative for money).
  - **Registries:** W3-16 cannot edit the W0-03 registries, so before Wave 3 the lead adds `asks` and `share` stubs to the jobs and seed registries, the sqlc queries folders and `sqlc.yaml` entries, `cmd/todayz/routes.go`, and the authz policy files with golden tables (07 Wave 3 preamble). W1-04 seeds only `asks.manage` (owner, manager) to gate `asksMuted`; W3-16's `policy/asks.go` enforces `asks.view` and `asks.reply`.
  - **Names and chips:** `summary {storesChecked, storesWithIt, minPricePaise}` everywhere; the `offers` chip (sets `hasOffer`) is in 01, 04 and C-05.
  - **Prices on a booked line:** `order_items.price_source` (`catalogue|ask_reply`); the quote returns `validUntil`; validity is checked at placement (`409 ask_price_expired` with the fresh quote, never a silent re-price); the replied price applies per unit to the accepted quantity, and a partial accept recomputes the total; replied prices never stack with offers (this replaces min(replied, normal)), and bill-level offers, the fee and credits work as usual; the line is capped at the asked quantity, so a line has one unit price (my choice; more units are a separate normal booking); a Yes without a price books at the catalogue price with offers.
  - **Voice search stores nothing:** one endpoint, no `ai_drafts` row; the transcript goes to the app only; the confirmed query is kept when the search runs, as a typed query is (otherwise voice searches would vanish from demand data), and when an ask is sent. 11 §1 rule 11 now names voice search and search assist as the two features that are not drafts; the person still confirms every effect by tapping, and the cost guard counts both.
  - **W2-08 Share** falls back to a plain link share until W3-16's cards exist, as W1-09's S-07 strip falls back to its curated list.
  - **Retention and deletion:** `ask_recipients` and `ask_store_blocks` cascade from `asks`; a block lives as long as the ask it came from (a later report moves it to the newer ask), so `ask_retention_days`; account deletion deletes the customer's asks through W3-16's `OnAccountDelete` hook (01 §16, 03 `users`, 04 `DELETE /v1/me`, 08 AU-18 and ASK-28).
  - **AI-C2 out of search:** search never calls a model; `refineHint` tells the app it may call `POST /v1/me/ai/search-assist` after the results render; a 3 s timeout (fixed constant); on any failure nothing shows. Suggestions apply only when tapped (my choice, so nothing changes the results by itself).
  - **Share cards:** only for a live store of a verified business (D-40); `410 share_card_unavailable` on create and on read once the store is not live; `share_card_create_per_hour` (count, 30, min 5, max 300) per IP or `anon_id`.
  - **Smaller fixes:** `ask_recipients.sent_at` with an index `(store_id, sent_at)`; "ready by" up to the end of tomorrow when need-by is tomorrow; a shop can report while the ask is kept; the low-yield cap lifts only on a collected or delivered order; the strip's k floor counts distinct signed-in customers only; one response for every reason a store was left out; `cursor` and `limit` on both ask lists; W3-16's API files are `asks.yaml`, `share.yaml` and `admin-demand.yaml`, one owner per file; the 07 graph shows the `AskDemand` edges to W3-03, W3-07, W3-14 and A-23 and the W2-08 share edge.
  - **Tests:** ASK-21…ASK-28 (booking a reply twice, retention cascade, an in-store-only Yes, a Yes without a price, a share card of a store that is not live, an abandoned voice search, a search-assist failure, account deletion), and ASK-01, ASK-03, ASK-05, ASK-08, ASK-13, ASK-15, ASK-16 and ASK-19 updated.
- **Design pass needed before these are built** (`design/README.md` "Where the plan differs from the design"; `design/source/` is not edited): the Ask sheet (C-25); Ask progress and replies (C-26); the Asks inbox and reply sheet (B-20); result badges and the "Confirmed" freshness tag; the summary line, refine chips and the mic; the share sheet and the share card preview; A-23 Demand (gaps and strip approval); the site hero line, search strip and comparison section (S-01, S-07).
- Lives in: 01 §3, §4, §11.1–§11.3, §12, §13, §15, §16, §17; 02 §2.2, §2.7, §3.3, §4.2, §4.5, §5, §8; 03 (`asks`, `ask_recipients`, `ask_store_blocks`, `share_cards`, `site_demand_marquee`, `stores.asks_muted`, `orders.ask_reply_id`), §10–§13; 04 §1.4, §1.6, §2.1–§2.8, §2.11; 05 §2.6, §3, dashboard 07; 06 C-05, C-07, C-08, C-23, C-25, C-26, B-04, B-14, B-20, A-23, the admin nav table, S-01, S-07; 07 W3-16 and the extensions above; 08 §2, §3.1 (AU-18), §3.7 (ASK-01…ASK-28), E2E-21, MQ-14; 11 §1, §3, §3.1, §5; `design/README.md`; manual book ASK-ask-shops-nearby.md, MQ-11, README, traceability; `moneyOperations` and the expected-count comments in `backend/internal/devtools/apigen/` and `mk/25-contract.mk`.

**D-102 · Marketing site CORS and draft locales (lead, 2026-10-23).** From the W1-09 review.
- **CORS.** The site's lead form posts to `/v1/public/leads` from `todayz.in`. Adding the site to the shared CORS list would have given it `Allow-Credentials: true` on every route, including `/v1/auth/refresh`. The site is same-site with the API and loads a third-party script (Turnstile), so that list stays exactly app., business. and admin. (D-14). `/v1/public/*` gets a separate rule: site origins only (`https://todayz.in`, `https://www.todayz.in`, locally `https://todayz.localhost`), POST and OPTIONS, `Content-Type` and `Idempotency-Key`, and no credentials. A test checks that a preflight from the site to `/v1/auth/refresh` is refused.
- **Draft locales on the site.** Machine drafts never ship (W1-03). The production site build leaves out every locale whose catalogue is not reviewed: no pages, sitemap entries, hreflang or switcher links. Local and e2e builds keep them, with the draft banner and `noindex`. The site's catalogue lives in `shared/i18n/messages/site/` with the other apps (D-103); the site keeps its own `t()` helper, and the `@todayz/i18n` message lint scans `site/src`.
- **Lighthouse.** Mobile performance on `/hi/` and `/te/` is gated at 0.85, not 0.95, until the Indic fonts in `@todayz/tokens` are subset or weight-limited (follow-up after the exec demo). Every other page and category stays at 0.95 or higher.
- Lives in: 02 §CORS (item 7 of the middleware order); `site/CLAUDE.md`; W1-09.

**D-103 · Site catalogue in `@todayz/i18n`; quoted labels in the sentence-case check (lead, 2026-10-23).** From the W1-09 rebase.
- **Catalogue.** D-102 put the site's messages in `site/src/i18n/messages/`. The message lint resolves keys only against `shared/i18n/messages/<app>/`, so a site-local catalogue failed it with 168 errors. The site catalogue moves to `shared/i18n/messages/site/`, compiled by `make gen` like the apps; the site keeps its own `t()` helper and the draft-locale rules of D-102.
- **Sentence case.** The copy check in `shared/i18n/test/i18n.test.ts` ignores words inside curly quotes, so a quoted UI label keeps its own case (Add opening hours to appear in “Open now” searches.). Proper nouns added: English, Cloudflare, Turnstile, I, weekdays and months.
- **For W1-04.** The lead form sends one `Idempotency-Key` per set of details and a fresh `turnstileToken` on every attempt. If `/v1/public/leads` uses idempotency, the request hash leaves out `turnstileToken`, or a retry gets `422 idempotency_mismatch`.
- Lives in: D-102; `site/CLAUDE.md`; `shared/i18n/README.md`; W1-04, W1-09.

**D-104 · `/v1/events` drops bad events and counts them; aliased-provider modules validate through their roots (lead, 2026-10-23).** From the W1-07 and W1-10 merges.
- **Events.** A batch is a client's offline queue, so one bad event must not reject the rest. A well-formed envelope gets `202 {accepted, dropped}`. Each dropped event adds to `tz_events_dropped_total{reason}`, where reason is one of `unknown_event`, `server_event`, `invalid_props`, `out_of_window`, `duplicate_id` or `invalid_id`. Malformed envelopes still get 400 or 422. The catalogue checks live in code, not the OpenAPI schema, so `postEvents` stays in the contract smoke. The accepted clock window (`maxEventAge`, `maxEventFuture`) is an ingestion guard, not a product rule, so it stays a code constant.
- **Terraform.** A module that declares `configuration_aliases` cannot be validated on its own. `scripts/ci/terraform.sh` skips standalone `validate` for such modules; the env roots that pass the alias validate them. Module lock files are git-ignored; only the env roots pin providers.
- **checkov skips accepted.** CKV2_AWS_57 (no rotation Lambda; keys rotate by runbook) and CKV_AWS_157 (Single-AZ RDS, D-96).
- **Open until W1-01.** `/v1/rum` and `/v1/events` have no rate limit until the W1-01 limiter lands.
- Lives in: 05 (events); `backend/CLAUDE.md`; `infra/terraform/README.md`; W1-07, W1-10.

**D-105 · Sign-in core: Turnstile fails closed, refresh grace, If-Match, session purge (lead, 2026-10-23).** From the W1-01 review.
- **Turnstile fails closed.** 02 §8 said a missing Turnstile key means "stricter rate limits". That leaves OTP send (which costs an SMS) open to bots in production. Now staging and prod refuse to start without `TURNSTILE_API_URL` and `TURNSTILE_SECRET_KEY` (and `MSG91_OTP_TEMPLATE_ID`): the preflight names the missing variables, never their values. A deployed build that still runs without Turnstile answers OTP send with 503. Local and test use the mock's always-pass keys.
- **Refresh grace.** A refresh token rotated `RotationGrace` (30 s) ago or less is answered 401 with no revoke and no audit: it is a retry whose response was lost, or a second tab racing the first. Older than that, it is reuse and the whole family is revoked (AU-07). The grace 401 does not clear the cookie, because the other tab's refresh may already have set the newer one. The trade-off: a thief who replays a stolen token within 30 s of the owner's refresh is not detected that time; the next replay after the window is. `RotationGrace` is a security guard, not a product number, so it stays a code constant (like D-104's event window).
- **If-Match.** The common `IfMatch` parameter is optional in the schema, so a missing header reaches the handler's `428 precondition_required` instead of the validator's 422. A wrong one is still 412.
- **No `X-TZ-Business` header.** The business scope is the URL path (04 §2.6); CORS allows only `X-TZ-Client` in addition to the standard headers, and exposes the `RateLimit-*` headers.
- **A deleted user** gets `403 account_blocked` on refresh and on `/v1/me`, as a blocked one does.
- **Session purge.** `identity_purge` runs hourly. It deletes OTP requests 24 h after they expire and sessions 30 days after `coalesce(revoked_at, expires_at)`. The retention must stay at least the longest refresh lifetime (30 days), so a rotated row outlives its token and a late replay is still detected as reuse.
- Lives in: 02 §8; `backend/api/common/parameters.yaml`, `backend/api/paths/me.yaml`; `backend/internal/identity/`; W1-01.

**D-106 · Admin two-step, account deletion and session locking (lead, 2026-10-23).** From W1-01 phase B and its two reviews.
- **TOTP lockout.** Wrong authenticator codes share the OTP limit (5 attempts). At the limit, the admin's admin-app sessions are revoked and the person signs in again from the SMS step. A refused code that is right (an admin or deletion check said no) uses no attempt.
- **Rotation.** A login TOTP verify or a recovery rotates the refresh token within the same family, without extending its expiry, and sets the new cookie. A step-up verify does not rotate. Stricter than leaving the OTP-step token in place.
- **Secrets.** The TOTP secret is sealed with `TOTP_ENC_KEY`, with the AAD bound to the user id. The preflight requires the key and refuses one equal to `PII_ENC_KEY`. Recovery codes carry 80 bits (`XXXX-XXXX-XXXX-XXXX`, Crockford base32).
- **Deletion.**
  - It uses the `step_up` OTP purpose.
  - It is refused with 409 while the person:
    - is an active admin (a super_admin disables them first);
    - owns an active business, including a draft (W1-04's `OwnsActiveBusiness`, wired by the lead);
    - is limited (`customer_profiles.limited_until > now`), so delete-and-sign-up cannot reset a no-show limit.
  - Referral abuse through deletion is W2-03's.
  - Session PII, devices, TOTP and recovery codes are scrubbed in the deletion transaction.
- **Problem codes.** `code_invalid` is 422 on bearer routes, and sign-in keeps 401. `otp_expired` stays 401. Clients key on the problem code and never refresh on `otp_expired`.
- **One lock per user for sessions.**
  - Every path that rotates or revokes a user's sessions takes the users row `FOR NO KEY UPDATE` first, so they run one at a time per user. That covers refresh (and its reuse revoke), logout, logout-all, the admin TOTP flows, the lockout and deletion. Without the lock, a revocation's snapshot misses a row that a concurrent rotation inserts, and a thief looping refresh keeps a live session.
  - Sign-in takes the row `FOR SHARE` and re-checks the status, so a sign-in racing a deletion is refused or scrubbed.
  - Deletion locks only a `step_up` OTP row, so it cannot deadlock with a sign-in that holds a login row.
  - Lock order: users, then admin_users, then sessions. Nothing takes `FOR SHARE` and then upgrades it.
- **Logout-all** refuses a grace-window token with 401 and revokes nothing; the client refreshes and retries once. Like refresh, it is a cookie route that needs `X-TZ-Client`.
- **OTP-only admin token.** Before the two-step check, an admin token is accepted on exactly `/v1/rum` and `/v1/events`, so the sign-in screens can report telemetry; nothing else in the admin namespace.
- Lives in: `backend/internal/identity/`, `backend/db/queries/identity/identity.sql`, `internal/platform/httpx/mw/authn.go`; `admin/src/app/totp.ts`, `shared/auth/src/session.ts`; `docs/qa/W1-01/REPORT-phase-b.md`; W1-01, W1-08.

**D-107 · Businesses, stores and staff: W1-04 questions and review (lead, 2026-10-23).** W1-04's open questions, kept as built, and the decisions from its security review.
- **Hours.** A range that runs past midnight stays open into a holiday the next day: the shop really is open until the range ends.
- **Verified business.** It is read-only to the owner. Renaming needs a re-review flow (later). A store may be submitted before its business is verified.
- **Live store changes.** A change to a live or paused store's name, address, phone or pin sends it back to `pending_review`. A rejected or suspended business cannot change stores or KYC. It can still create document upload URLs for now; open, decide with W2-10.
- **Staff.**
  - A manager scoped to some stores cannot add a store.
  - There is no ownership transfer or leaving a business yet; acting on the owner row is refused with 403.
  - A removed, unaccepted invite is not reused; a re-invite makes a new row.
  - An invite expires after 7 days (a code constant; 01 sets no number). It never shows the invitee's name or user id before it is accepted.
- **Account deletion.**
  - `OwnsActiveBusiness` counts a draft as active, so an owner closes or deletes the draft before deleting the account (D-106).
  - Deletion withdraws the invites bound to the account and those waiting for its phone, and clears their phone.
  - It ends the account's manager and cashier memberships (`status = removed`). Owner rows are not touched: an active owner cannot delete, and a rejected business's owner row stays as a record.
- **KYC.**
  - A GSTIN needs a PAN, sent with it or already stored.
  - Sealed values are bound to their row (`field:<owner id>` as the AAD).
  - Documents are at most 10 MB, with at most 5 per kind. Confirming a document from pending to uploaded comes with B-02 in W2-10.
  - Payout details: a change to the bank account or UPI needs a fresh OTP and a notice to the owner and managers. Payouts read only `business_kyc.status = 'verified'`. This lives in the payout WP.
- **Code constants.**
  - The 24 h dedupe window on shop leads is anti-spam, not a product rule.
  - `categoryId` is checked against `categories` once W1-05 lands.
  - Presigning moves to a platform storage package with W1-05.
- **Validation order.** The validator runs before authz (422 before 403), the platform's pipeline order.
- **Seed.** Only Lakshmi Traders has staff (owner, manager, cashier). That is enough for the core-loop business journey.
- Lives in: `backend/internal/business/`, `backend/db/queries/business/`, `backend/cmd/todayz/routes_business.go`; `business/src/app/shop.ts`; `docs/qa/W1-04/REPORT.md`; W1-04.

**D-108 · Orders: cancel races, deadlines, the pickup code and deferrals (lead, 2026-10-24).** From W2-02, its open questions and its review.
- **BK-19, a customer cancel racing a shop accept.** The two serialise on the order row (`FOR UPDATE`).
  - Cancel first: the accept answers 409 `invalid_transition`.
  - Accept first: the cancel then cancels the accepted order, because a customer may cancel in `accepted` (01 §6, BK-17).
  - Either way the order ends `cancelled` and every hold is released exactly once. A cancel failing would need an If-Match precondition on the customer's cancel, and nothing gains from it: no money moves before the shop (pay at the shop). 08-testing BK-19 is reworded to match.
- **Deadlines are strict.** After `accept_by` the shop cannot accept, and after `hold_until` it cannot hand over or cancel, even before the timer has run. A cancel in that gap answers 409, and the timer then expires the order as `no_show`.
- **Hand-over** (`verify-code`) is allowed from `accepted` or `ready`; `ready` is optional (01 §6).
- **Pickup code:**
  - It is returned only on the customer's own `GET /v1/me/orders/{orderId}`, while the order is `accepted` or `ready` and the code is not locked; everywhere else it is `null`.
  - A wrong code answers 422 `code_invalid` with `attemptsLeft`. The 5th wrong try locks it: 409 `code_locked`, and the right code is refused too.
  - `new-code` replaces a locked code only.
  - `codeTries = 5` is fixed by 01 §6, not a setting.
- **Code-lockout risk flag** (01 §6, `risk_code_lockouts_store_per_day`): deferred to the risk WP. The count comes from `orders.code_locked_at` per store per IST day, so nothing more needs storing now.
- **Totals in the guard trigger:** not frozen after placing, because partial accept (BK-08–10) will change them. `ck_orders_money` keeps them consistent.
- **Deferred with W2-02:**
  - partial accept (422 for now) and confirm-partial;
  - delivery, dispatch and returns (409 for now);
  - credits and cashback (0);
  - store reliability and risk flags;
  - auto-accept;
  - admin cancel;
  - the reconcile sweep for timers River discards (runbook `docs/runbooks/orders-timers.md`);
  - `cancelled` and `no_show` notification variants (generic text for now).
- Lives in: `backend/internal/orders/`, `backend/db/migrations/20201_w2-02_orders.sql`, `backend/cmd/todayz/routes_orders.go`; `docs/qa/W2-02/REPORT.md`; W2-02.

**D-109 · If-Match survives compressing proxies; a client hang-up is 499, not 500 (lead, 2026-10-24).** From the first stack run of the core loop.
- **What broke:** Caddy's `encode` turns the ETag of a compressed response into `"v1-gzip"`. The business app sent that back as If-Match, and the contract pattern `^"v[0-9]+"$` refused it with 422. Every PATCH whose GET body is large enough to compress failed in the browser, store settings first. In production, Cloudflare weakens a compressed response's ETag to `W/"v1"`, which fails the same way.
- **Decision:** the server keeps sending the strong `"v{version}"`. If-Match also accepts `W/"v{n}"` and `"v{n}-{coding}"` (lowercase letters and digits), and only the version is compared. The version is a row version, not a byte hash, so a weak match is the right comparison. One parser, `internal/platform/httpx/ifmatch`, replaces the three domain copies; new domains use it.
- **Client hang-up:** when the client has gone (`context.Canceled` with the request context done), `problem.Write` logs at info and records 499. It used to log at error and answer 500, which counts toward the 5xx error rate and pages. A cancel of our own making, while the client is still there, is still 500.
- Lives in: `backend/api/common/parameters.yaml` (IfMatch), `backend/internal/platform/httpx/ifmatch/`, `backend/internal/platform/httpx/problem/problem.go`; 04-api.md §1.

## Saved baskets and basket search (founder, 2026-10-24)

**D-110 · Saved baskets: find a shop that has the whole list, or the best two-shop split (founder, 2026-10-24).** The founder asked for a basket feature: a customer wants several things, builds a basket, and search finds a store where the whole basket is available. References: Aurora ("AI, find anything"), the same agent behind D-101. AI-C1 (shopping list search, W3-12) already covered a one-off list. The founder's four calls change it:
- **Saved, named baskets.** A signed-in customer keeps up to `basket_max_saved` baskets (default 10) on their account, each with a name (at most 40 characters, e.g. "Monthly groceries") and up to `basket_max_items` lines (default 30). A line is a master product or free text (search normalisation, as an ask is in §11.2), with a quantity (1–99). Baskets can be edited, renamed, deleted and searched again at any time. A signed-out customer builds one unsaved basket on this device only (like the cart); signing in offers to save it. Browsing never needs sign-in (01 §2).
- **Basket search.** It uses the customer's location and radius chips (01 §11) and the same filter as search: in stock, a `live` store of a `verified` business, within the radius. A line is covered when the store lists that product (free text: the top search match in that store) with `available ≥ quantity`. Ranking: stores that cover every line first, then more lines covered, then the lower basket total (store prices, item offers through `offers.Price`), then the nearer store. It never calls a model and is read-only. Each result shows "{n} of {m} · {distance} · ₹{total}" with what is missing, the freshness label per line (01 §11.1), and at most one badge from the existing set (**Has everything**, a new label, plus **Closest** and **Lowest price**, with D-101's rules: labels only, never a change to the ranking). The summary line reads "Checked {N} shops nearby · {k} have everything".
- **No full match: best partial plus a split.** When no store covers every line, the results add one suggested split across at most `basket_split_max_shops` stores (default 2). It picks the pair that covers the most lines, then the lower combined total, then the lower combined distance; each line goes to the cheaper store of the two that cover it. An order still has one store (01 §5), so a split is two orders: "Fill cart" fills the cart for the first store; after that order is placed, its tracking page offers "Next: the rest from {shop}", which fills the cart for the second. The basket keeps the split until it is searched again. Lines no store covers show "Ask shops nearby" (01 §11.2), prefilled.
- **Fill cart.** One tap puts the covered lines, at their quantities, into the cart for that store. The cart stays one store (W2-09). Replacing a non-empty cart for another store asks first, as adding from another shop already does. Checkout reprices and rechecks stock as usual (W2-02); the basket total is only a preview.
- **Order: next, in stage 2.** A new package **W2-14 Basket search** (backend and customer) comes right after the stage-2 core, before credits, referrals and delivery. The non-AI part comes first: build a basket from search results, the product page and the shop page ("Add to basket"), save it, search it, fill the cart. AI-C1 (W3-12) changes to filling a basket from typed, pasted or spoken text: the parse is a draft the customer confirms line by line before anything is saved or searched (AI rule, 11 §1). `/v1/me/ai/list-search` becomes `/v1/me/ai/list-parse` and returns basket lines, not shops.
- **Look: Aurora's patterns, TodayZ's design system.** One "just ask" box at the top of the basket screen ("Add atta, milk, Dolo 650…"), a slowly changing row of example items (static under reduced motion), live progress "Checking {N} shops…" while the search runs, then ranked cards with badges. White page, mint wash, pastel tints, Lucide icons, Anek + Figtree, light theme only. No new visual style and no design re-sign-off, because nothing outside the design system is added.
- **Not in v1:** phone calls to shops (rejected in D-101); splits across three or more stores; delivery for a split (pickup and delivery follow each order's own rules); sharing a basket with another customer.
- **Settings (01 §17):** `basket_max_saved` (10, min 1, max 50), `basket_max_items` (30, min 5, max 100), `basket_split_max_shops` (2, min 1, max 2; 1 turns splits off), `basket_results_max` (10, min 3, max 30).
- **TODOs for the founder, each with the plan's default:**
  - **Q-59:** should a split ever be suggested when one store has everything but costs more? The plan says no: a full match always wins.
  - **Q-60:** should a saved basket remind the customer ("Your monthly groceries: 2 shops have everything")? The plan has no reminders in v1.
  - **Q-61:** may a shop see that its customers keep baskets (demand insights)? The plan keeps baskets private. Only basket searches feed "Wanted nearby" and "Missed sales", like any search (D-99).
- Lives in: 01 §11.4, §17; 03 `baskets`, `basket_lines`; 04 `/v1/me/baskets…`, `POST /v1/discover/basket-search`; 06 C-22; 07 W2-14; 08 E2E-13, BSK-01…; 11 AI-C1.

**D-111 · Basket search as built: errors, request shape, the split rule and BSK-05 (W2-14, 2026-10-24).** Choices the build had to make where D-110 and the numbered files were silent or disagreed.
- **Errors.** One basket too many is `422 basket_limit_reached` (04 already said so; 01 §11.4 and the 03 row said `validation_failed`, now fixed). Too many lines is `422 validation_failed` on `body.lines`; a bad line on `body.lines[i]` (or its `text`, `productId`). "Exactly one of `productId` and `text`" is in the contract as a `oneOf` of `required`, so the request validator refuses it first and the contract smoke never sends a line the API would refuse. The line count is a setting (`basket_max_items`, 5–100), so the contract carries only its maximum (`maxItems: 100`) and `e2e/contract/schemathesis.toml` lets `searchBasket` answer 422 to a schema-valid basket longer than today's setting, as the cursor lists do. PATCH without `If-Match` is `428 precondition_required`; a stale or unreadable one is `412` and nothing changes. A line that fails leaves the old set (one transaction).
- **Request.** `POST /v1/discover/basket-search` takes `lat`, `lng` and `radius` (metres, the same chips as discover search, default 2 km), not `pin`, `placeId` or `radiusKm`; the customer app resolves a pin or place first, as it does for search. It sits in the `discover` tag, is public and shares discover search's per-IP rate limit.
- **Response additions.** Each store carries `storeId`, `locality`, `open` and `bookable`, and each covered line its product details and `lineTotalPaise`, so the customer app can draw the cards and fill the cart without a second call. `split` is always present (`null` when there is none) and the response has `serverNow` for the freshness labels.
- **Ranking and ties.** More lines covered, then the lower total, then the nearer store, then the store id, so the order is stable.
- **BSK-05 was wrong.** It read "then G, J"; by the rule above, J (3 of 4, ₹90) comes before G (3 of 4, ₹100). 08 now reads H, F, J, G.
- **Badges.** Has everything goes to the first card when it covers every line. Lowest price (bookable stores only) and Closest are decided among the cards with the most lines covered, ties to the nearer store. A badge whose winner already shows one goes to no card (01 §11.1's rule).
- **Coverage.** `available` (on hand less reserved) must reach the quantity. A free-text line is covered by the store's best text match; an item that matched only because the text names its category (e.g. "Dairy and eggs") never covers a line.
- **Split.** Exactly D-110's rule: only when no store has everything and `basket_split_max_shops` ≥ 2; the pair with the most lines, then the lower total, then the lower distance, then the ids; each line to the cheaper store, a tie to the nearer. Both stores must get at least one line, or the pair is no split. A split is not required to beat the best single store's total: it is offered because it covers more lines.
- **Free text** is stored normalised (BSK-03), at most 120 characters after normalisation. A normalised line that comes out empty ("!!!") is `422` on its `text`.
- **`lastSplit`** is written by the customer app with a PATCH after a search (the basket keeps the split until it is searched again). It must name the basket's own line ids and is refused together with a new `lines` set; a new `lines` set clears it.
- **Logging.** One `search_log` row per line with `source = basket` (the product's id for a product line); a product id that names no product is dropped silently and logs nothing.
- Lives in: `backend/internal/baskets/`, `backend/internal/search/basket*.go`, `backend/db/migrations/21401_w2-14_baskets.sql`, `backend/api/{paths,schemas,problems}/baskets.yaml`, `customer/src/basket/`; 01 §11.4, 03, 04 §2.3, 08 BSK-05; `docs/qa/W2-14/REPORT.md`.

## Credits ledger as built (W2-01, 2026-10-24)

**D-120 · A hold locks the store's balance row too (W2-01, from the review).** `store_credit_redemptions_per_day` is counted per store, so two customers holding at one store at once could each count 199 and both pass a cap of 200. `Hold` now locks the customer's wallet, the customer's held account and the store's `store_credit` row in one statement in account-id order, the same statement `Complete` already used, so holds at one store take turns while the cap is counted. `TestConcurrentStoreCapDB`: 12 customers at one store with the cap at 11 hold together; exactly one is refused with `store_credit_cap`.
- Lives in: `backend/internal/credits/service.go` (`Hold`, `lock`), `backend/internal/credits/service_db_test.go`.

**D-121 · New ledger accounts are created in (kind, owner) order (W2-01, from the review).** A customer's or store's first use creates its accounts inside `lock()`. `Hold` (wallet, held) and `Complete` (held, wallet, store) used to create them in different orders, so two first uses could deadlock on the unique index. `lock()` sorts the accounts by (kind, owner) before creating them, then locks the balance rows in account-id order. The platform rows are never locked up front: their entries are written last in each posting, so the balance trigger takes their lock after every other one.
- Lives in: `backend/internal/credits/service.go` (`lock` and the `Service` doc comment).

**D-122 · Poster keys carry their kind's prefix; a reused key with other legs is an error (W2-01, from the review).** `Post` refuses (`ErrInvalid`) an `IdempotencyKey` that does not start with its kind's prefix (`credits.KeyPrefix`: `referral:` for `referral_reward`, `adjust:` for `admin_adjustment`, `payout:` for `payout`) or that is only the prefix, so a caller can never take one of the service's own keys (`hold:`, `recap:`, `release:`, `capture:`, `cashback:`, `expiry:`, `forfeit:`). A repeated key returns `Duplicate: true` only when the posting is the same (kind, ref type and id, and the legs compared as sorted lists, so two legs on one account never collapse into one); otherwise it returns `ErrKeyReused` and posts nothing. W2-03's `referral:<id>:<side>` already fits.
- Lives in: `backend/internal/credits/credits.go`, `backend/internal/credits/service.go` (`Post`, `samePosting`), `backend/internal/credits/fake.go`, `backend/db/queries/credits/credits.sql` (`PostedLegs`).

**D-123 · Balances and lots move only through the ledger triggers (W2-01, CR-19, from the review).** In 20101 the app role could still UPDATE `account_balances` and `credit_lots`, so a bug could move a cached balance or a lot without an entry. Migration 20102 makes the two apply-trigger functions `SECURITY DEFINER` (their `search_path` is already fixed) and leaves `todayz_app` only column grants: INSERT (`account_id`, `kind`, `updated_at`) and UPDATE (`updated_at`) on `account_balances` (the UPDATE is there because `SELECT … FOR UPDATE` needs one), and UPDATE (`expired_at`, `reminded_at`) on `credit_lots` for the jobs. A balance row's `kind` must be its account's: a composite foreign key `(account_id, kind) → ledger_accounts (id, kind)` (with a unique on `ledger_accounts (id, kind)`), so a row cannot claim another kind's rules. `TestAppendOnlyDB` expects 42501 on a direct UPDATE of a balance or kind, an INSERT with a balance, a DELETE of a consumption, and an UPDATE of a lot's `remaining`, `held` or `original`, and 23503 on a balance row of another kind. CR-19 means exactly this: no direct UPDATE of balances or lots. The app role can still INSERT a `credit_lots` row with no consumption behind it (the service needs that INSERT); forging one takes arbitrary SQL as `todayz_app`, and the nightly `ledger_reconcile` reports it (`lots_vs_balances`).
- Lives in: `backend/db/migrations/20102_w2-01_ledger_grants.sql`, `backend/db/migrations/20101_w2-01_credits.sql` (header), `backend/internal/credits/service_db_test.go`.

**D-124 · An order pays the cashback rate it was placed at (W2-01, founder's conservative call; the lead approved the orders change).** Collection used to pay the store's `cashback_bp` at that moment, so a shop changing its rate while an order was open changed what the customer earned. Orders now snapshot the rate: `orders.cashback_bp` (0–2000, default 0) is set from the store row read in the same transaction that places the order, and `collect()` passes it to `Credits.Complete`; `credits.Completion.CashbackBP` (a pointer: nil takes the store's rate now, which is what a POS sale wants) is the rate paid, and one out of range is `ErrInvalid`. The migration is `20202_w2-02_order_cashback_bp.sql`, in orders' block, because a 201xx file may not alter `orders` (D-90); the lead approved editing the orders files, since W2-02 is closed. Orders placed before the migration are backfilled from their store's rate at migration time (no NULL meaning "read the store"), so Complete always uses the stored rate. The default 0 means a writer that does not know the column pays no cashback rather than an unshown rate. `TestCollectPaysPlacedCashbackHTTP` places at 5%, moves the store to 10% and collects 5%.
- Lives in: `backend/db/migrations/20202_w2-02_order_cashback_bp.sql`, `backend/db/queries/orders/orders.sql` (`InsertOrder`), `backend/internal/orders/{create,transitions,seams}.go`, `backend/internal/jobs/credits.go`, `backend/internal/credits/{credits,service,fake}.go`; 01 §7, 03 `orders`.

**D-125 · The business credit account shows only the member's stores (W2-01, founder's conservative call).** `GET /v1/biz/businesses/{bid}/credit-account` stays owner and manager (`credit_account.view`). A manager limited to some stores sees only those stores' balances and lines; an owner or a manager of every store sees all. The handler takes the stores from `authz.StaffResolver.Membership`; a membership that is gone answers 404. `TestCreditAccountHTTP` covers the owner, a manager of every store, a store-limited manager, a cashier (403) and another business's owner (403 or 404).
- Lives in: `backend/internal/credits/handlers.go`, `backend/internal/credits/reads.go`, `backend/db/queries/credits/credits.sql` (`BusinessStoreBalances`, `StoreLines`), `backend/cmd/todayz/routes_credits.go`, `backend/cmd/todayz/credits_http_test.go`.

**D-126 · W2-01's other open questions take the conservative defaults (founder, 24 Oct).**
- **Deleting an account with held credits** is refused with 409 `conflict` (`ErrHeld`); the hold is not released first. Orders already refuses a deletion with an unfinished order, so in practice this is a W2-04 bill-claim hold.
- **Risk flags from cap refusals:** at most one `risk_flags` row per subject, rule and IST day, best effort. A customer retrying at a cap does not add more rows that day (`InsertRiskFlag` inserts only when no flag for that subject and rule exists since the IST day start; the NOT EXISTS is not atomic, so two refusals at the same instant can write two). `TestCapsDB` checks a second refusal adds none.
- **A risk flag never holds up an order (from the second review).** The flag is written on the pool so it survives the caller's rollback, but `Hold` runs in the order's transaction and keeps the store's row locked until it commits. With the pool full of orders waiting on that lock, a flag waiting for a connection would stall them all. The insert has its own 2 s timeout (`flagTimeout`); a timeout or an error is logged (rule only, no customer data) and counted in `tz_credits_risk_flag_failures_total`, and the refusal stands. `TestFlagNeverStallsHoldDB` holds the only connection of a one-connection pool and checks `Hold` still answers the refusal promptly. The API pool size is a platform follow-up.
- **A short hold at create** (the balance dropped between the quote and the place) answers `409 price_changed` with the re-priced quote. There is no separate `no_balance` problem.
- **No business-side "credits earned" notice** yet; only the customer's `credits_earned` is sent.
- Lives in: `backend/internal/credits/service.go` (`OnAccountDelete`, `flag`, `flagTimeout`), `backend/internal/credits/jobs.go` (the metric), `backend/db/queries/credits/credits.sql`, `backend/internal/orders/create.go`.

**D-127 · An order's end always releases its credits (W2-01, from the review).** `end()` in orders calls `Credits.Release` for every end state, not only when `credits_used > 0`: a hold may exist whatever the order row says, and `Release` without an open hold does nothing. `TestEndReleasesHoldHTTP` forces `credits_used` to 0 on an order with a real hold and checks a customer cancel, the accept timeout and a no-show each release it; `TestCollectCapturesHoldHTTP` checks collection captures it.
- **Delivery hand-off.** No Go code moves an order to `delivered` or `delivery_failed` yet: W2-02 deferred delivery (BK-23–25 and the D-69 returns), and 07 gives delivery to W2-02 with no later WP. Until it is built, nothing captures or releases a delivery order's hold or pays its cashback. Whoever builds W2-02's delivery transitions must call `Credits.Complete` (with the order's `cashback_bp`) on `delivered` and `Credits.Release` on `delivery_failed`; the note is on the `Credits` seam in `orders/seams.go`.
- Lives in: `backend/internal/orders/transitions.go`, `backend/internal/orders/seams.go`, `backend/cmd/todayz/credits_http_test.go`.

## Reviews and ratings as built (W2-06, 2026-10-24)

**D-140 · Reviews and ratings as built (W2-06).** Choices the build had to make where 01 §10, 03 and 04 were silent.
- **Single writer.** `reviews` is the only code that writes `stores.rating_avg` and `rating_count` (D-59). The rating is the Bayesian average of the visible reviews, prior 4.0 with weight 5, computed in SQL and rounded to 2 places (`round`, half away from zero); `BayesAvg` in Go is the test oracle. Both numbers are fixed constants in code (D-92). Every review write recomputes its store inside the same transaction, under a per-store advisory lock taken after the row lock; the hourly `store_rating` job (rollup class) heals any drift.
- **Eligibility.** The order must be the caller's and `collected` or `delivered`. A POS sale needs the W2-04 `SaleSource`; until it lands, a `posSaleId` is not eligible (`422`). One review per order or sale (unique index); a second is `409`. Someone else's order, review or report is `404`, never `403`.
- **Edit window.** An edit is allowed while `now <= created_at + review_edit_window_days`; after that `409 review_edit_window_passed`. Stars, text and tags can change; the order cannot. A hidden review cannot be edited.
- **Reply.** One reply per review, editable, by an owner or manager of the store (cashiers do not see reviews). A reply is published at once.
- **Reports.** A customer may report a review that is not their own (`422` for their own); a manager may report a review of their store. One report per reporter per review; a duplicate returns `201` with the first report. A report never hides anything on its own.
- **Moderation.** Hide and restore (of a review or of its reply) need a reason, are soft deletes (`hidden_at`), write an audit row in the same transaction and recalculate the rating. `support`, `admin` and `super_admin` can hide and restore (D-41). Hiding twice or restoring a visible one is `409 invalid_transition`. Hiding a review marks its open reports `upheld`. Resolving a report as `upheld` does not hide the review by itself; hiding is the separate act.
- **Public list.** `GET /v1/discover/stores/{slug}/reviews` shows visible reviews only, newest first, with the customer's first name. A hidden reply is dropped from a visible review.
- **Added beyond 04.** `GET /v1/me/reviews` (my reviews), `POST /v1/admin/reviews/{id}/reply/hide` and `.../reply/restore`, and the admin review endpoints, pulled forward from W3-05 and mounted from `routes_reviews.go` so the moderation flow is testable end to end. W3-05 builds the console screens on them.
- **Not built.** The "New review" in-app notification (01 §10 table) waits for the W2-05 outbox consumer; nothing sends it yet.
- Lives in: `backend/internal/reviews/`, `backend/internal/jobs/reviews.go`, `backend/cmd/todayz/routes_reviews.go`, `backend/db/migrations/20601_w2-06_reviews.sql`, `backend/api/{paths,schemas,problems}/{reviews,admin-reviews}.yaml`, `authz/policy/reviews.go`; 01 §10, 03, 04 §2.3, §2.4, §2.6; `docs/qa/W2-06/REPORT.md`.

**D-141 · W2-06's open questions take the conservative defaults (lead, 24 Oct, following the founder's call in D-155).** As built:
- A review can be edited while `now <= created_at + review_edit_window_days`.
- A deleted customer's reviews stay, showing the first name only.
- Cashiers cannot see or reply to reviews.
- Upholding a report does not hide the review; hiding is a separate action, and it upholds open reports.
- A duplicate report returns `201` with the first report.
- Reporting your own review is `422`.
- Hiding or restoring twice is `409 invalid_transition`.
- The "New review" notification waits for the W2-05 consumer.

## Offers, discounts and banners as built (W2-07, 2026-10-24)

**D-150 · Offer pricing as built: kinds, rounding, caps and what is immutable (W2-07).** Choices the build had to make where 01 §12 was silent.

- **Flat is per unit.** A "₹ off" offer takes its value off each unit of a covered line, never off the line (OFFER-M.04: ₹2 off milk ×2 is ₹4). Buy X get Y gives `get_qty` free for every complete group of `buy_qty + get_qty` units of the same item (OFFER-M.05: buy 2 get 1 on ×3 pays for 2, ×6 pays for 4).
- **Percent floors in paise** (₹58 at 10% off takes 580 paise; 33% of 100 paise takes 33) and is capped at 90, so a typo cannot give an item away. A discount never exceeds its line and a bill never goes below ₹1 (01 §12). A flat discount that would exceed the unit price takes the whole unit price.
- **Terms are immutable after creation.** `PATCH` changes only status, title, channels, limits and dates. A change of kind, value or scope is a new offer, so an order's `offers_applied` and the shop's redemption counts always describe terms that existed.
- **`startsAt` is clamped to now** when it is in the past; a later start makes the offer `scheduled`.
- **The clock decides, the label follows.** Pricing reads `starts_at` and `ends_at` against the injected clock; the `offers_tidy` job (every minute) only moves the status label (scheduled to live, live to ended, spent to ended) and writes the catalogue-change record for each change. A tidy outage never leaves a stale price.
- **Pricing is channel-agnostic.** `offers.Price(Cart)` takes a channel (`online` or `pos`); the quote and the POS call the same function, so OFFER-M.03's "same price online and at the POS" holds by construction.
- Lives in: `backend/internal/offers/{pricing,engine,service}.go`, `01 §12`, `docs/qa/W2-07/REPORT.md`.

**D-151 · Redemption is counted in the order's transaction, per customer under a lock (W2-07).** `Redeem` claims the offers an order used in offer-id order inside the order's own transaction (after the store lock the order already holds, so the platform's lock order stands). It is idempotent per order, returns `ErrUnavailable` when `max_redemptions` ran out between the quote and the place (orders turns that into `409 price_changed` with the new quote), and counts a customer's earlier unreleased uses under the same row lock, so a per-customer limit cannot be beaten by two orders at once. `Release` (cancel, reject, expiry) gives the redemption back by marking it released. An offer with a per-customer limit does not apply to a caller with no known customer.

**D-152 · Banners: held uploads, the cap and taps (W2-07).**

- **Template banners are ours and show at once** (`auto_ok`, live or scheduled). An uploaded `imageKey` banner is `pending_review` with moderation `needs_review` until an admin approves it (no AI image check exists yet; when it does, it moves a banner to `auto_ok` the same way).
- **`store_banners_max` counts live, scheduled and pending_review banners**, checked under the store row lock, so a shop cannot queue five uploads behind a cap of three and have all go live. Paused, ended and removed banners do not count.
- **Taps: `banner_taps`.** One row per customer and banner holding the last tap (`POST /v1/me/banners/{bannerId}/tap`, 204). Attribution is D-77's: the last click, same store, within 24 hours. The daily counts (`banner_daily`) are filled by W3-07 from `banner_taps` and the orders.
- **Admin takedown** is `banners.Service.Remove(bannerID, adminID, reason)`; W3-05 calls it. A removed banner never returns.
- Lives in: `backend/internal/banners/`, `backend/db/migrations/20702_w2-07_banners.sql`, 03 §9.

**D-153 · The nearby offers feed (W2-07).** `GET /v1/discover/offers` returns at most 10 cards (`feedCards`) from live stores inside the customer's radius (default 2 km, the same chips as search; at most 200 candidate stores). A store shows one card: a banner when it has a live one (its banners take turns by rotation bucket), otherwise its best live offer; a store with neither shows nothing. Cards go nearest first, and the list starts at a different shop every 15 minutes (`feedRotation`; the bucket comes from the injected clock), so the top slots move round the shops in range. Each banner that makes the screen emits `banner_impression`. The ranking inside the 01 §12 "ranked by distance and discount" is therefore distance first; the discount only picks the store's best offer. `has_offer` on a store card means the store has a live item-level or bill offer. The constants are code constants in `internal/offers/feed.go`, not settings, until the founder asks to tune them (TODO(lead) in the report).

**D-154 · Seams between offers, orders, search and banners (W2-07).** The orders package owns `Pricer` and `Redeemer` (`internal/orders/seams.go`) and basket search owns `search.Pricer`; the offers service satisfies both through adapters in `cmd/todayz/offers_seams.go`, so no domain imports another. `NoRedeem` and the `Fixed` flag (a line whose price is already final, such as a flash price) let tests and the POS skip redemption. The offers feed takes banners through `offers.BannerSource`, adapted from `banners.LiveBanner`.

**D-155 · W2-07's open questions take the conservative readings (founder, 24 Oct).** The founder accepted what W2-07 built for each question in `docs/qa/W2-07/REPORT.md`:
- A percent offer is at most 90% (`MaxPercent`), and its discount floors to the paisa.
- A flat offer is per unit: `min(value, unit price) × qty`.
- A `bill_min` offer is whole-shop only.
- An offer with a per-customer limit does not apply when no customer is known.
- A flash offer applies to the whole item, not only the batch's units.
- Offer terms cannot change after creation; only the status changes (pause, resume, end). `startsAt` is clamped to now.
- The nearby feed shows 10 cards, rotates every 15 minutes, uses a 2 km default radius and looks at 200 candidate stores. These stay code constants (D-153).
- Uploaded banner images wait in the review queue until an AI image check exists, and a held banner counts toward `store_banners_max` (D-152).
- Banner taps are kept in `banner_taps`; the tap route is `/v1/me/banners/{bannerId}/tap`.
- No feature flag gates offers or banners.

Still open as work, not decisions: `has_offer` in search must agree with `offers.Price`, and the `banner_limit_reached` problem needs its i18n key.

## POS backend and sync as built (W2-04, 2026-10-24)

**D-130 · POS, sync and receipts as built: prices, times, links and the cursor (W2-04, 2026-10-24).** Choices the build had to make where 01, 02 §4.3, 04 and D-45/D-84 were silent or left a detail open.
- **Prices.** An online sale (`POST {S}/pos/sales`) refuses a catalogued line whose `unitPricePaise` is not the item's price now: `409 price_changed` with `storeItemId` and `pricePaise`, so the POS can redraw the line. `expectedTotalPaise`, when sent, must equal the server's total (`409 price_changed` with `totalPaise`). An offline sale at sync is never refused or repriced: the device's prices, discounts and offers stand (02 §4.3). A line without a `storeItemId` (or, offline, with one that is not this store's) is a custom line: it is sold and moves no stock. Online, another store's item is `422` on its field.
- **Times.** An online sale's `soldAt` is the server's time. An offline sale's `sold_at` is the device's `at`, and `received_at` is the server's time. D-131 clamps `sold_at` to between 72 h before `received_at` and `received_at`.
- **Stock.** Each catalogued line is one `sale_pos` movement (idempotent on the line's client id); a reversal adds one `pos_reversal` per movement. Offline damage and expiry are not capped at on hand; stock may go below zero, and an item below zero comes back in the sync changes with `countNeeded: true`. A count equal to on hand writes nothing.
- **Roles per entry.** Sync is `pos.sync` (cashier and above), and each entry is then checked as its own action: a sale is `pos.sell`, a sale carrying a manual or bill discount also needs `pos.discount`, a count is `stock.count`, a restock `stock.restock`, and damage and expired are `stock.damage` (the catalogue's own actions, so the cashier may count but not restock or write off). A refused entry is `rejected: forbidden` and the rest of the batch still applies.
- **Order and replays.** `deviceSeq` must rise strictly within a batch, or the whole batch is `422` on `body.entries[i].deviceSeq`. Across batches, idempotency is by entry `clientId` per store, so any device replaying any outbox gets `duplicate` with the original `saleId`/`movementId`. A sale rung online and then sent again by the outbox is a `duplicate` of the online sale. A duplicate never repeats the receipt token.
- **Cursor.** The cursor is the store's catalogue seq. `410 resync_required` when the cursor is ahead of the store, when the change it names is older than 30 days (D-36's retention), or when the device has not synced for 30 days. The 30 days is a constant, because no setting exists.
- **Bill QR.** The token is 64 base64url characters: the claim id, a stored nonce and an HMAC (`BILL_QR_SIGNING_KEY`) over id, nonce, store, amount and expiry. A token that does not verify is `404`, the same as an unknown one. It can be confirmed until 120 s (at exactly 120 s it is `claim_expired`). The first confirm wins (`FOR UPDATE`); the second is `claim_already_used`. Credits are held at confirm through the Credits seam. Under `credit_min_order_value_paise` the preview says `below_credit_minimum` and the confirm takes 0. The sale that uses a claim must name the same `saleClientId` and the same eligible amount (`claim_mismatch`); a shown but unconfirmed claim is `409 conflict`. A confirmed claim stays usable for its sale after the 120 s (Q in the W2-04 report). Each change publishes `bill_claim.updated` on the store channel.
- **Counter code.** TOTP per device (RFC 6238, SHA-1, 60 s, 4 digits), sealed under `DEVICE_SECRET_ENC_KEY`, checked at the server's step ±1. A (device, step) is accepted once, so the same code on another bill is wrong. A phone with no account counts as a wrong code. Five wrong codes on a bill lock it (`code_locked`, then even the right code is refused). D-131 adds locks per phone and per cashier across bills. Offline, the code is checked at sync at the device time less `clockOffsetMs`. It fails, while the sale still applies, when the offset is over 2 min, the code is older than `pos_offline_link_max_hours`, it is more than 60 s in the server's future, or it was used before. More than `pos_offline_links_risk_per_day` offline links in a store's IST day raises one `pos.offline_links` risk flag a day.
- **Rewards.** Cashback is `stores.cashback_bp` of the paid amount (the total less round-off), in whole credits rounded down, only on a `bill_qr` or `counter_code` link. A referral qualifies at `ref_qualifying_min_order_paise` or more. A reversal reverses both through the seams. `phone_unverified` records the phone (sealed and hashed), is `pending_verification`, earns nothing and is not in purchases.
- **Receipts and purchases.** A receipt token is 22 characters, stored hashed, valid 30 days (D-45), answered with `Cache-Control: no-store` and no customer data. `/v1/me/purchases` lists the caller's orders and linked (`bill_qr`, `counter_code`) POS sales, newest first, by an opaque cursor.
- **Seams.** Credits (W2-01), referrals and the Pricer (W2-07) are interfaces in `internal/pos/seams.go` with do-nothing defaults: no offers, no credits (`not_available`), no referrals. The real ones are wired in `newPos` (`cmd/todayz/routes_pos.go`).
- **Offers at the counter (W2-07).** An online sale is priced by `offers.Service` on `ChannelPOS` as a walk-in (no customer, so per-customer limits do not apply at the counter). The applied offers are redeemed with `Ref{POSSaleID}` inside the sale's transaction; an offer that ran out between pricing and the write is `409 price_changed` and the POS re-sends. A reversal releases the redemptions. An offline sale at sync carries the device's discounts with no offer ids, so nothing is redeemed and it does not count against an offer's limit. Custom lines are not sent to the pricer, so they do not count toward a minimum-bill offer.
- **Reviews of a sale (W2-06).** `reviews.SaleSource` is `pos.Service.SaleRef`: store, customer and link method only when the link earns rewards (`bill_qr` or `counter_code`, linked), and completed unless reversed. Reviews answers 404 for a `phone_unverified` sale, since it has no reviewing customer.
- **Cross-package FKs.** `reviews.pos_sale_id`, `offer_redemptions.pos_sale_id` and `credit_holds.bill_claim_id` get their FKs in the Wave 2 constraints migration (D-79), not in 204xx: a 204xx file sorts before 20601 and 20701, so on a fresh database it would run before those tables exist.
- Lives in: `backend/internal/pos/`, `backend/internal/jobs/pos.go` (`PosStock`), `backend/db/migrations/20401_w2-04_pos.sql`, `backend/api/{paths,schemas,problems}/{pos,sync,receipts}.yaml`, `backend/cmd/todayz/routes_pos.go`; `docs/qa/W2-04/REPORT.md`.

**D-131 · POS review fixes: code locks, times, guard tables and the claim lifecycle (W2-04, 2026-10-24).** The lead's W2-04 review. Where a choice was needed, the conservative reading.
- **Counter-code brute force across bills.** The 5-per-bill lock (D-130) is reset by a new `saleClientId`, so wrong codes are also counted in `counter_code_failures`. 5 wrong codes for one phone at one store in 15 minutes lock that phone at that store for 15 minutes, for every cashier. 20 wrong codes by one cashier in an hour lock that cashier's code checks for an hour, on any phone. A locked check answers `code_locked`, the right code included. The constants sit next to `codeTries` in `internal/pos/sale.go`; they are fixed values, not settings. The phone is counted by its keyed hash (`PII_HASH_KEY`), never stored.
- **Offline codes use the same counters.** At sync, a wrong or reused offline code counts as a failure at the sync's time (`source = 'sync'`). Once the phone or the cashier is locked, the sync's remaining counter-code links fail, the right code included; the sales still apply. An entry refused for its clock (offset over 2 min, older than `pos_offline_link_max_hours`, typed in the future) is not a code failure. A rejected sync entry rolls back with its savepoint, its failure included; no link result leaks from it, so that is accepted.
- **Lock order.** A code check takes transaction advisory locks: the cashier's first, then the phones' (per store and across stores), sorted. A sync takes them all once, at its start (round 2): it collects every counter-code phone in the batch, canonicalises it, and locks the cashier key then the phone keys in sorted order, deduplicated. The locks are re-entrant, so the per-entry checks after that take nothing new, and two cashiers syncing the same phones in opposite orders wait for each other instead of deadlocking.
- **`sold_at`.** An offline sale's `sold_at` is at most `received_at` (a device clock ahead cannot sell in the future) and at least 72 h before it (01 §17, "sold_at clamp for offline sales"; 01 names the number but not the direction, TODO(lead)). The reversal window (`pos_reversal_window_hours`) runs from `received_at` for an offline sale and from `sold_at` (the server's time) for an online one, so a wrong device clock never stretches it. 03 §5 says "of `sold_at`" and needs this note.
- **Guard tables.** `todayz_app` loses `DELETE` and `TRUNCATE` on `counter_code_checks` and `counter_code_uses`, and `UPDATE`, `DELETE` and `TRUNCATE` on `counter_code_failures`, `sync_entries` and `receipt_tokens`. Every W2-04 table but `sync_cursors` (a row per device the app updates) has a `no_truncate` trigger, so the owner cannot truncate them either. 20401 was edited in place: nothing outside this branch has applied it.
- **Offline offer discounts.** An offline line's `offerDiscountPaise` has no offer id, so it is kept as sold. At sync the catalogued lines are priced by the offers engine at the clamped `sold_at` (as a walk-in, in a savepoint, nothing redeemed). A sale is *unmatched* (`pos_sales.offline_offer_unmatched`) when some line's offer discount is more than the engine gives that line; a custom line's engine discount is 0, and an engine error counts as unmatched (logged with the store id only). The day's risk count is offline sales that are linked **or** unmatched, each sale counted once (round 2), against `pos_offline_links_risk_per_day` (the same `pos.offline_links` flag). When the cashier syncing an unmatched sale lacks `pos.discount`, the store also gets a `pos.offline_offer_without_discount_role` flag (once a day; score 50 as for `pos.offline_links`, TODO(lead): the plan names no score). A normal store-offer sale matches and raises nothing. The match is per line: an engine bill-level discount is not spread over lines (TODO(lead)). The sale is never refused.
- **Bill QR.** A sale may bind a confirmed claim only before its `expires_at` (120 s after the QR was made); after that it is `claim_expired`, and the claim reads `expired`. A retry with the same `clientId` under a new `Idempotency-Key`, even at the same time as the first, gets the same sale back (a replay), not `409`; the hold is captured once.
- **Credit holds.** A new QR for a bill cancels the bill's earlier live claims and releases their holds in its transaction. The bill's sale, whatever its link, cancels the bill's other live claims and releases their holds. The timer `pos_bill_claim_expiry` (every minute, 500 a page and every due claim per run (round 3); one claim per transaction since D-132, taken `FOR UPDATE SKIP LOCKED`) expires claims past `expires_at` and releases the holds of confirmed ones (runbook `docs/runbooks/pos-bill-claims.md`). The Credits seam's contract: `Capture` fails on a released hold, and `Release` is a no-op without a hold or the second time. The routes and the worker build the seams from one constructor, `jobs.PosDeps` (the API's `posDeps` adds the offers adapter on top), so the sweep releases holds through the same Credits the routes hold them with; `TestPOSDepsShared` checks it. W2-01 and W2-03 wire theirs in `jobs.PosDeps` (done in D-132). Each due claim gets its own transaction (a savepoint in round 2, D-132): a `Release` that fails rolls back that claim only, is logged with the claim id, counted in `tz_pos_claim_release_failed_total` and in the run's `failed`, and stays live for the next run; the rows behind it still expire.
- **Smaller fixes.** A manual discount larger than what the offer left on the line is refused (`422`, `manualDiscountPaise: range`), never cut down. A verified counter-code check binds a sale for 2 minutes only; after that it is `code_invalid`. `Stock.Reverse` errors (item gone, stock invalid) are `409 invalid_transition`, not a 500. Re-confirming a claim, by the same customer too, is `claim_already_used`.
- **Cross-store phone ceiling (round 2).** Besides the per-store budget, a phone with `pos_code_phone_failures_per_hour` (default 10, 01 §17) or more wrong codes in the last hour across all stores is refused at every store (`code_locked`), counted from `counter_code_failures` by `phone_hash` (index `ix_counter_code_failures_phone_any`). Phones are canonicalised before hashing and locking (spaces, `-`, `.`, brackets, a `+91`, `91` or `0` prefix), so format variants of one number share a key and a count.
- **Code check order (round 2).** `VerifyCode` reads the locks, then the bill's check: a check already locked is `code_locked`; a fresh verified check answers again only when the body's phone is the verified customer's (another phone falls through to a new check); a locked phone or cashier is then `code_locked` before any `counter_code_checks` row is written.
- **Round 3.** A sync batch with any counter-code entry that carries a code takes the cashier's lock at its start even when no phone in it parses, so the cashier lock always comes before stock row locks (the contract's phone pattern keeps an unparseable phone out over HTTP; this is defence in depth). The sweep pages through the claims due at its `now` by the keyset `(expires_at, id)`, 500 a page and one transaction a page, until the due set is done or the job's 30 s timeout ends, so failing claims never hold back the ones behind them; no schema change (the partial index on `expires_at` serves it). Alert `tz-pos-claim-release-failed` (`infra/grafana/alerts/pos.yaml`, critical): `tz_pos_claim_release_failed_total` rose in 10 min, for 5 min, since the run itself still succeeds and Job failing does not fire. A wrong code from another phone against a fresh verified check counts for the phone and the cashier but not toward that check's 5 attempts, so it cannot lock a verified bill.
- **Open items.** A replayed sale has no receipt token (the token is shown once and stored hashed). `counter_code_checks`, `counter_code_uses` and `counter_code_failures` keep `phone_hash` with no retention job (DELETE is revoked from the app); the report names the owner. The report lists both.
- Lives in: `backend/internal/pos/{sale,totp,sync,claims,seams}.go`, `backend/internal/jobs/pos.go`, `backend/db/migrations/20401_w2-04_pos.sql`, `docs/runbooks/pos-bill-claims.md`, `infra/grafana/alerts/pos.yaml`; `backend/cmd/todayz/routes_pos.go`; tests in `backend/cmd/todayz/pos_review_http_test.go` and `pos_round2_http_test.go`; `docs/qa/W2-04/REPORT.md`.

**D-132 · POS on the real credits ledger and referrals (W2-04 × W2-01 × W2-03, 2026-10-24). Changes W2-01's package.** `jobs.PosDeps` wires `credits.Service` and `referrals.Service` in place of `pos.NoCredits` and `pos.NoReferrals`; the API (`routes_pos.go`) and the worker (`jobs.PosJobDeps`, the sweep) build them from the same constructors, so the sweep releases holds through the ledger the routes hold them with.
- **Changes to `internal/credits` (W2-01's package, approved by the lead).** A new ref type `pos_sale` (`credits.RefPOSSale`): a counter sale's cashback and reversal; it never holds, since a bill QR sale spends its bill claim's hold. `Completion.RefuseReleased` (opt-in, off by default, so orders' `Complete` is unchanged): on a released hold `Complete` answers `ErrReleased` and posts nothing, cashback included. `Reverse(Reversal{Ref, Spend, CustomerID, StoreID})` follows D-08/D-74: the captured spend goes back to its original lots, the part of a lot whose `expires_at` has passed goes to `platform_expired`; the cashback posted on `Ref` is clawed back up to the customer's spendable balance and the store absorbs the shortfall; the store receivable is reversed. Keys `reverse:<ref>` and `clawback:<ref>`: a second call is a no-op returning the first result. Lock order as `Complete`: the accounts sorted, then the hold. A sale with no credits and no cashback is a clean no-op. `Complete` on a reversed ref is `ErrReversed`, so a reversal that commits first can never be followed by a capture or cashback on that sale.
- **Edges refused rather than guessed (the lead's answers, review round 4).** A spend whose hold is still `held` (not captured) is `ErrHoldOpen` (Q28): release the hold or complete the purchase first; a completed POS sale always has its hold captured in its own transaction, so this only guards a misuse. A ref reversed before it completed, an order as much as a sale, is refused by a later `Complete` (Q29): `Reverse` always posts the `reverse:<ref>` marker, with no entries when there is nothing to give back, so `Complete` answers `ErrReversed`. This changed one existing credits assertion: `TestReverseEdgesDB` expected no reversal transaction for an empty reverse and now expects the one empty marker. There is no partial reversal (Q30): `Reverse` takes the whole purchase, and the POS reversal is whole-sale only (D-130). Q27 stays open.
- **The shortfall is counted.** When the customer has already spent the cashback, the clawback takes what is spendable and the store absorbs the rest; `Reversed.Shortfall` comes back through the Credits seam (`ReverseSale` returns it) and, after the reversal commits, POS logs it at warn level (sale, store and customer ids, `shortfall_paise` only) and counts it in `tz_credits_reverse_shortfall_total`.
- **One credit is `money.PaisePerCredit` (100, ₹1, 01 §7)**, a fixed unit shared by pos, credits and referrals instead of three literals.
- **The sale's transaction.** In order: the claim row `FOR UPDATE`, the sale rows, `Capture` (Complete with `CashbackBP` 0 and `RefuseReleased`, on `bill_claim`), the cashback (Complete on `pos_sale` with `EligiblePaise` = paid before round-off, at the store's rate now, 01 §7), then `referrals.OnPurchaseCompleted` with `RefPOSSale`, the same place orders' collect calls it. Referral rewards never post inside the sale: they post after commit (D-169). A capture of a released hold maps to `pos.ErrHoldReleased` and the sale answers 409 `claim_expired` (it was 500), and the sale's transaction rolls back: no sale, no capture, no cashback.
- **Referrals get every linked ledger sale.** POS no longer drops sales under `ref_qualifying_min_order_paise`: referrals applies its own minimum (D-164, D-165) on `EligiblePaise` = paid + credits used × 100. A reversal calls `OnPurchaseReversed(pos_sale, id)` (D-166).
- **Lock order and deadlocks.** The order is pos rows first (claim `FOR UPDATE`, the sale lock; the sweep takes one claim at a time `FOR UPDATE SKIP LOCKED`), then ledger accounts (sorted inside each credits call, D-121), then `credit_holds`. This is the reverse of "ledger accounts, then pos rows" in the brief; it is the order every path already takes, and no path takes a ledger lock before a pos row. The sweep holds its page's claims and locks one customer's accounts at a time; a sale holds one claim (which the sweep skips) and its customer's and store's accounts; the sale's `CancelBillClaims` finds no other live claim of the bill, since a new claim cancels the old one when it is made. Every path keeps this order inside a transaction (the lead confirmed it, Q31). Across transactions it is not enough: each `Release` and `Complete` holds its accounts until its transaction commits, so a transaction that runs several of them in turn holds earlier customers' locks while it asks for the next. Review round 4 found one: a sweep page released up to 500 claims in one transaction (holding C1, wanting C2) while a sync batch of [C2, C1] held C2 and wanted C1, and the whole sync failed. Two fixes: the sweep reads the due page without locks and expires and releases each claim in its own transaction (`LockDueBillClaim`, `FOR UPDATE SKIP LOCKED`), so it never holds more than one customer's accounts; and the sync, which must stay one transaction for its batch, is retried whole on `40P01` or `40001` (up to 3 attempts, 20–40 ms then 40–60 ms of jittered backoff between them, `tz_pos_sync_retried_total`, a warn log with the store id and attempt). The savepoints inside a sync that keep going on a failure (the offline offer pricing, the referral reward's enqueue) pass a `40P01` or `40001` up, so the retry sees it. Pre-locking every customer's accounts at the start of the sync was not used: it would take ledger locks before the claim rows of later entries, against the order above. A sync is safe to retry: entries are keyed by device and sequence, and referral rewards are enqueued in the purchase's transaction (D-169), so a rolled-back attempt leaves nothing behind. What is left is a sync that reaches the same customer twice (a counter-code entry, then a bill claim) while a sweep holds that claim: either side may lose, the sync retries, and a sweep that loses counts the claim `failed` and takes it next minute. Postgres's deadlock detection is the backstop.
- **Tests.** Real ledger and referrals through HTTP (`pos_ledger_http_test.go`): a bill QR sale holds, binds, captures and pays cashback once over a replay, and a reversal returns the credits and claws the cashback back (once); the sweep releases an expired claim's hold and the late sale is refused; a hold released under a live claim gives 409 with nothing posted; a sync entry sent twice pays one cashback; a referral stays pending under ₹100, qualifies on a ₹100 linked sale and goes back to pending on its reversal; the bind races the sweep (a worker clock past expiry, the sweep started 0–500 ms late) with exactly one winner a round and the wallet right after each; the sweep races a sync of two customers' claims in reverse order (`TestPOSLedgerSweepRacesSyncDB`, 6 rounds, both always settle); a reversal after the customer spent the cashback leaves the wallet at 0, posts the empty marker and logs the shortfall (`TestPOSLedgerReverseSpentCashbackDB`); a second reversal is 409 `invalid_transition`. The credits tests are in `reverse_db_test.go` and `reverse_fake_test.go`; the fake behaves the same.
- Lives in: `backend/internal/credits/{credits,service,reverse,fake}.go`, `backend/db/queries/credits/credits.sql` (`CapturedPortions`), `backend/internal/pos/{seams,sale,claims,sync,money}.go`, `backend/internal/platform/{money,db}`, `backend/db/queries/pos/pos.sql` (`SaleBillClaim`, `LockDueBillClaim`), `backend/internal/jobs/pos.go`, `backend/cmd/todayz/routes_pos.go`; `docs/runbooks/credits-ledger.md`, `docs/runbooks/pos-bill-claims.md`.

## Referrals as built (W2-03, 2026-10-24)

**D-160 · A business claims its referral itself, owner only, before verification (W2-03).** 01 §8 lets a new shop be referred but names no claim step. A business claims through `POST /v1/biz/businesses/{bid}/referrals/claim`, which needs `referrals.claim` (owner only; owner and manager hold `referrals.view`). The window (`ref_attribution_window_days`) runs from the business's `created_at`, and a claim is refused once the business is verified (draft, submitted and needs_info may claim). A business takes a customer or business code, never a store code; a customer takes a customer or store code, never a business code (`referral_not_eligible`). Self covers the business's own code, its staff's customer codes, and a business sharing any staff member.
- Lives in: `backend/internal/referrals/claim.go`, `backend/internal/platform/authz/policy/referrals.go`, `backend/api/paths/referrals.yaml`.

**D-161 · Rewards post per side, idempotently, and wait when they cannot post (W2-03).** A referral holds `referrer_rewarded_at` and `referee_rewarded_at` and the two amounts snapshotted at the claim (RF-10). Each side posts on its own, in a transaction of its own after the qualifying purchase commits (D-169), with the idempotency key `referral:<id>:<side>`, so a ledger failure never fails the purchase that qualified it: the referral stays `qualified` and is retried with a backoff by the `referral_sweep` job (every 10 minutes, 200 per list per pass). A referral is `rewarded` when both sides are done. A store referrer's reward goes to its business's primary store (the oldest live store, D-72); with no live store that side waits. A deleted referrer's side is closed without a posting; a blocked customer referrer's side waits until they are unblocked (D-168). The ledger is the `referrals.Ledger` interface, which W2-01's `credits.Poster` implements through `jobs.ReferralsLedger` (D-169).
- Lives in: `backend/internal/referrals/qualify.go`, `backend/internal/referrals/seams.go`, `backend/internal/jobs/referrals.go`, `backend/db/migrations/20301_w2-03_referrals.sql`.

**D-162 · Links, standees and the attribution cookie (W2-03).** Every link and standee is `GET /v1/public/ref/{code}` (`?k=c|b` marks a customer or business link; `?s=<standeeId>` a standee scan). It records a `referral_clicks` row (the referrer resolved at click time, IP and user agent as keyed hashes only) and redirects: a store code to `/s/{slug}` in the customer app (keeping `?s=`), a business link to the business app, anything else to the customer app. An unknown code redirects home and records nothing. The click id goes in `__Secure-tz_ref` (Path=/v1, HttpOnly, Secure, SameSite=Lax, Max-Age = the window). First touch: a live cookie is kept on a later click, and at the claim a valid click made before sign-up and within the window beats a typed code; otherwise the typed code is used.
- Lives in: `backend/internal/referrals/click.go`, `backend/internal/referrals/claim.go`.

**D-163 · The fraud rules as built (W2-03).** One referral per referee (a repeated identical claim answers 200 with `created: false`; any other is `referral_already_claimed`). A phone hash already used by a referral of the same referee type is `referral_already_claimed` (a deleted account that signs up again). Same device (D-13): the referrer's (or, for a store or business, any staff member's) device at any time, or another account active on the device within 90 days, records the referral as `rejected_same_device`; clients see `rejected` with no reason. A device that has carried 3 or more accounts raises a `referral.device_accounts` risk flag but does not reject.
- Lives in: `backend/internal/referrals/claim.go`, `backend/internal/referrals/referrals.go`.

**D-164 · What qualifies a customer referral (W2-03).** The referee's first completed purchase whose eligible amount minus credits used is at least `ref_qualifying_min_order_paise` qualifies it: a collected order (through `orders.Referrals`, called in the hand-over transaction) or a POS sale linked by `bill_qr` or `counter_code` (`phone_unverified` and unlinked sales never qualify, D-07). A smaller purchase leaves the referral pending, with no expiry; a later big enough one still qualifies it.
- Lives in: `backend/internal/referrals/qualify.go`, `backend/internal/orders/seams.go`, `backend/internal/orders/transitions.go`.

**D-165 · How a new shop's sales are counted (W2-03).** Each completed order or linked POS sale at any store of a pending referred business adds one `referral_shop_sales` row (idempotent per purchase), of any amount. Not counted: purchases by the business's own staff, by the referrer (a customer referrer, or any staff of a referrer business), unlinked or `phone_unverified` POS sales. When `ref_shop_qualifying_orders` different customers have a counted purchase and the business is verified, the referral qualifies; if verification comes later, the sweep qualifies it. Distinct customers, not purchases, since D-168.
- Lives in: `backend/internal/referrals/qualify.go`, `backend/db/queries/referrals/referrals.sql`.

**D-166 · A reversed purchase (W2-03).** `OnPurchaseReversed(refType, refID)` reverts every referral that purchase qualified back to `pending` when neither side has posted; once a side has posted, the rewards stay and a `referral.reversed_after_reward` risk flag (score 40) is raised for admin. The purchase is always uncounted from a pending shop referral. One purchase can touch several referrals (a customer's and a shop's).
- Lives in: `backend/internal/referrals/qualify.go`.

**D-167 · Referral notifications and deletion (W2-03).** Two notice kinds: `referral_unlocked` (to a customer, either side) and `shop_referral_unlocked` (to the shop: the business and the store credited; notify picks the staff), sent when that side's reward posts. Deleting an account rejects the pending referral it joined through (`account_deleted`); referrals it made stay, and its device and phone hashes stay on the rows for the D-163 checks.
- Lives in: `backend/internal/platform/notice/notice.go`, `backend/internal/notify/catalogue.go`, `backend/internal/referrals/referrals.go`.

**D-168 · A new shop qualifies on distinct customers; a blocked referrer waits (founder, 24 Oct; W2-03).** The founder decided two of W2-03's questions:
- **Distinct customers.** A referred shop qualifies after purchases by `ref_shop_qualifying_orders` different customers, not that many purchases. Each counted purchase stores its buyer (`referral_shop_sales.customer_id`, migration 20302), and the count is distinct buyers. A customer's second purchase is recorded but adds nothing. The exclusions of D-165 still apply. `ordersCounted` in the business's referral stats is that count.
- **A blocked referrer is not paid.** When a referral qualifies while its customer referrer is blocked, that side stays qualified and unpaid. The referee is still paid. The `referral_sweep` job pays the referrer once they are unblocked. A referral waiting only on a blocked referrer is left out of the sweep's batch, so a pile of them cannot hold up other rewards. A deleted referrer stays void (D-161).
- Lives in: `backend/internal/referrals/qualify.go`, `backend/db/queries/referrals/referrals.sql`, `backend/db/migrations/20302_w2-03_shop_sale_customer.sql`, 01 §8.

**D-169 · Referral rewards post to the credits ledger (W2-03 × W2-01).** `referrals.Ledger` is wired to W2-01's `credits.Poster` through `jobs.ReferralsLedger`. `referrals.New` refuses a nil `Ledger`; `NoLedger` is deleted.
- **One constructor.** The API (`newReferrals`), the `referral_reward` job and the `referral_sweep` job all build the service with `jobs.ReferralsService`, so all post through the same ledger.
- **The posting.** Each side is one `referral_reward` transaction, `ref_type` `referral`, `ref_id` the referral, key `referral:<id>:<side>`. It moves `platform_marketing` −N to the customer's `customer_wallet` (which gets a lot that expires like any other) or the store's `store_credit` +N.
- **Units.** N is the snapshotted `referrer_credits`/`referee_credits`. Rewards and ledger amounts are both whole credits (01 §7: 1 credit = ₹1), so there is no conversion.
- **After the purchase commits (review fix).** A posting inside the purchase took a second ledger lock call after `credits.Complete` had locked the buyer's wallet, held and the store's credit. Posting to the referrer's wallet then waited on a row that the referrer's own `Hold` or `Complete` at the same store held, while that one waited on the store: 40P01, and the customer's purchase failed because of a reward (reproduced: 20 of 20 rounds deadlocked). So:
  - the purchase's transaction only marks the referral `qualified` and enqueues `referral_reward` with `InsertTx` in the same transaction, unique per referral (`referral:<id>`). If the purchase rolls back, the job goes with it; if the enqueue fails, it is logged and the sweep posts the referral, which is due at once;
  - the job (`ClassMoney`, 3 attempts) calls `PostRewards`, which posts each side in its own short transaction and refuses to run inside a caller's: an unlocked read of the referral, the ledger posting (one `LockBalances` call), then `SELECT … FOR UPDATE` on the referral and a recheck. If the referral was reversed or posted by another pass meanwhile, the whole side rolls back, posting included. Lock order is ledger accounts first, the referral row last, which is also what a purchase and a reversal take (their credit postings, then the referrals hook);
  - `referral_sweep` stays the backstop for a job that never ran.
  - A duplicate key with the same legs counts as posted, with the first transaction id.
  - `TestReferralRewardConcurrentWithReferrersOrderDB` races a referee's first purchase at store S (with cashback, so it locks S) against the referrer's own hold and order at S and the previous round's reward posting: nothing fails.
- **Retries back off (migration 20303).** `referrals.attempts` and `next_attempt_at`. A pass that leaves a side unposted (a ledger error, nothing posted, a store referrer with no live store yet) records an attempt and is next due in 10 minutes, doubling per attempt, at most 6 hours (operational pacing in code, not a product setting). `QualifiedReferrals` reads only due rows, ordered by `next_attempt_at, id`, so 200 stuck referrals cannot starve fresh ones. A blocked customer referrer (D-168) and a refused reward stay out of the batch by their own condition and come back when it changes. `tz_referral_reward_failed_total{reason}` (`ledger_error`, `refused`, `error`) counts each side left unposted, and the sweep's summary has `waiting` beside `rewarded` and `shops_qualified`.
- **A reversal while one side waits.** Once either side has posted, a reversal keeps the referral (01 §7: "if it has posted, the reward stays") and raises `referral.reversed_after_reward` (D-166). The side still waiting is paid when it becomes payable.
- **Refused for good.** `credits.ErrKeyReused` and `ErrInvalid` (and an unknown beneficiary kind) wrap `referrals.ErrRewardRefused`. Retrying cannot help, so:
  - the referral gets a `referral.reward_refused` risk flag (score 50, details: `kind` and `sides`, the refused sides), and the error is logged at error level. There is one such flag per referral: a retried job adds nothing, and a second refused side is added to the open flag's `sides`;
  - the sweep leaves out any referral with that flag `open` or `confirmed`, so it never retries in a loop and never fills its batch with them;
  - clearing the flag in admin lets the next sweep try once more. If the cause is still there, it raises a new flag.
  - The whole referral waits, including a side that is unposted for another reason.
- **Second review fixes.**
  - *Posted means a ledger transaction.* A side marked done without one (a deleted beneficiary, zero credits) is not a posted reward. `OnPurchaseReversed` and `RevertQualified` test `*_transaction_id IS NULL`, not `*_rewarded_at`, and the revert clears both `*_rewarded_at`. So a reversal while the referrer is void and the referee waits revokes the qualification, and nobody is paid for the reversed sale.
  - *The beneficiary is rechecked under the ledger lock.* Account deletion forfeits the wallet and marks the user deleted in one transaction, under the wallet lock. After `PostReward` holds that lock, the side reads the beneficiary again. If it changed (deleted, blocked, the store no longer live), the side rolls back, posting included, and is run once more, which voids or waits it. No lot lands in a forfeited wallet.
  - *One referral cannot stop the sweep.* An error posting one referral is logged and counted (`error`), that referral backs off as for any unposted side, and the sweep goes on; the first error is returned at the end, so the run is still marked failed.
  - *Waiting and blocked sides are counted.* `tz_referral_reward_failed_total` has `reason="waiting"` (a store referrer with no live store) and `reason="blocked"` (a blocked customer referrer) beside `ledger_error`, `refused` and `error`.
  - Tests: a reversal with one side posted and the other waiting; with a void side; the job and the sweep posting the same referral at once (exactly 2 reward transactions, 10 rounds); the beneficiary deleted between the read and the posting (fails with the recheck removed); a sweep that goes on after one referral fails; one refused flag for a retried job and for both sides refused.
- Lives in: `backend/internal/referrals/reward.go`, `backend/internal/jobs/referrals.go`, `backend/cmd/todayz/routes_referrals.go`, `backend/internal/referrals/seams.go`, `backend/internal/referrals/qualify.go`, `backend/db/queries/referrals/referrals.sql`, `backend/db/migrations/20303_w2-03_reward_attempts.sql`, `docs/runbooks/credits-ledger.md`.

## Business setup screens as built (W2-10 finish, 2026-10-24)

**D-200 · Business onboarding, stores and settings screens (W2-10 finish).** B-02 is a single resumable page at `/onboarding` (not a step-by-step flow): the business, its first store, hours, payout and tax details, documents and "Send for review", each saved on its own. Resume is by reading the server's state.
- Hours for the first store are saved through the settings endpoint, so a wizard run never creates the store twice.
- Skipped for now: language step, starter catalogue, notification prefs, custom delivery hours, photos, field-agent confirm (W3-06).
- Lives in: `business/src/onboarding/`, `business/src/stores/`, `business/src/settings/`.

**D-201 · Roles on the business screens (W2-10 finish).** Owner: onboarding, payout and tax edits, invite manager or cashier, edit staff, remove a non-owner. Manager: sees stores and settings, edits the store and settings, invites and removes cashiers, mutes asks, sees payout masked. Cashier: none of these. The server is the real gate; the screens only hide what the person cannot do.
- Lives in: `business/src/stores/perm.ts`, `business/src/credits/permissions.ts`.

**D-202 · Reads before writes with If-Match (W2-10 finish).** Business, store and settings edits send `If-Match` from the ETag of the last read; when none is known the screen reads first. A `412 precondition_failed` refetches and asks the person to try again. Create, invite, submit and document routes carry an `Idempotency-Key`.

**D-203 · Documents (W2-10 finish).** The file is checked in the browser (type, 10 MB, not empty) before any call. The screen registers the document, then PUTs the file to the presigned URL with only the signed `Content-Type`. There is no confirm call in 04, so a failed PUT leaves a `pending` document that uses one of the five slots until support clears it.
- Open question for the lead: should 04 add a confirm or delete route for a failed upload?

**D-204 · Conservative readings (W2-10 finish).** Where the plan is silent the screen takes the stricter reading: the map pin is latitude and longitude inputs plus "Use my location"; closing or moving a store says "contact support"; a manager scoped to some stores cannot add a store; the delivery radius limit of 20 km follows the API schema, not `platform_settings`; the invitation page treats the link token as the staff id (04 has no get-invitation-by-token route).

**D-205 · Standee page (W2-10 finish).** `/standee` previews the A5 PDF in a frame and downloads it, for owner and manager. The PDF is fetched with the session token and shown from a blob URL, so no token is ever in a URL. It uses the `getStandeePdf` adapter in `credits/api.ts` until `getQrStandee` is generated (`standee.yaml` is still a stub, owner W3-04 per its header, though D-51 gives the PDF to W2-10).
- Lives in: `business/src/standee/`.

**D-206 · Lead answers to the W2-10 finish questions (lead, 24 Oct).** Conservative readings, per the founder's standing call:
- `/invite/$token` may treat the token as the staff id, provided the backend only shows an invitation to the phone it was sent to.
- A document whose upload failed stays `pending` and uses one of the five slots. Open as work for the W2-10 backend owner: a route that deletes your own `pending` document.
- The 20 km delivery radius maximum stays in the API schema: 01 §17 does not mark it configurable, and it matches the largest radius chip (01 §11).
- The form may leave category empty for a draft; submit names it in plain words.
- Store-limited managers do not see Add a store until the backend `TODO(lead)` is settled.
- `standee.yaml` belongs to W2-10 (D-51); its header naming W3-04 is wrong and gets fixed with the next change to that file.
- The unused `shop.invite.*` keys are removed from `auth.en.json`.

## Customer finish as built (W2-08/09 finish, 2026-10-24)

**D-190 · Lead answers to the W2-08/09 finish questions (lead, 24 Oct).** Conservative readings:
- **Shop phone:** customers do not see it. `DiscoverStorePage` says never the phone, so "Call shop" stays out.
- **Credits copy** ("30 credits on ₹100+") stays out until a public settings endpoint exists.
- **Pickup-code QR:** waits; no dependency is added in Wave 2. The code is shown as text.
- **Production tiles and the `IMAGE_BASE_URL` key form** are settled in Wave 5 with the infra. Locally they are per-city tiles and `<base>/<imageKey>`.
- **C-05 when no shop confirmed** (founder, 24 Oct): say none of the shops has confirmed yet and offer a wider radius. The shops are not asked again automatically.

## POS counter and offline queue as built (W2-11, 2026-10-24)

**D-170 · The offline store is raw IndexedDB behind a `PosStorage` interface, not Dexie (W2-11).** D-88 names Dexie; the build had no new dependency to spend, and the POS needs three object stores (`outbox`, `catalogue`, `meta`, the last holding the cursor, settings and the in-flight marker). `business/src/sync/idb.ts` wraps them; `memoryStorage` backs the tests. When storage cannot open (private mode), the counter keeps working in memory and says the sales are not safe if the tab closes. Swapping in Dexie later touches only `idb.ts`. Lead (24 Oct): accepted; no new dependency.

**D-171 · The outbox and its send rules (W2-11).**
- Every sale gets a client ULID and the next device sequence in the same write that saves it; nothing is shown as sold before that write succeeds. A failed write keeps the bill on screen.
- A sale is first sent by itself (`createPosSale`, `Idempotency-Key` = its client id) when online, and by `syncStore` (cursor, entries in sequence order) otherwise or after a failure. The server's idempotency on `(store_id, client_id)` makes a double send safe.
- Failures back off (the engine exposes `nextAttemptAt`); `applied`, `duplicate` and `refused` results settle an entry. A `refused` sale stays in the list as "needs your attention" with the server's reason; it is never retried or deleted by the app.
- Sales always apply: stock may go negative and the item is flagged `countNeeded`; the sold price stands as sent (D-130, 02 §4.3).

**D-172 · Money units on the counter (W2-11).** Everything is integer paise; the wire is paise except credits, where 1 credit = ₹1 (01). Round-off is half up to the whole rupee and is shown as its own line. A bill under 50 paise would round to zero: TODO(lead) rule not stated.

**D-173 · No snapshot or changes endpoint exists, so the catalogue bootstraps from `syncStore` plus `listStoreItems` (W2-11).** First open syncs from cursor 0 with no entries, then pages `listStoreItems` into the local catalogue; later syncs apply the `store_item` changes (`upsert` and `delete`). Product, category and offer changes are ignored by the POS. A cursor the server has pruned (`410 resync_required`, D-36) restarts the bootstrap. TODO(lead): an explicit snapshot endpoint, and a feed for the best-seller order of the quick tiles (they fall back to alphabetical).

**D-174 · Settings are never assumed on the client (W2-11).** `PosSettings` fields are nullable; the counter does not gate on them and shows no number the server did not send. `credit_min_order_value_paise`, `pos_reversal_window_hours` and `pos_offline_link_max_hours` have no feed yet, so below-minimum, reversal-window and offline-link failures show the server's answer (`below_credit_minimum`, `reversal_window_passed`). TODO(lead): a settings feed on the store sync.

**D-175 · Customer links (W2-11).**
- A bill QR (`createBillClaim`, polled with `getBillClaim`) needs the network; once confirmed, the sale carries the claim and credits used. A bill-QR sale that fails after the claim expired is refused and must be rung again (`claim_expired`, `claim_already_used`, `claim_mismatch`).
- A counter code (`verifyCounterCode`) binds a sale for 2 minutes (D-130). A verified code that falls back to sync is sent with its entry time and clock offset; the server may refuse it as used, and the sale then shows as refused.
- An unverified phone is allowed and noted on the receipt; it earns no credits.
- `code_invalid` shows tries left; `code_locked` and the cross-store lock that W2-04 added are one generic locked state ("Too many wrong codes. Try again later or link by phone."). Codes are held in component state only and never logged or stored.

**D-176 · Roles on the counter (W2-11).** A cashier can ring and charge. Whole-bill and line discounts, reversal and the sales history's Reverse action are manager and above; those controls are hidden for a cashier, and the server is the gate. A reversal asks for a reason and confirms in an `alertdialog`.

**D-177 · Scanner (W2-11).** The keyboard wedge (fast keystrokes ending in Enter into the search box, or anywhere on the counter) always works. Camera scanning uses the browser's `BarcodeDetector` when present and otherwise says it is not available on this device; no ZXing dependency was added. Lead (24 Oct): the iOS Safari camera fallback is a Wave 3 decision; the keyboard wedge covers the counter until then. An unknown barcode opens "Add this product" (name, price), which saves a custom line and links to Stock for the full form (W2-12).

**D-178 · Receipts (W2-11).** The receipt view is rendered from the local sale or the server's, so it also works offline; printing is the browser's (`@page` 80 mm wide with a 72 mm receipt body, fixed 297 mm tall because `auto` height is not valid CSS, so a very long receipt may paginate). Share on WhatsApp or SMS uses the receipt link only when the server returned a token (a replayed sale has none, D-130). The bill-QR URL `…/scan/{token}` and the customer scan route are assumed; TODO(lead) confirm with W2-08/09.

**D-179 · What is cached on the device, and the conservative readings (W2-11).**
- The store reference and role are cached in `localStorage` so the counter opens offline; sign-out clears it before the logout call, so a failed call still clears it (lead, 24 Oct). Unsent sales stay in the outbox: they belong to the shop.
- Storage is one IndexedDB per store id. The in-flight marker records the entries being sent so a reload mid-send resends them with the same ids.
- `expectedTotalPaise` is not sent, so a price change the shop did not see is a `409 price_changed` and the sale stands at the price quoted; the line is redrawn on the next catalogue sync.
- The update toast is held while a sale is open (`useHoldUpdates`, W1-08).
## Cloud go-live (founder, 2026-10-24)

**D-210 · One repo, prod only, cloud now (founder, 24 Oct).** Supersedes the "local first until the release candidate" rule (09 intro, D-96 §5 order) and the "no GitHub repos" rule.
- **GitHub:** one private repo `todayz` (this monorepo) in a TodayZ GitHub organization, not the founder's personal account. A later split into the per-app repos stays possible because every app folder builds on its own.
- **No staging.** `envs/staging` is not applied. Every merge or push to `main` deploys straight to prod (founder: "on main branch merge/push"): the API and worker to Beanstalk, and the four apps (site, customer, business, admin) to their Cloudflare Pages projects. GitHub Actions decides what to deploy from the folders a merge changed. A `shared/` or contract change rebuilds every app that uses it.
- **Credentials:** cloud admin credentials live in `.env.cloud.local` (git-ignored, read only by the lead's shell for gh, Terraform and wrangler, never exported to app processes). This replaces `.env.local` for the cloud names, so the app processes never see them. The founder's plugins and connectors are not used. CI then deploys through a GitHub OIDC role.
- **Kept:** the budget alarm comes first. The founder sees the prod `terraform plan` summary (resources and cost, about ₹13–17k a month) before the first apply. App keys are generated fresh for prod, straight into Secrets Manager. `make check` stays the gate before every deploy.
- **Prod data:** reference data only (categories, places, master catalogue, admin users). Never the dev seed.
- **Guide:** `docs/runbooks/cloud-access.md`.

## Constraints migrations (lead, 2026-10-24)

**D-211 · The Wave 1 and Wave 2 constraints migrations (D-79).** The Wave 1 constraints migration was never written, so it lands now with Wave 2's.
- **Lead block.** A lead file needs a `w<wave>-NN` id for the name check (D-89), and "lead" does not match it. The lead uses WP number 99: `19901_w1-99_lead_constraints.sql` and `29901_w2-99_lead_constraints.sql`. No work package has number 99. Both sort after every package in their wave, and a lower block applied late is fine under D-90.
- **Wave 1 (16 FKs).** These are the 03 §13 columns except `product_review_queue.decided_by`, whose table was deferred in the W1-05 weekend slice. `job_runs.triggered_by` and `job_controls.updated_by` get the index they lacked.
- **Wave 2 (9 FKs).**
  - `credit_holds.order_id` and `credit_holds.bill_claim_id`;
  - `reviews.order_id` and `reviews.pos_sale_id`;
  - `offer_redemptions.order_id` and `offer_redemptions.pos_sale_id`;
  - `referrals.referrer_transaction_id` and `referrals.referee_transaction_id`, each with a new partial index. 03 §13 called these a single `reward_transaction_id`; it is corrected;
  - `notify_order_escalations.order_id` (W2-05). It was missing from 03 §13 and is added there.
- **Still deferred.** `referrals.standee_id` and `referral_clicks.standee_id` wait for a `qr_standees` table, and `support_tickets.order_id` waits for W2-09. Whichever package creates the table adds the FK in its own migration, the way a later-wave target does (D-35).
- All the constraints are plain `NO ACTION`, like the earlier FKs. No code deletes a parent row: orders, sales, claims and ledger transactions are never deleted, and users are anonymised, not removed.

## SMS on AWS End User Messaging (2026-10-24)

**D-212 · SMS through AWS End User Messaging, not MSG91 (lead, 24 Oct).** SMS (login OTPs and notice SMS) goes through AWS End User Messaging SMS (`pinpoint-sms-voice-v2` `SendTextMessage`) in the prod AWS account. The instance role signs each request, so no SMS key is stored anywhere (`MSG91_AUTH_KEY` is gone from config, `.env.example` and the Secrets Manager list). India DLT: every send carries `DestinationCountryParameters` `IN_ENTITY_ID` and `IN_TEMPLATE_ID`, and the DLT sender ID as the origination identity. Config: `SMS_SENDER_ID`, `SMS_DLT_ENTITY_ID`, `SMS_OTP_TEMPLATE_ID`, `SMS_NOTICE_TEMPLATE_IDS` (catalogue template name to DLT id, `name=id,name=id`), region from `AWS_REGION`; `SMS_API_URL` stays as the override, and local and test send to the mocks through it. `make check` stays offline. The SMS text must be exactly the registered DLT template (`docs/runbooks/aws-founder-steps.md` part C1), and DLT caps each `{#var#}` at 30 characters.

**D-213 · How D-212 was built, and the conservative readings (W3 SMS swap).**
- **One client for both callers.** `internal/platform/awssms` builds the SDK client once for identity (OTP) and notify (notice SMS). The client is built directly with `pinpointsmsvoicev2.New`, never `LoadDefaultConfig`. Its HTTP goes through `httpx.NewOutboundClient` (egress guard). It has no SDK retries, so each message is sent at most once, and a 10 s timeout.
- **Credentials.** Deployed (staging, prod) credentials come from the EC2 instance role through IMDSv2 only: the endpoint is pinned to `169.254.169.254` and there is no IMDSv1 fallback. The metadata client has its own transport, which never uses `HTTP(S)_PROXY` and keeps the egress guard. Nothing reads `~/.aws`, environment keys or the default chain (`TestDeployedUsesInstanceRoleOnly` proves it against a fake IMDS). Local and test sign with fixed dummy credentials and refuse any `SMS_API_URL` that is not a local host, so they can never sign for a real endpoint. Under `TODAYZ_NO_EGRESS` the IMDS call is refused before it leaves the machine.
- **No endpoint override when deployed.** Staging and prod refuse `SMS_API_URL` outright: config refuses to load, identity Preflight and the notify integration status name it, and `awssms.New` refuses it. A stray override would otherwise receive the role-signed request, with its session token, the phone and the OTP.
- **Credentials errors.** A failure to get the role's credentials reads `awssms: send failed: credentials: <reason>`, where the reason is one of `imds token timeout`, `imds token refused`, `no instance role`, `imds <AWS code>`, `imds status <n>`, `imds timeout`, `egress blocked` or `imds unavailable`. These are fixed words and a checked code only, never a value. One metadata request times out after 2 s.
- **Selection.**
  - Deployed needs `AWS_REGION`, `SMS_SENDER_ID` and `SMS_DLT_ENTITY_ID`. Local and test need `SMS_API_URL` instead of the region.
  - Login also needs `SMS_OTP_TEMPLATE_ID`. Identity preflight names every missing variable, never a value.
  - Locally, with `SMS_API_URL` unset, OTPs are dropped as before. With it set but the ids incomplete, login fails visibly rather than silently.
- **Notices.**
  - A notice template with no entry in `SMS_NOTICE_TEMPLATE_IDS` fails that delivery with a reason naming the template, and nothing is sent. The integration status lists the unmapped template names.
  - Deliver cuts every SMS template param to 30 runes (trailing spaces dropped) before rendering. Only the SMS is cut; the inbox row keeps the full value.
  - `TestSMSTextMatchesDLTTemplates` pins the catalogue's `order_unseen` en/hi text to the DLT texts.
- **OTP text** is exactly `<code> is your TodayZ login code. Do not share it with anyone.` (the DLT template), with a 300 s time to live.
- **Mocks.** `POST /sms-voice` speaks AWS JSON 1.0 and records each send in the SMS capture, so `/_mock/sms/latest-otp` works unchanged.
  - For a `+91` number it requires the sender ID and both DLT ids.
  - When the template id is listed in `fixtures/sms/dlt-templates.json`, the body must match that template with each `{#var#}` at most 30 characters. The real operator would drop such a message silently, so failing here shows the drift locally.
  - The MSG91 SMS routes stay as legacy.
- **Terraform.**
  - `policies/sms-send.json` is attached to both the api and the worker role. It allows only `sms-voice:SendTextMessage` on `sender-id/<SMS_SENDER_ID>/IN` and this account's opt-out lists. It is created only when `sms_sender_id` is set.
  - `check.py sms` (and `all`) keeps it strict: one Allow statement, that one action, no wildcard identity ARN, no `NotAction`, `NotResource` or `Condition`, and attached to both roles. The read-only api policy and the worker metrics policy are unchanged and still refuse it.
  - `AWS_REGION` and the four SMS values reach both environments as plain properties; an empty one is left out.
- TODO(lead): IAM resource types. Whether `SendTextMessage` is also authorised against the opt-out list (granted as `opt-out-list/*`) or a configuration set (not granted) is unverified without a cloud call. The go-live list (09 §6) now has a `DryRun` `SendTextMessage` from the instance role on the first prod deploy, which checks the resource types before any real send. A missing one shows as `AccessDeniedException`.
- **IMDS hop limit.** IMDSv2 from inside the Beanstalk Docker container needs a hop limit of 2. Beanstalk has no option for the IMDS hop limit, so we rely on the AL2023 AMI default, which is 2. The first-deploy smoke checks it: a hop limit of 1 shows as `credentials: imds token timeout` on the first send (09 §6).
- TODO(lead): roles. Both api (OTP) and worker (notice SMS) get the send permission. Narrow one if only one process should send.
- TODO(lead): Devanagari cut. Cutting a store name at 30 runes can split a Devanagari grapheme cluster (a vowel sign without its consonant). It still matches DLT's 30-character cap, but may look odd.
- TODO(lead): no WebOTP line. The OTP SMS has no WebOTP (`@app.todayz.in #code`) line, because DLT rejects any text other than the registered template. Adding autofill means registering a new template.
- TODO(lead): removing `MSG91_AUTH_KEY` from `modules/secrets` destroys that secret if it was ever applied. Nothing has been applied yet.
- TODO(lead): price. `CostPerUnitPaise` for SMS still needs the AWS India price per part.
- TODO(lead): legacy routes. Remove the legacy MSG91 SMS mock routes when nothing uses them.

## Frontends live on Cloudflare Pages before AWS (lead, 2026-10-24)

**D-214 · The four Pages sites go live now, and deploy from GitHub Actions on every push to main.**

The founder asked for the landing site and every UI to be live on GitHub and Cloudflare while the AWS account is still blocked.

- **What is live.** Pages projects `todayz-site`, `todayz-customer`, `todayz-business` and `todayz-admin`, serving todayz.in and www.todayz.in, app., business. and admin.todayz.in. The apex A record and the www CNAME that pointed at Vercel now point at `todayz-site.pages.dev`. Mail (MX, SPF, DMARC, autodiscover, `email.`) is unchanged; the `_vercel` TXT records are left in place and can go.
- **Until the API is on AWS**, the UIs load but sign-in and data fail: api.todayz.in has no record yet.
- **Turnstile.** A `todayz-prod` widget (managed, for todayz.in, app., business. and api.) was created. Its site key is public and lives in `infra/cloudflare/pages.env` with the other build settings. The secret stays in Cloudflare and goes into Secrets Manager as `TURNSTILE_SECRET_KEY` with the AWS stack.
- **Made by hand, not by Terraform.** The projects, Pages domains, DNS records and Turnstile widget were created through the Cloudflare API, because Terraform's state backend is the S3 bucket that does not exist yet. When the state bucket exists, `infra/cloudflare` imports them (import blocks with the same names) before its first apply, so nothing is recreated. The Terraform module does not yet know `www.`; add it to the site project there.
- **Deploy.** `.github/workflows/deploy.yml` has two jobs. `build` (no secrets) runs `scripts/deploy/pages.sh prepare` and `scripts/deploy/pages.sh build`, and uploads `.cache/pages`. `prepare` installs node_modules and oapi-codegen and runs only the generators the builds use (`gen-events`, `gen-contract`, `gen-frontend`): the first run with the full `make setup` died on the GitHub runner mid-step (no log), and the sites need none of its Docker images, tiles, Terraform providers or Python tools. The make targets sit in the script because workflows may call only the canonical targets (D-56). `deploy` downloads that and runs `scripts/deploy/pages.sh deploy` (wrangler pinned, via npx) with `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` from repository secrets, so the secret never shares a job with package installs.
- **wflint exception (amends W0-06's "no secrets").** `scripts/ci/wflint/secrets-allow.txt` lists `path NAME` pairs. A listed workflow may set a step `env:` value to exactly `${{ secrets.NAME }}` for a listed name; a secret anywhere else (run blocks, `with:`, longer strings, `if:`, `secrets:` keys, unlisted names or files) still fails. The selftest proves each case.
- **Build output.** Hidden source maps are deleted before upload (error symbolication can come with Sentry later). The three Vite apps get a `_headers` file from the script: nosniff, referrer policy, frame DENY, HSTS without `includeSubDomains`, a permissions policy, and caching for `sw.js` and `/assets/*`. They have no CSP yet; the site keeps its own CSP.
- The repository secret is the founder's admin Cloudflare token. A Pages-only token was offered; the founder chose to keep this one (2026-10-24).
- TODO(lead): a CSP for the three apps, proved by an e2e run against a prod-mode build.

**D-215 · The first green CI on GitHub: runner speed, scanner findings, Linux in the stack smoke (lead, 24 Oct).**

Once the runner had disk (D-214's `free-disk.sh`), ci, security and e2e each failed on something the laptop never showed.

- **Search latency.** `TestSearchAtScale` measured p95 387 ms on a shared GitHub-hosted runner against the 300 ms budget (W1-06 says "p95 < 300 ms local"). On a GitHub-hosted runner the subtest now gates at a 600 ms regression ceiling, which still catches a lost index (a sequential scan over the 180k listings takes seconds). The 300 ms stays the gate in a local `make check` and in k6 `browse_peak` (08 §6), which is where the 02 §11 budget is measured. The budget was not loosened anywhere else.
- **Trivy, vulnerabilities.** `golang.org/x/net` in `backend/tools` goes to v0.60.0 (out 16 days; the backend module already had it, D-88). Three fixes are younger than D-88's 14 days and fix code we do not call (zod 4.6.5 via codegen, undici 8.11.2 via astro's font fetcher, certifi in the Schemathesis run): each sits in `.trivyignore.yaml` with an `expired_at` the day after its fix turns 14 days old, so the gate fails again if nobody bumps it.
- **Trivy, misconfigurations.** Trivy's AWS checks flagged three things the checkov skips already accept: SSE-S3 on the ALB log bucket (AWS-0132; log delivery cannot use KMS), the AWS-managed SNS key (AWS-0136), and HTTPS egress to 0.0.0.0/0 from the app (AWS-0104; providers have no fixed addresses, port 443 only, the egress guard allow-lists hosts). Each gets an inline `#trivy:ignore` with its reason, next to the matching checkov skip.
- **Stack smoke on Linux.** `dev-smoke.sh` found the LAN address with macOS's `ipconfig getifaddr en0`; on Linux it now takes the source address of the default route (`ip -4 route get`), so the loopback-only check runs on the runner too.

## Orders finish (W2-02 finish, 2026-10-24)

**D-216 · The checkout quote is informational; only placing refuses credits (lead, 24 Oct, Q-1).** `POST /v1/me/orders` with `creditsToUse` above `credit_max_per_order` is refused with 409 `credit_max_exceeded` (04 §2, CR-02), never clamped. The same goes for credits on a bill under the credit minimum (409 `below_credit_minimum`). A wallet that no longer covers the credits asked is 409 `price_changed` with the current quote. `POST /v1/me/checkout/quote` keeps clamping to what the rules allow and reports why in `creditsLimit` (D-76), so the customer app's credits probe keeps working; the quote has no new `x-problems`. Test: `TestCR02PlacingRefusesCreditsItCannotUse` (quote with 31 clamps to 30 with `creditsLimit: credit_max_exceeded`; placing 31 is 409).

## Orders delivery (W2-02 delivery, 2026-10-24)

**D-218 · The failed-delivery reminders are settings; the 72 h auto-confirm stays a constant (lead, 24 Oct).** The two inbox reminders for an unconfirmed return (D-69) read `delivery_return_first_reminder_hours` (2, min 1, max 12) and `delivery_return_second_reminder_hours` (24, min 13, max 71) from `platform_settings`, seeded by `20204_w2-02_return_reminder_settings.sql`. The bounds keep the order first < second < 72 h. The 72 h auto-confirm and its risk flag score stay fixed constants (D-85, 01 §17 "Fixed constants"). This narrows D-92's last line. The reminder is the new business notice kind `delivery_return_pending` (in-app only). Until the business inbox can filter by role, every staff member sees it, cashiers included.
