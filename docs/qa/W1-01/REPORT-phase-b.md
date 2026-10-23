# W1-01 Identity and auth (phase B): QA report

Branch `worktree-agent-a17d03d0a9284659d`, rebased onto `main` at 7bd9b09 (W1-11 merged). Everything ran locally on testcontainers. Nothing touched a cloud account, the shared local stack or the network. The agent could not write report files, so the lead committed this from its hand-back. Phase A's report is `REPORT.md` in this folder; this one covers only what phase B added.

The W1-01b security review fixes (commits e7ddedf and b7459a1) are described under "Security review fixes (W1-01b)" below. The sections before it are corrected to the behaviour after those fixes.

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
  - The secret is 160 random bits, sealed with `pii.Sealer` under `TOTP_ENC_KEY`. The AAD is `totp_secret:<user id>` (`TOTPSealFieldFor`), so a sealed secret opens only on its own admin's row.
  - Codes are compared in constant time.
- **Admin sign-in is OTP plus TOTP.**
  - OTP verify on the admin app issues a token with `amr=otp` and `nextStep`.
  - Slot 11 (`mw/authn.go`) lets an `amr=otp` admin token reach `/v1/auth/*`, `/v1/rum` and `/v1/events` only. Every `/v1/admin/*` route needs `amr=otp+totp`, else 403.
  - Enrol returns the secret, the `otpauth://` URI and 10 recovery codes (16 Crockford base32 symbols, 80 bits, `XXXX-XXXX-XXXX-XXXX`), once. The codes are stored as keyed hashes. Enrolling again before the first verify replaces the pending secret and codes; after a verify it is 409.
  - Verify with `purpose: login` rotates the sign-in: the family's live row becomes `rotated` (the usual 30 s grace applies to its token), a new row with `totp_verified_at` (the step-up time) is inserted in the same family, and the answer carries a new access token with `amr=otp+totp` and sets the new refresh cookie. Refresh keeps the amr while the session stays verified.
  - The `Authenticator` downgrades a token claiming `otp+totp` to `otp` when the session row has no TOTP verification, so the claim alone is never trusted. A verification counts only while `admin_users.totp_enabled_at` is set and not newer than it, so a TOTP reset ends the elevation.
- **Step-up.** Verify with `purpose: step_up` refreshes the session's step-up time. It never finishes a sign-in. `authn.StepUpWindow` is 10 min.
- **Recovery codes.** Each is single use. Lowercase, spaces and missing dashes are accepted, and I, L and O are read as 1, 1 and 0. A used code is consumed in the same transaction that elevates the session, and a recovery rotates the refresh cookie like a login verify. The answer says how many are left.
- **Lockout.**
  - Wrong TOTP and recovery codes count together in `totp_failures`.
  - At 5 (`TOTPMaxFailures` = `OTPMaxAttempts`, `TODO(lead)`) the answer is 429 `otp_locked`, and every admin-app session of that user is revoked. Other apps' sessions are not.
  - A success resets the count.
- **Logout-all** (`Service.LogoutAll`):
  - It authenticates by the refresh cookie. A token rotated within `RotationGrace` (another tab refreshed a moment ago) is refused with 401 and nothing is revoked; the cookie is kept, as for a refresh grace retry. An older rotated token is 401 too.
  - It revokes every live session of that user on that app.
  - It is audited as `identity.sign_out_all` and clears the cookie.
- **`DELETE /v1/me`** (`Service.DeleteAccount`, customer app only, policy `me.delete`):
  1. The step-up OTP is checked like a sign-in code: 5 attempts, and a wrong code is 422 `code_invalid` (that attempt commits). Another user's request id, or a login-purpose code, is `otp_expired`.
  2. With the right code, an active admin is refused with 409 (lead: Q2).
  3. Every `CanDeleteAccount` check runs in a savepoint, identity's own first (no deletion while limited, D-106). A `*problem.Error` refusal is answered as is and the whole transaction rolls back: the code stays unused and no attempt is spent, since the code was right. Any other error is 500.
  4. Then everything below happens in one transaction; any hook error rolls all of it back:
     - the user is anonymised (name gone, `status = deleted`);
     - the phone identity is deleted, so the number signs up as a new user;
     - addresses are deleted; devices lose their push keys and counter secrets, and each `device_key` is replaced by a random one;
     - every session is revoked with reason `deleted`, and its user agent and IP hash are nulled;
     - for a disabled admin, the TOTP secret, enrolment and last step are cleared and the recovery codes deleted;
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

