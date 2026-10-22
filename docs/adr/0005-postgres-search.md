# ADR 0005: Postgres for search in v1

- Status: accepted
- Date: 2026-10-22
- Plan: docs/plan/02-architecture.md

## Context

Customers search nearby stock by text and distance. At v1 scale (about 1,000 stores per city), a separate search engine adds operations cost and a second copy of the data.

## Decision

Use PostGIS `ST_DWithin` to find candidate stores, `pg_trgm` + `unaccent` + a weighted `tsvector` + a synonyms table for text, and rank in SQL with weights from settings (D-42, D-63).

## Consequences

One source of truth that is transactionally fresh. A dedicated engine (Meilisearch/OpenSearch) is a later ADR, triggered if search p95 goes above 200 ms at scale.
