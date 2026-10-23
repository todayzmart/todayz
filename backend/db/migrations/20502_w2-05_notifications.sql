-- W2-05 notifications (01 §15; 03 §9): the in-app inbox, deliveries per
-- channel, per-kind preferences, and the once-per-order marker of the
-- "new order not seen" SMS.
--
-- Extension of 03 §9 (reported to the lead): a notification is for a user
-- (customer inbox, C-20) or for a business (business inbox, B-16, D-46), so
-- user_id is nullable and business_id / store_id are added; exactly one of
-- user_id and business_id is set. The text is rendered when it is read, from
-- the kind's template in the reader's language (falling back to English) and
-- params, which never hold a phone, code, address or token. ref_type/ref_id
-- is what the row opens (an order, an ask, a review).
--
-- Phones: a delivery that needs one keeps it only as to_enc, AES-256-GCM under
-- PII_ENC_KEY (platform/pii); it is cleared once the delivery ends.

-- +goose Up

CREATE TABLE notifications (
  id          uuid PRIMARY KEY,
  user_id     uuid,
  business_id uuid,
  store_id    uuid,
  app         text NOT NULL,
  kind        text NOT NULL,
  params      jsonb NOT NULL DEFAULT '{}',
  ref_type    text,
  ref_id      uuid,
  read_at     timestamptz,
  created_at  timestamptz NOT NULL,
  CONSTRAINT fk_notifications_user_id FOREIGN KEY (user_id) REFERENCES users (id),
  CONSTRAINT fk_notifications_business_id FOREIGN KEY (business_id) REFERENCES businesses (id),
  CONSTRAINT fk_notifications_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT ck_notifications_owner CHECK ((user_id IS NULL) <> (business_id IS NULL)),
  CONSTRAINT ck_notifications_store CHECK (store_id IS NULL OR business_id IS NOT NULL),
  CONSTRAINT ck_notifications_app CHECK (app IN ('customer','business','admin')),
  CONSTRAINT ck_notifications_kind CHECK (kind ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(kind) <= 63),
  CONSTRAINT ck_notifications_ref CHECK ((ref_type IS NULL) = (ref_id IS NULL)
    AND (ref_type IS NULL OR ref_type ~ '^[a-z][a-z_]{0,30}$')),
  CONSTRAINT ck_notifications_params CHECK (jsonb_typeof(params) = 'object' AND octet_length(params::text) <= 2048)
);
CREATE INDEX ix_notifications_user ON notifications (user_id, app, created_at DESC, id DESC) WHERE user_id IS NOT NULL;
CREATE INDEX ix_notifications_user_unread ON notifications (user_id, app) WHERE user_id IS NOT NULL AND read_at IS NULL;
CREATE INDEX ix_notifications_business ON notifications (business_id, created_at DESC, id DESC) WHERE business_id IS NOT NULL;
CREATE INDEX ix_notifications_business_unread ON notifications (business_id) WHERE business_id IS NOT NULL AND read_at IS NULL;
CREATE INDEX ix_notifications_created ON notifications (created_at);

-- One row per external channel attempt. status: queued (written with the
-- notification), sending (the provider call is about to be made; a retry that
-- finds it never calls the provider again, so an SMS is sent at most once),
-- sent, delivered (provider callback, later), failed.
CREATE TABLE notification_deliveries (
  id              uuid PRIMARY KEY,
  notification_id uuid NOT NULL,
  channel         text NOT NULL,
  provider        text NOT NULL,
  provider_id     text,
  status          text NOT NULL DEFAULT 'queued',
  error           text,
  cost_paise      bigint NOT NULL DEFAULT 0,
  lang            text NOT NULL DEFAULT 'en',
  to_enc          bytea,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT fk_notification_deliveries_notification_id FOREIGN KEY (notification_id) REFERENCES notifications (id) ON DELETE CASCADE,
  CONSTRAINT ck_notification_deliveries_channel CHECK (channel IN ('push','sms','whatsapp')),
  CONSTRAINT ck_notification_deliveries_status CHECK (status IN ('queued','sending','sent','delivered','failed')),
  CONSTRAINT ck_notification_deliveries_provider CHECK (provider ~ '^[a-z][a-z0-9_]{0,31}$'),
  CONSTRAINT ck_notification_deliveries_provider_id CHECK (provider_id IS NULL OR length(provider_id) <= 128),
  CONSTRAINT ck_notification_deliveries_error CHECK (error IS NULL OR length(error) <= 200),
  CONSTRAINT ck_notification_deliveries_cost CHECK (cost_paise >= 0),
  CONSTRAINT ck_notification_deliveries_lang CHECK (lang IN ('en','hi','kn','te','ta'))
);
CREATE INDEX ix_notification_deliveries_notification ON notification_deliveries (notification_id);
CREATE INDEX ix_notification_deliveries_status ON notification_deliveries (status, created_at) WHERE status IN ('queued','sending');

-- Per user, app, kind and external channel; no row means on. The inbox is
-- always written whatever the preference (01 §15).
CREATE TABLE notification_prefs (
  user_id    uuid NOT NULL,
  app        text NOT NULL,
  kind       text NOT NULL,
  channel    text NOT NULL,
  enabled    boolean NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT pk_notification_prefs PRIMARY KEY (user_id, app, kind, channel),
  CONSTRAINT fk_notification_prefs_user_id FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT ck_notification_prefs_app CHECK (app IN ('customer','business','admin')),
  CONSTRAINT ck_notification_prefs_kind CHECK (kind ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$' AND length(kind) <= 63),
  CONSTRAINT ck_notification_prefs_channel CHECK (channel IN ('push','sms','whatsapp'))
);

-- The "new order not seen" SMS goes once per order (01 §15): the row is the
-- claim, written in the transaction that queues the message.
CREATE TABLE notify_order_escalations (
  order_id        uuid PRIMARY KEY,
  store_id        uuid NOT NULL,
  notification_id uuid NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT fk_notify_order_escalations_store_id FOREIGN KEY (store_id) REFERENCES stores (id),
  CONSTRAINT fk_notify_order_escalations_notification_id FOREIGN KEY (notification_id) REFERENCES notifications (id) ON DELETE CASCADE
);
CREATE INDEX ix_notify_order_escalations_created ON notify_order_escalations (created_at);

-- +goose Down
DROP TABLE notify_order_escalations;
DROP TABLE notification_prefs;
DROP TABLE notification_deliveries;
DROP TABLE notifications;
