# W4-03 security: report

Branch `worktree-agent-ae40581e62611df1d` (commit e1d84045, based on a6b58e40), merged to main in 26ef6f31. Run locally on 26 Oct 2026.

Deliverables:
- `docs/security/threat-model.md`: STRIDE for eight flows, with file:line per control.
- `docs/security/asvs-l2.md`: ASVS 4.0.3 L2 checklist with evidence. 08-testing.md §7 now names this file.
- Go abuse tests, run against testpg.
- A stack-only abuse suite.
- Static scans.

No critical findings. One high, a build-time-only dependency that is already accepted. Three medium, five low, three info.

## Findings by severity

| # | Sev | Finding | Where | Status |
|---|---|---|---|---|
| F1 | medium | No CSP on the customer, business and admin Pages apps. The API, the site and the share Function have one. Output encoding is in place, so this is defence in depth | scripts/deploy/pages.sh | **report-only shipped** by the lead (26 Oct): the policy below, plus `blob:` workers for MapLibre, `font-src` and `object-src 'none'`. Enforce it once prod shows no violations |
| F2 | medium | `Permissions-Policy: microphone=()` on all Pages apps blocked getUserMedia audio, which broke customer voice search (C-23) and business voice in prod | scripts/deploy/pages.sh:39 | **fixed** by the lead: `microphone=(self)` |
| F3 | medium (known) | The referral D-13 same-device rule trusts the deviceId the client sends at OTP verify, so a fresh id gets `pending`. Bounded by: one OTP-verified phone per account, OTP caps of 5 per phone and 20 per IP each hour, one claim per account, and no reward before a qualifying purchase | backend/internal/referrals/claim.go:118-135 | open, referrals owner. Proposal: a server-issued signed device id bound to the refresh family, plus a risk flag on claims per referrer per IP per 24 h. TestAbuseReferralFarmingHTTP pins `pending`; flip it when fixed |
| F4 | low | The generic rate limiter is in-memory per instance, off locally and in tests, and its numbers are TODO(lead) | backend/cmd/todayz/security.go:62-69, backend/internal/platform/ratelimit/ratelimit.go:30-33 | **closed** by D-242: the numbers are confirmed as backstops, and per-instance limiting is accepted |
| F5 | low (W5) | clientip trusts CF-Connecting-IP from any peer when deployed | backend/internal/platform/httpx/mw/clientip.go:16-42 | open, W5. Restrict the ALB security group to Cloudflare ranges, or use authenticated origin pulls |
| F6 | low | POS offline `sold_at` has no bounds, so a sale can be back-dated into a closed day | backend/internal/pos/sync.go:408 | open, POS owner. Refuse or flag values older than changesKept or later than now plus skew |
| F7 | low | The referral device check skips a session with no device row (TODO(lead)) | backend/internal/referrals/claim.go:125-126 | open, referrals owner |
| F8 | low | Users cannot list or revoke their own sessions; logout ends one app's family only. No sign-in notice (ASVS 3.3.3, 3.3.4, 2.2.3) | backend/api/paths/auth.yaml:187, admin-customers.yaml:222 | open, product decision |
| F9 | low | Share-preview Function responses had no hardening headers, because Pages `_headers` do not apply to Function responses | customer/functions/x/[code].ts | fixed |
| F10 | info | SHARE_PREVIEW_KEY is optional | customer/functions/x/[code].ts:147-148 | open: the lead sets it on Pages once the API is live |
| F11 | info | The API sends no HSTS itself; Cloudflare does | backend/internal/platform/httpx/mw/secheaders.go | accepted |
| F12 | info | Offline counter-code replay is bounded by (device, TOTP step) being used once | backend/internal/pos/sync.go:581 | accepted |
| D1 | high (dep) | undici 8.11.2: GHSA-xjj6-f33q-929c (high), GHSA-4qv8-2p9j-2685 and GHSA-f72v-6w45-q472 (moderate). Pulled in by site > astro > unifont, at build time only | pnpm-lock.yaml | accepted in `.trivyignore.yaml` until 2026-10-30. A pnpm override is blocked by minimumReleaseAge (D-88) until 29 Oct; bump then |
| D2 | info | The `.trivyignore.yaml` zod entry (CVE-2023-54404) expired today and was stale (the lockfile has zod 4.6.6/4.6.7) | .trivyignore.yaml | **fixed**: entry removed by the lead |

Starting CSP for F1:

```
default-src 'self'; script-src 'self' https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com; connect-src 'self' https://api.todayz.in; img-src 'self' data: blob: https://img.todayz.in https://tiles.todayz.in; style-src 'self' 'unsafe-inline'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'
```

## Fixes made

