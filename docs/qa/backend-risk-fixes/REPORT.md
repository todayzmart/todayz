# Backend risk fixes: report

Branch `claude/backend-risk-fixes-124cfd`. Five fixes from the lead's risk list, each with tests. Decision-log entry D-237; 01 §7 updated, with 02, 03, 04 and 06 aligned. Migrations 49801 and 49802 (block w4-98).

## 1. Split wallet adjustments no longer dodge the second approver

**Before.** D-73 compared each adjustment alone with `admin_adjust_dual_approval_above_paise` (₹500), so ₹1,000 could go through as two ₹500 at-once adjustments.

**Now (D-237).** The threshold is compared with the day's total for that admin and customer:
- the absolute amounts of the same admin's adjustments to the same customer since IST midnight, this one included;
- at-once postings count, and so do the admin's own pending or approved requests;
- an approved request counts once, for the requester (its posting, made by the approver, is excluded from the ledger part);
- rejected and cancelled requests do not count.

At or under the threshold the adjustment posts at once; above it, it needs a second approver. No new number was added.

How it works:
- `credits.Admin.needsSecond` takes `pg_advisory_xact_lock` on `credits.adjust:<admin>:<customer>`, then sums with `AdminAdjustedSince`, inside the create transaction.
- A replay of an Idempotency-Key is found before the sum and keeps its first path.
- The audit row now has `dayTotalPaise`.

Files: `db/queries/credits/adjustments.sql`, `internal/credits/adjustments.go`.

Tests (`internal/credits/adjustments_db_test.go`):
- `TestAdjustmentDaySumDB`:
  - +200, −100 and +200 post; the next ±1 goes pending, with an audited day total of 50,200 paise;
  - another admin, or another customer, has a sum of its own;
  - a rejected request stops counting; an approved one counts for the requester only;
  - the next IST day starts again at zero.
- `TestAdjustmentDaySumConcurrentDB`: 6 concurrent ₹200 adjustments by one admin give exactly 2 posted and 4 pending.
- Existing tests now use separate admins where they relied on per-request behaviour.

## 2. Self-like referrals are held before the reward posts

**Before.** `referral.self_like` was raised only by the hourly risk scan, usually after the reward had posted.

**Now.** Before posting any side, `referrals.PostRewards` runs the same rule on that one referral.
- Where it runs: `risk.Service.CheckReferralSelfLike`, through a new seam `referrals.SelfLikeCheck`, wired in `jobs.ReferralsService`.
- What it checks: it then reads whether an open or confirmed `referral.self_like` flag exists.
- If one exists, the reward is held:
  - nothing posts and no attempt is recorded;
  - `RewardResult.Waiting` is set;
  - `tz_referral_reward_failed_total{reason="held"}` counts it;
  - the sweep skips the referral, as it does a `referral.reward_refused` hold (`QualifiedReferrals` now excludes both rules).
- When a person clears the flag, the next pass posts. A cleared flag is never raised again, because `InsertFlagOnce` checks the whole history.
- If the check fails, nothing posts and the job retries (fail closed).
- The scan rule stays as the backstop.
- `SelfLikeCandidates` takes an optional `referral_id`. The scan passes only `since`, as before.

Tests:
- `risk` `TestCheckReferralSelfLike`: one referral, raised once, and the scan does not raise it again.
- `referrals` `TestSelfLikeHoldsTheReward`: held, skipped by the sweep, held again by the job; posts after the flag is cleared; not raised again.
- `referrals` `TestSelfLikeCheckErrorPostsNothing`.

## 3. Review moderation reasons refuse phone numbers

`reviews.cleanModReason` now refuses a reason that holds a phone number (`body.reason`: "no phone numbers").

It reuses the admin console's existing check. Domain packages cannot import `internal/admin`, so that check's core moved to `platform/pii.ContainsPhone`, and `admin.noPhone` now calls it. The behaviour is unchanged and `TestNoPhone` still passes.

