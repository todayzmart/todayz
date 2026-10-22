# End-to-end acceptance

One continuous story across all four apps, run in one sitting on a clean reset, in this order: onboard a shop, digitise its stock, a customer finds, books and picks up, a delivery, POS sales online and offline, the month close, the payout, admin actions, and the dashboards. Every step depends on the one before, so stop at the first **F** and file it; do not skip ahead.

The story uses a new shop, **QA Kirana (QK)**, so its money is exact and separate from the seeded Lakshmi Traders month (₹890).

- **Covers:** SC-E2E; E2E-01, E2E-04, E2E-05, E2E-06, E2E-07, E2E-08, E2E-09, E2E-12; ST-02, ST-03, ST-04; RF-04; BK-22; OB-01, OB-06, OB-11; R-3.4, R-4.2, R-5.17, R-5.24, R-6.1 to R-6.7, R-7.7, R-8.2, R-9.3 to R-9.5, R-14.1, R-14.2, R-16.3.
- **Screens:** A-02, A-03, A-04, A-05, A-07, A-11, A-13, A-15, A-17, A-19; B-01 to B-05, B-07, B-10, B-12, B-14; C-03, C-05, C-07, C-09 to C-15, C-17, C-21, C-24.
- **Devices:** owner P360 (profile C); field agent P390 (profile E); cashier D1280 (profile B); rider P360 (profile F); customers P390 (profile A) and P360 (profile G); admin D1440 (profile D). Grafana on D1440.
- **Time:** about 3 hours.
- **Who signs:** QA lead, product owner (the user), and the finance and support leads for parts 6 to 8.

## Preconditions

1. `make dev`; `make reset`; `clock reset`. Seeds `<seed:month-SL>`, `<seed:payout-history>`, `<seed:stores>`, `<seed:admins>` loaded.
2. New phones for this run (reserved, OTP 000000): QK owner `+91 90000 00060`, QK cashier `+91 90000 00061`, QK rider `+91 90000 00062`, new customer C01 `+91 90000 00001`.
3. Worked money for QK (cashback rate 5% = `cashback_bp` 500, set in ACC-04; cashback = floor(base paise × 500 / 1,000,000) credits, base = eligible amount − credits used):

   | Step | Event | QK credits in | QK cashback out |
   |---|---|---|---|
   | ACC-16 | C01 pickup ₹240, no credits | | 12 |
   | ACC-18 | C01 qualifies the standee referral | 30 | |
   | ACC-24 | C10 delivery ₹300 + ₹20 fee, 30 credits | 30 | 13 |
   | ACC-27 | C01 POS bill QR ₹480, 30 credits | 30 | 22 |
   | ACC-31 | C11 POS offline counter code ₹200 | | 10 |
   | ACC-29 | Walk-in phone only ₹150 | | |
   | **October** | | **₹90** | **₹57** |

   Expected October statement for QA Kirana: opening ₹0, in ₹90, cashback ₹57, closing **₹33**.

## Steps

### 1. Onboard a shop

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-01 ★ | As F40 sign in to the admin console on P390 (OTP + TOTP). Open A-04 Create business. Enter QA Kirana: owner phone `+91 90000 00060`, address in Indiranagar, PIN 560038, category Grocery, hours 08:00–22:00, pickup and delivery on, store photo. | A-04 asks for the owner OTP. An SMS to the owner number shows in the mocks tab. | `R-14.1` `R-14.2` A-04 E2E-08 | ▢ | |
| ACC-02 | The owner reads the OTP from the mocks tab and F40 types it. | Business created as `submitted`; owner set to `+91 90000 00060`; A-15 attributes it to F40. | `R-14.2` A-15 | | |
| ACC-03 | In profile C sign in as the owner on the business app. | B-01 shows QA Kirana. B-03 shows a "Submitted, waiting for review" banner and the setup checklist. | B-01 B-03 | ▢ | |
| ACC-04 | Owner opens B-14 Settings: payout details UPI `qakirana@upi` (synthetic), cashback 5%, delivery radius 3 km, fee ₹20, minimum ₹150, auto-accept off. Adds staff: cashier `+91 90000 00061`, rider `+91 90000 00062` (delivery flag). | Saved. Payout details masked after save. Staff list shows both with roles. | `R-3.6` B-13 B-14 | | |
| ACC-05 ★ | As AD47 open A-03 → QA Kirana → review KYC and documents → Verify. Open A-05 → QK store → Approve. | Business `verified`; store `live`. Both audited. | `R-3.3` `R-3.4` E2E-08 | ▢ | |

