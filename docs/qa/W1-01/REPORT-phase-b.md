# W1-01 Identity and auth (phase B): QA report

Branch `worktree-agent-a17d03d0a9284659d`, rebased onto `main` at 7bd9b09 (W1-11 merged). Everything ran locally on testcontainers. Nothing touched a cloud account, the shared local stack or the network. The agent could not write report files, so the lead committed this from its hand-back. Phase A's report is `REPORT.md` in this folder; this one covers only what phase B added.

## What was built

- **Contract** (`api/paths|schemas/{auth,me}.yaml`, `api/problems/auth.yaml`; `make gen` in the same change):
  - `POST /v1/auth/logout-all` (`logoutAll`): a cookie route like logout, with the same origin check.
  - `POST /v1/auth/totp/enrol` (`enrolTotp`), `POST /v1/auth/totp/verify` (`verifyTotp`, `purpose: login | step_up`) and `POST /v1/auth/totp/recover` (`recoverTotp`). All three take the admin app's bearer and answer `Cache-Control: no-store`.
  - `DELETE /v1/me` (`deleteMe`): `{otpRequestId, code}`, where the code is an SMS OTP sent with `purpose: step_up`.
  - `OtpVerifyResponse.nextStep` (`totp_enrol | totp_verify`) for the admin app.
  - A new problem code, `code_invalid` (422, `extensions.attemptsLeft`). `step_up_required` (403) was already registered in `auth.yaml`.
- **Schema** (`10103_w1-01_admin_totp.sql`):
  - `admin_users.totp_last_step`: the RFC 6238 step of the last accepted code. A code is accepted only for a later step, so no code works twice.
  - `admin_users.totp_failures`, with a check that it is never negative.
  - A unique index on `admin_recovery_codes (admin_id, code_hash)`.
- **TOTP core** (`internal/identity/totp.go`):
  - RFC 6238 written in-house: HMAC-SHA1, 6 digits, 30 s, ±1 step. Checked against the RFC's test vectors.
  - The secret is 160 random bits, sealed with `pii.Sealer` under `TOTP_ENC_KEY`, with the field bound into the AAD.
  - Codes are compared in constant time.
- **Admin sign-in is OTP plus TOTP.**
  - OTP verify on the admin app issues a token with `amr=otp` and `nextStep`.
  - Slot 11 (`mw/authn.go`) lets an `amr=otp` admin token reach `/v1/auth/*` only. Every `/v1/admin/*` route needs `amr=otp+totp`, else 403.
  - Enrol returns the secret, the `otpauth://` URI and 10 recovery codes, once. The codes are stored as keyed hashes. Enrolling again before the first verify replaces the pending secret and codes; after a verify it is 409.
  - Verify with `purpose: login` marks the session (`totp_verified_at`, which is the step-up time) and returns a new access token with `amr=otp+totp` for the same session id. Refresh keeps the amr while the session stays verified.
  - The `Authenticator` downgrades a token claiming `otp+totp` to `otp` when the session row has no TOTP verification, so the claim alone is never trusted.
- **Step-up.** Verify with `purpose: step_up` refreshes the session's step-up time. It never finishes a sign-in. `authn.StepUpWindow` is 10 min.
- **Recovery codes.** Each is single use. Lowercase and missing dashes are accepted. A used code is consumed in the same transaction that verifies the session. The answer says how many are left.
- **Lockout.**
  - Wrong TOTP and recovery codes count together in `totp_failures`.
  - At 5 (`TOTPMaxFailures` = `OTPMaxAttempts`, `TODO(lead)`) the answer is 429 `otp_locked`, and every admin-app session of that user is revoked. Other apps' sessions are not.
  - A success resets the count.
- **Logout-all** (`Service.LogoutAll`):
  - It authenticates by the refresh cookie; a token rotated within `RotationGrace` also counts.
  - It revokes every live session of that user on that app.
  - It is audited as `identity.sign_out_all` and clears the cookie.
- **`DELETE /v1/me`** (`Service.DeleteAccount`, customer app only, policy `me.delete`):
  1. The step-up OTP is checked like a sign-in code: 5 attempts, and a wrong code is 422 `code_invalid`. Another user's request id, or a login-purpose code, is `otp_expired`.
  2. An active admin is refused with 409 (`TODO(lead)`).
  3. Every `CanDeleteAccount` check runs in a savepoint. A `*problem.Error` refusal is answered as is; the code stays unused and the attempt count commits. Any other error is 500.
  4. Then everything below happens in one transaction; any hook error rolls all of it back:
     - the user is anonymised (name gone, `status = deleted`);
     - the phone identity is deleted, so the number signs up as a new user;
     - addresses are deleted and devices scrubbed;
     - every session is revoked with reason `deleted`;
     - the OTP is consumed;
     - every `OnAccountDelete` hook runs;
     - an audit row is written.
