# Backup and restore

What is backed up, how to restore it, and how we prove the restore works. Names only; no URL,
password or key appears here.

## What is backed up

| What | How | Kept |
|---|---|---|
| Prod database (RDS `todayz-prod`, ap-south-1) | RDS automated backups with point-in-time restore (PITR), backup window 02:00–03:00 IST | 14 days (09 §3.4) |
| Weekly copy in another region | AWS Backup plan `todayz-prod-weekly-copy`: Monday 02:30 IST snapshot into vault `todayz-prod-weekly`, copied to `todayz-prod-weekly-dr` in ap-southeast-1 | 35 days |
| Final snapshot | `todayz-prod-final`, taken if the instance is ever deleted (deletion protection is on) | Until deleted by hand |
| Images and files (R2) | Not backed up separately. Originals stay in R2; rebuildable derivatives are regenerated | — |
| Code, migrations, Terraform, dashboards | Git (`todayz` repo) | Forever |
| Secrets | Secrets Manager `todayz/prod/<NAME>`; the founder keeps the cloud admin keys | See [Secret rotation](secret-rotation.md) |

The database is Single-AZ (D-96). An AZ failure means a PITR restore, estimated at 30–60 minutes.

## The local drill (any time)

```bash
make ops-restore-drill
```

It needs the local Postgres up (`make dev`, or just the stack's `postgres` service) with data in
`todayz` (`make reset` seeds it). It does not change `todayz`. The script
(`scripts/ops/restore-drill.sh`):

1. Opens a read-only snapshot on `todayz` and dumps exactly that snapshot (`pg_dump -Fc --snapshot`), so running apps do not skew the compare.
2. Runs the reconcile pass (`scripts/ops/restore-drill.sql`) on the source inside the snapshot.
3. Restores the dump into the scratch database `todayz_restore_drill` with `pg_restore --exit-on-error`.
4. Runs the same pass on the restored copy and compares:
   - every table's row count and an order-free checksum;
   - the `ledger_reconcile` invariants (each must be 0) and Σ of all ledger entries (must be 0);
   - credit sums per account kind, lots, holds, payouts, statements, adjustments;
   - partitions per partitioned table and their furthest bound;
   - goose and River migration versions;
   - counts of indexes, triggers, functions, constraints, sequences, and the extension versions.
5. Writes `docs/qa/ops/restore-drill-<date>.md`, drops the scratch database, and exits 1 on any mismatch.

Commit the evidence file. Run it after a migration that touches money tables, and before each
release that adds a partitioned table.

## Restore in the cloud (PITR to a side instance)

Use this for a lost or damaged database, or to recover rows changed by mistake. **Never restore
over `todayz-prod`.** A restore always makes a new instance next to it. The lead runs it; the
founder is told first (it costs money while the side instance runs).

Note the start time: the drill and a real restore are measured against the 30–60 minute estimate.

1. **Pick the moment.** The last good time in UTC, a minute before the damage (from the incident timeline, the audit log or the deploy time). PITR can go to any second inside the last 14 days, up to about 5 minutes ago.
2. **Restore to a side instance** (console: RDS → `todayz-prod` → Actions → Restore to point in time; or the CLI):
   `aws rds restore-db-instance-to-point-in-time --source-db-instance-identifier todayz-prod --target-db-instance-identifier todayz-prod-restore-<yyyymmdd> --restore-time <iso-utc> --db-subnet-group-name <prod subnet group> --vpc-security-group-ids <db security group> --db-parameter-group-name <prod parameter group> --no-multi-az --no-publicly-accessible`.
   Use the same subnet group, security group and parameter group as prod (Terraform outputs of `infra/terraform/modules/database`).
3. **Wait** until it is `available`: `aws rds wait db-instance-available --db-instance-identifier todayz-prod-restore-<yyyymmdd>`. Note the time.
4. **Reconcile** from an SSM session on an api instance, as `todayz_owner` in `BEGIN READ ONLY`, with `scripts/ops/restore-drill.sql`. Compare with prod's output for the same tables. Money first: the ledger invariants must all be 0.
5. **Decide:**
   - **Whole database lost:** point the app at the restored instance. Update `DATABASE_URL` and `DATABASE_MIGRATE_URL` in Secrets Manager to the new host, redeploy api then worker ([Secret rotation](secret-rotation.md), general steps). Then bring Terraform in line (import or rename) before the next apply.
   - **Some rows wrong:** copy back only the affected rows, in a reviewed script, by two people. Money is never copied back as rows: write `admin_adjustment` postings (two people, audited).
6. **Clean up:** delete the side instance when the incident note is closed (`aws rds delete-db-instance --db-instance-identifier todayz-prod-restore-<yyyymmdd> --skip-final-snapshot`, after a last check that nothing points at it).
7. **Write down** the times (start, available, reconciled, switched) in the incident note.

### From the weekly cross-region copy

Only if ap-south-1 is unusable. AWS Backup → vault `todayz-prod-weekly-dr` (ap-southeast-1) →
the latest recovery point → Restore. Data since that Monday is lost; tell the founder before you
start. The app, the load balancer and the secrets are in ap-south-1 too, so this is a full region
rebuild with Terraform, not a quick switch.

## The cloud drill (once, before launch, then every quarter)

W5-04 (09 §9): do steps 1–4 and 6 above on prod's own backups, with a restore time 10 minutes
ago, and note the time against the 30–60 minute estimate. Write the evidence to
`docs/qa/ops/restore-drill-cloud-<date>.md` in the same shape as the local one. It waits for the
AWS account; it has not run yet.
