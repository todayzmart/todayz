# Zero-result spike

Alert `tz-search-zero-result-spike` in `infra/grafana/alerts/search.yaml` (05 §5, owner W3-07). Severity: **info (ops)**.

## What it means

More than 40 % of the last hour's searches (at least 50) found nothing within the radius, for
an hour. Searches carry an H3 cell, not a city, so this is platform-wide (TODO(lead): per city).

## First three checks

1. [07 · Customer funnel](https://grafana.todayz.localhost/d/todayz-07): top zero-result queries (k ≥ 10).
2. [08 · Marketplace health](https://grafana.todayz.localhost/d/todayz-08): under-supplied cells.
3. [02 · API endpoints](https://grafana.todayz.localhost/d/todayz-02): search latency and errors (an error that returns an empty list looks like a zero result).

## Likely causes

- A store with many items went offline or paused.
- Demand from a new area with no stores.
- A search bug (tokenizer, radius setting changed).

## Fix

- Supply gap: tell the growth team the queries and cells. Bug: the search owner fixes it.

## Who to tell

- Growth / ops; the search owner (W1-06) if it looks like a bug.
