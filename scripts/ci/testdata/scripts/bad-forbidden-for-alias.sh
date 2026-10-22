#!/usr/bin/env bash
# The loop variable takes the word terraform, so $t is terraform: wflint must flag apply.
set -euo pipefail
for t in terraform; do
  $t apply -auto-approve
done
