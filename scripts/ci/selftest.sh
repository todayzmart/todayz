#!/usr/bin/env bash
# Proves every CI check catches what it claims to (W0-06). Run by check.sh in `make check`.
#
#   1. wflint fixtures (scripts/ci/testdata): each bad-<rule>-* file must exit 1 and
#      report [<rule>], without unrelated noise; good fixtures must pass. Every rule tag
#      has at least one fixture.
#   2. Generated Terraform cases: every cloud subcommand is refused.
#   3. Discovery: the real repo lints clean and lists every file it guards; a missing
#      folder or an empty workflows folder is a [discovery] finding.
#   4. actionlint, shellcheck and the Lighthouse D-58 lint each catch a seeded file.
#   5. B1: a token committed in .env.local is found by `gitleaks git` with the repo's
#      .gitleaks.toml, and missed with the working-tree allowlist (why that one is dir-only).
#   6. tracked-files.sh refuses each forbidden path committed in a scratch repo.
#   7. Script guards: terraform.sh refuses cloud subcommands, $GITHUB_ENV/$GITHUB_PATH
#      refuse newlines, runner-prep refuses a wrong or symlinked Caddy, ZAP refuses
#      non-local targets.
#   8. Mutations: temp copies of the real workflows, action and dependabot.yml, each
#      broken one way (cmp proves the edit happened), must fail with the matching rule.
#
# Offline, bash 3.2: needs git, gitleaks, actionlint, shellcheck and the wflint binary
# check.sh builds (CI_WFLINT).
#
# shellcheck disable=SC2016 # single quotes keep ${{ }}, $1 and $X literal on purpose
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

td="$ci_root/scripts/ci/testdata"
wf="$ci_root/.github/workflows"
wflint="${CI_WFLINT:-$ci_root/.cache/ci/bin/wflint}"
[[ -x "$wflint" ]] || ci_die "selftest: $wflint is missing; check.sh builds it"
actionlint_bin="$ci_root/.tools/bin/actionlint"
for t in git gitleaks shellcheck; do command -v "$t" >/dev/null || ci_die "selftest: $t is missing; run make setup"; done
tmp="$(mktemp -d "${TMPDIR:-/tmp}/todayz-ci-selftest.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
# The same spelling scripts get for their own root (`cd && pwd`), so exact path checks
# such as runner-prep's compare like with like (TMPDIR may end in a slash).
tmp="$(cd "$tmp" && pwd)"
# The word is assembled at run time so this file never holds a literal cloud command.
tfc="$(printf 'terra%s' form)"

pass=0 fail=0
ok() { pass=$((pass + 1)); printf 'ok   %s\n' "$*"; }
bad() { fail=$((fail + 1)); printf 'FAIL %s\n' "$*"; }
show() { sed 's/^/     /' "$tmp/out" | head -12; }

# Tags a fixture may only report when they are the rule under test: a missing default
# here would hide the rule a fixture is meant to prove.
noise="shell concurrency permissions timeout runner"

# expect_rule <label> <rule> <wflint args...>: exit code exactly 1 (a 127 or a crash
# does not count) and [rule] in the output, with no noise tags besides it.
expect_rule() {
  local label="$1" rule="$2" rc=0 n extra=""; shift 2
  "$wflint" -root "$ci_root" "$@" >"$tmp/out" 2>&1 || rc=$?
  if [[ $rc -ne 1 ]] || ! grep -qF "[$rule]" "$tmp/out"; then
    bad "$label: expected exit 1 with [$rule], got exit $rc"; show; return 0
  fi
  for n in $noise; do
    [[ "$n" == "$rule" ]] && continue
    # Anchor fixtures merge whole blocks, so their permissions findings are expected.
    [[ "$rule" == anchor && "$n" == permissions ]] && continue
    if grep -qF "[$n]" "$tmp/out"; then extra="$extra $n"; fi
  done
  if [[ -n "$extra" ]]; then bad "$label: [$rule] caught, but also unrelated [${extra# }]"; show; return 0; fi
  ok "$label -> [$rule]"
}

