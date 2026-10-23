# TodayZ implementation plan

This plan is written so that many Claude agents can build TodayZ in parallel without stepping on each other, and so that every piece is verified by tests, scenarios and manual QA before it counts as done.

Read in this order:

| # | File | What it holds |
|---|---|---|
| 1 | [01-product-rules.md](01-product-rules.md) | Every product decision and number (booking timers, credits, referrals, codes, payouts). Single source of truth. |
| 2 | [02-architecture.md](02-architecture.md) | Stack, folder layout, services, auth, offline, i18n, maps, voice, integrations, security. |
| 3 | [03-data-model.md](03-data-model.md) | Tables, state machines, the credit ledger, invariants. |
| 4 | [04-api.md](04-api.md) | API conventions and every endpoint group. |
| 5 | [05-observability.md](05-observability.md) | Metrics, logs, traces, errors, RUM, product analytics, retention, dashboards, alerts. |
| 6 | [06-screens.md](06-screens.md) | Every screen of the customer app, business app, admin console and site, with acceptance criteria. |
| 7 | [07-work-packages.md](07-work-packages.md) | The agent-sized work packages: owner folders, dependencies, deliverables, tests, done criteria. |
| 8 | [08-testing.md](08-testing.md) | Test strategy, the scenario catalogue, manual QA scripts, device matrix, load and security tests. |
| 9 | [09-infra-release.md](09-infra-release.md) | Environments, AWS and Cloudflare setup, CI/CD, secrets, launch checklist, rollback. |
| 10 | [10-later.md](10-later.md) | Agreed TODOs that are out of v1 (catalogue scraping, billing, storefronts). |
| 11 | [11-ai-features.md](11-ai-features.md) | AI features for retailers, consumers and admins: rules (draft then confirm), v1 picks, evals, work packages W3-10…W3-13. |
| 12 | [12-decision-log.md](12-decision-log.md) | Why the plan says what it says: every review finding and how it was settled (D-01…D-101). If a numbered file disagrees with the log, fix the numbered file and add an entry. |

The visual source of truth is the design system artifact: https://claude.ai/artifact/W91M8EtFX8zJc9SgMUygZF (v8: white page, mint wash, pastel tints, Kargul-style controls, Lucide icons, Anek + Figtree).

## How the work is organised

The build runs in **waves**. Inside a wave, every work package (WP) owns a disjoint set of folders/files, so agents can run at the same time. A wave starts only when the WPs it depends on are merged and `make check` is green.

```
Wave 0  Foundation (1 lead agent, sequential)        repo, tooling, contracts, DB baseline, design tokens, CI, local stack
Wave 1  Platform (8–11 agents in parallel)           auth, businesses, catalogue, search/geo, UI kit, i18n, infra, observability base, jobs platform, site
Wave 2  Core flows (13 agents in parallel)           bookings, codes, POS, credits ledger, referrals, notifications, realtime, reviews, delivery, app screens; W2-13 gate
Wave 3  Money, intelligence, ops, AI (16 agents)     payouts, fraud rules, demand insights, voice, AI features, admin pulse, dashboards, alerts, field agents, jobs and system consoles, ask shops nearby; W3-14 E2E gate
Wave 4  Hardening (6–8 agents)                       full E2E, load, security, accessibility, translations, manual QA, local release candidate
Wave 5  Cloud (after the user hands over credentials) AWS + Cloudflare provisioning, staging, prod, launch
```

**Local first (user decision, 2026-10-22):** waves 0–4 run entirely on the local machine: Docker Postgres+PostGIS, MinIO for R2, the Grafana LGTM container for observability, a mock SMS/push/WhatsApp/STT/LLM server, and local HTTPS hostnames (`app.todayz.localhost` …). Nothing is deployed and no cloud account is touched until the whole product is implemented end to end and every scenario, manual QA script, load and security test passes locally. Then the lead tells the user "ready for cloud" with the evidence, and the user puts the AWS, RDS, Beanstalk and Cloudflare credentials in the environment. Wave 5 starts only then. Terraform and CI files are written during the earlier waves. Before Wave 5 the only Terraform steps are `terraform fmt -check`, `terraform validate`, tflint and checkov; the first `terraform plan` runs in Wave 5, and nothing is applied before then (D-16). SMS, push and Sentry always use mocks or local tools before staging. Anthropic and Sarvam use mocks by default; the user may put their own keys in `.env.local` for live AI evals and manual AI QA, but the default suite never needs them (D-17).

