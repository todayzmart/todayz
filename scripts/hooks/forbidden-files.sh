#!/usr/bin/env bash
# Refuse to commit env files with values or generated code (CLAUDE.md, D-55).
set -euo pipefail
bad=0
for f in "$@"; do
  case "$f" in
    .env|*.env.local|.env.*.local|*/.env)
      echo "refusing to commit $f: env files hold secrets"; bad=1 ;;
    backend/gen/*|backend/api/openapi.yaml|shared/api-client/src/gen/*|*routeTree.gen.ts)
      echo "refusing to commit $f: generated code (make gen produces it)"; bad=1 ;;
    *.tfstate|*.tfstate.*)
      echo "refusing to commit $f: terraform state"; bad=1 ;;
  esac
done
if git diff --cached --name-only | grep -qx '.env.example'; then
  # .env.example holds names only: every NAME= line must be empty.
  if git show :.env.example | grep -E '^[A-Z][A-Z0-9_]*=.+' >/dev/null; then
    echo "refusing to commit .env.example with a value (names only)"; bad=1
  fi
fi
exit $bad
