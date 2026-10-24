# W2-04 POS backend and sync: QA report

The Go backend for the counter: online sales, the offline outbox sync, bill QR claims, counter codes, receipts and purchases. Branch `claude/wp-w2-04-pos-421f24`, cut from `main`, with `main` merged back in at 0f2f4ed (W2-06 reviews, W2-07 offers, the shared test Postgres); not yet merged into `main`. Everything ran locally against PostGIS (testcontainers, then the shared test server); nothing touched a cloud account. Decisions are in D-130 and, for the lead's review fixes, D-131 (`docs/plan/12-decision-log.md`).

## What was built

- **Contract:** the pos, sync and receipts domains in `backend/api/paths|schemas|problems/`. The new problem codes are `claim_expired`, `claim_already_used`, `claim_mismatch`, `reversal_window_passed` (pos) and `resync_required` (sync, 410). It reuses `code_invalid`, `code_locked`, `price_changed`, `conflict`, `invalid_transition` and `validation_failed`.
- **Migration 20401:** `pos_sales`, `pos_sale_lines`, `bill_claims`, `counter_code_checks`, `counter_code_uses`, `receipt_tokens`, `sync_cursors`, `sync_entries`.
- **Policy:**
  - `pos.sell`, `pos.link_customer` and `pos.sync` for all staff;
  - `pos.discount` and `pos.reverse` for owner and manager;
  - `me.counter_code`, `me.bill_claim` and `me.purchases` for customers.
- **Online sale** (`internal/pos/sale.go`, `money.go`):
  - priced on the server through the Pricer seam;
  - a stale catalogued price or a wrong `expectedTotalPaise` is 409 `price_changed`;
  - custom lines are allowed;
  - round-off is to the nearest ₹1, half up (D-94);
  - each catalogued line is one `sale_pos` movement.
- **Reversal:** within `pos_reversal_window_hours`, by owner or manager only. It writes `pos_reversal` movements, then calls `Credits.ReverseSale` and `Referrals.Revoke`.
- **Sync** (`sync.go`):
  - idempotent by entry client id;
  - entries must arrive in `deviceSeq` order;
  - each entry is checked against its own role (sale, count, restock, damage, expired);
  - offline prices stand, and an oversell goes negative and flags `countNeeded` (02 §4.3);
  - the server cursor reads `catalogue_changes` and is 410 `resync_required` past 30 days.
- **Bill QR** (`claims.go`):
  - a 64-char HMAC token under `BILL_QR_SIGNING_KEY` (503 when the key is unset), valid for 120 s;
  - confirm takes `FOR UPDATE`: the first confirm wins and the second is `claim_already_used`;
  - credits are held at confirm and captured by the sale;
  - each change emits `bill_claim.updated` through the outbox.
- **Counter code** (`totp.go`):
  - TOTP, HMAC-SHA1, 60 s, ±1 step, 4 digits;
  - the seed is sealed under `DEVICE_SECRET_ENC_KEY`;
  - five tries per bill, then `code_locked`;
  - an offline code is verified at sync against the recorded device time, within `pos_offline_link_max_hours` and a 2-minute clock offset, once only;
  - more than `pos_offline_links_risk_per_day` offline links raise one store risk flag.
- **Rewards:** a sale linked by `bill_qr` or `counter_code` earns cashback through the Credits seam and qualifies a referral at `ref_qualifying_min_order_paise`. A `phone_unverified` link earns nothing and writes no ledger rows (D-07).
- **Receipts and purchases** (`receipts.go`):
  - a 128-bit receipt token per sale;
  - `GET /v1/public/receipts/{token}` returns no customer, phone, wallet or link method, sends `no-store` and expires after 30 days;
  - `GET /v1/me/purchases` merges orders and linked POS sales, newest first, with a keyset cursor (D-45, D-84).