# expect_no_rule <label> <rule> <file>: [rule] must not appear.
expect_no_rule() {
  local label="$1" rule="$2"; shift 2
  "$wflint" -root "$ci_root" "$@" >"$tmp/out" 2>&1 || true
  if grep -qF "[$rule]" "$tmp/out"; then bad "$label: unexpected [$rule]"; show; else ok "$label -> no [$rule]"; fi
}

expect_clean() { # expect_clean <label> <wflint args...>
  local label="$1" rc=0; shift
  "$wflint" -root "$ci_root" "$@" >"$tmp/out" 2>&1 || rc=$?
  if [[ $rc -eq 0 ]]; then ok "$label -> clean"; else bad "$label: expected clean, exit $rc"; show; fi
}

# expect_cmd <label> <want-rc> <grep-text> <cmd...>: exact exit code and output text.
expect_cmd() {
  local label="$1" want="$2" text="$3" rc=0; shift 3
  "$@" >"$tmp/out" 2>&1 || rc=$?
  if [[ $rc -eq "$want" ]] && { [[ -z "$text" ]] || grep -qF -- "$text" "$tmp/out"; }; then
    ok "$label -> exit $rc${text:+, \"$text\"}"
  else
    bad "$label: expected exit $want${text:+ with \"$text\"}, got exit $rc"; show
  fi
}

tag_of() { local b="${1#bad-}"; b="${b%%-*}"; printf '%s\n' "${b%%.*}"; }
seen=" "

echo "--- 1. wflint fixtures (scripts/ci/testdata)"
expect_clean "workflows/good.yml" "$td/workflows/good.yml"
expect_clean "workflows/good-4space.yml" "$td/workflows/good-4space.yml"
expect_clean "actions/good" "$td/actions/good/action.yml"
expect_clean "dependabot/good" "$td/dependabot/good/dependabot.yml"
expect_clean "scripts/good-terraform.sh" "$td/scripts/good-terraform.sh"
for f in "$td"/workflows/bad-*.yml; do
  n="$(basename "$f")"; r="$(tag_of "$n")"; seen="$seen$r "
  expect_rule "workflows/$n" "$r" "$f"
done
for d in "$td"/actions/bad-*/; do
  n="$(basename "$d")"; r="$(tag_of "$n")"; seen="$seen$r "
  expect_rule "actions/$n" "$r" "$d/action.yml"
done
for d in "$td"/dependabot/bad-*/; do
  n="$(basename "$d")"; r="$(tag_of "$n")"; seen="$seen$r "
  expect_rule "dependabot/$n" "$r" "$d/dependabot.yml"
done
for f in "$td"/scripts/bad-*; do
  n="$(basename "$f")"; r="$(tag_of "$n")"; seen="$seen$r "
  expect_rule "scripts/$n" "$r" "$f"
done
expect_no_rule "workflows/bad-uses-reusable-call.yml (a call job needs no timeout)" timeout "$td/workflows/bad-uses-reusable-call.yml"
# discovery is proven in section 3; every other tag needs a fixture.
for r in trigger anchor yaml structure permissions secrets pin uses checkout shell timeout runner concurrency make forbidden dependabot; do
  case "$seen" in *" $r "*) ;; *) bad "no fixture for wflint rule [$r]" ;; esac
done

echo "--- 2. Terraform subcommands (generated)"
for sub in plan apply destroy import refresh "state list" taint untaint force-unlock "workspace new x" output console login test; do
  printf '#!/usr/bin/env bash\n%s -chdir=infra/terraform %s\n' "$tfc" "$sub" >"$tmp/tf.sh"
  expect_rule "$tfc $sub" forbidden "$tmp/tf.sh"
done
printf '#!/usr/bin/env bash\nX=%s; "$X" "%s"\n' "$tfc" apply >"$tmp/tf.sh"
expect_rule "$tfc through a variable, quoted subcommand" forbidden "$tmp/tf.sh"

