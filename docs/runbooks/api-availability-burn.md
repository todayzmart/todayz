# API availability burn

Alerts `tz-api-availability-fast-burn`, `tz-api-availability-slow-burn` in `infra/grafana/alerts/api.yaml` (05 §5, owner W3-07). Severity: **critical (fast) / warning (slow)**.

## What it means

The API's 5xx share is eating the 99.5 % availability budget (05 §6) faster than it can last a
month. Fast burn: both the 1 h and the 5 min ratios are above 14.4 × 0.5 % = 7.2 %; at that rate 2 %
of the 30-day budget goes every hour. Slow burn: both the 6 h and the 30 min ratios are above 3 %.
429s are left out of the denominator (problem codes `rate_limited`, `otp_locked`); 4xx never count.

## First three checks

1. [00 · What to fix today](https://grafana.todayz.localhost/d/todayz-00): the top rows rank the routes by 5xx impact; note whether one route or all of them fail.
2. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): filter the route; `code` shows which problem code the 5xx carry (`internal`, `unavailable`, a domain code).
3. Logs in Grafana Explore (Loki): `{service_name="todayz-api"} | json | status >= 500`, then follow `trace_id` into Tempo.

## Likely causes

- A deploy: compare the start with the release time (`version` in the logs).
- The database: pool saturated or slow queries ([03 · Database](https://grafana.todayz.localhost/d/todayz-03); [Database connections](database-connections.md)).
- A provider down (SMS, maps): one route's 5xx with the provider in the error.
- A panic in one handler: `msg="panic"` lines with a stack fingerprint.

## Fix

- A bad deploy: roll back (the lead; merges to `main` deploy to prod, D-210).
- A database cause: follow its runbook. A provider cause: wait it out or switch the feature off in `platform_settings` if it has a switch.
- Never silence the fast burn without a cause written in the incident note.

## Who to tell

- The **lead** at once for the fast burn; the owner of the failing route's WP for either.
