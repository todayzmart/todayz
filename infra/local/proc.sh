#!/usr/bin/env bash
# proc.sh: a small host process manager for `make dev` (W0-02).
#
#   proc.sh start NAME [--port PORT] [--health URL] [--cwd DIR] [--timeout SEC] -- CMD [ARGS...]
#   proc.sh stop NAME
#   proc.sh stop-all
#   proc.sh status
#   proc.sh logs [NAME]          follow one log, or every log
#
# - Pidfiles, logs and the registry live in .cache/dev/ (git-ignored):
#   pids/NAME.pid, NAME.log, registry.tsv (NAME, PORT, HEALTH URL, PID, CWD).
# - start is idempotent: if NAME is already running it does nothing.
# - The child gets infra/local/local.env, then .env.local, exported (no eval;
#   values are never printed). An empty value in .env.local does not override.
# - start refuses when --port is already taken by another process.
# - With --health, start waits (default 90 s) for the URL to answer 2xx/3xx.
# - Each child runs in its own process group, so stop also ends its children
#   (pnpm -> vite, air -> api, ...).
# - A process that dies stays in the registry, so the health check shows it red.
#   stop/stop-all remove it from the registry.
set -euo pipefail
# shellcheck source=infra/local/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

usage() {
  sed -n '2,9p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

PIDS="$TZ_DEV/pids"
mkdir -p "$TZ_DEV" "$PIDS"
touch "$TZ_REGISTRY"

lock() {
  local i
  for i in $(seq 1 100); do
    mkdir "$TZ_DEV/.registry.lock" 2>/dev/null && return 0
    sleep 0.05
    [[ $i -eq 100 ]] && rm -rf "$TZ_DEV/.registry.lock"
  done
  mkdir "$TZ_DEV/.registry.lock"
}
unlock() { rmdir "$TZ_DEV/.registry.lock" 2>/dev/null || true; }

valid_name() { [[ "$1" =~ ^[a-z0-9][a-z0-9_.-]*$ ]] || { echo "proc: invalid name '$1' (use a-z 0-9 . _ -)" >&2; exit 2; }; }

# Pidfile: line 1 the pid, line 2 its start time (tz_started_at), so a reused pid
# is never mistaken for ours and never signalled. See tz_pidfile_alive in lib.sh.
pid_of() { local f="$PIDS/$1.pid" pid=""; [[ -f "$f" ]] && pid="$(head -n 1 "$f")"; tz_valid_pid "$pid" && echo "$pid" || true; }
running() { tz_pidfile_alive "$PIDS/$1.pid"; }
no_ctl() { [[ "$2" != *$'\t'* && "$2" != *$'\n'* ]] || { echo "proc: $1 must not contain tabs or newlines" >&2; exit 2; }; }

registry_remove() {
  lock
  awk -F '\t' -v n="$1" '$1 != n' "$TZ_REGISTRY" >"$TZ_REGISTRY.tmp" && mv "$TZ_REGISTRY.tmp" "$TZ_REGISTRY"
  unlock
}

registry_add() { # name port health pid cwd
  lock
  awk -F '\t' -v n="$1" '$1 != n' "$TZ_REGISTRY" >"$TZ_REGISTRY.tmp"
  printf '%s\t%s\t%s\t%s\t%s\n' "$1" "${2:--}" "${3:--}" "$4" "$5" >>"$TZ_REGISTRY.tmp"
  mv "$TZ_REGISTRY.tmp" "$TZ_REGISTRY"
  unlock
}

health_ok() {
  local code
  code="$(tz_curl -s -o /dev/null -m 3 -w '%{http_code}' "$1" 2>/dev/null || true)"
  [[ "$code" =~ ^[23] ]]
}

cmd_start() {
  local name="${1:-}"; [[ -n "$name" ]] || usage; shift
  valid_name "$name"
  local port="" health="" cwd="$TZ_ROOT" timeout="${PROC_HEALTH_TIMEOUT:-90}"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --port) port="$2"; shift 2 ;;
      --health) health="$2"; shift 2 ;;
      --cwd) cwd="$2"; shift 2 ;;
      --timeout) timeout="$2"; shift 2 ;;
      --) shift; break ;;
      *) echo "proc: unknown option $1" >&2; usage ;;
    esac
  done
  [[ $# -gt 0 ]] || { echo "proc: start $name needs -- CMD [ARGS...]" >&2; exit 2; }
  [[ -z "$port" || "$port" =~ ^[1-9][0-9]{0,4}$ ]] || { echo "proc: --port must be a number" >&2; exit 2; }
  [[ "$timeout" =~ ^[0-9]{1,4}$ ]] || { echo "proc: --timeout must be a number of seconds" >&2; exit 2; }
  [[ -z "$health" || "$health" =~ ^https?://[^[:space:]]+$ ]] || { echo "proc: --health must be an http:// or https:// URL" >&2; exit 2; }
  no_ctl --health "$health"
  no_ctl --cwd "$cwd"
  [[ -d "$cwd" ]] || { echo "proc: --cwd $cwd is not a directory" >&2; exit 2; }

  if running "$name"; then
    echo "proc: $name already running (pid $(pid_of "$name"))"
    return 0
  fi
  if [[ -n "$port" ]]; then
    local owner
    owner="$(tz_port_owner "$port")"
    if [[ -n "$owner" ]]; then
      echo "proc: refusing to start $name: port $port is already in use by $owner" >&2
      echo "proc: stop that process (or 'proc.sh stop <name>' if it is ours) and try again" >&2
      exit 1
    fi
  fi

  local log="$TZ_DEV/$name.log" rel
  cwd="$(cd "$cwd" && pwd)"
  rel="${cwd#"$TZ_ROOT"}"; rel="${rel#/}"; rel="${rel:-.}"
  printf '\n===== %s start %s (cwd %s, port %s)\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$name" "$rel" "${port:--}" >>"$log"
  (
    cd "$cwd"
    tz_load_env
    # New session = new process group led by the child, so stop can signal the group.
    exec perl -MPOSIX -e 'POSIX::setsid(); exec { $ARGV[0] } @ARGV or die "exec $ARGV[0]: $!\n"' -- "$@"
  ) </dev/null >>"$log" 2>&1 &
  local pid=$! started
  started="$(tz_started_at "$pid")"
  if ! tz_valid_pid "$pid" || [[ -z "$started" ]]; then
    echo "proc: $name exited at once; see .cache/dev/$name.log" >&2
    exit 1
  fi
  printf '%s\n%s\n' "$pid" "$started" >"$PIDS/$name.pid"
  registry_add "$name" "$port" "$health" "$pid" "$rel"
  echo "proc: started $name (pid $pid${port:+, port $port}); log .cache/dev/$name.log"

  if [[ -n "$health" ]]; then
    local waited=0
    until health_ok "$health"; do
      if ! kill -0 "$pid" 2>/dev/null; then
        echo "proc: $name exited before it was healthy; see .cache/dev/$name.log" >&2
        exit 1
      fi
      if [[ $waited -ge $timeout ]]; then
        echo "proc: $name not healthy after ${timeout}s at $health; see .cache/dev/$name.log" >&2
        exit 1
      fi
      sleep 1; waited=$((waited + 1))
    done
    echo "proc: $name healthy ($health)"
  fi
}

cmd_stop() {
  local name="${1:-}"; [[ -n "$name" ]] || usage
  valid_name "$name"
  local pid; pid="$(pid_of "$name")"
  # running() proved pid is a valid number above 1 with our recorded start time.
  if [[ -n "$pid" ]] && running "$name"; then
    kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
    local i
    for i in $(seq 1 50); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.2
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
    fi
    # Children that ignored TERM keep the group alive; finish them too.
    kill -KILL -- "-$pid" 2>/dev/null || true
    echo "proc: stopped $name"
  else
    echo "proc: $name not running"
  fi
  rm -f "$PIDS/$name.pid"
  registry_remove "$name"
}

cmd_stop_all() {
  local f n any=0
  for f in "$PIDS"/*.pid; do
    [[ -e "$f" ]] || continue
    n="$(basename "$f" .pid)"; any=1
    cmd_stop "$n"
  done
  # Registry rows without a pidfile (crashed and cleaned) go too.
  lock; : >"$TZ_REGISTRY"; unlock
  [[ $any -eq 1 ]] || echo "proc: nothing running"
}

cmd_status() {
  printf '%-16s %-7s %-6s %-8s %s\n' NAME PID PORT STATE HEALTH
  local name port health pid cwd state hs
  while IFS=$'\t' read -r name port health pid cwd; do
    [[ -n "$name" ]] || continue
    if running "$name"; then state=running; else state=down; fi
    if [[ "$health" == "-" ]]; then hs="-"
    elif health_ok "$health"; then hs="ok $health"
    else hs="failing $health"; fi
    printf '%-16s %-7s %-6s %-8s %s\n' "$name" "$pid" "$port" "$state" "$hs"
  done <"$TZ_REGISTRY"
}

cmd_logs() {
  local files=()
  if [[ -n "${1:-}" ]]; then
    valid_name "$1"
    [[ -f "$TZ_DEV/$1.log" ]] || { echo "proc: no log for $1" >&2; exit 1; }
    files=("$TZ_DEV/$1.log")
  else
    local f
    for f in "$TZ_DEV"/*.log; do [[ -e "$f" ]] && files+=("$f"); done
  fi
  if [[ ${#files[@]} -eq 0 ]]; then
    echo "proc: no process logs yet"
    return 0
  fi
  exec tail -n 50 -F "${files[@]}"
}

case "${1:-}" in
  start) shift; cmd_start "$@" ;;
  stop) shift; cmd_stop "$@" ;;
  stop-all) cmd_stop_all ;;
  status) cmd_status ;;
  logs) shift; cmd_logs "$@" ;;
  *) usage ;;
esac
