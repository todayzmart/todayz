# Store not accepting

Alert `tz-store-not-accepting` in `infra/grafana/alerts/marketplace.yaml` (05 §5, owner W3-07). Severity: **ops (support queue, not paging)**.

## What it means

A store let 3 or more orders expire unaccepted (`status = 'expired'`, `status_reason = 'not_accepted'`)
among orders placed in the last 2 hours. The shop is probably closed with the store still live, or
the device is not getting order alerts.

## First three checks

1. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): expired-unaccepted orders by store.
2. [10 · Retailer health](https://grafana.todayz.localhost/d/todayz-10): the store's accept rate and last activity; is its POS device syncing?
3. [04 · Jobs and realtime](https://grafana.todayz.localhost/d/todayz-04): business SSE connections; if many stores are affected, see [Order alerts slow](realtime-order-alerts-slow.md).

## Likely causes

- The shop is closed or busy and did not pause the store.
- The device is offline, muted, or signed out.

## Fix

- Support calls the shop (the store screen in the admin console has the contact) and asks them to pause or reopen.
- No automatic pause (D-67); the store's strikes and reliability score update on their own.

## Who to tell

- Support (ops). The lead only if many stores are affected at once.
