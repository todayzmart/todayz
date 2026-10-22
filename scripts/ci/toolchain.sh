#!/usr/bin/env bash
# Go, Node and pnpm versions come from the repo, not from the workflows (W0-06).
#
#   toolchain.sh print   key=value lines for $GITHUB_OUTPUT: go, node, pnpm
#   toolchain.sh check   fail if the go/node/pnpm on PATH do not satisfy the repo
#
# Sources:
#   go    the highest `toolchain goX.Y.Z` or `go X.Y[.Z]` directive across every go.mod
#   pnpm  "packageManager": "pnpm@X.Y.Z" in the root package.json (exact)
#   node  .node-version or .nvmrc if present; else NODE_CI_VERSION from versions.env
#         (24, the LTS line CI uses), which must satisfy engines.node in package.json
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# ver_ge A B: succeed when dotted version A >= B.
ver_ge() { [[ "$(printf '%s\n%s\n' "$1" "$2" | sort -t. -k1,1n -k2,2n -k3,3n | head -1)" == "$2" ]]; }

go_mods() {
  (cd "$ci_root" && find . -name go.mod -not -path '*/node_modules/*' -not -path './.tools/*' \
     -not -path './.cache/*' -not -path './.claude/*' -not -path '*/testdata/*' | sort)
}

# go_have: the Go that actually builds the repo. With GOTOOLCHAIN=auto, go
# switches to a module's `toolchain` line, so ask inside each module and keep
# the lowest; a module that go cannot load reports nothing and fails the check.
go_have() {
  local low="" v f
  while IFS= read -r f; do
    v="$(cd "$ci_root/$(dirname "${f#./}")" && go env GOVERSION 2>/dev/null)" || { echo "0"; return; }
    v="${v#go}"
    if [[ -z "$low" ]] || ver_ge "$low" "$v"; then low="$v"; fi
  done < <(go_mods)
  printf '%s\n' "${low:-0}"
}

go_version() {
  local best="" v f
  while IFS= read -r f; do
    f="$ci_root/${f#./}"
    v="$(sed -nE 's/^toolchain go([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/p' "$f" | head -1)"
    [[ -n "$v" ]] || v="$(sed -nE 's/^go ([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/p' "$f" | head -1)"
    [[ -n "$v" ]] || continue
    if [[ -z "$best" ]] || ver_ge "$v" "$best"; then best="$v"; fi
  done < <(go_mods)
  [[ -n "$best" ]] || ci_die "no go.mod with a go directive found"
  printf '%s\n' "$best"
}

pnpm_version() {
  local pm
  pm="$(sed -nE 's/.*"packageManager"[[:space:]]*:[[:space:]]*"pnpm@([0-9]+\.[0-9]+\.[0-9]+)[^"]*".*/\1/p' "$ci_root/package.json")"
  [[ -n "$pm" ]] || ci_die "package.json has no \"packageManager\": \"pnpm@X.Y.Z\""
  printf '%s\n' "$pm"
}

node_engines() { sed -nE '/"engines"/,/}/ s/.*"node"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "$ci_root/package.json" | head -1; }

node_version() {
  local f
  for f in .node-version .nvmrc; do
    if [[ -s "$ci_root/$f" ]]; then tr -d ' v\n\r' <"$ci_root/$f"; echo; return; fi
  done
  [[ -n "${NODE_CI_VERSION:-}" ]] || ci_die "no .node-version and no NODE_CI_VERSION in versions.env"
  printf '%s\n' "$NODE_CI_VERSION"
}

# node_min: the lower bound of engines.node (">=22.12" -> 22.12).
node_min() { sed -nE 's/^>=[[:space:]]*([0-9]+(\.[0-9]+){0,2}).*/\1/p' <<<"$(node_engines)"; }

check() {
  local fail=0 want have eng min
  want="$(go_version)"
  command -v go >/dev/null || { echo "FAIL: go not on PATH (want >= $want)"; fail=1; }
  if command -v go >/dev/null; then
    have="$(go_have)"
    if ver_ge "$have" "$want"; then echo "ok: go $have (repo wants >= $want)"
    else echo "FAIL: go $have is older than the repo's go $want"; fail=1; fi
  fi

  eng="$(node_engines)"
  if command -v node >/dev/null; then
    have="$(node --version)"; have="${have#v}"
    min="$(node_min)"
    if [[ -z "$min" ]]; then echo "note: engines.node \"$eng\" is not a >= range; checked major only"; min="$(node_version)"; fi
    if ver_ge "$have" "$min"; then echo "ok: node $have (engines.node \"$eng\")"
    else echo "FAIL: node $have does not satisfy engines.node \"$eng\""; fail=1; fi
  else
    echo "FAIL: node not on PATH"; fail=1
  fi
  want="$(node_version)"
  min="$(node_min)"
  if [[ -n "$min" ]] && ! ver_ge "$want.99.99" "$min"; then
    echo "FAIL: the CI Node pin ($want) does not satisfy engines.node \"$eng\""; fail=1
  else echo "ok: CI Node pin $want satisfies engines.node \"$eng\""
  fi

  want="$(pnpm_version)"
  if command -v pnpm >/dev/null; then
    have="$(pnpm --version)"
    if [[ "$have" == "$want" ]]; then echo "ok: pnpm $have (packageManager)"
    else echo "FAIL: pnpm $have, packageManager wants $want"; fail=1; fi
  else
    echo "FAIL: pnpm not on PATH (want $want)"; fail=1
  fi
  return $fail
}

case "${1:-print}" in
  print)
    echo "go=$(go_version)"
    echo "node=$(node_version)"
    echo "pnpm=$(pnpm_version)"
    ;;
  check) check ;;
  *) echo "usage: toolchain.sh [print|check]" >&2; exit 2 ;;
esac