Tests: `reviews` `TestCleanModReason`, `pii` `TestContainsPhone`.

## 4. W3-06 field photos sweep

- **Claim, then delete.** `ClaimStaleFieldDocuments` sets `sweep_retry_at` a 10-minute lease ahead in a short transaction and commits. The objects are then deleted with no row lock held. Past `confirm_by` a photo can no longer be confirmed, so nothing else changes a claimed row. A sweep that dies leaves its rows to a later run.
- **Per-row failure and backoff.** A failed delete increments `sweep_attempts` and sets `sweep_retry_at`. The backoff starts at 1 h (the job is hourly), doubles per failed attempt and is capped at 24 h. The sweep goes on with the other rows; it used to stop at the first failed batch. Never-failed rows are claimed first.
- **Failed count.** The run summary keeps `failed`. A new counter `tz_field_photo_sweep_failed_total` is added for alerting.
- If the job's context ends, the rest of the claimed rows wait out their lease.

Files:
- migration `49801_w4-98_field_photo_sweep_retry.sql` (new columns);
- `db/queries/business/agent.sql`;
- `internal/business/agent_sweep.go`.

Tests:
- `business` `TestSweepFieldPhotosDB`: a failing delete does not block 3 others; backoff of 1 h, then 2 h; rejected once the bucket recovers; owner uploads untouched.
- `business` `TestSweepClaimLeaseDB`.
- `cmd/todayz` `TestFieldPhotoSweepHTTP` is unchanged and passes.

## 5. Backfill of `confirm_by` (migration 49802)

The migration sets `confirm_by = created_at + 20 min` on pending rows with a NULL `confirm_by` that the field routes created. A row counts as a field photo when it has a `document.create` audit row with `actor_kind = 'admin'`. Owner uploads are written as `staff` and keep NULL on purpose. The match encodes the uuid as the ULID text that `audit_log.subject_id` holds.

The next sweep rejects these rows. Down is empty: a backfilled row cannot be told from a new one.

Test: `business` `TestConfirmByBackfillDB` runs the Up SQL against real rows: field rows set, the owner upload and confirmed rows untouched.

## Checks run

- `make gen` (exit 0); `go build ./...`; `go vet` on the touched packages.
- `go test -p 2` against the shared test Postgres:
  - credits, risk, referrals, reviews, business, admin, jobs;
  - platform/pii, platform/settings, platform/migrate (file names, block order, up-down-up);
  - `cmd/todayz -run TestField`.
- `golangci-lint` on the touched packages: 0 issues.

## Open questions for the lead

1. **"Reaches" is read as "goes above".** An adjustment that brings the day's total to exactly ₹500 still posts at once. This keeps D-73's "at or under posts immediately". If you meant `>=`, the change is one character in `needsSecond`, plus the test edges.
2. **The approver's own day.** Approving someone else's request does not add to the approver's day total. Only the requester's total counts it.
3. **The window in 49802 is a code constant.** The task said to read it from the setting the code uses, but the code uses a constant: `pendingLife` = `uploadTTL` (15 min) + 5 min. No `platform_settings` key exists for it, so 49802 writes 20 minutes and says why. For these rows it makes no difference, since all of them are long past it.
4. **A small flag race.** If the reward job and the risk scan check the same referral at the same instant, both could insert a `referral.self_like` flag, because `InsertFlagOnce` is not backed by a unique index. The effect is a duplicate flag only: the reward is still held. A unique partial index would close it.
5. **No alert rule yet.** `tz_field_photo_sweep_failed_total` is exposed, but I did not add a Grafana rule: `check-runbooks` (D-236) needs a runbook for each rule, and alerting is outside this task. A follow-up could add `tz-field-photo-sweep-failed` with a runbook.
6. **DB outage during the work.** Colima's disk threw I/O errors, and `todayz-testpg` was down for a while. It was back up when the tests ran; I did not restart Colima.
