# Firebase phone sign-in: set up, switch on, switch back

D-275: when the API runs with `OTP_PROVIDER=firebase`, Firebase Authentication sends and checks the sign-in code for the customer, business and admin apps. TodayZ keeps everything else: users, sessions, refresh cookies, roles, audit. The admin step-up code (D-264) still goes through AWS End User Messaging SMS, so the AWS SMS setup in [aws-founder-steps.md](aws-founder-steps.md) stays in place.

Who does what (D-210):

- **The founder** creates the Firebase project in the Google console (sections 1 to 4) and puts four public values in `.env.cloud.local`.
- **The lead** puts them in the prod Terraform variables and applies, after the founder says yes to the plan summary (section 5).
- Worker sessions do none of this.

## 1. Create the project

1. Go to https://console.firebase.google.com and choose **Create a project**.
   - Name: `todayz-prod`. Note the project ID it shows; it may have a suffix, for example `todayz-prod-1a2b3`.
   - Google Analytics: **off**. TodayZ does not use it.
   - Gemini in Firebase: **off**. The wizard asks about it at creation; TodayZ does not use it.
2. Upgrade to **Blaze** (pay as you go): the gear icon, then **Usage and billing**, then **Details and settings**, then **Modify plan**.
   - Without billing, Google sends no real SMS at all: `sendVerificationCode` answers `400 BILLING_NOT_ENABLED` and the apps show "Something went wrong". The console's "sent SMS daily quota of 10/day" only applies once billing is on. Reserved test numbers still work without billing (section 8).
   - India SMS is billed per message. Check the current price on https://firebase.google.com/pricing.
   - Set a budget alert at the same time: the gear icon, then **Usage and billing**, then **Budget alerts**. Use the `BUDGET_ALERT_EMAIL` address.

## 2. Turn on phone sign-in

1. Open **Build**, then **Authentication**, then **Get started**.
2. Open **Sign-in method**, choose **Phone**, then **Enable**, then **Save**.
   - On some new projects, **Save** first opens **Upgrade to the new authentication**: Firebase Authentication with Identity Platform. Go through its steps and choose the upgrade. Phone stays **Disabled** until you do. The upgrade cannot be undone. Without Blaze it is capped at 3,000 daily active users, which is enough for a test project but not for prod. Then enable Phone again and save. The console confirms "Phone enabled". (`todayz-dev` showed this wizard; `todayz-prod` did not, and Phone saved straight away.)
   - Leave **Phone numbers for testing** empty in this project. Reserved test numbers are a local and test thing only (D-15). A separate test project may have one (section 8).
3. Open **Settings**, then **SMS region policy**. Choose **Allow**, pick **India** only, and save. A code is then never sent to a number outside India.
   - This step is required. Until a region is allowed, every send fails with `OPERATION_NOT_ALLOWED` ("SMS unable to be sent until this region enabled by the app developer"), and the apps show the generic "could not send" error.
4. Open **Settings**, then **Authorized domains**:
   - Add `app.todayz.in`, `business.todayz.in` and `admin.todayz.in`.
   - Remove `localhost`. Local work uses the mock (section 7), not this project.
5. Open **Settings**, then **User account management** (or **User actions**). Leave **Enable create (sign-up)** on. Firebase must create a user for each new phone, and TodayZ ignores those users.

reCAPTCHA needs no step. The apps use Firebase's invisible reCAPTCHA, and Firebase creates its keys. **reCAPTCHA SMS defense** (Enterprise, under **Settings**, then **Fraud prevention**) is optional and billed separately. Leave it off unless SMS fraud shows up in the Firebase usage page.

## 3. Register the web app and copy four values

1. Open the gear icon, then **Project settings**, then **General**, then **Your apps**. Choose **Add app**, then **Web** (`</>`).
   - Nickname: `todayz-web`.
   - Firebase Hosting: **no**.
