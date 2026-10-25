# Idempotency completion failed

Alert `tz-idempotency-complete-failed` in `infra/grafana/alerts/api.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

A request with an `Idempotency-Key` was answered, but storing its result failed
(`tz_idempotency_complete_failures`, platform/idempotency, W1-07). The client got its response;
a retry with the same key gets 409 `conflict` until the claim's 3-minute lease runs out, then the
request runs again. For an order or a POS sale that could mean a duplicate attempt, which the
domain's own uniqueness rules must catch.

## First three checks

1. Logs: `{service_name="todayz-api"} |= "idempotency"` around the alert; the error says why (database, encryption).
2. [03 · Database](https://grafana.todayz.localhost/d/todayz-03): errors or pool wait at the same time.
3. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): which routes (POST with `Idempotency-Key`) were busy.

## Likely causes

- The database refused the write (pool exhausted, a statement timeout, a failover).
- `PII_ENC_KEY` missing or wrong length on a new instance (the body is sealed before it is stored): `make env-check` names only.

## Fix

- Fix the database or key cause; nothing to replay by hand. Check the affected routes' domain data for duplicates if the count is more than a few.

## Who to tell

- **W1-07** (owner of platform/idempotency) and the lead.
