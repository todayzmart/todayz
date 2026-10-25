-- ai queries, generated into package aidb (gen/sqlc/aidb).
-- Only W3-10 (internal/ai/{guard,drafts}) edits this folder (D-50). Feature packages reach
-- ai_drafts through internal/ai/drafts, never with their own SQL.

-- name: InsertDraft :exec
INSERT INTO ai_drafts (id, feature, store_id, actor_user_id, input_ref, input_meta, prompt_version, status, created_at, updated_at)
VALUES (@id, @feature, sqlc.narg('store_id'), @actor_user_id, sqlc.narg('input_ref'), @input_meta, @prompt_version, 'processing', @now, @now);

-- name: GetDraft :one
SELECT * FROM ai_drafts WHERE id = @id;

-- name: LockDraft :one
SELECT * FROM ai_drafts WHERE id = @id FOR UPDATE;

-- name: MarkDraftReady :execrows
-- Only a processing draft becomes ready; a retried job finds it ready already and changes nothing.
UPDATE ai_drafts
SET status = 'ready', output = @output, model = @model, input_tokens = @input_tokens,
    output_tokens = @output_tokens, cost_paise = @cost_paise, latency_ms = @latency_ms, updated_at = @now
WHERE id = @id AND status = 'processing';

-- name: MarkDraftFailed :execrows
UPDATE ai_drafts
SET status = 'failed', failure = @failure, model = @model, input_tokens = @input_tokens,
    output_tokens = @output_tokens, cost_paise = @cost_paise, latency_ms = @latency_ms, updated_at = @now
WHERE id = @id AND status = 'processing';

-- name: CloseDraft :exec
-- Confirm or discard: the caller holds the row lock (LockDraft) and has checked it is ready.
UPDATE ai_drafts
SET status = @status, line_outcomes = @line_outcomes, confirmed_by = @confirmed_by,
    confirmed_at = @now, updated_at = @now
WHERE id = @id AND status = 'ready';

-- name: SettleDraftLines :exec
-- A partial confirm (Confirmation.KeepOpen): the lines settled so far, the draft stays ready.
-- The caller holds the row lock (LockDraft) and has checked it is ready.
UPDATE ai_drafts
SET line_outcomes = @line_outcomes, updated_at = @now
WHERE id = @id AND status = 'ready';

-- name: ReserveCall :one
-- Counts one call for (day, scope, subject) unless the count is already at the cap. No row back
-- means the cap is reached. A first call inserts calls = 1, so the caller checks cap >= 1 first.
INSERT INTO ai_usage_daily (day, scope, subject, calls, cost_paise, updated_at)
VALUES (@day, @scope, @subject, 1, 0, @now)
ON CONFLICT (day, scope, subject) DO UPDATE
SET calls = ai_usage_daily.calls + 1, updated_at = EXCLUDED.updated_at
WHERE ai_usage_daily.calls < sqlc.arg(cap)::int
RETURNING calls;

-- name: PlatformSpend :one
-- The platform's spend so far today (0 before the first call).
SELECT COALESCE((SELECT cost_paise FROM ai_usage_daily
                 WHERE day = @day AND scope = 'platform' AND subject = '00000000-0000-0000-0000-000000000000'), 0)::bigint AS cost_paise;

-- name: AddPlatformCost :one
-- Adds a finished call's cost to the platform row of the day and returns the new total.
INSERT INTO ai_usage_daily (day, scope, subject, calls, cost_paise, updated_at)
VALUES (@day, 'platform', '00000000-0000-0000-0000-000000000000', 0, @cost_paise, @now)
ON CONFLICT (day, scope, subject) DO UPDATE
SET cost_paise = ai_usage_daily.cost_paise + EXCLUDED.cost_paise, updated_at = EXCLUDED.updated_at
RETURNING cost_paise;
