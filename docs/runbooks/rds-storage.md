# RDS storage

CloudWatch alarm `rds-free-storage-low` (`todayz-prod-rds-free-storage-low`) in
`infra/terraform/modules/stack/alarms.tf`: `FreeStorageSpace` under 10 GB for 10 minutes. Sent to
the SNS topic `todayz-prod-alarms` (email). Cloud only.

## What it means

The prod database (RDS `todayz-prod`) has less than 10 GB free. Storage autoscales from 100 GB to
500 GB (09 §3.4), so this fires when autoscaling cannot keep up, is in its 6-hour cool-down, or has
reached the 500 GB ceiling. At 0 GB free, Postgres stops taking writes: no orders, no sales, no
ledger postings.

## First five minutes

1. RDS → `todayz-prod` → Configuration: allocated storage and the maximum (500 GB). At the maximum already?
2. RDS → Monitoring: free storage over the last 7 days. A slow trend or a sudden drop?
3. A sudden drop: look for a runaway table (Diagnosis 1) and a stuck replication slot or long transaction (Diagnosis 3) before anything else.

## Diagnosis

1. **Largest tables**, read-only as `todayz_owner` (SSM session, `BEGIN READ ONLY`):
   `SELECT relname, pg_size_pretty(pg_total_relation_size(oid)) FROM pg_class WHERE relkind IN ('r', 'p', 'm') ORDER BY pg_total_relation_size(oid) DESC LIMIT 15`.
2. **Partitioned tables** (`events`, `search_log`, `audit_log`, `job_runs`): is old-partition retention running? [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs), the partition maintenance kind; see [Missing partition](database-missing-partition.md).
3. **WAL held back:** `SELECT slot_name, active, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) FROM pg_replication_slots` and the oldest open transaction:
   `SELECT pid, now() - xact_start AS age, state FROM pg_stat_activity WHERE xact_start IS NOT NULL ORDER BY age DESC LIMIT 5`.
4. **Dead rows:** `SELECT relname, n_dead_tup, last_autovacuum FROM pg_stat_user_tables ORDER BY n_dead_tup DESC LIMIT 10`.

### Likely causes

- A table growing faster than planned (events, search log, job runs) because retention did not run.
- An inactive replication slot or a transaction left open for hours keeps WAL and dead rows.
- A migration that rewrote a large table.
- Real growth up to the 500 GB ceiling.

## Mitigation

- Retention not running: fix the partition maintenance kind, then let it drop old partitions. Do not `DROP` partitions by hand.
- Inactive replication slot: tell the lead; dropping a slot is a lead decision.
- A long open transaction: find its owner first; cancel it only if it is not a money step.
- Ceiling reached: the lead raises `max_allocated_storage_gb` in Terraform (founder's yes: cost). In an emergency, RDS console → Modify → allocated storage, then bring Terraform in line the same day.

## Escalation

**Level:** SEV2 while free space is falling; SEV1 under 2 GB or if writes fail ([Severity levels](../ops/severity.md)).

- The **lead** at once.
- The **founder** for a storage ceiling change (cost).

## Follow-up

- A retention or size check for the table that grew.
- Check the next [restore drill](../ops/backup-restore.md) still fits the time estimate with the larger database.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
