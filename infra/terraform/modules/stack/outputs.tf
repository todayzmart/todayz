output "api_environment_name" {
  value = module.api.environment_name
}

output "worker_environment_name" {
  value = module.worker.environment_name
}

output "api_origin_hostname" {
  description = "The ALB / Beanstalk endpoint Cloudflare's api. record points at."
  value       = module.api.endpoint_url
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
