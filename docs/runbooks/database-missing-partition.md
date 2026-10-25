# Missing partition

Alert `tz-db-missing-partition` in `infra/grafana/alerts/database.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

A partitioned table (a row in `partition_policies`) has no partition that holds the moment
7 days from now. Writes for that time would fail once it arrives. The `partition_maintenance` job
(`ensure_partitions`) creates partitions `ahead` periods in advance, so this means it has not run
or has failed.

## First three checks

1. [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): the partition maintenance kind; last runs and their errors.
2. The table's partitions (read-only, as `todayz_owner` in a read-only transaction): `SELECT c.relname, pg_get_expr(c.relpartbound, c.oid) FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid WHERE i.inhparent = 'public.<parent>'::regclass ORDER BY 1`.
3. `SELECT * FROM partition_policies` for the parent's granularity and `ahead`.

## Likely causes

- The maintenance job failing (permissions, a lock timeout).
- A new `partition_policies` row whose parent table is not partitioned or is misnamed.

## Fix

- Fix the job's cause and run it from A-21 Jobs; it is idempotent.
- A wrong policy row is fixed by a new migration, never by hand.

## Who to tell

- The **lead** and **W1-11** (jobs).
