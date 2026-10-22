# TIMER Order timers and time boundaries

Every timer in 01 §5.1 and every other clock rule, tested one second before and one second after its boundary with the test clock (README §3). Also: opening hours, IST midnight, order numbers across midnight, sign-in timers, the bill QR and receipt lifetimes, the POS reversal window, and the delivery-failed return clock.

All timers run on the server; the apps only display them (`R-5.8`). After every `clock` call, refresh the screen being checked; due jobs run at once when the clock moves.

- **Covers:** BK-06, BK-09, BK-14, BK-15, BK-16, BK-20, BK-21, BK-25, BK-26, BK-28, BK-34, BK-41, BK-46; AU-03, AU-04, AU-08; POS-02, POS-07, POS-08, POS-25; D-21, D-23, D-41, D-67, D-68, D-69.
- **Screens:** C-10, C-11, C-12, C-17, C-21, C-24; B-04, B-05, B-16, B-19; A-01, A-05, A-06, A-13.
- **Devices:** customer P360 (profile A), shop D1280 (profile B), admin D1440 (profile D).
- **Time:** about 2 hours 30 minutes.

## Preconditions

1. `make reset`. `clock set 2026-10-22T11:00:00+05:30` (Thursday) unless a section says otherwise.
2. Sri Lakshmi (SL) open 08:00–22:00, auto-accept off, pickup and delivery on.
3. K22 signed in on profile B at Sri Lakshmi with sound allowed. M21 available in a second business window.
4. Defaults in force: `order_accept_timeout_min` 10, `pickup_hold_min` 30, `pickup_extension_min` 10, `partial_confirm_timeout_min` 5, `delivery_dispatch_timeout_min` 60.
5. Write down each order's number and the server time of each transition in the run notes (`clock now`).

## Steps

### A. Accept timeout (`order_accept_timeout_min` 10)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.01 ★ | Sign in as C10. Place a pickup order at Sri Lakshmi: I1 ×1 and I10 ×1 (₹158), 30 credits. Note the time **T0**. | C-12: "Waiting for Sri Lakshmi to accept · 10:00 left" counting down. B-04 New shows the order with a countdown to auto-expiry and the sound loop. C-15: held 30. | `R-5.3` BK-01 C-12 B-04 | ▢ | |
| TIMER-M.02 | `clock set` T0 + 9 min 59 s. Refresh C-12 and B-04. | Still `placed`. C-12 shows 0:01 left (or 0:00 rounding). The Accept button is still enabled in B-04. | `R-5.3` BK-06 | ▢ | |
| TIMER-M.03 ★ | `clock +2s` (T0 + 10 min 01 s). Refresh both. | Order `expired` with reason `not_accepted`. C-12 shows the outcome and next actions (reorder, find elsewhere). C10's 30 credits released: C-15 held 0, history "Returned" line. A push and an in-app message "expired" for C10 in the mocks. B-04: the order is in Missed. | `R-5.3` `R-5.23` `R-15.3` BK-06 C-12 B-04 | ▢ | |
| TIMER-M.04 | K22 sends the accept request for the expired order (copy as cURL from an earlier accept, or keep a stale B-04 tab open and click Accept). | `409 invalid_transition`. No reservation is made: I1 available stays 10. | `R-5.9` BK-30 `invalid_transition` | | |
| TIMER-M.05 | S42 opens A-05 → Sri Lakshmi → reliability stats. | The unaccepted-expiry count for the last 30 days went up by 1 compared with before M.01 (note both numbers). | `R-5.26` BK-06 A-05 | | |
| TIMER-M.06 | C10 places the same order again at T1. `clock set` T1 + 9 min 59 s. K22 accepts all. | Accepted at the last second. `hold_until` = accept time + 30 min, shown on C-12 as a hold countdown of 30:00. | `R-5.3` `R-5.4` BK-05 | | |

