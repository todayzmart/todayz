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

## Layout

```
contract/     Schemathesis (W0-04): run.sh smoke|full, hooks.py (the todayz_problem check: every
              error is a registered problem code, SC-PL-02), test_hooks.py, requirements.in/.txt
              (uv-compiled, hash-pinned, D-88). The venv lives in .tools/schemathesis (make setup).
              out/ (reports) is git-ignored.
```

## Conventions

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

W0-04 built `contract/`. Playwright journeys, load, security and the mocks server come with their
WPs.
