# RDS CPU

CloudWatch alarms in `infra/terraform/modules/stack/alarms.tf`, sent to the SNS topic
`todayz-prod-alarms` (email). Cloud only: there is no local equivalent.

- `rds-cpu-high` (`todayz-prod-rds-cpu-high`): `CPUUtilization` average above 80 % for 15 minutes (3 × 5 min).
- `rds-cpu-credits-low` (`todayz-prod-rds-cpu-credits-low`): `CPUCreditBalance` under 30 for 15 minutes.

## What it means

The prod database (RDS `todayz-prod`, db.t4g.medium, Single-AZ, 09 §3.4) is working too hard.
db.t4g is a burstable class: it runs above its baseline only while it has CPU credits. When the
credits run out, CPU is capped at the baseline and every query slows down. Orders, timers and money
all live in this one database, so a slow database is a slow product.

## First five minutes

1. Check the API is still answering: [01 · Service overview](https://grafana.todayz.localhost/d/todayz-01) (Grafana Cloud in prod), 5xx and p95.
2. Open RDS → `todayz-prod` → Monitoring: CPU, credit balance and connections over the last 3 hours. A step up or a slow climb?
3. Did a deploy or a migration just run? If CPU jumped with it, roll the app back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)).

## Diagnosis

1. **Top statements**, read-only as `todayz_owner` (SSM session, `BEGIN READ ONLY`):
   `SELECT left(query, 80), calls, round(total_exec_time) AS ms, round(mean_exec_time::numeric, 1) AS mean_ms FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 15`.
2. **What runs now:**
   `SELECT pid, state, now() - query_start AS age, wait_event_type, left(query, 80) FROM pg_stat_activity WHERE datname = 'todayz' AND state <> 'idle' ORDER BY age DESC LIMIT 20`.
3. **Database Insights** (RDS console → `todayz-prod` → Database Insights): load by wait event and by SQL.
4. **Jobs:** [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): a batch kind (search refresh, reliability, payouts) running at the same time.
5. **CLI** (lead's shell, read-only): `aws cloudwatch get-metric-statistics --namespace AWS/RDS --metric-name CPUCreditBalance --dimensions Name=DBInstanceIdentifier,Value=todayz-prod --statistics Minimum --period 300 --start-time <iso> --end-time <iso>`.

### Likely causes

- A query lost its index after a migration, or a new query scans a large table.
- A batch job kind running in the evening peak.
- Real growth: CPU has stayed above 60 % for days and the credits never refill.

## Mitigation

- A runaway batch kind (not a `timer` kind): pause it in A-21 with a reason, resume it off-peak.
- A bad release: roll the app back. Migrations are expand-only, so the old app runs on the new schema.
- A missing index: add it in a migration with `CREATE INDEX CONCURRENTLY`; never hot-patch prod SQL.
- Sustained load: upgrade the class to db.m7g.large (09 §3.4). That is a Terraform change by the lead in a maintenance window; on Single-AZ it restarts the database (a few minutes of downtime). Tell the founder first.
- Never cancel or kill backend sessions blindly; a killed money transaction is rolled back, but find out what it was first.

## Escalation

**Level:** SEV2 if API latency or errors are up; SEV3 if only the alarm is up and users feel nothing ([Severity levels](../ops/severity.md)). Credits at 0 with rising latency: SEV2.

- The **lead** for any class change or a slow-query fix.
- The **founder** before an upgrade (cost, D-96).

## Follow-up

- The fix for the statement that caused it, with a `pg_stat_statements` before/after in the PR.
- If credits drained twice in a month, plan the upgrade with the founder.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
