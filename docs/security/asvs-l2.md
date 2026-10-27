# ASVS 4.0.3 Level 2 checklist (W4-03)

- **Scope:** the Go API (`backend/`), the four Pages apps, and the customer Pages Function. Checked at base `a6b58e40` on 26 Oct 2026; the 2.2.1, 2.7.1, 2.8, 4.3.1 and 4.3.3 rows were updated for D-264 on main at `a0402006`, 27 Oct 2026.
- **Status values:** pass, gap, partial, n/a.
- **Evidence:** a `file:line` or a test. Go paths are relative to `backend/`.
- **Coverage:** the items are grouped by ASVS chapter. Requirements that cannot apply to this stack are collapsed into one n/a row per chapter. TodayZ has no passwords, file execution, XML, GraphQL, SOAP or server-side templates.
- **Name:** 08-testing.md §7 names this file `docs/security/asvs.md`. The lead asked for `asvs-l2.md`, so the plan reference needs a one-line fix. This is noted in the W4-03 report.

## V1 Architecture

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 1.1.2 | Threat model for the design | pass | `docs/security/threat-model.md` |
| 1.2.3 | One vetted authentication mechanism | pass | `internal/platform/authn/` (slot 11). Every route goes through it, and `cmd/todayz/contract_test.go:45` ties routes to the contract |
| 1.4.1 | Access control is enforced at a trusted point | pass | slot 15, `internal/platform/httpx/mw/authz.go:17`, `internal/platform/authz/engine.go:101` |
| 1.4.4 | One access-control mechanism | pass | the `authz.Engine` with per-domain tables (`internal/platform/authz/policy/`) |
| 1.5.1 | Input and serialisation rules defined | pass | the OpenAPI contract (`backend/api/`), checked by the validator at slot 13 |
| 1.6.1 | Cryptographic key management policy | partial | Keys come from env and are checked at boot (`internal/identity/identity.go:178-222`). Rotation is a W5 runbook item |
| 1.8.1 | Sensitive data identified and classified | pass | 03 data model. Phones, payee bank details and codes are sealed with AES-GCM and hashed with HMAC (`internal/platform/pii/pii.go:3`) |
| 1.14.6 | No unsupported client-side tech | pass | React PWAs only |

## V2 Authentication

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 2.2.1 | Anti-automation on authentication | pass | Turnstile, a 30 s resend gap, 5 per phone per hour and 20 per IP per hour (`internal/identity/otp.go:52-104`), and 5 verify attempts per code. Admin sign-in also has a cumulative limit (D-264): 10 wrong codes for one number in 24 h, across requests, lock its admin sign-in for 24 h (`internal/identity/signin_limits.go`; settings `admin_signin_*`), unlocked early only by another super_admin. Admin step-up sends have their own 10 an hour. Stack test: `e2e/security/abuse.stack.test.mjs`; `internal/identity/signin_limits_test.go` |
| 2.2.3 | Notify the user after auth changes | gap (low) | No sign-in or new-device notice to the user. An OTP SMS is the only signal |
| 2.5.x | Credential recovery | n/a | No passwords. Recovery is an OTP to the phone |
| 2.7.1 | Out-of-band authenticator not via insecure channel | partial | SMS OTP is the product decision (01 §2), for admins too since D-264 (admin step-up is a second SMS code, not a separate factor) |
| 2.7.2 | OOB code expires within 10 min | pass | 5 min (`internal/identity/identity.go:44`) |
| 2.7.3 | OOB code used once | pass | the request is consumed on success (`internal/identity/otp.go:212-246`) |
| 2.7.4 | OOB code bound to the request | pass | HMAC over request id and code (`internal/identity/otp.go:409-411`) |
| 2.7.6 | OOB code ≥ 20 bits of entropy | gap (accepted) | 6 digits is about 20 bits. With 5 tries and a 5-minute TTL this is the product rule (01 §2) |
| 2.8.x | Single- or multi-factor one-time verifier (TOTP) | n/a | no user-facing TOTP since D-264 (the POS counter code is a device code checked by the shop, not an authenticator) |
| 2.9.x | Cryptographic authenticator | n/a | none |
| 2.10.x | Service secrets not in code | pass | all secrets come from env (`make env-check`). gitleaks over the history is clean apart from go.sum false positives (REPORT) |
| 2.1.x, 2.3.x, 2.4.x | Passwords | n/a | no passwords |

