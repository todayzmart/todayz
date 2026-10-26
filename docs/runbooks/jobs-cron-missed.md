# Cron missed

Alert `tz-jobs-cron-missed` in `infra/grafana/alerts/jobs.yaml` (05 §5, D-95, SC-JB-05).
Severity: **critical** for `money` kinds, **warning** for the rest.

## What it means

A scheduled job did not run a tick on time. `jobs_watchdog` runs every 5 minutes. For every
scheduled kind that is not paused, it looks at the latest tick whose allowance has passed:

- twice the kind's interval (a 5-minute kind: 10 minutes)
- 15 minutes for a daily or longer kind

If that tick has no run at all, the watchdog:

- writes one `missed` row to `job_runs`
- logs `scheduled job missed a period`
- sets `tz_job_missed{kind}` to 1

The gauge drops back to 0 once the kind has a succeeded run newer than the missed row.

"Missed" means the run never started. A run that started and failed is the *Job failing* alert,
not this one.

Ticks before the jobs platform first ran on this database are never reported.

## First five minutes

1. A-21 Jobs: which kind is *Missed*, and was it paused?
2. Dashboard 15: one kind or many? Many means the worker is down or not leader.
3. Check the worker is up (`/readyz`, Beanstalk health in the cloud). If it is down, see [Worker unhealthy](worker-unhealthy.md).

## Diagnosis

1. **A-21 Jobs** (https://admin.todayz.localhost/jobs): the kind shows *Missed*. Check:
   - its schedule and the last run
   - whether it was paused: a paused kind is not checked, so a missed row right after
     Resume is from before the pause
2. **Dashboard 15 · Jobs** (https://grafana.todayz.localhost/d/tz-jobs/15-jobs):
   - *Kinds missed now*: one kind or many?
   - *Oldest available job*: a large age means the worker is not taking jobs at all.
   - The kinds table shows `last_missed` and `last_run`.
3. **The worker** (dashboard 01, `todayz worker` `/readyz` on the health port, `make logs` locally):
   - Is it up?
   - Is it the leader? River's periodic scheduling runs on the elected leader only.
   - Did a deploy or restart happen at the tick?

### Likely causes

- **The worker was down, restarting or draining at the tick**, so no instance enqueued it. This
  is the most common cause, for example a deploy at 02:30 for a daily kind.
- **The queue is backed up.** The tick was enqueued but not worked inside the allowance. The
  *Oldest available job* stat shows it, and so does the River backlog on dashboard 15 and the
  Job backlog alert.
- **Leader election stuck.** River could not elect a leader, for example because database
  connections were exhausted.
- **The kind's queue has no workers.** A kind declared on a queue the worker does not serve.
  The jobs registration lint should catch this; check the worker's queue config.
- **The clock moved.** In local and test, the test clock was advanced past ticks; the watchdog
  sees the injected time. This is expected there; do not page.

## Mitigation

- Restore the worker and the queue first: restart it, scale it, or free database connections.
- Then **Run now** on A-21 with the missed period key, so the period is covered:
  - The key is shown on the missed row, for example `2026-10-22` for a daily kind.
  - A-21 refuses a future period, a key that is not one of the kind's ticks, and a run while
    one is queued or running.
  - Run now always runs, even for a period that already succeeded, so check the run history
    first. A late tick may have run after all.
  - A missed row can also be **Retried** from A-21, which runs its period the same way.
  - A `timer` kind (for example a hold expiry sweep) cannot be run by hand. Its next tick
    covers everything due, so restoring the worker is the fix.
  - Only the latest missed period matters for kinds whose run covers "everything up to now",
    such as rollups. Kinds that cover one period each (`PerDay` money kinds) need every missed
    period run.
  - Money kinds need super_admin with step-up.
- Do not insert rows into `job_runs` or `river_job` by hand.

The alert clears once a run of the kind succeeds (the next tick or your Run now).

## Escalation

**Level:** `money` kinds: SEV2 (SEV1 if payouts or the ledger are due today). Other kinds: SEV3 ([Severity levels](../ops/severity.md)).

- **The kind's owner** (A-21) if the cause was in the kind.
- **The lead**, if the worker or the database was the cause.
- **critical** (`money`): the finance admin, with the periods that were run late, because
  settlement and payout reports for those periods may have gone out late.

## Follow-up

- If a deploy hit the tick, move deploys away from that time or check the kind catches up on its own.
- If the queue was backed up, see [Job backlog](queue-job-backlog.md) follow-ups.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
