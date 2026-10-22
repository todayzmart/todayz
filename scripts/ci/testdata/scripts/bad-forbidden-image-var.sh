#!/usr/bin/env bash
set -euo pipefail
ci_docker_run "$TERRAFORM_IMAGE" -v "$PWD:/w" -w /w -- import aws_s3_bucket.b b
