-- W3-10 AI platform (11 §1 rules 7, 9, 10, 11; D-46, D-70, D-101; 03 ai_drafts).
--
-- ai_drafts: every AI draft of every feature is one row (D-46; voice_drafts is
-- folded in as feature `voice_stock`). A draft is a proposal: nothing changes
-- stock, money, orders, reviews or KYC until a person confirms, and confirm
-- writes through the owning domain with the confirmer's permissions per line
-- (D-06). feature is a snake_case name; the Go registry in internal/ai/guard
-- holds the list, so a new feature needs no migration. store_id is set for
-- business features and null for customer and admin ones.
--
-- status: processing (created, the model is running) → ready (lines to review)
-- → confirmed or discarded (every line discarded); failed when the model call,
-- the schema check or the grounding check failed. 04 §2.8 shows the API's
-- processing|ready|failed; 03's draft|confirmed|discarded|failed is the same
-- life with `draft` split into processing and ready (W3-10 REPORT open question).
-- line_outcomes: [{lineId, outcome}] with outcome accepted|edited|discarded|
-- not_permitted (04 §2.8 and dashboard 14; 03 says confirmed|rejected for
-- accepted|discarded). The shape is checked in Go; the column only has to be
-- an array.
--
-- ai_usage_daily: the cost guard's counters per IST day (11 §1 rule 7). scope
-- store and customer count calls against ai_daily_calls_store and
-- ai_daily_calls_customer; scope platform (subject is the nil uuid) counts every
-- call and the day's spend against ai_daily_cost_cap_paise. The customer search
-- features that keep no draft (D-101) count here too. No PII: subject is a
-- store or user id.
--
-- created_at, updated_at and the day are set from the injected clock by the
-- queries (no touch trigger).

-- +goose Up

CREATE TABLE ai_drafts (
  id             uuid PRIMARY KEY,
  feature        text NOT NULL,
  store_id       uuid REFERENCES stores (id),
  actor_user_id  uuid NOT NULL REFERENCES users (id),
  input_ref      text,
  input_meta     jsonb NOT NULL DEFAULT '{}'::jsonb,
  prompt_version text NOT NULL,
  model          text NOT NULL DEFAULT '',
  status         text NOT NULL DEFAULT 'processing',
  output         jsonb,
  line_outcomes  jsonb,
  failure        text,
  confirmed_by   uuid REFERENCES users (id),
  confirmed_at   timestamptz,
  input_tokens   int NOT NULL DEFAULT 0,
  output_tokens  int NOT NULL DEFAULT 0,
  cost_paise     bigint NOT NULL DEFAULT 0,
  latency_ms     int NOT NULL DEFAULT 0,
  created_at     timestamptz NOT NULL,
  updated_at     timestamptz NOT NULL,
  CONSTRAINT ck_ai_drafts_feature CHECK (feature ~ '^[a-z][a-z0-9_]{1,39}$'),
  CONSTRAINT ck_ai_drafts_status CHECK (status IN ('processing', 'ready', 'confirmed', 'discarded', 'failed')),
  CONSTRAINT ck_ai_drafts_output CHECK (status IN ('processing', 'failed') OR output IS NOT NULL),
  CONSTRAINT ck_ai_drafts_outcomes CHECK (line_outcomes IS NULL OR jsonb_typeof(line_outcomes) = 'array'),
  CONSTRAINT ck_ai_drafts_confirmed CHECK ((status IN ('confirmed', 'discarded')) = (confirmed_at IS NOT NULL)
                                          AND (confirmed_at IS NULL) = (confirmed_by IS NULL)),
  CONSTRAINT ck_ai_drafts_failure CHECK ((status = 'failed') = (failure IS NOT NULL)),
  CONSTRAINT ck_ai_drafts_counts CHECK (input_tokens >= 0 AND output_tokens >= 0 AND cost_paise >= 0 AND latency_ms >= 0)
);
CREATE INDEX ix_ai_drafts_store ON ai_drafts (store_id, created_at) WHERE store_id IS NOT NULL;
CREATE INDEX ix_ai_drafts_actor ON ai_drafts (actor_user_id, created_at);
CREATE INDEX ix_ai_drafts_confirmed_by ON ai_drafts (confirmed_by) WHERE confirmed_by IS NOT NULL;
CREATE INDEX ix_ai_drafts_feature ON ai_drafts (feature, created_at);

CREATE TABLE ai_usage_daily (
  day        date NOT NULL,
  scope      text NOT NULL,
  subject    uuid NOT NULL,
  calls      int NOT NULL DEFAULT 0,
  cost_paise bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (day, scope, subject),
  CONSTRAINT ck_ai_usage_daily_scope CHECK (scope IN ('store', 'customer', 'platform')),
  CONSTRAINT ck_ai_usage_daily_counts CHECK (calls >= 0 AND cost_paise >= 0)
);

-- +goose Down
DROP TABLE IF EXISTS ai_usage_daily;
DROP TABLE IF EXISTS ai_drafts;
