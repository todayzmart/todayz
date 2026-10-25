# W3-02 Risk and fraud: report

Base `main` f033f31, branch `claude/hopeful-zhukovsky-51926d`, local only (not pushed). Decisions are in D-225 (12-decision-log), the numbers in 01 §17 Risk.

## What was built

- **Backend `internal/risk`**: six rules run by the River job `risk_scan` (every 5 minutes, timeout 1m) that write only `risk_flags`:
  - `orders.code_lockouts`
  - `pos.bill_claim_spread`
  - `pos.reversal_ratio`
  - `referral.self_like`
  - `identity.otp_ip_fanout`
  - `identity.otp_failed_phone`

  Flags other packages already raise show in A-13 with evidence read from their own rows:
  - `referral.device_accounts`, `referral.reversed_after_reward`, `referral.reward_refused`
  - `credits.store_daily_cap`, `credits.customer_monthly_cap`
  - `pos.offline_links`, `pos.offline_offer_without_discount_role`
  - `store_strikes` (D-67)
  - `return_unconfirmed` (D-69)
- **Migration `30201_w3-02_risk.sql`**:
  - `order_code_lockouts`, append-only and written by a trigger on `orders`, so orders makes no risk call
  - `risk_flags.review_note`
  - seven new settings
  - the scan indexes
