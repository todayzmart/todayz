# infra/

Terraform (AWS + Cloudflare), Grafana dashboards and alerts as code, local stack config (`infra/local/`).

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 09-infra-release.md; 05-observability.md.

## Run

- Local stack: `make dev` / `make stop` / `make logs` (docker-compose.yml + infra/local)
- Terraform in `make check`: `fmt -check`, `validate`, tflint (Docker image), checkov only

## Rules for this folder

- Only the lead runs `terraform plan` or `apply` or creates cloud resources (D-210), with `TODAYZ_WAVE5=1 scripts/ci/terraform.sh` and the creds in `.env.cloud.local`. The first prod apply waits for the founder's yes on the plan summary. Worker sessions never run plan or apply.
- Never print AWS, RDS, Beanstalk or Cloudflare credentials; refer to variables by name.
- Dashboards: `grafana/dashboards/<nn>-<name>.json`; alerts: `grafana/alerts/<area>.yaml`; each alert links a runbook in `docs/runbooks/`.

## Layout

- `terraform/` AWS: `envs/{account,prod}` roots (`envs/staging` is unused: there is no staging, D-210), `modules/*`, `checks/` (offline IAM allow-list and timings checks, `make check-infra`). See `terraform/README.md`.
- `cloudflare/` DNS, WAF, rate limits, R2, Turnstile, cache rules, Pages. See `cloudflare/README.md`.
- `local/`, `grafana/`: local stack and dashboards.
- The API's read-only ops policy is a JSON file checked against an action allow-list (SC-OPS-05); change it only with its test.
- Shutdown and health-check timings live in `terraform/modules/stack/timings.tf` as plain literals (SC-GS-10).

## Status

W1-10 built `terraform/` and `cloudflare/`. The four Pages apps are live; the AWS backend is not yet applied (see `docs/guide/deployment.md`).

W1-07: Grafana dashboards 01-04 are in `grafana/dashboards/` (generated from a script, kept as JSON). They use
the datasource uids `prometheus`, `loki`, `tempo` (provisioned by otel-lgtm) and `todayz-postgres`.
`backend/internal/platform/telemetry/dashboards_test.go` checks they query only registered `tz_` metrics.
