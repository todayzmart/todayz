# backend

The TodayZ Go API and background worker: one module, one binary called `todayz`.

> As built on main at c8876a4c (27 Oct 2026). Update this file in the same change as the code it describes.

Stack: Go, chi (router), pgx v5 and sqlc (database), goose (migrations), River (jobs, stored in Postgres), PostGIS (maps and search), oapi-codegen and kin-openapi (contract). The folder's rules for contributors are in [CLAUDE.md](CLAUDE.md). The system as a whole is in [docs/guide/architecture.md](../docs/guide/architecture.md).

## Run it

Everything goes through `make` from the repo root.

| Command | What it does |
|---|---|
| `make setup` | Installs the Go tools and test images; makes missing app keys in `.env.local` (prints names only) |
| `make dev` | Starts the local stack, runs migrations, then the API on :8180 and the worker on :8181 (health only) |
| `make reset` | Recreates the database, migrates it as `todayz_owner`, then seeds `dev` as `todayz_app` |
| `make gen` | Bundles the OpenAPI contract and generates Go and TypeScript code (nothing generated is committed) |
| `make check` | gofmt, vet, golangci-lint, `go test -race`, gosec, govulncheck, settings lint, contract checks. Runs offline |
| `make env-check` | Prints env variable names with set or missing, never values |

The API is at `https://api.todayz.localhost` through Caddy. Postgres is on host port 5433. The full port table is in the root [CLAUDE.md](../CLAUDE.md), and the stack is explained in [docs/guide/local-dev.md](../docs/guide/local-dev.md).

Quick Go loop: `cd backend && go test -short ./...` skips the database tests. Without `-short` they need Docker (Colima).

## Environment

Names only. Non-secret local defaults are committed in [infra/local/local.env](../infra/local/local.env). Secrets live in `.env.local`, which is never printed or committed. Run `make env-check` to see which names are set.

| Name | Purpose |
|---|---|
| `APP_ENV` | `local`, `test`, `staging` or `prod`; the database must agree (`platform_environment`) |
| `DATABASE_URL` | Runtime connection, role `todayz_app` (data changes only) |
| `DATABASE_MIGRATE_URL` | Migration connection, role `todayz_owner` |
| `API_ADDR`, `WORKER_ADDR` | Listen addresses |
| `API_PUBLIC_URL` | The API's public base URL |
| `PII_ENC_KEY` | Seals personal data and stored idempotent responses |
| `ORDER_CODE_ENC_KEY`, `ORDER_CODE_HMAC_KEY` | Pickup code sealing and matching |
| `SMS_API_URL`, `WHATSAPP_API_URL`, `PUSH_API_URL`, `TURNSTILE_API_URL`, `STT_API_URL`, `LLM_API_URL` | Provider base URLs; locally they point to the mocks server |
| `R2_ENDPOINT`, `R2_BUCKET_IMAGES`, `R2_BUCKET_PRIVATE` | Object storage (R2 deployed, MinIO local) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Telemetry export |
| `SHUTDOWN_PRESTOP`, `SHUTDOWN_DRAIN` | Drain timings |
| `TODAYZ_NO_EGRESS` | Refuses outbound traffic (set by `make check`) |

The complete list, with required and optional markers, is in `backend/internal/platform/config`.

## The binary

`todayz <command>` (see `backend/cmd/todayz/main.go`):

| Command | What it does |
|---|---|
| `api` | Serves the HTTP API |
| `worker` | Runs the River worker and the cron schedules |
| `migrate up\|down\|status` | Runs schema migrations with `DATABASE_MIGRATE_URL`; `down` is local and test only |
| `seed dev\|demo\|test` | Loads seed data with `DATABASE_URL`; local and test only |
| `rollup --from D [--to D]` | Recomputes the analytics rollups for IST days |
| `rollup demo [--for 10m]` | Synthetic traffic against the local API, used by `make demo` |
| `ai eval <feature\|all> [--live]` | Runs AI evals on recorded replies; `--live` calls the model with your own key (`make ai-eval`) |
| `images import [--dry-run] [--limit N] [--source off\|obf\|opf] [--newest] [--report FILE]` | Drafts product images from Open Food, Beauty and Products Facts; `--newest` tries recent products first for the local demo |
| `demo catalogue [--source off|obf|opf] [--category-tag TAG | --search-terms WORDS] [--limit N] [--dry-run] [--from DIR]` | Imports mapped Open Facts products into local demo shops; local/test only and refused with `TODAYZ_NO_EGRESS` |
| `images types [--dry-run] [--again] [--limit N] [--contact URL] [--report FILE]` | Finds item-type photo candidates on Wikimedia Commons for A-08 (D-253) |
| `images types --picks FILE\|builtin [--dry-run] [--report FILE]` | Applies saved picks (`internal/imageimport/data/approved-type-photos.csv` is `builtin`) as A-08 would, audited as the system (D-265) |
| `admin unlock-sign-in --phone-file FILE\|- --reason TEXT` (or `--phone-env NAME`) | Break glass (D-264): lifts an admin's sign-in lock and clears its wrong-code count when no super admin can, as `todayz_app`; audited with the system actor; the number is read from a file, stdin or a variable and never printed. Prod: deploy RUNBOOK §8 |
| `version` | Prints build info |

