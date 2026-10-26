#!/usr/bin/env bash
# Create the Terraform state bucket (D-255): the one AWS resource made outside Terraform, because
# every root keeps its state in it. Idempotent; the lead runs it once with .env.cloud.local
# loaded, before the first `terraform init`. All three roots (envs/account, envs/prod,
# infra/cloudflare) use it with the S3 native lockfile, so there is no DynamoDB table.
#
#   scripts/deploy/state-bucket.sh            # bucket todayz-tfstate-<account>, ap-south-1
#
# Needs the aws CLI v2 and credentials for account 061292478202 (TODAYZ_AWS_ACCOUNT_ID
# overrides). Prints names only.
set -euo pipefail

ACCOUNT="${TODAYZ_AWS_ACCOUNT_ID:-061292478202}"
REGION="${TODAYZ_STATE_REGION:-ap-south-1}"
BUCKET="todayz-tfstate-$ACCOUNT"

caller="$(aws sts get-caller-identity --query Account --output text)"
if [[ "$caller" != "$ACCOUNT" ]]; then
  echo "state-bucket: these credentials are for another account, not $ACCOUNT" >&2
  exit 1
fi

if aws s3api head-bucket --bucket "$BUCKET" 2>/dev/null; then
  echo "state-bucket: $BUCKET exists; re-applying its settings"
else
  aws s3api create-bucket --bucket "$BUCKET" --region "$REGION" \
    --create-bucket-configuration "LocationConstraint=$REGION" >/dev/null
  echo "state-bucket: created $BUCKET"
fi

aws s3api put-public-access-block --bucket "$BUCKET" --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
aws s3api put-bucket-ownership-controls --bucket "$BUCKET" \
  --ownership-controls 'Rules=[{ObjectOwnership=BucketOwnerEnforced}]'
# Versioning is the undo for a bad state write.
aws s3api put-bucket-versioning --bucket "$BUCKET" --versioning-configuration Status=Enabled
aws s3api put-bucket-encryption --bucket "$BUCKET" --server-side-encryption-configuration \
  '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"},"BucketKeyEnabled":true}]}'
aws s3api put-bucket-lifecycle-configuration --bucket "$BUCKET" --lifecycle-configuration \
  '{"Rules":[{"ID":"old-state-versions","Status":"Enabled","Filter":{},"NoncurrentVersionExpiration":{"NoncurrentDays":90},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":7}}]}'
aws s3api put-bucket-policy --bucket "$BUCKET" --policy "$(
  cat <<EOF
{"Version":"2012-10-17","Statement":[{"Sid":"DenyInsecureTransport","Effect":"Deny","Principal":"*","Action":"s3:*","Resource":["arn:aws:s3:::$BUCKET","arn:aws:s3:::$BUCKET/*"],"Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}
EOF
)"
echo "state-bucket: $BUCKET private, versioned, encrypted, TLS only"
