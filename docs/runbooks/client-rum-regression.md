# RUM regression

Alert `tz-client-rum-regression` in `infra/grafana/alerts/client.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

p75 LCP on the customer app's `/` or `/search` route has been above 3 s for an hour (budget
2.5 s).

## First five minutes

1. Open [05 · Customer RUM](https://grafana.todayz.localhost/d/todayz-05): which route, version and connection type?
2. Did it start with a release? Compare LCP by version.
3. Check the API calls the page makes first on [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02).

## Diagnosis

1. [05 · Customer RUM](https://grafana.todayz.localhost/d/todayz-05): LCP p75 by route, version, connection type.
2. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): API latency for the routes those pages call first.
3. Lighthouse on the release (make e2e, locally).

### Likely causes

- A heavier bundle or a new image in a release.
- Slow API responses on first load.
- A shift in users towards slower connections (check by `conn`).

## Mitigation

- Fix in the app; roll back if a release caused it and it is far over budget.

## Escalation

**Level:** Warning: SEV3 ([Severity levels](../ops/severity.md)).

- The customer app owner.

## Follow-up

- A Lighthouse budget in `make e2e` for the route, if it was missing.
- File the fix against the release that caused it.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
