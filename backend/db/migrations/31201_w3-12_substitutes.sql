-- Rule-based substitutes (AI-C3, W3-12, D-235).
--
-- substitute_pack_tolerance_pct: how far a same-shop substitute's pack may be
-- from the out-of-stock item's, in percent either way, compared in base units
-- (g, ml, pc, pack). The nearby half uses the search radius and needs no setting.
-- TestSettingsMatchProductRules reads the platform_settings inserts of every migration (D-95).

-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('substitute_pack_tolerance_pct', '25', 'count', 5, 100, 'Search. AI-C3 substitutes: how far a same-shop substitute''s pack may be from the item''s, in percent either way (D-235)');

-- +goose Down
DELETE FROM platform_settings WHERE key = 'substitute_pack_tolerance_pct';
