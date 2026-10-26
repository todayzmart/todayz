# Account-level resources, applied first (09 §5, D-255): the AWS Budgets alarm, the shared ECR
# repository and the GitHub OIDC provider (one per account). Only the lead applies, with the
# credentials in .env.cloud.local; the first apply waits for the founder's yes (D-210).
#
# State: S3 with the native lockfile in the bucket scripts/deploy/state-bucket.sh creates. The
# settings are passed at init (docs/qa/deploy-prep/RUNBOOK.md), never committed:
#   -backend-config=bucket=todayz-tfstate-<account> -backend-config=key=account/terraform.tfstate
#   -backend-config=region=ap-south-1 -backend-config=use_lockfile=true -backend-config=encrypt=true

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
    tags = { Project = "todayz", ManagedBy = "terraform", Stack = "account" }
  }
}

variable "region" {
  type    = string
  default = "ap-south-1"
}

variable "aws_account_id" {
  type        = string
  default     = "061292478202"
  description = "The TodayZ AWS account (D-255). The provider refuses credentials for any other account."
}

variable "budget_alert_emails" {
  type        = list(string)
  description = "Founders' addresses for budget alerts."
}

variable "monthly_budget_amount" {
  type        = string
  default     = "30000"
  description = "Monthly cost alarm for the whole account (09 §6: ₹30k; prod only since D-210). In budget_limit_unit; about 350 when the account bills in USD (RUNBOOK: the founder confirms the billing currency)."
}

variable "budget_limit_unit" {
  type        = string
  default     = "INR"
  description = "The account's billing currency: INR for an AWS India (AISPL) account, USD otherwise. Budgets refuses a unit that does not match."

  validation {
    condition     = contains(["INR", "USD"], var.budget_limit_unit)
    error_message = "budget_limit_unit must be INR or USD."
  }
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

# GitHub Actions OIDC (D-254): lets the deploy workflow assume envs/prod's deploy role without a
# stored AWS key. One provider per account; the role's trust policy narrows it to the repository
# and its production environment.
resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

output "github_oidc_provider_arn" {
  value = aws_iam_openid_connect_provider.github.arn
}
