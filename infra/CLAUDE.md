# infra/

Terraform (AWS + Cloudflare), Grafana dashboards and alerts as code, local stack config (`infra/local/`).

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 09-infra-release.md; 05-observability.md.

## Run

- Local stack: `make dev` / `make stop` / `make logs` (docker-compose.yml + infra/local)
- Terraform before Wave 5: `fmt -check`, `validate`, tflint (Docker image), checkov only

## Rules for this folder

- No `terraform plan` or `apply`, and no cloud resources, before the user provides credentials for Wave 5.
- Never print AWS, RDS, Beanstalk or Cloudflare credentials; refer to variables by name.
- Dashboards: `grafana/dashboards/<nn>-<name>.json`; alerts: `grafana/alerts/<area>.yaml`; each alert links a runbook in `docs/runbooks/`.

## Layout

- `terraform/` AWS: `envs/{account,staging,prod}` roots, `modules/*`, `checks/` (offline IAM allow-list and timings checks, `make check-infra`). See `terraform/README.md`.
- `cloudflare/` DNS, WAF, rate limits, R2, Turnstile, cache rules, Pages. See `cloudflare/README.md`.
- `local/`, `grafana/`: local stack and dashboards.
- The API's read-only ops policy is a JSON file checked against an action allow-list (SC-OPS-05); change it only with its test.
- Shutdown and health-check timings live in `terraform/modules/stack/timings.tf` as plain literals (SC-GS-10).

## Status

W1-10 built `terraform/` and `cloudflare/` (not applied; the first plan is Wave 5).

W1-07: Grafana dashboards 01-04 are in `grafana/dashboards/` (generated from a script, kept as JSON). They use
the datasource uids `prometheus`, `loki`, `tempo` (provisioned by otel-lgtm) and `todayz-postgres`.
`backend/internal/platform/telemetry/dashboards_test.go` checks they query only registered `tz_` metrics.
