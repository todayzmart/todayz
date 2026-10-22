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

## Status

Stub from W0-01. The WP that builds this folder fills in layout and conventions.
