#!/usr/bin/env bash
# Lighthouse D-58 lint (W0-06): Lighthouse results stay on the local filesystem.
#
#   lighthouse.sh lint [dir]   the repo by default (the selftest passes fixture folders)
#
# Fails when
#   - a Lighthouse CI config (lighthouserc*, .lighthouserc*) uploads to temporary public
#     storage or an LHCI server, or
#   - a package.json script passes --upload.target with any value but filesystem.
# Lighthouse itself runs inside `make e2e` (the package that adds it registers the step
# on E2E_STEPS); there is no separate Lighthouse workflow (W0-06 S2).
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# find_files <dir> <find name expression...>: files under dir, skipping vendored and
# generated folders and the CI fixtures.
find_files() {
  local dir="$1"; shift
  (cd "$dir" && find . \( -name node_modules -o -name .git -o -name .tools -o -name .cache -o -name .claude \
     -o -name ci-reports -o -path ./scripts/ci/testdata \) -prune -o -type f \( "$@" \) -print) | sed "s#^\./#$dir/#"
}

lint() {
  local dir="${1:-$ci_root}" f bad=0 n=0 p=0 line
  dir="${dir%/}"
  while IFS= read -r f; do
    n=$((n + 1))
    if grep -nE 'temporary-public-storage|"?target"?[[:space:]]*[:=][[:space:]]*["'\'']?lhci["'\'']?' "$f"; then
      echo "FAIL: ${f#"$dir"/} uploads Lighthouse results off this machine (D-58: filesystem only)"
      bad=1
    fi
  done < <(find_files "$dir" -name 'lighthouserc*' -o -name '.lighthouserc*')
  while IFS= read -r f; do
    p=$((p + 1))
    # Every --upload.target value (= or space, quoted or not) must be filesystem.
    while IFS= read -r line; do
      [[ -n "$line" ]] || continue
      echo "FAIL: ${f#"$dir"/}: $line (D-58: --upload.target=filesystem only)"
      bad=1
    done < <(grep -oE -- '--upload\.target([= ]|\\")+[^ "\\,}]*' "$f" | grep -vE -- '--upload\.target([= ]|\\")+filesystem$' || true)
  done < <(find_files "$dir" -name package.json)
  [[ $bad -eq 0 ]] && echo "ok: lighthouse configs ($n) and package.json scripts ($p) write to the filesystem only"
  return $bad
}

case "${1:-}" in
  lint) shift; lint "$@" ;;
  *) echo "usage: lighthouse.sh lint [dir]" >&2; exit 2 ;;
esac
