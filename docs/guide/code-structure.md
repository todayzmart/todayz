# Code structure

Where everything is in the repo, and how each folder is organised.

> As built on main at d641cbe6 (27 Oct 2026). Update this file in the same change as the code it describes.

Use this page to find the file for a change. For how the pieces work together read [architecture.md](architecture.md), and for journeys read [flows.md](flows.md). Rules for each folder are in its own `CLAUDE.md`.

## 1. Top-level folders

| Folder | What | Notes |
|---|---|---|
| `backend/` | Go API and worker, one binary | See [backend/README.md](../../backend/README.md) |
| `customer/` | Customer PWA (app.todayz.in) | [customer/CLAUDE.md](../../customer/CLAUDE.md) |
| `business/` | Business PWA with the offline POS (business.todayz.in) | [business/CLAUDE.md](../../business/CLAUDE.md) |
| `admin/` | Admin console (admin.todayz.in) | [admin/CLAUDE.md](../../admin/CLAUDE.md) |
| `site/` | Astro marketing site (todayz.in) | [site/CLAUDE.md](../../site/CLAUDE.md) |
| `shared/` | `@todayz/*` packages used by the apps | [shared/CLAUDE.md](../../shared/CLAUDE.md) |
| `infra/` | Terraform (AWS, Cloudflare), Grafana dashboards and alerts, local stack config | [infra/CLAUDE.md](../../infra/CLAUDE.md) |
| `e2e/` | Playwright journeys, a11y, visual, contract, k6 load, security, provider mocks | [e2e/CLAUDE.md](../../e2e/CLAUDE.md) |
| `design/` | Design system source copy and generators | [design/README.md](../../design/README.md) |
| `docs/` | Plan, ADRs, guides, runbooks, ops, QA evidence | [docs/README.md](../README.md) |
| `mk/` | Make step files, run in name order | see section 8 |
| `scripts/` | Repo scripts called by make, hooks and CI | see section 9 |
| `.github/` | Workflows, composite actions, Dependabot | `ci.yml`, `deploy.yml`, `deploy-backend.yml`, `e2e.yml`, `security.yml` |

An app folder must build and test on its own: no imports into a sibling app. Every folder may depend on `shared/` and `design/` and nothing else in the repo. Each folder may become its own repo later, and `shared` then becomes a versioned package.

## 2. Backend layout

```
backend/
  cmd/todayz/        the binary: main.go, subcommands, route mounting, worker, seams
  api/               the OpenAPI contract, split per domain (paths, schemas, problems, common)
  db/migrations/     goose migrations (embedded in the binary)
  db/queries/        sqlc query files, one folder per domain
  db/sqlc.yaml       sqlc config (one Go package per domain)
  gen/               generated Go (git-ignored): api, sqlc, problems, events
  internal/          every domain and platform package
  tools/             go.mod that pins the build tools (oapi-codegen, sqlc, goose and so on)
```

| Path | What is in it | Edit by hand? |
|---|---|---|
| `backend/cmd/todayz` | `main.go` (subcommands), `api.go`, `worker.go`, `routes.go` and one `routes_<domain>.go` per domain, `*_seams.go` (wiring between domains), `security.go`, `ai.go`, `rollup.go`, `images.go`, `admin_cli.go` (break-glass sign-in unlock), `migrate.go`, plus the HTTP tests (`*_http_test.go`) | yes |
| `backend/api` | `root.yaml`, `paths/<domain>.yaml`, `schemas/<domain>.yaml`, `problems/<domain>.yaml`, `common/`, `redocly.yaml`; `openapi.yaml` is the bundle | the split files yes, the bundle no |
| `backend/db/migrations` | `NNNNN_<wave>_<name>.sql` files, 65 at this commit, and a [README](../../backend/db/migrations/README.md) | add new files only |
| `backend/db/queries` | `<domain>/*.sql` with sqlc annotations | yes |
| `backend/gen/api/<domain>` | Go types and strict server interface per domain | no |
| `backend/gen/sqlc/<domain>db` | typed query code per domain | no |
| `backend/gen/problems` | problem code constants | no |
| `backend/gen/events` | the event catalogue embedded for `eventcat` | no |

