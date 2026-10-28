# D-275 Firebase phone sign-in: report

The founder asked: "can we integrate firebase auth in this just for SMS OTP".

What we built: Firebase Authentication can now send and check the sign-in code for the customer, business and admin apps. TodayZ keeps its own users, sessions, refresh cookies, roles and audit. A deploy setting, `OTP_PROVIDER`, picks the path: `sms` is today's AWS SMS path and the default, `firebase` is the new one. The reasoning and the founder's answers are in [PROPOSAL.md](PROPOSAL.md) and in D-275 in the [decision log](../../plan/12-decision-log.md).

- **Branch:** `claude/firebase-sms-otp-761592`, local, not pushed.
- **Commits:**
  - `3f7ebd09` is the backend.
  - `d8857496` has the frontend, the mock, the config and the docs.
  - The next commit adds the live check against a real test project.
- **Cloud:**
  - The lead's brief said not to create a Firebase project. On 28 Oct 2026 the founder told this session directly to create one for testing.
  - So a test project, `todayz-dev`, now exists in the founder's Google account. It is on the free Spark plan, with no billing.
  - The existing InstaHunter project was not touched.
  - `todayz-dev` is not the prod project. Prod still needs `todayz-prod`: the founder creates it, and the lead applies the prod properties after the founder's yes (D-210). Both are in [the runbook](../../runbooks/firebase-phone-auth.md).

## What was built

