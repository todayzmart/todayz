# RDS PostgreSQL 16 (+ PostGIS, created at bootstrap), single instance by default (D-96).
# multi_az is one flag. Also: weekly cross-region snapshot copy through AWS Backup.

terraform {
  required_providers {
    aws = {
      source                = "hashicorp/aws"
      version               = "~> 6.0"
      configuration_aliases = [aws.dr]
    }
  }
}

variable "name" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "subnet_ids" {
  type = list(string)
}

variable "client_security_group_ids" {
  type        = list(string)
  description = "Security groups allowed to reach Postgres (Beanstalk instances)."
}

variable "instance_class" {
  type    = string
  default = "db.t4g.medium"
}

variable "multi_az" {
  type        = bool
  default     = false
  description = "D-96: Single-AZ at launch. Flip to true in a maintenance window when the founder decides."
}

variable "allocated_storage_gb" {
  type    = number
  default = 100
}

variable "max_allocated_storage_gb" {
  type    = number
  default = 500
}

variable "backup_retention_days" {
  type    = number
  default = 14
}

variable "deletion_protection" {
  type    = bool
  default = true
}

variable "skip_final_snapshot" {
  type    = bool
  default = false
}

variable "cross_region_copy_enabled" {
  type    = bool
  default = true
}

variable "dr_region_label" {
  type        = string
  default     = "ap-southeast-1"
  description = "Only used in names and tags; the region itself comes from the aws.dr provider."
}

resource "aws_db_subnet_group" "this" {
  name       = var.name
  subnet_ids = var.subnet_ids
}

resource "aws_security_group" "db" {
  name_prefix = "${var.name}-db-"
  description = "Postgres from the app instances only"
  vpc_id      = var.vpc_id
  tags        = { Name = "${var.name}-db" }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "from_app" {
  count                        = length(var.client_security_group_ids)
  security_group_id            = aws_security_group.db.id
  description                  = "Postgres from app instances"
  referenced_security_group_id = var.client_security_group_ids[count.index]
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}

resource "aws_db_parameter_group" "this" {
  name_prefix = "${var.name}-pg16-"
  family      = "postgres16"
  description = "TodayZ Postgres 16: pg_stat_statements, slow-query log, idle-in-transaction timeout"

  parameter {
    name         = "shared_preload_libraries"
    value        = "pg_stat_statements"
    apply_method = "pending-reboot"
  }
  parameter {
    name  = "log_min_duration_statement"
    value = "500"
  }
  parameter {
    name  = "idle_in_transaction_session_timeout"
    value = "60000"
  }
  parameter {
    name  = "rds.force_ssl"
    value = "1"
  }
  parameter {
    name  = "log_connections"
    value = "1"
  }
  parameter {
    name  = "log_disconnections"
    value = "1"
  }

  lifecycle {
    create_before_destroy = true
  }
}

data "aws_iam_policy_document" "monitoring_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["monitoring.rds.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "monitoring" {
  name_prefix        = "${var.name}-rds-mon-"
  assume_role_policy = data.aws_iam_policy_document.monitoring_assume.json
}

resource "aws_iam_role_policy_attachment" "monitoring" {
  role       = aws_iam_role.monitoring.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonRDSEnhancedMonitoringRole"
}

resource "aws_db_instance" "this" {
  identifier = var.name
  engine     = "postgres"
  # Major version only: RDS picks the default 16.x minor and auto-upgrades minors.
  engine_version             = "16"
  auto_minor_version_upgrade = true
  instance_class             = var.instance_class
  db_name                    = "todayz"
  username                   = "todayz"
  # The master password is generated and kept by RDS in Secrets Manager: never in config or state.
  manage_master_user_password = true

  storage_type          = "gp3"
  allocated_storage     = var.allocated_storage_gb
  max_allocated_storage = var.max_allocated_storage_gb
  storage_encrypted     = true

  multi_az               = var.multi_az
  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]
  publicly_accessible    = false
  parameter_group_name   = aws_db_parameter_group.this.name

  backup_retention_period   = var.backup_retention_days # PITR
  backup_window             = "20:30-21:30"             # 02:00-03:00 IST
  maintenance_window        = "sun:21:30-sun:22:30"
  copy_tags_to_snapshot     = true
  deletion_protection       = var.deletion_protection
  skip_final_snapshot       = var.skip_final_snapshot
  final_snapshot_identifier = var.skip_final_snapshot ? null : "${var.name}-final"

  # Database Insights, standard mode (needs Performance Insights on, 7-day retention).
  database_insights_mode                = "standard"
  performance_insights_enabled          = true
  performance_insights_retention_period = 7

  monitoring_interval             = 60
  monitoring_role_arn             = aws_iam_role.monitoring.arn
  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]

  iam_database_authentication_enabled = true

  #checkov:skip=CKV_AWS_157:Single-AZ is the founder's decision (D-96); multi_az is a variable.
  #checkov:skip=CKV_AWS_354:Performance Insights uses the default AWS-managed key.
}

# ---- Weekly cross-region snapshot copy (AWS Backup) -----------------------------------

resource "aws_backup_vault" "primary" {
  count = var.cross_region_copy_enabled ? 1 : 0
  name  = "${var.name}-weekly"
  #checkov:skip=CKV_AWS_166:The AWS-managed backup key is used.
}

resource "aws_backup_vault" "dr" {
  count    = var.cross_region_copy_enabled ? 1 : 0
  provider = aws.dr
  name     = "${var.name}-weekly-dr"
  tags     = { Region = var.dr_region_label }
  #checkov:skip=CKV_AWS_166:The AWS-managed backup key is used.
}

resource "aws_backup_plan" "weekly" {
  count = var.cross_region_copy_enabled ? 1 : 0
  name  = "${var.name}-weekly-copy"

  rule {
    rule_name         = "weekly-snapshot-and-copy"
    target_vault_name = aws_backup_vault.primary[0].name
    schedule          = "cron(0 21 ? * SUN *)" # Sunday 21:00 UTC, Monday 02:30 IST

    lifecycle {
      delete_after = 35
    }

    copy_action {
      destination_vault_arn = aws_backup_vault.dr[0].arn
      lifecycle {
        delete_after = 35
      }
    }
  }
}

data "aws_iam_policy_document" "backup_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["backup.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "backup" {
  count              = var.cross_region_copy_enabled ? 1 : 0
  name_prefix        = "${var.name}-backup-"
  assume_role_policy = data.aws_iam_policy_document.backup_assume.json
}

resource "aws_iam_role_policy_attachment" "backup" {
  count      = var.cross_region_copy_enabled ? 1 : 0
  role       = aws_iam_role.backup[0].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSBackupServiceRolePolicyForBackup"
}

resource "aws_backup_selection" "db" {
  count        = var.cross_region_copy_enabled ? 1 : 0
  name         = "${var.name}-db"
  iam_role_arn = aws_iam_role.backup[0].arn
  plan_id      = aws_backup_plan.weekly[0].id
  resources    = [aws_db_instance.this.arn]
}

output "endpoint" {
  value = aws_db_instance.this.address
}

output "port" {
  value = aws_db_instance.this.port
}

output "arn" {
  value = aws_db_instance.this.arn
}

output "identifier" {
  value = aws_db_instance.this.identifier
}

output "resource_id" {
  description = "DbiResourceId, used in the pi: ARNs."
  value       = aws_db_instance.this.resource_id
}

output "master_secret_arn" {
  value = aws_db_instance.this.master_user_secret[0].secret_arn
}

output "multi_az" {
  value = aws_db_instance.this.multi_az
}
