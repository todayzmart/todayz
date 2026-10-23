# One Elastic Beanstalk environment on the Docker / Amazon Linux 2023 platform, Graviton.
# role = "api": web tier behind an ALB (HTTPS, Cloudflare-only security group, access logs),
#               traffic-splitting deploys, health check on /readyz.
# role = "worker": Beanstalk worker tier (no ALB), immutable deploys.
#
# Scaling policies live in the stack module because they need the ASG and the target group.

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

variable "role" {
  type = string

  validation {
    condition     = contains(["api", "worker"], var.role)
    error_message = "role must be api or worker."
  }
}

variable "application_name" {
  type = string
}

variable "solution_stack_name" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "private_subnet_ids" {
  type = list(string)
}

variable "public_subnet_ids" {
  type    = list(string)
  default = []
}

variable "instance_type" {
  type    = string
  default = "t4g.small"
}

variable "min_size" {
  type = number
}

variable "max_size" {
  type = number
}

variable "service_role_arn" {
  type = string
}

variable "instance_profile_name" {
  type = string
}

variable "instance_security_group_ids" {
  type = list(string)
}

variable "alb_security_group_id" {
  type    = string
  default = ""
}

variable "certificate_arn" {
  type    = string
  default = ""
}

variable "access_logs_bucket" {
  type    = string
  default = ""
}

variable "app_port" {
  type        = number
  description = "Port the container listens on (API_ADDR :8080 for the api)."
  default     = 8080
}

variable "deployment_policy" {
  type        = string
  description = "TrafficSplitting for the api, Immutable for the worker (D-96)."
}

variable "traffic_split_percent" {
  type    = number
  default = 10
}

variable "traffic_split_minutes" {
  type    = number
  default = 10
}

variable "health_check_path" {
  type    = string
  default = "/readyz"
}

variable "health_check_interval_seconds" {
  type    = number
  default = 5
}

variable "health_check_timeout_seconds" {
  type    = number
  default = 4
}

variable "unhealthy_threshold" {
  type    = number
  default = 2
}

variable "healthy_threshold" {
  type    = number
  default = 2
}

variable "deregistration_delay_seconds" {
  type    = number
  default = 75
}

variable "environment_properties" {
  type        = map(string)
  default     = {}
  description = "Plain (non-secret) environment variables."
}

variable "secret_arns" {
  type        = map(string)
  default     = {}
  description = "Env var name to Secrets Manager ARN, injected by Beanstalk at deploy."
}

variable "log_retention_days" {
  type    = number
  default = 30
}

