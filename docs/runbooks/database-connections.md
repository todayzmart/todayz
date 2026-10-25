# Database connections

Alerts `tz-db-pool-saturated`, `tz-db-server-connections` in `infra/grafana/alerts/database.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

Either the app's pgx pool is saturated (mean wait per acquire above 50 ms, or more than 80 %
of its max connections open) for 5 minutes, or Postgres itself has more than 80 % of
`max_connections` in use. The pool exports a cumulative wait, not a histogram, so the 05 §5
"p95 wait" is read as the mean (TODO(lead): a wait histogram). DB CPU and free storage are RDS
CloudWatch alerts that come with Wave 5.

## First three checks

1. [03 · Database](https://grafana.todayz.localhost/d/todayz-03): pool acquired/idle/max per service, mean wait, server connections, slow statements.
2. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): did request volume jump, or did latency rise first (slow queries hold connections)?
3. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): a job kind running long or many at once (the worker shares the server).

## Likely causes

- Slow queries holding connections (a lock wait, a missing index).
- A traffic spike above what the pool size was set for.
- Connections leaked by code that does not release a row set (rare with pgx; look for a long `idle in transaction`).
- A migration holding a lock.

## Fix

- Kill nothing blindly. Find the blocker in `pg_stat_activity` (read-only, as `todayz_owner` in a read-only transaction) and fix the cause.
- Pool size is an env setting of the service; raising it only helps if the server has headroom.

## Who to tell

- The **lead**; the owner of the slow query's WP.
