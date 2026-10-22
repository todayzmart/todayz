# MONEY Wallet, credits and payouts

Every rule that moves a credit: the spend rules and their paise boundaries, lots and expiry, a lot that expires while held, the monthly and daily caps across IST midnight and month end, cashback rounding, referrals of every kind, reversals of linked POS sales, and account deletion. MQ-02, MQ-03 and MQ-06 cover the happy paths; this file covers the edges.

- **Covers:** CR-02, CR-03, CR-05, CR-07, CR-08, CR-09, CR-10, CR-11, CR-17, CR-21, CR-24, CR-25, CR-26, CR-28; RF-01 to RF-08, RF-10, RF-11, RF-12; POS-14, POS-19, POS-20, POS-21; AU-18; D-02, D-27, D-28, D-29, D-30, D-31, D-32, D-62, D-73, D-76.
- **Screens:** C-09, C-10, C-11, C-12, C-15, C-16, C-17, C-18, C-20; B-05, B-10, B-14; A-09, A-10, A-13.
- **Devices:** customer P360 (profile A, plus fresh profiles E to I for referral sign-ups); shop D1280 (profile B); admin D1440 (profile D).
- **Time:** about 3 hours. Sections can run on separate days; each starts with its own reset.

## Preconditions

1. `make reset`; `clock set 2026-10-22T11:00:00+05:30` unless a section says otherwise.
2. Seeds `<seed:customers>`, `<seed:staff>`, `<seed:admins>`, `<seed:stores>`, `<seed:items-SL>`, `<seed:standee-SL>` loaded.
3. FN45, FN46 (finance), AD47 (admin), S42 (support) and SA48 (super_admin) have TOTP enrolled.
4. In profile B, O20 sets Sri Lakshmi's hours to all day (00:00–24:00) on the dates used in section D. If B-14 cannot express all-day hours, mark section D **B** and note it.
5. Keep a table of every customer's wallet in your run notes. Expected balances below assume the step order of this file.

Cashback at Sri Lakshmi is 500 bp. Cashback = floor(base paise × `cashback_bp` / 1,000,000), base = eligible amount − credits used (`R-7.7`).

## Steps

### A. Spend rules and boundaries (profile A, C10, wallet 100)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.01 ★ | Sign in as C10. Cart at Sri Lakshmi: I10 QA pack ₹100.00 ×1, pickup. Open checkout and turn credits on. | Copy "Use up to 30 credits on orders of ₹100 or more" (`credit_max_per_order` 30, `credit_min_order_value_paise` 10000). Slider 0 to 30. At 30 the total reads ₹70.00. Do not place. | `R-7.2` `R-7.3` CR-03 C-11 | ▢ | |
| MONEY-M.02 | Copy the `POST /v1/me/checkout/quote` request as cURL (README §5). Change `creditsToUse` to 31 and send it. | Refused with code `credit_max_exceeded`. Record the HTTP status (04 does not name it, Q-10). Nothing is held: C-15 still shows 100 available, 0 held. | `R-7.3` CR-02 | | |
| MONEY-M.03 ★ | Replace the cart with I9 QA pack ₹99.99 ×1. Open checkout. | The credits toggle is off and disabled with the ₹100 rule shown. A quote with `creditsToUse` 30 → `below_credit_minimum` (eligible 9999 paise). | `R-7.3` CR-02 CR-21 D-02 C-09 | ▢ | |
| MONEY-M.04 | Replace the cart with I3 ×1 and I12 ×2 (₹25.00). Quote with `creditsToUse` 10. | `below_credit_minimum`. The order can still be placed without credits (do not place). | `R-7.3` CR-03 | | |
| MONEY-M.05 | Replace the cart with I11 QA pack ₹100.01 ×1. Turn credits on. | Up to 30 allowed. At 30 the total is ₹70.01. | `R-7.3` CR-21 | | |
| MONEY-M.06 | `set credit_max_per_order = 200` (SA48, money key). Cart I10 ×1 (₹100.00), credits on. | The slider goes to 99, not 100: the eligible amount never goes below ₹1 after credits. At 99 the total is ₹1.00. Restore `credit_max_per_order` to 30. | `R-7.3` CR-22 D-29 G7 | ▢ | |

