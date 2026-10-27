# Firebase Auth for the SMS OTP: proposal

The founder asked: "can we integrate firebase auth in this just for SMS OTP".

Yes, it can be done. Firebase would only send the code and check it. TodayZ keeps its own users, sessions, refresh cookies, roles, audit and limits. Once Firebase has checked the phone, the browser gets a Firebase ID token, the API checks that token, and the API signs the person in with the usual TodayZ session. Nothing else in auth changes.

- Status: proposal only. Nothing is built, and no Firebase or Google Cloud project exists. The lead sets up the cloud side after the founder says yes.
- Branch: `claude/firebase-sms-otp-761592` (local, not pushed).
- Today (D-212, D-213, D-264): the API sends a 6-digit code through AWS End User Messaging SMS with a DLT template, stores its HMAC in `otp_requests` and checks it on `POST /v1/auth/otp/verify`. Turnstile guards the send, and the limits come from 01 §2.

## 1. Flow: the client SDK, not the backend REST API

**Recommendation: the Firebase JS SDK in the browser** (`signInWithPhoneNumber` with an invisible `RecaptchaVerifier`). The app then sends the ID token to one new TodayZ endpoint.

```mermaid
sequenceDiagram
    participant U as User
    participant A as App (sign-in route)
    participant FB as Firebase Auth (Google)
    participant API as TodayZ API (identity)
    U->>A: Enter phone
    A->>FB: signInWithPhoneNumber (+ invisible reCAPTCHA)
    FB-->>U: SMS code (Google's sender, Google's text)
    U->>A: Enter code
    A->>FB: confirm(code)
    FB-->>A: Firebase ID token (RS256, 1 h)
    A->>API: POST /v1/auth/firebase/verify {idToken, app, deviceId}
    API->>API: Check signature (Google public keys), aud, iss, sub, exp, iat, auth_time, phone_number, sign_in_provider=phone, used once
    API-->>A: TodayZ access token + refresh cookie (same as otp/verify)
    A->>FB: signOut() (Firebase keeps no session; in-memory persistence)
```

Why not the Identity Toolkit REST API from the backend (`accounts:sendVerificationCode` and `accounts:signInWithPhoneNumber`)?

