# MQ-02 ★ Order pickup

A signed-out shopper fills a cart at Sri Lakshmi, signs in at checkout, uses 30 credits, places a pickup order, watches it live, collects it at the counter with the order code and leaves a review. The shop side runs on a second device.

- **Covers:** 08 §5 MQ-02 (cart, sign-in mid-checkout, credits, place, live tracking, code at counter, review); E2E-02 shape with this book's numbers.
- **Screens:** C-07, C-08, C-09, C-10, C-11, C-12, C-13, C-14, C-15, C-20; B-01, B-04, B-11.
- **Devices:** customer on P390 (profile A); shop on D1280 (profile B). Repeat the ★ steps with the customer on P360.
- **Time:** about 45 minutes.

## Preconditions

1. `make reset` done. `clock reset`. Run between 10:00 and 20:00 IST (SL is open 08:00–22:00).
2. Profile A: fresh, location set to the QA point (run MQ-01.01–MQ-01.10 if needed), not signed in, empty cart.
3. Profile B: business app, not signed in.
4. C10 (Ananya) wallet is 100 credits in one lot (`<seed:customers>`). SL stock: I1 10, I2 50, I4 6 (`<seed:items-SL>`).
5. Settings at defaults: `credit_max_per_order` = 30, `credit_min_order_value_paise` = 10000, `order_accept_timeout_min` = 10, `pickup_hold_min` = 30, `pickup_extension_min` = 10. SL cashback 500 bp.
6. Mocks UI open in a third tab (https://mocks.todayz.localhost) to see push messages.

## Steps

### A. Shop gets ready (profile B)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.01 ★ | Open https://business.todayz.localhost and sign in as K22 (cashier, Sri Lakshmi only) with OTP `000000`. | B-01: no picker (one store only); lands on B-03 Overview for Sri Lakshmi. The role shows as Cashier. | `R-2.4` B-01 | ▢ | |
| MQ-02.02 | Open Orders (B-04). Click the "Allow sound" prompt if shown. | Tabs New, Accepted, Ready, Out for delivery, Returns to confirm, Done, Missed. New shows no badge. The "Allow sound" prompt shows on first open. | B-04 `R-15.2` | ▢ | |
| MQ-02.03 | Note SL stock in B-07 for Amul Butter 100 g. | On hand 10, "Reserved for orders" 0. | `R-4.5` B-07 | | |

### B. Cart while signed out (profile A)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.04 ★ | Open Sri Lakshmi (C-07). Add Amul Butter 100 g, set quantity 2. | CartBar: 2 items, ₹116. No sign-in prompt. | `R-2.1` C-07 C-09 | | |
| MQ-02.05 | Add Maggi 2-Minute Noodles 70 g ×3 and Wheat atta 5 kg ×1. | CartBar: 6 items, ₹403. | C-09 | | |
| MQ-02.06 | Open Annapurna Provisions and tap Add on any item. | A dialog asks to replace the Sri Lakshmi cart (one shop per cart). Choose Keep. Cart still ₹403 at Sri Lakshmi. | `R-5.1` C-09 | ▢ | |
| MQ-02.07 ★ | Open the cart (C-09). | Three lines with steppers: Amul Butter 100 g ×2 ₹116, Maggi 70 g ×3 ₹42, Wheat atta 5 kg ×1 ₹245. Items ₹403. Pickup selected. Delivery option visible (SL offers delivery) with fee ₹30, free delivery on ₹500+, minimum ₹150. | `R-5.29` C-09 | ▢ | |
| MQ-02.08 | Read the credits row while signed out. | Copy "Use up to 30 credits on orders of ₹100 or more" (`credit_max_per_order`, `credit_min_order_value_paise`). The toggle asks to sign in to use credits. | `R-7.3` D-02 C-09 | ▢ | |
| MQ-02.09 | Tap Checkout. | C-10 sign-in opens with the +91 prefix. | `R-2.1` C-10 | | |

### C. Sign-in mid-checkout

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.10 | Type `90000 0001` (9 digits). | Continue stays disabled. | C-10 | | |
| MQ-02.11 ★ | Type `90000 00010` and continue. | OTP screen with 6 boxes and a resend timer counting down from 30 s. No Turnstile challenge shown (invisible mode). | `R-2.1` `R-2.6` C-10 | ▢ | |
| MQ-02.12 | Wait and watch the resend link. | Resend is disabled until the timer reaches 0, then becomes active. | `R-2.1` AU-04 C-10 | | |
| MQ-02.13 | Enter `111111`. | Inline error for a wrong code; 4 attempts remain (shown or implied by the copy). No raw code text. | `R-2.1` AU-02 C-10 | ▢ | |
| MQ-02.14 ★ | Enter `000000`. | Signed in. Returns to checkout (C-11), not Home. The cart is unchanged: 3 lines, items ₹403. | `R-2.1` `R-2.2` AU-01 C-11 | ▢ | |
| MQ-02.15 | In DevTools → Application → Cookies for `api.todayz.localhost`, find the refresh cookie. | Cookie `__Secure-tz_rt_c`, HttpOnly, Secure, SameSite Strict, Path `/v1/auth`, no Domain attribute. Do not copy its value. | `R-2.7` AU-01 | | |

### D. Checkout with credits

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.16 ★ | Read C-11. | Lines, Pickup, payment note "Pay at the shop: cash or UPI", credits row with wallet 100, total ₹403. Place order button is the brand style, 48 px tall. | `R-1.3` C-11 | ▢ | |
| MQ-02.17 | Turn credits on. Move the slider to the end. | Slider range 0–30. It stops at 30 even though the wallet has 100. | `R-7.3` C-11 | | |
| MQ-02.18 ★ | Leave 30 credits. Read the totals. | Items ₹403, credits −₹30, total ₹373. | `R-7.2` `R-7.3` BK-33 C-11 | ▢ | |
| MQ-02.19 | Tap Place order once. Watch the Network panel. | One `POST /v1/me/orders` with an `Idempotency-Key` header. 201. | `R-5.9` BK-04 | | |
| MQ-02.20 ★ | Look at C-12. | A 400 ms check animation plays once. Order number in the form `TZ-` plus 6 digits (for example `TZ-000301`). OrderTimeline shows Placed. Countdown "Waiting for Sri Lakshmi to accept · 10:00 left" (or 9:5x) counting down. | `R-5.16` `R-5.3` BK-01 BK-34 C-12 | ▢ | |
| MQ-02.21 | Open Wallet (C-15) in a second tab of profile A. | WalletCard: balance 70 available, 30 held. History has no "Spent" line yet. | `R-7.5` CR-01 C-15 | ▢ | |

### E. Shop sees the new order (profile B)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.22 ★ | Time from MQ-02.19 to the alert in profile B (stopwatch or screen recording). | Within 3 s: New tab badge 1, a new-order toast and a looping sound. A push appears in the mocks Push tab for K22's device if push is allowed. | `R-15.2` BK-01 B-04 | ▢ | |
| MQ-02.23 | Open the order card. | Customer "Ananya" with masked phone `90•••••010` style (never the full number). Countdown to auto-expiry about 9:xx. Three lines with stock: Amul Butter 100 g ×2 (10 in stock), Maggi ×3 (50), Wheat atta 5 kg ×1 (6). Buttons Accept, Accept part, Reject. | `R-16.2` BK-27 B-04 | ▢ | |
| MQ-02.24 | DevTools → Network: open the `GET …/orders/{orderId}` response in profile B. Search the body for the 4 digits the customer will see later (after MQ-02.29). | The order code is not in any shop response (no `code` field). | `R-6.1` BK-38 | | |
| MQ-02.25 | Open B-07 Amul Butter 100 g. | On hand 10, reserved 0 (placed reserves nothing). | `R-5.19` BK-01 | | |
| MQ-02.26 ★ | Back on the order, click Accept. | Order moves to the Accepted tab. Sound stops. | `R-5.12` BK-05 B-04 | ▢ | |
| MQ-02.27 | Open B-07 Amul Butter 100 g again. | On hand 10, "Reserved for orders" 2, available 8. History has no new movement. | `R-5.20` `R-4.5` BK-36 B-07 | ▢ | |

### F. Customer tracks the order (profile A)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.28 | Watch C-12 without reloading. | Within 3 s the status changes to Accepted (live via SSE). Mocks Push tab shows an "accepted" push for C10. | `R-15.3` C-12 | | |
| MQ-02.29 ★ | Read the accepted state. | PickupCode: 4 big digits and a QR, with a brightness hint. Hold countdown about 30:00. Buttons "I'm on my way (+10 min)", directions, call shop, cancel. Mask the code in the screenshot. | `R-6.1` `R-5.4` C-12 | ▢ | |
| MQ-02.30 | Tap "I'm on my way (+10 min)". | Hold countdown grows by 10 minutes (about 40:00). The button disappears or is disabled. | `R-5.5` BK-15 C-12 | ▢ | |
| MQ-02.31 | Call `POST /v1/me/orders/{orderId}/extend` again with Copy as cURL (new Idempotency-Key). | 409 `invalid_transition` (once per order). Hold unchanged. | `R-5.5` BK-15 `invalid_transition` | | |
| MQ-02.32 | Open the bell (C-20). | Inbox lines for placed and accepted, newest first, unread state shown. Tapping one opens C-12. | `R-15.3` C-20 | ▢ | |

### G. Hand over at the counter (profile B)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.33 | In the Accepted tab, open the order. Click Mark ready. | Order moves to Ready. Customer C-12 shows Ready within 3 s and a "ready" push appears in the mocks. | `R-15.4` B-04 C-12 | | |
| MQ-02.34 ★ | Enter a wrong 4-digit code (change the last digit). | Error with attempts left: 4. Order still Ready. | `R-6.1` BK-12 B-04 | ▢ | |
| MQ-02.35 | Enter another wrong code. | Attempts left: 3. | `R-6.1` BK-12 | | |
| MQ-02.36 ★ | Pick payment method Cash, then enter the right code the customer shows. | Order becomes Collected and moves to Done. Payment method Cash recorded on the order. | `R-6.1` `R-1.3` BK-11 B-04 | ▢ | |
| MQ-02.37 | Enter the same code again on the same order with Copy as cURL of `verify-code` (new Idempotency-Key). | 409 `invalid_transition` (single use; not valid after collected). | `R-6.1` BK-13 `invalid_transition` | | |
| MQ-02.38 ★ | Open B-07 Amul Butter 100 g. | On hand 8, reserved 0. History shows one "Online sale" movement of −2 by K22. Maggi on hand 47; atta on hand 5. | `R-5.21` `R-4.4` BK-11 BK-37 B-07 | ▢ | |
| MQ-02.39 | Open the order timeline in profile B. | Placed (customer), Accepted (K22), Ready (K22), Collected (K22), each with time, in order. | `R-5.15` BK-31 B-04 | ▢ | |

### H. Money after collection (profile A)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.40 ★ | Look at C-12. | Terminal state Collected with next actions: review, reorder. | `R-5.10` C-12 | ▢ | |
| MQ-02.41 ★ | Open Wallet (C-15). | Balance 88: 100 − 30 spent + 18 cashback. Held 0. Cashback is floor(37300 × 500 / 1,000,000) = 18 (base = ₹403 − ₹30 = ₹373). | `R-7.5` `R-7.7` CR-04 CR-23 C-15 | ▢ | |
| MQ-02.42 | Read the history lines. | "Spent at Sri Lakshmi · order TZ-000301" (−30) and "5% back at Sri Lakshmi" (+18), with the real order number. | `R-7.13` CR-04 C-15 | ▢ | |
| MQ-02.43 | In profile B as O20 (switch user), open B-10 Credits. | Lines "spent at your shop" +30 and cashback −18 for this order. Balance moved by +12. | `R-7.6` `R-7.7` `R-9.2` B-10 | ▢ | |
| MQ-02.44 | Open Orders (C-13) in profile A, tab Online. | The order shows under history with status Collected, total ₹373 and a reorder action. | C-13 | | |

### I. Review

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.45 ★ | From C-12 tap Review. | C-14: 5 stars, text field with a 1000-character limit, positive tags (fresh stock, fair price, quick service, friendly, stock was right, well packed) and negative tags (item missing, slow, rude, wrong item, expired item, overpriced). | `R-10.1` C-14 | ▢ | |
| MQ-02.46 | Paste 1001 characters into the text. | The field stops at 1000 or shows a counter error; Submit refuses 1001. | `R-10.1` C-14 | | |
| MQ-02.47 ★ | Give 4 stars, tags "Fresh stock" and "Quick service", text "Butter was fresh. Quick at the counter." Submit. | Review published at once. Sri Lakshmi's rating on C-07 changes (Bayesian, prior 4.0 weight 5) and the count goes up by 1. | `R-10.2` `R-10.4` C-14 C-07 | ▢ | |
| MQ-02.48 | Try `POST /v1/me/reviews` again for the same order (Copy as cURL). | Refused (one review per order): 409. | `R-10.1` | | |
| MQ-02.49 | Edit the review: change to 5 stars. | Saved; shop page rating updates. "Edit" stays available (within 7 days). | `R-10.1` C-14 | | |
| MQ-02.50 | `clock +7d1m`, reload the review. | Edit is no longer offered. A PATCH by Copy as cURL is refused with `409 review_edit_window_passed`. Then `clock reset`. | `R-10.1` C-14 RV-01 | | |
| MQ-02.51 | In profile B as O20 open Reviews (B-11). | The review shows with stars and text. Reply box available. In-app inbox (B-16) shows "New review". | `R-10.2` `R-15.9` B-11 B-16 | ▢ | |
| MQ-02.52 | Reply "Thank you, Ananya!" and save. Then edit the reply. | One reply only; editing changes it in place. Customer sees the reply on C-14. | `R-10.2` B-11 C-14 | ▢ | |

### J. Second order and repeat checks

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-02.53 | Place a second pickup order at SL as C10: I3 Nandini Toned Milk 500 ml ×5 (₹120), no credits. | New number is the previous number + 1 (one global sequence) or higher if other orders were placed. Countdown 10:00. | `R-5.16` BK-34 | | |
| MQ-02.54 | `clock +9m59s`, then reload C-12. | Still Placed; countdown about 0:01. If the screen still shows ~10:00, the client is using device time instead of `serverNow` (D-93): fail. | `R-5.3` `R-5.8` BK-64 | | |
| MQ-02.55 | In profile B accept it. | Accepted at 9 m 59 s. | `R-5.3` TIMER-M | | |
| MQ-02.56 | As C10 cancel it from C-12 with reason "Changed my mind". | Cancelled (customer). Reservation released (I3 reserved back to 0). Shop gets an in-app line. | `R-5.11` BK-17 | ▢ | |
| MQ-02.57 | After this second order, watch Home for the PWA install prompt. | The install prompt appears now, on C-12 right after the second order is placed (D-87), and did not appear after the first. Dismiss it. Detailed in PWA-M. | D-87 C-03 | ▢ | |

## Cleanup

1. `clock reset`.
2. Sign out in both profiles (Account → Sign out; B-14 → Sign out).
3. `make reset` before the next money script, because C10's wallet is now 88.
