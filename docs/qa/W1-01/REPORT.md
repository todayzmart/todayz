# W1-01 Identity and auth (phase A): QA report

Branch `worktree-agent-a52a1dee280984955`. Everything ran locally on testcontainers. Nothing touched a cloud account, the shared local stack or the network. The agent could not write report files, so the lead committed this from its hand-back.

## What was built

- **Contract.** `/v1/auth/otp/send`, `/v1/auth/otp/verify`, `/v1/auth/refresh`, `/v1/auth/logout`, and `GET` and `PATCH /v1/me`.
  - Files: `api/paths|schemas/{auth,me}.yaml` and `api/problems/auth.yaml`.
  - `make gen` ran in the same change.
- **Schema** (`10101_w1-01_identity.sql`):
  - `users`, with status and version.
  - `phone_identities`: the phone sealed with AES-GCM, plus a unique HMAC hash of the phone.
  - `otp_requests`: the code as an HMAC hash, attempts, phone and IP hashes, expiry, and whether it was used.
  - `devices`.
  - `sessions`: one row per refresh token, with `family_id`, the token hashed, the revoke reason, and `totp_verified_at` for phase B.
  - `admin_users` (the D-64 roles) and `admin_recovery_codes` (for phase B).
  - `customer_profiles`: referral code and `limited_until`.
  - `addresses`: the table only.
- **`platform/pii`.**
  - `Sealer`: AES-256-GCM, with the field bound into the AAD.
  - `Hasher`: a keyed HMAC prefixed by purpose. Its `IP` hash matches `audit_log.ip_hash`.
  - `NormalizePhone` accepts +91 mobile numbers only.
  - `MaskPhone`, and `IsReservedTestPhone` for `+9190000000xx`.
- **`platform/authn`.**
  - EdDSA access tokens that last 15 min. The `kid` can rotate: every key in `JWT_SIGNING_KEYS` verifies, and `JWT_CURRENT_KID` signs.
  - Strict parsing: the algorithm is pinned, and iss, aud, sub, sid, amr and app are checked. Tokens are capped at 4 KB.
  - `Principal` and its context helpers.
  - `CheckRefreshOrigin` (AU-16).
  - An `Authenticator` that checks the session row on every request, so a revoke or a block takes effect at once (AU-17).
- **`platform/authz`.**
  - An engine over per-domain policy tables, one file per domain under `policy/`.
  - Each action is one of four kinds: public, self, business-scoped or admin.
  - For admins, the strongest grant across a user's roles wins, plus a step-up window.
  - Golden files, and the identity policies `me.view` and `me.edit`.
  - `authz.Strict`, which mounts slot 15 on oapi strict servers.
- **`platform/ratelimit` and the middleware slots.**
  - `mw.Authn` is slot 11.
  - `mw.RateLimit` is slot 12. It limits per user or per IP depending on the route group, and sends `RateLimit-*` headers and `Retry-After`.
  - `mw.Authz` is slot 15.
- **Sign-in.**
  - OTP send and verify for the customer, business and admin apps.
  - Limits, all held in Postgres and serialised by advisory locks:
    - 30 s between sends to one number;
    - 5 sends per number per hour;
    - 20 sends per IP per hour;
    - 5 attempts per code;
    - codes expire after 5 min.
  - SMS goes through an interface shaped like MSG91. Staging and prod refuse to send without a key.
  - Turnstile goes through an interface too.
  - Refresh rotates every time. Presenting an already-rotated token revokes the whole family.
  - Logout revokes the family.
  - A blocked user is refused everywhere.
  - Admin sign-in works only for an existing, enabled `admin_users` row, and never creates a user.
- **`/v1/me`.**
  - `GET` returns the masked phone, name, language, app, roles (admin only), the customer's referral code and the no-show order limit.
  - `PATCH` takes `If-Match` and returns an `ETag` of the form `"v{version}"`. `"name": null` clears the name.
- **Ops.**
  - `identity.purge` is an hourly River job. It deletes expired OTP requests after 24 h, and sessions 30 days after they expire or are revoked.
  - `identity.Preflight` stops staging and prod from starting without the keys or the SMS provider. It names only the variables.
  - The seed adds 3 customers and 3 staff, with fixed ULIDs, reserved test numbers and sealed phones. It skips with a warning when `PII_ENC_KEY` or `PII_HASH_KEY` is unset.

