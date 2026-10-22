-- W0-03 baseline: extensions, shared helpers, platform_settings, audit_log,
-- risk_flags (D-78), idempotency_keys and the test clock (D-80).
-- Conventions (03): ULIDs stored as uuid and generated in Go; created_at on
-- every table; updated_at + version on mutable tables; ix_/ux_/ck_/fk_ names.
-- Change the schema only through a new file: NNNNN_<wp-id>_<what>.sql.

-- +goose Up
-- Extensions (03). Migrations run as the schema owner (todayz_owner), which
-- is not a superuser: a superuser creates the extensions first (locally
-- infra/local/postgres/init.sql and testkit; on RDS the bootstrap step), and
-- this block only checks. A missing extension the owner cannot create fails
-- with a message that says what to do.
-- +goose StatementBegin
DO $$
DECLARE
  ext text;
BEGIN
  FOREACH ext IN ARRAY ARRAY['postgis', 'pg_trgm', 'unaccent', 'citext', 'pgcrypto'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = ext) THEN
      BEGIN
        EXECUTE format('CREATE EXTENSION IF NOT EXISTS %I', ext);
      EXCEPTION WHEN insufficient_privilege THEN
        RAISE EXCEPTION 'extension % is missing and role % cannot create it; create it as a superuser in database % first (locally: make reset re-runs infra/local/postgres/init.sql)',
          ext, current_user, current_database();
      END;
    END IF;
  END LOOP;
  -- pg_stat_statements is for dashboards only: create it when allowed, else carry on.
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')
     AND EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'pg_stat_statements') THEN
    BEGIN
      CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
    EXCEPTION WHEN insufficient_privilege THEN
      RAISE NOTICE 'pg_stat_statements not created (role % is not a superuser); skipped', current_user;
    END;
  END IF;
END
$$;
-- +goose StatementEnd

-- Every function here pins `SET search_path = pg_catalog, pg_temp` and names
-- every object with its schema. pg_temp is searched first unless it is listed,
-- so without the pin a session could shadow pg_class (or any unqualified
-- table) with a temporary table of its own and steer the function.

-- touch_updated_at: BEFORE UPDATE trigger for mutable tables. Code still bumps
-- version itself in the WHERE version = $n update (optimistic locking).
-- +goose StatementBegin
CREATE FUNCTION touch_updated_at() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  NEW.updated_at := pg_catalog.now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- partition_policies: the allow-list of partitioned tables ensure_partitions
-- maintains. A domain's migration that adds a partitioned table inserts its
-- own row (parent, 'day' or 'month', how many partitions to keep ahead); the
-- worker's nightly platform_maintenance job reads this table, so nothing in Go
-- lists the tables (D-50). Only the owner writes it.
CREATE TABLE partition_policies (
  parent      text PRIMARY KEY,
  granularity text NOT NULL,
  ahead       int NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_partition_policies_parent CHECK (parent ~ '^[a-z][a-z0-9_]{0,40}$'),
  CONSTRAINT ck_partition_policies_granularity CHECK (granularity IN ('day','month')),
  CONSTRAINT ck_partition_policies_ahead CHECK (ahead BETWEEN 1 AND 400)
);
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON partition_policies FROM todayz_app;

-- ensure_partitions creates the range partitions partition_policies asks for:
-- `ahead` partitions of the policy's granularity (UTC boundaries), starting
-- with the one that contains from_ts. parent_name must have a policy row and
-- be a partitioned table in schema public, owned by this function's owner,
-- range-partitioned on one timestamptz column. Rows already in the parent's
-- default partition for a new range move into it. Each new partition gets the
-- parent's privileges (default privileges would otherwise give todayz_app
-- UPDATE and DELETE on it) and the parent's TRUNCATE triggers (statement-level
-- TRUNCATE triggers are not cloned to partitions). Partitions are named
-- <parent>_pYYYYMM or <parent>_pYYYYMMDD. Returns how many it created.
-- Idempotent; the worker's platform_maintenance job calls it every night.
-- SECURITY DEFINER: the worker runs as todayz_app, which cannot create
-- tables; the function runs as the schema owner, so new partitions are owned
-- by it.
-- +goose StatementBegin
CREATE FUNCTION ensure_partitions(parent_name text, from_ts timestamptz)
RETURNS int
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  gran       text;
  ahead      int;
  parent_oid pg_catalog.oid;
  step       interval;
  fmt        text;
  col        text;
  coltype    pg_catalog.oid;
  def_oid    pg_catalog.oid;
  lo         timestamptz;
  hi         timestamptz;
  part       text;
  part_oid   pg_catalog.oid;
  g          record;
  t          record;
  created    int := 0;