## Security review fixes (W1-01b)

The review found no High findings. Each fix has a test that fails without it.

- **M1, mass revocation missed a just-rotated session.** Logout-all, the TOTP lockout and deletion each ran one `UPDATE sessions … revoked_at IS NULL`; under READ COMMITTED its snapshot missed the row a concurrent refresh inserted.
  - Fix: the users row serialises them. Refresh reads the token's user without a lock, then takes the users row lock (`FOR SHARE` in W1-01b, `FOR NO KEY UPDATE` since the re-review below), then the session `FOR UPDATE`. The mass revokers take `users … FOR NO KEY UPDATE` (`LockUser`) first: `LogoutAll` through `lockSessionUser`, `VerifyTOTP` and `RecoverTOTP` through `lockUserAdmin`, `DeleteAccount` directly. A rotation either commits before the revocation starts (its new row is revoked with the rest) or runs after it (and finds its token revoked).
  - Lock order everywhere: users, then admin_users, then sessions.
  - Tests (`account_race_test.go`): `TestLogoutAllRacesRefresh`, `TestTOTPLockoutRacesRefresh` and `TestDeleteAccountRacesRefresh`. Each runs 12 trials of a goroutine chaining refreshes on another sign-in of the same user against the revocation, then asserts zero live rows for that user and app (logout-all also checks the business app stays signed in).
  - Without refresh's `LockUserShared`, all three fail at trial 0: logout-all and the lockout leave a live row, and deletion fails with `deadlock detected (SQLSTATE 40P01)`.
- **L2, deadlock with refresh.** `LockUser` is now `FOR NO KEY UPDATE`. The deadlock above is the evidence: the old refresh took no users lock, its session insert took `KEY SHARE` on users through the foreign key, and deletion's `FOR UPDATE` conflicted with it. With refresh taking `FOR SHARE` first, the two now queue instead. `FOR NO KEY UPDATE` also leaves `KEY SHARE` free, so a sign-in or another domain's insert that references the user is not blocked by a revocation.
- **M2, Preflight.** `identity.Preflight` requires `TOTP_ENC_KEY` in staging and prod (the same rule as the other keys) and refuses it when it equals `PII_ENC_KEY` (constant-time compare; the error names the variables, never a value). `config.Load` already refuses a missing app key in a deployed env, so the missing-key check is defence in depth; the test builds the config and then clears the key. Test: `TestPreflight`.
- **M3, deletion while limited (D-106).** Identity registers its own `CanDeleteAccount` check first: while `customer_profiles.limited_until > now`, 409 `conflict` with the detail saying the account can be deleted after that date, and `extensions.limitedUntil` (RFC 3339). The other checks do not run, the code stays unused and no attempt is spent. The audit row's `checks` count includes it. Test: `TestDeleteAccountWhileLimited` (409 at `until - 1 s`, success 1 s later). Contract text in `api/paths/me.yaml`.
- **L1, PII left after deletion.** In the deletion transaction:
  - every session of the user gets `user_agent` and `ip_hash` nulled (`ScrubSessionPII`);
  - `device_key` is the browser's own id, kept in localStorage, so a new account in that browser would link back to the deleted one. Each of the user's devices gets a new random key (128 bits, ULID form); the rows stay;
  - a disabled admin's `totp_secret_enc`, `totp_enabled_at` and `totp_last_step` are cleared and their recovery codes deleted (`ClearAdminTotp`, `DeleteRecoveryCodes`);
  - `otp_requests.phone_enc` is NOT NULL (the verify step needs it), so it is left alone; the 24 h OTP purge removes it. `phone_hash` is kept, as the send limits need it.
  - Tests: `TestDeleteAccount` (device keys replaced, rows kept, no session user agent or IP hash), `TestDeleteAccountDisabledAdminDropsTOTP`, `TestDeleteAccountRacesRefresh`.
