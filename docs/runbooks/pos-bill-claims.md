# POS bill claims

Kind `pos_bill_claim_expiry` (owner W2-04, class `timer`, code in
`backend/internal/pos/claims.go` `ExpireClaims`, declared in `backend/internal/jobs/pos.go`).
The general alert is [Job failing](jobs-job-failing.md) (`tz-jobs-failing-event`, critical for
timers). A failed release does not fail the run, so it has its own alert:
`tz-pos-claim-release-failed` (*Bill claim hold not released*, critical,
`infra/grafana/alerts/pos.yaml`): `tz_pos_claim_release_failed_total` rose in 10 min, for 5 min.

## What it means

`tz-pos-claim-release-failed` fired: the claim-expiry sweep could not release a bill claim's credit hold. The customer's credits stay held after the bill QR expired. A failed sweep run is [Job failing](jobs-job-failing.md).

## First five minutes

1. Logs: "pos: claim hold not released" gives the claim id.
2. Check [Ledger invariant](ledger-invariant.md) is not firing too.
3. Tell the credits owner with the claim id. Never edit `bill_claims` or a hold by hand.

## What it does

Every minute it expires every bill QR claim (`bill_claims`) that is `open` or `claimed`,
has no sale and is past `expires_at` (120 s after the POS made the QR). For a claim a customer
confirmed (`claimed_by` set), it releases the credits the confirm held (the Credits seam,
`Release`, which is the W2-01 ledger in the worker as in the API since D-132: `jobs.PosJobDeps`).
A sale whose hold the ledger already released is refused with 409 `claim_expired` and spends
nothing (`RefuseReleased`, D-132). Each claim runs in its own transaction, so one that fails does not hold up the rest, and the
sweep never holds more than one customer's ledger accounts at a time (D-132).
The summary is `expired`, `released` and `failed`.

The sweep only gives holds back. The reads and the sale do not wait for it: a claim past
`expires_at` already shows `expired`, and a sale that tries to bind it gets `claim_expired`
(D-131). Two other paths cancel claims and release their holds in their own transaction: a new
QR for the same bill, and the bill's sale being rung.

It is idempotent:

- The update only takes live rows (`status IN ('open','claimed')`, no sale), so a second run finds
  nothing to do.
- `Release` of a claim with no hold, or one released before, is a no-op (seams.go contract).
- Rows a sale is binding right now are skipped (`FOR UPDATE SKIP LOCKED`); that sale sees the
  claim expired and refuses, and the next tick expires it.
- It reads the due claims by `(expires_at, id)`, 500 a page, without locks, then expires and
  releases each one in its own transaction (`LockDueBillClaim`), until none is left or the job's
  30 s timeout ends; a claim that committed stays done.
- A claim whose `Release` fails is rolled back on its own, stays live and is retried next tick
  (counted in `failed` and in `tz_pos_claim_release_failed_total`).

## Diagnosis

1. **A-21 Jobs**: open the kind, then the latest failed run: the error, the attempt and the
   summary (`expired`, `released`, `failed`).
2. **The claims**: read `bill_claims` (read-only) for live rows past `expires_at`:
   `status`, `claimed_by`, `credits_used`, `pos_sale_id`.
3. **Logs**: dashboard 15 *Job logs* with the run id; follow `trace_id`. Logs carry claim ids only,
   never a token.

### Likely causes

- **503 "pos: not configured"**: the worker started without the database or settings. Check the
  worker's boot log and `make env-check` names.
- **`failed` above 0, or `tz_pos_claim_release_failed_total` rising** (log line "pos: claim hold
  not released; retried next pass" with the claim id): the credits ledger refused that release.
  Only that claim is rolled back; the run still succeeds and the next tick retries it. A claim
  that fails every tick is the credits owner's (W2-01) to read with the claim id. Failing claims
  do not hold back the others (the run pages past them), but each is retried every tick: fix the
  ledger first.
- **Live claims pile up past `expires_at` with no failed runs**: the kind is not scheduled on the
  worker (check A-21 for recent runs and the [missed cron](jobs-cron-missed.md) alert).

## Mitigation

- Timer kinds are **never paused, run or retried by hand** (04 §2.7; A-21 refuses with
  `job_not_runnable`). Fix the cause; the next tick picks the claims up.
- Never edit `bill_claims` or a credit hold by hand. A hold that stays after its claim expired is
  a credits incident: tell the W2-01 owner with the claim id.

## Sync retries

A sync batch is one transaction. If Postgres picks it as a deadlock victim (`40P01`) or a
serialization failure (`40001`), usually against this sweep releasing a claim of a customer in
the batch, the whole batch is retried, up to 3 attempts (D-132). Each retry logs
`pos: sync transaction retried` (store id, attempt) and counts in `tz_pos_sync_retried_total`.
A few a day is expected; a steady rise means a new lock-order problem: read the warn logs'
errors and D-132's lock order before anything else.

## Escalation

**Level:** Critical: SEV2, because customer credits are stuck. SEV1 if ledger drift shows too ([Severity levels](../ops/severity.md)).

- The **W2-04** owner for the sweep and the **W2-01** owner for the credits ledger.
- The **lead** for any SEV2.

## Follow-up

- A test with the claim state the ledger refused.
- Confirm the hold was released (the claim's next tick) before closing.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
