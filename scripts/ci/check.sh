#!/usr/bin/env bash
# check-ci (W0-06): the CI definitions' own checks, run by `make check` under
# scripts/no-egress.sh, so everything here works offline.
#
#   1. actionlint (the pinned ACTIONLINT_VERSION in .tools/bin) on every workflow
#   2. wflint (scripts/ci/wflint, built here from the Go module cache): the TodayZ
#      workflow policy: triggers, anchors, permissions, secrets, pins, checkout,
#      shell, timeouts, concurrency, canonical make targets, Terraform subcommands
#      across .github, scripts, mk, Makefile and infra, dependabot.yml
#   3. tracked-files.sh: no env files, keys, state or build output in `git ls-files`
#   4. gitleaks dir on the working tree (scripts/ci/gitleaks-dir.toml)
#   5. shellcheck on scripts/ci/*.sh, versions.env and paths.env
#   6. lighthouse.sh lint: Lighthouse results stay on the filesystem (D-58)
#   7. toolchain.sh check, and install-tools.sh --check: local tools against versions.env
#      (a warning on a laptop, where mk/00-repo.mk setup-tools installs brew's latest;
#      an error in CI)
#   8. selftest.sh: every check above catches a seeded violation
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$ci_root"

actionlint_bin="$ci_root/.tools/bin/actionlint"
[[ -x "$actionlint_bin" ]] || ci_die "actionlint $ACTIONLINT_VERSION is not in .tools/bin; run make setup"
have="$("$actionlint_bin" -version 2>/dev/null | head -1)"
[[ "$have" == "$ACTIONLINT_VERSION" ]] || ci_die "actionlint is $have, want $ACTIONLINT_VERSION; run make setup"
for t in shellcheck gitleaks git go; do command -v "$t" >/dev/null || ci_die "$t is missing; run make setup"; done

ci_group "actionlint $have"
"$actionlint_bin" -no-color
echo "ok: actionlint ($(find .github/workflows -maxdepth 1 -type f \( -name '*.yml' -o -name '*.yaml' \) | wc -l | tr -d ' ') workflows)"
ci_endgroup

ci_group "wflint (workflow policy)"
CI_WFLINT="$ci_root/.cache/ci/bin/wflint"
export CI_WFLINT
# -mod=readonly: go.sum must already cover every module (make setup downloads them).
(cd scripts/ci/wflint && go build -mod=readonly -trimpath -o "$CI_WFLINT" .)
"$CI_WFLINT" -root "$ci_root"
ci_endgroup

ci_group "tracked files"
scripts/ci/tracked-files.sh
ci_endgroup

ci_group "gitleaks (working tree)"
(cd scripts/ci && gitleaks dir "$ci_root" --config gitleaks-dir.toml --no-banner --redact --log-level warn)
echo "ok: gitleaks dir"
ci_endgroup

ci_group "shellcheck scripts/ci"
shellcheck -x scripts/ci/*.sh
shellcheck -s bash scripts/ci/versions.env scripts/ci/paths.env
echo "ok: shellcheck scripts/ci"
ci_endgroup

ci_group "lighthouse configs and scripts (D-58)"
scripts/ci/lighthouse.sh lint
ci_endgroup

ci_group "toolchain and tool pins"
scripts/ci/toolchain.sh check
scripts/ci/install-tools.sh --check gitleaks lefthook shellcheck caddy
ci_endgroup

ci_group "selftest (seeded violations)"
scripts/ci/selftest.sh
ci_endgroup

echo "ok: check-ci"