echo "--- 3. discovery"
rc=0; "$wflint" -root "$ci_root" -v >"$tmp/disc" 2>&1 || rc=$?
if [[ $rc -eq 0 ]]; then ok "the repo lints clean"; else bad "the repo has wflint findings (exit $rc)"; sed 's/^/     /' "$tmp/disc"; fi
for f in .github/workflows/ci.yml .github/workflows/e2e.yml .github/workflows/security.yml \
  .github/actions/setup-toolchain/action.yml .github/actions/upload-reports/action.yml \
  .github/dependabot.yml Makefile mk/90-ci.mk scripts/ci/terraform.sh scripts/ci/scan.sh \
  scripts/ci/versions.env infra/local/stack.sh; do
  if grep -qE "[[:space:]]$f\$" "$tmp/disc"; then ok "discovery lists $f"; else bad "discovery does not list $f"; fi
done
if grep -qE '^wflint: linted [0-9]+ files' "$tmp/disc"; then ok "discovery prints the file count"; else bad "no 'linted N files' line"; fi
# Scratch roots with a valid policy (Makefile CANONICAL, the wflint allow-lists) and
# missing pieces. A broken policy is a usage error (exit 2), never a pass.
policy_root() { # policy_root <dir>
  mkdir -p "$1/scripts/ci/wflint"
  grep -E '^CANONICAL[[:space:]]*:?=' "$ci_root/Makefile" >"$1/Makefile"
  cp "$ci_root/scripts/ci/wflint/allowed-owners.txt" "$ci_root/scripts/ci/wflint/id-token-allow.txt" "$1/scripts/ci/wflint/"
}
mkdir -p "$tmp/no-makefile"
expect_cmd "a root with no Makefile (fails closed)" 2 "root Makefile" "$wflint" -root "$tmp/no-makefile"
policy_root "$tmp/missing-dirs"
expect_cmd "a root with no .github, mk or infra" 1 "[discovery]" "$wflint" -root "$tmp/missing-dirs"
policy_root "$tmp/nowf"
mkdir -p "$tmp/nowf/.github/workflows" "$tmp/nowf/.github/actions/a" "$tmp/nowf/mk" "$tmp/nowf/infra"
cp "$td/actions/good/action.yml" "$tmp/nowf/.github/actions/a/action.yml"
cp "$td/dependabot/good/dependabot.yml" "$tmp/nowf/.github/dependabot.yml"
expect_cmd "an empty .github/workflows" 1 "no workflow files found" "$wflint" -root "$tmp/nowf"

echo "--- 4. actionlint, shellcheck, Lighthouse (D-58)"
[[ -x "$actionlint_bin" ]] || ci_die "selftest: actionlint is not in .tools/bin; run make setup"
expect_cmd "actionlint on testdata/actionlint/bad.yml" 1 "[expression]" "$actionlint_bin" -no-color "$td/actionlint/bad.yml"
expect_cmd "shellcheck on testdata/shellcheck/bad.sh" 1 "SC2086" shellcheck "$td/shellcheck/bad.sh"
expect_cmd "lighthouse lint testdata/lighthouse/bad" 1 "FAIL" "$ci_root/scripts/ci/lighthouse.sh" lint "$td/lighthouse/bad"
expect_cmd "lighthouse lint testdata/lighthouse/bad-pkg (package.json --upload.target)" 1 "FAIL" "$ci_root/scripts/ci/lighthouse.sh" lint "$td/lighthouse/bad-pkg"
expect_cmd "lighthouse lint testdata/lighthouse/good" 0 "" "$ci_root/scripts/ci/lighthouse.sh" lint "$td/lighthouse/good"
expect_cmd "lighthouse lint testdata/lighthouse/good-pkg" 0 "" "$ci_root/scripts/ci/lighthouse.sh" lint "$td/lighthouse/good-pkg"

