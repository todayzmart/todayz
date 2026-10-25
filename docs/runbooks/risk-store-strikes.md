# Store strikes

Alert `tz-risk-store-strikes` in `infra/grafana/alerts/risk.yaml` (05 §5, owner W3-07). Severity: **ops (risk queue)**.

## What it means

A store reached `store_strike_risk_threshold` unreset strikes in 30 days and the `store_strikes`
risk flag was raised (by `store_reliability` or an admin cancel with `strike: store`). Nothing is
paused automatically (D-67).

## First five minutes

1. Open the flag in the admin risk queue.
2. Check [10 · Retailer health](https://grafana.todayz.localhost/d/todayz-10): is it this store only?
3. Nothing is paused automatically (D-67). The risk admin decides.

## Diagnosis

1. [10 · Retailer health](https://grafana.todayz.localhost/d/todayz-10): the store's strikes, accept rate and expiries.
2. The admin risk queue: the flag and its details (strike count, window).
3. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): is it this store only, or many (then it is a platform problem)?

### Likely causes

- The shop often fails to accept or fulfil.
- An order alert or device problem making a good shop look bad.

## Mitigation

- Review in the risk queue: contact the shop, then clear or confirm the flag. A pause is a manual admin decision.

## Escalation

**Level:** Ops: SEV4, a risk queue item ([Severity levels](../ops/severity.md)).

- Ops / risk reviewer.

## Follow-up

- Record the decision on the flag.
- If many stores fire, look for a platform cause (alerts, timers).
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
