# On-call basics

TodayZ is small. On-call is one founder at a time, with the lead as second. This page says what the
person on call does, and what they never do.

## Where alerts arrive

| Source | Where it goes (Wave 5 routing, 05 §5) |
|---|---|
| Grafana `critical` | Phone push or SMS, and email |
| Grafana `warning` | Email (or the ops WhatsApp group webhook, if chosen) |
| Grafana `ops` / `info` | The admin console queues (support, risk); no page |
| CloudWatch alarms | SNS topic `todayz-prod-alarms` → email to `alarm_email` |
| AWS Budgets | Email at 80 % actual and 100 % forecast of ₹30k a month |

Locally, alerts only show in Grafana (https://grafana.todayz.localhost) and the console. Nothing pages.

## The shift

- A week at a time, handed over on Monday at 10:00 IST with a short note: open incidents, things to watch, settings changed.
- Carry a phone with signal and a laptop that can reach Grafana, the admin console and the AWS console.
- If you cannot answer for an hour, hand over first. A silent on-call is worse than none.

## When an alert fires

1. **Acknowledge** within the time for its level ([Severity levels](severity.md)). Say "I have it" in the ops channel.
2. **Open the runbook.** The alert's `runbook_url` points at it; the index is [docs/runbooks/README.md](../runbooks/README.md).
3. **Do the runbook's "First five minutes".** Decide the level.
4. **SEV1 or SEV2:** open an incident note from [the template](incident-template.md) and tell the lead. SEV1: tell the founder too.
5. **Mitigate before you debug.** Rollback, feature flag off, pause a non-timer job kind. [Release checklist → Rollback](release-checklist.md#rollback).
6. **Close** when the alert clears and the impact is gone. Fill the incident note.

## Tools you may use

- Grafana dashboards 00–15, Explore (Loki logs, Tempo traces).
- Admin console: A-21 Jobs (pause, retry, run now, with a reason), A-22 System, A-17 Settings (audited, step-up), support and risk queues.
- AWS console, read-only by default: Beanstalk health, RDS, CloudWatch. Changes go through the release path or a runbook step.
- A read-only SQL session as `todayz_owner` inside `BEGIN READ ONLY`. Never select `phone_enc` or any `*_enc` column.

## Never

- Edit ledger, order, `job_runs` or `river_job` rows by hand. Money fixes are two-person `admin_adjustment` postings.
- Run, pause or retry a `timer` job kind by hand (04 §2.7). Fix the cause; the sweeps catch up.
- Run a down migration in prod, or `terraform apply` outside the lead's release path.
- Print, paste or screenshot a secret, an OTP, an order code, a token or a phone number.
- Silence a `critical` alert without a reason written in the incident note.
- Raise an abuse limit (OTP, AI calls) to make an alert go away.

## Who to call

| Need | Who |
|---|---|
| Engineering decision, rollback, deploy | The lead |
| Money, payouts, refunds, a cap change | The founder (two people for any money correction) |
| A shop | Support, from the store screen in the admin console |
| AWS or Cloudflare account access | The founder (account owner) |
| SMS / DLT | The founder (provider and DLT portal) |