- **L3, TOTP reset.** `totpCurrent(enabledAt, verifiedAt)`: CheckSession and Refresh grant `otp+totp` and a step-up time only when `admin_users.totp_enabled_at` is set and the session's `totp_verified_at` is not before it. This also covers a reset followed by a re-enrolment. Test: `TestTOTPResetDropsElevation`.
- **L4, seal binding.** The seal AAD is `totp_secret:<user id>` in the service and the admin seed. Nothing is deployed, so no old blobs were migrated. Test: `TestTOTPSealBoundToAdmin` (admin A's sealed secret copied to admin B's row does not open: 500; A still verifies), plus the seed tests.
- **L5, recovery codes.** 16 symbols of Crockford base32 (80 bits), shown as `XXXX-XXXX-XXXX-XXXX`; no new key. The contract pattern is `^[A-Za-z0-9 -]{16,32}$`. Test: `TestRecoveryCodeFormat`, `TestAdminRecoveryCodes`.
- **L7, telemetry for OTP-only admin tokens.** Slot 11 lets an `amr=otp` admin token reach `/v1/rum` and `/v1/events` (exact paths; `/v1/rum/x` and `/v1/eventsx` stay 403). Test: the `admin otp only, rum|events|rum sub|eventsx` cases of `TestAuthn` in `mw/authn_test.go`.
- **L9 (Q7), rotate on elevation.** A login-purpose TOTP verify or a recovery rotates the refresh cookie (`rotateElevated`): the family's live rows become `rotated` with the usual grace, and a new row is inserted with `totp_verified_at = now`, the same family, the family's absolute expiry (never extended), the old row's device and user agent, and this request's IP hash. The family comes from the access token's session id, so verify needs no cookie. A step-up verify does not rotate. The handler sets the new cookie; the contract text in `api/paths/auth.yaml` and the `Set-Cookie` headers are updated, and `make gen` ran. Tests: `TestAdminTOTPEnrolAndVerify` (the old token is a grace retry, then refused; the new one refreshes to `otp+totp`), `TestAdminRecoveryCodes`, `TestStepUpDoesNotRotate`, `TestIdentityHTTPAdminTOTP` (a new cookie on login verify, none on step-up).
- **L10a, refused deletions.** A correct code refused by a check (409) spends no attempt: the attempt is recorded only on a wrong code, and a refusal rolls back. Test: `TestDeleteAccount` (5 refusals in a row after one wrong code leave `attempts = 1`).
- **L8, report wording.** The logout-all text above is corrected: a grace-window token is refused with 401 and nothing is revoked.
- **L6:** left as is, per the lead.

## Re-review fixes (W1-01b, second pass)

The re-review confirmed M1–M3, L1–L5, L7, L9 and L10a. Two findings stayed open, plus two optional items.

