# 08 · Testing

## 1. Test pyramid and where tests live

| Level | Tool | Where | Runs in |
|---|---|---|---|
| Unit (Go) | `go test`, testify, go-cmp, `rapid` (property) | next to code | `make check` |
| Integration (Go + real Postgres/PostGIS) | testcontainers-go, `testkit.NewDB(t)` (a template DB per package, so each test gets a fresh copy in about 50 ms) | `*_integration_test.go` | `make check` |
| Contract | Schemathesis against the running API; MSW mocks generated from the spec; generated-code freshness check | `e2e/contract` | `make check` (short), `make e2e` (full) |
| Component (React) | Vitest + Testing Library + MSW, axe-core | next to components | `make check` |
| Visual | Playwright screenshots of Storybook stories and key screens (en + `en-XA`) | `e2e/visual` | `make e2e` |
| End to end | Playwright (multi-context: customer + shop + admin in one test), against the full local stack with the mocks and the test clock | `e2e/journeys`, `e2e/scenarios` | `make e2e` |
| Load | k6 | `e2e/load` | `make load` |
| Security | gosec, govulncheck, npm audit, gitleaks, ZAP baseline/full, custom abuse tests | `e2e/security` | `make check` (static), `make security` (dynamic) |
| Accessibility | axe in Playwright on every route; manual screen-reader scripts | `e2e/a11y` | `make e2e` |
| AI evals | `todayz ai eval` on recorded fixtures (live model optional) | `backend/internal/ai/*/eval` | `make check` (recorded), `make ai-eval` (live) |
| Manual | Scripts below, run by the QA agent in the browser pane, plus the human founders on real phones before launch | `docs/qa/manual` | per WP + wave 4 |

Coverage targets (enforced): Go domain packages ≥ 85% lines, `credits`, `orders`, `pos`, `authz` ≥ 95% branches; frontend `shared/ui` ≥ 90%, apps ≥ 70%. Coverage is a floor, not the goal: the scenario list is the goal.

## 2. Test infrastructure

- **Test clock (D-80):** in `APP_ENV=local` and `APP_ENV=test`, `POST /v1/test/clock {advance: "31m"}` moves the server clock and waits (up to `wait`, default 10 s, at most 50 s) for the River jobs that became due to run; QA agents use it for manual testing on the local stack. In staging and prod builds the route is not registered: a test asserts it returns 404 in a `staging` and a `prod` build.
- **Test OTP (D-15):** there is no `dev` environment; the environments are `local`, `test`, `staging`, `prod`.
  - In `local` and `test` only, the reserved numbers `+91 90000 000xx` accept `000000`. A test asserts that a `staging` or `prod` build treats them like any other number (`000000` refused).
  - Every other number goes through the mock SMS server. Sends to reserved numbers (OTP and notification SMS) also go to the mock and show in the mocks UI; the only shortcut is that verification accepts `000000`. Tests and QA agents read the OTP from the mocks UI/API at `mocks.todayz.localhost`, never from a log.
  - An OTP is never logged in any environment, including `local` and `test` (AU-15).
- **No real keys (D-17):** the default suite (`make check`, `make e2e`) runs with every provider key unset. SMS, push, Sentry, Anthropic and Sarvam go to the mocks or local tools (PL-04). Live AI evals (`make ai-eval`) and manual AI QA use real keys only if the user puts their own in `.env.local`.
- **Fixtures:** `todayz seed test` builds a small deterministic world: 2 cities; stores covering every business status (verified, needs info, suspended) and every store status (`draft`, `pending_review`, `live`, `paused`, `suspended`), pickup only, delivery only, both, and in-store only (both off), auto-accept on, cashback 0/5%; 200 products; customers (new, regular, limited by strikes, high wallet, a wallet with a lot expiring tomorrow, referred-pending, blocked, one with an open order); staff in every role; admins in every role of D-64 (field_agent, support, catalogue, analyst, admin, and two each of finance and super_admin for the two-person approval tests); all enrolled, plus one unenrolled admin so A-01 first-login enrolment can be tested. Their deterministic TOTP secrets live in `e2e/fixtures/admins.yaml`, never in docs or logs. The backend does not read `e2e/`: `infra/local/local.env` sets `SEED_ADMINS_FILE=e2e/fixtures/admins.yaml`, the seed module reads that path, with the variable unset it seeds no TOTP admins and logs the variable's name, and it refuses to run unless `APP_ENV` is `local` or `test`.
- **Slow route and slow query:** `/v1/test/slow?ms=` and `/v1/test/slow-sql?ms=` (04 §2.9, same gate as the test clock) force a slow request or a slow query for the latency alerts and the slow-network checks.
- **Factories:** Go `testkit/factory` and TS `e2e/factory` create any entity through the API (not SQL) so tests exercise real rules.
- **Isolation:** each E2E worker gets its own city slice of the seed (prefix-based) so tests run in parallel without collisions.
- **Flake policy:** a test that fails then passes on retry is marked flaky automatically and quarantined; the WP owner fixes it within the wave. Zero quarantined tests at wave exit.

## 3. Scenario catalogue

Format: `ID · Given / When / Then · level (U unit, I integration, E E2E, M manual)`. A WP must make all its listed IDs pass. Ranges in [07-work-packages.md](07-work-packages.md) that go past the scenarios written here are filled in by the builder in `e2e/scenarios/<AREA>.md` (same format) and approved by the reviewer. The written ones are mandatory minimums.

### 3.1 Auth (SC-AU)