locals {
  is_api = var.role == "api"

  common = [
    { ns = "aws:elasticbeanstalk:environment", name = "ServiceRole", value = var.service_role_arn },
    { ns = "aws:elasticbeanstalk:healthreporting:system", name = "SystemType", value = "enhanced" },
    { ns = "aws:autoscaling:launchconfiguration", name = "IamInstanceProfile", value = var.instance_profile_name },
    { ns = "aws:autoscaling:launchconfiguration", name = "SecurityGroups", value = join(",", var.instance_security_group_ids) },
    { ns = "aws:autoscaling:launchconfiguration", name = "DisableIMDSv1", value = "true" },
    { ns = "aws:autoscaling:launchconfiguration", name = "RootVolumeType", value = "gp3" },
    { ns = "aws:ec2:instances", name = "InstanceTypes", value = var.instance_type },
    { ns = "aws:ec2:instances", name = "SupportedArchitectures", value = "arm64" },
    { ns = "aws:autoscaling:asg", name = "MinSize", value = tostring(var.min_size) },
    { ns = "aws:autoscaling:asg", name = "MaxSize", value = tostring(var.max_size) },
    { ns = "aws:ec2:vpc", name = "VPCId", value = var.vpc_id },
    { ns = "aws:ec2:vpc", name = "Subnets", value = join(",", var.private_subnet_ids) },
    { ns = "aws:ec2:vpc", name = "AssociatePublicIpAddress", value = "false" },
    { ns = "aws:elasticbeanstalk:command", name = "DeploymentPolicy", value = var.deployment_policy },
    { ns = "aws:elasticbeanstalk:cloudwatch:logs", name = "StreamLogs", value = "true" },
    { ns = "aws:elasticbeanstalk:cloudwatch:logs", name = "DeleteOnTerminate", value = "false" },
    { ns = "aws:elasticbeanstalk:cloudwatch:logs", name = "RetentionInDays", value = tostring(var.log_retention_days) },
    { ns = "aws:elasticbeanstalk:application:environment", name = "PORT", value = tostring(var.app_port) },
  ]

  api_only = [
    { ns = "aws:elasticbeanstalk:environment", name = "EnvironmentType", value = "LoadBalanced" },
    { ns = "aws:elasticbeanstalk:environment", name = "LoadBalancerType", value = "application" },
    { ns = "aws:ec2:vpc", name = "ELBSubnets", value = join(",", var.public_subnet_ids) },
    { ns = "aws:ec2:vpc", name = "ELBScheme", value = "public" },
    { ns = "aws:elbv2:loadbalancer", name = "SecurityGroups", value = var.alb_security_group_id },
    { ns = "aws:elbv2:loadbalancer", name = "ManagedSecurityGroup", value = var.alb_security_group_id },
    { ns = "aws:elbv2:loadbalancer", name = "AccessLogsS3Enabled", value = "true" },
    { ns = "aws:elbv2:loadbalancer", name = "AccessLogsS3Bucket", value = var.access_logs_bucket },
    { ns = "aws:elbv2:loadbalancer", name = "AccessLogsS3Prefix", value = "alb" },
    # HTTPS only: Cloudflare connects on 443 (Full strict), port 80 is closed.
    { ns = "aws:elbv2:listener:default", name = "ListenerEnabled", value = "false" },
    { ns = "aws:elbv2:listener:443", name = "ListenerEnabled", value = "true" },
    { ns = "aws:elbv2:listener:443", name = "Protocol", value = "HTTPS" },
    { ns = "aws:elbv2:listener:443", name = "SSLCertificateArns", value = var.certificate_arn },
    { ns = "aws:elbv2:listener:443", name = "SSLPolicy", value = "ELBSecurityPolicy-TLS13-1-2-2021-06" },
    # Target group health check (SC-GS-10).
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "Port", value = tostring(var.app_port) },
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "Protocol", value = "HTTP" },
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "HealthCheckPath", value = var.health_check_path },
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "HealthCheckInterval", value = tostring(var.health_check_interval_seconds) },
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "HealthCheckTimeout", value = tostring(var.health_check_timeout_seconds) },
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "UnhealthyThresholdCount", value = tostring(var.unhealthy_threshold) },
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "HealthyThresholdCount", value = tostring(var.healthy_threshold) },
    { ns = "aws:elasticbeanstalk:environment:process:default", name = "DeregistrationDelay", value = tostring(var.deregistration_delay_seconds) },
    # Traffic splitting: 10% of traffic to the new version for 10 minutes, then promote or roll back.
    { ns = "aws:elasticbeanstalk:trafficsplitting", name = "NewVersionPercent", value = tostring(var.traffic_split_percent) },
    { ns = "aws:elasticbeanstalk:trafficsplitting", name = "EvaluationTime", value = tostring(var.traffic_split_minutes) },
  ]

  settings = concat(
    local.common,
    local.is_api ? local.api_only : [],
    [for k, v in var.environment_properties : { ns = "aws:elasticbeanstalk:application:environment", name = k, value = v }],
    [for k, v in var.secret_arns : { ns = "aws:elasticbeanstalk:application:environmentsecrets", name = k, value = v }],
  )
}

resource "aws_elastic_beanstalk_environment" "this" {
  name                = var.name
  application         = var.application_name
  solution_stack_name = var.solution_stack_name
  tier                = local.is_api ? "WebServer" : "Worker"

  dynamic "setting" {
    for_each = local.settings
    content {
      namespace = setting.value.ns
      name      = setting.value.name
      value     = setting.value.value
    }
  }

  tags = { Role = var.role }
}

output "environment_name" {
  value = aws_elastic_beanstalk_environment.this.name
}

output "environment_id" {
  value = aws_elastic_beanstalk_environment.this.id
}

output "autoscaling_group_name" {
  value = aws_elastic_beanstalk_environment.this.autoscaling_groups[0]
}

output "load_balancer_arn" {
  description = "Empty for the worker."
  value       = length(aws_elastic_beanstalk_environment.this.load_balancers) > 0 ? aws_elastic_beanstalk_environment.this.load_balancers[0] : ""
}

output "endpoint_url" {
  value = aws_elastic_beanstalk_environment.this.endpoint_url
}
