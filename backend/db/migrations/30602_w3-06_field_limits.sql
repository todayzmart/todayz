-- W3-06 field agents: the per-agent owner-code cap (the lead's review of b42d9ee).
--
--   field_owner_codes_per_agent_hour   owner codes one field agent may send in a rolling
--                                      hour, over every business and number. The per-number
--                                      limit (01 §2) alone let an agent send one code each to
--                                      many numbers from many drafts.
--
-- The cap reads one agent's codes in the last hour, so sent_by's index gains created_at.

-- +goose Up

CREATE INDEX ix_field_owner_codes_sent_by_created ON field_owner_codes (sent_by, created_at DESC);
DROP INDEX ix_field_owner_codes_sent_by;

-- 01 §17. TestSettingsMatchProductRules reads this insert (D-95).
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('field_owner_codes_per_agent_hour', '10', 'count', 1, 30, 'Field agents. 01 §14: owner codes one agent may send in a rolling hour');

-- +goose Down
DELETE FROM platform_settings WHERE key = 'field_owner_codes_per_agent_hour';
CREATE INDEX ix_field_owner_codes_sent_by ON field_owner_codes (sent_by);
DROP INDEX ix_field_owner_codes_sent_by_created;
