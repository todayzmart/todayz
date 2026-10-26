# Order timers late

Alert `tz-order-timers-late` in `infra/grafana/alerts/queue.yaml` (05 §5, owner W3-07). Severity: **critical**.

## What it means

A timer-class job kind (order accept timeout, pickup no-show, partial timeout, bill claim
expiry, the reconcile sweep) ran more than 30 s after its scheduled time at p99 for 5 minutes.
Customers and shops see orders expire or release later than the app promised. The kind list is
not written here: the rule joins the lateness histogram with `tz_job_runs_total{class="timer"}`, so
a new timer kind is covered when it is registered.

## First five minutes

1. Open [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): lateness by kind, queue depth.
2. Is the worker up? If not, see [Worker unhealthy](worker-unhealthy.md). If a non-timer kind floods the queue, pause it.
3. Open an [incident note](../ops/incident-template.md) and tell the lead.

## Diagnosis

1. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): lateness p99 by kind, queue depth and the oldest available job age.
2. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): worker health, running jobs, River's queues.
3. The worker's logs: restarts, `queue.Drain` lines, errors.

### Likely causes

- The worker is down or restarting (deploy, crash loop).
- Another kind is flooding the shared queue (see [Job backlog](queue-job-backlog.md)).
- Database pressure slowing every job ([Database connections](database-connections.md)).

## Mitigation

- Get the worker healthy; River runs the late timers in order, and `order_timer_reconcile` sweeps anything past its deadline every minute.
- Timer kinds are never paused or run by hand (04 §2.7). See [Orders timers](orders-timers.md).

## Escalation

**Level:** Critical: SEV2. SEV1 if timers are minutes late for many orders ([Severity levels](../ops/severity.md)).

- The **lead** and **W2-02** at once.

## Follow-up

- If a busy kind starved the timers, move it to its own queue.
- Check the reconcile sweep caught up all late orders.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
