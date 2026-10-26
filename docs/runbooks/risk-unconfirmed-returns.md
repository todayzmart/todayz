# Unconfirmed returns

Alert `tz-risk-unconfirmed-returns` in `infra/grafana/alerts/risk.yaml` (05 §5, owner W3-07). Severity: **info (ops)**.

## What it means

A delivery-failed order's return was not confirmed by the shop within 72 h, so it was
auto-confirmed with `return_outcome = 'unconfirmed'` and a `return_unconfirmed` risk flag (D-69).

## First five minutes

1. Open the flag and the order in the admin risk queue.
2. Check the store's other returns: a pattern or one-off?
3. No page; work it in the ops queue.

## Diagnosis

1. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): returns by outcome.
2. The admin risk queue: the flag and the order.
3. The store's other returns: a pattern or one-off?

### Likely causes

- The shop did not check the returned goods in the app.
- Goods were not actually returned (a delivery partner problem).

## Mitigation

- Support asks the shop to confirm; ops reviews the flag.

## Escalation

**Level:** Info: SEV4, a risk queue item ([Severity levels](../ops/severity.md)).

- Ops / support.

## Follow-up

- Record the outcome on the flag.
- If a store repeats it, talk to the shop.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