| ID | Scenario | Lv |
|---|---|---|
| AU-01 | New phone, send OTP, enter the right code → signed in, `isNewUser=true`, a session exists. Refresh cookie per app (D-14): customer `__Secure-tz_rt_c`, business `__Secure-tz_rt_b`, admin `__Secure-tz_rt_a`; each host-only on the API host (no `Domain` attribute; `api.todayz.localhost` locally), `Path=/v1/auth`, `HttpOnly`, `Secure`, `SameSite=Strict`. One app's cookie never refreshes another app's session; the cookie is not sent to any non-`/v1/auth` path | I,E |
| AU-02 | Wrong OTP 5 times → 6th attempt refused `otp_locked` even with the right code; a new send is needed | I |
| AU-03 | OTP after 5 min → `otp_expired` | I |
| AU-04 | Resend before 30 s → `429` with `Retry-After` | I |
| AU-05 | 6th send to one phone in an hour → refused; 21st send from one IP in an hour → refused | I |
| AU-06 | Send OTP to unknown and to known numbers → identical responses and timing within 50 ms | I |
| AU-07 | Refresh rotates the token; old refresh reused → whole session family revoked; both devices signed out | I |
| AU-08 | Access token expired → client refreshes silently once and retries; refresh fails → sign-in screen with the cart preserved | E |
| AU-09 | Customer token on a business endpoint → 403; business token for business A on business B's store → 403 | I |
| AU-10 | Cashier calls `GET {B}/kyc`, staff management, analytics → 403 each. Cashier restock, record damage, change a price, give a manual discount, reverse a POS sale, cancel an accepted order → 403 each (D-06, D-65); cashier sell at POS (with automatic round-off to ₹1), count stock (`count_correction`), view items and orders, accept, reject, partial accept, mark ready, dispatch, mark delivered, mark delivery failed, enter the order code (hand over), create a voice draft and confirm its `sale` and `count` lines → allowed (D-65). A manager cancels the same accepted order → allowed | I |
| AU-11 | Admin sign-in without TOTP → cannot reach any admin endpoint; with TOTP → OK | I,E |
| AU-12 | Admin settings change with a TOTP verified 11 min ago → `step_up_required`; after re-verify → OK | I |
| AU-13 | Logout-all → all sessions of that app revoked; other apps unaffected | I |
| AU-14 | Every (role × action) pair matches the golden policy table | U |
| AU-15 | Run a full sign-in under `APP_ENV=local`, `test` and a `prod` build: logs, traces and Sentry events contain no phone, OTP or token in any of them (D-15). The OTP appears only in the mock SMS server's captured message | I |
| AU-16 | Refresh endpoint with a foreign `Origin` or without `X-TZ-Client` → 403 | I |
| AU-17 | Blocked customer signs in → `account_blocked` with support link | I,E |
| AU-18 | Account deletion (D-31). With an order in any non-terminal state → `409 account_has_active_order`, naming the open orders; nothing changes. With none → profile anonymised, sessions revoked, orders kept without PII, wallet balance forfeited by one `customer_wallet → platform_expired` posting of kind `forfeit_deletion` (amount shown in the confirm text, CR-28); ledger invariants hold. The flow calls every registered `CanDeleteAccount` / `OnAccountDelete` hook (BK-44 registers the order check, CR-28 the forfeiture) | I,E |
| AU-19 | JWT signed with an old `kid` still valid until its expiry after key rotation; with an unknown `kid` → 401 | U |
| AU-20 | Turnstile failure token → OTP not sent | I |
| AU-21…30 | UI: sign-in on each app, WebOTP autofill, resend timer, errors translated, cart survives sign-in, invite acceptance, TOTP enrolment with recovery codes | E |

### 3.2 Booking and orders (SC-BK)