BEGIN
  SELECT p.granularity, p.ahead INTO gran, ahead
  FROM public.partition_policies p WHERE p.parent = parent_name;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'ensure_partitions: % has no row in partition_policies', parent_name;
  END IF;
  IF gran = 'day' THEN
    step := interval '1 day'; fmt := 'YYYYMMDD';
  ELSE
    step := interval '1 month'; fmt := 'YYYYMM';
  END IF;

  SELECT c.oid INTO parent_oid
  FROM pg_catalog.pg_class c
  WHERE c.relname = parent_name
    AND c.relnamespace = 'public'::pg_catalog.regnamespace
    AND c.relkind = 'p'
    AND pg_catalog.pg_get_userbyid(c.relowner) = current_user;
  IF parent_oid IS NULL THEN
    RAISE EXCEPTION 'ensure_partitions: public.% is not a partitioned table owned by %', parent_name, current_user;
  END IF;

  SELECT a.attname, a.atttypid INTO col, coltype
  FROM pg_catalog.pg_partitioned_table pt
  JOIN pg_catalog.pg_attribute a ON a.attrelid = pt.partrelid AND a.attnum = pt.partattrs[0]
  WHERE pt.partrelid = parent_oid AND pt.partstrat = 'r' AND pt.partnatts = 1;
  IF col IS NULL OR coltype <> 'pg_catalog.timestamptz'::pg_catalog.regtype THEN
    RAISE EXCEPTION 'ensure_partitions: public.% is not range-partitioned on one timestamptz column', parent_name;
  END IF;

  SELECT i.inhrelid INTO def_oid
  FROM pg_catalog.pg_inherits i JOIN pg_catalog.pg_class c ON c.oid = i.inhrelid
  WHERE i.inhparent = parent_oid AND c.relispartition
    AND pg_catalog.pg_get_expr(c.relpartbound, c.oid) = 'DEFAULT';

  lo := pg_catalog.date_trunc(gran, from_ts AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';
  FOR i IN 1..ahead LOOP
    hi := ((lo AT TIME ZONE 'UTC') + step) AT TIME ZONE 'UTC';
    part := parent_name || '_p' || pg_catalog.to_char(lo AT TIME ZONE 'UTC', fmt);
    IF pg_catalog.to_regclass(pg_catalog.format('public.%I', part)) IS NULL THEN
      EXECUTE pg_catalog.format('CREATE TABLE public.%I (LIKE public.%I INCLUDING DEFAULTS INCLUDING CONSTRAINTS)',
                                part, parent_name);
      part_oid := pg_catalog.to_regclass(pg_catalog.format('public.%I', part))::pg_catalog.oid;

      -- Privileges: exactly the parent's (grantees other than the owner).
      FOR g IN
        SELECT 'REVOKE' AS verb, x.grantee, x.privilege_type FROM (
          SELECT a.grantee, a.privilege_type
          FROM pg_catalog.pg_class c, pg_catalog.aclexplode(c.relacl) a
          WHERE c.oid = part_oid AND a.grantee <> c.relowner
          EXCEPT
          SELECT a.grantee, a.privilege_type
          FROM pg_catalog.pg_class c, pg_catalog.aclexplode(c.relacl) a
          WHERE c.oid = parent_oid) x
        UNION ALL
        SELECT 'GRANT', y.grantee, y.privilege_type FROM (
          SELECT a.grantee, a.privilege_type
          FROM pg_catalog.pg_class c, pg_catalog.aclexplode(c.relacl) a
          WHERE c.oid = parent_oid AND a.grantee <> c.relowner
          EXCEPT
          SELECT a.grantee, a.privilege_type
          FROM pg_catalog.pg_class c, pg_catalog.aclexplode(c.relacl) a
          WHERE c.oid = part_oid) y
      LOOP
        EXECUTE pg_catalog.format('%s %s ON public.%I %s %s', g.verb, g.privilege_type, part,
          CASE g.verb WHEN 'GRANT' THEN 'TO' ELSE 'FROM' END,
          CASE g.grantee WHEN 0 THEN 'PUBLIC' ELSE pg_catalog.quote_ident(pg_catalog.pg_get_userbyid(g.grantee)) END);
      END LOOP;

      -- TRUNCATE triggers (bit 32 of tgtype; bit 2 is BEFORE).
      FOR t IN
        SELECT tg.tgname, tg.tgtype::int AS tgtype, tg.tgfoid::pg_catalog.regproc AS fn
        FROM pg_catalog.pg_trigger tg
        WHERE tg.tgrelid = parent_oid AND NOT tg.tgisinternal AND (tg.tgtype::int & 32) <> 0
      LOOP
        EXECUTE pg_catalog.format('CREATE TRIGGER %I %s TRUNCATE ON public.%I FOR EACH STATEMENT EXECUTE FUNCTION %s()',
          t.tgname, CASE WHEN (t.tgtype & 2) <> 0 THEN 'BEFORE' ELSE 'AFTER' END, part, t.fn);
      END LOOP;

      IF def_oid IS NOT NULL THEN
        -- Allowed past the append-only guard for this transaction only.
        PERFORM pg_catalog.set_config('todayz.partition_maintenance', 'on', true);
        EXECUTE pg_catalog.format('WITH moved AS (DELETE FROM %s WHERE %I >= $1 AND %I < $2 RETURNING *) INSERT INTO public.%I SELECT * FROM moved',
                                  def_oid::pg_catalog.regclass, col, col, part) USING lo, hi;
        PERFORM pg_catalog.set_config('todayz.partition_maintenance', 'off', true);
      END IF;
      EXECUTE pg_catalog.format('ALTER TABLE public.%I ATTACH PARTITION public.%I FOR VALUES FROM (%L) TO (%L)',
                                parent_name, part, lo, hi);
      created := created + 1;
    END IF;
    lo := hi;
  END LOOP;
  RETURN created;
END
$$;
-- +goose StatementEnd

-- Only the owner and roles granted EXECUTE (todayz_app, through default
-- privileges) may call the SECURITY DEFINER function.
REVOKE EXECUTE ON FUNCTION ensure_partitions(text, timestamptz) FROM PUBLIC;

-- append_only: refuses UPDATE and DELETE on an append-only table. Only
-- ensure_partitions may move rows out of a default partition: the switch is
-- honoured only when the current role owns the table, which inside that
-- SECURITY DEFINER function it does, and an app session never does. The app
-- has no UPDATE or DELETE privilege on append-only tables either; this
-- trigger is the second wall.
-- +goose StatementBegin
CREATE FUNCTION append_only() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF TG_OP = 'DELETE' AND pg_catalog.current_setting('todayz.partition_maintenance', true) = 'on'
     AND current_user = (SELECT pg_catalog.pg_get_userbyid(c.relowner)
                         FROM pg_catalog.pg_class c WHERE c.oid = TG_RELID) THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION '% is append-only (% refused)', TG_TABLE_NAME, TG_OP
    USING ERRCODE = 'insufficient_privilege';
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION append_only_truncate() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  RAISE EXCEPTION '% is append-only (TRUNCATE refused)', TG_TABLE_NAME
    USING ERRCODE = 'insufficient_privilege';
END
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- platform_environment: one row, the APP_ENV this database belongs to.
-- `todayz migrate up` writes it once (as the owner) and refuses to run when
-- APP_ENV disagrees; migrate down, seed and the test clock refuse to run
-- unless it says local or test and matches APP_ENV. The app can only read it.
------------------------------------------------------------------------------
CREATE TABLE platform_environment (
  id     boolean PRIMARY KEY DEFAULT true,
  env    text NOT NULL,
  set_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_platform_environment_single CHECK (id),
  CONSTRAINT ck_platform_environment_env CHECK (env IN ('local','test','staging','prod'))
);
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON platform_environment FROM todayz_app;

------------------------------------------------------------------------------
-- platform_settings (03; 01 §17; D-61, D-19, D-64)
------------------------------------------------------------------------------
CREATE TABLE platform_settings (
  key         text PRIMARY KEY,
  value       jsonb NOT NULL,
  unit        text NOT NULL,
  min         bigint,
  max         bigint,
  description text NOT NULL DEFAULT '',
  updated_by  uuid,  -- FK to users added by W1-01 (D-35)
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  version     int NOT NULL DEFAULT 1,
  CONSTRAINT ck_platform_settings_key CHECK (key ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$'),
  CONSTRAINT ck_platform_settings_unit CHECK (unit IN ('paise','bp','credits','minutes','hours','days','count','meters','bool','text')),
  -- D-19: flags and numbers only; never provider keys, tokens or other secrets.
  CONSTRAINT ck_platform_settings_no_secrets CHECK (
    key !~ '(^|_)(key|keys|apikey|authkey|auth|secret|secrets|token|tokens|password|passwd|pwd|dsn|credential|credentials|privkey|cert|pem|hmac|salt|seed|signing|webhook)(_|$)'),
  -- D-03: money keys end in _paise and only they use unit paise.
  CONSTRAINT ck_platform_settings_paise CHECK ((unit = 'paise') = (key LIKE '%\_paise')),
  CONSTRAINT ck_platform_settings_value CHECK (
    CASE
      WHEN unit = 'bool' THEN jsonb_typeof(value) = 'boolean'
      WHEN unit = 'text' THEN jsonb_typeof(value) = 'string'
      WHEN jsonb_typeof(value) <> 'number' THEN false
      ELSE (value::numeric = trunc(value::numeric))
    END),
  CONSTRAINT ck_platform_settings_range CHECK (
    CASE
      WHEN jsonb_typeof(value) <> 'number' THEN min IS NULL AND max IS NULL
      ELSE (min IS NULL OR value::numeric >= min) AND (max IS NULL OR value::numeric <= max)
    END),
  CONSTRAINT ck_platform_settings_bp CHECK (
    unit <> 'bp' OR (min IS NOT NULL AND max IS NOT NULL AND min >= 0 AND max <= 10000))
);

CREATE TRIGGER trg_platform_settings_touch BEFORE UPDATE ON platform_settings
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- Every change wakes every API and worker instance's settings cache.
-- +goose StatementBegin
CREATE FUNCTION platform_settings_notify() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  PERFORM pg_catalog.pg_notify('platform_settings', COALESCE(NEW.key, OLD.key));
  RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER trg_platform_settings_notify AFTER INSERT OR UPDATE OR DELETE ON platform_settings
  FOR EACH ROW EXECUTE FUNCTION platform_settings_notify();

------------------------------------------------------------------------------
-- audit_log (03): append-only, partitioned by month on created_at.
-- subject_id is text so a setting key or an id can be the subject.
------------------------------------------------------------------------------
CREATE TABLE audit_log (
  id           uuid NOT NULL,
  actor_kind   text NOT NULL,
  actor_id     uuid,
  action       text NOT NULL,
  subject_type text NOT NULL,
  subject_id   text,
  before       jsonb,
  after        jsonb,
  ip_hash      bytea,  -- HMAC(PII_HASH_KEY, ip); never the address
  request_id   text,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pk_audit_log PRIMARY KEY (id, created_at),
  CONSTRAINT ck_audit_log_actor_kind CHECK (actor_kind IN ('user','staff','admin','system','device')),
  CONSTRAINT ck_audit_log_action CHECK (action ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
  CONSTRAINT ck_audit_log_subject_type CHECK (subject_type ~ '^[a-z][a-z0-9_]*$')
) PARTITION BY RANGE (created_at);

CREATE TABLE audit_log_default PARTITION OF audit_log DEFAULT;
CREATE INDEX ix_audit_log_subject ON audit_log (subject_type, subject_id, created_at DESC);
CREATE INDEX ix_audit_log_actor ON audit_log (actor_id, created_at DESC) WHERE actor_id IS NOT NULL;
CREATE INDEX ix_audit_log_action ON audit_log (action, created_at DESC);

CREATE TRIGGER trg_audit_log_append_only BEFORE UPDATE OR DELETE ON audit_log
  FOR EACH ROW EXECUTE FUNCTION append_only();
CREATE TRIGGER trg_audit_log_no_truncate BEFORE TRUNCATE ON audit_log
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();
CREATE TRIGGER trg_audit_log_default_no_truncate BEFORE TRUNCATE ON audit_log_default
  FOR EACH STATEMENT EXECUTE FUNCTION append_only_truncate();

-- The app inserts and reads; it never updates, deletes or truncates. A
-- partition is checked only when it is named directly, so the default
-- partition is revoked too, and ensure_partitions copies these privileges and
-- the TRUNCATE trigger to every partition it creates.
REVOKE UPDATE, DELETE, TRUNCATE ON audit_log, audit_log_default FROM todayz_app;

INSERT INTO partition_policies (parent, granularity, ahead) VALUES ('audit_log', 'month', 3);
SELECT ensure_partitions('audit_log', now());

------------------------------------------------------------------------------
-- risk_flags (03, D-78): any package raises a flag; W3-02 owns the rules and screens.
------------------------------------------------------------------------------
CREATE TABLE risk_flags (
  id           uuid PRIMARY KEY,
  subject_type text NOT NULL,
  subject_id   uuid NOT NULL,
  rule         text NOT NULL,
  score        int NOT NULL DEFAULT 0,
  details      jsonb NOT NULL DEFAULT '{}'::jsonb,
  status       text NOT NULL DEFAULT 'open',
  reviewed_by  uuid,  -- FK to users added by W1-01 (D-35)
  reviewed_at  timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  version      int NOT NULL DEFAULT 1,
  CONSTRAINT ck_risk_flags_status CHECK (status IN ('open','cleared','confirmed')),
  CONSTRAINT ck_risk_flags_subject_type CHECK (subject_type ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT ck_risk_flags_rule CHECK (rule ~ '^[a-z][a-z0-9_.]*$'),
  CONSTRAINT ck_risk_flags_score CHECK (score BETWEEN 0 AND 100),
  CONSTRAINT ck_risk_flags_details CHECK (jsonb_typeof(details) = 'object'),
  CONSTRAINT ck_risk_flags_reviewed CHECK ((status = 'open') = (reviewed_at IS NULL))
);
CREATE INDEX ix_risk_flags_open ON risk_flags (created_at DESC) WHERE status = 'open';
CREATE INDEX ix_risk_flags_subject ON risk_flags (subject_type, subject_id, created_at DESC);
CREATE INDEX ix_risk_flags_rule ON risk_flags (rule, created_at DESC);
CREATE TRIGGER trg_risk_flags_touch BEFORE UPDATE ON risk_flags
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- idempotency_keys (02 §2.4 step 10, 04 §1.6): one row per principal + route + key,
-- kept 24 h with the response. state in_flight holds a lease until locked_until.
------------------------------------------------------------------------------
CREATE TABLE idempotency_keys (
  principal        text NOT NULL,
  route            text NOT NULL,
  key              text NOT NULL,
  request_hash     bytea NOT NULL,
  state            text NOT NULL DEFAULT 'in_flight',
  claim_id         uuid,         -- fencing token of the current in-flight claim
  locked_until     timestamptz,
  status_code      int,
  response_headers jsonb,        -- allow-listed headers only
  response_body    bytea,        -- AES-GCM under PII_ENC_KEY (D-18): version byte, nonce, sealed body
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  expires_at       timestamptz NOT NULL,
  CONSTRAINT pk_idempotency_keys PRIMARY KEY (principal, route, key),
  CONSTRAINT ck_idempotency_keys_state CHECK (state IN ('in_flight','done')),
  CONSTRAINT ck_idempotency_keys_done CHECK (state <> 'done' OR status_code IS NOT NULL),
  CONSTRAINT ck_idempotency_keys_claim CHECK (state <> 'in_flight' OR claim_id IS NOT NULL),
  CONSTRAINT ck_idempotency_keys_hash CHECK (octet_length(request_hash) = 32)
);
CREATE INDEX ix_idempotency_keys_expires ON idempotency_keys (expires_at);
CREATE TRIGGER trg_idempotency_keys_touch BEFORE UPDATE ON idempotency_keys
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

------------------------------------------------------------------------------
-- test_clock (D-80): one row, the offset the local/test clock adds to real
-- time, shared by api and worker. Read only when APP_ENV is local or test.
------------------------------------------------------------------------------
CREATE TABLE test_clock (
  id         boolean PRIMARY KEY DEFAULT true,
  offset_ms  bigint NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_test_clock_single CHECK (id)
);
INSERT INTO test_clock (id) VALUES (true);
CREATE TRIGGER trg_test_clock_touch BEFORE UPDATE ON test_clock
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- +goose StatementBegin
CREATE FUNCTION test_clock_notify() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  PERFORM pg_catalog.pg_notify('test_clock', NEW.offset_ms::text);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_test_clock_notify AFTER UPDATE ON test_clock
  FOR EACH ROW EXECUTE FUNCTION test_clock_notify();

-- +goose Down
DROP TABLE IF EXISTS test_clock;
DROP FUNCTION IF EXISTS test_clock_notify();
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS risk_flags;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS platform_settings;
DROP FUNCTION IF EXISTS platform_settings_notify();
DROP TABLE IF EXISTS platform_environment;
DROP FUNCTION IF EXISTS append_only_truncate();
DROP FUNCTION IF EXISTS append_only();
DROP FUNCTION IF EXISTS ensure_partitions(text, timestamptz);
DROP TABLE IF EXISTS partition_policies;
DROP FUNCTION IF EXISTS touch_updated_at();
-- Extensions stay: other databases' objects or later re-runs may rely on them,
-- and dropping postgis is slow. CREATE EXTENSION IF NOT EXISTS makes Up re-runnable.
