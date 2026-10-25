-- W3-05 A-16: the integrations row shows each channel's latest failed
-- delivery. Failed rows are few, so the partial index stays small while the
-- table grows with every message.

-- +goose Up
CREATE INDEX ix_notification_deliveries_failed ON notification_deliveries (channel, updated_at DESC) WHERE status = 'failed';

-- +goose Down
DROP INDEX ix_notification_deliveries_failed;
