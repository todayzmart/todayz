# ABUSE Abuse cases

A tester acting as a bad customer, a bad shop or a script. Each step checks that the limit holds, the honest user is not hurt, and the right risk flag lands in A-13. The automated abuse suite (`e2e/security`, `make security`) runs the same ideas at volume; this file is the hands-on pass.

- **Covers:** SC-RK; SC-RF (abuse rows); RF-05, RF-06; CR-10, CR-11; POS-23; BK-12; POS-25; R-2.1, R-5.26, R-6.1, R-6.3, R-6.7, R-7.10, R-8.4, R-10.1, R-10.2; D-13, D-67, D-76.
- **Screens:** C-10, C-11, C-14, C-16, C-17; B-04, B-05, B-11; A-06, A-13, A-15.
- **Devices:** P360 profiles A, B and C (three fresh browser profiles count as three devices); D1280 (B); D1440 (D). Terminal for the cURL steps.
- **Time:** about 75 minutes.

## Preconditions

1. `make reset`. Mocks UI open. `clock reset`.
2. Rate limits are on in local (record if a dev flag turns them off; then mark the limit steps **B**).
3. Never paste an OTP, order code or counter code into the run notes. Record counts and statuses only.

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ABUSE-M.01 ★ | From one terminal, send OTP requests for C01 (`+91 90000 00001`) 6 times inside one hour (`POST /v1/auth/otp/send`, wait 30 s between). | Sends 1 to 5 succeed; the 6th returns `429` with `Retry-After`. The mocks SMS tab shows exactly 5 messages for that number. | `R-2.1` AU-04 | | |
| ABUSE-M.02 | From the same IP send one OTP each to 21 different reserved numbers (`+91 90000 00030` … `+91 90000 00050`) inside one hour. | The 21st returns `429`. A different number from profile C on a second IP (phone hotspot, if available) still gets its OTP. Record whether a flag appears on dashboard 12 Security and abuse. | `R-2.1` `otp_abuse` | | |
| ABUSE-M.03 | Enter a wrong OTP 5 times for C02, then the right one. | `otp_locked` after the 5th; the right OTP is refused until a new OTP is sent. | `R-2.1` AU-03 | | |
| ABUSE-M.04 | C10 creates a pickup order at SL; K22 opens it in B-04. Type 5 wrong codes, then try the right code. | Attempts 1 to 4: "Wrong code" with no hint. 5th: `code_locked` and "Ask customer for a new code". The right old code is refused. C10 taps New code; the new code collects. A-07 shows 5 attempts (count only). | BK-12 `R-6.1` | ▢ | |
| ABUSE-M.05 | `set risk_code_lockouts_store_per_day = 2` (AD47, a count key). As K22 call the collect route for 3 other placed-and-accepted orders with 5 wrong codes each. Then restore the setting to 10. | Each order locks on its own count (5 wrong tries, 01 §6); there is no store-wide lock. The per-store risk flag appears on A-13 on the third lockout and not before. | `R-6.1` | | |
| ABUSE-M.06 ★ | Self-referral: C10 opens their own referral link in profile A and signs up again with C10's phone. | Refused; no referral row. | RF-05 `R-8.4` | | |
| ABUSE-M.07 | Same device: in profile A (C10's device) open C10's link and sign up as C03. | Recorded as `rejected_same_device`; no reward, never qualifies. A-15 shows the row. | RF-06 D-13 | ▢ | |
| ABUSE-M.08 ★ | Mass referrals: in profile B (a fresh device) sign up C04, then sign out and sign up C05 with the same profile, then a third new number `+91 90000 00006`, each from a link of C10. | Third account on one device: a risk flag in A-13 with the device and the three accounts. Rewards none (same-device rule). | RF-06 `R-8.4` SC-RK | ▢ | |
| ABUSE-M.09 | Customer credit cap: as C17 (9 credit orders this month) place a 10th order with credits, then try an 11th with credits. | 10th allowed. 11th quote shows "You've used this month's credit limit"; placing with credits → `credit_monthly_cap`; without credits → `placed`. | CR-10 `R-7.10` `credit_orders_per_month` D-76 | ▢ | |
| ABUSE-M.10 | Store redemption cap: `set store_credit_redemptions_per_day = 2` (SA48; count). C10 and C11 each place an order with credits at KF. As C12 open a quote with credits at KF. Restore 200. | The 3rd quote shows "Credits can't be used at this shop today". Placing without credits → `placed`; with credits → refused and a risk flag. | CR-11 `R-7.10` `store_credit_redemptions_per_day` | | |
| ABUSE-M.11 ★ | Offline link flood: `set pos_offline_links_risk_per_day = 2` (AD47; count). As K22 at SL go offline and record 3 sales linked by counter code (C10, C11, C12 read their codes). Go online. Restore 20. | All 3 verify normally. After the 3rd (more than 2) a risk flag for SL appears in A-13. With the default 20, the 21st link of the day raises it (POS-23). | POS-23 `R-6.7` | ▢ | |
| ABUSE-M.12 | Store strikes: `set store_strike_risk_threshold = 2` (AD47). Let 2 KF delivery orders expire with `dispatch_timeout` (`clock +` the dispatch timeout each). Restore 5. | Two strikes on KF; a `store_strikes` risk flag in A-13. KF is not paused (no automatic pause, D-67). | `R-5.26` D-67 | | |
| ABUSE-M.13 | Receipt enumeration: request `GET /v1/public/receipts/{token}` with 30 random 128-bit tokens in a minute, then a valid token. | Each random token: not found, same timing as a valid lookup (within a few ms). After the rate limit: `429`. The valid token later shows items, totals and the store only. | POS-25 | | |
| ABUSE-M.14 | Review spam: as C10 review the same order twice, then edit it on day 8 (`clock +8d`). | Second review refused (one per order). Edit after 7 days refused. Record the codes. | `R-10.1` | | |
| ABUSE-M.15 | Report abuse: as O20 report a review on SL 10 times from B-11. | One open report per reporter per review (record the rule the build applies). A-12 shows the report once with a count. | `R-10.2` A-12 | | |
| ABUSE-M.16 | Fake bill QR claim: copy a bill QR token from POS at SL, then claim it as C10 and again as C11 in another profile. | The first claim holds the bill; the second is refused. Replaying C10's claim request returns the same result (idempotent), never a second hold. | POS-07 `R-6.3` | | |

## Cleanup

1. `restore defaults`: `store_credit_redemptions_per_day` 200, `pos_offline_links_risk_per_day` 20, `store_strike_risk_threshold` 5.
2. Clear the QA risk flags in A-13 with reason "QA cleanup". `clock reset`; `make reset`.
