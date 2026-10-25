-- W3-08 support tickets (01 §13, 03 support_tickets and support_messages, 06 A-14 and B-15; D-211, D-226).
--
-- W2-09 owned these tables but did not ship them, so W3-08 creates them in its own block
-- (D-226). A ticket is opened by a customer (C-19) or by a business owner or
-- manager (B-15). opener_user_id is the person who opened it; business_id is set for a
-- business ticket and only for one.
--
-- SLA (01 §13, D-226): sla_due_at is set exactly while the ticket is open, that is while
-- support owes a reply. It starts at opening or at the opener's next reply, from the
-- support_sla_<priority>_min setting at that moment, and is cleared by an agent's reply,
-- a resolve or a close. sla_started_at is when that timer started; a priority change
-- moves the due time to sla_started_at plus the new priority's minutes. Past the due
-- time, the inbox shows the ticket as overdue; nothing escalates on its own.
--
-- A note (kind = 'note') is written only by support and is never shown to the opener.
-- Attachments are objects in the private bucket (R2, MinIO locally), uploaded first
-- through a presigned PUT and linked to a message when the message is posted.
--
-- order_id gets its foreign key here (D-211: the package that creates the table adds it).

-- +goose Up

CREATE SEQUENCE support_ticket_number_seq START 1001;

CREATE TABLE support_tickets (
  id                uuid PRIMARY KEY,
  number            bigint NOT NULL DEFAULT nextval('support_ticket_number_seq'),
  display_number    text GENERATED ALWAYS AS ('SUP-' || lpad(number::text, greatest(6, length(number::text)), '0')) STORED,
  opener_type       text NOT NULL,
  opener_user_id    uuid NOT NULL REFERENCES users (id),
  business_id       uuid REFERENCES businesses (id),
  store_id          uuid REFERENCES stores (id),
  order_id          uuid REFERENCES orders (id),
  category          text NOT NULL,
  subject           text NOT NULL,
  status            text NOT NULL DEFAULT 'open',
  priority          text NOT NULL DEFAULT 'normal',
  assignee_id       uuid REFERENCES admin_users (user_id),
  sla_started_at    timestamptz,
  sla_due_at        timestamptz,
  first_response_at timestamptz,
  last_message_at   timestamptz NOT NULL,
  resolved_at       timestamptz,
  closed_at         timestamptz,
  close_reason      text,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  version           int NOT NULL DEFAULT 1,
  CONSTRAINT ck_support_tickets_opener CHECK (opener_type IN ('customer','business')),
  CONSTRAINT ck_support_tickets_business CHECK ((opener_type = 'business') = (business_id IS NOT NULL)),
  CONSTRAINT ck_support_tickets_category CHECK (category IN
    ('order','credits','payouts','catalogue','account','verification','app','other')),
  CONSTRAINT ck_support_tickets_subject CHECK (char_length(subject) BETWEEN 3 AND 120),
  CONSTRAINT ck_support_tickets_status CHECK (status IN ('open','waiting','resolved','closed')),
  CONSTRAINT ck_support_tickets_priority CHECK (priority IN ('normal','high','urgent')),
  CONSTRAINT ck_support_tickets_sla CHECK ((status = 'open') = (sla_due_at IS NOT NULL)),
  CONSTRAINT ck_support_tickets_sla_start CHECK ((status = 'open') = (sla_started_at IS NOT NULL)),
  CONSTRAINT ck_support_tickets_resolved CHECK ((status = 'resolved') = (resolved_at IS NOT NULL)),
  CONSTRAINT ck_support_tickets_closed CHECK ((status = 'closed') = (closed_at IS NOT NULL)),
  CONSTRAINT ck_support_tickets_close_reason CHECK (close_reason IS NULL OR char_length(close_reason) BETWEEN 1 AND 500)
);
CREATE UNIQUE INDEX uq_support_tickets_number ON support_tickets (number);
CREATE INDEX ix_support_tickets_open_sla ON support_tickets (sla_due_at, id) WHERE status = 'open';
CREATE INDEX ix_support_tickets_status ON support_tickets (status, last_message_at DESC, id DESC);
CREATE INDEX ix_support_tickets_business ON support_tickets (business_id, last_message_at DESC, id DESC)
  WHERE business_id IS NOT NULL;
