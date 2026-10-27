#!/usr/bin/env bash
# W0-01: the pre-commit guard refuses env files, generated code and terraform state;
# the pre-push guard refuses every push. Works on file names only (no real files staged).
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
guard="$root/scripts/hooks/forbidden-files.sh"
fail=0
for f in .env .env.local customer/.env.local .env.staging.local backend/gen/server.go \
         backend/api/openapi.yaml shared/api-client/src/gen/index.ts customer/src/routeTree.gen.ts \
         infra/terraform.tfstate; do
  if (cd "$root" && "$guard" "$f" >/dev/null); then echo "FAIL: guard allowed $f"; fail=1; fi
done
for f in .env.example README.md backend/api/paths/orders.yaml customer/src/routes/index.tsx; do
  (cd "$root" && "$guard" "$f" >/dev/null) || { echo "FAIL: guard refused $f"; fail=1; }
done
if "$root/scripts/hooks/no-push.sh" 2>/dev/null; then echo "FAIL: push allowed"; fail=1; fi
grep -q 'no-push.sh' "$root/lefthook.yml" || { echo "FAIL: pre-push hook not configured"; fail=1; }
grep -q 'docs-gate.sh' "$root/lefthook.yml" || { echo "FAIL: commit-msg docs gate not configured"; fail=1; }
# .env.example must hold names only.
if grep -E '^[A-Z][A-Z0-9_]*=.+' "$root/.env.example"; then echo "FAIL: .env.example has a value"; fail=1; fi
[[ $fail -eq 0 ]] && echo "ok: git hooks guard secrets, generated code and pushes"
exit $fail