Exit codes: 0 ok, 1 failed, 2 usage.

## Package map

| Path | What is in it |
|---|---|
| `cmd/todayz` | The binary, route mounting (`routes_<domain>.go`), worker, wiring seams |
| `api/` | The OpenAPI contract, split per domain ([api/README.md](api/README.md)) |
| `db/migrations` | goose SQL ([README](db/migrations/README.md)) |
| `db/queries/<domain>` | sqlc queries |
| `internal/<domain>` | One package per domain: orders, catalogue, credits, pos, and so on |
| `internal/platform` | Shared plumbing: router, middleware, auth, jobs, realtime, config |
| `internal/jobs` | Registry of job kinds, one file per domain |
| `internal/seed` | Seed registry |
| `gen/` | Generated code, git-ignored |

The full table of every package, with main files, is in [docs/guide/code-structure.md](../docs/guide/code-structure.md#3-backendinternal-every-package). User journeys with their endpoints are in [docs/guide/flows.md](../docs/guide/flows.md).

## Add an endpoint, end to end

Example: a new domain operation `GET /v1/me/things`. Change only your own domain's files (the list is in [CLAUDE.md](CLAUDE.md)). The steps are in this order because each one feeds the next: sqlc reads the migrations as its schema, and the generated server interface tells you what the handler must implement.

1. **Contract.** Add the path to `api/paths/<domain>.yaml` and any schemas to `api/schemas/<domain>.yaml`. Give it an `operationId`, the `<domain>` tag, an example for every body, `maxLength` on strings and `maxItems` on arrays, and `security: []` if it is public. Add new error codes to `api/problems/<domain>.yaml`. Rules and examples: [api/README.md](api/README.md).
2. **Migration, if the schema changes.** Add a new file `db/migrations/NNNNN_<wp-id>_<what>.sql` in your block (D-89). Never edit an applied migration. Read [db/migrations/README.md](db/migrations/README.md) first (roles, numbering, partitions).
3. **Query.** Add the SQL to `db/queries/<domain>/` with a sqlc name annotation.
4. **Generate.** Run `make gen`. This bundles the contract, writes `gen/api/<domain>` (types and strict server interface) and `gen/sqlc/<domain>db`, and rebuilds `shared/api-client`. Commit only your sources.
5. **Service and handler.** Add the method to the domain's `Service` and implement the generated interface in `handlers.go`. Use `s.q(ctx)` for queries inside the transaction. Read configurable numbers from `platform_settings` through `settings.Cache`, never a literal. Take time from the injected clock. Write audit, outbox and job rows in the same transaction.
6. **Permission.** Add the operation to `internal/platform/authz/policy/<domain>.go`. An operation without a policy fails closed. Update the golden table.
7. **Mount.** The domain's `Register(r, svc, az)` is called from `cmd/todayz/routes_<domain>.go`. Mark unsafe routes with an idempotency policy where retries matter (the contract's `x-idempotency` sets the floor).
8. **Jobs and seed, if needed.** Declare job kinds in `internal/jobs/<domain>.go` and add a runbook. Add seed rows to `internal/seed/<domain>.go`.
9. **Tests.** Add unit tests beside the code and an HTTP test in `cmd/todayz/<domain>_http_test.go` using `internal/testkit`. Run `make check`. `TestRoutesAreInTheContract` fails if a route and the contract disagree.
10. **Docs.** If the change alters a flow or a package, update [docs/guide/flows.md](../docs/guide/flows.md) or [docs/guide/code-structure.md](../docs/guide/code-structure.md) in the same change.

## Testing

Unit tests sit next to the code. Integration tests use a real PostGIS database through `internal/testkit`. HTTP tests in `cmd/todayz` run the full middleware pipeline. The contract is checked with Redocly lint, `apigen check` and a Schemathesis smoke run. Order timers are tested with the test clock, never real waiting. The whole approach, and how to run each layer, is in [docs/guide/testing.md](../docs/guide/testing.md).

## Rules

Read [CLAUDE.md](CLAUDE.md) before changing anything here. The main ones:

- Generated code (`gen/`, `api/openapi.yaml`) is never committed.
- Database changes only through new migration files.
- Never hard-code a number that `01-product-rules.md` marks configurable.
- Never log phones, OTPs, order codes, tokens, addresses or keys.
- Outbound HTTP only through `httpx.NewOutboundClient`; time only from the injected clock.
- AI output is always a draft a person confirms.