### B. Lots, holds and expiry

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.07 ★ | Sign in as C12 (wallet 60). Open Wallet (C-15). | WalletCard: balance 60, held 0, expiring soon 40 with tomorrow's date. "How credits work" explains 365-day expiry (`credit_expiry_days`) and the 7-day reminder. | `R-7.4` `R-7.9` C-15 | ▢ | |
| MONEY-M.08 | C12 places a pickup order at Sri Lakshmi: I4 Wheat atta ×1 (₹245), 30 credits. In profile D, FN45 opens A-09, finds C12's wallet and opens the lots. | C-15: available 30, held 30. A-09: lot L1 (40, oldest) has 30 held and 10 free; lot L2 (20) untouched. | `R-7.4` `R-7.5` CR-09 A-09 | ▢ | |
| MONEY-M.09 | C12 cancels the order in `placed` with "Changed my mind". Refresh A-09 and C-15. | C-15 history: "Returned: order cancelled" +30. Balance 60, held 0. A-09: L1 back to 40 free, L2 20; no new lot row; L1 still listed first. | `R-7.5` CR-05 CR-25 D-27 | ▢ | |
| MONEY-M.10 | FN45 opens A-09 → Expiry preview. | C12's L1 (40) listed under tomorrow's date. C10's lot (100) listed under its date 365 days after it was earned. Note C10's lot expiry time as **E**. | `R-7.9` A-09 | | |
| MONEY-M.11 | `clock set` E − 7 days − 5 minutes. Open C10's inbox (C-20). Then `clock set` E − 6 days 23 h. Open C-20 again. | No "credits expiring" message at the first time. At the second, one in-app message (and a push if allowed) that 100 credits expire on E's date. If the job runs once a day, record when it arrived: it must be within the 7 days before E, never earlier. | `R-7.9` `R-15.7` CR-08 C-20 | ▢ | |
| MONEY-M.12 | `clock set` E − 1 s. Open C10's wallet. | Balance 100. Nothing expired. | `R-7.9` CR-08 | | |
| MONEY-M.13 ★ | `clock set` E + 1 s. Refresh. In A-09 open C10's ledger entries. | Balance 0. History line "Expired" −100. A-09: one entry customer wallet → `platform_expired` for 100. | `R-7.9` `R-7.13` CR-08 A-09 | ▢ | |

### C. A lot that expires while held (CR-24, D-27)

Run this section twice: run 1 ends delivered, run 2 ends cancelled.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.14 | `make reset`. In A-09 read C12's L1 expiry time as **E1** (tomorrow about 23:59 IST). `clock set` E1's date at 21:30 IST. Sign in as C12, add an address at the QA point, and place a **delivery** order at Sri Lakshmi: I4 ×1 (₹245), 30 credits. | Items ₹245, credits −₹30, delivery fee ₹30, total ₹245. Held 30, all from L1. | `R-5.30` `R-7.5` | | |
| MONEY-M.15 | In profile B, K22 accepts. R23 dispatches. | Order `out_for_delivery`. No dispatch timer remains on it. | `R-5.9` BK-24 | | |
| MONEY-M.16 ★ | `clock set` E1 + 1 s. Refresh A-09 and C-15. | Only L1's 10 free credits expired (one `platform_expired` entry of 10). The 30 held stay held. C-15: available 20 (L2), held 30. | `R-7.5` CR-24 | ▢ | |
| MONEY-M.17 | **Run 1.** `clock +1h`. R23 enters C12's code. | Delivered. The 30 move to Sri Lakshmi's credit account; no further `platform_expired` entry. Cashback floor(21500 × 500 / 1,000,000) = 10 credits as a new lot. Wallet 20 + 10 = 30. | `R-7.5` `R-7.6` `R-7.7` CR-24 | ▢ | |
| MONEY-M.18 | **Run 2.** Repeat M.14 to M.16. `clock +1h`. S42 opens the order in A-07 and admin-cancels it: reason `customer_request`, strike none. | Order `cancelled` (admin). The 30 released go straight to `platform_expired` (one entry of 30). C-15 shows 20 available, 0 held; the wallet does not regain the 30. History shows "Expired" for them. | `R-5.14` `R-7.5` CR-24 | ▢ | |