## V3 Session management

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 3.1.1 | No session token in the URL | pass | Bearer header and the refresh cookie only. The access log leaves out the query (`internal/platform/httpx/mw/accesslog.go:30`) |
| 3.2.1 | New token on authentication | pass | a new family on each OTP verify (`internal/identity/session.go`) |
| 3.2.2 | Token ≥ 64 bits of entropy | pass | 32 random bytes (`internal/identity/identity.go:53`, `internal/identity/session.go:63`) |
| 3.2.3 | Stored securely in the browser | pass | refresh cookie is HttpOnly, Secure, SameSite=Strict, Path=/v1/auth (`internal/identity/handlers.go:132-150`). The access token is in memory only (customer/CLAUDE.md W1-08) |
| 3.3.1 | Logout invalidates the session | pass | `internal/identity/session.go:302` revokes the family. The access token dies at the next request's session check (`internal/platform/authn/authenticator.go:57-90`) |
| 3.3.2 | Re-authentication after idle or absolute time | pass | 15 min access TTL, then refresh. Admin step-up after 10 min (`internal/platform/authn/principal.go:95`) |
| 3.3.3 | Option to end all other sessions | gap (low) | No self-service route. Logout ends one app's family only (`backend/api/paths/auth.yaml:187`). Support can revoke a customer's sessions (`backend/api/paths/admin-customers.yaml:222`), and blocking revokes all of them |
| 3.3.4 | Users can see and revoke active sessions | gap (low) | as 3.3.3: the list exists for admins only (`backend/api/paths/admin-customers.yaml:52-77`) |
| 3.4.1 | Cookie Secure | pass | `internal/identity/handlers.go:140` |
| 3.4.2 | Cookie HttpOnly | pass | `internal/identity/handlers.go:140` |
| 3.4.3 | Cookie SameSite | pass | Strict, `internal/identity/handlers.go:140` |
| 3.4.4 | `__Host-`/`__Secure-` prefix | pass | `__Secure-tz_rt_{c,b,a}` (`__Host-` is not possible because Path=/v1/auth) |
| 3.4.5 | Cookie path scoped | pass | `/v1/auth` |
| 3.5.2 | Session tokens rather than static API secrets | pass | |
| 3.5.3 | Stateless tokens signed and checked | pass | EdDSA, kid pinned, alg pinned (`internal/platform/authn/tokens.go:111-120`) |
| 3.7.1 | Full session or re-auth before sensitive actions | pass | admin † actions need step-up (`internal/platform/authz/engine.go:180`). DELETE /v1/me needs a step_up OTP (`internal/identity/identity.go:51`) |

## V4 Access control

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 4.1.1 | Enforced server side | pass | slot 15 |
| 4.1.2 | Attributes cannot be changed by the user | pass | scope from the path is re-checked against the DB (`internal/platform/authz/engine.go:139-147`). Roles are read live |
| 4.1.3 | Least privilege | pass | per-role grant tables (`internal/platform/authz/policy/*.go`). Role matrix tests in `cmd/todayz/business_http_test.go:344-436` |
| 4.1.5 | Fail securely | pass | an unknown action is a 500, and a nil authorizer refuses (`internal/platform/authz/engine.go:105`, `internal/platform/httpx/mw/authz.go:17`) |
| 4.2.1 | IDOR protection | pass | store-scoped loads plus business scope. `cmd/todayz/security_abuse_http_test.go` TestAbuseIDORSweepHTTP covers items, orders, bill claims, POS sales, offers and staff across stores and businesses, and checks nothing changed |
| 4.2.2 | CSRF protection | pass | Bearer API (no ambient credential), except refresh and logout, which use an Origin allowlist plus `X-TZ-Client` (`internal/identity/handlers.go:239-264`). Tested in `cmd/todayz/identity_http_test.go:147` and the stack suite |
| 4.3.1 | Admin interfaces need MFA | gap (accepted) | Admins sign in with the SMS OTP alone and confirm † actions with a second SMS code (D-264). The founder accepted that this does not protect against a SIM swap |
| 4.3.2 | No directory browsing or metadata exposure | pass | API serves JSON only. Pages serves the built `dist` |
| 4.3.3 | Step-up for high-value actions | pass | `StepUp: true` on † actions, for example payouts (`internal/platform/authz/policy/payouts.go:28-29`); the step-up is a fresh SMS code within 10 min (`internal/identity/stepup.go`, D-264) |