### B. Pickup hold, the 10-minute warning and the extension

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.07 | Continue with M.06's order. Note the accept time **TA**. `clock set` TA + 19 min 59 s. Open the mocks push tab for C10. | No "hold ending" push yet. | `R-15.5` | | |
| TIMER-M.08 ★ | `clock set` TA + 20 min 01 s. | One push to C10 that the hold ends in 10 minutes (01 §15). No in-app inbox line is required for this event; record if one appears. | `R-15.5` C-12 | ▢ | |
| TIMER-M.09 | `clock set` TA + 29 min 59 s. Refresh C-12 and B-04. | Still `accepted`. Hold countdown 0:01. The code still shows on C-12. | `R-5.4` BK-14 | | |
| TIMER-M.10 ★ | `clock +2s` (TA + 30 min 01 s). Refresh. | `expired` with reason `no_show`. C10 gets one strike: A-06 → C10 → strikes shows 1, with the order number. I1 reservation released (available back to 10, on hand 10, no movement). The 30 held credits released. | `R-5.4` `R-5.24` `R-5.25` BK-14 BK-37 A-06 | ▢ | |
| TIMER-M.11 | C10 places a new pickup order (I1 ×1). K22 accepts at **TB**. `clock set` TB + 25 min. On C-12 tap "I'm on my way (+10 min)". | Hold countdown jumps from 5:00 to 15:00. `hold_until` = TB + 40 min. The button is gone or disabled after the tap. B-04 shows the new hold time. | `R-5.5` BK-15 C-12 | ▢ | |
| TIMER-M.12 | Send the extension request again via cURL. | Refused (one extension per order). Record the code and status. `hold_until` unchanged. | `R-5.5` BK-15 | | |
| TIMER-M.13 | `clock set` TB + 39 min 59 s. Refresh. | Still `accepted`. | `R-5.5` BK-15 | | |
| TIMER-M.14 | `clock set` TB + 40 min 01 s. Refresh. Then send the extension request again. | `expired` (`no_show`). C10 strikes 2. The extension request is refused (`invalid_transition`). | `R-5.5` `R-5.24` BK-15 | | |
| TIMER-M.15 | C10 places another order; K22 accepts at **TC**. `clock set` TC + 29 min 59 s. K22 enters C10's code. Then `clock set` TC + 31 min. | Collected at the last second. At +31 min nothing changes: no expiry, no strike (still 2). One outcome only. | `R-5.4` BK-20 | ▢ | |
| TIMER-M.16 | S42 opens A-06 → C10 → Reset strikes, reason "QA TIMER-M.16". | Strikes 0. An audit row in A-19 with actor S42 and the reason. | `R-5.28` A-06 A-19 | | |

### C. Partial confirm timeout (`partial_confirm_timeout_min` 5)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.17 | C11 places a pickup order: I1 ×2, I3 ×2, I5 ×1 (₹204), 30 credits. K22 opens it, Accept part: I1 ×1, drop I5. Note **TP**. | `awaiting_customer`. C-12 shows the confirm screen with a 5:00 countdown, new items total ₹106, credits 30, new total ₹76. | `R-5.6` `R-5.18` BK-08 C-12 | ▢ | |
| TIMER-M.18 | `clock set` TP + 4 min 59 s. C11 taps Confirm. | Accepted with the new totals. Hold countdown starts at 30:00 from now. | `R-5.6` BK-09 | | |
| TIMER-M.19 ★ | C11 places the same order again; K22 partial-accepts it the same way at **TQ**. `clock set` TQ + 5 min 01 s. Refresh C-12. | `cancelled` with system reason `partial_timeout`. C-12 says "The changed order wasn't confirmed in time". Held credits back in the wallet (C-15 held 0). | `R-5.6` `R-5.13` BK-09 D-68 C-12 | ▢ | |
| TIMER-M.20 | Check strikes: A-06 → C11, and A-05 → Sri Lakshmi strikes. Then C11 taps Confirm on a stale C-12 tab. | No strike on either side. The late confirm → `409 invalid_transition`. | `R-5.13` `R-5.25` BK-09 `invalid_transition` | | |

