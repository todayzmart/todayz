-- W3-05 admin console: the indexes its screens read through.
-- A-07 order detail lists the order's notifications; A-19 filters the audit log
-- by actor and action already (baseline indexes), and by subject (baseline).

-- +goose Up
CREATE INDEX ix_notifications_ref ON notifications (ref_type, ref_id, created_at) WHERE ref_id IS NOT NULL;

-- +goose Down
DROP INDEX ix_notifications_ref;
