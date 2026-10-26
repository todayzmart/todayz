# Incident template

Copy this to `docs/ops/incidents/<yyyy-mm-dd>-<short-name>.md` when you open a SEV1 or SEV2. Fill it
as you go, not afterwards. Write times in IST. Never paste a secret, a phone number, an OTP, an order
code, a token or a customer's name. Use ids (order id, store id, run id, trace id).

```markdown
# <yyyy-mm-dd> <short name>

- **Level:** SEV1 | SEV2 (see docs/ops/severity.md)
- **Status:** investigating | mitigated | resolved
- **Started:** <time the impact started, IST>
- **Detected:** <time and how: alert uid, a shop called, a founder noticed>
- **Mitigated:** <time>
- **Resolved:** <time>
- **Incident lead:** <one person; they decide and write>
- **Alert / runbook:** `<alert uid>` → docs/runbooks/<file>.md

## Impact

Who was hurt and how much. Customers, shops, admins. Which city. Orders failed or late (count).
Money affected (₹, which ledger accounts). Was any data lost or exposed?

## Timeline

| Time (IST) | What happened or what we did |
|---|---|
| hh:mm | Alert fired |
| hh:mm | Acknowledged by … |
| hh:mm | … |

## Cause

What broke, and why it broke then. Link the commit, the deploy, the setting change or the provider status page.

## Mitigation

What stopped the impact (rollback, feature flag off, restart, provider failover).

## Data and money check

- Ledger reconcile clean after the fix? (`ledger_reconcile` run id)
- Orders stuck in a wrong state? (count, and how they were moved)
- Anyone to refund or credit? Who approved it (two people, A-19 audit ids)?

## Follow-up

| Action | Owner | Due | Done |
|---|---|---|---|
| A test or check that would have caught it | | | |
| Alert or runbook change | | | |

## What went well, what did not

Short. No blame; describe the system, not the person.
```