## Evidence

All commands ran in `backend/`, with `DOCKER_HOST` set to the colima socket and the Ryuk override, unless the row says `make`.

| Check | Result |
|---|---|
| `gofmt -l .` (excluding `gen/`) | clean |
| `go vet ./...` | clean |
| `golangci-lint run ./...` | 0 issues |
| `go test -race -count=1 ./internal/identity/` | ok, coverage 60.1%. 21 integration tests, each on a fresh PostGIS. |
| `go test -race -count=1 ./cmd/...` | ok. Includes the 5 `TestIdentityHTTP*` tests, which go through `buildRouter`, the real pipeline and the contract validator. |
| `go test -race` on authn, authz, authz/policy, ratelimit, pii, httpx/mw, seed and jobs | ok |
| `go test -fuzz FuzzVerify -fuzztime 30s ./internal/platform/authn/` | PASS. 521,363 runs, 178 corpus entries, no crash. |
| `GOFLAGS=-v make contract-smoke` | `TestContractSmoke` PASS: the Schemathesis examples and coverage phases, and every error is a registered problem. `TestContractProblemCheckFires` PASS. |
| `make check`, in the agent's worktree before the rebase | exit 0 |
| No secrets in the logs | Every identity integration test logs through a JSON handler that redacts nothing. The test fails if any phone, OTP code or access or refresh token appears; every code the fake SMS received is checked. Two tests confirm the capture is live. |

### What the tests cover

- **AU-01, sign-in.**
  - New and returning customers.
  - One user per phone across all apps.
  - Neither the code nor the phone is stored as given.
  - The refresh token is stored hashed.
  - Sign-in is audited as `identity.sign_in`.
- **AU-06, no account enumeration.** Known and unknown numbers get the same answer, including for admin sends.
- **AU-03, AU-04 and AU-05, send limits.**
  - The 30 s gap returns `Retry-After`. Switching IP or app does not get round it.
  - At most 5 sends per number per hour, sliding. A refused send stores nothing.
  - At most 20 sends per IP per hour.
  - When the same number is sent twice at once, exactly one send wins.
- **AU-02 and AU-03, verify.**
  - Wrong codes count `attemptsLeft` down. After 5 the code is `otp_locked`, even when the right code is entered.
  - Codes expire after 5 min and work once.
  - A code for another app is refused, and so are unknown and malformed request ids.
- **Test numbers.** The reserved test numbers accept `000000` in test, and nothing else does. Staging refuses it.
- **AU-20, Turnstile.** A failed check returns 403 and nothing is stored or sent. A provider error returns 503.
- **SMS failure.** Returns 503, and the send still counts towards the limits.
- **AU-07, refresh.**
  - Refresh rotates the token, and the session id stays the same.
  - Replaying an old token revokes the whole family, the newer token too. It is audited as `identity.refresh_reuse`, and the access token fails on its next request.
  - A token for the wrong app, an expired one, a malformed one and an unknown one are all refused.
- **Logout.**
  - Revokes only that sign-in, also when the token has been rotated.
  - No cookie returns 401. A junk token or another app's token changes nothing. A second logout succeeds.
  - Over HTTP, the access token stops working at once.
- **AU-17, blocked users.**
  - `CheckSession` reports the block.
  - Refresh, verify and `GetMe` return `account_blocked`.
  - A deleted user gets 403.
- **Admin.**
  - An unknown number gets 403, and no user is created. A customer number gets 403 too.
  - Roles come from `admin_users`. The token's `amr` is `otp`.
  - The 12 h expiry is absolute: refresh does not extend it.
  - Disabling an admin stops both the live session and the next refresh.
- **Me.**
  - The phone is masked, and the referral code uses the right alphabet.
  - A stale `If-Match` returns 412.
  - Patching only the language keeps the name. `null` clears the name.
  - The no-show limit is shown. The business app has no customer section.
