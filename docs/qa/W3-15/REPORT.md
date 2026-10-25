# W3-15 Ops console: QA report

A-21 Jobs and A-22 System health in the admin console, and `internal/ops` behind `GET /v1/admin/system`. Branch `claude/w3-15-ops-9027b5`, with `main` 6a2c248 merged in. Everything ran locally; nothing called AWS or any other cloud service.

## What was built

- **A-22 API (`internal/ops`, `GET /v1/admin/system`):**
  - Policy `admin.system.view` (new `ops` domain and golden) allows admin and super_admin only.
  - **AWS panels:** Beanstalk environments with their last deploys, the ALB (requests per minute, 5xx rate and p95 over the last complete 5 minutes), RDS instances, and CloudWatch alarm states.
    - The AWS client is built only when `APP_ENV` is `staging` or `prod`. It uses an explicit EC2 instance-role (IMDSv2) provider, never the SDK default chain.
    - In `local` and `test` there is no AWS adapter, so the panels say `not_configured` whatever `AWS_*` variables are set.
    - The panels are cached 60 s per API instance, one fetch at a time. A failed fetch is cached too, so a broken AWS is not asked on every request.
  - **Database panel:**
    - Connections in use and allowed, and the database size.
    - Multi-AZ: off locally (one server), stated as a fact. On AWS it is the RDS instance's answer when there is exactly one instance.
    - The top 10 statements by total time from `pg_stat_statements`, guarded when the extension is missing, not preloaded or unreadable.
  - **River panel:** jobs running, plus the depth and oldest waiting age per queue.
  - The database and River panels are cached at most 5 s.
  - A failing panel carries `status: error` and an error class (an AWS error code, Timeout, Credentials or Error). The other panels still render and the response stays 200.
  - The contract is `backend/api/paths/admin-system.yaml` and `schemas/admin-system.yaml`.
- **SC-OPS-02:** the DB test forces a slow statement through the existing `/v1/test/slow-sql?ms=500` (from `internal/telemetry`, registered in the test), then reads it back from the top-10 panel.
- **A-21 Jobs (`admin/src/jobs/`, routes `admin/src/routes/jobs/`).** Built on W1-11's existing admin-jobs API, with no backend change to it.
  - **List:**
    - The status badge (OK, Failing, Missed, Paused) sits next to the job name, so it shows without scrolling.
    - IST schedule text such as "Every day at 6:00 am" and "Every Monday at 9:00 am", the same shapes as the API's `scheduleText`.
    - "On events" for event kinds.
    - Next and last run, 7-day success rate, and average and longest duration.
    - Class and status filters live in the URL.
  - **Kind page:**
    - About this job: owner, schedule, limits and runbook.
    - Run history, filtered by status, trigger and date.
    - **Run detail:**
      - counts and the note;
      - the error with personal data removed by the API;
      - every attempt;
      - Grafana Explore links for logs and trace (±5 min).
  - **Actions:** Run now (with an optional period key), Pause, Resume and Retry.
    - Each takes a reason, sends an `Idempotency-Key` and is audited.
    - Two audit panels, "Changes to this job" and "Changes to this run".
  - **Who sees the buttons** (`gate.ts`):
    - `timer` kinds show none, only the note "Order timers can't be paused or run by hand."
    - `money` kinds show them to super_admin only. A 403 `step_up_required` opens the authenticator dialog and the action is retried. admin sees why there are no buttons.
    - Read-only roles see none: support, analyst, and finance (which sees `money` kinds only).
- **A-22 page (`admin/src/system/`):**
  - admin and super_admin only. The sidebar entry and its role rule were already there from W3-05.
  - One panel per area, each with its age ("45 s old").
  - Refresh button.
  - Grafana dashboard links.
  - Locally the four AWS panels read "Not configured (local)".
- **Audit:** `job_kind` and `job_run` are audit subjects (the `admin-audit.yaml` enum, `internal/admin/audit.go`), viewable by the roles that may act on a job (`AdminAuditView`).

## Evidence

All of these ran on the merged tree (`main` 6a2c248 merged in).