### D. Caps across IST midnight and month end (D-30, D-76)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.19 | `make reset`. `set store_credit_redemptions_per_day = 2` (AD47, count key). `clock set 2026-10-22T23:50:00+05:30`. C10 and C11 each place a pickup order at Sri Lakshmi with I4 ×1 and 30 credits. | Both placed with 30 held. | `R-7.10` CR-11 | | |
| MONEY-M.20 ★ | Sign in as C12. Cart I4 ×1 at Sri Lakshmi. Open the cart and checkout. | Credits toggle off and disabled with "Credits can't be used at this shop today". Total ₹245 without credits. | `R-7.10` CR-11 D-76 C-09 C-11 | ▢ | |
| MONEY-M.21 | Send the place request with `creditsToUse` 30 via cURL. Then place without credits in the app. | With credits: refused (the code is not named, Q-10; record it), and a risk flag or flag event appears in A-13 (CR-11 "refused and flagged"). Without credits: `placed`. | `R-7.10` CR-11 | | |
| MONEY-M.22 | `clock set 2026-10-22T23:59:59+05:30`. C12 opens a new cart I1 ×2 at Sri Lakshmi and checks out. | Still "Credits can't be used at this shop today". | `R-7.10` CR-26 | | |
| MONEY-M.23 ★ | `clock set 2026-10-23T00:00:00+05:30` (18:30:00 UTC). Refresh checkout. | Credits offered again, up to 30. Place it with 30 credits: `placed`. | `R-7.10` CR-26 D-30 | ▢ | |
| MONEY-M.24 | Restore `store_credit_redemptions_per_day` to 200. `clock set 2026-10-31T23:50:00+05:30`. Sign in as C17 (9 credit-using orders this month, wallet 200). Place a pickup order at Sri Lakshmi with I4 ×1 and 30 credits. | `placed`, 30 held. This is C17's 10th credit-using order this IST month. | `R-7.10` CR-10 | | |
| MONEY-M.25 ★ | C17 starts another cart (I1 ×2) and opens checkout. | Credits toggle off and disabled with "You've used this month's credit limit". Placing via cURL with credits → `credit_monthly_cap`. Placing without credits in the app → `placed`. | `R-7.10` CR-10 D-76 C-11 | ▢ | |
| MONEY-M.26 | `clock set 2026-10-31T23:59:59+05:30`, then `clock set 2026-11-01T00:00:00+05:30`. Refresh checkout each time. | At 23:59:59 still the monthly-cap message. At 00:00:00 credits offered up to 30. `GET /v1/me/wallet` cap info resets with the month. | `R-7.10` CR-26 | ▢ | |
| MONEY-M.27 | Open Grafana dashboard 08 "Marketplace health". | The credits-blocked-by-caps panel counts the refusals from M.20 to M.25 by cap kind. | 05 §4 dashboard 08 | | |

Whether an order counts toward a cap when it is placed or only when its credits are captured, and whether a cancelled order still counts, is not stated (Q-38). Record what you see in M.19 to M.25.

### E. Cashback rounding and the 0% store

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.28 | `make reset`; `clock set 2026-10-22T11:00:00+05:30`. C10 orders at Sri Lakshmi 2 (cashback 0): any two items totalling ₹100 or more, pickup, no credits. M24 accepts and hands over with the code. | Collected. No cashback line in C-15. B-10 for Sri Lakshmi 2 shows no cashback line for this order. | `R-7.7` CR-07 | | |
| MONEY-M.29 | C10 orders I9 ₹99.99 ×1 at Sri Lakshmi, no credits. K22 accepts and hands over. | Cashback floor(9999 × 500 / 1,000,000) = 4 credits. History "5% back at Sri Lakshmi" +4. | `R-7.7` CR-06 | | |
| MONEY-M.30 | C10 orders I10 ₹100.00 ×1 the same way. | Cashback 5. Sri Lakshmi's credit account −5 (the shop funds its cashback). | `R-7.7` `R-9.2` | | |
| MONEY-M.31 | In profile B, O20 opens B-14 → Cashback. Move the slider to the top. Then send `PATCH {S}/settings` with `cashbackBp` 2050. | The slider runs 0 to 20% in 0.5% steps. At 20% the example reads "On ₹500 the customer earns 100 credits; you fund it". The API refuses 2050 (record the status and field). Set it back to 5%. | `R-7.7` B-14 | ▢ | |

