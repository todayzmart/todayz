# 03 · Data model

PostgreSQL 16 + PostGIS 3.4, extensions `postgis`, `pg_trgm`, `unaccent`, `citext`, `pgcrypto`, `pg_stat_statements`. Migrations in `backend/db/migrations` (goose). IDs are ULIDs stored as `uuid` (time-ordered, generated in Go). Money is `bigint` paise (`amount_paise`), never float; credits are `bigint` whole credits (1 credit = ₹1). Every table has `created_at timestamptz not null default now()`; mutable tables add `updated_at` and `version int` (optimistic locking). Soft delete only where stated (`deleted_at`).

Naming: snake_case, plural tables, `fk_<table>_<col>`, `ix_<table>_<cols>`, `ux_<table>_<cols>` (unique), `ck_<table>_<rule>`.

Encryption (key names from [12-decision-log.md](12-decision-log.md) D-18): `*_enc` columns are AES-256-GCM with `PII_ENC_KEY` unless stated; `*_hash` lookup columns are HMAC with `PII_HASH_KEY`. Display copies of encrypted values keep only the last 4 characters (`*_last4`).

**Owner** column: the work package (file [07-work-packages.md](07-work-packages.md)) whose migration creates the table. Other packages read it or write through the owner's Go interface; they never alter it.

**Foreign keys (D-35).** A migration adds a foreign key only to a table from an earlier wave or from the same work package. A column that points at a table built by another package in the same wave, or in a later wave, is created as a plain `uuid` (with its index) and gets its FK constraint later: same-wave constraints come from the lead's constraints migration at the end of the wave (D-79), later-wave ones from the package that creates the target. §13 lists every such column and who adds the constraint. Polymorphic references (`ref_type`/`ref_id`, `subject_type`/`subject_id`, `owner_type`/`owner_id`) never get an FK. Every FK column has an index.

## 1. Identity

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `users` | id, name, preferred_lang (`en|hi|kn|te|ta`), status (`active|blocked|deleted`), deleted_at | One per human. Deletion anonymises the row (D-31: blocked while an order is not finished; wallet forfeited first). The same transaction deletes the customer's `asks` (with their `ask_recipients` and `ask_store_blocks` by cascade) through the `OnAccountDelete` hook W3-16 registers (D-101) | W1-01 |
| `phone_identities` | id, user_id, phone_enc (bytea), phone_hash (bytea unique), verified_at | Phone stored encrypted + keyed hash for lookup | W1-01 |
| `otp_requests` | id, phone_hash, purpose (`login|step_up|owner_confirm`), code_hash, expires_at, attempts, consumed_at, ip_hash | Purged after 24 h. The OTP is never logged (D-15) | W1-01 |
| `sessions` | id, user_id, app (`customer|business|admin`), family_id, refresh_hash, expires_at, revoked_at, revoked_reason, device_id, user_agent, ip_hash, last_used_at | Rotation + reuse detection | W1-01 |
| `devices` | id, user_id, kind (`web`), push_endpoint, push_keys (jsonb, encrypted), counter_secret_enc (counter-code TOTP secret, `DEVICE_SECRET_ENC_KEY`), last_seen_at | Device history also feeds the same-device referral check (D-13) | W1-01 |
| `admin_users` | user_id PK, roles text[] (`field_agent|support|catalogue|analyst|finance|admin|super_admin`, D-64; check constraint on the array elements), totp_secret_enc (`TOTP_ENC_KEY`), totp_enabled_at, created_by, disabled_at | | W1-01 |
| `admin_recovery_codes` | id, admin_id (→ `admin_users`), code_hash, used_at | 10 per admin, single use, regenerated as a set (D-60) | W1-01 |
| `customer_profiles` | user_id PK, referral_code (unique, 6 chars, no ambiguous letters; customer-to-customer referrals only), referred_by_referral_id (plain uuid, D-35), strikes_count_30d (cached from `strikes`), limited_until | | W1-01 |
| `addresses` | id, user_id, label, line1, line2, landmark, pincode, location geography(Point), is_default | | W1-01 |

## 2. Business

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `businesses` | id, brand_name, legal_name, category_id (plain uuid, D-35), status (`draft|submitted|needs_info|verified|rejected|suspended`), onboarded_by (admin user id, nullable), referral_code (unique; shop-to-shop referrals only, D-26), referred_by_referral_id (plain uuid, D-35), verified_at, verified_by | KYC verification is at business level (D-40). **Primary store** (D-72) = the business's oldest store (lowest `stores.live_at`) that is `live` when the reward posts; shop referral rewards go to its `store_credit` account (D-26). With no live store the reward stays `pending` and posts when one goes live | W1-04 |
| `business_kyc` | business_id PK, pan_enc, pan_last4, gstin_enc, gstin_last4, upi_id_enc, upi_id_last4, bank_account_enc, bank_account_last4, bank_ifsc, legal_name, status (`missing|submitted|verified|rejected`), verified_by, verified_at, notes | GSTIN, UPI ID and bank account are encrypted; only the `*_last4` copies are shown (D-44). Owners see it; managers and `finance` see it masked (D-38, D-64); `admin` and `super_admin` reveal one field at a time, audited, and verify KYC | W1-04 |
| `business_documents` | id, business_id, store_id (nullable), kind (`pan|gst|shop_photo|storefront|license|other`), object_key, content_type, size_bytes (≤ 10 MB, signed into the upload URL), status (`pending|uploaded|accepted|rejected`; `pending` until the upload is confirmed), reviewed_by, reviewed_at, reject_reason | Files in the private bucket (D-46). Owner per D-84. At most 5 per business per kind | W1-04 |
| `stores` | id, business_id, slug (unique), short_code (unique; the code in the QR standee URL, D-26), name, address fields, location geography(Point) (GiST), phone_enc, hours jsonb (per weekday, multiple ranges, holiday overrides; an entry `{"open":"00:00","close":"24:00"}` means open all day), closed_until (manual close, nullable), status (`draft|pending_review|live|paused|suspended`, D-40), live_at, reviewed_by, reviewed_at, accepts_pickup bool, accepts_delivery bool (D-41), asks_muted bool default false (D-101: owner and manager mute asks, audited), delivery_radius_m int, delivery_fee_paise, delivery_free_above_paise, delivery_min_order_paise, delivery_hours jsonb, cashback_bp int (basis points, 0–2000), auto_accept bool, catalogue_seq bigint (last `catalogue_changes.seq`), rating_avg numeric(3,2), rating_count, accept_rate_30d, unaccepted_expiry_rate_30d, reliability_score numeric(4,3) (nullable), reliability_computed_at | Settings live on the row; history in `audit_log`. Customers see a store only when it is `live` and its business is `verified` (D-40). With `accepts_pickup` and `accepts_delivery` both off the store is browsable but not bookable ("In-store only"). Ordering outside `hours` or before `closed_until` returns 409 `store_closed` (D-41). **Single writers (D-59):** `rating_avg`/`rating_count` only by the W2-06 rating job; `accept_rate_30d`, `unaccepted_expiry_rate_30d`, `reliability_score`, `reliability_computed_at` only by the `store_reliability` job (W2-02, D-52); a null reliability ranks as 1.0 | W1-04 |
| `staff` | id, business_id, user_id, role (`owner|manager|cashier`), store_ids uuid[] (null = all), can_deliver bool, invited_by, status (`invited|active|removed`), invite_phone_hash/enc (sealed per row; cleared on accept or remove), invited_at (an invite lapses after 7 days) | unique (business_id, user_id); unique (business_id, invite_phone_hash) while invited | W1-04 |
| `qr_standees` | id, store_id, campaign (1–40 chars), created_by, printed_at, reorder_requested_at, reorder_requested_by, created_at, updated_at, version | No code of its own (D-26): the URL carries the store's `short_code`, and a scan is tracked with `?s=<standee_id>`. A scan is a `referral_clicks` row with this `standee_id`; the counts are computed from `referral_clicks` and `referrals`, nothing is stored (D-223). At most 20 per store (`TODO(lead)` setting) | W3-09 |

