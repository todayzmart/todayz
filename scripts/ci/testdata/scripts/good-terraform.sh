#!/usr/bin/env bash
# Must pass wflint: static terraform calls only. terraform apply in a comment is fine.
set -euo pipefail
terraform fmt -check -recursive infra/terraform
terraform -chdir=infra/terraform init -backend=false -input=false
terraform -chdir=infra/terraform validate
terraform providers lock -platform=linux_arm64
terraform -version
echo "terraform apply waits for Wave 5"

# A for-loop word list is data, not a command (scripts/ci/terraform.sh pulls images this way).
for img in "$TERRAFORM_IMAGE" "$TFLINT_IMAGE"; do docker pull --quiet "$img" >/dev/null; done
