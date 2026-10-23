# infra/terraform

AWS for TodayZ, the D-96 shape (09 section 3). **Nothing here has been planned or applied.** Before
Wave 5 the only commands are `terraform fmt -check`, `terraform validate` (after
`init -backend=false`), tflint and checkov (`make check`, `make security`), plus the offline policy
checks in `checks/`.

```
envs/account   AWS Budgets alarm (first resource, 80% actual / 100% forecast) and the ECR repository
envs/staging   the stack, smaller (t4g.micro x2, db.t4g.small), single-AZ, no deletion protection
envs/prod      the stack: api 2-6 x t4g.small, worker 1-3 x t4g.small, db.t4g.medium Single-AZ
modules/stack  composes the rest for one environment; timings.tf holds the shutdown timings
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

## Wave 5 order (for the lead)

1. `envs/account` (budget first, then ECR). Push the first image.
2. `envs/staging`: apply `-target=module.stack.module.edge.aws_acm_certificate.api` first, create the
   validation CNAMEs in `infra/cloudflare` (output `certificate_validation_records`), then the rest.
3. Bootstrap the database (roles and `REVOKE TEMPORARY`, as `infra/local/postgres/init.sql`), put
   the secret values into Secrets Manager out of band, run `todayz migrate up` as a one-off.
4. Then `envs/prod` the same way after the user confirms the production plan.

## Open items

- Worker tier: Beanstalk's worker environment provisions an SQS queue and `sqsd` that this app does not
  use; the River worker health on :8081 is not what Beanstalk checks. W5-04 decides the final health
  path (09 section 3.2).
- Container stop timeouts (120 s / 180 s) belong in the application bundle's compose file
  (`stop_grace_period`); here they are passed as `CONTAINER_STOP_TIMEOUT_SECONDS` and checked.
- The target-tracking scaling policy finds Beanstalk's target group by the
  `elasticbeanstalk:environment-name` tag; confirm in the first plan.
- Alarm descriptions link runbooks in `docs/runbooks/` that do not exist yet.