## 3. `backend/internal`: every package

Each name is a directory under `backend/internal`. "Main files" are where to start reading.

### Domains

| Package | Purpose | Main files |
|---|---|---|
| `backend/internal/admin` | The admin console's own domain: customers, businesses, stores, catalogue review, product images, banners, audit views, AI assist, the D-269 shop check queue | `admin.go`, `handlers.go`, `businesses.go`, `shop_checks.go`, `product_images.go` |
| `backend/internal/ai` | The AI platform and every AI feature, one subfolder each (see section 4) | `client/`, `guard/`, `drafts/`, `scrub/` |
| `backend/internal/asks` | "Ask shops nearby": create, pick stores, shop inbox, replies, windows | `asks.go`, `create.go`, `inbox.go`, `handlers.go` |
| `backend/internal/banners` | Shop banners: upload, review queue, daily stats, tidy job | `banners.go`, `manage.go`, `handlers.go` |
| `backend/internal/baskets` | A customer's saved baskets | `baskets.go`, `handlers.go` |
| `backend/internal/business` | Businesses, stores, staff, onboarding, KYC documents, field agents | `business.go`, `handlers.go`, `agent.go` |
| `backend/internal/catalogue` | Products, store items, stock movements, batches, photos; the single stock writer | `catalogue.go`, `writer.go`, `movements.go`, `photos.go` |
| `backend/internal/coach` | The shop coach: detectors, feed, runs, templates | `coach.go`, `feed.go`, `runs.go` |
| `backend/internal/credits` | The credits ledger: wallet, lots, holds, cashback, expiry, adjustments | `credits.go`, `adjustments.go`, `reads.go`, `jobs.go` |
| `backend/internal/identity` | Sign-in (OTP), sessions, refresh rotation, admin SMS step-up, addresses, profile photo, accounts | `identity.go`, `otp.go`, `session.go`, `stepup.go` |
| `backend/internal/imageimport` | Drafts product images from Open Food, Beauty and Products Facts; finds item-type photo candidates on Wikimedia Commons and applies saved picks (`data/approved-type-photos.csv`) | `imageimport.go`, `sources.go`, `types.go`, `picks.go` |
| `backend/internal/insights` | Business Overview and Demand insights reads | `service.go`, `overview.go`, `analytics.go` |
| `backend/internal/jobs` | Registry of every job kind, one file per domain | `jobs.go`, `orders.go`, `credits.go` |
| `backend/internal/notify` | Notification inboxes, delivery (SMS, WhatsApp), escalation, preferences | `notify.go`, `deliver.go`, `provider.go` |
| `backend/internal/offers` | Offers and their pricing engine, feed, redemptions | `offers.go`, `engine.go`, `pricing.go` |
| `backend/internal/ops` | The admin System screen: Beanstalk, DB and queue health | `ops.go`, `aws.go`, `db.go` |
| `backend/internal/orders` | Online booking: create, state machine, timers, codes, delivery, returns, strikes, the shop-reject cancel (D-269) | `orders.go`, `machine.go`, `create.go`, `transitions.go`, `timers.go`, `shop_reject.go` |
| `backend/internal/payouts` | Monthly statements and payouts | `payouts.go`, `close.go`, `business.go` |
| `backend/internal/pos` | The shop counter: sales, sync, bill claims, receipts | `pos.go`, `sale.go`, `sync.go`, `claims.go` |
| `backend/internal/referrals` | Referral programme: clicks, claims, qualifying, rewards | `referrals.go`, `claim.go`, `qualify.go`, `reward.go` |
| `backend/internal/reviews` | Customer reviews of a shop, replies, reports | `reviews.go`, `handlers.go` |
| `backend/internal/risk` | Risk and fraud rules, flags, the `risk_scan` job | `rules.go`, `flags.go`, `service.go` |
| `backend/internal/rollups` | Analytics rollups and the Pulse screen | `rollups.go`, `pulse.go` |
| `backend/internal/search` | Places, discovery, product and basket search, search logging | `search.go`, `discover.go`, `basket.go`, `logging.go` |
| `backend/internal/share` | Public share cards and the site's marquee | `cards.go`, `render.go`, `handlers.go` |
| `backend/internal/support` | Support tickets for business, customers and admins | `support.go`, `biz.go`, `me.go`, `admin.go` |
| `backend/internal/telemetry` | Browser RUM batches and the app event intake | `rum.go`, `events.go`, `telemetry.go` |
| `backend/internal/voice` | Voice stock update (speak sales, confirm a draft) | `voice.go`, `process.go`, `confirm.go` |

