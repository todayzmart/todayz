# Store not accepting

Alert `tz-store-not-accepting` in `infra/grafana/alerts/marketplace.yaml` (05 §5, owner W3-07). Severity: **ops (support queue, not paging)**.

## What it means

A store let 3 or more orders expire unaccepted (`status = 'expired'`, `status_reason = 'not_accepted'`)
among orders placed in the last 2 hours. The shop is probably closed with the store still live, or
the device is not getting order alerts.

## First five minutes

1. Open [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): one store or many?
2. Many stores: check [Order alerts slow](realtime-order-alerts-slow.md) and [SSE down](realtime-sse-down.md).
3. One store: support calls the shop from the store screen in the admin console.

## Diagnosis

1. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): expired-unaccepted orders by store.
2. [10 · Retailer health](https://grafana.todayz.localhost/d/todayz-10): the store's accept rate and last activity; is its POS device syncing?
3. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): business SSE connections; if many stores are affected, see [Order alerts slow](realtime-order-alerts-slow.md).

### Likely causes

- The shop is closed or busy and did not pause the store.
- The device is offline, muted, or signed out.

## Mitigation

- Support calls the shop (the store screen in the admin console has the contact) and asks them to pause or reopen.
- No automatic pause (D-67); the store's strikes and reliability score update on their own.

## Escalation

**Level:** Ops: SEV4, a support queue item. SEV2 if many stores fire at once (then it is a platform problem) ([Severity levels](../ops/severity.md)).

- Support (ops). The lead only if many stores are affected at once.

## Follow-up

- If the shop was closed with the store live, remind them of the hours setting.
- If the device missed alerts, record the device and app version for the business app owner.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
