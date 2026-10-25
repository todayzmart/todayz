# Asks timers

Kinds `asks_close_window` (class `timer`), `asks_retention` (class `maintenance`),
`rollup_ask_demand` (class `rollup`) and `site_marquee_build` (class `rollup`), owner W3-16, all declared in
`backend/internal/jobs/asks.go`. Code: `backend/internal/asks/maintenance.go` and
`backend/internal/share/marquee.go`. The general alert is [Job failing](jobs-job-failing.md)
(`tz-jobs-failing-event`, critical for timers).

## What they do

| Kind | Runs at | Outcome |
|---|---|---|
| `asks_close_window` | every minute (one run per minute) | Closes each open ask that every recipient has answered (`all_replied`) or whose last reply window (`ask_reply_window_min`) is over (`window_ended`). The customer's C-26 sees the close on `/me/stream` (`ask.updated`; each recipient shop gets `ask.closed`) or on its next poll. At most 500 asks per run; the next minute takes the rest. Summary: `closed`. |
| `asks_retention` | daily at 03:20 IST | Deletes asks older than `ask_retention_days` with their recipients, replies and the store blocks that came from them (01 §11.2). Orders booked from a reply keep their lines; the reply link clears. Batches of 500 until none are left. Summary: `deleted`. |
| `rollup_ask_demand` | every 15 minutes | Recomputes yesterday's and today's (IST) unanswered ask demand per receiving store and per H3 cell (`rollup_daily_ask_demand`), and adds the centre of any new cell to `insight_geo_cells`. A day is deleted and rewritten in one transaction, so a rerun gives the same rows. Summary: `rows`. |
| `site_marquee_build` | daily at 04:10 IST | Builds the proposed "people near you searched for" strip per city from the last 7 days of `search_log`, with the category denylist and the k-floor applied. A strip is never shown until an admin approves it on A-23; an already decided strip for the day is left alone. Summary: `cities`. |

The job arguments carry no customer id, phone, address or query text.

All four are idempotent:

- `asks_close_window` closes an ask under its row lock; an ask another run (or the customer)
  already closed is skipped and not counted.
- A reply arriving in the same instant as the close either lands first (then the ask may close as
  `all_replied`) or finds the ask closed and gets `invalid_transition`.
- The rollup and the marquee build recompute from source rows; reruns overwrite with the same
  result.
- Retention deletes by age only; a second run finds nothing.

## First three checks

1. Grafana, Jobs dashboard: is the kind failing, or just slow (`tz_job_duration_seconds{kind}`)?
2. Worker logs for the kind (`kind=asks_close_window`): the error line names the ask id, never a
   customer.
3. `select count(*) from asks a where a.status = 'open' and not exists (select 1 from ask_recipients r where r.ask_id = a.id and r.answer is null and r.reply_by > now() - interval '5 minutes')`
   (read-only, as `todayz_app`): a growing number means closes are not landing.

## Likely causes

- Database unavailable or the pool exhausted (every kind fails at once).
- A settings row missing or out of range (`ask_retention_days`, `ask_reply_window_min`): the run
  fails with the key name.
- A lock held by a long transaction on `asks` (close runs time out at 50 s).

## Fix

- Restore the database or the setting; the next tick catches up by itself. Nothing needs replaying:
  the close sweep finds every overdue ask, and the rollup recomputes two days each run.
- A demand day older than yesterday that needs rebuilding: run the rollup for that day from a Go
  test harness or ask the lead; there is no admin endpoint.
- Never close asks by hand in SQL: the customer and the shops would get no stream event.

## Who to tell

The W3-16 owner, then the lead. A customer-visible delay of more than 5 minutes on C-26 goes to
support (they see the ask as still waiting).
