# TodayZ threat model (W4-03)

STRIDE for each flow, based on the code at base `a6b58e40` (Waves 0–3 merged) as of 26 Oct 2026. Each control points to `file:line`. Paths are relative to the repo root; `backend/` is left out for Go files under `internal/` and `cmd/`. **GAP** marks a weakness that is still open, and **known** marks one the plan already accepts. Every finding is listed with its severity and status in `docs/qa/W4-03/REPORT.md`. Section 6 was updated for D-264 (no admin TOTP, SMS step-up, the admin sign-in lock and the step-up quota) on main at `a0402006`, 27 Oct 2026.

## System and trust boundaries

```
 phone / browser ──TLS──► Cloudflare (Pages: 4 apps + customer Function /x/{code})
        │                                │  (Function → API with X-TZ-Preview-IP/Key)
        └──TLS──► Cloudflare ──► ALB ──► API (Go, chi, slots 1–15) ──► Postgres 16 (RDS)
                                          │        └──► River worker (same DB)
                                          └──► SMS / WhatsApp / Sentry / Anthropic / Sarvam (egress)
```

Boundaries:
- B1: internet to the API, through Cloudflare and the ALB.
- B2: internet to Pages, including the Function.
- B3: Function to API.
- B4: API to Postgres.
- B5: API to the providers.
- B6: an offline POS device to the API (sync).

Actors:
- An anonymous visitor.
- A customer.
- Business staff with the role owner, manager or cashier, possibly in another business.
- An admin with the role support, finance, admin or super_admin.
- A link-preview bot.
- An attacker on the network.

### Request pipeline (applies to every flow)

Slots are wired in `cmd/todayz/app.go`, and each middleware file names its slot.