| Part | What |
|---|---|
| API | `GET /v1/auth/config`: public, `no-store`, gives the provider plus the public Firebase web config.<br>`POST /v1/auth/firebase/verify`: takes a Firebase ID token and returns the normal TodayZ access token and refresh cookie. |
| Token checks | `internal/platform/firebaseauth` checks:<br>• an RS256 signature against Google's keys, cached by `Cache-Control` (clamped to 1 minute to 24 hours);<br>• `aud` and `iss` match the project;<br>• `exp` and `iat` hold, allowing for clock skew;<br>• `sign_in_provider` is `phone`;<br>• `auth_time` is at most 5 minutes old (founder). |
| Single use | `firebase_sign_ins` (migration 49910) keeps an HMAC of the Firebase user and the `auth_time`. A second swap answers `401 otp_expired`. `identity_purge` clears the expired rows. |
| Switching | While the provider is firebase, `otp/send` (login) answers `409 sign_in_method_changed`, and so does `firebase/verify` while it is sms. The screen then starts again on the right path. |
| Config | `OTP_PROVIDER`, `FIREBASE_PROJECT_ID`, `FIREBASE_WEB_API_KEY`, `FIREBASE_AUTH_DOMAIN` and `FIREBASE_APP_ID`. Deployed, the API refuses to start when the provider is firebase and one of the four is missing.<br>`FIREBASE_JWKS_API_URL` and `FIREBASE_AUTH_EMULATOR_URL` are local and test only, and refused in staging and prod. |
| Apps | `shared/auth`:<br>• `PhoneOtpFlow` reads the config.<br>• On firebase it lazy-loads the SDK (`@firebase/app` 0.16.3 and `@firebase/auth` 1.13.7, about 28 KB gzip in its own chunk), signs in with an invisible reCAPTCHA and in-memory persistence, swaps the token, then signs out of Firebase.<br>• Errors are reduced to a kind.<br>The new copy is `auth.error.methodChanged` and `auth.firebase.recaptchaNotice` (Google's reCAPTCHA notice). Customer and business have it in en, hi, kn, te and ta; admin has it in en. |
| Admin | Admin sign-in can use Firebase. The step-up code always goes through AWS SMS (founder). |
| Privacy and CSP | The privacy page now says Firebase Authentication and reCAPTCHA receive the number and device signals.<br>The Pages CSP (report-only) allows Google's reCAPTCHA, Identity Toolkit and Secure Token hosts. |
| Local and test | `e2e/mocks` stands in for Identity Toolkit (`recaptchaConfig`, `sendVerificationCode`, `signInWithPhoneNumber`, `lookup`) and serves `/firebase/jwks`. Codes show in the mocks UI like SMS. `infra/local/local.env` points both sides at the mock.<br>`OTP_PROVIDER` stays unset locally, so the default path is unchanged. |

## Checks run (27 Oct, this worktree)

| Check | Result |
|---|---|
| Backend Firebase tests with a database (testcontainers): `TestFirebaseSignIn`, `TestFirebaseRefusals`, `TestFirebaseAdminSignIn`, `TestPurgeFirebaseSignins`, `TestPreflightFirebase`, `TestFirebaseVerifyHTTP`, `TestRoutesAreInTheContract`, `TestUpDownUp`, `TestMigrationFileNames` | pass |
| `go test -short` on `firebaseauth`, `config`, `identity` | pass |
| `e2e/mocks`: `go vet`, `go test ./internal/server/` (the Firebase stand-in, CORS, logs carry no phone or code) | pass |
| `@todayz/auth` (87), `@todayz/api-client` (42), customer (218), admin (295), business (563) vitest | pass |
| lint and typecheck: auth, api-client, customer, business, admin | pass |
| Initial JS (size-limit, `build:e2e`): customer 146.2 / 170 KB, business 180.1 / 250 KB. Firebase is only in the lazy `firebase-sdk-*.js` chunk in all three apps and is never preloaded | pass |
| `infra/local/test/static.sh` (local.env rules) | pass |
| `make check-docs`, `make check-runbooks` | pass |
| `pnpm install --frozen-lockfile --offline` | pass |

Not run:

- The full `make check`.
- A run against the live local stack (`make dev` is the main checkout's).
- Playwright.

## Live check against `todayz-dev` (28 Oct)

`todayz-dev` was set up as runbook sections 1 to 3, with no Blaze plan, `localhost` left in, and one reserved test number (D-15). Its four public values live in a file outside the repo.

| Step | Result |
|---|---|
| Identity Toolkit REST: `sendVerificationCode` for the test number | 200, no SMS sent |
| `signInWithPhoneNumber` with `000000` | 200, a real Google-signed ID token |
| `TestFirebaseLive`: TodayZ's verifier against Google's real keys (`project`, `iss`, `aud`, `phone`, `auth_time`) | pass, token 2 s old |
| Same test: `POST /v1/auth/firebase/verify` on a fresh database | 200, TodayZ access token and refresh cookie |
| Same test: the same token swapped again | 401 `otp_expired` |
| `TestFirebaseLive` without the variable (as in `make check`) | skipped |
| `golangci-lint run ./cmd/todayz/` | 0 issues |

What the console showed that the runbook did not say before (now fixed in runbook sections 1 and 2):

- Enabling Phone on a new project first needs the Identity Platform upgrade. It cannot be undone, and without Blaze it is capped at 3,000 daily active users.
- Until the SMS region policy allows India, every send fails with `OPERATION_NOT_ALLOWED`.
- The project wizard asks about Gemini; leave it off.

Not checked live:

- the browser SDK flow with a real invisible reCAPTCHA;
- a real SMS to a real phone, which needs Blaze.

The prod check in runbook section 5 covers both.

## Findings and follow-ups

1. **The admin sign-in lock sees no wrong codes on the Firebase path.** Wrong codes go from the browser to Google, so D-264's "lock after 10 wrong" counts nothing while the provider is firebase. An existing lock still refuses the swap. The founder accepted this ("accept Firebase's limits"). Firebase's per-IP and per-project limits apply instead.
2. **The SMS failures alert does not cover Firebase.** `tz-auth-sms-failures` watches AWS SMS only. Firebase delivery is visible only in the Firebase console's usage page. If the founder wants an alert, it needs the Firebase or Cloud Monitoring side, which is cloud work for the lead.
3. **Every device precaches the Firebase chunk.** It is about 95 KB raw, 28 KB gzip. The service worker's precache holds every chunk (`precache-check.mjs` insists on that), so devices on the SMS path download it once in the background too. The initial load is not affected. If that matters, the chunk could be left out of the precache, with the check changed to allow it.
4. **Lockfile and D-88.**
   - main's lockfile already has a few entries younger than 14 days (rollup and rolldown Android bindings), so any non-frozen `pnpm install` on main fails the D-88 age check today. The founder allowed one install for the two Firebase packages, which are 20+ days old and so meet D-88.
   - The lockfile diff adds the five `@firebase/*` packages. It also carries pnpm's own tidy-up: one orphan `undici@8.12.3` with no dependents is removed, and the `site` importer's vitest and html-validate peer suffixes are rewritten. A frozen offline install passes.
5. **Numbering.** Migration `49910` and decision `D-275` were the next free numbers on this branch. Renumber them at merge if main has used them meanwhile.
6. **Pre-existing failure, not from this change.** `e2e/mocks/internal/ui` `TestTzCSSUpToDate` fails because `tz.css` is stale after the design-token changes on main (fix: `go run ./cmd/gencss` in `e2e/mocks`). This branch does not touch `design/` or the mocks UI.
7. **Local exception.** `infra/local/test/static.sh` refuses `_API_KEY` names in `local.env`. It now lets `FIREBASE_WEB_API_KEY` through, because a Firebase web key is public by design and the local value is a placeholder for the mock.

## Founder and lead steps to go live

1. Founder: sections 1 to 4 of [the runbook](../../runbooks/firebase-phone-auth.md). That means creating the project on Blaze with India-only SMS and the three app domains, then restricting the browser key. The four values go in `.env.cloud.local`; names are in `.env.cloud.example`.
2. Lead: section 5. Add `OTP_PROVIDER=firebase` and the four values to the prod `extra_properties`, show the plan summary to the founder, and apply on their yes. No app rebuild is needed.
3. To switch back: section 6, `OTP_PROVIDER=sms`.
