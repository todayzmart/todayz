# CloudWatch alarms. They notify an SNS topic the founders subscribe to; Grafana also shows
# their state (A-22). Each alarm's description names its runbook in docs/runbooks/.

# Trivy AWS-0136: the AWS-managed SNS key is used; a CMK adds cost for no threat-model gain (as the database and secrets modules).
#trivy:ignore:AWS-0136
resource "aws_sns_topic" "alarms" {
  name              = "${local.name}-alarms"
  kms_master_key_id = "alias/aws/sns"
}

resource "aws_sns_topic_subscription" "email" {
  count     = var.alarm_email == "" ? 0 : 1
  topic_arn = aws_sns_topic.alarms.arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

locals {
  alarm_actions = [aws_sns_topic.alarms.arn]
  lb_suffix     = one(data.aws_lb.api[*].arn_suffix)
  tg_suffix     = one(data.aws_lb_target_group.api[*].arn_suffix)
}

resource "aws_cloudwatch_metric_alarm" "rds_cpu" {
  alarm_name          = "${local.name}-rds-cpu-high"
  alarm_description   = "RDS CPU above 80% for 15 minutes. Runbook: docs/runbooks/rds-cpu.md (upgrade to db.m7g.large at a sustained 60%)."
  namespace           = "AWS/RDS"
  metric_name         = "CPUUtilization"
  dimensions          = { DBInstanceIdentifier = module.database.identifier }
  statistic           = "Average"
  period              = 300
  evaluation_periods  = 3
  threshold           = 80
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "missing"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "rds_free_storage" {
  alarm_name          = "${local.name}-rds-free-storage-low"
  alarm_description   = "RDS free storage under 10 GB. Runbook: docs/runbooks/rds-storage.md."
  namespace           = "AWS/RDS"
  metric_name         = "FreeStorageSpace"
  dimensions          = { DBInstanceIdentifier = module.database.identifier }
  statistic           = "Minimum"
  period              = 300
  evaluation_periods  = 2
  threshold           = 10 * 1024 * 1024 * 1024
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "rds_burst_balance" {
  alarm_name          = "${local.name}-rds-cpu-credits-low"
  alarm_description   = "RDS CPU credit balance draining on a burstable class. Runbook: docs/runbooks/rds-cpu.md."
  namespace           = "AWS/RDS"
  metric_name         = "CPUCreditBalance"
  dimensions          = { DBInstanceIdentifier = module.database.identifier }
  statistic           = "Minimum"
  period              = 300
  evaluation_periods  = 3
  threshold           = 30
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "missing"
  alarm_actions       = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "alb_5xx" {
  count               = local.envs
  alarm_name          = "${local.name}-alb-target-5xx"
  alarm_description   = "More than 10 target 5xx responses a minute for 5 minutes. Runbook: docs/runbooks/api-5xx.md."
  namespace           = "AWS/ApplicationELB"
  metric_name         = "HTTPCode_Target_5XX_Count"
  dimensions          = { LoadBalancer = local.lb_suffix }
  statistic           = "Sum"
  period              = 60
  evaluation_periods  = 5
  threshold           = 10
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "alb_unhealthy_hosts" {
  count               = local.envs
  alarm_name          = "${local.name}-alb-unhealthy-hosts"
  alarm_description   = "At least one api target failing /readyz for 3 minutes. Runbook: docs/runbooks/api-unhealthy.md."
  namespace           = "AWS/ApplicationELB"
  metric_name         = "UnHealthyHostCount"
  dimensions          = { LoadBalancer = local.lb_suffix, TargetGroup = local.tg_suffix }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 3
  threshold           = 0
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "alb_latency_p95" {
  count               = local.envs
  alarm_name          = "${local.name}-alb-latency-p95-high"
  alarm_description   = "p95 target response time above 1.5 s for 10 minutes. Runbook: docs/runbooks/api-latency.md."
  namespace           = "AWS/ApplicationELB"
  metric_name         = "TargetResponseTime"
  dimensions          = { LoadBalancer = local.lb_suffix }
  extended_statistic  = "p95"
  period              = 60
  evaluation_periods  = 10
  threshold           = 1.5
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "eb_api_health" {
  count               = local.envs
  alarm_name          = "${local.name}-api-environment-health"
  alarm_description   = "Beanstalk api environment health worse than Ok (EnvironmentHealth above 15). Runbook: docs/runbooks/api-unhealthy.md."
  namespace           = "AWS/ElasticBeanstalk"
  metric_name         = "EnvironmentHealth"
  dimensions          = { EnvironmentName = module.api[0].environment_name }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 5
  threshold           = 15
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "eb_worker_health" {
  count               = local.envs
  alarm_name          = "${local.name}-worker-environment-health"
  alarm_description   = "Beanstalk worker environment health worse than Ok. Runbook: docs/runbooks/worker-unhealthy.md."
  namespace           = "AWS/ElasticBeanstalk"
  metric_name         = "EnvironmentHealth"
  dimensions          = { EnvironmentName = module.worker[0].environment_name }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 5
  threshold           = 15
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "worker_queue_age_stuck" {
  alarm_name          = "${local.name}-worker-oldest-job-age-critical"
  alarm_description   = "Oldest available job older than 5 minutes for 5 minutes (the worker is not keeping up). Runbook: docs/runbooks/job-queue-backlog.md."
  namespace           = "TodayZ/Worker"
  metric_name         = var.worker_oldest_job_age_metric_name
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 5
  threshold           = 300
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
  ok_actions          = local.alarm_actions
}
