# MQ-03 ★ Shop day

One day at Sri Lakshmi: staff sign in, the new-order sound plays, orders are accepted, partly accepted and rejected, the counter rings 10 sales with a barcode scanner, 5 more sales happen offline and sync, and a walk-in customer pays part with credits through the bill QR.

- **Covers:** 08 §5 MQ-03 (sign-in, sound alert, accept/partial/reject, POS 10 sales with scanner, offline 5 sales, sync, bill QR with customer phone).
- **Screens:** B-01, B-03, B-04, B-05, B-07, B-12, B-16; C-12, C-13, C-15, C-17, C-24.
- **Devices:** counter desktop D1280 (profile B, USB scanner that types as a keyboard); shop phone P360 (profile C); customer phone P390 (profile A).
- **Time:** about 75 minutes.

## Preconditions

1. `make reset`; `clock reset`; run between 10:00 and 19:00 IST.
2. Stock at SL as seeded: I1 10, I2 50, I3 30, I4 6, I5 12, I7 8, I8 20, I9 50, I12 100, I14 20.
3. Customer wallets: C10 100, C11 500, C12 60, C18 0.
4. Settings at defaults. Mocks UI open.
5. A printed or on-screen barcode for `8901262010016` (I1), `8901058000177` (I2) and an unknown `8900000000001`.

## Steps

