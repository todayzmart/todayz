-- The risk alert on one admin's day of wallet adjustments across all customers
-- (D-246 B9). The two-person rule (D-237) sums one admin's adjustments to one
-- customer; this catches many small ones spread over many customers.
-- Block 497xx (go-live gaps) so it never takes a number the W4-98 fixes use.

-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('admin_adjust_daily_total_alert_above_paise', '500000', 'paise', 0, 10000000, 'Credits and payouts. 01 §7: the risk alert fires when one admin''s wallet adjustments in an IST day, across all customers, pass this (D-246)');

-- +goose Down
DELETE FROM platform_settings WHERE key = 'admin_adjust_daily_total_alert_above_paise';
