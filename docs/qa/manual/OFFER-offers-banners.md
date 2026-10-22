# OFFER Offers, banners and offer search

Each offer kind, the pricing rule (one best item offer per line plus one bill offer, never below ₹1, same price online and at the POS), limits and schedules, where offers show to customers, and banners from creation to takedown. v1 has no coupon codes: 01 §12 lists no code-entry offer, so the per-customer limit is the nearest rule (Q-41).

- **Covers:** SC-OF; SR-02, SR-03, SR-04; E2E-14 manual side; D-42, D-77.
- **Screens:** B-08, B-05, B-12; C-03, C-05, C-07, C-08, C-09, C-11; A-20.
- **Devices:** shop D1280 (profile B, M21), customer P360 (profile A), admin D1440 (profile D).
- **Time:** about 2 hours.

## Preconditions

1. `make reset`; `clock set 2026-10-22T11:00:00+05:30`.
2. Seeds `<seed:offer-I13-21off>` and `<seed:banner-SL>` loaded (one live offer, one live banner at Sri Lakshmi).
3. `feature_offers_enabled` and `feature_banners_enabled` are true.
4. Keep a price table in the run notes: item, list price, offer, expected line price.

## Steps

### A. Offer kinds and the customer preview

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| OFFER-M.01 ★ | M21 opens B-08 → Offers → Create. Kind "% off", items: I1 Amul Butter, 10%, channel online and POS, start now, no end. Watch the live preview. | The phone-frame preview shows the product card with ₹58 struck, ₹52.20 and a "10% off" tag, and the shop page Offers row. Save → status live. | `R-12.2` `R-12.3` B-08 | ▢ | |
| OFFER-M.02 | In profile A (C10) open Sri Lakshmi (C-07) and Amul Butter (C-08). Add 1 to the cart. | Product card: ₹52.20 with ₹58 struck and "10% off". StoreCard on Home has an offer tag. Cart line ₹52.20. | `R-12.5` C-03 C-07 C-08 C-09 | ▢ | |
| OFFER-M.03 | K22 at the POS scans I1 (`8901262010016`). | The line shows ₹52.20 with the offer named: the same price as the online quote. | `R-12.4` B-05 | ▢ | |
| OFFER-M.04 | Create "₹ off" on the category of I3 (milk), ₹2 off. C10 adds I3 ×2. | Line ₹44.00 (₹22 each). | `R-12.2` | | |
| OFFER-M.05 | Create "Buy X get Y": I2 Maggi, buy 2 get 1. C10 adds I2 ×3, then ×2, then ×6. | ×3 → ₹28.00; ×2 → ₹28.00 with a hint to add one more if the app shows it; ×6 → ₹56.00. Record the display. | `R-12.2` | ▢ | |
| OFFER-M.06 | Create a minimum-bill discount "₹50 off above ₹500". C10 builds three carts: I9 ×5 (₹499.95), I10 ×5 (₹500.00) and I11 ×5 (₹500.05). | ₹499.95 → no discount. ₹500.05 → ₹50 off, total ₹450.05. ₹500.00 → record (whether "above" includes ₹500 is not stated, Q-27). | `R-12.2` `R-12.4` | ▢ | |
| OFFER-M.07 | Open the I8 Curd item (batch of 10 expiring in 3 days). From the item or Overview "Going to waste", create a flash offer 30% off on that batch. | The flash offer shows a countdown to the batch expiry on C-07 and C-08. Price ₹24.50. | `R-12.2` `R-12.8` E2E-14 B-03 | ▢ | |
| OFFER-M.08 | Create a whole-store 5% off, channel online only. Open C-07. | Every item without a better item-level offer shows 5% off; I1 keeps 10% (the better one). | `R-12.2` `R-12.4` | | |

### B. Pricing rule and credits

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| OFFER-M.09 | With I1 at 10% (₹5.80) and the store-wide 5% (₹2.90), add a second item offer "₹10 off" on I1. C10's cart: I1 ×1. | Only one item offer per line: the best one, ₹10 off → ₹48.00. The quote names the offer used. | `R-12.4` | ▢ | |
| OFFER-M.10 | Create a second bill offer "₹30 off above ₹300". Cart: I14 ×1 and I1 ×1 (list ₹545). | Item offers first: I1 ₹48.00 (₹10 off), I14 ₹462.65 (store-wide 5%), so ₹510.65. One bill offer only, the best: ₹50 off. Total ₹460.65. If the bill threshold is measured on the list price instead, record it. | `R-12.4` | | |
| OFFER-M.11 | Create "₹ off" ₹100 on I10 (₹100.00). C10's cart: I10 ×1, pickup. | Line ₹0.00 but the order total is ₹1.00 (never below ₹1). Same at the POS. Record the line display. | `R-12.4` BK-33 POS-16 | ▢ | |
| OFFER-M.12 | Cart: I13 ×1 (₹120 with the seeded ₹21 off = ₹99). Turn credits on. | Credits disabled: the eligible amount is ₹99, under ₹100. Quote with credits → `below_credit_minimum`. | `R-7.2` `R-7.3` CR-21 D-02 C-09 | ▢ | |
| OFFER-M.13 | Cart: I13 ×1 (₹99) and I3 ×1 (₹22 after the ₹2 category offer): eligible ₹121. Use 30 credits. K22 hands over. | Total ₹91. Cashback floor(9100 × 500 / 1,000,000) = 4. The offer cost is not a ledger entry: B-10 shows +30 spent and −4 cashback only. | `R-7.7` `R-12.4` B-10 | | |