- **Admin gate** (`internal/platform/authz/admingate.go`): role plus fresh step-up, exported for W1-11 and later admin routes. It is wired into W1-11's admin-jobs API in place of `DenyGate`.
- **Admin seed** (`internal/seed/identity.go`, fixture `e2e/fixtures/admins.yaml`):
  - The fixture path is read only from `SEED_ADMINS_FILE`.
  - Unset: the variable's name is logged and no admins are seeded.
  - Set: the seed refuses unless `APP_ENV` is local or test.
  - The fixture holds 10 admins: one enrolled admin per D-64 role, a second finance, a second super_admin, and one unenrolled admin.
  - Secrets are sealed under `TOTP_ENC_KEY`. Without that key the admins are skipped with a warning that names the key.

## Evidence

All commands ran in `backend/` with `DOCKER_HOST` set to the colima socket and the Ryuk override, unless the row says `make`. Every run is after the rebase onto main at 7bd9b09.

| Check | Result |
|---|---|
| `gofmt -l .` (excluding `gen/`) | clean |
| `go vet ./...` | clean |
| `golangci-lint run ./...` | 0 issues |
| `gosec -exclude-generated -exclude-dir=testdata -exclude=G104 ./...` | 0 issues |
| `go test -race -count=1 ./...` | ok, every package, including identity, cmd/todayz, authz, authn, seed, jobs and jobs/adminapi. W1-11's adminapi tests (stub Authenticator, fake gate) pass unchanged. |
| `make check` | Passed: gen, check-repo, check-stack, check-backend (gofmt, vet, golangci-lint 0 issues, `go test -race` all packages ok, gosec, govulncheck: no called vulnerabilities), check-contract, check-mocks, check-frontend, check-i18n-messages, check-ui. Stopped at check-infra: "infra/cloudflare is not initialised; run make setup". The agent could not prefetch providers (no network), and the branch changes no terraform files. `make check-ci` run on its own: ok (226 selftests). check-tf-static needs the same init and was not run. |
| No secrets in the logs | The identity integration tests log through a capture that redacts nothing. They fail if a phone, OTP, access or refresh token, TOTP secret, TOTP code or recovery code appears in it. The seed test checks that no fixture TOTP secret or phone is logged. |

### What the tests cover

- **AU-11, admin TOTP enrol and verify** (`TestAdminTOTPEnrolAndVerify`):
  - The secret is stored sealed and the recovery codes hashed.
  - A pending enrolment can be replaced; an enrolled admin gets 409.
  - A step-up verify never finishes a sign-in.
  - A login verify gives `otp+totp` on the same session id.
  - A replayed code is refused.
  - Refresh keeps the amr, and the next sign-in starts at `totp_verify`.
  - A customer token gets 403.
- **AU-12, step-up expiry** (`TestAdminStepUp`, `TestAdminGate`):
  - 10 min + 1 s later, the gate answers `step_up_required`.
  - A step-up verify makes it fresh again.
  - An OTP-only token gets 403.
- **Recovery codes** (`TestAdminRecoveryCodes`): lowercase and no-dash input work, each code works once, and the count left goes from 9 to 8.
- **Lockout** (`TestAdminTOTPLockout`, `TestAdminTOTPConcurrentReplay`):
  - A success resets the count, and recovery codes count towards it.
  - At 5 failures the answer is `otp_locked`, the admin sessions are revoked and the customer session stays.
  - Of 5 concurrent verifies with the same code, exactly 1 succeeds.
- **TOTP unconfigured** (`TestTOTPUnconfigured`): 503 without `TOTP_ENC_KEY`.
- **AU-13, logout-all** (`TestLogoutAll`):
  - No cookie, a junk cookie or another app's cookie → 401.
  - A token rotated within the grace window works; an older rotated token → 401.
  - Both sessions are revoked; the business session and other users are untouched.
  - It is audited, and a second call → 401.
- **AU-18, `DELETE /v1/me`** (`TestDeleteAccount`, `TestDeleteAccountRefusesAnAdmin`, `TestDeleteAccountOtherUsersCode`):
  - Who may ask: anonymous → 401, a business token → 403, a bad request id or a login code → `otp_expired`, another user's code → `otp_expired`.
  - A wrong code → `code_invalid` with `attemptsLeft` 4.
  - A refusing check → 409; nothing changes and the code stays unused. A check's non-problem error → 500.
  - A failing hook rolls everything back, its own write included (problem → 409, plain error → 500).
  - A hook runs inside the deletion transaction: it takes `SELECT … FOR UPDATE NOWAIT` on the user row and writes to it.
  - On success: the user is anonymised, the phone identity and addresses are deleted, devices are scrubbed, all sessions are revoked with reason `deleted`, the OTP is consumed and an audit row is written. The number then signs up as a new user.
  - An active admin → 409 until disabled.
