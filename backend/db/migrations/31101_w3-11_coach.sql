-- W3-11 shop coach and retailer AI (11 §2.1, AI-R4, AI-R5; 01 §12, §16; D-46, D-48, D-71, D-99).
--
-- coach_suggestions: one coach card (03). A detector in internal/coach/detectors
-- fires a fact; the chooser keeps the top few (coach_max_cards_per_day) and the
-- writer turns each into a title and body, by the model (generated_by ai, with
-- prompt_version and model) or by the rule template (generated_by rule). The
-- card is a draft a person acts on: its action_type and action_payload only
-- prefill a screen, nothing is applied (11 §1 rule 1). detector names the
-- detector, subject_ref what the card is about (a store item, a batch, a pair
-- a:b, or `store`), so a Not useful quiets that subject, never the whole kind,
-- until quiet_until (coach_dismiss_quiet_days). numbers holds every number the
-- text may use; the grounding check reads it. ai_draft_id is a plain uuid (no
-- draft row is kept for a background card: no person started it).
-- status: new → seen (shown) → done or dismissed; expired when valid_until
-- passed or the detector no longer fires. pushed_at is set when the card went
-- out as a push (coach_max_push_per_day). One live card per (store, detector,
-- subject): the partial unique index stops a rerun writing it twice.
--
-- coach_weights: the per-shop detector weights (11 §2.1 step 5): Done, Not
-- useful and ignored adjust weight_bp (10000 = neutral) inside 2500–20000.
--
-- insight_texts: the AI-R4 sentence for one Demand insights row in one
-- language, kept only while facts_hash still matches the row's facts, so a
-- changed number never shows an old sentence.
--
-- Timestamps are set from the injected clock by the queries (no touch trigger).

-- +goose Up

CREATE TABLE coach_suggestions (
  id             uuid PRIMARY KEY,
  store_id       uuid NOT NULL REFERENCES stores (id),
  detector       text NOT NULL,
  subject_ref    text NOT NULL,
  kind           text NOT NULL,
  topic          text NOT NULL,
  title          text NOT NULL,
  body           text NOT NULL,
  numbers        jsonb NOT NULL DEFAULT '{}'::jsonb,
  action_type    text NOT NULL DEFAULT 'none',
  action_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  priority       int NOT NULL DEFAULT 0,
  status         text NOT NULL DEFAULT 'new',
  generated_by   text NOT NULL,
  ai_draft_id    uuid,
  prompt_version text,
  model          text,
  lang           text NOT NULL DEFAULT 'en',
  job            text NOT NULL,
  day            date NOT NULL,
  valid_until    timestamptz NOT NULL,
  pushed_at      timestamptz,
  seen_at        timestamptz,
  acted_at       timestamptz,
  acted_by       uuid,
  quiet_until    timestamptz,
  created_at     timestamptz NOT NULL,
  updated_at     timestamptz NOT NULL,
  CONSTRAINT ck_coach_suggestions_detector CHECK (detector ~ '^[a-z][a-z0-9_]{1,59}$'),
  CONSTRAINT ck_coach_suggestions_subject CHECK (char_length(subject_ref) BETWEEN 1 AND 210),
  CONSTRAINT ck_coach_suggestions_kind CHECK (kind IN ('high', 'low', 'action', 'tip')),
  CONSTRAINT ck_coach_suggestions_topic CHECK (topic IN ('sales', 'stock', 'orders', 'reliability', 'offers',
    'reviews', 'demand', 'setup')),
  CONSTRAINT ck_coach_suggestions_title CHECK (char_length(title) BETWEEN 1 AND 60),
  CONSTRAINT ck_coach_suggestions_body CHECK (char_length(body) BETWEEN 1 AND 200),
  CONSTRAINT ck_coach_suggestions_numbers CHECK (jsonb_typeof(numbers) = 'object'),
  CONSTRAINT ck_coach_suggestions_action CHECK (action_type IN ('restock', 'create_offer', 'flash_offer',
    'add_product', 'mark_expired', 'edit_price', 'check_cost', 'count_stock', 'see_customers', 'reply_reviews',
    'turn_on_auto_accept', 'update_stock', 'enable_delivery', 'none')),
  CONSTRAINT ck_coach_suggestions_payload CHECK (jsonb_typeof(action_payload) = 'object'),
  CONSTRAINT ck_coach_suggestions_status CHECK (status IN ('new', 'seen', 'done', 'dismissed', 'expired')),
  CONSTRAINT ck_coach_suggestions_generated CHECK (generated_by IN ('rule', 'ai')),
  CONSTRAINT ck_coach_suggestions_ai CHECK ((generated_by = 'ai') = (model IS NOT NULL AND prompt_version IS NOT NULL)),
  CONSTRAINT ck_coach_suggestions_lang CHECK (lang IN ('en', 'hi', 'kn', 'te', 'ta')),
  CONSTRAINT ck_coach_suggestions_job CHECK (job IN ('coach_daily', 'coach_weekly', 'coach_realtime_rules')),
  CONSTRAINT ck_coach_suggestions_acted CHECK ((status IN ('done', 'dismissed')) = (acted_at IS NOT NULL)
                                               AND (acted_at IS NULL) = (acted_by IS NULL)),
  CONSTRAINT ck_coach_suggestions_quiet CHECK ((status = 'dismissed') = (quiet_until IS NOT NULL))
);
CREATE INDEX ix_coach_suggestions_store_status ON coach_suggestions (store_id, status, created_at DESC);
CREATE INDEX ix_coach_suggestions_store_day ON coach_suggestions (store_id, day);
CREATE INDEX ix_coach_suggestions_quiet ON coach_suggestions (store_id, quiet_until) WHERE status = 'dismissed';
CREATE UNIQUE INDEX ux_coach_suggestions_live ON coach_suggestions (store_id, detector, subject_ref)
  WHERE status IN ('new', 'seen');

CREATE TABLE coach_weights (
  store_id   uuid NOT NULL REFERENCES stores (id),
  detector   text NOT NULL,
  weight_bp  int NOT NULL DEFAULT 10000,
  shown      int NOT NULL DEFAULT 0,
  done       int NOT NULL DEFAULT 0,
  dismissed  int NOT NULL DEFAULT 0,
  ignored    int NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (store_id, detector),
  CONSTRAINT ck_coach_weights_weight CHECK (weight_bp BETWEEN 2500 AND 20000),
  CONSTRAINT ck_coach_weights_counts CHECK (shown >= 0 AND done >= 0 AND dismissed >= 0 AND ignored >= 0)
);

CREATE TABLE insight_texts (
  store_id       uuid NOT NULL REFERENCES stores (id),
  kind           text NOT NULL,
  subject_ref    text NOT NULL,
  lang           text NOT NULL,
  facts_hash     text NOT NULL,
  text           text NOT NULL,
  prompt_version text NOT NULL,
  model          text NOT NULL,
  created_at     timestamptz NOT NULL,
  PRIMARY KEY (store_id, kind, subject_ref, lang),
  CONSTRAINT ck_insight_texts_lang CHECK (lang IN ('en', 'hi', 'kn', 'te', 'ta')),
  CONSTRAINT ck_insight_texts_text CHECK (char_length(text) BETWEEN 1 AND 400),
  CONSTRAINT ck_insight_texts_hash CHECK (facts_hash ~ '^[0-9a-f]{64}$')
);

-- +goose Down
DROP TABLE IF EXISTS insight_texts;
DROP TABLE IF EXISTS coach_weights;
DROP TABLE IF EXISTS coach_suggestions;
