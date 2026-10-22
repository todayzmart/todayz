#!/usr/bin/env bash
# Prepare a GitHub-hosted Linux runner for `make dev` / `make e2e` (W0-06).
#
# The local stack (W0-02) runs Caddy on the host on :80 and :443. A runner's user is
# not root, so the pinned Caddy build in .tools/bin gets the one capability it needs to
# bind those ports (cap_net_bind_service) instead of running Caddy as root.
# Caddy's local CA path needs no export: infra/local/lib.sh picks the per-OS default.
#
# Guards (W0-06 S4): setcap runs only when GITHUB_ACTIONS=true and
# RUNNER_ENVIRONMENT=github-hosted, never on a laptop or a self-hosted runner; the
# target must be exactly $root/.tools/bin/caddy, a regular file and not a symlink;
# and `caddy version` must match CADDY_VERSION. Anything else is refused.
#
#   runner-prep.sh               prepare (a no-op with a message where the guards say so)
#   runner-prep.sh check-target PATH   only the target checks (the selftest uses it)
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

caddy_bin="$ci_root/.tools/bin/caddy"

# check_target <path>: the exact pinned Caddy build in .tools/bin, nothing else.
check_target() {
  local p="$1" have
  [[ "$p" == "$caddy_bin" ]] || ci_die "runner-prep: refused $p: only $caddy_bin gets a capability"
  [[ ! -L "$p" ]] || ci_die "runner-prep: refused $p: a symlink"
  [[ -f "$p" ]] || ci_die "runner-prep: refused $p: not a regular file (setup-toolchain installs caddy)"
  have="$("$p" version 2>/dev/null | awk '{print $1; exit}')"
  [[ "${have#v}" == "${CADDY_VERSION#v}" ]] || ci_die "runner-prep: refused $p: caddy ${have:-unknown}, versions.env pins $CADDY_VERSION"
}

case "${1:-}" in
  check-target) check_target "${2:-}"; echo "ok: $2"; exit 0 ;;
  "") ;;
  *) echo "usage: runner-prep.sh [check-target PATH]" >&2; exit 2 ;;
esac

if [[ "$(uname -s)" != "Linux" ]]; then echo "runner-prep: not Linux; nothing to do"; exit 0; fi
if [[ "${GITHUB_ACTIONS:-}" != "true" || "${RUNNER_ENVIRONMENT:-}" != "github-hosted" ]]; then
  echo "runner-prep: not a GitHub-hosted runner; no capability is set (act runs jobs as root)"
  exit 0
fi
if [[ "$(id -u)" == 0 ]]; then echo "runner-prep: running as root; Caddy can bind :80/:443"; exit 0; fi

check_target "$caddy_bin"
command -v setcap >/dev/null || ci_die "setcap is missing (apt-get install libcap2-bin)"
sudo setcap 'cap_net_bind_service=+ep' "$caddy_bin"
ci_log "runner-prep: .tools/bin/caddy may bind :80/:443"
