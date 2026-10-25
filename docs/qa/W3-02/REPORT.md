# W3-02 Risk and fraud: report

Base `main` f033f31, branch `claude/hopeful-zhukovsky-51926d`, local only (not pushed). This version includes the fixes from the lead's review of 3aa92c1. Decisions are in D-225 (12-decision-log), the numbers in 01 §17 Risk.

## What was built

- **Backend `internal/risk`**: six rules run by the River job `risk_scan` (every 5 minutes, timeout 1m) that write only `risk_flags`. One scan runs at a time (a transaction advisory lock; an overlapping scan returns without work). Each rule runs in its own savepoint: a failing rule is rolled back, logged by its rule name only, and the scan goes on with the others, then reports the collected errors.
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
  - `ask_reported` (W3-16's "Report this ask", 01 §11), kept as a known rule with its A-13 label

  Dedupe: the code-lockout and bill-claim rules dedupe per IST day; the OTP rules and the reversal ratio dedupe on their own rolling window, so a flag that was cleared is not raised again just because midnight passed.
- **Migration `30201_w3-02_risk.sql`**:
  - `order_code_lockouts`, append-only and written by a trigger on `orders`, so orders makes no risk call
  - `risk_flags.review_note`
  - seven new settings
  - the scan indexes
- **API** (domains `admin-risk` and `admin-referrals`):
  - `GET /v1/admin/risk/flags` (filters `status` and `rule`, open count per rule, cursor)
  - `GET /v1/admin/risk/flags/{id}` (facts, evidence, the subject's other flags, `assistEnabled`)
  - `POST …/clear` and `POST …/confirm`, each with a reason and `from` (the status the reviewer saw). Confirm is from `open` only; clear is from `open` or `confirmed`. If the flag has moved, the call is `409 invalid_transition` and nothing is written, so of two concurrent calls exactly one wins
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
  - Clear and Confirm, each with a required reason; a confirmed flag still offers Clear
  - a business or store flag that is open or confirmed shows "Holds payout"; the Confirm and Clear dialogs say what happens to the payout
  - after a confirm, Block customer or Suspend business as its own audited call; the confirm itself never blocks anyone. On a referral flag the dialog names the party (the referee or the referrer)
  - `<AssistSlot/>` (D-82) mounted only when `ai_riskexplain_enabled` is on
  - support sees the same screen read-only
- **Admin A-10 Referrals** (`admin/src/referrals/`):
  - filters for status, kind and "only with open risk flags", all in the URL
  - detail with both parties, the credits, timestamps, links to the open flags in A-13, and the History panel
  - Reject while pending, or while qualified and unpaid; never after a payout

No flag blocks a person automatically. A flag's one money effect is the payout hold (01 §9, D-225): a flag on a business or one of its stores holds that business's payout while it is open **or confirmed**. Close, unhold and mark-paid all check it. Clearing lifts that flag's hold; finance then releases the payout with Unhold in A-11 once nothing else holds it. `referral.reward_refused` also holds that referral's reward until it is cleared. Every threshold is read from `platform_settings`. No flag, fact, evidence line or log holds a phone number, an OTP or a code.

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
| SC-RK-13 | OTP fan-out: `risk_otp_phones_per_ip_hour` phones from one IP | TestScanOTPAbuse; TestOTPRulesHoldNoNumberOTPOrCode; TestOTPFlagsDoNotComeBackAtMidnight |
| SC-RK-14 | Failed OTPs for one phone in 24 h; a phone with an account is flagged on the user; never a number, OTP or code | TestScanOTPAbuse; TestOTPRulesHoldNoNumberOTPOrCode; TestOTPFlagsDoNotComeBackAtMidnight |
| SC-RK-15 | Clear and confirm need a reason and `from`, are audited and change nothing else; clear works from confirmed; concurrent clear and confirm: one wins, the other is 409; support gets 403 | TestClearAndConfirm; TestConcurrentClearAndConfirm; `cmd/todayz` TestAdminRiskHTTP; `risk.golden`; `admin/src/risk/risk.test.tsx`; `admin/e2e/risk.spec.ts` |
| (payout hold) | A confirmed business or store flag keeps holding the payout; only Clear lifts it, and Unhold in A-11 is still finance's step | `payouts` TestConfirmedRiskFlagKeepsTheHoldDB; risk.test.tsx (the "Holds payout" note on open and confirmed store and business flags only) |
| (scan) | One scan at a time; a failing rule does not stop the others, is logged by rule name and SQLSTATE (or error type) only, and the job result keeps the other rules' counts | TestScanRunsOneAtATime; TestScanKeepsGoingPastAFailedRule; TestFailCauseHasNoErrorText |
| (A-10) | Reject before any reward, 409 after; audited; a reject racing the reward posting either wins with no credit kept or is 409 with both sides paid; support gets 403 | TestRejectReferral; `jobs` TestRejectRacesPostRewardsDB; TestAdminRiskHTTP; `admin/src/referrals/referrals.test.tsx`; `admin/e2e/risk.spec.ts` |
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
| Admin vitest | 23 files, 198 tests pass (risk 12, referrals 5 new) |
| Admin `build:e2e` | ok; initial JS 161.33 KB of 250 KB, second budget 9.73 of 20 KB, precache and sourcemap checks pass |
| Admin Playwright | 26 pass, including AD-02 `roles.spec.ts` (Risk and Referrals in each role's sidebar, READS entries) and the 3 new journeys in `risk.spec.ts` |

Notes:
- The production `pnpm run build` stops with "VITE_TURNSTILE_SITE_KEY must be a real Turnstile site key" when no real key is set locally. This is expected; `check-frontend-build` uses `build:e2e` as well.
- The Go tests need `DOCKER_HOST` from the docker context, plus `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE` on colima, as `mk/20-backend.mk` exports them. Run without them, testcontainers fails with "rootless Docker not found". The earlier realtime failure had the same cause and passes with the env set.
- `golangci-lint` is in `.tools/bin` and is on `PATH` only through make.
- Run with `-p 3` next to twelve other packages, `cmd/todayz` hit go test's 10-minute default while in `TestPOS34ConcurrentReceipts` (a W2 test). Run on its own it passes in 355 s.

## Screenshots

Refreshed after the review fixes.

- `admin-a13-detail.png`: an admin on an open `referral.self_like` flag, showing facts, evidence, Clear and Confirm
- `admin-a13-queue-support.png`: support's read-only queue with the rule tab
- `admin-a10-detail.png`: A-10 filtered to flagged referrals, with the referral detail, Reject and History

## Lead's answers (review of 3aa92c1)

1. **Numbers (D-225).** The defaults stand, except `risk_reversal_min_sales`, now 30.
2. **Confirm and block or suspend stay separate.** ADMIN-M.23 is reworded: confirm, then Suspend business as its own step.
3. **No seeded flags.** ADMIN-M.23 now raises flags through the rules' own conditions (or `make demo` traffic) and waits for `risk_scan`; the seed stays clean for `TestSeedRaisesNoFlags`.
4. **Referral reject semantics** are fine as built.
5. **Payout hold.** Business and store flags keep holding the payout, now also while confirmed, and A-13 says so. D-225 records the money effect, and the 01 Risk line points at §9.
6. **Nav placement** is as the lead confirmed on 25 Oct (06 §3). The admin console is English-only, so no other locale keys.
7. **Edits outside my area** (unchanged):
   - files from W3-05: the admin-settings risk group, the audit subjects `risk_flag` and `referral` with their labels, the settings labels, `roles.spec.ts` (ALL, the support nav and READS) and `fake-api.ts` (an empty `/v1/admin/risk/flags` answer)
   - one comment in `orders/transitions.go` that names the lockout trigger
   - the `risk_scan` job timeout, cut to 1m to meet the job lint
8. **D-225** is fine.
9. **`ask_reported`** stays a known rule with an A-13 label.
10. **Main moved after my base f033f31** (`e2e/contract/schemathesis.toml`). This branch does not touch that file.

## Open follow-up

- **`referral.self_like` can fire after the reward posts.** The rule looks at referrals that have qualified, on the 5-minute `risk_scan`, and the referral reward job may post both sides before that scan reaches the referral. The flag then lands on a `rewarded` referral: Reject is `409`, and taking the credits back is an A-09 adjustment. Checking self-like before the reward posts (or holding the reward until the next scan has seen it) is a later change.
