# Worker unhealthy

CloudWatch alarm `worker-environment-health` (`todayz-prod-worker-environment-health`) in
`infra/terraform/modules/stack/alarms.tf`: Beanstalk environment `todayz-prod-worker` health worse
than Ok (`EnvironmentHealth` above 15) for 5 minutes. Sent to the SNS topic `todayz-prod-alarms`
(email). Cloud only. Locally, the worker's health is `/readyz` on port 8181 (loopback).

## What it means

The worker runs every background job: order timers, bill claim expiry, ledger reconcile, payouts,
search refresh, notifications. If no worker instance is healthy, nothing moves on time: orders do
not expire, holds are not released, and pushes are not sent. The job alerts follow within minutes
([Order timers late](queue-order-timers-late.md), [Job backlog](queue-job-backlog.md),
[Cron missed](jobs-cron-missed.md)).

## First five minutes

1. Beanstalk → `todayz-prod-worker` → Health: instances and the cause text. Is a deploy running? The worker deploys immutably; a short dip during a deploy is expected.
2. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs) (Grafana Cloud in prod): are jobs still running? Oldest available job age?
3. Jobs stopped and no deploy is running: open an [incident note](../ops/incident-template.md) and tell the lead.

## Diagnosis

1. **Health detail** (lead's shell, read-only): `aws elasticbeanstalk describe-environment-health --environment-name todayz-prod-worker --attribute-names All` and `aws elasticbeanstalk describe-events --environment-name todayz-prod-worker --max-items 30`.
2. **Worker logs:** Loki `{service_name="todayz-worker"}`: boot lines, `readyz`, River leader election, panics.
3. **Leader:** River's periodic jobs run on the elected leader only. Many kinds *Missed* at once with the worker up means no leader (often database connections; see [Database connections](database-connections.md)).
4. **Database:** [03 · Database](https://grafana.todayz.localhost/d/todayz-03); the worker cannot be ready without it.

### Likely causes

- A bad release that fails at start (a job kind registration error, a missing secret).
- The database unreachable or out of connections.
- Out of memory from one job kind (a large batch).
- A deploy in progress (expected).

## Mitigation

- Bad release: roll the worker back to the previous version label ([Release checklist → Rollback](../ops/release-checklist.md#rollback)). The api can stay on the new version; releases are expand/contract.
- One instance stuck: Beanstalk → Instances → terminate it; it is replaced, and River hands periodic jobs to the new leader.
- A job kind eating memory: pause it in A-21 (never a `timer` kind) and tell its owner.
- Never edit `river_job` or `job_runs` rows by hand. Once the worker is back, the timer sweeps catch up on their own.

## Escalation

**Level:** SEV1 if no worker instance is healthy for more than 5 minutes (timers stop); SEV2 if reduced; SEV3 if it cleared after a deploy ([Severity levels](../ops/severity.md)).

- The **lead** at once.
- The owner of a job kind that caused it.

## Follow-up

- Check the sweeps caught up: no orders past their deadline, no holds left after expiry, ledger reconcile clean.
- The test or config check for the failed start.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