### Support packages

| Package | Purpose | Main files |
|---|---|---|
| `backend/internal/devtools` | Code generators and lints run by `make gen` and `make check`: `apigen`, `eventgen`, `problemgen`, `settingslint`, `vulngate` | one folder per tool |
| `backend/internal/seed` | Seed data registry for `todayz seed dev|demo|test` | `seed.go`, one file per domain |
| `backend/internal/testkit` | Integration test helpers: a real PostGIS database and fixtures | `testkit.go`, `fixtures.go` |
| `backend/internal/platform` | Shared plumbing with no product rules (next table) | see below |

### `backend/internal/platform`

| Package | Purpose |
|---|---|
| `backend/internal/platform/audit` | Append-only `audit_log` writes |
| `backend/internal/platform/authn` | Who is calling: principal, token parsing and signing |
| `backend/internal/platform/authz` | Permission engine, policies per domain (`policy/`), admin gate, staff scope |
| `backend/internal/platform/awssms` | SMS through AWS End User Messaging |
| `backend/internal/platform/buildinfo` | Version stamped at build time |
| `backend/internal/platform/clock` | The only source of "now" |
| `backend/internal/platform/config` | Process configuration read from the environment |
| `backend/internal/platform/db` | pgx pool and transactions carried through context |
| `backend/internal/platform/dbenv` | Records which `APP_ENV` a database belongs to (refuses a mismatch) |
| `backend/internal/platform/egress` | Outbound HTTP clients that can be locked to loopback |
| `backend/internal/platform/eventcat` | Loads the product event catalogue |
| `backend/internal/platform/geo` | Geo helpers; `h3/` holds the H3 cell code |
| `backend/internal/platform/health` | `/healthz`, `/readyz`, `/version` |
| `backend/internal/platform/httpx` | chi router, `mw/` middleware slots, `problem/`, `route/` policy, `openapi/` validator, `ifmatch/` |
| `backend/internal/platform/idempotency` | Safe retries for unsafe requests |
| `backend/internal/platform/ids` | ULIDs |
| `backend/internal/platform/imageclean` | Re-encodes an uploaded photo so only pixels are kept (profile photos) |
| `backend/internal/platform/jobs` | The jobs framework: kinds, classes, definitions |
| `backend/internal/platform/lifecycle` | SIGTERM drain for the API and the worker |
| `backend/internal/platform/migrate` | Runs the embedded goose migrations |
| `backend/internal/platform/money` | Integer paise arithmetic |
| `backend/internal/platform/notice` | The notify seam: inbox row, stream event and delivery job together |
| `backend/internal/platform/pgnotify` | One LISTEN connection to Postgres channels |
| `backend/internal/platform/pii` | Seals and hashes personal data |
| `backend/internal/platform/queue` | River wrapper: insert in a transaction, run workers |
| `backend/internal/platform/ratelimit` | In-process token bucket |
| `backend/internal/platform/realtime` | SSE hub, publish, stream, cursor, cleanup |
| `backend/internal/platform/reqctx` | Request id and other per-request values |
| `backend/internal/platform/settings` | `platform_settings` reads through a short cache |
| `backend/internal/platform/storage` | Object storage (R2 deployed, MinIO local) |
| `backend/internal/platform/telemetry` | Logs, traces, metrics, error reporting |
| `backend/internal/platform/testclock` | `/v1/test/clock` (non-prod only) |

