#!/usr/bin/env bash
# Operating-system libraries for Playwright's Chromium on Linux runners (W0-06).
#
# `make setup` (setup-frontend, W0-05) downloads the Chromium headless shell with the
# Playwright version the repo locks. On a bare Ubuntu runner that browser still needs
# system libraries, which only `playwright install-deps` adds (apt, via sudo). `make e2e`
# then runs offline under scripts/no-egress.sh with everything in place.
#
#   playwright-install.sh            install-deps for PLAYWRIGHT_BROWSERS (default chromium)
#
# Uses the first workspace package that depends on @playwright/test (customer first,
# the package setup-frontend uses). Does nothing on macOS, and passes with a message
# when no package uses Playwright yet. install-deps runs apt through sudo, so it runs
# only when CI=true (a runner); elsewhere it prints the command to run by hand (W0-06 S4).
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "playwright: not Linux; the browsers need no extra system libraries here"
  exit 0
fi

pkg=""
for d in customer business admin site e2e shared/config; do
  f="$ci_root/$d/package.json"
  if [[ -f "$f" ]] && grep -q '"@playwright/test"' "$f"; then pkg="$ci_root/$d"; break; fi
done
if [[ -z "$pkg" ]]; then
  echo "no package depends on @playwright/test yet; no browser libraries to install"
  exit 0
fi

if [[ "${CI:-}" != "true" ]]; then
  echo "playwright: CI is not true; not running sudo apt here. To add the libraries by hand:"
  echo "  pnpm --dir ${pkg#"$ci_root"/} exec playwright install-deps ${PLAYWRIGHT_BROWSERS:-chromium}"
  exit 0
fi

read -r -a browsers <<<"${PLAYWRIGHT_BROWSERS:-chromium}"
ci_log "playwright install-deps ${browsers[*]} (from ${pkg#"$ci_root"/})"
pnpm --dir "$pkg" exec playwright install-deps "${browsers[@]}"
