# 04 · API

The contract lives in `backend/api/` (OpenAPI 3.1), split per domain: `paths/<domain>.yaml`, `schemas/<domain>.yaml`, `problems/<domain>.yaml`. A work package edits only its own domain files (D-55). `make gen` bundles them into `openapi.yaml` and generates the Go server stubs, the TypeScript client (`@todayz/api-client`, in `shared/api-client`) and sqlc. Generated output is not committed; every build and check runs `make gen` first (D-50).

## 1. Conventions

- Base: `https://api.todayz.in/v1` (local: `https://api.todayz.localhost/v1`).
- JSON, `camelCase` fields, ULID string ids, money as integer paise in fields named `*Paise`, credits as integers (1 credit = ₹1), timestamps RFC 3339 UTC.
- Pagination: cursor based. `?limit=20&cursor=…` → `{ "items": [...], "nextCursor": "…" | null }`. Max limit 100.
- Filtering and sorting through named query params only (no free-form SQL-ish filters).
- Concurrency: mutable resources return `ETag: "v{version}"`; updates send `If-Match`. Mismatch → `412`.
- Rate-limit headers: `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`; `429` with `Retry-After`.
- Tracing: clients send `traceparent`; every response returns `X-Request-Id`.
- Versioning: `/v1`. Additive changes only within v1. The oldest client allowed per app is set in settings (`min_client_version_customer`, `min_client_version_business`, `min_client_version_admin`); an older app shows "Update available" and reloads its service worker.
- Language: `Accept-Language` picks translated category names and notification text.

### 1.1 Namespaces (D-10)

| Prefix | Who | Auth label |
|---|---|---|
| `/v1/auth/...` | Sign-in, refresh, sign-out | pub, cookie |
| `/v1/discover/...` | Anyone; read-only view of `live` stores of `verified` businesses (D-40) | pub |
| `/v1/me/...` | The signed-in customer. The profile rows in 2.2 marked **any** serve whoever is signed in, in any app | cus |
| `/v1/biz/businesses/{bid}/...` and `/v1/biz/businesses/{bid}/stores/{sid}/...` | Staff of that business; the server checks membership, store scope and role | biz(role) |
| `/v1/admin/...` | Admins | adm(roles) |
| `/v1/public/...` | No account: leads, receipts, referral links | pub |
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
- Admin roles (D-64): `field_agent`, `support`, `catalogue`, `analyst`, `finance`, `admin`, `super_admin`. `admin` can do everything except credit adjustments, admin-user and role management, money settings (unit `paise`, `bp` or `credits`), and payout hold, unhold, mark-paid and export. `analyst` only reads Pulse and dashboards. "all but field_agent" in 2.7 means `support`, `catalogue`, `analyst`, `finance`, `admin`.
- Permission keys (e.g. `item.price.edit`, `stock.restock`, `stock.count`) and the full role matrix are in 02 (D-38). Policy code lives in `platform/authz/policy/<domain>.go` (D-50).
- Step-up: sensitive admin actions need a TOTP re-prompt within 10 min (02); marked **step-up** below.

### 1.4 Errors and problem codes (D-24)

- RFC 9457 problem JSON: `{ "type": "https://todayz.in/errors/insufficient_stock", "title": "…", "status": 409, "code": "insufficient_stock", "detail": "…", "requestId": "…", "fields": { "items[2].quantity": "max 3" } }`. `code` is stable and translated on the client; `title`/`detail` are English for logs.
- One registry: `backend/api/problems/<domain>.yaml`, bundled. Each entry has the code, HTTP status and i18n key. W0-03 creates `problems/common.yaml`; each package adds codes in its own domain file. A response may only use a registered code.
- A refused state change is always `409 invalid_transition` (detail names the from and to states).
- Codes named in this file: `invalid_transition`, `idempotency_mismatch` (common); `insufficient_stock`, `store_closed`, `store_not_bookable`, `code_locked`, `account_has_active_order` (orders); `resync_required` (sync); `reversal_window_passed` (pos); `approver_is_requester` (admin-credits).

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