| ID | Scenario | Lv |
|---|---|---|
| BK-01 | Place a pickup order with 3 in-stock items → `placed`, `accept_by` = now + 10 min, store gets SSE + push within 3 s, no stock reserved | I,E |
| BK-02 | Place with an item that went out of stock after quote → `409 insufficient_stock` naming the line; nothing held | I |
| BK-03 | Price changed after quote → `409 price_changed` with new quote; second confirm succeeds | I,E |
| BK-04 | Same `Idempotency-Key` twice → one order; different body with same key → 422 | I |
| BK-05 | Store accepts all → `accepted`, `hold_until` = accepted_at + 30 min, reservations held, available stock drops, on hand unchanged (BK-36), customer sees the code | I,E |
| BK-06 | No answer for 10 min → `expired` (not_accepted); credits released; customer notified; store's unaccepted counter increments | I |
| BK-07 | Store rejects with reason → `rejected`; credits released; reason shown to customer in their language | I,E |
| BK-08 | Partial accept (2 of 3 lines, one qty reduced) → `awaiting_customer`; the confirm screen shows the new items total, credits, delivery fee and total; customer confirms within 5 min → `accepted` with those totals. Held credits are re-capped against the new eligible amount (CR-17); new eligible amount under ₹100 → all held credits released and the order uses none (D-28). Delivery fee is recomputed on the new item subtotal (after item-level offers, before the minimum-bill discount and credits; D-94; BK-45) | I,E |
| BK-09 | Partial accept, customer does nothing for `partial_confirm_timeout_min` (5) → `cancelled` with system reason `partial_timeout` (D-68); credits released; reservations released; no strike for the customer or the shop; at 4 m 59 s the customer can still confirm | I |
| BK-10 | Partial accept that removes every line → refused (use reject) | I |
| BK-11 | Correct code at the counter → `collected`; one `sale_online` movement per line consumes the reservation; on hand drops; credits captured to store; cashback posted; payment method recorded; review prompt sent | I,E |
| BK-12 | Wrong code 4 times then right → collected; wrong 5 times → locked; staff sees "Ask customer for a new code"; customer taps New code → new code works, old doesn't | I,E |
| BK-13 | Code entered after `expired` → 409 `invalid_transition` (D-24), no capture, no movement | I |
| BK-14 | Pickup order: 30 min pass after accept → `expired` (no_show); customer strike added; reservation released; credits released. No-show applies to pickup only (BK-41) | I |
| BK-15 | Customer taps "+10 min" at minute 25 → hold_until +10; second tap refused; tap after expiry refused | I,E |
| BK-16 | 3 no-shows in 30 days → 4th order allowed only if no other active order; banner shows until the limit ends 14 days later | I,E |
| BK-17 | Customer cancels in `placed`, `accepted`, `ready` → `cancelled`, holds released, store notified; cancel in `collected` → 409 `invalid_transition` | I |
| BK-18 | Shop cancels an accepted order (manager or owner) with a `ShopCancelReason` (`out_of_stock`, `customer_unreachable`, `closing_soon`, `other` with a note; D-68) → `cancelled`, customer notified, credits and reservations released; no strike and no reliability effect (D-68), counted in the store's order-outcome breakdown (B-12) and visible to admin on A-05. A cashier → 403 (D-65); a reason outside the list, or `other` without a note → 422 | I |
| BK-19 | Race: customer cancel and shop accept at the same millisecond → exactly one wins; the loser gets 409; ledger and stock consistent | I |
| BK-20 | Race: expiry job and code verify at hold_until ± 1 s → exactly one outcome; if collected, no strike | I |
| BK-21 | Expiry job runs twice or late by 5 min → second run no-op; late run still applies the correct outcome based on state | I |
| BK-22 | Auto-accept store with all items available → accepted instantly; with an unavailable item → stays `placed` for manual decision | I |
| BK-23 | Delivery order outside radius → quote refuses delivery; inside radius with the item subtotal (D-94: after item-level offers, before the minimum-bill discount and credits) below the minimum → `below_minimum`; the free-above threshold uses the same item subtotal; both compare inclusively (`>=`), so exactly the minimum or exactly the free-above amount passes | I,E |
| BK-23b | Item subtotal vs eligible amount (D-94): free delivery on ₹500+, fee ₹30, a minimum-bill offer "₹50 off on ₹500+", cart item subtotal exactly ₹500 → delivery fee ₹0 and total ₹450.00 (the offer and the free delivery both apply because both compare the ₹500 item subtotal); the same cart with a ₹30 item offer (subtotal ₹490 from ₹520) → fee ₹30, no minimum-bill offer | I,E |
| BK-24 | Delivery: accept → dispatch with rider → `out_for_delivery`; rider enters code → `delivered`; `sale_online` movement consumes the reservation; credits captured; cashback | I,E |
| BK-25 | Delivery order still in `accepted` after `delivery_dispatch_timeout_min` (60) → `expired` with reason `dispatch_timeout` (D-21); shop gets a strike, customer does not; reservation and held credits released | I |
| BK-26 | From `out_for_delivery`, delivery failed with a reason from the D-60 list → `delivery_failed`; held credits released; no cashback; no stock movement. Shop confirms the goods came back → `return_outcome = returned`, reservation released; shop says they did not → a manager records `damage` (a cashier → 403, D-69), `return_outcome = damaged`, reservation released (D-22). Until the shop answers, the reservation stays held. Unanswered: inbox reminders to the shop at 2 h and 24 h after the failure (not at 1 h 59 m); at 72 h the system auto-confirms the return (`return_outcome = unconfirmed`), releases the reservation and raises a `risk_flags` row; at 71 h 59 m nothing has happened yet (D-69). Support ticket suggested | I |
| BK-27 | Customer's full phone visible to the shop (order detail, order list, SSE payload) only while the order is `out_for_delivery`; masked in every other state, before dispatch and after `delivered` / `delivery_failed` | I |
| BK-28 | Order outside opening hours, or while the shop is closed manually → 409 `store_closed` with next opening time (D-41), at quote and at place; one minute before closing → allowed | I |
| BK-29 | Suspended store disappears from search; its open orders can still complete | I |
| BK-30 | Every invalid (from, to, actor) triple → 409 `invalid_transition`; the table includes the admin-cancel rows (D-20) | U |
| BK-31 | Timeline on both sides shows every transition with actor and reason, in order | E |
| BK-32 | 100 concurrent orders for the last 5 units → total accepted reservations never exceed available stock | I |
| BK-33 | Order totals: subtotal − offers − credits + delivery fee; never below ₹1; rounding to paise consistent between quote, order and receipt | U |
| BK-34 | Order numbers come from one global sequence (D-23): increasing across stores and across IST midnight, no per-day reset; 200 concurrent creates → no duplicates; display `TZ-` plus at least 6 digits, zero-padded (`24817` → `TZ-024817`, `1000000` → `TZ-1000000`) | U,I |
| BK-35 | Customer offline on the tracking screen → reconnect shows the latest state (SSE replay) | E |
| BK-36 | Stock math at accept (D-01): accept 3 of an item with on hand 10 → `stock_reservations` rows created, `store_items.reserved` = 3, available = 7, on hand still 10 and equal to the sum of `stock_movements`; no movement row written | I |
| BK-37 | `sale_online` is written only at `collected` / `delivered` and consumes the reservation (reserved back to 0, on hand −3). For `rejected`, `expired`, `cancelled` and `delivery_failed` → no movement, reservation released, on hand unchanged | I |
| BK-38 | Order code storage (D-04): the customer's `GET /v1/me/orders/{id}` returns the plain code; no `/v1/biz/...` response, order list, SSE event, push or `order_events` row ever contains it; the `orders` row holds only `code_enc` and `code_hmac`; counter lookup by HMAC of the typed code works | I |
| BK-39 | Admin cancel (D-20) from each non-terminal state (`placed`, `awaiting_customer`, `accepted`, `ready`, `out_for_delivery`) by `support`, `admin` and `super_admin` with a reason from the admin-cancel list → `cancelled`; reservations and held credits released; no strike; audit row and `order_events` row with actor and reason. From a terminal state → 409 `invalid_transition`; reason missing or not in the list → 422; `finance` or `field_agent` → 403 | I |
| BK-40 | Admin cancel where the admin picks a side → a strike goes only to that side (customer or shop); without a side, no strike | I |
| BK-41 | No-show is pickup only (D-21): a delivery order in `accepted` 30 min after accept is not `no_show` and the customer gets no strike; only the dispatch timeout (BK-25) ends it | I |
| BK-42 | `delivery_failed` only from `out_for_delivery` (D-22): attempt from `placed`, `accepted`, `ready` or from a pickup order → 409 `invalid_transition` | U,I |
| BK-43 | Store with `accepts_pickup` and `accepts_delivery` both off (D-41) → visible and browsable in discover, shows "In-store only", quote and place refused with `409 store_not_bookable`, no order row. Only one mode off → that mode refused, the other works | I,E |
| BK-44 | `CanDeleteAccount` order hook (D-31): customer with an order in any non-terminal state → false; only terminal orders → true; the hook is registered by orders and runs inside AU-18 | I |
| BK-45 | Partial accept and delivery (D-28): order placed with item subtotal ₹520 (free delivery on ₹500+, fee ₹30) → fee 0; partial accept drops the item subtotal to ₹300 → fee recomputed to ₹30 and shown on the confirm screen; drop below the delivery minimum → order still proceeds (minimum checked only at placement) | I |
| BK-46 | Store strikes (D-67): each `dispatch_timeout` or admin-cancel strike against a store lowers its `store_reliability` score on the next job run, and its search rank with it. 4 strikes in 30 days → no flag; the 5th (`store_strike_risk_threshold`) → one `risk_flags` row, the store stays `live` and bookable (no automatic pause); a strike older than 30 days does not count; changing the setting moves the threshold without a deploy | I |
| BK-61 | Delivery hours (D-91): delivery hours 10:00–12:00 and the clock at 15:00 IST → placing a delivery order returns `409 delivery_closed` with `extensions.nextDeliveryWindow`; pickup at the same store still places; C-11 shows the next window and "Switch to pickup" | I,E |
| BK-62 | No-show limit (D-91): C13 (3 no-shows in 30 days) places one pickup order → `placed`; a second order while the first is active → `409 ordering_suspended` with `extensions.until` = the limit end date; after the first order is cancelled the second is allowed | I,E |
| BK-63 | One extension (D-91): "I'm on my way" extends the pickup hold once → allowed; a second extension on the same order → `409 extension_already_used` | I,E |
| BK-64 | `serverNow` (D-93): every order response and SSE order event carries `serverNow`; with the test clock advanced 9m59s the C-12 and B-04 countdowns read about 0:01 computed from `serverNow`, and a device clock set 1 h wrong changes nothing | I,E |
| BK-65 | Store hours 00:00–24:00 (D-93): a store with `{"open":"00:00","close":"24:00"}` is open at 00:00:00, 12:00 and 23:59:59 IST and never returns `store_closed`; B-14 shows the "Open 24 hours" toggle on and saves that value | I,E |
| BK-47…60 | Builder-written: notifications per transition, ready state, alert escalation SMS after 3 min unseen, store hours edge cases, language of notifications, multi-store business routing | — |

