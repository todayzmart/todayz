# W0-06 CI definitions: QA report

- **Branch:** `wp/W0-06-ci`, rebased onto main `e7cf216`
- **Date:** 2026-10-22
- **Scope:** `.github/workflows/**`, `.github/actions/**`, `scripts/ci/**`, `mk/90-ci.mk`
- Nothing ran on GitHub: there is no remote, by rule. Every run was local (macOS host, and `act` on linux/arm64 under Colima).
- Built by the W0-06 agent. The lead saved this report.

## What was built

- **Workflows**
  - `ci.yml`: `make setup`, then `make check`. Uploads `.cache/ci`, `test-results` and coverage when it fails.
  - `e2e.yml`: `make setup`, Playwright system libraries, `runner-prep.sh`, `make e2e`, then `make stop` (always). Uploads `docs/qa`, `test-results`, `.cache/dev/smoke` and the reports.
  - `lighthouse.yml`: runs a registered Lighthouse step once one exists. Until then it prints "no Lighthouse step registered yet" and lints any lighthouserc for filesystem-only output (D-58).
  - `security.yml`: gitleaks (full history), Trivy (filesystem), checkov (`infra/`). The `dynamic` job (`make dev`, `make security`, ZAP baseline) runs only on schedule or dispatch.
  - `terraform.yml`: `fmt -check`, `validate` (`init -backend=false`), tflint and checkov from digest-pinned images. It never runs plan or apply (D-16), and it skips with a message while `infra/terraform` has no `.tf` files.
- **Composite action `setup-toolchain`:** Go from `go.mod`, Node 24 LTS, pnpm from `packageManager`, and pinned CI tools in `.tools/bin` (actionlint, gitleaks, lefthook, shellcheck, caddy).
- **Workflow policy**, enforced offline by `make check` (`check-ci`):
  - `permissions: contents: read`
  - actions pinned by full SHA with a `# vX.Y.Z` comment; Docker images pinned by digest
  - a concurrency group and a timeout on every job
  - `persist-credentials: false`
  - no `pull_request_target`, no Terraform plan or apply
  - no secrets apart from commented Wave 5 placeholders
  - only canonical make targets
- **`mk/90-ci.mk`:** `SETUP_STEPS += setup-ci` (actionlint v1.7.12). `CHECK_STEPS += check-ci` runs the actionlint version check, actionlint, shellcheck, lint-workflows, the Lighthouse config lint, the toolchain check and the selftest.

## Decisions

- **Node 24 LTS in CI** (`NODE_CI_VERSION=24`). `engines.node` stays `>=22.12`. `toolchain.sh check` fails if the CI pin leaves that range, and a `.node-version` file would take precedence.
- **Stack images** are pulled during `make setup`, before `make check` and `make e2e` run under no-egress (compose uses `--pull never`).
- **Linux runner prep** (`runner-prep.sh`): Caddy gets `cap_net_bind_service` through setcap instead of running as root, and `TODAYZ_CADDY_ROOT_CERT` points at Caddy's Linux CA path.

## Evidence

- **Policy lint and selftest** (`make check`, step `check-ci`):

  | Check | Result |
  |---|---|
  | actionlint | 5 workflows, clean |
  | shellcheck | `scripts/ci`, clean |
  | lint-workflows | 18 files clean on 7 rules |
  | selftest | 33 passed, 0 failed |

  The selftest covers:
  - 15 bad workflow fixtures, each caught under the right rule;
  - the action and script fixtures;
  - the actionlint, shellcheck and Lighthouse fixtures;
  - 10 mutations of the real workflows, all caught;
  - the repo itself linting clean.