### D. Delivery dispatch timeout (`delivery_dispatch_timeout_min` 60) and no-show for delivery

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.21 | C11 places a delivery order to "Home": I4 ×1 (₹245), fee ₹30, 30 credits. K22 accepts at **TD**. Do not dispatch. | `accepted`. No pickup hold countdown on C-12 (delivery has no hold). | `R-5.7` BK-24 | | |
| TIMER-M.22 | `clock set` TD + 30 min 01 s. Refresh. | Still `accepted`. No `no_show`; C11 strikes unchanged (0). | `R-5.24` BK-41 D-21 | | |
| TIMER-M.23 | `clock set` TD + 59 min 59 s. Refresh. | Still `accepted`. Dispatch still possible. | `R-5.7` BK-25 | | |
| TIMER-M.24 ★ | `clock +2s` (TD + 60 min 01 s). Refresh C-12, B-04 and A-05. | `expired` with reason `dispatch_timeout`. Sri Lakshmi gets one strike (A-05 strikes +1); C11 gets none. I4 reservation released; 30 held credits released. C11 gets the "expired" push and in-app message. | `R-5.7` `R-5.25` BK-25 D-21 A-05 | ▢ | |

### E. No-show limit (C13) and store strikes

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.25 ★ | Sign in as C13 (3 no-shows in the last 30 days). Open Home and checkout. | C-21 banner on Home and Checkout explains the limit (1 active order) and its end date, 14 days after the 3rd strike (`noshow_limit_days`). Note the end date **EL**. | `R-5.24` BK-16 C-21 | ▢ | |
| TIMER-M.26 | C13 places one pickup order at Sri Lakshmi. Then tries a second order at Annapurna Provisions. | First: `placed`. Second: refused with the limit explained. Record the code and status (not named in 04, Q-10). | `R-5.24` BK-16 | ▢ | |
| TIMER-M.27 | C13 cancels the first order. Tries the second order again. | Allowed (only one active order). Cancel it again. | `R-5.24` BK-16 | | |
| TIMER-M.28 | `clock set` EL − 1 s. Open Home. Then `clock set` EL + 1 s. | At EL − 1 s the banner still shows. At EL + 1 s it is gone and two orders can be active at once. | `R-5.24` BK-16 | ▢ | |
| TIMER-M.29 | `make reset`; `clock set 2026-10-22T11:00:00+05:30`. Repeat M.21 and M.24 four times at Sri Lakshmi (4 dispatch timeouts). Open A-13 and A-05. | A-05: 4 new strikes. A-13: no `store_strikes` flag for Sri Lakshmi. Store still `live`. | `R-5.26` BK-46 | | |
| TIMER-M.30 ★ | Do it a 5th time. Refresh A-13, then search for "atta" in the customer app. | One `store_strikes` flag for Sri Lakshmi in A-13 with the 5 strikes as evidence (`store_strike_risk_threshold` 5). The store stays `live` and bookable. Record its search position for "atta" against Annapurna (reliability lowers rank after the next job run). | `R-5.26` `R-11.5` BK-46 D-67 A-13 | ▢ | |
| TIMER-M.31 | `clock +31d`. Refresh A-05. | The 5 strikes are outside the 30-day window and no longer count toward the threshold. No new flag. | `R-5.26` BK-46 | | |
| TIMER-M.32 | `make reset`. `set shop_expiry_penalty_threshold_bp = 0` (SA48). Let one order at Annapurna Provisions expire unaccepted (M.01 to M.03 pattern). Run the coach job (`clock +1h`). Sign in to the business app as X25 (owner of Annapurna Provisions) and open Overview and B-19. | A High coach card shows the 30-day unaccepted-expiry rate, says the shop ranks lower in search until it drops, and links to order sound and auto-accept settings. Restore the key to 3000 and confirm the card no longer appears on the next day's run. | `R-5.27` D-46 B-19 | ▢ | |

