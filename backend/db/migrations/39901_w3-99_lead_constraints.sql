-- Wave 3 constraints migration (D-79, 03 §13; lead block w3-99, D-211).
--
-- The one same-wave column 03 §13 lists for Wave 3: coach_suggestions
-- .ai_draft_id (W3-11) points at ai_drafts (W3-10). The coach never sets it
-- today (a background card keeps no draft row), so every row is null. W3-11
-- created it without an index; it gets one here. The other Wave 3 deferred
-- FKs were added by the packages that created their targets (qr_standees in
-- 30901, ask_recipients in 31601). Rollup product_id columns stay unconstrained
-- by design (rollups outlive merged or removed products).

-- +goose Up

-- ai_drafts (W3-10)
CREATE INDEX ix_coach_suggestions_ai_draft_id ON coach_suggestions (ai_draft_id) WHERE ai_draft_id IS NOT NULL;
ALTER TABLE coach_suggestions ADD CONSTRAINT fk_coach_suggestions_ai_draft_id FOREIGN KEY (ai_draft_id) REFERENCES ai_drafts (id) ON DELETE SET NULL;

-- +goose Down

ALTER TABLE coach_suggestions DROP CONSTRAINT fk_coach_suggestions_ai_draft_id;
DROP INDEX ix_coach_suggestions_ai_draft_id;
