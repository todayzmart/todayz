# Order alerts slow

Alert `tz-order-alerts-slow` in `infra/grafana/alerts/realtime.yaml` (05 §5, owner W3-07). Severity: **critical**.

## What it means

New-order and order-update events take more than 10 s at p95 to reach business devices over
SSE (`tz_sse_delivery_lag_seconds{app="business"}`), for 10 minutes. Shops see orders late and more
of them expire unaccepted.

## First three checks

1. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): SSE delivery lag, open connections and reconnects by app.
2. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): expired-unaccepted orders rising?
3. API logs for the realtime publisher (`pgnotify`) errors and the LISTEN loop restarting.

## Likely causes

- The LISTEN/NOTIFY loop disconnected and is reconnecting.
- API instances overloaded (CPU, GC pauses).
- A proxy buffering the stream (a CDN or Caddy config change).

## Fix

- Restart the unhealthy instance; check the proxy keeps `text/event-stream` unbuffered.
- The business app also polls; shops still get orders, only later.

## Who to tell

- The **lead** and the realtime owner (W2-05) at once.
