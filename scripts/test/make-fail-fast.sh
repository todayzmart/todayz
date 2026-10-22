#!/usr/bin/env bash
# The root Makefile stops at the first failing step and returns its status,
# and a failing command inside a recipe line fails that line (make 3.81 and 4.x).
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
cat >"$tmp/t.mk" <<MK
tz-test-fail: ; @false; touch "$tmp/after-false"
tz-test-after: ; @touch "$tmp/after-step"
tz-test-pipe: ; @false | true
MK
fail=0
# MAKEFILES reaches the sub-makes run_steps starts; command-line lists override the registered steps.
if MAKEFILES="$tmp/t.mk" make -C "$root" -s gen GEN_STEPS="tz-test-fail tz-test-after" >"$tmp/out" 2>&1; then
  echo "FAIL: make gen exited 0 after a failing step"; fail=1
fi
[[ -e "$tmp/after-false" ]] && { echo "FAIL: recipe line kept going after a failed command"; fail=1; }
[[ -e "$tmp/after-step" ]] && { echo "FAIL: run_steps ran the next step after a failure"; fail=1; }
grep -q "step tz-test-fail failed" "$tmp/out" || { echo "FAIL: no failed-step message"; fail=1; }
if MAKEFILES="$tmp/t.mk" make -C "$root" -s tz-test-pipe >/dev/null 2>&1; then
  echo "FAIL: pipefail is not set in recipes"; fail=1
fi
[[ $fail -eq 0 ]] && echo "ok: make stops at the first failing step"
exit $fail
