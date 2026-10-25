-- Background AI wording gets its own budget (D-228, W3-11 review).
--
-- ai_daily_calls_store_background: model calls a store's background wording
-- (AI-R4 insight sentences, AI-R5 coach card rewording) may make per IST day,
-- counted in ai_usage_daily under scope store_background, separate from the
-- calls the owner starts (ai_daily_calls_store). When it is spent the template
-- sentence shows; it is never an error.
--
-- ai_text_misses: the negative cache. A row's facts (kind, subject, language,
-- facts_hash) whose wording failed grounding or the schema is kept here, so the
-- same facts are not sent to the model again; a changed number changes the hash
-- and asks once more. feature is insight_text or coach_card; ref is the row's
-- kind/subject. created_at is set from the injected clock by the queries.
-- TestSettingsMatchProductRules reads the platform_settings inserts of every migration (D-95).

-- +goose Up
INSERT INTO platform_settings (key, value, unit, min, max, description) VALUES
  ('ai_daily_calls_store_background', '20', 'count', 0, 1000, 'AI. Background wording (insight sentences, coach cards) model calls per store per IST day, separate from ai_daily_calls_store; when spent the template shows (D-228)');

ALTER TABLE ai_usage_daily DROP CONSTRAINT ck_ai_usage_daily_scope;
ALTER TABLE ai_usage_daily ADD CONSTRAINT ck_ai_usage_daily_scope
  CHECK (scope IN ('store', 'customer', 'platform', 'store_background'));

CREATE TABLE ai_text_misses (
  feature    text NOT NULL,
  store_id   uuid NOT NULL REFERENCES stores (id),
  ref        text NOT NULL,
  lang       text NOT NULL,
  facts_hash text NOT NULL,
  reason     text NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (feature, store_id, ref, lang),
  CONSTRAINT ck_ai_text_misses_feature CHECK (feature IN ('insight_text', 'coach_card')),
  CONSTRAINT ck_ai_text_misses_lang CHECK (lang IN ('en', 'hi', 'kn', 'te', 'ta')),
  CONSTRAINT ck_ai_text_misses_hash CHECK (facts_hash ~ '^[0-9a-f]{64}$'),
  CONSTRAINT ck_ai_text_misses_reason CHECK (reason IN ('grounding', 'schema', 'refused', 'skipped'))
);

-- +goose Down
DROP TABLE IF EXISTS ai_text_misses;
DELETE FROM ai_usage_daily WHERE scope = 'store_background';
ALTER TABLE ai_usage_daily DROP CONSTRAINT ck_ai_usage_daily_scope;
ALTER TABLE ai_usage_daily ADD CONSTRAINT ck_ai_usage_daily_scope
  CHECK (scope IN ('store', 'customer', 'platform'));
DELETE FROM platform_settings WHERE key = 'ai_daily_calls_store_background';
