-- Keys added to 01 §17 by Demand insights v2 (D-99).
-- TestSettingsMatchProductRules reads the platform_settings inserts of every migration (D-95).

-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('insight_cover_days', '7', 'days', 1, 30, 'Demand insights and coach. "Selling fast": the restock quantity covers this many days at the current pace, and a row fires below this many days of stock'),
  ('insight_missed_sales_min_zero_days', '1', 'days', 1, 30, 'Demand insights and coach. "Missed sales": whole IST days at closing stock 0 in the period before the row can fire'),
  ('insight_missed_sales_min_searches', '10', 'count', 10, NULL, 'Demand insights and coach. "Missed sales": nearby searches matching the product on those days (never below 10, §16)'),
  ('insight_pair_min_bills', '20', 'count', 5, 1000, 'Demand insights and coach. "Bought together": bills containing both items in the window'),
  ('insight_pair_window_days', '60', 'days', 14, 180, 'Demand insights and coach. "Bought together": bill window for the pair counts'),
  ('insight_pair_min_share_bp', '2000', 'bp', 500, 10000, 'Demand insights and coach. "Bought together": share of item A''s bills that also contain B'),
  ('insight_pair_min_lift_pct', '150', 'count', 100, 1000, 'Demand insights and coach. "Bought together": lift needed in percent, P(B given A) / P(B); 150 = 1.5x'),
  ('insight_regular_min_visits', '3', 'count', 2, 20, 'Demand insights and coach. "Regulars slipping away": visits (distinct IST purchase days) needed in the regular window'),
  ('insight_regular_window_days', '60', 'days', 14, 180, 'Demand insights and coach. "Regulars slipping away": window in which the visits are counted'),
  ('insight_regular_lapse_days', '21', 'days', 7, 90, 'Demand insights and coach. "Regulars slipping away": no purchase for at least this many days'),
  ('insight_regular_min_linked_customers', '10', 'count', 1, NULL, 'Demand insights and coach. "Regulars slipping away": phone-linked customers in the window before the kind shows anything'),
  ('insight_price_compare_enabled', 'false', 'bool', NULL, NULL, 'Demand insights and coach. "Priced above nearby" on or off (Q-52); needs at least 10 other live verified stores of distinct businesses in the radius');

UPDATE platform_settings
   SET description = 'Demand insights and coach. Insight period ("Last 7 days") and the window for the average daily pace'
 WHERE key = 'insight_period_days';

-- +goose Down
UPDATE platform_settings
   SET description = 'Demand insights and coach. Insight period ("Past 7 days")'
 WHERE key = 'insight_period_days';

DELETE FROM platform_settings WHERE key IN (
  'insight_cover_days',
  'insight_missed_sales_min_zero_days',
  'insight_missed_sales_min_searches',
  'insight_pair_min_bills',
  'insight_pair_window_days',
  'insight_pair_min_share_bp',
  'insight_pair_min_lift_pct',
  'insight_regular_min_visits',
  'insight_regular_window_days',
  'insight_regular_lapse_days',
  'insight_regular_min_linked_customers',
  'insight_price_compare_enabled'
);