## 3. Catalogue and stock

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `categories` | id, parent_id, slug, names jsonb (per lang), tint (`green|amber|rose|sky|lilac|peach`), icon, sort | Seeded | W1-05 |
| `brands` | id, name, slug | | W1-05 |
| `products` (master) | id, name, brand_id, category_id, variant, pack_size numeric, unit (`g|kg|ml|l|pc|pack`), mrp_paise, image_key, barcodes text[] (GIN), search tsvector (generated), status (`active|pending_review|merged|retired`), merged_into, created_by_store_id (plain uuid, D-35) | | W1-05 |
| `product_synonyms` | term, synonym, lang | "atta" → "wheat flour" | W1-06 |
| `store_items` | id, store_id (plain uuid, D-35), product_id, price_paise, mrp_paise, on_hand int (cached), reserved int (cached), low_stock_at int, is_listed bool, stock_updated_at, search tsvector (copy of product + local name override) | unique (store_id, product_id). Available = `on_hand − reserved`. Both counters are caches (see stock invariants below) | W1-05 |
| `stock_batches` | id, store_item_id, received_quantity int, quantity int (remaining), expires_on date (nullable), received_at, cost_paise (nullable; the cost per sale unit as paid, GST included), found_in_count bool | A batch is created only by stock in that carries an expiry or a cost, and by a positive count correction (`found_in_count = true`, no expiry); there is no endpoint that creates one directly. A manager or above can correct `cost_paise` and `expires_on` afterwards (`PATCH {S}/batches/{batchId}`, audited in `audit_log` with before and after); `received_quantity`, `quantity` and the rest cannot be edited. `quantity` is used up as stock moves (stock rule 8) and is what the Demand insights kinds read, never `received_quantity`: "going to waste" and "expired on shelf" (remaining above 0 and `expires_on` before today) and "selling below cost" and the value at cost (the latest batch by `received_at` with remaining above 0 **that has a `cost_paise`**; a batch with no cost is skipped; D-99). Index: partial `(store_item_id, expires_on) where quantity > 0` | W1-05 |
| `stock_movements` | id, store_id (plain uuid, D-35), store_item_id, delta int, source (enum below), ref_type, ref_id, actor_user_id, device_id, client_id (nullable), note, created_at | Append-only. **Not partitioned in v1** (D-34; partitioning is in [10-later.md](10-later.md)): BRIN on `created_at`, `ux_stock_movements_store_client (store_id, client_id)`. `client_id` is the device-generated id of the one offline operation line that produced the movement (a POS sale line, a count entry), so a replayed sync never writes twice. Server-originated movements (online orders, reversals) have a null `client_id`; a reversal made on a device uses new client ids | W1-05 |
| `stock_movement_batches` (D-99) | movement_id, batch_id, delta int | PK (movement_id, batch_id). The per-batch allocation of one movement, written in the same transaction as the movement (a table, not a jsonb column, so a batch's history and the FEFO property test are plain queries). Σ `delta` of a movement's rows equals the part of the movement that came from batches; any rest came from stock with no batch. Index `(batch_id)` | W1-05 |
| `stock_reservations` | id, order_id, order_item_id, store_item_id, quantity, status (`held|consumed|released`), consumed_movement_id, released_reason, created_at, updated_at | The only place reservations live (D-01) | W2-02 |
| `catalogue_changes` | store_id, seq bigint, entity (`store_item|product|category|offer`), entity_id, op (`upsert|delete`), created_at | PK (store_id, seq). POS catalogue sync cursor (D-36); `seq` is taken from `stores.catalogue_seq` with `UPDATE … RETURNING` in the writing transaction, so per store it commits in order and a cursor never skips a row. Kept 30 days; a cursor older than the oldest kept row gets `resync_required` and the device downloads a full snapshot. Owner per D-84 | W1-05 |
| `product_review_queue` | id, product_id, reason, duplicate_candidates jsonb, status, decided_by | | W1-05 |

Stock movement `source` enum, complete (D-01): `opening`, `restock`, `supplier_bill`, `import`, `count_correction`, `damage`, `expired`, `sale_pos`, `sale_online`, `pos_reversal`. Reservations are not movements.

Stock rules and invariants (property-tested, checked nightly):

1. `store_items.on_hand` = Σ `stock_movements.delta` for the item (kept by the movement trigger; nightly reconcile alerts on drift). POS sync may drive it negative; that is flagged, not refused.
2. `store_items.reserved` = Σ `quantity` of the item's `stock_reservations` with status `held` (kept by a trigger on `stock_reservations`).
3. Accepting an order (or the customer confirming a partial accept) creates `held` reservations and does not touch on hand.
4. `collected` / `delivered` writes one `sale_online` movement per line and sets the line's reservation to `consumed` (`consumed_movement_id`). Every other terminal state releases the reservations, except `delivery_failed`, whose reservations stay `held` until the return is settled (§4.3, D-69).
5. A reversed POS sale writes `pos_reversal` movements that put the quantities back (D-08), into the batches the sale took them from (its saved allocation, rule 8). A return does the same.
6. "Mark expired" (the action of the "expired on shelf" insight, D-99) writes one `expired` movement per ticked batch (`ref_type = 'stock_batch'`, `ref_id` = the batch id) with a negative `delta` equal to min(the batch's remaining quantity, available left). The form fills the ticked batches in earliest-expiry order, so the total written off never exceeds available. It names its batches explicitly and does not use rule 8's sale order. The batch's remaining drops by that `delta`, in the same transaction; it reaches 0 only when all of it was written off. Manager and above (D-06).
7. Time at zero (the "missed sales" insight, D-99) is derived from the movement ledger: a store item is "out" on an IST calendar day when its closing stock for that whole day, the running sum of `delta` at the end of the day, is 0 or below. Today never counts. Example: an item whose last unit sold on 20 Oct (closing stock 1 on 19 Oct) closes 20 and 21 Oct at 0, so it has exactly 2 days out in 15-21 Oct. No separate history table exists.
8. **Batches are used up as stock moves (D-99; W1-05).** A sale (`sale_pos`, `sale_online`) takes from the item's stock in this order: (1) unexpired dated batches with remaining above 0, earliest `expires_on` first, ties to the oldest `received_at`; (2) undated batches, oldest `received_at` first; (3) stock in no batch, which is max(`on_hand`, 0) − Σ batch `quantity` and records nothing; (4) expired batches, earliest `expires_on` first. "Expired" means `expires_on` is before today IST, the test the "expired on shelf" insight uses. `damage`, an `expired` that names no batches and `count_correction` down keep the plain order: batches with remaining above 0, earliest `expires_on` first, ties and undated batches oldest `received_at` first with undated after dated, then stock in no batch. The allocation is written to `stock_movement_batches` in the same transaction. `pos_reversal` and returns put the quantity back through the saved allocation of the movement they reverse. A positive `count_correction` creates a batch with `found_in_count = true` and no `expires_on`. Invariants (property-tested): no batch quantity goes below 0; Σ batch `quantity` ≤ max(`on_hand`, 0); a sale uses an expired batch only when no unexpired stock remains (every unexpired batch is at 0 and no stock sits outside batches); selling a whole batch leaves it at 0 so neither "going to waste" nor "expired on shelf" lists it.

## 4. Orders

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `orders` | id, number bigint (unique, default `nextval('order_number_seq')`), display_number text (generated, see below), customer_id, store_id, ask_reply_id (nullable plain `uuid` → `ask_recipients.id`, D-35; set when the order is booked from an ask reply, D-101), fulfilment (`pickup|delivery`), status (enum below), status_reason (§4.2), status_note, subtotal_paise, discount_paise, items_after_offers_paise, item_subtotal_paise (sum of line totals after item-level offers, before the minimum-bill discount, the manual discount and credits; the D-94 base for the delivery minimum, free delivery and the minimum-bill offer), credits_used int, delivery_fee_paise, total_paise, cashback_bp int (the store's rate snapshotted at placement, 0–2000, D-124), cashback_credits int, payment_status (`unpaid|paid`), payment_method (`cash|upi|card|other|null`), address_snapshot jsonb, code_enc (bytea), code_hmac (bytea), code_attempts, code_locked_at, accept_by, partial_confirm_by, hold_until, extended_at, dispatch_by, placed_at, accepted_at, collected_at, delivery_failed_at, return_confirmed_at, return_confirmed_by (null when the system confirms), return_outcome (`returned|damaged|unconfirmed`, nullable; `unconfirmed` = auto-confirmed at 72 h, D-69), return_reminders_sent smallint (0–2), version | Status changes only through the state machine function | W2-02 |
| `order_items` | id, order_id, store_item_id, product_snapshot jsonb (name, pack, image), unit_price_paise, quantity_requested, quantity_accepted, line_total_paise, price_source (`catalogue|ask_reply`, default `catalogue`, D-101) | `price_source = 'ask_reply'` only on the line of the order's `ask_reply_id` store item: its `unit_price_paise` is the replied price, no item-level offer applies, and `line_total_paise = unit_price_paise × quantity_accepted` after a partial accept (01 §11.2) | W2-02 |
| `order_events` | id, order_id, from_status, to_status, actor_kind (`customer|staff|system|admin`), actor_id, reason, meta jsonb, created_at | Append-only timeline | W2-02 |
| `strikes` | id, subject_type (`customer|store`), subject_id, order_id, reason (`no_show|dispatch_timeout|admin_cancel`), created_at, reset_at, reset_by, reset_reason | Customer strikes feed `customer_profiles.strikes_count_30d`; store strikes lower the `store_reliability` score, and 5 or more unreset store strikes in 30 days (`store_strike_risk_threshold`) raise a `risk_flags` row, with no automatic pause (D-67). `partial_timeout` and shop cancels give no strike (D-68). Support and above can reset with a reason, audited (D-41) | W2-02 |
| `carts` | — | Client-side only (IndexedDB); server validates on checkout | — |

Order money columns (D-02, D-29):

- `subtotal_paise`: items at list price. `discount_paise`: offers and manual discounts.
- `items_after_offers_paise` = `subtotal_paise − discount_paise`. This is the **eligible amount**: after offers and discounts, before credits, without the delivery fee. The credit minimum and the credit cap use it. The delivery minimum, the free-delivery threshold and a minimum-bill offer use the **item subtotal** instead: the items total after item-level offers, before the minimum-bill discount and before credits, compared inclusively (`>=`) (D-94). Quote order: item-level offers, minimum-bill offer, delivery fee, credits.
- `total_paise` = `items_after_offers_paise − credits_used × 100 + delivery_fee_paise`. Credits never pay the delivery fee.
- `cashback_credits` = `floor((items_after_offers_paise − credits_used × 100) × cashback_bp / 1000000)`, set at `collected`/`delivered`, with the order's own `cashback_bp` (the store's rate when the order was placed, D-124), not the store's rate at that moment.
- After a partial accept all of these are recomputed; held credits are re-capped, and released in full if the eligible amount drops below the minimum (D-28).

Order code (D-04): `code_enc` is the 4-digit code encrypted with `ORDER_CODE_ENC_KEY` (only the customer's own GET decrypts it); `code_hmac` is HMAC with `ORDER_CODE_HMAC_KEY` for lookup at the counter. The shop never receives the plain code. `ux_orders_store_code_active (store_id, code_hmac) where status in ('placed','awaiting_customer','accepted','ready','out_for_delivery')` keeps codes unique among a store's open orders (generation retries on conflict).

Order number (D-23): one global sequence `order_number_seq`, no per-day reset. `display_number` = `'TZ-' || lpad(number::text, greatest(6, length(number::text)), '0')` (stored generated column; `TZ-024817`, and `TZ-1024817` once past six digits; plain `lpad` would truncate). Gaps from rolled-back transactions are acceptable.

Order status enum: `placed, awaiting_customer, accepted, ready, out_for_delivery, collected, delivered, rejected, expired, cancelled, delivery_failed`. Terminal: `collected, delivered, rejected, expired, cancelled, delivery_failed`.

### 4.1 Allowed transitions (enforced in Go and by a DB trigger)

| From | To | Who | Guard and effects |
|---|---|---|---|
| placed | accepted | staff (cashier+, D-65) / auto-accept | stock available for all accepted qty; creates `held` reservations |
| placed | awaiting_customer | staff (cashier+) | at least one qty reduced, ≥ 1 line remains; totals recomputed, held credits re-capped (D-28) |
| placed | rejected | staff (cashier+) | reason from the reject list |
| placed | expired | system | now ≥ accept_by; reason `not_accepted` |
| placed, awaiting_customer, accepted, ready | cancelled | customer | reason from the customer cancel list (`declined_partial` when declining a partial accept) |
| awaiting_customer | accepted | customer | now < partial_confirm_by; creates `held` reservations |
| awaiting_customer | cancelled | system | now ≥ partial_confirm_by; reason `partial_timeout`; no strike for anyone (D-68) |
| accepted | ready | staff (cashier+) | pickup only |
| accepted, ready | collected | staff (cashier+) | pickup; correct code; `sale_online` movements consume reservations; credits captured; cashback posted |
| accepted, ready | expired | system | **pickup only** (D-21); now ≥ hold_until; reason `no_show`; customer strike |
| accepted | expired | system | **delivery only** (D-21); now ≥ dispatch_by (`delivery_dispatch_timeout_min`); reason `dispatch_timeout`; store strike |
| accepted, ready | cancelled | staff (**manager+**, D-65) | reason from the shop cancel list (`ShopCancelReason`, D-68); no strike |
| accepted | out_for_delivery | staff (cashier+) | delivery only |
| out_for_delivery | delivered | staff (rider, cashier+) | correct code; same effects as `collected` |
| out_for_delivery | delivery_failed | staff (rider, cashier+) | **only from `out_for_delivery`** (D-22); reason from the delivery-failed list; held credits released; no cashback; sets `delivery_failed_at`; reservations stay `held` until the return is settled (§4.3) |
| placed, awaiting_customer, accepted, ready, out_for_delivery | cancelled | admin (`support`, `admin`, `super_admin`) | D-20: reason from the admin cancel list; releases reservations and held credits; a strike only if the admin picks a side (`strikes.reason = admin_cancel`); audited |

"cashier+" means any staff with access to the store; every shop transition above except staff cancel is open to cashiers (D-65). Every move into a terminal state other than `collected`/`delivered`/`delivery_failed` releases reservations and held credits in the same transaction. Any other pair is rejected with `409 invalid_transition`. A table-driven test covers every (from, to, actor) triple (11 × 11 × 4).

### 4.2 Reason values (D-60)

`orders.status_reason` and `order_events.reason` take these values (check constraint per target status; `other` needs `status_note`):

| Status | Values |
|---|---|
| rejected | `out_of_stock`, `closing_soon`, `too_busy`, `cannot_deliver_address`, `price_changed`, `other` |
| expired | `not_accepted`, `no_show`, `dispatch_timeout` |
| cancelled by customer | `changed_mind`, `declined_partial`, `too_slow`, `other` |
| cancelled by staff (`ShopCancelReason`, D-68) | `out_of_stock`, `customer_unreachable`, `closing_soon`, `other` |
| cancelled by system | `partial_timeout` (D-68) |
| cancelled by admin | `customer_request`, `store_request`, `fraud_suspected`, `technical_issue`, `other` |
| delivery_failed | `customer_unreachable`, `address_not_found`, `customer_refused`, `other` |

The check constraint for `cancelled` accepts the union of the customer, staff, system and admin lists; `order_events.actor_kind` says which list applied, and the API validates the value against the actor's list.

### 4.3 Settling a failed delivery (D-69)

The order stays `delivery_failed`; settling the return changes only the return columns, never the status.

| Event | Who | Effect |
|---|---|---|
| Goods back | staff (cashier+) | `return_outcome = returned`, `return_confirmed_at`, `return_confirmed_by`; reservations released |
| Goods damaged | staff (manager+, needs `stock.damage`; a cashier asks a manager) | `return_outcome = damaged`; one `damage` movement per line, then reservations released |
| 2 h and 24 h after `delivery_failed_at`, still unsettled | system | inbox reminder to the store's managers and owner (B-16); `return_reminders_sent` + 1 |
| 72 h after `delivery_failed_at`, still unsettled | system | auto-confirmed as returned: `return_outcome = unconfirmed`, `return_confirmed_by` null, reservations released, a `risk_flags` row raised for the store |

The 2 h, 24 h and 72 h values and the 30-day store-strike window are fixed constants (D-85), not settings. The customer no-show window is the setting `noshow_window_days`.

A River job per failed order drives the reminders and the auto-confirm; the partial index `orders (delivery_failed_at) where status = 'delivery_failed' and return_confirmed_at is null` finds stragglers after a restart. The 2 h, 24 h and 72 h values are the ones 01 sets for D-69; they are constants in code (D-85).

## 5. POS

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `pos_sales` | id, store_id, client_id (unique), device_id, device_seq, cashier_user_id, customer_id (nullable), link_method (`bill_qr|counter_code|phone_unverified`, null = no customer), link_status (`linked|pending_verification|failed`, nullable), offline_link_code (typed counter code, cleared after verification), offline_link_at (device time of the typed code), device_clock_skew_ms (at the device's last online check), item_subtotal_paise (sum of line totals after item-level offers, before the manual discount and credits; the D-94 base), subtotal_paise, discount_paise, round_off_paise, credits_used, cashback_credits, total_paise, payment_method, status (`completed|reversed`), reversed_at, reversed_by, reversal_reason, sold_at (device time), received_at (server time) | D-07: only `bill_qr` and `counter_code` touch the ledger, earn cashback or qualify a referral; `phone_unverified` only adds the customer to the shop's own customer list. `ck_pos_sales_credits_link`: `credits_used > 0` only with `link_method = 'bill_qr'`. Credit minimum is checked on `subtotal_paise − discount_paise` (D-30). D-12 offline counter code: verified at sync (within `pos_offline_link_max_hours`, skew ≤ 2 min, code unused), else `link_status = failed`. Reversal (D-08, D-74) marks the original row `reversed` within `pos_reversal_window_hours`; no separate reversal row. `round_off_paise` is automatic to the nearest ₹1, half rounds up, so it is between −49 and +50 (D-94); allowed for cashiers (D-65) | W2-04 |
| `pos_sale_lines` | id, sale_id, client_id (from the device; reused as the movement's `client_id`), store_item_id (nullable for custom line), name_snapshot, unit_price_paise, quantity, line_total_paise | | W2-04 |
| `counter_code_uses` | customer_device_id, time_step bigint, pos_sale_id, created_at | PK (customer_device_id, time_step): a counter code is accepted once (D-12) | W2-04 |
| `bill_claims` | id, store_id, pos_sale_id or draft payload, amount_paise, token_nonce, expires_at, claimed_by, claimed_at, credits_requested, status (`open|claimed|expired|cancelled`) | Bill QR | W2-04 |
| `receipt_tokens` | id, token_hash (bytea unique), pos_sale_id, expires_at, created_at | D-45: 128-bit token, 30-day expiry, `GET /v1/public/receipts/{token}`; the page shows items, totals and the store only. Owner per D-84 | W2-04 |
| `sync_cursors` | device_id, store_id, last_catalogue_seq, last_synced_at | Catalogue cursor into `catalogue_changes` (D-36) | W2-04 |
| `outbox_events` | seq bigserial, channel, payload jsonb, created_at | SSE replay only (24 h); not used for catalogue sync (D-36) | W2-05 |

`GET /v1/me/purchases` (D-45, D-75; W2-04 per D-84) lists the customer's orders and their POS sales linked by `bill_qr` or `counter_code` (`ix_pos_sales_customer_sold (customer_id, sold_at desc) where link_method in ('bill_qr','counter_code')`). Sales linked by `phone_unverified` never appear, even though they carry a `customer_id`. The list backs the "In store" tab on C-13 Orders.

## 6. Credits ledger (double-entry)

Accounts and entries; balances are derived, never edited.

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `ledger_accounts` | id, kind, owner_type, owner_id, currency (`credit`), unique (kind, owner_id) | Kinds below | W2-01 |
| `ledger_transactions` | id, kind (`spend_hold, spend_capture, spend_release, cashback, referral_reward, expiry, admin_adjustment, payout, reversal, forfeit_deletion`), ref_type, ref_id, idempotency_key (unique), created_by, memo, created_at | | W2-01 |
| `ledger_entries` | id, transaction_id, account_id, amount (bigint, signed), created_at | Σ amount per transaction = 0 (deferred constraint trigger) | W2-01 |
| `credit_lots` | id, customer_id, customer_account_id, source_transaction_id, source_kind (`cashback|referral_reward|admin_adjustment`), original int, remaining int, held int, earned_at, expires_at, expired_at | D-27: every credit **into** `customer_wallet` from cashback, a referral reward or a positive admin adjustment creates a lot. Releases and reversal restorations go back to the original lots; they never create one. `ck`: remaining ≥ 0, held ≥ 0, remaining + held ≤ original | W2-01 |
| `credit_lot_consumptions` | id, lot_id, transaction_id, hold_id (nullable), kind (`hold|capture|release|expiry|clawback|adjustment|forfeit|restore`), remaining_delta int, held_delta int, created_at | Append-only record of every change to a lot; this is how a reversal or a release finds the lots to restore (D-08, D-27) | W2-01 |
| `credit_holds` | id, customer_id, order_id (plain uuid, D-35), bill_claim_id (plain uuid, D-35), amount, status (`held|captured|released`) | `ck`: exactly one of order_id, bill_claim_id. Lot allocations are the hold's `credit_lot_consumptions` rows | W2-01 |
| `account_balances` | account_id PK, balance bigint, updated_at | Cache, updated in the same transaction; nightly job recomputes and alerts on drift | W2-01 |
| `admin_adjustment_requests` | id, customer_id, amount (signed credits), reason, requested_by, requested_at, status (`pending|approved|rejected|cancelled`), approved_by, decided_at, decision_note, transaction_id | D-05, D-73: an adjustment with \|amount\| above `admin_adjust_dual_approval_above_paise` (₹500) creates a `pending` row, for super_admin too; at or under it, it posts at once (audited) and no request row is created. Only `finance` and `super_admin` request or approve; `ck_admin_adjustment_requests_two_people`: approved_by ≠ requested_by. The `admin_adjustment` transaction posts on approval | W3-01 |

Account kinds:

- `customer_wallet` (per customer): available credits.
- `customer_held` (per customer): credits held for open orders and open bill claims.
- `store_credit` (per store): money TodayZ owes the store (spent credits in, cashback out, referral rewards in, payouts out).
- `platform_marketing` (one): funds referral rewards (goes negative = expense).
- `platform_expired` (one): receives expired and forfeited credits.
- `platform_adjustments` (one): manual adjustments.
- `platform_payouts_cash` (one): counterpart of payouts made outside the system.

Lot handling (D-27): a hold consumes lots FIFO (earliest `expires_at` first) and moves those portions from `remaining` to `held`. Capture removes them from `held`. Release returns them to `remaining` on the same lots. A lot that reaches `expires_at` while partly held expires only its `remaining`; the held portion does not expire, and if it is released after the expiry date it posts to `platform_expired` at once.

Postings (amounts in credits; + = increase of that account):

| Event | Entries | Lots |
|---|---|---|
| Hold 30 at checkout or bill QR confirm (`spend_hold`) | customer_wallet −30, customer_held +30 | FIFO: remaining −30, held +30 |
| Capture on collected/delivered or a completed bill QR sale (`spend_capture`) | customer_held −30, store_credit(store) +30 | held −30 |
| Release on any other end state, or re-cap after a partial accept (`spend_release`) | customer_held −30, customer_wallet +30 | held −30, remaining +30 on the same lots |
| Release of 30 where 8 sit on a lot that expired while held (`spend_release`) | customer_held −30, customer_wallet +22, platform_expired +8 | held −30; remaining +22 on unexpired lots |
| Cashback 24 (`cashback`) | store_credit(store) −24, customer_wallet +24 | new lot |
| Referral reward, customer referee 100 and referrer 30 (`referral_reward`) | platform_marketing −130, customer_wallet(referee) +100, customer_wallet(referrer) +30 | one new lot per wallet credited |
| Referral reward, retailer referrer of a customer (`referral_reward`) | platform_marketing −130, customer_wallet(referee) +100, store_credit(referring store) +30 | lot for the referee |
| Shop referral, new shop 100 and referrer 30 (`referral_reward`) | platform_marketing −130, store_credit(new business's primary store) +100, customer_wallet(referrer) or store_credit(referrer business's primary store) +30 | lot only if a customer is credited |
| Expiry of 12 (`expiry`) | customer_wallet −12, platform_expired +12 | remaining −12 |
| Admin adjustment +50 (`admin_adjustment`) | platform_adjustments −50, customer_wallet +50 | new lot |
| Admin adjustment −50 (`admin_adjustment`) | customer_wallet −50, platform_adjustments +50 | FIFO: remaining −50 |
| Account deletion with 40 available (`forfeit_deletion`, D-31) | customer_wallet −40, platform_expired +40 | every lot: remaining → 0 |
| POS reversal (`reversal`, D-08) | see §6.1 | see §6.1 |
| Monthly payout ₹1,840 (`payout`) | store_credit(each store) −its `payout_lines` amount, platform_payouts_cash +1840 | — |

### 6.1 Reversing a linked POS sale (D-08)

Allowed within `pos_reversal_window_hours` of `sold_at`, by manager and above. One `reversal` transaction, in this order:

1. **Credits spent on the sale (S).** The sale's hold capture rows in `credit_lot_consumptions` name the lots. Each portion goes back to its lot (`restore`, remaining +). The part E whose lot has expired since then goes to `platform_expired`. Entries: store_credit(store) −S, customer_wallet +(S − E), platform_expired +E. The store's receivable for those credits is reversed by the −S.
2. **Cashback earned on the sale (C).** Clawback K = min(C, the customer's `customer_wallet` balance after step 1); held credits are never touched and the wallet never goes negative. Entries: customer_wallet −K, store_credit(store) +K. Lots: the cashback's own lot first, then FIFO (`clawback`). The store absorbs C − K, because it funded the cashback; the platform loses nothing.
3. **Referral the sale qualified.** If the reward has not posted, the referral goes back to `pending` (`qualifying_ref_*` cleared). If it has posted, the reward stays and a `risk_flags` row is raised.
4. **Stock.** One `pos_reversal` movement per line puts the quantities back.

Example: spent 30, of which 5 came from a lot that has since expired; cashback 24; wallet after step 1 is 35. Entries: store_credit −30, customer_wallet +25, platform_expired +5, customer_wallet −24, store_credit +24.

Invariants (property-tested with `rapid` and checked nightly):

1. Every transaction sums to 0.
2. `customer_wallet` and `customer_held` are never negative (check in the posting function under `SELECT … FOR UPDATE` on the account).
3. For each customer, Σ over lots of (remaining + held) = `customer_wallet` balance + `customer_held` balance (D-27).
4. Every hold ends in exactly one of captured or released.
5. For every order with `credits_used > 0`: `items_after_offers_paise ≥ credit_min_order_value_paise`, `credits_used ≤ credit_max_per_order`, and `credits_used × 100 ≤ items_after_offers_paise − 100` (the items total never goes below ₹1). The same holds for linked POS sales on `subtotal_paise − discount_paise` (D-30). Checked against the setting values in force when the order was placed.
6. The same idempotency key never posts twice.
7. Cached `account_balances` equal Σ entries.
8. For each lot, over its `credit_lot_consumptions`: `remaining = original + Σ remaining_delta` and `held = Σ held_delta`.

## 7. Referrals

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `referrals` | id, kind (`retailer_customer|customer_customer|any_shop`), referrer_type (`customer|store|business`), referrer_id, referee_type (`customer|business`), referee_id, source (`qr_standee|link|code`), standee_id (nullable), status (`pending|qualified|rewarded|rejected|rejected_same_device`), reject_reason, device_hash, qualifying_ref_type (`order|pos_sale`), qualifying_ref_id, qualified_at, reward_transaction_id (plain uuid, D-35) | unique (referee_type, referee_id). D-13: a sign-up on a device already tied to the referrer, or to another account in the last 90 days, is `rejected_same_device` with no reward; 3+ accounts on one device raises a risk flag. The qualifying amount is the eligible amount minus credits used, ≥ `ref_qualifying_min_order_paise` (D-32); a `phone_unverified` POS sale never qualifies (D-07) | W2-03 |
| `referral_clicks` | id, code, standee_id (→ `qr_standees`, nullable; set only for a standee of the code's store, D-223), landing (`store|app`), ip_hash, ua_hash, device_hash, created_at | First touch wins within `ref_attribution_window_days` (7) from opening the link to signing up (D-32) | W2-03 |

Codes, one job each (D-26): `customer_profiles.referral_code` for customer-to-customer, `businesses.referral_code` for shop-to-shop, `stores.short_code` in the QR standee URL (retailer-to-customer). Standees carry no code.

## 8. Payouts

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `payout_statements` | id, business_id, period (date, month start), opening, credits_in, cashback_out, referral_in, adjustments, reversals, payouts_out, closing, status (`open|closed`), closed_at, payout_id | unique (business_id, period). A monthly report only; it does not move money. Closes on the 1st at 00:30 IST | W3-01 |
| `payout_statement_lines` | statement_id, store_id, kind, amount, count | Per-store breakdown | W3-01 |
| `payouts` | id, business_id, amount (credits = ₹), status (`pending|held|paid|cancelled`), hold_reason (`kyc_incomplete|risk_flag|business_suspended`), exported_at timestamptz (stamped when the row goes into a payout export CSV, D-93), computed_at, last_statement_id, utr, paid_at, paid_by, transaction_id, version | `ux_payouts_open_business (business_id) where status in ('pending','held')`: at most one open payout per business (D-09) | W3-01 |
| `payout_lines` | payout_id, store_id, amount | Per-store split used for the `payout` posting; PK (payout_id, store_id) | W3-01 |

Payout amount (D-09) = Σ `store_credit` balances of the business's stores − Σ amounts of the business's unpaid payouts (other than this one). When a statement closes: if the business has an open payout, its amount and lines are recomputed in place (`version` + 1, `computed_at`, `last_statement_id`); otherwise a new `pending` payout is created. No second payout row is ever created while one is open. An amount ≤ 0 creates no payout (an open one becomes `cancelled`). The `payout` ledger transaction posts only when it is marked `paid`.

## 9. Reviews, offers, notifications, support, risk, admin, AI

| Table | Key columns | Owner |
|---|---|---|
| `reviews` | id, store_id, customer_id, order_id (plain uuid, unique), pos_sale_id (plain uuid, unique), stars 1–5, text, tags text[] (values below), edited_until, hidden_at, hidden_by, hide_reason. `ck`: exactly one of order_id, pos_sale_id. Support and above can restore a hidden review (clears `hidden_*`, audited, D-41) | W2-06 |
| `review_replies` | review_id PK, author_user_id, text, deleted_at… | W2-06 |
| `review_reports` | id, review_id, reporter, reason, status | W2-06 |
| `offers` | id, store_id, kind (`percent|flat|bxgy|bill_min`), value, buy_qty, get_qty, min_bill_paise, scope (`store|items|category`), store_item_ids uuid[], category_id, channels (`online|pos|both`), per_customer_limit, max_redemptions, redemptions, starts_at, ends_at, status (`scheduled|live|paused|ended`), source (`manual|flash_expiry|coach_suggestion`), batch_id | W2-07 |
| `offer_redemptions` | id, offer_id, order_id (plain uuid, D-35), pos_sale_id (plain uuid, D-35), customer_id, discount_paise. `ck`: exactly one of order_id, pos_sale_id | W2-07 |
| `store_banners` | id, store_id, image_key, template jsonb (tint, headline, subline, product image), headline, target_type (`offer|items|category|store`), target_id, starts_at, ends_at, status (`pending_review|live|scheduled|paused|ended|removed`), moderation (`auto_ok|needs_review|rejected`), removed_by, remove_reason | W2-07 |
| `banner_daily` | banner_id, date, impressions, clicks, attributed_orders. PK (banner_id, date). Built from `banner_impression`/`banner_click` events; an order is attributed when it is at the banner's store within 24 h of a click by the same customer (D-60, D-77); with clicks on more than one banner, the last click wins | W2-07 |
| `coach_suggestions` | id, store_id, kind (`high|low|action|tip`), topic (`sales|stock|orders|reliability|offers|reviews|demand|setup`), title, body, numbers jsonb (the computed facts quoted), action_type (`restock|create_offer|flash_offer|add_product|mark_expired|edit_price|check_cost|count_stock|see_customers|reply_reviews|turn_on_auto_accept|update_stock|enable_delivery|none`; the first nine are the shared insight action types, D-99), action_payload jsonb, priority int, status (`new|seen|done|dismissed|expired`), generated_by (`rule|ai`), ai_draft_id (plain uuid, D-35), prompt_version, model, valid_until, created_at | W3-11 |
| `ai_drafts` | id, feature (`voice_stock` and one value per AI feature in [11-ai-features.md](11-ai-features.md)), store_id (nullable), actor_user_id, input_ref (object key or source row), input_meta jsonb (e.g. transcript, lang), prompt_version, model, output jsonb, line_outcomes jsonb (per line: `confirmed|edited|rejected|not_permitted`). For `voice_stock`, each `output` line has a kind `sale|restock|count|set_price` (D-65): `sale` records a quick POS sale with no customer (`sale_pos` movement through a `pos_sales` row, cashier+); "out of stock" is a `count` of 0; status (`draft|confirmed|discarded|failed`), confirmed_by, confirmed_at, input_tokens, output_tokens, cost_paise, latency_ms. Replaces `voice_drafts` (D-46). A draft never changes stock, money or orders by itself; confirmation writes through the owning domain with the confirmer's permissions per line (D-06) | W3-10 |
| `notifications` | id, user_id, app, kind, title_key, body_key, params jsonb, read_at, created_at | W2-05 |
| `notification_deliveries` | id, notification_id, channel (`push|sms|whatsapp`), provider_id, status (`queued|sent|delivered|failed`), error, cost_paise | W2-05 |
| `notification_prefs` | user_id, app, kind, channel, enabled | W2-05 |
| `insight_feedback` | id, store_id, user_id (plain uuid, D-35), kind (the insight kind enum in [04-api.md](04-api.md) §2.6), subject_ref text (by kind: a `store_item_id` for selling fast, sitting on the shelf, missed sales, selling below cost and priced above nearby; the master `product_id`, or `q:` plus `query_norm` when no product matches, for wanted nearby; a `stock_batches.id` for going to waste; an item pair `a:b` (item A first, see the pair rule in [01-product-rules.md](01-product-rules.md) §12) for bought together; `store` for stock check due and regulars slipping away), reason (`not_useful`), quiet_until, created_at | "Not useful" on a Demand insights row (D-99). That subject, not the whole kind, stays quiet until `quiet_until` (`coach_dismiss_quiet_days`). A "going to waste" row is dismissed by its earliest-expiring batch id and the server writes one row per batch in the row at that moment. `expired_on_shelf` is never written (it cannot be dismissed). A repeat dismissal while `quiet_until` is in the future keeps the existing `quiet_until`; after it has passed, the row is renewed. The coach reads it with `coach_suggestions.status` for per-shop detector weights (11 §2.1 step 5). Unique `(store_id, kind, subject_ref)` | W3-03 |
| `support_tickets` | id, opener_type, opener_id, order_id (plain uuid, D-35), category, status (`open|waiting|resolved|closed`), assignee, priority, sla_due_at | W2-09 (D-52; W3-08 owns the admin side) |
| `support_messages` | id, ticket_id, author, body, attachments. Owner per D-84 | W2-09 |
| `risk_flags` | id, subject_type, subject_id, rule, score, details jsonb, status (`open|cleared|confirmed`), reviewed_by | W0-03 creates the table; W3-02 owns rules and screens (D-78, see note) |
| `audit_log` | id, actor_kind, actor_id, action, subject_type, subject_id (`text`, so a setting key or an id can be the subject), before jsonb, after jsonb, ip_hash, request_id, created_at. Append-only, partitioned by month (UTC month boundaries). The app role has INSERT and SELECT only: UPDATE, DELETE and TRUNCATE are revoked on the parent, the default partition and every new partition, and triggers refuse them for every role | W0-03 |
| `partition_policies` | parent (PK), granularity (`day|month`), ahead (1–400), retention_days (nullable int; the floor `drop_old_partitions` enforces, NULL = never droppable), created_at. Owner-only: the list of partitioned tables `ensure_partitions` maintains and `drop_old_partitions` may trim. A WP that adds a partitioned table inserts its own row in its migration | W0-03 |
| `job_runs` | id (ULID), kind, class, river_job_id, trigger (`schedule|event|manual|retry|backfill|continuation`), period_key (nullable), attempt, status (`running|succeeded|failed|cancelled|skipped|discarded|missed|yielded`), scheduled_at, started_at, finished_at, duration_ms, lateness_ms, summary jsonb (counts, e.g. `{"sent":120,"skipped":3}`), note, error (scrubbed, at most 2 kB, never PII or secrets), trace_id, triggered_by (admin user, nullable, plain `uuid`, D-35), created_at. Partitioned by month on `created_at` (primary key `(id, created_at)`), with a row in `partition_policies`; the `job_runs_retention` maintenance kind drops whole months older than `job_runs_retention_days` (01 §17) by calling `drop_old_partitions('job_runs', before)`. Every scheduled, manual, retry, backfill and continuation run is logged. A continuation (D-98) is the run a `Batched` kind enqueues when it yields at a shutdown; its unique key is `<period_key>#<n>` (n = the checkpoint sequence), so River's uniqueness does not drop it while the original run is still `running`. For event-triggered kinds only failed, retried and discarded attempts are logged; successes are only counted in `job_stats_hourly`. A `missed` row is written by the watchdog. D-95, D-98 | W1-11 |
| `job_stats_hourly` | kind, hour (IST hour start), succeeded, failed, cancelled, skipped, discarded, yielded, sum_ms, max_ms, max_lateness_ms. PK (kind, hour). Upserted once per attempt by the jobs middleware (this is where event-kind successes are counted). The 7-day success rate is `succeeded / (succeeded + failed + cancelled + discarded)`; `skipped` and `yielded` are left out. D-95 | W1-11 |
| `job_controls` | kind (PK), paused bool, reason, updated_by (admin user, plain `uuid`, D-35), updated_at. A paused scheduled kind stops enqueuing; a paused event kind snoozes. `timer` kinds can never be paused (enforced in Go). D-95 | W1-11 |
| `platform_environment` | one row: env (`local|test|staging|prod`), set_at. See "Platform guards" below | W0-03 |
| `idempotency_keys` | principal, route, key (PK together), request_hash, state (`in_flight|done`), claim_id, locked_until, status_code, response_headers, response_body (AES-256-GCM), expires_at. See [04-api.md](04-api.md) §1.6 | W0-03 |
| `test_clock` | one row: offset_ms. Read only when `APP_ENV` is `local` or `test` | W0-03 |
| `platform_settings` | key PK, value jsonb, unit (`paise|bp|credits|minutes|hours|days|count|meters|bool|text`), min, max, description, updated_by, updated_at. Every key from [01-product-rules.md](01-product-rules.md). Key names are exactly those in 01 §17 (D-61). D-03: money keys end in `_paise`, percentages are basis points ending in `_bp`, credits are whole numbers (e.g. `credit_min_order_value_paise = 10000`, `credit_max_per_order = 30`, `ref_qualifying_min_order_paise = 10000`). Keys with unit `paise`, `bp` or `credits` are money settings and only `super_admin` changes them (D-64). Holds on/off flags only, never provider keys or other secrets (D-19) | W0-03 |
| `leads` | id, name, phone_enc, shop_name, city, source, created_at. Site lead form | W1-04 |
| `places` | id, kind (`pincode|locality|city`), name, names jsonb, pincode, location, bbox. Seeded from open data | W1-06 |

Review tags (D-60), check constraint on `reviews.tags`: positive `fresh_stock`, `fair_price`, `quick_service`, `friendly`, `stock_was_right`, `well_packed`; negative `item_missing`, `slow`, `rude`, `wrong_item`, `expired_item`, `overpriced`.

`risk_flags` note (D-78): Wave 2 packages raise flags (W2-03 for D-13, W2-04 for D-08 and D-12, W2-02 for D-67 store strikes and D-69 unconfirmed returns), so the table must exist before Wave 2. W0-03 creates it next to `audit_log` so any package can raise a flag; W3-02 adds the rule engine, actions and A-10/A-13.

### Platform guards (W0-03)

- **Partitions.** Partitioned tables get their partitions from `ensure_partitions(parent, from_ts)`, a `SECURITY DEFINER` function the worker's nightly `platform_maintenance` job calls as `todayz_app`. It works only on a table listed in `partition_policies`, owned by the schema owner, range-partitioned on one `timestamptz` column. Names are `<parent>_pYYYYMM` or `<parent>_pYYYYMMDD`, bounded at UTC midnight. New partitions copy the parent's privileges and TRUNCATE triggers. No Go code lists the partitioned tables (D-50). `job_runs` (W1-11) is one of them, monthly.
- **Dropping old partitions.** `drop_old_partitions(parent, before timestamptz)` is the matching owner `SECURITY DEFINER` function (W1-11's migration), under the same rules as `ensure_partitions`: it works only on a table listed in `partition_policies` with a non-NULL `retention_days`, uses qualified names and a pinned `search_path`, drops only whole partitions that end before `before`, and refuses a `before` newer than `now() - retention_days`. The `job_runs_retention` kind calls it as `todayz_app`; no DDL grant is needed.
- **Roles.** `todayz_owner` runs migrations and owns every object. `todayz_app` (API, worker, seed) gets DML through default privileges and cannot run DDL.
- **Pinned search path.** Every trigger and function the migrations define sets `search_path = pg_catalog, pg_temp` and qualifies every name, so a planted temporary table or function cannot shadow it. `TEMPORARY` is revoked from `PUBLIC` on the database.
- **`platform_environment`.** `todayz migrate up` writes the row as the owner and refuses to run when `APP_ENV` disagrees with it. The app role can only read it. `migrate down`, `seed` and the test clock refuse to run on staging or prod, when the row disagrees with `APP_ENV`, or when the row is missing.

### Asks, share cards and the site search strip (W3-16, D-101)

Rules in [01-product-rules.md](01-product-rules.md) §11.1–§11.3. W3-16 creates these in its own migrations in the 31601–31699 block (D-89).

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `asks` | id, customer_id (FK `users`), product_id (nullable, FK `products`), query_norm text, quantity int (≥ 1, default 1), need_by (`now|today|tomorrow`, nullable), geo_cell (H3 res 8), city_place_id (FK `places`, the city of the search location), radius_m, target_store_id (nullable FK `stores`; set for "Ask this shop"), recipients_count, status (`open|closed`), close_reason (`all_replied|window_ended|customer_closed`), low_yield bool (the ask was narrowed to `ask_low_yield_max_stores`), created_at, closed_at | The exact search point is used only to pick the stores and to compute each recipient's distance; it is never stored. No name or phone is copied. Unique partial `ux_asks_open (customer_id, coalesce(product_id::text, 'q:' || query_norm)) where status = 'open'` (one open ask per product, `409 ask_already_open`) | W3-16 |
| `ask_recipients` | id (the **reply id**, `ask_reply_id`), ask_id (FK `asks`, `on delete cascade`), store_id (FK `stores`), sent_at timestamptz not null (when the ask reached the store; `ask_daily_limit_store` counts these per IST day), distance_m (shown only to the customer), distance_band (`lt_1km|1_3km|3_5km|gt_5km`, the only distance the shop sees), reply_by, answer (`yes|no`, nullable until answered), price_paise (nullable), store_item_id (nullable FK `store_items`; set for a listed item, or when the shop adds the item afterwards), ready_by (nullable timestamptz), replied_at, replied_by (staff user), price_valid_until (replied_at + `ask_price_valid_min`, fixed at reply time), reported_at, reported_by | Unique (ask_id, store_id). One reply per row: a second reply is `409 invalid_transition`. `ck`: `answer = 'yes'` or `price_paise is null`; `price_paise > 0`; the MRP check (`price_paise ≤` the store item's `mrp_paise`, else the master product's `mrp_paise` when set) is done in Go in the reply transaction (`422 price_above_mrp`). A Yes on a listed item sets `store_items.stock_updated_at = now()` in the same transaction through the catalogue interface (W1-05), with no movement | W3-16 |
| `ask_store_blocks` | store_id, customer_id, created_at, ask_id (FK `asks`, `on delete cascade`) | Written by "Report this ask": that store gets no more asks from that customer. PK (store_id, customer_id); a later report by the same store on the same customer moves the row to the newer ask (`created_at`, `ask_id` updated), so a block lasts `ask_retention_days` from the latest report and is deleted with that ask. The store never sees the customer id, and the customer is never told (01 §11.2) | W3-16 |
| `share_cards` | id, store_item_id (unique, FK `store_items`), code (unique, 10 characters base62), image_key (object key `share/{code}.png`, public bucket), price_paise_rendered, rendered_at | One card per store item. The image is rendered again when `store_items.price_paise` differs from `price_paise_rendered` (checked on read and by the price-change hook). It shows the item, the shop, the price and the shop's locality, never a distance | W3-16 |
| `site_demand_marquee` | id, city_place_id (FK `places`), product_id (FK `products`), searchers (distinct signed-in `customer_id`s, ≥ `site_marquee_min_searchers`), searches_total (all searches of the window, anonymous included), rank, built_on date, status (`proposed|approved|rejected`), decided_by (admin user), decided_at | Built nightly by `site_marquee_build` from the `search_performed` events of the window that carry a `product_id` (W1-06 writes it in the event props, D-101), counting distinct signed-in `user_id`s only for `searchers` and the k floor; events with only an `anon_id` add to `searches_total` and never to `searchers`, because one person can make many anonymous ids; the city is the `places` city of the event's `geo_cell`. Approval is per city and per build: an admin approves or rejects the whole proposed list of a city, optionally after removing products; the latest approved build of a city is what `GET /v1/public/site-marquee` serves. Products in the denylisted categories (a fixed list in code, 01 §17 fixed constants) are never proposed, and the admin can leave out any other product when approving. Unique (city_place_id, built_on, product_id) | W3-16 |

`stores.asks_muted` (bool, default false) is created by W1-04 with the `stores` table and changed only through `PATCH S/settings` (`asks.manage`, audited). `orders.ask_reply_id` is created by W2-02 as a plain `uuid` (§13). Retention: the `asks_retention` maintenance kind deletes `asks` older than `ask_retention_days`; `ask_recipients` and `ask_store_blocks` go with them (`on delete cascade` on both `ask_id` FKs). `orders.ask_reply_id` is set to null by its FK (`on delete set null`), and the order keeps its lines, `price_source` and the prices it was placed at. Account deletion runs the same delete for one customer's asks (W3-16's `OnAccountDelete` hook).

### Baskets (W2-14, D-110)

Rules in [01-product-rules.md](01-product-rules.md) §11.4. W2-14 creates these in its own migrations in the 21401–21499 block (D-89).

| Table | Key columns | Notes | Owner |
|---|---|---|---|
| `baskets` | id, customer_id (FK `users`), name text (1–40 characters), version, created_at, updated_at, deleted_at (nullable), last_split jsonb (nullable: the split the last search suggested, two store ids with their line ids, kept until the basket is searched again) | Owned by the customer: every query filters on `customer_id` from the session, and another customer's basket is `404`. Soft delete: `DELETE` sets `deleted_at`, and the row is hidden from every read and from the `basket_max_saved` count. A purge job is not needed for v1; the row and its lines are deleted with the account. `ck_baskets_name` (`char_length(btrim(name)) between 1 and 40`). The limit `basket_max_saved` is checked in Go in the create transaction (`422 basket_limit_reached`, 04, D-111) | W2-14 |
| `basket_lines` | id, basket_id (FK `baskets`, `on delete cascade`), product_id (nullable, FK `products`), text text (nullable, 1–120 characters, stored normalised as search does, 01 §11), quantity int (1–99), position int, created_at | `ck_basket_lines_one_of` (exactly one of `product_id` and `text` is not null), `ck_basket_lines_quantity` (`quantity between 1 and 99`). Unique (basket_id, position). The limit `basket_max_items` is checked in Go in the same transaction as the write (`422 validation_failed` on `lines`). Lines are replaced as a set by `PATCH`, in one transaction with the version bump | W2-14 |

Retention and privacy: baskets are private to the customer and are deleted with the account (01 §16): the same transaction that anonymises `users` deletes the customer's `baskets` (their `basket_lines` go by cascade) through an `OnAccountDelete` hook W2-14 registers. No shop and no analytics table reads `baskets`; basket searches are logged as any search is (`search_log`, 01 §11.4).

## 10. Analytics tables (see [05-observability.md](05-observability.md))

| Table | Purpose | Owner |
|---|---|---|
| `events` (partitioned daily, 400-day retention) | Product events: id, name, occurred_at, received_at, app, user_id, anon_id, session_id, store_id, business_id, props jsonb, geo_cell (H3 res 8), app_version, platform | W1-07 |
| `search_log` (partitioned daily) | query_norm, results_count, radius_m, geo_cell (H3 res 8), category, store_count, product_id (nullable `uuid`, written by W1-06: the master product when the query is an exact barcode or the normalised query equals a master product's name or one of its synonyms, else null; no threshold; D-99), user_id null-able | W1-06 |
| `rollup_daily_platform` | date, dau, wau, mau, new_customers, orders by status, gmv_paise, pos_gmv_paise, credits issued/spent/expired, searches, zero_result_rate … | W3-07 |
| `rollup_daily_store` | date, store_id, orders, accepted, expired_unaccepted, no_shows, gmv, pos_sales, pos_gmv, avg_accept_seconds, stock_updates, items_listed, items_stale, views, searches_hit, conversions | W3-07 |
| `rollup_cohort_weekly` | cohort_week, kind (`customer|store`), week_n, active_count, ordering_count | W3-07 |
| `rollup_daily_search_demand` | date, geo_cell (H3 res 8, D-59), query_norm, product_id (nullable `uuid`, from `search_log.product_id`; D-99), searches, zero_results, stores_with_stock | Created, with its `product_id` column and its partial index, in W3-07's own creating migration in its 307xx block (D-89) | W3-07 |
| `rollup_daily_ask_demand` (D-101) | date, geo_cell (H3 res 8), store_id, product_id (nullable), query_norm, asks, asks_without_yes | Built nightly by `rollup_ask_demand` from `asks` and `ask_recipients`: one row per receiving store. "Wanted nearby" and "Missed sales" add `asks_without_yes` to `searches` for that store before the k ≥ 10 test, after removing an ask whose customer searched the same product in the hour before it (that search is already in `search_log`) | W3-16 |
| `rollup_item_pairs` (D-99) | store_id, store_item_a, store_item_b, computed_at, window_days, bills_both, bills_a, bills_b, bills_total, lift_bp | A is the item with more bills in the window (ties: the lower item id); see the pair rule below. One row per stored pair per store: every pair with `bills_both ≥ insight_pair_min_bills`, the top 100 by `lift_bp` | W3-07 |

Location in analytics (D-81): analytics tables store only H3 resolution-8 cells (`geo_cell`, `bigint` H3 index), never coordinates. "Wanted nearby" for a store takes the res-8 cells whose centres lie within `insight_wanted_nearby_radius_m` of the store (an H3 disk around the store's cell, trimmed by distance), sums `searches` from `rollup_daily_search_demand` over those cells for the period, and applies k ≥ 10 (`insight_min_searches`, never below 10) to that total, not to each cell. Index: `rollup_daily_search_demand (geo_cell, date)`.

Demand insights data (D-99):
- **Missed sales** takes the listed (`is_listed`) items whose closing stock was 0 on at least `insight_missed_sales_min_zero_days` whole IST days of the period (stock rule 7; today never counts). It sums `searches` from `rollup_daily_search_demand` for the item's `product_id` over the same cells and only over those days, and applies k ≥ 10 (`insight_missed_sales_min_searches`, never below 10) to the total, not to each cell. It counts searches, not people. Rows whose `product_id` is null are never used for it (they still feed "wanted nearby" by `query_norm`).
- **Wanted nearby, "No shop nearby has it":** computed at read time, not from the rollup: live, verified stores of other businesses (not this one, not another store of this business) inside the radius. The sentence is appended only when at least 10 such stores of distinct businesses are in the radius and none has `on_hand - reserved > 0` for the product. The count is used only for that decision and is never returned (D-99).
- **Bought together:** `rollup_item_pairs` is rebuilt nightly per store from `pos_sale_lines` and `order_items` of completed bills in the last `insight_pair_window_days` whole IST days ending yesterday. A bill counts once per item; `bills_total` is the store's completed bills in the same window. The pair rule is the one in [01-product-rules.md](01-product-rules.md) §12, word for word: Item A is the item of the pair with more bills in the window (ties: the lower item id) and B is the other; the share is `bills_both ÷ bills_A`. A pair fires only when `bills_both ≥ insight_pair_min_bills`, the share is at least `insight_pair_min_share_bp`, and the lift, `(bills_both × bills_total) ÷ (bills_A × bills_B)`, is at least `insight_pair_min_lift_pct` (default 150, 1.5×; 100 means no lift needed). The rollup stores the lift as `lift_bp`, lift × 10000 (21428 for 2.14×), and a pair fires when `lift_bp ≥ insight_pair_min_lift_pct × 100`. The rollup filters on the bill floor only: it stores every pair with `bills_both ≥ insight_pair_min_bills`, with its `lift_bp`, and keeps the top 100 pairs per store by lift. It does not apply the share or the lift test. A read applies the share and lift tests with the current settings, so changing `insight_pair_min_share_bp` or `insight_pair_min_lift_pct` (up or down) takes effect at once. Changing `insight_pair_min_bills` or `insight_pair_window_days` takes effect at the next nightly run or `todayz rollup`, because the stored rows were built with the old values. Index: `(store_id, lift_bp desc)`. It is a job kind of class `rollup` in the D-95 job registry ([05-observability.md](05-observability.md) §3).
- **Regulars slipping away** and **stock check due** read live tables only (`pos_sales`, `orders`, `store_items.stock_updated_at`); no rollup. A visit is a distinct IST purchase day of a phone-linked customer, with reversed POS sales excluded; walk-ins without a phone are not counted, and fewer than `insight_regular_min_linked_customers` linked customers who bought in the regular window means the kind shows only its needs-data line. "Stock check due" lists listed items with `on_hand > 0` and `stock_updated_at` older than `stock_stale_after_hours`.

## 11. Indexes that matter (checked in the DB review WP)

- `stores using gist(location) where status='live'` (queries also join `businesses.status = 'verified'`, D-40)
- `store_items (store_id) include (price_paise, on_hand, reserved)`; `store_items using gin(search)`; `products using gin(name gin_trgm_ops)`; `products using gin(barcodes)`
- `orders (store_id, status, placed_at desc)`; `orders (customer_id, placed_at desc)`; partial `orders (accept_by) where status='placed'`, `(partial_confirm_by) where status='awaiting_customer'`, `(hold_until) where status in ('accepted','ready') and fulfilment='pickup'`, `(dispatch_by) where status='accepted' and fulfilment='delivery'`; unique `ux_orders_store_code_active` (§4); unique partial `ux_orders_ask_reply_active (ask_reply_id) where ask_reply_id is not null and status not in ('cancelled','expired','rejected')` (one active order per ask reply, D-101; a violation is `409 ask_reply_used`; `delivery_failed` keeps the reply used)
- `stock_movements (store_item_id, created_at desc)`; BRIN `stock_movements (created_at)`; unique `stock_movements (store_id, client_id)`
- partial `stock_reservations (store_item_id) where status='held'`; `stock_reservations (order_id)`
- `catalogue_changes` PK (store_id, seq); BRIN on `created_at` for the 30-day purge
- `ledger_entries (account_id, created_at)`; partial `credit_lots (customer_id, expires_at) where remaining > 0 or held > 0`; `credit_lot_consumptions (lot_id)`, `(transaction_id)`, `(hold_id)`
- `pos_sales (store_id, sold_at desc)`; partial `pos_sales (customer_id, sold_at desc)` for linked sales (§5)
- unique partial `payouts (business_id) where status in ('pending','held')`
- `events (name, occurred_at)` per partition, BRIN on `occurred_at`.
- partial `orders (delivery_failed_at) where status='delivery_failed' and return_confirmed_at is null` (§4.3)
- `rollup_daily_search_demand (geo_cell, date)` (§10); partial `rollup_daily_search_demand (product_id, geo_cell, date) where product_id is not null` (D-99)
- `ask_recipients (store_id, reply_by) where answer is null` (the inbox); `ask_recipients (store_id, sent_at)` (the store's daily limit and the inbox list); `ask_recipients (ask_id)`; `ask_store_blocks (ask_id)` (the cascade); `asks (customer_id, created_at desc)`; `asks (city_place_id, product_id, created_at) where status = 'closed'` (demand gaps); `rollup_daily_ask_demand (store_id, date)`; unique `share_cards (code)`, `share_cards (store_item_id)` (W3-16, D-101)
- `baskets (customer_id, updated_at desc) where deleted_at is null` (the list and the `basket_max_saved` count); `basket_lines (basket_id, position)`; `basket_lines (product_id) where product_id is not null` (the FK index) (W2-14, D-110)
- `rollup_item_pairs (store_id, lift_bp desc)`; unique `insight_feedback (store_id, kind, subject_ref)`; `stock_movement_batches (batch_id)`; partial `stock_batches (store_item_id, expires_on) where quantity > 0` (W1-05, D-99)

## 12. Seed data

- `seed` runs as `todayz_app`, not the owner, and refuses on a staging or prod database (see "Platform guards").
- `seed dev`: 3 cities (Bengaluru, Hyderabad, Chennai), 60 stores across 6 categories with real-looking hours and pins, 3,000 master products with images from a licensed or self-shot set (placeholder initials when missing), stock for each store, 50 customers, 300 historical orders in all end states, POS sales for 30 days, reviews, referrals, credit history, 2 payout months, admin users for each role. Deterministic (fixed seed) so screenshots and tests are stable.
- Both seeds make every Demand insights kind fire in the seeded demo shop (D-99; the rows are listed in [08-testing.md](08-testing.md) §2 and the manual book README §7): items out of stock with matching searches, an expired and a near-expiry batch (and a batch that was sold in full, which neither row lists), an item priced under its last cost and a case cost entered as a unit price, items not confirmed for longer than `stock_stale_after_hours`, a bill history that pairs two items and a milk-like item that is in nearly every bill but pairs with nothing, regulars who stopped coming, and a Chennai shop priced above 10 neighbours (that last kind stays off until Q-52 is decided).
- Both seeds also hold the ask fixtures (D-101; listed in [08-testing.md](08-testing.md) §2 and the manual book README §7): open, answered, expired and booked asks at the QA shops, a muted store, a low-yield customer, a city with a demand gap of 10 customers and one of 9, and a proposed and an approved site strip list.
- `seed demo`: same plus a scripted "live" generator for investor demos (new orders every few minutes in one area).
- Test phone numbers `+91 90000 000xx` are reserved for seeds and accept OTP `000000` only when `APP_ENV` is `local` or `test`.

## 13. Deferred foreign keys (D-35)

Each column below is created as a plain `uuid` with an index. The constraint is added by a constraints migration that sorts after both tables exist. For a target from a later wave, the package that creates the target adds the FK in its own migration. For a target from the same wave, the lead writes one constraints migration at the end of that wave, after every package in it has merged (D-79): "Wave 1 constraints migration", "Wave 2 constraints migration", "Wave 3 constraints migration" below. Integration packages do not add constraints.

| Target (owner, wave) | Plain `uuid` columns | FK added by |
|---|---|---|
| `users` (W1-01, 1) | `platform_settings.updated_by`, `risk_flags.reviewed_by` (W0-03) | W1-01 |
| `users` (W1-01, 1) | `job_runs.triggered_by`, `job_controls.updated_by` (W1-11); `businesses.onboarded_by`, `businesses.verified_by`, `business_kyc.verified_by`, `business_documents.reviewed_by`, `staff.user_id`, `staff.invited_by`, `stores.reviewed_by` (W1-04); `stock_movements.actor_user_id` (W1-05) | Wave 1 constraints migration (`19901`, D-211) |
| `users` (W1-01, 1) | `product_review_queue.decided_by` (W1-05) | The package that creates `product_review_queue` (deferred, D-211) |
| `devices` (W1-01, 1) | `stock_movements.device_id` (W1-05) | Wave 1 constraints migration |
| `categories` (W1-05, 1) | `businesses.category_id` (W1-04) | Wave 1 constraints migration |
| `stores` (W1-04, 1) | `products.created_by_store_id`, `store_items.store_id`, `stock_movements.store_id`, `catalogue_changes.store_id` (W1-05) | Wave 1 constraints migration |
| `referrals` (W2-03, 2) | `customer_profiles.referred_by_referral_id` (W1-01), `businesses.referred_by_referral_id` (W1-04) | W2-03 |
| `orders` (W2-02, 2) | `credit_holds.order_id` (W2-01), `notify_order_escalations.order_id` (W2-05), `reviews.order_id` (W2-06), `offer_redemptions.order_id` (W2-07) | Wave 2 constraints migration |
| `orders` (W2-02, 2) | `support_tickets.order_id` (W2-09) | W2-09, when it creates the table (D-211) |
| `pos_sales` (W2-04, 2) | `reviews.pos_sale_id` (W2-06), `offer_redemptions.pos_sale_id` (W2-07) | Wave 2 constraints migration |
| `bill_claims` (W2-04, 2) | `credit_holds.bill_claim_id` (W2-01) | Wave 2 constraints migration |
| `ledger_transactions` (W2-01, 2) | `referrals.referrer_transaction_id`, `referrals.referee_transaction_id` (W2-03) | Wave 2 constraints migration |
| `qr_standees` (W3-09, 3) | `referrals.standee_id`, `referral_clicks.standee_id` (W2-03) | W3-09, `30901_w3-09_qr_standees.sql` (D-211, D-223) |
| `ai_drafts` (W3-10, 3) | `coach_suggestions.ai_draft_id` (W3-11) | Wave 3 constraints migration |
| `ask_recipients` (W3-16, 3) | `orders.ask_reply_id` (W2-02), `on delete set null` (D-101) | W3-16 |
