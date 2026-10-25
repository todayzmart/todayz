-- W2-15 saved addresses (D-222; 01 §5.5, §17): how many addresses one
-- customer keeps becomes a setting instead of a constant in Go. The cap is
-- checked in Go in the write transaction (under the per-user lock).

-- 01 §17 (D-222). TestSettingsMatchProductRules reads these inserts (D-95).
-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('customer_max_saved_addresses', '20', 'count', 1, 50, 'Saved addresses. Addresses one customer can keep (422 validation_failed beyond)');

-- +goose Down
DELETE FROM platform_settings WHERE key IN ('customer_max_saved_addresses');
