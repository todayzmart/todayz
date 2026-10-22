# W0-01 Repo bootstrap: report

## Built

- Local git repo on `main` with no remote. A pre-push hook refuses every push (CLAUDE.md: no pushing).
- Root files:
  - `.gitignore`: env files, generated code, `routeTree.gen.ts`, tool caches, local stack data, Terraform state
  - `.editorconfig`, `.npmrc`
  - `.env.example`: names only, guarded by a test
  - `package.json`: pnpm 10.32, Node ≥ 22.12
  - `pnpm-workspace.yaml`
- `Makefile` with the 14 canonical targets (D-56) and no `make seed`.
  - Packages register steps from `mk/<area>.mk` on `*_STEPS` lists, so nobody edits the root file.
  - `check` and `e2e` run `gen` first, under `scripts/no-egress.sh`.
- `mk/repo.mk` (setup-tools, setup-keys, setup-node, setup-hooks, check-repo).
- `scripts/envtool` (Go, stdlib only, built to `.tools/bin/envtool`):
  - `generate`: the D-18 keys. Ed25519 JWK set with a kid, plus 32-byte random keys. Written atomically with mode 0600.
  - `check`: prints `NAME set|missing`. Fails if an app key is missing.
  - `leakscan`: finds a value raw, as base64 (four alphabets) or as hex, including the JWK `d` seed.
  - `egress-sink`: a proxy that refuses requests and logs only the host name.
- `scripts/no-egress.sh`:
  - Points every proxy-honouring client (Go, curl, Node via `NODE_USE_ENV_PROXY`, pnpm) at the sink.
  - Lets `*.localhost` / 127.0.0.1 / ::1 go direct.
  - Sets `GOPROXY=off`, `TODAYZ_NO_EGRESS=1` and `MOCK_LLM=1`.
  - Unsets every provider and cloud key, plus SDK default names (`ANTHROPIC_BASE_URL`…).
  - Fails the run if anything tried to reach a non-local host.
- Provider base URLs are named `*_API_URL` (`LLM_API_URL`, `STT_API_URL`…), so a value another tool exports under an SDK default name (Claude Code exports `ANTHROPIC_BASE_URL`) is never picked up.
- `lefthook.yml`:
  - pre-commit: gitleaks on staged changes, plus a guard that refuses env files, generated code and tfstate.
  - pre-push: always refuses.
- `.gitleaks.toml`: the default rules, with git-ignored local paths allowlisted.
- Per-folder `CLAUDE.md` stubs (backend, customer, business, admin, site, shared, infra, e2e) and `storefront/README.md`.
- ADRs 0001–0008 plus an index in `docs/adr/`.

## Tests (all run in `make check` → `check-repo`)

| Test | What it proves | Result |
|---|---|---|
| `scripts/envtool` unit tests (14) | Exact `generated: NAME` output; a second run is silent and leaves the file unchanged; existing keys are kept; the kid is consistent; Ed25519 x/d match; 32-byte keys are unique; check prints names only and sees process env; leakscan catches every form and never echoes a value; the sink logs only the host | pass (`-race`) |
| `makefile-targets.sh` | All 14 targets exist; no `seed`; CLAUDE.md and 02 §1.2 list each one; every registered step is a real target | pass |
| `sc-pl-03.sh` (SC-PL-03) | Fresh env file: setup prints exactly 9 `generated:` lines; second run prints nothing; env-check lines match `NAME (set\|missing)`; file mode is 600; leakscan over all output finds no value | pass |
| `sc-pl-04.sh` (SC-PL-04) | curl, Go and Node to public hosts are refused and reported; localhost goes direct; a failing command still fails; provider keys are unset inside | pass |
| `hooks.sh` | The guard refuses 9 forbidden paths and allows 4 normal ones; pushes are refused; `.env.example` holds no values | pass |
| shellcheck, gitleaks dir | Scripts are clean; no secrets in the tree | pass |

Mutation check: removing the proxy exports from `no-egress.sh` makes SC-PL-04 fail. The Go, Node and curl requests reach the internet and are flagged. Restored afterwards.

## Manual QA

- `make setup` on a repo with no `.env.local` printed 9 `generated:` lines. A second run printed none.
- Every canonical target runs. Targets with no steps yet print "no steps registered yet" and exit 0.
- A real `git commit` ran the lefthook pre-commit (gitleaks and the guard) and passed.
- `make env-check` showed the 9 app keys as `set`. No values were printed anywhere in this session.

## Known gaps

- The egress block works at the proxy level. A client that ignores proxy env, such as a raw socket, is not caught by the wrapper alone. W0-03 adds a dial-level guard in the backend's outbound HTTP client when `TODAYZ_NO_EGRESS=1`. ADR 0008 records this.
- SC-PL-03 must also scan `make dev` start-up output. W0-02 adds that; the script already accepts extra log files as arguments.
- tflint is not in Homebrew. W0-06 runs it from its Docker image.
