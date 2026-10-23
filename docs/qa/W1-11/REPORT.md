# W1-11 Jobs platform: QA report

## Scope

- **What:**
  - the jobs platform (D-95, D-98) in `backend/internal/platform/jobs`;
  - the registry plumbing in `backend/internal/jobs`;
  - the admin-jobs API (04 §2.7);
  - dashboard 15;
  - the jobs alerts and their runbooks.
- **Branch:** `worktree-agent-a26ccb496531d5e56`.
- **How it was tested:** locally, on testcontainers databases. No cloud resources, no outbound network, and no `make dev`.
- **Who wrote this:** the lead committed this report from the agent's hand-back.

## What was built

### `jobs.Define`
- **Commit:** `46b47cf`.
- **What it provides:**
  - `Kind`, `Result`, `Yield` and `ErrYielded`;
  - `Enqueuer.EnqueueTx`;
  - an in-memory `Fake`.
- **Value sets:**
  - 7 classes, 6 triggers and 8 statuses;
  - uniqueness periods PerTick, PerHour, PerDay and PerMonth.
- **`Validate` enforces:**
  - Owner, Class and Description are set;
  - timer and money kinds have a runbook;
  - `UniqueBy` is set if and only if the kind is scheduled;
  - `Timeout` is 60 s or less unless the kind is Batched;
  - `MaxAttempts` is from 1 to 25;
  - names are snake_case.

### Tables
- **Commit and migration:** `0db7a7e`, migration 11101.
- **`job_runs`:** monthly partitions; the app role cannot delete rows.
- **`job_stats_hourly`:** includes cancelled and yielded counts.
- **`job_controls`.**
- **`partition_policies.retention_days`.**
- **`drop_old_partitions`:**
  - owner-only and `SECURITY DEFINER`, with a pinned `search_path`;
  - refuses a table that is not listed, has no retention set, or whose partition is too recent.

### Runtime
- **Commit:** `2f8e02f`.
- **Run logging:**
  - The middleware writes a `job_runs` row for every run except event successes, which are only counted.
  - It also writes `job_stats_hourly` and the `tz_job_*` metrics.
  - Errors are scrubbed and capped at 2 kB.
  - Logs carry `run_id` and `trace_id`.
- **Scheduling and control:**
  - an IST cron scheduler that honours `UniqueBy`;
  - pause: scheduled ticks are not run and events are snoozed;
  - concurrency slots;
  - `jobs_watchdog`;
  - `job_runs_retention`.
- **Other:**
  - W0-03's jobs are now registered as `platform_heartbeat` and `platform_maintenance`;
  - the registration lint.

### Admin-jobs API
- **Commit:** `1889d2c`.
- **Endpoints:** 8, behind a `Gate` seam. Production uses `DenyGate` until W1-01 lands.
- **Class rules:**
  - timer kinds are never run by hand;
  - event kinds are not run now or retried;
  - money kinds need `super_admin` with step-up;
  - finance can only read.
- **Writes:** every write is audited, and run-now needs an `Idempotency-Key`.

### Dashboard, alerts and runbooks
- **Commit:** `0d1ffdb`.
- **Dashboard:** `15-jobs.json` (uid `tz-jobs`).
- **Alerts:** `alerts/jobs.yaml`, with 4 rules.
- **Runbooks:** `jobs-job-failing`, `jobs-cron-missed` and `jobs-hard-cancelled`.

### Label allow-list
- **Commit:** `69a731b`.
- **What it does:** applies SC-JB-13 to every registered kind.

## Scenario coverage

| Scenario | Test |
|---|---|
| SC-JB-01 | TestValidate, TestDefinePanicsOnInvalidAndDuplicate, TestWorkersOnlyThroughDefine, TestRegistrationLintCatches |
| SC-JB-02 | TestScheduledRunIsLogged |
| SC-JB-03 | TestEventSuccessesAreCountedNotLogged |
| SC-JB-04 | TestFailureIsScrubbedRetriedAndDiscarded, TestFakeRetriesThenDiscards |
| SC-JB-05 | TestWatchdogFlagsMissedPeriods |
| SC-JB-06 | TestRunNowAdminOnlyAuditedIdempotent, TestRunNowIsWorked |
| SC-JB-07 | TestTimerAndEventKindsAreNotRunnable |
| SC-JB-08 | TestMoneyKindsNeedSuperAdminWithStepUp |
| SC-JB-09 | TestPauseResume, TestPauseSkipsTicksAndSnoozesEvents |
| SC-JB-10 | TestRetry |
| SC-JB-11 | TestRunNowAdminOnlyAuditedIdempotent, TestScheduledRunIsLogged, TestFakeScheduledUniqueBy |
| SC-JB-12 | TestRetentionDropsOldPartitions |
| SC-JB-13 | TestScheduledRunIsLogged, TestEveryKindCarriesOnlyAllowedLabels, TestRegistryLookupsAndLabels |
| SC-JB-14 | Not testable yet: there are no ai, marketing or notify kinds. Shared with W3-11. |
| SC-JB-15 | TestValidate |

## Evidence (agent, before the rebase onto W1-07)

| Check | Result |
|---|---|
| `go test -race` with DB (jobs, internal/jobs, cmd/todayz) | ok |
| go vet, gofmt | clean |
| golangci-lint | 0 issues |
| Dashboard 15 | The JSON parses. Every Postgres query ran on a migrated DB (throwaway test). |
| `jobs.yaml` | The YAML parses. On seeded rows, the "Job failing (scheduled)" SQL flags only the kind with 3 failures in a row (throwaway test). |
| make check | Passed, exit 0, all steps. Backend lint: 0 issues. |

## Decisions

- **Run ids.** Stored as uuid and shown as ULID in the API and logs. The dashboard's run history shows the uuid.
- **Runs list.** `from` is inclusive and `to` is exclusive.
- **Run-now periods.**
  - A PerTick run-now period must be a real tick of the kind.
  - A future period is refused.
- **Admin access before W1-01.**
  - The validator answers 401 on `/v1/admin` until W1-01 lands.
  - `DenyGate` is a second lock.
- **Brief 409.** Run-now may answer 409 for under a second after a run finishes, while River finalises jobs in batches.
- **Job failing alerts.**
  - The scheduled-kind rule counts attempts, not ticks.
  - The event-kind rule has no minimum volume.
- **Event kinds in alerts.** They are told apart by `unless on (kind) tz_job_missed`.
- **Hard-cancel counter.** `tz_job_hard_cancelled_total` starts at 0 for every kind.

## Lead decisions (2026-10-23)

- **No `job_kinds` table for now.**
  - Without it, dashboard 15 cannot show owner, schedule or next run.
  - A-21 shows them from the registry.
  - Revisit after the core loop.
- **Event-kind retry stays refused** (conservative). River's own retries still apply.
- **No jobs seed domain for `make demo`** this weekend. The platform kinds give A-21 real runs.
- **Runbook names** keep the `jobs-` prefix.

## Open

1. **W1-01 wiring.** Swap `DenyGate` for the real gate, and register `step_up_required`.
2. **Local alert loading.** The alert files are not loaded by the local stack.

## Not verified

- **PromQL** was not run against a live Prometheus: no promtool was available offline, and the stack was not started.
- **Grafana alerts.** Grafana did not load the alert file, so the severity templating was not exercised.