### 3.3 Credits (SC-CR)

| ID | Scenario | Lv |
|---|---|---|
| CR-01 | Wallet 100, order ₹250, use 30 → held 30; wallet shows available 70, held 30 | I |
| CR-02 | Use 31 → refused `credit_max_exceeded`; order ₹99 with credits → `below_credit_minimum` | I |
| CR-03 | Order ₹100 with 30 credits → total ₹70; order ₹25 with credits → refused (min order) | U |
| CR-04 | Collected → held 30 moves to store_credit; customer history line "Spent at <shop> · order TZ-…" | I |
| CR-05 | Cancel/expire → released; history shows "Returned: order cancelled" | I |
| CR-06 | Cashback 5% on ₹487 paid → 24 credits (floor); store_credit −24 | U,I |
| CR-07 | Cashback store with 0% → no posting | U |
| CR-08 | Expiry: lots earned 366 days ago expire FIFO; partial lot consumption tracked; reminder sent 7 days before | I |
| CR-09 | Spending consumes oldest lots first | U |
| CR-10 | Customer at the monthly cap (`credit_orders_per_month`, 10 credit-using orders this IST month): the quote shows "You've used this month's credit limit" (D-76) before placing; placing with credits → `credit_monthly_cap`; placing without credits → `placed` (D-30) | I |
| CR-11 | Store at `store_credit_redemptions_per_day` (200) redemptions this IST day: the 201st quote shows "Credits can't be used at this shop today" (D-76); placing without credits → `placed`; placing with credits → refused with `409 store_credit_cap` and a `risk_flags` row | I |
| CR-12 | 50 goroutines spend from one wallet of 100 credits, 30 each → at most 3 succeed; balance never negative | I |
| CR-13 | Random sequences (rapid, 10k runs) keep invariants 1–7 | U |
| CR-14 | Nightly reconcile with a manually corrupted cache row → alert fires, nothing auto-fixed | I |
| CR-15 | Admin adjustment (D-05, D-73, threshold `admin_adjust_dual_approval_above_paise` = 50000). Adjustments are whole credits; a fraction is rejected with `422 validation_failed`. The threshold compares the absolute value of the amount and is exclusive: +₹200, +₹500, −₹200 and −₹500 → post at once with no `admin_adjustment_requests` row; +₹501, −₹501, +₹600 and −₹600 → pending `admin_adjustment_requests` row, nothing posted until a different `finance` or `super_admin` approves. The E2E part (A-09 Pending approvals) runs at the W3 gate | I,E |
| CR-16 | Store credit balance goes negative from cashback → allowed; shown in red in B-10; carried to statement | I |
| CR-17 | Partial accept re-cap (D-28): 30 credits held from lots L1 and L2. New eligible amount still ≥ ₹100 and above the per-order cap → 30 stay held. Per-order cap raised in settings above the new eligible amount → held reduced to the new max and the difference released to the same lots. New eligible amount 9999 paise → all 30 released to L1 and L2, order uses no credits, confirm screen shows it | I |
| CR-18 | Bill QR claim with credits → held then captured when the POS sale completes; claim expired → released | I |
| CR-19 | Ledger entries are append-only: UPDATE/DELETE as the app role → permission denied | I |
| CR-20 | Wallet history text in each language uses the right shop name and order number | E |
| CR-21 | Eligible amount boundaries (D-02, `credit_min_order_value_paise` = 10000): eligible 9999 paise with credits → `below_credit_minimum`; 10000 → allowed; 10001 → allowed. Items ₹120 with a ₹21 offer (eligible 9900) → refused. Items ₹95 plus delivery fee ₹20 → refused (fee is not part of the eligible amount). `orders.items_after_offers_paise` stores the eligible amount and invariant 5 checks it against the setting | U,I |
| CR-22 | Credits never pay the delivery fee (D-29): with the per-order cap raised in settings above the items total, credits used never exceed the eligible amount, so the delivery fee is always paid in money; with fee 0 the total still never goes below ₹1. The per-order cap is applied to the eligible amount. The checkout copy reads "₹100 or more" with the number from settings | U,I |
| CR-23 | Cashback base = eligible amount − credits used, delivery fee excluded (D-29): eligible ₹300, 30 credits, fee ₹25, 5% → floor(270 × 5 / 100) = 13 credits (not 14) | U |
| CR-24 | Lot expiring while held (D-27): lot L (30) expires at T while all 30 are held. At T the expiry job skips the held portion. Order `collected` at T + 1 h → 30 captured, nothing to `platform_expired`. Second run: order cancelled at T + 1 h → the 30 post to `platform_expired` at once and the wallet does not regain them. Invariant 3 holds after every step | I |
| CR-25 | A released hold goes back to the lots it came from (D-27): hold 30 from L1 (20) and L2 (10), release → L1 and L2 restored, no new lot row, FIFO order unchanged; every credit into `customer_wallet` (cashback, referral reward, positive admin adjustment) creates a lot | I |
| CR-26 | IST day and month boundaries (D-30): store-day cap reached at 23:59:59 IST → order at 00:00:00 IST (18:30:00 UTC) can use credits; still refused at 23:59:59. Monthly cap reached on the 31st at 23:59 IST → allowed on the 1st at 00:00 IST | I |
| CR-27 | Two-person approval roles (D-05): a `super_admin` adjustment above the threshold is also pending; the requester approving their own request → refused; a second `super_admin` or a `finance` admin approves → posted, audit holds both ids; `admin`, `support` and `field_agent` request or approve → 403 | I |
| CR-28 | Deletion forfeiture (D-31): wallet 83 across 2 lots → `OnAccountDelete` posts one `customer_wallet → platform_expired` entry of kind `forfeit_deletion` for 83; lots closed; balance 0; the hook run twice → one posting | I |
| CR-29…30 | Builder-written | — |

### 3.4 Referrals (SC-RF)

