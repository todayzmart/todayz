# SSE down

Alert `tz-sse-down` in `infra/grafana/alerts/realtime.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

Open business-app SSE connections fell below half of what they were 5 minutes earlier, with at
least 10 open then, between 09:30 and 21:30 IST (TODO(lead): per-store hours). Business devices
have probably lost their live stream.

## First three checks

1. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): open connections and reconnects by app; did they drop to 0 or halve?
2. [01 · Service overview](https://grafana.todayz.localhost/d/todayz-01): did an API deploy or restart happen at the same minute?
3. API logs for SSE handler errors.

## Likely causes

- An API deploy (connections drop and should come back within a minute).
- A proxy or DNS change.
- An auth change that makes the stream's token check fail (401s on the SSE route).

## Fix

- If it does not recover within 5 minutes of a deploy, roll back.

## Who to tell

- The realtime owner (W2-05) and the lead.