### F. Referrals (RF)

New sign-ups use fresh browser profiles E to I so each has its own device id. Mock-route numbers N101 to N104 are `+91 90000 00101` to `+91 90000 00104` (README §2).

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.32 ★ | Sign in as C10. Open Refer (C-16). | Code, share link, "how it works": C10 gets 30, the friend gets 100 (`ref_customer_customer_referrer_credits`, `ref_customer_customer_referee_credits`), unlocked by the friend's first order of ₹100 or more, link valid 7 days (`ref_attribution_window_days`). List shows Divya (C14) pending. | `R-8.1` `R-8.2` `R-8.3` C-16 | ▢ | |
| MONEY-M.33 | Tap Share. | The Web Share sheet opens with WhatsApp text that includes the link. Cancel. | C-16 | | |
| MONEY-M.34 ★ | In fresh profile E open C10's link. Sign up as C01. | The link goes through `/v1/public/ref/{code}` and lands in the app. On C-10, the new-user step shows the referral code prefilled. After sign-up, C10's list shows C01 pending. No credits for either. | `R-8.3` RF-01 C-10 | ▢ | |
| MONEY-M.35 | As C10, send `POST /v1/me/referrals/claim` with C10's own code. As C01, send it with C11's code. | Both refused (self-referral; second claim). Record the codes. C01 stays attributed to C10. | `R-8.4` RF-05 | | |
| MONEY-M.36 | C01 places a pickup order at Sri Lakshmi: I4 ×1 (₹245). Cancel it in `placed`. | C10's list: C01 still pending. | `R-8.2` RF-11 | | |
| MONEY-M.37 | C01 orders I5 ×2 (₹80); K22 accepts and hands over. | Collected. C01 +4 cashback. Referral still pending (₹80 is under ₹100, `ref_qualifying_min_order_paise`). | `R-8.2` RF-03 | | |
| MONEY-M.38 | FN45 in A-09 posts an adjustment of +₹30 to C01's wallet with reason "QA MONEY-M.38". | Posts at once (₹30 is at or under ₹500, `admin_adjust_dual_approval_above_paise`), no request row. A new lot of 30 in A-09. C01 wallet 34. Record the history copy for an adjustment. | `R-7.4` `R-7.12` CR-15 CR-25 | | |
| MONEY-M.39 | C01 orders I3 ×5 (₹120) with 30 credits; K22 accepts and hands over. | Qualifying amount ₹120 − ₹30 = ₹90 → still pending. Cashback floor(9000 × 500 / 1,000,000) = 4. Wallet 34 − 30 + 4 = 8. | `R-8.2` RF-03 D-32 | | |
| MONEY-M.40 ★ | C01 orders I4 ×1 (₹245), no credits; K22 accepts and hands over. | Referral unlocked. C10 +30 "Referral bonus for <C01's name>'s first order"; C01 +100. A-09: both from `platform_marketing`, not from Sri Lakshmi. C01 wallet 8 + 12 (cashback) + 100 = 120. Both get an in-app message (C-20). | `R-7.8` `R-8.2` `R-15.7` RF-02 C-20 | ▢ | |
| MONEY-M.41 | Fresh profile F: open C10's link, then C11's link. Sign up as C02. | C02 is attributed to C10 (first touch). C11's list does not show C02. | `R-8.3` RF-07 | | |
| MONEY-M.42 | Profile B as K22: POS sale I4 ×1 (₹245), Link customer → Phone only, C02's number. | Sale saved, label "Adds to your customer list · no credits or cashback". C02's referral stays pending; no ledger entry. | `R-6.5` RF-12 POS-17 | | |
| MONEY-M.43 | Fresh profile G: open C11's link. `clock +6d23h59m`. Sign up as C03. | C03 attributed to C11 (pending). | `R-8.3` RF-08 | | |
| MONEY-M.44 | Fresh profile H: open C11's link. `clock +7d0h0m1s`. Sign up as C04. | No referral for C04. C11's list does not show C04. | `R-8.3` RF-08 | | |
| MONEY-M.45 | `set ref_attribution_window_days = 3` (AD47). Profile H, signed out: open C11's link, `clock +3d0h0m1s`, sign up as N101. Restore the key to 7. | No attribution for N101: the new window applied without a deploy. | `R-8.3` RF-08 PL-06 | | |
| MONEY-M.46 ★ | K22 at the POS: I4 ×1 (₹245), Show bill QR. C03 scans in C-17 and confirms with 0 credits. Charge. | Sale linked by `bill_qr`. C03's referral unlocks: C11 +30, C03 +100, plus 12 cashback to C03. | `R-8.2` POS-14 C-17 | ▢ | |
| MONEY-M.47 | `set ref_customer_customer_referee_credits = 150` (SA48, money key). C02 (signed up before the change) completes a ₹245 pickup order. Then fresh profile I opens C10's link, signs up as C05 and completes a ₹245 order. Restore to 100. | C02 gets 100 (the amount when they signed up). C05 gets 150. C10 gets 30 for each. | `R-8.1` RF-10 | | |
| MONEY-M.48 | Profile E (C01's device): sign out. Open C10's link and sign up as N102. Then sign out and sign up as N103 through C10's link. | N102: referral recorded `rejected_same_device`, no reward ever. N103: also `rejected_same_device`, and one risk flag in A-13 for 3 accounts on one device. | `R-8.4` RF-06 A-13 | ▢ | |
| MONEY-M.49 | `clock +91d`. Profile E: open C11's link, sign up as N104. | A normal pending referral (the device's last new account was over 90 days ago). | `R-8.4` RF-06 | | |
| MONEY-M.50 | Fresh profile with the standee URL `https://app.todayz.localhost/s/<SL slug>?r=<SL short code>&s=<standee_id>`. Sign up as a new mock-route number. Complete a ₹245 pickup order at Sri Lakshmi. | Referral kind `retailer_customer`. On collect: the customer +100; Sri Lakshmi's credit account +30 in B-10 as a referral reward (Sri Lakshmi is the primary store, live since 2026-01-10). | `R-3.12` `R-8.1` `R-8.5` RF-04 B-10 | ▢ | |
| MONEY-M.51 ★ | Sign in as AD47. Open A-10 Referrals. Filter by status and by risk-flagged. | C01 rewarded, C02 and C05 rewarded, N102 and N103 `rejected_same_device` with the risk flag shown, N104 pending. | `R-8.4` A-10 | ▢ | |
| MONEY-M.52 | Reject N104's pending referral with reason "QA MONEY-M.52". Then sign in as S42 and open `/referrals`. | N104's referral rejected; nothing posted. S42 sees "You don't have access to this" (A-10 is `admin` and above). | `R-13.2` A-10 AD-02 | | |