| Slot | Control | Where |
|---|---|---|
| 2 | `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, CSP `default-src 'none'; frame-ancestors 'none'`, `Cache-Control: no-store` on /v1 | `internal/platform/httpx/mw/secheaders.go:11-34` |
| 5 | Client IP. CF-Connecting-IP is trusted from any peer when deployed, so this relies on the ALB security group (W5) | `internal/platform/httpx/mw/clientip.go:16-42` |
| 7 | CORS allowlist from config | `internal/platform/httpx/mw/cors.go` |
| 8 | Path canonicalisation: a raw path two routers could read differently is a 400 (D-100 H-1) | `internal/platform/httpx/mw/pathguard.go:13-` |
| 10 | Body limit (413) | `internal/platform/httpx/mw/bodylimit.go:12-19` |
| 11 | Authentication: EdDSA only, kid pinned, exp/iss/aud checked, and the live session checked on every request | `internal/platform/authn/tokens.go:111-120`, `internal/platform/authn/authenticator.go:57-90` |
| 12 | Generic rate limit. It is in-memory and only on when deployed, and its numbers are TODO(lead) | `cmd/todayz/security.go:62-69`, `internal/platform/ratelimit/ratelimit.go:30-33` |
| 15 | Authorization: one action per operationId, fail closed (an unknown action is a 500) | `internal/platform/authz/engine.go:101-105`, `internal/platform/httpx/mw/authz.go:17-30` |
| — | Access log leaves out the query string and body; logs go through the redaction handler | `internal/platform/httpx/mw/accesslog.go:30`, `internal/platform/telemetry/log.go:30-68` |

Business scope (AU-09) works like this:
- `checkBusiness` takes the business id and store id from the path.
- It refuses a store that belongs to a different business than the one in the path.
- It then needs a live staff membership, the grant for the member's role, and, if the member has a store list, the store in that list.
- Code: `internal/platform/authz/engine.go:134-163`.

Handlers then load every resource through the store in the path (for example `ofStore(storeID)` in `internal/orders/transitions.go:495`), so an id from another store is a 404.

---

## 1. Auth: OTP, sessions, refresh

**Flow**
1. POST /v1/auth/otp/send (Turnstile, limits) sends an SMS.
2. POST /v1/auth/otp/verify returns an access JWT (15 min) and a refresh cookie for the app.
3. POST /v1/auth/refresh rotates the refresh token.

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| S | OTP guessing | 6 digits, 5 min TTL, 5 tries per request, then `otp_locked` (`internal/identity/identity.go:43-49`, `internal/identity/otp.go:195-218`). The code is stored as HMAC(PII_HASH_KEY, request id + code) and compared with `hmac.Equal` (`internal/identity/otp.go:212`, `:409-411`) | pass |
| S | Test code 000000 in prod | Accepted only for reserved numbers (`pii.IsReservedTestPhone`), and only when `Env.AllowsTestRoutes()`, TestRoutes is set and API_ADDR is loopback (`internal/identity/identity.go:127`, `internal/identity/otp.go:213`). Tested in `internal/identity/identity_test.go:517-525` | pass |
| S | SMS pumping / OTP flooding | Turnstile on send (`internal/identity/otp.go:52-57`). A 30 s resend gap, 5 per phone per hour and 20 per IP per hour, counted in Postgres under advisory locks (`internal/identity/otp.go:76-104`). Applies in every env | pass |
| S | Stolen access token | 15 min TTL. Session row checked on every request, so revoking or blocking takes effect at once (`internal/platform/authn/authenticator.go:57-90`) | pass |
| S | Stolen refresh token | Rotation on each use. Reuse after the 30 s grace revokes the whole family and is audited (`internal/identity/session.go:44`, `:139-215`) | pass |
| T | CSRF on refresh/logout | Cookie is `__Secure-tz_rt_{c,b,a}`, Path=/v1/auth, HttpOnly, Secure, SameSite=Strict (`internal/identity/handlers.go:132-150`). Origin allowlist per app plus the `X-TZ-Client` header (`internal/identity/handlers.go:239-264`, `internal/platform/authn/authenticator.go:94`). Tested in `cmd/todayz/identity_http_test.go:147`; stack check in `e2e/security/abuse.stack.test.mjs` | pass |
| T | Token forgery (alg confusion) | Only EdDSA accepted (`internal/platform/authn/tokens.go:120`) | pass |
| R | Unattributed sign-ins and revocations | Audit rows for refresh reuse and admin revoke (`internal/identity/session.go:193`) | pass |
| I | Phone or OTP in logs | Access log has no query or body. Redacting slog handler. Phones are stored as AES-GCM ciphertext plus an HMAC hash (`internal/platform/pii/pii.go:3`, `:29-45`). `/v1/me` returns `phoneMasked` only | pass |
| D | OTP endpoint exhausting the SMS budget | The limits above, plus the generic limiter when deployed. **GAP (low):** the generic limiter is in-memory per instance and its numbers are TODO(lead) (`internal/platform/ratelimit/ratelimit.go:30-33`) | open |
| D | Spoofed client IP evading the per-IP cap | **GAP (low, W5):** CF-Connecting-IP is believed from any peer when deployed (`internal/platform/httpx/mw/clientip.go:16`). Safe only if the ALB accepts Cloudflare ranges alone; the infra security group must enforce that | open (W5) |
| E | One app's token used on another app | The token's `app` claim is checked by authz (`internal/platform/authz/engine.go:136`). Tested in `cmd/todayz/security_abuse_http_test.go` (TestAbuseRoleEscalationHTTP) | pass |

## 2. Orders and pickup/delivery codes

**Flow**
1. The customer places an order.
2. The store accepts it.
3. At handover, staff type the customer's 4-digit code (POST …/verify-code).

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| S | Guessing a code to collect someone else's order | 4 digits, stored as HMAC bound to the store (`internal/orders/code.go:99-101`). Wrong tries are counted inside the order lock, and the code locks after `codeTries` (5) (`internal/orders/transitions.go:505-526`, test TestBK12). A locked code stays locked (`:505`) | pass |
| S | Brute force through a foreign store's route | The order is loaded `ofStore(storeID)` and authz checks the store's business, so a foreign store or business gets 404/403 and the attempt counter does not move. Tested in TestAbuseOrderCodeForeignRoutesHTTP | pass |
| T | IDOR: accepting, reading or changing another store's order | Store-scoped load and business scope in authz. Tested in TestAbuseIDORSweepHTTP | pass |
| I | Code leaking | Stored sealed (AES-GCM, `internal/orders/code.go:80-96`) and shown only to the owning customer. The customer app keeps it in the query cache only (customer/CLAUDE.md W2-09) | pass |
| R | Disputed handover | The collect writes the actor and the payment method (`internal/orders/transitions.go:527`) | pass |
| D | Locking a victim's order on purpose (5 wrong codes) | Needs a staff session at that store. The lock is flagged to risk through `trg_orders_code_lockout` | accepted |
| E | Cashier acting outside their store | `m.StoreIDs` check (`internal/platform/authz/engine.go:159`). Tested in TestAbuseIDORSweepHTTP and TestAbuseRoleEscalationHTTP | pass |

## 3. Credits/wallet and payouts

`internal/credits` and `internal/referrals` are being changed on a pending branch. This review covers them at the API boundary only, and their files were not edited.

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| T | Double spend of credits | Hold, capture and release touch the accounts FOR UPDATE in account id order (`internal/credits/credits.go:9-22`). Spend caps per store per day and per customer per month (`internal/credits/credits.go:79-80`) | pass (boundary) |
| T | Capturing credits on a sale at another store | Bill claim hold checked against customer and store (`internal/credits/fake.go:388` mirrors the DB rule). TestAbuseBillTokenHTTP links S's claim into an S2 sale and checks that no credits are captured | pass |
| S | Referral farming (D-13) | Self-referral is refused, and there is one claim per account. Same-device claims are rejected (`internal/referrals/claim.go:118-135`). **GAP (medium, known):** the device is the `deviceId` the client sends at OTP verify, so a farmer who sends a fresh one per account gets a `pending` referral. What bounds the farm: one OTP-verified phone per account, the per-phone and per-IP OTP caps, and no reward until a qualifying purchase. **GAP (low):** a session with no device row is not checked (TODO(lead) at `internal/referrals/claim.go:125-126`). Tested in TestAbuseReferralFarmingHTTP | open (pending dir) |
| E | Admin paying out money without finance rights | `admin.payout.manage` and `.export` are granted to finance and super_admin only, and need step-up (`internal/platform/authz/policy/payouts.go:22-30`) | pass |
| I | Payee bank/UPI exposure | Masked to the last 4 except in the step-up export, which is audited with ids only (`internal/platform/authz/policy/payouts.go:8-10`) | pass |
| R | Unattributed adjustments | Admin adjustments are audited (`internal/credits/adjustments.go`) | pass (boundary) |

## 4. POS offline sync

**Flow:** the device rings sales offline into an outbox, then POST …/sync applies the entries in `deviceSeq` order.

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| T | Replaying an entry to double-sell | Entries are idempotent per (store, clientId). A bill rung online and retried offline is matched by client id (`internal/pos/sync.go:261-270`). The order is checked, with 500 entries max (`internal/pos/sync.go:27-30`, `:81-84`) | pass |
| T | Device inventing prices or discounts | Prices are kept as sold (02 §4.3). A device offer discount the offers engine would not give at sold_at is flagged (`internal/pos/sync.go:401`, `:449-`) | accepted (design) |
| T | Back-dated or future `sold_at` | **GAP (low):** `e.At` has no lower or upper bound (`internal/pos/sync.go:408`). The clock offset is recorded (`:531`). Reports for a closed day could change | open |
| S | Offline counter code reused | A verified offline code is bound to (device, TOTP step) and used once (`internal/pos/sync.go:581`, `internal/pos/totp.go`). Replay inside the step window from the same device is bounded by that use-once rule | accepted |
| E | Removed cashier's device syncing | Authz with a live membership on every request. Tested in TestAbuseRoleEscalationHTTP (a removed cashier's live token gets 403 on movements) | pass |
| D | Huge outbox | Body limit plus 500 entries per call | pass |

## 5. Bill QR tokens

**Flow**
1. The cashier opens a bill claim, and the API returns a token of id, nonce and HMAC tag, valid 120 s.
2. The customer previews the bill, then confirms with credits.

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| S | Forged or altered token | The tag is checked with `hmac.Equal` against the nonce from the row (`internal/pos/claims.go:60-87`). Any malformed or forged token gets 404 (`:384-`). Tested in TestAbuseBillTokenHTTP | pass |
| T | Replay or race of a confirm | Row is FOR UPDATE, so the first confirm wins and every later one gets `claim_already_used`, whoever sends it (`internal/pos/claims.go:416-421`, `:493-537`). Tests TestPOS09, TestPOSBillQRSale and TestAbuseBillTokenHTTP | pass |
| T | Linking a claim into another store's sale | Refused, and no pos_sales row and no captured credits. Tested in TestAbuseBillTokenHTTP | pass |
| I | Token in logs or URLs | Access log has no query. The receipt link token is hashed (`internal/pos/receipts.go:29`) | pass |
| D | Exhausting claims | 120 s TTL (`internal/pos/claims.go:31`) | pass |

## 6. Admin and step-up

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| S | Admin sign-in with OTP only | Accepted by the founder (D-264, 27 Oct): admins sign in with the SMS OTP alone; the authenticator (TOTP) is gone. Whoever controls the admin's number (SIM swap, SMS interception) can sign in and pass step-up. Mitigations: only a super_admin creates staff, every † action is audited with a reason, the last active super_admin cannot be disabled or demoted, a disabled admin is refused on the next request | known |
| S | Admin sign-in code guessing across many requests | Besides 5 tries per code, wrong admin-app sign-in codes are counted per number across requests: the 10th inside 24 h (`admin_signin_max_failures`, `admin_signin_failure_window_hours`) locks that number's admin sign-in for 24 h (`admin_signin_lock_hours`); sends and verifies answer 429 `signin_locked` while it holds (`internal/identity/signin_limits.go:60`, `:81`; `internal/identity/otp.go:115`, `:241-290`). A good code clears the count. Kept for every number tried in the admin app, admin or not, so it reveals no admin-ness. Another super_admin unlocks on A-18 (step-up, audited; `internal/admin/users.go:308`). Tested in `internal/identity/signin_limits_test.go` (TestAdminSigninLock) and `cmd/todayz/admin_signin_lock_http_test.go` | pass |
| D | Locking an admin out on purpose (10 wrong codes to their number) | Anyone who knows the number can lock that admin's console sign-in for a day. Accepted by the founder (D-264, 27 Oct): another super_admin unlocks at once, and customer and business sign-in are not affected. If every super_admin is locked out, the operator runs the break-glass `todayz admin unlock-sign-in` on the ops task (`backend/cmd/todayz/admin_cli.go`; deploy RUNBOOK §8): the number comes from a file or an override variable, never argv or a log, and the audit row has the system actor. Only a super_admin may change the four limits (`internal/admin/settings.go`, `superOnly`), so a plain admin cannot loosen or tighten them | accepted |
| S | Step-up code guessing | Codes are HMAC-bound to the request and compared in constant time (`internal/identity/stepup.go:154`), 5 tries per request; the fifth wrong code revokes every admin session of the person and is audited `identity.step_up_locked` (`internal/identity/stepup.go:171-178`). Sends have their own bucket: 30 s between step-up codes and `admin_step_up_sends_per_hour` (10) per admin an hour, apart from the sign-in caps (`internal/identity/signin_limits.go:116`). Verify refuses a login-purpose request (TestVerifyStepUpRefusesLoginRequest); two verifies of one good code stamp once (TestConcurrentStepUpVerify); `/v1/auth/step-up/*` take the bearer only, never the refresh cookie (TestAdminStepUpIgnoresCookieHTTP) | pass |
| E | † actions without a fresh step-up | An SMS step-up within `StepUpWindow` (10 min) is required (`internal/platform/authn/principal.go:95`, `internal/platform/authz/engine.go:180`, `internal/platform/authz/admingate.go:78`); the code goes only to the number on the admin's account (`internal/identity/stepup.go` SendStepUp). Tested in `internal/identity/stepup_test.go` (TestAdminSMSStepUp, TestAdminStepUpWindow) and `cmd/todayz/identity_admin_http_test.go:37` | pass |
| E | Role creep | Roles are read live from `admin_users`. A disabled admin's session is inactive (`internal/platform/authn/authenticator.go:16-28`) | pass |
| R | Admin actions not attributable | Audit log entries on money, KYC, blocking and payouts (D-83) | pass |
| I | Support agent names leaking to customers | The customer sees "TodayZ support" (customer/CLAUDE.md W3-08). Tickets are scoped by business or customer (`internal/support/biz.go:61`, `:215`, `internal/support/me.go:61`, `:197`) | pass |

## 7. Customer Pages Function `/x/{code}` (share previews)

**Flow**
1. A bot or browser requests /x/{code} on app.todayz.in.
2. The Function calls GET /v1/public/share-cards/{code} and returns HTML with Open Graph tags and a meta refresh to the item.

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| T | XSS through item or store names in the OG tags | Every interpolated value goes through `esc()`, which escapes the five HTML metacharacters (`customer/functions/x/[code].ts:41-43`, `:51-72`). The code must match `^[0-9A-Za-z]{10}$` (`:21`, `:95`) | pass |
| T | Open redirect | The target is built from `storeSlug`/`storeItemId` with `encodeURIComponent` and is always same-origin (`customer/functions/x/[code].ts:23-25`). The no-API 302 goes to `/x/{code}?app=1` (`:145`) | pass |
| I | SHARE_PREVIEW_KEY leaking to another host | `redirect: 'manual'`, so an API redirect is never followed with the key (`customer/functions/x/[code].ts:102-105`). The key is never logged | pass |
| S | Spoofed X-TZ-Preview-IP to dodge the read limit | The API counts the forwarded IP only when the key matches (`internal/share/preview.go:43`). Without the key it counts Cloudflare's IP. **GAP (info):** the key is optional, and a missing key only logs a warning (`customer/functions/x/[code].ts:147-148`) | open (info) |
| T/I | Function responses lack the app's hardening headers (Pages `_headers` do not apply to Function responses) | **Fixed in W4-03:** `HARDENING_HEADERS` (a deny-all CSP, nosniff, DENY, referrer policy, HSTS) on the preview page and the 302 (`customer/functions/x/[code].ts:27-39`, `:110`, `:145`). Tested in `customer/src/test/asks.test.tsx` | fixed |
| T | Clickjacking or script injection on the PWAs | **GAP (medium):** the customer, business and admin Pages apps send no CSP (`scripts/deploy/pages.sh:29-45`). X-Frame-Options DENY is set. The site has a CSP (`site/scripts/headers.mjs`) | open |

## 8. AI drafts

**Flow:** a model output becomes an `ai_drafts` row. A person confirms it line by line, and `Apply` writes through the owning domain with the confirmer's permissions.

| STRIDE | Threat | Control | Status |
|---|---|---|---|
| E | Model output changing stock, money or orders directly | Drafts only. `Confirm` needs `Apply` and `By`, and `Apply` re-checks every line against the current state and the confirmer's role (`internal/ai/drafts/drafts.go:3-21`, `:250-252`) | pass |
| E | Confirming another store's or customer's draft | Store drafts check `d.Store == store` (`internal/ai/retail/retail.go:278`, `:294`, `internal/voice/confirm.go:64`). Customer drafts check `d.Store == 0 && d.Actor == caller` (`internal/ai/assist/assist.go:35`, `internal/ai/listparse/listparse.go:133`) | pass |
| I | PII sent to the model | `internal/ai/scrub` removes personal data first (11 §1 rule 6, `internal/ai/scrub/scrub.go:1`) | pass |
| T | Prompt injection through item names or reviews | The output is a schema-checked draft (`internal/ai/schema`), and a person confirms it. The guard (`internal/ai/guard`) gates the providers | accepted (design) |
| D | Cost exhaustion | AI features sit behind flags and quotas (D-231, D-235). Not re-measured here | not verified |

---

## Gaps at a glance

| Sev | Gap | Owner |
|---|---|---|
| medium | No CSP on the customer, business and admin Pages apps (`scripts/deploy/pages.sh:29-45`) | lead (deploy) and app owners |
| medium | `Permissions-Policy: microphone=()` on the Pages apps blocks the shipped voice features (customer C-23, business voice) in prod (`scripts/deploy/pages.sh:39`). A functional break found during the header review. Fixed: `microphone=(self)` (lead, 26 Oct) | lead (deploy), fixed |
| medium (known) | Referral same-device rule trusts a client-chosen deviceId | pending referrals branch |
| low | Generic rate limiter: in-memory, off locally, numbers TODO(lead) | lead |
| low (W5) | CF-Connecting-IP trusted from any peer; needs the ALB security group | infra W5 |
| low | POS `sold_at` has no bounds | POS owner |
| low | Referral check skips sessions with no device row | pending referrals branch |
| info | SHARE_PREVIEW_KEY optional; API sends no HSTS itself (Cloudflare does) | lead |
