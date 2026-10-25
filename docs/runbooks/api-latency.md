# API latency

Alert `tz-api-latency` in `infra/grafana/alerts/api.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

p95 latency over 10 minutes is above 400 ms for reads (GET) or 800 ms for writes, twice the
05 §6 budgets (200 / 400 ms), and it stayed there for 10 minutes.

## First three checks

1. [00 · What to fix today](https://grafana.todayz.localhost/d/todayz-00): *slowest routes by impact* (p95 × volume).
2. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): the route's p50/p95/p99 and request rate; a jump in volume or one slow route?
3. [03 · Database](https://grafana.todayz.localhost/d/todayz-03): slow statements (`pg_stat_statements`), pool wait, and locks.

## Likely causes

- A missing index or a query plan that changed after a migration.
- Pool wait: too many concurrent requests for the pool size.
- A slow provider called inline (should be a job).
- A noisy client (a script hammering search): check `tz_http_server_requests_total` by route.

## Fix

- Find the slow statement and fix it in a migration (index) or in the query; never hot-patch prod SQL.
- Load from one client: the rate limiter should answer 429; check its settings in `platform_settings`.

## Who to tell

- The owner of the slow route's WP; the **lead** if it is everywhere.
