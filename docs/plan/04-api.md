# 04 · API

The contract lives in `backend/api/` (OpenAPI 3.1), split per domain: `paths/<domain>.yaml`, `schemas/<domain>.yaml`, `problems/<domain>.yaml`. A work package edits only its own domain files (D-55). `make gen` bundles them into `openapi.yaml` and generates the Go server stubs, the TypeScript client (`@todayz/api-client`, in `shared/api-client`) and sqlc. Generated output is not committed; every build and check runs `make gen` first (D-50).

## 1. Conventions

- Base: `https://api.todayz.in/v1` (local: `https://api.todayz.localhost/v1`).
- JSON, `camelCase` fields, ULID string ids, money as integer paise in fields named `*Paise`, credits as integers (1 credit = ₹1), timestamps RFC 3339 UTC.
- Pagination: cursor based. `?limit=20&cursor=…` → `{ "items": [...], "nextCursor": "…" | null }`. Max limit 100.
- Filtering and sorting through named query params only (no free-form SQL-ish filters).
- Concurrency: mutable resources return `ETag: "v{version}"`; updates send `If-Match`. Mismatch → `412`. If-Match also accepts the forms a compressing proxy hands out (`W/"v3"`, `"v3-gzip"`); only the version is compared (D-109).
- Rate-limit headers: `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`; `429` with `Retry-After`.
- Tracing: clients send `traceparent`; every response returns `X-Request-Id`.
- Versioning: `/v1`. Additive changes only within v1. The oldest client allowed per app is set in settings (`min_client_version_customer`, `min_client_version_business`, `min_client_version_admin`); an older app shows "Update available" and reloads its service worker when the user taps, never by itself and never while a POS sale or a checkout is open (D-98).
- Language: `Accept-Language` picks translated category names and notification text.
- Time: timed responses (every order response and every SSE order event) include `serverNow` (RFC 3339). Clients compute countdowns against it: remaining = deadline − (`serverNow` + time elapsed since receipt), never against the device clock.

### 1.1 Namespaces (D-10)

| Prefix | Who | Auth label |
|---|---|---|
| `/v1/auth/...` | Sign-in, refresh, sign-out | pub, cookie |
| `/v1/discover/...` | Anyone; read-only view of `live` stores of `verified` businesses (D-40) | pub |
| `/v1/me/...` | The signed-in customer. The profile rows in 2.2 marked **any** serve whoever is signed in, in any app | cus |
| `/v1/biz/businesses/{bid}/...` and `/v1/biz/businesses/{bid}/stores/{sid}/...` | Staff of that business; the server checks membership, store scope and role | biz(role) |
| `/v1/admin/...` | Admins | adm(roles) |
| `/v1/public/...` | No account: leads, receipts, referral links, share cards and the site search strip (D-101) | pub |
| `/v1/rum`, `/v1/events`, `/v1/client-errors` | Telemetry | pub, rate-limited |
| `/v1/test/...` | Registered only when `APP_ENV` is `local` or `test`, for manual and automated testing; staging and prod builds return `404` (D-80) | none |

Shorthand in 2.6: `{B}` = `/v1/biz/businesses/{bid}`, `{S}` = `{B}/stores/{sid}`.

### 1.2 Path rules

- No method + path pair is defined twice.
- No literal segment sits beside a path parameter at the same level. For example `/items/{itemId}` and `/items/bulk` cannot both exist, so imports are `/item-imports`. A slug can never be read as a sub-route, and a literal can never be read as an id.
- Public store paths take only `{slug}` (lowercase letters, digits, hyphens). Nothing under `/v1/discover/stores/` takes an id, and no literal segment sits beside `{slug}`. Ids (ULIDs) appear only under `/v1/me`, `/v1/biz`, `/v1/admin`.
- A parameter at the same position under the same parent has the same name everywhere.
- `make gen` runs a path lint that fails on any of the above.

### 1.3 Auth and roles

- Access token: EdDSA JWT with `kid` (keys from `JWT_SIGNING_KEYS`, signer `JWT_CURRENT_KID`), held in memory, sent as `Authorization: Bearer`.
- Refresh token (D-14): one cookie per app: `__Secure-tz_rt_c` (customer), `__Secure-tz_rt_b` (business), `__Secure-tz_rt_a` (admin). Host-only on the API host (no `Domain`), `Path=/v1/auth`, `HttpOnly`, `Secure`, `SameSite=Strict`. Only `/v1/auth/*` ever receives it. Refresh is `POST /v1/auth/refresh` with `{app}`; the server reads only that app's cookie.
- Labels: **pub** = anonymous OK; **cus** = customer; **any** = any signed-in user; **biz(cashier)** = any staff with access to that store; **biz(manager)** = manager or owner; **biz(owner)** = owner only; **adm(roles)** = the listed admin roles. `super_admin` can do everything, so it is not repeated.
- Admin roles (D-64): `field_agent`, `support`, `catalogue`, `analyst`, `finance`, `admin`, `super_admin`. `admin` can do everything except credit adjustments, admin-user and role management, money settings (unit `paise`, `bp` or `credits`), payout hold, unhold, mark-paid and export, and pausing or running `money` jobs (D-95). `analyst` only reads Pulse, dashboards and jobs (A-21). "all but field_agent" in 2.7 means `support`, `catalogue`, `analyst`, `finance`, `admin`.
- Permission keys (e.g. `item.price.edit`, `stock.restock`, `stock.count`) and the full role matrix are in 02 (D-38). Policy code lives in `platform/authz/policy/<domain>.go` (D-50).
- Step-up: sensitive admin actions need a TOTP re-prompt within 10 min (02); marked **step-up** below.

### 1.4 Errors and problem codes (D-24)

- RFC 9457 problem JSON: `{ "type": "https://todayz.in/errors/insufficient_stock", "title": "…", "status": 409, "code": "insufficient_stock", "detail": "…", "requestId": "…", "fields": { "body.items[2].quantity": "max 3" } }`. `code` is stable and translated on the client; `title`/`detail` are English for logs. A `fields` key is the full path with its location prefix (`body.`, `query.`, `path.`, `header.`), exactly as the validator names it (D-100); `validation_failed` is 422, malformed JSON and an unreadable path are `400 bad_request` with a generic detail, the wrong media type is 415.
- One registry: `backend/api/problems/<domain>.yaml`, bundled. Each entry has the code, HTTP status and i18n key. W0-03 creates `problems/common.yaml`; each package adds codes in its own domain file. A response may only use a registered code.
- A refused state change is always `409 invalid_transition` (detail names the from and to states).
- Codes named in this file: `invalid_transition`, `idempotency_mismatch` (common); `insufficient_stock`, `store_closed`, `store_not_bookable`, `code_locked`, `account_has_active_order` (orders); `resync_required` (sync); `reversal_window_passed` (pos); `approver_is_requester` (admin-credits).
- Codes added after the QA book review (D-91). Each is registered in its domain file before its handler ships:

  | Code | Status | Domain file | Meaning |
  |---|---|---|---|
  | `otp_locked` | 429 | `auth.yaml` | too many wrong OTP attempts |
  | `otp_expired` | 401 | `auth.yaml` | OTP older than 5 min |
  | `account_blocked` | 403 | `auth.yaml` | account blocked by admin |
  | `step_up_required` | 403 | `auth.yaml` | admin TOTP older than 10 min |
  | `price_changed` | 409 | `orders.yaml` | price moved since the quote; the fresh quote is in `extensions` |
  | `below_minimum` | 409 | `orders.yaml` | under the store's delivery minimum |
  | `delivery_closed` | 409 | `orders.yaml` | delivery ordered outside delivery hours; `extensions.nextDeliveryWindow`; pickup still offered |
  | `ordering_suspended` | 409 | `orders.yaml` | no-show limit active; `extensions.until` |
  | `extension_already_used` | 409 | `orders.yaml` | the one "I'm on my way" extension is already used |
  | `below_credit_minimum` | 409 | `wallet.yaml` | eligible amount under the credit minimum |
  | `credit_max_exceeded` | 409 | `wallet.yaml` | more credits than `credit_max_per_order` |
  | `credit_monthly_cap` | 409 | `wallet.yaml` | customer's monthly credit-order cap reached |
  | `store_credit_cap` | 409 | `wallet.yaml` | store's daily credit-redemption cap reached |
  | `claim_expired` | 409 | `pos.yaml` | bill QR expired |
  | `claim_already_used` | 409 | `pos.yaml` | bill QR already claimed |
  | `review_edit_window_passed` | 409 | `reviews.yaml` | edit after `review_edit_window_days` |
  | `ai_daily_cap_reached` | 429 | `ai.yaml` | daily AI cap reached; `extensions.scope` is `customer`, `store` or `platform`; `Retry-After` is the next IST midnight |

  Codes added for the jobs platform (D-95), registered in `problems/admin-jobs.yaml` before the handlers ship:

  | Code | Status | Domain file | Meaning |
  |---|---|---|---|
  | `job_not_runnable` | 409 | `admin-jobs.yaml` | run-now, pause, resume or retry on a `timer` kind (retry of a `timer` kind's run), or run-now on an event-triggered kind |
  | `job_already_running` | 409 | `admin-jobs.yaml` | run-now or retry while that kind (or that period) is already running |
  | `job_paused` | 409 | `admin-jobs.yaml` | run-now or retry on a paused kind; resume it first |

  Codes added for Ask shops nearby and search proof (D-101), each registered in its domain file before its handler ships:

  | Code | Status | Domain file | Meaning |
  |---|---|---|---|
  | `ask_limit_reached` | 429 | `asks.yaml` | the customer has sent `ask_daily_limit_customer` asks this IST day; `Retry-After` is the next IST midnight |
  | `ask_already_open` | 409 | `asks.yaml` | the customer already has an open ask for this product (or query); `extensions.askId` |
  | `ask_no_shops` | 409 | `asks.yaml` | no store qualifies (radius, open now, not muted, under its daily limit) |
  | `ask_store_unavailable` | 409 | `asks.yaml` | "Ask this shop": that store is closed, muted, at its daily limit or blocked this customer. The body, `detail` and copy are the same for every reason; the reason is never sent (01 §11.2) |
  | `ask_closed` | 409 | `asks.yaml` | a reply after the store's reply window, or on a closed ask |
  | `price_above_mrp` | 422 | `asks.yaml` | a Yes price above the item's MRP (the master product's MRP for an unlisted item) |
  | `ask_reply_used` | 409 | `orders.yaml` | an order from an ask reply that already has an active order (any state but `cancelled`, `expired`, `rejected`); the unique index `ux_orders_ask_reply_active` (03) decides a race; `extensions.orderId` is the customer's own active order |
  | `ask_price_expired` | 409 | `orders.yaml` | the replied price ended between the quote and the order; the fresh quote at the current price is in `extensions`; never re-priced silently |
  | `share_card_unavailable` | 410 | `share.yaml` | share card create for an item whose store is not `live` or whose business is not `verified`, or a read of a card whose store is no longer `live` (D-40) |

  A second reply from the same store is `409 invalid_transition`.

  A credit cap refusal (CR-11 "refused and flagged") is the matching code plus a `risk_flags` row; the order can still be placed without credits.

