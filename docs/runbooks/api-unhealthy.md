# API unhealthy

CloudWatch alarms in `infra/terraform/modules/stack/alarms.tf`, sent to the SNS topic
`todayz-prod-alarms` (email). Cloud only.

- `alb-unhealthy-hosts` (`todayz-prod-alb-unhealthy-hosts`): at least one api target failing `/readyz` for 3 minutes.
- `api-environment-health` (`todayz-prod-api-environment-health`): Beanstalk environment `todayz-prod-api` health worse than Ok (`EnvironmentHealth` above 15) for 5 minutes.

## What it means

One or more api instances are out of service. The load balancer checks `/readyz` every 5 s and
takes an instance out after two failures (09 §3.3). `/readyz` fails when the instance cannot reach
the database, is draining on shutdown, or has not finished starting. With fewer instances, the rest
carry the load; with none, every request gets a 502/503 from the load balancer.

## First five minutes

1. Beanstalk → `todayz-prod-api` → Health: how many instances, which are not Ok, and the cause text.
2. Is a deploy running? Traffic-split deploys take new instances in and out; an alarm that clears when the deploy ends is expected. A deploy that fails health rolls back on its own.
3. All instances down: open an [incident note](../ops/incident-template.md), tell the lead, and check the database first ([RDS CPU](rds-cpu.md), [Database connections](database-connections.md)).

## Diagnosis

1. **Health detail** (lead's shell, read-only): `aws elasticbeanstalk describe-environment-health --environment-name todayz-prod-api --attribute-names All` and `aws elasticbeanstalk describe-instances-health --environment-name todayz-prod-api --attribute-names All`.
2. **Events:** `aws elasticbeanstalk describe-events --environment-name todayz-prod-api --max-items 30` (deploys, scaling, failed health checks).
3. **App logs:** Loki `{service_name="todayz-api"} |= "readyz"` and the boot lines of the newest instance. A boot that stops at config means a missing secret name.
4. **Targets:** EC2 → Target groups → the api group → Targets: the health check's reason (timeout, 503).
5. [01 · Service overview](https://grafana.todayz.localhost/d/todayz-01): instance count and restarts.

### Likely causes

- The database is unreachable or slow, so `/readyz` fails everywhere at once.
- A bad release that fails at start (missing secret, failed config check).
- Out of memory on one instance (it restarts in a loop).
- A deploy in progress (expected, clears on its own).

## Mitigation

- Bad release: roll back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)).
- Missing secret: add it in Secrets Manager and redeploy the same version ([Secret rotation](../ops/secret-rotation.md)).
- One instance stuck: Beanstalk → Instances → terminate it; the Auto Scaling group replaces it.
- Too few instances for the load: raise the minimum in Beanstalk → Configuration → Capacity, then bring Terraform (`api_min_size`) in line the same day.

## Escalation

**Level:** SEV1 if no healthy instance is left; SEV2 if capacity is reduced and latency rises; SEV3 if it cleared after a deploy ([Severity levels](../ops/severity.md)).

- The **lead** at once for SEV1 or SEV2.
- The **founder** if the AWS account itself has a problem (limits, billing).

## Follow-up

- The cause of the failed start, fixed and tested (a config check in `make check`, if it was config).
- If an instance ran out of memory, its size or the leak.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
