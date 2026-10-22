-- Keys added to 01 §17 after 00002: the QA-book decisions (D-91–D-94) and the jobs platform (D-95).
-- TestSettingsMatchProductRules reads the platform_settings inserts of every migration (D-95).

-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('order_unseen_sms_after_min', '3', 'minutes', 0, NULL, 'Notifications. 01 §15: SMS to the shop if a new order is not seen'),
  ('pickup_hold_reminder_min', '10', 'minutes', 0, NULL, 'Notifications. 01 §15: hold ending push to the customer'),
  ('risk_code_lockouts_store_per_day', '10', 'count', 0, NULL, 'Risk. 01 §6: order-code lockouts at one store in an IST day that raise a risk flag'),
  ('credit_expiry_reminder_days', '7', 'days', 0, NULL, 'Credits and payouts. 01 §7: reminder before a lot expires'),
  ('review_edit_window_days', '7', 'days', 0, NULL, 'Reviews. 01 §10'),
  ('job_runs_retention_days', '90', 'days', 1, NULL, 'Jobs. 01 §13: how long job_runs rows are kept');

-- +goose Down
DELETE FROM platform_settings WHERE key IN (
  'order_unseen_sms_after_min',
  'pickup_hold_reminder_min',
  'risk_code_lockouts_store_per_day',
  'credit_expiry_reminder_days',
  'review_edit_window_days',
  'job_runs_retention_days'
);