## 4. How a domain package is organised

Domain packages follow one pattern. Not every file exists in every package.

| File | Role |
|---|---|
| `<domain>.go` | The `Service`, its `Deps` and `New(Deps)`; package comment naming the plan sections |
| `handlers.go` | Implements the generated strict-server interface from `backend/gen/api/<domain>`; decodes, calls the service, maps the result |
| `reads.go` | Read-only queries and response mapping |
| `create.go`, `transitions.go`, `manage.go` and similar | Write paths, named for what they do |
| `jobs.go` or `timers.go` | Handlers for the package's job kinds (registered in `backend/internal/jobs`) |
| `seams.go` | Small interfaces other domains implement, so domains never import each other's internals |
| `*_test.go` | Unit tests beside the code; integration tests use `backend/internal/testkit` |

Rules that hold across packages:

- Queries are sqlc: the SQL is in `backend/db/queries/<domain>/`, the Go in `backend/gen/sqlc/<domain>db`. There is no separate store layer; the service gets a `Queries` from the transaction in context (`s.q(ctx)`).
- A route's policy is in `backend/internal/platform/authz/policy/<domain>.go`. A new endpoint without a policy fails closed.
- The wiring is in `backend/cmd/todayz/routes_<domain>.go`: it builds the service, mounts the generated server and registers the policy.
- Job kinds are declared in `backend/internal/jobs/<domain>.go` through `platform/jobs`, and their runbooks live in [docs/runbooks](../runbooks/README.md).

### AI packages (`backend/internal/ai`)

| Folder | Purpose |
|---|---|
| `client`, `guard`, `scrub`, `schema`, `prompts`, `eval` | The platform: provider calls, switches and caps, PII removal, tool schemas, prompt registry, evals |
| `drafts` | The `ai_drafts` table and its lifecycle (draft, ready, confirmed) |
| `retail` | What the shop's draft-and-confirm features share |
| `shelfscan`, `billscan`, `listparse`, `voicesearch`, `querynorm` | Shelf photo, supplier bill, shopping list, voice search, query reading |
| `replydraft`, `bannercopy`, `insighttext` | Review replies, banner copy, one-line demand insights |
| `assist`, `catalogue`, `triage`, `riskexplain` | Admin helpers: catalogue clean-up, ticket triage, risk summaries |
| `imagecheck` | Banner image advice; not wired yet |

## 5. Frontend app layout (customer, business, admin)

The three apps share one shape: React 19, Vite, TanStack Router (file based) and TanStack Query.

```
<app>/src/
  main.tsx            entry
  app/                App.tsx, env, i18n, labels, session, router setup, error fallback
  shell/              the frame: Shell.tsx, nav.ts, NotFound, Placeholder
  routes/             one file per URL; thin: createFileRoute(...) points at a feature page
  routeTree.gen.ts    generated by TanStack Router (git-ignored)
  <feature>/          one folder per feature: pages, components, api hooks, tests, css modules
  test/               test setup
```

