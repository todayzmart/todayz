-- W1-11 jobs platform (D-95, D-98; 03 §9): job_runs (monthly partitions),
-- job_stats_hourly, job_controls, the retention floor on partition_policies
-- and drop_old_partitions.

-- +goose Up

-- retention_days is the floor drop_old_partitions enforces: a partition may
-- be dropped only once it ends more than retention_days before now(). NULL
-- (every existing row, audit_log included) means never droppable.
ALTER TABLE partition_policies ADD COLUMN retention_days int;
ALTER TABLE partition_policies ADD CONSTRAINT ck_partition_policies_retention_days
  CHECK (retention_days IS NULL OR retention_days BETWEEN 1 AND 3650);

-- job_runs: one row per logged attempt (03 §9). Scheduled, manual, retry,
-- backfill and continuation runs are always logged; event kinds only when an
-- attempt fails, is retried or is discarded. The watchdog writes `missed`
-- rows (attempt 0). Rows are updated from running to their end status, so
-- todayz_app has UPDATE but no DELETE: whole months go by drop_old_partitions.
-- created_at comes from the injected clock, so partitions follow the test
-- clock; the default partition catches a clock ahead of the partitions made.
CREATE TABLE job_runs (
  id            uuid NOT NULL,
  kind          text NOT NULL,
  class         text NOT NULL,
  river_job_id  bigint,
  trigger       text NOT NULL,
  period_key    text,
  attempt       int NOT NULL,
  status        text NOT NULL,
  scheduled_at  timestamptz,
  started_at    timestamptz,
  finished_at   timestamptz,
  duration_ms   bigint,
  lateness_ms   bigint,
  summary       jsonb,
  note          text,
  error         text,
  trace_id      text,
  triggered_by  uuid, -- admin user (plain uuid, D-35; the FK comes with W1-01's users)
  created_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pk_job_runs PRIMARY KEY (id, created_at),
  CONSTRAINT ck_job_runs_kind CHECK (kind ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(kind) <= 63),
  CONSTRAINT ck_job_runs_class CHECK (class IN ('timer','money','notify','marketing','ai','rollup','maintenance')),
  CONSTRAINT ck_job_runs_trigger CHECK (trigger IN ('schedule','event','manual','retry','backfill','continuation')),
  CONSTRAINT ck_job_runs_status CHECK (status IN ('running','succeeded','failed','cancelled','skipped','discarded','missed','yielded')),
  CONSTRAINT ck_job_runs_attempt CHECK (attempt BETWEEN 0 AND 100),
  CONSTRAINT ck_job_runs_period_key CHECK (period_key IS NULL OR length(period_key) <= 32),
  CONSTRAINT ck_job_runs_summary CHECK (summary IS NULL OR jsonb_typeof(summary) = 'object'),
  CONSTRAINT ck_job_runs_note CHECK (note IS NULL OR length(note) <= 500),
  CONSTRAINT ck_job_runs_error CHECK (error IS NULL OR octet_length(error) <= 2048),
  CONSTRAINT ck_job_runs_trace_id CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$'),
  CONSTRAINT ck_job_runs_durations CHECK (duration_ms IS NULL OR duration_ms >= 0)
) PARTITION BY RANGE (created_at);
CREATE TABLE job_runs_default PARTITION OF job_runs DEFAULT;
CREATE INDEX ix_job_runs_kind_created ON job_runs (kind, created_at DESC);
CREATE INDEX ix_job_runs_kind_period ON job_runs (kind, period_key) WHERE period_key IS NOT NULL;
CREATE INDEX ix_job_runs_running ON job_runs (kind) WHERE status = 'running';
CREATE INDEX ix_job_runs_river_job ON job_runs (river_job_id) WHERE river_job_id IS NOT NULL;
CREATE INDEX ix_job_runs_status_created ON job_runs (status, created_at DESC);
REVOKE DELETE, TRUNCATE ON job_runs, job_runs_default FROM todayz_app;

INSERT INTO partition_policies (parent, granularity, ahead, retention_days) VALUES ('job_runs', 'month', 3, 1);
SELECT ensure_partitions('job_runs', now());

-- job_stats_hourly: one row per kind and IST hour, upserted once per attempt
-- by the jobs middleware (where event-kind successes are counted). The 7-day
-- success rate is succeeded / (succeeded + failed + cancelled + discarded).
CREATE TABLE job_stats_hourly (
  kind             text NOT NULL,
  hour             timestamptz NOT NULL,
  succeeded        bigint NOT NULL DEFAULT 0,
  failed           bigint NOT NULL DEFAULT 0,
  cancelled        bigint NOT NULL DEFAULT 0,
  skipped          bigint NOT NULL DEFAULT 0,
  discarded        bigint NOT NULL DEFAULT 0,
  yielded          bigint NOT NULL DEFAULT 0,
  sum_ms           bigint NOT NULL DEFAULT 0,
  max_ms           bigint NOT NULL DEFAULT 0,
  max_lateness_ms  bigint NOT NULL DEFAULT 0,
  CONSTRAINT pk_job_stats_hourly PRIMARY KEY (kind, hour),
  CONSTRAINT ck_job_stats_hourly_kind CHECK (kind ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(kind) <= 63),
  CONSTRAINT ck_job_stats_hourly_counts CHECK (succeeded >= 0 AND failed >= 0 AND cancelled >= 0 AND skipped >= 0
    AND discarded >= 0 AND yielded >= 0 AND sum_ms >= 0 AND max_ms >= 0)
);
CREATE INDEX ix_job_stats_hourly_hour ON job_stats_hourly (hour);
REVOKE DELETE, TRUNCATE ON job_stats_hourly FROM todayz_app;

-- job_controls: pause state per kind. A paused scheduled kind does not run
-- at its ticks; a paused event kind snoozes. timer kinds can never be paused
-- (enforced in Go, 04 §2.7).
CREATE TABLE job_controls (
  kind        text PRIMARY KEY,
  paused      boolean NOT NULL DEFAULT false,
  reason      text,
  updated_by  uuid, -- admin user (plain uuid, D-35)
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_job_controls_kind CHECK (kind ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(kind) <= 63),
  CONSTRAINT ck_job_controls_reason CHECK (reason IS NULL OR length(reason) <= 500)
);
REVOKE DELETE, TRUNCATE ON job_controls FROM todayz_app;

-- drop_old_partitions drops the partitions of parent_name that end at or
-- before `before`, under the same rules as ensure_partitions: the table must
-- have a partition_policies row (with a non-NULL retention_days), be a
-- partitioned table in public owned by this function's owner, range-
-- partitioned on one timestamptz column. It refuses a `before` newer than
-- now() - retention_days (the floor), never drops the default partition and
-- returns how many partitions it dropped. Idempotent; the worker's
-- job_runs_retention kind calls it as todayz_app (SECURITY DEFINER: the
-- owner drops its own tables, so todayz_app needs no DDL grant).
-- +goose StatementBegin
CREATE FUNCTION drop_old_partitions(parent_name text, before timestamptz)
RETURNS int
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  keep_days  int;
  parent_oid pg_catalog.oid;
  col        text;
  coltype    pg_catalog.oid;
  p          record;
  upper_txt  text;
  hi         timestamptz;
  dropped    int := 0;
BEGIN
  IF before IS NULL THEN
    RAISE EXCEPTION 'drop_old_partitions: before is null';
  END IF;
  SELECT pp.retention_days INTO keep_days
  FROM public.partition_policies pp WHERE pp.parent = parent_name;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'drop_old_partitions: % has no row in partition_policies', parent_name;
  END IF;
  IF keep_days IS NULL THEN
    RAISE EXCEPTION 'drop_old_partitions: % has no retention_days; its partitions are never dropped', parent_name;
  END IF;
  IF before > pg_catalog.now() - pg_catalog.make_interval(days => keep_days) THEN
    RAISE EXCEPTION 'drop_old_partitions: % keeps at least % days; before % is too recent', parent_name, keep_days, before;
  END IF;

  SELECT c.oid INTO parent_oid
  FROM pg_catalog.pg_class c
  WHERE c.relname = parent_name
    AND c.relnamespace = 'public'::pg_catalog.regnamespace
    AND c.relkind = 'p'
    AND pg_catalog.pg_get_userbyid(c.relowner) = current_user;
  IF parent_oid IS NULL THEN
    RAISE EXCEPTION 'drop_old_partitions: public.% is not a partitioned table owned by %', parent_name, current_user;
  END IF;

  SELECT a.attname, a.atttypid INTO col, coltype
  FROM pg_catalog.pg_partitioned_table pt
  JOIN pg_catalog.pg_attribute a ON a.attrelid = pt.partrelid AND a.attnum = pt.partattrs[0]
  WHERE pt.partrelid = parent_oid AND pt.partstrat = 'r' AND pt.partnatts = 1;
  IF col IS NULL OR coltype <> 'pg_catalog.timestamptz'::pg_catalog.regtype THEN
    RAISE EXCEPTION 'drop_old_partitions: public.% is not range-partitioned on one timestamptz column', parent_name;
  END IF;

  FOR p IN
    SELECT c.relname, pg_catalog.pg_get_expr(c.relpartbound, c.oid) AS bound
    FROM pg_catalog.pg_inherits i JOIN pg_catalog.pg_class c ON c.oid = i.inhrelid
    WHERE i.inhparent = parent_oid AND c.relispartition AND c.relkind = 'r'
      AND c.relnamespace = 'public'::pg_catalog.regnamespace
      AND pg_catalog.pg_get_userbyid(c.relowner) = current_user
    ORDER BY c.relname
  LOOP
    -- FOR VALUES FROM ('…') TO ('…'); the default partition has no TO.
    upper_txt := (pg_catalog.regexp_match(p.bound, '^FOR VALUES FROM \(''[^'']+''\) TO \(''([^'']+)''\)$'))[1];
    CONTINUE WHEN upper_txt IS NULL;
    hi := upper_txt::timestamptz;
    IF hi <= before THEN
      EXECUTE pg_catalog.format('DROP TABLE public.%I', p.relname);
      dropped := dropped + 1;
    END IF;
  END LOOP;
  RETURN dropped;
END
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION drop_old_partitions(text, timestamptz) FROM PUBLIC;

-- +goose Down
DROP FUNCTION IF EXISTS drop_old_partitions(text, timestamptz);
DROP TABLE IF EXISTS job_controls;
DROP TABLE IF EXISTS job_stats_hourly;
DELETE FROM partition_policies WHERE parent = 'job_runs';
DROP TABLE IF EXISTS job_runs;
ALTER TABLE partition_policies DROP CONSTRAINT IF EXISTS ck_partition_policies_retention_days;
ALTER TABLE partition_policies DROP COLUMN IF EXISTS retention_days;
