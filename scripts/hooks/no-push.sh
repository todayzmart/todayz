#!/usr/bin/env bash
# Only the lead pushes (D-210): every push to main deploys to prod. Worker sessions never set
# TODAYZ_LEAD_PUSH, so their pushes stop here.
if [[ "${TODAYZ_LEAD_PUSH:-}" == "1" ]]; then
  exit 0
fi
echo "push blocked: only the lead pushes, and a push to main deploys to prod (D-210)." >&2
exit 1
