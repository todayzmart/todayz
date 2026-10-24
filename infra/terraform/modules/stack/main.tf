# One environment (staging or prod) of the D-96 shape: network, RDS, Secrets Manager, edge
# (ALB security group, logs, certificate), Beanstalk api and worker, IAM, scaling, alarms.

terraform {
  required_version = ">= 1.9"
  required_providers {
    aws = {
      source                = "hashicorp/aws"
      version               = "~> 6.0"
      configuration_aliases = [aws.dr]
    }
  }
}

data "aws_region" "current" {}
data "aws_caller_identity" "current" {}

locals {
  name = "todayz-${var.environment}"
}

module "network" {
  source            = "../network"
  name              = local.name
  cidr_block        = var.vpc_cidr_block
  nat_gateway_count = var.nat_gateway_count
}

module "secrets" {
  source                  = "../secrets"
  environment             = var.environment
  recovery_window_in_days = var.secret_recovery_window_in_days
}

# One security group for every app instance (api and worker): the database lets it in.
resource "aws_security_group" "app" {
  #checkov:skip=CKV2_AWS_5:Attached to the instances through the aws:autoscaling:launchconfiguration SecurityGroups option in modules/beanstalk_env and allowed into the database; static analysis cannot see option settings.
  name_prefix = "${local.name}-app-"
  description = "Beanstalk instances (api and worker)"
  vpc_id      = module.network.vpc_id
  tags        = { Name = "${local.name}-app" }

  lifecycle {
    create_before_destroy = true
  }
}

# Outbound: HTTPS (providers, ECR, AWS APIs through NAT or endpoints) and Postgres.
# Trivy AWS-0104: HTTPS out must reach providers whose addresses are not fixed (SMS, WhatsApp,
# Anthropic, Sarvam, Sentry); port 443 only, and the app's own egress guard allow-lists hosts.
#trivy:ignore:AWS-0104
resource "aws_vpc_security_group_egress_rule" "app_https" {
  security_group_id = aws_security_group.app.id
  description       = "HTTPS out"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "app_postgres" {
  security_group_id = aws_security_group.app.id
  description       = "Postgres"
  cidr_ipv4         = module.network.vpc_cidr
  ip_protocol       = "tcp"
  from_port         = 5432
  to_port           = 5432
}

# The ALB reaches the api instances on the app port.
resource "aws_vpc_security_group_ingress_rule" "app_from_alb" {
  security_group_id            = aws_security_group.app.id
  description                  = "api port from the ALB"
  referenced_security_group_id = module.edge.alb_security_group_id
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
}

module "database" {
  source = "../database"
  providers = {
    aws    = aws
    aws.dr = aws.dr
  }

  name                      = local.name
  vpc_id                    = module.network.vpc_id
  subnet_ids                = module.network.private_subnet_ids
  client_security_group_ids = [aws_security_group.app.id]
  instance_class            = var.db_instance_class
  multi_az                  = var.db_multi_az
  allocated_storage_gb      = var.db_allocated_storage_gb
  max_allocated_storage_gb  = var.db_max_allocated_storage_gb
  deletion_protection       = var.db_deletion_protection
  skip_final_snapshot       = var.db_skip_final_snapshot
  cross_region_copy_enabled = var.db_cross_region_copy_enabled
}

module "edge" {
  source       = "../edge"
  name         = local.name
  vpc_id       = module.network.vpc_id
  vpc_cidr     = module.network.vpc_cidr
  api_hostname = var.api_hostname
}

# ---- IAM -----------------------------------------------------------------------------

data "aws_iam_policy_document" "eb_service_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["elasticbeanstalk.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = ["elasticbeanstalk"]
    }
  }
}

resource "aws_iam_role" "eb_service" {
  name_prefix        = "${local.name}-eb-service-"
  assume_role_policy = data.aws_iam_policy_document.eb_service_assume.json
}

resource "aws_iam_role_policy_attachment" "eb_service" {
  for_each = toset([
    "arn:aws:iam::aws:policy/service-role/AWSElasticBeanstalkEnhancedHealth",
    "arn:aws:iam::aws:policy/AWSElasticBeanstalkManagedUpdatesCustomerRolePolicy",
  ])
  role       = aws_iam_role.eb_service.name
  policy_arn = each.value
}

data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

# Pull the backend image, read this environment's secrets. Nothing else is shared.
data "aws_iam_policy_document" "instance_common" {
  statement {
    sid       = "EcrLogin"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }
  statement {
    sid       = "EcrPull"
    actions   = ["ecr:BatchCheckLayerAvailability", "ecr:GetDownloadUrlForLayer", "ecr:BatchGetImage"]
    resources = [var.ecr_repository_arn]
  }
  statement {
    sid       = "ReadEnvironmentSecrets"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = values(module.secrets.secret_arns)
  }
}

# ---- api role: web tier, read-only ops policy (A-22, D-97) ----------------------------

resource "aws_iam_role" "api" {
  name_prefix        = "${local.name}-api-"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
}

resource "aws_iam_role_policy_attachment" "api_web_tier" {
  role       = aws_iam_role.api.name
  policy_arn = "arn:aws:iam::aws:policy/AWSElasticBeanstalkWebTier"
}

