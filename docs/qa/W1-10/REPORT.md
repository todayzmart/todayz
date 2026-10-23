# W1-10 Infrastructure as code (not applied): QA report

Branch `worktree-agent-aed58796714ea7e6f`, rebased by the lead onto `main` and merged on 2026-10-23. Nothing was planned or applied, no credentials were used, and nothing talked to AWS or Cloudflare.

## What was built

- **Image:** `backend/Dockerfile` and `backend/.dockerignore`.
  - A multi-stage Go build into `distroless/static-debian12:nonroot`, running as user 65532.
  - Entrypoint `/todayz`, default command `api`, ports 8080 and 8081.
  - Needs `make gen` first, because `backend/gen` is git-ignored.
- **Terraform modules** in `infra/terraform/modules/`: network, database, secrets, ecr, edge, beanstalk_env, stack. They follow the D-96 shape:
  - api runs on 2–6 t4g.small and the worker on 1–3 t4g.small;
  - RDS PG16 on db.t4g.medium, Single-AZ behind a `multi_az` flag;
  - a weekly cross-region backup copy;
  - an ALB restricted to the Cloudflare ranges, with its `/readyz` health check at 5/4/2/2;
  - alarms and Budgets.
- **Policies and timings:**
  - The A-22 read-only policy and the worker's `PutMetricData` policy (limited to namespace `TodayZ/Worker`) are in `modules/stack/policies/`.
  - The SC-GS-10 timings are in `modules/stack/timings.tf`.
- **Environment roots** in `infra/terraform/envs/`:
  - `account`: Budgets first, then ECR.
  - `staging`: smaller, all-at-once deploys, no deletion protection.
  - `prod`.
- **Offline checks** in `infra/terraform/checks/` (`check.py`, `test_check.py`):
  - the SC-OPS-05 action allow-list (`Describe*`, `Get*`, `List*` plus six named reads);
  - the worker policy has exactly one statement, with the namespace condition;
  - the SC-GS-10 timings and their wiring.
- **Cloudflare** in `infra/cloudflare/`: zone TLS, DNS, Pages projects and domains, R2 with `img.` and `tiles.` domains, the Turnstile widget, WAF, rate limits and cache rules.
- **Make steps:** `mk/50-infra.mk` adds `check-infra` (offline) and `setup-infra-cloudflare` (network, prefetch only).
- **Docs:** `infra/terraform/README.md` and `infra/cloudflare/README.md` cover the layout, the checks and the Wave 5 apply order.

## Evidence

| Check | Result |
|---|---|
| `docker build` of `backend` (after `make gen`) | ok; image about 9.5 MB, user 65532:65532 (agent) |
| Image against a throwaway PostGIS container on its own network (API on 18080; the shared 5433 stack untouched) | `migrate up` ok (head 12); `/readyz` 200 with db, migrations and river ok; `docker stop` drained cleanly, exit 0 (agent) |
| `terraform fmt -check -recursive infra` | ok (agent) |
| Policy and timing checks: 16 unit tests, plus `check.py all` | ok (agent, lead) |
| `make check-tf-static` (fmt, validate of the three roots and the modules, tflint, checkov on `infra/terraform`) | exit 0. checkov: 370 passed, 0 failed, 92 skipped (18 distinct `#checkov:skip` comments, each with its reason) |
| `make check-infra` (16 unit tests, `check.py all`, fmt, validate, tflint, checkov on `infra/cloudflare`) | exit 0 |
| `make check` in the agent's worktree | stopped at `check-backend` because that worktree had no govulncheck database (`.cache/govulndb`). Every other backend step passed. The lead reran the steps on main after the merge. |

## Not verified

- Terraform `validate`, tflint and checkov now run offline (see Evidence). Resource and attribute names were still written for AWS provider 6.x and Cloudflare provider 5.x, so only a real `plan` in Wave 5 proves them.
- Scaling, alarms, the ALB and traffic-splitting deploys need a real account (Wave 5).

## checkov notes

- Fixed in code: managed platform updates on (CKV_AWS_340, `aws:elasticbeanstalk:managedactions` as static settings), zone identity pinned by name (CKV_AWS_394).
- Skipped with a reason: CKV_AWS_312 (enhanced health is on through `SystemType`; the check looks for `HealthStreamingEnabled` in a namespace Beanstalk does not define it in, so health streaming is set in `aws:elasticbeanstalk:cloudwatch:logs:health`), CKV_AWS_293 (prod keeps deletion protection on, only staging turns it off), CKV2_AWS_5 twice (both security groups are attached through Beanstalk option settings, which static analysis cannot see).
- Judgement call to confirm: CKV2_AWS_57 (no secret rotation Lambda, keys rotate by runbook) and the single-AZ skip CKV_AWS_157 (D-96).
- Module lock files are git-ignored; only the env roots pin providers.

## Open

1. **Worker environment:** Beanstalk's worker tier brings an SQS queue and `sqsd`, which this app does not use. Its health check is not the River worker's `:8081`. W5-04 decides.
2. **Container stop timeouts** (120 s and 180 s) are passed as `CONTAINER_STOP_TIMEOUT_SECONDS`. The real stop grace setting is W5-04's.
3. **Target-tracking:** the scaling policies find the target group by the `elasticbeanstalk:environment-name` tag. Confirm this in the first plan.
4. **Worker scaling metric:** the name `OldestAvailableJobAgeSeconds` is a placeholder until W5-04 and W1-07 fix it.
5. **Runbooks:** the alarm descriptions link runbooks that do not exist yet.
6. **ACM validation records live in Cloudflare,** so Wave 5 applies in two steps: the certificate first, then the rest.
7. **WAF:** the default Cloudflare WAF ruleset is the Free plan's.

## Questions for the founder or lead

- **Budget:** AWS Budgets is set in USD, but the plan says ₹30k a month. The variable defaults to 30000 with no conversion, so it must be set to the right USD amount (or INR, if the account allows it) before Wave 5.
- **Rate limits:** the plan gives no numbers. These starting values are marked in the code:
  - 20 per minute for `/v1/auth/`;
  - 600 per minute for `/v1/`.
- **Staging hostnames:** they use a hyphen (`app-staging.todayz.in`), because Universal SSL covers only one wildcard level. `app.staging.todayz.in` would need Advanced Certificate Manager.

## Files outside the owned paths

- `backend/.dockerignore` (new)
- `mk/50-infra.mk` (new)
- `infra/CLAUDE.md` (Layout and Status sections)