2. Firebase shows a `firebaseConfig` block. Copy four of its values into `.env.cloud.local`, one `NAME=value` per line:

   | Name | From `firebaseConfig` |
   |---|---|
   | `FIREBASE_PROJECT_ID` | `projectId` |
   | `FIREBASE_WEB_API_KEY` | `apiKey` |
   | `FIREBASE_AUTH_DOMAIN` | `authDomain` (`<project>.firebaseapp.com`) |
   | `FIREBASE_APP_ID` | `appId` |

   These four are public. Every page that signs in with Firebase ships them to the browser, so they are not secrets. Still, do not paste them in chat: the file is the one place they live.

The API does not need a service account, a private key or any Admin SDK credential. It checks Firebase ID tokens against Google's public keys.

## 4. Lock the browser key to TodayZ

The web API key works from any site until it is restricted.

1. Open https://console.cloud.google.com/apis/credentials and pick the same project.
2. Open **Browser key (auto created by Firebase)**.
3. Under **Application restrictions**, choose **Websites** and add:
   - `https://app.todayz.in/*`
   - `https://business.todayz.in/*`
   - `https://admin.todayz.in/*`
   - `https://<project>.firebaseapp.com/*`, the auth domain: the SDK's helper frame there calls Identity Toolkit with the same key
4. Under **API restrictions**, choose **Restrict key** and tick:
   - **Identity Toolkit API**
   - **Token Service API**
   - **reCAPTCHA Enterprise API**, if it is listed

   Firebase pre-selects about 25 APIs on its auto-created key. Untick all of them except these.
5. Choose **Save**. A change can take a few minutes to apply. The credentials list then shows the key as "HTTP referrers, 2 APIs".

## 5. Switch prod to Firebase (lead)

1. Run `make env-check`. All four `FIREBASE_*` names should show as set.
2. In the prod variables for `infra/terraform/envs/prod`, add the values to `extra_properties`:

   ```hcl
   extra_properties = {
     OTP_PROVIDER         = "firebase"
     FIREBASE_PROJECT_ID  = "..."
     FIREBASE_WEB_API_KEY = "..."
     FIREBASE_AUTH_DOMAIN = "..."
     FIREBASE_APP_ID      = "..."
   }
   ```

   - Do not set `FIREBASE_JWKS_API_URL` or `FIREBASE_AUTH_EMULATOR_URL`. The API refuses to start in staging or prod with either one set.
   - With `OTP_PROVIDER=firebase` and any of the four missing, the API refuses to start and names the missing variable. A forgotten value cannot quietly lock everyone out. The AWS SMS and Turnstile values stay required too, because the step-up and the fallback use them.
3. Run `TODAYZ_WAVE5=1 scripts/ci/terraform.sh plan` for prod. Show the founder the plan summary: only the Beanstalk environment properties should change. Apply only after the founder's yes. Beanstalk then restarts the API and the worker with the new properties.
4. No app rebuild is needed. The apps read the provider and the web config from `GET /v1/auth/config` each time the sign-in screen opens. The Pages CSP (report-only) already allows Google's reCAPTCHA and Identity Toolkit hosts (`scripts/deploy/pages.sh`).

### Check it

1. Run `curl -s https://api.todayz.in/v1/auth/config`. It should answer `"otpProvider":"firebase"` and a `firebase` object with the four public values.
2. Sign in on https://app.todayz.in with your own phone. The SMS comes from Google's sender with Google's text (accepted, D-275). The app opens signed in.
3. Do the same on https://business.todayz.in and https://admin.todayz.in.
4. On the admin console, try a † action. The step-up code still arrives through AWS SMS with the DLT template.
5. In Grafana (Logs), find the API lines for `POST /v1/auth/firebase/verify`. They should be 200s.
   - `401 otp_expired` means the phone sign-in was more than 5 minutes old when the app swapped it, or the same sign-in was swapped twice.
   - `401 unauthorized` means the token failed a check: signature, project, expiry, or not a phone sign-in. Many of these after the switch point to a wrong `FIREBASE_PROJECT_ID`.
   - `503 service_unavailable`, with "Firebase public keys unavailable" logged, means the API could not fetch Google's keys.

## 6. Switch back to AWS SMS

Use this when Firebase is down, SMS delivery in India is bad, or the bill looks wrong.

