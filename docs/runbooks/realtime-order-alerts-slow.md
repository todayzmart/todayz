# Order alerts slow

Alert `tz-order-alerts-slow` in `infra/grafana/alerts/realtime.yaml` (05 §5, owner W3-07). Severity: **critical**.

## What it means

New-order and order-update events take more than 10 s at p95 to reach business devices over
SSE (`tz_sse_delivery_lag_seconds{app="business"}`), for 10 minutes. Shops see orders late and more
of them expire unaccepted.

## First five minutes

1. Open [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): lag and connections.
2. Check [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): are orders expiring unaccepted?
3. If it started with a deploy, roll back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)). Open an [incident note](../ops/incident-template.md).

## Diagnosis

1. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): SSE delivery lag, open connections and reconnects by app.
2. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): expired-unaccepted orders rising?
3. API logs for the realtime publisher (`pgnotify`) errors and the LISTEN loop restarting.

### Likely causes

- The LISTEN/NOTIFY loop disconnected and is reconnecting.
- API instances overloaded (CPU, GC pauses).
- A proxy buffering the stream (a CDN or Caddy config change).

## Mitigation

- Restart the unhealthy instance; check the proxy keeps `text/event-stream` unbuffered.
- The business app also polls; shops still get orders, only later.

## Escalation

**Level:** Critical: SEV2. SEV1 if shops get no alerts and orders expire ([Severity levels](../ops/severity.md)).

- The **lead** and the realtime owner (W2-05) at once.

## Follow-up

- A load test for SSE fan-out at the peak's connection count.
- Support checks the stores whose orders expired.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
