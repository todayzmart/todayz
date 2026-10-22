#!/usr/bin/env bash
# D-56: the canonical make targets exist, there is no `make seed`, and the
# list in CLAUDE.md, 02-architecture.md and 09-infra-release.md matches.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
canonical=(setup dev stop logs reset gen check e2e load security ai-eval demo env-check release-candidate)
db="$(make -C "$root" -pRrq : 2>/dev/null || true)"
fail=0
for t in "${canonical[@]}"; do
  grep -qE "^${t}:" <<<"$db" || { echo "FAIL: make target '$t' missing"; fail=1; }
  grep -q "make ${t}" "$root/CLAUDE.md" || { echo "FAIL: CLAUDE.md does not list 'make $t'"; fail=1; }
  grep -q "\`${t}\`" "$root/docs/plan/02-architecture.md" || { echo "FAIL: 02-architecture.md §1.2 does not list '$t'"; fail=1; }
done
if grep -qE "^seed:" <<<"$db"; then echo "FAIL: 'make seed' must not exist (make reset seeds)"; fail=1; fi
# Every registered step must be a real target.
for list in SETUP_STEPS DEV_STEPS STOP_STEPS LOGS_STEPS RESET_STEPS GEN_STEPS CHECK_STEPS E2E_STEPS LOAD_STEPS SECURITY_STEPS AI_EVAL_STEPS DEMO_STEPS; do
  steps="$(make -C "$root" -s --no-print-directory -f "$root/Makefile" -f - print-list LIST="$list" <<'MK'
print-list: ; @echo $($(LIST))
MK
)"
  for s in $steps; do
    grep -qE "^${s}:" <<<"$db" || { echo "FAIL: $list step '$s' has no target"; fail=1; }
  done
done
[[ $fail -eq 0 ]] && echo "ok: canonical make targets (${#canonical[@]})"
exit $fail
