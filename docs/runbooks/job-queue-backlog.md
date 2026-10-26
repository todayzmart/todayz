# Worker queue age (cloud)

CloudWatch alarms on the worker's oldest available River job (namespace `TodayZ/Worker`), sent to
the SNS topic `todayz-prod-alarms` (email). Cloud only.

- `worker-oldest-job-age-critical` (`todayz-prod-worker-oldest-job-age-critical`, `infra/terraform/modules/stack/alarms.tf`): older than 5 minutes for 5 minutes.
- `worker-oldest-job-age-high` (`todayz-prod-worker-oldest-job-age-high`, `infra/terraform/modules/stack/main.tf`): older than `worker_scale_out_age_seconds` (30 s) for 3 minutes. It also scales the worker out by one instance.

The Grafana alert on the same signal is [Job backlog](queue-job-backlog.md) (`tz-job-backlog`).

## What it means

Jobs are waiting longer than they should before a worker picks them up. The *high* alarm is
the early one: Auto Scaling adds a worker, and the alarm usually clears on its own. The *critical*
alarm means the backlog lasted 5 minutes anyway: either scaling has reached its maximum, or the
worker is not taking jobs at all. Timer jobs wait in the same queues, so orders and holds move late.

## First five minutes

1. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs) (Grafana Cloud in prod): which queue and kind is piling up?
2. Beanstalk → `todayz-prod-worker` → Health: instance count. At the maximum? Unhealthy? If unhealthy, go to [Worker unhealthy](worker-unhealthy.md).
3. *Critical* alarm: open an [incident note](../ops/incident-template.md) and tell the lead.

## Diagnosis

1. **Metric** (lead's shell, read-only): `aws cloudwatch get-metric-statistics --namespace TodayZ/Worker --metric-name <worker_oldest_job_age_metric_name> --statistics Maximum --period 60 --start-time <iso> --end-time <iso>`.
2. **Scaling:** `aws autoscaling describe-scaling-activities --auto-scaling-group-name <worker ASG> --max-items 10`: did the scale-out run, or hit the maximum?
3. **Which kind:** dashboard 15, runs by kind and duration; a kind with durations near its timeout holds workers.
4. Follow the checks in [Job backlog](queue-job-backlog.md#diagnosis).

### Likely causes

- A burst of enqueued work from one kind (a fan-out, a backfill).
- A slow kind holding all worker slots (a provider timing out, a slow query).
- Worker instances unhealthy or not leader.
- The scaling maximum is too low for the load.

## Mitigation

- One flooding, non-timer kind: pause it in A-21 with a reason; resume off-peak.
- A slow provider: the kind's own runbook; most fall back or retry later.
- Scaling at the maximum and the load is real: raise `worker_max_size` (Terraform, the lead; founder's yes for cost). In an emergency, Beanstalk → Configuration → Capacity, then Terraform the same day.
- Never pause, run or retry a `timer` kind by hand.

## Escalation

**Level:** *high*: SEV4, it scales on its own; SEV3 if it fires several times a day. *critical*: SEV2; SEV1 if order timers are minutes late ([Severity levels](../ops/severity.md)).

- The **lead** for *critical*.
- The owner of the kind that flooded the queue.

## Follow-up

- If one kind caused it, a batch size or rate limit in its producer.
- If scaling was too slow, tune `worker_scale_out_age_seconds` or the maximum with the numbers.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
