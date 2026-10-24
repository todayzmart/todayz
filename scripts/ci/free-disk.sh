#!/usr/bin/env bash
# Free disk on a GitHub-hosted Linux runner before the toolchain is set up (W0-06, D-214).
#
# The ubuntu-24.04 image leaves too little free disk for `make setup` (Go tools, Docker
# images, Playwright, Terraform providers): the first runs on the private repo died with
# "no space left on device". The image carries SDKs TodayZ never uses; this removes a fixed
# list of them (about 25 GB) and the preinstalled Docker images, and prints `df` before
# and after.
#
# Guards (as runner-prep.sh): runs only when GITHUB_ACTIONS=true and
# RUNNER_ENVIRONMENT=github-hosted on Linux, never on a laptop, under act or on a
# self-hosted runner. Only the absolute paths below are removed; nothing is taken from
# the environment or the arguments.
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[[ $# -eq 0 ]] || { echo "usage: free-disk.sh" >&2; exit 2; }

if [[ "$(uname -s)" != "Linux" ]]; then echo "free-disk: not Linux; nothing to do"; exit 0; fi
if [[ "${GITHUB_ACTIONS:-}" != "true" || "${RUNNER_ENVIRONMENT:-}" != "github-hosted" || -n "${ACT:-}" ]]; then
  echo "free-disk: not a GitHub-hosted runner; nothing removed"
  exit 0
fi

UNUSED=(
  /usr/local/lib/android
  /usr/share/dotnet
  /opt/ghc
  /usr/local/.ghcup
  /opt/hostedtoolcache/CodeQL
  /usr/local/share/powershell
  /usr/share/swift
  /usr/local/julia*
  /opt/microsoft
)

df -h / | sed 's/^/free-disk: before: /'
sudo rm -rf "${UNUSED[@]}"
if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
  docker image prune --all --force >/dev/null
fi
df -h / | sed 's/^/free-disk: after:  /'
ci_log "free-disk: removed unused runner SDKs and Docker images"
