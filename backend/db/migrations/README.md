# Migrations

goose SQL files, embedded in the binary (`db/embed.go`). `todayz migrate up|down|status`
runs them; `make reset` and `make dev` run `up`.

## Rules

- Change the schema only by adding a file: `NNNNN_<wp-id>_<what>.sql`. Never edit a file that
  is on `main`.
- **Numbering (D-89).** The five-digit number is `<wave><wp:2><seq:2>`: each work package owns a
  block, so WPs built in parallel never pick the same number. W1-02 numbers its files
  10201–10299 (`10201_w1-02_users.sql`, `10202_w1-02_sessions.sql`), W3-07 30701–30799. Wave 0
  shares 00001–00099. `TestMigrationFileNames` (internal/platform/migrate, `CheckFileName`)
  fails on a file whose number is outside its wp-id's block, a bad shape or a reused number;
  `TestCheckFileNameFires` is its fixture.
- Blocks merge in any order, so a database can already hold a higher block than the one a new
  file adds. goose refuses to apply a file numbered below the newest it has applied. Locally,
  `make reset` fixes it (recreate, migrate, seed). For staging and prod the lead decides the
  merge order before Wave 5.
- Every file has `-- +goose Up` and `-- +goose Down`; Down undoes Up exactly. `TestUpDownUp`
  (internal/platform/migrate) runs up, down to zero and up again on a fresh database.
- **Version 3 is reserved.** It is the Go migration in `internal/platform/migrate` that installs
  River's tables (River schema version 8). Do not create `00003_*.sql` (the name check refuses it).
  When River ships a new schema version, add a new Go migration there (TestRiverVersionIsLatest
  fails until you do).
- sqlc reads only the `+goose Up` part of these files as the schema (`db/sqlc.yaml`). It does
  not see River's tables: use the River client for jobs, never SQL.
- A configurable number's default is a `platform_settings` row (01 §17). Change a default only
  through a new migration that updates the row, after 01 §17 changes. `TestSettingsMatchProductRules`
  checks the rows against 01 §17 in both directions. Never put a key, token or secret in
  `platform_settings` (D-19; a CHECK constraint and `TestNoSecretSettings` refuse it).

## Roles (the database role contract)

| Role | Used by | Can |
|---|---|---|
| superuser (`todayz` locally, the RDS master in AWS) | bootstrap only | creates the roles, the databases and the extensions |
| `todayz_owner` | `todayz migrate` (`DATABASE_MIGRATE_URL`) | owns schema `public` and every table; runs DDL |
| `todayz_app` | `todayz api`, `todayz worker`, `todayz seed` (`DATABASE_URL`) | DML only, granted by `ALTER DEFAULT PRIVILEGES FOR ROLE todayz_owner`; no DDL |

- Extensions (`postgis`, `pg_trgm`, `unaccent`, `pgcrypto`, `pg_stat_statements`) are created by
  the superuser before any migration (locally `infra/local/postgres/init.sql`; in tests
  `internal/testkit/database.sql`). `citext` is a trusted extension, so the baseline creates it
  as the owner. If a required extension is missing and the owner cannot create it, the baseline
  fails with a message that says which one and what to do.
- Tables created by `todayz_owner` are readable and writable by `todayz_app` through the
  default privileges; nothing else needs a GRANT. Functions are `EXECUTE` for `todayz_app`
  only: the default privileges revoke PUBLIC, and the baseline revokes it explicitly on
  `ensure_partitions` as well.
- `ensure_partitions(parent, from)` is `SECURITY DEFINER` (owner, `search_path = pg_catalog,
  pg_temp`, every name qualified) so the worker, as `todayz_app`, can add next partitions. It
  works only on a table listed in `partition_policies` (the allow-list: parent, `day` or `month`,
  how many ahead), owned by the owner, range-partitioned on one timestamptz column. A domain
  migration that adds a partitioned table inserts its own `partition_policies` row; the nightly
  `platform_maintenance` job reads that table, so nothing in Go lists the tables (D-50). Only the
  owner writes `partition_policies`. New partitions get the parent's privileges and TRUNCATE
  triggers.
- Every function the baseline defines sets `search_path = pg_catalog, pg_temp` and qualifies
  every name, so a table or function `todayz_app` could plant cannot shadow it. `todayz_app` has
  no `TEMPORARY` privilege on the database either.
- `audit_log` is append-only: `todayz_app` has no UPDATE, DELETE or TRUNCATE on it or any
  partition (revoked on the parent and the default partition; ensure_partitions copies it), and
  triggers refuse them for every role as a second wall. Partitions are monthly, bounded at UTC
  midnight.
- `platform_environment` holds one row, the `APP_ENV` this database belongs to. `todayz migrate
  up` writes it as the owner and refuses when `APP_ENV` disagrees; `migrate down`, `seed` and the
  test clock refuse unless it says `local` or `test` and matches `APP_ENV`. The app only reads it
  (`internal/platform/dbenv`).
- The test clock row (`test_clock`) NOTIFYs `test_clock` on change; the trigger works when
  `todayz_app` fires it.

## Files

| File | WP | What |
|---|---|---|
| `00001_w0-03_baseline.sql` | W0-03 | extension check, `touch_updated_at`, `partition_policies`, `ensure_partitions`, `append_only`, `platform_environment`, `platform_settings` (+ NOTIFY), `audit_log` (monthly partitions, append-only), `risk_flags` (D-78), `idempotency_keys`, `test_clock` (D-80) |
| `00002_w0-03_settings_seed.sql` | W0-03 | every 01 §17 key with its unit and default (D-61) |
| (Go) version 3 | W0-03 | River schema 8 (`internal/platform/migrate`) |
| `00010_w0-03_settings_d91_d95.sql` | lead | the five D-91–D-94 keys and `job_runs_retention_days` (D-95). A key added to 01 §17 after W0-03 is seeded by its own Wave 0 settings migration in the same merge |
