# Testing

Every test layer in TodayZ, where it lives, how to run it, and how to add a test for a new feature.

> As built on main at 2c2f23d6 (27 Oct 2026). Counts are rough, from reading the source (not from a run). Update this file in the same change as the code it describes.

The plan behind this is [docs/plan/08-testing.md](../plan/08-testing.md). The Definition of done is in [docs/plan/README.md](../plan/README.md). Product numbers come from [01-product-rules.md](../plan/01-product-rules.md); tests read configurable ones from `platform_settings`, never hard-code them.

## Test layers

| Layer | Where it lives | Command | When it runs | Roughly |
|---|---|---|---|---|
| Go unit and integration (real PostGIS through [testkit](../../backend/internal/testkit/)) | `backend/internal/**/*_test.go` | `make check-backend-test` (inside `make check`), or `go -C backend test ./internal/<pkg>/...` | `make check`, CI `ci.yml` | ~260 files, ~1,350 test funcs |
| Go fuzz and property tests | `backend/internal/**` (one fuzz test, two `rapid` files) | run with the Go tests | `make check` | a handful |
| API contract smoke | [backend/api/](../../backend/api/), `TestContractSmoke` | in `make check` | `make check` | 1 suite, every operation |
| Contract hooks (Schemathesis checks) | [e2e/contract/](../../e2e/contract/) | in `make check` | `make check` | 14 unit tests |
| Contract full (Schemathesis fuzz against the running API) | [e2e/contract/run.sh](../../e2e/contract/run.sh) `full` | `make e2e` (step `e2e-contract`) | `make e2e`, CI `e2e.yml` | all operations |
| Mocks server | [e2e/mocks/](../../e2e/mocks/) | in `make check` | `make check` | ~80 test funcs |
| Front end unit and component (Vitest, Testing Library, MSW) | `customer/`, `business/`, `admin/`, `site/` (`*.test.ts(x)`) | `pnpm --filter @todayz/<app> test` | `make check` | customer ~220, business ~500, admin ~250, site ~75 |
| Shared package tests | `shared/*` | `pnpm --filter './shared/*' test` | `make check` | api-client ~40, auth ~77, config ~17, i18n ~76, realtime ~24, telemetry ~26, tokens ~11, ui ~175 |
| App smoke (Playwright, built `build:e2e` preview) | `<app>/e2e/*.spec.ts` | `pnpm --filter @todayz/<app> e2e:smoke` | `make e2e` (step `e2e-frontend-smoke`) | customer ~40, business ~50, admin ~40, site ~40 (with the stack specs below) |
| App stack specs (Playwright against `make dev`) | `<app>/e2e/*.stack.spec.ts`, `playwright.stack.config.ts` | `pnpm --filter @todayz/<app> exec playwright test -c playwright.stack.config.ts` | `make e2e` | a few per app |
| Cross-app journeys | [e2e/journeys/specs](../../e2e/journeys/specs/) | `make e2e-journeys` | `make e2e` | 23 files, ~38 cases |
| Accessibility (axe) | [e2e/a11y/](../../e2e/a11y/) | in `make e2e` (step `e2e-a11y`) | `make e2e` | ~75 routes at 360, 390 and 1280 px, plus a phone pass in hi, kn, te, ta |
| Visual regression | [e2e/visual/](../../e2e/visual/) (apps), `shared/ui` (components) | in `make e2e` (steps `e2e-visual`, `e2e-ui-visual`) | `make e2e` | 56 app baselines (en and pseudo-locale), plus component baselines |
| Lighthouse, marketing site | [site/lighthouserc.json](../../site/lighthouserc.json), [site/scripts/lighthouse.mjs](../../site/scripts/lighthouse.mjs) | ends `e2e:smoke` in the site | `make e2e` | every page, mobile and desktop |
| Lighthouse, app shells | [e2e/a11y/lighthouse.mjs](../../e2e/a11y/lighthouse.mjs) | `make lighthouse-apps` | manual, not in `make e2e` | three apps |
| Bundle size budgets | `size-limit` in each app | in `make check` | `make check` | customer 170 KB gzip, business and admin 250 KB gzip |
| Load (k6) | [e2e/load/](../../e2e/load/) | `make load` | `make load`, release candidate | scenarios below |
| Security (static and dynamic) | [e2e/security/](../../e2e/security/), `.trivyignore.yaml` | `make security` | `make security`, CI `security.yml` | see below |
| AI evals, recorded | `backend/internal/ai/eval` and per-feature eval folders | `go run ./cmd/todayz ai eval all` (inside `make check`) | `make check` | every AI feature |
| AI evals, live | same packages, `MOCK_LLM=0` | `make ai-eval` | opt-in only | with your own keys |
| Repo hygiene | `scripts/`, `mk/` | in `make check` | `make check` | gitleaks, shellcheck, makefile tests, hooks, actionlint, runbook links, tracked-files |
| Terraform static | [infra/](../../infra/) | in `make check` | `make check` | fmt, validate, tflint, checkov, policy allow-list |