# A scratch repo with no hooks, no signing and a fixed identity.
scratch_repo() { # scratch_repo <dir>
  mkdir -p "$1"
  git -C "$1" init -q
  git -C "$1" config user.name selftest
  git -C "$1" config user.email selftest@todayz.localhost
  git -C "$1" config commit.gpgsign false
  git -C "$1" config core.hooksPath /dev/null
}

echo "--- 5. B1: a committed .env.local is visible to the gitleaks history scan"
scratch_repo "$tmp/leak"
# A GitHub-token-shaped value made here, never stored anywhere but the scratch repo.
tok="ghp_$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 36 || true)"
printf 'GITHUB_TOKEN=%s\n' "$tok" >"$tmp/leak/.env.local"
unset tok
git -C "$tmp/leak" add -f .env.local
git -C "$tmp/leak" commit -qm "fixture: a committed .env.local"
rm -f "$tmp/leak/.env.local"
expect_cmd "gitleaks git with .gitleaks.toml finds the committed .env.local" 1 "" \
  gitleaks git "$tmp/leak" --config "$ci_root/.gitleaks.toml" --no-banner --redact --log-level error --exit-code 1
# Without the fix: the working-tree allowlist hides the same commit (gitleaks applies a
# path allowlist in git mode too), so gitleaks-dir.toml is used for `gitleaks dir` only.
expect_cmd "the same scan with gitleaks-dir.toml misses it (why it is dir-only)" 0 "" \
  bash -c 'cd "$1" && gitleaks git "$2" --config gitleaks-dir.toml --no-banner --redact --log-level error --exit-code 1' _ \
  "$ci_root/scripts/ci" "$tmp/leak"

# scan.sh gitleaks in a scratch root: the scripts it needs, plus a scratch history.
scan_root() { # scan_root <dir>
  mkdir -p "$1/scripts/ci"
  cp "$ci_root/scripts/ci/scan.sh" "$ci_root/scripts/ci/lib.sh" "$ci_root/scripts/ci/versions.env" \
    "$ci_root/scripts/ci/gitleaks-dir.toml" "$1/scripts/ci/"
  if [[ ! -f "$1/.gitleaks.toml" ]]; then cp "$ci_root/.gitleaks.toml" "$1/.gitleaks.toml"; fi
}
ci_env=(env -u GITHUB_EVENT_NAME -u BASE_REF CI=true GITHUB_ACTIONS=true TODAYZ_CI_TOOLS=local)
s1="$tmp/scan-nogit"; scan_root "$s1"
expect_cmd "scan.sh gitleaks in CI without .git fails (no working-tree fallback)" 1 "no git repository" \
  "${ci_env[@]}" "$s1/scripts/ci/scan.sh" gitleaks
s2="$tmp/scan-shallow"; scratch_repo "$s2"
echo a >"$s2/README"; git -C "$s2" add README; git -C "$s2" commit -qm one
echo b >>"$s2/README"; git -C "$s2" commit -qam two
git -C "$s2" rev-parse HEAD >"$s2/.git/shallow"
scan_root "$s2"
expect_cmd "scan.sh gitleaks in CI on a shallow clone fails" 1 "shallow clone" \
  "${ci_env[@]}" "$s2/scripts/ci/scan.sh" gitleaks
# A pull request that loosens .gitleaks.toml to hide its own committed .env.local.
s3="$tmp/scan-pr"; scratch_repo "$s3"
echo a >"$s3/README"; cp "$ci_root/.gitleaks.toml" "$s3/.gitleaks.toml"
git -C "$s3" add README .gitleaks.toml; git -C "$s3" commit -qm base
git -C "$s3" update-ref refs/remotes/origin/main HEAD
cp "$td/gitleaks/loosened.toml" "$s3/.gitleaks.toml"
tok="ghp_$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 36 || true)"
printf 'GITHUB_TOKEN=%s\n' "$tok" >"$s3/.env.local"; unset tok
git -C "$s3" add -f .env.local .gitleaks.toml; git -C "$s3" commit -qm "pr: loosen the scan"
rm -f "$s3/.env.local"
scan_root "$s3"
expect_cmd "a push scan with the loosened config misses it (the hole)" 0 "history clean" \
  "${ci_env[@]}" "$s3/scripts/ci/scan.sh" gitleaks