resource "aws_iam_role_policy" "api_common" {
  name   = "ecr-and-secrets"
  role   = aws_iam_role.api.id
  policy = data.aws_iam_policy_document.instance_common.json
}

# The policy text is a JSON file so infra/terraform/checks/check.py can test it against the
# action allow-list (SC-OPS-05). The ARNs are templated into Resource only.
resource "aws_iam_role_policy" "api_ops_readonly" {
  name = "ops-readonly"
  role = aws_iam_role.api.id
  policy = templatefile("${path.module}/policies/api-ops-readonly.json", {
    rds_arn = module.database.arn
    pi_arn  = "arn:aws:pi:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:metrics/rds/${module.database.resource_id}"
  })
}

# SMS (D-212): both processes send through AWS End User Messaging with the instance role
# (OTPs from the api, notices from the worker), only from the DLT sender ID. The JSON file is
# checked by infra/terraform/checks/check.py (sms). No sender ID set: no SMS permission.
resource "aws_iam_role_policy" "api_sms_send" {
  count  = var.sms_sender_id == "" ? 0 : 1
  name   = "sms-send"
  role   = aws_iam_role.api.id
  policy = local.sms_send_policy
}

resource "aws_iam_instance_profile" "api" {
  name_prefix = "${local.name}-api-"
  role        = aws_iam_role.api.name
}

# ---- worker role: worker tier, PutMetricData limited to TodayZ/Worker -----------------

resource "aws_iam_role" "worker" {
  name_prefix        = "${local.name}-worker-"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
}

resource "aws_iam_role_policy_attachment" "worker_tier" {
  role       = aws_iam_role.worker.name
  policy_arn = "arn:aws:iam::aws:policy/AWSElasticBeanstalkWorkerTier"
}

resource "aws_iam_role_policy" "worker_common" {
  name   = "ecr-and-secrets"
  role   = aws_iam_role.worker.id
  policy = data.aws_iam_policy_document.instance_common.json
}

resource "aws_iam_role_policy" "worker_metrics" {
  name   = "put-worker-metrics"
  role   = aws_iam_role.worker.id
  policy = file("${path.module}/policies/worker-metrics.json")
}

resource "aws_iam_role_policy" "worker_sms_send" {
  count  = var.sms_sender_id == "" ? 0 : 1
  name   = "sms-send"
  role   = aws_iam_role.worker.id
  policy = local.sms_send_policy
}

resource "aws_iam_instance_profile" "worker" {
  name_prefix = "${local.name}-worker-"
  role        = aws_iam_role.worker.name
}

# ---- Beanstalk -----------------------------------------------------------------------

data "aws_elastic_beanstalk_solution_stack" "docker_al2023" {
  most_recent = true
  name_regex  = "^64bit Amazon Linux 2023 (.*) running Docker$"
}

resource "aws_elastic_beanstalk_application" "this" {
  name        = local.name
  description = "TodayZ ${var.environment}: api and worker"
}

locals {
  sms_send_policy = templatefile("${path.module}/policies/sms-send.json", {
    region     = data.aws_region.current.region
    account_id = data.aws_caller_identity.current.account_id
    sender_id  = var.sms_sender_id
  })
  # SMS (D-212): the SDK signs in AWS_REGION with the instance role; the DLT ids are not
  # secrets. SMS_API_URL is never set deployed (the regional endpoint is the default). An
  # unset value is left out (the app's preflight then names it) rather than set to "".
  sms_properties = { for k, v in {
    AWS_REGION              = data.aws_region.current.region
    SMS_SENDER_ID           = var.sms_sender_id
    SMS_DLT_ENTITY_ID       = var.sms_dlt_entity_id
    SMS_OTP_TEMPLATE_ID     = var.sms_otp_template_id
    SMS_NOTICE_TEMPLATE_IDS = var.sms_notice_template_ids
  } : k => v if v != "" }
  # Non-secret configuration both processes share (names from backend/CLAUDE.md).
  common_properties = merge(local.sms_properties, {
    APP_ENV                    = var.environment
    API_PUBLIC_URL             = "https://${var.api_hostname}"
    LOG_LEVEL                  = "info"
    CONTAINER_IMAGE_REPOSITORY = var.ecr_repository_url
  })
  api_properties = merge(local.common_properties, {
    API_ADDR                       = ":8080"
    SHUTDOWN_PRESTOP               = "${local.api_prestop_seconds}s"
    SHUTDOWN_DRAIN                 = "${local.api_drain_seconds}s"
    CONTAINER_STOP_TIMEOUT_SECONDS = tostring(local.api_stop_timeout_seconds)
  })
  worker_properties = merge(local.common_properties, {
    WORKER_ADDR                    = ":8081"
    CONTAINER_STOP_TIMEOUT_SECONDS = tostring(local.worker_stop_timeout_seconds)
  })
}

module "api" {
  source = "../beanstalk_env"

