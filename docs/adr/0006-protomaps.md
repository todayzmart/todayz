# ADR 0006: Protomaps PMTiles for maps

- Status: accepted
- Date: 2026-10-22
- Plan: docs/plan/02-architecture.md

## Context

The customer home shows a map with store counts. Per-request map APIs cost money at scale and leak user locations to a third party.

## Decision

Serve Protomaps PMTiles (OpenStreetMap data, ODbL attribution) from R2 at `tiles.todayz.in` (locally MinIO via Caddy), render them with MapLibre GL, and style them with the design system's `map-*` tokens.

## Consequences

No per-request cost or third-party tracking. City extracts are rebuilt by hand when needed. Geocoding uses open PIN-code and locality data in `places`.