- **D-14 and AU-16, HTTP.**
  - The cookie is `__Secure-tz_rt_c`, on `Path=/v1/auth`. It is host-only, HttpOnly, Secure and SameSite=Strict, with Max-Age 30 days.
  - No response body carries a refresh token. Responses send `Cache-Control: no-store`.
  - Refresh and logout return 403 and leave the cookie alone when the origin is missing, foreign, another app's or the public site's, or when `X-TZ-Client` is missing.
  - Token reuse over HTTP clears the cookie.
  - `/v1/me` with no token, or a junk bearer token, returns 401.
  - The `ETag` goes from `"v1"` to `"v2"`. A stale one returns 412. A missing `If-Match` returns 422 on `header.If-Match`.
  - A 429 carries `Retry-After: 30`. A wrong code returns `extensions.attemptsLeft`.
  - A bad phone returns 422 and does not echo the number.
- **Platform.**
  - Tokens:
    - forged ones are refused: alg none, HS256 signed with the public key, another key, an unknown or missing kid, bad claims, a flipped signature byte;
    - AU-19 kid rotation works;
    - the parser is fuzzed.
  - Authz: the business scope (AU-09), the admin strongest grant and step-up window (AU-11), table validation, and the goldens.
  - Rate limiter: buckets, the clock moving backwards, sweeping, and concurrent use.
  - PII: sealing is bound to its field, and tampering or another key is detected. Keyed hashes.
  - The middleware in slots 11, 12 and 15.

## Interface summary (for W1-02…W1-11)

- **`authn.Principal{UserID, SessionID, App, AMR, Roles, StepUpAt}`.**
  - Get and set it with `authn.FromContext(ctx)` and `WithPrincipal`.
  - It has `p.IsAnonymous()` and `p.HasRole(r)`.
  - `authn.App` is one of `AppCustomer`, `AppBusiness` or `AppAdmin`. It carries `CookieName` and `RefreshTTL`.
- **`authn.Tokens`.** `NewKeySet(cfg.Keys.JWTSigningKeys, kid)`, `NewTokens(ks, clock)`, `Issue` and `Verify`.
- **`authn.SessionChecker`.** Implemented by `identity.Service.CheckSession`.
- **`authz`.**
  - A domain adds `platform/authz/policy/<domain>.go` with a `Table`, and calls `Register` in `init`.
  - Action kinds:
    - public;
    - self;
    - business: the `X-TZ-Business` scope plus a staff role;
    - admin: role grants, with an optional step-up.
  - Mount with `mw.Authz(engine, "domain.action")`. Strict servers use `authz.Strict(engine, map[operationId]action)`.
  - `authz.DecisionFrom(ctx)` returns the business id and the role.
- **Business staff.** W1-02 must supply `authz.StaffResolver`. Until then, every business-kind action is refused.
- **Account deletion.** `identity.Hooks{Can []CanDeleteAccount, On []OnAccountDelete}` is the seam for phase B.
- **Seed ids.** `seed.identityUserID("customer.asha")` gives fixed ids for other domains' seeds.
- **PII.** Any other domain that stores a phone or an address uses `pii.Sealer` and `pii.Hasher`.

## Files changed outside W1-01's owned paths

- **`backend/cmd/todayz/`.**
  - `routes.go`: `deps` gained `Identity` and `Authz`, which `buildSecurity` fills before the domains mount.
  - `api.go`: `buildRouter` wires slots 11, 12 and 15, and `runAPI` calls `identity.Preflight`.
  - `security.go` and `identity_http_test.go` are new.
  - `migrate.go` passes the keys to the seed.
- **The middleware pipeline.** Slots 11, 12 and 15 are plugged into `backend/internal/platform/httpx/router.go`, `httpx/mw/doc.go` and `httpx/openapi/openapi_test.go`.
- **`backend/internal/platform/pii/`** is a new package.
- **`backend/internal/seed/seed.go`** gained `Deps.Keys`.
- **`backend/internal/jobs/jobs_test.go`** now expects at least 2 periodic jobs.
- **`backend/go.mod` and `go.sum`** add `github.com/golang-jwt/jwt/v5 v5.3.1`. It is the most widely used Go JWT library. It does EdDSA with `crypto/ed25519`, and it lets the parser pin the algorithm, so there is no hand-written JWS parsing.

## Questions (marked `TODO(lead)` in the code where noted)