CREATE INDEX ix_support_tickets_opener ON support_tickets (opener_user_id, last_message_at DESC);
CREATE INDEX ix_support_tickets_assignee ON support_tickets (assignee_id, status) WHERE assignee_id IS NOT NULL;
CREATE INDEX ix_support_tickets_order ON support_tickets (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX ix_support_tickets_store ON support_tickets (store_id) WHERE store_id IS NOT NULL;
CREATE TRIGGER trg_support_tickets_touch BEFORE UPDATE ON support_tickets
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE support_messages (
  id             uuid PRIMARY KEY,
  ticket_id      uuid NOT NULL REFERENCES support_tickets (id) ON DELETE CASCADE,
  author_type    text NOT NULL,
  author_user_id uuid NOT NULL REFERENCES users (id),
  kind           text NOT NULL DEFAULT 'reply',
  body           text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_support_messages_author CHECK (author_type IN ('customer','business','admin')),
  CONSTRAINT ck_support_messages_kind CHECK (kind IN ('reply','note')),
  CONSTRAINT ck_support_messages_note CHECK (kind = 'reply' OR author_type = 'admin'),
  CONSTRAINT ck_support_messages_body CHECK (char_length(body) BETWEEN 1 AND 4000)
);
CREATE INDEX ix_support_messages_ticket ON support_messages (ticket_id, created_at, id);

-- An upload belongs to the person who asked for it (and, for a business upload, to that
-- business; a customer's or an agent's upload has none) until a message claims it. message_id is set once and never moves.
CREATE TABLE support_attachments (
  id           uuid PRIMARY KEY,
  uploaded_by  uuid NOT NULL REFERENCES users (id),
  business_id  uuid REFERENCES businesses (id),
  ticket_id    uuid REFERENCES support_tickets (id) ON DELETE CASCADE,
  message_id   uuid REFERENCES support_messages (id) ON DELETE CASCADE,
  object_key   text NOT NULL,
  content_type text NOT NULL,
  size_bytes   int NOT NULL,
  file_name    text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_support_attachments_type CHECK (content_type IN ('image/jpeg','image/png','image/webp','application/pdf')),
  CONSTRAINT ck_support_attachments_size CHECK (size_bytes BETWEEN 1 AND 10485760),
  CONSTRAINT ck_support_attachments_name CHECK (char_length(file_name) BETWEEN 1 AND 120),
  CONSTRAINT ck_support_attachments_linked CHECK ((ticket_id IS NULL) = (message_id IS NULL))
);
CREATE UNIQUE INDEX uq_support_attachments_key ON support_attachments (object_key);
CREATE INDEX ix_support_attachments_message ON support_attachments (message_id) WHERE message_id IS NOT NULL;
CREATE INDEX ix_support_attachments_ticket ON support_attachments (ticket_id) WHERE ticket_id IS NOT NULL;
CREATE INDEX ix_support_attachments_uploader ON support_attachments (uploaded_by, created_at DESC);
CREATE INDEX ix_support_attachments_business ON support_attachments (business_id) WHERE business_id IS NOT NULL;

-- 01 §17 (D-226). TestSettingsMatchProductRules reads these inserts (D-95).
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('support_sla_urgent_min', '60', 'minutes', 15, 10080, 'Support. 01 §13: time to the first or next support reply on an urgent ticket'),
  ('support_sla_high_min', '240', 'minutes', 15, 10080, 'Support. 01 §13: time to the first or next support reply on a high-priority ticket'),
  ('support_sla_normal_min', '1440', 'minutes', 15, 10080, 'Support. 01 §13: time to the first or next support reply on a normal ticket');

-- +goose Down
DELETE FROM platform_settings WHERE key IN (
  'support_sla_urgent_min',
  'support_sla_high_min',
  'support_sla_normal_min'
);
DROP TABLE support_attachments;
DROP TABLE support_messages;
DROP TABLE support_tickets;
DROP SEQUENCE support_ticket_number_seq;