- **Admin gate** (`TestAdminGate`, `TestAdminGateMiddleware`):
  - Misuse → 500, anonymous → 401.
  - A non-admin app or an OTP-only amr → 403; a missing role → 403.
  - super_admin holds every role.
  - A stale step-up → `step_up_required`.
- **Authenticator** (`TestAuthenticator`): an `otp+totp` claim on an unverified session is downgraded to `otp`.
- **Seed** (`TestRepoFixtureComposition`, `TestSeedAdmins`, `TestSeedAdminsUnsetLogsTheName`, `TestSeedAdminsRefusedOutsideLocalAndTest`, `TestSeedAdminsWithoutTOTPKey`, `TestLoadSeedAdminsRefusesBadFiles`):
  - The repo fixture has the D-64 composition.
  - Each seeded admin has its roles and a phone identity that sign-in finds. An enrolled admin's secret is sealed and opens to the fixture value; the unenrolled admin has none.
  - Unset → the name is logged and nothing is seeded.
  - Staging and prod are refused, and bad files are refused.
  - Without `TOTP_ENC_KEY` the admins are skipped with the key's name logged.
- **HTTP, through `buildRouter` and the contract validator** (`TestIdentityHTTPAdminTOTP`, `TestIdentityHTTPLogoutAll`, `TestIdentityHTTPDeleteMe`):
  - **Admin TOTP:**
    - An OTP-only admin token and a customer token both get 403 on `/v1/admin/jobs`.
    - Enrol and verify answer `no-store`, then jobs answers 200.
    - A support admin pausing `identity_purge` gets 403; after the role becomes admin, 200. The audit actor is the admin's user id.
    - A step-up verify after the window → 200; a malformed code → 422.
  - **Logout-all:**
    - A forged origin → 403 with no `Set-Cookie`; no cookie → 401.
    - Success → 204, the cookie is cleared and both access tokens get 401.
    - A revoked cookie → 401, and the cookie is cleared.
  - **`DELETE /v1/me`:** anonymous → 401; a wrong code → 422 with `extensions.attemptsLeft`; success → 204 with the cookie cleared.

## Interface summary

- **Admin gate** (`internal/platform/authz`):
  - `authz.NewAdminGate(clk clock.Clock) *AdminGate`.
  - `(*AdminGate).Require(ctx, anyOf []authz.Role, stepUp bool) (authn.Principal, error)`. Its checks, in order:
    1. misuse → 500;
    2. anonymous → 401;
    3. not the admin app, or amr not `otp+totp` → 403 `forbidden`;
    4. none of `anyOf` held (super_admin holds all) → 403 `forbidden`;
    5. `stepUp` with a zero step-up time, or one older than `authn.StepUpWindow` → 403 `step_up_required`.
  - `(*AdminGate).Middleware(anyOf, stepUp) func(http.Handler) http.Handler` does the same for a plain chi route.
  - Roles are read from the session on every request, so a grant or a removal applies to tokens already issued.
  - `cmd/todayz/routes_admin.go` has the adapter `adminJobsGate`, which maps the gate to `adminapi.Gate` as `Admin{ID: user id, Roles: the admin roles held}`.
- **Deletion hooks** (`internal/identity/hooks.go`):
  - The interfaces are `identity.CanDeleteAccount{ CanDeleteAccount(ctx, userID ids.ID) error }` and `identity.OnAccountDelete{ OnAccountDelete(ctx, userID ids.ID) error }`. `CanDeleteFunc` and `OnDeleteFunc` adapt plain functions.
  - Register them through `Deps.Hooks` (`identity.Hooks{Can, On}`), or `(*Service).AddDeletionCheck` and `AddDeletionHook`.
  - **A check** refuses by returning a `*problem.Error`, for example 409 `conflict` with a detail. That answer reaches the client as is. Any other error is 500.
  - **A hook** runs inside the deletion transaction. It must use `db.DB.Q(ctx)`, which joins the transaction carried in `ctx`, and must not open its own pool connection for the same rows. Any error rolls back the whole deletion.
- **Service methods:**
  - `EnrolTOTP(ctx, p) (Enrolment{Secret, URI, RecoveryCodes}, error)`.
  - `VerifyTOTP(ctx, p, code, purpose)`, where purpose is `TOTPPurposeLogin` or `TOTPPurposeStepUp`.
  - `RecoverTOTP(ctx, p, code)`, which returns `RecoveryCodesLeft`.
  - `LogoutAll(ctx, RefreshInput) (int64, error)`.
  - `DeleteAccount(ctx, p, DeleteInput{RequestID, Code})`.
  - `SignIn.NextStep` is `NextStepTOTPEnrol` or `NextStepTOTPVerify` for the admin app.