1. **Wrong OTP code.** 04 names no error code for it. It returns the common `401 unauthorized` with `extensions.attemptsLeft`.
2. **Failed Turnstile check.** Returns `403 forbidden`. A provider outage returns 503. (`TODO(lead)` in `otp.go`.)
3. **Turnstile unset in staging or prod.** 02 §8 asks for "stricter rate limits" but gives no numbers, so the OTP limits stay as they are. (`TODO(lead)` in `providers.go`.)
4. **Generic rate limits.** The numbers in `ratelimit.DefaultRules` are invented and deliberately high. That limiter is off in local and test; the OTP limits apply everywhere. (`TODO(lead)` in `ratelimit.go`.)
5. **MSG91.** The request body shape needs confirming with MSG91. (`TODO(lead)` in `providers.go`.)
6. **`isNewUser`.** It is true only when verify created the user. Someone created through the business app who signs in to the customer app for the first time is not new.
7. **AU-06 for admins.** An admin send to a non-admin number returns 200 and sends a real SMS. That way the console never reveals who is an admin. The cost is SMS spend, which the limits cap.
8. **Admin with OTP only.** Until phase B adds TOTP, an admin's token has `amr=otp`. Phase B must make admin actions require OTP plus TOTP. Today the engine checks only roles and step-up.
9. **Strongest grant.** When an admin holds several roles, the engine applies the strongest grant for each action. Please confirm this reading of the role table.
10. **Sliding sessions.** Customer and business sessions slide: each refresh restarts the 30 days. Admin sessions last 12 h, absolute. (`TODO(lead)` in `session.go`.)
11. **Concurrent refresh.** If the same token is refreshed twice at the same moment, the second counts as reuse and revokes the family. The PWAs (W1-08) must make sure only one refresh runs at a time across tabs.
12. **Session check on every request.** Each authenticated request reads the session row, an indexed primary-key lookup, so that AU-17 takes effect at once. A short cache can come later if the load tests need one.
13. **SMS failures count.** A send that the SMS provider fails still counts towards the limits.
14. **A deleted user signing in again** gets `403 forbidden` until phase B releases the phone identity. (`TODO(lead)` in `otp.go`.)
15. **Missing `If-Match`.** The contract marks `If-Match` as required, so the validator returns 422 (`fields: header.If-Match`) before the handler can return 428 `precondition_required`. Either make it optional in the contract, and the handler then returns 428 as 04 §1 documents, or accept 422.
16. **Logout with an unknown or junk token** returns 204 and clears the cookie. Only a missing cookie gets 401.
17. **Profile updates are not audited.** They change only the name and language. Sign-in, reuse, logout and admin disable are audited.
18. **Session retention.** A session is deleted by `identity.purge` 30 days after it expires or is revoked. (`TODO(lead)` in `purge.go`.)
19. **Blank name.** `PATCH /v1/me` refuses a blank name with 422 on `body.name`. `null` clears it.
20. **Seed without keys.** It skips with a warning that names `PII_ENC_KEY` and `PII_HASH_KEY`, rather than failing, so `make reset` works before `make setup` has generated the keys.
21. **W1-11 overlap.** The `identity.purge` periodic job is registered in `internal/jobs/identity.go`. If W1-11 changes the registry API, moving it is a one-line change.

## Phase B (not built; the seams are in place)

- **Admin TOTP:** enrol, verify and step-up. `sessions.totp_verified_at` exists and `TOTP_ENC_KEY` is already loaded. `StepUpAt` already flows from `CheckSession` into the principal and the engine.
- **Recovery codes.** The `admin_recovery_codes` table exists.
- **Admin seed** from `SEED_ADMINS_FILE`.
- **Logout-all:** revoke every family a user has.
- **`DELETE /v1/me`** through `identity.Hooks` (`CanDeleteAccount`, `OnAccountDelete`, `Deps.Hooks`), including releasing the phone identity.

## Not verified

- **Real MSG91 and real Turnstile.** Both are fakes in the integration tests. In the HTTP tests, SMS goes to the drop provider and Turnstile always passes. The request shape was not tried against the local mocks server, because parallel agents must not run `make dev`.
- **Cookie handling in real browsers** by the PWAs (W1-08), and Caddy's host-only cookies on `*.todayz.localhost`.
- **The generic limiter under real load.** It is off in local and test, and k6 load tests belong to a later wave.