| ID | Scenario | Lv |
|---|---|---|
| RF-01 | Customer A shares link; B signs up through it → referral pending; no credits yet | I,E |
| RF-02 | B completes first order with a qualifying amount ≥ ₹100 (`collected`) → A +30, B +100, from platform_marketing; both notified | I,E |
| RF-03 | Qualifying amount = eligible amount − credits used (D-32): B's first order ₹80 → not qualified; ₹120 with 30 credits (₹90) → not qualified; second order ₹150 → qualified | I |
| RF-04 | Scan of a store's QR standee then sign-up → referral kind retailer_customer; store_credit +30 on qualification | I,E |
| RF-05 | Self-referral (same phone/user) → rejected | I |
| RF-06 | Same device (D-13): a sign-up on a device already tied to the referrer, or to another account in the last 90 days → recorded `rejected_same_device`, no reward, never qualifies; the same device after 91 days → normal pending referral. A 3rd account on one device → risk flag raised | I |
| RF-07 | B clicks A's link then C's link within 7 days → A gets it (first touch) | I |
| RF-08 | Attribution window (D-32, `ref_attribution_window_days` = 7): link opened 6 d 23 h 59 m before sign-up → attributed; 7 d + 1 s → no attribution; changing the setting to 3 changes the window without a deploy | I |
| RF-09 | New shop referred by a customer → after verification + 10 completed orders/linked sales → customer +30, the new shop +100 to the store credit account of its primary store (first live store, D-26) | I |
| RF-10 | Referral amounts changed in settings → only new referrals use new amounts | I |
| RF-11 | Referee's order cancelled → no qualification | I |
| RF-12 | A POS sale linked by `phone_unverified` (D-07) as the referee's first purchase → referral stays pending | I |
| RF-13…20 | Builder-written. Standee scenarios are SC-ST (§3.6) | — |

### 3.5 POS (SC-POS)

| ID | Scenario | Lv |
|---|---|---|
| POS-01 | Online sale of 3 lines, cash → sale stored, stock movements `sale_pos`, receipt link works | I,E |
| POS-02 | Reverse a sale (manager) within `pos_reversal_window_hours` (24) → reversal sale + `pos_reversal` movements put the quantities back; at 24 h + 1 s → refused; cashier cannot reverse | I |
| POS-03 | Offline: 20 sales, reload app, still listed as pending; online → synced in order; server totals match | E |
| POS-04 | Two offline devices sell the last unit → both accepted, stock −1, item flagged "count needed" | I |
| POS-05 | Same outbox sent twice (network retry) → no duplicates (client id) | I |
| POS-06 | Device clock 2 h wrong → `sold_at` stored as device time, `received_at` server time; reports use sold_at clamped to [received_at − 72 h, received_at] | I |
| POS-07 | Bill QR shown, customer scans within 120 s, confirms 30 credits → POS updates live "claimed by Ananya · 30 credits"; total reduced | E |
| POS-08 | Bill QR after 120 s → `claim_expired`; POS can refresh | I,E |
| POS-09 | Two phones scan one QR → first confirm wins; the second gets `409 claim_already_used` and sees "already claimed" | I |
| POS-10 | Tampered token (amount changed) → signature invalid | U |
| POS-11 | Counter code correct (offline phone) → sale linked, cashback earned, no credits spent | I,E |
| POS-12 | Counter code from the previous 60 s step → accepted; two steps old → refused | U |
| POS-13 | 5 wrong counter codes on one bill → locked for that bill | I |
| POS-14 | Sale linked by `bill_qr` or `counter_code` with a qualifying amount ≥ ₹100 as the referee's first purchase → referral qualifies | I |
| POS-15 | Scanner input "8901058000177⏎" adds the matching item in < 100 ms; unknown barcode → "Add product" sheet | E |
| POS-16 | Discounts and round-off never make total negative; receipt shows them. Round-off is to the nearest ₹1 with half rounding up (D-94): ₹100.49 → ₹100 (−₹0.49), ₹100.50 → ₹101 (+₹0.50); `round_off_paise` stays within −49…+50 | U |
| POS-17 | Phone-only link (D-07): cashier types a phone with no code → `link_method = phone_unverified`; customer added to the shop's customer list; no cashback, credit spending refused, no referral qualification, zero ledger entries. Only `bill_qr` and `counter_code` sales touch the ledger | I |
| POS-18 | Reverse a linked sale with credits spent (D-08): 30 credits came from L1 (20) and L2 (10) → one `reversal` ledger entry; 30 back to L1 and L2 per the consumption rows; the store credit receivable for those 30 reversed | I |
| POS-19 | Same as POS-18 but L1 expired between sale and reversal → its 20 post to `platform_expired`; only L2's 10 return to the wallet; invariant 3 holds | I |
| POS-20 | Reverse a sale whose cashback (15) was partly spent: available 5, held 10 on another order → clawback 5, available 0, held untouched, wallet never negative; the store absorbs the 10 shortfall; platform accounts unchanged | I |
| POS-21 | Reverse a sale that qualified a referral: reward not yet posted → qualification revoked, referral back to pending; reward already posted → reward stays, a risk flag is raised. The not-yet-posted branch applies to the shop reward, which waits for the 10th order (D-72); a customer referral reward posts with the qualifying sale, so that branch is not reachable for it | I |
| POS-22 | Offline counter code verified at sync (D-12): the POS stores the typed code and device time; at sync the server checks the code against the time step at the recorded time. Accepted only if synced within `pos_offline_link_max_hours` (11 h 59 m ok, 12 h 1 m refused), the device clock was within 2 min of the server at its last online check (1 m 59 s ok, 2 m 1 s refused), and the code was not used before (reuse refused). On failure the sale is kept, unlinked, no ledger entries, and the shop sees "Customer link failed" | I |
| POS-23 | 21st offline counter-code link at one store in an IST day (`pos_offline_links_risk_per_day` = 20) → risk flag raised; links are still verified normally | I |
| POS-24 | Sync cursor (D-36): cursor 29 days old → delta from `catalogue_changes`; older than 30 days → `resync_required`, the device downloads the full snapshot, then deltas from the new cursor work. `outbox_events` is never used for catalogue sync | I |
| POS-25 | Receipt link (D-45): `GET /v1/public/receipts/{token}` with a 128-bit token shows items, totals and the store only; no customer name, phone, wallet or link method in the body or headers; after 30 days → gone; a random token → not found with the same timing; requests are rate limited | I,E |
| POS-26 | `GET /v1/me/purchases` (D-45) lists the customer's online orders and POS sales linked by `bill_qr` / `counter_code` in one list, newest first; another customer's sales never appear; the C-13 Orders "In store" tab shows the POS rows (D-75) | I,E |
| POS-27 | ₹100 minimum at POS (D-30): bill after discounts 9999 paise → bill QR credit use refused, claim without credits still links and earns cashback; 10000 → credits allowed. A bill QR claim that spends credits counts toward the customer's monthly cap and the store's daily cap, like an online order (D-30). There are no earn caps (D-62): cashback on a linked sale is limited only by the store's cashback rate | I |
| POS-28…35 | Builder-written (backend) | — |
| POS-36…55 | Builder-written (W2-11) including camera scanning, receipt share, shortcuts, catalogue delta sync after price change | — |

### 3.6 QR standee (SC-ST)

D-54 renumbers W3-09's standee scenarios. W2-10 owns the PDF and preview (SC-ST-01…02); W3-09 owns campaigns and analytics (SC-ST-03…04).

