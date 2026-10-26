# Zero-result spike

Alert `tz-search-zero-result-spike` in `infra/grafana/alerts/search.yaml` (05 §5, owner W3-07). Severity: **info (ops)**.

## What it means

More than 40 % of the last hour's searches (at least 50) found nothing within the radius, for
an hour. Searches carry an H3 cell, not a city, so this is platform-wide (TODO(lead): per city).

## First five minutes

1. Open [07 · Customer funnel](https://grafana.todayz.localhost/d/todayz-07): top zero-result queries.
2. Check [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): are search errors rising? Then it is an incident, not supply.
3. Otherwise it is supply: pass the queries and cells to the field team.

## Diagnosis

1. [07 · Customer funnel](https://grafana.todayz.localhost/d/todayz-07): top zero-result queries (k ≥ 10).
2. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): under-supplied cells.
3. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): search latency and errors (an error that returns an empty list looks like a zero result).

### Likely causes

- A store with many items went offline or paused.
- Demand from a new area with no stores.
- A search bug (tokenizer, radius setting changed).

## Mitigation

- Supply gap: tell the growth team the queries and cells. Bug: the search owner fixes it.

## Escalation

**Level:** Info: SEV4. SEV2 if search errors are hiding behind empty lists ([Severity levels](../ops/severity.md)).

- Growth / ops; the search owner (W1-06) if it looks like a bug.

## Follow-up

- Synonyms or catalogue fixes for the top queries.
- Field onboarding in the under-supplied cells.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
