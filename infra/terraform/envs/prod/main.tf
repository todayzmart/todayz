# Production: the D-96 shape. Nothing here has been planned or applied (first plan is Wave 5,
# staging first). State: the backend block is a placeholder.

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
    tags = { Project = "todayz", Environment = "prod", ManagedBy = "terraform" }
  }
}

# Region for the weekly cross-region snapshot copy.
provider "aws" {
  alias  = "dr"
  region = var.dr_region

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
