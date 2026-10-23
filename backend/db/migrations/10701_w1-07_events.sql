-- W1-07: the product events table (03 §Telemetry, 05 §2.6).
-- Partitioned daily on occurred_at; the platform maintenance job creates the
-- next partitions from partition_policies. Retention is 400 days: dropping
-- old partitions belongs to the jobs platform (W1-11), not to this migration.
-- No foreign keys: users, stores and businesses live in other blocks and a
-- bad id must never stop an event from being stored. Ids are ULIDs as uuid.
-- Free text is never stored: props are validated against the catalogue
-- (shared/telemetry/events) before the insert.

-- +goose Up
CREATE TABLE events (
  id           uuid NOT NULL,
  name         text NOT NULL,
  occurred_at  timestamptz NOT NULL,
  received_at  timestamptz NOT NULL DEFAULT now(),
  app          text NOT NULL,
  user_id      uuid,
  anon_id      uuid,
  session_id   uuid,
  store_id     uuid,
  business_id  uuid,
  props        jsonb NOT NULL DEFAULT '{}'::jsonb,
  geo_cell     text,  -- H3 resolution 8 index (15 hex digits); never coordinates (D-59)
  app_version  text,
  platform     text,
  CONSTRAINT pk_events PRIMARY KEY (id, occurred_at),
  CONSTRAINT ck_events_name CHECK (name ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(name) <= 64),
  CONSTRAINT ck_events_app CHECK (app IN ('customer', 'business', 'admin', 'site', 'api', 'worker')),
  CONSTRAINT ck_events_platform CHECK (platform IS NULL OR platform IN ('web', 'android', 'ios', 'desktop', 'other')),
  CONSTRAINT ck_events_geo_cell CHECK (geo_cell IS NULL OR geo_cell ~ '^[0-9a-f]{15}$'),
  CONSTRAINT ck_events_app_version CHECK (app_version IS NULL OR app_version ~ '^[A-Za-z0-9._+-]{1,40}$'),
  CONSTRAINT ck_events_props CHECK (jsonb_typeof(props) = 'object' AND pg_column_size(props) <= 4096)
) PARTITION BY RANGE (occurred_at);

CREATE TABLE events_default PARTITION OF events DEFAULT;
CREATE INDEX ix_events_name_time ON events (name, occurred_at);
CREATE INDEX brin_events_occurred_at ON events USING brin (occurred_at);

-- The app inserts and reads; it never truncates. ensure_partitions copies
-- these privileges to every partition it creates.
REVOKE TRUNCATE ON events, events_default FROM todayz_app;

INSERT INTO partition_policies (parent, granularity, ahead) VALUES ('events', 'day', 7);
SELECT ensure_partitions('events', now());

-- +goose Down
DELETE FROM partition_policies WHERE parent = 'events';
DROP TABLE IF EXISTS events;
