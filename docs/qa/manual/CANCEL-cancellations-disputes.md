# CANCEL Cancellations, partial accepts, failed delivery, reviews and tickets

Every way an order ends early, by every actor, with the reason lists of 01 §5.6. Then the disputes after an order: reviews (report, hide, restore, rating), and support tickets on both sides.

- **Covers:** BK-07, BK-10, BK-17, BK-18, BK-19, BK-29, BK-40, BK-42; E2E-03, E2E-10; SC-RV; D-20, D-22, D-41, D-60, D-65, D-68.
- **Screens:** C-12, C-13, C-14, C-19, C-20; B-04, B-11, B-15; A-05, A-06, A-07, A-12, A-14.
- **Devices:** customer P360 (profile A), shop D1280 (profile B, K22; profile C, M21 or O20), admin D1440 (profile D).
- **Time:** about 2 hours.

## Preconditions

1. `make reset`; `clock set 2026-10-22T11:00:00+05:30`.
2. Seeds `<seed:tickets>` loaded. S42 and AD47 have TOTP enrolled.
3. Each order below is a pickup order at Sri Lakshmi with I1 ×1 and I10 ×1 (₹158) and 30 credits unless the step says otherwise. Call this "the standard order".

## Steps

### A. Customer cancels (customer-cancel reasons)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.01 ★ | C10 places the standard order. On C-12 tap Cancel. | The reason picker lists exactly the labels for `changed_mind`, `too_slow` and `other` (i18n labels, G8). `declined_partial` is not offered here (it is only for declining a partial accept). Record the label text. | `R-5.11` `R-5.34` G8 C-12 | ▢ | |
| CANCEL-M.02 | Pick `changed_mind` and confirm. | `cancelled` (customer, `changed_mind`). C-15: "Returned: order cancelled" +30, held 0. B-04: the order leaves New; K22 gets a toast. The timeline on both sides shows the actor and reason. | `R-5.11` `R-5.15` BK-17 C-12 B-04 | ▢ | |
| CANCEL-M.03 | Standard order; K22 accepts. C10 cancels with `too_slow`. | `cancelled`. Reservations released (I1 available back to 10). Credits released. | `R-5.11` `R-5.23` BK-17 | | |
| CANCEL-M.04 | Standard order; K22 accepts and marks ready. C10 cancels with `other` and a note "QA". | `cancelled` from `ready`. Same releases. | `R-5.11` BK-17 | | |
| CANCEL-M.05 | Standard order; K22 accepts; K22 enters the code (collected). C10 sends the cancel request via cURL. | `409 invalid_transition`. The C-12 Cancel button was already gone after collection. | `R-5.10` BK-17 `invalid_transition` | | |
| CANCEL-M.06 | Delivery order to C11's Home (I4 ×1); K22 accepts and dispatches to R23. On C-12 look for Cancel; then send the cancel request via cURL. | No Cancel button while out for delivery. The API → `409 invalid_transition` (customers cannot cancel in `out_for_delivery`, 01 §5.2). | `R-5.11` C-12 `invalid_transition` | ▢ | |