Migrations (about 65 files in `backend/db/migrations`) are exercised by every DB-backed Go test: testkit migrates a template database first.

## The gates

| Command | What it proves | Network |
|---|---|---|
| `make check` | Everything above except the browser journeys, load, live AI and manual Lighthouse. Starts with `make gen` | none (no-egress) |
| `make e2e` | Journeys, contract fuzz, a11y, visual and smoke, site Lighthouse. Starts `make dev` first (idempotent) | none (no-egress) |
| `make load` | k6 scenarios | local only |
| `make security` | Secret scan, image and dependency scan, infra scan, ZAP baseline, abuse suite | local only |
| `make release-candidate` | All of them on a clean reset; writes [docs/qa/rc-<date>/](../qa/) | local only |

Run `make check` before every change you hand over. `make e2e` runs the steps in this order: stack smoke, app smoke, journeys, contract fuzz, a11y, visual, UI visual. Journeys come before the fuzz on purpose: the sign-in code limit counts every send, and the fuzzer would use it up. Start from a fresh `make reset` ([local-dev.md](local-dev.md)).

### No-egress

`make check` and `make e2e` run under [scripts/no-egress.sh](../../scripts/no-egress.sh) (SC-PL-04, D-17). It points proxy variables at a sink, unsets every provider key, sets `TODAYZ_NO_EGRESS=1`, `MOCK_LLM=1` and `GOPROXY=off`, and exits 3 if anything tried to go outside. Provider base URLs are `*_API_URL` names that point at the local mocks, never SDK defaults. If you add a new outside call, route it through `internal/egress` and add a mock.

## Journeys

Cross-app Playwright journeys in [e2e/journeys/specs](../../e2e/journeys/specs/). Each is named after a scenario ID in [e2e/scenarios](../../e2e/scenarios/) (the catalogue files `AI`, `ASK`, `BSK`, `FA`, `OB`, `UI`) or the plan's E2E list.

| Spec | Scenario | What it proves |
|---|---|---|
| 01 | E2E-02 | Referral credits, a Rs 300 order using credits, cashback, wallet and the credit account |
| 02 | E2E-03 | Shop accepts part of an order, customer confirms, order is collected |
| 03 | E2E-06 | POS walk-in sale, bill QR credits the customer |
| 04 | E2E-01 | Standee scan, search, pickup order, collect, review, store referral |
| 05 | E2E-10 | Admin hides a review, the public rating drops |
| 06 | E2E-12 | Super admin changes a credit cap with step-up, audit entry written |
| 07 | E2E-13 | Baskets: fill the cart, order splits by shop |
| 08 | E2E-15 | Language switch in the middle of a journey |
| 09 | E2E-16 | Offline banner |
| 10 | E2E-17 | One owner, several stores |
| 11 | E2E-18 | Cashier permissions |
| 12 | E2E-19 | Blocked customer |
| 13 | E2E-20 | Suspended shop |
| 14 | E2E-05 | Delivery order |
| 15 | CR-15 | Credit adjustment needs a second finance admin |
| 16 | E2E-08 | Field-agent onboarding |
| 17 | E2E-11 | Voice stock update |
| 18 | E2E-13 | AI shopping-list parse |
| 19 | E2E-14 | Bill scan, waste, flash offer |
| 20 | E2E-04 | No-shows lead to strikes |
| 21 | E2E-07 | Offline counter code (seven cases, with variants) |
| 22 | E2E-21 | Ask shops nearby |
| 30 | E2E-09 | Statement to payout |

Global setup opens the two main seeded shops for the whole day and resets the test clock afterward. Specs that move the clock (20 and 21) run last. Reports never store sign-in codes, tokens or phone numbers.

## Accessibility and visual

- **a11y.** [e2e/a11y/routes.ts](../../e2e/a11y/routes.ts) lists every route per app. Each is checked with axe at 360, 390 and 1280 px; the sign-in and home pages are also checked in Hindi, Kannada, Telugu and Tamil on a phone. A new route must be added there.
- **Visual.** Routes marked `visual: true` get a baseline screenshot in English and the pseudo-locale (`en-XA`, which stretches text to catch clipping). Refresh baselines on purpose with `TZ_VISUAL_APP=<app> ... --update-snapshots` and review the diff. `shared/ui` has its own component baselines (`design`, `a11y` and `overlays` specs, plus a Storybook build in `make check`).

## Lighthouse

