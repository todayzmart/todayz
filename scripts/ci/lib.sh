#!/usr/bin/env bash
# Shared helpers for scripts/ci/*.sh (W0-06). Source it; do not run it.
#
# Tool resolution (TODAYZ_CI_TOOLS):
#   auto   (default) use a local binary if one is on PATH, else the pinned image
#   docker always use the pinned image (the workflows set this, so CI is reproducible)
#   local  never use docker; fail if the binary is missing
# Never prints env values; scripts here only refer to variables by name.

ci_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/ci/versions.env
. "$ci_root/scripts/ci/versions.env"
export PATH="$ci_root/.tools/bin:$PATH"

ci_log() { printf '%s\n' "ci: $*"; }
ci_die() { printf '%s\n' "ci: $*" >&2; exit 1; }

# GitHub Actions log groups when running in Actions (act included); plain lines otherwise.
ci_group() { if [[ -n "${GITHUB_ACTIONS:-}" ]]; then echo "::group::$*"; else echo "--- $*"; fi; }
ci_endgroup() { if [[ -n "${GITHUB_ACTIONS:-}" ]]; then echo "::endgroup::"; fi; }

# ci_in_ci: succeed on a CI runner (GitHub Actions and act set these).
ci_in_ci() { [[ -n "${CI:-}" || -n "${GITHUB_ACTIONS:-}" ]]; }

# ci_use_docker <binary>: succeed when the pinned image should be used for <binary>.
ci_use_docker() {
  case "${TODAYZ_CI_TOOLS:-auto}" in
    docker) return 0 ;;
    local) return 1 ;;
    auto) ! command -v "$1" >/dev/null 2>&1 ;;
    *) ci_die "TODAYZ_CI_TOOLS must be auto, docker or local" ;;
  esac
}

# ci_docker_run <image> [docker run args...] -- [command args...]
# Mounts nothing by itself. Runs as the calling user on Linux so files written
# into the workspace are not owned by root (Docker Desktop and Colima map users already).
# Under scripts/no-egress.sh (TODAYZ_NO_EGRESS=1) the image must already be present:
# the Docker daemon is outside the egress block, so --pull never keeps the run offline.
ci_docker_run() {
  local image="$1"; shift
  local -a dargs=()
  while [[ $# -gt 0 && "$1" != "--" ]]; do dargs+=("$1"); shift; done
  [[ "${1:-}" == "--" ]] && shift
  command -v docker >/dev/null 2>&1 || ci_die "docker is needed for $image (or install the tool locally)"
  if [[ "$(uname -s)" == "Linux" ]]; then dargs+=(--user "$(id -u):$(id -g)" -e HOME=/tmp); fi
  if [[ "${TODAYZ_NO_EGRESS:-}" == "1" ]]; then dargs+=(--pull never); fi
  docker run --rm ${dargs[@]+"${dargs[@]}"} "$image" "$@"
}

# ci_out <name>: a report folder, ci-reports/<name>. The folder is not hidden and is
# ignored by version control; the workflows upload the paths listed in
# scripts/ci/paths.env (W0-06 S3).
ci_out() {
  local d="$ci_root/ci-reports/$1"
  mkdir -p "$d"
  printf '%s\n' "$d"
}

# ci_github_env <NAME> <value>: append NAME=value to $GITHUB_ENV for later steps.
# Refuses a value holding a newline or CR, which could inject more variables (S4).
# Values are paths and flags only, never secrets, and nothing is printed.
ci_github_env() {
  local name="$1" value="$2"
  [[ "$name" =~ ^[A-Z_][A-Z0-9_]*$ ]] || ci_die "ci_github_env: bad variable name"
  case "$value" in *$'\n'*|*$'\r'*) ci_die "ci_github_env: the value for $name has a newline; refused" ;; esac
  [[ -n "${GITHUB_ENV:-}" ]] || ci_die "ci_github_env: GITHUB_ENV is not set"
  printf '%s=%s\n' "$name" "$value" >>"$GITHUB_ENV"
}

# ci_github_path <dir>: put dir first on PATH for later steps ($GITHUB_PATH), with the
# same newline refusal as ci_github_env.
ci_github_path() {
  local dir="$1"
  case "$dir" in *$'\n'*|*$'\r'*) ci_die "ci_github_path: the directory has a newline; refused" ;; esac
  [[ -n "${GITHUB_PATH:-}" ]] || ci_die "ci_github_path: GITHUB_PATH is not set"
  printf '%s\n' "$dir" >>"$GITHUB_PATH"
}
