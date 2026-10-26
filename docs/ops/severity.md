# Severity levels

Every incident gets one level. Pick it in the first five minutes. Raise it when you learn more;
lower it only when the impact is gone, not when you think you know the cause.

| Level | Meaning | Examples | Answer within | Who works it |
|---|---|---|---|---|
| **SEV1** | The core loop is down for many people, or money or data is wrong. | Customers cannot sign in, search or place orders. Order timers stopped. Ledger drift. Data loss. A key leaked. | 15 min, day or night | On-call and the lead, until it is mitigated. The founder is told at once. |
| **SEV2** | The core loop works but is badly degraded, or one city or app is down. | 5xx on one key route. Order alerts to shops late. SMS failing in part. One app's release broken. | 30 min, 08:00–23:00 IST; next morning otherwise | On-call; the lead if it lasts over an hour. |
| **SEV3** | A non-core feature is degraded, or a warning that needs work this week. | AI features off at the cap. Slow but working pages. A job retrying. A missing future partition. | Next working day | On-call files it; the owner fixes it. |
| **SEV4 (ops)** | A queue item, not an incident. | A store not accepting. Store strikes. Zero-result spike. An unconfirmed return. | Ops queue SLA | Support or ops, no page. |

## Alert severity to incident level

Alerts carry `severity` labels (`infra/grafana/alerts/*.yaml`, 05 §5). Start from this mapping:

| Alert severity | Start at |
|---|---|
| `critical` | SEV1 if customers or shops are blocked or money is wrong; else SEV2 |
| `warning` | SEV3; SEV2 if it is customer-facing and still rising after 30 minutes |
| `ops`, `info` | SEV4 |
| CloudWatch alarm (SNS email) | As the runbook says |

## Rules

- SEV1 and SEV2 open an incident note from [the template](incident-template.md) at once.
- SEV1 and SEV2 get a written review within two working days.
- Never silence a `critical` alert without a written reason in the incident note.
- Money first: if a ledger, payout or credit number may be wrong, it is SEV1 until shown otherwise.
- A leaked secret is SEV1. Follow [Secret rotation](secret-rotation.md).