- **`make check` on the macOS host:** rc 0.
- **act** (nektos/act, linux/arm64, on a clone of HEAD):
  - `act -l` lists 8 jobs.
  - `act pull_request -n`: every PR job succeeds (dry run).
  - `ci.yml` check: Job succeeded. `make setup` took 4m13s and `make check` 1m02s.
    - This ran with `sc-pl-03.sh` patched in the act clone only.
    - Without the patch, check-repo failed on Linux, because of the macOS-only `stat -f %Lp`.
    - The lead has since fixed it on main in `a1731d3` (GNU stat first, BSD stat as the fallback).
  - `terraform.yml` static: Job succeeded (there are no `.tf` files yet, so it skips with a message).
  - `lighthouse.yml`: Job succeeded (no step yet; config lint ok).
- **Tool runs on the host** (images pinned by digest):

  | Tool | Result |
  |---|---|
  | terraform.sh on the fixture module | fmt ok, validate "Success!", tflint ok, checkov ok |
  | gitleaks | no leaks |
  | Trivy | 0 vulnerabilities |
  | ZAP baseline vs a scratch server | PASS 60, WARN-NEW 7, FAIL 0 |

## Not run, and why

- **`e2e.yml` and the security `dynamic` job under act:** their stack binds the fixed host ports other agents were using on the shared Docker daemon. Their steps (`make e2e`, `make security`) are canonical targets that are tested locally.
- **Nothing ran on GitHub** (no remote, by rule).

## Open items and lead decisions

1. **`sc-pl-03.sh` portable stat:** fixed on main (`a1731d3`).
2. **`infra/local/lib.sh` Caddy CA default per OS:** fixed on main (`a1731d3`). dev-smoke's LAN check (`ipconfig`) is macOS-only and is skipped on Linux; that is acceptable.
3. **`mk/00-repo.mk` setup-tools is brew-only:** CI is unaffected, because setup-toolchain installs the tools. It stays as is.
4. **Register a Lighthouse step** (name contains "lighthouse" or "lhci"): W4 e2e work.
5. **Add a ZAP step or `e2e/security/zap-baseline.conf`:** the security WP.
6. **Terraform static checks in `make check`** once `infra/terraform` has `.tf` files: the infra WP.
7. **`.node-version`:** not added; local Node 25 stays allowed.
8. **tflint plugin downloads without `GITHUB_TOKEN`:** acceptable until Wave 5.

## Review fixes (round 1)

The branch is rebased onto main `6ee56d8`, and the fixes are in commit "W0-06: review fixes".

**Round 0 is superseded.** "What was built" and "Evidence" above describe round 0. Since then:
- `terraform.yml` and `lighthouse.yml` are removed.
- `lint-workflows.sh` is replaced by `scripts/ci/wflint` (Go, yaml.v3).
- Terraform static checks run in `make check` (`check-tf-static`).
- Lighthouse runs inside `make e2e`.
- Scanners run as `make security` steps.
- Open items 4–6 above are resolved by S1 and S2.

**How the fixtures are checked.** Every fix has a fixture in `scripts/ci/selftest.sh` (run by `check-ci`). A fixture passes only if:
- the expected exit code (wflint: 1) and the `[rule]` tag both appear; a 127 does not count;
- no other noise tag appears;
- every sed or awk mutation actually changed the file (`cmp`).

### B1: secrets in history

| Item | Status | Fixture |
|---|---|---|
| `.gitleaks.toml` has no local-path allowlist; it is used for `gitleaks git` | fixed | selftest B1: a scratch repo commits a fake `ghp_` token in `.env.local`; `gitleaks git` with `.gitleaks.toml` reports it (rc 1) |
| The working-tree allowlist moves to `scripts/ci/gitleaks-dir.toml` (`[extend] path = "../../.gitleaks.toml"`) and is used only for `gitleaks dir` in check-repo and check-ci | fixed | The same scratch repo scanned with `gitleaks-dir.toml` gives rc 0. This proves the allowlist hides git history, so it must stay dir-only |
| check-ci fails when `git ls-files` matches a forbidden path (`scripts/ci/tracked-files.sh`, which sources the case list from `scripts/hooks/forbidden-files.sh` instead of copying it) | fixed | selftest refuses tracked `.env.local`, `web/.env.local`, `.env.production.local`, `x.pem`, `certs/x.key`, `.cache/f`, `dist/f` and `a.tfstate`; a clean repo passes |
| In CI, gitleaks fails (no fallback to dir) when `.git` is missing or the clone is shallow | fixed | selftest: "no git repository" and "shallow clone" |
| On `pull_request`, the scan uses the base branch's `.gitleaks.toml` (`git show origin/$BASE_REF:.gitleaks.toml`, with BASE_REF passed through env and validated) | fixed | selftest: a PR commits a loosened config (`testdata/gitleaks/loosened.toml`) plus `.env.local`. A push scan says "history clean" (the hole). The PR scan reports "leaks found". `BASE_REF=main..x` is refused |

