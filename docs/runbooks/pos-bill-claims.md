# POS bill claims

Kind `pos_bill_claim_expiry` (owner W2-04, class `timer`, code in
`backend/internal/pos/claims.go` `ExpireClaims`, declared in `backend/internal/jobs/pos.go`).
The general alert is [Job failing](jobs-job-failing.md) (`tz-jobs-failing-event`, critical for
timers).

## What it does

Every minute it expires up to 500 bill QR claims (`bill_claims`) that are `open` or `claimed`,
have no sale and are past `expires_at` (120 s after the POS made the QR). For a claim a customer
confirmed (`claimed_by` set), it releases the credits the confirm held (the Credits seam,
`Release`). The summary is `expired` and `released`.

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
- More than 500 due claims are taken over the following ticks, oldest first.

## First three checks

1. **A-21 Jobs**: open the kind, then the latest failed run: the error, the attempt and the
   summary (`expired`, `released`).
2. **The claims**: read `bill_claims` (read-only) for live rows past `expires_at`:
   `status`, `claimed_by`, `credits_used`, `pos_sale_id`.
3. **Logs**: dashboard 15 *Job logs* with the run id; follow `trace_id`. Logs carry claim ids only,
   never a token.

## Likely causes

- **503 "pos: not configured"**: the worker started without the database or settings. Check the
  worker's boot log and `make env-check` names.
- **"pos: release credits"**: the credits ledger refused the release. That is the credits
  owner's (W2-01) to read with the claim id; the whole batch rolls back and the next tick
  retries it.
- **Live claims pile up past `expires_at` with no failed runs**: the kind is not scheduled on the
  worker (check A-21 for recent runs and the [missed cron](jobs-cron-missed.md) alert).

## Fix

- Timer kinds are **never paused, run or retried by hand** (04 §2.7; A-21 refuses with
  `job_not_runnable`). Fix the cause; the next tick picks the claims up.
- Never edit `bill_claims` or a credit hold by hand. A hold that stays after its claim expired is
  a credits incident: tell the W2-01 owner with the claim id.
