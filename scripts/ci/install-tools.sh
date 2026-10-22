#!/usr/bin/env bash
# Install pinned CI tools into .tools/bin (ignored by version control; the Makefile and
# lib.sh put it first on PATH).
#
#   install-tools.sh actionlint gitleaks lefthook shellcheck caddy
#   install-tools.sh --check [tool...]   compare what PATH resolves with versions.env; no installs
#
# Every tool must resolve to the version pinned in versions.env. A tool already on PATH
# at the pinned version is used as it is (brew on a Mac). One at a different version
# gets the pinned build installed into .tools/bin, which wins on PATH; if the pinned
# build still is not what resolves, that is an error in CI and a warning on macOS (W0-06 S6).
#
# Go tools: `go install module@version` with the public checksum database forced on
# (GOSUMDB=sum.golang.org; GONOSUMDB, GONOSUMCHECK, GOPRIVATE and GOINSECURE cleared).
# ShellCheck: the release tarball over HTTPS only (--proto '=https' --tlsv1.2), checked
# against a pinned sha256 (Linux only; on macOS use `brew install shellcheck`).
# The temporary folder is removed on every exit, failures included.
# Needs the network, so it runs from `make setup` and the workflows, never from `make check`.
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

bin="$ci_root/.tools/bin"
mkdir -p "$bin"
tmp=""
cleanup() { if [[ -n "$tmp" ]]; then rm -rf "$tmp"; fi; }
trap cleanup EXIT

# Version a binary reports, without a leading "v".
tool_version() { # tool_version <tool> <path>
  local out=""
  case "$1" in
    actionlint) out="$("$2" -version 2>/dev/null | head -1)" ;;
    gitleaks) out="$("$2" version 2>/dev/null | head -1)" ;;
    lefthook) out="$("$2" version 2>/dev/null | head -1)" ;;
    caddy) out="$("$2" version 2>/dev/null | awk '{print $1; exit}')" ;;
    shellcheck) out="$("$2" --version 2>/dev/null | sed -n 's/^version: //p')" ;;
  esac
  printf '%s\n' "${out#v}"
}

pinned() { # pinned <tool>
  local v
  case "$1" in
    actionlint) v="$ACTIONLINT_VERSION" ;;
    gitleaks) v="$GITLEAKS_VERSION" ;;
    lefthook) v="$LEFTHOOK_VERSION" ;;
    caddy) v="$CADDY_VERSION" ;;
    shellcheck) v="$SHELLCHECK_VERSION" ;;
    *) ci_die "unknown tool: $1" ;;
  esac
  printf '%s\n' "${v#v}"
}

mismatch() { # mismatch <message>: an error in CI, a warning elsewhere
  if ci_in_ci; then ci_die "$1"; fi
  ci_log "warning: $1"
}

go_tool() { # go_tool <name> <module/cmd path> <version> [go install flags...]
  local name="$1" mod="$2" ver="$3"
  shift 3
  ci_log "installing $name $ver into .tools/bin"
  env -u GONOSUMDB -u GONOSUMCHECK -u GOPRIVATE -u GOINSECURE \
    GOSUMDB=sum.golang.org GOFLAGS='' GOBIN="$bin" go install "$@" "$mod@$ver"
}

install_shellcheck_tarball() {
  [[ "$(uname -s)" == "Linux" ]] || { mismatch "shellcheck $SHELLCHECK_VERSION has no macOS build here; brew install shellcheck"; return 0; }
  local arch sum
  arch="$(uname -m)"; [[ "$arch" == "arm64" ]] && arch=aarch64
  case "$arch" in
    x86_64) sum="$SHELLCHECK_SHA256_x86_64" ;;
    aarch64) sum="$SHELLCHECK_SHA256_aarch64" ;;
    *) ci_die "no shellcheck pin for $arch" ;;
  esac
  tmp="$(mktemp -d)"
  ci_log "installing shellcheck $SHELLCHECK_VERSION ($arch) into .tools/bin"
  curl --proto '=https' --tlsv1.2 -fsSL -o "$tmp/sc.tar.xz" \
    "https://github.com/koalaman/shellcheck/releases/download/$SHELLCHECK_VERSION/shellcheck-$SHELLCHECK_VERSION.linux.$arch.tar.xz"
  echo "$sum  $tmp/sc.tar.xz" | sha256sum -c - >/dev/null || ci_die "shellcheck checksum mismatch"
  tar -xJf "$tmp/sc.tar.xz" -C "$tmp"
  install -m 0755 "$tmp/shellcheck-$SHELLCHECK_VERSION/shellcheck" "$bin/shellcheck"
  rm -rf "$tmp"; tmp=""
}

