# Infrastructure

The local stack, Terraform for AWS and Cloudflare, and Grafana dashboards and alerts.

| Folder | What | Docs |
|---|---|---|
| `local` | Docker services, Caddy, process runner, health page, local env | [README](local/README.md) |
| `terraform` | AWS: account, prod stack, modules, policy checks | [README](terraform/README.md) |
| `cloudflare` | DNS, WAF, rate limits, R2, Turnstile, cache rules, Pages | [README](cloudflare/README.md) |
| `grafana` | 16 dashboards, 15 alert files; every alert links a runbook | [runbooks](../docs/runbooks/README.md) |

Prod URLs: todayz.in, app.todayz.in, business.todayz.in, admin.todayz.in on Cloudflare Pages; api.todayz.in on AWS (not applied yet). Local URLs and ports: [local-dev guide](../docs/guide/local-dev.md).

## Run

```bash
make dev                  # local stack
make stop
make check-tf-static      # fmt, validate, tflint, checkov (part of make check)
make check-runbooks       # every alert has a runbook
```

Before Wave 5 Terraform runs static checks only. `plan` and `apply` are refused unless `TODAYZ_WAVE5=1`, which only the lead sets, with the founder's yes on the plan summary (D-210). Secrets are never printed; use `make env-check`.

## More

- [Deployment guide](../docs/guide/deployment.md): pipeline, scripts, rollback
- [Local development](../docs/guide/local-dev.md)
- [Plan: infra and release](../docs/plan/09-infra-release.md)
- [Working rules for this folder](CLAUDE.md)
