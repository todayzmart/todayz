# 01 · Product rules (single source of truth)

Everything here was decided by the founders on 2026-10-22 unless marked **default** (my recommendation, accepted with "everything else seems great"; still adjustable). Changes after the 2026-10-22 plan review are recorded in [12-decision-log.md](12-decision-log.md) (D-xx).

Numbers in `code` are `platform_settings` keys that admins can change in the admin console without a deploy. Each change is audited. §17 lists every key with its unit and default.

- Money keys end in `_paise`. Percentages are basis points and end in `_bp` (100 bp = 1%). Credits are whole numbers (1 credit = ₹1).
- All user-facing copy that contains a number reads it from settings; copy is never written with a fixed number.

## 1. Surfaces

| Surface | Who | Where | Form |
|---|---|---|---|
| Customer app | Shoppers | `app.todayz.in` | PWA, phone first; desktop shows the phone column centred |
| Business app | Shop owners, managers, cashiers | `business.todayz.in` | PWA, phone and desktop; includes the POS |
| Admin console | TodayZ staff (field agents, support, catalogue, analysts, finance, admins) | `admin.todayz.in` | Web, desktop first; field-agent screens work on phone |
| Site | Everyone (retailer marketing) | `todayz.in` | Static site, rebuild of the current todayz.in in the new design |
| API | All of the above | `api.todayz.in` | Go service |
| Storefronts | Per shop | later (see [10-later.md](10-later.md)) | — |

Languages: English (default), Hindi, Kannada, Telugu, Tamil. Asked at onboarding, changeable in settings. Only UI text is translated; product names stay as written. Light theme only.

