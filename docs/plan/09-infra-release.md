# 09 · Environments, infrastructure and release

**Rule (user, 2026-10-22):** everything runs and is tested locally until the product is implemented end to end and fully tested. No cloud resource is created, and no cloud credential is needed, before Wave 5. When the local release candidate passes, the lead tells the user "ready for cloud" with the evidence pack. The user then puts AWS, RDS, Beanstalk and Cloudflare credentials in the environment.

## 1. Environments

| Env | Where | When | Data | Integrations |
|---|---|---|---|---|
| `local` | Developer machine via `docker compose` + Caddy | Waves 0–4 (and forever for development) | `seed dev` / `seed demo` | Mocks (W0-07). SMS, push and Sentry always mocked or local; Anthropic and Sarvam mocked unless the user adds their own keys (§2) |
| `test` | Ephemeral containers in `make check`/`make e2e` | Every run | `seed test`, test clock on | Mocks only, even when `.env.local` has provider keys |
| `staging` | AWS + Cloudflare, `staging.*` hosts | Wave 5 | `seed demo` | Real providers in sandbox/test modes |
| `prod` | AWS `ap-south-1` + Cloudflare | Wave 5 launch | Real | Real |

### 1.1 Local stack (`make dev`)

| Service | Image / process | Host |
|---|---|---|
| Postgres 16 + PostGIS 3.4 | `postgis/postgis:16-3.4` (amd64) or the same build multi-arch as `imresamu/postgis:16-3.4` (arm64), with `shared_preload_libraries=pg_stat_statements` | `127.0.0.1:5433` (5432 is left for a developer's own Postgres; D-86) |
| MinIO (R2 stand-in) | `pgsty/minio` (MinIO stopped publishing images; D-86) | `s3.todayz.localhost` (API 9100, console 9101) |
| API + worker | `go run ./cmd/todayz api|worker` with `air` hot reload | `api.todayz.localhost` |
| Customer / business / admin | Vite dev servers | `app.` `business.` `admin.todayz.localhost` |
| Site | Astro dev | `todayz.localhost` |
| Tiles and images | MinIO buckets behind Caddy | `tiles.` `img.todayz.localhost` |
| Observability | `grafana/otel-lgtm` (Grafana, Mimir, Loki, Tempo, OTel collector) + `postgres_exporter` | `grafana.todayz.localhost` |
| Mocks | `e2e/mocks` | `mocks.todayz.localhost` |
| Reverse proxy | Caddy with a local CA (`caddy trust`) for real HTTPS, so secure cookies, service workers, camera, mic and push all behave as in production | all hosts |

Caddy hosts (D-57): `todayz.localhost`, `app.`, `business.`, `admin.`, `api.`, `grafana.`, `mocks.`, `s3.`, `tiles.`, `img.todayz.localhost`. Mailpit is not part of the stack.

Refresh cookies (D-14) are host-only on `api.todayz.localhost`: no `Domain` attribute, so they are never set on `.todayz.localhost`. Each app has its own cookie (`__Secure-tz_rt_c`, `__Secure-tz_rt_b`, `__Secure-tz_rt_a`) with `Path=/v1/auth`, `HttpOnly`, `Secure` and `SameSite=Strict`. The apps and the API share a registrable domain, so their requests are same-site, locally and in the cloud.

OTP (D-15): an OTP is never logged in any environment. In `local` and `test`, the reserved numbers `+91 90000 000xx` accept `000000`; any other number goes through the mock SMS server and the message shows in the mocks UI. There is no `dev` environment.

`make stop` stops the stack and `make logs` tails it. `make reset` drops and recreates the DB, then runs migrations and the seed (there is no `make seed`). `make demo` starts the live order generator so dashboards and the business app show activity.

## 2. Configuration and secrets

- All configuration comes from env vars; `.env.example` lists every name with a comment and no values. Local values go in `.env.local` (git-ignored).
- **Never print secret values.** Config loading reports missing names only. `make env-check` prints a table of names with `set` / `missing` (never the values). Agents must not `cat`, `echo`, `printenv` or log secret variables.
- Production secrets live in AWS Secrets Manager and are injected into Beanstalk at deploy. Cloudflare and AWS admin credentials are used only by Terraform/CI, never by the running app.
- Provider keys live in env only. `platform_settings` holds on/off flags only; a channel works only when its flag is on and its key is set. A-17 shows each key as `set` / `missing`, never the value (D-19).
- **Third-party services before Wave 5 (D-17, default chosen; the user can change it):**
  - SMS, push and Sentry always use mocks or local tools before staging. Real SMS would reach real phones and needs DLT registration first. Errors go to local Grafana/Loki. `MSG91_*`, push and Sentry keys are not used locally.
  - Anthropic and Sarvam use the mocks by default. If the user chooses to put their own `ANTHROPIC_API_KEY` / `SARVAM_API_KEY` in `.env.local`, live AI evals (`make ai-eval`) and manual AI QA can run locally. That is the user's choice; `make check`, `make e2e` and every other default target never need them.

### 2.1 App keys (D-18)

| Env var | Purpose |
|---|---|
| `JWT_SIGNING_KEYS` | JSON key set of Ed25519 keys with `kid` |
| `JWT_CURRENT_KID` | Which key in the set signs new tokens |
| `ORDER_CODE_ENC_KEY` | AES-256-GCM for the stored order code (`code_enc`) |
| `ORDER_CODE_HMAC_KEY` | HMAC for order code lookup at the counter (`code_hmac`) |
| `BILL_QR_SIGNING_KEY` | Signs bill QRs |
| `PII_ENC_KEY` | AES-256-GCM for phone, PAN, GSTIN, UPI ID and bank details |
| `PII_HASH_KEY` | HMAC for PII lookups |
| `DEVICE_SECRET_ENC_KEY` | Counter-code device secrets |
| `TOTP_ENC_KEY` | Admin TOTP secrets |

- `make setup` generates any missing key into `.env.local` and prints only `generated: NAME`, never the value.
- In staging and prod these keys live in Secrets Manager.

## 3. Cloud architecture (Wave 5)

```
Users ─► Cloudflare (DNS, TLS, WAF, rate limits, Turnstile, cache)
          ├─ todayz.in, app., business., admin.  → Cloudflare Pages (static builds)
          ├─ img., tiles.                         → R2 (custom domains, cached)
          └─ api.                                 → AWS ALB (accepts Cloudflare IPs only)
                                                     └─ Beanstalk "api" (Docker, 2× t4g.small, autoscale 2–6)
                                                  Beanstalk "worker" (1–2× t4g.small, River workers)
                                                  RDS Postgres 16 + PostGIS (db.t4g.medium, 100 GB gp3, PITR 14 d, private subnets)
                                                  Secrets Manager · ECR · CloudWatch
Observability: OTel → Grafana Cloud (metrics/logs/traces) · Sentry · CloudWatch + Cloudflare datasources in Grafana
```

- Region `ap-south-1` (Mumbai) for data residency and latency.
- Network: VPC with 2 AZs, public subnets for the ALB only, private subnets for Beanstalk and RDS, NAT gateway (one at launch to save cost), VPC endpoints for S3/ECR/Secrets Manager.
- RDS: Single-AZ at launch with automated backups + PITR and a weekly cross-region snapshot copy; Multi-AZ once there is revenue (documented trade-off). Parameter group: `pg_stat_statements`, `log_min_duration_statement=500ms`, `idle_in_transaction_session_timeout=60s`, `statement_timeout` per role.
- Beanstalk: Docker on Amazon Linux 2023, rolling deploys with additional batch, health check `/readyz`, enhanced health reporting, instance profile with least privilege, logs to CloudWatch, plus the OTel exporter.
- Worker stop: on SIGTERM the worker stops fetching jobs, gives running jobs 30 s to finish (`Stop`), then cancels the rest (`StopAndCancel`) and waits another 30 s for them to return. The Beanstalk worker's stop timeout (and any container stop grace period) must be **longer than 60 s**, or the platform kills the process mid-job. Jobs must be safe to re-run (02 §2.5). The worker serves `/healthz`, `/readyz` and `/version` on its internal port 8181.
- Migrations run as a separate step (`todayz migrate up`, a one-off task with the migration role) before the new version deploys; migrations must be backward compatible with the previous app version (expand → migrate → contract).
- Estimated monthly cost at launch: about ₹9–14k (RDS t4g.medium ~₹5k, 3 small instances ~₹3k, ALB ~₹1.5k, NAT ~₹2.5k, storage/backup ~₹1k). Cloudflare free/pro plan, Grafana Cloud and Sentry free tiers at first.

## 4. CI/CD

Until GitHub repos exist, **the local `make` targets are the pipeline**:

The canonical target list (D-56), the same in 02, this file, `CLAUDE.md` and W0-01. The root Makefile is owned by W0-01 and includes `mk/*.mk`; each package adds its own `mk/<area>.mk` (D-50).

| Target | Runs |
|---|---|
| `make setup` | Installs toolchains and deps; generates missing app keys into `.env.local` (§2.1), printing names only |
| `make dev` | The local stack (§1.1) |
| `make stop` | Stops the local stack |
| `make logs` | Tails the local stack's logs |
| `make reset` | Drops and recreates the DB, migrates, seeds |
| `make gen` | Bundles OpenAPI, generates the Go and TS clients and sqlc code. Generated code (`backend/gen`, `shared/api-client`, the bundled `openapi.yaml`) is not committed; every build and check runs `make gen` first (D-50) |
| `make check` | `make gen` first; lint (golangci-lint, ESLint, Stylelint), type check, unit + integration tests, contract smoke, builds, size-limit, gitleaks, gosec, govulncheck, `terraform fmt -check`, `terraform validate`, tflint, AI evals (recorded fixtures only) |
| `make e2e` | the full Playwright suite, Schemathesis full, visual, axe, Lighthouse CI (results written to the local filesystem only, D-58) |
| `make load` | k6 scenarios |
| `make security` | ZAP baseline, Trivy, checkov, the abuse suite |
| `make ai-eval` | AI evals against the live model. Opt-in: runs only when the user has put their own keys in `.env.local` (D-17); otherwise it says which names are missing and exits |
| `make demo` | The live order generator |
| `make env-check` | A table of env var names with `set` / `missing`, never the values |
| `make release-candidate` | `check`, `e2e`, `load` and `security` on a clean `make reset`, then writes `docs/qa/rc-<date>/` (the evidence pack) |

**Terraform before Wave 5 (D-16):** the only steps are `terraform fmt -check`, `terraform validate`, tflint and checkov. No `terraform plan` or `apply` runs before Wave 5.

GitHub Actions workflows are written in Wave 0 (W0-06) to mirror these. They will run once the user creates the repos and asks to push. Deploy workflows (Wave 5): build image → push to ECR → migrate → deploy Beanstalk api and worker → build and deploy Pages apps → upload Sentry source maps → smoke test → annotate Grafana. Production deploys need a manual approval.

## 5. Wave 5 steps (after credentials)

1. The user adds credentials to env (names listed in `.env.example` under "Cloud"). The lead runs `make env-check` and confirms only `set`/`missing`.
2. The first `terraform plan` ever run, for staging; the user reviews the plan summary (resources and estimated cost) and says yes.
3. Apply staging; run migrations; seed demo; deploy apps; point `staging.*` DNS.
4. Run `make e2e`, `make load` (staging-sized), `make security` and the ★ manual scripts against staging. Fix, repeat.
5. The user confirms the production plan (cost, domain cut-over time).
6. Apply prod; seed reference data (categories, places, master catalogue, admin users); DNS cut-over for `todayz.in` (old site URLs 301 to new); enable monitoring and alerts.
7. Hyper-care for 72 h: dashboards 00/01/08 watched, daily report to the user.

## 6. Launch checklist (prod)

- [ ] All Wave 4 evidence green; zero P0/P1 defects open.
- [ ] Restore drill done on staging (PITR to a new instance, reconcile passes).
- [ ] Alerts route to the founders' phones; a test alert received.
- [ ] Runbooks linked from every alert.
- [ ] Rate limits and WAF rules on; Turnstile keys set; OTP via real SMS verified on Jio, Airtel and Vi numbers.
- [ ] DLT registration for SMS templates done (India TRAI requirement for MSG91 sender/templates).
- [ ] Privacy policy, terms, grievance officer contact live; consent text in sign-up.
- [ ] Super admin accounts with TOTP; no shared accounts.
- [ ] `platform_settings` reviewed by the founders (timers, credit caps, referral amounts).
- [ ] Old site redirect map tested; sitemap submitted to Search Console.
- [ ] Sentry releases and source maps working; Grafana dashboards show prod data.
- [ ] Cost alarms (AWS Budgets at ₹15k/month, Anthropic/SMS spend alerts).
- [ ] First 10 shops onboarded and verified with real catalogues before public announcement.

## 7. Rollback

- App: Beanstalk redeploys the previous application version (one click/command); Pages rolls back to the previous deployment.
- DB: forward-only migrations designed expand/contract, so the previous app version works against the new schema. Destructive changes need a separate later migration. Data repair uses PITR to a side instance and copies back only the affected rows.
- Feature flags in `platform_settings` turn off risky features without a deploy (D-25):
  - `feature_delivery_enabled`, `feature_bill_qr_enabled`, `feature_voice_enabled`, `feature_offers_enabled`, `feature_banners_enabled`
  - one `ai_<feature>_enabled` per AI feature (list in [11-ai-features.md](11-ai-features.md) §1)
  - `ai_daily_cost_cap_paise` stops AI calls for the day once spend reaches it