- **Seams** (`seams.go`): Stock, Pricer, Redeemer, Credits and Referrals. The defaults are `NoOffers`, `NoRedeem`, `NoCredits` (`not_available`) and `NoReferrals`. Stock is the catalogue writer through `jobs.PosStock`.
- **Offers (W2-07, real):** `offersForPos` in `cmd/todayz/routes_pos.go` adapts `offers.Service`:
  - `Price` on `ChannelPOS`, as a walk-in, with no Fixed lines (a catalogued line already carries the catalogue price);
  - `Redeem(ctx, Ref{POSSaleID}, customer, quote)` runs inside the sale's transaction (`db.InTx` puts the tx in ctx), after the lines and before the claim bind; `offers.ErrUnavailable` becomes `pos.ErrOfferGone`, which the sale returns as `409 price_changed`;
  - a reversal calls `Release(Ref{POSSaleID})` in the reversal's transaction;
  - an offline sale at sync redeems nothing (its discounts are the device's, with no offer ids).
- **Reviews (W2-06):** `posForReviews` implements `reviews.SaleSource` through `pos.Service.SaleRef` (store, customer, link method only for a rewarding link, completed unless reversed). `routes_reviews.go` now passes it as `Sales`, so a POS sale can be reviewed.
- **Wiring:** `newPos` in `cmd/todayz/routes_pos.go` builds the service for `mountPos` and for reviews. It has a `posSeams` test hook that sets the fakes (credits, referrals, and the offers fakes in the older tests).

## Evidence

Runs were on this branch at 5e3a82b (first table) and 64b075b (second table), one heavy command at a time, Go tests with `-p 2`. The full `make check` was not run (lead's rule after the crash).

| Check | Result |
|---|---|
| gofmt | clean |
| `go vet ./...` | clean |
| golangci-lint (`internal/pos`, `internal/jobs`, `cmd/todayz`) | 0 issues |
| gosec (same packages) | 0 issues |
| settings lint | clean |
| `make gen` | exit 0 |
| `redocly lint` | valid; 10 warnings, none in pos, sync or receipts |
| `TestContractSmoke`, `TestContractProblemCheckFires` | PASS (smoke 11.7 s) |
| `go test -race -p 2 -run POS ./cmd/todayz/` (27 tests) plus `TestRoutesAreInTheContract`, `TestOneMountPerDomain` | ok, 39 s |
| `internal/pos` unit tests (7) | ok |
| `TestMigrationFileNames` | ok |

After merging `main` and wiring offers and reviews (64b075b), with `TODAYZ_TEST_PG_URL_FILE` set to the shared test server:

| Check | Result |
|---|---|
| `go vet`, golangci-lint, gosec, settings lint (`internal/pos`, `cmd/todayz`) | clean, 0 issues |
| `make gen` | exit 0 |
| `TestPOSOffersRedeemAndRelease`, `TestPOSSaleReview` | ok, 5.7 s |
| `go test -race -p 2 -run 'POS\|TestRoutesAreInTheContract\|TestOneMountPerDomain\|Offer\|Review' ./cmd/todayz/` | ok, 56 s |
| `internal/pos`, `internal/reviews`, `internal/offers` unit and DB tests | ok |
| `go test -race -p 2 ./cmd/todayz/` (whole package) | ok, 161 s |

Review round (D-131), at df6d08e, shared test server:

| Check | Result |
|---|---|
| golangci-lint, gosec, settings lint (`internal/pos`, `internal/jobs`, `cmd/todayz`) | clean, 0 issues |
| `make gen` | exit 0 |
| `go test -race -p 2 ./cmd/todayz/ ./internal/pos/ ./internal/jobs/` | ok (cmd/todayz 195 s); `internal/platform/migrate` ok at a6c7ba0 |
| `TestMigrationFileNames`, `TestRoutesAreInTheContract` | ok |
| `TestContractProblemCheckFires` | PASS |
| `TestContractSmoke` | FAIL, 136/137 operations pass. The one failure is not W2-04's: `GET /v1/discover/stores/{slug}/reviews` (W2-06, `api/paths/reviews.yaml`) answers 422 `query.cursor: not a cursor from this list` to a schema-valid long cursor in the coverage phase. This round changed no contract file. For the reviews owner: either give the cursor parameter a `pattern`/`maxLength` the server keeps, or treat an unknown cursor as allowed by the schema. |

Review round 2, at 2702ec8, shared test server:

| Check | Result |
|---|---|
| gofmt, `go vet` | clean |
| golangci-lint, gosec (`internal/pos`, `internal/jobs`, `cmd/todayz`, `internal/platform/settings`) | 0 issues |
| settings lint | exit 0 |
| `make gen` | exit 0 |
| `go test -race -p 2 -count=1 ./cmd/todayz ./internal/pos ./internal/jobs ./internal/platform/settings ./internal/platform/migrate` | ok (cmd/todayz 277 s), `TestSettingsMatchProductRules` included |
| `TestGuardPhoneVariantsShareKey` (added after the race run started) | ok |
| `TestContractProblemCheckFires` | PASS |
| `TestContractSmoke` | FAIL, 136/137: the same W2-06 failure as above (`GET /v1/discover/stores/{slug}/reviews`, 422 on a long cursor). Round 2 changed no contract file |

Review round 3, after merging `main` (fe475f2), shared test server:

| Check | Result |
|---|---|
| gofmt, `go vet ./...` | clean |
| golangci-lint, gosec (`internal/pos`, `internal/jobs`, `cmd/todayz`) | 0 issues |
| settings lint | exit 0 |
| `make gen` | exit 0 |
| `go test -race -p 2 -count=1 ./cmd/todayz ./internal/pos ./internal/jobs ./internal/platform/settings ./internal/platform/migrate` | ok (cmd/todayz 310 s) |
| `TestContractProblemCheckFires` | PASS |
| `TestContractSmoke` | PASS, 145 of 145 selected operations tested, no issues (147 in the bundle, the two `/v1/test/slow*` excluded). The count is 145, not 137, because `main` added the W2-01 and W2-03 operations. W2-06's reviews-cursor failure is gone |

## Review fixes (D-131)

The lead's W2-04 review: no blockers. Each item was landed on this branch with a test (`cmd/todayz/pos_review_http_test.go` unless named).

| Item | What changed | Test |
|---|---|---|
| MUST 1, code brute force | `counter_code_failures`: 5 per (store, phone) in 15 min lock the phone at the store for 15 min; 20 per cashier in an hour lock the cashier for an hour. Advisory locks: cashier, then phone. Offline wrong or reused codes count too (`source = 'sync'`); a locked phone fails the sync's remaining links | `TestPOSPhoneLockAcrossBills` (a new `saleClientId` is still locked; unlocks after 15 min), `TestPOSCashierCodeLock`, `TestPOSOfflineCodesHitTheLock` (7 wrong then the right code in one sync: all failed, 5 counted, locked online after) |
| MUST 2, `sold_at` | Clamped to at most `received_at` (and at least 72 h before it, 01 §17). An offline sale's reversal window runs from `received_at` | `TestPOSReversalWindowFromReceived` (`at` 20 h ahead, reversal after 24 h is `reversal_window_passed`; a sale 5 h behind reverses 23 h after it arrived), `TestPOS06DeviceClock` |
| MUST 3, guard tables | `REVOKE DELETE, TRUNCATE` on `counter_code_uses` and `counter_code_checks`; `UPDATE, DELETE, TRUNCATE` on `sync_entries`, `receipt_tokens` and `counter_code_failures`; `no_truncate` triggers; 20401 edited in place | `TestPOSGuardTablesAppendOnly` (`todayz_app` DELETE, TRUNCATE and UPDATE denied; the owner's TRUNCATE refused) |
| SHOULD 4, offline offer discount | Accepted as sold; counts with the offline links against `pos_offline_links_risk_per_day`; a cashier without `pos.discount` also raises `pos.offline_offer_without_discount_role` | `TestPOSOfflineOfferDiscount` |
| SHOULD 5, bill-QR retry | Same `clientId` under a new key returns the sale (replay), also when both arrive at once | `TestPOSBillQRConcurrentRetry` (3 rounds of two at once: both 200, one sale, one capture) |
| SHOULD 6, credit holds | A new QR cancels the bill's earlier claims and releases their holds; a confirmed claim past `expires_at` is `claim_expired` at bind; the bill's sale releases the others; timer `pos_bill_claim_expiry` + runbook; `Capture` must fail on a released hold (seams.go). The test fake records each Release and Capture and refuses a capture after a release | `TestPOSClaimHoldLifecycle` (two claims for one bill; late bind; plain sale; the sweep through the service and through the registered timer) |
| Nits | Manual discount past the offer's remainder is `422`, not clamped; a verified check binds for 2 min; `Stock.Reverse` errors are `409 invalid_transition`; the re-confirm comment fixed; receipt token on replay left open (question 17) | `TestPOSManualDiscountAndCheckAge` |

`TestPOSBillQRSale` now sells within the claim's 120 s, since a late bind is refused.

## Review fixes, round 2 (D-131)

The lead's fix review: no blockers. 20401 was edited in place again. Tests are in `cmd/todayz/pos_round2_http_test.go` unless named.

| Item | What changed | Test |
|---|---|---|
| 1, sync deadlock | At the start of a sync, `lockSyncCodes` collects every counter-code phone in the batch and takes the cashier lock, then the phone locks (per store and across stores) sorted and deduplicated (`lockCodeKeys`). The per-entry checks re-enter locks already held | `TestPOSSyncCodeLocksNoDeadlock`: a cashier and a manager sync the same two phones in opposite orders at once, 6 rounds, both 200 every time. With the pre-lock disabled, both syncs got 500 (deadlock), so the test catches the bug |
| 2, one Credits | `jobs.PosDeps(base)` sets Stock, Credits and Referrals; the worker's `PosJobDeps` and the API's `posDeps` both call it (the API adds the offers adapter) | `TestPOSDepsShared`: the Stock, Credits and Referrals types match |
| 3, poison claim | `ExpireClaims` returns `Sweep{Expired, Released, Failed}`. Each due claim runs in its own savepoint; a failing `Release` rolls back that claim, is logged with the claim id only, counted in `tz_pos_claim_release_failed_total` and in the run summary's `failed`, and stays live for the next run | `TestPOSSweepPoisonClaim`: three confirmed claims past `expires_at`, the poison one first: `{Expired: 2, Released: 2, Failed: 1}`, the poison row still `claimed`, the other two `expired`; once the fault clears, the next run gives `{Expired: 1, Released: 1}` |
| 4, cross-store ceiling | `pos_code_phone_failures_per_hour` (10, range 5–100) in `platform_settings` and 01 §17; `ix_counter_code_failures_phone_any (phone_hash, failed_at)`; `codeLocks` counts a phone's failures in the last hour at any store. Phones are canonicalised (`canonPhone`) before hashing and locking | `TestPOSPhoneCeilingAcrossStores` (4 wrong at store S, 16 min later 4 more at S, then at a second store the 9th is `code_invalid` and the 10th `code_locked`; the right code is then `code_locked` at both stores, another phone is not; 44 min later the phone is under the ceiling again), `TestGuardPhoneVariantsShareKey` (unit: `+91 98765 43210`, `+91-98765-43210`, `(+91) 98765.43210`, `919876543210`, `09876543210` and `9876543210` give the E.164 number's hash, the key that is locked and counted), `TestCanonPhone`, `TestPOSPhoneVariantsRefused` (the contract refuses a non-E.164 phone with 422 before any count) |
| 5, offline offer risk | A synced sale is priced by the engine at the clamped `sold_at`; it is `offline_offer_unmatched` only when a line's offer discount is over the engine's for that line. `OfflineRiskSalesSince` counts each sale once (linked or unmatched). `pos.offline_offer_without_discount_role` only for an unmatched sale | `TestPOSOfflineOfferMatchesEngine`: a 10% store offer, a 2900 discount on a 29000 line is not flagged and a 3000 one is; 19 linked sales plus an unmatched linked sale make 20 (no flag); the 21st flags `pos.offline_links` once |
| 6, VerifyCode order | Locks, then the bill's check; a locked phone or cashier is `code_locked` before `EnsureCounterCheck`, so no `counter_code_checks` row is added. The fresh-verified answer now needs the same phone (item 8) | `TestPOSPhoneLockEndsAt15` (no new check row while locked), `TestPOSFreshVerifiedNeedsSamePhone` |
| 7, missing tests | — | `TestPOSCaptureOfReleasedHold` (500, nothing written, the claim stays `claimed`), `TestPOSStockReverseRefused` (409 `invalid_transition`), `TestPOSRejectedEntryRollsBackFailure` (`rejected:invalid`, no `counter_code_failures` and no `sync_entries` row), `TestPOSPhoneLockEndsAt15` (locked at 14:59.999, open at exactly 15:00) |
| 8, report items | See questions 22 and 23 | — |

`TestPOSClaimHoldLifecycle` now expects `Sweep{Expired: 1, Released: 1}`.

## Review fixes, round 3 (D-131)

The lead's round-2 review: no blockers. No schema change. `main` was merged first (fe475f2). Tests are in `cmd/todayz/pos_round2_http_test.go` unless named.

| Item | What changed | Test |
|---|---|---|
| 1, merge `main` | Merged; the decision-log conflict kept the POS section and main's sections | The contract smoke, see Evidence |
| 2 (S2), cashier lock with no parseable phone | `syncCodeKeys` returns the cashier key whenever the batch has a counter-code entry with a code, even when no phone in it parses, then the parsed phones' keys, sorted and deduplicated. So the cashier lock is always taken before any stock row lock. Over HTTP the contract's phone pattern (`^\+91[6-9][0-9]{9}$`) refuses such a phone with 422 first, so this case is defence in depth and is tested at the unit level | `TestSyncCodeKeys` (unit, `internal/pos`): no code gives no key; a restock plus a counter code with an unparseable phone gives the cashier key only; four entries (two variants of one phone, one unparseable) give 3 keys, cashier first |
| 3 (S1), sweep starvation | `DueBillClaims` takes a keyset `(expires_at, id) > (@after_at, @after_id)`, ordered by `(expires_at, id)`. `ExpireClaims` pages 500 at a time, one transaction a page, until a page comes back short or the job's context ends (its 30 s timeout), so a page that committed stays done. A failing row only moves the keyset on | `TestPOSSweepPagesPastFailures`: 3 confirmed claims, then 501 claims due before them whose releases all fail. One run gives `{Expired: 3, Released: 3, Failed: 501}`; the 3 are `expired` with their holds released, the 501 stay `claimed`. `TestPOSSweepPoisonClaim` and `TestPOSClaimHoldLifecycle` still pass |
| 4, alert | `infra/grafana/alerts/pos.yaml`, rule `tz-pos-claim-release-failed` (*Bill claim hold not released*): `sum(increase(tz_pos_claim_release_failed_total[10m])) > 0` for 5 min, critical (customer credits stay held), runbook `docs/runbooks/pos-bill-claims.md`. The run itself succeeds when a release fails, so Job failing never fires for it. The counter has no zero sample before its first failure, so `increase()` misses that step; a claim that keeps failing adds one each tick, and those fire it | YAML parses. No test reads `infra/grafana/alerts/` (see question 27) |
| 5, wrong code on a verified bill | In `VerifyCode`, a wrong code against a fresh verified check (another phone) skips `FailCounterCheck`: the check's attempts stay as they were and `attemptsLeft` reports them. The phone's and cashier's counters still take the failure. A stale verified check (older than 2 min) is checked as before | `TestPOSFreshVerifiedNeedsSamePhone`: two wrong codes from another phone are `code_invalid` with `attemptsLeft` 5, the check's `attempts` stays 0, `counter_code_failures` has 2 rows, and the verified phone then answers and sells |

## The real credits ledger and referrals (D-132)

The lead's task after round 3: replace `NoCredits` and `NoReferrals` with W2-01's and W2-03's services, in the API and the worker. The lead approved three additions to `internal/credits` (W2-01's package), committed first in d6f5b92: the `pos_sale` ref, `Completion.RefuseReleased` (opt-in) and `Reverse`. Orders' behaviour is unchanged and the existing credits tests pass untouched. No schema change, no FKs.

| Item | What changed | Test |
|---|---|---|
| Wiring | `jobs.PosDeps(base, credits, referrals)` sets Stock, Credits and Referrals; the API (`routes_pos.go`) passes `creditsService(d)` and `newReferrals(d)`, the worker's sweep builds the same through `jobs.PosJobDeps` (notify, credits, referrals) | `TestPOSDepsShared` |
| Capture | `Complete` on `bill_claim` with `CashbackBP` 0 and `RefuseReleased`; `credits.ErrReleased` maps to `pos.ErrHoldReleased`, and the sale answers 409 `claim_expired` (question 24) and rolls back | `TestPOSLedgerCaptureAfterReleaseDB`: the ledger released the hold under a live claim; the sale is 409, no `pos_sales` row, no capture, no cashback, wallet unchanged, claim still `claimed` |
| Cashback | `Complete` on `pos_sale`, `EligiblePaise` = paid before round-off, the store's rate now | `TestPOSLedgerBillQRDB`: 50 credits, 10 held, sale of ₹290 pays ₹280, 14 cashback at 5%; wallet 54, store −4; a replay of the bill gives the same sale, one `spend_capture`, one `cashback` |
| Reversal | `Reverse{Ref: pos_sale, Spend: bill_claim when the sale had one}` | Same test: wallet back to 50, store 0, two `reversal` transactions; a second reverse is refused and moves nothing |
| Sweep | The worker releases through the ledger | `TestPOSLedgerSweepReleasesDB`: the hold is `released`, the wallet back to 50, the late sale `claim_expired` with no capture |
| Sync replay | — | `TestPOSLedgerSyncReplayDB`: a counter-code entry synced twice: one sale, one cashback (14), wallet 64 |
| Referrals | `Qualify` → `OnPurchaseCompleted(Outcome{RefPOSSale, …})` for every linked ledger sale (POS no longer applies `ref_qualifying_min_order_paise` itself; referrals does, D-164/D-165); `Revoke` → `OnPurchaseReversed(pos_sale, id)` | `TestPOSLedgerReferralDB`: a pending store→customer referral stays pending after a ₹75 sale, qualifies on a ₹100 sale (`qualifying_ref_id` = the sale), no reward posted inside the sale, and goes back to `pending` when that sale is reversed |
| Bind vs sweep | — | `TestPOSLedgerBindRacesSweepDB` (`-race`): the API's clock 1 s before expiry, the worker's 1 s after, the sweep started 0–500 ms after the sale over 8 rounds. Each round has exactly one winner (sale: hold `captured`, sweep result empty; sweep: `{Expired 1, Released 1}`, hold `released`, sale `claim_expired`), no error, the wallet right after each. Four runs gave sale/sweep 6/2, 5/3, 6/2, 5/3 |

Checks on this change (shared test Postgres, `-p 2`): `make check-contract` passes; gofmt and `go vet ./...` clean; golangci-lint and gosec on `internal/pos`, `internal/jobs`, `internal/credits`, `cmd/todayz` 0 issues; settingslint clean; `go test -race` passes for `internal/credits`, `internal/pos`, `internal/jobs`, `internal/referrals`, and `cmd/todayz` but for one flake: `TestPOSCashierCodeLock` (round 1, unchanged here) failed once in `noSecrets`, which looks for the 4-digit code anywhere in the log text, so a timestamp or id can match by chance; it passed 8 of 8 on a rerun (question 33).

The lock order as built is pos rows, then ledger accounts, then `credit_holds`, the reverse of the brief's wording; D-132 has the analysis. Existing POS tests changed where the behaviour did: `TestPOS14` and `TestPOS22` now expect every linked sale handed to referrals (2 each), and `TestPOSCaptureOfReleasedHold` expects 409 `claim_expired` instead of 500.

## Review fixes, round 4 (D-132)

The lead's review of D-132: nothing blocking in the sale, capture, cashback or reverse paths, no double or lost postings. Tests are in `cmd/todayz/pos_ledger_http_test.go` unless named.

| Item | What changed | Test |
|---|---|---|
| 1, sweep vs sync deadlock | The sweep read a page of 500 claims and released them in one transaction, holding each customer's ledger accounts until the page committed; a sync of [C2, C1] could hold C2 and want C1. Now `ExpireClaims` reads the page without locks and expires and releases each claim in its own transaction (`LockDueBillClaim`, `FOR UPDATE SKIP LOCKED`). The sync (one transaction per batch) is retried whole on `40P01`/`40001`, up to 3 attempts, logged at warn and counted in `tz_pos_sync_retried_total` (`db.IsRetryable`). Pre-locking every customer's accounts at the sync's start was not used: it takes ledger locks before later entries' claim rows, against the D-132 order | `TestPOSLedgerSweepRacesSyncDB`: two customers' confirmed claims past expiry, a sync of counter-code sales for [cust2, cust1] run against the sweep, the sweep started 0–20 ms late, 6 rounds: the sync is 200 with both entries applied and linked, the sweep `{Expired 2, Released 2}`, both holds released, each wallet +2. Not checked: that it fails on the old sweep |
| 2, shortfall | `Credits.ReverseSale` returns the shortfall; after the reversal commits POS logs it at warn (sale, store, customer ids, `shortfall_paise`) and counts `tz_credits_reverse_shortfall_total` | `TestPOSLedgerReverseSpentCashbackDB`: 10 granted, a counter-code sale pays 14 cashback at 5%, the customer spends all 24, the reversal is 200, the wallet stays 0, one `reversal` transaction (the marker), the log line has `shortfall_paise` 1400, no secrets in the logs |
| 3, one constant | `money.PaisePerCredit` (100) in `internal/platform/money`, used by pos (`sale.go`, `money.go`, `claims.go`), credits (`rules.go`) and referrals | Existing tests |
| 4, tests | The second reverse asserts 409 `invalid_transition`; `TestPOSLedgerBindRacesSweepDB` keeps exactly one winner a round and drops the two timing-dependent `Positive` checks | As named |
| 5, leak check | `noSecrets` uses `leaked`: a value with a letter, digit or `_.:-+` on either side is not a match | `TestLeaked` (table) |
| Q28–Q31 | `ErrHoldOpen` kept, comment cites the answer; `Reverse` always posts its marker (orders too); no partial reversal; the lock order and the cross-transaction point in D-132 | `TestReverseBeforeCompleteDB` (a sale ref and an order with no hold or a released hold: reverse, reverse again, one marker, `Complete` is `ErrReversed`, nothing posted); `reverse_fake_test.go` edges; `TestReverseEdgesDB` changed (question 29) |

Checks on this change (shared test Postgres, `-p 2`): `make gen` and `make check-contract` pass; gofmt and `go vet ./...` clean; golangci-lint and gosec on `internal/{pos,jobs,credits,referrals}`, `internal/platform/{money,db}` and `cmd/todayz`: 0 issues; settingslint clean; `go test -race -count=1` passes for all of those packages, `cmd/todayz` in full (225 s). Follow-up outside W2-04: `internal/orders/quote.go` still multiplies credits by a literal 100.

## Follow-ups after the merge (D-132)

The lead merged round 4 (969f40b) and asked for three small follow-ups.

| Item | What changed | Test |
|---|---|---|
| 1, retry test and backoff | `syncTx` calls `retryTx(ctx, inTx, fn, retried)`, which waits `retryBackoff` between attempts: 20 ms per past attempt plus up to 20 ms of jitter, cut short if the context ends | `TestRetryTx` (unit, in `internal/pos`): two 40P01 then success is 3 calls and 2 retries; four failures stop at 3 calls and return the last; another error is not retried; a context cancelled during the backoff stops it. `TestRetryBackoff`, `TestIsRetryable` (wrapped, other codes, nil) |
| 2, leak check | `leaked` now skips a match only inside a longer token: a digit or letter touching it, a `.` before it (a fraction), or `-` on both sides (a UUID group). `code:4821`, `code=4821` and a bare JSON number count. Letters stay a boundary, since with digits alone a code inside a UUID's hex or a timestamp's `.4821Z` would flag, which was the original flake | `TestLeaked` (table) |
| 3, savepoint errors | `offerUnmatched` returns a 40P01/40001 instead of flagging the sale unmatched; `referrals.enqueueReward` returns it instead of logging, and `OnPurchaseCompleted` passes it up (other enqueue failures are still logged, and the sweep posts the reward) | Covered by the existing pos, referrals and sync tests (no fault injection for a deadlock inside the savepoint) |

## Coverage of SC-POS (08-testing.md)

| Scenario | Where | Notes |
|---|---|---|
| POS-01, 02, 04, 05, 06, 08, 09, 11, 13, 14, 17 | `cmd/todayz/pos_http_test.go` | HTTP against a real database |
| POS-10 | `TestClaimTokenTamper` (unit) | amount, store, expiry, tag bit, key |
| POS-12 | `TestTOTPDrift` (unit), `TestPOSCodeDrift` (HTTP) | ±1 step passes, ±2 fails |
| POS-16 | `TestRoundOff`, `TestPriceBill` (unit) | D-94 boundaries, credits leave ≥ ₹1 |
| POS-22…27 | `pos_http_test.go` | offline code boundaries, risk flag, cursor, receipt expiry, purchases, ₹100 credit minimum |
| POS-28…34 (builder-written) | `pos_http_test.go` | online then sync, entry roles, item not found, tenancy, price changed, deviceSeq order, concurrent receipts |
| Bill-claim race | `TestPOS09ClaimRace` | one winner; credits held once |
| Sync property | `TestPOSSyncProperty` | random batches and replays; every entry applies once, stock ends the same |
| POS-03, 07, 15 | — | E2E with the POS frontend (W2-11) |
| POS-18…21 | `pos_ledger_http_test.go` | on the real ledger and referrals (D-132): hold, capture, cashback, reversal with clawback, referral qualify and revoke. The fake-seam tests in `pos_http_test.go` still check the calls |
| POS-35 | — | not written; the slot is free |
| Offers at the counter | `TestPOSOffersRedeemAndRelease` | a 10% offer prices the line and writes one redemption; a reversal releases it; an offline sale writes none; a flat offer with `maxRedemptions` 1 applies to the first sale only |
| Reviews of a sale | `TestPOSSaleReview` | a counter-code sale is reviewable once (`review_exists` after); `phone_unverified` is 404; a reversed sale is `review_not_eligible` |

Tests never print codes, tokens or phones. `noSecrets` captures the API's log output and asserts that no receipt token, claim token, counter code or phone reached it, in POS-01, 08, 11, 13, 17, 22, 25 and the bill QR test. POS-01 also checks that the receipt has `no-store` and that its body holds no phone and no `customer` field. That sale is unlinked; a receipt for a linked sale is not checked for PII separately, since the Receipt schema has no such fields.

## Not verified

- **Real seams:** the ledger tests (D-132) run the real credits and referrals; the rest of the POS tests keep the recording fakes. The referral reward job itself (`referral_reward`) is not run from POS tests; W2-03's tests cover it.
- **SSE delivery:** `bill_claim.updated` is checked as an `outbox_events` row. Delivery to a connected POS was not run.
- **Authenticated contract smoke:** the smoke runs unauthenticated, so `/v1/biz` and `/v1/me` return only 401s there. Their shapes are covered by the HTTP tests and the request validator.
- `make check`, `make e2e`, load and the abuse suite were not run.

## Open / Questions

Each one has the most conservative reading built, and the ones in code are marked `TODO(lead):`.

1. **How long a confirmed QR stays usable:** resolved by the review (D-131). A sale binds a claim only before its `expires_at`.
2. **Hold release:** resolved by the review (D-131). A new QR, the bill's sale and the `pos_bill_claim_expiry` timer release holds. The routes and the worker share one constructor (`jobs.PosDeps`), which now wires W2-01's ledger and W2-03's referrals (D-132).
3. **`phone_unverified` sales:** should they link later, once the customer verifies that phone? Built: they never link.
4. **Realtime catalogue:** `bill_claim.updated` is not in `realtime.yaml` or the shared catalogue yet.
5. **Pricer:** resolved. W2-07's `offers.Service` is wired through `offersForPos`.
6. **Offline-link risk flag score** (`sync.go`, TODO). Built: 50.
7. **Counter-code lockout:** it now locks per phone and per cashier (D-131), but it still raises no risk flag. Should a phone or cashier lock flag the store?
8. **Device secret rotation** (`totp.go`, TODO). `DEVICE_SECRET_ENC_KEY` rotation and a customer's "reset my code" are not built.
9. **Fixed 30-day windows:** `changesKept` (cursor) and the receipt TTL are constants, not `platform_settings`. 01 lists no setting for them.
10. **Files outside the owned folders:** `cmd/todayz/pos_http_test.go` and the `posSeams` hook in `routes_pos.go` live in `cmd/todayz`.
11. **Counts in the sync property test:** they are absolute and depend on order, so the property covers sales, restocks, damage and expiries only.
12. **Offline sales and offer limits:** a synced sale carries the device's discounts with no offer ids, so it redeems nothing and does not count against `maxRedemptions` or a budget. Should the outbox entry carry offer ids so sync can redeem (and accept going over a limit)?
13. **Custom lines and minimum-bill offers:** custom lines (no `storeItemId`) are not sent to the pricer, so they do not count toward a minimum-bill threshold.
14. **Walk-in pricing:** the sale is priced with no customer, even when a customer is linked; the redemption records the linked customer. Per-customer offers therefore never apply at the counter. Should the sale price with the linked customer?
15. **An offer that runs out mid-sale:** `409 price_changed` with no extension members; the POS re-sends and gets the new prices. A dedicated code may read better at the counter.
16. **FKs to `pos_sales` and `bill_claims`:** not added here (see below).
17. **Receipt token on a replay** (open item from the review). A replayed sale (same `clientId`, the idempotent retry or the bill-QR retry) comes back without `receiptToken`: the token is shown once and stored only hashed. The POS keeps the first one; reissuing would need a new token row per replay.
18. **The 72 h `sold_at` clamp** (`sale.go`, TODO). 01 §17 lists "sold_at clamp for offline sales, 72 h" with no direction. Built: an offline `sold_at` is never more than 72 h before `received_at`, and never after it.
19. **`pos.offline_offer_without_discount_role` score** (`sync.go`, TODO). Built: 50, as for `pos.offline_links`.
20. **Plan text to update** (not W2-04's files): 03 §5 says that a reversal is allowed within the window "of `sold_at`". For an offline sale, the window now runs from `received_at`. The counter-code row in 01 §6 says "5 wrong tries per bill"; D-131 adds 5 per phone at a store in 15 minutes and 20 per cashier in an hour.
21. **Deadlocks between syncs:** resolved in round 2. A sync takes all its code locks at its start, in sorted order.
22. **Retention of `counter_code_*` rows.** `counter_code_checks`, `counter_code_uses` and `counter_code_failures` keep `phone_hash` forever, and `todayz_app` has no `DELETE` on them (and the owner cannot truncate them). Nothing removes them, not even account deletion (W1-01's `OnAccountDelete`). Proposed owner: **W3-02 Risk and fraud**, which owns the abuse signals these rows are: a retention kind through `jobs.Define`, run as the owner (or a `SECURITY DEFINER` function, as `drop_old_partitions`), with a `platform_settings` key for the days (the lockout only needs one hour of failures; checks and uses need the 2-minute bind and the offline replay window of `pos_offline_link_max_hours`). W4-03 Security should check it under DPDP. Not built here.
23. **Fresh-verified branch and the body's phone:** tightened in round 2. A verified check under 2 minutes old answers again only when the body's phone (canonicalised) belongs to the verified customer; the code is not re-checked, since that (device, step) is already used. Another phone falls through to a new check with the usual locks.
24. **Capture of a released hold:** now 409 `claim_expired` (D-132), and the sale rolls back.
25. **Offer matching is per line.** An engine bill-level discount is not spread over the lines, so an offline sale whose line discounts add up to an engine bill discount reads as unmatched. Offline matching prices as a walk-in (`ids.Zero`), as the online sale does (question 14).
26. **Sweep starvation.** Resolved in round 3: one run pages past failing claims by `(expires_at, id)`, and alert `tz-pos-claim-release-failed` fires on the counter.
27. **Alert rules are not tested.** Nothing checks `infra/grafana/alerts/*.yaml` against the registered metrics, as `dashboards_test.go` does for dashboards. `pos.yaml` follows `jobs.yaml`. The 05 §5 alert table does not list it yet; TODO(lead): add a row there or say where W2 alerts are listed.
28. **Reverse of an uncaptured hold:** answered (round 4): keep refusing it with `credits.ErrHoldOpen`; the TODO(lead) is now a comment citing the answer. A POS sale captures in its own transaction, so it cannot reach this.
29. **Reverse before Complete:** answered (round 4): refused for orders too. `Reverse` now always posts its `reverse:<ref>` marker (no entries when there is nothing to give back), so a later `Complete` on that ref is `ErrReversed`. This changed one existing credits assertion, in `TestReverseEdgesDB` (an empty reverse posted no transaction; it now posts the one empty marker).
30. **Partial reversal:** answered (round 4): none for now. `Reverse` takes the whole purchase, as the POS reversal does (D-132).
31. **Lock order:** answered (round 4): pos rows, then ledger accounts, then `credit_holds` is fine as long as every path keeps it; written in D-132 with the cross-transaction point (the sweep against a sync, round 4 item 1).
32. **Changed test expectations.** `TestPOS14`, `TestPOS22` (every linked sale now reaches referrals) and `TestPOSCaptureOfReleasedHold` (409 instead of 500). Round 4 changed one credits assertion, in `TestReverseEdgesDB` (question 29).
33. **`noSecrets` and 4-digit codes:** fixed in round 4 (the lead gave it to W2-04). A value counts as leaked only when the characters on both sides are not letters, digits or `_.:-+`, so a code inside a timestamp or id no longer matches, and a code on its own still does (`TestLeaked`).

## The cross-package FKs

The lead asked for the FKs from `reviews.pos_sale_id` and `offer_redemptions.pos_sale_id` to `pos_sales` in a new migration in this block. They are not in this branch. Migrations run in file-name order, and any 204xx file sorts before `20601_w2-06_reviews.sql` and `20701_w2-07_offers.sql`, so on a fresh database it would run before those tables exist and fail. The plan puts these in the Wave 2 constraints migration that sorts after every Wave 2 file (03 §13, D-35, D-79). The SQL for it:

```sql
ALTER TABLE reviews
  ADD CONSTRAINT fk_reviews_pos_sale FOREIGN KEY (pos_sale_id) REFERENCES pos_sales(id);
ALTER TABLE offer_redemptions
  ADD CONSTRAINT fk_offer_redemptions_pos_sale FOREIGN KEY (pos_sale_id) REFERENCES pos_sales(id);
-- after W2-01 lands credit_holds:
-- ALTER TABLE credit_holds
--   ADD CONSTRAINT fk_credit_holds_bill_claim FOREIGN KEY (bill_claim_id) REFERENCES bill_claims(id);
```

The indexes they need already exist (`uq_reviews_pos_sale`, `ix_offer_redemptions_pos_sale`). No `ON DELETE` is needed: POS sales are never deleted, only reversed.

## Assumed seam interfaces (`internal/pos/seams.go`)

```go
type Pricer interface {
    Price(ctx, customerID, storeID ids.ID, lines []PriceLine) (Pricing{LineDiscounts []int64; BillDiscount int64; Offers []AppliedOffer}, error)
}
type Redeemer interface {
    Redeem(ctx, saleID, customerID ids.ID, applied []AppliedOffer) error // in the sale's tx; ErrOfferGone when one ran out
    Release(ctx, saleID ids.ID) error
}
type Credits interface {
    Allow(ctx, customerID, storeID ids.ID, requested int, eligiblePaise int64) (CreditsAllowance{Allowed int; Limit string}, error)
    Hold(ctx, customerID, storeID, claimID ids.ID, credits int, eligiblePaise int64) (int, error)
    Capture(ctx, claimID, customerID, storeID ids.ID) error // ErrHoldReleased on a released hold
    Release(ctx, claimID ids.ID) error
    Cashback(ctx, Cashback{CustomerID, StoreID, SaleID ids.ID; PaidPaise int64; CashbackBP int}) (int, error)
    ReverseSale(ctx, SaleReversal{SaleID, CustomerID, StoreID, ClaimID ids.ID}) error
}
type Referrals interface {
    Qualify(ctx, Purchase{CustomerID, StoreID, SaleID ids.ID; LinkMethod string; EligiblePaise int64; CreditsUsed int}) error
    Revoke(ctx, customerID, saleID ids.ID) error
}
type Stock interface {
    Sell(ctx, tx, StockSale) (ids.ID, error)
    Reverse(ctx, tx, StockReversal) (ids.ID, error)
    Change(ctx, tx, StockChange) (StockResult, error)
}
```
