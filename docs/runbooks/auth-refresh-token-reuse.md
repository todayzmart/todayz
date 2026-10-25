# Refresh token reuse

Alert `tz-auth-refresh-reuse` in `infra/grafana/alerts/auth.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

More than 5 session families were revoked in 10 minutes because a refresh token that had
already been rotated was presented again (`sessions.revoked_reason = 'reuse'`, AU-07). Each one
signs that user out everywhere. A few a day are normal (a client retry race); a burst is theft or a
client bug.

## First three checks

1. [12 · Security and abuse](https://grafana.todayz.localhost/d/todayz-12): reuse revocations over time, by app.
2. `sessions` (read-only, as `todayz_owner` in a read-only transaction): `SELECT app, count(*) FROM sessions WHERE revoked_reason = 'reuse' AND revoked_at > now() - interval '1 hour' GROUP BY 1`.
3. Did a client release go out (app_version in RUM, 05)? A new refresh race shows up right after one.

## Likely causes

- A client bug refreshing twice in parallel (two tabs, a retry without the new token).
- Stolen refresh tokens replayed.

## Fix

- Client bug: the app owner fixes the refresh lock; users only need to sign in again.
- Theft: tell the lead; look at the user agents and address hashes of the families.

## Who to tell

- **W1-01** and the owner of the affected app.
