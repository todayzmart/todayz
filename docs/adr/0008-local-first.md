# ADR 0008: Local-first build and test

- Status: accepted
- Date: 2026-10-22
- Plan: docs/plan/02-architecture.md

## Context

The user wants the whole product implemented and tested end to end before any cloud account is touched. AWS and Cloudflare credentials arrive only for Wave 5.

## Decision

Waves 0–4 run entirely on the developer machine: Docker (Colima) PostGIS on host port 5433, MinIO for R2, grafana/otel-lgtm for Grafana Cloud, a mocks server for SMS/WhatsApp/push/STT/LLM/Turnstile, and Caddy local TLS on `*.todayz.localhost`. `make check` and `make e2e` run with outbound traffic blocked and provider keys unset (`scripts/no-egress.sh`, SC-PL-04). Terraform only runs `fmt`/`validate`/tflint/checkov before Wave 5.

## Consequences

The default suite is reproducible offline and never sends a real SMS. The egress block works at the proxy level (every HTTP client honours it), and the backend adds a dial-level guard when `TODAYZ_NO_EGRESS=1`. A raw socket that ignores proxies would not be caught by the wrapper alone. Staging is the first place real providers are used (D-17).