1. Set `OTP_PROVIDER = "sms"` in `extra_properties`, or remove it (sms is the default). The `FIREBASE_*` values can stay.
2. Plan and apply. Once the lead holds the founder's standing approval for this switch, it may skip the plan review. Otherwise follow step 5.3.
3. Check that `curl -s https://api.todayz.in/v1/auth/config` answers `"otpProvider":"sms"`.

During the switch:

- Anyone halfway through a sign-in gets `409 sign_in_method_changed` from the API. The screen says the sign-in method changed and goes back to the phone step.
- People who are already signed in stay signed in: sessions are TodayZ's own.
- A person keeps their TodayZ account across the switch, because the account is keyed by the phone number, not by the Firebase user.

Switching to Firebase again is section 5 with `OTP_PROVIDER = "firebase"`.

## 7. Local and test

`infra/local/local.env` points the API at the mocks server's Firebase stand-in:

- `FIREBASE_PROJECT_ID=todayz-local`
- `FIREBASE_JWKS_API_URL=http://127.0.0.1:4010/firebase/jwks`
- `FIREBASE_AUTH_EMULATOR_URL=https://mocks.todayz.localhost`

`OTP_PROVIDER` is not set in `local.env`, so local sign-in is the AWS SMS mock as before.

To try the Firebase path:

1. Put `OTP_PROVIDER=firebase` in `.env.local`.
2. Restart the stack's API.
3. Read the code at https://mocks.todayz.localhost, as for SMS.

No Google host is called, and no reCAPTCHA appears. See [e2e/mocks/README.md](../../e2e/mocks/README.md).

## 8. Live check against a test project

This checks TodayZ's verifier and the swap against a real Firebase project and Google's real keys, without sending any SMS. Use a separate test project, never `todayz-prod`. On 28 Oct 2026, the founder asked for one to be made, `todayz-dev`, on the free Spark plan. It was set up with sections 1 to 3, with these differences:

- no Blaze upgrade;
- `localhost` left in **Authorized domains**;
- one test phone number from the reserved range (`+91 90000 000xx`, code `000000`, D-15), added under **Phone numbers for testing**.

Its four values live in a file outside the repo, never in `.env.local` and never in chat.

1. Get an ID token for the test number. Use the Identity Toolkit REST API with the project's web key:
   - First call `accounts:sendVerificationCode` with the phone number. A test number needs no reCAPTCHA token, and no SMS is sent. It returns a `sessionInfo`.
   - Then call `accounts:signInWithPhoneNumber` with that `sessionInfo` and code `000000`. It returns the `idToken`.
   - Write the token to a file with mode 600. Never echo it.
2. Within 5 minutes of step 1, run:

   ```bash
   cd backend && TODAYZ_FIREBASE_LIVE_TOKEN_FILE=<token file> FIREBASE_PROJECT_ID=<test project id> go test -run '^TestFirebaseLive$' -v ./cmd/todayz/
   ```

   It needs Docker for the test database (as `make check` sets it, see `backend/CLAUDE.md`). `TestFirebaseLive` ([identity_firebase_live_test.go](../../backend/cmd/todayz/identity_firebase_live_test.go)) checks three things:
   - TodayZ's verifier accepts the token against Google's real keys.
   - `POST /v1/auth/firebase/verify` returns a TodayZ session and refresh cookie. In that part, a loopback server hands on Google's keys, because `APP_ENV=test` refuses egress.
   - A second swap of the same token answers `401 otp_expired`.

   Without the variable the test skips, so `make check` never calls Google.

## What TodayZ cannot see on the Firebase path

- **Wrong codes go from the browser to Google.** TodayZ never sees them, so the admin sign-in lock (D-264) counts nothing while the provider is firebase. The founder accepted this. Firebase's own limits apply instead: its per-IP and per-project caps are listed at https://firebase.google.com/docs/auth/limits.
- **Delivery failures are reported in Firebase, not in TodayZ.** The `tz-auth-sms-failures` alert covers only AWS SMS. Watch **Authentication**, then **Usage**, in the Firebase console.
