#!/usr/bin/env bash
# bad() always returns 0, so "A || bad" only runs bad when A fails.
# shellcheck disable=SC2015
# proc.sh behaviour tests (W0-03b), run by `make check` (check-stack).
# Needs no network, no Docker and no running stack; everything lives in a
# throwaway TODAYZ_ROOT, so the real .cache/dev and .env.local are never touched.
#   1. No AWS_* variable reaches an app process (D-97, SC-OPS-09), whether it
#      comes from the env file or from the calling shell. Dummy values only.
#   2. stop-all stops api and worker first and waits for them to exit, then the
#      rest, mocks last (D-98).
#   3. A process that ignores SIGTERM is SIGKILLed after its stop timeout.
# Only variable names are ever printed, never values.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
proc="$here/../proc.sh"

fail=0
bad() { echo "FAIL: $*" >&2; fail=1; sec=1; }
sec=0

tmp="$(mktemp -d)"
export TODAYZ_ROOT="$tmp/root"
export TODAYZ_ENV_FILE="$tmp/env.local"
export PROC_HEALTH_TIMEOUT=5
mkdir -p "$TODAYZ_ROOT"
# shellcheck disable=SC2329 # run by the EXIT trap
cleanup() {
  "$proc" stop-all >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

# A fake app: logs "NAME up", and on SIGTERM logs "NAME term", takes DELAY
# seconds, logs "NAME exit". With DELAY "ignore" it ignores SIGTERM.
fake="$tmp/fake.sh"
cat >"$fake" <<'EOF'
#!/usr/bin/env bash
name="$1" delay="$2" log="$3"
if [[ "$delay" == ignore ]]; then
  trap '' TERM
else
  trap 'echo "$name term" >>"$log"; sleep "$delay"; echo "$name exit" >>"$log"; exit 0' TERM
fi
echo "$name up" >>"$log"
while :; do sleep 0.1; done
EOF
chmod +x "$fake"

# A fake app that writes the names (never the values) of its environment.
names="$tmp/names.sh"
cat >"$names" <<'EOF'
#!/usr/bin/env bash
compgen -e >"$1.tmp" && mv "$1.tmp" "$1"
while :; do sleep 0.1; done
EOF
chmod +x "$names"

wait_for() { # FILE PATTERN: up to 10 s for PATTERN to appear in FILE
  local _
  for _ in $(seq 1 100); do
    [[ -f "$1" ]] && grep -q "$2" "$1" && return 0
    sleep 0.1
  done
  return 1
}

# --- 1. AWS_* never reaches an app process -----------------------------------------
sec=0
cat >"$TODAYZ_ENV_FILE" <<'EOF'
AWS_ACCESS_KEY_ID=test-dummy
AWS_SECRET_ACCESS_KEY=test-dummy-secret
AWS_REGION=ap-south-1
TZ_PROC_TEST_MARK=1
EOF
for app in api worker; do
  AWS_SESSION_TOKEN=test-dummy-token AWS_PROFILE=test-dummy \
    "$proc" start "$app" -- "$names" "$tmp/$app.names" >/dev/null
  wait_for "$tmp/$app.names" TZ_PROC_TEST_MARK || bad "$app never wrote its environment names"
  # The positive control proves the env file was loaded at all.
  grep -qx TZ_PROC_TEST_MARK "$tmp/$app.names" || bad "$app did not get the env file (TZ_PROC_TEST_MARK missing)"
  leaked="$(grep '^AWS_' "$tmp/$app.names" | tr '\n' ' ' || true)"
  [[ -z "$leaked" ]] || bad "$app got AWS_* variables: $leaked"
done
"$proc" stop-all >/dev/null
[[ $sec -eq 0 ]] && echo "ok: no AWS_* variable reaches an app process (env file and shell)"

# --- 2. stop-all order ---------------------------------------------------------------
sec=0
: >"$TODAYZ_ENV_FILE"
order="$tmp/order.log"
: >"$order"
"$proc" start mocks -- "$fake" mocks 0 "$order" >/dev/null
"$proc" start web -- "$fake" web 0 "$order" >/dev/null
"$proc" start api -- "$fake" api 1 "$order" >/dev/null
"$proc" start worker -- "$fake" worker 2 "$order" >/dev/null
for n in mocks web api worker; do wait_for "$order" "^$n up$" || bad "$n did not start"; done
out="$("$proc" stop-all 2>&1)" || bad "stop-all failed"
line() { grep -nx "$1" "$order" | head -n 1 | cut -d: -f1; }
for ev in "api term" "api exit" "worker term" "worker exit" "web term" "web exit" "mocks term" "mocks exit"; do
  [[ -n "$(line "$ev")" ]] || bad "no '$ev' in the order log"
done
if [[ $sec -eq 0 ]]; then
  [[ $(line "web term") -gt $(line "api exit") && $(line "web term") -gt $(line "worker exit") ]] ||
    bad "web was stopped before api and worker had exited"
  [[ $(line "worker term") -lt $(line "api exit") ]] || bad "api and worker were not signalled together"
  [[ $(line "mocks term") -gt $(line "web exit") ]] || bad "mocks was not stopped last"
fi
grep -q "stop-all 1/3: draining" <<<"$out" || bad "stop-all did not announce the drain"
grep -q "SIGKILL" <<<"$out" && bad "stop-all SIGKILLed a process that exited on SIGTERM"
[[ -z "$(ls "$TODAYZ_ROOT/.cache/dev/pids")" ]] || bad "pidfiles left after stop-all"
[[ ! -s "$TODAYZ_ROOT/.cache/dev/registry.tsv" ]] || bad "registry not empty after stop-all"
[[ $sec -eq 0 ]] && echo "ok: stop-all drains api and worker first, then the rest, mocks last"

# --- 3. SIGKILL after the stop timeout -------------------------------------------------
sec=0
: >"$order" # section 2 already wrote "worker up"; the wait below must see this worker's line
"$proc" start worker -- "$fake" worker ignore "$order" >/dev/null
wait_for "$order" "^worker up$" || bad "the stubborn worker did not start"
pid="$(head -n 1 "$TODAYZ_ROOT/.cache/dev/pids/worker.pid")"
out="$(PROC_STOP_TIMEOUT_WORKER=1 "$proc" stop worker 2>&1)" || bad "stop worker failed"
grep -q "worker still running after 1s; sent SIGKILL" <<<"$out" || bad "no SIGKILL message for the stubborn worker"
sleep 0.2
kill -0 "$pid" 2>/dev/null && bad "the stubborn worker is still alive"
out="$(PROC_STOP_TIMEOUT_WORKER=abc "$proc" stop worker 2>&1)" && bad "a non-numeric stop timeout was accepted"
[[ $sec -eq 0 ]] && echo "ok: a process that ignores SIGTERM is SIGKILLed after its stop timeout"

exit $fail
