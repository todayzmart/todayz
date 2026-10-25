# Ledger invariant

Alert `tz-ledger-invariant` in `infra/grafana/alerts/ledger.yaml` (05 §5, owner W3-07). Severity: **critical**.

## What it means

`ledger_reconcile` found ledger rows that break an invariant: `tz_ledger_reconcile_drift{reason}`
is above 0, or the worker logged `event=ledger_invariant`. Customer credit balances may be wrong.
The reasons and their meaning are in [Credits ledger](credits-ledger.md#the-reconcile-reasons).

## First five minutes

1. Open an [incident note](../ops/incident-template.md). Tell the lead and the founder.
2. Open [11 · Money and payouts](https://grafana.todayz.localhost/d/todayz-11): which reason, how many rows?
3. Stop new damage: if one feature causes it, turn it off (`feature_*_enabled`). Never edit ledger rows.

## Diagnosis

1. [11 · Money and payouts](https://grafana.todayz.localhost/d/todayz-11): ledger panels and the drift by reason.
2. Loki: `{service_name=~"todayz-.*"} | json | event="ledger_invariant"` (counts only, never a customer).
3. The ledger (read-only, as `todayz_owner` in a read-only transaction): see the checks in [Credits ledger](credits-ledger.md#first-three-checks).

### Likely causes

- A bug in a posting path, or a write that went around the credits service.
- A migration that touched balances.

## Mitigation

- Never edit ledger rows by hand. Corrections are `admin_adjustment` postings by two people (W3-01).
- Re-run `ledger_reconcile` after the fix to bring the gauge back to 0.

## Escalation

**Level:** Critical: SEV1 until shown otherwise. Money may be wrong ([Severity levels](../ops/severity.md)).

- The **lead** and **W2-01** at once; the founder if money left the platform.

## Follow-up

- A test that writes the bad posting and checks it is refused.
- Two-person `admin_adjustment` postings for every affected customer, with audit ids in the incident note.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