- **Firebase limits each IP.** The limits are 50 SMS a minute and 500 an hour per IP, and 150 verification requests an hour per IP ([Firebase Auth limits](https://firebase.google.com/docs/auth/limits)). If the backend sent every request, all of them would come from our one NAT gateway IP, so all of TodayZ would get about 500 codes an hour. Once that is used up, Firebase answers `TOO_MANY_ATTEMPTS_TRY_LATER` for everyone.
- **The browser still has to pass reCAPTCHA.** On the web, `sendVerificationCode` needs a reCAPTCHA token from the page. So the browser would load Google's reCAPTCHA anyway, through a flow that Firebase's own docs don't cover.
- **It isn't the documented path.** The SDK flow is what Google supports. The REST route copies SDK internals, and those changed in 2023–24 when reCAPTCHA Enterprise "SMS defense" arrived.

What the backend REST route would have kept, and the SDK route loses: TodayZ would see every send and every wrong code. With the SDK, wrong codes go from the browser straight to Google, so TodayZ cannot count them. That is why the admin app stays on today's path (section 2), and why section 5 has open questions.

## 2. Which sign-ins switch

| Path | Proposal | Why |
|---|---|---|
| Customer sign-in (C-10) | **Firebase** | The main volume of sign-ins |
| Business sign-in (B-01: owner, manager, cashier) | **Firebase** | Same flow, same `PhoneOtpFlow` |
| Admin console sign-in (A-01), field agents included (they use the admin app) | **Stays on AWS SMS** | The D-264 lock counts wrong admin codes per number (10 in 24 h locks sign-in for 24 h). With Firebase, TodayZ never sees a wrong code, so the lock could not work. |
| Admin step-up for † actions (D-264) | **Stays on AWS SMS** | The code must go to the number on the admin's account, it stamps `sessions.step_up_at`, and the fifth wrong code ends the admin's sessions. TodayZ would see none of that with Firebase. |
| Field owner code (A-04, `tz_owner_code`) | **Stays on AWS SMS** | This is not a sign-in. It confirms a shop to a field agent, and TodayZ checks the code itself. |
| Notice SMS (order alerts and the like) | **Stays on AWS SMS** | Firebase only sends verification codes |

**AWS End User Messaging SMS and DLT registration are still needed** for the admin, the owner code and the notices. So Firebase doesn't remove the DLT work. It only takes the customer and business sign-in codes off our DLT template.

## 3. Fallback

**Proposal:** keep the AWS SMS path working, and switch per app with one config value: `OTP_PROVIDER_CUSTOMER` and `OTP_PROVIDER_BUSINESS`, each `firebase` or `sms`. The API publishes the active provider on a public, cacheable `GET /v1/auth/config`, and the sign-in screen reads it. If Firebase is down or over quota, the lead flips the value and redeploys (or, if the founder prefers, an admin changes it as a setting). Nothing fails over by itself: if failing over were automatic, someone could force it just to make us send through the other provider.

## 4. Effects on the apps

- **Offline POS:** none. Sign-in is online only, as it is today. Once signed in, the counter and sync run on the TodayZ session, and Firebase holds no session (`inMemoryPersistence`, then `signOut()` straight after the exchange). A lost connection after sign-in behaves exactly as it does now.
- **Bundle:** `firebase/app` + `firebase/auth` (phone only, modular) is about 45–60 KB gzip; the build will measure it. It loads only in the sign-in route's lazy chunk, so the initial JS budget (customer 170 KB, `.size-limit.js`) doesn't change. reCAPTCHA (`www.google.com/recaptcha/api.js`, about 150 KB+) loads only when the person taps "Send code".
- **Lighthouse:** the home and search pages are unaffected. The sign-in page loses some performance points while reCAPTCHA loads, after the tap and not on first paint.
- **CSP** (`scripts/deploy/pages.sh`, report-only today): add to `script-src` `https://www.google.com/recaptcha/` and `https://www.gstatic.com/recaptcha/`; to `frame-src` `https://www.google.com/recaptcha/` and `https://recaptcha.google.com/recaptcha/`; to `connect-src` `https://identitytoolkit.googleapis.com`. For the customer and business apps only; the admin app's CSP stays as it is.
- **Turnstile:** on the Firebase path, Firebase's reCAPTCHA protects the send, and Turnstile isn't used. Running both bot checks on one tap would be slow and adds nothing. Turnstile stays on the SMS fallback and the admin app.
- **Privacy:** reCAPTCHA is a Google service that sees the visitor's browser signals, and Google sees the phone number and sends the SMS. The privacy page (16 §, the site) needs a line saying so. The founder should confirm that this is acceptable.
- **SMS text and WebOTP:** Google writes the text, roughly "123456 is your verification code for <app>", in the language the SDK sets (`auth.languageCode`; Hindi and the other Indian languages are supported). We can't change it, and it carries no WebOTP `@app.todayz.in #code` line, so the WebOTP autofill (AU-05) won't fire. Android's keyboard suggestion of the code still works.

## 5. Limits and rules that change

Firebase controls the send and the check, so these 01 §2 numbers stop applying on the Firebase path and Firebase's apply instead.

| 01 §2 today | Firebase |
|---|---|
| 6 digits | 6 digits |
| Code valid 5 min | Firebase's validity, which we can't configure |
| 5 tries per code, then `otp_locked` | Firebase's own count (not published) |
| Resend after 30 s | The screen still waits 30 s; Firebase only enforces its own limits |
| 5 sends per number an hour, 20 per IP an hour | Per number: a limit Firebase doesn't publish. Per IP: 50 a minute and 500 an hour. Per project: 3,000 SMS a day (more needs an Identity Platform upgrade) |
| Turnstile | reCAPTCHA (invisible), plus an SMS region policy set to India only |
| Risk rules `identity.otp_ip_fanout`, `identity.otp_failed_phone` (D-224) | No data on the Firebase path; Firebase's console usage logs instead |

TodayZ keeps its side of the exchange:
- The ID token must be fresh. `auth_time` must be within `FirebaseMaxAuthAge` of now (proposed: 5 min, a code constant like the OTP life, D-92).
- Each token works once (replay refused).
- A blocked account is refused, the admin app is refused, sign-in is audited, and the same session and refresh rules apply.

## 6. Cost

- **Firebase** needs the Blaze (pay-as-you-go) plan for phone auth. The first 10 SMS a day are free. After that, India costs about **US$0.07 an SMS (around ₹6)** according to recent price comparisons ([authgear](https://www.authgear.com/post/best-otp-service-providers/), [logto](https://blog.logto.io/it/firebase-authentication-pricing)). The [official pricing page](https://cloud.google.com/identity-platform/pricing) should be checked by the lead before deciding, because I could not read the India row from here.
- **AWS End User Messaging (today):** Indian DLT transactional SMS typically costs ₹0.15–0.45 a message. The exact AWS India price is still a D-213 TODO.
- **What that means:** Firebase is about **15–40 times** the price per code. At 1,000 sign-ins a day that's roughly ₹6,000 a day (about ₹1.8 lakh a month), against a few thousand rupees a month on AWS. The 30-day refresh cookie keeps sign-ins rare per person, but a resend costs again.
- What Firebase gives for the money: no DLT template for the sign-in code, Google's sender routes and delivery, and Google's bot defence. Google's carrier-based "Phone Number Verification" (May 2026, no SMS) works only on Android with partner carriers, so it isn't a fit for a PWA today.

## 7. Local dev and tests

`make check` and `make e2e` run under `scripts/no-egress.sh`, so nothing in them can reach Google.

- **Recommendation: a Firebase Auth mock in the mocks server** (`e2e/mocks`, port 4010, `mocks.todayz.localhost`). It speaks the subset of the Auth emulator's REST API that the SDK uses in emulator mode: `accounts:sendVerificationCode`, `accounts:signInWithPhoneNumber`, `accounts:lookup`, and the emulator's `GET /emulator/v1/projects/{id}/verificationCodes`.
  - It records the code in the SMS capture, so `/_mock/sms/latest-otp` works as it does now.
  - It signs ID tokens with a fixed **local-only** RS256 key and serves its public JWK.
  - The apps call `connectAuthEmulator` with `VITE_FIREBASE_AUTH_EMULATOR_URL`, and the API reads keys from `FIREBASE_JWKS_API_URL`, which is the mock locally.
  - The backend checks a local token with **the same code as prod**: real RS256 signature, `aud`, `iss`. There is no separate branch that accepts unsigned tokens.
  - Reserved test numbers keep working: the mock accepts `000000` for `+91 90000 000xx`.
- **Why not the real `firebase-tools` emulator:** it issues unsigned (`alg: none`) tokens, so the backend would need a local-only path that accepts unsigned tokens. That is the kind of switch that ends up in prod by accident. It also adds another Node process to `make dev` on a 16 GB Mac. Anyone who wants it can still run it by hand against the same `VITE_FIREBASE_AUTH_EMULATOR_URL`. It works for the app, but not with the API, which keeps checking signatures.
- **Unit tests:** Go tests sign tokens with a test key and cover a good token, the wrong project (`aud`/`iss`), an expired token, a stale `auth_time`, a missing `phone_number`, a non-phone provider, an unknown `kid`, a bad signature, and a replay. App tests stub the `firebase/auth` module.

## 8. Config and secrets (names only)

| Name | Where | Secret? | What |
|---|---|---|---|
| `FIREBASE_PROJECT_ID` | API | No | The expected `aud` and `iss` (`https://securetoken.google.com/<id>`) |
| `FIREBASE_JWKS_API_URL` | API, local and test only | No | Override for the key URL (the mock). Refused when deployed, as `SMS_API_URL` is (D-213); deployed builds use Google's published `securetoken@system.gserviceaccount.com` JWK URL. |
| `OTP_PROVIDER_CUSTOMER`, `OTP_PROVIDER_BUSINESS` | API | No | `firebase` or `sms` (section 3) |
| `VITE_FIREBASE_API_KEY`, `VITE_FIREBASE_AUTH_DOMAIN`, `VITE_FIREBASE_PROJECT_ID`, `VITE_FIREBASE_APP_ID` | Customer and business builds (`infra/cloudflare/pages.env`) | **No.** Firebase web config is public by design and ends up in the bundle. The API key gets restricted by HTTP referrer and to the Identity Toolkit API in the console. | SDK init |
| `VITE_FIREBASE_AUTH_EMULATOR_URL` | Local Vite only | No | Points the SDK at the mock |

- **No service-account key.** Checking an ID token needs only Google's public keys, cached as their `Cache-Control` says. The Firebase Admin SDK isn't used, so no private key is stored anywhere, and Secrets Manager stays as it is.
- The JWK fetch goes through `httpx.NewOutboundClient` (the egress guard), and `TODAYZ_NO_EGRESS` refuses it, so `make check` stays offline.
- Nothing logs a phone, a code or a token. The audit row carries the Firebase `sub` hash, never the token.

## 9. Questions for the founder

1. **Cost.** Firebase is about ₹6 a code against well under ₹1 on AWS, roughly 15–40 times as much. Is that worth it for Google's delivery and not needing DLT for the sign-in code? Or would you rather keep AWS SMS and use Firebase only as a backup?
2. **Which apps.** Customer and business on Firebase, with admin sign-in, admin step-up, field agents and the owner code staying on AWS SMS (section 2)? Or a different split, for example the customer app only?
3. **Limits.** On the Firebase path, Firebase's own limits replace 01 §2's (5 sends per number an hour, 20 per IP an hour, 5 tries, 5-minute codes). Is that acceptable? The alternative is the backend REST flow, which keeps our limits but caps all of TodayZ at about 500 codes an hour (section 1).
4. **Fallback.** Keep AWS SMS as a manual fallback, chosen per app (section 3)? If so, should switching be a deploy-time setting the lead flips, or an admin setting in the console?
5. **Bot check.** Use reCAPTCHA (Google) instead of Turnstile on the Firebase path? And may the privacy page say that Google sees the phone number and runs reCAPTCHA?
6. **Freshness.** A Firebase sign-in has to reach TodayZ within 5 minutes of the code being checked, and each sign-in works once. Is 5 minutes right?
7. **SMS text.** Google's text ("… is your verification code for …") replaces ours, and WebOTP autofill stops working on Android Chrome. Is that acceptable?
