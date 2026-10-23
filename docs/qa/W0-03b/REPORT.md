# W0-03b Graceful shutdown: QA report

- **Branch:** `worktree-agent-a08c69041af874e47`, rebased onto `main` 5426967
- **Machine:** macOS arm64 (Apple M-series), Docker via Colima, Go 1.26.10, Bash 3.2 (`/bin/bash`) for the scripts
- **Dates:** 2026-10-22 (build), 2026-10-23 (finish, rebase, checks)
- Everything ran locally. Nothing touched a cloud account; no remote, no push. No migration. No generated file is committed; the setup symlinks (`.tools`, `.cache/govulndb`) stay ignored. `proc-test.sh` uses dummy `AWS_*` values in a throwaway env file and prints variable names only.
- Built by the W0-03b agent (D-98; D-95, D-97 and D-100 apply). The lead reran the changed packages and `proc-test.sh` and had the diff reviewed before merging.

## What was built

**API drain (`internal/platform/lifecycle`, `cmd/todayz/api.go`).**
- `lifecycle.Server.Run` owns the order on SIGTERM:
  1. Set the `/readyz` draining flag.
  2. Keep serving for `SHUTDOWN_PRESTOP` (15 s deployed, 1 s locally).
  3. `srv.Shutdown` with `SHUTDOWN_DRAIN` (75 s).
  4. Stop the in-process loops and close the DB pool.
  5. Flush telemetry (5 s).
- A drain that runs past its deadline force-closes the server, logs at error level and returns `ErrDrainDeadline`.
- Constants: `PreStopDeployed`, `PreStopLocal`, `Drain`, `TelemetryFlush`, `APIStopTimeout` (120 s), `WorkerStopTimeout` (180 s).

**Draining flag (`internal/platform/health`).**
- `SetDraining()` and `Draining()` on an `atomic.Bool`.
- While draining, `/readyz` answers 503 `{"status":"draining"}` at once, with no DB check, and `/healthz` stays 200.
- Contract: `ReadinessDraining` in `schemas/platform.yaml`; the `/readyz` 503 response is a `oneOf`.

**Config.** Optional `SHUTDOWN_PRESTOP` and `SHUTDOWN_DRAIN`, each above 0 and at most 1 h. An invalid value is named in the error, never shown. Both names are in `.env.example` and in `backend/CLAUDE.md`.

**`BaseContext`.** `context.Background()` in `httpx/router.go`, so SIGTERM never cancels a request context.

**Worker drain (`queue/drain.go`, `cmd/todayz/worker.go`).**
- The worker sets the draining flag first, then calls `queue.Drain(c, 90s, 30s, log)`:
  - River `Stop` runs for up to 90 s.
  - If jobs are still running, `StopAndCancel` gets up to 30 s.
- An `inflight` worker middleware tracks running jobs. Each job still running at the hard cancel is logged at error level and counted in `tz_job_hard_cancelled_total{kind}`.
- The health server stops only after the drain.
- The "twice this" comment is gone.

**`proc.sh`.**
- `lib.sh` gains `tz_strip_aws_env` and `tz_load_app_env`. App processes, migrate and seed lose every `AWS_*` variable, whether it comes from the env files or from the calling shell (D-97).
- `stop_group` checks every limit before sending any signal, then SIGTERMs, polls every 0.2 s, and SIGKILLs only at the limit (saying so on stderr).
- Limits: api 120 s, worker 180 s, others 10 s, overridable with `PROC_STOP_TIMEOUT_API`, `PROC_STOP_TIMEOUT_WORKER` and `PROC_STOP_TIMEOUT`.
- `stop-all` stops api and worker together, then the rest, mocks last.

**Docs.** `backend/CLAUDE.md` (Shutdown and Workers bullets, env list), `infra/local/README.md`.

## Tests

| Acceptance item | Test |
|---|---|
| Drain > WriteTimeout; 120 s > prestop + drain; 90 + 30 < 180 s (SC-GS-03) | `lifecycle.TestDrainOutlivesWriteTimeout`, `TestAPITimingsDefaults`, `config.TestShutdownDurations` |
| BaseContext not cancelled by SIGTERM (SC-GS-03) | `lifecycle.TestBaseContextIsNotTheSignal` (mutation checked) |
| In-test 5 s handler: 200 with the full body; `/readyz` 503 at once, `/healthz` 200; refused after the prestop (SC-GS-01/02) | `lifecycle.TestSIGTERMDrainsInFlightRequest` (mutation checked) |
| Drain deadline | `lifecycle.TestDrainDeadlineCutsAndLogs`, `TestListenErrorReturns` |
| `/readyz` draining | `health.TestReadyzDraining` |
| `queue.Drain` graceful / hard paths, metric and error log | `queue.TestDrainLetsRunningJobFinish`, `TestDrainHardCancelsAfterGrace`, `TestDrainGivesUpAfterHardGrace` |
| Worker job in flight finishes (SC-GS-04) | `cmd/todayz.TestWorkerSIGTERMLetsJobFinish` |
| No `AWS_*` in app processes (SC-OPS-09, proc.sh part) | `infra/local/test/proc-test.sh` §1 |
| `stop-all` order; SIGKILL at the limit; bad limit refused | `proc-test.sh` §2, §3 |

