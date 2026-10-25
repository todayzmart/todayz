# Idempotency completion failed

Alert `tz-idempotency-complete-failed` in `infra/grafana/alerts/api.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

A request with an `Idempotency-Key` was answered, but storing its result failed
(`tz_idempotency_complete_failures`, platform/idempotency, W1-07). The client got its response;
a retry with the same key gets 409 `conflict` until the claim's 3-minute lease runs out, then the
request runs again. For an order or a POS sale that could mean a duplicate attempt, which the
domain's own uniqueness rules must catch.

## First five minutes

1. Loki: `{service_name="todayz-api"} |= "idempotency"` for the error.
2. Check [03 · Database](https://grafana.todayz.localhost/d/todayz-03): is the database the cause? Then follow [Database connections](database-connections.md).
3. Note the routes. For orders or POS sales, look for duplicates by the domain's unique keys (read-only).

## Diagnosis

1. Logs: `{service_name="todayz-api"} |= "idempotency"` around the alert; the error says why (database, encryption).
2. [03 · Database](https://grafana.todayz.localhost/d/todayz-03): errors or pool wait at the same time.
3. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): which routes (POST with `Idempotency-Key`) were busy.

### Likely causes

- The database refused the write (pool exhausted, a statement timeout, a failover).
- `PII_ENC_KEY` missing or wrong length on a new instance (the body is sealed before it is stored): `make env-check` names only.

## Mitigation

- Fix the database or key cause; nothing to replay by hand. Check the affected routes' domain data for duplicates if the count is more than a few.

## Escalation

**Level:** Warning: SEV3. SEV2 if it keeps firing on order or POS routes, because retries can run twice ([Severity levels](../ops/severity.md)).

- **W1-07** (owner of platform/idempotency) and the lead.

## Follow-up

- If duplicates got through, a uniqueness rule or test in that domain.
- If the cause was the database, link the database incident.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
