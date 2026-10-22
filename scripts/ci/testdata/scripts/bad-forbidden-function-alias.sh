#!/usr/bin/env bash
set -euo pipefail
tfx() {
  terraform "$@"
}
tfx state rm aws_s3_bucket.b
