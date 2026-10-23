-- Keys added to 01 §17 by Ask shops nearby and search proof (D-101).
-- TestSettingsMatchProductRules reads the platform_settings inserts of every migration (D-95).

-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('ask_daily_limit_customer', '5', 'count', 1, 50, 'Ask shops nearby. Asks one customer can send per IST day'),
  ('ask_radius_m', '3000', 'meters', 500, 10000, 'Ask shops nearby. Stores within this distance of the search location get the ask'),
  ('ask_max_stores', '8', 'count', 1, 20, 'Ask shops nearby. Stores one ask goes to'),
  ('ask_daily_limit_store', '40', 'count', 5, 200, 'Ask shops nearby. Asks one store receives per IST day; after that it gets none until midnight'),
  ('ask_reply_window_min', '15', 'minutes', 5, 120, 'Ask shops nearby. Time a store has to reply; then the ask closes for that store'),
  ('ask_price_valid_min', '60', 'minutes', 15, 240, 'Ask shops nearby. A replied price holds for booking this long after the reply (Q-53)'),
  ('ask_low_yield_after_count', '20', 'count', 5, 100, 'Ask shops nearby. A customer''s last asks in a row with no Yes before their asks are narrowed'),
  ('ask_low_yield_max_stores', '3', 'count', 1, 20, 'Ask shops nearby. Stores a narrowed customer''s ask goes to, until one of their online orders is collected or delivered'),
  ('ask_retention_days', '90', 'days', 30, 365, 'Ask shops nearby. Asks are deleted after this with their recipients, replies and store blocks; also how long a shop can report an ask'),
  ('ask_whatsapp_enabled', 'false', 'bool', NULL, NULL, 'Ask shops nearby. WhatsApp delivery of asks to shops; a Wave 5 option, off in v1 (Q-55)'),
  ('ask_gap_min_customers', '10', 'count', 10, NULL, 'Ask shops nearby. Distinct customers who asked for a product in a city before it shows in Demand gaps (never below 10, §16)'),
  ('ask_gap_window_days', '7', 'days', 1, 30, 'Ask shops nearby. Demand gaps window'),
  ('site_marquee_min_searchers', '10', 'count', 10, NULL, 'Site strip. Distinct signed-in customers who searched a product in a city before the nightly build proposes it (never below 10, §16)'),
  ('site_marquee_window_days', '7', 'days', 1, 30, 'Site strip. Search window'),
  ('site_marquee_max_items', '20', 'count', 5, 50, 'Site strip. Products proposed per city'),
  ('refine_ai_min_words', '4', 'count', 2, 20, 'Search. Words a query with no chip needs before search sets refineHint and the app may call search assist (AI-C2)'),
  ('ai_voicesearch_enabled', 'false', 'bool', NULL, NULL, 'AI. AI-C5'),
  ('share_card_create_per_hour', '30', 'count', 5, 300, 'Search proof. Share cards one IP or anonymous device can create per hour (429); re-creating an existing card does not count');

-- +goose Down
DELETE FROM platform_settings WHERE key IN (
  'ask_daily_limit_customer',
  'ask_radius_m',
  'ask_max_stores',
  'ask_daily_limit_store',
  'ask_reply_window_min',
  'ask_price_valid_min',
  'ask_low_yield_after_count',
  'ask_low_yield_max_stores',
  'ask_retention_days',
  'ask_whatsapp_enabled',
  'ask_gap_min_customers',
  'ask_gap_window_days',
  'site_marquee_min_searchers',
  'site_marquee_window_days',
  'site_marquee_max_items',
  'refine_ai_min_words',
  'ai_voicesearch_enabled',
  'share_card_create_per_hour'
);