- **API** (domains `admin-risk` and `admin-referrals`):
  - `GET /v1/admin/risk/flags` (filters `status` and `rule`, open count per rule, cursor)
  - `GET /v1/admin/risk/flags/{id}` (facts, evidence, the subject's other flags, `assistEnabled`)
  - `POST …/clear` and `POST …/confirm`, each with a reason
  - `GET /v1/admin/referrals` (filters `status`, `kind` and `flagged`)
  - `POST /v1/admin/referrals/{id}/reject`, with a reason

  The policy golden is `authz/policy/testdata/risk.golden`:
  - `admin.risk.view`: support and up
  - `admin.risk.resolve`, `admin.referrals.view`, `admin.referrals.reject`: admin and super_admin
- **Audit**:
  - `risk.flag_clear` and `risk.flag_confirm` on subject `risk_flag`
  - `referral.reject` on subject `referral`
  - the reason is kept on every audit row
- **Admin A-13 Risk** (`admin/src/risk/`):
  - status switch, rule tabs with open counts, everything in the URL
  - detail with facts, an evidence table, the subject's other flags and the History (audit) panel
  - Clear and Confirm, each with a required reason
  - after a confirm, Block customer or Suspend business as its own audited call; the confirm itself never blocks anyone
  - `<AssistSlot/>` (D-82) mounted only when `ai_riskexplain_enabled` is on
  - support sees the same screen read-only
- **Admin A-10 Referrals** (`admin/src/referrals/`):
  - filters for status, kind and "only with open risk flags", all in the URL
  - detail with both parties, the credits, timestamps, links to the open flags in A-13, and the History panel
  - Reject while pending, or while qualified and unpaid; never after a payout

Nothing in W3-02 moves money or blocks a person automatically. Every threshold is read from `platform_settings`. No flag, fact, evidence line or log holds a phone number, an OTP, an order or counter code, or a hash.

## SC-RK scenarios

The plan says "SC-RK-01…15" without listing them. The list below is how W3-02 numbers them, at least one per rule (08 §SC rule).

| SC | Scenario | Tests |
|---|---|---|
| SC-RK-01 | More than `risk_code_lockouts_store_per_day` code lockouts at one store in an IST day raise one flag (deduped per day) | `rules_test.go` TestCodeLockoutsMoreThanTheLimit; `db_test.go` TestScanCodeLockouts |
| SC-RK-02 | The lockout day is IST; a "new code" does not erase the count | TestCodeLockoutsDayIsIST; TestScanCodeLockouts (trigger rows) |
| SC-RK-03 | One customer claims bills at `risk_bill_claim_stores` or more stores within `risk_bill_claim_window_minutes` | TestBillClaimSpreadWithinTheWindow; TestScanBillClaimSpread |
| SC-RK-04 | Bill-claim false positives: the same store twice, or claims spread past the window | TestBillClaimSpreadFalsePositives |
| SC-RK-05 | A store's reversal ratio of `risk_reversal_ratio_percent` or more over `risk_reversal_window_days` | TestReversalRatio; TestScanReversalRatio |
| SC-RK-06 | No ratio flag below `risk_reversal_min_sales`; only linked sales count | TestReversalRatio; TestScanReversalRatio |
| SC-RK-07 | A referee's first order at the referrer's own shop, where the referee is staff or invited as staff | TestSelfLikeReferrals; TestScanSelfLikeReferral |
| SC-RK-08 | Self-like through a shared device; a real customer at the shop is not flagged | TestSelfLikeReferrals; TestScanSelfLikeReferral |
| SC-RK-09 | Same device, many referees (`referral.device_accounts`, 01 §8) | `referrals/referrals_test.go` (device-accounts case); A-13 evidence: TestOtherPackagesFlagsHaveEvidence |
| SC-RK-10 | Credits over the cap at one shop (`credits.*`) show with their POS sale as evidence | TestOtherPackagesFlagsHaveEvidence/credits.store_daily_cap |
| SC-RK-11 | Offline links per store per day; an offline counter code never shows | TestOtherPackagesFlagsHaveEvidence/pos.offline_links |
| SC-RK-12 | Store strikes at the threshold (D-67) and delivery-failed returns auto-confirmed after 72 h (D-69) | TestOtherPackagesFlagsHaveEvidence/store_strikes, /return_unconfirmed |
| SC-RK-13 | OTP fan-out: `risk_otp_phones_per_ip_hour` phones from one IP | TestScanOTPAbuse; TestOTPRulesHoldNoHashOrNumber |
| SC-RK-14 | Failed OTPs for one phone in 24 h; a phone with an account is flagged on the user; never a number, hash or code | TestScanOTPAbuse; TestOTPRulesHoldNoHashOrNumber |
| SC-RK-15 | Clear and confirm need a reason, are audited and change nothing else; support cannot | TestClearAndConfirm; `risk.golden`; `admin/src/risk/risk.test.tsx`; `admin/e2e/risk.spec.ts` |
| (A-10) | Reject before any reward, 409 after; audited | TestRejectReferral; `admin/src/referrals/referrals.test.tsx`; `admin/e2e/risk.spec.ts` |
| (held reward) | `referral.reward_refused` holds the reward until the flag is cleared | `jobs/referrals_test.go`; A-13 warns before clearing (risk.test.tsx) |

## False-positive review on the seed

`TestSeedRaisesNoFlags` loads the full dev seed, moves the clock an hour forward and runs every rule. Every rule raises 0 flags and `risk_flags` stays empty. The seed's stores, staff, customers and referrals look like normal use to these rules; staff who shop at their own store do not trip `referral.self_like`, because the rule needs a referral row. The fixtures also hold the false-positive cases above: SC-RK-04, 06 and 08.

## Checks (all local)

| Check | Result |
|---|---|
| Backend `gofmt -l`, `go vet ./...` | clean |
| Backend `golangci-lint run ./...` (repo `.tools/bin`) | 0 issues |
| Backend `go test -race` for admin, risk, authz (+policy), settings, jobs, payouts, referrals, credits, orders, pos, identity, cmd/todayz, platform/realtime | all ok |
| Contract lint and client (`make gen`, contract smoke) | pass (42) |
| i18n `lint:messages` | 0 admin errors |
| Admin typecheck, eslint `--max-warnings 0` | clean |
| Admin vitest | 23 files, 194 tests pass (risk 8, referrals 5 new) |
| Admin `build:e2e` | ok; initial JS 161.07 KB of 250 KB, second budget 9.73 of 20 KB, precache and sourcemap checks pass |
| Admin Playwright | 26 pass, including AD-02 `roles.spec.ts` (Risk and Referrals in each role's sidebar, READS entries) and the 3 new journeys in `risk.spec.ts` |

Notes:
- The production `pnpm run build` stops with "VITE_TURNSTILE_SITE_KEY must be a real Turnstile site key" when no real key is set locally. This is expected; `check-frontend-build` uses `build:e2e` as well.
- The Go tests need `DOCKER_HOST` from the docker context, plus `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE` on colima, as `mk/20-backend.mk` exports them. Run without them, testcontainers fails with "rootless Docker not found". The earlier realtime failure had the same cause and passes with the env set.
- `golangci-lint` is in `.tools/bin` and is on `PATH` only through make.

## Screenshots

- `admin-a13-detail.png`: an admin on an open `referral.self_like` flag, showing facts, evidence, Clear and Confirm
- `admin-a13-queue-support.png`: support's read-only queue with the rule tab
- `admin-a10-detail.png`: A-10 filtered to flagged referrals, with the referral detail, Reject and History

## Open questions for the lead

1. **New numbers (D-225).** The new defaults:
   - bill claims: 3 stores in 10 minutes
   - reversal ratio: 20 % over 7 days, once a store has 10 sales
   - OTP: 10 phones per IP per hour, and 3 failed OTPs per phone per day

   The per-rule scores are fixed in code, and the scan runs every 5 minutes. Please confirm or change these.
2. **Confirm does not block or suspend.** A-13 offers Block customer or Suspend business as a separate audited step. The manual case `docs/qa/manual/ADMIN-console-screens.md` ADMIN-M.23 says "confirm … with 'suspend'" and "Confirm suspends Kaveri Fresh"; in this build that is confirm, then Suspend business. Should ADMIN-M.23 be reworded, or should confirm carry an action?
3. **No seeded flags.** ADMIN-M.23 says "Open each seeded flag kind", but the seed has no flags, and `TestSeedRaisesNoFlags` is the false-positive review. Seeding flag rows directly, outside the rules, would satisfy both. That is a seed change, so I left it to you.
4. **Referral reject semantics.** Reject is allowed while pending, or while qualified with neither side paid; otherwise it is `409 invalid_transition`. Taking paid credits back stays an A-09 adjustment.
5. **Payouts hold on any open flag.** The existing payouts code holds a payout on any open risk flag whose subject is the business or one of its stores. Referral, user, IP and phone flags do not hold it. A W3-02 flag (for example `pos.reversal_ratio` on a store) therefore now holds that business's payout until it is cleared. This is consistent with D-78, but it is a money effect of a flag. Please confirm.
6. **Nav placement.** Risk sits ungrouped after Banner moderation, matching the support sidebar in 06 §3. Referrals sits under Growth after "Credits and referrals" and is admin-only. Finance has no A-10, per 06 §3.
7. **Edits outside my area:**
   - files from W3-05: the admin-settings risk group, the audit subjects `risk_flag` and `referral` with their labels, the settings labels, `roles.spec.ts` (ALL, the support nav and READS) and `fake-api.ts` (an empty `/v1/admin/risk/flags` answer)
   - one comment in `orders/transitions.go` that names the lockout trigger
   - the `risk_scan` job timeout, cut to 1m to meet the job lint
8. **D-225 numbering.** D-225 was the next free number on f033f31. Please renumber it if another package has taken D-225 since.
9. **`ask_reported`** is no longer an A-13 rule: nothing raises it.
10. **Main moved after my base f033f31** (`e2e/contract/schemathesis.toml` has a newer commit). This branch does not touch that file.
