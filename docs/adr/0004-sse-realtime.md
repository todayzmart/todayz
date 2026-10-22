# ADR 0004: Server-Sent Events for realtime

- Status: accepted
- Date: 2026-10-22
- Plan: docs/plan/02-architecture.md

## Context

Shops need new-order alerts within 3 s, and customers need order status updates. Traffic is mostly server to client.

## Decision

Use SSE streams per store, per user and for admin. Fan out across API instances with Postgres LISTEN/NOTIFY. Send a 25 s heartbeat. Replay with `Last-Event-ID` from `outbox_events` (24 h). Fall back to 15 s polling.

## Consequences

It works through Cloudflare and ALB with plain HTTP and needs no WebSocket infrastructure. Payloads are small and clients refetch through TanStack Query. Web push covers closed apps.
