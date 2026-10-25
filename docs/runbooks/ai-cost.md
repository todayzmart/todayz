# AI spend at 80 % of the daily cap / AI daily cap reached

Alerts `tz-ai-cost-80` (severity **warning**) and `tz-ai-cost-cap-reached` (severity **critical**) in
`infra/grafana/alerts/ai.yaml` (11 §1 rule 7, D-70; owner W3-10).

## What it means

Every AI call goes through the cost guard (`backend/internal/ai/guard`). It adds each finished
call's list-price cost to today's platform row in `ai_usage_daily` (IST day) and refuses new calls
once that spend reaches `ai_daily_cost_cap_paise` (₹500 by default).

- **80 %**: calls still run. Without action they stop later today.
- **100 %**: every AI call gets 429 `ai_daily_cap_reached` (scope `platform`) until IST midnight. The
  screens show "AI help is paused until tomorrow." with the entry point disabled (11 §1 rule 7); the
  manual flows keep working and drafts already made stay.

A cap of 0 is the kill switch: it stops every call but fires neither alert, since it is a choice.

## First three checks

1. [14 · AI quality and cost](https://grafana.todayz.localhost/d/todayz-14): *Spend by feature* and
   *Calls by feature and outcome*. One feature far above its usual share is the usual cause.
2. The same dashboard, *Calls refused by cap*: are the per-customer and per-store caps
   (`ai_daily_calls_customer`, `ai_daily_calls_store`) doing their job, or is one subject retrying?
3. `ai_usage_daily` (read-only), today's top subjects:
   `SELECT scope, subject, calls FROM ai_usage_daily WHERE day = (now() AT TIME ZONE 'Asia/Kolkata')::date ORDER BY calls DESC LIMIT 10;`
   Subjects are store and user ids only; look them up in the admin console, never paste them into chat.

## Likely causes

- Real growth: more stores using a feature (look at the 7-day *Spend per day* trend).
- A loop: a client or a job retrying one feature (many calls, one subject, many `invalid` or `timeout`).
- A prompt or model change that made calls longer (tokens per call up on the same traffic).
- A wrong price: `backend/internal/ai/client/price.go` is list price at ₹88/$ and may be out of date.

## Fix

All through the admin console's settings (A-17, step-up; the cap is a money setting, so super_admin only).
Every change is audited (A-19):

- Growth, and the spend is worth it: raise `ai_daily_cost_cap_paise`. Calls resume at once.
- One feature misbehaving: turn its flag off (`ai_<feature>_enabled`, 01 §17). Its entry points hide; the other features keep running.
- Abuse by one store or customer: lower `ai_daily_calls_store` or `ai_daily_calls_customer`, and
  raise it with risk if it looks deliberate.
- Everything has to stop now: set `ai_daily_cost_cap_paise` to 0. Set it back when fixed.

The cap resets at IST midnight on its own; nothing needs clearing.

## Who to tell

- The founder for any cap change (it is money, D-70).
- The owner of the feature that spent it (W3-04, W3-11…W3-13).