| ID | Scenario | Lv |
|---|---|---|
| ST-01 | Standee PDF and PNG: the QR decodes to the standee URL carrying the store short code and `?s=<standee_id>` and no other code (D-26); text in the shop's language plus English, with the credit number read from settings; PDF snapshot matches | U |
| ST-02 | Scanning the decoded URL on a phone opens the store page in the customer app; signing up from it creates a `retailer_customer` referral to that store | E |
| ST-03 | A scan records one scan against that standee; an unknown or another store's `standee_id` → the store page still opens, the scan is not attributed | I |
| ST-04 | Standee analytics per standee: scans → sign-ups → qualified referrals match the seeded events; the business sees only its own standees | I,E |

### 3.7 Other areas (minimum lists)

Numbered scenarios below are fixed IDs. The unnumbered items take the next free IDs in the owning WP's range.

- **Business (SC-BZ):** staff limits (BZ-01 below); onboarding save/resume; submit with missing fields → list of fields; admin request-info → owner sees exact fields; business verified and store `live` → appears in search within 1 min; hours across midnight; holiday closed; staff invite accept; cashier scoped to one store; KYC PAN format; GSTIN checksum; GSTIN and UPI ID stored encrypted with a masked copy showing the last 4 (D-44); KYC and payout details (PAN, GSTIN, UPI ID, bank): the owner edits them; a manager sees them masked (last 4); a cashier gets 403. On the admin side, finance, admin and super_admin see them masked (`admin.payout.bank_view`); full values appear only in the payout export CSV (D-85) or through `kyc/reveal` (admin, audited).
- **Catalogue (SC-CT):** create store item from master; create missing product → usable at once, in review queue; duplicate suggestion by barcode; import 5,000 rows with 37 errors → preview lists all 37; movement history; on hand always equals the sum of movements and a movement source outside the D-01 enum is refused; stale stock (> 72 h) flagged and ranked lower; batches expiring in 5 days appear in "going to waste".
- **Search (SC-SR):** typo "amull buttr" finds Amul Butter; "dahi" finds curd (synonym); Kannada query via transliteration; results grouped and ranked (open + in stock first); radius change; zero results logged; p95 latency on 1,000 synthetic stores.
- **Notifications (SC-NT):** each event in rules §15 delivered on the right channels; disabled channel skipped; SMS escalation; push permission denied → inbox only; templates per language.
- **Reviews (SC-RV):** one per order; edit window; reply; admin hide sets `hidden_at` and the rating recalculates; restore clears it; both are audited; support and above restore a hidden review with a reason, audited (D-41); report flow.
- **Offers and banners (SC-OF):** percent, flat, buy X get Y, minimum-bill; one item offer per line + one bill offer; scheduled start/end; per-customer limit; never below ₹1; same price in quote and POS; flash offer from batch; 4th live banner refused; banner with unsafe image held for review; admin takedown hides it everywhere within 1 min; Offers near you shows max 1 per shop and rotates fairly.
- **Shop coach (SC-AI-10…40, W3-11; consumer AI is SC-AI-41…55, W3-12; D-54):** seeded week with 3 unaccepted expiries at 8 pm → a Low card with the right numbers and the auto-accept action; top seller at 2 days of cover → Restock card with quantity; record sales day → High card; AI off → template text; ungrounded number in a draft → rejected and template used; Done/Not useful recorded; a statement about other people (searches nearby, or a nearby benchmark) built on fewer than 10 people or stores is not shown (k ≥ 10, D-71).
- **Payouts (SC-PO):** month close with sample ledger → statement numbers exact; negative carries forward; KYC missing → held; mark paid needs UTR + step-up; CSV matches.
- **Risk (SC-RK), Insights (SC-IN), Voice (SC-VO), Admin (SC-AD), Field agents (SC-FA), Support (SC-SP), Observability (SC-OB), AI (SC-AI), UI kit (SC-UI), i18n (SC-I18N), Site (SC-SITE), Infra (SC-INF), Customer UI (SC-CU), Business UI (SC-BU):** builder-written in their WP, minimum 1 scenario per acceptance criterion in [06-screens.md](06-screens.md) or rule in [01-product-rules.md](01-product-rules.md).
- **Observability must include:** a request appears as metric + log + trace with one trace id (OB-01); PII absent from all three (OB-02); a forced slow query appears in dashboard 00 "Slowest SQL" (OB-03); a forced slow route ranks first in "Slowest endpoints by impact" (OB-04); a forced timer delay raises the lateness alert (OB-05); RUM from a throttled Playwright run appears on dashboard 05 within 2 min (OB-06); rollups match raw counts for a seeded day (OB-11).

Fixed scenarios in other areas:

