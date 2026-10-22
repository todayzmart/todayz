#!/usr/bin/env bash
# Shared helpers for infra/local scripts (W0-02). Source it; do not run it.
# Nothing here prints an env value: only names, ports, paths and states.

# shellcheck disable=SC2034 # variables are used by the scripts that source this file
TZ_ROOT="${TODAYZ_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
TZ_LOCAL="$TZ_ROOT/infra/local"
TZ_DEV="$TZ_ROOT/.cache/dev"
TZ_LOCAL_ENV="$TZ_LOCAL/local.env"
TZ_ENV_LOCAL="${TODAYZ_ENV_FILE:-$TZ_ROOT/.env.local}"
TZ_REGISTRY="$TZ_DEV/registry.tsv"
TZ_CADDY_ROOT_CERT="${TODAYZ_CADDY_ROOT_CERT:-$HOME/Library/Application Support/Caddy/pki/authorities/local/root.crt}"
TZ_CADDY_ADMIN="${TODAYZ_CADDY_ADMIN:-localhost:2019}"

# PostGIS 16-3.4 pinned by digest (D-86). postgis/postgis is amd64 only; on arm64
# the same docker-postgis build comes from imresamu/postgis (multi-arch index).
# Bump the tag and digest together (docker buildx imagetools inspect IMAGE:TAG).
TZ_POSTGIS_AMD64="postgis/postgis:16-3.4@sha256:44126d872ac91993766c341e369c539e8196614321765d36a6f1bab0419a5fa5"
TZ_POSTGIS_ARM64="imresamu/postgis:16-3.4@sha256:6da75969915039b7356058b4310d43fde88c275ada982c2dfee29da68445ff4d"
if [[ -z "${TODAYZ_POSTGIS_IMAGE:-}" ]]; then
  case "$(uname -m)" in
    arm64 | aarch64) TODAYZ_POSTGIS_IMAGE="$TZ_POSTGIS_ARM64" ;;
    *) TODAYZ_POSTGIS_IMAGE="$TZ_POSTGIS_AMD64" ;;
  esac
fi
export TODAYZ_POSTGIS_IMAGE

# Compose never reads local.env or .env.local: every value it needs is literal in
# docker-compose.yml, so a real key in .env.local can never reach a container.
tz_compose() {
  docker compose --project-directory "$TZ_ROOT" -f "$TZ_ROOT/docker-compose.yml" "$@"
}

# Under scripts/no-egress.sh (TODAYZ_NO_EGRESS=1) compose must not try to pull.
TZ_PULL=()
if [[ "${TODAYZ_NO_EGRESS:-}" == 1 ]]; then TZ_PULL=(--pull never); fi

# Names an env file must never set in our processes or shells.
tz_env_name_allowed() {
  case "$1" in
    PATH | HOME | IFS | ENV | BASH_ENV | SHELLOPTS | BASHOPTS | PS4 | CDPATH | GLOBIGNORE | PROMPT_COMMAND | DYLD_* | LD_*) return 1 ;;
  esac
  # Readonly variables (UID, EUID, PPID, ...): exporting them would abort the script.
  [[ "$(declare -p "$1" 2>/dev/null)" =~ ^declare\ -[a-zA-Z]*r ]] && return 1
  return 0
}

