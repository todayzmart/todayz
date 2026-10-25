# Unconfirmed returns

Alert `tz-risk-unconfirmed-returns` in `infra/grafana/alerts/risk.yaml` (05 §5, owner W3-07). Severity: **info (ops)**.

## What it means

A delivery-failed order's return was not confirmed by the shop within 72 h, so it was
auto-confirmed with `return_outcome = 'unconfirmed'` and a `return_unconfirmed` risk flag (D-69).

## First three checks

1. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): returns by outcome.
2. The admin risk queue: the flag and the order.
3. The store's other returns: a pattern or one-off?

## Likely causes

- The shop did not check the returned goods in the app.
- Goods were not actually returned (a delivery partner problem).

## Fix

- Support asks the shop to confirm; ops reviews the flag.

## Who to tell

- Ops / support.
