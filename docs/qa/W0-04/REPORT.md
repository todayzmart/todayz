# W0-04 Contract and client: QA report

- **Branch:** `worktree-agent-ada8bdc994bd05835`, rebased onto `main` d402367
- **Machine:** macOS arm64 (Apple M-series), Docker via Colima (6 CPU / 10 GB), Go 1.26.10, Node v25.9.0, pnpm
- **Dates:** 2026-10-22 (build, review fixes), 2026-10-23 (review fixes, round 2)
- Everything ran locally. Nothing touched a cloud account; no remote, no push. Lefthook (gitleaks, forbidden-files) ran on every commit, never bypassed. No generated file is committed (`backend/gen`, `backend/api/openapi.yaml`, `shared/api-client/src/gen`, `handlers.gen.ts`, `e2e/contract/out`, `.tools` are ignored).
- Built by the W0-04 agent (Backend Architect). The lead reviewed twice; both fix rounds are recorded here and in D-100.

## What was built

**`backend/api/` (contract source, D-50/D-55).** `root.yaml` (info, servers, bearer and three refresh-cookie security schemes, platform tag), `common/{headers,parameters,responses}.yaml` (Problem, pagination, Idempotency-Key, If-Match/ETag, Accept-Language, common params), `redocly.yaml`, `README.md` ("how a domain adds a path"), and one pre-created, commented, empty `paths/<domain>.yaml` and `schemas/<domain>.yaml` per domain: 42 domains (the 40 from 04 plus `admin-jobs` W1-11 and `admin-system` W3-15). Only `platform.yaml` has operations (`/healthz`, `/readyz`, `/version`, `GET|POST /v1/test/clock`) until the domain WPs land; 294 operations are expected in all.

