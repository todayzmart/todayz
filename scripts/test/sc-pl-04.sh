#!/usr/bin/env bash
# SC-PL-04 (D-17): the default suite needs no real keys and cannot reach the internet.
# Proves the no-egress wrapper (used by make check and make e2e) blocks and reports
# outbound calls from curl, Go and Node, lets local hosts through, and unsets provider keys.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ne="$root/scripts/no-egress.sh"
work="$(mktemp -d "${TMPDIR:-/tmp}/tz-pl04.XXXXXX")"
trap 'rm -rf "$work"; [[ -n "${srv:-}" ]] && kill "$srv" 2>/dev/null || true' EXIT
fail=0

# 1. curl to a public host is refused and reported (wrapper exits non-zero).
if "$ne" curl -sS -m 5 -o /dev/null https://example.com 2>"$work/curl.err"; then
  echo "FAIL: curl reached example.com"; fail=1
fi
grep -q "example.com" "$work/curl.err" || { echo "FAIL: curl attempt not reported"; fail=1; }

# 2. Go net/http is refused and reported.
cat >"$work/egress.go" <<'GO'
package main

import ("fmt"; "net/http"; "os"; "time")

func main() {
	c := &http.Client{Timeout: 5 * time.Second}
	if _, err := c.Get(os.Args[1]); err == nil { fmt.Println("reached"); os.Exit(0) }
	os.Exit(7)
}
GO
(cd "$work" && go build -o egress egress.go)
if TODAYZ_EGRESS_REPORT="$work/go.hosts" "$ne" "$work/egress" https://api.anthropic.com/v1/messages 2>/dev/null; then
  echo "FAIL: Go reached api.anthropic.com"; fail=1
fi
grep -qx "api.anthropic.com" "$work/go.hosts" 2>/dev/null || { echo "FAIL: Go attempt not reported"; fail=1; }

# 3. Node fetch is refused and reported.
if TODAYZ_EGRESS_REPORT="$work/node.hosts" "$ne" node -e 'fetch("https://control.msg91.com/api").then(()=>process.exit(0),()=>process.exit(7))' 2>/dev/null; then
  echo "FAIL: Node reached control.msg91.com"; fail=1
fi
grep -qx "control.msg91.com" "$work/node.hosts" 2>/dev/null || { echo "FAIL: Node attempt not reported"; fail=1; }

# 4. Local hosts go direct and the run passes with nothing reported.
python3 -u -m http.server 0 --bind 127.0.0.1 --directory "$work" >"$work/srv.log" 2>&1 &
srv=$!
for _ in $(seq 1 50); do port="$(grep -oE 'port [0-9]+' "$work/srv.log" | awk '{print $2}' || true)"; [[ -n "$port" ]] && break; sleep 0.1; done
if [[ -z "${port:-}" ]]; then echo "FAIL: local test server did not start"; fail=1
else
  "$ne" curl -sS -m 5 -o /dev/null "http://127.0.0.1:$port/egress.go" || { echo "FAIL: local request blocked"; fail=1; }
  "$ne" "$work/egress" "http://localhost:$port/egress.go" >/dev/null || { echo "FAIL: Go local request blocked"; fail=1; }
fi

# 5. Provider keys are unset inside the wrapper; markers are set.
sa="sentinel-pl04-anthropic" # gitleaks:allow (test sentinel)
sm="sentinel-pl04-msg91" # gitleaks:allow (test sentinel)
# shellcheck disable=SC2016 # the inner bash expands these
env ANTHROPIC_API_KEY="$sa" MSG91_AUTH_KEY="$sm" SENTRY_DSN=sentinel-pl04 \
  "$ne" bash -c '[[ -z "${ANTHROPIC_API_KEY:-}${MSG91_AUTH_KEY:-}${SENTRY_DSN:-}" && "$TODAYZ_NO_EGRESS" == 1 && "$MOCK_LLM" == 1 ]]' \
  || { echo "FAIL: provider keys leak into the default suite"; fail=1; }

# 6. A failing command still fails.
if "$ne" false; then echo "FAIL: wrapper hides a failing command"; fail=1; fi

[[ $fail -eq 0 ]] && echo "ok: SC-PL-04 outbound blocked, local allowed, provider keys unset"
exit $fail
