# backend/

Go API and workers: one module, one binary (`todayz api|worker|migrate|seed|rollup|ai|images|version`).
chi, pgx v5 + sqlc, goose, River, PostGIS. Every domain package is built; the package map is in
`docs/guide/code-structure.md` and the human entry point is `backend/README.md`.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then
02-architecture.md §2–3, §9–10; 03-data-model.md; 04-api.md; 05-observability.md; 11-ai-features.md.

## Run

- `make dev` starts the local stack, migrates, then runs the API on :8180
  (https://api.todayz.localhost through Caddy) and the worker on :8181 (health only) under
  `infra/local/proc.sh`. Postgres/PostGIS is on host port 5433.
- `make reset` recreates the DB, migrates (as `todayz_owner`) and seeds `dev` (as `todayz_app`).
- `make gen` writes `gen/sqlc/<domain>db` (sqlc, one package per domain), `gen/problems`
  (problem codes from `api/problems/`), and the contract (`mk/25-contract.mk`): `gen/api/root.yaml`
  (the domain files joined), the bundle `api/openapi.yaml`, `gen/api/spec` (the bundle embedded for
  the request validator) and `gen/api/<domain>/api.gen.go` (oapi-codegen types + strict chi server
  per domain tag). `api/README.md` says how a domain adds a path; never edit the bundle.
- `make check` (backend part, `mk/20-backend.mk`): gofmt, go vet, golangci-lint, `go test -race`,
  gosec, govulncheck against the offline DB in `.cache/govulndb`, and the settings lint. It runs
  offline under `scripts/no-egress.sh` once `make setup` has run online. The contract part
  (`mk/25-contract.mk`): `redocly lint`, `apigen check` (SC-PL-01), the api-client checks, and the
  Schemathesis smoke (`TestContractSmoke`: examples + coverage phases against an in-process api on
  a fresh database, every error a registered problem, SC-PL-02).
- Quick loop: `cd backend && go test -short ./...` skips the database tests; without `-short`
  they need Docker (colima). `make check` sets `DOCKER_HOST` and the Ryuk socket override for you.

Environment (names only; values live in `.env.local`): `APP_ENV` (local|test|staging|prod),
`DATABASE_URL` (todayz_app), `DATABASE_MIGRATE_URL` (todayz_owner), `API_ADDR` (default
127.0.0.1:8180 locally, :8080 deployed), `WORKER_ADDR` (127.0.0.1:8181 / :8081), `API_PUBLIC_URL`,
`LOG_LEVEL`, `TODAYZ_NO_EGRESS`, `ADMIN_TOTP_OFF` (local and test only, on loopback: admins sign in
without the authenticator step, D-244), `SHUTDOWN_PRESTOP` and `SHUTDOWN_DRAIN` (optional Go durations,
see Shutdown below), `PII_ENC_KEY` (32 bytes, required: it seals stored idempotent
response bodies; `make setup` generates it), plus the provider keys in `internal/platform/config`.
Secret values are `config.Secret` (prints as `[redacted]`). Config logs the names of unset
variables, never a value.

## Layout (02 §2.2)

```
cmd/todayz/            main, api, worker, migrate/seed commands; routes.go (lead-owned) and
                       routes_<domain>.go (one per domain, D-50)
db/migrations/         goose SQL (see db/migrations/README.md: roles, numbering, version 3 is River)
db/queries/<domain>/   sqlc queries, one folder per domain → package gen/sqlc/<domain>db
db/sqlc.yaml           sqlc reads the Up sections of the migrations as the schema
internal/platform/     shared plumbing, no domain logic:
  audit awssms buildinfo clock config db dbenv egress health httpx(+mw, problem, route) idempotency
  ids migrate money pgnotify queue reqctx settings telemetry testclock
internal/jobs/         River registry: jobs.go + one <domain>.go per domain (24)
internal/seed/         seed registry: seed.go + one <domain>.go per domain (24)
internal/testkit/      integration-test database (testcontainers PostGIS)
internal/devtools/     apigen, eventgen, problemgen, settingslint, vulngate (build/check tools, not shipped)
tools/                 separate go.mod pinning the Go tools (sqlc, goose, golangci-lint, gosec, govulncheck)
gen/                   generated, git-ignored
```

Domain packages (`internal/<domain>/`, from W1 on) depend on `internal/platform/...` only, never
on each other's internals; a cross-domain call goes through a small interface in the caller.

## Rules for this folder

- Edit only your own domain's files in `api/paths|schemas|problems/`, `db/queries/<domain>/`,
  your entry in `db/sqlc.yaml`, `cmd/todayz/routes_<domain>.go`, `internal/jobs/<domain>.go`,
  `internal/seed/<domain>.go`, `platform/authz/policy/<domain>.go`.
- Change the database only through a new migration `db/migrations/NNNNN_<wp-id>_<what>.sql`,
  numbered inside your WP's block (D-89: `<wave><wp:2><seq:2>`, so W1-02 uses 10201–10299;
  `TestMigrationFileNames` checks it). Read `db/migrations/README.md` first. goose applies
  out of order (D-90), so a database that already holds a later block still takes an earlier one.