- **Finding 1 (Medium-Low): plain logout and refresh's reuse branch could miss a freshly rotated row.** Refresh held only `users … FOR SHARE`, so two refreshes of one user ran side by side. Reuse detection's `RevokeFamily` (or a plain `Logout`, which took no users lock at all) waited on the old row while another refresh inserted the next one, and READ COMMITTED never showed that row to the UPDATE.
  - Fix: one lock mode for every path that rotates or bulk-revokes. `lockSessionUser` (the token's user, read unlocked, then `LockUser`, i.e. `FOR NO KEY UPDATE`) is now used by `Refresh`, `Logout` and `LogoutAll`. `VerifyTOTP` and `RecoverTOTP` already took it through `lockUserAdmin`, and `DeleteAccount` directly. No transaction takes `FOR SHARE` and then upgrades, since two concurrent reusers would deadlock on the upgrade. Lock order stays users, then admin_users, then sessions.
  - Multi-tab refresh: refreshes of one user now queue behind each other. Under the users lock, refresh runs only indexed single-row statements (the session `FOR UPDATE`, the insert, the rotate/revoke updates, the admin row), `crypto/rand` for the new token and the audit insert. The access token is signed after commit and there is no outbound call. Logout's critical section is shorter still. Two tabs refreshing at once wait on the order of milliseconds; the loser of the race still gets the usual grace retry.
  - `FOR NO KEY UPDATE` does not conflict with the `KEY SHARE` that inserts referencing users take, so other domains' writes for the user are not blocked.
  - Tests (`account_race_test.go`): `TestRefreshReuseRacesRefresh` (12 trials: an old token presented after the grace window, while a loop refreshes the newer one; the reuse call is 401 and not a grace retry, and the family has zero live rows) and `TestLogoutRacesRefresh` (12 trials: `Logout` with the older cookie against the loop; zero live rows).
  - Mutation: with `lockSessionUser` taking `FOR SHARE`, both fail at trial 1 ("a refresh slipped a live row past reuse detection", "… past logout").
- **Finding 2 (Low): sign-in was not serialised with deletion.** `signIn` read the user by phone hash without a lock, so a deletion that committed in between left a live session (with user agent and IP hash) and possibly a device row for a deleted user.
  - Fix: after the lookup, `signIn` takes `LockUserShared` (`FOR SHARE`, users first, as the lock order requires) and re-checks the status from the locked row. A non-active user is refused exactly as before (`statusRefusal`: a deleted user gets `account_blocked`, and the code is consumed as for any refused sign-in). Sign-in never upgrades the lock. Concurrent sign-ins of one user still run side by side; a deletion, which takes `FOR NO KEY UPDATE`, waits for them and then revokes and scrubs what they wrote.
  - A deadlock found and fixed along the way: `DeleteAccount` used to lock the OTP row whose id the client sent. A client that sent a login request id could make deletion hold the users row and wait on that OTP row, while a sign-in held the OTP row and waited on the users row. Deletion now locks only a `purpose = 'step_up'` row (`GetStepUpOtpRequestForUpdate`). A login request id gives no rows, i.e. `otp_expired`, the same answer the purpose check gave before.
  - Tests: `TestSignInRacesDeleteAccount` (the sign-in is paused after its user lookup by a test transaction holding an uncommitted delete of the user's customer profile; the deletion queues behind it; afterwards the user is deleted, there is no live session, no session keeps a user agent or IP hash, no device keeps the sign-in's key, and the sign-in's refresh token is 401) and `TestSignInDuringDeletionIsRefused` (the deletion is paused in an `OnAccountDelete` hook after its users lock; the sign-in waits, then gets `account_blocked` with no session or device written and its code consumed).
  - Mutation: with the lock removed (status read from the unlocked lookup), `TestSignInRacesDeleteAccount` fails ("a sign-in left a live session for a deleted user") and `TestSignInDuringDeletionIsRefused` fails (the sign-in succeeds).
- **Finding 3 (optional): TOTP verify and recovery signed the access token after commit.** Done: `VerifyTOTP` and `RecoverTOTP` now call `issueTOTP` inside the transaction, so a signing failure rolls back the rotation and the audit row. There is no test: `authn.Tokens` is a concrete type and its signer cannot be made to fail from a test. `Refresh` and `VerifyOTP` still sign after commit (out of scope here). A signing failure there is a 500 after a committed rotation or sign-in; the client's next refresh with the old cookie is a grace retry or, after 30 s, reuse.
- **Finding 4 (optional): L7 at the HTTP level.** `TestIdentityHTTPAdminTOTP` now posts a RUM sample with the OTP-only admin token through the real router: `/v1/rum` is 204, `/v1/rum/x` and `/v1/rum/` are 403.

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
| W1-01b: `gofmt -l internal cmd` | clean |
| W1-01b: `go vet ./...` | clean |
| W1-01b: `golangci-lint run ./...` | 0 issues |
| W1-01b: `gosec -quiet -exclude-generated -exclude-dir=testdata -exclude=G104 ./...` | 0 issues |
| W1-01b: settings lint (`go run ./internal/devtools/settingslint .`) | ok |
| W1-01b: `go test -race -count=1 ./...` | ok, every package (identity 139 s, cmd/todayz 128 s, seed, mw, authn, authz). |
| W1-01b: `make check-contract-lint` | valid; 17 `no-unused-components` warnings, all present before this change |
| W1-01b: `make check-contract-client` | lint, typecheck and 39 tests pass |
| W1-01b: `make contract-smoke` | ok (`TestContractSmoke` and `TestContractProblemCheckFires`, Schemathesis from `.tools/schemathesis`, on testcontainers) |
| W1-01b: mutation runs | Each fix was reverted in turn and the tests rerun, then restored. No users lock in refresh: the three race tests fail at trial 0 (a live row after logout-all and the lockout; a deadlock in deletion). `totpCurrent` ignoring `totp_enabled_at`: `TestTOTPResetDropsElevation` fails. A constant seal AAD: `TestTOTPSealBoundToAdmin` fails. The attempt recorded before the match: `TestDeleteAccount` fails. Identity's check not registered: `TestDeleteAccountWhileLimited` fails. The L5, L7, L9, M2 and L1 tests fail by construction without their fix (old format, 403, no rotation, no key check, unchanged keys and user agents). |
| Re-review: `gofmt -l .`, `go vet ./...` | clean |
| Re-review: `golangci-lint run ./...`, `gosec -quiet -exclude-generated -exclude-dir=testdata -exclude=G104 ./...` | 0 issues |
| Re-review: `GOFLAGS=-p=3 go test -race -count=1 ./...` (on top of 041666c, rebased onto main at bb315da) | ok, all 40 packages with tests (identity 14 s, cmd/todayz 24 s) |
| Re-review: mutation runs | `lockSessionUser` taking `FOR SHARE`: `TestRefreshReuseRacesRefresh` and `TestLogoutRacesRefresh` fail at trial 1. `signIn` without the users lock: `TestSignInRacesDeleteAccount` and `TestSignInDuringDeletionIsRefused` fail. Both restored. |
| No secrets in the logs | The identity integration tests log through a capture that redacts nothing. They fail if a phone, OTP, access or refresh token, TOTP secret, TOTP code or recovery code appears in it. The seed test checks that no fixture TOTP secret or phone is logged. |

### What the tests cover

- **AU-11, admin TOTP enrol and verify** (`TestAdminTOTPEnrolAndVerify`):
  - The secret is stored sealed and the recovery codes hashed.
  - A pending enrolment can be replaced; an enrolled admin gets 409.
  - A step-up verify never finishes a sign-in.
  - A login verify gives `otp+totp` on the same session id and a new refresh token with the sign-in's expiry; the old token is a grace retry, one row is `rotated` and one live row is verified.
  - A replayed code is refused.
  - Refresh keeps the amr, and the next sign-in starts at `totp_verify`.
  - A customer token gets 403.
- **AU-12, step-up expiry** (`TestAdminStepUp`, `TestAdminGate`):
  - 10 min + 1 s later, the gate answers `step_up_required`.
  - A step-up verify makes it fresh again.
  - An OTP-only token gets 403.
- **Recovery codes** (`TestAdminRecoveryCodes`, `TestRecoveryCodeFormat`): the 16-symbol format, lowercase and no-dash input work, each code works once, the count left goes from 9 to 8, and a recovery rotates the refresh token.
- **Lockout** (`TestAdminTOTPLockout`, `TestAdminTOTPConcurrentReplay`):
  - A success resets the count, and recovery codes count towards it.
  - At 5 failures the answer is `otp_locked`, the admin sessions are revoked and the customer session stays.
  - Of 5 concurrent verifies with the same code, exactly 1 succeeds.
- **TOTP unconfigured** (`TestTOTPUnconfigured`): 503 without `TOTP_ENC_KEY`.
- **AU-13, logout-all** (`TestLogoutAll`):
  - No cookie, a junk cookie or another app's cookie → 401.
  - A token rotated within the grace window → 401 (a grace retry) and nothing is revoked; after the window → 401, not a grace retry; 4 live sessions remain throughout.
  - Both sessions are revoked; the business session and other users are untouched.
  - It is audited, and a second call → 401.
- **AU-18, `DELETE /v1/me`** (`TestDeleteAccount`, `TestDeleteAccountRefusesAnAdmin`, `TestDeleteAccountOtherUsersCode`):
  - Who may ask: anonymous → 401, a business token → 403, a bad request id or a login code → `otp_expired`, another user's code → `otp_expired`.
  - A wrong code → `code_invalid` with `attemptsLeft` 4.
  - A refusing check → 409; nothing changes, the code stays unused and five refusals in a row spend no attempt. A check's non-problem error → 500.
  - While limited (`TestDeleteAccountWhileLimited`) → 409 naming the date, until the date has passed.
  - A failing hook rolls everything back, its own write included (problem → 409, plain error → 500).
  - A hook runs inside the deletion transaction: it takes `SELECT … FOR UPDATE NOWAIT` on the user row and writes to it.
  - On success: the user is anonymised, the phone identity and addresses are deleted, devices are scrubbed and get new keys, all sessions are revoked with reason `deleted` and lose their user agent and IP hash, the OTP is consumed and an audit row is written. The number then signs up as a new user.
  - An active admin → 409 until disabled; a disabled admin's TOTP secret and recovery codes go with the account (`TestDeleteAccountDisabledAdminDropsTOTP`).
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
- `backend/internal/platform/httpx/mw/authn.go` (slot 11, W1-01's slot): `/v1/auth/logout-all` joins the cookie paths, and `/v1/admin` needs `otp+totp`. W1-01b: an OTP-only admin token may reach `/v1/rum` and `/v1/events` (L7).
- `backend/internal/seed/identity.go` and `admins_test.go` (W1-01b): the seed seals with `TOTPSealFieldFor` (L4).
- `backend/api/paths/me.yaml` (W1-01b): the 409 while limited, and a refusal not counting as a try.
- `e2e/fixtures/admins.yaml`: new, the admin seed fixture (08 §2 names it).
- `infra/local/local.env`: `SEED_ADMINS_FILE=e2e/fixtures/admins.yaml`.
- `.env.example`: `SEED_ADMINS_FILE=` (the name only).

The W1-07 `TODO(W1-01)` spots (`telemetry/metrics.go:345` and `internal/telemetry/telemetry.go:38`, the event user id) were left untouched: they are outside phase B's scope.

## Questions (marked `TODO(lead)` in the code where noted)

The lead answered these after the review (to be recorded with D-106): Q1 (5 tries), Q2 (409 for an active admin), Q4 (`code_invalid` 422 on bearer routes; sign-in keeps its 401), Q6 (reusing `step_up`) and Q8 to Q11 are accepted. Q3: W1-04 exposes an owner check and the lead wires it. Q5: the 401 registration stays; clients key on `code` (W1-08 told). Q7: done in W1-01b (rotation on elevation, L9). The original text follows.

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

## Notes for later WPs

- **W2-03, referral codes.** A referral code that points at a deleted user is not handled here; the `TODO(lead)` moves to W2-03.
- **Admin app and L9.** The admin app must store the refresh cookie set by a login verify or a recovery. If it keeps sending the old one, the old token is a grace retry for 30 s and then counts as reuse, which revokes the family.

## Not verified

- **Step-up over HTTP through a money-class job.** No money-class job kind is registered yet; `identity_purge` is maintenance. Step-up is tested with the real gate in `TestAdminGate`, `TestAdminStepUp` and the identity HTTP test, but not through an admin-jobs action that needs it.
- **Authenticator apps.** The codes come from the in-house RFC 6238 implementation, checked against the RFC vectors. No real authenticator app scanned the `otpauth://` URI.
- **`DeleteAddresses` and foreign keys.** Addresses are deleted outright. Once orders (W2) reference addresses, deletion will need to anonymise them instead, or the foreign key will refuse it.
- **`job_runs.triggered_by`** has no foreign key to users, so a deleted admin's past runs keep a dangling id. This is by design for the audit trail; noted for W1-11.
- **check-infra and check-tf-static** after the rebase and after W1-01b: they need `make setup` online to initialise `infra/cloudflare`. W1-01b changes no terraform.
- **Re-review finding 3** has no test (see above), and `Refresh` and `VerifyOTP` still sign the access token after commit.
- **W1-01b race tests are probabilistic in the other direction.** With the lock they cannot miss a row; without it they failed at trial 0 on this machine. On a much slower or faster machine the unfixed code might survive more trials, but the fixed code never fails them.
- **Real MSG91, real Turnstile, and cookies in real browsers**, as in phase A.