**`apigen` (`backend/internal/devtools/apigen`).** `root` joins the domain files into `gen/api/root.yaml` and lints the raw YAML (one domain per file, tag equals the file's domain, unique paths / operationIds / schema names, a domain may only reference its own and common schemas). `check` loads the Redocly bundle with kin-openapi, validates examples against schemas, and runs the contract lints: SC-PL-01 (allowed prefixes, `/healthz|/readyz|/version` the only non-`/v1` paths, duplicate method+path), SC-PL-02 (`x-problems` codes registered in `api/problems`), a Problem `default` on every operation, `x-idempotency`/`x-upload`/`x-money` rules, 04 §1.2 path rules, the keyword ban, `$ref` domain ownership, request-body bounds (`maxLength`/`maxItems`), closed request objects, media types, the 04 §1.6 money list, `-expect-count N`. `codegen` writes `gen/api/spec` (embedded bundle) and one oapi-codegen package (`types,chi-server,strict-server`) plus `register.gen.go` per domain tag. 47 `testdata/` fixtures, each proving one lint fires with its exact message.

**Request validator (slot 13, `backend/internal/platform/httpx/openapi`).** Runs after chi has picked the route (one router decides, D-100) and validates every request against the embedded bundle: 422 `validation_failed` with one `fields` entry per problem keyed by the full prefixed path (`body.items[2].quantity`, `query.limit`, `path.orderId`; at most 50, then `fieldsTruncated`), 415 for a wrong `Content-Type`, 400 `bad_request` for malformed JSON, trailing data, more than 10,000 values or an array past its `maxItems`, 413 past the body limit. Slot 8 `pathguard` answers 400 first for a raw path two routers could read differently. Authn (`/v1/me`, `/v1/biz`, `/v1/admin` prefixes) runs before validation, idempotency after it. Route policies come from the operation's `x-idempotency`/`x-upload`/`x-money` through `route.Source`, merged with the code's (stronger wins; code weaker than the contract, or `Upload` without `x-upload`, fails startup). `cmd/todayz/contract_test.go` fails closed both ways (`TestRoutesAreInTheContract`, `TestContractOperationsAreMounted`).

**`@todayz/api-client` (`shared/api-client`).** `scripts/bundle.mjs` (Redocly bundle/lint, telemetry off), `scripts/gen.mjs` (bundle to JSON, orval fetch client split per domain tag plus Zod schemas with a `request()` mutator, `src/mocks/handlers.gen.ts` from the contract examples via `scripts/msw.mjs`, `src/gen/problems.json` from the problem registry `make gen` passes in), `src/http.ts` (`configure`, `request`, `ApiError` carrying the problem code, `requestId`, `retryAfterSeconds`; registered fallback codes only; `checkPath`), `src/mocks/index.ts` (`handlers`, `examples`, `problemResponse`). Exports: `@todayz/api-client`, `…/gen/<domain>/<domain>`, `…/<domain>.zod`, `…/mocks`.

**Schemathesis (`e2e/contract`).** `run.sh smoke|full` (offline, pinned, `--checks all`, JUnit and Hypothesis data under the report dir, base URL allowlist), `hooks.py` with the `todayz_problem` check (every error is `application/problem+json` with a registered code, SC-PL-02), `test_hooks.py`, hash-pinned `requirements.txt`. `mk/25-contract.mk`: `setup-contract` (uv 0.10.6 and CPython 3.12.12 pinned into `.tools/` by `scripts/install-uv.sh`, SHA-256 of the tarball and of the binary per platform), `gen-contract`, `check-contract` (= lint:spec, api-client lint/typecheck/test, hooks unit tests, `contract-smoke`), `e2e-contract` (full run through Caddy with the local CA, after `e2e-stack-up`; `moveTestClock` excluded), `contract-lock`.

**D-90.** `goose.WithAllowOutofOrder(true)`; `TestOutOfOrderBlocks`; `migrate.CheckBlockOrder` + `TestMigrationBlockOrder` (a migration touches only tables of its own or a lower block).

**Docs.** `backend/CLAUDE.md`, `backend/api/README.md`, `backend/db/migrations/README.md`, `shared/CLAUDE.md`, `shared/api-client/CLAUDE.md`, `e2e/CLAUDE.md`; `docs/plan/02-architecture.md` §2.4, `04-api.md` §1.4/§1.6, `08-testing.md` PL-01, `12-decision-log.md` (D-90 status, D-100).

## Tests

| Suite | What |
|---|---|
| `httpx/openapi` | valid, missing field, wrong type, unknown field, `x-open-object`, oversized (413 by Content-Length and by stream), 415, non-JSON 400, query/path/header errors, policy from the spec, chi 404/405 with `Allow`, OPTIONS passes, anonymous 401 under the three prefixes, media-type refusal at `New`, `maxItems` under `oneOf`/`anyOf`, amplification refused (H-2) and bounded (`TestErrorAmplificationBound`) |
| `httpx/mw/pathguard` | the H-1 bypass program as a regression test |
| `httpx/route`, `httpx` | policy merge, `Upload` needs the contract, trailing-slash pattern refused, code weaker than the contract refused |
| `apigen` | 47 fixtures (each lint's exact message), `boundedPattern` table, real-contract round-trip, `TestCodegenRoundTrip` (never skips under `make check`) |
| `cmd/todayz` | `TestRoutesAreInTheContract`, `TestContractOperationsAreMounted`, `TestContractSmoke` (in-process api on a fresh testkit DB, examples + coverage phases, every error a registered problem), `TestContractProblemCheckFires`, `TestDomainsRegisterThroughGeneratedRegister` |
| `migrate` | `TestOutOfOrderBlocks`, `TestMigrationBlockOrder` over the real migrations, `TestCheckBlockOrderFires` (16 cases) |
| `shared/api-client` vitest | 39: `http.test.ts` 18 (wrapper, path checks, Content-Type resolution, registry from `problems.json`), `msw.test.ts`, `generated.test.ts`, `regen.test.ts` |
| `e2e/contract/test_hooks.py` | 13 (registry, declared statuses, problem errors on content type / unregistered code / status mismatch / non-JSON) |

Acceptance proof: adding `GET /v1/discover/ping` to `paths/discover.yaml` and running `make gen` produced `backend/gen/api/discover/api.gen.go`, `shared/api-client/src/gen/discover/discover.ts` and the MSW handler; removing it and running `make gen` again removed all three, `git status` clean.

## Review fixes

Applied the lead-approved fix list from the code and security reviews in commit `15f6725` (`lead: W0-04 review fixes`). Decisions recorded as D-100: validation errors are 422 `validation_failed` (400 for malformed JSON, 415 for the wrong media type); one router decides (chi routes first, the validator looks the operation up by chi's route pattern, a new path guard 400s ambiguous raw paths; H-1 regression test); authn runs before validation and idempotency after it; field paths keep their prefixes; route policies come from the contract (`x-idempotency`, `x-upload`, `x-money`) merged with the code, stronger wins; decoder caps against amplification (10,000 values, `maxItems`, 50 field errors; H-2 regression test); Redocly stays in `shared/api-client` with every path passed explicitly; `make e2e` brings the stack up itself. Toolchain hardened: uv and CPython pinned to the patch into `.tools`, `run.sh` only fuzzes `*.todayz.localhost`/loopback, Hypothesis data kept under the report dir, kin-openapi aligned at v0.149.0. New lints: keyword ban, `$ref` domain ownership, `maxItems`/`maxLength` bounds, `x-open-object` on typed objects, D-90 migration block order. Client: `urlEncodeParameters`, path checks in `request()`, registered codes only, safe `ApiError.toJSON()`, MSW output escaped. Evidence: `make check` exit 0; `make e2e` exit 0 from a stopped stack (Schemathesis full: 39/39 passed, JUnit in `e2e/contract/out/`).

## Review fixes, round 2

Applied the lead's second fix list (`lead: W0-04 review fixes, round 2`); every fix has a test that fails without it. Recorded in D-100 ("Round 2").

- **M-A media types.** A request body is `application/json` only unless the operation is `x-upload: true`, and then it may not be JSON; `apigen check` (fixtures `merge-patch-without-upload`, `json-plus-text`) and `openapi.New` (`TestNewRefusesMediaTypes`) refuse any other contract.
- **M-B `/v1/me` is 401.** `authPrefixes` gained `/v1/me`; `TestAnonymousIs401` proves an anonymous call with a bad body is 401 with no field name on `/v1/me`, `/v1/biz` and `/v1/admin`.
- **M-C type arrays.** `lintBounds`/`lintClosedObjects` use `Types.Includes` (fixtures `nullable-array`, `nullable-string`, `nullable-object`); the `maxItems` pre-scan walks `oneOf`/`anyOf` (`TestMaxItemsUnderOneOf`).
- **Low 1.** D-100 and `backend/CLAUDE.md` say the body is buffered (at most 1 MB), pre-scanned, then validated; no "while streaming".
- **Low 2.** kin-openapi v0.149.0 has no "stop after N errors" option (only fail-fast or every error), so the bound is measured and tested: `TestErrorAmplificationBound`, a 1,022,459-byte body with 9,627 values and 4,800 unknown fields, 10.8 MB allocated, 17–20 ms wall-clock (three runs, Apple M-series, Go 1.26.10); the test fails above 200 MB or 2 s. Recorded in D-100.
- **Low 3.** `Upload: true` in code on an operation the contract does not mark `x-upload` fails startup (`route.TestUploadNeedsTheContract`, `httpx.TestRouterCheck`).
- **Low 4.** apigen holds the 04 §1.6 money operationIds (`moneyOperations`, 31 ids); an operation with one of them and no `x-money` fails (fixture `money-id-without-x-money`). The list is written in 04 §1.6 and `backend/api/README.md`.
- **Low 5.** A contract path ending in `/` is refused (fixture `trailing-slash`, `TestLintPathShapesTrailingSlash`), and so is a mounted pattern (`Check()`, `TestRouterCheck`).
- **Low 6.** `boundedPattern` refuses a top-level alternation outside a group and treats an escaped `\$` as not anchored (fixtures `pattern-alternation`, `pattern-escaped-dollar`, eight new `TestBoundedPattern` rows).
- **Low 7.** `$dynamicRef`, `$dynamicAnchor`, `contentSchema` banned (three fixtures).
- **Low 8.** `run.sh` refuses a base URL containing a backslash, whitespace or `@` before parsing it (allowlist script rerun: 12 refused, 6 allowed).
- **Low 9.** `install-uv.sh` records the SHA-256 of the uv binary inside each tarball (four platforms, computed from the pinned tarballs) and compares the installed file on every run; a version-reporting wrapper with another sum is reinstalled, and a tarball whose inner binary mismatches is refused after extraction (offline script with four cases, all behave).
- **Low 10.** `request()` resolves by the response's `Content-Type`: JSON and `+json` parsed, `text/*` as text, else a Blob; the CSV test asserts the text and swallows nothing; `generated.test.ts` stubs send `application/json` like the API.
- **Low 11.** `checkPath` refuses a backslash, tab, CR or LF (six cases).
- **Low 12.** "an unknown field is 422 validation_failed" in all 42 schema headers; `registry_test.go`'s fixture says 422; no other 400 wording in the repo (`docs/qa/W0-03/REPORT.md` is historical).
- **Low 13.** `shared/` no longer reads `backend/`: `make gen` passes `TODAYZ_PROBLEMS_DIR` to `scripts/gen.mjs`, which writes `src/gen/problems.json` (git-ignored); `http.test.ts` reads that, fails when it is missing under `TODAYZ_CHECK` and skips otherwise (both proven).
- **Low 14.** `CheckBlockOrder` follows `REFERENCES` in CREATE and ALTER TABLE, `CREATE POLICY … ON`, and the FROM/JOIN tables of `CREATE [MATERIALIZED] VIEW` (a view counts as created; a CTE name the view declares is not a table); six new failing cases and one allowed file in `TestCheckBlockOrderFires`. What it still does not read is listed in `blocks.go` and `db/migrations/README.md`.
- **Also.** D5 wording: every path is passed explicitly, the problems dir goes to the client generator and the hooks. `docs/plan/README.md` and the root `CLAUDE.md` say D-01…D-100.

## Evidence, round 2

- `make check` exit 0 (2026-10-23): gofmt, vet, golangci-lint, `go test -race` (all packages ok, including `cmd/todayz` contract smoke, `apigen`, `httpx/openapi`, `migrate`), gosec, govulncheck, settings lint, Redocly lint, `apigen check`, api-client eslint/tsc/vitest (39 passed, registry test running against `src/gen/problems.json`), hooks unit tests, repo self-test 226 ok. One pre-existing Redocly warning (`DeliveryFailedReason` unused until its domain lands, D-60).
- `make e2e` exit 0 from a stopped stack (2026-10-23): `e2e-stack` smoke, then `e2e-contract` (Schemathesis v4.29.1 full run through Caddy: 39 generated, 39 passed, `todayz_problem` check on; JUnit under `e2e/contract/out/`), then the Playwright suites (5, 7, 4, 3 passed). `make stop` exit 0 afterwards; the stack is down.