The exact list, with dependencies and file ownership, is in [07-work-packages.md](07-work-packages.md).

**Wave gates (D-11):** the W2-13 gate runs only the E2E journeys whose dependencies are all in Waves 0–2 (the "Gate" column of the E2E table in [08-testing.md](08-testing.md)). E2E-01, 08, 09, 11, 13 and 14, and the E2E part of CR-15, run at the Wave 3 gate, W3-14. W4-01 runs all of them.

## Roles every wave uses

| Role | Agent type | Job |
|---|---|---|
| Lead / orchestrator | main session | Starts WPs, keeps the dependency graph, merges, resolves conflicts, updates this plan when a decision changes. |
| Builder | Backend Architect / Frontend Developer / DevOps Automator | Implements one WP in its own git worktree, with tests. |
| Reviewer | Code Reviewer (+ Security Engineer for auth, money, codes) | Reviews the WP diff against its acceptance criteria and [01-product-rules.md](01-product-rules.md). Cannot be the builder. |
| QA | Evidence Collector / API Tester | Runs the WP's scenarios and manual script in the browser pane, attaches screenshots, files defects back to the builder. |
| Reality check | Reality Checker | At the end of each wave: runs everything end to end and says what is not actually done. |

## Definition of done (every WP)

A WP is done only when all of these are true:

1. Code is in the folders the WP owns, and nothing else changed (or the lead approved the extra change).
2. `make check` passes at the repo root: lint, type check, unit tests, integration tests, contract tests, build for every app.
3. New behaviour has tests at the levels the WP lists (unit, integration, contract, component, E2E). Money, codes, timers and permissions have table-driven tests for every branch.
4. Every scenario ID the WP lists in [08-testing.md](08-testing.md) passes, automated where marked.
5. The manual QA script for the WP was run by the QA agent with screenshots, on the device sizes listed.
6. The UI matches the design system: tokens only (no raw hex), `tz-` classes or the shared React components, Lucide icons, sentence case, light only.
7. All user-facing text goes through i18n keys (English filled, other languages can be pending).
8. Metrics, logs and traces for the new code follow [05-observability.md](05-observability.md). New endpoints appear on the API dashboard without extra work; new business events are emitted.
9. No secret, key, token, OTP or phone number appears in code, logs, test snapshots or chat output. An OTP is never logged in any environment, `local` and `test` included (D-15).
10. The reviewer approved, and the WP's docs (folder `CLAUDE.md`, API spec, plan file if a rule changed) are updated.

## Ground rules for agents

- Never print, echo or log the values of environment variables that hold secrets (AWS, Cloudflare, SMS, WhatsApp, Sentry, Grafana, LLM keys). Refer to them by name only.
- Never log an OTP, in any environment. In `local` and `test`, the reserved numbers `+91 90000 000xx` accept `000000`; any other number goes through the mock SMS server and shows in the mocks UI (`mocks.todayz.localhost`). There is no `dev` environment (D-15).
- Do not create GitHub repositories or push anywhere. The user will say when and give repo names. Work in the local git repo at the project root.
- The API contract is the agreement between backend and frontends. The bundled `backend/api/openapi.yaml` is generated: never edit it. Edit only your own domain's files in `backend/api/paths/` and `backend/api/schemas/`, then run `make gen` in the same change (D-55). Generated code (`backend/gen`, `shared/api-client`, the bundled `openapi.yaml`) is not committed (D-50).
- Database changes only through new migration files. Never edit a migration that has been merged.
- Product rules live in [01-product-rules.md](01-product-rules.md) and in `platform_settings`; never hard-code a number that the rules say is configurable.
- When something in the plan is ambiguous, stop and ask the lead; the lead asks the user. Do not guess on money, codes, timers or permissions.
