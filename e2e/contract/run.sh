#!/usr/bin/env bash
# Schemathesis against a running todayz api (W0-04; 08 §3.8, D-88 pins the version).
#
#   run.sh smoke <openapi.yaml> <base-url>   examples and coverage phases: every operation with
#                                            its contract examples, then the deterministic
#                                            positive and negative cases derived from the schema
#                                            (make check, in-process api on a fresh database)
#   run.sh full  <openapi.yaml> <base-url>   examples, coverage and fuzzing (make e2e, against
#                                            the stack through Caddy); POST /v1/test/clock is
#                                            left out so fuzzing never moves the stack's clock
#
# Every answer goes through Schemathesis' own checks and hooks.py's todayz_problem
# (SC-PL-02: an unregistered problem code fails). JUnit output lands in the report dir.
#
# Environment: TODAYZ_SCHEMATHESIS (the binary; default .tools/schemathesis/bin/schemathesis),
# TODAYZ_CONTRACT_OUT (report dir, also the Hypothesis database; default ./out),
# TODAYZ_PROBLEMS_DIR (the registry; default backend/api/problems), TODAYZ_CA_FILE (CA bundle
# for an https base URL: Caddy's local root). The base URL must be a *.todayz.localhost name
# or a loopback address (D-100, security L-5).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mode="${1:?usage: run.sh smoke|full <openapi.yaml> <base-url>}"
spec="${2:?usage: run.sh smoke|full <openapi.yaml> <base-url>}"
base="${3:?usage: run.sh smoke|full <openapi.yaml> <base-url>}"

st="${TODAYZ_SCHEMATHESIS:-$here/../../.tools/schemathesis/bin/schemathesis}"
out="${TODAYZ_CONTRACT_OUT:-$here/out}"
if [ ! -x "$st" ]; then
  echo "run.sh: schemathesis not found at $st (make setup installs it)" >&2
  exit 1
fi
# The fuzzer only ever points at the local stack (D-100, security L-5): a *.todayz.localhost
# name or a loopback address, http or https, with an optional port. Anything else is refused
# before Schemathesis starts, so a stray --url never fuzzes a shared or public host.
refuse_base() { echo "run.sh: $1, got '$base'" >&2; exit 2; }
# A backslash, whitespace or @ anywhere in the URL is refused before anything is parsed: a
# shell, curl or a proxy could read it as a separator or userinfo (D-100 round 2, Low 8).
case "$base" in *\\* | *[[:space:]]* | *@*) refuse_base "base URL may not contain a backslash, whitespace or @" ;; esac
case "$base" in http://* | https://*) ;; *) refuse_base "base URL must start with http:// or https://" ;; esac
hostport="${base#*://}"
case "$hostport" in
  */* | *'?'* | *'#'* | *@* | *[[:space:]]*) refuse_base "base URL takes no path, query, fragment or userinfo" ;;
esac
case "$hostport" in
  *.todayz.localhost | *.todayz.localhost:[0-9]* | localhost | localhost:[0-9]* | \
  127.0.0.1 | 127.0.0.1:[0-9]* | '[::1]' | '[::1]:'[0-9]*) ;;
  *) refuse_base "base URL must be https?://<name>.todayz.localhost or a loopback address" ;;
esac
mkdir -p "$out"
case "$spec" in /*) ;; *) spec="$PWD/$spec" ;; esac

export SCHEMATHESIS_HOOKS=hooks
export PYTHONPATH="$here${PYTHONPATH:+:$PYTHONPATH}"
export PYTHONDONTWRITEBYTECODE=1
export TODAYZ_PROBLEMS_DIR="${TODAYZ_PROBLEMS_DIR:-$here/../../backend/api/problems}"
# Hypothesis' example database goes next to the reports, never into the working directory of
# whoever invoked Schemathesis (D-100, security L-5; a stray backend/cmd/todayz/.hypothesis was
# the symptom).
export HYPOTHESIS_STORAGE_DIRECTORY="$out/.hypothesis"

args=(run "$spec" --url "$base"
  --checks all
  --no-color --warnings off
  --generation-database none
  --max-failures 20
  --exclude-operation-id getTestSlow --exclude-operation-id getTestSlowSql # they sleep up to 30 s by design (W1-07)
  --report junit --report-dir "$out")
case "$mode" in
  smoke) args+=(--phases "examples,coverage") ;;
  full) args+=(--phases "examples,coverage,fuzzing" --max-examples 25 --exclude-operation-id moveTestClock) ;;
  *) echo "run.sh: mode must be smoke or full, got '$mode'" >&2; exit 2 ;;
esac
if [ -n "${TODAYZ_CA_FILE:-}" ]; then
  args+=(--tls-verify "$TODAYZ_CA_FILE")
fi

# Schemathesis writes its cache (.schemathesis/) under the working directory: keep it in the report dir.
cd "$out"
exec "$st" "${args[@]}"
