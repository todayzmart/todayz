# Database connections

Alerts `tz-db-pool-saturated`, `tz-db-server-connections` in `infra/grafana/alerts/database.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

Either the app's pgx pool is saturated (mean wait per acquire above 50 ms, or more than 80 %
of its max connections open) for 5 minutes, or Postgres itself has more than 80 % of
`max_connections` in use. The pool exports a cumulative wait, not a histogram, so the 05 §5
"p95 wait" is read as the mean (TODO(lead): a wait histogram). DB CPU and free storage are RDS
CloudWatch alerts that come with Wave 5.

## First five minutes

1. Open [03 · Database](https://grafana.todayz.localhost/d/todayz-03): pool or server? which service?
2. Look for a long query or a lock holding connections (slow statements panel).
3. If a job kind is the cause, pause it in A-21 (not a `timer` kind). If a deploy is the cause, roll back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)).

## Diagnosis

1. [03 · Database](https://grafana.todayz.localhost/d/todayz-03): pool acquired/idle/max per service, mean wait, server connections, slow statements.
2. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): did request volume jump, or did latency rise first (slow queries hold connections)?
3. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): a job kind running long or many at once (the worker shares the server).

### Likely causes

- Slow queries holding connections (a lock wait, a missing index).
- A traffic spike above what the pool size was set for.
- Connections leaked by code that does not release a row set (rare with pgx; look for a long `idle in transaction`).
- A migration holding a lock.

## Mitigation

- Kill nothing blindly. Find the blocker in `pg_stat_activity` (read-only, as `todayz_owner` in a read-only transaction) and fix the cause.
- Pool size is an env setting of the service; raising it only helps if the server has headroom.

## Escalation

**Level:** Warning: SEV3. SEV2 if API requests fail or time out because of it ([Severity levels](../ops/severity.md)).

- The **lead**; the owner of the slow query's WP.

## Follow-up

- A statement timeout or an index for the query that held connections.
- If the pool size was too small, change it with the numbers in the PR.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