| ID | Scenario | Lv |
|---|---|---|
| SR-01 | Store visibility (D-40): only `live` stores of `verified` businesses appear in discover summary, list, search and `/v1/discover/stores/{slug}`; `draft`, `pending_review`, `paused`, `suspended` stores and stores of unverified businesses → absent (404 on the store page). A new store at a verified business stays hidden in `pending_review` until an admin review makes it `live` | I |
| SR-02 | Filter first (D-42): an out-of-stock item, a non-`live` store or a store outside the radius never appears, whatever its text score. Score weights come from `search_w_*`; changing one in settings changes the order without a deploy; equal scores → nearer store first | I |
| SR-03 | `has_offer=true` (D-42) returns only items with a live offer; the C-05 chip sets it; turning it off returns the full set | I,E |
| SR-04 | "Nearest first" sort orders results strictly by distance; switching back restores the ranked order | I,E |
| SR-05 | In-store only store (D-41) appears in search and on its page with the "In-store only" label and no add-to-cart | I,E |
| FA-01 | Field agent window (D-39, `field_agent_edit_window_days` = 30): the agent adds and edits items on `/v1/admin/field/businesses/{bid}/stores/{sid}/items` for a business they onboarded, while it is still onboarding and until 30 days after verification (29 d 23 h ok, 30 d + 1 s → 403); a business onboarded by another agent → 403; changing the setting moves the window | I |
| PO-01 | One open payout (D-09): month 1 closes → payout P1 `pending`; month 2 closes while P1 is unpaid → P1's amount is recomputed to store credit balance − other unpaid payouts and no second row is created; a direct insert of a second `pending`/`held` row for the business → unique-index violation | I |
| PO-02 | A `held` payout (KYC missing) counts as open: the next close recomputes it; after P1 is marked paid, the next close creates a new payout | I |
| PO-03 | Payout actions by role (D-64): `finance` and `super_admin` hold, unhold, mark paid (UTR + step-up) and export → allowed; `admin` mark paid and hold → 403; `support`, `catalogue`, `analyst` and `field_agent` → 403 on every payout action; `finance` sees bank details masked | I |
| PO-04 | Payout export (D-93): `finance` export at step-up returns a CSV with exactly the columns `payout_id, period, business_id, legal_name, beneficiary_name, amount_inr, upi_id, bank_account_number, ifsc` (`amount_inr` with 2 decimals); only `pending` payouts are in the file (a `held` and a `paid` payout are absent); the exported rows get `exported_at`; a cell beginning with `=`, `+`, `-` or `@` is prefixed with `'`; `admin` → 403 | I |
| BZ-01 | Staff limits (D-66): the owner adds, changes and removes managers and cashiers → allowed. A manager adds and removes a cashier → allowed; a manager adds a manager, removes a manager or the owner, or changes a cashier to manager → 403. A cashier → 403 on every staff action. There is no business-delete endpoint: `DELETE` on the business → 405, nothing changes | I |
| AD-01 | Admin roles (D-64), API and UI: `catalogue` approves, merges and rejects in the A-08 review queue → allowed; `catalogue` on orders, credits, payouts, settings or risk → 403. `analyst` reads Pulse → allowed; `analyst` on any write (cancel, adjust, settings, catalogue merge, payout) → 403. `field_agent` reads `/v1/admin/field/me/stats` → only their own numbers. Pulse → every role except `field_agent`. KYC verify → `admin` and `super_admin` only. Money settings (unit `paise`, `bp` or `credits`) → `super_admin` only; `admin` changing one → 403 | I |
| AD-02 | Role-aware nav (D-64): signed in as each of the 7 roles, the admin sidebar shows exactly that role's screens (06 §3 table); a user with two roles sees the union; opening a hidden screen by URL shows the no-access state, and no 403 appears in a normal flow | E |
| RV-01 | Review edit window (D-91): a review edited within `review_edit_window_days` → allowed; the test clock moved past the window → `409 review_edit_window_passed`; the stored text is unchanged | I |
| AI-01 | AI daily cap (D-91): with `ai_daily_calls_customer` = 2, the third call by one customer → `429 ai_daily_cap_reached` with `extensions.scope` `customer`, `Retry-After` at the next IST midnight, no model call made; the same for the store scope and the platform scope (`ai_daily_cost_cap_paise`, super_admin to change) | I |
| VO-01 | Voice confirm per line (D-06, D-65): a cashier opens a draft with `sale`, `count`, `restock` and `set_price` lines → `restock` and `set_price` shown disabled with "Ask a manager"; forcing them through the API → 403 for those lines while the `count` line applies as `count_correction` and the `sale` line records a no-customer POS sale (`sale_pos`); "Coca-Cola 750 ml out of stock" becomes a `count` of 0; a manager confirms all four | I,E |
| VO-02 | No voice line changes stock or price without a person's confirm: a draft left unconfirmed writes nothing | I |

### 3.8 Platform, contract and secrets (SC-PL)

| ID | Scenario | Owner | Lv |
|---|---|---|---|
| PL-01 | No duplicate paths (D-10): the bundled `openapi.yaml` defines no method + path pair twice, and every path starts with one of `/v1/discover`, `/v1/me`, `/v1/biz/businesses/{bid}`, `/v1/admin`, `/v1/public`, `/v1/auth`, `/v1/rum`, `/v1/events`, `/v1/client-errors`, `/v1/test` (the namespaces in 04 §1.1); `/healthz`, `/readyz` and `/version` are the only paths outside `/v1`. A seeded duplicate or stray prefix fails `make gen` and `make check` | W0-04 | U |
| PL-02 | Problem registry (D-24): every problem code the API returns is in `backend/api/problems/<domain>.yaml` with an HTTP status and an i18n key that exists; `invalid_transition` is the only code for a refused state change; Schemathesis fails on an unregistered code | W0-04 | U,I |
| PL-03 | Secrets never printed (D-18): on a fresh clone with no `.env.local`, `make setup` generates every key in the D-18 table and prints only `generated: NAME` per key; a second run generates nothing; `make env-check` prints only `NAME set` / `NAME missing`. The test captures stdout and stderr of both and of `make dev` start-up and finds none of the generated values (raw, base64 or hex) | W0-01 | I |
| PL-04 | Default suite needs no real keys (D-17): `make check` and `make e2e` pass with every provider key unset (SMS, WhatsApp, push, Sentry, Anthropic, Sarvam); outbound traffic to any non-local host is blocked during the run and nothing tries to reach one | W0-01 | I |
| PL-05 | Provider keys live in env only (D-19): no `platform_settings` key holds a key, token or secret (schema test over every key and its `unit`); with a sentinel value in a provider key env var, no admin API response, A-17 screen, audit row, log or trace contains it, and A-17 shows `set` / `missing` only. A channel works only with its flag on and its key set: flag on + key missing → not used, status `missing`; flag off + key set → not used | W3-05 | I,E |
| PL-06 | Configurable numbers come from settings: for each rule in 01 marked configurable that a test above covers, changing its `platform_settings` value changes the behaviour without a deploy; a lint (`settingslint`) finds no hard-coded copy of a distinctive default in `backend/internal` (escape: `//settings:ok <reason>`; a bare escape is a finding; a seeded fixture proves it fires); every key, unit and default in 01 §17 matches the settings seed migration and the generated key constants, both ways | W0-03 | U,I |

### 3.9 Cross-app journeys (SC-E2E)

The Gate column (D-11) says which wave gate runs the journey: W2 (the W2-13 gate, journeys whose dependencies are all in Waves 0–2) or W3 (the W3-14 gate). W4-01 runs all of them.

| ID | Journey | Gate |
|---|---|---|
| E2E-01 | New customer via QR standee → search → order pickup with no credits → shop accepts → collect with code → review → referral (store) qualifies → store sees +30 | W3 |
| E2E-02 | Referred customer with 100 credits → order ₹300 using 30 → collected → 5% cashback 13 → wallet 83 → shop credit account +30 −13 | W2 |
| E2E-03 | Partial accept → customer sees and confirms the new totals → collected | W2 |
| E2E-04 | No-show (pickup) → strike → after 3, limit banner | W2 |
| E2E-05 | Delivery order end to end with rider; full phone visible to the shop only while out for delivery | W2 |
| E2E-06 | POS walk-in with bill QR credits | W2 |
| E2E-07 | POS goes offline after an online clock check → customer reads the counter code → sale recorded with the typed code and device time → POS back online within 12 h → server verifies the code at the recorded time step → sale linked, cashback posted. Variants: sync after 12 h, device clock off by more than 2 min at the last online check, or a reused code → sale kept but unlinked, no ledger entries, shop sees "Customer link failed" (D-12) | W2 |
| E2E-08 | Field agent onboards shop → owner confirms → admin verifies the business and approves the store (`live`) → shop appears in search → first order | W3 |
| E2E-09 | Month end → statement → finance marks paid → business sees Paid with UTR | W3 |
| E2E-10 | Admin hides a review → rating changes on the customer shop page (W2 gate: admin API; W3 gate: admin UI, D-83) | W2, again at W3 |
| E2E-11 | Voice update draft → edit → confirm → stock changes → customer sees updated freshness | W3 |
| E2E-12 | Settings change (`credit_max_per_order` 30 → 40) by super admin with step-up → next checkout allows 40; audit entry visible (W2 gate: admin API; W3 gate: admin UI, D-83) | W2, again at W3 |
| E2E-13 | Shopping list search (AI-C1) → shop with 4/4 → cart filled → order | W3 |
| E2E-14 | Supplier bill scan (AI-R2) → confirm → stock and batches updated → "going to waste" shows expiring batch → flash offer → customer sees offer | W3 |
| E2E-15…20 | Builder-written (language switch mid-journey, offline customer, multi-store owner, cashier permissions, blocked customer, suspended shop) | Set per journey from its dependencies |