## V5 Validation, sanitisation, encoding

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 5.1.1 | HTTP parameter pollution | pass | the strict oapi-codegen binding plus the slot 13 validator |
| 5.1.3 | Input validated against an allowlist | pass | OpenAPI schemas, with extra checks in handlers (for example the payment method allowlist, `internal/orders/transitions.go:487-490`) |
| 5.1.4 | Structured data strongly typed | pass | generated types |
| 5.1.5 | Redirects to allowlisted targets only | pass | `?next=` must be a same-app path (customer/CLAUDE.md `next.ts`, `safeNext`). The share Function redirects same-origin only (`customer/functions/x/[code].ts:23-25`) |
| 5.2.x | Sanitisation of HTML input | pass | No HTML input is accepted. The share page escapes all values (`customer/functions/x/[code].ts:41-43`) |
| 5.3.1 | Context-aware output encoding | pass | React escapes by default. The share Function uses `esc()` |
| 5.3.3 | XSS protection | partial | output encoding everywhere. **No CSP on the Pages apps** (`scripts/deploy/pages.sh:29-45`) |
| 5.3.4 | Parameterised queries | pass | sqlc and pgx only. gosec G201/G202 are clean |
| 5.3.8 | OS command injection | pass | no exec of user input. gosec G204 is clean |
| 5.5.x | Deserialisation | pass | JSON into typed structs only |
| 5.2.4-5.2.8, 5.3.7, 5.3.10 | eval, SSTI, SVG/scriptable content, LDAP, XPath, XML | n/a | none in the stack. Uploads are photos and PDFs on presigned PUT URLs, never rendered by the API |

## V6 Stored cryptography

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 6.1.1 | Regulated private data encrypted at rest | pass | AES-256-GCM under PII_ENC_KEY (`internal/platform/pii/pii.go:29-45`) |
| 6.2.1 | Crypto modules fail securely | pass | a seal or open error is returned. Keys are checked at boot (`internal/identity/identity.go:178-222`) |
| 6.2.2 | Proven algorithms | pass | AES-GCM, HMAC-SHA256, Ed25519 |
| 6.2.5 | No insecure modes or padding | pass | GCM only |
| 6.2.6 | Nonces unique | pass | random 12-byte nonce per seal (`internal/platform/pii/pii.go:29`) |
| 6.3.1 | Random numbers from a CSPRNG | pass | crypto/rand for codes and tokens. The one math/rand use is jitter, annotated `#nosec G404` (`internal/pos/sync.go:189`) |
| 6.4.1 | Key management | partial | separate keys per purpose (`PII_ENC_KEY`, `ORDER_CODE_ENC_KEY`, `DEVICE_SECRET_ENC_KEY`, …); the old TOTP_ENC_KEY ≠ PII_ENC_KEY check went with the key (D-264). The secrets-manager design is W5 |

## V7 Errors and logging

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 7.1.1 | No credentials or session tokens in logs | pass | redacting handler (`internal/platform/telemetry/log.go:30-68`) and no query string in the access log |
| 7.1.2 | No other sensitive data in logs | pass | phones are hashed or masked. The share key is never logged (`customer/functions/x/[code].ts:8`) |
| 7.1.3 | Security events logged | pass | refresh reuse (`internal/identity/session.go:192-193`), lockouts and step-up are audited |
| 7.2.1 | Authentication decisions logged | pass | OTP outcome metrics and audit |
| 7.3.1 | Log injection | pass | structured JSON (slog) |
| 7.4.1 | Generic error to the user | pass | RFC 7807 problems with registered codes only (`e2e/contract/hooks.py` todayz_problem check). Recover middleware at slot 3 |