### 2. Digitise stock

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-06 ★ | As F40 (inside the 30-day window) on `/v1/admin/field/...` or the A-04 items step, add 5 master items to QK: I1 Amul Butter ₹58 ×20, I2 Maggi ₹14 ×50, I3, I4, I5 with quantities. | Items appear in B-07 for QK with the quantities, each with a `restock` movement by F40. | FA-01 `R-14.3` B-07 | | |
| ACC-07 | Owner on B-07 scans 3 more barcodes (camera), sets prices and quantities, and creates one missing product "QA Haldi 100 g" ₹30 ×10. | All 9 items live in QK. The missing product is usable at once and appears in the A-08 review queue. | `R-4.3` B-07 | ▢ | |
| ACC-08 | Owner imports a 20-row CSV of items (`<seed:import-QK>` or a file built from the template) with 1 bad row. | Preview shows 19 good and 1 error. Apply adds 19 items. Stock count total matches the file. | `R-4.4` B-07 | | |
| ACC-09 | Owner sets I6 (last unit) to quantity 1. | B-07 shows 1; the low-stock marker shows (a quantity at or below the item's threshold is low, 01 §4). | `R-4.7` | | |
| ACC-10 | As G43 promote "QA Haldi 100 g" to master from A-08. | A master product exists; QK's item links to it. Adding it to another store adds no stock there. | `R-4.2` A-08 | | |
| ACC-11 | Owner downloads the QR standee PDF from B-10 and scans its QR with a phone camera. | URL `https://app.todayz.localhost/s/<qk-slug>?r=<qk short code>&s=<standee id>`; no other code in it. | ST-01 `R-3.12` B-10 | ▢ | |

### 3. A customer finds, books and picks up

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-12 ★ | In profile A (fresh) open the standee URL from ACC-11. | QA Kirana shop page (C-07) opens without sign-in. One scan recorded against that standee (B-10 standee analytics: scans 1). | ST-02 ST-03 C-07 | ▢ | |
| ACC-13 | Open the same page with a made-up `s=` value and with another store's standee id. | The shop page still opens; scans for QK's standee stay at 1 (not attributed). | ST-03 | | |
| ACC-14 | From C-03 Home (location Indiranagar) search "butter". | Results grouped by shop; QA Kirana shows Amul Butter ₹58 with freshness "updated … ago". | `R-11.3` C-05 E2E-01 | | |
| ACC-15 ★ | Add I1 ×2, I2 ×5 and 3 more items to reach ₹240. Checkout, pickup. Sign in as C01 (new, `+91 90000 00001`) when asked. Place. | Sign-in keeps the cart. Order `placed` with `TZ-` number; accept-by in 10 min. Referral pending (kind `retailer_customer`, referrer QK). | E2E-01 RF-04 `R-8.2` C-11 C-12 | ▢ | |
| ACC-16 ★ | QK cashier (profile B, `+91 90000 00061`) gets the alert, accepts, marks ready. C01 shows the pickup code at the counter; cashier types it. | `collected`. Stock for each line drops by its quantity. C01 earns 12 credits (C-15). QK credit account −₹12 (B-10). | E2E-01 BK-11 `R-6.1` `R-7.7` | ▢ | |
| ACC-17 | C01 leaves a 5-star review with "Fresh stock". | Shown on QK's shop page at once; QK rating "5.0 · 1 review" or the Bayesian value (4.2 with prior 4.0 × 5; record). | E2E-01 `R-10.1` `R-10.4` | | |
| ACC-18 ★ | Read referrals after the collect. | The standee referral qualified (₹240 ≥ ₹100): QK +₹30 (B-10), C01 +100 credits (C-15 now 112). B-10 standee analytics: scans 1, sign-ups 1, qualified 1. | E2E-01 ST-04 RF-04 `R-8.2` | ▢ | |
| ACC-19 | No-show path (E2E-04): as C02 (new, `+91 90000 00002`) place a ₹100 pickup at QK; the cashier accepts; `clock +31m` (`pickup_hold_min` 30). Repeat twice more. Then place two pickup orders at once. | Each expires as `no_show` and adds a strike (A-06 shows 1, 2, 3). After the 3rd, C-21 explains the limit of 1 active order for 14 days (`noshow_strikes` 3, `noshow_window_days` 30, `noshow_limit_days` 14). The first new order is placed; the second is refused with `account_has_active_order`. Cancel the open one. | E2E-04 `R-5.24` C-21 | ▢ | |

### 4. A delivery

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-20 ★ | `clock reset`. C10 (wallet 100) at a saved address inside 3 km orders ₹300 of items from QK for delivery with 30 credits. | Quote: items ₹300, credits −₹30, delivery ₹20, total ₹290. Credits never cover the fee. | E2E-05 `R-7.3` C-11 | ▢ | |
| ACC-21 | Cashier accepts; the phone in B-04 shows masked (`+91 90000 0****` or the build's mask). | Masked phone before dispatch. | E2E-05 `R-16.2` | | |
| ACC-22 | Owner assigns the rider; rider (profile F) marks out for delivery. | Status `out_for_delivery`. The full phone is visible to the rider and the shop only now. C-12 shows "Out for delivery". | E2E-05 `R-6.2` | ▢ | |
| ACC-23 | Rider enters a wrong delivery code once, then the right one. | Wrong: "Wrong code". Right: `delivered`. | `R-6.2` | | |
| ACC-24 ★ | Read C-15 and B-10. Re-open the order in B-04. | C10 wallet 100 − 30 + 13 = 83. QK +₹30 in, −₹13 cashback. The full phone is masked again after delivery. | E2E-05 `R-7.6` `R-7.7` | ▢ | |
| ACC-25 | Auto-accept (BK-22): as O28 (Auto Mart, auto-accept on) receive an order from C11 with all items in stock, then an order with one unavailable item. | First: `accepted` at once. Second: stays `placed` for a manual decision. | BK-22 `R-5.17` | | |

### 5. POS sales, online and offline

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-26 ★ | QK cashier on B-05 rings up ₹480 and taps Show bill QR. C01 scans it in C-17 within 120 s. | Preview "₹480 at QA Kirana · use up to 30 credits". C01 confirms 30. B-05 updates live "claimed by … · 30 credits"; total ₹450. | E2E-06 POS-07 `R-6.3` | ▢ | |
| ACC-27 | Cashier completes with cash. Open the receipt link (C-24). | Sale linked; C01 wallet 112 − 30 + 22 = 104. QK +₹30, −₹22. Receipt shows items, totals and the store only. | E2E-06 `R-12.10` C-24 | ▢ | |
| ACC-28 | Cashier sells ₹120 to a walk-in with no phone. | Sale recorded; no customer; no ledger line. | `R-12.9` | | |
| ACC-29 | Cashier sells ₹150 with phone only `+91 90000 00019` (C19 typed, not verified). | Customer added to B-09; no cashback; no ledger line. | `R-6.5` `R-6.6` | | |
| ACC-30 ★ | Cashier's device checks the clock online, then goes offline (DevTools Offline). C11 shows the counter code; cashier sells ₹200 linked by that code. | Sale saved offline with the typed code and device time; the sync indicator shows 1 pending. | E2E-07 `R-6.4` `R-6.7` | ▢ | |
| ACC-31 | Go online within 12 h (`clock +2h`). | Sync sends the sale; the server verifies the code at the recorded time step; sale linked; C11 +10 credits; QK −₹10. | E2E-07 `R-6.7` | | |
| ACC-32 | Read B-10 for QK. | Balance ₹33 (in 90, cashback 57) as in precondition 3. Lines grouped by event with order or sale numbers. | `R-9.1` `R-9.2` | ▢ | |

### 6. Month close

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-33 ★ | `clock set 2026-11-01T00:30:30+05:30` (or 00:30:30 IST on the 1st of the next month on another date; statements close at 00:30 IST). As FN45 open A-11 for October. | Statements for QA Kirana (opening ₹0, in ₹90, cashback ₹57, closing ₹33, KYC verified) and Lakshmi Traders (closing ₹890). Numbers exact to the paisa. | E2E-09 `R-9.3` A-11 | ▢ | |
| ACC-34 | As the QK owner open B-10 → Statements. | October: closing ₹33, status "Pending". | `R-9.3` B-10 | | |

### 7. Payout

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-35 ★ | FN45 opens QK's payout (₹33), pays it by UPI outside TodayZ (no real money in QA: skip), clicks Mark paid with UTR `QAUTR0000000010` and the TOTP step-up. | Status `paid`, UTR, paid-by FN45, time. Audit row. | E2E-09 `R-9.5` | ▢ | |
| ACC-36 | Owner refreshes B-10. | October "Paid" with UTR `QAUTR0000000010`. Balance after the payout ₹0. | E2E-09 `R-9.5` | ▢ | |

### 8. Admin actions

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-37 ★ | SA48 changes `credit_max_per_order` 30 → 40 in A-17 with reason "ACC-37" and step-up. C10 opens a quote at QK with ₹500 of items. | The quote allows 40 credits. A-19 shows the change with before 30 and after 40. Restore 30. | E2E-12 `R-17.1` | ▢ | |
| ACC-38 | AD47 hides C01's review from ACC-17 with reason "ACC-38", then restores it. | Hidden: gone from the shop page; rating recalculates. Restored: back. Both audited. | E2E-10 `R-10.3` A-12 | | |
| ACC-39 | C10 places a ₹200 pickup at QK; the cashier accepts. S42 cancels it in A-07 on C10's behalf, fault "shop", reason "ACC-39". | Order cancelled; held credits released; QK gets one strike. Audit row with the reason. | `R-5.25` A-07 | | |
| ACC-40 | AD47 opens A-13. | Any flags raised in this run (none expected unless a step said so) are listed with evidence. | `R-13.1` A-13 | | |

### 9. Dashboards

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ACC-41 ★ | Open A-02 Pulse for today and for October. | Orders, GMV, live stores (QK counted) and new stores match the run: QK's collected and delivered orders (₹240 and ₹300) appear in GMV; record whether GMV includes the ₹20 delivery fee and the POS sales (A-02 KPI definition). | `R-13.1` OB-11 A-02 | ▢ | |
| ACC-42 | Open Grafana dashboards 08 Marketplace health and 10 Retailer health. | QK's orders and acceptance show in the totals. No store id, phone or order id used as a metric label. | OB-11 05 §4 | ▢ | |
| ACC-43 | Pick one ACC request (the ACC-16 collect), copy its trace id, open it in Tempo, Loki and the metric exemplar. | One trace id across all three. No phone, code or OTP anywhere. | OB-01 OB-02 | | |
| ACC-44 | Open dashboard 05 Customer app experience and 06 Business app and POS experience for the run's time window. | RUM rows for the run's screens (C-07, C-11, B-05) within 2 minutes of use. | OB-06 | ▢ | |
| ACC-45 | Open the A-02 city map and B-03 Demand insights for QK. | Searches made in this run appear only as area counts; nothing below 10 people per area is shown. On B-03 a Wanted nearby row shows "N searches within 2 km this week" only when the summed count is 10 or more, and it never names another shop or counts shops. | `R-16.3` `R-16.5` IN-06 IN-20 | | |

## Cleanup

1. `restore defaults`; `clock reset`; `make reset`. Remove QA Kirana screenshots that show phone numbers in full (none should).

## Sign-off

| Part | Steps | Role | Name | Date | Result (P/F/B) | Notes |
|---|---|---|---|---|---|---|
| 1. Onboard a shop | ACC-01 to ACC-05 | QA lead | | | | |
| 2. Digitise stock | ACC-06 to ACC-11 | QA lead | | | | |
| 3. Find, book, pick up | ACC-12 to ACC-19 | QA lead | | | | |
| 4. Delivery | ACC-20 to ACC-25 | QA lead | | | | |
| 5. POS online and offline | ACC-26 to ACC-32 | QA lead | | | | |
| 6. Month close | ACC-33 to ACC-34 | Finance lead | | | | |
| 7. Payout | ACC-35 to ACC-36 | Finance lead | | | | |
| 8. Admin actions | ACC-37 to ACC-40 | Support lead | | | | |
| 9. Dashboards | ACC-41 to ACC-45 | QA lead | | | | |
| **Whole run** | ACC-01 to ACC-45 | Product owner | | | | |
