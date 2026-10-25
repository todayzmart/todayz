# Secrets Manager containers for the app's keys (09 §2.1). Terraform creates the secrets and
# never a version: values are put there out of band (the lead's shell), so no secret value is
# ever in a .tf file or in the state. Beanstalk injects them by ARN.

terraform {
  required_version = ">= 1.9"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

variable "environment" {
  type        = string
  description = "staging or prod."
}

variable "recovery_window_in_days" {
  type    = number
  default = 30
}

locals {
  # App keys, 09 §2.1.
  app_keys = [
    "JWT_SIGNING_KEYS", "JWT_CURRENT_KID", "ORDER_CODE_ENC_KEY", "ORDER_CODE_HMAC_KEY",
    "BILL_QR_SIGNING_KEY", "PII_ENC_KEY", "PII_HASH_KEY", "DEVICE_SECRET_ENC_KEY", "TOTP_ENC_KEY",
    "SHARE_PREVIEW_KEY", # also set on the customer Pages project (W3-16)
  ]
  # Database URLs (built at the Wave 5 bootstrap, from the RDS-managed master secret) and
  # provider keys (02 §9, names from internal/platform/config).
  other = [
    "DATABASE_URL", "DATABASE_MIGRATE_URL",
    # No SMS key: AWS End User Messaging is signed by the instance role (D-212).
    "ANTHROPIC_API_KEY", "SARVAM_API_KEY", "TURNSTILE_SECRET_KEY",
    "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "SENTRY_DSN", "VAPID_PRIVATE_KEY",
    "WHATSAPP_API_KEY", "OTEL_EXPORTER_OTLP_HEADERS", "GEOCODER_API_KEY",
  ]
  names = toset(concat(local.app_keys, local.other))
}

resource "aws_secretsmanager_secret" "this" {
  for_each                = local.names
  name                    = "todayz/${var.environment}/${each.key}"
  description             = "TodayZ ${var.environment} ${each.key}; value set out of band"
  recovery_window_in_days = var.recovery_window_in_days
  #checkov:skip=CKV_AWS_149:The AWS-managed key is used; a CMK adds cost and a second place to lose access.
  #checkov:skip=CKV2_AWS_57:App keys are rotated by a runbook (JWT kid rollover), not by an AWS rotation Lambda.
}

output "secret_arns" {
  description = "Map of variable name to secret ARN."
  value       = { for k, s in aws_secretsmanager_secret.this : k => s.arn }
}

output "secret_arn_prefix_pattern" {
  description = "IAM resource pattern covering every secret of this environment."
  value       = "arn:aws:secretsmanager:*:*:secret:todayz/${var.environment}/*"
}
