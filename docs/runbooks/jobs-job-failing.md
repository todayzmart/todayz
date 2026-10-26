# Job failing

Alerts `tz-jobs-failing-scheduled` and `tz-jobs-failing-event` in `infra/grafana/alerts/jobs.yaml`
(05 §5, D-95). Severity: **critical** for `timer` and `money` kinds, **warning** for the rest.

## What it means

A background job kind keeps failing.

- **Scheduled kind** (it has a cron schedule): its last three finished runs all failed or were
  discarded. Every River attempt is its own run row, so one tick that failed three attempts in a
  row fires this too. Skipped runs (the period was already done) and yielded runs (handed over at
  shutdown) do not count either way.
- **Event kind** (enqueued by the product, for example an order timer or a push): more than 20 % of
  its finished attempts in the last 15 minutes failed or were discarded. There is no minimum
  volume, so one failure out of one attempt fires.

A failed attempt is retried with backoff until `MaxAttempts`; then the run is **discarded** and
nothing retries it on its own. For a `timer` kind this means an order, booking or hold is not
being moved on time. For a `money` kind, a ledger, payout or reconciliation step is not done.

## First five minutes

1. A-21 Jobs: open the kind, then the latest failed run. Read the error.
2. Is it one kind or many? Many failing at once usually means the database or a deploy.
3. If a deploy started it, roll back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)). Never run, pause or retry a `timer` kind by hand.

## Diagnosis

1. **A-21 Jobs** (https://admin.todayz.localhost/jobs): find the kind (badge *Failing*). Open
   the latest run. It shows the error (at most 2 KB, scrubbed), the attempt, the summary counts
   and every attempt of the run. The owner and the runbook of the kind are on the same screen.
2. **Dashboard 15 · Jobs** (https://grafana.todayz.localhost/d/tz-jobs/15-jobs?var-kind=KIND):
   - *Runs by status* and *Attempts and retries* show when it started and whether some runs
     still succeed.
   - *Duration p50/p95/max* shows timeouts: a duration near the kind's `Timeout` means the
     attempt was cut, not refused.
3. **Logs and trace**: in dashboard 15, *Job logs*, enter the run id from A-21. Follow
   `trace_id` to Tempo to see the failing database or provider call. Then check whether other
   kinds fail at the same moment on dashboard 01 (API/worker health) and 02 (database):
   - All kinds failing together points at the database, the worker or a deploy.
   - One kind failing points at that kind's code or data.

### Likely causes

- **A dependency is down or slow.** Database (pool exhausted, lock wait), the mocks or provider
  (SMS, WhatsApp, push, payments): the error names the call, and egress shows refused hosts.
- **A deploy changed the handler or the data it reads.** Compare the first failing run's time
  with the release.
- **One bad row** (a poison event): the same entity fails every attempt while other runs of the
  kind succeed. The error or the summary names it.
- **Timeouts.** A non-batched kind is capped at 60 s (D-98). A kind that grew past that must
  become `Batched` and `Yield` between batches.
- **A paused dependency kind**, or a setting changed in `platform_settings` (A-20 audit log).

## Mitigation

- Fix the cause first (dependency, data or code).
- **Scheduled kind, not `timer`:** on A-21, **Retry** the failed or discarded run that still
  matters.
  - Retry runs the same period again as a new run (trigger `retry`). It needs a reason and is
    audited.
  - It runs even if a later attempt of that period already succeeded, so check the run history
    first.
  - **Run now** with a period key does the same for any past tick.
  - For a `money` kind, both need super_admin with step-up.
- **Event kind:** A-21 cannot retry it.
  - Its arguments are not kept in `job_runs`; River's own retries are the only replay.
  - A discarded event run must be re-driven by the owning domain (its repair or reconcile
    path). Ask the owner.
- **`timer` kinds are never paused, run or retried by hand** (04 §2.7; A-21 refuses with
  `job_not_runnable`). Fix the cause; River's retries and the next timer sweep catch up.
- If a non-timer kind is doing harm while broken (for example sending wrong notifications),
  **Pause** it on A-21 with a reason:
  - Paused scheduled ticks are not run. They are logged, not recorded, and the watchdog does
    not report them missed.
  - Paused event jobs are snoozed and wait in the queue until Resume.
- Never edit `job_runs` or `river_job` by hand. The app role cannot delete from `job_runs`.
- A stuck `running` row is closed as failed by `jobs_watchdog` after the kind's Timeout plus
  5 minutes.

The alert clears when:

- **Scheduled kind:** one of the next three finished runs succeeds.
- **Event kind:** the 15-minute failure ratio drops to 20 % or below.

## Escalation

**Level:** `timer` and `money` kinds: SEV1 if orders or money are stuck for many people, else SEV2. Other kinds: SEV3 ([Severity levels](../ops/severity.md)).

- **The kind's owner** (the work package on A-21, for example W2-02 for order timers). They own
  the fix.
- **critical** (`timer`, `money`): also the lead at once.
  - Money: the finance admin, before any manual correction.
  - Timers: the support admin, if customers or shops saw late or wrong order states.
- After a discarded money run is retried, note it in the incident log with the run ids.

## Follow-up

- A test that reproduces the error.
- If runs were discarded, check what they left undone and who fixes it (the kind's runbook).
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