- **Routes are thin.** For example `customer/src/routes/cart.tsx` only calls `createFileRoute('/cart')` and points at `CartPage` in `customer/src/cart`.
- **Feature folders** hold the screens and their data hooks. Customer examples: `search`, `store`, `product`, `cart`, `checkout`, `orders`, `asks`, `basket`, `account`. Business examples: `pos`, `stock`, `orders`, `credits`, `statements`, `coach`, `ai-drafts`, `onboarding`, `sync`. Admin examples: `businesses`, `shop-checks`, `catalogue`, `orders`, `payouts`, `risk`, `jobs`, `system`, `settings`.
- **`routeTree.gen.ts` is generated and git-ignored.** `make gen` writes it. Never edit it.
- **Business specifics.** `business/src/sync` is the offline engine (`engine.ts`, `idb.ts`, `storage.ts`) and `business/src/pos` is the counter. Both are described in [flows.md](flows.md#7-pos-sale-offline-then-sync).
- **Admin specifics.** `admin/src/app` has the SMS step-up dialog (`StepUpDialog.tsx`, D-264) and `ReasonDialog.tsx` for audited actions.
- **Labels and text** come from `@todayz/i18n` through `app/i18n.ts`; no user-facing string is inline.
- **API calls** go through `@todayz/api-client`; no app builds its own fetch.

## 6. Site (Astro)

`site/` is a static site with no login.

| Path | What |
|---|---|
| `site/src/pages` | `[...slug].astro` (all content pages), `404.astro`, `robots.txt.ts`, `sitemap.xml.ts` |
| `site/src/views` | Page bodies: `HomeView`, `RetailersView`, `InfoView`, `LegalView`, `NotFoundView` |
| `site/src/layouts` | `Base.astro` |
| `site/src/components`, `lib`, `i18n`, `styles`, `assets` | Components, helpers, messages, styles, images |

## 7. Shared packages

| Package | Folder | What it is |
|---|---|---|
| `@todayz/tokens` | `shared/tokens` | Design tokens from `src/` per app, built to `generated/` (CSS, JSON, TS, fonts, icons); `generated/` is git-ignored |
| `@todayz/ui` | `shared/ui` | React component kit (Button, Dialog, OtpInput, DataTable and more) with Storybook |
| `@todayz/api-client` | `shared/api-client` | Typed client of the contract: `src/http.ts` by hand, `src/gen/` and `src/mocks/handlers.gen.ts` generated |
| `@todayz/auth` | `shared/auth` | Session and token handling, phone OTP form, Turnstile, update prompt, shell pieces |
| `@todayz/config` | `shared/config` | Shared ESLint, Prettier, Stylelint, TypeScript, Vite, Vitest and Playwright presets |
| `@todayz/i18n` | `shared/i18n` | Messages per app and language (`messages/`), compiler, lint, glossary |
| `@todayz/realtime` | `shared/realtime` | SSE client, resume, backoff, polling fallback, query invalidation hook |
| `@todayz/telemetry` | `shared/telemetry` | Typed event emitter, RUM, trace headers, Sentry; event catalogue in `events/<domain>.yaml` |

## 8. Generated code

`make gen` produces all of the following. None of it is committed. Every build and check runs `make gen` first.

| Output | Made by | Step |
|---|---|---|
| `backend/api/openapi.yaml` (the bundle) | Redocly from `backend/api/paths`, `schemas`, `problems` | `gen-contract` |
| `backend/gen/api/<domain>` | oapi-codegen strict server per domain | `gen-contract` |
| `backend/gen/sqlc/<domain>db` | sqlc from `backend/db/queries` | `gen-backend` |
| `backend/gen/problems` | `backend/internal/devtools/problemgen` | `gen-backend` |
| `backend/gen/events` and `shared/telemetry/src/gen/events.ts` | `backend/internal/devtools/eventgen` from `shared/telemetry/events/*.yaml` | `gen-events` |
| `shared/api-client/src/gen/**` and `src/mocks/handlers.gen.ts` | orval (client and Zod schemas) and MSW handlers | `gen-frontend` |
| `**/routeTree.gen.ts` | TanStack Router plugin | `gen-frontend` |
| `shared/tokens/generated` | the tokens build script | `setup-frontend` and builds |

To change anything in this list, change its source and run `make gen`.

## 9. Make step files (`mk/`)

Steps run in file-name order. Each file adds its own steps to a list (`SETUP_STEPS`, `GEN_STEPS`, `CHECK_STEPS`, `DEV_STEPS`, `STOP_STEPS`, `LOGS_STEPS`, `RESET_STEPS`, `DEMO_STEPS`, `LOAD_STEPS`, `SECURITY_STEPS`). Nobody edits the root `Makefile`.

| File | Registers | What it covers |
|---|---|---|
| `mk/00-repo.mk` | setup-tools, setup-keys, setup-node, setup-hooks; check-repo | Toolchain check, app keys, git hooks, repo checks |
| `mk/10-stack.mk` | setup-stack-images, setup-tiles; dev-stack, dev-buckets, dev-caddy; stop-stack; logs-stack; reset-db; check-stack | Docker stack, Caddy, buckets, map tiles |
| `mk/20-backend.mk` | setup-backend; gen-backend; check-backend; reset-backend; dev-backend | Go tools, sqlc and problem codes, Go lint and tests, migrate and seed, API and worker processes |
| `mk/22-journeys.mk` | check-journeys | Wave 2 journey gate (Playwright, multi-principal) |
| `mk/25-contract.mk` | setup-contract; gen-contract; check-contract | OpenAPI bundle, generation, contract checks |
| `mk/26-events.mk` | gen-events | Product event catalogue generation |
| `mk/27-rollups.mk` | demo-rollups | `make demo` synthetic traffic |
| `mk/28-ai.mk` | check-ai-eval | Recorded AI evals (no key, no network) |
| `mk/30-mocks.mk` | setup-mocks; check-mocks; dev-mocks | Provider mocks in `e2e/mocks` |
| `mk/40-frontend.mk` | setup-frontend; gen-frontend; check-frontend; dev-i18n, dev-frontend | The four web apps and shared packages |
| `mk/42-i18n.mk` | check-i18n-messages | Message lint for the four apps |
| `mk/45-ui.mk` | check-ui | `@todayz/ui` lint, types, tests, Storybook build |
| `mk/46-auth.mk` | check-auth | `@todayz/auth` lint, types, tests |
| `mk/47-realtime.mk` | check-realtime | `@todayz/realtime` lint, types, tests |
| `mk/48-a11y.mk` | check-a11y | Accessibility types check; axe, visual and Lighthouse run in `make e2e` |
| `mk/50-infra.mk` | setup-infra-cloudflare; check-infra | Terraform policy checks (offline) |
| `mk/60-ops.mk` | check-runbooks | Every alert and job kind has a runbook; links resolve |
| `mk/62-docs.mk` | check-docs | Docs exist, links resolve, every package and mk file is named |
| `mk/70-load.mk` | load-browse, load-orders, load-pos, load-otp, load-sse, load-soak | k6 scenarios and the SSE fan-out driver |
| `mk/90-ci.mk` | setup-ci; check-ci, check-tf-static; security-gitleaks, security-trivy, security-checkov, security-zap, security-abuse | CI definitions, workflow lint, security steps |
| `mk/99-health.mk` | dev-health | Last `make dev` step: checks the whole stack |

## 10. `scripts/`

| Folder or file | What |
|---|---|
| `scripts/ci` | CI helpers: `check.sh`, `act.sh` (run workflows locally), toolchain install and pinned versions, Lighthouse, report collection, `wflint/` (workflow lint) |
| `scripts/deploy` | Deploy scripts: `beanstalk.sh`, `pages.sh`, `secrets.sh`, `state-bucket.sh` (run by the lead, see [deployment.md](deployment.md)) |
| `scripts/docs` | `docs_check.py` and its test (behind `make check-docs`) |
| `scripts/envtool` | Go tool behind `make env-check`; prints names with set or missing, never values |
| `scripts/hooks` | Git hook helpers: `docs-gate.sh`, `forbidden-files.sh`, `no-push.sh` |
| `scripts/ops` | `restore-drill.sh` and `runbooks_check.py` |
| `scripts/test` | Tests of the scripts themselves (hooks, make targets, no-egress) |
| `scripts/no-egress.sh` | Wraps `make check` and `make e2e` so outbound traffic is refused and provider keys are unset |
| `scripts/testpg.sh` | Opt-in shared test Postgres (`make testpg-up`) |
| `scripts/install-uv.sh`, `scripts/make-shell.sh` | Tool install and the make shell wrapper |
