#!/usr/bin/env bash
# Seeded violation: a CI script must never reach a cloud account before Wave 5.
set -euo pipefail
terraform -chdir=infra/terraform apply -auto-approve
