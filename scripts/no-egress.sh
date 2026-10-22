#!/usr/bin/env bash
# Run a command with outbound traffic to non-local hosts blocked (SC-PL-04, D-17).
#
# Every HTTP(S) client that honours proxy env (Go net/http, curl, Node with
# NODE_USE_ENV_PROXY, Python, pnpm) is pointed at a local sink that refuses the
# request and records the host. Local hosts (*.localhost, 127.0.0.1, ::1) go direct.
# Provider keys are unset so the default suite proves it needs none of them.
# Backend code adds a dial-level guard when TODAYZ_NO_EGRESS=1 (W0-03).
#
# Exits non-zero if the command fails OR if anything tried to reach a non-local host.
# Set TODAYZ_EGRESS_REPORT=<file> to keep the list of blocked hosts.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
envtool="${ENVTOOL:-$root/.tools/bin/envtool}"
[[ -x "$envtool" ]] || { echo "no-egress: $envtool missing; run make setup" >&2; exit 1; }
[[ $# -gt 0 ]] || { echo "usage: no-egress.sh command [args...]" >&2; exit 2; }

work="$(mktemp -d "${TMPDIR:-/tmp}/tz-no-egress.XXXXXX")"
log="$work/egress.log"
: >"$log"
"$envtool" egress-sink -port-file "$work/port" -log "$log" &
sink=$!
# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup() { kill "$sink" 2>/dev/null || true; wait "$sink" 2>/dev/null || true; rm -rf "$work"; }
trap cleanup EXIT

for _ in $(seq 1 100); do [[ -s "$work/port" ]] && break; sleep 0.05; done
[[ -s "$work/port" ]] || { echo "no-egress: sink did not start" >&2; exit 1; }
proxy="http://127.0.0.1:$(cat "$work/port")"

export HTTP_PROXY="$proxy" HTTPS_PROXY="$proxy" ALL_PROXY="$proxy"
export http_proxy="$proxy" https_proxy="$proxy" all_proxy="$proxy"
export NO_PROXY="localhost,127.0.0.1,::1,.localhost" no_proxy="localhost,127.0.0.1,::1,.localhost"
export NODE_USE_ENV_PROXY=1
# GOTOOLCHAIN=auto: use the toolchain go.mod pins (D-88). make setup put it in
# the module cache; with GOPROXY=off a missing one fails instead of downloading.
# GOSUMDB stays on: the toolchain is verified against the checksum lookup make
# setup cached (GOSUMDB=off makes go refuse to switch toolchains); modules are
# verified against go.sum.
export GOPROXY=off GOTOOLCHAIN=auto GOFLAGS="${GOFLAGS:-} -mod=readonly"
export npm_config_offline=true npm_config_update_notifier=false npm_config_proxy="$proxy" npm_config_https_proxy="$proxy"
export DO_NOT_TRACK=1 ASTRO_TELEMETRY_DISABLED=1 STORYBOOK_DISABLE_TELEMETRY=1 NEXT_TELEMETRY_DISABLED=1
export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 CHECKPOINT_DISABLE=1 TESTCONTAINERS_RYUK_DISABLED="${TESTCONTAINERS_RYUK_DISABLED:-false}"
export TODAYZ_NO_EGRESS=1 MOCK_LLM=1

# The default suite must pass with every provider key unset (SC-PL-04).
unset MSG91_AUTH_KEY MSG91_SENDER_ID MSG91_OTP_TEMPLATE_ID WHATSAPP_API_KEY \
      VAPID_PRIVATE_KEY TURNSTILE_SECRET_KEY SARVAM_API_KEY ANTHROPIC_API_KEY \
      SENTRY_DSN VITE_SENTRY_DSN GEOCODER_API_KEY OTEL_EXPORTER_OTLP_HEADERS \
      AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE \
      CLOUDFLARE_API_TOKEN CLOUDFLARE_ACCOUNT_ID CLOUDFLARE_ZONE_ID \
      ANTHROPIC_BASE_URL ANTHROPIC_AUTH_TOKEN OPENAI_API_KEY OPENAI_BASE_URL

set +e
"$@"
rc=$?
set -e

if [[ -s "$log" ]]; then
  echo "no-egress: blocked outbound attempts (host, count):" >&2
  sort "$log" | uniq -c | sort -rn >&2
  [[ -n "${TODAYZ_EGRESS_REPORT:-}" ]] && cp "$log" "$TODAYZ_EGRESS_REPORT"
  [[ $rc -eq 0 ]] && rc=3
fi
exit $rc