| Command | Result |
|---|---|
| `make gen` | exit 0 |
| `go test -race -count=1 ./internal/ops/...` (own Postgres container with `pg_stat_statements` preloaded; Colima's Docker socket) | ok. 16 tests: SC-OPS-01, 02, 03 (role gate), 04, 08 and 09 (no client off AWS even with `AWS_*` set; instance role only on AWS) |
| `go test -race` on `internal/admin`, `internal/platform/authz/...`, `internal/platform/jobs/adminapi`, `internal/platform/telemetry`, `cmd/todayz` (shared test Postgres via `TODAYZ_TEST_PG_URL_FILE`) | all ok. `adminapi` covers SC-OPS-07's API half: catalogue and field_agent get 403 on the jobs API |
| `golangci-lint run ./...` (backend) | 0 issues |
| `gosec ./internal/ops/...`, `gofmt -l` | quiet, clean |
| `infra/terraform/checks/check.py iam …/api-ops-readonly.json` and its 24 tests | ok (SC-OPS-05 is W1-10's check). Every AWS call in `internal/ops` is in that policy: `elasticbeanstalk:Describe*`, `cloudwatch:GetMetricData`, `cloudwatch:DescribeAlarms`, `rds:DescribeDBInstances` |
| admin `typecheck`, `lint`, `lint:css` | clean |
| admin `test` (vitest) | 26 files, 254 tests passed. W3-15 adds 34 (jobs) and 11 (system) |
| `pnpm -C shared/i18n exec vitest run` | 198 passed |
| admin `build:e2e` (what `make check` builds) | ok. Precache and dist checks pass |
| admin `size` | initial JS 166.7 kB gzip (budget 250 kB); CSS 9.7 kB |
| `playwright test e2e/jobs.spec.ts e2e/system.spec.ts e2e/roles.spec.ts` | 23 passed: SC-OPS-01, 03, 06 and 07 per role, plus AD-02 with the A-22 read gate |
| `playwright test` (whole admin suite) | 42 passed |

**SC-OPS coverage**

| Test | Where |
|---|---|
| SC-OPS-01 | `ops/db_test.go` TestLocalPanels; `system.test.tsx`; `e2e/system.spec.ts` (Not configured (local), Multi-AZ "Off" with no alert) |
| SC-OPS-02 | `ops/db_test.go` TestTopStatements: a slow query through `/v1/test/slow-sql`, then listed with calls and mean time, with no literal in the text |
| SC-OPS-03 | `ops/ops_test.go` TestRoleGate and `ops.golden`; `system.test.tsx`; `e2e/system.spec.ts` (sidebar and URL visit for 5 roles) |
| SC-OPS-04 | `ops/ops_test.go` TestAWSCache (one fetch within 60 s, a second at 61 s) and TestAWSCacheIsOneFetchAtATime; `db_test.go` TestRiverPanelAndCache (5 s) |
| SC-OPS-05 | W1-10's `infra/terraform/checks/check.py iam`, rerun here |
| SC-OPS-06 | `jobs.test.tsx`; `e2e/jobs.spec.ts` (badges, "Every day at 6:00 am", class and status filters narrow the list) |
| SC-OPS-07 | `jobs.test.tsx` (`jobGate` table, step-up retry); `e2e/jobs.spec.ts` (admin, super_admin, support, analyst, finance, timer); `e2e/roles.spec.ts` (catalogue and field_agent have no Jobs); `adminapi` tests (403) |
| SC-OPS-08 | `ops/ops_test.go` TestOnePanelFailing (CloudWatch AccessDenied gives an error class, the other panels stand, 200); `system.test.tsx` shows "(AccessDenied)" |
| SC-OPS-09 | `ops/ops_test.go` TestNoAWSClientOutsideStagingAndProd and TestDeployedClientUsesTheInstanceRoleOnly. The `proc.sh` half (no `AWS_*` in the API and worker environment) is W0-03b's `infra/local/test/proc-test.sh` and was not rerun here |

## Screenshots

In `docs/qa/W3-15/`, at desktop 1280×800, written by Playwright with `TZ_QA_DIR`:

- `admin-a21-jobs.png`: the list with OK, Failing, Missed and Paused.
- `admin-a21-kind.png`: a kind with a run open, Grafana links and both audit panels.
- `admin-a21-run-now.png`: the Run now dialog refusing a badly formatted period.
- `admin-a21-money-admin.png`: a money job seen by admin (the note, no buttons).
- `admin-a21-timer.png`: an order timer (the note, no buttons).
- `admin-a21-support-readonly.png`: support, read-only.
- `admin-a22-system.png`: A-22 locally.
- `admin-a22-no-access-support.png`: support visiting `/system`.

## Conservative readings (recorded, not asked)

1. **Grafana in production:** a production build without `VITE_GRAFANA_URL` leaves the Grafana links out. It does not point them at a guess.
2. **Statements shown:** only data and DML statements are listed. Postgres normalises their literals to `$n`; utility statements (SET, CREATE, …) are stored as written and could hold a value.
3. **Failed AWS fetches are cached 60 s** like good ones, so a broken permission is not retried on every page load.
4. **Deploy outcome:** read from Beanstalk's fixed event phrases ("Environment update completed successfully", "Failed to deploy application", …). Any other event is not counted as a deploy.
5. **Multi-AZ on AWS:** shown only when RDS answered and there is exactly one instance. Otherwise the row is left out rather than guessed.
6. **Job history panels:** visible to `AdminAuditView` (admin and super_admin), the roles that may act on a job. support, analyst and finance read A-21 without the history panels.

## Questions for the lead

1. **`VITE_GRAFANA_URL` for prod:** what is the Grafana URL? Without it A-21 shows "Grafana links are off in this build." and A-22 has no dashboard links.
2. **RDS role for top statements:** on RDS the API's database role needs `pg_read_all_stats` to see other roles' statements, and `pg_stat_statements` must be in the parameter group's `shared_preload_libraries`. Both are W1-10 or a migration owner's call. Without them the panel says why top statements are off.
3. **`DescribeEnvironmentResources` permissions:** Beanstalk may also require read permissions on the resources underneath (Auto Scaling, CloudFormation, ELB describe calls). If prod shows the Beanstalk panel as `AccessDenied`, W1-10's policy needs those reads. Not verifiable without AWS.
4. **Performance Insights is unused:** W1-10's policy grants `pi:GetResourceMetrics` and `pi:DescribeDimensionKeys`, but the top-10 panel reads `pg_stat_statements` instead. Should the policy keep the `pi:*` grant?
5. **Test import across packages:** `internal/ops`'s DB test imports `internal/telemetry` to mount `/v1/test/slow-sql` (test code only). Is that acceptable, or should the slow-SQL helper move to `testkit`?
6. **Job history for read-only roles:** should support, analyst and finance see the job and run audit panels? They are hidden for now (reading 6 above).

## Not verified

- Anything against real AWS: IMDS credentials, the real Beanstalk, ALB, RDS and CloudWatch answers, and the IAM policy in practice. The adapter is faked in unit tests.
- A-21 and A-22 against the live local stack (`make dev`). Per the brief, Playwright ran against the built bundle with the API faked, and the Go tests ran against test Postgres.
- The admin production `build` stops at the Turnstile guard: `VITE_TURNSTILE_SITE_KEY` must be a real key in production mode. This predates W3-15, and `make check` uses `build:e2e`, which passed.
- The `proc.sh` half of SC-OPS-09 (W0-03b's test) was not rerun.
- **Environment note:** `internal/ops`'s DB tests start their own Postgres container, because the shared server may lack `pg_stat_statements` in `shared_preload_libraries`. So they need Docker. Here that meant `DOCKER_HOST` pointing at Colima's socket.

## Files changed outside the owned paths

- **`admin/src/shell/nav.ts`:** `jobs` and `system` added to `BUILT_SECTIONS`; the entries were already there. **`admin/src/app/nav/access.ts`:** the action rules `jobAct` (admins) and `jobMoneyAct` (super_admin); the section rules were already there.
- **`admin/src/app/env.ts`:** `GRAFANA_URL`.
- **`admin/src/audit/AuditPanel.tsx`:** an optional `title` prop, so the kind page can name its two panels. Other screens are unchanged.
- **`admin/src/audit/labels.ts` and `shared/i18n/messages/admin/audit.en.json`:** labels for the `jobs.*` actions and the two subjects.
- **`admin/e2e/fake-api.ts`:** `localSystem`, the A-22 answer in `answerEmptyLists`. **`admin/e2e/roles.spec.ts`:** the READS entry for `/v1/admin/system`.
- **`backend/api/schemas/admin-audit.yaml`:** `job_kind` and `job_run` added to the subject enum. **`backend/internal/admin/audit.go` and `admin_test.go`:** their view policy.
- **`backend/cmd/todayz/routes_admin.go`:** mounts `internal/ops`.
- **`backend/internal/platform/authz/policy/ops.go` and `testdata/ops.golden`:** the new `ops` domain.
- **`backend/internal/testkit/testkit.go` and `scripts/testpg.sh`:** start Postgres with `shared_preload_libraries=pg_stat_statements`. Other packages are unaffected; the shared server needs a restart (`make testpg-up` after it is stopped) to pick this up.
- **`backend/go.mod`, `go.sum`:** the lead pinned the AWS SDK modules in 006d394 (D-227). `go mod tidy` then marked `service/cloudwatch`, `elasticbeanstalk` and `rds` as direct requirements. It also dropped `service/pi` v1.46.3, because nothing imports it: the top-10 panel reads `pg_stat_statements` (Question 4). If Performance Insights is wanted later, it comes back at the same pin. Versions are unchanged, and `go mod verify` is ok.
- **`admin/CLAUDE.md`, `backend/CLAUDE.md`:** W3-15 status lines.