install_pinned() { # install_pinned <tool>
  case "$1" in
    actionlint) go_tool actionlint github.com/rhysd/actionlint/cmd/actionlint "$ACTIONLINT_VERSION" ;;
    # gitleaks reports "version is set by build process" unless the release ldflag is set
    # (as its Makefile does), and the version check above would then fail.
    gitleaks) go_tool gitleaks github.com/zricethezav/gitleaks/v8 "$GITLEAKS_VERSION" \
      "-ldflags=-X=github.com/zricethezav/gitleaks/v8/version.Version=$GITLEAKS_VERSION" ;;
    lefthook) go_tool lefthook github.com/evilmartians/lefthook/v2 "$LEFTHOOK_VERSION" ;;
    # Built as the main module, caddy reports "unknown" without CustomVersion.
    caddy) go_tool caddy github.com/caddyserver/caddy/v2/cmd/caddy "$CADDY_VERSION" \
      "-ldflags=-X=github.com/caddyserver/caddy/v2.CustomVersion=$CADDY_VERSION" ;;
    shellcheck) install_shellcheck_tarball ;;
  esac
}

# ensure <tool>: the tool that PATH resolves is the pinned version.
ensure() {
  local t="$1" want path have
  want="$(pinned "$t")"
  path="$(command -v "$t" || true)"
  have=""; [[ -n "$path" ]] && have="$(tool_version "$t" "$path")"
  if [[ "$have" == "$want" ]]; then ci_log "$t $have ok ($path)"; return 0; fi
  if [[ -n "$path" ]]; then ci_log "$t on PATH is ${have:-unknown} ($path), pinned $want; installing the pinned build"; fi
  install_pinned "$t"
  hash -r
  path="$(command -v "$t" || true)"
  have=""; [[ -n "$path" ]] && have="$(tool_version "$t" "$path")"
  if [[ "$have" != "$want" ]]; then mismatch "$t resolves to ${have:-nothing} (${path:-not on PATH}); versions.env pins $want"; return 0; fi
  ci_log "$t $have ok ($path)"
}

# check <tool>: report only (check-ci uses it; a warning locally, an error in CI).
check() {
  local t="$1" want path have
  want="$(pinned "$t")"
  path="$(command -v "$t" || true)"
  [[ -n "$path" ]] || { mismatch "$t is not on PATH (versions.env pins $want; run make setup)"; return 0; }
  have="$(tool_version "$t" "$path")"
  if [[ "$have" == "$want" ]]; then echo "ok: $t $have"
  else mismatch "$t is ${have:-unknown} at $path; versions.env pins $want"
  fi
}

[[ $# -gt 0 ]] || { echo "usage: install-tools.sh [--check] actionlint|gitleaks|lefthook|shellcheck|caddy ..." >&2; exit 2; }
mode=install
if [[ "$1" == "--check" ]]; then mode=check; shift; fi
for t in "$@"; do
  case "$t" in
    actionlint|gitleaks|lefthook|shellcheck|caddy) if [[ $mode == check ]]; then check "$t"; else ensure "$t"; fi ;;
    *) ci_die "unknown tool: $t" ;;
  esac
done
# On a runner, later steps get .tools/bin first on PATH (through lib.sh's newline check).
if [[ $mode == install && "${GITHUB_ACTIONS:-}" == true && -n "${GITHUB_PATH:-}" ]]; then
  ci_github_path "$bin"
fi
