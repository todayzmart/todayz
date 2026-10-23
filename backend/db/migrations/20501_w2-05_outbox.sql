-- W2-05 realtime (02 §5; 03 §9; D-98): outbox_events, the SSE replay log, and
-- outbox_watermark, how far the 24 h cleanup has deleted.
--
-- An event is written by realtime.Publish inside the domain's own transaction,
-- with a pg_notify on the same transaction, so it is delivered only if that
-- transaction commits. Rows are kept 24 h (realtime_outbox_cleanup) and are
-- read only for Last-Event-ID replay; POS sync uses catalogue_changes (D-36).
--
-- Replay cursor (internal/platform/realtime): bigserial order is not commit
-- order, so a client cannot resume from a seq. Each row records the writing
-- transaction's id (xid8, never wraps) and a client resumes from a snapshot
-- xmin X: every transaction with xid < X had finished, and had been streamed
-- to that client, when X was handed out. Replay is `xid >= X`, a superset;
-- clients only refetch, so a duplicate is harmless and a gap is impossible.

-- +goose Up

CREATE TABLE outbox_events (
  seq        bigserial PRIMARY KEY,
  channel    text NOT NULL,
  -- {type, id, version}: what changed, never the data itself (02 §5).
  -- channel: 'admin', 'store:<ulid>' or 'user:<ulid>' (the API's id form).
  payload    jsonb NOT NULL,
  xid        xid8 NOT NULL DEFAULT pg_current_xact_id(),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_outbox_events_channel CHECK (
    channel = 'admin' OR channel ~ '^(store|user):[0-9A-HJKMNP-TV-Z]{26}$'),
  CONSTRAINT ck_outbox_events_payload CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 512)
);
CREATE INDEX ix_outbox_events_channel_xid ON outbox_events (channel, xid);
CREATE INDEX ix_outbox_events_created ON outbox_events (created_at);

-- One row: the greatest xid the cleanup has deleted. A cursor at or below it
-- may have lost rows to retention, so the stream answers `reset` (refetch
-- everything) instead of replaying.
CREATE TABLE outbox_watermark (
  id                  smallint PRIMARY KEY DEFAULT 1,
  deleted_through_xid xid8 NOT NULL DEFAULT '0',
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_outbox_watermark_one CHECK (id = 1)
);
INSERT INTO outbox_watermark (id) VALUES (1);
REVOKE INSERT, DELETE, TRUNCATE ON outbox_watermark FROM todayz_app;

-- +goose Down
DROP TABLE outbox_watermark;
DROP TABLE outbox_events;