## V8 Data protection

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 8.1.1 | No caching of sensitive data in server components | pass | `Cache-Control: no-store` default on /v1 (`internal/platform/httpx/mw/secheaders.go:15`) |
| 8.2.1 | Anti-caching headers on sensitive data | pass | as above |
| 8.2.2 | No sensitive data in browser storage | pass | the access token is in memory and the pickup code only in the query cache (customer/CLAUDE.md W1-08, W2-09) |
| 8.3.1 | Sensitive data in the body or headers, not the URL | pass | Bill tokens and order codes are in bodies. Receipts are a hashed token in the path |
| 8.3.4 | Sensitive data identified | pass | 03 data model, PII columns |
| 8.3.5 | Access to sensitive data audited | pass | payout export audited with ids only (`internal/platform/authz/policy/payouts.go:28`) |
| 8.3.8 | Retention policy | pass | `internal/identity/purge.go` (account deletion purge) |

## V9 Communications

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 9.1.1 | TLS for all client connections | pass | Cloudflare in front of Pages and the API. Pages HSTS (`scripts/deploy/pages.sh:38`). The Function now sends HSTS too (`customer/functions/x/[code].ts:38`) |
| 9.1.2 | Strong TLS configuration | not verified | Cloudflare and ALB settings are W5 Terraform. Not checked here |
| 9.2.1 | Trusted certs for outbound connections | pass | Go default roots. Provider base URLs are `*_API_URL` |
| 9.2.2 | Backend connections encrypted | partial | RDS TLS is W5. Not verifiable locally |

## V10 Malicious code

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 10.2.1 | No phone-home or unexpected egress | pass | `scripts/no-egress.sh` around `make check` and `make e2e` (SC-PL-04) |
| 10.3.2 | Integrity of third-party code | pass | pnpm lockfile with minimumReleaseAge (D-88), hash-pinned Python (`e2e/contract/requirements.txt`), go.sum |
| 10.3.3 | Subdomain takeover | not verified | Cloudflare DNS is W5 |

## V11 Business logic

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 11.1.1 | Business flows in sequential order | pass | the order state machine (`internal/orders/machine.go`) and invalid transitions refused (`internal/orders/transitions.go:500-503`) |
| 11.1.2 | Realistic human timing | partial | OTP resend gap, code lock and bill claim TTL. The generic limiter is off locally and its numbers are TODO(lead) |
| 11.1.3 | Limits per business rule | pass | credit caps per store per day and per customer per month (`internal/credits/credits.go:79-80`), and one referral claim per account |
| 11.1.4 | Anti-automation against excessive calls | partial | as 11.1.2 |
| 11.1.5 | Business logic limits against known attacks | partial | referral farming bounded but the device rule is advisory (threat model §3). POS `sold_at` is unbounded (§4) |
| 11.1.6 | No TOCTOU on money | pass | FOR UPDATE on credit accounts, bill claims and orders (`internal/credits/credits.go:22`, `internal/pos/claims.go:493`, `internal/orders/transitions.go:495`). Race tests: `internal/payouts/payouts_race_db_test.go`, `internal/identity/account_race_test.go` |
| 11.1.7 | Monitor for unusual events | pass | risk flags (`internal/risk`), code lockout trigger, refresh-reuse audit |
| 11.1.8 | Configurable alerting | partial | Grafana alerts (W4-02) |

## V12 Files and resources

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 12.1.1 | Large files refused | pass | the body limit at slot 10. Upload PUTs are signed with the declared Content-Length, and the stored object's size is re-read (`internal/support/me.go:252`, `internal/support/presign.go:110`) |
| 12.4.1 | Uploads stored outside the web root | pass | S3/MinIO buckets |
| 12.5.2 | Uploads never executed or served as HTML | pass | images and PDFs from the img host with their Content-Type. Image checks in `internal/ai/imagecheck` |
| 12.6.1 | SSRF allowlist | pass | outbound calls only to configured `*_API_URL` hosts. The share Function calls only `API_URL` with `redirect: 'manual'` |
| 12.2.x, 12.3.x | Path traversal or execution of user files | n/a | no server-side file paths come from users |