### 1.5 Reason enums (D-60)

Reasons are schemas referenced by name; values are defined once in 01 and in `schemas/common.yaml`, never inline in a path file. The system reasons (`not_accepted`, `no_show`, `dispatch_timeout`, `partial_timeout`) appear only in responses and are never accepted in a request.

| Schema | Used by |
|---|---|
| `RejectReason` | shop reject |
| `DeliveryFailedReason` | fail-delivery |
| `ShopCancelReason` | shop cancel (`out_of_stock`, `customer_unreachable`, `closing_soon`, `other`; D-68) |
| `AdminCancelReason` | admin cancel |
| `CustomerCancelReason` | customer cancel (`declined_partial` is set by confirm-partial `{accept:false}`) |
| `ReviewTagPositive`, `ReviewTagNegative` | review create/edit |

### 1.6 Idempotency

`Idempotency-Key` header (ULID from the client) required on: order create, every order transition (customer, shop, admin), confirm-return, code verify, POS sale, POS reverse, sync, bill-claim confirm, counter-code verify, credit adjustment request and approve, payout mark-paid, job run-now (D-95), every AI draft confirm, ask create and ask reply (D-101). Same key + same body → same response; same key + different body → `422 idempotency_mismatch`.

These operations carry `x-money: true` in the contract, and apigen holds their operationIds as a list (`moneyOperations` in `backend/internal/devtools/apigen/check.go`, D-100 round 2): `createOrder`, `cancelOrder`, `confirmPartialOrder`, `acceptOrder`, `rejectOrder`, `readyOrder`, `dispatchOrder`, `failOrderDelivery`, `confirmOrderReturn`, `cancelStoreOrder`, `verifyOrderCode`, `cancelAdminOrder`, `syncStore`, `createPosSale`, `reversePosSale`, `confirmBillClaim`, `verifyCounterCode`, `createCreditAdjustment`, `approveCreditAdjustment`, `markPayoutPaid`, `runJob`, and the AI confirms `confirmVoiceStockDraft`, `confirmSupplierBillDraft`, `confirmShelfPhotoDraft`, `confirmReviewReplyDraft`, `confirmBannerCopyDraft`, `confirmListParseDraft`, `confirmCatalogueCleanupDraft`, `confirmTicketTriageDraft`, `confirmRiskSummaryDraft`. An operation with one of these ids and no `x-money` fails `apigen check`; a domain WP that names its operation differently renames it in both places in the same change. D-101 removed `confirmVoiceSearchDraft`: voice search and search assist are not drafts and move nothing (§2.8), so the list has 30 ids; ask create and ask reply are `x-idempotency: required` but not money (a reply's price becomes money only through the existing order operations).

How it works (W0-03, `platform/idempotency`):

- It is opt-in per route: a route declares `IdempotencyRequired` (a missing header is `400 idempotency_key_required`) or `IdempotencyOptional` in its route policy. Other routes ignore the header. It applies to POST, PUT, PATCH and DELETE only. A key that is not a ULID is `400 idempotency_key_invalid`.
- The contract is the primary source of the policy (D-100): every unsafe operation under `/v1/me`, `/v1/biz` or `/v1/admin` states `x-idempotency: none|optional|required`; an operation that moves money says `x-money: true` and must be `required`. The route's effective policy is the contract's merged with what the code declares, the stronger rule winning; code weaker than the contract fails startup. Idempotency runs after request validation, so a refused request is never stored or replayed.
- A record is scoped by principal + method and path + key, and kept 24 h with the response.
- "Same body" means the same raw bytes: the request hash covers the raw query string and the raw body bytes, so reordered JSON is a different request.
- The first request claims the key with a claim id and a 3-minute lease (longer than the 60 s write timeout, which the shutdown drain of 75 s also exceeds, D-98). A second request with the same key while the first runs gets `409 conflict` with `Retry-After: 1`. Only the holder of the claim id can complete or release the key.
- Replays repeat the stored status, body and an allow-list of headers (`Content-Type`, `Content-Language`, `Location`, `ETag`, `Cache-Control`, `Retry-After`) and add `Idempotent-Replayed: true`.
- Stored response bodies are encrypted with AES-256-GCM under `PII_ENC_KEY`.
- Never stored, and the key is released so a retry runs again: a 5xx, a body over 1 MB, a panic, and any response with `Cache-Control: no-store` (auth, OTP and token responses).
- If the response was sent but storing it fails, the key is held until its lease ends (releasing would let a retry run the request twice). The failure is logged at Error and counted in `tz_idempotency_complete_failures`; alert on that metric.

## 2. Endpoint groups

**Pkg** = the work package that owns the endpoint (07). The domain file is named in each heading.

### 2.1 Auth (`auth.yaml`, W1-01)

| Method | Path | Auth | Notes |
|---|---|---|---|
| POST | /v1/auth/otp/send | pub | `{phone, purpose, app, turnstileToken}` → `{requestId, resendAfter}`; never reveals whether the phone exists; the OTP is never logged (D-15) |
| POST | /v1/auth/otp/verify | pub | `{requestId, code, app, deviceId}` → access token + the app's refresh cookie; `{isNewUser}` |
| POST | /v1/auth/totp/enrol | adm | first admin login: secret QR + 10 recovery codes shown once |
| POST | /v1/auth/totp/verify | adm | `{code, purpose: login|step_up}`; second factor after OTP and step-up |
| POST | /v1/auth/totp/recover | adm | `{recoveryCode}`; single use (`admin_recovery_codes`) |
| POST | /v1/auth/refresh | cookie | `{app}`; rotates; reuse of an old token revokes the family |
| POST | /v1/auth/logout | cookie | current session of this app |
| POST | /v1/auth/logout-all | cookie | all sessions of this app |

### 2.2 Me: profile and account (`me.yaml`)

| Method | Path | Auth | Pkg | Notes |
|---|---|---|---|---|
| GET/PATCH | /v1/me | any | W1-01 | name, language; for customers also the active-order limit from no-show strikes (C-21) |
| DELETE | /v1/me | cus | W1-01 | `{otpRequestId, code}`; anonymise; `409 account_has_active_order` while any order is unfinished; forfeits the wallet (D-31); deletes the customer's asks with their replies and store blocks (W3-16's `OnAccountDelete` hook, D-101) |
| GET/POST | /v1/me/addresses | cus | W1-01 | |
| PATCH/DELETE | /v1/me/addresses/{addressId} | cus | W1-01 | |
| GET | /v1/me/stream | cus | W2-05 | SSE; active orders and open asks (`ask.updated` with `{askId, version}` on a reply or close, D-101) |
| GET | /v1/me/notifications | cus | W2-05 | inbox (C-20) |
| POST | /v1/me/notifications/read | cus | W2-05 | `{ids}` or `{all:true}` |
| GET/PATCH | /v1/me/notification-prefs | any | W2-05 | |
| POST | /v1/me/push-subscriptions | any | W2-05 | VAPID subscription |
| GET | /v1/me/counter-code-seed | cus | W2-04 | per-device TOTP secret for the offline counter code (once per device, encrypted at rest on device) |

### 2.3 Discover (`discover.yaml`, public)