`proc-test.sh` gives 8 FAILs against the old `proc.sh` and passes against the new one in about 9 s.

## Evidence

- **`make check`:** exit 0 on 63ee03b (2026-10-23). golangci-lint reported 0 issues; `go test -race` passed every package; the three proc-test sections printed ok.
  - An earlier run under host load of about 50 failed only on testcontainers start timeouts and on W0-04's wall-clock bound in `TestErrorAmplificationBound` (2.29 s against 2 s). Those are environment failures, not this change.
- **Lead rerun (2026-10-23):** `go test -race -count=1` on lifecycle, health, config, queue and cmd/todayz passed; `proc-test.sh` printed ok for all three sections.
- **`make e2e`:** exit 0 from a stopped stack.
  - dev-smoke: all checks pass.
  - Schemathesis: 39 generated, 39 passed.
  - Playwright: 5, 7, 4 and 3 passed.
  - Before that, the pinned `pgsty/mc` image was missing (`make setup-stack-images` pulled it), and the worktree had no local app keys (`make setup-keys` generated them).
- **Live SC-OPS-09 check:** api and worker restarted from a shell with four dummy `AWS_*` values. Each had 127 variable names and no `AWS_*` name.
- **`make stop`:** exit 0. Output: `stop-all 1/3: draining api worker`; SIGTERM api (up to 120 s) and worker (up to 180 s); `stopped worker after 0s`; `stopped api after 1s` (the local prestop); `2/3: admin business customer site`; `3/3: mocks`; Caddy and the containers stopped. The api log shows draining, in-flight wait, drained, loops closed, pool closed, telemetry flushed.

## Deviations

- `mk/20-backend.mk`: migrate and seed use `tz_load_app_env`.
- "succeeded" in SC-GS-04 is River's `completed`. A hard-cancelled job ends `available`, ready to retry.
- SC-GS-04's 20 s job runs on test-controlled time.

## Review fixes (2026-10-23)

A fresh review found six issues and five nits; all are fixed in five commits (`bound the pool close so telemetry always flushes`, `hard-cancel counter starts at 0; escalate on any Stop error`, `a second SIGTERM or Ctrl-C exits at once`, `proc-test section 3 waits on a fresh order log`, `backend/CLAUDE.md: bounded pool close, second signal, ~/.aws`).

- **Pool close is bounded** (`lifecycle.StepWithin`, `PoolClose` 5 s) on every shutdown path, so a stuck pool cannot stop the telemetry flush (`TestStuckPoolStillFlushesTelemetry`, `TestStepWithin`).
- **Exit budgets** now count the pool close and flush plus a 5 s margin: `PreStop + Drain + APIExit` (15 s) stays under 120 s, so `SHUTDOWN_PRESTOP + SHUTDOWN_DRAIN` may total at most 105 s; worker 90 + 30 + health + pool + flush + margin = 140 s < 180 s.
- **`tz_job_hard_cancelled_total`** starts at 0 for every registered kind.
- **`queue.Drain`** escalates to StopAndCancel on any `Stop` error.
- **A second SIGTERM or Ctrl-C exits at once** (`notifyOnce` in `main.go`; `TestSecondSignalExits`, negative control checked).
- **`TestSIGTERMDrainsInFlightRequest`** has its own 3 s pre-stop and a 6 s handler.
- **Nits:** `Shutdown` also runs when `Serve` fails early; "telemetry flushed" is logged only on success; durations are logged as strings; `backend/CLAUDE.md` notes that the `AWS_*` strip does not stop the SDK reading `~/.aws` (W3-15's explicit IMDS provider covers it).

**Evidence.**
- The agent ran `-race` on the five packages, the timing tests with `-count=5` (all pass), proc-test 3 times, golangci-lint (0 issues) and gosec (no findings).
- After rebasing onto 5426967, the lead reran `go test -race -count=1` on lifecycle, health, config, queue and cmd/todayz (all ok) and `proc-test.sh` (three sections ok).

**Untested:** a River `Stop` error other than a deadline, and a `Serve` failure with connections still open.

## Decisions on the open questions

- **Worker grace values:** stay constants (90 s, 30 s). No env overrides and no worker pre-stop.
- **Second signal:** a second SIGTERM forces an immediate exit (done).
- **Timing bounds:** each above 0 and at most 1 h. Pre-stop plus drain plus the API exit budget must fit under 120 s.
- **Shutdown log durations:** now logged as strings.

## Still open

- proc.sh's stop limits duplicate the Go constants.
- Could `tz_job_hard_cancelled_total` appear in Grafana as `..._total_total`? Check this when the W3 dashboards are built.
