# Job hard-cancelled at shutdown

Alert `tz-jobs-hard-cancelled` in `infra/grafana/alerts/jobs.yaml` (05 §5, §8, D-98).
Severity: **warning**.

## What it means

A worker was stopping (deploy, scale-in, restart) and a job of this kind was still running
after the 90-second drain grace. The worker then called River's `StopAndCancel`:

- the job's context was cancelled
- the worker logged `queue: job hard-cancelled at shutdown` at error level, with the kind and
  River job id
- the worker added 1 to `tz_job_hard_cancelled_total{kind}`

River retries the attempt on another worker, so nothing is lost if the handler is idempotent,
as every handler must be. The run shows `cancelled` on A-21, followed by the retry's attempt.

D-98 caps a non-batched attempt at 60 s and makes long kinds `Batched`, calling `Yield` between
batches. A hard cancel therefore means a kind did not keep to that contract.

## First three checks

1. **Dashboard 15 · Jobs** (https://grafana.todayz.localhost/d/tz-jobs/15-jobs?var-kind=KIND):
   - *Hard-cancelled at shutdown (24 h)*: how many, and which kinds?
   - *Duration p50/p95/max* for the kind: how long do its attempts take normally?
2. **A-21 Jobs** (https://admin.todayz.localhost/jobs): open the kind's runs around the shutdown.
   - The cancelled attempt should be followed by a retry that succeeded.
   - A retry that failed or was discarded is the *Job failing* runbook, and possibly a
     non-idempotent handler.
3. **Logs** (dashboard 15, *Job logs*, or `make logs` locally):
   - Find `queue: jobs still running after the drain grace` and the lines after it.
   - `queue: jobs did not return after the hard cancel` means a handler ignores its context.
     This is worse: it can hold a database connection until the process exits.

## Likely causes

- **A non-batched kind ran longer than expected**: a slow query, a slow provider, or a lock
  wait. Its `Timeout` should have cut it first. Check that the handler passes `ctx` to every
  database and HTTP call.
- **A batched kind did not call `Yield(ctx)`** between batches, so it never handed over at
  shutdown.
- **A handler ignores cancellation**: a loop without a `ctx.Err()` check, or `context.Background()`
  used inside the handler.
- **The drain was shortened**: a container stop timeout below 180 s, or `SHUTDOWN_DRAIN`
  changed. Check the deploy config.

## Fix

- Check that the retried run succeeded and did not double-apply:
  - For money and stock kinds, compare the run's summary counts with the ledger or stock rows.
  - Idempotency keys and unique periods should make a retry a no-op. If they did not, that is a
    bug in the kind: raise it with the owner now.
- Make the kind finish inside the grace:
  - Pass `ctx` everywhere.
  - Lower its batch size.
  - Make it `Batched` with `Yield`.
  - Lower its `Timeout`.
- No manual action is needed on the run itself; River has retried it.

The alert clears 10 minutes after the last hard cancel.

## Who to tell

- **The kind's owner** (A-21): they must make the kind drain-safe.
- **The lead**, if the retry double-applied anything, or if `did not return after the hard
  cancel` appeared.
- **The finance admin**, for a `money` kind, with the run ids.
