# Account-level resources, applied first in Wave 5 (09 §5): the AWS Budgets alarm is the first
# Terraform resource, then the shared ECR repository.
#
# Nothing here has been planned or applied. State: the backend block is a placeholder; Wave 5
# fills in a bucket (S3 + native lockfile) the lead creates by hand once.

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
    tags = { Project = "todayz", ManagedBy = "terraform", Stack = "account" }
  }
}

variable "region" {
  type    = string
  default = "ap-south-1"
}

variable "budget_alert_emails" {
  type        = list(string)
  description = "Founders' addresses for budget alerts."
}

variable "monthly_budget_amount" {
  type        = string
  default     = "30000"
  description = "Prod and staging together (09 §6). TODO(lead): AWS Budgets limits are normally in USD; if the account does not bill in INR set this to the USD equivalent and budget_limit_unit to USD."
}

variable "budget_limit_unit" {
  type    = string
  default = "INR"
}

resource "aws_budgets_budget" "monthly" {
  name         = "todayz-monthly"
  budget_type  = "COST"
  limit_amount = var.monthly_budget_amount
  limit_unit   = var.budget_limit_unit
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = var.budget_alert_emails
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = var.budget_alert_emails
  }
}

module "ecr" {
  source = "../../modules/ecr"
}

output "ecr_repository_url" {
  value = module.ecr.repository_url
}

output "ecr_repository_arn" {
  value = module.ecr.repository_arn
}