- A partitioned table gets a `partition_policies` row in its own migration; nothing in Go lists
  partitioned tables.
- Read every configurable number from `platform_settings` (01 §17) through `settings.Cache`;
  never hard-code it. The settings lint (`internal/devtools/settingslint`, SC-PL-06) fails on an
  integer literal equal to a distinctive seeded default outside platform/devtools/testkit/seed and
  tests. A literal that only happens to match takes `//settings:ok <reason>` on the same line.
- Never put a key, token or secret in `platform_settings` (D-19); keys come from the environment.
- Never log phones, OTPs, order codes, tokens, addresses or key values; the slog redaction handler
  is a backstop, not a licence. Log through the request's logger (`telemetry.Logger(ctx)`);
  `slog.Default` and the package-level `slog.Info`/`Error`… are forbidden by forbidigo (the
  binary sets the redacting logger as the default anyway).
- Time comes from the injected `clock.Clock`, never `time.Now` (golangci forbidigo). In local and
  test the clock is the test clock (D-80): `GET/POST /v1/test/clock`, mounted only when `APP_ENV`
  is local or test, `API_ADDR` is a loopback address and the database's `platform_environment`
  row agrees (`internal/platform/dbenv`). POST needs `Content-Type: application/json` (else 415);
  `advance` adds atomically in the database; `wait` is capped at 50 s.
- `platform_environment` (one row) names the database's environment: `todayz migrate up` writes
  it and refuses on a mismatch; `migrate down`, `seed` and the test clock refuse unless it is
  local/test and matches `APP_ENV`.
- Outbound HTTP only through `httpx.NewOutboundClient` (egress guard; refused under
  `TODAYZ_NO_EGRESS`). `http.DefaultClient`, `http.Get/Post` and a bare `http.Client{}` are refused
  by forbidigo and the `make check` grep. Logs only through slog (`telemetry.NewLogger`).
- Idempotency is opt-in per route (04 §1.6): register an unsafe route with
  `route.Handle(route.Policy{Idempotency: route.IdempotencyRequired}, h)` (or `Optional`); a route
  without it ignores `Idempotency-Key`. The request hash is over the raw body bytes (method, route,
  body): a client retrying must resend the same bytes, not re-serialised JSON. A response with
  `Cache-Control: no-store` is never stored; stored bodies are AES-GCM sealed under `PII_ENC_KEY`.
  Claims are fenced by a claim id with a 3-minute lease.
- `/version` is on the API only in local and test; in staging and prod it is on the worker's
  internal health port. `/readyz` reports only `ok`/`fail` per check, no error text.
- Errors are problem+json with a code from `api/problems/` (`problem.New(code, …)`); the generated
  registry (`gen/problems`) is the list. Idempotency mismatch is 422; a request already in flight
  is 409 `conflict`.
