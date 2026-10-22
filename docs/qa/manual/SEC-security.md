# SEC Security

Hands-on security checks a tester can do with a browser, DevTools and cURL: sign-in limits, sessions and tokens, object-level access (IDOR), codes, injection in text fields and exports, headers, request limits and the test-only routes. `make security` (ZAP, Trivy, checkov, abuse suite) runs the automated half; this file is the manual half.

- **Covers:** AU-02, AU-04 to AU-07, AU-13, AU-16, AU-17, AU-19, AU-20; BK-04, BK-12, BK-38; POS-09, POS-10, POS-13, POS-22, POS-25; PL-01, PL-02, PL-05; R-2.1, R-2.6, R-2.7, R-6.1 to R-6.7.
- **Screens:** C-10, C-12, C-13, C-18, C-24; B-04, B-05; A-06.
- **Devices:** D1280 with DevTools, a terminal for cURL; P360 for the customer.
- **Time:** about 2.5 hours.

## Preconditions

1. `make reset`; `clock reset`. Mocks UI open.
2. Use mock-route numbers `+91 90000 00130` to `…00139` where a step needs a real OTP (README §2).
3. Never paste a token, cookie, OTP or code into the run file. Record status, `code` and header names only.

## Steps

### A. OTP and sign-in

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| SEC-M.01 ★ | Sign in with `+91 90000 00130`. Enter a wrong code 5 times, then the right code from the mock. | Attempts 1–5 refused. The 6th (right code) refused with `otp_locked`; the UI asks for a new code. | `R-2.1` AU-02 C-10 `otp_locked` | ▢ | |
| SEC-M.02 | Tap Resend at 29 s, then at 30 s. | At 29 s the button is disabled with a countdown (API `429` with `Retry-After` if called). At 30 s a new code is sent. | `R-2.1` AU-04 | | |
| SEC-M.03 | Send 5 OTPs to `…00131` in one hour (wait 30 s between). Send a 6th. | The 6th is refused (`429`, record the `code`). No SMS in the mock. | `R-2.1` AU-05 | | |
| SEC-M.04 | From one browser (one IP) send OTPs to 20 different mock-route numbers within an hour, then a 21st. | The 21st is refused (`429`). | `R-2.1` AU-05 | | |
| SEC-M.05 | Time `POST /v1/auth/otp/send` for a known number (C10's) and an unknown one (`…00138`), 5 times each. | Same status and body shape for both. Median times within 50 ms. | AU-06 | | |
| SEC-M.06 | In DevTools change the Turnstile token in the send request to `XXXX.DUMMY.TOKEN.FAIL` and resend. | Refused; the Turnstile tab in the mock shows `invalid-input-response`; no SMS sent. The UI shows a retry message, not a blank screen. | `R-2.6` AU-20 | ▢ | |
| SEC-M.07 | Sign in as C15 (blocked). | `account_blocked`; the screen shows a support link. No session cookie set. | AU-17 `account_blocked` | ▢ | |

### B. Sessions and tokens

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| SEC-M.08 | Sign in as C10. In DevTools → Application → Cookies (API host) read the refresh cookie's attributes. | Name `__Secure-tz_rt_c`; no `Domain`; `Path=/v1/auth`; `HttpOnly`; `Secure`; `SameSite=Strict`. Not sent on `/v1/me` requests. | AU-01 D-14 | ▢ | |
| SEC-M.09 | Sign in as C10 on profile A and profile F. On A, copy the refresh request as cURL. Let A refresh once (wait 15 min or reload after `clock +15m`). Then replay the old cURL. | The replay is refused, and both A and F are signed out at their next call (whole family revoked). | `R-2.7` AU-07 | | |
| SEC-M.10 | As C10 signed in on A and F, and in the business app as X25: tap "Sign out of all devices" in C-18. | Both customer sessions end. The business app session stays. | `R-2.7` AU-13 C-18 | | |
| SEC-M.11 | Call `POST /v1/auth/refresh` with `Origin: https://evil.example`, then without the `X-TZ-Client` header. | `403` both. | AU-16 | | |
| SEC-M.12 | Take a valid access token, change one character of its signature, call `GET /v1/me`. Then change the header's `kid` to `qa-unknown`. | `401` both. | AU-19 | | |
| SEC-M.13 | Call `POST /v1/auth/refresh` with `{"app":"business"}` while only the customer cookie exists. | Refused (`401`). The customer cookie never refreshes a business session. | AU-01 D-14 | | |

### C. Object-level access (IDOR)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| SEC-M.14 ★ | As C11 call `GET /v1/me/orders/{C10's order id}`, then cancel it, then `POST .../new-code`. | `404` for each (or `403`; record which, and that it is the same for an id that does not exist). | AU-09 | | |
| SEC-M.15 | As O26 (Kaveri Fresh) call `GET {S}/orders/{Sri Lakshmi order id}` using Kaveri Fresh's own `{B}` and `{S}` in the path. | `404`: an order of another store under your own path is not found. | AU-09 | | |
| SEC-M.16 | As K22 read `GET {S}/orders/{id}` and the SSE stream `{S}/stream` for an accepted pickup order. Search both for the 4-digit code shown on C-12. | The code is not in any business response, list, SSE event or push. | `R-6.1` BK-38 D-04 | | |
| SEC-M.17 | As K22 open `{S}/stream` for Sri Lakshmi 2 (another store of the same business). | `403`. | AU-09 `R-3.1` | | |
| SEC-M.18 | Open a receipt link `/r/{token}` from a POS sale. Change one character of the token. Then try 20 random tokens quickly. | Real token: store, items, totals, no customer name, phone, wallet or link method in the body or headers. Changed token: "This receipt link has expired" with the shop link, same timing as an expired one. The 20 quick tries hit a rate limit (`429`). | POS-25 D-45 C-24 | ▢ | |

### D. Codes

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| SEC-M.19 ★ | Accepted pickup order for C10. K22 enters 4 wrong codes, then the right one. | Collected. | `R-6.1` BK-12 | | |
| SEC-M.20 | Another order. K22 enters 5 wrong codes. Then the right one. | After 5: `code_locked`; B-04 shows "Ask customer for a new code". The right code is refused. C10 taps New code on C-12; the new code is accepted, the old one is not. | `R-6.1` BK-12 `code_locked` C-12 | ▢ | |
| SEC-M.21 | Open a bill QR on the POS. Scan it with C10 on profile A and C11 on profile F. Both tap Confirm. | The first confirm wins. The second sees "already claimed" (record the code). | `R-6.3` POS-09 | | |
| SEC-M.22 | Decode the bill QR payload. Change the amount and submit the claim via cURL. | Refused: signature invalid. Nothing linked. | `R-6.3` POS-10 | | |
| SEC-M.23 | At the POS pick counter code, type C10's phone and 5 wrong codes on one bill. | Locked for that bill; the right code is refused on that bill and accepted on a new bill. | `R-6.4` POS-13 | | |
| SEC-M.24 | Offline POS (DevTools Offline), counter code sale for C10 with the right code. Use the same code again on a second offline sale. Go online. | First links. Second shows "Customer link failed" (reuse refused); the sale is kept, unlinked, no ledger entry. | `R-6.7` POS-22 | ▢ | |
| SEC-M.25 | Offline counter-code sale; `clock +11h59m`; sync. Another; `clock +12h1m`; sync. | First linked. Second "Customer link failed" (`pos_offline_link_max_hours` 12). | `R-6.7` POS-22 | | |
| SEC-M.26 | Phone-only POS sale for C10 (`phone_unverified`). Check C10's wallet and B-10. | No cashback, no ledger line. C10 is added to B-09 only. | `R-6.5` `R-6.6` D-07 | | |

### E. Injection and output encoding

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| SEC-M.27 | C10 posts a review with text `<img src=x onerror=alert(1)>QA` and a ticket with `<script>alert(2)</script>`. M21 creates a product named `"><svg onload=alert(3)>`. Open C-07 reviews, B-11, A-12, A-14, B-07, A-08. | The text shows literally everywhere. No alert. | SC-RV | ▢ | |
| SEC-M.28 | M21 names a product `=HYPERLINK("http://evil.example","x")` and `+cmd|' /C calc'!A0`. Export B-07 items CSV, the B-10 statement CSV and the A-11 payout CSV. Open each in a spreadsheet. | Each such cell starts with `'` (or is otherwise neutralised); no formula runs. | SC-PO | ▢ | |
| SEC-M.29 | Search C-05 for `' OR 1=1 --` and `%00`. | A normal zero-results state. No `500`. | SC-SR | | |

### F. Headers, limits and transport

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| SEC-M.30 | Read the response headers of `https://app.todayz.localhost/`, `https://api.todayz.localhost/v1/discover/summary` and `https://admin.todayz.localhost/`. | Record: `Strict-Transport-Security`, `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy`, frame protection. Admin and business are not frameable. | SC-INF | ▢ | |
| SEC-M.31 | `PATCH {S}/items/{I1}` with `If-Match: "v1"` after M21 has saved I1 twice. | `412`. The UI shows a "changed by someone else" reload prompt. | 04 §1 | | |
| SEC-M.32 | `POST /v1/me/orders` twice with the same `Idempotency-Key` and the same body, then the same key with another quantity. | One order; the second call returns the same response. The third → `422 idempotency_mismatch`. | BK-04 `idempotency_mismatch` | | |
| SEC-M.33 | `POST /v1/rum` with a 2 MB body. | `413`. | SC-INF | | |
| SEC-M.34 | `curl -H 'Host: evil.example' http://127.0.0.1:4010/_mock/sms`. | `421`: the mock answers only its allowed hosts. | e2e/mocks | | |
| SEC-M.35 | Call `GET /v1/me` with no token. | `401` with problem JSON. | PL-02 | | |
| SEC-M.36 | Open `https://api.todayz.localhost/v1/test/clock` (local). In the staging check-list for Wave 5, the same URL must return `404`. | Local: the clock JSON. Record the staging check as **NA** until Wave 5. | `R-2.3` D-80 | | |
| SEC-M.37 | Open `https://api.todayz.localhost/openapi.yaml` (or the bundled spec the build serves) and search for a duplicate method + path, and for any path outside the namespaces in 04 §1.1. | None. (PL-01 is automated; this is a spot check.) | PL-01 | | |
| SEC-M.38 | Trigger five different errors (C-11 store closed, a wrong code, a cashier 403, a bad OTP, a 412). For each read `code` and the screen message. | Each `code` maps to a translated message, never a raw code or English `detail` on screen. | PL-02 | ▢ | |
| SEC-M.39 | As SA48 open A-17 → Provider keys and A-16. In DevTools search all responses for the first 6 characters of any provider key you know is set (do not paste it anywhere). | Only `set` / `missing`. No match in any response. | PL-05 D-19 | | |
| SEC-M.40 | As C10 copy a valid `POST /v1/me/orders` request from C-11, change the store to Nandi Hardware (pickup and delivery both off) and an NH item, new `Idempotency-Key`, and send it. Also send the quote request. | Both `409 store_not_bookable`; no order row in A-07. The UI never offers the button (MQ-01.37). | BK-43 `R-3.5` D-41 `store_not_bookable` | | |

## Cleanup

1. `clock reset`; `make reset` (clears locks, reviews and products made here).
2. `DELETE https://mocks.todayz.localhost/_mock/reset`.