### B. Shop rejects and cancels

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.07 | Standard order. K22 opens it and taps Reject. | The picker lists exactly the labels for `out_of_stock`, `closing_soon`, `too_busy`, `cannot_deliver_address`, `price_changed`, `other`. | `R-5.34` G8 B-04 | ▢ | |
| CANCEL-M.08 | Pick `too_busy`. Switch C10's language to Kannada and open C-12. | `rejected`. The reason shows to C10 in Kannada. Credits released. Push and in-app "rejected" in the mocks. | `R-5.34` `R-15.3` BK-07 C-12 | ▢ | |
| CANCEL-M.09 | Standard order; K22 accepts. As K22 open the order detail. | No Cancel button for the cashier. Via cURL, `POST {S}/orders/{id}/cancel` with K22's session → `403`. | `R-5.12` `R-3.9` BK-18 D-65 B-04 | | |
| CANCEL-M.10 | As M21 open the same order. Tap Cancel. | The picker lists exactly the labels for `out_of_stock`, `customer_unreachable`, `closing_soon`, `other`. | `R-5.34` BK-18 D-68 B-04 | ▢ | |
| CANCEL-M.11 | Pick `other` and leave the note empty. Then send the request via cURL with reason `too_busy`. | `other` without a note: the form will not submit; the API → `422`. `too_busy` (not in the shop-cancel list) → `422`. | `R-5.34` BK-18 | | |
| CANCEL-M.12 ★ | Pick `customer_unreachable` and confirm. | `cancelled` (shop). C10 notified with the reason. Reservations and held credits released. A-05: the cancel is counted in Sri Lakshmi's reliability. C10 gets no strike. | `R-5.12` `R-5.26` BK-18 A-05 | ▢ | |
| CANCEL-M.13 | Standard order (I1 ×1, I10 ×1). K22 taps Accept part and removes both lines. | The app does not allow confirming with zero lines and points to Reject. Via cURL `accept` with an empty `lines` → refused (record status and code). | `R-5.9` BK-10 | | |

### C. Partial accept: customer declines and confirms

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.14 | C11 places I1 ×2, I3 ×2, I5 ×1 (₹204), 30 credits. K22 accepts part: I1 ×1, drops I5. C11 taps Cancel on the confirm screen. | `cancelled` (customer, `declined_partial`). No reason picker: the reason is set by the action. Credits released. | `R-5.9` `R-5.34` D-68 C-12 | ▢ | |
| CANCEL-M.15 | Same order again; same partial accept. C11 taps Confirm. K22 hands over with the code. | `accepted` then `collected` at ₹106 items, 30 credits, total ₹76. Cashback floor(7600 × 500 / 1,000,000) = 3. | `R-5.18` E2E-03 C-12 | ▢ | |

### D. Admin cancels with a strike side (D-20, BK-40)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.16 | C10 places the standard order; K22 accepts. S42 opens it in A-07 → Cancel. | Reason picker with the labels for `customer_request`, `store_request`, `fraud_suspected`, `technical_issue`, `other`. "Strike to" defaults to none. | `R-5.14` `R-5.34` A-07 | ▢ | |
| CANCEL-M.17 | Pick `customer_request`, strike to customer. Confirm. | `cancelled` (admin). C10 strikes +1 in A-06; Sri Lakshmi strikes unchanged in A-05. An audit row in A-19. | `R-5.14` `R-5.25` BK-40 A-06 | | |
| CANCEL-M.18 | Repeat with `store_request`, strike to store. | Sri Lakshmi strikes +1; C10 unchanged. | `R-5.14` `R-5.25` BK-40 A-05 | | |
| CANCEL-M.19 | Repeat with `technical_issue`, strike none. Then send an admin cancel for an already `collected` order via cURL, and one with reason `changed_mind`. | No strike anywhere. Collected → `409 invalid_transition`. Wrong reason → `422`. | `R-5.14` BK-39 BK-40 | | |

### E. Races and wrong-state actions

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.20 | Standard order. Put C-12 (profile A) and B-04 (profile B) side by side. Click Cancel → `changed_mind` → Confirm on C-12 and Accept on B-04 within the same second. | Exactly one wins. The loser sees a message from `409 invalid_transition` and the screen refreshes to the real state. If accepted: stock reserved once. If cancelled: no reservation. C-15 held matches the state. | `R-5.9` BK-19 | ▢ | |
| CANCEL-M.21 | Pickup order; K22 accepts. Send `fail-delivery` with reason `customer_unreachable` via cURL. Repeat on a `placed` delivery order and an `accepted` delivery order. | `409 invalid_transition` each time. `delivery_failed` only comes from `out_for_delivery`. | `R-5.22` BK-42 D-22 | | |

