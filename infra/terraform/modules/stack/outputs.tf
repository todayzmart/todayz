output "api_environment_name" {
  value = one(module.api[*].environment_name)
}

output "worker_environment_name" {
  value = one(module.worker[*].environment_name)
}

output "api_origin_hostname" {
  description = "The ALB / Beanstalk endpoint Cloudflare's api. record points at; empty until app_environments_enabled."
  value       = one(module.api[*].endpoint_url)
}

output "certificate_validation_records" {
  value = module.edge.certificate_validation_records
}

output "database_endpoint" {
  value = module.database.endpoint
}

output "database_master_secret_arn" {
  value = module.database.master_secret_arn
}

output "alarm_topic_arn" {
  value = aws_sns_topic.alarms.arn
}

output "multi_az" {
  value = module.database.multi_az
}

output "ops_run_task_parameter" {
  description = "SSM parameter with the ops task settings (scripts/deploy/beanstalk.sh)."
  value       = aws_ssm_parameter.ops_run_task.name
}

output "app_versions_bucket" {
  value = aws_s3_bucket.app_versions.id
}

output "github_deploy_role_arn" {
  description = "The role .github/workflows/deploy-backend.yml assumes; empty without github_oidc_provider_arn."
  value       = one(aws_iam_role.github_deploy[*].arn)
}