`RF-09` (a new shop qualifying after verification and 10 completed orders) is automated only: it needs 10 completed orders at a newly verified shop. See traceability.

### G. Reversing linked POS sales (`R-7.11`)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.53 | `make reset`. Read C12's L1 expiry **E1** in A-09. `clock set` E1's date at 21:30 IST. K22: POS sale I4 ×1 (₹245), Show bill QR. C12 scans and confirms 30 credits. Charge. | Linked by `bill_qr`. 30 spent from L1. Cashback floor(21500 × 500 / 1,000,000) = 10. C12 wallet 60 − 30 + 10 = 40. | `R-6.3` `R-7.3` | | |
| MONEY-M.54 ★ | `clock set` E1 + 1 s. In profile B sign in as M21 and reverse the sale. Read the confirm step, then confirm. | The confirm step shows: stock back I4 +1, credits returned 30, cashback clawed back 10. After: the 30 from L1 go straight to `platform_expired` (L1 is past its date); L1's other 10 already expired at E1. Wallet 20 (L2 only). Sri Lakshmi's account: −30 received and +10 cashback reversed. I4 history shows a `pos_reversal` movement. | `R-7.11` POS-19 D-08 B-05 | ▢ | |
| MONEY-M.55 | `make reset`; `clock set 2026-10-22T11:00:00+05:30`. K22: POS sale I4 ×1 and I1 ×1 (₹303), bill QR, C18 claims with 0 credits. | Linked. C18 +15 cashback (floor(30300 × 500 / 1,000,000)). | `R-7.7` | | |
| MONEY-M.56 | C18 places a pickup order at Sri Lakshmi: I10 ×1 (₹100), 10 credits. Do not accept. | C-15: available 5, held 10. | `R-7.5` | | |
| MONEY-M.57 ★ | M21 reverses the ₹303 sale. | Clawback 5: C18 available 0, held 10 untouched; wallet never negative. Sri Lakshmi absorbs the 10 shortfall. `platform_*` accounts unchanged. | `R-7.11` POS-20 | ▢ | |
| MONEY-M.58 | M21 reverses C03's bill QR sale from M.46 (run this right after section F if the stack was not reset; otherwise repeat M.43 and M.46 first). | The referral reward stays posted (C11 +30, C03 +100 remain). A risk flag is raised in A-13 for a reversed qualifying sale. | `R-7.11` POS-21 A-13 | ▢ | |

