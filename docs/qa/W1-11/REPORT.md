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

## Rebase onto W1-07 (2026-10-23)

- Rebased onto main 8cf046f (W1-07 merged). Commits a06fed0 and 039d408.
- `cmd/todayz/worker.go`: conflict resolved. The worker builds the jobs registry and runs it through `platformjobs.Runner` next to W1-07's queue metrics.
- `telemetry/labels.go`: the jobs labels (`kind`, `class`, `status`) are added to W1-07's allow-list.
- `telemetry/dashboards_test.go`: dashboard 15 is covered by W1-07's dashboard test.
- The jobs histograms use W1-07's exponential view.
- TestPauseResume: `updatedAt` is now bounded by the moving offset clock (it flaked under load).
- Files outside W1-11's paths: `backend/cmd/todayz/worker.go`, `backend/internal/platform/telemetry/labels.go` and `backend/internal/platform/telemetry/dashboards_test.go`.
- Post-rebase `make check`: every step through check-ui passed. check-infra stopped on uninitialised Terraform providers. Fixed with `scripts/ci/terraform.sh prefetch && make setup-infra-cloudflare`, as the lead allowed.

## Review fixes (2026-10-23)

| # | Review item | Fix | Test |
|---|---|---|---|
| 1 | `tz_job_missed` was per process | The gauge callback reads missed state from the database: one query for every scheduled kind, the same rule as `missedNow`. A paused kind (not a timer) reads 0. The result is cached for 30 s and read again at once after this process's watchdog commits or a scheduled kind succeeds here. A failed read keeps the last values. The watchdog no longer sets the gauge inside its transaction. `Registry.MissedKinds(ctx)` reads through the same cache. | TestMissedGaugeIsSharedAcrossWorkers: two runners on one DB. A's watchdog flags and both read 1. B succeeds: B reads 0 at once, A reads 1 (cached), then 0 after the TTL. |
| 2 | Watchdog after a pause | `job_controls.paused_at` and `resumed_at` hold the latest pause window. They are set by the pause/resume upsert. The watchdog does not flag a tick when the kind was paused at the tick or during its allowance and has since resumed. | TestWatchdogSkipsPauseWindows: paused across the tick, or inside the allowance, is not flagged. Resumed before the tick is flagged. TestPauseResume checks that the window is stored. |
| 3 | `drop_old_partitions` lock wait | The function sets `lock_timeout = '3s'`. Behind a long reader it fails with 55P03, and the daily retention retries it. | TestDropOldPartitionsGivesUpOnLock: a reader holds `job_runs`, the drop waits, an insert queued behind it goes through within the timeout, and the drop returns 55P03. After the reader ends, the retry drops the partitions. |
| 4 | Bad bytes in errors | `scrubError`, `scrubNote` and the new `scrubSummary` (on Summary keys) apply `strings.ToValidUTF8` and strip NUL. Summary keys that collide after cleaning are summed. | TestBadBytesAreSanitised: the handler returns NUL and invalid UTF-8 in its error, note and summary. The run ends as discarded with the cleaned text. |
| 5 | Retention floor | The `job_runs` policy is seeded with `retention_days = 30`. The job still uses `job_runs_retention_days` (90). | TestRetentionDropsOldPartitions: a 29-day cut-off is refused ("keeps at least 30 days"). A 31-day cut-off is allowed. |
| 6 | First-boot catch-up | A tick due before the runner started is caught up only when the kind has a `job_runs` row. Without one, the scheduler writes a skipped first-seen row for that period, under a per-kind advisory lock. The watchdog counts it as a run, so the period is not flagged. The kind first runs at its next tick. | TestFirstBootWaitsForNextTick: the kind with history catches up and the fresh kind does not. A second booting worker writes nothing. The watchdog does not flag the period, and both kinds run at the next tick. |
| 7 | Hard cancel on the last attempt | `outcome` records `discarded`, not `cancelled`, when the context was cancelled on the last attempt and the cause is not River's remote cancel. | TestOutcome (new cases) |
| 8 | Cron `*/N` in day fields | A day-of-month or day-of-week field starting with `*` counts as unrestricted for the OR rule (Vixie). | TestScheduleNextPrev: `0 0 */2 * 1` and `0 0 1 * */2` |
| 9 | Concurrency | `Validate` caps `Concurrency` at 10 (`maxConcurrency`). Each capped run pins one pool connection for its whole run, and the pool defaults to max(4, NumCPU). `job_stats_hourly` pruning is unchanged. | TestValidate: 10 is accepted, 11 is refused. |

**Migration edited in place.** `11101_w1-11_jobs.sql` is not on main, so it was edited rather than given an 11102. A local database that already applied 11101 needs `make reset`.

## Evidence (agent, after the review fixes)

| Check | Result |
|---|---|
| `go test -race` with DB: `internal/platform/jobs/...`, `internal/platform/migrate` (TestUpDownUp), `internal/jobs`, `cmd/todayz` | ok |
| gofmt, go vet | clean |
| make check (commit e77b0c9) | Passed, exit 0, every step: check-repo through check-tf-static. 267 Go `ok` lines; golangci-lint 0 issues; checkov 370 passed, 0 failed. |

## Questions (review round)

1. **Retention setting below the floor.** `job_runs_retention_days` allows values down to 1 (migration 00010). A value under 30 now makes `job_runs_retention` fail with "too recent". The failure is loud, nothing is dropped, and the failing alert fires. Should the setting's minimum be raised to 30? TODO(lead)
2. **Pause history.** Only the latest pause window is kept. A pause, resume and second pause inside one allowance excuses only by the latest window.
3. **W1-01 merge.** On main, `backend/internal/jobs/identity.go` registers `identity.purge` with the W0-03 River registry (`Add` and `r.Every`). W1-11 replaced that registry with `jobs.Define`, so the merge will not compile until identity.go is ported. Suggested port: `Define(r, Kind{Name: "identity_purge", Class: ClassMaintenance, Schedule: "0 * * * *", UniqueBy: PerTick, ...})` calling `identity.Purge`. The old kind name `identity.purge` also fails the snake_case rule. The file is W1-01's.
4. **Carried over.** The events-retention gap. 05 §2.1 should list `status` among the allowed labels.