`Idempotency-Key` header (ULID from the client) required on: order create, every order transition (customer, shop, admin), confirm-return, code verify, POS sale, POS reverse, sync, bill-claim confirm, counter-code verify, credit adjustment request and approve, payout mark-paid, every AI draft confirm. Same key + same body → same response; same key + different body → `422 idempotency_mismatch`.

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
| DELETE | /v1/me | cus | W1-01 | `{otpRequestId, code}`; anonymise; `409 account_has_active_order` while any order is unfinished; forfeits the wallet (D-31) |
| GET/POST | /v1/me/addresses | cus | W1-01 | |
| PATCH/DELETE | /v1/me/addresses/{addressId} | cus | W1-01 | |
| GET | /v1/me/stream | cus | W2-05 | SSE |
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
| GET | /v1/discover/summary?lat=&lng=&radius= | W1-06 | `{storesNearby, openNow, productsInRadius, pins:[{storeSlug, lat, lng, open, categoryTint}] or clusters}`: the StoreMap card |
| GET | /v1/discover/categories | W1-05 | tree with tints and icons |
| GET | /v1/discover/stores?lat=&lng=&radius=&category=&openNow=&delivery=&hasOffer= | W1-06 | shop list with distance, rating, cashbackPct, delivery flag, open state, `bookable` (false = "In-store only", D-41) |
| GET | /v1/discover/stores/{slug} | W1-06 | shop page (hours, rating, offers, cashback, delivery terms); `?s={standeeId}` records a standee scan (D-26) |
| GET | /v1/discover/stores/{slug}/items?q=&category=&cursor= | W1-06 | shop catalogue with stock + freshness |
| GET | /v1/discover/stores/{slug}/reviews | W2-06 | |
| GET | /v1/discover/search?q=&lat=&lng=&radius=&category=&openNow=&delivery=&hasOffer=&sort= | W1-06 | results grouped by store; filters first (in stock, `live`, in radius), then the D-42 score; `sort=relevance` (default) `|nearest|cheapest`; `hasOffer` is the C-05 chip; logs to `search_log` |
| GET | /v1/discover/search/suggest?q= | W1-06 | product/brand/category suggestions |
| GET | /v1/discover/offers?lat=&lng=&radius= | W2-07 | Offers near you: banners + top offers with fair rotation |

### 2.4 Customer orders, purchases, wallet, referrals, reviews, support (`/v1/me`, all cus)

| Method | Path | Pkg | Notes |
|---|---|---|---|
| POST | /v1/me/checkout/quote | W2-02 | `{storeId, items, fulfilment, addressId?, creditsToUse}` → per-line availability, offers applied, `itemsAfterOffersPaise` (eligible amount, D-02), delivery fee, credits allowed with a reason when 0 (under ₹100, a cap reached: D-30), total. No side effects. |
| POST | /v1/me/orders | W2-02 | idempotent; re-prices, rechecks stock, holds credits, sets `accept_by`, enqueues expiry job, notifies store; `409 store_closed` outside hours or when closed manually; `409 store_not_bookable` when pickup and delivery are both off (D-41) |
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
| POST | /v1/me/reviews | W2-06 | `{orderId|posSaleId, stars, text, tags: ReviewTagPositive[] | ReviewTagNegative[]}` |
| PATCH | /v1/me/reviews/{reviewId} | W2-06 | own review, within 7 days |
| POST | /v1/me/review-reports | W2-06 | `{reviewId, reason}`; report someone else's review |
| GET/POST | /v1/me/support/tickets | W2-09 | list / create (linked to an order) (D-52) |
| GET | /v1/me/support/tickets/{ticketId} | W2-09 | |
| GET/POST | /v1/me/support/tickets/{ticketId}/messages | W2-09 | |

### 2.5 Public (`public.yaml`, pub)