| Method | Path | Pkg | Notes |
|---|---|---|---|
| GET | /v1/discover/places/search?q= | W1-06 | PIN / locality autocomplete |
| GET | /v1/discover/places/reverse?lat=&lng= | W1-06 | nearest locality label |
| GET | /v1/discover/summary?lat=&lng=&radius= | W1-06 | `{storesNearby, openNow, productsInRadius, pins:[{storeSlug, lat, lng, open, categoryTint}] or clusters}`: the StoreMap card. `productsInRadius` is the number of in-stock listings across live stores within the radius (not distinct catalogue products) |
| GET | /v1/discover/categories | W1-05 | tree with tints and icons |
| GET | /v1/discover/stores?lat=&lng=&radius=&category=&openNow=&delivery=&hasOffer= | W1-06 | shop list with distance, rating, cashbackPct, delivery flag, open state, `bookable` (false = "In-store only", D-41) |
| GET | /v1/discover/stores/{slug} | W1-06 | shop page (hours, rating, offers, cashback, delivery terms); a standee scan is recorded by `/v1/public/ref/{code}?s=` before the redirect here, not by this route (D-162, D-223) |
| GET | /v1/discover/stores/{slug}/items?q=&category=&cursor= | W1-06 | shop catalogue with stock + freshness (`confirmedAt`, D-101) |
| GET | /v1/discover/stores/{slug}/reviews | W2-06 | |
| GET | /v1/discover/stores/{slug}/substitutes?itemId=&lat=&lng=&radius= | W3-12 | AI-C3, rule-based, no model and no flag (D-235): `{sameShop:[item] (at most 3: same category, pack within `substitute_pack_tolerance_pct` in base units, same brand first, then closest pack, then price), nearby:[store with its cheapest item] (at most 3: the same product at other live shops of verified businesses in the radius, nearest first), serverNow}`. The item is a query parameter, not a path segment (1.2). `404 not_found` for a shop that is not live or an item it does not list. Not logged as a search |
| GET | /v1/discover/search?q=&lat=&lng=&radius=&category=&openNow=&delivery=&pickup=&brand=&maxPricePaise=&hasOffer=&sort=&parse= | W1-06 | results grouped by store; filters first (in stock, `live`, in radius), then the D-42 score; `sort=relevance` (default) `|nearest|cheapest`; `hasOffer` is the C-05 chip; logs to `search_log`. D-101 adds: `summary {storesChecked, storesWithIt, minPricePaise}` (absent when `storesWithIt` is 0); per result `confirmedAt` (from `stock_updated_at`, null when stale), `badge` (`lowest_price|closest`, at most one, on the first page only) and `deliveryFeePaise`; `parse=true` (default) runs the refine-chip parser on `q` and returns `chips [{kind: maxPrice|openNow|pickup|delivery|offers|nearest|brand, value, text}]` (applied as filters; `offers` sets `hasOffer`) and `query` (the rest of the text); removing a chip re-runs with the remaining chips as parameters and `parse=false`. `refineHint: true` when the synonym table had no match or the parser found no chip in a query of at least `refine_ai_min_words` words; the app may then call `POST /v1/me/ai/search-assist` (§2.8) after the results render. Search itself never calls a model (01 §11.1) |
| GET | /v1/discover/search/suggest?q= | W1-06 | product/brand/category suggestions |
| POST | /v1/discover/basket-search | W2-14 | public, no sign-in, rate-limited per IP like discover search (D-110). `{lines:[{productId?, text?, quantity}], lat, lng, radius?}` (`radius` in metres from discover search's chips, default 2000; lines as in a basket, at most `basket_max_items`, `422 validation_failed` on `body.lines` beyond, D-111) → `{summary:{storesChecked, storesWithAll}, results:[{storeId, storeSlug, storeName, locality, distanceM, open, bookable, coveredCount, lineCount, totalPaise, badge, covered:[{lineIndex, storeItemId, productId, name, variant, packSize, unit, pricePaise, mrpPaise, imageKey, quantity, lineTotalPaise, confirmedAt}], missing:[{lineIndex}]}], split:{stores:[{storeId, storeSlug, storeName, distanceM, bookable, lineIndexes, totalPaise, covered}], uncovered:[lineIndex]}|null, serverNow}`. At most `basket_results_max` results, ranked as 01 §11.4; `badge` is `has_everything|closest|lowest_price`, labels only. `split` appears only when no store covers every line and `basket_split_max_shops` is above 1. Read-only, never calls a model; logged as a search. The POST is for the body only: it changes nothing |
| GET | /v1/discover/offers?lat=&lng=&radius= | W2-07 | Offers near you: banners + top offers with fair rotation |

### 2.4 Customer orders, purchases, wallet, referrals, reviews, support (`/v1/me`, all cus)

| Method | Path | Pkg | Notes |
|---|---|---|---|
| POST | /v1/me/checkout/quote | W2-02 | `{storeId, items, fulfilment, addressId?, creditsToUse, askReplyId?}` (`askReplyId`, D-101: within its `priceValidUntil`, the line of that store item is priced at the replied price per unit with no item-level offer, `priceSource: ask_reply`, and the response carries `validUntil`; a Yes without a price gives the catalogue price, `priceSource: catalogue`. The line's quantity is at most the asked quantity (`422 validation_failed` on `items[i].quantity`). Through the `asks.Pricer` interface W3-16 registers. After `priceValidUntil` the line is at the catalogue price with no `validUntil`; `askReplyId` is ignored when the reply is not this customer's, not a Yes, or for another store; `409 store_not_bookable` for a reply from an in-store-only store; `409 ask_reply_used` when the reply already has an active order). Every line carries `priceSource` → per-line availability, offers applied, `itemsAfterOffersPaise` (eligible amount, D-02), delivery fee, credits allowed with a reason when 0 (under ₹100, a cap reached: D-30), total. No side effects. Order of operations (D-94): item-level offers, then the minimum-bill offer, then the delivery fee, then credits. The delivery minimum, the free-delivery threshold and the minimum-bill offer are compared inclusively (`>=`) with the item subtotal after item-level offers, before the minimum-bill discount and before credits. |
| POST | /v1/me/orders | W2-02 | idempotent; takes the quote's optional `askReplyId` and stores it as `ask_reply_id`, with `price_source` on the line (D-101); checks the replied price's validity at placement: `409 ask_price_expired` with the fresh quote in `extensions` when it ended after the quote, never a silent re-price; `409 ask_reply_used` when the reply already has an active order (the unique index decides a race: exactly one of two concurrent orders succeeds); re-prices, rechecks stock, holds credits, sets `accept_by`, enqueues expiry job, notifies store; `409 store_closed` outside hours or when closed manually; `409 store_not_bookable` when pickup and delivery are both off (D-41), and also when `feature_delivery_enabled` is off and the store has no pickup; `409 delivery_closed` for delivery outside delivery hours |
| GET | /v1/me/orders | W2-02 | own online orders |
| GET | /v1/me/orders/{orderId} | W2-02 | detail with timeline; includes `code`, decrypted from `code_enc` for the owner only, only while valid (D-04) |
| POST | /v1/me/orders/{orderId}/cancel | W2-02 | `{reason: CustomerCancelReason}` |
| POST | /v1/me/orders/{orderId}/confirm-partial | W2-02 | `{accept: bool}` from `awaiting_customer`; response shows the new totals, re-capped credits and recomputed delivery fee (D-28); `false` → cancelled `declined_partial` |
| POST | /v1/me/orders/{orderId}/extend | W2-02 | "+10 min on my way", once, pickup only |
| POST | /v1/me/orders/{orderId}/new-code | W2-02 | after `code_locked`; rotates code |
| GET | /v1/me/purchases | W2-04 | online orders and POS sales linked by `bill_qr` or `counter_code` in one list; sales linked by `phone_unverified` never appear (D-75). Backs the "In store" tab on C-13 Orders (D-45, D-75; owners per D-84) |
| GET | /v1/me/wallet | W2-01 | balance, held, expiring soon, cap info |
| GET | /v1/me/wallet/transactions | W2-01 | human-readable lines |
| GET | /v1/me/referrals | W2-03 | my code, link, stats (pending/rewarded) |
| POST | /v1/me/referrals/claim | W2-03 | `{code}` at sign-up (also auto from the attribution cookie; first touch, 7-day window: D-32) |
| POST | /v1/me/bill-claims/{token}/preview | W2-04 | scanned bill QR → store, amount, max credits |
| POST | /v1/me/bill-claims/{token}/confirm | W2-04 | `{creditsToUse}` → links sale (`bill_qr`), holds/captures credits |
| GET | /v1/me/reviews | W2-06 | my reviews, newest first (added by D-140) |
| POST | /v1/me/reviews | W2-06 | `{orderId|posSaleId, stars, text, tags: ReviewTagPositive[] | ReviewTagNegative[]}` |
| PATCH | /v1/me/reviews/{reviewId} | W2-06 | own review, within `review_edit_window_days`; `409 review_edit_window_passed` after that |
| POST | /v1/me/review-reports | W2-06 | `{reviewId, reason}`; report someone else's review |
| GET/POST | /v1/me/support/tickets | W2-09 | list / create (linked to an order) (D-52) |
| GET | /v1/me/support/tickets/{ticketId} | W2-09 | |
| GET/POST | /v1/me/support/tickets/{ticketId}/messages | W2-09 | |
| GET | /v1/me/asks?status=&cursor=&limit= | W3-16 | own asks, newest first, paginated (§1; `asks.yaml`, D-101) |
| POST | /v1/me/asks | W3-16 | idempotent; `{productId?, query?, quantity, needBy?, lat, lng, storeSlug?}` (exactly one of `productId`, `query`; `storeSlug` for "Ask this shop") → the ask with its recipients count. `429 ask_limit_reached`, `409 ask_already_open`, `409 ask_no_shops`, `409 ask_store_unavailable`. The point is used to pick stores and is not stored (03) |
| GET | /v1/me/asks/{askId} | W3-16 | progress `{asked, replied, bestPricePaise}` and the Yes replies `[{askReplyId, storeSlug, storeName, distanceM, pricePaise?, readyBy?, repliedAt, priceValidUntil, storeItemId?}]`; No replies are counted only |
| POST | /v1/me/asks/{askId}/close | W3-16 | the customer closes an open ask; `409 invalid_transition` when already closed |
| GET | /v1/me/baskets | W2-14 | own saved baskets, `{baskets:[{basketId, name, lineCount, updatedAt, version}]}`, newest first (`baskets.yaml`, D-110); soft-deleted ones never appear |
| POST | /v1/me/baskets | W2-14 | idempotent (`x-idempotency: optional`); `{name, lines:[{productId?, text?, quantity}]}` (exactly one of `productId`, `text` per line; quantity 1–99; name 1–40 characters) → the basket. `422 validation_failed` with the field (`name`, `lines`) when `basket_max_items` is exceeded or a line is invalid; `422 basket_limit_reached` when the customer already has `basket_max_saved` baskets (D-110) |
| GET | /v1/me/baskets/{basketId} | W2-14 | `{basketId, name, lines:[{lineId, productId?, productName?, text?, quantity, position}], lastSplit?, updatedAt, version}`; `ETag: "v{version}"`; another customer's or a deleted basket is `404` |
| PATCH | /v1/me/baskets/{basketId} | W2-14 | `If-Match`; `{name?, lines?}`; `lines`, when sent, replaces the whole set in one transaction; same limits and errors as create; `412` on a stale version (as the other PATCH routes); `404` for another customer's basket |
| DELETE | /v1/me/baskets/{basketId} | W2-14 | soft delete (03 `baskets.deleted_at`); `204`; `404` for another customer's basket |

### 2.5 Public (`public.yaml`, pub)

| Method | Path | Pkg | Notes |
|---|---|---|---|
| POST | /v1/public/leads | W1-04 | site lead form (Turnstile) (D-46) |
| GET | /v1/public/receipts/{token} | W2-04 | 128-bit token, expires after 30 days; items, totals, store; no customer data. Screen C-24, route `/r/{token}` (D-45) |
| GET | /v1/public/ref/{code} | W2-03 | referral click → 302 to the app with the attribution cookie |
| POST | /v1/public/share-cards | W3-16 | `{storeItemId}` → `{code, url, text, imageUrl}`; get-or-create, one card per store item; renders the preview image when missing or when the price changed; only for a `live` store of a `verified` business (D-40), else `410 share_card_unavailable`; at most `share_card_create_per_hour` new cards per IP or `anon_id` (`429`; returning an existing card does not count) (`share.yaml`, D-101) |
| GET | /v1/public/share-cards/{code} | W3-16 | `{storeItemId, itemName, storeName, locality, pricePaise, imageUrl}` for the link preview; never a distance; `410 share_card_unavailable` once the store is not `live` or its business not `verified` (`share.yaml`, D-101) |
| GET | /v1/public/site-marquee?city= | W3-16 | `city` is a `places` city slug; the latest approved list `{items:[{productName}]}`, or `{fallback:true}` when the city has none; cached 1 h (D-101) |

### 2.6 Business (`{B}` = `/v1/biz/businesses/{bid}`, `{S}` = `{B}/stores/{sid}`)

Cashier rights are D-06 as amended by D-65: cashiers run orders day to day (accept, reject, partial accept, ready, dispatch, delivered, delivery failed, hand over by code) and sell at the POS with automatic round-off. Manager and above only: cancelling an accepted order, POS reverse, manual discounts, recording damage. Anything not listed for a cashier needs manager or above. Voice drafts are in 2.8.

**Business, stores, staff, KYC (`business.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| GET | /v1/biz/businesses | any | W1-04 | businesses and stores I am staff of, with my role (B-01 picker) |
| POST | /v1/biz/businesses | any | W1-04 | onboarding draft; caller becomes owner |
| GET/PATCH | {B} | manager | W1-04 | there is no business-delete endpoint in v1; an owner asks support (D-66) |
| POST | {B}/submit | owner | W1-04 | to KYC verification (`businesses.status`) |
| GET/PUT | {B}/kyc | owner (GET also manager, masked) | W1-04 | PAN, GSTIN, UPI, bank; GSTIN and UPI encrypted, shown masked (last 4) (D-44) |
| GET/POST | {B}/documents | owner | W1-04 | `business_documents`: kind `pan|gst|shop_photo|storefront|license|other`; POST returns a presigned upload (D-46) |
| GET | {B}/staff | manager | W1-04 | all staff with role, store access, delivery flag |
| POST | {B}/staff | owner; manager for cashiers only | W1-04 | invite by phone, role, store access, delivery flag. A manager can invite only `cashier` (D-66) |
| PATCH | {B}/staff/{staffId} | owner | W1-04 | role, store access, delivery flag; the owner manages all staff (D-66) |
| DELETE | {B}/staff/{staffId} | owner; manager for cashiers only | W1-04 | remove; a manager can remove only a cashier, anything else is `403` (D-66) |
| GET/POST | {B}/stores | manager | W1-04 | |
| GET/PATCH | {S} | manager | W1-04 | |
| POST | {S}/submit | manager | W1-04 | new store → `pending_review` (D-40) |
| GET/PATCH | {S}/settings | manager | W1-04 | hours, holidays, manual close, `acceptsPickup`, `acceptsDelivery` (D-41), delivery terms, cashback, auto-accept, notification prefs, `asksMuted` (`asks.manage`, audited, D-101) |
| GET | {B}/inbox | cashier | W2-05 | B-16 (D-46) |
| POST | {B}/inbox/read | cashier | W2-05 | `{ids}` or `{all:true}` |
| GET/POST | {B}/support/tickets | manager | W3-08 | B-15 |
| GET/POST | {B}/support/tickets/{ticketId}/messages | manager | W3-08 | |

**Products, items, stock (`catalogue.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| POST | {B}/products | manager | W1-05 | create a missing master product (pending review) (D-37) |
| GET | {B}/products/search?q=&barcode= | cashier | W1-05 | master catalogue (D-37) |
| GET | {S}/items | cashier | W1-05 | |
| POST | {S}/items | manager | W1-05 | list a product with price; opening stock writes an `opening` movement |
| GET | {S}/items/{itemId} | cashier | W1-05 | |
| PATCH | {S}/items/{itemId} | manager | W1-05 | price and listing need `item.price.edit` |
| POST | {S}/items/{itemId}/movements | cashier / manager | W1-05 | `{kind, quantity, note, batchIds?}`: `count_correction` needs `stock.count` (cashier+); `restock` needs `stock.restock`, `damage` and `expired` need manager+ (D-06). Negative movements use up batches by 03 stock rule 8 (D-99): a sale (`sale_pos`, `sale_online`) takes unexpired dated batches first (earliest expiry first), then undated batches, then stock in no batch, and expired batches last; `damage` and a downward `count_correction` take the earliest expiry first. For `expired`, `batchIds` (optional) names the batches being written off instead: one `expired` movement per batch, each for min(the batch's remaining, available left), so the total never exceeds available, and a batch reaches 0 only if all of it was written off (03 stock rule 6; the "Mark expired" action of the insights, D-99). Other sources (D-01) are written only by their own flows |
| GET | {S}/items/{itemId}/history | cashier | W1-05 | movements |
| GET | {S}/batches | manager | W1-05 | expiry batches with remaining quantity, expiry and cost |
| PATCH | {S}/batches/{batchId} | manager | W1-05 | `{costPaise?, expiresOn?}` and nothing else (`batches.manage`, manager and above; D-99). Corrects what was entered at stock in; `quantity` and `received_quantity` cannot be changed here (that is a stock movement). Audited in `audit_log` with before and after. It re-runs nothing: the next read of insights uses the new cost and expiry. There is no `POST {S}/batches`: batches are created only by stock in and by a positive count (03 stock rule 8) |
| POST | {S}/item-imports | manager | W1-05 | CSV/XLSX upload → validated preview with row errors |
| POST | {S}/item-imports/{importId}/apply | manager | W1-05 | writes `import` movements |
| GET | {S}/catalogue/snapshot | cashier | W1-05 | full snapshot for the offline POS, with the current `catalogue_changes` cursor |
| GET | {S}/catalogue/changes?cursor= | cashier | W1-05 | deltas from `catalogue_changes`; cursor older than 30 days → `410 resync_required` (D-36) |

**Orders (`orders.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| GET | {S}/orders?status=&cursor= | cashier | W2-02 | customer phone masked except while `out_for_delivery` (D-60) |
| GET | {S}/orders/{orderId} | cashier | W2-02 | never contains the order code (D-04) |
| POST | {S}/orders/{orderId}/accept | cashier | W2-02 | `{lines:[{orderItemId, quantity}]}` full or partial (partial → `awaiting_customer`); creates reservations, no movement (D-01); `409 insufficient_stock` when any accepted qty exceeds available |
| POST | {S}/orders/{orderId}/reject | cashier | W2-02 | `{reason: RejectReason}` |
| POST | {S}/orders/{orderId}/ready | cashier | W2-02 | pickup only, optional |
| POST | {S}/orders/{orderId}/dispatch | cashier | W2-02 | delivery: `accepted` → `out_for_delivery` |
| POST | {S}/orders/{orderId}/fail-delivery | cashier | W2-02 | `{reason: DeliveryFailedReason}`; only from `out_for_delivery` (D-22); reservations stay held until the return is settled (D-69) |
| POST | {S}/orders/{orderId}/confirm-return | cashier (`returned`) / manager (`damaged`) | W2-02 | `{outcome: returned|damaged}` on a `delivery_failed` order. `returned` releases the reservations; `damaged` writes `damage` movements, then releases (manager+; a cashier asks a manager, D-69). Unsettled orders get inbox reminders to the store at 2 h and 24 h; at 72 h the system settles them as `returnOutcome: unconfirmed` and raises a risk flag (D-69). `409 invalid_transition` once settled |
| POST | {S}/orders/{orderId}/cancel | manager | W2-02 | `{reason: ShopCancelReason, note?}` (D-68); manager and above only (D-65); no strike |
| POST | {S}/orders/{orderId}/verify-code | cashier | W2-02 | hand over: `{code}` → HMAC lookup on `code_hmac` (D-04); 5 tries then `code_locked`; marks collected/delivered (writes `sale_online`, D-01), records payment method |
| GET | {S}/stream | cashier | W2-05 | SSE; also `ask.created` and `ask.closed` for the store's asks (D-101) |

**POS and sync (`pos.yaml`, `sync.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| POST | {S}/sync | cashier | W2-04 | offline outbox (sales, movements) in device seq order; returns server deltas. An offline counter-code link carries the typed code, device time and the clock offset at the last online check; verified per D-12, else the sale stays unlinked ("Customer link failed") |
| GET | {S}/pos/sales | cashier | W2-16 | the store's sales from all devices, newest first by sale time, `limit`/`cursor` keyset; each with `link.status` and, for a failed offline counter code, `link.failure` (`code_used|code_wrong|clock_off|too_late|locked|unverified`, D-12, D-221); no phone, no receipt token. Backs B-05 "Store sales" |
| POST | {S}/pos/sales | cashier | W2-04 | online sale; `link.method: bill_qr|counter_code|phone_unverified` (D-07); manual line or bill discounts need manager+ (D-06); round-off to the nearest ₹1 is automatic, half rounds up (₹100.50 becomes ₹101, `round_off_paise` −49…+50; D-94), and allowed for cashiers (D-65); response has `receiptToken` (D-45) |
| POST | {S}/pos/sales/{saleId}/reverse | manager | W2-04 | within `pos_reversal_window_hours` else `409 reversal_window_passed`; `pos_reversal` movement + `reversal` ledger entry (D-08) |
| POST | {S}/bill-claims | cashier | W2-04 | create bill QR token for a sale/draft |
| GET | {S}/bill-claims/{claimId} | cashier | W2-04 | status (also via SSE) |
| POST | {S}/counter-code/verify | cashier | W2-04 | `{phone, code, saleClientId}` |

**Offers, banners, reviews, insights, coach (`offers.yaml`, `banners.yaml`, `reviews.yaml`, `insights.yaml`, `coach.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| GET/POST | {S}/offers | manager | W2-07 | all offer kinds |
| GET/PATCH | {S}/offers/{offerId} | manager | W2-07 | pause/end via `status` |
| POST | {S}/offer-previews | manager | W2-07 | how an unsaved offer looks; prices a sample cart |
| GET/POST | {S}/banners | manager | W2-07 | image upload or template; uploads pass the AI image check (AI-R10), unsure → A-20 queue |
| GET/PATCH | {S}/banners/{bannerId} | manager | W2-07 | pause/end via `status` |
| GET | {S}/reviews | manager | W2-06 | |
| POST/PATCH | {S}/reviews/{reviewId}/reply | manager | W2-06 | one reply, editable |
| POST | {S}/reviews/{reviewId}/report | manager | W2-06 | |
| GET | {S}/overview?range= | manager | W3-03 | KPI strip, sales by hour, top items (D-46) |
| GET | {S}/insights?all=&kind= | manager | W3-03 | Demand insights (D-99). Default: the B-03 card, top 4 firing rows by severity, one per kind; kinds that do not fire, including kinds below their minimum evidence, are not returned. `all=1`: every firing row, grouped by kind, plus one `needs_data` entry for each kind that has no row and is below its minimum evidence, for the B-12 Insights tab, with the usual `limit` and `cursor` (the same endpoint, no second path). `kind=` limits to one kind. Shape below |
| POST | {S}/insights/dismiss | manager | W3-03 | "Not useful" on a row: `{kind, subjectRef}`; writes `insight_feedback`; that subject, not the whole kind, stays quiet for that shop for `coach_dismiss_quiet_days` (D-99). `subjectRef` is validated per kind (see the response table): a wrong shape, an item, batch or pair that does not belong to this store, or `store` on a kind that is not store-wide returns `422 validation_failed`; `expired_on_shelf` is refused with `422 validation_failed` too, because it cannot be dismissed. Response `200 { quietUntil }`. A repeat call while the subject is still quiet keeps the existing `quiet_until`. "Idempotent" here means a natural upsert on `(store_id, kind, subject_ref)`; the call takes no `Idempotency-Key` |
| GET | {S}/analytics?range=&metric= | manager | W3-03 | |
| GET | {S}/customers?segment=&cursor= | manager | W3-03 | repeat customers (masked) (D-46). `segment=slipping` returns the regulars behind the "Regulars slipping away" insight (D-99); phones masked as in B-09 (R-16.2) |
| GET | {S}/coach/cards?status= | manager | W3-11 | coach feed (D-48) |
| POST | {S}/coach/cards/{cardId}/done | manager | W3-11 | |
| POST | {S}/coach/cards/{cardId}/dismiss | manager | W3-11 | "Not useful" |

**Demand insights response (D-99).** `GET {S}/insights` returns `{ items: [Insight], nextCursor, period: { days, label: "Last 7 days" } }`. Each `Insight`:

| Field | Meaning |
|---|---|
| `kind` | `selling_fast`, `sitting_on_shelf`, `going_to_waste`, `wanted_nearby`, `missed_sales`, `expired_on_shelf`, `selling_below_cost`, `stock_check_due`, `bought_together`, `regulars_slipping`, `priced_above_nearby` |
| `subjectRef` | Stable id of what the row is about, used by `dismiss`: a `store_item_id` (`selling_fast`, `sitting_on_shelf`, `missed_sales`, `selling_below_cost`, `priced_above_nearby`); the master product id, or `q:` plus the normalised query when no product matches (`wanted_nearby`); a batch id (`going_to_waste`: the earliest-expiring batch in the row; `expired_on_shelf`: its batch, which cannot be dismissed); an item pair `a:b` with item A first (`bought_together`); or `store` (`stock_check_due`, `regulars_slipping`) |
| `severity` | `high`, `medium` or `low`. `high`: `expired_on_shelf`, `going_to_waste`, `selling_below_cost`, and `selling_fast` when the run-out is within 2 days. `medium`: the other `selling_fast`, `missed_sales`, `stock_check_due`, `regulars_slipping`. `low`: `sitting_on_shelf`, `wanted_nearby`, `bought_together`, `priced_above_nearby`. Inside a severity, kinds go in the kind order of 01 §12; rows of the same kind go by the money or units at stake, larger first |
| `evidence` | `{ state: "ready" \| "needs_data", needs? }`. `needs_data` entries come only with `all=1` and carry the localisation key and parameters of the "what data it needs" text and no `subjectRef`, no `facts` and no `action`; such a kind never shows a number. `ready` rows are the ones that fire |
| `facts` | The computed numbers the sentence quotes (for example `soldQty`, `runOutOn`, `searches`, `radiusM`, `noShopNearby: boolean` (true only with at least 10 other live verified stores of distinct businesses in the radius and none stocking it), `expiringCount`, `names[]`, `valueAtCostPaise?`, `staleCount`, `staleAfterHours`, `billsA`, `billsBoth`). Never a competing shop count, another shop's name or a customer's phone |
| `action` | `{ type, payload }` with `type` one of `restock`, `create_offer`, `flash_offer`, `add_product`, `mark_expired`, `edit_price`, `check_cost`, `count_stock`, `see_customers`. `payload` carries the prefill (the query parameters the screens read are defined once in 06 "Insight deep links"): for `restock` the item and the quantity `ceil(sold × insight_cover_days ÷ insight_period_days) − available`, never below 0; for `create_offer` the item and a % inside the safe limits (AI-R4 may adjust the % and the wording only); for `flash_offer` the batch ids; for `add_product` the master product id or the search text; for `mark_expired` the item and the batch ids, earliest expiry first, each at min(batch remaining, available left) so the total stays within available; for `edit_price` the item and the suggested reference price (the cost rounded up to a whole rupee, never above the MRP); for `check_cost` the item and the batch id (used when the cost is at or above the MRP: the row reads "Check the cost entry for {item}"); it opens the edit form for that batch, which saves with `PATCH {S}/batches/{batchId}`; for `count_stock` the item ids; for `see_customers` the `segment` filter. The client opens the target screen with it. The API applies nothing |
| `text` | AI-R4 sentence when on and valid (else null; the client renders the template from `facts`) |

Priced above nearby is returned only while `insight_price_compare_enabled` is true. Staff below manager get 403.

**Asks (`asks.yaml`, W3-16, D-101)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| GET | {S}/asks?status=&cursor=&limit= | cashier | W3-16 | `asks.view`; the Asks inbox (B-20), paginated (§1): open asks first, by `replyBy`. Each: `{askId, itemName, productId?, storeItemId?, quantity, needBy?, distanceBand, replyBy, available?, listedPricePaise?, mrpPaise?}`; never a name, phone, address or exact distance |
| POST | {S}/asks/{askId}/reply | cashier | W3-16 | idempotent; `asks.reply`; `{answer: yes|no, pricePaise?, readyBy?}`. `readyBy` is today (IST), or up to the end of tomorrow when the ask's `needBy` is `tomorrow`; later is `422 validation_failed`. A price other than the listed price needs `item.price.edit` (`403` otherwise); on an unlisted item only `item.price.edit` may send a price. `422 price_above_mrp`, `409 ask_closed`, `409 invalid_transition` (already answered). A Yes on a listed item moves `stock_updated_at` with no movement |
| POST | {S}/asks/{askId}/report | cashier | W3-16 | `asks.reply`; `{reason}`; raises a `risk_flags` row (rule `ask_reported`) for the customer and stops asks from that customer to this store; allowed while the ask is kept (`ask_retention_days`), open or closed; `409 invalid_transition` when this store already reported it |

**Credits, statements, referrals, standees (`business-credits.yaml`, `referrals.yaml`, `standee.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| GET | {B}/credit-account | manager | W2-01 | balance, lines |
| GET | {B}/statements | manager | W3-01 | monthly statements (D-51) |
| GET | {B}/statements/{period} | manager | W3-01 | detail; `Accept: text/csv` for CSV |
| GET | {B}/referrals | manager | W2-03 | business referral code (shop-to-shop), stats (D-26) |
| GET | {S}/qr-standee.pdf?standeeId= | manager | W2-10 | printable A5 standee; URL carries the store short code and `?s={standeeId}` (D-26, D-52) |
| GET | {S}/standees | manager | W3-09 | `listStandees`: standee rows (no code of their own), newest first, each with its link and `counts {scans, signUps, qualified}`, plus `totals` and `limit` (D-51, D-223) |
| POST | {S}/standees | manager | W3-09 | `createStandee`, idempotent; `{campaign}` (1–40 chars); audited; `409 standee_limit_reached` at the cap (D-223) |
| POST | {S}/standees/{standeeId}/reorder | manager | W3-09 | `requestStandeeReorder`, idempotent; asks for a reprint (`reorderRequestedAt`); a request already waiting is answered unchanged; audited (D-223) |

### 2.7 Admin (`/v1/admin`, `admin-*.yaml`)

| Group | Method | Path | Roles | Pkg | Notes |
|---|---|---|---|---|---|
| Pulse | GET | /v1/admin/pulse?range= | all but field_agent | W3-07 | GMV, orders, funnel, active stores, credits, cohorts |
| Pulse | GET | /v1/admin/pulse/map | all but field_agent | W3-07 | read-only for every role (D-64) |
| Businesses | GET | /v1/admin/businesses | support, admin | W3-05 | queue tabs, filters |
| Businesses | GET | /v1/admin/businesses/{bid} | support, admin | W3-05 | KYC masked |
| Businesses | POST | /v1/admin/businesses/{bid}/verify | admin | W3-05 | KYC verification is `admin` and `super_admin` only (D-64) |
| Businesses | POST | /v1/admin/businesses/{bid}/request-info | admin | W3-05 | |
| Businesses | POST | /v1/admin/businesses/{bid}/reject | admin | W3-05 | |
| Businesses | POST | /v1/admin/businesses/{bid}/suspend | admin | W3-05 | |
| Businesses | POST | /v1/admin/businesses/{bid}/reinstate | admin | W3-05 | |
| Businesses | POST | /v1/admin/businesses/{bid}/kyc/reveal | admin | W3-05 | `{field}`; audited |
| Businesses | POST | /v1/admin/businesses/{bid}/kyc/review | admin | W3-05 | `{field: pan|gstin|upi|bank, decision}` |
| Businesses | GET | /v1/admin/businesses/{bid}/documents | admin | W3-05 | |
| Businesses | POST | /v1/admin/businesses/{bid}/documents/{docId}/review | admin | W3-05 | `{decision}`; sets status and reviewer |
| Field | GET/POST | /v1/admin/field/businesses | field_agent, admin | W3-06 | mine / create-for-shop (A-04); sets `onboarded_by`. A field agent sees only businesses they onboarded; `admin` sees all |
| Field | GET | /v1/admin/field/me/stats | field_agent | W3-06 | my onboarding stats: businesses onboarded by status, stores live, items added, in-window count (D-64) |
| Field | GET/PATCH | /v1/admin/field/businesses/{bid} | field_agent, admin | W3-06 | a field agent: only businesses they onboarded |
| Field | POST | /v1/admin/field/businesses/{bid}/owner-confirm | field_agent, admin | W3-06 | sends the owner OTP |
| Field | POST | /v1/admin/field/businesses/{bid}/owner-confirm/verify | field_agent, admin | W3-06 | `{code}` from the owner's phone |
| Field | POST | /v1/admin/field/businesses/{bid}/documents | field_agent, admin | W3-06 | photos; presigned upload |
| Field | POST | /v1/admin/field/businesses/{bid}/stores | field_agent, admin | W3-06 | |
| Field | GET/POST | /v1/admin/field/businesses/{bid}/stores/{sid}/items | field_agent, admin | W3-06 | until `field_agent_edit_window_days` after verification (D-39) |
| Field | PATCH | /v1/admin/field/businesses/{bid}/stores/{sid}/items/{itemId} | field_agent, admin | W3-06 | same window |
| Field agents | GET | /v1/admin/field-agents | admin | W3-06 | per-agent stats (A-15) |
| Stores | GET | /v1/admin/stores | support, admin | W3-05 | |
| Stores | GET/PATCH | /v1/admin/stores/{sid} | admin (GET also support) | W3-05 | edit pin/hours |
| Stores | POST | /v1/admin/stores/{sid}/review | admin | W3-05 | `pending_review` → `live` or back with a note (D-40) |
| Stores | POST | /v1/admin/stores/{sid}/force-close | admin | W3-05 | |
| Stores | POST | /v1/admin/stores/{sid}/strikes/reset | support, admin | W3-05 | `{reason}`; audited (D-41) |
| Customers | POST | /v1/admin/customer-search | support, admin | W3-05 | `{phone}`, exact match on the keyed hash; a POST so the phone never sits in a URL or an access log (W3-05 review). Its own resource by the 1.2 path rule, like `basket-search` |
| Customers | GET | /v1/admin/customers/{customerId} | support, admin | W3-05 | |
| Customers | POST | /v1/admin/customers/{customerId}/block | admin | W3-05 | customer accounts only: an admin or a staff-only account is `409` |
| Customers | POST | /v1/admin/customers/{customerId}/unblock | admin | W3-05 | |
| Customers | POST | /v1/admin/customers/{customerId}/strikes/reset | support, admin | W3-05 | `{reason}`; audited (D-41) |
| Customers | DELETE | /v1/admin/customers/{customerId}/sessions/{sessionId} | admin | W3-05 | revoke a device session |
| Orders | GET | /v1/admin/orders | support, admin | W3-05 | by number (`TZ-024817`), store, customer, status, date |
| Orders | GET | /v1/admin/orders/{orderId} | support, admin | W3-05 | timeline, code attempts, holds, notifications |
| Orders | POST | /v1/admin/orders/{orderId}/cancel | support, admin | W3-05 | `{reason: AdminCancelReason, strike: none|customer|store}` from any non-terminal state; releases reservations and held credits; audited (D-20) |
| Catalogue | GET/POST | /v1/admin/products | admin (GET also catalogue) | W3-05 | `catalogue` reads products to pick a merge target |
| Catalogue | GET/PATCH | /v1/admin/products/{productId} | admin (GET also catalogue) | W3-05 | |
| Catalogue | GET | /v1/admin/pending-products | catalogue, admin | W3-05 | master catalogue review queue with duplicate suggestions (D-64) |
| Catalogue | POST | /v1/admin/pending-products/{pendingId}/approve | catalogue, admin | W3-05 | |
| Catalogue | POST | /v1/admin/pending-products/{pendingId}/merge | catalogue, admin | W3-05 | `{intoProductId}` |
| Catalogue | POST | /v1/admin/pending-products/{pendingId}/reject | catalogue, admin | W3-05 | |
| Catalogue | POST | /v1/admin/product-imports | admin | W3-05 | preview |
| Catalogue | POST | /v1/admin/product-imports/{importId}/apply | admin | W3-05 | |
| Catalogue | GET/POST | /v1/admin/categories | admin | W3-05 | |
| Catalogue | PATCH | /v1/admin/categories/{categoryId} | admin | W3-05 | |
| Catalogue | GET/POST | /v1/admin/synonyms | admin | W3-05 | |
| Catalogue | DELETE | /v1/admin/synonyms/{synonymId} | admin | W3-05 | |
| Banners | GET | /v1/admin/banners?moderation= | support, admin | W3-05 | A-20 queue: image check unsure, reported (D-46); banner moderation is support and above (D-64) |
| Banners | POST | /v1/admin/banners/{bannerId}/approve | support, admin | W3-05 | |
| Banners | POST | /v1/admin/banners/{bannerId}/takedown | support, admin | W3-05 | `{reason}`; audited |
| Reviews | GET | /v1/admin/reviews | support, admin | W2-06 (console: W3-05) | |
| Reviews | POST | /v1/admin/reviews/{reviewId}/hide | support, admin | W2-06 (console: W3-05) | `{reason}`; audited |
| Reviews | POST | /v1/admin/reviews/{reviewId}/restore | support, admin | W2-06 (console: W3-05) | `{reason}`; audited (D-41) |
| Reviews | POST | /v1/admin/reviews/{reviewId}/reply/hide, .../reply/restore | support, admin | W2-06 (console: W3-05) | `{reason}`; audited (D-140) |
| Reviews | GET | /v1/admin/review-reports | support, admin | W2-06 (console: W3-05) | |
| Reviews | POST | /v1/admin/review-reports/{reportId}/resolve | support, admin | W2-06 (console: W3-05) | |
| Credits | GET | /v1/admin/ledger/accounts?q= | finance, admin | W3-01 | |
| Credits | GET | /v1/admin/ledger/accounts/{accountId}/entries | finance, admin | W3-01 | |
| Credits | GET | /v1/admin/credits/expiry-preview | finance, admin | W3-01 | |
| Credits | GET/POST | /v1/admin/credit-adjustments | finance | W3-01 | POST = request, step-up, idempotent: while the admin's IST-day total for the customer (D-237) stays at or under `admin_adjust_dual_approval_above_paise` it posts at once; above it creates a pending `admin_adjustment_requests` row, for super_admin too (D-05). `admin` cannot adjust |
| Credits | POST | /v1/admin/credit-adjustments/{requestId}/approve | finance | W3-01 | step-up, idempotent; approver ≠ requester else `403 approver_is_requester` |
| Credits | POST | /v1/admin/credit-adjustments/{requestId}/reject | finance | W3-01 | approver ≠ requester |
| Payouts | GET | /v1/admin/statements?period= | finance, admin | W3-01 | |
| Payouts | GET | /v1/admin/payouts?period=&status= | finance, admin | W3-01 | at most one open payout per business (D-09) |
| Payouts | GET | /v1/admin/payouts/{payoutId} | finance, admin | W3-01 | with the business's bank details masked (last 4) (D-64) |
| Payouts | POST | /v1/admin/payouts/{payoutId}/hold | finance | W3-01 | `{reason}`; `admin` cannot (D-64) |
| Payouts | POST | /v1/admin/payouts/{payoutId}/unhold | finance | W3-01 | `admin` cannot (D-64) |
| Payouts | POST | /v1/admin/payouts/{payoutId}/mark-paid | finance | W3-01 | `{utr}`; step-up, idempotent; `admin` cannot (D-64) |
| Payouts | GET | /v1/admin/payout-exports?period= | finance | W3-01 | CSV for the bank/UPI batch; step-up (D-85); `admin` cannot (D-64). The CSV holds `pending` payouts only; held and paid rows are left out. Columns: `payout_id, period, business_id, legal_name, beneficiary_name, amount_inr` (2 decimals), `upi_id, bank_account_number, ifsc`. Exporting stamps `exported_at` on the rows. Cells are sanitised against formula injection (D-93) |
| Leads | GET | /v1/admin/leads?status= | field_agent, support, admin | W3-05 | site leads for the "Leads" tab on A-03 |
| Leads | POST | /v1/admin/leads/{leadId}/status | field_agent, support, admin | W3-05 | `{status: new|contacted|closed}`; audited |
| Referrals | GET | /v1/admin/referrals | admin | W3-02 | `admin-referrals.yaml` (D-51) |
| Referrals | POST | /v1/admin/referrals/{referralId}/reject | admin | W3-02 | `{reason}`; reverses pending |
| Risk | GET | /v1/admin/risk/flags | support, admin | W3-02 | `admin-risk.yaml` (D-51) |
| Risk | GET | /v1/admin/risk/flags/{flagId} | support, admin | W3-02 | evidence |
| Risk | POST | /v1/admin/risk/flags/{flagId}/clear | admin | W3-02 | |
| Risk | POST | /v1/admin/risk/flags/{flagId}/confirm | admin | W3-02 | can block/suspend |
| Demand | GET | /v1/admin/demand-gaps?city=&days= | field_agent, catalogue, analyst, admin | W3-16 | `admin-demand.yaml` (D-101): products asked by at least `ask_gap_min_customers` distinct customers in the city in `ask_gap_window_days` with no Yes; product, category, count, last asked; never a customer |
| Demand | GET | /v1/admin/site-marquee?city= | catalogue, admin | W3-16 | the proposed list of the latest build and the approved list in use (D-101) |
| Demand | PUT | /v1/admin/site-marquee/{cityId} | catalogue, admin | W3-16 | `{builtOn, decision: approve|reject, productIds}`: approve the listed products of that build (a subset of the proposal) or reject the build; audited (D-101) |
| Support | GET | /v1/admin/tickets | support, admin | W3-08 | A-14 (D-47, D-52) |
| Support | GET | /v1/admin/tickets/{ticketId} | support, admin | W3-08 | |
| Support | POST | /v1/admin/tickets/{ticketId}/assign | support, admin | W3-08 | |
| Support | POST | /v1/admin/tickets/{ticketId}/reply | support, admin | W3-08 | |
| Support | POST | /v1/admin/tickets/{ticketId}/notes | support, admin | W3-08 | internal |
| Support | POST | /v1/admin/tickets/{ticketId}/close | support, admin | W3-08 | |
| Notifications | GET | /v1/admin/notification-templates | admin | W3-05 | per kind × language |
| Notifications | PATCH | /v1/admin/notification-templates/{templateId} | admin | W3-05 | |
| Notifications | POST | /v1/admin/notification-templates/{templateId}/test-send | admin | W3-05 | to own phone |
| Notifications | GET | /v1/admin/integrations | admin | W3-05 | on/off flag and key `set`/`missing`, never values (D-19) |
| Settings | GET | /v1/admin/settings | admin | W3-05 | with unit, min/max (D-03) |
| Settings | PATCH | /v1/admin/settings/{key} | admin | W3-05 | `{value, reason}`; step-up; keys are the 01 §17 names (D-61); money settings (unit `paise`, `bp` or `credits`) are super_admin only (D-64) |
| Settings | GET | /v1/admin/settings/{key}/history | admin | W3-05 | |
| Admin users | GET/POST | /v1/admin/users | super_admin | W3-05 | invite with one or more of the 7 roles (D-64) |
| Admin users | PATCH | /v1/admin/users/{adminId} | super_admin | W3-05 | roles, disable; step-up |
| Admin users | POST | /v1/admin/users/{adminId}/reset-totp | super_admin | W3-05 | step-up |
| Audit | GET | /v1/admin/audit | admin | W3-05 | |
| Jobs | GET | /v1/admin/jobs | support, analyst, finance (`money` kinds only), admin | W1-11 | `admin-jobs.yaml` (D-95). Every kind with its class, owner, schedule, last run, next run, 7-day success rate, average and maximum duration, paused state; filters `class=`, `status=` |
| Jobs | GET | /v1/admin/jobs/{kind} | same as the list | W1-11 | one kind: settings, schedule in IST, control state (`paused`, `reason`, who and when) |
| Jobs | GET | /v1/admin/jobs/{kind}/runs?status=&trigger=&from=&to= | same as the list | W1-11 | run history, newest first, server-paginated; each row has the summary counts |
| Jobs | GET | /v1/admin/job-runs/{runId} | same as the list | W1-11 | run detail: summary, note, scrubbed error, attempt, timeline, `triggeredBy`, Grafana Explore links (Loki by `run_id`, Tempo by `trace_id`). The path is `job-runs`, not `jobs/runs`, because of the 1.2 path rule |
| Jobs | POST | /v1/admin/jobs/{kind}/run | admin; `money` kinds super_admin | W1-11 | `{reason, periodKey?}`; scheduled kinds only; `Idempotency-Key`; `money` needs step-up; `409 job_not_runnable`, `job_already_running`, `job_paused`; audited |
| Jobs | POST | /v1/admin/jobs/{kind}/pause | admin; `money` kinds super_admin | W1-11 | `{reason}`; stops new scheduled enqueues, and event jobs snooze; `money` needs step-up; `409 job_not_runnable` for `timer`; audited |
| Jobs | POST | /v1/admin/jobs/{kind}/resume | admin; `money` kinds super_admin | W1-11 | `{reason}`; same limits as pause; audited |
| Jobs | POST | /v1/admin/job-runs/{runId}/retry | admin; `money` kinds super_admin | W1-11 | `{reason}`; a `failed`, `discarded` or `missed` run; `money` kinds need super_admin plus step-up; `409 job_not_runnable` for a `timer` kind; `409 job_already_running`, `job_paused`; `409 invalid_transition` for any other status; audited |
| System | GET | /v1/admin/system | admin | W3-15 | `admin-system.yaml` (D-97). Beanstalk, ALB, RDS, River, CloudWatch alarm states and the top 10 SQL statements; read-only; cached 60 s per instance; AWS panels say "Not configured (local)" off AWS |

### 2.8 AI drafts (`ai.yaml`, D-46)

W3-10 owns the shared draft envelope, `ai_drafts` (feature, actor, input ref, prompt version, model, output, per-line outcomes, cost, latency) and the base schemas; each feature package owns its feature's paths. `voice_drafts` is folded in as feature `voice_stock`.

Every feature has exactly three endpoints under its prefix:

| Method | Path | Notes |
|---|---|---|
| POST | `<prefix>/drafts` | input (multipart for audio/images) → `202 {draftId, status: processing}`; flag off or key missing → feature hidden (`404`) |
| GET | `<prefix>/drafts/{draftId}` | `processing|ready|failed` + draft lines; only the creator (and same-store managers for business features) |
| POST | `<prefix>/drafts/{draftId}/confirm` | idempotent; `{lines:[{lineId, outcome: accepted|edited|discarded, value?}]}`; the server re-validates every line as if typed by hand and records per-line outcomes. All lines `discarded` = discard the draft |

Nothing changes stock, money, orders, reviews or KYC except through confirm, and confirm applies only what the person sent.

| Feature | Prefix | Create | Confirm | Confirm does | Pkg |
|---|---|---|---|---|---|
| `voice_stock` (AI-R1) | {S}/ai/voice-stock | cashier | per line (D-06, D-65): `sale` needs `pos.sell` (cashier+), `set_price` needs `item.price.edit` (manager+), `restock` needs `stock.restock` (manager+), `count` needs `stock.count` (cashier+; "out of stock" is a `count` of 0); lines the user may not confirm come back `disabled: "ask_manager"` | `sale`: a POS sale with no customer (`sale_pos` movements); price edits; `restock` and `count_correction` movements | W3-04 |
| `supplier_bill` (AI-R2) | {S}/ai/supplier-bill | manager | manager | `supplier_bill` movements + batches; unmatched lines → `POST {B}/products` drafts | W3-11 |
| `shelf_photo` (AI-R3) | {S}/ai/shelf-photo | manager | manager | creates store items (price defaults to MRP) | W3-11 |
| `review_reply` (AI-R6) | {S}/ai/review-reply | manager | manager | posts the edited reply | W3-11 |
| `banner_copy` (AI-R9) | {S}/ai/banner-copy | manager | manager | returns the chosen, edited copy for `POST {S}/banners`; publishes nothing | W3-11 |
| `list_parse` (AI-C1) | /v1/me/ai/list-parse | cus | cus | parses typed, pasted or spoken text into basket lines (a draft); confirm returns the lines the customer accepted or edited for the basket editor (nothing is saved or searched by confirm; the customer saves the basket and searches it with `POST /v1/discover/basket-search`, D-110) | W3-12 |
| `catalogue_cleanup` (AI-A1) | /v1/admin/ai/catalogue-cleanup | catalogue, admin | catalogue, admin | applies normalised fields / merge to a pending product | W3-13 |
| `ticket_triage` (AI-A2) | /v1/admin/ai/ticket-triage | support, admin | support, admin | sets category/priority/linked order; sends the edited reply | W3-13 |
| `risk_summary` (AI-A3) | /v1/admin/ai/risk-summary | support, admin | support, admin | attaches the summary to the flag | W3-13 |

The three admin features (W3-13) keep the paths above. Their UI lives in `admin/src/ai-assist/{catalogue,support,risk}`, mounted through `<AssistSlot/>` on A-08, A-14 and A-13; the shared AI code (`internal/ai/{client,schema,scrub,guard,eval,prompts,drafts}`) is W3-10's (D-82).

Insight text (AI-R4) and coach cards (AI-R5) come back inside `{S}/insights` and `{S}/coach/cards`; the banner image check (AI-R10) runs inside banner upload. None has its own endpoint.

**Customer search AI without a draft (D-101).** Voice search (AI-C5) and search assist (AI-C2) return a suggestion the customer acts on by tapping, so they have one endpoint each, not the three draft endpoints: no `ai_drafts` row, no audio, transcript or query stored, nothing in logs or traces. The W3-10 cost guard counts each call against `ai_daily_calls_customer` (`429 ai_daily_cap_reached`). Both are hidden (`404`) while their flag is off or the provider key is missing (11 §1 rule 11).

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| POST | /v1/me/ai/voice-search | cus | W3-12 | multipart audio (at most 15 s) → `200 {transcript, chips}`; the transcript goes back to the app only. The search then runs as a normal `GET /v1/discover/search` with `source=voice`, logged as a typed query. Flag `ai_voicesearch_enabled`, key `SARVAM_API_KEY` |
| POST | /v1/me/ai/search-assist | cus | W3-12 | `{q}` → `200 {query?, chips}`, called by the app only after the results render and only when search returned `refineHint`. Model timeout 3 s (01 §17 fixed constants); on a timeout, provider error or output that fails validation → `204` and the app shows nothing. Flag `ai_querynorm_enabled` |
| GET | /v1/me/ai/features | cus | W3-12 | `{listParse, voiceSearch, searchAssist, maxAudioSeconds, maxAudioBytes, audioTypes}` (D-231): a flag is on only while its switch is on and its provider key is set, so the app never shows a helper that answers `404`. Counts nothing against `ai_daily_calls_customer` |

### 2.9 Telemetry, test, health

| Method | Path | Auth | Pkg | Notes |
|---|---|---|---|---|
| POST | /v1/rum | pub, rate-limited | W1-07 | batched web vitals + long tasks + API timings from browsers (sendBeacon) |
| POST | /v1/events | pub, rate-limited | W1-07 | batched product events (≤ 50 per call, schema-validated against `shared/telemetry/events/<domain>.yaml`) |
| POST | /v1/client-errors | pub, rate-limited | W1-07 | only when Sentry is off |
| GET/POST | /v1/test/clock | none | W0-03 | read / set / advance the fake clock, for manual testing of timers and expiries as well as automated tests; registered only when `APP_ENV` is `local` or `test` and `API_ADDR` is a loopback address; staging and prod builds return `404` (D-46, D-80). Contract below |
| GET | /v1/test/slow?ms= | none | W1-07 | answers after `ms` milliseconds (capped at 30000), to force a slow route for latency alerts and the slow-network checks (D-93). Same gate as `/v1/test/clock` |
| GET | /v1/test/slow-sql?ms= | none | W1-07 | runs `pg_sleep(ms / 1000)` in a query, to force a slow query for the slow-query alert (D-93); `ms` is capped at 30000. Same gate as `/v1/test/clock` |

`GET /healthz`, `GET /readyz` and `GET /version` sit outside `/v1`. Owner W0-03. The API and the worker both serve `/healthz` and `/readyz`. They are plain JSON, not problem JSON, and send `Cache-Control: no-store`.

| Endpoint | Meaning | Response |
|---|---|---|
| `/healthz` | Process is up. Never touches a dependency. | `200 {"status":"ok"}` |
| `/readyz` | DB reachable, migrations at head, River running. Result cached 2 s; one run in flight at a time. After SIGTERM it answers 503 at once and stays 503 until the process exits (D-98, 02 §2.8). | `200 {"status":"ok","checks":{"db":"ok","migrations":"ok","river":"ok"}}`. When a check fails: `503 {"status":"unavailable","checks":{"db":"fail",...}}`. While draining: `503 {"status":"draining"}`. Error text never appears in the body; it is logged. |
| `/version` | Build info. On the API only in `local` and `test`. On the worker's internal port (`WORKER_ADDR`, 8181) in every environment. | `200 {"version","commit","buildTime","dirty"}` |

The load balancer health check uses `/readyz`. Nothing in this table is exposed through Cloudflare.

**Test clock contract (`/v1/test/clock`, D-80).** Registered only when `APP_ENV` is `local` or `test`, the database's `platform_environment` row agrees, and `API_ADDR` is a loopback address (`localhost`, `127.0.0.0/8`, `::1`; `:8180` is not). Otherwise it answers `404`.

| Call | Body | Response |
|---|---|---|
| `GET` | none | `200 {"now","offsetMs"}` |
| `POST` | `{"advance":"5m"}` (Go duration above zero; concurrent advances add up) | `200 {"now","offsetMs","dueJobs"}` |
| `POST` | `{"set":"<RFC 3339>"}` | same |
| `POST` | `{"reset":true}` | same |

`POST` takes JSON only (`415 unsupported_media_type` otherwise) and exactly one of `advance`, `set`, `reset` (`422 validation_failed` otherwise). An optional `"wait"` (default `10s`, at most `50s`, `0s` to skip) bounds how long the call waits for River to work the jobs that became due; `dueJobs` is how many were still due when it returned.

### 2.10 Open items

None. (The shop cancel reason list was settled by D-68: `ShopCancelReason`, 1.5.)

### 2.11 Count

315 method + path pairs in `/v1`, plus 3 health endpoints outside it. By namespace: admin 126 (9 AI; +1 for `GET /v1/admin/field/me/stats`, D-64; +8 jobs and +1 system, D-95, D-97; +3 demand gaps and site strip, D-101), biz 103 (15 AI; +3 asks, D-101; +1 for `POST {S}/insights/dismiss`, D-99; `GET/POST {S}/batches` became `GET {S}/batches` and `PATCH {S}/batches/{batchId}`, D-99, so the count is unchanged), me 52 (6 AI: list parse 3, voice search 1, search assist 1, D-101, D-110, and features 1, D-231; +4 asks, D-101; +5 baskets, D-110), discover 13 (D-101 adds parameters and fields to search, no pair; +1 `POST /v1/discover/basket-search`, D-110; +1 substitutes, D-235), auth 8, public 6 (+3 share cards and site strip, D-101), telemetry 3, test 4. The AI-C1 rename from list search to list parse (D-110) keeps its 3 pairs. Store mute (D-101) is a field on `PATCH {S}/settings`, not a pair. Re-checked after the D-61…D-84 follow-ups (2026-10-22): no pair appears twice, no literal segment sits beside a parameter, and `/v1/discover/stores/` has only `{slug}` beneath it. The path lint (1.2) repeats the check on the bundled spec on every `make gen`.

## 3. Contract tests

- **Schemathesis** runs against the local API from the spec (fuzzing every endpoint with valid and invalid inputs; no 500s allowed).
- Frontend tests use **MSW** handlers generated from the spec examples, so a spec change breaks stale mocks.
- Every endpoint has at least one example request/response in the spec; examples are validated in `make check`.
- The path lint (1.2) and a problem-code lint (every `code` a handler can return is in `problems/`) run in `make gen`.
