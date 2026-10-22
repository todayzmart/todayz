# MQ-04 Delivery

Delivery settings, the radius, the minimum and free-above checks on the eligible amount, a rider handing over with the code, the phone-masking window, and a failed delivery with its return.

- **Covers:** 08 §5 MQ-04 (settings radius, out-of-radius customer, rider code); BK-23, BK-24, BK-26, BK-27, BK-45 manual side; E2E-05.
- **Screens:** B-14, B-04, B-16, B-07; C-07, C-09, C-11, C-12.
- **Devices:** customer P390 (profile A); shop D1280 (profile B); rider phone P360 (profile C).
- **Time:** about 50 minutes.

## Preconditions

1. `make reset`; `clock reset`; run between 10:00 and 18:00 IST.
2. C11 (wallet 500) has the saved addresses in `<seed:addresses>`: "Home" 0.6 km from SL and 3.5 km from KF; "Office" 4.0 km from SL and 4.5 km from KF; "Far" 6.0 km from KF.
3. SL delivery: on, radius 3 km, fee ₹30, free above ₹500, minimum ₹150, delivery hours = opening hours. KF: delivery only, radius 5 km, fee ₹0, minimum ₹100, cashback 200 bp.
4. `feature_delivery_enabled` = true.

## Steps

### A. Delivery settings (profile B, O20)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-04.01 ★ | Open B-14 Settings → Delivery for Sri Lakshmi. | Delivery on; radius slider at 3 km with a map preview ring; fee ₹30; free above ₹500; minimum ₹150; delivery hours "Same as opening hours". | `R-5.29` B-14 | ▢ | |
| MQ-04.02 | Drag the radius to 1 km without saving. | The map preview ring shrinks live. Cancel the change. | B-14 | | |
| MQ-04.03 | Turn delivery off and save. In profile A open Home at the QA point. | SL's StoreCard no longer has the "Delivery" tag. C-07 for SL shows pickup only. | `R-5.32` C-03 C-07 | ▢ | |
| MQ-04.04 | Turn pickup off too and save. | Warning "Your shop will be In-store only: customers can browse but not order". Cancel, so pickup stays on. Turn delivery back on and save. | `R-3.5` D-41 B-14 | ▢ | |
| MQ-04.05 | Refresh Home in profile A. | The "Delivery" tag is back on SL. | `R-5.32` C-03 | | |

### B. Out of radius and the minimum (profile A, C11)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-04.06 ★ | Sign in as C11. Cart at SL: Maggi ×5 (₹70) and Coca-Cola 750 ml ×2 (₹80) = ₹150. Pick Delivery, address "Office". | Delivery cannot be chosen for this address: a message that the address is outside the shop's delivery area. Pickup still offered. | `R-5.32` BK-23 C-09 | ▢ | |
| MQ-04.07 | Call `POST /v1/me/checkout/quote` with `fulfilment: delivery` and the Office address id (Copy as cURL from a pickup quote, edit the body). | The quote refuses delivery for this address. Record the `code`. | BK-23 | | |
| MQ-04.08 ★ | Switch to address "Home". | Delivery allowed. Fee ₹30. Items ₹150, total ₹180. | `R-5.29` `R-5.30` C-09 | ▢ | |
| MQ-04.09 | Reduce Coca-Cola to 1 (items ₹110). | Message that the shop's minimum for delivery is ₹150. Delivery Place disabled; quote returns `below_minimum`. | `R-5.30` BK-23 `below_minimum` C-09 | ▢ | |
| MQ-04.10 | Replace the cart with I13 QA pack ₹120 ×1 (offer ₹21 off, so ₹99) and Nandini Toned Milk ×2 (₹48). | List price total ₹168, but the eligible amount is ₹147 (after the offer). Delivery refused with `below_minimum`. | `R-5.30` `R-7.2` BK-23 `below_minimum` | ▢ | |
| MQ-04.11 | Replace the cart with I14 QA pack ₹487 ×1 and I12 QA pack ₹0.50 ×24 (₹12) = ₹499, Delivery to Home. | Fee ₹30 (₹499 is not above ₹500). Total ₹529. | `R-5.30` BK-23 | ▢ | |
| MQ-04.12 | Change the cart to I14 ×1 and Maggi ×1 = ₹501. | Fee ₹0 (free above ₹500). Total ₹501. | `R-5.30` BK-45 | ▢ | |
| MQ-04.13 | Change to exactly ₹500 (I14 ×1 and I12 ×26). | Record whether the fee is ₹0 or ₹30. The plan says "free above ₹500"; whether ₹500 itself is free is Q-27. | `R-5.29` | | |

