# ADR 0003: River for jobs and timers

- Status: accepted
- Date: 2026-10-22
- Plan: docs/plan/02-architecture.md

## Context

Orders have hard timers: accept within 10 minutes and hold for 30. Each timer must commit atomically with the order row, and v1 has no Redis.

## Decision

Use River (Postgres-backed queue). Enqueue jobs in the same transaction as the state change. Schedule them at absolute times. Every handler re-reads the row and is idempotent.

## Consequences

There is no extra infrastructure, and a rolled-back order never leaves an orphan timer. Postgres carries the queue load, which is fine at v1 scale. `todayz worker` runs as its own Beanstalk worker tier.