- **Seed:**
  - `SEED_ADMINS_FILE` is set to `e2e/fixtures/admins.yaml` in `infra/local/local.env`, and present but empty in `.env.example`.
  - Fixture ids are fixed ULIDs.
  - Phones are `+919000000021` to `+919000000030`.

## Files changed outside W1-01's owned paths

- `backend/cmd/todayz/routes_admin.go`: the admin gate replaces `DenyGate` (the lead allowed the wiring).
- `backend/internal/platform/jobs/adminapi/gate.go` and `service.go`: comments only, at the `TODO(W1-01)` spots (allowed).
- `backend/internal/platform/authn/authenticator.go` and `authn_test.go`: the `otp+totp` downgrade for an unverified session. authn is W1-01's own package from phase A; it is listed here because it is platform code.
- `backend/internal/platform/httpx/mw/authn.go` (slot 11, W1-01's slot): `/v1/auth/logout-all` joins the cookie paths, and `/v1/admin` needs `otp+totp`.
- `e2e/fixtures/admins.yaml`: new, the admin seed fixture (08 §2 names it).
- `infra/local/local.env`: `SEED_ADMINS_FILE=e2e/fixtures/admins.yaml`.
- `.env.example`: `SEED_ADMINS_FILE=` (the name only).

The W1-07 `TODO(W1-01)` spots (`telemetry/metrics.go:345` and `internal/telemetry/telemetry.go:38`, the event user id) were left untouched: they are outside phase B's scope.

## Questions (marked `TODO(lead)` in the code where noted)

1. **TOTP lockout number.** 01 §2 gives none. It reuses the OTP's 5 tries. At the limit the admin's sessions are revoked and the admin signs in again with OTP. (`TODO(lead)` in `totp.go`.)
2. **Deleting an admin's account.** Not specified. An active admin is refused with 409 until a super_admin disables them. (`TODO(lead)` in `account.go`.)
3. **Deleting a business owner's account.** Not specified. W1-04 should register a `CanDeleteAccount` check that refuses while the user owns an active business.
4. **`code_invalid` versus phase A's 401.** A wrong code on a bearer route answers 422 `code_invalid`, because a 401 there would tell the client its access token is bad and send it into a refresh loop. A wrong sign-in OTP stays 401 `unauthorized`, as in phase A. Confirm, and say whether sign-in should move to `code_invalid` too.
5. **`otp_expired` on `DELETE /v1/me`.** It is a 401 (its registered status) on a bearer route, so the same refresh-loop risk applies. A client should read the code, not the status. Should it be re-registered as 422 for bearer routes?
6. **Deletion reuses the `step_up` OTP purpose.** 04 names no purpose for deletion. `POST /v1/auth/otp/send` now accepts `purpose: step_up` on every app; only `DELETE /v1/me` consumes it today.
7. **No refresh rotation on TOTP elevation.** Verify marks the session and issues a new access token, but keeps the refresh token. The session row carries the elevation (and the Authenticator checks it), so an old refresh token gains `otp+totp` too. Rotating on elevation would be stricter.
8. **The gate's answer for a non-admin is 403**, not 401, since the caller is signed in.
9. **Seed phones.** `+919000000021` to `+919000000030` are in the reserved test range. Other domains' seeds must not reuse them.
10. **No recovery codes are seeded** for the fixture admins. Tests enrol a fresh admin when they need codes.
11. **W1-08** should add `/v1/auth/logout-all` to the PWA's no-bearer list, as it did for logout.

## Not verified

- **Step-up over HTTP through a money-class job.** No money-class job kind is registered yet; `identity_purge` is maintenance. Step-up is tested with the real gate in `TestAdminGate`, `TestAdminStepUp` and the identity HTTP test, but not through an admin-jobs action that needs it.
- **Authenticator apps.** The codes come from the in-house RFC 6238 implementation, checked against the RFC vectors. No real authenticator app scanned the `otpauth://` URI.
- **`DeleteAddresses` and foreign keys.** Addresses are deleted outright. Once orders (W2) reference addresses, deletion will need to anonymise them instead, or the foreign key will refuse it.
- **`job_runs.triggered_by`** has no foreign key to users, so a deleted admin's past runs keep a dangling id. This is by design for the audit trail; noted for W1-11.
- **check-infra and check-tf-static** after the rebase: they need `make setup` online to initialise `infra/cloudflare`.
- **Real MSG91, real Turnstile, and cookies in real browsers**, as in phase A.
