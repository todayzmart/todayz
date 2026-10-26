# Job backlog

Alert `tz-job-backlog` in `infra/grafana/alerts/queue.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

A River queue has had an available (not yet started) job waiting more than 2 minutes
(`tz_queue_oldest_available_age_seconds{queue}`). Work is arriving faster than the worker takes it,
or the worker is not taking any.

## First five minutes

1. Open [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): which queue and kind is piling up?
2. Is the worker up and taking jobs? If not, see [Worker unhealthy](worker-unhealthy.md).
3. If one non-timer kind floods the queue, pause it in A-21 with a reason.

## Diagnosis

1. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): queue depth and oldest age per queue, runs by kind (which kind is piling up).
2. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): worker up? running count against its concurrency.
3. Worker logs for errors or a stuck job (a duration near its timeout).

### Likely causes

- The worker is down or draining.
- One kind enqueued in bulk (a backfill, a broadcast).
- Jobs slowed by the database or a provider.

## Mitigation

- Restore the worker. A bulk enqueue drains on its own; pause a non-timer kind from A-21 Jobs if it starves others.

## Escalation

**Level:** Warning: SEV3. SEV2 if timer kinds are late too ([Order timers late](queue-order-timers-late.md)) ([Severity levels](../ops/severity.md)).

- **W1-11** (jobs) and the lead if timers are on the same queue.

## Follow-up

- If load grew, raise the worker's concurrency or count with the numbers.
- If one kind enqueued too much, fix that producer.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