### Linter (`scripts/ci/wflint`)

wflint builds offline after `make setup` (`go build -mod=readonly`). Exit codes are 0 clean, 1 findings and 2 policy or usage error. It prints "linted N files". actionlint stays.

| Rule | Status | Fixtures (`scripts/ci/testdata/`) |
|---|---|---|
| Triggers: only pull_request, push, workflow_dispatch and schedule are allowed | fixed | `bad-trigger-*` (pull_request_target, workflow_run, flow mapping, escaped `_`/`\x5f`, complex key); mutation on e2e.yml |
| Anchors and aliases are rejected anywhere | fixed | `bad-anchor-permissions`, `bad-anchor-permission-values`, `bad-anchor-uses-alias` |
| permissions: a mapping at workflow and job level, values read or none only (write-all, read-all and any write rejected in any spelling); `id-token-allow.txt` is the commented Wave 5 hook and is empty | fixed | 17 `bad-permissions-*` (quoted, capitalised, flow, folded scalar, id-token, job missing); mutations on ci.yml |
| Secrets: any `secrets` in `${{ }}` fails, including toJSON, run blocks, comments and `secrets: inherit` | fixed | 15 `bad-secrets-*`; mutation on ci.yml |
| uses: remote actions pinned by a 40-hex SHA with `# vX.Y.Z`; `docker://` and container/services images pinned by `@sha256:`; `./` only under `.github/actions/`; owners from `allowed-owners.txt` | fixed | `bad-pin-*` (tag, short SHA, zero SHA, missing comment, quoted key, flow mapping, reusable remote, container string, services), `bad-uses-*`, `actions/bad-pin*`, `bad-uses-local-outside`; mutations on ci.yml, security.yml and setup-toolchain |
| actions/checkout sets `persist-credentials: false` | fixed | `bad-checkout-missing-persist`, `bad-checkout-persist-true`, `actions/bad-checkout-persist`; mutation |
| `defaults: run: shell: bash` in every workflow | fixed | `bad-shell-no-defaults`, `bad-shell-step-sh`, `actions/bad-shell-missing`; mutation |
| timeout-minutes on every job except reusable-workflow calls | fixed | `bad-timeout-*` (4-space indent, flow job, step level, not a number, first of two); `bad-uses-reusable-call` must not report `[timeout]`; mutation |
| A concurrency group that includes `${{ github.event_name }}` | fixed | `bad-concurrency-missing`, `bad-concurrency-no-event-name`; two mutations |
| make calls: `\` continuations joined, options skipped, quotes stripped, canonical targets only, `${{` in an argument fails, `bash -c`/`sh -c` checked | fixed | 20 `bad-make-*` (alias, bash -c, sh -lc, continuation, env prefix, eval, substitution, MAKEFLAGS, matrix expression, multi target, `-C`, `-f`, quoted target, variable override, internal step); mutation `make check-ci` |
| `m=make; $m x` | documented as out of reach | Noted in `scripts/ci/wflint/shell.go` |
| Terraform: only fmt, validate, `init -backend=false`, version and `providers lock`, including through variables, quoting, aliases and for-loop word lists; the scan covers `.github/**`, `scripts/**`, `mk/**`, `Makefile` and `infra/**` | fixed | `bad-forbidden-*` workflows and scripts (concat, for alias, function alias, image var, make var, passthrough, chdir apply, providers mirror, init without `-backend=false`); 15 generated subcommands; `good-terraform.sh` stays clean |
| Discovery fails closed: a missing dir or 0 files is an error; "linted N files" is printed; the selftest asserts each known file | fixed | selftest discovery: the repo is clean, `-v` lists the 12 known files, a missing dir gives `[discovery]`, an empty workflows dir gives "no workflow files found", and no Makefile gives exit 2 |
| YAML hygiene | fixed (extra) | `bad-yaml-duplicate-key`, `bad-yaml-explicit-tag`, `bad-yaml-two-documents`, `bad-structure-unknown-key`, `bad-runner-self-hosted` |
| Selftest rules: `expect_fail` needs rc 1 plus the tag; every mutation is checked with `cmp`; every bypass from both reviews is a fixture; a 4-space workflow fixture | fixed | `good-4space.yml`, `bad-timeout-4space.yml`; 17 cmp-checked mutations |
| zizmor | not done (optional) | none |

### Structure (D-56)

**S1: fixed.**
- `mk/90-ci.mk` registers `SECURITY_STEPS += security-gitleaks security-trivy security-checkov security-zap` and `CHECK_STEPS += check-tf-static`.
- check-tf-static runs fmt, validate, tflint and checkov, and skips with a message while there are no `.tf` files.
- security-zap runs `scan.sh zap --if-up` and prints a skip message when the stack does not answer.
- `security.yml` has two jobs: `static` (make setup, make security) and `dynamic` (schedule or dispatch only: make setup, make dev, make security, make stop).
- `terraform.yml` is removed.
- Trivy gates on HIGH,CRITICAL with `--ignore-unfixed`; the schedule adds a report-only pass without it.
- Fixture: the canonical-target check in wflint covers both workflows, and `make check` runs check-tf-static, which skips.

**S2: fixed.**
- `lighthouse.yml` and lighthouse.sh `run` are removed. Lighthouse belongs to `make e2e`.
- The D-58 lint in check-ci also greps package.json scripts for any `--upload.target` other than filesystem.
- Fixtures: `testdata/lighthouse/bad-pkg` (fails) and `good-pkg` (passes).

**S3: fixed.**
- Reports go to the non-hidden, git-ignored `ci-reports/`.
- One path list lives in `scripts/ci/paths.env`, used by the `upload-reports` composite action.
- There is no `include-hidden-files` and no root `**/` glob, and no whole `docs/qa/**` upload.
- Fixture: wflint rejects a `./` action outside `.github/actions/`, and `paths.env` is shellchecked.

**S4: fixed.**
- `runner-prep.sh` runs setcap only when `GITHUB_ACTIONS=true` and `RUNNER_ENVIRONMENT=github-hosted`.
- The target must be exactly `$root/.tools/bin/caddy`, a regular file and not a symlink, and `caddy version` must match `CADDY_VERSION`.
- `ci_github_env` and `ci_github_path` reject newlines; install-tools.sh adds `.tools/bin` through `ci_github_path`.
- playwright-install.sh runs sudo apt only when `CI=true`.
- The duplicate CA export is dropped (main's `infra/local/lib.sh` sets the CA per OS).
- Fixtures, all in selftest guards:
  - an outside path, a symlink and a wrong version are each refused, and a good binary is accepted;
  - GITHUB_ENV and GITHUB_PATH newlines are refused and nothing is written.

**S5: fixed.**
- ZAP is pinned to `zaproxy/zap-stable:20261008@sha256:7aaa659b…`, created 2026-10-08 (D-88).
- It targets the Caddy hosts (`https://*.todayz.localhost`, using `--add-host` and the Caddy CA mounted read-only).
- Only loopback or `*.todayz.localhost` targets are accepted.
- It runs as `--user uid:gid` with `-z "-dir /tmp/zap-home"`, because that user has no home in the image. There is no chmod 0777.
- `e2e/security/zap-baseline.conf` triages the current WARNs.
- Fixtures:
  - selftest: zap-target-ok refuses `example.com`, `api.todayz.localhost.example.com`, `todayz.localhost@example.com`, `10.0.0.1` and `todayz.local`, and accepts `api.todayz.localhost`, `127.0.0.1:58080` and `[::1]:5301`;
  - a ZAP smoke against a scratch python server on :58080 applied the conf: 10036 became FAIL-NEW, there were 3 WARN-NEW and 57 PASS, and the report files are owned by the calling user. With no target answering, it dies with "no target answered".

**S6: fixed.**
- A PATH tool at a different version means the pinned build is installed into `.tools/bin`: an error in CI, a warning on macOS.
- go_tool sets `GOSUMDB=sum.golang.org` and unsets GONOSUMDB, GONOSUMCHECK, GOPRIVATE and GOINSECURE.
- curl uses `--proto '=https' --tlsv1.2`.
- A temp dir trap cleans up on every failure.
- actionlint is installed in one place (install-tools.sh).
- `.tools/bin` is cached, keyed on `hashFiles('scripts/ci/versions.env')`.
- Fixture: check-ci runs `install-tools.sh --check`. The local brew lefthook 2.2.2 against the pinned 2.1.16 shows the warning.
- Found by act and fixed: when gitleaks and caddy are built with `go install`, they report "version is set by build process" and "unknown", so the post-install check failed on the runner. install-tools.sh now sets each tool's version ldflag:
  - gitleaks: `-X github.com/zricethezav/gitleaks/v8/version.Version`;
  - caddy: `-X github.com/caddyserver/caddy/v2.CustomVersion`.
- Checked on the host: the real builds report v8.30.1 and v2.11.7.
- Fixtures in selftest:
  - a stub `go` builds fake binaries that print the ldflag value;
  - install-tools passes with the ldflags;
  - with the gitleaks ldflag removed (cmp-checked), install-tools fails in CI with "versions.env pins".

**S7: fixed.**
- terraform.sh `tf()` refuses plan, apply, destroy, import, state, taint, refresh and force-unlock (and anything outside the allow-list) unless `TODAYZ_WAVE5=1`.
- Fixture: selftest runs `terraform.sh apply`, `plan`, `state list`, `destroy` and `-chdir=… import` with TODAYZ_WAVE5 unset, and each is refused.

**S8: fixed.**
- `.github/dependabot.yml` covers:
  - github-actions (`/` and `/.github/actions/*`);
  - docker and docker-compose;
  - gomod (backend, backend/tools, e2e/mocks, scripts/envtool, scripts/ci/wflint);
  - npm.
- All are weekly with `cooldown: default-days: 14`.
- No CODEOWNERS file was added.
- Fixtures: `testdata/dependabot/bad-*` (cooldown, daily, gomod dir uncovered, no npm), plus a mutation to cooldown 7.

### Nits

- **act.sh:** fixed. It always clones HEAD into `.cache/act/src` (ACT_FROM_WORKTREE is gone), and `scripts/ci/act.sh clean` removes `.cache/act`.
- **Unpinned brew tools:** noted. `mk/00-repo.mk` setup-tools installs the latest brew versions (unpinned). check-ci runs `install-tools.sh --check`, which warns on macOS when a local version differs from `versions.env` (today: lefthook 2.2.2 against 2.1.16) and errors in CI.

### Residual risks and caveats

- **`gitleaks:allow`:** an inline `gitleaks:allow` comment still suppresses a finding. On a pull request, a second pass with `--ignore-gitleaks-allow --exit-code 0` writes `ci-reports/gitleaks/pr-allow-comments.sarif` for the reviewer. It is report-only, because test sentinels use the comment.
- **ZAP and the Caddy CA:** the CA is mounted, but ZAP does not verify upstream certificates by default. The mount is for parity, not a trust check.
- **Dependabot coverage:** dependabot does not see the images and tools in `scripts/ci/versions.env`. Those are bumped by hand under the 14-day rule (D-88).
- **wflint's reach:** wflint follows make and terraform through literals, quoting, assignments, functions and for-loop word lists. It cannot follow `m=make; $m x` or other run-time indirection; reviewers own those.

### Verification

| Check | Result |
|---|---|
| `scripts/ci/selftest.sh` under bash 3.2 (`/bin/bash` first on PATH) | 226 ok, 0 failed |
| `make check` (runs under `scripts/no-egress.sh`) | rc 0, including check-repo, check-stack, check-mocks, check-frontend, check-ci and check-tf-static (skip) |
| wflint on the repo | 55 files, rc 0 |
| actionlint | rc 0 |
| shellcheck | rc 0 |
| act `ci.yml` check job (pull_request, linux/arm64, clone of HEAD) | Job succeeded. Install pinned CI tools 3m22s, make setup 5m14s, make check 1m29s. wflint linted 55 files. Selftest 226 ok, 0 failed. The first run failed on the gitleaks version (fixed under S6) |
| act on `e2e.yml` and on the security `dynamic` job | not run, by instruction (main's stack is running) |

### Repo split checklist (lead adds to 09)

1. **CODEOWNERS** (added at the split, not now) for:
   - `.github/**`, `scripts/ci/**` and `mk/90-ci.mk`;
   - the scanner configs (`.gitleaks.toml`, `scripts/ci/gitleaks-dir.toml`, `.gitleaks*`, `e2e/security/**`);
   - `scripts/ci/wflint/allowed-owners.txt` and `id-token-allow.txt`.
2. **Branch protection on main:**
   - at least one required review, with code-owner review;
   - stale approvals dismissed;
   - no force push.
3. **Required checks:** `ci / check`, `e2e`, `security / static`.
4. **Outside collaborators:** in Actions settings, set "Require approval for all outside collaborators".
5. **Runners:** GitHub-hosted only. wflint rejects `self-hosted`, and runner-prep's setcap needs `RUNNER_ENVIRONMENT=github-hosted`.
6. **Wave 5 OIDC:**
   - the AWS role trust policy scopes `sub` to `repo:<org>/<repo>:environment:production`;
   - `id-token: write` is allowed only in deploy jobs listed in `scripts/ci/wflint/id-token-allow.txt` (empty until then);
   - those jobs never run `make setup` or any repo script that installs tools.

## Lead verification (2026-10-22)

Rebased onto main a0da8c8 (W0-03 merged, plan aligned).

- **First `make check` failed in the toolchain step:** "go 1.26.0 is older than the repo's go 1.26.10". `toolchain.sh` ran `go env GOVERSION` at the repo root. The root has no go.mod, so `GOTOOLCHAIN=auto` never switched and reported the host Go.
- **Lead fix 35e4969:**
  - `go_have` asks Go inside every module (the `go_mods` list) and keeps the lowest version.
  - A module that Go cannot load fails the check.
- **What the fix exposed:** `scripts/envtool/go.mod` and `scripts/ci/wflint/go.mod` lacked the D-88 `toolchain go1.26.10` pin. Both are pinned now.
- **wflint `go.sum`:** gains the `gopkg.in/check.v1 …/go.mod` hash, which yaml.v3's go.mod requires.
- **`make check`:** exit 0.
  - toolchain: "ok: go 1.26.10 (repo wants >= 1.26.10)".
  - selftest: 226 ok, 0 failed.
  - wflint: 56 files linted.
- **`make e2e`:** exit 0. The stack came up healthy, and frontend smoke passed for all four apps: 5 + 7 + 4 + 3 tests.
  - No Lighthouse step is registered yet; that is W4 e2e work (open item 4).
  - `make e2e` used this checkout's stack; main's stack was stopped first (S5).
- **Residual:**
  - zizmor is not run.
  - The brew lefthook is 2.2.2 against the pinned 2.1.16, so it warns locally.
  - The repo split checklist goes to `docs/plan/09` §4.