The POS-21 branch where the reward has not yet posted needs the reversal to land between qualification and posting. If the reward posts at once, that branch is automated only (Q-39).

### H. Partial accept re-cap and account deletion

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MONEY-M.59 | `make reset`. `set credit_max_per_order = 200` (SA48). C11 (wallet 500) places a pickup order at Sri Lakshmi: I4 ×1 and I1 ×2 (₹361), 200 credits. | 200 held, oldest lots first. Total ₹161. | `R-7.3` `R-7.5` | | |
| MONEY-M.60 ★ | K22 accepts part: removes I4 (new eligible ₹116). C11 reads the confirm screen and confirms. | The confirm screen shows credits re-capped from 200 to 115 (eligible ₹116 − ₹1), 85 returned to the same lots, new total ₹1.00. After confirm: held 115. Restore `credit_max_per_order` to 30. | `R-5.18` `R-7.5` CR-17 D-28 C-12 | ▢ | |
| MONEY-M.61 | Sign in as C16 (one order in `placed`). Open Account (C-18) → Delete account. | Deletion is blocked with an explanation naming the open order. `DELETE /v1/me` → `409 account_has_active_order` naming it. Nothing changes. | `R-16.6` AU-18 BK-44 C-18 | ▢ | |
| MONEY-M.62 ★ | Sign in as C12 (wallet 60 in 2 lots, no open order). Account → Delete account. Read the confirm text, enter the OTP. | The confirm text says the wallet balance of 60 credits is forfeited. After confirm: signed out. A-09: one `forfeit_deletion` entry customer wallet → `platform_expired` for 60; both lots closed. A-06 shows the customer anonymised; past orders kept without name or phone. | `R-16.6` AU-18 CR-28 D-31 A-06 A-09 | ▢ | |
| MONEY-M.63 | Sign in again as C12 (+91 90000 00012). | A new, empty profile (wallet 0, no orders). The old orders are not linked to it. | `R-16.6` AU-18 | | |

## Cleanup

1. Restore defaults: `credit_max_per_order` 30, `store_credit_redemptions_per_day` 200, `ref_attribution_window_days` 7, `ref_customer_customer_referee_credits` 100.
2. O20 sets Sri Lakshmi's hours back to 08:00–22:00 and cashback to 5%.
3. `clock reset`; `make reset`.
