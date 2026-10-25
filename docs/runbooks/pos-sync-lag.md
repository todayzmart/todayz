# POS sync lag

Alert `tz-pos-sync-lag` in `infra/grafana/alerts/pos-sync.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

At least 5 POS devices of live stores that synced earlier today have not synced for over an
hour, between 10:00 and 21:00 IST. The server cannot see a device's outbox, so this stands in for
"outbox older than 1 h" (TODO(lead): the device could report its oldest unsent entry).

## First five minutes

1. Open [06 · Business and POS](https://grafana.todayz.localhost/d/todayz-06): which stores, since when?
2. Check the sync route's errors on [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02). A 4xx jump after a deploy means a payload change: roll back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)).
3. One store: support asks the shop whether the device is online.

## Diagnosis

1. [06 · Business and POS](https://grafana.todayz.localhost/d/todayz-06): devices by last sync, sync errors, POS sales over time.
2. `sync_cursors` (read-only, as `todayz_owner` in a read-only transaction): `SELECT store_id, count(*) FROM sync_cursors WHERE last_synced_at < now() - interval '1 hour' AND last_synced_at > now() - interval '24 hours' GROUP BY 1`.
3. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): the sync route's 4xx/5xx (a validation change refusing device payloads).

### Likely causes

- The sync endpoint failing after a deploy.
- Devices offline (an area outage) - sales wait safely in the outbox.
- A device app version the server no longer accepts.

## Mitigation

- Fix the server side if it is failing; devices retry on their own. Never ask a shop to clear its device data.

## Escalation

**Level:** Warning: SEV3. SEV2 if one cause hits many stores in the evening peak ([Severity levels](../ops/severity.md)).

- **W2-04** (POS backend and sync) and **W2-11** (POS and offline).

## Follow-up

- A contract test for the payload the devices send.
- If a device stayed offline for long, check its sales arrived after it reconnected.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
