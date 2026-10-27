#!/usr/bin/env bash
# Docs move with the code (lead, 27 Oct 2026). A commit that changes code must also change a doc
# (any *.md), or say why not with a "Docs: none (<reason>)" line in the message.
# Merge commits pass. Usage (commit-msg hook): docs-gate.sh <message-file>
set -euo pipefail
msg_file="${1:?message file}"
if git rev-parse -q --verify MERGE_HEAD >/dev/null; then
  exit 0
fi
staged="$(git diff --cached --name-only --diff-filter=ACMRD)"
code="$(grep -E '^(backend|customer|business|admin|site|shared|infra|e2e|mk|scripts|\.github)/' <<<"$staged" \
  | grep -vE '\.md$|^docs/|/testdata/|/baselines/' || true)"
[[ -z "$code" ]] && exit 0
grep -qE '\.md$' <<<"$staged" && exit 0
grep -qiE '^Docs: none \(.+\)' "$msg_file" && exit 0
cat >&2 <<'EOF'
commit blocked: this commit changes code but no doc.
Update the docs this change affects (docs/features, docs/guide, the folder README.md or CLAUDE.md),
or add a line to the message:  Docs: none (<why no doc changes>)
See docs/guide/working-on-todayz.md#keep-the-docs-true.
EOF
exit 1