| Method | Path | Pkg | Notes |
|---|---|---|---|
| POST | /v1/public/leads | W1-04 | site lead form (Turnstile) (D-46) |
| GET | /v1/public/receipts/{token} | W2-04 | 128-bit token, expires after 30 days; items, totals, store; no customer data. Screen C-24, route `/r/{token}` (D-45) |
| GET | /v1/public/ref/{code} | W2-03 | referral click → 302 to the app with the attribution cookie |

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
| GET/PATCH | {S}/settings | manager | W1-04 | hours, holidays, manual close, `acceptsPickup`, `acceptsDelivery` (D-41), delivery terms, cashback, auto-accept, notification prefs |
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
| POST | {S}/items/{itemId}/movements | cashier / manager | W1-05 | `{kind, quantity, note}`: `count_correction` needs `stock.count` (cashier+); `restock` needs `stock.restock`, `damage` and `expired` need manager+ (D-06). Other sources (D-01) are written only by their own flows |
| GET | {S}/items/{itemId}/history | cashier | W1-05 | movements |
| GET/POST | {S}/batches | manager | W1-05 | expiry batches |
| POST | {S}/item-imports | manager | W1-05 | CSV/XLSX upload → validated preview with row errors |
| POST | {S}/item-imports/{importId}/apply | manager | W1-05 | writes `import` movements |
| GET | {S}/catalogue/snapshot | cashier | W1-05 | full snapshot for the offline POS, with the current `catalogue_changes` cursor |
| GET | {S}/catalogue/changes?cursor= | cashier | W1-05 | deltas from `catalogue_changes`; cursor older than 30 days → `410 resync_required` (D-36) |

