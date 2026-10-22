# NOTIFY Notifications

Every event in 01 §15 on the channels it names, read back in the mocks UI (https://mocks.todayz.localhost, tabs SMS, WhatsApp and Push) and in the in-app inboxes. Also: the SMS escalation for an unseen new order, channel flags, push denied, dead push subscriptions, opt-outs, templates per language and the admin test send.

- **Covers:** SC-NT; BK-47…60 (notification rows); R-15.1 to R-15.9; D-19, D-46.
- **Screens:** C-18, C-20, B-14, B-16, A-16, A-17.
- **Devices:** customer P360 (profile A), shop D1280 (profile B, K22; profile C, O20), admin D1440 (profile D, AD47 or SA48).
- **Time:** about 90 minutes.

## Preconditions

1. `make reset`; `clock set 2026-10-22T11:00:00+05:30`. Mocks UI open in its own tab; `DELETE https://mocks.todayz.localhost/_mock/reset` before each section.
2. OTP and notification SMS to reserved numbers still go to the mock and show in the mocks UI; only verification is short-circuited (README §2). For SMS rows this file uses C10 for push and inbox, and a mock-route customer `+91 90000 00110` ("C110", signed up in step NOTIFY-M.01) for SMS.
3. In profile A allow notifications when the browser asks (except in section D). In profile B keep the business app open on Overview.
4. Push records in the mock show `title` and `body` from the `X-TZ-Mock-Plaintext` header (local only).
5. Crop phones and codes from every mocks screenshot (README §9).

## Steps

### A. Each event on its channels

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| NOTIFY-M.01 | Sign up `+91 90000 00110` on the customer app. Read the OTP from the SMS tab. | One SMS to 919000000110 with a 6-digit code. Nothing about the OTP in the push or WhatsApp tabs. | `R-15.1` `R-2.2` AU-15 | ▢ | |
| NOTIFY-M.02 ★ | C10 places a pickup order at Sri Lakshmi (I2 ×2). Watch profile B and the Push tab. | Business: new-order toast and sound on any screen within 3 s, an inbox row in B-16, and one push to each of K22's and O20's subscriptions. No SMS yet. Customer gets nothing for "placed". | `R-15.2` BK-01 B-16 | ▢ | |
| NOTIFY-M.03 ★ | K22 accepts. | Customer: push "Order accepted" (record the exact title and body) and a C-20 inbox row that opens C-12 for that order. | `R-15.3` C-20 | ▢ | |
| NOTIFY-M.04 | Place three more orders (C10): K22 partial-accepts one, rejects one with `out_of_stock`, and lets one time out (`clock +10m`). | One push + one inbox row each: partial ("Shop changed your order", or the built copy), rejected (with the reason in C10's language), expired. | `R-15.3` BK-06 BK-07 BK-08 | | |
| NOTIFY-M.05 | Accept a new C10 order and mark it ready. | Push + inbox "Ready for pickup". | `R-15.4` | | |
| NOTIFY-M.06 | `clock +20m` after accept (hold 30 min). | One push 10 minutes before the hold ends. No inbox row (01 §15 lists push only). | `R-15.5` | ▢ | |
| NOTIFY-M.07 | C11 places a delivery order at Sri Lakshmi from "Home" (₹520). K22 accepts, R23 dispatches, then enters the code. | Two customer pushes + inbox rows: out for delivery, delivered. | `R-15.6` BK-24 | | |
| NOTIFY-M.08 | Collect C14's first order (C14 was referred by C10; ₹100 or more). | C10: inbox row for the referral unlock and the credits earned, plus a push if push is on. Sri Lakshmi gets an inbox row in B-16 if a store reward applies. | `R-15.7` RF-04 | | |
| NOTIFY-M.09 | Set C12's lot L1 so it expires tomorrow (seed). Run the expiry reminder (`clock` to the reminder time the build uses; record it). | C12 gets an in-app row "credits expiring" naming 40 credits and the date. | `R-15.7` | | |
| NOTIFY-M.10 | FN45 marks the Lakshmi Traders payout paid with UTR `QAUTR0000000002` (MQ-06 state or a seeded open payout). | O20: push and B-16 row "Payout paid" with the amount and UTR. K22 (cashier) gets nothing. | `R-15.8` B-16 | ▢ | |
| NOTIFY-M.11 | C10 posts a 4-star review on a collected order. | Sri Lakshmi: B-16 inbox row only (no push). | `R-15.9` B-11 | | |
| NOTIFY-M.12 | Open https://mocks.todayz.localhost → WhatsApp tab after the whole section. | Empty: `notify_whatsapp_enabled` is false by default. | `R-15.1` | | |

### B. SMS escalation for an unseen new order

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| NOTIFY-M.13 ★ | O20's staff phone for this test is the mock-route number `+91 90000 00120` (add it as a second owner contact in B-14 if the build has no other way; otherwise mark **B** and note it). Close every business app tab. C10 places an order. `clock +2m59s`. | No SMS yet. | `R-15.2` | | |
| NOTIFY-M.14 | `clock +2s` (3 min 1 s after placing). | One SMS to the shop number naming the order number. Exactly one, not one per staff member unless the build says so (record). | `R-15.2` | ▢ | |
| NOTIFY-M.15 | Place another order. Open profile B so the new-order toast shows (seen). `clock +3m1s`. | No SMS for that order. | `R-15.2` | | |
| NOTIFY-M.16 | `set notify_sms_enabled = false` (AD47). Repeat M.13 and M.14. Restore true. | No SMS. Push and inbox still sent. The 3 minutes is the setting `order_unseen_sms_after_min`. | `R-15.1` PL-06 | | |

### C. Flags and keys

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| NOTIFY-M.17 | AD47 opens A-16 → Integration status. | One row per channel: flag on/off and key `set` / `missing` and last error. No value or part of a value. | `R-15.1` A-16 D-19 | ▢ | |
| NOTIFY-M.18 | `set notify_push_enabled = false`. C10 places an order; K22 accepts. | No push records in the mock. Inbox rows still appear in C-20 and B-16. Restore true. | `R-15.1` | | |
| NOTIFY-M.19 | `set notify_whatsapp_enabled = true` while the WhatsApp key is `missing`. Trigger "accepted". | Nothing in the WhatsApp tab: a channel needs both its flag and its key. A-16 shows flag on, key missing. Restore false. | `R-15.1` PL-05 | | |

### D. Push permission and dead subscriptions

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| NOTIFY-M.20 | In a fresh profile F sign in as C11. Open C-18 → Notifications. Read the explanation. Tap Turn on, then Block in the browser prompt. | The app explains why before the browser prompt. After Block: C-18 shows push off with how to turn it on in browser settings. No push subscription row created. | C-18 SC-NT | ▢ | |
| NOTIFY-M.21 | C11 places an order; K22 accepts. | No push in the mock for C11. The C-20 inbox row appears (inbox only). | `R-15.3` SC-NT C-20 | | |
| NOTIFY-M.22 | Via cURL, register a push subscription for C10 with endpoint `https://mocks.todayz.localhost/push/gone-404-qa1` and another with `.../push/gone-qa2` (`POST /v1/me/push-subscriptions`). Trigger "accepted" for C10 twice. | First send: the mock returns 404 for one and 410 for the other. Second send: neither endpoint is called again (both pruned). C10's real subscription still gets both. | SC-NT | | |
| NOTIFY-M.23 | In the Push tab, read the `TTL` and `Urgency` on the "accepted" and on the "hold ending" pushes. Check that no push body is over 4096 bytes (no 413 rows). | Each record has a TTL. No 413 in the tab. | SC-NT | | |

### E. Opt-outs and inboxes

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| NOTIFY-M.24 | O20 opens B-14 → Notification prefs for Sri Lakshmi. Turn off push for new orders. C10 places an order. | No push to O20. The B-16 row and the in-app sound on open screens still happen. Record which events the prefs list. | B-14 `R-15.2` | ▢ | |
| NOTIFY-M.25 | C10 opens C-18 → Notifications and turns off push for credits. Earn cashback on a collected order. | No push for the credit; the C-20 row still appears. Order pushes still arrive. | C-18 `R-15.7` | | |
| NOTIFY-M.26 | C10 opens C-20 with 5 unread rows. Tap one. Then "Mark all read". | The row opens its deep link (order, wallet). Unread count drops by 1, then to 0. | C-20 | ▢ | |
| NOTIFY-M.27 | O20 opens B-16, taps the payout row, then the weekly summary row if present, then Mark all read. | Each deep link opens the right statement or card. Count 0. | B-16 D-46 | | |

### F. Language and templates

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| NOTIFY-M.28 | Set C10's language to ಕನ್ನಡ (C-18). K22 accepts a C10 order. | The push title and body in the mock are in Kannada. The C-20 row too. | SC-NT `R-1.2` | ▢ | |
| NOTIFY-M.29 | Set K22's business app language to हिन्दी. Place a C10 order with the business app closed. Read the SMS after 3 min (M.13 set-up). | The SMS text is in Hindi, or record that business SMS follows the store language instead. | SC-NT | | |
| NOTIFY-M.30 | AD47 opens A-16 → template "order accepted" → Tamil. Preview. Edit the body (add "QA"), save. Trigger it for a Tamil-language customer. | Preview shows the variables filled. The next push uses the edited text. An audit row in A-19 with before/after. Revert. | A-16 A-19 | ▢ | |
| NOTIFY-M.31 | AD47 taps Test send on the SMS template. AD47's phone is reserved, so set A-16's test number as the build allows, or mark **B**. | One SMS to the admin's own number only, never to a customer. | A-16 | | |
| NOTIFY-M.32 | As S42 (support) open `/notifications` by URL and call `GET /v1/admin/notification-templates`. | No-access state; API `403`. | A-16 AD-01 | | |

## Cleanup

1. `restore defaults`: `notify_push_enabled` true, `notify_sms_enabled` true, `notify_whatsapp_enabled` false.
2. Revert template edits. Reset language choices to English.
3. `DELETE https://mocks.todayz.localhost/_mock/reset`; `clock reset`; `make reset`.
