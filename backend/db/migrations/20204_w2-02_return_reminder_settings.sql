-- W2-02 delivery returns (D-218; 01 §5.3, §17): the two inbox reminders
-- for a failed delivery whose return is not confirmed become settings. The
-- 72 h auto-confirm stays a fixed constant (D-85). The bounds keep the
-- second reminder after the first and before the 72 h auto-confirm.

-- 01 §17 (D-218). TestSettingsMatchProductRules reads these inserts (D-95).
-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('delivery_return_first_reminder_hours', '2', 'hours', 1, 12, 'Orders and strikes. 01 §5.3: first inbox reminder after a failed delivery while the return is unconfirmed'),
  ('delivery_return_second_reminder_hours', '24', 'hours', 13, 71, 'Orders and strikes. 01 §5.3: second inbox reminder after a failed delivery while the return is unconfirmed');

-- +goose Down
DELETE FROM platform_settings WHERE key IN (
  'delivery_return_first_reminder_hours',
  'delivery_return_second_reminder_hours'
);
