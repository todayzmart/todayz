#!/usr/bin/env bash
# k6 load scenarios against the local stack (W4-02; 08 §6).
#
#   run.sh <scenario> [extra k6 args]
#
# Scenarios: bootstrap (signs in the load shop owners and names them, once after load-data's
# generate step), browse_peak, order_burst, pos_rush, otp_abuse, soak (k6), and sse_fanout (the Go
# driver in ssefanout/, because k6 cannot hold an SSE stream open).
#
# Environment:
#   LOAD_API_URL    API origin (default http://127.0.0.1:8180, the api host process directly)
#   LOAD_MOCKS_URL  mocks origin, for the SMS OTP (default http://127.0.0.1:4010)
#   LOAD_OUT        report dir (default e2e/load/out, git-ignored)
#   LOAD_K6         the k6 binary (default: k6 on PATH)
#   LOAD_SOAK_DURATION, LOAD_SCALE and the scenario's own LOAD_* knobs pass through to k6.
#
# Both origins must be a *.todayz.localhost name or a loopback address, with no path (the same
# guard as e2e/contract/run.sh, D-100): load never points at a shared or public host. The suite
# signs in with the stack's mock SMS and a fake client IP per sign-in (CF-Connecting-IP, which
# the api believes only from a loopback peer outside a deployed environment), so it cannot work
# against a deployed api anyway.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scenario="${1:?usage: run.sh <scenario> [k6 args]}"
shift

api="${LOAD_API_URL:-http://127.0.0.1:8180}"
mocks="${LOAD_MOCKS_URL:-http://127.0.0.1:4010}"
out="${LOAD_OUT:-$here/out}"

refuse() { echo "run.sh: $1, got '$2'" >&2; exit 2; }
guard() {
  local base="$1" hostport
  # A backslash, whitespace or @ anywhere is refused before anything is parsed (D-100 Low 8).
  case "$base" in *\\* | *[[:space:]]* | *@*) refuse "URL may not contain a backslash, whitespace or @" "$base" ;; esac
  case "$base" in http://* | https://*) ;; *) refuse "URL must start with http:// or https://" "$base" ;; esac
  hostport="${base#*://}"
  case "$hostport" in
    */* | *'?'* | *'#'* | *@* | *[[:space:]]*) refuse "URL takes no path, query, fragment or userinfo" "$base" ;;
  esac
  case "$hostport" in
    *.todayz.localhost | *.todayz.localhost:[0-9]* | localhost | localhost:[0-9]* | \
    127.0.0.1 | 127.0.0.1:[0-9]* | '[::1]' | '[::1]:'[0-9]*) ;;
    *) refuse "URL must be https?://<name>.todayz.localhost or a loopback address" "$base" ;;
  esac
}
guard "$api"
guard "$mocks"

case "$scenario" in
  bootstrap | browse_peak | order_burst | pos_rush | otp_abuse | soak | sse_fanout) ;;
  *) echo "run.sh: unknown scenario '$scenario'" >&2; exit 2 ;;
esac

mkdir -p "$out"
fixture="$out/fixture.json"
if [ "$scenario" != otp_abuse ] && [ "$scenario" != bootstrap ] && [ ! -s "$fixture" ]; then
  echo "run.sh: $fixture is missing; run make load-data first" >&2
  exit 1
fi

if [ "$scenario" = sse_fanout ]; then
  cd "$here/ssefanout"
  exec go run . -api "$api" -mocks "$mocks" -fixture "$fixture" -out "$out" "$@"
fi

k6="${LOAD_K6:-k6}"
command -v "$k6" >/dev/null || { echo "run.sh: k6 not found (brew install k6)" >&2; exit 1; }

# Stop k6 from phoning home (usage report) and from reading any cloud token.
export K6_NO_USAGE_REPORT=true
unset K6_CLOUD_TOKEN
exec "$k6" run --quiet --no-color \
  -e LOAD_API_URL="$api" -e LOAD_MOCKS_URL="$mocks" -e LOAD_OUT="$out" -e LOAD_FIXTURE="$fixture" \
  "$@" "$here/scenarios/$scenario.js"
