# Admin adjustments in a day

Alert `tz-risk-admin-adjust-day` in `infra/grafana/alerts/risk.yaml` (D-246 B9). Severity: **ops (risk queue)**.

## What it means

One admin's wallet adjustments in the current IST day, summed over all customers, passed
`admin_adjust_daily_total_alert_above_paise` (₹5,000 by default). They are counted the way the
two-person rule counts them for one customer (D-237): posted at-once adjustments plus that admin's
pending and approved requests, by absolute amount, so credits and debits both count.

The two-person rule stops one large adjustment, or many to one customer. It does not stop many
small ones to many customers; this alert is the check on that. Nothing is blocked.

## First five minutes

1. Note the admin id from the alert. In the admin console, open the audit log filtered to that
   admin and the actions `credits.adjust` and `credits.adjustment_request` for today.
2. Check [11 · Money and payouts](https://grafana.todayz.localhost/d/todayz-11): the two-person
   approvals panel and today's adjustments.
3. Ask the admin's lead whether there is a known reason (a goodwill batch after an outage, a
   campaign clean-up).

## Diagnosis

- **A known batch:** the reasons and links on the adjustments match one incident. Record it.
- **Unknown:** many customers, round amounts, reasons that repeat or say little, customers that are
  new or linked to each other. Treat as possible misuse.

## Mitigation

- Known batch: nothing to do; note it on the incident or the ops log.
- Unknown: a `super_admin` removes the admin's `finance` role (D-64) while it is checked, and
  reverses the adjustments that have no reason, each with its own reason. Pending requests can be
  rejected in the approvals queue.

## Escalation

**Level:** Ops: SEV3 if unexplained, otherwise SEV4 ([Severity levels](../ops/severity.md)).

- Ops lead, then the founder for possible misuse.

## Follow-up

- If real batches keep firing it, a `super_admin` raises
  `admin_adjust_daily_total_alert_above_paise` in the admin settings (a money setting, §17)
  rather than everyone ignoring the alert.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
