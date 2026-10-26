# Releases (D-247): the S3 bucket that holds the Beanstalk source bundles, and the role the
# GitHub deploy workflow assumes through OIDC. A release is an image in ECR plus a small
# docker-compose bundle here; scripts/deploy/beanstalk.sh writes both.

resource "aws_s3_bucket" "app_versions" {
  bucket = "${local.name}-app-versions-${data.aws_caller_identity.current.account_id}"
  #checkov:skip=CKV_AWS_18:Source bundles are a few kilobytes written only by the deploy role; access logging needs another bucket.
  #checkov:skip=CKV_AWS_144:A lost bundle is rebuilt from the git tag; cross-region replication is not wanted.
  #checkov:skip=CKV_AWS_145:SSE-S3; a CMK adds cost and a key policy Beanstalk would also need.
  #checkov:skip=CKV2_AWS_62:No event notifications are needed.
}

resource "aws_s3_bucket_public_access_block" "app_versions" {
  bucket                  = aws_s3_bucket.app_versions.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "app_versions" {
  bucket = aws_s3_bucket.app_versions.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_versioning" "app_versions" {
  bucket = aws_s3_bucket.app_versions.id
  versioning_configuration {
    status = "Enabled"
  }
}

# Trivy AWS-0132: SSE-S3 (CKV_AWS_145 above).
#trivy:ignore:AWS-0132
resource "aws_s3_bucket_server_side_encryption_configuration" "app_versions" {
  bucket = aws_s3_bucket.app_versions.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Current bundles stay (a rollback redeploys an old application version, which needs its
# bundle); overwritten copies go after 90 days.
resource "aws_s3_bucket_lifecycle_configuration" "app_versions" {
  bucket = aws_s3_bucket.app_versions.id
  rule {
    id     = "noncurrent"
    status = "Enabled"
    filter {}
    noncurrent_version_expiration {
      noncurrent_days = 90
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

data "aws_iam_policy_document" "app_versions" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.app_versions.arn, "${aws_s3_bucket.app_versions.arn}/*"]
    principals {
      type        = "*"
      identifiers = ["*"]
    }
    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "app_versions" {
  bucket = aws_s3_bucket.app_versions.id
  policy = data.aws_iam_policy_document.app_versions.json
}

# ---- GitHub deploy role ----------------------------------------------------------------
# Only a job in the repository's `production` GitHub environment can assume it (the environment
# can require a reviewer). No long-lived AWS key is stored in GitHub.

locals {
  github_deploy = var.github_oidc_provider_arn == "" ? 0 : 1
}

data "aws_iam_policy_document" "github_deploy_assume" {
  count = local.github_deploy
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [var.github_oidc_provider_arn]
    }
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:${var.github_repository}:environment:production"]
    }
  }
}

resource "aws_iam_role" "github_deploy" {
  count                = local.github_deploy
  name                 = "${local.name}-github-deploy"
  assume_role_policy   = data.aws_iam_policy_document.github_deploy_assume[0].json
  max_session_duration = 3600
}

# Beanstalk's update-environment acts with the caller's permissions across EC2, Auto Scaling,
# ELB, CloudFormation and S3; AWS publishes this policy for exactly that caller.
resource "aws_iam_role_policy_attachment" "github_deploy_beanstalk" {
  count      = local.github_deploy
  role       = aws_iam_role.github_deploy[0].name
  policy_arn = "arn:aws:iam::aws:policy/AdministratorAccess-AWSElasticBeanstalk"
}

data "aws_iam_policy_document" "github_deploy" {
  statement {
    sid       = "EcrLogin"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }
  statement {
    sid = "EcrPush"
    actions = [
      "ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:CompleteLayerUpload",
      "ecr:DescribeImages", "ecr:GetDownloadUrlForLayer", "ecr:InitiateLayerUpload",
      "ecr:PutImage", "ecr:UploadLayerPart",
    ]
    resources = [var.ecr_repository_arn]
  }
  statement {
    sid       = "SourceBundles"
    actions   = ["s3:GetObject", "s3:PutObject", "s3:ListBucket", "s3:GetBucketLocation"]
    resources = [aws_s3_bucket.app_versions.arn, "${aws_s3_bucket.app_versions.arn}/*"]
  }
  statement {
    sid       = "ReadOpsSettings"
    actions   = ["ssm:GetParameter"]
    resources = [aws_ssm_parameter.ops_run_task.arn]
  }
  statement {
    sid     = "RunOpsTasks"
    actions = ["ecs:RunTask"]
    resources = [
      "arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:task-definition/${aws_ecs_task_definition.migrate.family}:*",
      "arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:task-definition/${aws_ecs_task_definition.db_bootstrap.family}:*",
    ]
    condition {
      test     = "ArnEquals"
      variable = "ecs:cluster"
      values   = [aws_ecs_cluster.ops.arn]
    }
  }
  statement {
    sid       = "WatchOpsTasks"
    actions   = ["ecs:DescribeTasks"]
    resources = ["arn:aws:ecs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:task/${aws_ecs_cluster.ops.name}/*"]
  }
  statement {
    # Neither action supports a resource ARN; a new migrate revision carries the release's image.
    sid       = "MigrateRevisions"
    actions   = ["ecs:DescribeTaskDefinition", "ecs:RegisterTaskDefinition"]
    resources = ["*"]
  }
  statement {
    sid       = "PassOpsExecutionRole"
    actions   = ["iam:PassRole"]
    resources = [aws_iam_role.ops_execution.arn]
    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["ecs-tasks.amazonaws.com"]
    }
  }
  statement {
    sid       = "ReadOpsLogs"
    actions   = ["logs:GetLogEvents", "logs:FilterLogEvents"]
    resources = ["${aws_cloudwatch_log_group.ops.arn}:*"]
  }
}

resource "aws_iam_role_policy" "github_deploy" {
  count  = local.github_deploy
  name   = "release"
  role   = aws_iam_role.github_deploy[0].id
  policy = data.aws_iam_policy_document.github_deploy.json
}
