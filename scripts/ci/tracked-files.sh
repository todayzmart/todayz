#!/usr/bin/env bash
# Fail when the repository tracks a file that must never be committed (W0-06 B1).
#
#   tracked-files.sh [repo]   default: the repo root
#
# Three checks over `git ls-files`:
#   1. scripts/hooks/forbidden-files.sh, the pre-commit hook's own case list (env
#      files, generated code, Terraform state), run on every tracked path. The list
#      is reused, not copied, so the hook and this check cannot drift apart.
#   2. Tracked paths that .gitignore would ignore (`ls-files -ci --exclude-standard`):
#      .env.local, *.pem, *.key, .cache/, dist/, *.tfstate and the rest.
#   3. Key material and build output by name, so a `!negation` in .gitignore cannot
#      hide them: *.pem, *.key, .cache/, dist/.
# A committed .env.local is the case B1 found: the gitleaks history scan reads it now
# (no path allowlist in .gitleaks.toml), and this check names it directly.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
repo="${1:-$root}"
hook="$root/scripts/hooks/forbidden-files.sh"
[[ -x "$hook" ]] || { echo "tracked-files: $hook is missing" >&2; exit 1; }
cd "$repo"
git rev-parse --git-dir >/dev/null 2>&1 || { echo "tracked-files: $repo is not a git repository" >&2; exit 1; }

bad=0
list="$(mktemp "${TMPDIR:-/tmp}/todayz-tracked.XXXXXX")"
trap 'rm -f "$list"' EXIT
git ls-files >"$list"
n="$(wc -l <"$list" | tr -d ' ')"

# 1. The hook's case list. xargs may split a long list over several runs; any failure counts.
if [[ -s "$list" ]] && ! tr '\n' '\0' <"$list" | xargs -0 "$hook"; then bad=1; fi

# 2. Tracked but ignored.
ignored="$(git ls-files -ci --exclude-standard)"
if [[ -n "$ignored" ]]; then
  while IFS= read -r f; do echo "tracked but git-ignored: $f"; done <<<"$ignored"
  bad=1
fi

# 3. By name, whatever .gitignore says.
while IFS= read -r f; do
  case "$f" in
    *.pem|*.key|.cache/*|*/.cache/*|dist/*|*/dist/*) echo "refusing tracked file $f: key material or build output"; bad=1 ;;
  esac
done <"$list"

if [[ $bad -eq 0 ]]; then echo "ok: tracked files ($n) hold no env files, keys, state or build output"; fi
exit $bad