| Target | Gate |
|---|---|
| Marketing site, every page, mobile and desktop | at least 0.95 in every category |
| Exception D-102, widened to `/kn/` by D-262 | mobile performance on `/hi/`, `/kn/` and `/te/` is gated at 0.85 (the other categories stay at 0.95) |
| Draft-locale pages in the e2e build | the SEO category is replaced by its other audits, because those pages carry `noindex` until approved |
| App shells | measured by hand with `make lighthouse-apps`; reports go to `e2e/a11y/out/lighthouse` (git-ignored) |
| Bundle size | customer 170 KB gzip; business and admin 250 KB gzip |

## Load and security

- **Load** ([e2e/load](../../e2e/load/README.md)): k6 scenarios `browse_peak`, `order_burst`, `pos_rush`, `otp_abuse` and a long `soak`, plus a Go driver for live-update fan-out (`sse_fanout`). Data comes from `make load-data` (1,000 shops, 50,000 customers). SQL checks assert money and stock stay consistent after `order_burst` and `pos_rush`.
- **Security** (`make security`): gitleaks; Trivy, which fails on fixed HIGH or CRITICAL findings (entries in `.trivyignore.yaml` expire, D-215); checkov on `infra/`; a ZAP baseline and the abuse suite ([abuse.stack.test.mjs](../../e2e/security/abuse.stack.test.mjs), seven cases) which run only when the stack is up. The weekly CI run also reports unfixed Trivy findings.

## AI evals

AI output is always a draft a person confirms. Evals check that drafts are good and that nothing acts without confirmation.

- **Recorded** (in `make check`): runs every feature's eval against recorded model replies, offline, deterministic. This is the gate.
- **Live** (`make ai-eval`): calls the real model with your own key in `.env.local`. Opt-in; never runs in CI or under no-egress. Pick a suite with `AI_EVAL_SUITE`. Re-record fixtures after a prompt or model change.

## Run one test fast

```bash
# One Go package (fast, no DB needed with -short)
go -C backend test -short ./internal/credits/...
# One Go test with a real DB (start the shared server once: make testpg-up)
export TODAYZ_TEST_PG_URL_FILE=$PWD/.cache/testpg.url
go -C backend test ./internal/orders/ -run TestAccept -count=1

# One front end file
pnpm --filter @todayz/business exec vitest run src/pos/cart.test.ts
# One Playwright file against the built preview
pnpm --filter @todayz/customer exec playwright test e2e/search.spec.ts
# One journey (stack must be up and freshly reset)
pnpm --filter @todayz/e2e-journeys exec playwright test specs/04-*.spec.ts
# Component stories and their visual checks
pnpm --filter @todayz/ui storybook
```

Run `make gen` first on a fresh checkout: generated code is not committed. See [local-dev.md](local-dev.md) for the stack and the shared test database.

## Adding a test for a new feature

1. **Rule first.** Write the rule in [01-product-rules.md](../plan/01-product-rules.md) (or ask the lead). Tests check rules, not code.
2. **Backend.** A table-driven Go test next to the code. Use `testkit.NewDB(t)` for anything that touches SQL. Cover the money, code, timer and permission cases, and the error each failure returns. Money and codes get a property test where the rule is an invariant.
3. **Contract.** Edit only your own files in `backend/api/paths/` and `backend/api/schemas/`, then `make gen`. Every operation needs error responses as `application/problem+json`; the contract hooks check that.
4. **Front end.** Vitest and Testing Library for the screens and hooks; MSW handlers from the generated `@todayz/api-client`. Check loading, empty, error and offline states.
5. **Strings.** Add keys to `shared/i18n`, then run the message lint ([shared/i18n/README.md](../../shared/i18n/README.md)). No hard-coded text.
6. **Routes.** Add the route to `e2e/a11y/routes.ts`; mark it `visual: true` if it is a main screen, and refresh baselines on purpose.
7. **Journey.** If the feature crosses apps or moves money, add a spec to `e2e/journeys/specs` and a scenario ID in `e2e/scenarios`.
8. **AI.** Add recorded eval cases next to the feature's prompt, including a case where the model is wrong.
9. **Run** `make check`, then `make e2e` from a fresh `make reset`.

## Definition of done

A change is done when the plan's checklist in [docs/plan/README.md](../plan/README.md) holds: the rule is in the plan, tests exist at the right layers, `make check` is green, docs and the feature page are updated, and the decision log has an entry if you changed a rule. The latest release candidate record is in [docs/qa/rc-2026-10-26/](../qa/rc-2026-10-26/summary.md); at that run, reset, e2e and load passed while check and security were failing, so read the summary before trusting a green claim.

## Next

[local-dev.md](local-dev.md), [deployment.md](deployment.md), [docs/plan/08-testing.md](../plan/08-testing.md).
