# What the ALB needs that Beanstalk does not create: a security group that accepts Cloudflare's
# published IP ranges only (02 §2.4 step 5), an S3 bucket for ALB access logs, and the ACM
# certificate for the API host. Beanstalk creates the ALB itself and is handed these.
#
# The ranges below are Cloudflare's published list (https://www.cloudflare.com/ips/). They are
# public data, not a secret. Refresh them from that page when Cloudflare changes them; the
# list changes rarely and Cloudflare announces it.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

variable "name" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "vpc_cidr" {
  type = string
}

variable "api_hostname" {
  type        = string
  description = "Public API host, for example api.todayz.in."
}

variable "log_retention_days" {
  type    = number
  default = 90
}

variable "cloudflare_ipv4_cidrs" {
  type = list(string)
  default = [
    "173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
    "141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
    "197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
    "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
  ]
}

variable "cloudflare_ipv6_cidrs" {
  type = list(string)
  default = [
    "2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
    "2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
  ]
}

data "aws_caller_identity" "current" {}
data "aws_elb_service_account" "this" {}

# ---- ALB security group --------------------------------------------------------------

resource "aws_security_group" "alb" {
  name_prefix = "${var.name}-alb-"
  description = "ALB: HTTPS from Cloudflare ranges only"
  vpc_id      = var.vpc_id
  tags        = { Name = "${var.name}-alb" }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "cloudflare_v4" {
  for_each          = toset(var.cloudflare_ipv4_cidrs)
  security_group_id = aws_security_group.alb.id
  description       = "HTTPS from Cloudflare"
  cidr_ipv4         = each.value
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}

resource "aws_vpc_security_group_ingress_rule" "cloudflare_v6" {
  for_each          = toset(var.cloudflare_ipv6_cidrs)
  security_group_id = aws_security_group.alb.id
  description       = "HTTPS from Cloudflare"
  cidr_ipv6         = each.value
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}

# The ALB only talks to targets inside the VPC.
resource "aws_vpc_security_group_egress_rule" "to_vpc" {
  security_group_id = aws_security_group.alb.id
  description       = "To targets in the VPC"
  cidr_ipv4         = var.vpc_cidr
  ip_protocol       = "-1"
}

# ---- Access log bucket ---------------------------------------------------------------

resource "aws_s3_bucket" "alb_logs" {
  bucket = "${var.name}-alb-logs-${data.aws_caller_identity.current.account_id}"
  #checkov:skip=CKV_AWS_18:This is the log bucket; logging it needs another bucket.
  #checkov:skip=CKV_AWS_21:Logs are write-once; versioning only adds cost.
  #checkov:skip=CKV_AWS_144:Cross-region replication of access logs is not wanted.
  #checkov:skip=CKV_AWS_145:ALB log delivery supports SSE-S3 only, not KMS.
  #checkov:skip=CKV2_AWS_62:No event notifications are needed.
}

resource "aws_s3_bucket_public_access_block" "alb_logs" {
  bucket                  = aws_s3_bucket.alb_logs.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "alb_logs" {
  bucket = aws_s3_bucket.alb_logs.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "alb_logs" {
  bucket = aws_s3_bucket.alb_logs.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "alb_logs" {
  bucket = aws_s3_bucket.alb_logs.id
  rule {
    id     = "expire"
    status = "Enabled"
    filter {}
    expiration {
      days = var.log_retention_days
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

data "aws_iam_policy_document" "alb_logs" {
  statement {
    sid       = "AlbDelivery"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.alb_logs.arn}/alb/AWSLogs/${data.aws_caller_identity.current.account_id}/*"]
    principals {
      type        = "AWS"
      identifiers = [data.aws_elb_service_account.this.arn]
    }
  }
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.alb_logs.arn, "${aws_s3_bucket.alb_logs.arn}/*"]
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

resource "aws_s3_bucket_policy" "alb_logs" {
  bucket     = aws_s3_bucket.alb_logs.id
  policy     = data.aws_iam_policy_document.alb_logs.json
  depends_on = [aws_s3_bucket_public_access_block.alb_logs]
}

# ---- Certificate ---------------------------------------------------------------------
# DNS validation. The validation CNAME lives in Cloudflare (infra/cloudflare), so Wave 5
# applies this resource first (-target), creates the record there, then applies the rest;
# see infra/terraform/README.md.

resource "aws_acm_certificate" "api" {
  domain_name       = var.api_hostname
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_acm_certificate_validation" "api" {
  certificate_arn = aws_acm_certificate.api.arn
}

output "alb_security_group_id" {
  value = aws_security_group.alb.id
}

output "access_logs_bucket" {
  value = aws_s3_bucket.alb_logs.id
}

output "certificate_arn" {
  value = aws_acm_certificate_validation.api.certificate_arn
}

output "certificate_validation_records" {
  description = "CNAME records to create in Cloudflare (DNS only)."
  value = [for o in aws_acm_certificate.api.domain_validation_options : {
    name  = o.resource_record_name
    type  = o.resource_record_type
    value = o.resource_record_value
  }]
}
