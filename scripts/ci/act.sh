#!/usr/bin/env bash
# Run the workflows locally with nektos/act (W0-06). Not part of `make check`:
# act pulls runner images and actions from the network.
#
#   scripts/ci/act.sh -l                                  list every job
#   scripts/ci/act.sh -n                                  dry run of every push job
#   scripts/ci/act.sh pull_request -j check               run ci.yml's make check job
#   scripts/ci/act.sh workflow_dispatch -W .github/workflows/security.yml -j static
#   scripts/ci/act.sh clean                               remove .cache/act (clone,
#                                                         artifacts, cache server data)
#
# act always runs on a fresh clone of the committed HEAD in .cache/act/src, never on the
# working tree, so uncommitted files (.env.local included) never reach a job container.
# Commit first; `act.sh clean` (or rm -rf .cache/act) frees the space afterwards.
#
# Adds: the pinned runner image for ubuntu-24.04, the host's container architecture
# (Colima on Apple silicon is arm64), and local artifact and cache servers under
# .cache/act/. No secrets are passed (there are none before Wave 5).
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cache="$ci_root/.cache/act"
if [[ "${1:-}" == "clean" ]]; then
  rm -rf "$cache"
  ci_log "act: removed .cache/act"
  exit 0
fi

command -v act >/dev/null || ci_die "act is not installed (brew install act)"

case "$(uname -m)" in
  arm64|aarch64) arch=linux/arm64 ;;
  *) arch=linux/amd64 ;;
esac

src="$cache/src"
rm -rf "$src"
mkdir -p "$cache"
git clone --quiet --no-hardlinks "$ci_root" "$src"
ci_log "act: running on a clone of HEAD ($(git -C "$src" rev-parse --short HEAD)) in .cache/act/src"

# act only reads DOCKER_HOST, not the docker CLI context (Colima, Docker Desktop).
if [[ -z "${DOCKER_HOST:-}" ]]; then
  DOCKER_HOST="$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)"
  [[ -n "$DOCKER_HOST" ]] && export DOCKER_HOST
fi
# The job container gets the daemon's socket. The daemon runs in a Linux VM on a Mac,
# so the bind source is the VM's own socket path, not the Mac-side one.
sock="${ACT_DAEMON_SOCKET:-/var/run/docker.sock}"

mkdir -p "$cache/artifacts" "$cache/cache"
cd "$src"
exec act \
  -P "ubuntu-24.04=$ACT_IMAGE" \
  --container-architecture "$arch" \
  --container-daemon-socket "$sock" \
  --artifact-server-path "$cache/artifacts" \
  --cache-server-path "$cache/cache" \
  "$@"