### F. Suspended store with open orders (BK-29)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.22 | C10 places the standard order at Annapurna Provisions; X25 accepts. AD47 suspends Annapurna Provisions in A-03 with reason "QA CANCEL-M.22". | In profile A, Annapurna is gone from Home and search; its store page → 404 state. C-12 for the open order still shows the code. | `R-3.4` BK-29 SR-01 A-03 | ▢ | |
| CANCEL-M.23 | X25 enters C10's code in B-04. | `collected`. Credits captured to Annapurna; cashback posts at Annapurna's rate (0, so none). Reinstate the business in A-03. | `R-3.4` BK-29 | | |

### G. Reviews: report, hide, restore, rating

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.24 | Pick a live store with 0 reviews (note it; if none, use the store made live in MQ-05). C18 orders there and collects. Review 5 stars. | Store rating shows "4.2 · 1 review": (4.0 × 5 + 5) / (5 + 1) = 4.17, shown to one decimal. Record the exact display. | `R-10.4` C-07 | ▢ | |
| CANCEL-M.25 | C18 edits the review text to 1,001 characters. Then to exactly 1,000. | 1,001: the field stops or the API → `422`. 1,000: saved. | `R-10.1` C-14 | | |
| CANCEL-M.26 | K22 makes a POS sale I4 ×1 linked by bill QR to C19. C19 opens C-13 → In store → the sale. Then K22 makes a Phone only sale for C19 (I2 ×1). | The bill QR sale offers Review. The Phone only sale does not appear in C-13 In store and cannot be reviewed. | `R-10.1` `R-6.5` C-13 C-14 | | |
| CANCEL-M.27 | O20 opens B-11, picks C18's review → Report, reason "Not a real customer". C10 reports one of Sri Lakshmi's reviews from C-07 → reviews tab. | Both land in the A-12 reports queue with reporter, reason and review. The reviews stay published. | `R-10.2` A-12 B-11 | ▢ | |
| CANCEL-M.28 ★ | S42 opens A-12 → C18's review → Hide, reason "QA CANCEL-M.28". In profile A reload the store page. | The review is gone from the shop page and B-11 shows it hidden. The rating goes back to the no-reviews state (count 0). Audit row in A-19. | `R-10.3` `R-10.4` E2E-10 A-12 A-19 | ▢ | |
| CANCEL-M.29 | S42 restores it with reason "QA restore". Resolve both reports. | The review shows again; rating "4.2 · 1 review". Both reports closed with S42 as resolver. | `R-10.3` A-12 | | |
| CANCEL-M.30 | O20 replies to C18's review, edits the reply, then tries a second reply via cURL. S42 hides the reply only. | One reply, edited in place. A second reply → refused. Hiding the reply leaves the review visible without the reply. | `R-10.2` `R-10.3` B-11 | | |

### H. Support tickets

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| CANCEL-M.31 ★ | C10 opens Help (C-19) → New ticket, links the order from M.17, writes "Why did I get a strike?". | Ticket created and listed under My tickets with status open. FAQs and the call/WhatsApp support link show on C-19. | C-19 | ▢ | |
| CANCEL-M.32 | S42 opens A-14. Find the ticket, assign it to self, add an internal note "QA note", reply "Checking your order". | The ticket shows the linked order and customer; SLA timer counts from creation. C10 sees the reply in C-19, but not the internal note. | A-14 C-19 | ▢ | |
| CANCEL-M.33 | S42 resets C10's strike from the linked customer (A-06) with reason "QA ticket" and closes the ticket. | Strikes −1. Ticket closed on both sides. Audit rows for the reset and the close. | `R-5.28` A-06 A-14 | | |
| CANCEL-M.34 | O20 opens B-15 Help → new ticket "Statement question". M21 opens B-15. K22 opens More. | O20 and M21 see FAQs, tickets, call support and "How TodayZ works for your shop"; the ticket shows for both. K22 has no Help tickets entry (`{B}/support/tickets` is manager and above). | B-15 | ▢ | |
| CANCEL-M.35 | Y44 (analyst) opens `/support` in the admin console. | "You don't have access to this". | AD-02 A-14 | | |

## Cleanup

1. Reinstate Annapurna Provisions if still suspended.
2. `clock reset`; `make reset`.