**Orders (`orders.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| GET | {S}/orders?status=&cursor= | cashier | W2-02 | customer phone masked except while `out_for_delivery` (D-60) |
| GET | {S}/orders/{orderId} | cashier | W2-02 | never contains the order code (D-04) |
| POST | {S}/orders/{orderId}/accept | cashier | W2-02 | `{lines:[{orderItemId, quantity}]}` full or partial (partial → `awaiting_customer`); creates reservations, no movement (D-01) |
| POST | {S}/orders/{orderId}/reject | cashier | W2-02 | `{reason: RejectReason}` |
| POST | {S}/orders/{orderId}/ready | cashier | W2-02 | pickup only, optional |
| POST | {S}/orders/{orderId}/dispatch | cashier | W2-02 | delivery: `accepted` → `out_for_delivery` |
| POST | {S}/orders/{orderId}/fail-delivery | cashier | W2-02 | `{reason: DeliveryFailedReason}`; only from `out_for_delivery` (D-22); reservations stay held until the return is settled (D-69) |
| POST | {S}/orders/{orderId}/confirm-return | cashier (`returned`) / manager (`damaged`) | W2-02 | `{outcome: returned|damaged}` on a `delivery_failed` order. `returned` releases the reservations; `damaged` writes `damage` movements, then releases (manager+; a cashier asks a manager, D-69). Unsettled orders get inbox reminders to the store at 2 h and 24 h; at 72 h the system settles them as `returnOutcome: unconfirmed` and raises a risk flag (D-69). `409 invalid_transition` once settled |
| POST | {S}/orders/{orderId}/cancel | manager | W2-02 | `{reason: ShopCancelReason, note?}` (D-68); manager and above only (D-65); no strike |
| POST | {S}/orders/{orderId}/verify-code | cashier | W2-02 | hand over: `{code}` → HMAC lookup on `code_hmac` (D-04); 5 tries then `code_locked`; marks collected/delivered (writes `sale_online`, D-01), records payment method |
| GET | {S}/stream | cashier | W2-05 | SSE |

**POS and sync (`pos.yaml`, `sync.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| POST | {S}/sync | cashier | W2-04 | offline outbox (sales, movements) in device seq order; returns server deltas. An offline counter-code link carries the typed code, device time and the clock offset at the last online check; verified per D-12, else the sale stays unlinked ("Customer link failed") |
| POST | {S}/pos/sales | cashier | W2-04 | online sale; `link.method: bill_qr|counter_code|phone_unverified` (D-07); manual line or bill discounts need manager+ (D-06); round-off to the nearest ₹1 is automatic and allowed for cashiers (D-65); response has `receiptToken` (D-45) |
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
| GET | {S}/insights | manager | W3-03 | Demand insights (4 groups) |
| GET | {S}/analytics?range=&metric= | manager | W3-03 | |
| GET | {S}/customers | manager | W3-03 | repeat customers (masked) (D-46) |
| GET | {S}/coach/cards?status= | manager | W3-11 | coach feed (D-48) |
| POST | {S}/coach/cards/{cardId}/done | manager | W3-11 | |
| POST | {S}/coach/cards/{cardId}/dismiss | manager | W3-11 | "Not useful" |

**Credits, statements, referrals, standees (`business-credits.yaml`, `referrals.yaml`, `standee.yaml`)**

| Method | Path | Role | Pkg | Notes |
|---|---|---|---|---|
| GET | {B}/credit-account | manager | W2-01 | balance, lines |
| GET | {B}/statements | manager | W3-01 | monthly statements (D-51) |
| GET | {B}/statements/{period} | manager | W3-01 | detail; `Accept: text/csv` for CSV |
| GET | {B}/referrals | manager | W2-03 | business referral code (shop-to-shop), stats (D-26) |
| GET | {S}/qr-standee.pdf?standeeId= | manager | W2-10 | printable A5 standee; URL carries the store short code and `?s={standeeId}` (D-26, D-52) |
| GET/POST | {S}/standees | manager | W3-09 | standee rows (no code of their own) with scan and sign-up counts (D-51) |

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
| Customers | GET | /v1/admin/customers?phone= | support, admin | W3-05 | exact match on the keyed hash |
| Customers | GET | /v1/admin/customers/{customerId} | support, admin | W3-05 | |
| Customers | POST | /v1/admin/customers/{customerId}/block | admin | W3-05 | |
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
| Reviews | GET | /v1/admin/reviews | support, admin | W3-05 | |
| Reviews | POST | /v1/admin/reviews/{reviewId}/hide | support, admin | W3-05 | `{reason}`; audited |
| Reviews | POST | /v1/admin/reviews/{reviewId}/restore | support, admin | W3-05 | `{reason}`; audited (D-41) |
| Reviews | GET | /v1/admin/review-reports | support, admin | W3-05 | |
| Reviews | POST | /v1/admin/review-reports/{reportId}/resolve | support, admin | W3-05 | |
| Credits | GET | /v1/admin/ledger/accounts?q= | finance, admin | W3-01 | |
| Credits | GET | /v1/admin/ledger/accounts/{accountId}/entries | finance, admin | W3-01 | |
| Credits | GET | /v1/admin/credits/expiry-preview | finance, admin | W3-01 | |
| Credits | GET/POST | /v1/admin/credit-adjustments | finance | W3-01 | POST = request, step-up, idempotent: at or under `admin_adjust_dual_approval_above_paise` posts at once; above it creates a pending `admin_adjustment_requests` row, for super_admin too (D-05). `admin` cannot adjust |
| Credits | POST | /v1/admin/credit-adjustments/{requestId}/approve | finance | W3-01 | step-up, idempotent; approver ≠ requester else `403 approver_is_requester` |
| Credits | POST | /v1/admin/credit-adjustments/{requestId}/reject | finance | W3-01 | approver ≠ requester |
| Payouts | GET | /v1/admin/statements?period= | finance, admin | W3-01 | |
| Payouts | GET | /v1/admin/payouts?period=&status= | finance, admin | W3-01 | at most one open payout per business (D-09) |
| Payouts | GET | /v1/admin/payouts/{payoutId} | finance, admin | W3-01 | with the business's bank details masked (last 4) (D-64) |
| Payouts | POST | /v1/admin/payouts/{payoutId}/hold | finance | W3-01 | `{reason}`; `admin` cannot (D-64) |
| Payouts | POST | /v1/admin/payouts/{payoutId}/unhold | finance | W3-01 | `admin` cannot (D-64) |
| Payouts | POST | /v1/admin/payouts/{payoutId}/mark-paid | finance | W3-01 | `{utr}`; step-up, idempotent; `admin` cannot (D-64) |
| Payouts | GET | /v1/admin/payout-exports?period= | finance | W3-01 | CSV for the bank/UPI batch; `admin` cannot (D-64) |
| Referrals | GET | /v1/admin/referrals | admin | W3-02 | `admin-referrals.yaml` (D-51) |
| Referrals | POST | /v1/admin/referrals/{referralId}/reject | admin | W3-02 | `{reason}`; reverses pending |
| Risk | GET | /v1/admin/risk/flags | support, admin | W3-02 | `admin-risk.yaml` (D-51) |
| Risk | GET | /v1/admin/risk/flags/{flagId} | support, admin | W3-02 | evidence |
| Risk | POST | /v1/admin/risk/flags/{flagId}/clear | admin | W3-02 | |
| Risk | POST | /v1/admin/risk/flags/{flagId}/confirm | admin | W3-02 | can block/suspend |
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
| `list_search` (AI-C1) | /v1/me/ai/list-search | cus | cus | returns shops ranked by list coverage, price, distance; read-only (cart filled on the client) | W3-12 |
| `voice_search` (AI-C2) | /v1/me/ai/voice-search | cus | cus | returns `/v1/discover/search` results for the confirmed query; read-only | W3-12 |
| `catalogue_cleanup` (AI-A1) | /v1/admin/ai/catalogue-cleanup | catalogue, admin | catalogue, admin | applies normalised fields / merge to a pending product | W3-13 |
| `ticket_triage` (AI-A2) | /v1/admin/ai/ticket-triage | support, admin | support, admin | sets category/priority/linked order; sends the edited reply | W3-13 |
| `risk_summary` (AI-A3) | /v1/admin/ai/risk-summary | support, admin | support, admin | attaches the summary to the flag | W3-13 |

The three admin features (W3-13) keep the paths above. Their UI lives in `admin/src/ai-assist/{catalogue,support,risk}`, mounted through `<AssistSlot/>` on A-08, A-14 and A-13; the shared AI code (`internal/ai/{client,schema,scrub,guard,eval,prompts,drafts}`) is W3-10's (D-82).

Insight text (AI-R4) and coach cards (AI-R5) come back inside `{S}/insights` and `{S}/coach/cards`; the banner image check (AI-R10) runs inside banner upload. None has its own endpoint.

### 2.9 Telemetry, test, health

| Method | Path | Auth | Pkg | Notes |
|---|---|---|---|---|
| POST | /v1/rum | pub, rate-limited | W1-07 | batched web vitals + long tasks + API timings from browsers (sendBeacon) |
| POST | /v1/events | pub, rate-limited | W1-07 | batched product events (≤ 50 per call, schema-validated against `shared/telemetry/events/<domain>.yaml`) |
| POST | /v1/client-errors | pub, rate-limited | W1-07 | only when Sentry is off |
| GET/POST | /v1/test/clock | none | W0-03 | read / set / advance the fake clock, for manual testing of timers and expiries as well as automated tests; registered only when `APP_ENV` is `local` or `test`; staging and prod builds return `404` (D-46, D-80) |

`GET /healthz` (process up), `GET /readyz` (DB reachable, migrations at head, River running), `GET /version` (git sha, build time) sit outside `/v1`. Not exposed through Cloudflare except `/version`. Owner W0-03.

### 2.10 Open items

None. (The shop cancel reason list was settled by D-68: `ShopCancelReason`, 1.5.)

### 2.11 Count

281 method + path pairs in `/v1`, plus 3 health endpoints outside it. By namespace: admin 112 (9 AI; +1 for `GET /v1/admin/field/me/stats`, D-64), biz 99 (15 AI), me 43 (6 AI), discover 11, auth 8, public 3, telemetry 3, test 2. Re-checked after the D-61…D-84 follow-ups (2026-10-22): no pair appears twice, no literal segment sits beside a parameter, and `/v1/discover/stores/` has only `{slug}` beneath it. The path lint (1.2) repeats the check on the bundled spec on every `make gen`.

## 3. Contract tests

- **Schemathesis** runs against the local API from the spec (fuzzing every endpoint with valid and invalid inputs; no 500s allowed).
- Frontend tests use **MSW** handlers generated from the spec examples, so a spec change breaks stale mocks.
- Every endpoint has at least one example request/response in the spec; examples are validated in `make check`.
- The path lint (1.2) and a problem-code lint (every `code` a handler can return is in `problems/`) run in `make gen`.
