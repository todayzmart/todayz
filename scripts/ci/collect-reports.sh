#!/usr/bin/env bash
# Copy this run's reports into ci-reports/collected/ for upload (W0-06 S3).
# The paths come from scripts/ci/paths.env, the single list. A leading "." in any path
# part becomes "_" so nothing uploaded is hidden. Prints the count, never file contents.
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
# shellcheck source=scripts/ci/paths.env
. "$ci_root/scripts/ci/paths.env"

dest="$ci_root/ci-reports/collected"
rm -rf "$dest"
mkdir -p "$dest"
n=0

visible() { sed -E 's#(^|/)\.#\1_#g' <<<"$1"; }

copy() { # copy <path relative to the repo root>
  local rel="$1" to
  [[ -e "$ci_root/$rel" ]] || return 0
  to="$dest/$(visible "$rel")"
  mkdir -p "$(dirname "$to")"
  cp -R "$ci_root/$rel" "$to"
  n=$((n + 1))
}

for p in $CI_REPORT_PATHS; do copy "$p"; done
for pkg in $CI_REPORT_PACKAGES; do
  for d in $CI_REPORT_PACKAGE_DIRS; do copy "$pkg/$d"; done
done
if git -C "$ci_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  for c in $CI_REPORT_CHANGED; do
    while IFS= read -r f; do
      [[ -n "$f" && -f "$ci_root/$f" ]] && copy "$f"
    done < <(git -C "$ci_root" status --porcelain --untracked-files=all -- "$c" | sed -E 's/^.. //; s/^.* -> //')
  done
fi
ci_log "collected $n report paths into ci-reports/collected"