### C. Limits, schedules, pause and end

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| OFFER-M.14 | Create 20% off I5 with limit "per customer once", channel online. C10 orders I5 ×1 and collects. C10 builds a second cart with I5. | First order ₹32. Second cart: the 20% offer is gone for C10; the store-wide 5% applies (₹38). C11 still sees ₹32. | `R-12.3` | ▢ | |
| OFFER-M.15 | Create 15% off I4 with "first 2 orders". C10, C11 and C12 each order I4 ×1 in that order. | C10 and C11 get ₹208.25; C12 gets only the store-wide 5% (₹232.75). Record whether the count is taken at place or collect. | `R-12.3` | | |
| OFFER-M.16 | Create 10% off I7 with channel "online only". K22 sells I7 at the POS. | POS price ₹160, no offer. Online ₹144. | `R-12.3` | | |
| OFFER-M.17 | Create 25% off I2, start 2026-10-22 14:00 IST, end 16:00 IST. Check C-07 at 13:59:59, 14:00:00, 15:59:59 and 16:00:00 (`clock set`). | Not shown at 13:59:59; shown at 14:00:00 and 15:59:59; gone at 16:00:00. B-08 status scheduled → live → ended. | `R-12.3` B-08 | ▢ | |
| OFFER-M.18 | M21 pauses the I1 10% offer and the I1 ₹10 off offer, then ends the store-wide 5% offer. | Both gone from C-07 and from the POS within one refresh. A cart that already had I1 re-quotes to ₹58 and C-11 asks for a second tap to accept the new total. | `R-12.3` BK-03 `price_changed` C-11 | ▢ | |
| OFFER-M.19 | M21 opens B-08 → the I5 offer → stats. Then B-12 Analytics. | Redemptions and sales uplift for the offer match the orders placed in M.14. | `R-12.3` B-08 B-12 | | |

### D. Where offers show, and offer search

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| OFFER-M.20 | Create a 10% off offer at Annapurna Provisions (X25) and Kaveri Fresh (O26). C10 opens Home at radius 5 km. | "Offers near you" shows at most one item per shop and includes the Sri Lakshmi banner. Reload 5 times: the order rotates across shops. | `R-12.5` `R-12.6` C-03 | ▢ | |
| OFFER-M.21 ★ | C10 searches "butter" on C-05, then taps the Offers chip. | With the chip: only items with a live offer (the URL holds `hasOffer=true`). Turn it off: the full set returns. | `R-11.6` SR-03 D-42 C-05 | ▢ | |
| OFFER-M.22 | Search "atta" with sort Best match, then Nearest first, then Best match again. | Nearest first orders shop groups strictly by distance (SL 0.6, NH 0.9, AP 1.2, SL2 1.8 for those that stock it). Back to Best match restores the first order. | `R-11.5` SR-04 C-05 | ▢ | |
| OFFER-M.23 | `set search_w_offer = 5000` (SA48: unit `bp` makes it a money setting, Q-45). Search "atta" again with Best match. Restore to 500. | A store with an atta offer moves up without a deploy. Record both orders. Equal scores put the nearer store first. | `R-11.5` SR-02 PL-06 | | |

### E. Banners

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| OFFER-M.24 | M21 opens B-08 → Banners. Create from template: tint mint, headline of 41 characters. Then 40 characters, sub-line, product photo I1, target the I1 offer, start now. | 41 characters refused. 40 saved. State "Checking image…" until the check passes (AI moderation off by default → it waits in the A-20 queue). | `R-12.6` B-08 A-20 | ▢ | |
| OFFER-M.25 | AD47 opens A-20 and approves it. C10 opens C-07 and Home. | Live on the shop page carousel above the header and in "Offers near you". Two live banners now. | `R-12.6` A-20 C-07 | ▢ | |
| OFFER-M.26 | Create and get approved a third banner. Then create a fourth with a start time now. | Third live. Fourth refused: up to 3 live banners per store (`store_banners_max`). A scheduled one that would overlap is refused too. | `R-12.6` B-08 | ▢ | |
| OFFER-M.27 | Upload a banner image from `e2e/fixtures` marked unsafe (if no such fixture, mark **B**). | Held for review; not shown to customers. | `R-12.6` SC-OF | | |
| OFFER-M.28 ★ | AD47 takes down the seeded banner in A-20 with reason "QA takedown". Within 1 minute reload C-07 and Home in profile A. | Gone everywhere within 1 minute. B-08 shows "Removed by TodayZ: QA takedown". Audit row in A-19. | `R-12.6` A-20 B-08 | ▢ | |
| OFFER-M.29 | C10 taps banner 1 at **TK**. `clock set` TK + 23 h 59 min, C10 orders at Sri Lakshmi. Then C11 taps banner 1, then banner 2, and orders 10 minutes later. Then C12 taps banner 1, `clock +24h1m`, orders. | C10's order attributed to banner 1. C11's to banner 2 (last click wins). C12's not attributed. B-08 banner stats: impressions, taps and attributed orders match. | `R-12.6` D-77 B-08 | | |
| OFFER-M.30 | `set feature_banners_enabled = false`, then `set feature_offers_enabled = false` (AD47). Open C-07, Home and B-08. | Banners gone from the app and the Banners tab hidden; then offers gone (no tags, no Offers chip, POS at list price). Restore both to true. | `R-17.2` C-03 C-05 B-08 | ▢ | |
| OFFER-M.31 | K22 (cashier) opens `/offers` in the business app and calls `GET {S}/offers` via cURL. | No Offers in K22's nav; the URL shows the no-access state. API → `403`. | `R-3.9` B-08 AU-10 | | |

## Cleanup

1. End every offer and banner created here.
2. `restore defaults`: `search_w_offer` 500, `feature_offers_enabled` true, `feature_banners_enabled` true.
3. `clock reset`; `make reset`.