## V13 API

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 13.1.1 | Same encoding and parser everywhere | pass | pathguard (`internal/platform/httpx/mw/pathguard.go:13`) |
| 13.1.3 | No sensitive data in API URLs | pass | as 8.3.1 |
| 13.1.4 | Authorization at the URI and the resource | pass | slot 15 plus store-scoped loads |
| 13.1.5 | Content type checked | pass | the validator at slot 13 refuses a non-JSON body |
| 13.2.1 | Methods allowlisted | pass | chi routes only the contract's methods (`cmd/todayz/contract_test.go:45`) |
| 13.2.2 | JSON schema validation | pass | slot 13 |
| 13.2.3 | CSRF on cookie-based endpoints | pass | only refresh and logout use the cookie. See 4.2.2 |
| 13.2.5 | Content-Type checked on responses | pass | `nosniff` and JSON |
| 13.2.6 | Headers and payload integrity | pass | TLS. Idempotency keys on writes (slot 14) |
| 13.4.x | GraphQL | n/a | |

## V14 Configuration

| ID | Requirement | Status | Evidence |
|---|---|---|---|
| 14.1.1 | Repeatable builds | pass | `make gen` plus lockfiles. CI runs the same steps |
| 14.2.1 | Components up to date | partial | govulncheck: no called vulnerabilities. Trivy fs gate: clean. pnpm audit --prod: 1 high and 2 moderate in undici 8.11.2, build-time only (astro > unifont), accepted in `.trivyignore.yaml` until 30 Oct. The zod entry there expires 26 Oct. See the REPORT |
| 14.2.2 | Unneeded features removed | pass | `/v1/test/*` only in local and test (`cmd/todayz/main_test.go:124` TestRoutesPerEnv). Test code 000000 also needs a loopback API_ADDR |
| 14.2.4 | Third-party assets with SRI or self-hosted | pass | Fonts and icons are bundled. Tests refuse Google Fonts hosts (`shared/tokens/test/tokens.test.ts`, `site/src/lib/verify-dist.test.ts`). The one third-party script is Turnstile (`shared/auth/src/turnstile.ts`), which cannot carry SRI because Cloudflare versions it |
| 14.3.2 | Debug modes off in prod | pass | `Env.IsDeployed()` gates. Hidden source maps (`sourcemap: 'hidden'`, dist-check) |
| 14.3.3 | No version leaks in headers | pass | no Server or X-Powered-By from the API |
| 14.4.1 | Content-Type with charset | pass | |
| 14.4.3 | CSP on every response | gap | the API has a deny-all CSP and the Function now has one, but **the customer, business and admin Pages apps have none** (`scripts/deploy/pages.sh:29-45`) |
| 14.4.4 | `X-Content-Type-Options: nosniff` | pass | API, Pages and the Function |
| 14.4.5 | HSTS | pass | Pages (`scripts/deploy/pages.sh:38`) and the Function (`customer/functions/x/[code].ts:38`). The API relies on Cloudflare |
| 14.4.6 | Referrer-Policy | pass | API `no-referrer`. Pages `strict-origin-when-cross-origin` |
| 14.4.7 | Framing controlled | pass | API `frame-ancestors 'none'`. Pages `X-Frame-Options: DENY` |
| 14.5.1 | Only needed HTTP methods | pass | as 13.2.1 |
| 14.5.2 | Origin header not used for auth decisions alone | pass | the Origin check on refresh is CSRF defence on top of the cookie |
| 14.5.3 | CORS allowlist | pass | `internal/platform/httpx/mw/cors.go`. A foreign preflight gets no ACAO (stack suite) |

## Summary

| Status | Count (rows above) |
|---|---|
| gap | 5 (14.4.3 CSP on the Pages apps; 3.3.3 and 3.3.4 no self-service session list or revoke; 2.2.3 no sign-in notice; 2.7.6 OTP entropy, accepted by product rule) |
| partial | 12 |
| not verified | 3 (9.1.2, 10.3.3 and 9.2.2 depend on W5 cloud config) |
| pass | the rest |