The E2E part of CR-15 (A-09 adjustment and approval screens) also runs at the W3 gate. E2E-10 and E2E-12 are W2 by D-11, but the admin screens they use (A-12 Reviews, A-17 Settings) come from W3-05. At the W2 gate they drive the admin API; at the W3 gate they run again through the admin UI, and W4-01 runs them through the UI too (D-83).

## 4. Test data and fake time conventions

- Money in tests uses named constants (`rupees(250)`), never bare numbers.
- Every timer test states time explicitly (`clock.Set("2026-11-03T10:00:00+05:30")`) and covers the IST midnight boundary. Day and month caps, order numbers and offline-link counts are also tested across IST midnight and month end.
- Every boundary test reads the number from `platform_settings` and tests both sides of it in the setting's unit (for a ₹100 rule: 9999, 10000 and 10001 paise), never a hard-coded copy of the default.
- Every money test asserts the ledger invariants after the action (`testkit.AssertLedger(t, db)`).

## 5. Manual QA scripts

Stored in `docs/qa/manual/<area>.md`. Each step has an expected result and a screenshot slot. The QA agent runs them in the browser pane with device emulation; before cloud launch the founders run the ★ scripts on real phones.

| Script | Covers | Devices |
|---|---|---|
| MQ-01 ★ First run customer | language, location denied → PIN, home map, radius chips, categories, shop, product | 360×740 Android Chrome, 390×844 iOS Safari |
| MQ-02 ★ Order pickup | cart, sign-in mid-checkout, credits, place, live tracking, code at counter, review | two devices (customer phone + shop phone/desktop) |
| MQ-03 ★ Shop day | sign-in, sound alert, accept/partial/reject, POS 10 sales with scanner, offline 5 sales, sync, bill QR with customer phone | Android phone + 1280×800 desktop + USB scanner |
| MQ-04 Delivery | settings radius, out-of-radius customer, rider code | 2 devices |
| MQ-05 ★ Onboarding | self-serve wizard and field agent flow with owner OTP | phone |
| MQ-06 Money month | seed a month, close, review statement, mark paid, business view | desktop |
| MQ-07 Admin roles | each of the 7 roles (D-64) signs in, checks the sidebar shows only its screens, and tries forbidden actions; credit adjustment above ₹500 needs a second person; A-17 shows provider keys as set/missing only | desktop |
| MQ-08 Languages | each language on the 5 main screens: no clipping, correct fonts, numbers/₹ | phone |
| MQ-09 Accessibility | TalkBack/VoiceOver through MQ-01 and MQ-02; keyboard-only admin | phone + desktop |
| MQ-10 Slow network | Chrome "Slow 4G" + CPU 4× throttle on customer home and search; record LCP | phone emulation |
| MQ-11 AI features | list search, voice search, bill scan, shelf photo, review reply draft, ticket triage: wrong outputs never applied without confirm | phone + desktop |
| MQ-12 Observability | run MQ-02 and MQ-03, then open dashboards 00, 02, 05, 06, 07, 08, 10 and confirm the activity shows; open one trace from a slow request | desktop |

Device matrix for the cloud-launch pass (real devices): Redmi/Realme Android Go-class (2 GB RAM), Samsung A-series mid-range, an iPhone SE or 11 on iOS 17+, Windows laptop Chrome, MacBook Safari, an Android tablet at the counter.

## 6. Load tests (k6, local in wave 4, staging in wave 5)

| Scenario | Shape | Pass |
|---|---|---|
| `browse_peak` | 300 VUs: home summary, store list, search, shop page, product (think time 3–8 s) | p95 reads < 200 ms (search < 300 ms), errors < 0.1% |
| `order_burst` | 50 orders/min across 100 stores with accept/collect by shop VUs | p95 writes < 400 ms; timers late < 10 s p99; ledger reconcile clean |
| `pos_rush` | 200 stores × 1 sale/20 s + 10% via sync batches of 20 | p95 < 400 ms, no duplicate sales |
| `sse_fanout` | 2,000 business + 3,000 customer SSE connections, 20 events/s | delivery p95 < 2 s, memory stable |
| `otp_abuse` | 1 IP sending 100 OTPs/min | blocked after limits; legit users unaffected |
| `soak` | browse + orders at 30% peak for 2 h | no memory growth, no connection leak |

Target sizing for launch: 1,000 stores, 50,000 customers, 5,000 orders/day, 50,000 POS sales/day.

## 7. Security tests

- Static: gosec, govulncheck, `npm audit --production`, gitleaks (full history), Semgrep OWASP rules, Trivy on the Docker image, checkov on Terraform.
- Dynamic: ZAP baseline on every app in `make security`; ZAP full + authenticated scan in wave 4.
- Abuse suite (`e2e/security`): IDOR on every `{id}` route with another tenant's id; role escalation attempts; brute force of OTP, order codes and counter codes; replayed bill tokens and refresh tokens; mass referral sign-ups; CSV injection in exports (cells starting with `=+-@` escaped); XSS payloads in shop names, reviews and product names rendered in all apps; oversized/polyglot image uploads; SSE subscription to another store's channel; the order code requested through any `/v1/biz` route (BK-38); receipt token enumeration (POS-25); reserved test phone numbers in a `prod` build (§2 Test OTP); provider and generated key values absent from every response, log and trace (PL-03, PL-05); the `/v1/test/*` routes absent (404) in staging and prod builds (D-80).
- ASVS L2 checklist with evidence links in `docs/security/asvs.md`.

## 8. Release gates

`make check` (every merge) → `make e2e` (every WP merge and nightly locally) → `make load` + `make security` + MQ full run (wave 4 exit) → staging run of everything (wave 5) → production smoke (MQ-01/02/03 shortened) after each deploy.
