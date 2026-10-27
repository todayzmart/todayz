# e2e/

Cross-app tests: Playwright journeys, contract (Schemathesis), k6 load, security (ZAP, abuse suite), and the mocks server (`e2e/mocks`).

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 08-testing.md; 07-work-packages.md (scenario owners).

## Run

- `make e2e`, `make load`, `make security`
- The Wave 2 journeys alone, from a fresh `make reset`: `scripts/no-egress.sh make gen e2e-journeys`
- Mock SMS/push UI: https://mocks.todayz.localhost

## Rules for this folder

- The default suite needs no real provider keys and cannot reach the internet (SC-PL-04).
- Never store an OTP, code, token or phone in a snapshot or report.
- Each WP owns `scenarios/<AREA>.md`; QA evidence goes in `docs/qa/<wp-id>/`.

## Layout

```
contract/     Schemathesis (W0-04): run.sh smoke|full, hooks.py (the todayz_problem check: every
              error is a registered problem code, SC-PL-02), test_hooks.py, requirements.in/.txt
              (uv-compiled, hash-pinned, D-88). The venv lives in .tools/schemathesis (make setup).
              out/ (reports) is git-ignored.
fixtures/     @todayz/e2e-fixtures (W2-13): seed.ts (seed ids, slugs, prices, test people),
              api.ts (JSON client, ULID, the POS counter-code TOTP), principals.ts (sign-in once
              per run through the mocks' OTP, session kept in a 0600 temp file; an admin's
              `stepUp()` does the SMS step-up through the mocks too and reuses it for 8 min,
              D-264), journey.ts (Shop, Customer, clock and browser helpers, `test` with `who`,
              `passStepUpDialog` for the admin console's dialog). admins.yaml holds test admins
              (reserved phones and roles, no secrets).
journeys/     @todayz/e2e-journeys (W2-13): Playwright multi-context journeys, specs/NN-e2e-XX-*.
              globalSetup opens SL and SL2 all day and globalTeardown restores their hours and
              resets the test clock. Specs that move the clock run last (20-*, 21-*).
a11y/         @todayz/e2e-a11y (W4-04): routes.ts (every route of the four apps), harness.ts (context per
              person, English or hi/kn/te/ta, the seed's place and shop), a11y.spec.ts (axe WCAG 2.2 AA
              at 360, 390 and 1280 px, no sideways scroll, the phone again in hi/kn/te/ta),
              lighthouse.mjs (the three app shells; the site has `site/scripts/lighthouse.mjs`).
              out/ (findings, Lighthouse reports) is git-ignored.
visual/       @todayz/e2e-visual (W4-04): visual.spec.ts and baselines/ (key screens in en and the
              en-XA pseudo-locale). Refresh on purpose with `TZ_VISUAL_APP=<app> ... --update-snapshots`
              and say which baselines changed in the WP report. `make e2e-a11y e2e-visual` run both.
```

## Conventions

- Journeys print step names, routes, statuses and problem codes only: never a body, an OTP, a
  pickup or counter code, a bill token or a phone. Admin steps go through the admin API (D-83).
  A step that needs unbuilt work is a `test.fixme` with a one-line reason.
- `make check` runs `run.sh smoke` (examples + coverage phases) from `backend/cmd/todayz`'s
  `TestContractSmoke` against an in-process api on a fresh database, plus the hooks' unit tests.
  `make e2e` runs `run.sh full` (examples, coverage, fuzzing) against the running stack through
  Caddy with its local root certificate; `POST /v1/test/clock` is excluded so fuzzing never moves
  the stack's clock.
- `make e2e` brings the stack up itself (`e2e-stack-up` runs the idempotent `make dev` before
  `e2e-contract`, D-100 D6), because the stack smoke before it (`e2e-stack`, `dev-smoke.sh`) ends
  with `make stop`. It passes from a stopped stack; only one stack runs per machine.
- `run.sh` refuses a base URL that is not `https?://<name>.todayz.localhost` or a loopback
  address (`localhost`, `127.0.0.1`, `[::1]`, optional port, no path), and any URL containing a
  backslash, whitespace or `@`: the fuzzer never points at a shared or public host (D-100).
  Hypothesis' example database goes under the report dir
  (`HYPOTHESIS_STORAGE_DIRECTORY`), never into the caller's working directory; `.hypothesis/` is
  git-ignored as a backstop.
- A new domain needs nothing here: Schemathesis reads the bundle. A new problem code goes in
  `backend/api/problems/<domain>.yaml`; the check refuses any other.
- The Python toolchain is pinned to the patch (D-100 L8): `make setup` installs uv
  `UV_VERSION` into `.tools/bin` through `scripts/install-uv.sh` (SHA-256 of the tarball and of
  the uv binary inside it per platform recorded there; the installed binary is compared with
  its sum on every setup and reinstalled on a mismatch; no package manager), the uv-managed
  CPython `CONTRACT_PYTHON` into `.tools/python`
  (never a system Python), and the hash-pinned wheels into `.tools/schemathesis`. Re-pin the
  Python dependencies with `make contract-lock` (network; `--exclude-newer` is the D-88 date in
  `mk/25-contract.mk`), then `make setup`.

## Status

W0-04 built `contract/`. W2-13 built `fixtures/` and `journeys/` (the Wave 2 gate, report in
`docs/qa/W2-13/`). Load (`e2e/load`, `make load`, W4-02) and security (`e2e/security`, `make security`, W4-03) are built.
