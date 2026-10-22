# ADR 0001: Go for the backend

- Status: accepted
- Date: 2026-10-22
- Plan: docs/plan/02-architecture.md

## Context

TodayZ needs one API and worker binary that is cheap to run on Elastic Beanstalk, starts fast, and handles SSE fan-out and timers for many shops. The user prefers Go.

## Decision

Write the backend in Go: one module, one binary with sub-commands (`api`, `worker`, `migrate`, `seed`, `ai eval`, `admin`). Use chi v5 for routing and the stdlib `net/http`. The contract is spec-first (oapi-codegen strict server, kin-openapi validation).

## Consequences

Small static binaries and low memory per instance. Strong concurrency for SSE and timers. The team writes plain SQL instead of using an ORM (see 0002). Frontends stay TypeScript, so types cross the boundary only through the OpenAPI spec.
