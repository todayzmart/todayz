# Job backlog

Alert `tz-job-backlog` in `infra/grafana/alerts/queue.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

A River queue has had an available (not yet started) job waiting more than 2 minutes
(`tz_queue_oldest_available_age_seconds{queue}`). Work is arriving faster than the worker takes it,
or the worker is not taking any.

## First three checks

1. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): queue depth and oldest age per queue, runs by kind (which kind is piling up).
2. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): worker up? running count against its concurrency.
3. Worker logs for errors or a stuck job (a duration near its timeout).

## Likely causes

- The worker is down or draining.
- One kind enqueued in bulk (a backfill, a broadcast).
- Jobs slowed by the database or a provider.

## Fix

- Restore the worker. A bulk enqueue drains on its own; pause a non-timer kind from A-21 Jobs if it starves others.

## Who to tell

- **W1-11** (jobs) and the lead if timers are on the same queue.