- The request validator (slot 13, `httpx/openapi`) runs on every mounted route after chi has
  picked it (D-100: one router decides; it finds the operation by method and chi's route
  pattern, so an unmounted path or method is chi's 404/405 with `Allow` listing mounted methods
  only, everywhere). It answers 422 `validation_failed` with one `fields` entry per problem, keyed
  by the full prefixed path (`body.items[2].quantity: required`, `query.limit`, `path.orderId`;
  reasons never repeat the client's value; at most 50 entries, then `fieldsTruncated: true`), 415
  for the wrong `Content-Type`, 400 `bad_request` for a body that is not JSON, has trailing data,
  more than 10,000 values or an array past its `maxItems`, 413 past the body limit. Slot 8
  (`pathguard`) answers 400 first for a raw path two routers could read differently (`%2F`,
  `%5C`, a non-canonical escape). Authn runs before validation (an unauthenticated `/v1/me`,
  `/v1/biz`, `/v1/admin` call is 401, never a field name) and idempotency after it (a refused
  request is never stored). A JSON body is buffered (at most the 1 MB limit), pre-scanned for
  the 10,000-value budget and `maxItems` (also under `oneOf`/`anyOf`), then validated. A request
  body is `application/json` only, unless the operation is `x-upload: true`, and then it may
  not be JSON at all; `openapi.New` (and `apigen check`) refuse any other contract. An upload
  body is checked for media type and size only and streamed to the handler. Route policies come
  from the operation's `x-idempotency` (`none|optional|required`) and `x-upload`, merged with
  what the code declares: the stronger rule wins, code weaker than the contract fails startup
  and `TestRoutesAreInTheContract`, and `Upload: true` in code on an operation the contract does
  not mark `x-upload` fails startup too (only the contract grants an upload). A mounted pattern
  may not end in `/` (`Check()` refuses it, as apigen refuses the path). That
  test also fails on a chi route the contract does not know and, in reverse, on a contract
  operation without a route once its domain has landed (the allowlist is in the test). Every
  generated `Register` builds its strict handler with `openapi.StrictOptions()`.
- Database: the API, worker and seed connect as `todayz_app` (DML only); only `todayz migrate`
  uses `todayz_owner`. River runs as `todayz_app`.
- Do not commit `gen/` or `api/openapi.yaml`.
- `cmd/todayz/routes.go` belongs to the lead and calls `mountX(r, &d)` once per domain; your WP
  fills `mountX` in `cmd/todayz/routes_<domain>.go` (call your domain's `Register(r, deps)`) and
  edits no other routes file (`TestOneMountPerDomain`).
- Shutdown (02 §2.8, D-98; `internal/platform/lifecycle`): on SIGTERM `/readyz` turns 503
  `{"status":"draining"}` at once (`/healthz` stays 200). The API keeps serving for
  `SHUTDOWN_PRESTOP` (15 s deployed, 1 s locally), then `srv.Shutdown` waits up to
  `SHUTDOWN_DRAIN` (75 s, above the 60 s `WriteTimeout`; the API refuses to start otherwise), then
  the LISTEN/NOTIFY loop stops, the pool closes (given up after 5 s, logged at error level, since a
  cut request or a job that ignored its cancel can hold a connection forever) and telemetry
  flushes (5 s). A second SIGTERM or Ctrl-C exits at once. Request contexts never
  derive from the signal (`BaseContext` is a background context), so never pass the signal context
  into a handler. Container stop timeouts: api 120 s, worker 180 s; `lifecycle.APIExit` and
  `WorkerExit` are what each process still needs after its drain.
- `infra/local/proc.sh` strips every `AWS_*` variable from the api and worker (D-97), but that does
  not stop the AWS SDK's default credential chain from reading `~/.aws`; W3-15's explicit
  instance-role (IMDS) provider, never the default chain, is what covers that.
- Workers: start River with `queue.Start` (detached from the signal context); the worker stops it
  with `queue.Drain(c, queue.DrainGrace, queue.HardGrace, log)`: running jobs get 90 s to finish,
  then `StopAndCancel` gets 30 s, logged at error level and counted in
  `tz_job_hard_cancelled_total{kind}`; the attempt is retried, so every job must be idempotent.
- Middleware: one file per slot in `internal/platform/httpx/mw/` (order in `doc.go`); edit only
  your WP's slot.

## Tests

- Unit tests sit next to the code. Integration tests call `testkit.NewDB(t)` (fresh database
  copied from a migrated template; `db.Pool` is `todayz_app`, `db.OwnerPool` is `todayz_owner`)
  and the package has `func TestMain(m *testing.M) { testkit.Main(m) }`.
- The testkit image is `TODAYZ_POSTGIS_IMAGE` (`repo:tag` or `repo:tag@sha256:…`), defaulting to
  the pinned `postgis/postgis:16-3.4` (amd64) or `imresamu/postgis:16-3.4` (arm64) digests, the
  same as `infra/local/lib.sh`. `internal/testkit/database.sql` runs the same privilege statements
  as init.sql's `todayz_test` block (`TestMatchesLocalInitSQL`); test databases record
  `platform_environment = test`. Containers carry the label `in.todayz.testkit`; Ryuk is the backstop.
- Shared mode (opt-in, saves RAM when several checkouts test at once): `make testpg-up` starts one
  PostGIS (`todayz-testpg`, 127.0.0.1:5439, tmpfs, fsync off) and writes its URL to
  `.cache/testpg.url` (mode 600, never printed). With `TODAYZ_TEST_PG_URL_FILE` set to that file,
  testkit uses the server instead of a container per package: each test binary migrates its own
  template `tz_template_<tag>` (worktrees carry different migrations) and drops it on exit; every
  test still gets a fresh database. `make testpg-down` removes the server and every leftover
  database. `make check` does not set the variable, so it keeps the container per package.
  A package whose tests depend on cluster-wide transaction ids or snapshots uses
  `testkit.MainOwnServer(m)` (realtime does), which keeps a container of its own.
- Platform tests that guard the plan: `TestSettingsMatchProductRules` (01 §17 ↔ migrations, both
  ways), `TestNoSecretSettings` (D-19), `TestUpDownUp`, `TestRiverVersionIsLatest`, the problem
  code registry test, and the settings lint fixture.
- govulncheck: a called vulnerability in a go.mod module always fails, and so does one in the
  standard library (the go.mod `toolchain go1.26.10` line pins it, D-88). `make check
  GOVULN_STDLIB=warn` only prints the fixing Go version.

## Status

W0-03 built the skeleton: platform packages, the middleware slots (`httpx/mw/doc.go` lists them;
13–15 run after chi has routed, D-100), `/healthz`, `/readyz`,
`/version`, `/v1/test/clock` (local/test), the goose baseline (00001, 00002) and River (version 3),
sqlc, testkit, the jobs and seed registries for the 23 domains (24 since W2-14 added baskets), and the SC-PL-06 checks. W0-04
built `api/` (root, common, one `paths`/`schemas` file per domain, `api/README.md`), `apigen`
(root, check, codegen), the request validator (`httpx/openapi`) with spec-driven route policies,
`gen/api/spec` + per-domain oapi-codegen stubs, the Schemathesis smoke and fixture, and goose
out-of-order migrations (D-90). The platform paths (`/healthz`, `/readyz`, `/version`,
`/v1/test/clock`) are the only operations until the domain WPs land; `/v1/test/slow*` come with
W1-07 and the D-91 domain problem codes with their domains. (Historical: the domain packages
have all landed since; see `docs/guide/code-structure.md` for the current list.)

W1-07 built the observability base: RED middleware with a label allow-list, runtime, pool, queue
(`telemetry.RegisterQueue`, `QueueOldestAge`) and SSE metrics, the pgx tracer, the partitioned `events`
table (`partition_policies` row), the event catalogue (`shared/telemetry/events/*.yaml`, `eventcat`,
`eventgen`, `mk/26-events.mk`), `POST /v1/rum`, `POST /v1/events` (stores the signed-in caller's
user id; drops bad events and counts them in `tz_events_dropped_total`) and the gated `GET /v1/test/slow`, `/v1/test/slow-sql` (excluded from the contract smoke
because they sleep).

W3-08 built support (`internal/support`, migration 30801, D-226): business tickets on
`/v1/biz/businesses/{bid}/support/*` (`support.tickets`, owner and manager), customer tickets on
`/v1/me/support/*` (`me.support.tickets`, any signed-in customer, scoped to tickets they opened as a customer) and the
A-14 inbox on `/v1/admin/tickets*` (`admin.support.tickets`, policy golden `testdata/support.golden`).
The SLA due time reads `support_sla_<priority>_min` at the moment it is set, from `sla_started_at`;
a priority change (`POST /v1/admin/tickets/{ticketId}/priority`) moves it to `sla_started_at` plus
the new minutes; notes never leave the admin side; assign, priority, resolve and close write
`audit_log` rows; attachments go through `presign.go` into the
private bucket (MinIO locally), claimed once by their uploader's message. After the lead's review (D-226):
per-person limits from settings (`support_open_tickets_max`, `support_tickets_per_hour`,
`support_uploads_per_hour`) under a per-person advisory lock, a HEAD check of the object at claim, an agent's
upload tied to its ticket, the `support_uploads_sweep` maintenance job (`support_upload_ttl_hours`),
and the identity deletion hook (`retention.go`) that closes, blanks and deletes a deleted customer's
support data.

W3-15 built `internal/ops` (A-22, `GET /v1/admin/system`, policy `admin.system.view` for admin and
super_admin): Beanstalk, ALB, RDS and CloudWatch alarm panels read from AWS only when `APP_ENV` is
staging or prod, through an explicit EC2 instance-role (IMDSv2) provider, never the default chain; in
local and test there is no AWS adapter, so nothing calls AWS whatever `AWS_*` is set. AWS panels are
cached 60 s per instance (failures too), database (`pg_stat_statements` top 10, DML only, guarded when
the extension is absent) and River panels 5 s. A failing panel carries an error class; the response
stays 200. `job_kind` and `job_run` are audit subjects for the jobs actions.

W3-13 built the admin AI assistants (D-233, D-234): `internal/ai/catalogue` (AI-A1, A-08),
`internal/ai/triage` (AI-A2, A-14) and `internal/ai/riskexplain` (AI-A3, A-13) on the shared drafts
envelope under `/v1/admin/ai/*/drafts`, with `internal/ai/assist` for the admin caller and draft
ownership (another admin's draft is the common 404). Each route takes its screen's own policy and is
the common 404 while its `ai_*_enabled` flag is off. A confirmed line goes through the domain's own
writer: `internal/admin/assist.go` (A-08 edit, merge, category names) and `internal/support/assist.go`
(A-14 triage and reply); the risk summary is an audit entry only (`risk.flag_summary`). Catalogue lines
need their A-08 button's permission, else `not_permitted`. Recorded evals: `catalogueeval` (SC-AI-57),
`triageeval` (SC-AI-60), `riskeval` (SC-AI-63). Report: `docs/qa/W3-13/REPORT.md`.

W3-12 built consumer AI (D-231, D-235): `internal/ai/listparse` (AI-C1 drafts on `/v1/me/ai/list-parse/drafts*`; confirm answers lines and writes nothing, BSK-15), `internal/ai/querynorm` (the deterministic refine-chip parser `chips.go` that search reads through `search.ChipReader`, and AI-C2 search assist on `POST /v1/me/ai/search-assist`: 3 s, 204 on any failure, stores nothing), `internal/ai/voicesearch` (AI-C5 on `POST /v1/me/ai/voice-search`: W3-04's `voice.ReadRecording`, keeps no audio or words, one call per use against `ai_daily_calls_customer`) and `GET /v1/me/ai/features` (`me.ai.use`). Search reads chips unless `parse=false` and sets `refineHint` (`internal/search/refine.go`). AI-C3 substitutes are rule-based in `internal/search/substitutes.go` (`GET /v1/discover/stores/{slug}/substitutes`, no model, not logged). Recorded evals: `listparseeval` (200), `querynormeval` (100), `voicesearcheval` (100), run in `make check`.
