# Staging: the same shape, smaller and single-AZ (09 §3.5): api and worker 1 x t4g.micro,
# RDS db.t4g.small. Nothing here has been planned or applied. State: placeholder backend.

terraform {
  required_version = ">= 1.9"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  # backend "s3" {}   # Wave 5: bucket, key, region via -backend-config; never committed.
}

provider "aws" {
  region = var.region

  default_tags {
    tags = { Project = "todayz", Environment = "staging", ManagedBy = "terraform" }
  }
}

provider "aws" {
  alias  = "dr"
  region = var.dr_region

  default_tags {
    tags = { Project = "todayz", Environment = "staging", ManagedBy = "terraform" }
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

variable "api_hostname" {
  type        = string
  default     = "api-staging.todayz.in"
  description = "A hyphenated label, not api.staging.: Cloudflare Universal SSL covers one wildcard level."
}

variable "ecr_repository_arn" {
  type = string
}

variable "ecr_repository_url" {
  type = string
}

variable "alarm_email" {
  type    = string
  default = ""
}

module "stack" {
  source = "../../modules/stack"
  providers = {
    aws    = aws
    aws.dr = aws.dr
  }

  environment        = "staging"
  api_hostname       = var.api_hostname
  ecr_repository_arn = var.ecr_repository_arn
  ecr_repository_url = var.ecr_repository_url
  alarm_email        = var.alarm_email

  vpc_cidr_block = "10.30.0.0/16"

  api_instance_type    = "t4g.micro"
  api_min_size         = 1
  api_max_size         = 2
  worker_instance_type = "t4g.micro"
  worker_min_size      = 1
  worker_max_size      = 1

  # api traffic splitting needs at least two instances to mean anything; staging deploys
  # all at once.
  api_deployment_policy = "AllAtOnce"

  db_instance_class            = "db.t4g.small"
  db_allocated_storage_gb      = 20
  db_max_allocated_storage_gb  = 100
  db_deletion_protection       = false
  db_skip_final_snapshot       = true
  db_cross_region_copy_enabled = false

  secret_recovery_window_in_days = 7
}

output "api_origin_hostname" {
  value = module.stack.api_origin_hostname
}

output "certificate_validation_records" {
  value = module.stack.certificate_validation_records
}
