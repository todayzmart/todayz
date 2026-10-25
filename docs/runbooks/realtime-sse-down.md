# SSE down

Alert `tz-sse-down` in `infra/grafana/alerts/realtime.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

Open business-app SSE connections fell below half of what they were 5 minutes earlier, with at
least 10 open then, between 09:30 and 21:30 IST (TODO(lead): per-store hours). Business devices
have probably lost their live stream.

## First five minutes

1. Open [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): did connections drop to 0 or halve?
2. Did the API restart or deploy at that minute ([01 · Service overview](https://grafana.todayz.localhost/d/todayz-01))? Clients reconnect within a minute.
3. If they do not come back, see [Order alerts slow](realtime-order-alerts-slow.md) and the API logs.

## Diagnosis

1. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): open connections and reconnects by app; did they drop to 0 or halve?
2. [01 · Service overview](https://grafana.todayz.localhost/d/todayz-01): did an API deploy or restart happen at the same minute?
3. API logs for SSE handler errors.

### Likely causes

- An API deploy (connections drop and should come back within a minute).
- A proxy or DNS change.
- An auth change that makes the stream's token check fail (401s on the SSE route).

## Mitigation

- If it does not recover within 5 minutes of a deploy, roll back.

## Escalation

**Level:** Warning: SEV3. SEV2 if connections do not come back in 10 minutes ([Severity levels](../ops/severity.md)).

- The realtime owner (W2-05) and the lead.

## Follow-up

- If a deploy dropped them, check the load balancer drains SSE gracefully.
- If the per-store hours made it noisy, tune the alert window.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