### F. Opening hours and `store_closed` (BK-28, D-41)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.33 | `make reset`. `clock set 2026-10-22T21:59:00+05:30`. C10 places a pickup order at Sri Lakshmi (closes 22:00): I2 ×10. | Placed (one minute before closing). Accept countdown 10:00. | `R-5.2` BK-28 | | |
| TIMER-M.34 ★ | `clock set 2026-10-22T22:00:00+05:30`. C10 builds a new cart at Sri Lakshmi and opens checkout. Tap Place order. | Quote and place both refused `409 store_closed`. C-11 shows "This shop is closed now" with today's hours (08:00–22:00) and the next opening time; cart kept; no order created. | `R-5.2` BK-28 D-41 C-11 `store_closed` | ▢ | |
| TIMER-M.35 | Open Home and the shop page. | Sri Lakshmi shows closed, with its next opening time. | `R-11.2` C-03 C-07 | | |
| TIMER-M.36 | `clock set 2026-10-23T07:59:59+05:30`, place again. Then `clock set 2026-10-23T08:00:00+05:30`, place again. | 07:59:59 → `store_closed`. 08:00:00 → placed. | `R-5.2` BK-28 | | |
| TIMER-M.37 | In profile B, O20 closes the shop for today in B-14 (holiday or closed-now control). C10 tries to place. | `store_closed`. Record which control B-14 offers. Reopen the shop. | `R-5.2` BK-28 B-14 | ▢ | |
| TIMER-M.38 | The M.33 order is still `placed` at 22:00. K22 accepts it at 22:05. | Record whether accepting after closing time is allowed. 01 only refuses placing; accepting is not addressed (do not mark F; note it). | `R-5.2` | | |

### G. Order numbers across IST midnight (D-23)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.39 | Set Sri Lakshmi hours to all day (O20, B-14). `clock set 2026-10-22T23:59:58+05:30`. C10 places a pickup order. Then `clock set 2026-10-23T00:00:02+05:30` and C11 places one. Then C12 places one at Auto Mart. | Three numbers in increasing order, `TZ-` and at least 6 digits, zero-padded. No reset to a small number after midnight; Auto Mart's number continues the same sequence. | `R-5.16` BK-34 D-23 | ▢ | |

### H. Delivery-failed return clock (D-69)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.40 | `make reset`; `clock set 2026-10-22T11:00:00+05:30`. C11 places a delivery order to Home (I4 ×1). K22 accepts, dispatches to R23. R23 marks Delivery failed, reason "Customer refused", at **TF**; on "Did the goods come back?" tap No. | `delivery_failed`. R23 sees "Ask a manager to record damage". The order sits in Returns to confirm. I4 reserved 1. | `R-5.22` BK-26 B-04 | | |
| TIMER-M.41 | `clock set` TF + 23 h 59 min. Open B-16. `clock set` TF + 24 h 01 min. Refresh. | One reminder (from 2 h) before; a second reminder after 24 h, linking to the order in Returns to confirm. | `R-5.22` BK-26 B-16 | ▢ | |
| TIMER-M.42 | `clock set` TF + 71 h 59 min. Refresh B-04. | Still in Returns to confirm. I4 still reserved 1. | `R-5.22` BK-26 | | |
| TIMER-M.43 ★ | `clock set` TF + 72 h 01 min. Refresh B-04 and A-13. | The order leaves the tab with "Auto-confirmed after 72 h". `return_outcome = unconfirmed`. I4 reserved 0, on hand unchanged. One risk flag in A-13 for it. | `R-5.22` BK-26 D-69 B-04 A-13 | ▢ | |

