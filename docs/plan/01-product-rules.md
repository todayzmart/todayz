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
- Admin staff sign in with mobile OTP alone, like everyone else; there is no authenticator app and no recovery codes (D-264). After `admin_signin_max_failures` wrong admin sign-in codes for one number within `admin_signin_failure_window_hours` (counted across its codes), that number's admin sign-in is locked for `admin_signin_lock_hours`: sending and checking admin sign-in codes answer `429 signin_locked` until the lock ends or another super admin unlocks it on A-18 ("Unlock sign-in", a † action, audited). A good sign-in code clears the count. The count is kept for every number tried in the admin app, admin or not, so the lock never shows which numbers are admins; anyone who knows an admin's number can lock that admin out, and the founder accepted that (D-264). A † admin action (money, settings, admin users and roles, `money` jobs) needs a **step-up**: a fresh SMS code sent to the admin's own number and typed in the step-up dialog, valid for 10 min of that sign-in. Step-up codes follow the sign-in code's rules above (6 digits, 5 min, 5 attempts) with their own send limits: resend after 30 s between step-up codes and at most `admin_step_up_sends_per_hour` an hour per admin, apart from the sign-in limits (a step-up straight after signing in is never refused for the sign-in's sends). The fifth wrong code of one step-up request signs the admin out everywhere in the admin app. An SMS step-up does not protect against a SIM swap; the founder accepted that risk (D-264).
- Cloudflare Turnstile protects "send OTP" (invisible mode; challenge only when risky).
- Sessions: short access token (15 min) + rotating refresh token (30 days customers and business, 12 h admin). The refresh token is in an httpOnly, Secure, SameSite=Strict cookie, one per app, host-only on the API host (details in 02). "Sign out of all devices" exists for every account.
- A phone number can be a customer and a business user at once; they are separate profiles of one identity.
- **Anyone can sign up a shop** (D-274). Any signed-in phone can create a business in the business app (B-02), fill in the steps and start selling at once; no admin or field agent has to do it for them.
- **Profile photo** (D-259). A customer may add, change or remove a profile photo on Account (C-18). JPEG, PNG or WebP, at most 5 MB; the server re-encodes it as a JPEG of at most 512 px a side, so EXIF (location included) and other metadata are never stored. It shows at once (no review) and only to the customer: it is in the private bucket and reaches the app as a link valid for 15 minutes; shops, staff and other customers never see it. At most `profile_photo_uploads_per_hour` uploads per person in a rolling hour (`429 rate_limited`). Removing the photo or deleting the account deletes the file.

## 3. Businesses, stores, staff

- A business has one or more stores (branches). Stock, prices, hours, delivery settings and orders are per store.
- Business onboarding collects: brand name, store name, category, address + map pin, contact person and mobile, opening hours.
- **Live on submit, checked later** (D-274, founder 27 Oct). A business goes live (`verified`) as soon as its onboarding is filled in and submitted, and every complete store goes `live` with it. Nobody approves anything first: not the business, a store, the shop photos or GST. A store added or edited later goes live (or stays live) the same way. A store still missing a detail (address, map pin, phone, hours) stays `draft` until it is filled in.
- **Business status:** `draft → verified`, and later `suspended` or `rejected`. `submitted` and `needs_info` are kept only for rows made before D-274.
- **Shop check** (D-274). Each business also has a check status, `unchecked → checked | rejected`. A field agent, admin or super_admin works the daily check queue (A-24, newest first) and either marks the shop checked (optional note) or rejects it with a reason; both are audited. Until it is checked, the business's payouts are held (§9); the shop sells and earns credits as normal. A reject hides the shop at once (business `rejected`) and cancels its open online orders with the system reason `shop_rejected`: no strike for the customer or the shop, held stock, credits and offers are released, and each customer is told in their language. A checked shop that later goes wrong is suspended on A-03 instead.
- **Each store has its own status:** `draft | live | paused | suspended` (`pending_review` only for rows made before D-274). Only `live` stores of `verified` businesses appear to customers.
- **Fulfilment options per store:** `accepts_pickup` and `accepts_delivery`. With both off, the store is visible and browsable but cannot be booked, and shows "In-store only".
- Payout details (needed only before the first payout): PAN, GSTIN (optional if not registered: then PAN only), UPI ID, legal name. PAN and GSTIN format-checked. The documents (shop photos, GST and PAN files) are accepted as soon as they are uploaded (D-274); the shop check looks at them, and an admin may still verify the KYC or ask for something on A-03, but KYC no longer holds a payout. PAN, GSTIN, UPI ID and bank details are stored encrypted; screens show a masked copy (last 4 characters).
- Staff roles per business (summary; the full permission matrix is in 02):
  - **owner:** everything, including managing all staff.
  - **manager:** everything except editing KYC or bank details, and staff management beyond adding and removing cashiers (D-66). Can view KYC masked.
  - **cashier** (D-65): sell at the POS (including voice `sale` lines), count stock, view items and orders, create voice drafts, and handle orders day to day: accept, reject, partial accept, mark ready, dispatch, mark delivered, mark delivery failed, and hand over (enter the order code). Cannot restock, record damage, change prices, give manual discounts, cancel an accepted order or reverse sales.
  - Manager and above only: cancelling an accepted order, POS reversals, manual discounts, recording damage. Customers, banners, offers, coach, review replies, batches and bulk import: owner and manager.
  - Asks (§11.2, D-101): cashier and above read the Asks inbox, reply and report an ask (`asks.view`, `asks.reply`). Only owner and manager mute asks (`asks.manage`) or change the price on a reply (`item.price.edit`).
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
- **No item goes below zero at the counter (D-270).** The POS never sells more of an item than is available (on hand less the units blocked for accepted online orders that are not yet collected or delivered). Adding or raising a line past available opens **Update stock** instead: on hand, blocked for online orders and available, a "Counted now" field and, only when units are blocked, the box "This count includes the N units set aside for online orders" (unticked by default: on hand = count + blocked; ticked: on hand = count). Saving writes an ordinary stock count, which every role may do, cashiers included; the line then takes what the new available allows, never more. Cancel leaves the bill as it was. An online sale above available is refused (`409 insufficient_stock` with the item and its available). Offline, the device checks against the last available it synced, less what it has sold since. Damage and expiry are capped at on hand, on the device and on the server. Item tiles and search results show "N blocked for online orders" when N > 0.
- **The one race left (D-270).** Two devices selling the last unit offline both really sold it, so sync still applies both sales and on hand can end below zero. Every read then shows 0 (on hand and available) with "Count needed" until someone counts; the next count sets it right.
- Customers always see freshness: "In stock · Confirmed 12 min ago" (D-101 changed the word from "updated"; the wording rules are in §11.1). A store item not updated for `stock_stale_after_hours` (default 72) shows "Stock not confirmed recently".
- **What refreshes freshness (D-99):** `stock_updated_at` moves on any stock movement of the item (sale, restock, damage, expiry, count correction, reversal) and on a count that confirms the item with no change (no movement is written, only the timestamp moves). A **Yes** reply to an ask on a listed item refreshes it the same way, with no movement (§11.2, D-101). Price edits, listing changes and reservations do not refresh it. "Stock check due" (§12) uses the same moment and the same setting.
- **Batches are used up as stock moves (D-99):** a batch has a received quantity, a remaining quantity, an optional `expires_on`, a received time and an optional cost per sale unit as paid (GST included). Batches are created only by stock-in that carries an expiry or a cost, and by a count that finds more than the books; there is no other way to add one. A manager or above can correct a batch's cost and expiry afterwards (`PATCH`, audited); nothing else on a batch can be edited. **A sale (`sale_pos`, `sale_online`) uses stock in this order:** (1) unexpired dated batches, earliest `expires_on` first; (2) undated batches, oldest received first; (3) stock in no batch; (4) expired batches, earliest expiry first. "Expired" means `expires_on` is before today in IST, the same test as Expired on shelf, so a sale never takes an expired unit while any unexpired stock remains. `damage`, a downward `count_correction` and the expired write-off keep the plain order: earliest `expires_on` first, ties to the oldest received, undated after dated, oldest received first, and a quantity beyond the batches comes from stock in no batch. The allocation is saved on the movement. A `pos_reversal` or a return gives the quantity back to the batches it came from. A count that finds more than the books adds a batch marked "found in count" with no expiry. The "Mark expired" write-off names its batches explicitly instead of taking the earliest. Insights read a batch's remaining quantity, never the received quantity. An item with no batches is a plain quantity.
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
  - The shop gets an inbox reminder 2 h and 24 h after the failure while it is unresolved (`delivery_return_first_reminder_hours`, `delivery_return_second_reminder_hours`, D-218).
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
- A customer keeps up to `customer_max_saved_addresses` saved addresses (C-11, C-18; `422 validation_failed` on one more, D-222). One of them is the default.
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

  If verification fails, the sale stays unlinked and the shop sees "Customer link failed". More than `pos_offline_links_risk_per_day` (20) offline links at one store in a day raises a risk flag. More than `risk_code_lockouts_store_per_day` (10) order-code lockouts at one store in an IST day also raises a risk flag. An offline sale is never refused for its time: an odd device time (ahead of the server, past the 72 h clamp, or well before the device's previous sync) is kept as sold, with `sold_at` clamped as before, and raises a `pos.offline_sold_at_odd` risk flag once a day per store (D-243).

## 7. Credits

- 1 credit = ₹1. Credits are not cash for customers: they are only spent on orders, never withdrawn.
- **Eligible amount:** the items total after offers and discounts, before credits, without the delivery fee. At the POS it is the bill after discounts. Credit rules and cashback use it. The delivery minimum, free delivery and minimum-bill offers use the item subtotal instead (§5.5), which is measured before the minimum-bill discount.
- **Spending:** at most 30 credits per order (`credit_max_per_order`), only when the eligible amount is ₹100 or more (`credit_min_order_value_paise`; copy says "₹100 or more"). The same minimum applies at the POS. Credits apply only to the eligible amount, which can never go below ₹1 after credits; they never pay the delivery fee. The customer picks how many (0 to the max) at checkout or when confirming a bill QR.
- **Lots:** every credit into a customer wallet creates a lot (cashback, referral reward, positive admin adjustment). Each lot expires on its own date.
- **Holding:** credits chosen at checkout are held when the order is placed. A hold consumes lots oldest first and marks those portions as held. The hold is captured on `collected`/`delivered` and released on any other terminal state; a released hold goes back to the same lots (no new lot). A lot that expires while held does not expire then; if the hold is released after the lot's expiry date, that portion expires at once.
- **Where spent credits go:** to the shop's credit account (customer spends 30 → shop's account +30). The shop is paid that money with its payout (§9).
- **Purchase cashback:** each store sets a cashback rate in settings (0–20%, stored as `cashback_bp` 0–2000, step 50 bp, default 0). On `collected`/`delivered` or a linked POS sale, the customer earns `floor(base_paise × cashback_bp / 1 000 000)` credits, where base = eligible amount − credits used (the delivery fee is excluded). Shown on the shop in the customer app ("5% back"). An order earns at the rate its store had when the order was placed, even if the shop changes it before collection (D-124). **The shop pays for its own cashback**: it is debited from the shop's credit account (**default**; may go negative, netted at payout).
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
  - The threshold is checked against the day's total, not each adjustment alone (D-237): the sum of the absolute amounts of the same admin's adjustments to the same customer in the current IST day, including this one. Posted at-once adjustments and that admin's pending or approved requests count; rejected and cancelled requests do not.
  - At or under the threshold: the adjustment posts immediately, with no request row.
  - Above it: it needs a second approver who is a different person; this applies to `super_admin` too.
  - Across customers (D-251): when one admin's adjustments in the current IST day, summed the same way over all customers, pass `admin_adjust_daily_total_alert_above_paise` (₹5,000), a risk alert fires for the ops team. Nothing is blocked.
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
  - a new shop: checked by a field agent or admin (D-274; live alone is not enough) plus completed orders or linked sales by 10 different customers (`ref_shop_qualifying_orders`, **default**; distinct customers, not purchases, D-168).
- Customer rewards post in the same transaction as the qualifying order outcome; shop rewards post once the 10th different customer's purchase is counted (D-72, D-168).
- **Attribution:** one referrer per new person. First touch wins, within 7 days from opening the link to signing up (`ref_attribution_window_days`).
- Self-referral, a second claim, and the same phone are refused. A sign-up on a device already tied to the referrer, or to another account in the last 90 days, is recorded as `rejected_same_device` with no reward. 3 or more accounts on one device raises a risk flag.
- Rewards to a shop (as referrer or as the new shop) go to the credit account of the business's primary store and are paid out in money with payouts (§9).
  - The primary store is the business's oldest store that is `live` when the reward posts (D-72).
  - If the business has no live store at that moment, the reward waits as pending and posts when one goes live.
- A referrer whose account is blocked is not paid while blocked: their reward stays qualified and unpaid, and posts once they are unblocked. A deleted referrer's reward is void (D-168).

## 9. Retailer credit account and payouts

- Each business has a credit account per store (statement is per business, broken down by store).
- Credits in: customer credits spent at the store, referral rewards earned by the business. Credits out: cashback the store gave, payouts. Reversals (§7) and admin adjustments post here too.
- **Monthly statement** (manual payout): statements close on the 1st of each month at 00:30 IST. The system closes the previous month's statement per business: opening balance + credits in − cashback ± reversals and adjustments − payouts paid = closing balance.
- **Payout amount** = the business's store credit balance − its unpaid payouts. If the closing amount is above 0, a payout is created for finance. It is `pending` when the shop has been checked (§3, D-274), the business has somewhere to be paid (a UPI ID or a bank account), there is no open fraud flag (a risk flag on the business or one of its stores that nobody has cleared: `open` or `confirmed`, D-225) and the business is not suspended. Otherwise it is `held` with `hold_reason` `business_suspended`, `shop_unchecked`, `payout_details_missing` or `risk_flag` (the first that applies, in that order), and finance unholds it when the cause clears. `kyc_incomplete` is kept only for payouts held before D-274.
  - A held `shop_unchecked` payout stays held after the check until finance releases it on A-11; a close never lifts a hold, and later closes no longer hold for it. The business app shows "Payouts start after our team checks your shop" on Credits and statements while the business is unchecked.
  - A business has at most one open payout (`pending` or `held`).
  - If the open payout is still unpaid when the next statement closes, its amount is recomputed; no second payout is created.
- Finance in the admin console reviews (fraud flags shown), pays by UPI outside TodayZ, then marks it paid with the UTR reference. Only then does the business app show "Paid". Negative balances carry forward.
- Payout holds: shop not checked yet, no payout details, open or confirmed risk flag, suspended business.

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
- Every recorded search carries a `product_id` when the query is an exact barcode or the normalised query equals a master product's name or one of its synonyms; otherwise it is empty. There is no fuzzy threshold and no setting. "Missed sales" counts only searches with a `product_id`; "Wanted nearby" groups the others by normalised query text (D-99).

### 11.1 Proof on results, refine chips, voice and share (D-101)

- **Freshness label.** A result card, a shop catalogue row and the product page show "Confirmed {time} ago" while the store item's `stock_updated_at` is within `stock_stale_after_hours` ("just now" under a minute, then minutes, hours or days, in the app's language). A Yes to an ask (§11.2) refreshes it and reads the same. After that the item shows "Stock not confirmed recently" (§4), unchanged.
- **Badges.** Labels only: they never change the order of results (the score above) or the filters. A card carries at most one badge, and each badge goes to at most one card of the result page the customer sees (the first page of the current search). Ties go to the nearer store. When one card wins two badges it shows the first in the order Lowest price, Closest, Ready soonest, and the badge it does not show goes to no other card.
  - **Closest:** the card nearest to the search location.
  - **Lowest price:** the lowest total for the customer's chosen mode. With the Pickup chip, the item price after item-level offers. With the Delivery chip, that price plus the store's delivery fee for one unit (0 when that price reaches the store's free-delivery threshold, §5.5). With no mode chosen, each store counts with its cheaper mode: the item price when it accepts pickup, otherwise price plus fee. Credits never count. In-store-only stores (§3) never get it.
  - **Ready soonest: not shown on search results in v1.** No store has a pickup prep time or a delivery ETA (03 `stores`), so there is nothing true to compare. It shows only among ask replies (§11.2), on the Yes with the earliest "ready by". TODO for the founder (Q-56): add a prep time per store, or keep it ask-only.
  - The card of a store that delivers shows its fee, "+₹20 delivery", when the Delivery chip is on or the store is delivery-only (nothing when the fee is 0).
- **Summary line.** Above the results: "Checked {N} shops nearby · {M} have it · from ₹{min}". N is the `live` stores of `verified` businesses inside the selected radius, open or closed. M is how many of them have at least one matching item in stock (they are the shops in the results). The minimum is the lowest item price after item-level offers among the results. With M = 0 the line is hidden and "Ask shops nearby" (§11.2) shows instead. It needs no k floor: it counts shops whose listings the customer can already see, not people (§16).
- **Refine chips.** Words in a typed or spoken query that are filters become chips above the results: a top price ("under ₹200"), open now, pickup, delivery, offers ("on offer", "discount"), near me, and a brand that is in the master catalogue. Each chip has a remove (×); removing it runs the search again without that filter, and the words it came from do not come back into the query. The chips map to the search filters (04 §2.3); "offers" sets `hasOffer`, "near me" sets "Nearest first". "Deliver by 7" sets the Delivery chip only: v1 has no delivery-time filter (TODO for the founder, D-101).
  - A deterministic parser runs first, with patterns for all 5 languages and their digits (Latin, Devanagari, Kannada, Telugu and Tamil numerals).
  - **Search assist** (AI-C2). The model never runs inside search: results come from the synonym table and the parser only, and the search response carries `refineHint: true` when the parser found no chip in a query of at least `refine_ai_min_words` words, or when the synonym table had no match for the words left after the chips and nothing else recognised them: no listing in the search, no active master product with those words, not a barcode, and not a search for one product (D-238). A query the table does not know but the shops or the catalogue do, such as a brand name, gets no hint. Never with `parse=false`. Only then, only after the results have rendered, and only when the customer is signed in and `ai_querynorm_enabled` is on, the app calls the search assist endpoint (04 §2.8). The model call has a timeout (§17 fixed constants); on a timeout, a provider error, the daily cap or output that fails validation, the customer sees nothing and the results stay as they are. Its output shows as suggestion chips plus "Search for '{query}'" under the summary line; nothing applies until the customer taps one, which runs the search again with that filter (`parse=false`). Each call counts against `ai_daily_calls_customer`. It writes no `ai_drafts` row (nothing is saved, so there is nothing to confirm later) and never touches stock, money or orders ([11-ai-features.md](11-ai-features.md) §3). Anonymous customers get the synonym table and the parser only.
- **Voice search** (AI-C5). A mic in the customer search bar, shown only when `ai_voicesearch_enabled` is on, the speech provider key is set, the device is online and the customer is signed in (each use counts against `ai_daily_calls_customer`). Otherwise the mic is hidden, never shown broken.
  - Speech goes to the speech-to-text provider (Sarvam, `SARVAM_API_URL`) in the app's language plus English, at most the voice recording length (§17 fixed constants).
  - The transcript goes back to the app only and fills the search box. The customer can edit it, and the search runs only when they tap search: voice never submits by itself.
  - Nothing of a voice search is stored: no audio, no transcript and no `ai_drafts` row, in the database, object storage, logs or traces. When the customer taps search, the query they confirmed runs as a normal search and is logged exactly as a typed query (`source=voice`); when they send an ask from it, the ask stores that confirmed query. A voice search the customer abandons leaves no text anywhere.
  - At the daily cap the mic is disabled with "Voice search is used up for today. Type instead." (`429 ai_daily_cap_reached`).
  - When the provider fails or times out, the customer sees "Voice search isn't working right now. Type instead." and the mic stays hidden for the rest of the session.
  - The business app has no voice search in v1 (D-101).
- **Share.** "Share" on a result card and on the product page. On Android it opens a small TodayZ sheet with WhatsApp first, then "More" (the system share sheet) and "Copy link"; elsewhere it opens the system share sheet. A web app cannot reorder the system sheet itself.
  - Text: "{item} at {shop}, ₹{price} · TodayZ" and the link.
  - The link is `https://app.todayz.in/x/{code}`: one share card per store item, opening that item's product page (C-08) with `ref=share`. It needs no sign-in.
  - A card is created only for an item of a `live` store of a `verified` business (D-40); otherwise `410 share_card_unavailable`. Reading a card whose store is no longer `live` (paused, suspended, closed, or its business no longer `verified`) returns the same `410`, and the link opens "This shop isn't on TodayZ right now" with a search box. One IP or anonymous device can create at most `share_card_create_per_hour` cards an hour (`429 rate_limited`); creating the card for the same store item again returns the existing one and does not count.
  - The link preview shows the item, the shop, the price and the shop's area (locality). It never shows a distance, which depends on the viewer. The preview image is rendered once per store item, kept in object storage and rendered again when the price changes.
  - A share gives no reward in v1. `ref=share` is not a referral (`r=` is, §3): it only records the share and the open in events, with no PII.

### 11.2 Ask shops nearby (D-101)

A customer who cannot find an item in stock asks the shops near them, and the shops answer in the business app with one tap. Nobody is phoned.

- **Where it starts.** (1) A search with no in-stock result, or none confirmed within `stock_stale_after_hours`: the empty or stale state offers "Ask shops nearby". (2) Every product page (C-08) offers "Ask shops nearby" for that product. (3) A shop page (C-07) offers "Ask this shop": the same flow, sent to that one store.
- **Who can ask.** A signed-in customer (sign-in verifies the phone, §2), at most `ask_daily_limit_customer` asks per IST day (`429 ask_limit_reached`), and one open ask per product (or per normalised query text when there is no product) at a time (`409 ask_already_open`). A blocked customer cannot ask.
- **The ask.** A master product, or free text when there is none; free text goes through the same normalisation as search (§11). A quantity (default 1) and an optional "need by": now, today or tomorrow. The customer's name and phone never go with it.
- **Which shops get it.** `live` stores of `verified` businesses within `ask_radius_m` of the customer's search location, at most `ask_max_stores`.
  - Left out: stores closed now (hours or manual close), stores that have already received `ask_daily_limit_store` asks this IST day, stores that have muted asks, and stores that reported this customer.
  - Order: stores that list the product (in any stock state) first, then stores that list an item in its category, then the rest; nearest first inside each group.
  - "Ask this shop" ignores the radius and the order but keeps every exclusion; a store that fails one refuses the ask (`409 ask_store_unavailable`).
  - A customer never learns why a store was left out. "Ask this shop" shows on every `live` shop page, and the refusal is the same response and the same copy, "This shop is not taking asks right now.", whether the store is closed, muted, at its daily limit or reported this customer. The recipients count and the progress line never say that a store was skipped.
  - No store qualifies: `409 ask_no_shops`, and the customer sees "No shops nearby can take an ask right now."
- **What the shop sees.** A push and a card in the new Asks inbox (B-20): the item, the quantity, "need by" and a distance band (under 1 km, 1–3 km, 3–5 km, over 5 km). Never the customer's name, phone, address or exact distance.
- **The reply.** One tap, within `ask_reply_window_min` of the ask reaching the store; after that the ask closes for that store.
  - **Yes**, with a price. For a listed item the price is prefilled from its listed price. Only owner and manager may change it (`item.price.edit`, the rule for prices in §3); a cashier sends the listed price. TODO for the founder (Q-57): may a cashier edit it? The price may be above the MRP (D-245). An optional "ready by" time: today, or up to the end of tomorrow (IST) when the ask's "need by" is tomorrow; a later time is `422 validation_failed`.
  - **No.**
  - A Yes on a listed item confirms its stock: it refreshes `stock_updated_at` the way a count that confirms with no change does (§4), and writes no movement. When available stock is below the asked quantity the form says "You have {n}" and still allows Yes; a reply never changes stock.
  - A Yes on an item the shop does not list offers "Add it to your stock", which opens the normal add-item form prefilled (owner and manager, `item.manage`). Nothing is created until the shop saves. On an unlisted item only owner and manager enter a price (any price, D-245); a cashier's Yes on an unlisted item carries no price.
  - Cashier and above can reply (`asks.reply`). A store answers an ask once: a second reply is `409 invalid_transition`.
- **What the customer sees.** Live progress on the ask (C-26): "Asked 8 shops · 3 replied · best ₹38", updated over the same SSE stream as orders, with the same polling fallback (02 §5). Each Yes card shows the shop name, distance, price, "ready by" and "Replied 4 min ago"; No replies are counted, not listed. The customer gets one push for an ask, at the first Yes (§15).
- **Booking from a reply.** "Book" on a Yes card opens the normal checkout (C-11) for that store item with the asked quantity.
  - **One booking per reply.** A reply books at most one active order: while an order placed from it is in any state other than `cancelled`, `expired` or `rejected`, a second order from the same reply is refused (`409 ask_reply_used`), and of two placed at the same moment exactly one succeeds. A `delivery_failed` order keeps the reply used. Once that order is cancelled, expired or rejected, the reply can book again while its price is valid.
  - **The replied price.** It holds for `ask_price_valid_min` from the reply. Within it, the reply line is priced at the replied price for each unit, and the quote says so (`priceSource: ask_reply`) with the time it ends (`validUntil`). The line's quantity is at most the asked quantity (`422 validation_failed`); more units are a separate booking at the normal price. The replied price never stacks with an offer: no item-level offer applies to that line, whether it would be lower or higher. Bill-level offers, the delivery fee and credits work as usual on the bill, and the reply line counts in the bill subtotal (§5.5, §7). The customer who sees a lower normal price can book the item normally from the product page instead. TODO for the founder (Q-53): binding or only indicative, and no stacking; the plan takes binding with no stacking.
  - **Validity is checked at placement.** When the price ends between the quote and the order, the order is refused with `409 ask_price_expired` and the fresh quote at the current price in `extensions`; the customer confirms the new total with a second tap, as for `price_changed` (04 §1.4). A booking is never re-priced silently. After the price ends the card says "Price may have changed" and Book opens checkout at the current price.
  - **A partial accept** (§5.2) keeps the replied unit price: the line total is the replied price times the accepted quantity, and the bill is recomputed as for any partial accept.
  - A Yes without a store item (unlisted, not added yet) has no Book button until the shop adds the item; then Book follows the rules above. A Yes without a price shows "Ask the shop for the price" and books at the catalogue price, with item-level offers as usual; the quote shows that price (`priceSource: catalogue`), and the one-booking rule still applies. An in-store-only store (§3) still gets asks, since the customer can walk in; its Yes card shows "In-store only: visit the shop" and has no Book button, and an order from its reply through the API is refused (`409 store_not_bookable`).
  - The order records `ask_reply_id`, and the reply line records `price_source` (`catalogue` or `ask_reply`). Stock, availability and the store's hours are checked at placement as for any order (§5.3); a reply never reserves stock.
- **When an ask closes.** When every store has answered, when the last reply window ends, or when the customer closes it. A closed ask takes no replies (`409 ask_closed`). Booking from a Yes stays possible while its price is valid.
- **Unanswered demand.** An ask that got no Yes counts as one search, for each store that received it, in "Wanted nearby" and "Missed sales" (§12), so the same k ≥ 10 floors apply. If the same customer searched for the same product in the hour before the ask, the search and the ask count once.
- **Demand gaps (admin, A-23).** A list for the field team: products asked by at least `ask_gap_min_customers` distinct customers in one city in the last `ask_gap_window_days` days with no Yes. It is aggregated, shows a count and the city, and never names or links a customer.
- **Abuse controls.**
  - A customer whose last `ask_low_yield_after_count` asks in a row got no Yes is not blocked, but their asks go to at most `ask_low_yield_max_stores` stores until one of their online orders reaches `collected` or `delivered` (placing an order is not enough; a POS sale does not count).
  - The shop can "Report this ask" for as long as the ask is kept (`ask_retention_days`), open or closed, once per ask (a second report is `409 invalid_transition`). It raises a `risk_flags` row with rule `ask_reported` for the customer (§13 Risk, A-13), and that store gets no more asks from that customer (a block, kept as long as the ask it came from).
  - Owner and manager can mute asks for a store (`asks.manage`, store setting `asks_muted`, audited). Muted stores get no asks.
  - The usual per-IP and per-user rate limits apply (02).
- **Kept.** Asks and their replies are kept for `ask_retention_days`, then deleted; the demand rollups keep only cells and counts. Deleting an ask deletes its replies and the store blocks that came from reporting it, so a block lasts `ask_retention_days` from the ask. An order booked from a reply keeps its lines and prices; only its link to the reply is cleared. Deleting the customer's account deletes their asks, replies and blocks the same way (§16).
- **Not in v1.** WhatsApp delivery of asks to shops (`ask_whatsapp_enabled`, off; a Wave 5 option because each message costs money, Q-55). AI voice calls to shops: rejected (TRAI rules on automated calls, cost, shop goodwill, and they go around the shop's own app). Asks are free for shops; a fee per ask (lead generation) is not planned (TODO for the founder, Q-54).

### 11.3 Site search sections (D-101)

- **Rotating hero line** on the site home (S-01): "Just ask and find {item}". The items come from a fixed, curated list in each language's i18n bundle, not live data.
- **"What people near you are searching for"** strip (S-07). A nightly job builds, per city, the master products searched by at least `site_marquee_min_searchers` distinct signed-in customers (distinct `customer_id`s; searches by anonymous devices add to the totals but never count towards this floor, since one person can make many anonymous ids) in the last `site_marquee_window_days` days, at most `site_marquee_max_items`. Only master product names, never free-text queries; a denylist of categories (§17 fixed constants) is applied before an admin sees the list, and the admin can leave out any product; nothing can identify a person.
  - An admin approves the list for a city in A-23 before it shows. Anything not approved never shows.
  - Until a city's list is approved, or when the API cannot be reached, the strip shows the curated hero list.
  - The visitor picks the city in the strip; there is no IP location.
- **Comparison section:** "Search engines, chatbots and directories vs TodayZ", written for India (JustDial-style directories), in the site's copy style. English source keys only; the other languages follow the i18n process.

### 11.4 Saved baskets and basket search (D-110)

A customer who wants several things builds a basket, and search finds a shop that has the whole list, or the best two-shop split.

- **Saved baskets.** A signed-in customer keeps up to `basket_max_saved` baskets on their account (`422 basket_limit_reached` when one more is created, D-111). A basket has a name of at most 40 characters and up to `basket_max_items` lines (`422 validation_failed` on `lines`). A line is a master product or free text, with a quantity of 1 to 99. Free text goes through the same normalisation as search (§11, §11.2). A basket can be edited, renamed, deleted and searched again at any time.
- **Signed out.** A signed-out customer builds one unsaved basket on this device only, like the cart. Signing in offers to save it. Browsing and basket search never need sign-in (§2).
- **Where it starts.** "Add to basket" on a search result, the product page (C-08) and the shop page (C-07), and the basket screen (C-22).
- **Basket search.** It uses the customer's location and radius chips (§11) and the same filter as search: in stock, a `live` store of a `verified` business, within the radius. It is read-only and never calls a model.
  - **Covered.** A line is covered when the store lists that product (for free text, the top search match in that store) with `available` at least the quantity.
  - **Ranking.** Stores that cover every line first, then more lines covered, then the lower basket total, then the nearer store. The total uses store prices and item-level offers through `offers.Price`; credits never count. At most `basket_results_max` stores are shown.
  - **Result card.** "{n} of {m} · {distance} · ₹{total}", what is missing, and the freshness label of each line (§11.1).
  - **Badges.** At most one per card, labels only, the rules of §11.1: **Has everything** (a store that covers every line), **Closest** and **Lowest price**. They never change the order.
  - **Summary line.** "Checked {N} shops nearby · {k} have everything". N is counted as in §11.1; k is the stores that cover every line.
  - **Preview only.** The total is a preview. Checkout reprices and rechecks stock as usual (§5.3, §5.5).
- **No full match.** When no store covers every line, the results show the best partial matches and one suggested split across at most `basket_split_max_shops` stores (at most 2).
  - It picks the pair that covers the most lines, then the lower combined total, then the lower combined distance. Each line goes to the cheaper of the two stores that cover it.
  - An order has one store (§5), so a split is two orders. "Fill cart" fills the cart for the first store. After that order is placed, its tracking page (C-12) offers "Next: the rest from {shop}", which fills the cart for the second. The basket keeps the split until it is searched again.
  - `basket_split_max_shops` = 1 turns splits off. A full match always wins over a split, even when it costs more (TODO for the founder, Q-59).
  - Lines no store covers show "Ask shops nearby" (§11.2), prefilled.
- **Fill cart.** One tap puts the covered lines, at their quantities, into the cart for that store. The cart stays one store. Replacing a non-empty cart for another store asks first, as adding from another shop does.
- **Not in v1.** Phone calls to shops (D-101), splits across three or more stores, delivery for a split (each order follows its own pickup or delivery rules), sharing a basket with another customer, and reminders on a saved basket (Q-60). A shop never sees that a customer keeps baskets (Q-61); basket searches feed "Wanted nearby" and "Missed sales" like any search (§11, D-99).
- **Privacy.** Baskets are private to the customer and are deleted with the account (§16).

## 12. Business app features (v1)

Overview (KPI strip, sales by hour, new orders, demand insights, top items), Orders, Asks (§11.2, D-101), Sell (POS), Voice update, Stock and products, Offers, Customers, Credits and referrals (credit account, statements, QR standee), Reviews, Analytics, Stores and staff, Settings (profile, hours, pickup/delivery, cashback %, auto-accept, mute asks, payout details, notifications, language), Help.

- **Offers and discounts** (user, 2026-10-22: retailers add banners, offers and discounts visible to consumers):
  - Kinds: % off or ₹ off on selected items, a category, or the whole store; **buy X get Y** (same item); **minimum-bill discount** ("₹50 off on ₹500+"); **flash offer** from an expiring batch (with a countdown).
  - Each has a start/end, can be paused or ended early, and has a customer preview. Optional limits: per-customer once, first N orders, online only / POS too. "First N orders" and `max_redemptions` are used up when the order is placed and released on cancel or expiry.
  - Pricing rule: one best item-level offer per line, plus at most one bill-level offer; the total never goes below ₹1. A minimum-bill offer applies when the item subtotal (after item-level offers, §5.5) is at or above `min_bill_paise`. Offers apply in checkout quotes and on the POS identically. Offer cost is the shop's own (it is a price cut, not credits). Rounding and limits (D-150): a ₹ off offer takes its value off each unit; a percent floors in paise and is at most 90; buy X get Y gives Y free per complete group of X + Y; offer terms cannot be edited after creation (make a new offer); expiry is by the clock, not by the status label.
  - Shown to customers: strike-through price + "20% off" tag on products, an "Offers" row on the shop page, offer tags on StoreCards, the "Offers near you" strip on Home (ranked by distance and discount), and an "Offers" filter in search.
- **Banners:** a shop can publish up to 3 live banners per store (`store_banners_max`; live, scheduled and held-for-review banners all count, D-152): a 16:9 image (uploaded or made from a template: colour from the tint palette, headline, sub-line, product photo), a headline (≤ 40 chars), and a target (an offer, a set of items, a category in the shop, or the shop page), with start/end dates. Banners show on the shop page header carousel and, if the shop is within the customer's radius, in the Home "Offers near you" carousel (fair rotation across nearby shops, max 1 banner per shop per screen). Published at once (like reviews); images pass an automatic safety check (AI image moderation when enabled, otherwise admin review queue before going live); admin can take any banner down with a reason. The shop's analytics show banner impressions, clicks and attributed orders. An order is attributed to a banner when the same customer orders at the same store within 24 h of clicking it; if they clicked more than one banner, the last click wins (D-77).
- **Voice update:** speak "Sold 3 Maggi", "Received 12 Amul butter 100 g", "Coca-Cola 750 ml out of stock" (English, Hindi, mixed). The app shows the interpreted change and saves only after the retailer confirms. Never updates stock from an unconfirmed interpretation.
  - Line kinds (D-65): `sale`, `restock`, `count`, `set_price`.
    - `sale` records a quick POS sale with no customer linked (a `sale_pos` movement), e.g. "Sold 3 Maggi".
    - "Out of stock" is a `count` of 0.
  - Each line is checked against the user's permission: `sale` and `count` need cashier or above; `restock` and `set_price` need manager or above. Lines the user may not confirm show disabled with "Ask a manager".
- **Demand insights (D-99):** the B-03 card is titled "Demand insights" with the line "What sells, what sits, what is about to go to waste, and what people nearby want" and a "Last 7 days" tag (`insight_period_days`). Owner and manager only. Every row has a tinted icon, a label, one sentence with the product name in bold, and a text action specific to the kind. The action opens its target screen prefilled (the deep links are defined once in [06-screens.md](06-screens.md)) and applies nothing by itself. There is no metric chip.
  - **Card and tab (changed from the first D-99 wording, which put a "what data it needs" line on the card).** The card shows only kinds that fire: the top 4 rows by severity, at most one per kind. If no kind fires, it shows "Nothing needs you right now" with the setup checklist. "All insights" opens the Insights tab in B-12: every kind, grouped, each row with its action. A kind that has no row and is below its minimum evidence (`insight_min_*`, see §17 and [11-ai-features.md](11-ai-features.md) §2.1) shows, in the tab only, a "what data it needs" line instead of a number, never a guess.
  - **Not useful** quiets one subject, not the kind: that subject stays quiet for that shop for `coach_dismiss_quiet_days` and feeds the same per-shop weights the coach uses. The subject is the store item (Selling fast, Sitting on the shelf, Missed sales, Selling below cost, Priced above nearby); the master product, or the normalised query text when no product matches (Wanted nearby); the batch (Going to waste, where the row's subject is its earliest-expiring batch and every batch in the row is quieted together); the pair `a:b` (Bought together); or the store (Stock check due, Regulars slipping away). Dismissing again while the subject is still quiet changes nothing. **Expired on shelf has no "Not useful"**: it is a food-safety row and goes away only when the batch is written off or reaches 0.
  - **Windows.** Every window is whole IST calendar days ending yesterday; today's partial day is never counted. "Last 7 days" is the seven days before today.
  - **Severity ladder.** High: Expired on shelf, Going to waste, Selling below cost, and Selling fast when available ÷ pace is 2 days or less. Medium: the other Selling fast rows, Missed sales, Stock check due, Regulars slipping away. Low: Sitting on the shelf, Wanted nearby, Bought together, Priced above nearby. Inside a severity, kinds go in the order of the table below; rows of the same kind go by the money or units at stake, larger first. The "2 days" is a fixed constant (§17), not a setting.

  | Kind | Fires when | Sentence | Action (opens, prefilled) |
  |---|---|---|---|
  | Selling fast | At least `insight_min_units_selling_fast` units sold in the period and available ÷ (sold ÷ `insight_period_days`) is below `insight_cover_days` days | "{item} sold N this week. At this pace you run out on Saturday." The run-out date is the IST date of now plus available ÷ pace; it shows as a weekday when it is within 7 days, otherwise as a date such as "on 3 Nov". "this week" reads "in 14 days" and so on when `insight_period_days` is not 7 | **Restock →**: restock form with the quantity `ceil(sold × insight_cover_days ÷ insight_period_days) − available`, never below 0 (whole-number maths) |
  | Sitting on the shelf | In stock, no sale in `insight_no_sale_days` | "{item}: 12 in stock (₹480 at cost), no sale in 21 days." N is the number of whole days since the last sale (since the item was listed when it never sold). The value at cost shows only when the cost is known: it reads the latest batch with remaining above 0 that has a `cost_paise`; a batch with no cost is skipped | **Create offer →**: offer form with the item and a % inside the backend's safe limits (AI-R4 when on) |
  | Going to waste | Batches with remaining above 0 that expire within `insight_expiring_within_days` from today (today included, not yet expired) | "N items expire within 5 days: A, B, C." Up to 3 names, earliest expiry first, then "+ N more"; with one item "1 item expires within 5 days: A." The number follows `insight_expiring_within_days` | **Flash offer →**: flash offer form with the expiring batches |
  | Wanted nearby | Searches within `insight_wanted_nearby_radius_m` in the period for a product the store does not list, at least `insight_min_searches` | "{item}: N searches within 2 km this week." plus "No shop nearby has it." only when at least 10 other live verified stores of distinct businesses are inside the radius and none lists the product in stock; otherwise only the search count. The number of competing shops is never shown | **Add product →**: add product, prefilled from the master catalogue match |
  | Missed sales | A listed item (`is_listed`) whose closing stock was 0 or below at the end of the IST day on at least `insight_missed_sales_min_zero_days` days of the period, and at least `insight_missed_sales_min_searches` (never below 10) nearby searches matched the product on those days (searches, not people; cell rule, D-81) | "{item} was out of stock 2 days; 18 searches nearby matched it." A day counts as a day at 0 when the closing stock at the end of that IST day is 0 or below (an offline POS sale can drive stock negative, see the stock rules in [03-data-model.md](03-data-model.md)). Today never counts as a day at 0 | **Restock →**: restock form with the quantity `ceil(sold × insight_cover_days ÷ insight_period_days) − available`, the same formula as Selling fast, never below 0; `sold` is the item's units sold in the period |
  | Expired on shelf | A batch with remaining above 0 has `expires_on` before today | "{item}: 6 expired yesterday. Remove them from the shelf." 6 is the batch's remaining quantity | **Mark expired →**: stock adjustment `expired` (manager and above), prefilled with the ticked batches, earliest expiry first, each at min(batch remaining, available left), so the total never exceeds available. No "Not useful" |
  | Selling below cost | Selling price (GST-inclusive) below `cost_paise` of the latest batch with remaining above 0 **that has a cost** (a batch with no cost is skipped); `cost_paise` is the cost per sale unit as paid, GST included. No such batch: the item does not fire | "{item} sells at ₹140, last cost ₹146." When the cost is at or above the MRP: "Check the cost entry for {item}." (a cost that high is probably a case price entered as a unit price); that row ranks last in its kind | **Edit price →**: price editor with a suggested price equal to the cost rounded up to a whole rupee, never above the MRP. On the "Check the cost entry" row, **Check cost →** opens the edit form for that batch (cost and expiry, `PATCH`; manager and above), not the price editor |
  | Stock check due | Listed items on hand above 0 whose `stock_updated_at` is older than `stock_stale_after_hours`, the same moment customers start to see "Stock not confirmed recently" (§4) | "14 items not confirmed in 3 days. Customers see them as 'Stock not confirmed recently'." (one item: "1 item not confirmed in 3 days. Customers see it as 'Stock not confirmed recently'.") "3 days" is `stock_stale_after_hours` ÷ 24 when that is a whole number of days, otherwise "N hours" | **Count stock →**: count mode with those items |
  | Bought together | A pair that passes the pair rule below | "1 of 4 bills with {A} also have {B}." N is `bills_A` ÷ `bills_both`, rounded to the nearest whole number | **Create offer →**: offer form with the two items prefilled: a bill or category offer, or a banner (buy X get Y is same-item only, see Offers above) |
  | Regulars slipping away | At least `insight_regular_min_linked_customers` phone-linked customers bought in the regular window, and customers with at least `insight_regular_min_visits` visits in `insight_regular_window_days` whose last visit was at least `insight_regular_lapse_days` ago (last visit date on or before today minus the lapse days) | "8 regular customers haven't bought in 3 weeks." A visit is a distinct IST purchase day; reversed POS sales are excluded; walk-in sales with no linked phone are not counted. Below the minimum of linked customers the tab shows "Link customers at the counter to see this" | **See customers →**: B-09 filtered to them. No messaging in v1 |
  | Priced above nearby | Off by default (`insight_price_compare_enabled`, Q-52). The store's price is above the median listed price of the same product across at least 10 other live verified stores of distinct businesses within the radius (k ≥ 10 stores, D-71) | "{item}: ₹62 here, ₹58 typical nearby." | **Edit price →** |

  **Pair rule (Bought together).** Item A is the item of the pair with more bills in the window (ties: the lower item id) and B is the other; the share is `bills_both ÷ bills_A`. A pair fires only when `bills_both ≥ insight_pair_min_bills`, the share is at least `insight_pair_min_share_bp`, and the lift, `(bills_both × bills_total) ÷ (bills_A × bills_B)`, is at least `insight_pair_min_lift_pct` (default 150, 1.5×; 100 means no lift needed). The window is `insight_pair_window_days` whole IST days ending yesterday; `bills_total` is the store's bills in that window. The rollup stores the lift as `lift_bp`, lift × 10000 (21428 for 2.14×), and a pair fires when `lift_bp ≥ insight_pair_min_lift_pct × 100`. The nightly rollup stores every pair with `bills_both ≥ insight_pair_min_bills` and keeps the top 100 per store by lift; it does not filter on share or lift. The share and lift tests run when insights are read, so changing `insight_pair_min_share_bp` or `insight_pair_min_lift_pct` takes effect at once. Changing `insight_pair_min_bills` or `insight_pair_window_days` takes effect at the next nightly run or `todayz rollup`. Pairs rank by lift. A "milk with everything" pair has a lift near 1× and does not fire.

  Wording that follows a setting: "this week" reads "in N days" (and the tag "Last N days") when `insight_period_days` is not 7; "within 2 km" follows `insight_wanted_nearby_radius_m` ("within 500 m" below 1 km); "3 weeks" reads "N days" when `insight_regular_lapse_days` is not 21; "3 days" reads "N hours" when `stock_stale_after_hours` is not a multiple of 24.

  Wanted nearby: analytics stores searches as H3 resolution-8 cells, so this adds up the cells within the radius and then applies k ≥ 10 to the total (D-81). The same facts feed the coach (one computation, [11-ai-features.md](11-ai-features.md) §2.1). Nothing names another shop or shows a customer's phone (B-09 rules apply); a percentage needs `insight_min_events_for_percent`.
- **POS:** search or scan (USB/Bluetooth scanner as keyboard, or camera), quantities, discounts (manual discounts: manager and above), automatic round-off to the nearest ₹1, half rounds up (₹100.50 becomes ₹101; allowed for cashiers, D-65, D-94), payment method record (cash, UPI, card, credit-to-customer "khata" **later**), link customer (bill QR / counter code / phone only, §6), works offline and syncs. Corrections are reversals (§7), never silent edits.
- **Receipts:** each POS sale has a receipt link `/r/{token}` (128-bit token, expires after 30 days) shared by WhatsApp link or SMS. The page shows items, totals and the store, and no customer data.
- **Customer purchases** (D-75): the customer's purchase list shows their online orders plus POS sales linked by `bill_qr` or `counter_code`. Sales linked by phone only never show there. In-store sales are on the "In store" tab of the Orders list (C-13).

## 13. Admin console features (v1)

Pulse (business health), Shop checks (A-24: the daily check queue, D-274), Businesses (details, check status, suspend), Stores (status), Customers, Orders (incl. admin cancel, §5.2), Master catalogue (review queue, merge duplicates, bulk import), Credits (ledger browser, manual adjustments with reason and 2-person approval above ₹500, §7), Referrals, Payouts (monthly statements, review, mark paid with UTR), Reviews (hide, restore, reports), Risk (fraud flags), Demand (A-23: demand gaps and the site search strip, §11.2, §11.3, D-101), Support tickets, Field agents (onboarding attribution), Notifications (templates, provider keys shown only as set/missing), Settings (`platform_settings`), Jobs (A-21: every background job, its runs and controls, D-95), System (A-22: AWS and database health, read-only, D-97), Admin users and roles, Audit log.

Admin roles (D-64; summary, the full permission matrix is in 02). There are 7 roles:

| Role | Can |
|---|---|
| `field_agent` | Onboard businesses and manage their items (§14); work the shop check queue: check or reject (A-24, D-274); see their own stats |
| `support` | View orders and users, cancel orders, tickets, review moderation and restore, strike reset, view risk, banner moderation; read jobs (A-21) |
| `catalogue` | Master catalogue review queue and merges |
| `analyst` | Read-only Pulse and dashboards; read jobs (A-21) |
| `finance` | Ledger; payouts (hold, unhold, mark paid, export); request and approve credit adjustments; payees' bank and UPI details, masked only. No KYC. Reads the `money` class on A-21 only. |
| `admin` | Everything except credit adjustments, admin-user and role management, money settings, payout hold, unhold, mark-paid and export, and pausing or running `money` jobs |
| `super_admin` | Everything |

- KYC verification: `admin` and `super_admin` only. It no longer gates going live or payouts (D-274).
- Shop check (A-24, D-274): `field_agent`, `admin` and `super_admin` read the queue and check or reject a shop (audited). Every agent sees the whole queue, not only the shops they onboarded.
- Pulse: every admin role except `field_agent`.
- Demand (A-23, D-101): demand gaps are read by `field_agent`, `catalogue`, `analyst`, `admin` and `super_admin`; the site search strip list is approved by `catalogue`, `admin` and `super_admin` (audited).
- **Money settings** are `platform_settings` keys with unit `paise`, `bp` or `credits`. Only `super_admin` can change them.

**Support tickets** (D-226). A business owner or manager opens a ticket from B-15 (cashiers cannot); a signed-in customer opens one from C-19, optionally on one of their own orders. Each ticket has a category, a status (`open`, `waiting`, `resolved`, `closed`) and a priority (`normal`, `high`, `urgent`); every new ticket starts at `normal`.
- `support`, `admin` and `super_admin` work the inbox (A-14): assign (to themselves or another of these roles), reply, add internal notes, change the priority, resolve and close. Assign, priority, resolve and close are audited; resolve and close take a reason.
- **SLA.** While a ticket is `open`, support owes a reply, due `support_sla_<priority>_min` minutes after the ticket was opened or reopened (the setting at that moment). A further message from the opener while it is still `open` keeps the due time, so writing again never pushes support's deadline back. An agent's reply moves it to `waiting` and stops the timer. Changing the priority of an `open` ticket moves its due time to when the timer started plus the new priority's minutes, so it can be overdue at once; on a `waiting` or `resolved` ticket it only sets the minutes the next timer uses; a `closed` ticket keeps its priority. Past the due time the inbox shows it as overdue; nothing escalates or changes on its own. The opener is never shown a reply time.
- The opener's reply on a `waiting` or `resolved` ticket reopens it (`open`, a new due time). A `closed` ticket takes no more messages (`409 invalid_transition`); the opener opens a new one.
- A customer sees only the tickets they opened as a customer, and support's messages without an agent's name; a business sees only its own tickets. Internal notes are never shown to the opener. Attachments (JPEG, PNG, WebP or PDF, at most 10 MB each, at most 5 per message) are private files, shown through short-lived links.
- **Limits** (D-226). One person may have at most `support_open_tickets_max` tickets `open` or `waiting` at once (`422 support_ticket_limit_reached`), and may open at most `support_tickets_per_hour` tickets and ask for at most `support_uploads_per_hour` uploads in any rolling hour (`429 rate_limited`). An upload is claimed by one message: of its uploader, of the same business (none for a customer or an agent) and, for an agent's upload, on the ticket it was asked for. The file must be in the bucket with the declared size and type when the message is posted (`422` otherwise). An upload no message has claimed within `support_upload_ttl_hours` is refused, and the `support_uploads_sweep` job (`maintenance`) deletes it, row and file.
- **Account deletion** (§2). Deleting a customer account closes their open customer tickets (reason `account_deleted`), replaces the subjects and the customer's own messages on their customer tickets with a neutral placeholder, and deletes the customer's attachments on them, rows and files. Support's replies and notes stay. A business ticket stays with the business.

**Background jobs** (D-95). Every background job is a registered kind with a class: `timer` (order and hold expiries), `money` (ledger, payouts, credit expiry), `notify`, `marketing`, `ai`, `rollup` or `maintenance` (partitions, retention).
- `timer` kinds can never be paused or run by hand: order correctness depends on them.
- `money` kinds: `super_admin` only, with an SMS step-up (within 10 min, D-264) and a reason.
- Every other kind: `admin` and `super_admin`, with a reason.
- Read access to A-21: `admin`, `super_admin`, `analyst` and `support` (all classes); `finance` (the `money` class only). `field_agent` and `catalogue` have none.
- Every pause, resume, run-now and retry writes an audit row.
- `marketing` and `notify` kinds take their caps and quiet rules from §15 and §17. A job adds no rule of its own.
- `ai` kinds only write drafts a person confirms (11).
- A paused scheduled kind stops enqueuing; a paused event kind snoozes its jobs.
- System (A-22) is read-only and visible to `admin` and `super_admin` (D-97).
- Kinds added by D-101: `asks_close_window` (`timer`: closes each store's reply window and the ask), `asks_retention` (`maintenance`: deletes asks older than `ask_retention_days`), `rollup_ask_demand` and `site_marquee_build` (`rollup`, nightly).

## 14. Field agents (salespeople)

- TodayZ employees with a `field_agent` admin login (phone-friendly screens). No commission.
- Shops can sign themselves up (§2, D-274); agents help those who want it. They create a business on the shop's behalf (shop owner confirms by OTP on their own phone), capture photos and documents, add products from the master catalogue (or create missing ones), and print/hand over the QR standee.
- They can add and edit items only for businesses they onboarded, until `field_agent_edit_window_days` (30) after that business is verified. They edit only the items they added themselves; an item the shop added is the shop's (D-232).
- Once the owner gives the agent their code, the business goes live and every draft store with nothing missing goes `live` at once, so the owner need not open the app to submit. A store still missing a detail stays draft for the owner or the agent to finish (D-241, D-274).
- Every day they work the shop check queue (A-24, phone-friendly): visit or look at each new shop, then mark it checked (releases its payouts) or reject it with a reason (hides the shop, cancels its open online orders; §3, D-274).
- The owner's code follows the sign-in OTP rules (§2). The per-number send limit counts the sign-in codes sent to that number too. One agent may send at most `field_owner_codes_per_agent_hour` owner codes in any rolling hour (`429 rate_limited`). No invite or code goes to a number whose account is blocked (`422` on the owner number, the same answer as any number that cannot own a business).
- A field photo counts only once the agent's app confirms it is stored. One never confirmed within the upload URL's life plus 5 minutes does not count toward the 5-per-kind cap (on the business app either), and the hourly `business_field_photos_sweep` job (`maintenance`) deletes its file and marks it rejected (D-232).
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
| New ask (§11.2) | — | push + in-app (Asks inbox); no SMS |
| A shop replied Yes to your ask (§11.2) | push for the first Yes of an ask only, in-app for every Yes | — |

WhatsApp, when enabled, carries the same events as the SMS column; SMS is the fallback. The "not seen" SMS goes to the store's contact phone, falling back to the owner's login phone. It is sent once per order, in the business's notification language, falling back to English.

## 16. Privacy and data

- Store the minimum: name (optional), phone, saved addresses, orders. Location is used for search, stored only as a saved address or as coarse search events (an H3 resolution-8 cell) for demand data.
- Retailers see a customer's name and masked phone (`98•••••521`) for orders. The full phone number is shown to the shop only while the order is `out_for_delivery`.
- A customer sees the store's contact phone on their own order while it is not in a terminal state (§5.2), never in search, store pages, baskets or asks, and never the owner's or a staff member's own phone (D-220).
- Demand data shown to retailers or brands is aggregated and never names a person. k ≥ 10 always applies, including coach cards (e.g. "12 people near you searched…").
- Every coach statement about other people needs k ≥ 10, including benchmarks against other shops (D-71). The B-12 "nearby shops" benchmark card is an analytics card, not a coach statement; it needs k ≥ 5 stores ([05-observability.md](05-observability.md) §4).
- "Priced above nearby" insights compare a shop only with the median of at least 10 other live verified stores of distinct businesses, and "No shop nearby has it." shows only when at least 10 such stores are in the radius and none lists the product in stock. Neither names or counts a shop (D-71, D-99). Insight rows and their `facts` never contain a customer's phone number; the "Slipping away" list on B-09 shows phones masked as in R-16.2.
- Analytics stores location as H3 resolution-8 cells. An area count (such as "wanted nearby") adds up the cells inside the radius first, then applies k ≥ 10 to the total (D-81).
- Account deletion on request (customer self-serve), with ledger and order records kept anonymised for accounting.
  - Deletion is blocked while the customer has an order that is not finished.
  - On deletion, the wallet balance is forfeited to TodayZ (ledger kind `forfeit_deletion`).
  - On deletion, the customer's asks are deleted with their replies and the store blocks that came from them (D-101, §11.2); orders booked from a reply keep their lines, only the link to the reply is cleared.
  - On deletion, the customer's saved baskets and their lines are deleted (D-110, §11.4).
- **Asks (D-101).** An ask a shop sees is the customer's own request, sent at their choice; it carries the item, quantity, "need by" and a distance band, never the customer's name, phone, address or exact distance. A customer is never told that a store blocked them. Unanswered asks used as demand data follow the k ≥ 10 rules above. Demand gaps (A-23) need at least `ask_gap_min_customers` (never below 10) distinct customers. Asks, their replies and their blocks are deleted after `ask_retention_days`, or when the account is deleted.
- **Search proof (D-101).** The summary line "Checked N shops · M have it" counts stores, not people, and only stores whose listings the customer can already see, so it needs no k floor. A share link and its preview carry no customer data and never a distance, and a card of a store that is no longer live stops resolving.
- **Site search strip (D-101).** Built only from master product names searched by at least `site_marquee_min_searchers` (never below 10) distinct signed-in customers in a city (anonymous devices never count towards the floor), after a denylist, and shown only after an admin approves it. Free-text queries never reach the site.
- **Voice search (D-101).** Audio is sent to the speech provider and never stored. The transcript goes back to the app only and is never stored; only the query the customer confirms is kept, when they run the search (as a typed query is) or send an ask from it. Search assist (AI-C2) stores nothing either.

## 17. Settings keys

This is the complete, canonical list of `platform_settings` keys, with unit and default (D-61). Every other plan file, the seed and the code use exactly these names; a key that is not here does not exist. Store-level settings (cashback, delivery fee, radius, minimum, auto-accept, pickup/delivery on/off, asks muted) live on the store, not here.

Keys with unit `paise`, `bp` or `credits` are money settings: only `super_admin` can change them (§13, D-64).

Every numeric key has a min and a max (the Rule column, or after the default). A change outside them is refused, never clamped; the console also caps each unit (minutes at one week, hours at one year, days at ten years, everything else at 2^31−1) so no key can overflow a timer.

**Orders and strikes**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `order_accept_timeout_min` | minutes | 10 | §5.1. Min 2, max 60. |
| `pickup_hold_min` | minutes | 30 | §5.1. Min 10, max 240. |
| `pickup_extension_min` | minutes | 10 | §5.1. Min 5, max 60. |
| `partial_confirm_timeout_min` | minutes | 5 | §5.1. Min 2, max 30. |
| `delivery_dispatch_timeout_min` | minutes | 60 | §5.1. Min 15, max 240. |
| `noshow_strikes` | count | 3 | §5.4. Min 1, max 10. |
| `noshow_window_days` | days | 30 | §5.4. Min 7, max 90. |
| `noshow_limit_days` | days | 14 | §5.4. Min 1, max 90. |
| `shop_expiry_penalty_threshold_bp` | bp | 3000 | §5.4 |
| `store_strike_risk_threshold` | count | 5 | §5.4: store strikes in 30 days that raise a risk flag. Min 1, max 50. |
| `order_unseen_sms_after_min` | minutes | 3 | §15: SMS to the shop if a new order is not seen. Min 1, max 30. |
| `pickup_hold_reminder_min` | minutes | 10 | §15: "Hold ending" push to the customer. Min 1, max 60. |
| `risk_code_lockouts_store_per_day` | count | 10 | §6: order-code lockouts at one store in an IST day that raise a risk flag. Min 1, max 100. |
| `delivery_return_first_reminder_hours` | hours | 2 | §5.3: first inbox reminder after a failed delivery while the return is unconfirmed. Min 1, max 12 (D-218). |
| `delivery_return_second_reminder_hours` | hours | 24 | §5.3: second reminder. Min 13, max 71, so it stays before the 72 h auto-confirm (D-218). |

**Stock and catalogue**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `stock_stale_after_hours` | hours | 72 | §4; also the moment "Stock check due" (§12) fires. Min 12, max 720. |
| `low_stock_default` | count | 5 | §4. Min 0, max 1000. |
| `store_banners_max` | count | 3 | §12. Min 1, max 10. |

**Credits and payouts**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `credit_max_per_order` | credits | 30 | §7. Min 0, max 1000. |
| `credit_min_order_value_paise` | paise | 10000 | §7. Min 0, max 500000. |
| `credit_expiry_days` | days | 365 | §7. Min 30, max 1095. |
| `credit_expiry_reminder_days` | days | 7 | §7: customers are told this many days before a lot expires. Min 1, max 30. |
| `review_edit_window_days` | days | 7 | §10. Min 1, max 30. |
| `credit_orders_per_month` | count | 10 | §7. Min 1, max 100. |
| `store_credit_redemptions_per_day` | count | 200 | §7. Min 1, max 10000. |
| `admin_adjust_dual_approval_above_paise` | paise | 50000 | §7. Min 0, max 500000. |
| `admin_adjust_daily_total_alert_above_paise` | paise | 500000 | §7: risk alert on one admin's IST-day total across all customers (D-251). Min 0, max 10000000. |

**Referrals**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `ref_retailer_customer_referrer_credits` | credits | 30 | §8. Min 0, max 1000. |
| `ref_retailer_customer_referee_credits` | credits | 100 | §8. Min 0, max 1000. |
| `ref_customer_customer_referrer_credits` | credits | 30 | §8. Min 0, max 1000. |
| `ref_customer_customer_referee_credits` | credits | 100 | §8. Min 0, max 1000. |
| `ref_any_shop_referrer_credits` | credits | 30 | §8. Min 0, max 1000. |
| `ref_any_shop_referee_credits` | credits | 100 | §8. Min 0, max 1000. |
| `ref_qualifying_min_order_paise` | paise | 10000 | §8. Min 0, max 500000. |
| `ref_shop_qualifying_orders` | count | 10 | §8. Min 1, max 100. |
| `ref_attribution_window_days` | days | 7 | §8. Min 1, max 30. |

**POS**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `pos_reversal_window_hours` | hours | 24 | §7. Min 1, max 72. |
| `pos_offline_link_max_hours` | hours | 12 | §6. Min 1, max 72. |
| `pos_offline_links_risk_per_day` | count | 20 | §6. Min 1, max 500. |
| `pos_code_phone_failures_per_hour` | count | 10 | §6 (D-131): wrong counter codes for one phone across all stores in an hour; at this count the phone is refused at every store until the hour's count drops below it. Min 5, max 100. |

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
| `insight_no_sale_days` | days | 21 | "Sitting on the shelf": no sale in this many days. Min 7, max 90. |
| `insight_min_days_listed` | days | 21 | Listing history needed before "sitting on the shelf" (equal to the no-sale rule). Min 7, max 90. |
| `insight_expiring_within_days` | days | 5 | "Going to waste": batch expiry window. Min 1, max 30. |
| `insight_wanted_nearby_radius_m` | meters | 2000 | "Wanted nearby" search radius. Min 500, max 10000. |
| `insight_min_searches` | count | 10 | Searches needed before "wanted nearby" shows (never below 10, §16). Min 10. Min 10, max 1000. |
| `insight_min_sales_lines` | count | 20 | Sales lines in the period for share-of-sales claims. Min 5, max 1000. |
| `insight_min_units_selling_fast` | count | 5 | Units sold before "selling fast" shows. Min 1, max 1000. |
| `insight_min_events_for_percent` | count | 20 | Events behind any percentage shown. Min 10, max 1000. |
| `insight_period_days` | days | 7 | Insight period ("Last 7 days") and the window for the average daily pace. Min 1, max 90. |
| `insight_cover_days` | days | 7 | "Selling fast": the restock quantity covers this many days at the current pace, and a row fires below this many days of stock. Min 1, max 30. |
| `insight_missed_sales_min_zero_days` | days | 1 | "Missed sales": whole IST days with closing stock of 0 or below in the period before the row can fire. Min 1, max 30. |
| `insight_missed_sales_min_searches` | count | 10 | "Missed sales": nearby searches matching the product on those days (never below 10, §16). Min 10. Min 10, max 1000. |
| `insight_pair_min_bills` | count | 20 | "Bought together": bills containing both items in the window. Min 5, max 1000. |
| `insight_pair_window_days` | days | 60 | "Bought together": bill window for the pair counts. Min 14, max 180. |
| `insight_pair_min_share_bp` | bp | 2000 | "Bought together": share of item A's bills that also contain B (2000 bp = 20%); unit `bp`, so super_admin-only (§13). Min 500, max 10000. |
| `insight_pair_min_lift_pct` | count | 150 | "Bought together": minimum lift in percent, P(B given A) ÷ P(B) (150 = 1.5×, 100 = no lift needed), so an item that is in nearly every bill does not pair with everything. Min 100, max 1000. |
| `insight_regular_min_visits` | count | 3 | "Regulars slipping away": visits (distinct IST purchase days) needed in the regular window. Min 2, max 20. |
| `insight_regular_window_days` | days | 60 | "Regulars slipping away": window in which the visits are counted. Min 14, max 180. |
| `insight_regular_lapse_days` | days | 21 | "Regulars slipping away": no purchase for at least this many days. Min 7, max 90. |
| `insight_regular_min_linked_customers` | count | 10 | "Regulars slipping away": phone-linked customers who bought in the regular window before the kind shows anything; walk-ins without a phone are not counted. Min 1. Min 1, max 1000. |
| `insight_price_compare_enabled` | bool | false | "Priced above nearby" on or off (Q-52); needs at least 10 other live verified stores of distinct businesses in the radius whatever the setting |
| `coach_max_cards_per_day` | count | 5 | New coach cards per store per day. Min 0, max 20. |
| `coach_max_push_per_day` | count | 1 | Coach pushes per store per day. Min 0, max 5. |
| `coach_dismiss_quiet_days` | days | 7 | A dismissed subject (an item, a pair, a batch or the store; never a whole kind) stays quiet. Min 1, max 90. |

**Field agents**

| Key | Unit | Default | Rule |
|---|---|---|---|
| `field_agent_edit_window_days` | days | 30 | §14. Min 1, max 90. |
| `field_owner_codes_per_agent_hour` | count | 10 | §14: owner codes one field agent may send in a rolling hour, over every shop and number. Min 1, max 30. |

**Jobs** (D-95)

| Key | Unit | Default | Rule |
|---|---|---|---|
| `job_runs_retention_days` | days | 90 | §13: how long `job_runs` rows are kept (the `job_runs_retention` maintenance kind drops older months, 03 §9). Min 30, max 730. |

**Asks and search proof** (§11.1–§11.3, D-101)

| Key | Unit | Default | Rule |
|---|---|---|---|
| `ask_daily_limit_customer` | count | 5 | §11.2: asks one customer can send per IST day. Min 1, max 50. |
| `ask_radius_m` | meters | 3000 | §11.2: stores within this distance of the search location get the ask. Min 500, max 10000. |
| `ask_max_stores` | count | 8 | §11.2: stores one ask goes to. Min 1, max 20. |
| `ask_daily_limit_store` | count | 40 | §11.2: asks one store receives per IST day; after that it gets none until midnight. Min 5, max 200. |
| `ask_reply_window_min` | minutes | 15 | §11.2: time a store has to reply; then the ask closes for that store. Min 5, max 120. |
| `ask_price_valid_min` | minutes | 60 | §11.2: a replied price holds for booking this long after the reply (Q-53). Min 15, max 240. |
| `ask_low_yield_after_count` | count | 20 | §11.2: a customer's last asks in a row with no Yes before their asks are narrowed. Min 5, max 100. |
| `ask_low_yield_max_stores` | count | 3 | §11.2: stores a narrowed customer's ask goes to, until one of their online orders is `collected` or `delivered`. Min 1, max 20. |
| `ask_retention_days` | days | 90 | §11.2: asks are deleted after this with their recipients, replies and the store blocks that came from them (the `asks_retention` maintenance kind); also how long a shop can report an ask. Min 30, max 365. |
| `ask_whatsapp_enabled` | bool | false | §11.2: WhatsApp delivery of asks to shops; a Wave 5 option, off in v1 (Q-55) |
| `ask_gap_min_customers` | count | 10 | §11.2: distinct customers who asked for a product in a city before it shows in Demand gaps (never below 10, §16). Min 10. Min 10, max 1000. |
| `ask_gap_window_days` | days | 7 | §11.2: Demand gaps window. Min 1, max 30. |
| `site_marquee_min_searchers` | count | 10 | §11.3: distinct signed-in customers (`customer_id`s; anonymous devices never count) who searched a product in a city before the nightly build proposes it (never below 10, §16). Min 10. Min 10, max 1000. |
| `site_marquee_window_days` | days | 7 | §11.3: search window for the site strip. Min 1, max 30. |
| `site_marquee_max_items` | count | 20 | §11.3: products proposed per city. Min 5, max 50. |
| `refine_ai_min_words` | count | 4 | §11.1: words a query with no chip needs before search sets `refineHint` and the app may call search assist (AI-C2). Min 2, max 20. |
| `substitute_pack_tolerance_pct` | count | 25 | §11.1, [11-ai-features.md](11-ai-features.md) AI-C3 (D-235): how far a same-shop substitute's pack may be from the out-of-stock item's, in percent either way, in base units. Min 5, max 100. |
| `share_card_create_per_hour` | count | 30 | §11.1: share cards one IP or anonymous device can create per hour (`429`); re-creating an existing card does not count. Min 5, max 300. |

**Baskets** (§11.4, D-110)

| Key | Unit | Default | Rule |
|---|---|---|---|
| `basket_max_saved` | count | 10 | §11.4: saved baskets one customer can keep. Min 1, max 50. |
| `basket_max_items` | count | 30 | §11.4: lines in one basket. Min 5, max 100. |
| `basket_split_max_shops` | count | 2 | §11.4: stores in a suggested split; 1 turns splits off. Min 1, max 2. |
| `basket_results_max` | count | 10 | §11.4: stores shown in basket search results. Min 3, max 30. |

**Saved addresses** (§5.5, D-222)

| Key | Unit | Default | Rule |
|---|---|---|---|
| `customer_max_saved_addresses` | count | 20 | §5.5: saved addresses one customer can keep (`422 validation_failed` on one more). Min 1, max 50. |
| `profile_photo_uploads_per_hour` | count | 10 | §2: profile photo uploads one customer may ask for in a rolling hour. Min 1, max 60. |

**Admin sign-in** (§2, D-264)

| Key | Unit | Default | Rule |
|---|---|---|---|
| `admin_signin_max_failures` | count | 10 | §2: wrong admin sign-in codes for one number within `admin_signin_failure_window_hours`, across its codes, that lock its admin sign-in. A good sign-in code clears the count. Min 3, max 50. |
| `admin_signin_failure_window_hours` | hours | 24 | §2: the window in which wrong admin sign-in codes are counted. Min 1, max 168. |
| `admin_signin_lock_hours` | hours | 24 | §2: how long a locked admin sign-in stays locked (`429 signin_locked`), unless another super admin unlocks it on A-18. Min 1, max 168. |
| `admin_step_up_sends_per_hour` | count | 10 | §2: step-up codes one admin may ask for in a rolling hour; a bucket of its own, apart from the sign-in send limits. Min 1, max 60. |

**Risk** (A-13, D-225). Each raises a `risk_flags` row only; no one is blocked or suspended because of it. The one money effect is §9's payout hold: a flag on a business or one of its stores holds that business's payout while it is open or confirmed, until someone clears it.

| Key | Unit | Default | Rule |
|---|---|---|---|
| `risk_bill_claim_stores` | count | 3 | §6: different stores one customer confirms bill QRs at within `risk_bill_claim_window_minutes` that raise a risk flag. Min 2, max 20. |
| `risk_bill_claim_window_minutes` | minutes | 10 | §6: the window for `risk_bill_claim_stores`. Min 1, max 240. |
| `risk_reversal_ratio_percent` | count | 20 | §7: share of a store's linked POS sales (bill QR or counter code) reversed within `risk_reversal_window_days` that raises a risk flag, in percent. Min 1, max 100. |
| `risk_reversal_min_sales` | count | 30 | §7: linked sales a store needs in the window before the ratio counts. Min 1, max 1000. |
| `risk_reversal_window_days` | days | 7 | §7: the window for the reversal ratio. Min 1, max 30. |
| `risk_otp_phones_per_ip_hour` | count | 10 | §2: different phone numbers one IP asks OTPs for within an hour that raise a risk flag. Min 2, max 100. |
| `risk_otp_failed_per_phone_day` | count | 3 | §2: OTPs for one phone that used every attempt without the right code within 24 h that raise a risk flag. Min 1, max 50. |

**Support** (§13, D-226)

| Key | Unit | Default | Rule |
|---|---|---|---|
| `support_sla_urgent_min` | minutes | 60 | §13: time to support's next reply on an urgent ticket. Min 15, max 10080. |
| `support_sla_high_min` | minutes | 240 | §13: the same for a high-priority ticket. Min 15, max 10080. |
| `support_sla_normal_min` | minutes | 1440 | §13: the same for a normal ticket. Min 15, max 10080. |
| `support_open_tickets_max` | count | 5 | §13: tickets one person may have open or waiting at once. Min 1, max 50. |
| `support_tickets_per_hour` | count | 5 | §13: tickets one person may open in a rolling hour. Min 1, max 60. |
| `support_uploads_per_hour` | count | 30 | §13: attachment uploads one person may ask for in a rolling hour. Min 5, max 300. |
| `support_upload_ttl_hours` | hours | 24 | §13: an upload no message has claimed is refused and deleted after this. Min 1, max 168. |

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
| `ai_voicesearch_enabled` (AI-C5, D-101) | bool | false |
| `ai_catalogue_enabled` (AI-A1) | bool | false |
| `ai_triage_enabled` (AI-A2) | bool | false |
| `ai_riskexplain_enabled` (AI-A3) | bool | false |
| `ai_model_fast` | text | `claude-haiku-5-5` (one of `claude-haiku-5-5`, `claude-sonnet-5-5`, `claude-opus-5-5`) |
| `ai_model_smart` | text | `claude-sonnet-5-5` (one of `claude-haiku-5-5`, `claude-sonnet-5-5`, `claude-opus-5-5`) |
| `ai_daily_calls_store` | count | 100 (min 0, max 1000) |
| `ai_daily_calls_store_background` | count | 20 (background wording: AI-R4 insight sentences and AI-R5 coach cards, per store per IST day, separate from `ai_daily_calls_store`; when spent the template shows, never an error, D-228; min 0, max 1000) |
| `ai_daily_calls_customer` | count | 30 (min 0, max 300) |
| `ai_daily_cost_cap_paise` | paise | 50000 (₹500 a day; AI calls stop for the rest of the IST day when reached, alert at 80%, D-70; min 0, max 1000000) |

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
| OTP limits | 6 digits, 5 min, 5 attempts, resend 30 s, 5 sends per number per hour, 20 per IP per hour (admin step-up codes are counted apart, in `admin_step_up_sends_per_hour`, with the 30 s gap between step-up codes only) | §2 |
| Device clock tolerance for offline links | 2 min | §6, D-12 |
| Voice recording length | 15 s | §12; also customer voice search, §11.1, D-101 |
| `sold_at` clamp for offline sales | 72 h | 02 §4.3 |
| Odd offline sale time (kept, flagged once a day per store) | device clock more than 5 min ahead, past the 72 h clamp, or more than 30 min before the device's previous sync | §6, D-243 |
| Order SSE poll interval | 15 s | 02 |
| "3 or more orders expired in 2 h" alert | 3 in 2 h | 05 §5 |
| Failed-delivery auto-confirm | 72 h | §5.3, D-85 (the 2 h and 24 h reminders are settings, D-218) |
| Store-strike window (risk flag and reliability) | 30 days | §5.4, D-85 |
| Risk flag scores (0–100, shown in A-13) | fixed per rule in `internal/risk` | §13, D-225 |
| Jobs watchdog interval | 5 min | §13, D-95 |
| "Cron missed" threshold | no succeeded run within 2× the schedule interval (daily kinds: 15 min grace after the expected time) | §13, D-95 |
| Job error text stored | scrubbed, at most 2 kB | §13, D-95 |
| Demand insights card rows | 4, at most one per kind | §12, D-99 |
| "Selling fast" is high severity when stock lasts at most | 2 days | §12, D-99 |
| Pairs kept per store by the nightly rollup | 100, by lift | §12, D-99 |
| Stores needed for "No shop nearby has it." and "Priced above nearby" | 10 other live verified stores of distinct businesses | §12, §16, D-71, D-99 |
| Badges per result card; cards per badge | 1; 1 | §11.1, D-101 |
| Ask distance bands shown to a shop | under 1 km, 1–3 km, 3–5 km, over 5 km | §11.2, D-101 |
| Customer pushes per ask | 1, at the first Yes | §11.2, §15, D-101 |
| Search before an ask counted once with it | same customer, same product, within 1 h | §11.2, D-101 |
| Share code | 10 characters, base62, random | §11.1, D-101 |
| Search assist model timeout (AI-C2) | 3 s; the app stops waiting at the same time and shows nothing | §11.1, D-101 |
| Categories never proposed for the site search strip | medicines and health, personal and intimate care, tobacco, adult | §11.3, D-101 |

**Client versions** (an older client shows "Update available"; it reloads when the user taps, never on its own and never while a sale or checkout is open, D-98)

| Key | Unit | Default |
|---|---|---|
| `min_client_version_customer` | text | `0.0.0` |
| `min_client_version_business` | text | `0.0.0` |
| `min_client_version_admin` | text | `0.0.0` |
