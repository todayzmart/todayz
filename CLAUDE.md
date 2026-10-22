# TodayZ

Indian hyperlocal retail: shops digitise their stock; customers search what is nearby, book it, and pick it up or get it delivered. Shops run their counter (POS), stock, offers and customers from the business app.

## Read first

1. `docs/plan/README.md`: the build plan, waves, roles, Definition of done
2. `docs/plan/01-product-rules.md`: every product rule and number (single source of truth)
3. `docs/plan/12-decision-log.md`: why the plan says what it says (D-01…D-60). If a numbered plan file disagrees with it, the numbered file is wrong: fix it and add an entry
4. The plan file for your area (architecture, data model, API, observability, screens, work packages, testing, infra, AI)
5. The `CLAUDE.md` in the folder you are changing

Design source of truth: https://claude.ai/artifact/W91M8EtFX8zJc9SgMUygZF (copy in `design/source/`). White page, mint wash, pastel tints, Lucide icons, Anek + Figtree, light theme only.

## Folder map

| Folder | What | Future repo |
|---|---|---|
| `backend/` | Go API + workers (chi, pgx/sqlc, River, PostGIS) | todayz-backend |
| `customer/` | Customer PWA, app.todayz.in | todayz-customer |
| `business/` | Business PWA + offline POS, business.todayz.in | todayz-business |
| `admin/` | Admin console, admin.todayz.in | todayz-admin |
| `site/` | Astro marketing site, todayz.in | todayz-site |
| `shared/` | `@todayz/tokens`, `ui`, `api-client`, `i18n`, `telemetry`, `config` | todayz-shared |
| `infra/` | Terraform (AWS + Cloudflare), Grafana dashboards and alerts, local stack config | todayz-infra |
| `e2e/` | Cross-app Playwright, contract, load (k6), security, mocks | (stays with infra or backend) |
| `design/` | Design system source copy and generators | — |
| `docs/` | Plan, ADRs, runbooks, QA evidence | — |
| `storefront/` | Later | — |

Each app folder must build and test on its own (no imports into a sibling app), so it can become its own repo later. Every folder may depend on `shared/` and `design/`, and on nothing else in this repo. When the repos split, `shared` becomes a versioned package.

## Rules

- **Never print, echo or log secret values** (AWS, RDS, Beanstalk, Cloudflare, SMS, WhatsApp, Sentry, Grafana, Anthropic, Sarvam keys). Refer to variables by name. Use `make env-check`, which prints only set/missing.
- **Local first.** Build and test everything locally (`make dev`) until the whole product is done and tested end to end. Do not create or touch cloud resources until the user has provided credentials for Wave 5.
- **No GitHub repos and no pushing.** One local git repo at this root. The user will say when to split and give repo names.
- The API contract is `backend/api/` (split per domain, bundled by `make gen`). The bundled `openapi.yaml` is generated: edit only your own domain's files in `backend/api/paths/` and `backend/api/schemas/`, and run `make gen` in the same change.
- **Generated code is not committed.** `backend/gen`, `shared/api-client` and the bundled `openapi.yaml` are git-ignored; `make gen` produces them, and every build and check runs `make gen` first. `routeTree.gen.ts` is git-ignored and generated too.
- Database changes only through new migration files.
- Never hard-code a number that `01-product-rules.md` marks configurable; read it from `platform_settings`.
- AI output is always a draft a person confirms; nothing changes stock, money or orders from a model output directly.
- Ask the lead (or the user) when a rule about money, codes, timers, permissions or user-visible behaviour is unclear. Do not guess.

## Commands (after Wave 0)

```
make setup              # install toolchains and deps; generate missing app keys into .env.local (prints names only)
make dev                # local stack: Postgres/PostGIS, MinIO, Grafana LGTM, mocks, Caddy HTTPS, apps
make stop               # stop the local stack
make logs               # tail the local stack's logs
make reset              # recreate DB, migrate, seed (there is no make seed)
make gen                # bundle OpenAPI, generate Go + TS clients, sqlc (output is not committed)
make check              # make gen, lint, types, unit + integration, contract smoke, builds, security static, terraform validate, recorded AI evals
make e2e                # Playwright journeys, contract full, visual, a11y, Lighthouse (local files only)
make load               # k6
make security           # ZAP, Trivy, checkov, abuse suite
make ai-eval            # live-model AI evals; opt-in, only with the user's own keys in .env.local
make demo               # live order generator for dashboards and the business app
make env-check          # env var names with set/missing, never values
make release-candidate  # all of the above on a clean reset; writes docs/qa/rc-<date>/
```

The list is canonical (D-56): the same in `docs/plan/02-architecture.md`, `docs/plan/09-infra-release.md` and W0-01. Before Wave 5, Terraform runs only `fmt -check`, `validate`, tflint and checkov; no `plan` or `apply`.
