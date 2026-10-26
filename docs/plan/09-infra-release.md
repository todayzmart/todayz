# 09 · Environments, infrastructure and release

> **Amended by D-210 (founder, 24 Oct):** one GitHub repo, no staging, merges deploy straight to prod, cloud work starts now. Where this file says staging or "after the local release candidate", D-210 wins. Cloud credentials live in `.env.cloud.local` (`docs/runbooks/cloud-access.md`).

**Rule (user, 2026-10-22):** everything runs and is tested locally until the product is implemented end to end and fully tested. No cloud resource is created, and no cloud credential is needed, before Wave 5. When the local release candidate passes, the lead tells the user "ready for cloud" with the evidence pack. The user then puts the AWS IAM access keys (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`) and the Cloudflare credentials in the environment (D-96).

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
- Production secrets live in AWS Secrets Manager and are injected into Beanstalk at deploy. Cloudflare and AWS admin credentials (the founder's IAM access keys) are used only by Terraform and the lead (later CI, through GitHub OIDC roles), never by the running app. The app uses instance profiles.
- Provider keys live in env only. `platform_settings` holds on/off flags only; a channel works only when its flag is on and its key is set. A-17 shows each key as `set` / `missing`, never the value (D-19).
- **Third-party services before Wave 5 (D-17, default chosen; the user can change it):**
  - SMS, push and Sentry always use mocks or local tools before staging. Real SMS would reach real phones and needs DLT registration first. Errors go to local Grafana/Loki. Real SMS (the AWS instance role), push and Sentry keys are not used locally; `SMS_API_URL` must point at the mocks outside staging and prod.
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
| `SHARE_PREVIEW_KEY` | Shared with the customer Pages Function: a share-link preview read forwards the visitor's IP with it, so the API rate-limits and counts opens per visitor, not per Cloudflare IP (W3-16). `make setup` generates it locally and `make env-check` lists it with the other app keys. The API starts without it (it is not in `config.AppKeyNames`); previews are then counted by Cloudflare IP. Prod needs the same value set on both the Pages project and the API |

- `make setup` generates any missing key into `.env.local` and prints only `generated: NAME`, never the value.
- In staging and prod these keys live in Secrets Manager.

## 3. Cloud architecture (Wave 5)

The shape is D-96 (with the RDS change in its amendment) and D-98. Local-first is unchanged: no cloud resource exists until the whole product passes locally.

```
Users ─► Cloudflare (DNS, TLS, WAF, rate limits, Turnstile, cache)
          ├─ todayz.in, app., business., admin.  → Cloudflare Pages (static builds)
          ├─ img., tiles.                         → R2 (custom domains, cached)
          └─ api.                                 → AWS ALB (HTTPS via ACM, Cloudflare IPs only, access logs → S3)
                                                     └─ Beanstalk "api"    Docker · AL2023 · Graviton
                                                          2–6 × t4g.small over 2 AZs · deploys: traffic splitting
                                                  Beanstalk "worker" (no ALB)
                                                          1–3 × t4g.small · River workers · deploys: immutable
                                                  RDS Postgres 16 + PostGIS · db.t4g.medium · Single-AZ
                                                          100 GB gp3 → 500 GB · PITR 14 d · private subnets
                                                  Secrets Manager · ECR · CloudWatch
Observability: OTel → Grafana Cloud (metrics/logs/traces) · Sentry · CloudWatch + Cloudflare datasources in Grafana
```

### 3.1 Access

- The founder provides IAM access keys for Wave 5 only. They go in `.env.cloud.local` (D-210) as `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_REGION`, and are never printed (`make env-check` shows names only).
- Only Terraform and the lead's shell see them. The running app never does: it uses instance profiles. `infra/local/proc.sh` strips every `AWS_*` variable from app processes (W0-03b), and `internal/ops` builds an AWS client only when `APP_ENV` is `staging` or `prod`, with an explicit EC2 instance-role (IMDS) credential provider and never the SDK's default chain (§3.6, 02 §10).
- The first Terraform resource is an AWS Budgets alarm (§6).
- CI deploys use GitHub OIDC: `deploy-backend.yml`'s deploy job assumes `todayz-prod-github-deploy`, whose trust is `repo:todayzmart/todayz:environment:production` (D-254, the W0-06 checklist in §4). The account is 061292478202, and every provider refuses any other (D-255).

### 3.2 Compute (Elastic Beanstalk, Docker on Amazon Linux 2023, Graviton)

| | api | worker |
|---|---|---|
| Size | 2–6 × t4g.small, spread across 2 AZs | 1–3 × t4g.small |
| Scaling | Target tracking on the ALB's `RequestCountPerTarget`, starting at 1,000 per minute per target and tuned from the k6 results (W4-02), plus CPU at 60% | Step scaling on a custom CloudWatch metric (`TodayZ/Worker`): the oldest available River job's age above 30 s for 3 minutes. The worker publishes the value itself (§3.6) |
| Deploy policy | **Traffic splitting**: 10% of traffic to the new version for 10 minutes; rolls back automatically if health degrades | **Immutable** |
| Health check | `/readyz` on the app port | `/readyz` on the internal port 8181 (Wave 5 decides how the environment's health check reaches it) |

- Enhanced health reporting, an instance profile with least privilege, logs to CloudWatch, plus the OTel exporter.
- Migrations run as a separate step (`todayz migrate up`, an ECS Fargate ops task connecting as `todayz_owner`, D-254) before the new version deploys. They must be backward compatible with the previous app version: expand, migrate, contract.
- The worker serves `/healthz`, `/readyz` and `/version` on its internal port 8181.
- **Release mechanics (D-254).** One image runs api, worker and migrate. The Beanstalk bundle is a compose file with host networking and no nginx (`ProxyServer none`). `scripts/deploy/beanstalk.sh` builds, pushes, migrates, creates the application versions and deploys the worker, then the api. Terraform creates the environments once (two applies on the first deploy) and ignores the version label afterwards.

### 3.3 Load balancer

An Application Load Balancer with HTTPS through ACM. Its security group accepts Cloudflare IP ranges only (02 §2.4 step 5). Access logs go to S3. Deregistration delay: 75 s (§3.7).

Target group health check (`/readyz`): interval 5 s, timeout 4 s, unhealthy threshold 2, healthy threshold 2. A draining instance (`/readyz` 503 on SIGTERM) leaves rotation after two failed checks, about 10 s, inside the 15 s `SHUTDOWN_PRESTOP` (§3.7, SC-GS-10).

### 3.4 Database (RDS prod)

- PostgreSQL 16 + PostGIS on **db.t4g.medium, Single-AZ**: a single instance, no standby (founder's choice, to save the cost of a standby).
- gp3 storage at 100 GB, autoscaling to 500 GB.
- Automated backups and PITR for 14 days, deletion protection, and a weekly cross-region snapshot copy.
- CloudWatch Database Insights (the successor to Performance Insights) in standard mode, Enhanced Monitoring every 60 s, and Postgres logs exported to CloudWatch.
- Parameter group: `pg_stat_statements`, `log_min_duration_statement=500ms`, `idle_in_transaction_session_timeout=60s`, `statement_timeout` per role.
- No RDS Proxy: pgx pools are sized per instance. Revisit above 6 api instances.
- Upgrade to db.m7g.large when CPU stays above 60% or the burst balance drains.
- **Trade-off (D-96).** Orders, timers and money live in this database. An AZ failure means a restore from PITR, which takes an estimated 30-60 minutes. Moving to Multi-AZ is a single Terraform flag (`multi_az = true`), applied in a maintenance window, when the founder decides. A-22 shows "Multi-AZ: off" as a fact, not a warning.
- Database bootstrap (Wave 5): the RDS bootstrap applies the same database-level guards as `infra/local/postgres/init.sql` before the first migration: roles `todayz_owner` and `todayz_app`, and `REVOKE TEMPORARY ON DATABASE ... FROM PUBLIC`. The W0-03 audit tests rely on them.

### 3.5 Network, staging and Redis

- Region `ap-south-1` (Mumbai) for data residency and latency.
- Network: VPC with 2 AZs, public subnets for the ALB only, private subnets for Beanstalk and RDS, NAT gateway (one at launch to save cost), VPC endpoints for S3/ECR/Secrets Manager.
- **Staging:** the same shape, smaller and single-AZ: api 1 × t4g.micro, worker 1 × t4g.micro, RDS db.t4g.small. It can be stopped off-hours on a schedule.
- **Redis:** none in v1 (02 §2.3). Add ElastiCache Serverless (Valkey) only on a measured trigger: rate-limit or cache queries take more than 20% of database load in Database Insights, or `LISTEN/NOTIFY` fan-out limits SSE.

### 3.6 Observability and the admin System screen

- Keep Grafana Cloud for app telemetry. It is the same LGTM stack as local, so dashboards and alerts port one for one. AWS resource metrics and logs stay in CloudWatch, added as a Grafana Cloud datasource through a read-only IAM role (W5-03).
- Not Amazon Managed Grafana: it needs IAM Identity Center, costs per editor, and would duplicate the dashboards. Revisit if the founder prefers everything in AWS.
- A-22 System (D-97) reads AWS through the **API instance role**, with a read-only policy: `cloudwatch:GetMetricData`, `cloudwatch:DescribeAlarms`, `elasticbeanstalk:Describe*`, `rds:DescribeDBInstances`, `pi:GetResourceMetrics`, `pi:DescribeDimensionKeys`. Actions that cannot be scoped to a resource (the CloudWatch reads, `Describe*`) use `Resource: "*"`; the others are scoped to TodayZ resources.
- AWS keys stay out of the app. `internal/ops` builds an AWS client only when `APP_ENV` is `staging` or `prod`, with an explicit EC2 instance-role (IMDS) credential provider, never the SDK's default chain (which would pick up `AWS_ACCESS_KEY_ID` from the environment). `proc.sh` also strips every `AWS_*` variable from app processes. Locally, A-22 says "Not configured (local)" and makes no AWS call, even when `AWS_ACCESS_KEY_ID` is set in `.env.local` (SC-OPS-09).
- The no-write check (W1-10) is an **action allow-list**, not a wildcard-resource rule: the API role's policy may contain only `Describe*`, `Get*`, `List*` and the named reads above. A checkov or custom check in Terraform fails on any other action.
- The worker publishes the age of the oldest available River job with `cloudwatch:PutMetricData`, restricted by a `cloudwatch:namespace` condition to `TodayZ/Worker`. It does so only when `APP_ENV` is `staging` or `prod`. This is the one write the worker role holds. W5-04 owns the metric and the policy; W1-07 exposes the value.

### 3.7 Graceful shutdown (D-98)

**Rule (founder): no deploy, scale-in or restart cuts off an API request or a running job.** The process behaviour is in 02 §2.8. The timings Wave 5 must set:

| Setting | Value | Why |
|---|---|---|
| `SHUTDOWN_PRESTOP` (api) | 15 s deployed (1 s locally) | The ALB health checks and deregistration stop new traffic before the server closes |
| `SHUTDOWN_DRAIN` (api) | 75 s | Must exceed `WriteTimeout` (60 s) |
| ALB deregistration delay | 75 s | Matches the drain |
| Container stop timeout, api | 120 s | Pre-stop plus drain, with margin |
| Worker `DrainGrace` | 90 s | More than the 60 s job cap (`Batched` kinds yield) |
| Worker `StopAndCancel` | 30 s to return | Last resort; logged, counted in `tz_job_hard_cancelled_total`, alerted; the attempt is retried |
| Container stop timeout, worker | 180 s | `DrainGrace` 90 s + `StopAndCancel` 30 s + margin |

- Deploys are immutable for the worker: new instances are healthy before old ones get SIGTERM, and River's leader election hands periodic scheduling over. The api uses traffic splitting, so old instances drain behind the load balancer.
- Jobs must be safe to re-run (02 §2.5, §2.8). Side effects (an SMS, a push, a ledger post) carry an idempotency key.
- A Terraform or `infra` check asserts the deregistration delay, both stop timeouts and the target group health check of §3.3 (SC-GS-10).
- The worker's stop timeout has to hold on the host too: W5-04 checks the stop timeout of the Docker unit on the worker instance. If it is under 180 s, set it, or add an Auto Scaling termination lifecycle hook that keeps a scaling-in instance alive until the drain ends.

### 3.8 Estimated monthly cost (D-258; to be re-checked with the AWS Pricing Calculator and the first bill)

Bottom-up from ap-south-1 list prices; the per-resource table is in `docs/qa/deploy-prep/RUNBOOK.md` §6. It replaces the earlier ₹13–17k, which left out the interface endpoints, public IPv4, Secrets Manager, logs and the DR copy.

| Item | About ($/month) |
|---|---|
| RDS db.t4g.medium Single-AZ, 100 GB gp3, backups, monitoring | 72 |
| 3 × t4g.small (api 2, worker 1) with root volumes | 40 |
| ALB | 24 |
| NAT gateway | 44 |
| Interface endpoints (ECR api/dkr, Secrets Manager, 2 AZs) | 48 |
| Public IPv4, Secrets Manager (23), CloudWatch, DR copy, ECR/S3 | 49 |
| **Prod total** | **about $270–300 a month before GST (≈ ₹27–30k with 18% GST on an AISPL account)** |
| **Staging** | not deployed (D-210) |

Cloudflare free or pro plan, Grafana Cloud and Sentry free tiers at first.

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

**Repo split checklist (W0-06).** When the user creates the repos:

1. **CODEOWNERS** for:
   - `.github/**`, `scripts/ci/**` and `mk/90-ci.mk`;
   - the scanner configs (`.gitleaks.toml`, `scripts/ci/gitleaks-dir.toml`, `e2e/security/**`);
   - `scripts/ci/wflint/allowed-owners.txt` and `id-token-allow.txt`.
2. **Branch protection on main:**
   - one required review, including code-owner review;
   - stale approvals dismissed;
   - no force push.
3. **Required checks:** `ci / check`, `e2e`, `security / static`.
4. **Actions settings:** "Require approval for all outside collaborators".
5. **Runners:** GitHub-hosted only. wflint rejects `self-hosted`.
6. **Wave 5 OIDC:**
   - the AWS role trust scopes `sub` to `repo:<org>/<repo>:environment:production`;
   - `id-token: write` is allowed only in the deploy jobs listed in `scripts/ci/wflint/id-token-allow.txt`;
   - those jobs never run `make setup` or any repo script that installs tools.

## 5. Wave 5 steps (after credentials)

**Since D-210 there is no staging**, so steps 2–4 below are history. The prod first deploy is the 13 steps of `docs/qa/deploy-prep/RUNBOOK.md` §4:
1. state bucket;
2. account root;
3. first image;
4. prod apply #1;
5. Cloudflare apply #1;
6. secrets;
7. database bootstrap;
8. migrate;
9. application versions;
10. prod apply #2;
11. Cloudflare apply #2;
12. smoke test;
13. CI on.

The founder's yes on the prod plan summary comes before step 4 (D-254).

1. The user adds credentials to env (names listed in `.env.example` under "Cloud"). The lead runs `make env-check` and confirms only `set`/`missing`.
2. The first Terraform resource is the AWS Budgets alarm (§6). Then the first `terraform plan` ever run, for staging; the user reviews the plan summary (resources and estimated cost) and says yes.
3. Apply staging; run migrations; seed demo; deploy apps; point `staging.*` DNS.
4. Run `make e2e`, `make load` (staging-sized), `make security` and the ★ manual scripts against staging. Fix, repeat.
5. The user confirms the production plan (cost, domain cut-over time).
6. Apply prod; seed reference data (categories, places, master catalogue, admin users); DNS cut-over for `todayz.in` (old site URLs 301 to new); enable monitoring and alerts.
7. Hyper-care for 72 h: dashboards 00/01/08 watched, daily report to the user.

## 6. Launch checklist (prod)

- [ ] All Wave 4 evidence green; zero P0/P1 defects open.
- [ ] Restore drill done on staging (PITR to a new instance, reconcile passes), with the time noted against the 30-60 minute estimate for an AZ failure (§3.4).
- [ ] Alerts route to the founders' phones; a test alert received.
- [ ] Runbooks linked from every alert.
- [ ] Graceful-shutdown timings set and checked (§3.7); a deploy during a load run drops no request.
- [ ] A-21 Jobs shows every scheduled kind with a recent succeeded run; A-22 System shows the AWS panels with data.
- [ ] Rate limits and WAF rules on; Turnstile keys set; OTP via real SMS verified on Jio, Airtel and Vi numbers.
- [ ] DLT registration for the SMS sender ID and templates done (India TRAI requirement; AWS End User Messaging sends with the DLT entity and template ids, D-212, `docs/runbooks/aws-founder-steps.md` part C).
- [ ] SMS smoke on the first prod deploy, before any real send (D-213):
  - From an SSM session on the api instance and on the worker instance, using the instance role, run `aws pinpoint-sms-voice-v2 send-text-message --dry-run` with the sender ID as `--origination-identity`, a sandbox-verified number and both DLT ids. It must succeed, which proves the policy's IAM resource types (`sender-id`, `opt-out-list`). `AccessDeniedException` means a resource type is missing from `policies/sms-send.json`.
  - The app's first OTP must not log `credentials: imds token timeout`. Beanstalk has no IMDS hop-limit option, so we rely on the AL2023 default of 2, and this proves it from inside the container.
- [ ] Privacy policy, terms, grievance officer contact live; consent text in sign-up.
- [ ] Super admin accounts with TOTP; no shared accounts.
- [ ] `platform_settings` reviewed by the founders (timers, credit caps, referral amounts).
- [ ] Old site redirect map tested; sitemap submitted to Search Console.
- [ ] Sentry releases and source maps working; Grafana dashboards show prod data.
- [ ] Cost alarms (AWS Budgets at ₹30k/month for prod and staging together, alerts at 80% actual and 100% forecast, created first; Anthropic/SMS spend alerts).
- [ ] First 10 shops onboarded and verified with real catalogues before public announcement.

## 7. Rollback

- App: `scripts/deploy/beanstalk.sh deploy <older tag>` moves the worker, then the api, back to that release's application versions (D-254). Pages rolls back to the previous deployment.
- DB: forward-only migrations designed expand/contract, so the previous app version works against the new schema. Destructive changes need a separate later migration. Data repair uses PITR to a side instance and copies back only the affected rows.
- Feature flags in `platform_settings` turn off risky features without a deploy (D-25):
  - `feature_delivery_enabled`, `feature_bill_qr_enabled`, `feature_voice_enabled`, `feature_offers_enabled`, `feature_banners_enabled`
  - one `ai_<feature>_enabled` per AI feature (list in [11-ai-features.md](11-ai-features.md) §1)
  - `ai_daily_cost_cap_paise` stops AI calls for the day once spend reaches it