# tz_export_env_file FILE [skip-empty]: export NAME=VALUE lines without eval.
# Understands comments, blank lines, "export " prefixes and single/double quotes,
# like scripts/envtool. With skip-empty, NAME= lines do not override earlier values.
tz_export_env_file() {
  { set +x; } 2>/dev/null # never trace values
  local file="$1" skip_empty="${2:-}" line name val
  [[ -f "$file" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    line="${line#export }"
    [[ "$line" == *=* ]] || continue
    name="${line%%=*}"
    val="${line#*=}"
    [[ "$name" =~ ^[A-Z][A-Z0-9_]*$ ]] || continue
    tz_env_name_allowed "$name" || continue
    val="${val%"${val##*[![:space:]]}"}"
    if [[ ${#val} -ge 2 && ( ("${val:0:1}" == "'" && "${val: -1}" == "'") || ("${val:0:1}" == '"' && "${val: -1}" == '"') ) ]]; then
      val="${val:1:${#val}-2}"
    fi
    [[ -n "$skip_empty" && -z "$val" ]] && continue
    export "$name=$val"
  done <"$file"
}

# tz_load_env: local defaults first, then .env.local (generated keys, own provider keys) wins.
tz_load_env() {
  { set +x; } 2>/dev/null
  tz_export_env_file "$TZ_LOCAL_ENV"
  tz_export_env_file "$TZ_ENV_LOCAL" skip-empty
}

# tz_port_owner PORT: prints "command(pid)" of the process listening on PORT,
# "an unknown process" when the port answers but lsof cannot name it (another
# user, a VM port forward), or nothing when the port is free.
tz_port_owner() {
  local owner
  owner="$(lsof -nP -iTCP:"$1" -sTCP:LISTEN -Fpc 2>/dev/null | awk '
    /^p/ { pid = substr($0, 2) }
    /^c/ { printf "%s(%s)\n", substr($0, 2), pid; exit }' || true)"
  if [[ -n "$owner" ]]; then echo "$owner"
  elif tz_port_open "$1"; then echo "an unknown process"; fi
}

# tz_curl: curl for local URLs only. Trusts Caddy's local CA when it exists (no -k
# needed) and never goes through a proxy (scripts/no-egress.sh sets one).
tz_curl() {
  if [[ -f "$TZ_CADDY_ROOT_CERT" ]]; then
    curl --noproxy '*' --cacert "$TZ_CADDY_ROOT_CERT" "$@"
  else
    curl --noproxy '*' "$@"
  fi
}

# tz_port_open PORT: true when something accepts TCP connections on 127.0.0.1:PORT.
tz_port_open() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

# --- Process identity (proc.sh, health.sh, Caddy) ------------------------------
# A pid is only ever signalled when it is a plain number above 1 (never 0, -1 or
# empty, which kill would treat as "the group" or "everything") and still has the
# start time recorded when we launched it.
tz_valid_pid() { [[ "${1:-}" =~ ^[1-9][0-9]*$ && "$1" -gt 1 && "$1" != "$$" ]]; }

# tz_started_at PID: the process start time in a fixed format, or nothing.
tz_started_at() {
  tz_valid_pid "${1:-}" || return 0
  LC_ALL=C TZ=UTC ps -o lstart= -p "$1" 2>/dev/null | tr -s ' ' || true
}

# tz_pidfile_alive FILE: true when FILE holds "pid\nstart time" of a live process.
tz_pidfile_alive() {
  local f="$1" pid want have
  [[ -f "$f" ]] || return 1
  pid="$(sed -n 1p "$f")"; want="$(sed -n 2p "$f")"
  tz_valid_pid "$pid" || return 1
  [[ -n "$want" ]] || return 1
  have="$(tz_started_at "$pid")"
  [[ -n "$have" && "$have" == "$want" ]]
}

# tz_stack_owner_ok: refuse to drive a stack another checkout started. Only one
# local stack runs per machine (fixed ports); run it from the main checkout.
tz_stack_owner_ok() {
  local dir
  dir="$(docker ps --filter name='^todayz-postgres-1$' --filter status=running \
    --format '{{.Label "com.docker.compose.project.working_dir"}}' 2>/dev/null | head -n 1 || true)"
  [[ -z "$dir" || "$dir" == "$TZ_ROOT" || "${TODAYZ_STACK_FORCE:-}" == 1 ]] && return 0
  echo "stack: the running todayz stack was started from $dir, not $TZ_ROOT." >&2
  echo "stack: one local stack runs per machine; run make from that checkout (normally the main one)," >&2
  echo "stack: or stop it there first. TODAYZ_STACK_FORCE=1 overrides." >&2
  return 1
}

tz_color() { [[ -t 1 && -z "${NO_COLOR:-}" ]]; }
