# Runbooks

One runbook per alert. Each alert's `runbook_url` (Grafana) or description (CloudWatch) points at
its file here. Alert runbooks have the same six sections: **What it means**, **First five
minutes**, **Diagnosis**, **Mitigation**, **Escalation**, **Follow-up**.

`make check` runs `check-runbooks` (`mk/60-ops.mk`, `scripts/ops/runbooks_check.py`). It fails when
an alert, a paging CloudWatch alarm or a job kind has no runbook, a runbook lacks a section or does
not name its alert, or a dashboard uid or relative link does not resolve. A new alert needs its
runbook and a line in this index in the same change.

How to work an alert: [On-call basics](../ops/on-call.md). Levels: [Severity levels](../ops/severity.md).

## Grafana alerts (`infra/grafana/alerts/`)

| Runbook | Alerts | Severity |
|---|---|---|
| [API availability burn](api-availability-burn.md) | `tz-api-availability-fast-burn`, `tz-api-availability-slow-burn` | critical, warning |
| [API latency](api-latency.md) | `tz-api-latency` (and CloudWatch `alb-latency-p95-high`) | warning |
| [Idempotency completion failed](api-idempotency-complete-failed.md) | `tz-idempotency-complete-failed` | warning |
| [OTP abuse](auth-otp-abuse.md) | `tz-auth-otp-spike`, `tz-auth-otp-one-address` | warning |
| [Refresh token reuse](auth-refresh-token-reuse.md) | `tz-auth-refresh-reuse` | warning |
| [SMS failures](auth-sms-failures.md) | `tz-auth-sms-failures` | critical |
| [Client errors](client-errors.md) | `tz-client-errors`, `tz-client-release-errors` | warning |
| [RUM regression](client-rum-regression.md) | `tz-client-rum-regression` | warning |
| [Database connections](database-connections.md) | `tz-db-pool-saturated`, `tz-db-server-connections` | warning |
| [Missing partition](database-missing-partition.md) | `tz-db-missing-partition` | warning |
| [Job failing](jobs-job-failing.md) | `tz-jobs-failing-scheduled`, `tz-jobs-failing-event` | critical for timer and money kinds, else warning |
| [Cron missed](jobs-cron-missed.md) | `tz-jobs-cron-missed` | critical for money kinds, else warning |
| [Job hard-cancelled at shutdown](jobs-hard-cancelled.md) | `tz-jobs-hard-cancelled` | warning |
| [Job backlog](queue-job-backlog.md) | `tz-job-backlog` | warning |
| [Order timers late](queue-order-timers-late.md) | `tz-order-timers-late` | critical |
| [Orders timers](orders-timers.md) | `tz-order-reconcile-failed` | critical |
| [POS bill claims](pos-bill-claims.md) | `tz-pos-claim-release-failed` | critical |
| [POS sync lag](pos-sync-lag.md) | `tz-pos-sync-lag` | warning |
| [Ledger invariant](ledger-invariant.md) | `tz-ledger-invariant` | critical |
| [Order alerts slow](realtime-order-alerts-slow.md) | `tz-order-alerts-slow` | critical |
| [SSE down](realtime-sse-down.md) | `tz-sse-down` | warning |
| [Store not accepting](marketplace-store-not-accepting.md) | `tz-store-not-accepting` | ops |
| [Store strikes](risk-store-strikes.md) | `tz-risk-store-strikes` | ops |
| [Unconfirmed returns](risk-unconfirmed-returns.md) | `tz-risk-unconfirmed-returns` | info |
| [Admin adjustments in a day](risk-admin-adjust-day.md) | `tz-risk-admin-adjust-day` | ops |
| [Zero-result spike](search-zero-result-spike.md) | `tz-search-zero-result-spike` | info |
| [AI spend](ai-cost.md) | `tz-ai-cost-80`, `tz-ai-cost-cap-reached` | warning, critical |

## CloudWatch alarms (`infra/terraform/modules/stack/`, cloud only)

They email the SNS topic `todayz-prod-alarms`. Names are `todayz-prod-<name>`.

| Runbook | Alarms |
|---|---|
| [RDS CPU](rds-cpu.md) | `rds-cpu-high`, `rds-cpu-credits-low` |
| [RDS storage](rds-storage.md) | `rds-free-storage-low` |
| [API 5xx at the load balancer](api-5xx.md) | `alb-target-5xx` |
| [API unhealthy](api-unhealthy.md) | `alb-unhealthy-hosts`, `api-environment-health` |
| [API latency](api-latency.md) | `alb-latency-p95-high` |
| [Worker unhealthy](worker-unhealthy.md) | `worker-environment-health` |
| [Worker queue age (cloud)](job-queue-backlog.md) | `worker-oldest-job-age-critical`, `worker-oldest-job-age-high` |

`worker-oldest-job-age-low` only scales the worker in; it pages nobody and has no runbook.

## Job kinds and domains

A job kind's runbook is declared with the kind (`Runbook:` in `backend/internal/jobs/`); A-21 links it.

| Runbook | What |
|---|---|
| [Orders timers](orders-timers.md) | Order accept, no-show and partial timers, the reconcile sweep, strikes, store reliability |
| [Asks timers](asks-timers.md) | Ask-shops-nearby window close and retention, demand rollup, site marquee, share cards |
| [POS bill claims](pos-bill-claims.md) | Bill QR claim expiry and credit hold release |
| [Credits ledger](credits-ledger.md) | Credits expiry and reminders, `ledger_reconcile` and its reasons, referral rewards |
| [Payouts](payouts.md) | Monthly statement close and the admin payout and adjustment actions |
| [Shop coach jobs (W3-11)](coach.md) | The shop coach's daily, weekly and 15-minute rule jobs |

## Cloud setup (founder and lead)

| Runbook | What |
|---|---|
| [AWS: everything the founder does, in order](aws-founder-steps.md) | Account unblock, region, SMS and DLT |
| [Cloud access: what the founder sets up](cloud-access.md) | Who holds which cloud credentials |

## Ops pages

[docs/ops/](../ops/README.md): severity levels, on-call, incident template, release checklist,
secret rotation, backup and restore.
