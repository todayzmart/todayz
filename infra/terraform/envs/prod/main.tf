# Production: the D-96 shape, the only cloud environment (D-210: merges to main deploy here).
# Only the lead plans and applies, with .env.cloud.local; the first apply waits for the
# founder's yes on the plan summary. The first deploy is two applies (D-247):
# app_environments_enabled=false, then true once the certificate, secrets, database and first
# release exist. Steps: docs/qa/deploy-prep/RUNBOOK.md.
#
# State: S3 with the native lockfile, key prod/terraform.tfstate, passed at init with
# -backend-config (bucket todayz-tfstate-<account>, region, use_lockfile=true, encrypt=true).

terraform {
  required_version = ">= 1.9"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  backend "s3" {}
}

provider "aws" {
  region              = var.region
  allowed_account_ids = [var.aws_account_id]

  default_tags {
    tags = { Project = "todayz", Environment = "prod", ManagedBy = "terraform" }
  }
}

# Region for the weekly cross-region snapshot copy.
provider "aws" {
  alias               = "dr"
  region              = var.dr_region
  allowed_account_ids = [var.aws_account_id]

  default_tags {
    tags = { Project = "todayz", Environment = "prod", ManagedBy = "terraform" }
  }
}

variable "region" {
  type    = string
  default = "ap-south-1"
}

variable "dr_region" {
  type    = string
  default = "ap-southeast-1"
}

variable "aws_account_id" {
  type        = string
  default     = "061292478202"
  description = "The TodayZ AWS account (D-248). The providers refuse credentials for any other account."
}

variable "api_hostname" {
  type    = string
  default = "api.todayz.in"
}

variable "ecr_repository_arn" {
  type        = string
  description = "From envs/account output ecr_repository_arn."
}

variable "ecr_repository_url" {
  type        = string
  description = "From envs/account output ecr_repository_url."
}

variable "alarm_email" {
  type    = string
  default = ""
}

# SMS (D-212): the DLT ids, not secrets; set as TF_VAR_sms_* from .env.cloud.local.
variable "sms_sender_id" {
  type    = string
  default = ""
}

variable "sms_dlt_entity_id" {
  type    = string
  default = ""
}

variable "sms_otp_template_id" {
  type    = string
  default = ""
}

variable "sms_notice_template_ids" {
  type    = string
  default = ""
}

# ---- First deploy and releases (D-247) ----------------------------------------------

variable "app_environments_enabled" {
  type        = bool
  default     = true
  description = "false on the very first apply only (RUNBOOK step 4)."
}

variable "initial_image_tag" {
  type        = string
  description = "The first release's image tag (scripts/deploy/beanstalk.sh build prints it). Keep it after the first deploy: later releases do not change it."
}

variable "github_oidc_provider_arn" {
  type        = string
  description = "From envs/account output github_oidc_provider_arn."
}

variable "github_repository" {
  type    = string
  default = "todayzmart/todayz"
}

variable "provider_secret_names" {
  type        = list(string)
  default     = []
  description = "Provider keys already put in Secrets Manager, injected into the api and worker (e.g. [\"TURNSTILE_SECRET_KEY\", \"R2_ACCESS_KEY_ID\", \"R2_SECRET_ACCESS_KEY\"])."
}

# Non-secret settings. Empty values are left out of the environments.
variable "r2_account_id" {
  type        = string
  default     = ""
  description = "Cloudflare account id (CLOUDFLARE_ACCOUNT_ID); R2 is reached at <id>.r2.cloudflarestorage.com."
}

variable "turnstile_site_key" {
  type        = string
  default     = "0x4AAAAAAFj6Q6j9e1vnXIkj"
  description = "Public site key of the todayz-prod widget (infra/cloudflare/pages.env, D-214)."
}

variable "vapid_public_key" {
  type    = string
  default = ""
}

variable "otel_exporter_otlp_endpoint" {
  type        = string
  default     = ""
  description = "Grafana Cloud OTLP endpoint; empty: no export (the headers go in Secrets Manager)."
}

variable "extra_properties" {
  type        = map(string)
  default     = {}
  description = "Any other non-secret property, e.g. { MOCK_LLM = \"false\", WHATSAPP_PROVIDER = \"...\" }; overrides the ones above."
}

module "stack" {
  source = "../../modules/stack"
  providers = {
    aws    = aws
    aws.dr = aws.dr
  }

  environment        = "prod"
  api_hostname       = var.api_hostname
  ecr_repository_arn = var.ecr_repository_arn
  ecr_repository_url = var.ecr_repository_url
  alarm_email        = var.alarm_email

  sms_sender_id           = var.sms_sender_id
  sms_dlt_entity_id       = var.sms_dlt_entity_id
  sms_otp_template_id     = var.sms_otp_template_id
  sms_notice_template_ids = var.sms_notice_template_ids

  app_environments_enabled = var.app_environments_enabled
  initial_image_tag        = var.initial_image_tag
  github_oidc_provider_arn = var.github_oidc_provider_arn
  github_repository        = var.github_repository
  provider_secret_names    = var.provider_secret_names
  extra_properties = merge({
    R2_ACCOUNT_ID               = var.r2_account_id
    R2_ENDPOINT                 = var.r2_account_id == "" ? "" : "https://${var.r2_account_id}.r2.cloudflarestorage.com"
    R2_BUCKET_IMAGES            = "todayz-images"
    R2_BUCKET_PRIVATE           = "todayz-private"
    R2_BUCKET_TILES             = "todayz-tiles"
    TURNSTILE_SITE_KEY          = var.turnstile_site_key
    VAPID_PUBLIC_KEY            = var.vapid_public_key
    OTEL_EXPORTER_OTLP_ENDPOINT = var.otel_exporter_otlp_endpoint
  }, var.extra_properties)

  # D-96: db.t4g.medium Single-AZ. `db_multi_az = true` is the one-flag switch.
  db_instance_class = "db.t4g.medium"
  db_multi_az       = false
}

output "api_origin_hostname" {
  value = module.stack.api_origin_hostname
}

output "certificate_validation_records" {
  value = module.stack.certificate_validation_records
}

output "multi_az" {
  value = module.stack.multi_az
}

output "api_environment_name" {
  value = module.stack.api_environment_name
}

output "worker_environment_name" {
  value = module.stack.worker_environment_name
}

output "database_endpoint" {
  value = module.stack.database_endpoint
}

output "ops_run_task_parameter" {
  value = module.stack.ops_run_task_parameter
}

output "app_versions_bucket" {
  value = module.stack.app_versions_bucket
}

output "github_deploy_role_arn" {
  value = module.stack.github_deploy_role_arn
}

output "alarm_topic_arn" {
  value = module.stack.alarm_topic_arn
}
