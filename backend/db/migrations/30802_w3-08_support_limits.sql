-- W3-08 support: abuse limits, admin uploads tied to their ticket, the inbox counts index
-- (01 §13 and §17, D-226; the lead's review of b45d936).
--
-- Limits, per user, read at the moment of the request:
--   support_open_tickets_max   tickets one person may have open or waiting at once
--   support_tickets_per_hour   tickets one person may open in a rolling hour
--   support_uploads_per_hour   attachment uploads one person may ask for in a rolling hour
--   support_upload_ttl_hours   an upload no message has claimed is refused after this and
--                              the support_uploads_sweep job deletes it (row and object)
--
-- An admin upload is asked for on one ticket (POST /v1/admin/tickets/{ticketId}/attachments)
-- and now records it, so only a message on that ticket may claim it. A customer or
-- business upload still has no ticket until a message claims it.

-- +goose Up

ALTER TABLE support_attachments DROP CONSTRAINT ck_support_attachments_linked;
ALTER TABLE support_attachments ADD CONSTRAINT ck_support_attachments_linked
  CHECK (message_id IS NULL OR ticket_id IS NOT NULL);

-- The sweep reads unclaimed uploads oldest first.
CREATE INDEX ix_support_attachments_unclaimed ON support_attachments (created_at, id) WHERE message_id IS NULL;

-- The open-ticket cap and the creation rate read one person's tickets.
CREATE INDEX ix_support_tickets_opener_created ON support_tickets (opener_user_id, created_at DESC);
CREATE INDEX ix_support_tickets_opener_active ON support_tickets (opener_user_id) WHERE status IN ('open','waiting');

-- CountTickets counts each status inside its own range of this index (index-only).
CREATE INDEX ix_support_tickets_counts ON support_tickets (status, opener_type, assignee_id) INCLUDE (sla_due_at);

-- 01 §17 (D-226). TestSettingsMatchProductRules reads these inserts (D-95).
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('support_open_tickets_max', '5', 'count', 1, 50, 'Support. 01 §13: tickets one person may have open or waiting at once'),
  ('support_tickets_per_hour', '5', 'count', 1, 60, 'Support. 01 §13: tickets one person may open in a rolling hour'),
  ('support_uploads_per_hour', '30', 'count', 5, 300, 'Support. 01 §13: attachment uploads one person may ask for in a rolling hour'),
  ('support_upload_ttl_hours', '24', 'hours', 1, 168, 'Support. 01 §13: an upload no message has claimed is refused and deleted after this');

-- +goose Down
DELETE FROM platform_settings WHERE key IN (
  'support_open_tickets_max',
  'support_tickets_per_hour',
  'support_uploads_per_hour',
  'support_upload_ttl_hours'
);
DROP INDEX ix_support_tickets_counts;
DROP INDEX ix_support_tickets_opener_active;
DROP INDEX ix_support_tickets_opener_created;
DROP INDEX ix_support_attachments_unclaimed;
DELETE FROM support_attachments WHERE message_id IS NULL AND ticket_id IS NOT NULL;
ALTER TABLE support_attachments DROP CONSTRAINT ck_support_attachments_linked;
ALTER TABLE support_attachments ADD CONSTRAINT ck_support_attachments_linked
  CHECK ((ticket_id IS NULL) = (message_id IS NULL));