expect_cmd "a pull_request scan reads origin/BASE_REF:.gitleaks.toml and finds it" 1 "leaks found" \
  "${ci_env[@]}" GITHUB_EVENT_NAME=pull_request BASE_REF=main "$s3/scripts/ci/scan.sh" gitleaks
expect_cmd "a BASE_REF holding .. is refused" 1 "not a plain branch name" \
  "${ci_env[@]}" GITHUB_EVENT_NAME=pull_request BASE_REF=main..x "$s3/scripts/ci/scan.sh" gitleaks

echo "--- 6. tracked-files.sh"
i=0
for p in .env.local web/.env.local .env.production.local x.pem certs/x.key .cache/f dist/f a.tfstate; do
  i=$((i + 1)); r="$tmp/tracked$i"
  scratch_repo "$r"
  mkdir -p "$r/$(dirname "$p")"; echo x >"$r/$p"; echo x >"$r/README"
  git -C "$r" add -f "$p" README
  git -C "$r" commit -qm fixture
  expect_cmd "tracked-files refuses a committed $p" 1 "$p" "$ci_root/scripts/ci/tracked-files.sh" "$r"
done
r="$tmp/tracked-clean"; scratch_repo "$r"; echo x >"$r/README"; git -C "$r" add README; git -C "$r" commit -qm clean
expect_cmd "tracked-files passes a clean repo" 0 "ok: tracked files" "$ci_root/scripts/ci/tracked-files.sh" "$r"

echo "--- 7. script guards"
for sub in apply plan "state list" destroy "-chdir=infra/terraform import a b"; do
  # shellcheck disable=SC2086 # sub is split into words on purpose
  expect_cmd "terraform.sh $sub" 1 "refused" env -u TODAYZ_WAVE5 "$ci_root/scripts/ci/terraform.sh" $sub
done
expect_cmd "ci_github_env refuses a value with a newline" 1 "has a newline; refused" \
  bash -c 'GITHUB_ENV="$2/github_env"; export GITHUB_ENV; . "$1/scripts/ci/lib.sh"; ci_github_env TZ_X "$(printf "a\nINJECTED=1")"' _ "$ci_root" "$tmp"
if [[ -s "$tmp/github_env" ]]; then bad "ci_github_env wrote to GITHUB_ENV before refusing"; else ok "nothing reached GITHUB_ENV"; fi
expect_cmd "ci_github_path refuses a directory with a newline" 1 "has a newline; refused" \
  bash -c 'GITHUB_PATH="$2/github_path"; export GITHUB_PATH; . "$1/scripts/ci/lib.sh"; ci_github_path "$(printf "/a\n/b")"' _ "$ci_root" "$tmp"
expect_cmd "ci_github_env writes a clean value" 0 "" \
  bash -c 'GITHUB_ENV="$2/github_env_ok"; export GITHUB_ENV; . "$1/scripts/ci/lib.sh"; ci_github_env TZ_X /a/b && grep -qx "TZ_X=/a/b" "$GITHUB_ENV"' _ "$ci_root" "$tmp"

