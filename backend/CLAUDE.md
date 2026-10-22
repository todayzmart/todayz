# backend/

Go API and workers: one module, one binary (`todayz api|worker|migrate|seed|ai|admin`). chi, pgx v5 + sqlc, goose, River, PostGIS.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §2–3, §9–10; 03-data-model.md; 04-api.md; 05-observability.md; 11-ai-features.md.

## Run

- `make dev` starts Postgres/PostGIS (host port 5433), MinIO, LGTM, mocks and Caddy; the API is at https://api.todayz.localhost
- `make reset` recreates the DB, migrates and seeds; `make gen` regenerates `gen/` and the bundled `api/openapi.yaml`
- `cd backend && go test ./...` (integration tests use testcontainers `postgis/postgis:16-3.4`)

## Rules for this folder

- Edit only your own domain's files in `api/paths|schemas|problems/`, `db/queries/`, `internal/jobs/<domain>.go`, `internal/seed/<domain>.go`, `platform/authz/policy/<domain>.go`.
- Change the database only through a new migration `db/migrations/NNNNN_<wp-id>_<what>.sql`.
- Domain packages depend on `platform`, never on each other's internals; cross-domain calls go through a small interface in the caller.
- Read every configurable number from `platform_settings` (01 §17); never hard-code it.
- Never log phones, OTPs, order codes, tokens, addresses or key values; the slog redaction handler is a backstop, not a licence.
- Do not commit `gen/` or `api/openapi.yaml`.
- `cmd/todayz/routes.go` belongs to the lead; expose `Register(r chi.Router, deps)` from your domain.

## Status

Stub from W0-01. The WP that builds this folder fills in layout and conventions.