  name                        = "${local.name}-api"
  role                        = "api"
  application_name            = aws_elastic_beanstalk_application.this.name
  solution_stack_name         = data.aws_elastic_beanstalk_solution_stack.docker_al2023.name
  vpc_id                      = module.network.vpc_id
  private_subnet_ids          = module.network.private_subnet_ids
  public_subnet_ids           = module.network.public_subnet_ids
  instance_type               = var.api_instance_type
  min_size                    = var.api_min_size
  max_size                    = var.api_max_size
  service_role_arn            = aws_iam_role.eb_service.arn
  instance_profile_name       = aws_iam_instance_profile.api.name
  instance_security_group_ids = [aws_security_group.app.id]
  alb_security_group_id       = module.edge.alb_security_group_id
  certificate_arn             = module.edge.certificate_arn
  access_logs_bucket          = module.edge.access_logs_bucket
  app_port                    = 8080
  deployment_policy           = var.api_deployment_policy

  health_check_interval_seconds = local.health_check_interval_seconds
  health_check_timeout_seconds  = local.health_check_timeout_seconds
  unhealthy_threshold           = local.unhealthy_threshold
  healthy_threshold             = local.healthy_threshold
  deregistration_delay_seconds  = local.deregistration_delay_seconds

  environment_properties = local.api_properties
  secret_arns            = module.secrets.secret_arns
}

module "worker" {
  source = "../beanstalk_env"

  name                        = "${local.name}-worker"
  role                        = "worker"
  application_name            = aws_elastic_beanstalk_application.this.name
  solution_stack_name         = data.aws_elastic_beanstalk_solution_stack.docker_al2023.name
  vpc_id                      = module.network.vpc_id
  private_subnet_ids          = module.network.private_subnet_ids
  instance_type               = var.worker_instance_type
  min_size                    = var.worker_min_size
  max_size                    = var.worker_max_size
  service_role_arn            = aws_iam_role.eb_service.arn
  instance_profile_name       = aws_iam_instance_profile.worker.name
  instance_security_group_ids = [aws_security_group.app.id]
  app_port                    = 8081
  deployment_policy           = var.worker_deployment_policy

  environment_properties = local.worker_properties
  secret_arns            = module.secrets.secret_arns
}

# ---- Scaling -------------------------------------------------------------------------
# TODO(lead): verify in the first Wave 5 plan that Beanstalk tags its target group with
# elasticbeanstalk:environment-name (the lookup below relies on it).

data "aws_lb" "api" {
  arn = module.api.load_balancer_arn
}

data "aws_lb_target_group" "api" {
  tags = {
    "elasticbeanstalk:environment-name" = module.api.environment_name
  }
  depends_on = [module.api]
}

resource "aws_autoscaling_policy" "api_requests" {
  name                   = "${local.name}-api-requests-per-target"
  autoscaling_group_name = module.api.autoscaling_group_name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    target_value = var.requests_per_target_per_minute
    predefined_metric_specification {
      predefined_metric_type = "ALBRequestCountPerTarget"
      resource_label         = "${data.aws_lb.api.arn_suffix}/${data.aws_lb_target_group.api.arn_suffix}"
    }
  }
}

resource "aws_autoscaling_policy" "api_cpu" {
  name                   = "${local.name}-api-cpu"
  autoscaling_group_name = module.api.autoscaling_group_name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    target_value = var.cpu_target_percent
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }
  }
}

# Worker: step scaling on the age of the oldest available River job (TodayZ/Worker).
resource "aws_autoscaling_policy" "worker_out" {
  name                   = "${local.name}-worker-scale-out"
  autoscaling_group_name = module.worker.autoscaling_group_name
  policy_type            = "StepScaling"
  adjustment_type        = "ChangeInCapacity"

  step_adjustment {
    metric_interval_lower_bound = 0
    metric_interval_upper_bound = 60
    scaling_adjustment          = 1
  }
  step_adjustment {
    metric_interval_lower_bound = 60
    scaling_adjustment          = 2
  }
}

resource "aws_autoscaling_policy" "worker_in" {
  name                   = "${local.name}-worker-scale-in"
  autoscaling_group_name = module.worker.autoscaling_group_name
  policy_type            = "StepScaling"
  adjustment_type        = "ChangeInCapacity"

  step_adjustment {
    metric_interval_upper_bound = 0
    scaling_adjustment          = -1
  }
}

resource "aws_cloudwatch_metric_alarm" "worker_queue_age_high" {
  alarm_name          = "${local.name}-worker-oldest-job-age-high"
  alarm_description   = "Oldest available River job older than ${var.worker_scale_out_age_seconds}s for 3 minutes: scale the worker out."
  namespace           = "TodayZ/Worker"
  metric_name         = var.worker_oldest_job_age_metric_name
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 3
  threshold           = var.worker_scale_out_age_seconds
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_autoscaling_policy.worker_out.arn, aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "worker_queue_age_low" {
  alarm_name          = "${local.name}-worker-oldest-job-age-low"
  alarm_description   = "Queue age back under 10s for 15 minutes: scale the worker in."
  namespace           = "TodayZ/Worker"
  metric_name         = var.worker_oldest_job_age_metric_name
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 15
  threshold           = 10
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_autoscaling_policy.worker_in.arn]
}
