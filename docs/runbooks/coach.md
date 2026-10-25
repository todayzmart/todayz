# Shop coach jobs (W3-11)

Kinds: `coach_daily` (06:00 IST, batched), `coach_weekly` (Mon 07:00 IST, batched),
`coach_realtime_rules` (every 15 min, rules only). Class `ai`: they write coach cards
(`coach_suggestions`) a person acts on; nothing changes stock, money or orders.

## A run failed or is late

1. Admin → Jobs: open the run. `summary.failed` counts stores (or detectors) that errored;
   the run goes on past them. The log line `coach: store run failed` names the store id.
2. A batched run resumes from `coach_checkpoints (job, period)`. Retrying the run continues
   from the last finished batch; a store already done finds its cards live and writes nothing.
3. To rerun one period from the start, delete its `coach_checkpoints` row first (owner role).

## Cards are all `generated_by = rule`

Expected when `ai_coach_enabled` is false, the AI cost cap or a store's call cap is reached
(dashboard 14, `tz_ai_*`), the provider is down, or the model's text failed the schema or
number-grounding check (`tz_ai_rejected_total{feature="coach_card"}`). The template text is
always correct; no action is needed unless the rejected rate climbs (then check the prompt
version against the recorded eval: `make check` runs it).

## Too many or too few cards

`coach_max_cards_per_day`, `coach_max_push_per_day` and `coach_dismiss_quiet_days` are in
`platform_settings` (01 §12). Per-store detector weights are in `coach_weights`.