### A. Sign-in and picker

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-03.01 | In profile B sign in as X25 (`+91 90000 00025`, OTP `000000`). | B-01 picker lists two businesses with the role shown: Annapurna Provisions · Owner, Lakshmi Traders · Cashier (Sri Lakshmi). | `R-2.4` B-01 | ▢ | |
| MQ-03.02 | Pick Lakshmi Traders. Sign out. Sign in again as X25. | The picker preselects Lakshmi Traders (last store remembered). | `R-2.4` B-01 | | |
| MQ-03.03 ★ | Sign out. Sign in as O20 (owner). | Picker shows Sri Lakshmi and Sri Lakshmi 2, role Owner. Pick Sri Lakshmi. B-03 Overview opens with the mint greeting band, store switcher and a sync state such as "Synced 6:40 pm". | `R-2.4` B-01 B-03 | ▢ | |
| MQ-03.04 | Read B-03. | KPI strip (today's sales with delta, orders, avg bill, items low), sales by hour chart, DemandInsights card, top items table. Setup checklist card absent (SL is fully set up) or showing all four items done (KYC, 50 products, staff, QR standee printed). | `R-12.1` B-03 | ▢ | |
| MQ-03.05 | In profile C (P360) sign in as K22 (cashier). | Lands on Overview for Sri Lakshmi; bottom tabs Overview, Orders, Sell, Stock, More. | `R-3.9` B-01 | ▢ | |

### B. New-order alert

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-03.06 | In profile C open Orders. Accept the "Allow sound" prompt. Then go to the Stock tab. | Sound allowed. Stock screen shows. | B-04 `R-15.2` | | |
| MQ-03.07 ★ | In profile A sign in as C11 and place pickup order **O-A** at SL: I1 ×2, I2 ×2, I5 ×1 (₹184), no credits. | Order placed; C-12 shows "Waiting for Sri Lakshmi to accept · 10:00 left". | BK-01 C-12 | | |
| MQ-03.08 ★ | Watch profile C (still on Stock) and profile B (on Overview). | Within 3 s on both: a new-order toast and a sound loop, on whatever screen is open. B-03 shows a "New orders" card with Accept buttons. Orders tab badge 1. | `R-15.2` BK-01 B-03 B-04 | ▢ | |
| MQ-03.09 | Do not open the order. `clock +3m1s`. Open the mocks SMS tab. | One SMS to the Sri Lakshmi staff phone about a new order not yet seen (3 min escalation). Crop the phone before saving. See Q-26 for reserved numbers. | `R-15.2` NOTIFY-M | ▢ | |
| MQ-03.10 | Open the order in profile C. | Sound stops on this device. Countdown about 6:5x (10:00 − 3:01). | `R-5.3` B-04 | | |
| MQ-03.11 ★ | Click Accept. | O-A in Accepted. Profile A shows Accepted with the PickupCode within 3 s. | `R-5.12` BK-05 | ▢ | |

### C. Partial accept and reject

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-03.12 ★ | In profile A sign in as C12 (wallet 60). Place pickup order **O-B** at SL: I3 ×4 (₹96), I5 ×2 (₹80), I8 ×2 (₹70) = ₹246, using 30 credits. Total ₹216. | Placed. Wallet available 30, held 30. | `R-7.5` CR-01 | | |
| MQ-03.13 ★ | In profile C open O-B, click Accept part. Remove I5 and set I3 to 2. Confirm. | O-B shows "Waiting for customer" (awaiting_customer). | `R-5.18` BK-08 B-04 | ▢ | |
| MQ-03.14 ★ | In profile A read C-12. | Confirm screen: changed lines (I3 4 → 2, Coca-Cola removed), new items total ₹118, credits 30 (still allowed: ₹118 ≥ ₹100), new total ₹88. Countdown 5:00 (`partial_confirm_timeout_min`). Buttons Confirm and Cancel. | `R-5.18` `R-5.6` BK-08 C-12 | ▢ | |
| MQ-03.15 | Tap Confirm. | O-B Accepted with the new totals. Profile C shows it in Accepted. Reserved: I3 2, I8 2, I5 0. | `R-5.18` `R-5.20` BK-08 | | |
| MQ-03.16 ★ | In profile A sign in as C18. Place pickup order **O-C** at SL: I4 ×1 (₹245). In profile C click Reject. | A reason picker with exactly: Out of stock, Closing soon, Too busy, Can't deliver to the address, Price changed, Other. No free text unless Other. | `R-5.34` G8 B-04 | ▢ | |
| MQ-03.17 | Pick "Out of stock" and confirm. | O-C Rejected, moves to Missed. Profile A shows Rejected with the reason in the app language and a "Find elsewhere" action. | `R-5.9` BK-07 C-12 | ▢ | |
| MQ-03.18 | In profile C open the Missed tab. | O-C with reason "Out of stock". | B-04 | | |
| MQ-03.19 | Mark O-A Ready, then hand over with the code C11 shows (payment UPI). | O-A Collected. | BK-11 | | |
| MQ-03.20 | Hand over O-B with C12's code (payment cash). | O-B Collected. C12 wallet: 60 − 30 + floor(8800 × 500 / 1,000,000) = 30 + 4 = 34. | `R-7.7` CR-04 | ▢ | |

### D. Ten counter sales with the scanner (profile B, desktop, as K22)

Sign profile B out and in as K22 for this section, so the cashier limits show.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-03.21 ★ | Open Sell (B-05). | Two-pane layout: search/scan bar, quick tiles (top 24 items), cart, black total bar, payment buttons, Charge. No line discount or bill discount controls (cashier). | `R-12.9` `R-3.9` B-05 | ▢ | |
| MQ-03.22 ★ | **Sale 1.** Click the scan bar and scan I2 (`8901058000177`). Time it with a screen recording. | Maggi 70 g ×1, ₹14, appears in under 100 ms. | `R-12.9` POS-15 B-05 | ▢ | |
| MQ-03.23 | Pick Cash and click Charge. | Sale saved. Receipt options show (share link, SMS, print 80 mm). Cart clears. | POS-01 B-05 | | |
| MQ-03.24 | **Sale 2.** Scan I1, then I2 twice. | Amul Butter ×1 ₹58, Maggi ×2 ₹28 (the second scan increments the line). Total ₹86. UPI, Charge. | POS-15 B-05 | | |
| MQ-03.25 | **Sale 3.** Tap quick tiles Coca-Cola 750 ml ×2 and Nandini Toned Milk ×1. | Total ₹104. Card, Charge. | B-05 | | |
| MQ-03.26 ★ | **Sale 4.** Scan the unknown barcode `8900000000001`. | An "Add product" sheet opens. Close it. Nothing added. | POS-15 B-05 | ▢ | |
| MQ-03.27 | Add I9 QA pack ₹99.99 ×1 and I12 QA pack ₹0.50 ×1. | Subtotal ₹100.49. Round-off line −₹0.49. Total ₹100. Cashier can see and use round-off. | `R-12.9` POS-16 B-05 | ▢ | |
| MQ-03.28 | Cash, Charge. | Sale saved at ₹100. | B-05 | | |
| MQ-03.29 ★ | **Sale 5.** Press F2, type `atta`, press Enter, press + once, press F4. | Wheat atta 5 kg ×2, ₹490, charged by keyboard only. Pick Cash when asked. | B-05 | ▢ | |
| MQ-03.30 ★ | **Sale 6.** Add I7 Toor dal ×1 (₹160). Link customer → Phone only. Type C19's number. | Label "Adds to your customer list · no credits or cashback". No credits field. Cash, Charge. | `R-6.5` POS-17 B-05 | ▢ | |
| MQ-03.31 | In profile A sign in as C19 and open Orders → In store. Then Wallet. | No row for Sale 6 (phone-only sales never show). Wallet unchanged (0). | `R-12.11` POS-17 C-13 | | |
| MQ-03.32 ★ | **Sale 7.** Add I14 QA pack ₹487 ×1. Link customer → Counter code. In profile A sign in as C18, open Scan bill (C-17), read the 4-digit code under "Paying at the counter without internet?". Type C18's phone and the code. | Linked to C18. No credits offered (spending needs the bill QR). Cash, Charge. | `R-6.4` POS-11 C-17 B-05 | ▢ | |
| MQ-03.33 | In profile A open Wallet as C18. | +24 credits, "5% back at Sri Lakshmi": floor(48700 × 500 / 1,000,000) = 24. Balance 24. | `R-7.7` CR-06 C-15 | ▢ | |
| MQ-03.34 ★ | **Sale 8.** Add I7 Toor dal ×3 (₹480). Link customer → Show bill QR. | A QR with "Waiting for customer…" and a 120 s countdown. | `R-6.3` POS-07 B-05 | ▢ | |
| MQ-03.35 ★ | In profile A sign in as C10, open Scan bill and scan the QR on the desktop screen (or open its URL). | Preview "₹480 at Sri Lakshmi · use up to 30 credits", slider 0–30. | `R-6.3` C-17 | ▢ | |
| MQ-03.36 ★ | Choose 30 and tap Confirm. | Profile B updates live: "claimed by Ananya, 30 credits"; total ₹450. C-17 shows a success state. | `R-6.3` POS-07 CR-18 B-05 C-17 | ▢ | |
| MQ-03.37 | Pick UPI, Charge. Then open C10's wallet. | Wallet 100 − 30 + 22 = 92 (cashback floor(45000 × 500 / 1,000,000) = 22). History "Spent at Sri Lakshmi" −30 and "5% back at Sri Lakshmi" +22. | `R-7.7` CR-18 C-15 | ▢ | |
| MQ-03.38 | Open C10 Orders → In store tab. | One row: Sri Lakshmi, today, total ₹480, credits used 30, earned 22, a receipt link. | `R-12.11` POS-26 C-13 | ▢ | |
| MQ-03.39 | **Sale 9.** Add I1 ×2. Look for a line discount. | No discount control for the cashier. Total ₹116. UPI, Charge. | `R-3.9` AU-10 B-05 | | |
| MQ-03.40 ★ | **Sale 10.** Add I8 Curd ×1 and I3 Milk ×2 (₹83). Cash, Charge. Click Share receipt → WhatsApp link. Copy the `/r/{token}` link. | A link of the form `https://app.todayz.localhost/r/<long token>`. | `R-12.10` B-05 | | |
| MQ-03.41 ★ | Open the receipt link in a private window. | C-24: store name and address, date, items, totals ₹83. No customer name, phone or wallet. Page source has `noindex`. | `R-12.10` POS-25 C-24 | ▢ | |
| MQ-03.42 | Open Sales history in B-05. | 10 sales today, newest first, with method and link label (Sale 6 "Phone only", Sale 7 "Counter code · C18 masked", Sale 8 "Bill QR · Ananya"). No Reverse button for the cashier. | `R-7.11` B-05 | ▢ | |
| MQ-03.43 | Open B-07 for I7 Toor dal. | On hand 4 (8 − 1 − 3). Two "POS sale" movements by K22. The stale tag clears ("updated just now") because stock changed. | `R-4.4` `R-4.6` B-07 | | |

### E. Five sales offline (profile C, phone, as M21)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-03.44 | In profile C sign out K22 and sign in as M21 (manager). Open Sell while online. | Persistent sync indicator shows Synced. | B-05 | | |
| MQ-03.45 ★ | DevTools → Network → Offline. | Offline banner. Sell still works. | `R-12.9` B-05 | ▢ | |
| MQ-03.46 ★ | Ring 4 cash sales: I2 ×1 (₹14), I3 ×1 (₹24), I5 ×1 (₹40), I1 ×1 (₹58). | Each saves locally. Banner "Offline · 4 sales waiting to sync". | `R-12.9` POS-03 B-05 | ▢ | |
| MQ-03.47 ★ | Sale 5 offline: I14 ×1 (₹487) with Counter code for C18 (C18's phone also offline: set profile A offline too and read the code from C-17). | Sale saved with the typed code. Banner "Offline · 5 sales waiting to sync". | `R-6.7` E2E-07 C-17 B-05 | ▢ | |
| MQ-03.48 | Reload the business app while still offline. | App loads from the service worker. The 5 sales still show as waiting. | POS-03 PWA-M | ▢ | |
| MQ-03.49 ★ | Go online in profiles C and A. Wait up to 30 s. | Sync runs. Banner gone; "Synced" with the time. Sales history shows the 5 sales in the order rung. | POS-03 B-05 | ▢ | |
| MQ-03.50 | Open C18's wallet in profile A. | +24 credits for the offline linked sale: balance 48. | `R-6.7` E2E-07 | | |
| MQ-03.51 | Open B-07 in profile B for I2, I3, I5, I1, I14. | On hand reduced by the offline sales (I14 now 18). No duplicate movements. | POS-05 B-07 | | |

### F. Bill QR edge cases

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-03.52 | In profile B (K22) add I1 ×2 (₹116), Show bill QR. Wait 121 s without scanning. Then scan it as C10. | C-17 shows the expired QR message. A confirm by API returns `claim_expired`. Profile B offers Refresh. | `R-6.3` POS-08 `claim_expired` C-17 | ▢ | |
| MQ-03.53 | Click Refresh on the POS, scan the new QR as C10, confirm with 0 credits. | Linked; cashback floor(11600 × 500 / 1,000,000) = 5 credits. | `R-6.3` CR-18 | | |
| MQ-03.54 | Ring I2 ×5 (₹70), Show bill QR, scan as C10. | Preview offers no credits (bill under ₹100). Confirm links the sale; cashback floor(7000 × 500 / 1,000,000) = 3. | `R-7.3` POS-27 C-17 | ▢ | |

### G. Close of day

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-03.55 | In profile B sign in as O20. Open Sales history, pick Sale 8, click Reverse. | Confirm step lists: stock returned (Toor dal ×3), credits returned to the customer (30), cashback clawed back (22). | `R-7.11` POS-02 B-05 | ▢ | |
| MQ-03.56 | Confirm the reversal. Open C10's wallet. | +30 back to the lot it came from and −22 clawback: balance 92 + 5 + 3 + 30 − 22 = 108. History shows both lines. | `R-7.11` POS-18 C-15 | ▢ | |
| MQ-03.57 | Open B-07 Toor dal. | On hand 7. A "POS reversal" movement +3 by O20. | `R-4.4` POS-02 | | |
| MQ-03.58 | Open B-03 Overview. | Orders 3 today (O-A, O-B collected; O-C missed). KPI and sales by hour include the POS and online sales; the hour bars match the times rung. | B-03 | ▢ | |
| MQ-03.59 | Open B-12 Analytics for today. | Sales split online vs POS; funnel received 3 → accepted 2 → collected 2, missed reason "Out of stock" 1. | B-12 | ▢ | |
| MQ-03.60 | Open B-16 inbox. | An in-app line for each of the 3 new orders, each linking to the order in B-04. "Mark all read" clears the badge. | B-16 | ▢ | |

## Cleanup

1. Network online in all profiles. `clock reset`.
2. Sign out all three profiles.
3. `make reset` (wallets, stock and sales changed).
