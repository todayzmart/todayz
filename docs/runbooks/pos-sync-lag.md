# POS sync lag

Alert `tz-pos-sync-lag` in `infra/grafana/alerts/pos-sync.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

At least 5 POS devices of live stores that synced earlier today have not synced for over an
hour, between 10:00 and 21:00 IST. The server cannot see a device's outbox, so this stands in for
"outbox older than 1 h" (TODO(lead): the device could report its oldest unsent entry).

## First three checks

1. [06 · Business and POS](https://grafana.todayz.localhost/d/todayz-06): devices by last sync, sync errors, POS sales over time.
2. `sync_cursors` (read-only, as `todayz_owner` in a read-only transaction): `SELECT store_id, count(*) FROM sync_cursors WHERE last_synced_at < now() - interval '1 hour' AND last_synced_at > now() - interval '24 hours' GROUP BY 1`.
3. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): the sync route's 4xx/5xx (a validation change refusing device payloads).

## Likely causes

- The sync endpoint failing after a deploy.
- Devices offline (an area outage) - sales wait safely in the outbox.
- A device app version the server no longer accepts.

## Fix

- Fix the server side if it is failing; devices retry on their own. Never ask a shop to clear its device data.

## Who to tell

- **W2-04** (POS backend and sync) and **W2-11** (POS and offline).
