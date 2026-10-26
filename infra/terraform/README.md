# infra/terraform

AWS for TodayZ, the D-96 shape (09 section 3), account **061292478202** (every provider has
`allowed_account_ids`, D-255). **Nothing here has been planned or applied yet.** CI and worker sessions
run only `terraform fmt -check`, `terraform validate` (after `init -backend=false`), tflint and checkov
(`make check`, `make security`), plus the offline policy checks in `checks/`. Plan and apply are the
lead's, through `TODAYZ_WAVE5=1 scripts/ci/terraform.sh`, following
`docs/qa/deploy-prep/RUNBOOK.md`.

```
envs/account   AWS Budgets alarm (first resource, 80% actual / 100% forecast), the ECR repository,
               the GitHub Actions OIDC provider
envs/staging   unused since D-210 (prod only); kept validating, has no backend
envs/prod      the stack: api 2-6 x t4g.small, worker 1-3 x t4g.small, db.t4g.medium Single-AZ
modules/stack  composes the rest for one environment; timings.tf holds the shutdown timings;
               ops.tf the Fargate ops tasks (db bootstrap, migrate); deploy.tf the app-versions
               bucket and the GitHub deploy role
modules/network   VPC, 2 AZs, public/private subnets, one NAT, S3/ECR/Secrets Manager endpoints, flow logs
modules/database  RDS Postgres 16 (+ PostGIS at bootstrap), gp3 100 -> 500 GB, PITR 14 d, Database
                  Insights standard, Enhanced Monitoring 60 s, logs to CloudWatch, `multi_az` flag
                  (default false), weekly AWS Backup copy to a second region
modules/secrets   Secrets Manager containers (no values, ever)
modules/ecr       image repository, immutable tags, scan on push
modules/edge      ALB security group (Cloudflare ranges only), ALB access-log bucket, ACM certificate
modules/beanstalk_env  Docker / AL2023 / Graviton environment (role api or worker)
checks/        offline checks and their tests (python3, standard library)
```

## Checks

`python3 -I checks/check.py all` (also `make check-infra`):

- **SC-OPS-05**: `modules/stack/policies/api-ops-readonly.json` is the API role's policy. The check
  is an **action allow-list** (only `Describe*`, `Get*`, `List*` and the six named reads), not a
  wildcard-resource rule; a read on `Resource: "*"` passes and `rds:ModifyDBInstance` fails.
- The worker policy has one statement: `cloudwatch:PutMetricData` with `cloudwatch:namespace` =
  `TodayZ/Worker`.
- **SC-GS-10**: `modules/stack/timings.tf` (deregistration 75 s, api stop 120 s, worker stop 180 s,
  DrainGrace 90 s + hard-cancel 30 s below 180 s, health check 5 s / 4 s / 2 / 2) and that
  `main.tf` wires those locals into the api environment.

`checks/test_check.py` seeds violations and proves each is caught.

## State

One S3 bucket, `todayz-tfstate-061292478202`, made by `scripts/deploy/state-bucket.sh` (D-255). The
roots declare `backend "s3" {}` and take bucket, key (`account/`, `prod/`,
`cloudflare/terraform.tfstate`), region and `use_lockfile=true` through `-backend-config` at init
(the S3 native lockfile, no DynamoDB). `init -backend=false` for the static checks is unchanged.

## First deploy (for the lead)

`docs/qa/deploy-prep/RUNBOOK.md` has every step, every variable and where its value comes from, the
founder's inputs, the cost and rollback. In short (D-254):

1. state bucket;
2. `envs/account`;
3. build and push the first image (`scripts/deploy/beanstalk.sh build`, then `push`);
4. `envs/prod` with `app_environments_enabled=false`;
5. Cloudflare with the imports and the ACM records;
6. secrets (`scripts/deploy/secrets.sh`);
7. `beanstalk.sh db-bootstrap`, `migrate`, `release`;
8. `envs/prod` with `app_environments_enabled=true`;
9. Cloudflare with `api_origin_hostname`.

From then on, a release is `scripts/deploy/beanstalk.sh` (CI: `.github/workflows/deploy-backend.yml`),
not Terraform.

## Open items

- Worker tier: Beanstalk's worker environment provisions an SQS queue and `sqsd` that this app does not
  use; the River worker health on :8181 is not what Beanstalk checks. W5-04 decides the final health
  path (09 section 3.2).
- Container stop timeouts (120 s / 180 s) are passed as `CONTAINER_STOP_TIMEOUT_SECONDS` and checked
  here; the compose file `beanstalk.sh release` writes reads them as `stop_grace_period`.
- The target-tracking scaling policy finds Beanstalk's target group by the
  `elasticbeanstalk:environment-name` tag; confirm in the first plan.
- Alarm descriptions link runbooks in `docs/runbooks/` that do not exist yet.
