# e2e/

Cross-app tests: Playwright journeys, contract (Schemathesis), k6 load, security (ZAP, abuse suite), and the mocks server (`e2e/mocks`).

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 08-testing.md; 07-work-packages.md (scenario owners).

## Run

- `make e2e`, `make load`, `make security`
- Mock SMS/push UI: https://mocks.todayz.localhost

## Rules for this folder

- The default suite needs no real provider keys and cannot reach the internet (SC-PL-04).
- Never store an OTP, code, token or phone in a snapshot or report.
- Each WP owns `scenarios/<AREA>.md`; QA evidence goes in `docs/qa/<wp-id>/`.

## Status

Stub from W0-01. The WP that builds this folder fills in layout and conventions.