# runner-prep check-target in a scratch copy of scripts/ci, so .tools/bin/caddy can be
# a symlink, a wrong build or the right one without touching the real .tools.
rp="$tmp/rp"; mkdir -p "$rp/scripts/ci" "$rp/.tools/bin"
cp "$ci_root/scripts/ci/lib.sh" "$ci_root/scripts/ci/versions.env" "$ci_root/scripts/ci/runner-prep.sh" "$rp/scripts/ci/"
printf '#!/bin/sh\necho "v%s h1:selftest"\n' "${CADDY_VERSION#v}" >"$tmp/caddy-good"
printf '#!/bin/sh\necho "v0.0.1 h1:selftest"\n' >"$tmp/caddy-old"
chmod +x "$tmp/caddy-good" "$tmp/caddy-old"
expect_cmd "runner-prep refuses a target outside .tools/bin" 1 "refused" "$rp/scripts/ci/runner-prep.sh" check-target "$tmp/caddy-good"
ln -s "$tmp/caddy-good" "$rp/.tools/bin/caddy"
expect_cmd "runner-prep refuses a symlinked .tools/bin/caddy" 1 "a symlink" "$rp/scripts/ci/runner-prep.sh" check-target "$rp/.tools/bin/caddy"
rm -f "$rp/.tools/bin/caddy"; cp "$tmp/caddy-old" "$rp/.tools/bin/caddy"
expect_cmd "runner-prep refuses a caddy at another version" 1 "pins" "$rp/scripts/ci/runner-prep.sh" check-target "$rp/.tools/bin/caddy"
cp "$tmp/caddy-good" "$rp/.tools/bin/caddy"
expect_cmd "runner-prep accepts the pinned caddy" 0 "ok:" "$rp/scripts/ci/runner-prep.sh" check-target "$rp/.tools/bin/caddy"

# install-tools.sh go-installs gitleaks and caddy with the version ldflag; without it
# they report "version is set by build process" and "unknown", and the post-install check
# fails on a runner (found by act on ci.yml). A stub `go` builds a fake binary that prints
# the -X value it was given, or what the real build prints without one.
it="$tmp/it"; mkdir -p "$it/scripts/ci" "$tmp/gostub"
cp "$ci_root/scripts/ci/lib.sh" "$ci_root/scripts/ci/versions.env" "$ci_root/scripts/ci/install-tools.sh" "$it/scripts/ci/"
cat >"$tmp/gostub/go" <<'STUB'
#!/bin/sh
val=""; mod=""
for a in "$@"; do
  case "$a" in
    -ldflags=-X=*) val="${a##*=}" ;;
    *@*) mod="$a" ;;
  esac
done
case "$mod" in
  *gitleaks*) name=gitleaks; dflt="version is set by build process" ;;
  *caddy*) name=caddy; dflt="unknown" ;;
  *) echo "go stub: unexpected $mod" >&2; exit 2 ;;
esac
printf '#!/bin/sh\necho "%s"\n' "${val:-$dflt}" >"$GOBIN/$name"
chmod +x "$GOBIN/$name"
STUB
chmod +x "$tmp/gostub/go"
stub_path="$tmp/gostub:/usr/bin:/bin"
expect_cmd "install-tools builds gitleaks and caddy that report the pinned versions" 0 "caddy ${CADDY_VERSION#v} ok" \
  env -u GITHUB_ACTIONS CI=true PATH="$stub_path" "$it/scripts/ci/install-tools.sh" gitleaks caddy
if grep -qF "gitleaks ${GITLEAKS_VERSION#v} ok" "$tmp/out"; then ok "gitleaks reports ${GITLEAKS_VERSION#v}"; else bad "gitleaks version line missing"; show; fi
rm -f "$it/.tools/bin/gitleaks" "$it/.tools/bin/caddy"
cp "$it/scripts/ci/install-tools.sh" "$tmp/install-tools.orig"
sed -i.bak '/-ldflags=-X=github.com\/zricethezav/d' "$it/scripts/ci/install-tools.sh"
sed -i.bak 's/^\(    gitleaks) go_tool .*"\$GITLEAKS_VERSION"\) \\$/\1 ;;/' "$it/scripts/ci/install-tools.sh"
if cmp -s "$tmp/install-tools.orig" "$it/scripts/ci/install-tools.sh"; then bad "ldflag mutation did not change install-tools.sh"; else ok "ldflag mutation applied"; fi
expect_cmd "install-tools without the gitleaks ldflag fails in CI" 1 "versions.env pins" \
  env -u GITHUB_ACTIONS CI=true PATH="$stub_path" "$it/scripts/ci/install-tools.sh" gitleaks

scan="$ci_root/scripts/ci/scan.sh"
for u in https://example.com https://api.todayz.localhost.example.com "https://todayz.localhost@example.com/" http://10.0.0.1:8080/ https://todayz.local; do
  expect_cmd "zap refuses $u" 1 "refused" "$scan" zap-target-ok "$u"