TodayZ is free for retailers in v1. No payment gateway: customers pay the shop directly (cash, UPI to the shop, card on the shop's machine) at pickup or delivery. TodayZ records the payment method; it never claims to have processed a payment.

## 2. Accounts and identity

- Customers sign in with mobile number + SMS OTP (6 digits, 5 min validity, 5 attempts, resend after 30 s, max 5 sends per number per hour and 20 per IP per hour). Browsing never needs sign-in; sign-in is asked at checkout, and the cart survives sign-in.
- An OTP is never logged, in any environment. In `local` and `test` only, the reserved numbers `+91 90000 000xx` accept `000000`; any other number goes through the mock SMS server. Sends to reserved numbers still go to the mock SMS server; only verification is short-circuited. There is no `dev` environment.
- In `local` and `test` only, a test clock (`/v1/test/clock`) lets testers move server time for manual testing of timers and expiries. Staging and prod builds return 404 for it (D-80).
- Business users sign in the same way. One person can belong to several businesses with a role in each.
- Admin staff sign in with mobile OTP plus a TOTP authenticator app (mandatory for every admin role). Each admin gets 10 one-time recovery codes.
- Cloudflare Turnstile protects "send OTP" (invisible mode; challenge only when risky).
- Sessions: short access token (15 min) + rotating refresh token (30 days customers and business, 12 h admin). The refresh token is in an httpOnly, Secure, SameSite=Strict cookie, one per app, host-only on the API host (details in 02). "Sign out of all devices" exists for every account.
- A phone number can be a customer and a business user at once; they are separate profiles of one identity.

## 3. Businesses, stores, staff

- A business has one or more stores (branches). Stock, prices, hours, delivery settings and orders are per store.
- Business onboarding collects: brand name, store name, category, address + map pin, contact person and mobile, opening hours.
- **Verification (KYC) is per business:** `draft → submitted → verified | needs_info | rejected`, and later `suspended`.
- **Each store has its own status:** `draft | pending_review | live | paused | suspended`. A store added after the business is verified needs a quick admin review (address and photos) before it goes live. Only `live` stores of `verified` businesses appear to customers.
- **Fulfilment options per store:** `accepts_pickup` and `accepts_delivery`. With both off, the store is visible and browsable but cannot be booked, and shows "In-store only".
- Payout details (needed only before the first payout): PAN, GSTIN (optional if not registered: then PAN only), UPI ID, legal name. PAN and GSTIN format-checked; UPI ID verified by admin (v1 manual). PAN, GSTIN, UPI ID and bank details are stored encrypted; screens show a masked copy (last 4 characters).
- Staff roles per business (summary; the full permission matrix is in 02):
  - **owner:** everything, including managing all staff.
  - **manager:** everything except editing KYC or bank details, and staff management beyond adding and removing cashiers (D-66). Can view KYC masked.
  - **cashier** (D-65): sell at the POS (including voice `sale` lines), count stock, view items and orders, create voice drafts, and handle orders day to day: accept, reject, partial accept, mark ready, dispatch, mark delivered, mark delivery failed, and hand over (enter the order code). Cannot restock, record damage, change prices, give manual discounts, cancel an accepted order or reverse sales.
  - Manager and above only: cancelling an accepted order, POS reversals, manual discounts, recording damage. Customers, banners, offers, coach, review replies, batches and bulk import: owner and manager.
  - There is no way to delete a business in the app in v1; the owner asks support (D-66).
- **Codes:** each code has one job.
  - The **business referral code** is for shop-to-shop referrals.
  - The **store short code** is the one in the QR standee URL.
  - Each store gets a printed **QR standee**: `https://app.todayz.in/s/{store-slug}?r={store-short-code}&s={standee_id}`. Standees carry no code of their own; `s` only tracks which standee was scanned. Scanning opens the shop page in the customer app; a sign-up from it is the retailer's referral.

## 4. Catalogue and stock

- One shared **master catalogue** (product, brand, variant, pack size, unit, category, barcode(s), image). Each store has its own **store catalogue**: a copy of master items with its own price, MRP, stock quantity, availability, and optional batches with expiry dates.
- Adding to the master list never adds stock to any store.
- Missing products: a store can create one; it is usable in that store at once and goes to an admin review queue to be promoted to master (duplicate check by barcode and fuzzy name + pack size).
- Every change to stock on hand is a **stock movement** with a source, quantity, actor and timestamp. The source is one of:

  | Group | Sources |
  |---|---|
  | Stock in | `opening`, `restock`, `supplier_bill`, `import` |
  | Adjustments | `count_correction`, `damage`, `expired` |
  | Sales | `sale_pos`, `sale_online`, `pos_reversal` |

  Voice and AI drafts write the ordinary source of each confirmed line; there is no `voice` source.
- **Stock math:** on hand = sum of movements. Reservations are not movements: they live only in stock reservations, with a cached reserved count per store item. Available = on hand − reserved.
- Customers always see freshness: "In stock · updated 12 min ago". A store item not updated for `stock_stale_after_hours` (default 72) shows "Stock not confirmed recently".
- Low stock threshold: per item, default `low_stock_default` = 5 units. An item is low when its quantity is at or below its threshold.

## 5. Booking (online order) flow

One order = one store. Fulfilment is **pickup** or **delivery**, each only if the store accepts it (§3) and, for delivery, `feature_delivery_enabled` is on.

`feature_delivery_enabled` off is a platform kill switch: every store's effective `accepts_delivery` becomes false. A store with `accepts_pickup` false then shows "In-store only", and booking it returns `409 store_not_bookable`. Delivery orders already in progress finish normally.

- Ordering outside the store's opening hours, or while the shop is closed manually, is refused (`store_closed`).

### 5.1 Timers

| Rule | Value | Key |
|---|---|---|
| Retailer must accept or reject a new order | 10 min | `order_accept_timeout_min` |
| Accepted pickup order is held for the customer | 30 min from acceptance | `pickup_hold_min` |
| "I'm on my way" extension, once per order | +10 min | `pickup_extension_min` |
| Customer must answer a partial acceptance | 5 min | `partial_confirm_timeout_min` (**default**) |
| Delivery order must be dispatched after acceptance | 60 min | `delivery_dispatch_timeout_min` (**default**) |

Timers can be changed per platform only (not per shop) in v1. All timers run on the server; clients only display them. Changing a timer setting never moves the deadlines of existing orders; each order stores its own deadlines.

### 5.2 States

```
placed ──accept (all items)──────────────► accepted
  ├─accept partial──► awaiting_customer ──customer confirms──► accepted
  │                     ├─customer declines──► cancelled (customer, declined_partial)
  │                     └─5 min no answer──► cancelled (system, partial_timeout)   no strike
  ├─reject (reject reason)──► rejected
  ├─10 min no answer──► expired (not_accepted)
  └─customer cancels (reason)──► cancelled (customer)

Pickup
accepted ──mark ready (optional)──► ready
accepted|ready
  ├─code verified at counter──► collected            (terminal, success)
  ├─30 min (+10 if extended) passed──► expired (no_show)   → customer strike
  ├─customer cancels (reason)──► cancelled (customer)
  └─shop cancels (shop-cancel reason; manager and above)──► cancelled (shop)

Delivery
accepted ──dispatch──► out_for_delivery ──code verified by rider──► delivered (terminal, success)
accepted
  ├─60 min not dispatched──► expired (dispatch_timeout)    → shop strike
  ├─customer cancels (reason)──► cancelled (customer)
  └─shop cancels (shop-cancel reason; manager and above)──► cancelled (shop)
out_for_delivery
  └─delivery failed (reason)──► delivery_failed      (terminal)

Any non-terminal state
  └─admin cancels (admin-cancel reason)──► cancelled (admin)
```

- Terminal states: `collected`, `delivered`, `rejected`, `expired`, `cancelled`, `delivery_failed`.
- Customers can cancel any time before `collected`/`delivered`/`out_for_delivery`.
- A shop may accept an order after its closing time, until `accept_by`; `store_closed` applies to placing only.
- **Who acts for the shop** (D-65): cashiers and above accept, reject, partial accept, mark ready, dispatch, verify the code (collected or delivered) and mark delivery failed. Cancelling an accepted order needs manager or above.
- **Partial timeout** (D-68): if the customer does not answer a partial accept within `partial_confirm_timeout_min`, the order becomes `cancelled` with system reason `partial_timeout`. Nobody gets a strike.
- **Admin cancel:** `support`, `admin` and `super_admin` can cancel from any non-terminal state, with a reason from the admin-cancel list (§5.6). It releases reservations and held credits. No strike is given unless the admin assigns fault to the customer or the shop. It is audited.
- Every transition writes an `order_events` row (who, when, from, to, reason) shown on both timelines.
- **Order numbers:** one global sequence, shown as `TZ-` plus at least 6 digits, zero-padded (e.g. `TZ-024817`). No per-day reset.
- **Auto-accept** (store setting, default off): if on and every item is available, the order is accepted at once.
- **Partial accept:** the retailer reduces quantities or removes items. In `awaiting_customer` the customer sees the new totals and confirms or cancels. On confirm:
  - held credits are re-capped against the new eligible amount (§7); if it is under ₹100, all held credits are released and the order uses no credits;
  - the delivery fee is recomputed on the new item subtotal (§5.5) and the free-delivery threshold is applied again. The delivery minimum is checked only at placement.

### 5.3 Stock during an order

- On `placed`: nothing is reserved; availability is rechecked and the order is refused if any item is unavailable.
- On `accepted`: reservations are created for the accepted quantities (available drops; on hand does not).
- On `collected`/`delivered`: a `sale_online` movement is written and consumes the reservation (on hand drops).
- On `delivery_failed` (D-69): the reservation stays until the shop confirms the goods came back, or records them as `damage`. Either one releases the reservation.
  - A cashier can confirm the return. Recording damage needs manager or above; a cashier asks a manager.
  - The shop gets an inbox reminder 2 h and 24 h after the failure while it is unresolved.
  - After 72 h with no answer, the system confirms the goods as returned (`return_outcome = unconfirmed`), releases the reservation, and raises a risk flag.
- On any other terminal state: the reservation is released.

### 5.4 Strikes and shop reliability (**defaults**)

- No-show expiry applies to pickup orders only. 3 `no_show` expiries in 30 days → the customer can hold only 1 active order for 14 days (`noshow_strikes`, `noshow_window_days`, `noshow_limit_days`). Shown to the customer with the reason.
- A delivery order that expires with `dispatch_timeout` gives the shop a strike. An admin cancel gives a strike only to the side the admin assigns fault to (§5.2). A `partial_timeout` cancel gives no strike.
- **Store strikes** (D-67): strikes lower the store's `store_reliability` score. 5 or more strikes in 30 days (`store_strike_risk_threshold`) raises a risk flag. There is no automatic pause.
- Shop acceptance rate and unaccepted-expiry rate over 30 days feed search ranking (§11). Shops above 30% unaccepted expiry get a nudge in the business app and rank lower (`shop_expiry_penalty_threshold_bp`).
- `support`, `admin` and `super_admin` can reset a store's or a customer's strikes, with a reason. Audited.

### 5.5 Delivery

- Store settings: delivery on/off, radius (km), fee (₹, may be 0), free above (₹, optional), minimum order (₹), delivery hours (default = opening hours).
- **₹ thresholds are inclusive** (D-94). The delivery minimum, the free-above threshold and a minimum-bill offer are all compared with the **item subtotal**: the items total after item-level offers, before the minimum-bill discount and before credits. Delivery is free when the item subtotal is ₹X or more; the business app label reads "Free delivery on ₹X+". Credits never pay the delivery fee.
  - Order of operations in a quote: item-level offers, then the minimum-bill offer, then the delivery fee, then credits.
  - Example, assuming free delivery on ₹500+: a ₹500 cart with a "₹50 off on ₹500+" offer gets free delivery and the ₹50 off, so it pays ₹450. A ₹520 cart with a ₹30 item offer has an item subtotal of ₹490: delivery is charged and the minimum-bill offer does not apply.
- Ordering delivery outside delivery hours returns `409 delivery_closed` (with `nextDeliveryWindow` in `extensions`); pickup is still offered, and checkout (C-11) offers "Switch to pickup".
- The shop's own staff delivers. The customer's delivery code is told to the rider, who enters it in the business app (rider = staff member with cashier role or a "delivery" flag).
- Customer app shows "Delivery" on shops that offer it and only offers it if the address is inside the radius.
- `delivery_failed`: held credits are released and no cashback is issued. The goods follow §5.3.

### 5.6 Reason lists

| List | Values |
|---|---|
| Reject | `out_of_stock`, `closing_soon`, `too_busy`, `cannot_deliver_address`, `price_changed`, `other` |
| Shop cancel (`ShopCancelReason`, D-68) | `out_of_stock`, `customer_unreachable`, `closing_soon`, `other` |
| Delivery failed | `customer_unreachable`, `address_not_found`, `customer_refused`, `other` |
| Admin cancel | `customer_request`, `store_request`, `fraud_suspected`, `technical_issue`, `other` |
| Customer cancel | `changed_mind`, `declined_partial` (declining a partial accept), `too_slow`, `other` |
| System cancel | `partial_timeout` (no answer to a partial accept, D-68) |

## 6. Proving a transaction happened in the app

| Situation | Method | Rules |
|---|---|---|
| Online order, pickup | **Order code**: 4 digits, random per order, shown in the customer app as digits + QR | Valid only in `accepted`/`ready`; single use; 5 wrong tries lock it (retailer asks the customer to tap "New code", which needs the customer's signed-in app); retailer types it or scans the QR. The shop never receives the code from the API; it only types what the customer shows |
| Online order, delivery | Same order code, given to the rider | Valid only in `out_for_delivery` |
| Walk-in sale at the POS | **Bill QR** (`bill_qr`): the POS shows a QR for this bill (signed: bill id, store, amount, expires in 120 s). The customer scans it in the TodayZ app, sees "₹480 at Sri Lakshmi · use 30 credits?" and taps Confirm. The POS updates live. | One claim per bill; claim expires with the QR (POS can refresh); the customer approves credit use on their own phone |
| Walk-in, customer phone offline | **Counter code** (`counter_code`): retailer types the customer's mobile number; the customer reads out the 4-digit code the app shows, which changes every 60 s and works offline (TOTP, per-device secret) | ±1 time step accepted; 5 wrong tries per bill; earns credits; **spending credits needs the online bill QR** (**default**) |
| Walk-in, phone number only | **Phone only** (`phone_unverified`): retailer types the customer's mobile number, nothing is verified | Adds the customer to the shop's own customer list only. No cashback, no credit spending, does not qualify a referral |

- Only `bill_qr` and `counter_code` links touch the ledger. Credits are earned or spent only on an online order or a sale linked by one of them. Any other POS sale is a plain sale.
- **Counter code while the POS is offline:** the POS records the typed code and the device time with the sale. At sync, the server checks the code against the time step at the recorded time and accepts only if:
  - the sale syncs within `pos_offline_link_max_hours` (12);
  - the device clock was within 2 minutes of the server's at the device's last online check (the device records it);
  - the code has not been used before.

  If verification fails, the sale stays unlinked and the shop sees "Customer link failed". More than `pos_offline_links_risk_per_day` (20) offline links at one store in a day raises a risk flag. More than `risk_code_lockouts_store_per_day` (10) order-code lockouts at one store in an IST day also raises a risk flag.

## 7. Credits

- 1 credit = ₹1. Credits are not cash for customers: they are only spent on orders, never withdrawn.
- **Eligible amount:** the items total after offers and discounts, before credits, without the delivery fee. At the POS it is the bill after discounts. Credit rules and cashback use it. The delivery minimum, free delivery and minimum-bill offers use the item subtotal instead (§5.5), which is measured before the minimum-bill discount.
- **Spending:** at most 30 credits per order (`credit_max_per_order`), only when the eligible amount is ₹100 or more (`credit_min_order_value_paise`; copy says "₹100 or more"). The same minimum applies at the POS. Credits apply only to the eligible amount, which can never go below ₹1 after credits; they never pay the delivery fee. The customer picks how many (0 to the max) at checkout or when confirming a bill QR.
- **Lots:** every credit into a customer wallet creates a lot (cashback, referral reward, positive admin adjustment). Each lot expires on its own date.
- **Holding:** credits chosen at checkout are held when the order is placed. A hold consumes lots oldest first and marks those portions as held. The hold is captured on `collected`/`delivered` and released on any other terminal state; a released hold goes back to the same lots (no new lot). A lot that expires while held does not expire then; if the hold is released after the lot's expiry date, that portion expires at once.
- **Where spent credits go:** to the shop's credit account (customer spends 30 → shop's account +30). The shop is paid that money with its payout (§9).
- **Purchase cashback:** each store sets a cashback rate in settings (0–20%, stored as `cashback_bp` 0–2000, step 50 bp, default 0). On `collected`/`delivered` or a linked POS sale, the customer earns `floor(base_paise × cashback_bp / 1 000 000)` credits, where base = eligible amount − credits used (the delivery fee is excluded). Shown on the shop in the customer app ("5% back"). **The shop pays for its own cashback**: it is debited from the shop's credit account (**default**; may go negative, netted at payout).
- **Referral credits** are paid by TodayZ (marketing expense), see §8.
- **Expiry** (**default**): each lot expires 365 days after it was earned (`credit_expiry_days`); spending uses the oldest lots first; customers are told `credit_expiry_reminder_days` (7) days before. A lot earned on day X (IST) with `credit_expiry_days` = N is usable through day X+N and expires at 00:00 IST on day X+N+1.
- **Caps** (**default**, fraud guard): one customer can spend credits on at most 10 orders per month (`credit_orders_per_month`); one store can accept at most 200 credit redemptions per day (`store_credit_redemptions_per_day`).
  - "Day" is the IST calendar day; "month" is the IST calendar month.
  - When a cap is reached, the order can still be placed, without credits. The quote says so before the customer places it (D-76):
    - store's daily cap: "Credits can't be used at this shop today";
    - customer's monthly cap: "You've used this month's credit limit".
  - Counted when the hold is taken (when the order is placed). A cancelled or expired order releases its hold and no longer counts.
  - These are spend caps only. There are no caps on earning credits; cashback is limited per bill by the store's cashback rate (at most 20%, above), and that is what limits earning (D-62).
- **Reversing a linked POS sale** (manager and above, within `pos_reversal_window_hours`, 24):
  - Credits spent on the sale go back to the lots they came from. Any part of a lot that has expired since then expires at once.
  - Cashback earned on the sale is clawed back, up to the customer's available (not held) balance. The wallet never goes negative; the store absorbs any shortfall, because it funded the cashback.
  - The shop's credit account gives back the credits it received for the sale.
  - A referral the sale qualified: if the reward has not posted, the qualification is revoked; if it has posted, the reward stays and a risk flag is raised.
  - Stock: a `pos_reversal` movement puts the quantities back.
- **Manual adjustments** (admin console): only `finance` and `super_admin`, with a reason. The threshold `admin_adjust_dual_approval_above_paise` (₹500) is compared with the absolute value of the amount, so it applies to debits and credits alike (D-73).
  - At or under the threshold: the adjustment posts immediately, with no request row.
  - Above it: it needs a second approver who is a different person; this applies to `super_admin` too.
- Wallet history explains every line: "Spent at Sri Lakshmi · order TZ-024817", "Referral bonus for Ananya's first order", "5% back at MedPlus", "Expired".

## 8. Referrals

| Kind | Referrer | New person | Referrer gets | New person gets |
|---|---|---|---|---|
| `retailer_customer` | Retailer (QR standee or link) | Customer | 30 | 100 |
| `customer_customer` | Customer | Customer | 30 | 100 |
| `any_shop` | Anyone (customer or retailer) | New shop | 30 | 100 (to the new shop's credit account) |

Keys: `ref_<kind>_referrer_credits` and `ref_<kind>_referee_credits` for each kind above (same values for all three kinds in v1, stored per kind so they can diverge).

- A referral is **pending** at sign-up and **unlocks** when the new person completes the qualifying action:
  - a customer: their first `collected`/`delivered` order or `bill_qr`/`counter_code` POS sale whose qualifying amount (eligible amount − credits used) is at least ₹100 (`ref_qualifying_min_order_paise`);
  - a new shop: verification plus 10 completed orders or linked sales (`ref_shop_qualifying_orders`, **default**).
- Customer rewards post in the same transaction as the qualifying order outcome; shop rewards post after the 10th order (D-72).
- **Attribution:** one referrer per new person. First touch wins, within 7 days from opening the link to signing up (`ref_attribution_window_days`).
- Self-referral, a second claim, and the same phone are refused. A sign-up on a device already tied to the referrer, or to another account in the last 90 days, is recorded as `rejected_same_device` with no reward. 3 or more accounts on one device raises a risk flag.
- Rewards to a shop (as referrer or as the new shop) go to the credit account of the business's primary store and are paid out in money with payouts (§9).
  - The primary store is the business's oldest store that is `live` when the reward posts (D-72).
  - If the business has no live store at that moment, the reward waits as pending and posts when one goes live.

## 9. Retailer credit account and payouts

- Each business has a credit account per store (statement is per business, broken down by store).
- Credits in: customer credits spent at the store, referral rewards earned by the business. Credits out: cashback the store gave, payouts. Reversals (§7) and admin adjustments post here too.
- **Monthly statement** (manual payout): statements close on the 1st of each month at 00:30 IST. The system closes the previous month's statement per business: opening balance + credits in − cashback ± reversals and adjustments − payouts paid = closing balance.
- **Payout amount** = the business's store credit balance − its unpaid payouts. If the closing amount is above 0, a payout is created for finance. It is `pending` when KYC is verified, there is no open fraud flag and the business is not suspended. Otherwise it is `held` with `hold_reason` `kyc_incomplete`, `risk_flag` or `business_suspended`, and finance unholds it when the cause clears.
  - A business has at most one open payout (`pending` or `held`).
  - If the open payout is still unpaid when the next statement closes, its amount is recomputed; no second payout is created.
- Finance in the admin console reviews (fraud flags shown), pays by UPI outside TodayZ, then marks it paid with the UTR reference. Only then does the business app show "Paid". Negative balances carry forward.
- Payout holds: unverified KYC, open fraud flag, suspended business.

## 10. Reviews

- After `collected`/`delivered` (or a `bill_qr`/`counter_code` POS sale), the customer can review the store: 1–5 stars + optional text (≤ 1000 chars) + optional tags. One review per order; editable for `review_edit_window_days` (7) days; after that, an edit returns `409 review_edit_window_passed`.

  | Tags | Values |
  |---|---|
  | Positive | `fresh_stock`, `fair_price`, `quick_service`, `friendly`, `stock_was_right`, `well_packed` |
  | Negative | `item_missing`, `slow`, `rude`, `wrong_item`, `expired_item`, `overpriced` |

- Published at once. The retailer can reply once (editable). Customers and retailers can report a review to admin.
- `support`, `admin` and `super_admin` can hide a review or reply (soft delete with reason) and restore a hidden one (with reason). Both are audited.
- Store rating shown = Bayesian average (prior 4.0 with weight 5) and count, e.g. "4.4 · 23 reviews".

## 11. Search and ranking

- Customers pick location by GPS, PIN code, or locality search; it stays visible at the top and changes at any time. Radius chips: 1, 2, 5, 10, 20 km (default 2).
- Home map: shops nearby, open now, products in radius; open/closed pins; clusters.
- Search matches product name, brand, category, pack size, with typo tolerance. Results are grouped by shop.
- **Filter first:** in stock, `live` store of a `verified` business, within the radius.
- **Then score:** 0.45·text + 0.30·exp(−d / 1.5 km) + 0.10·reliability + 0.10·rating_norm + 0.05·has_offer.
  - The weights are settings (`search_w_text`, `search_w_distance`, `search_w_reliability`, `search_w_rating`, `search_w_offer`).
  - reliability comes from the shop's acceptance rate and expiry penalty (§5.4); a store with no value yet counts as 1.0.
  - Ties go to the nearer store. The customer can switch to "Nearest first".
  - There is no freshness term in the score (D-63). Stale stock is handled only by the coach and by the shop's expiry-rate penalty (§5.4). Customers still see each item's freshness (§4).
- "Has offer" is a search filter (a chip on the search screen).
- Zero-result searches are recorded (they feed "Wanted nearby" insights).

## 12. Business app features (v1)

Overview (KPI strip, sales by hour, new orders, demand insights, top items), Orders, Sell (POS), Voice update, Stock and products, Offers, Customers, Credits and referrals (credit account, statements, QR standee), Reviews, Analytics, Stores and staff, Settings (profile, hours, pickup/delivery, cashback %, auto-accept, payout details, notifications, language), Help.

- **Offers and discounts** (user, 2026-10-22: retailers add banners, offers and discounts visible to consumers):
  - Kinds: % off or ₹ off on selected items, a category, or the whole store; **buy X get Y** (same item); **minimum-bill discount** ("₹50 off on ₹500+"); **flash offer** from an expiring batch (with a countdown).
  - Each has a start/end, can be paused or ended early, and has a customer preview. Optional limits: per-customer once, first N orders, online only / POS too. "First N orders" and `max_redemptions` are used up when the order is placed and released on cancel or expiry.
  - Pricing rule: one best item-level offer per line, plus at most one bill-level offer; the total never goes below ₹1. A minimum-bill offer applies when the item subtotal (after item-level offers, §5.5) is at or above `min_bill_paise`. Offers apply in checkout quotes and on the POS identically. Offer cost is the shop's own (it is a price cut, not credits).
  - Shown to customers: strike-through price + "20% off" tag on products, an "Offers" row on the shop page, offer tags on StoreCards, the "Offers near you" strip on Home (ranked by distance and discount), and an "Offers" filter in search.
- **Banners:** a shop can publish up to 3 live banners per store (`store_banners_max`): a 16:9 image (uploaded or made from a template: colour from the tint palette, headline, sub-line, product photo), a headline (≤ 40 chars), and a target (an offer, a set of items, a category in the shop, or the shop page), with start/end dates. Banners show on the shop page header carousel and, if the shop is within the customer's radius, in the Home "Offers near you" carousel (fair rotation across nearby shops, max 1 banner per shop per screen). Published at once (like reviews); images pass an automatic safety check (AI image moderation when enabled, otherwise admin review queue before going live); admin can take any banner down with a reason. The shop's analytics show banner impressions, clicks and attributed orders. An order is attributed to a banner when the same customer orders at the same store within 24 h of clicking it; if they clicked more than one banner, the last click wins (D-77).
- **Voice update:** speak "Sold 3 Maggi", "Received 12 Amul butter 100 g", "Coca-Cola 750 ml out of stock" (English, Hindi, mixed). The app shows the interpreted change and saves only after the retailer confirms. Never updates stock from an unconfirmed interpretation.
  - Line kinds (D-65): `sale`, `restock`, `count`, `set_price`.
    - `sale` records a quick POS sale with no customer linked (a `sale_pos` movement), e.g. "Sold 3 Maggi".
    - "Out of stock" is a `count` of 0.
  - Each line is checked against the user's permission: `sale` and `count` need cashier or above; `restock` and `set_price` need manager or above. Lines the user may not confirm show disabled with "Ask a manager".
- **Demand insights:** selling fast (days of stock left), sitting on the shelf (no sale in 21 days, `insight_no_sale_days`), going to waste (batches expiring within 5 days, `insight_expiring_within_days`), wanted nearby (searches within 2 km, `insight_wanted_nearby_radius_m`, for items the store does not stock; analytics stores searches as H3 resolution-8 cells, so this adds up the cells within the radius and then applies k ≥ 10 to the total, D-81). Same row design as the old app's card (icon, label, sentence with bold product name, metric chip, "Check stock ›" link, "Past 7 days" tag). Minimum evidence before any number is shown (`insight_min_*`, see §17 and [11-ai-features.md](11-ai-features.md) §2.1); below it the card explains what data it needs.
- **POS:** search or scan (USB/Bluetooth scanner as keyboard, or camera), quantities, discounts (manual discounts: manager and above), automatic round-off to the nearest ₹1, half rounds up (₹100.50 becomes ₹101; allowed for cashiers, D-65, D-94), payment method record (cash, UPI, card, credit-to-customer "khata" **later**), link customer (bill QR / counter code / phone only, §6), works offline and syncs. Corrections are reversals (§7), never silent edits.
- **Receipts:** each POS sale has a receipt link `/r/{token}` (128-bit token, expires after 30 days) shared by WhatsApp link or SMS. The page shows items, totals and the store, and no customer data.
- **Customer purchases** (D-75): the customer's purchase list shows their online orders plus POS sales linked by `bill_qr` or `counter_code`. Sales linked by phone only never show there. In-store sales are on the "In store" tab of the Orders list (C-13).

## 13. Admin console features (v1)

Pulse (business health), Businesses (verification queue, details, suspend), Stores (store review, status), Customers, Orders (incl. admin cancel, §5.2), Master catalogue (review queue, merge duplicates, bulk import), Credits (ledger browser, manual adjustments with reason and 2-person approval above ₹500, §7), Referrals, Payouts (monthly statements, review, mark paid with UTR), Reviews (hide, restore, reports), Risk (fraud flags), Support tickets, Field agents (onboarding attribution), Notifications (templates, provider keys shown only as set/missing), Settings (`platform_settings`), Admin users and roles, Audit log.

Admin roles (D-64; summary, the full permission matrix is in 02). There are 7 roles:

| Role | Can |
|---|---|
| `field_agent` | Onboard businesses and manage their items (§14); see their own stats |
| `support` | View orders and users, cancel orders, tickets, review moderation and restore, strike reset, view risk, banner moderation |
| `catalogue` | Master catalogue review queue and merges |
| `analyst` | Read-only Pulse and dashboards |
| `finance` | Ledger; payouts (hold, unhold, mark paid, export); request and approve credit adjustments; payees' bank and UPI details, masked only. No KYC. |
| `admin` | Everything except credit adjustments, admin-user and role management, money settings, and payout hold, unhold, mark-paid and export |
| `super_admin` | Everything |

- KYC verification: `admin` and `super_admin` only.
- Pulse: every admin role except `field_agent`.
- **Money settings** are `platform_settings` keys with unit `paise`, `bp` or `credits`. Only `super_admin` can change them.

## 14. Field agents (salespeople)

- TodayZ employees with a `field_agent` admin login (phone-friendly screens). No commission.
- They create a business on the shop's behalf (shop owner confirms by OTP on their own phone), capture photos and documents, add products from the master catalogue (or create missing ones), and print/hand over the QR standee.
- They can add and edit items only for businesses they onboarded, until `field_agent_edit_window_days` (30) after that business is verified.
- Every business records `onboarded_by`; admin sees per agent: shops onboarded, verified, products added, first-order rate. Each field agent sees the same numbers for themselves only.

## 15. Notifications

Channels: in-app inbox (always), web push (VAPID, free), SMS (OTP and critical updates), WhatsApp (optional). Provider keys live only in environment variables, never in settings. Settings hold an on/off flag per channel (`notify_<channel>_enabled`); a channel works only when its flag is on and its key is set. The admin console shows each key as set or missing, never its value.

| Event | Customer | Business |
|---|---|---|
| New order | — | push + in-app + sound loop on open app; SMS if not seen in `order_unseen_sms_after_min` (3 min, **default**) |
| Accepted / partial / rejected / expired | push + in-app | — |
| Ready for pickup | push + in-app | — |
| Hold ending in `pickup_hold_reminder_min` (10) min | push | — |
| Out for delivery / delivered | push + in-app | — |
| Referral unlocked, credits earned, credits expiring | in-app (+ push) | in-app for referral unlocked and credits earned; – for credits expiring |
| Payout paid | — | push + in-app |
| New review | — | in-app |

WhatsApp, when enabled, carries the same events as the SMS column; SMS is the fallback. The "not seen" SMS goes to the store's contact phone, falling back to the owner's login phone. It is sent once per order, in the business's notification language, falling back to English.

## 16. Privacy and data

- Store the minimum: name (optional), phone, saved addresses, orders. Location is used for search, stored only as a saved address or as coarse search events (an H3 resolution-8 cell) for demand data.
- Retailers see a customer's name and masked phone (`98•••••521`) for orders. The full phone number is shown to the shop only while the order is `out_for_delivery`.
- Demand data shown to retailers or brands is aggregated and never names a person. k ≥ 10 always applies, including coach cards (e.g. "12 people near you searched…").
- Every coach statement about other people needs k ≥ 10, including benchmarks against other shops (D-71). The B-12 "nearby shops" benchmark card is an analytics card, not a coach statement; it needs k ≥ 5 stores ([05-observability.md](05-observability.md) §4).
- Analytics stores location as H3 resolution-8 cells. An area count (such as "wanted nearby") adds up the cells inside the radius first, then applies k ≥ 10 to the total (D-81).
- Account deletion on request (customer self-serve), with ledger and order records kept anonymised for accounting.
  - Deletion is blocked while the customer has an order that is not finished.
  - On deletion, the wallet balance is forfeited to TodayZ (ledger kind `forfeit_deletion`).

## 17. Settings keys

This is the complete, canonical list of `platform_settings` keys, with unit and default (D-61). Every other plan file, the seed and the code use exactly these names; a key that is not here does not exist. Store-level settings (cashback, delivery fee, radius, minimum, auto-accept, pickup/delivery on/off) live on the store, not here.

Keys with unit `paise`, `bp` or `credits` are money settings: only `super_admin` can change them (§13, D-64).

**Orders and strikes**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `order_accept_timeout_min` | minutes | 10 | §5.1 |
| `pickup_hold_min` | minutes | 30 | §5.1 |
| `pickup_extension_min` | minutes | 10 | §5.1 |
| `partial_confirm_timeout_min` | minutes | 5 | §5.1 |
| `delivery_dispatch_timeout_min` | minutes | 60 | §5.1 |
| `noshow_strikes` | count | 3 | §5.4 |
| `noshow_window_days` | days | 30 | §5.4 |
| `noshow_limit_days` | days | 14 | §5.4 |
| `shop_expiry_penalty_threshold_bp` | bp | 3000 | §5.4 |
| `store_strike_risk_threshold` | count | 5 | §5.4: store strikes in 30 days that raise a risk flag |
| `order_unseen_sms_after_min` | minutes | 3 | §15: SMS to the shop if a new order is not seen |
| `pickup_hold_reminder_min` | minutes | 10 | §15: "Hold ending" push to the customer |
| `risk_code_lockouts_store_per_day` | count | 10 | §6: order-code lockouts at one store in an IST day that raise a risk flag |

**Stock and catalogue**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `stock_stale_after_hours` | hours | 72 | §4 |
| `low_stock_default` | count | 5 | §4 |
| `store_banners_max` | count | 3 | §12 |

**Credits and payouts**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `credit_max_per_order` | credits | 30 | §7 |
| `credit_min_order_value_paise` | paise | 10000 | §7 |
| `credit_expiry_days` | days | 365 | §7 |
| `credit_expiry_reminder_days` | days | 7 | §7: customers are told this many days before a lot expires |
| `review_edit_window_days` | days | 7 | §10 |
| `credit_orders_per_month` | count | 10 | §7 |
| `store_credit_redemptions_per_day` | count | 200 | §7 |
| `admin_adjust_dual_approval_above_paise` | paise | 50000 | §7 |

**Referrals**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `ref_retailer_customer_referrer_credits` | credits | 30 | §8 |
| `ref_retailer_customer_referee_credits` | credits | 100 | §8 |
| `ref_customer_customer_referrer_credits` | credits | 30 | §8 |
| `ref_customer_customer_referee_credits` | credits | 100 | §8 |
| `ref_any_shop_referrer_credits` | credits | 30 | §8 |
| `ref_any_shop_referee_credits` | credits | 100 | §8 |
| `ref_qualifying_min_order_paise` | paise | 10000 | §8 |
| `ref_shop_qualifying_orders` | count | 10 | §8 |
| `ref_attribution_window_days` | days | 7 | §8 |

**POS**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `pos_reversal_window_hours` | hours | 24 | §7 |
| `pos_offline_link_max_hours` | hours | 12 | §6 |
| `pos_offline_links_risk_per_day` | count | 20 | §6 |

**Search ranking** (§11). Unit `bp`, therefore super_admin-only (§13), by design: ranking weights change every shop's visibility.

| Key | Unit | Default |
|---|---|---|
| `search_w_text` | bp | 4500 |
| `search_w_distance` | bp | 3000 |
| `search_w_reliability` | bp | 1000 |
| `search_w_rating` | bp | 1000 |
| `search_w_offer` | bp | 500 |

**Demand insights and coach** (§12, [11-ai-features.md](11-ai-features.md) §2.1)

| Key | Unit | Default | Rule |
|---|---|---|---|
| `insight_no_sale_days` | days | 21 | "Sitting on the shelf": no sale in this many days |
| `insight_min_days_listed` | days | 21 | Listing history needed before "sitting on the shelf" (equal to the no-sale rule) |
| `insight_expiring_within_days` | days | 5 | "Going to waste": batch expiry window |
| `insight_wanted_nearby_radius_m` | meters | 2000 | "Wanted nearby" search radius |
| `insight_min_searches` | count | 10 | Searches needed before "wanted nearby" shows (never below 10, §16) |
| `insight_min_sales_lines` | count | 20 | Sales lines in the period for share-of-sales claims |
| `insight_min_units_selling_fast` | count | 5 | Units sold before "selling fast" shows |
| `insight_min_events_for_percent` | count | 20 | Events behind any percentage shown |
| `insight_period_days` | days | 7 | Insight period ("Past 7 days") |
| `coach_max_cards_per_day` | count | 5 | New coach cards per store per day |
| `coach_max_push_per_day` | count | 1 | Coach pushes per store per day |
| `coach_dismiss_quiet_days` | days | 7 | A dismissed topic stays quiet |

**Field agents**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `field_agent_edit_window_days` | days | 30 | §14 |

**Feature flags** (turn a feature off without a deploy)

| Key | Unit | Default |
|---|---|---|
| `feature_delivery_enabled` | bool | true |
| `feature_bill_qr_enabled` | bool | true |
| `feature_voice_enabled` | bool | true |
| `feature_offers_enabled` | bool | true |
| `feature_banners_enabled` | bool | true |
| `notify_push_enabled` | bool | true |
| `notify_sms_enabled` | bool | true |
| `notify_whatsapp_enabled` | bool | false |

**AI** ([11-ai-features.md](11-ai-features.md)). A feature works only when its flag is on and its provider key is set. Code reads the model from settings and never hard-codes it.

| Key | Unit | Default |
|---|---|---|
| `ai_voice_stock_enabled` (AI-R1) | bool | false |
| `ai_billscan_enabled` (AI-R2) | bool | false |
| `ai_shelfscan_enabled` (AI-R3) | bool | false |
| `ai_insighttext_enabled` (AI-R4) | bool | false |
| `ai_coach_enabled` (AI-R5) | bool | false |
| `ai_replydraft_enabled` (AI-R6) | bool | false |
| `ai_bannercopy_enabled` (AI-R9) | bool | false |
| `ai_imagecheck_enabled` (AI-R10) | bool | false |
| `ai_listparse_enabled` (AI-C1) | bool | false |
| `ai_querynorm_enabled` (AI-C2) | bool | false |
| `ai_catalogue_enabled` (AI-A1) | bool | false |
| `ai_triage_enabled` (AI-A2) | bool | false |
| `ai_riskexplain_enabled` (AI-A3) | bool | false |
| `ai_model_fast` | text | `claude-haiku-5-5` |
| `ai_model_smart` | text | `claude-sonnet-5-5` |
| `ai_daily_calls_store` | count | 100 |
| `ai_daily_calls_customer` | count | 30 |
| `ai_daily_cost_cap_paise` | paise | 50000 (₹500 a day; AI calls stop for the rest of the IST day when reached, alert at 80%, D-70) |

**Fixed constants (code, not settings)** (D-92). These protect security, define a protocol, or feed an alert rule, so they change through code review, not in the console. Operational timings that an operator may need to tune are settings (the tables above).

| Constant | Value | Where |
|---|---|---|
| Review text length | 1000 characters | §10 |
| Bill QR validity | 120 s | §6 |
| Counter code step | 60 s, ±1 step accepted | §6 |
| Wrong tries before a code locks | 5 | §6 |
| Receipt token life | 30 days | §12, D-45 |
| Same-device window for referrals | 90 days | §8, D-13 |
| Store rating prior | 4.0 with weight 5 | §10 |
| Admin step-up validity | 10 min | 02 |
| Sync cursor life | 30 days | D-36 |
| Banner attribution window | 24 h | §12, D-77 |
| OTP limits | 6 digits, 5 min, 5 attempts, resend 30 s, 5 sends per number per hour, 20 per IP per hour | §2 |
| Device clock tolerance for offline links | 2 min | §6, D-12 |
| Voice recording length | 15 s | §12 |
| `sold_at` clamp for offline sales | 72 h | 02 §4.3 |
| Order SSE poll interval | 15 s | 02 |
| "3 or more orders expired in 2 h" alert | 3 in 2 h | 05 §5 |
| Failed-delivery reminders and auto-confirm | 2 h, 24 h, 72 h | §5.3, D-85 |
| Store-strike window (risk flag and reliability) | 30 days | §5.4, D-85 |

**Client versions** (an older client shows "Update available" and reloads)

| Key | Unit | Default |
|---|---|---|
| `min_client_version_customer` | text | `0.0.0` |
| `min_client_version_business` | text | `0.0.0` |
| `min_client_version_admin` | text | `0.0.0` |
