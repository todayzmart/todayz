# API 5xx at the load balancer

CloudWatch alarm `alb-target-5xx` (`todayz-prod-alb-target-5xx`) in
`infra/terraform/modules/stack/alarms.tf`: more than 10 `HTTPCode_Target_5XX_Count` a minute for 5
minutes. Sent to the SNS topic `todayz-prod-alarms` (email). Cloud only.

## What it means

The api instances behind the load balancer answered more than 10 requests a minute with a 5xx, for
5 minutes. This is the AWS view of the same failure that [API availability burn](api-availability-burn.md)
(`tz-api-availability-fast-burn`) sees from the app's own metrics. If Grafana Cloud is down or the
app cannot export metrics, this alarm still fires.

## First five minutes

1. Open [00 · What to fix today](https://grafana.todayz.localhost/d/todayz-00) (Grafana Cloud in prod): which routes fail? If Grafana shows nothing, the app may not be exporting: go to step 2.
2. Beanstalk → `todayz-prod-api` → Health: are instances Ok, and did a deploy just run?
3. A deploy just ran and 5xx started with it: roll back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)). Open an [incident note](../ops/incident-template.md).

## Diagnosis

1. **By route and code:** [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02); the problem `code` on the 5xx.
2. **Logs:** Loki `{service_name="todayz-api"} | json | status >= 500`, then follow `trace_id` into Tempo.
3. **Load balancer view** (lead's shell, read-only): `aws cloudwatch get-metric-statistics --namespace AWS/ApplicationELB --metric-name HTTPCode_Target_5XX_Count --dimensions Name=LoadBalancer,Value=<alb dimension> --statistics Sum --period 60 --start-time <iso> --end-time <iso>`. Compare with `HTTPCode_ELB_5XX_Count`: 502/503 from the load balancer itself means no healthy target ([API unhealthy](api-unhealthy.md)).
4. **Dependencies:** [03 · Database](https://grafana.todayz.localhost/d/todayz-03) and the provider panels; a 503 `unavailable` often means the database or a provider.

### Likely causes

- A bad release.
- The database is slow or out of connections ([Database connections](database-connections.md), [RDS CPU](rds-cpu.md)).
- A missing secret after a deploy (the app answers 503 "not configured").
- One instance unhealthy while still in rotation.

## Mitigation

- Bad release: roll back first, debug after.
- Missing secret: add it to Secrets Manager (`todayz/prod/<NAME>`) and redeploy the same version ([Secret rotation](../ops/secret-rotation.md)).
- Database: follow its runbook.
- A feature causing it: turn it off in `platform_settings` (A-17).

## Escalation

**Level:** SEV1 if the core loop fails (sign in, search, order); SEV2 if one route fails ([Severity levels](../ops/severity.md)).

- The **lead** at once for SEV1 or SEV2.
- The owner of the failing route's WP.

## Follow-up

- The test or contract check that would have caught it.
- If the app's metrics were missing during the incident, a fix for that too.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
