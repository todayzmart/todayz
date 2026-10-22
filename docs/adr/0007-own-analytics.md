# ADR 0007: Our own product analytics in Postgres

- Status: accepted
- Date: 2026-10-22
- Plan: docs/plan/02-architecture.md

## Context

Search and demand data is TodayZ's core asset (to be sold later to brands, aggregated). It must stay in our database and be joined with orders, stores and geography.

## Decision

Write product events to a daily-partitioned `events` table, using a catalogue in `shared/telemetry/events/<domain>.yaml`. Rollups run every 5 minutes (today) and nightly (history). Locations are stored as H3 resolution-8 cells, never raw points (D-59). Infra telemetry goes to OpenTelemetry/Grafana.

## Consequences

Full control and no SaaS data sharing, with DPDP-friendly retention. If volume passes about 5 M events per day, ClickHouse is a later ADR.