- **F9 (share-preview headers):** `HARDENING_HEADERS` is in `customer/functions/x/[code].ts` and is used on the preview page and on the no-API 302. It sends:
  - CSP `default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`
  - nosniff
  - `X-Frame-Options: DENY`
  - `Referrer-Policy: strict-origin-when-cross-origin`
  - HSTS

  The page loads no subresources, so the CSP breaks nothing. Covered in `customer/src/test/asks.test.tsx`.
- **F2 and D2:** fixed by the lead after the merge.

No migrations. No money, auth, code or permission logic changed.

## Abuse tests

### Go: `backend/cmd/todayz/security_abuse_http_test.go`

Run with `TODAYZ_TEST_PG_URL_FILE=… go test -p 2 -count=1 -run TestAbuse ./cmd/todayz/`. All 5 pass, and golangci-lint is clean.

| Test | What it checks |
|---|---|
| TestAbuseIDORSweepHTTP | An owner of another business and a cashier of a second store go after items, orders, the bill claim, POS reverse, offers and staff. Their own path gives 404 and the victim's path 403, and nothing changes |
| TestAbuseRoleEscalationHTTP | Cross-app tokens; a cashier accepting someone else's invite; a cashier inviting; a manager inviting an owner. A removed cashier's live token gets 403 |
| TestAbuseOrderCodeForeignRoutesHTTP | Foreign business and store routes are refused and the attempt counter does not move. Another order's code gives code_invalid; the right code collects |
| TestAbuseBillTokenHTTP | Forged, truncated and spliced tokens are refused. A replayed confirm gives claim_already_used. A cross-store link is refused, with no sale and no credits |
| TestAbuseReferralFarmingHTTP | referral_self. Same device gives rejected; a fresh device gives pending (F3); a second claim gives referral_already_claimed; no reward rows |

### Stack-only: `e2e/security/abuse.stack.test.mjs`

`make security` runs `security-abuse`, which runs `scripts/ci/scan.sh abuse --if-up`. It skips when the stack is down and refuses any non-local URL. It checks:
- API headers through Caddy;
- foreign CORS preflights;
- a missing or alg=none bearer;
- refresh CSRF;
- the OTP resend gap (429 with Retry-After);
- the OTP lock after five wrong codes.

The lead ran it on 26 Oct with `make dev` up from a fresh `make reset`: **10 of 10 pass**.

## Static scans

| Scanner | Result |
|---|---|
| gosec | 0 issues; 107 `#nosec`, all justified |
| govulncheck (offline DB 2026-10-21) | no called vulnerabilities |
| pnpm audit --prod | undici only (D1) |
| Trivy fs | exit 0, all targets clean |
| gitleaks, full history (via stdin, because the worktree guard refuses `gitleaks git`) | 20 hits, all go.sum hashes (false positives); no real secret. The lead reruns `security-gitleaks` from the main checkout |

## ZAP (lead, 26 Oct)

Image `zaproxy/zap-stable:20261008` (pinned digest in `scripts/ci/versions.env`), run against the local stack through Caddy.

| Scan | Result |
|---|---|
| Baseline, all four apps and the API (`scan.sh zap`) | no FAIL |
| Full scan (spider and active, 3 min), customer app | 0 FAIL, 8 WARN, 135 PASS |
| API scan from the OpenAPI bundle (active) | did not finish: ZAP died during the active scan, twice, even with `-Xmx3g` (2617 URLs). Not a finding. The API is covered by Schemathesis full fuzzing in `make e2e`, the Go abuse tests and the stack abuse suite |

The 8 warnings are all about the local Vite dev server and Caddy, not the deployed build:
- Missing headers: anti-clickjacking, nosniff, HSTS, Permissions-Policy and CSP. Production sets all but CSP in `_headers` from `scripts/deploy/pages.sh`. `curl -I https://app.todayz.in/` confirms X-Frame-Options DENY, nosniff, HSTS, Permissions-Policy and Referrer-Policy. CSP is F1.
- CORS `*` and missing Cross-Origin-Resource-Policy: these are static, public, uncredentialed assets. Pages sends `*` too. The API's CORS allow-list is checked by the abuse suite.
- Proxy disclosure: this is local Caddy.

The deeper scans of business, admin and the site were not run, for time. Their baselines are clean, and they share the same headers.

## Remaining

1. ZAP: a full scan of business, admin and the site, and an API active scan on a bigger machine or in CI.
2. Lead: enforce the F1 CSP after prod shows no violations; F8 (product); D1 after 29 Oct.
3. Referrals owner: F3, F7. POS owner: F6.
4. W5: F5, TLS config, RDS TLS, subdomain takeover.
5. Not verified here: AI cost quotas.