### C. Delivered by the rider

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-04.14 ★ | Cart: Maggi ×5 and Coca-Cola ×2 (₹150), Delivery to Home, 30 credits. | Items ₹150, credits −₹30, delivery fee ₹30, total ₹150. The fee is not reduced by credits. | `R-5.30` `R-7.3` CR-22 C-11 | ▢ | |
| MQ-04.15 | Try moving the credits slider above 30. | It stops at 30. | `R-7.3` | | |
| MQ-04.16 ★ | Place the order (**D-1**). | Placed; countdown 10:00. | BK-01 C-12 | | |
| MQ-04.17 ★ | In profile B sign in as K22, open D-1. | Customer name and masked phone. Delivery address shown. Accept. | `R-16.2` BK-27 B-04 | ▢ | |
| MQ-04.18 | Open D-1 in Accepted. Click Dispatch. | A rider picker lists staff who can deliver at SL: R23 (delivery flag) and cashiers. No Mark ready for delivery orders. | `R-5.31` B-04 | ▢ | |
| MQ-04.19 ★ | Pick R23 and confirm. | D-1 moves to Out for delivery. The customer's full phone number now shows on the order detail and in the order list. | `R-16.2` BK-27 B-04 | ▢ | |
| MQ-04.20 | In profile A read C-12. | State Out for delivery with the code "to tell the rider" (4 digits and QR). A push "out for delivery" in the mocks. Cancel is not offered (Q-11). | `R-6.2` `R-15.6` C-12 | ▢ | |
| MQ-04.21 | In profile C sign in as R23 (rider). Open Orders → Out for delivery → D-1. | R23 sees D-1 with Delivered and Delivery failed. Full phone shown. | `R-5.31` B-04 | ▢ | |
| MQ-04.22 | Enter a wrong code. | Attempts left 4. | `R-6.2` BK-12 | | |
| MQ-04.23 ★ | Enter the right code, payment Cash. | D-1 Delivered. Customer C-12 shows Delivered with review and reorder actions. A push "delivered". | `R-6.2` BK-24 C-12 | ▢ | |
| MQ-04.24 ★ | In profile B reopen D-1. | The phone is masked again. | `R-16.2` BK-27 | ▢ | |
| MQ-04.25 | C11 Wallet. | 500 − 30 + 6 = 476. Cashback floor((15000 − 3000) × 500 / 1,000,000) = 6; the ₹30 fee is not in the base. | `R-7.7` CR-23 C-15 | ▢ | |
| MQ-04.26 | B-07 for Maggi and Coca-Cola. | Online sale movements −5 and −2; reserved 0. | BK-24 B-07 | | |

### D. Delivery failed and the return

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-04.27 ★ | As C11 place **D-2** at SL: Wheat atta ×1 (₹245), Delivery to Home, no credits. Accept as K22, dispatch with R23. | D-2 Out for delivery. Atta reserved 1. | BK-24 | | |
| MQ-04.28 ★ | In profile C (R23) click Delivery failed. | Reason picker with exactly: Customer unreachable, Address not found, Customer refused, Other. | `R-5.34` G8 B-04 | ▢ | |
| MQ-04.29 ★ | Pick "Customer unreachable". | D-2 `delivery_failed`. Then "Did the goods come back?" with Yes / No. | `R-5.22` BK-26 B-04 | ▢ | |
| MQ-04.30 | As R23 (cashier role) tap No. | "Ask a manager to record damage". D-2 stays in Returns to confirm with the time since it failed. Atta still reserved 1. | `R-5.22` D-69 B-04 | ▢ | |
| MQ-04.31 | Profile A C-12 for D-2. | Outcome "Delivery failed" with the reason; no cashback; wallet unchanged (476). | `R-5.33` BK-26 C-12 | ▢ | |
| MQ-04.32 | `clock +1h59m`. Open B-16 in profile B. | No reminder yet for D-2. | `R-5.22` BK-26 | | |
| MQ-04.33 | `clock +1m`. Refresh B-16. | One reminder linking to D-2 in Returns to confirm (2 h). | `R-5.22` BK-26 B-16 | ▢ | |
| MQ-04.34 | Sign in profile B as M21. Open Returns to confirm → D-2 → "Did the goods come back?" → Yes. | D-2 leaves the tab with outcome Returned. Atta reserved 0, on hand unchanged. | `R-5.22` BK-26 B-07 | ▢ | |
| MQ-04.35 | `clock reset`. | | | | |

### E. Delivery-only shop (Kaveri Fresh)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-04.36 | Set radius chip to 5 km and open Kaveri Fresh (3.5 km). | StoreCard and header: "Delivery" tag, "2% back". C-07 shows delivery terms fee ₹0, minimum ₹100. | `R-5.32` `R-7.7` C-07 | ▢ | |
| MQ-04.37 | Add items worth ₹99 and open the cart, address Home. | Pickup not offered. Delivery refused with `below_minimum` (₹100). | `R-5.30` `below_minimum` C-09 | | |
| MQ-04.38 | Add items to reach exactly ₹100. | Allowed, fee ₹0, total ₹100. | `R-5.30` | | |
| MQ-04.39 | Change the address to "Far" (6.0 km). | Delivery refused (outside the 5 km radius); no other mode available, so Place is disabled with a clear message. | `R-5.32` BK-23 | ▢ | |

### F. Delivery feature flag

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-04.40 | `set feature_delivery_enabled = false` (admin AD47 can change it: not a money key). | Saved with reason "QA MQ-04.40". | `R-17.2` A-17 | | |
| MQ-04.41 | Refresh Home and SL's page in profile A. | No "Delivery" tags anywhere. SL's cart offers pickup only. | `R-5.1` `R-17.2` C-03 C-09 | ▢ | |
| MQ-04.42 | Open Kaveri Fresh. | Record what shows. The plan does not say whether a delivery-only store becomes "In-store only" when the flag is off (Q-28). Ordering must not be possible. | `R-5.1` | ▢ | |
| MQ-04.43 | `set feature_delivery_enabled = true`. | Delivery tags back. | `R-17.2` | | |

## Cleanup

1. `clock reset`; `restore defaults` (`feature_delivery_enabled` = true).
2. SL settings back to the precondition values.
3. `make reset`.
