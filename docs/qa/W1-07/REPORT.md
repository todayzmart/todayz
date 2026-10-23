# W1-07 Observability base: QA report

Branch `worktree-agent-ae3dd71055361fabe`. Everything ran locally; nothing touched a cloud account and no secrets were printed. The live stack (`make dev`) was not started, so nothing below was seen in Grafana.

## What was built

- **RED metrics:** middleware with route templates (never raw paths) and a label allow-list (`AllowedLabels`, `TestRegisteredMetricsUseAllowedLabels`). Metrics are prefixed `tz_`.
- **Runtime, pool, queue, SSE metrics:** Go runtime, pgx pool (including wait time), River queue depth and oldest-available-job age per queue (`RegisterQueue`, `QueueOldestAge` for the worker's later CloudWatch publisher, W5-04), and SSE hooks (open, published, reconnects, delivery lag). A pgx tracer and server spans are wired in.
- **Events:** partitioned `events` table (migration 10701, `partition_policies` row, daily partitions kept by the existing maintenance job), the catalogue loader `eventcat` and the generator `eventgen` (`mk/26-events.mk`) that turn `shared/telemetry/events/*.yaml` into Go and TypeScript.
- **Endpoints (anonymous):**
  - `POST /v1/rum`: vitals to histograms, errors logged scrubbed with a fingerprint, 1 in 100 raw vitals logged.
  - `POST /v1/events`: up to 50 events per call. An event that is unknown, server-emitted, has invalid props, falls outside the time window or repeats an id is dropped and counted in `tz_events_dropped_total{reason}`. The call still returns 202 with `{accepted, dropped}`. Only a malformed envelope (bad JSON, more than 50 events, a missing required field) is 400 or 422.
  - `GET /v1/test/slow?ms=` and `/v1/test/slow-sql?ms=` behind the same gate as `/v1/test/clock`.
  - The principal comes through `Deps.UserID`, nil until W1-01 (`TODO(W1-01)`).
- **`shared/telemetry`:** web-vitals, long tasks, route timing, traced fetch (`traceparent`), a Sentry seam (injected SDK, staging and production with a DSN only), and a typed event emitter generated from the YAML. Nothing is sent from local or test.
- **Grafana:** dashboards 01 to 04 in `infra/grafana/dashboards/`, plus `TestDashboardsUseRegisteredMetrics`.

## Evidence

| Check | Result |
|---|---|
| `go vet ./...` (backend) | clean |
| `golangci-lint run ./...` (backend) | 0 issues |
| `go test -race` over `internal/telemetry`, `internal/platform/telemetry`, `internal/platform/httpx/...`, `cmd/...` (with Docker) | all ok; `cmd/todayz` includes `TestContractSmoke` with `postEvents` back in the smoke |
| Earlier full run: `go test -race` over `internal/platform/...`, `internal/telemetry`, `cmd/...`, `internal/devtools/...` | all ok |
| `pnpm --dir shared/telemetry run typecheck`, `lint`, `test` | clean; 49 tests passed (16 new) |
| `make check` (before the events change) | passed on the third run |

New tests for events: a mixed batch (2 stored, 4 dropped: unknown name, bad enum, too old, repeated id), a fully unknown batch for each drop case (all dropped, nothing stored), a retried batch (0 stored), and bad JSON (400). The 422 for a missing envelope field or more than 50 events comes from the request validator, which the telemetry test rig does not include; the contract smoke covers it.

**Flakiness.** Two earlier `make check` runs failed in the backend step with testkit "ready" timeouts and "connection refused" while the shared Colima VM was busy. They passed on rerun and in isolation; no code change was made for them.

## Open items

- **Smoke exclusions** in `e2e/contract/run.sh`: only `getTestSlow` and `getTestSlowSql` (they sleep up to 30 s by design).
- **`/v1/client-errors` is not built.** Frontend errors travel in the RUM batch.
- **Events retention (400 days)** is W1-11's job, and its setting key is not seeded. The `events` table has no append-only trigger, so erasure flows keep working.
- **Identity labels** (`role`, `user_kind`, `store_id`) stay anonymous until W1-01 calls `NotePrincipal` and supplies `UserID`.
- **No rate limit** on `/v1/rum` and `/v1/events` until W1-01's slot 12.
- **Pool wait is a mean** (total wait over acquires), not a histogram.
- **Not sent by the client:** online/offline transitions and API-timing samples, because the contract has no field for them.
- **Events time window** (30 days back, 1 hour ahead) is a named code constant in `internal/telemetry`, commented as not a product rule (01 §17 does not list it).
- **`tz_events_dropped_total`** has no dashboard panel yet; `reason` is in the label allow-list with the fixed set `unknown_event`, `server_event`, `invalid_props`, `out_of_window`, `duplicate_id`, `invalid_id`.

## Not verified

- The dashboards in a live Grafana, and native-histogram PromQL against the real Prometheus.
- Dashboard 03's postgres_exporter metric names and `pg_stat_statements`, which may not be enabled locally.
- CORS for the site app on `/v1/rum` and `/v1/events`.
- The Sentry seam against a real SDK, and the browser client in a real browser (jsdom only).
- No new datasource or local otel config file was added; the dashboards use the uids `prometheus`, `loki`, `tempo` (provisioned by the otel-lgtm image) and `todayz-postgres`.

## Outside owned paths

`backend/cmd/todayz/app.go`, `routes.go`, `api.go`, `worker.go`; `backend/internal/platform/db/db.go` (`Options.Tracer`); `backend/internal/platform/httpx/router.go`; `backend/internal/platform/httpx/problem/problem.go` (`NoteErrCode`); `backend/internal/platform/httpx/mw/accesslog.go` (lint fix); `backend/internal/platform/telemetry/` (metrics and labels, the platform package this WP owns the observability slice of); `e2e/contract/run.sh`; status notes in `backend/CLAUDE.md`, `shared/CLAUDE.md`, `infra/CLAUDE.md`; `.gitignore`; `shared/telemetry/eslint.config.mjs`.