### I. Sign-in timers

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.44 | Signed out, enter C18's number on C-10 and send the OTP at **TO**. `clock set` TO + 29 s. Tap Resend. | Resend disabled with the countdown. Via cURL, a resend → `429` with `Retry-After` and `RateLimit-*` headers. | `R-2.1` AU-04 C-10 | | |
| TIMER-M.45 | `clock set` TO + 31 s. Tap Resend. | A new OTP is sent (a second message in the mocks for C18). | `R-2.1` AU-04 | | |
| TIMER-M.46 | Send a new OTP at **TO2**. `clock set` TO2 + 4 min 59 s. Enter `000000`. | Signed in. Sign out. | `R-2.1` AU-03 | | |
| TIMER-M.47 ★ | Send a new OTP at **TO3**. `clock set` TO3 + 5 min 01 s. Enter `000000`. | Refused `otp_expired`. C-10 shows the expired message translated, with a resend action. | `R-2.1` AU-03 C-10 `otp_expired` | ▢ | |
| TIMER-M.48 | Sign in as C18. Add I1 to the cart. `clock +15m1s`. Tap any screen that calls the API. | The access token expired; the app refreshed silently once and the call succeeded. No sign-in screen. DevTools shows one `POST /v1/auth/refresh` then the retried call. | `R-2.7` AU-08 | | |
| TIMER-M.49 | `clock +30d1m`. Tap Checkout. | The refresh token (30 days) has expired. The sign-in screen shows and the cart is still there after signing in again. | `R-2.7` AU-08 C-10 | ▢ | |
| TIMER-M.50 | Sign in to the admin console as AD47 at **TAD**. `clock set` TAD + 11 h 59 min, open A-07. Then `clock set` TAD + 12 h 01 min, open A-07. | First: works after a silent refresh. Second: back to A-01 sign-in (admin refresh lasts 12 h). | `R-2.7` A-01 | | |

### J. Bill QR, receipt links and the POS reversal window

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.51 | K22 at the POS: I4 ×1, Show bill QR at **TQR**. `clock set` TQR + 119 s. C10 scans in C-17. | Preview "₹245 at Sri Lakshmi · use up to 30 credits". Confirm → claimed. | `R-6.3` POS-07 C-17 | | |
| TIMER-M.52 | New sale, Show bill QR at **TQR2**. `clock set` TQR2 + 121 s. C10 scans. | The expired QR message on C-17; the API returns `claim_expired`; nothing claimed. B-05 lets K22 show a fresh QR. | `R-6.3` POS-08 C-17 `claim_expired` | ▢ | |
| TIMER-M.53 | From the M.51 sale, K22 shares the receipt and copies the `/r/{token}` link at **TR**. `clock set` TR + 29 d 23 h 59 min. Open the link signed out. | C-24 shows the store, date, items and totals, and no customer data. | `R-12.10` C-24 | | |
| TIMER-M.54 ★ | `clock set` TR + 30 d 1 min. Reload the link. | "This receipt link has expired" with a link to the shop page. | `R-12.10` POS-25 C-24 | ▢ | |
| TIMER-M.55 | `clock reset`; `make reset`. K22 makes a sale (I1 ×1) at **TS**. `clock set` TS + 23 h 59 min 59 s. M21 opens sales history. | Reverse is offered; the confirm step shows stock returned I1 +1. Do not confirm. | `R-7.11` B-05 | | |
| TIMER-M.56 ★ | `clock set` TS + 24 h 00 min 01 s. Refresh sales history. Send the reverse request via cURL. | Reverse no longer offered. The API → `409 reversal_window_passed` (`pos_reversal_window_hours` 24). | `R-7.11` POS-02 `reversal_window_passed` B-05 | ▢ | |

### K. A timer setting change and a late job

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| TIMER-M.57 | `set order_accept_timeout_min = 12` (AD47, a minutes key). C10 places a new order. | C-12 shows 12:00 left; B-04 countdown 12:00. Expiry happens at +12 min 01 s, not at +10 min 01 s. Whether an order placed before the change keeps 10 minutes is not stated: record it. Restore to 10. | `R-5.3` `R-5.8` PL-06 G7 | ▢ | |
| TIMER-M.58 | Stop the worker (`infra/local/proc.sh stop worker`). C10 places an order at **TW**. `clock set` TW + 15 min. Start the worker again. Wait 1 minute and refresh. | The order becomes `expired` (`not_accepted`) once, with the expiry time recorded as the server's run time. Credits released once (one release line in A-09). Running the job again (restart the worker) changes nothing. | `R-5.8` BK-21 | | |

## Cleanup

1. `restore defaults`: `order_accept_timeout_min` 10, `shop_expiry_penalty_threshold_bp` 3000.
2. O20 sets Sri Lakshmi hours back to 08:00–22:00 and reopens the shop if closed.
3. Start the worker if stopped. `clock reset`; `make reset`.