done
for u in https://api.todayz.localhost https://todayz.localhost/ http://127.0.0.1:58080/ "http://[::1]:5301/x"; do
  expect_cmd "zap accepts $u" 0 "ok" "$scan" zap-target-ok "$u"
done

echo "--- 8. mutations of the real files"
# mutate <label> <rule> <source> <name> <sed|awk> <program>: copy, edit, prove the edit
# changed the file, then expect [rule].
mutate() {
  local label="$1" rule="$2" src="$3" name="$4" tool="$5" prog="$6" dir
  dir="$tmp/mut/$(printf '%s' "$label" | tr -c 'A-Za-z0-9' '_')"; mkdir -p "$dir"
  case "$tool" in
    sed) sed -e "$prog" "$src" >"$dir/$name" ;;
    awk) awk "$prog" "$src" >"$dir/$name" ;;
  esac
  if cmp -s "$src" "$dir/$name"; then bad "mutation '$label' did not change $(basename "$src")"; return 0; fi
  expect_rule "mutation: $label" "$rule" "$dir/$name"
}
ci="$wf/ci.yml"
mutate "checkout pinned by tag" pin "$ci" ci.yml sed 's#actions/checkout@[0-9a-f]\{40\}#actions/checkout@v7#'
mutate "version comment dropped" pin "$ci" ci.yml sed 's# \# v[0-9][0-9.]*$##'
mutate "permissions dropped" permissions "$ci" ci.yml awk 'BEGIN{s=0} /^ *permissions:/ {s=1; next} s==1 && /^ *contents:/ {s=0; next} {s=0; print}'
mutate "contents: write" permissions "$ci" ci.yml sed 's#contents: read#contents: write#'
mutate "a $tfc apply step" forbidden "$ci" ci.yml sed "s#run: make check\$#run: $tfc -chdir=infra/terraform apply -auto-approve#"
mutate "pull_request_target trigger" trigger "$ci" ci.yml sed 's#^  pull_request:$#  pull_request_target:#'
mutate "a secret in env" secrets "$ci" ci.yml sed 's#TODAYZ_CI_TOOLS: docker#TODAYZ_CI_TOOLS: ${{ secrets.X }}#'
mutate "concurrency dropped" concurrency "$ci" ci.yml awk '/^concurrency:/ {s=1; next} s && /^[[:space:]]/ {next} {s=0; print}'
mutate "event_name dropped from the group" concurrency "$ci" ci.yml sed 's#-\${{ github.event_name }}##'
mutate "timeout-minutes dropped" timeout "$ci" ci.yml sed '/timeout-minutes:/d'
mutate "an internal make step" make "$ci" ci.yml sed 's#run: make check$#run: make check-ci#'
mutate "defaults shell dropped" shell "$ci" ci.yml awk '/^defaults:/ {s=1; next} s && /^[[:space:]]/ {next} {s=0; print}'
mutate "persist-credentials: true" checkout "$ci" ci.yml sed 's#persist-credentials: false#persist-credentials: true#'
mutate "security.yml checkout by tag" pin "$wf/security.yml" security.yml sed 's#actions/checkout@[0-9a-f]\{40\}#actions/checkout@v7#'
mutate "e2e.yml workflow_run trigger" trigger "$wf/e2e.yml" e2e.yml sed 's#^  workflow_dispatch:$#  workflow_run:#'
mutate "setup-toolchain setup-go by tag" pin "$ci_root/.github/actions/setup-toolchain/action.yml" action.yml sed 's#actions/setup-go@[0-9a-f]\{40\}#actions/setup-go@v7#'
mutate "dependabot cooldown 7 days" dependabot "$ci_root/.github/dependabot.yml" dependabot.yml sed 's#default-days: 14#default-days: 7#'

echo "--- selftest: $pass ok, $fail failed"
[[ $fail -eq 0 ]]
