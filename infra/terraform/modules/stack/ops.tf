# One-off ops tasks on ECS Fargate (D-254): the database bootstrap, `todayz migrate up` and the
# item-photo picks (D-265). They
# run inside the VPC with the app security group (the database lets it in), so nobody needs a
# bastion or a public database. scripts/deploy/beanstalk.sh starts them and waits for the exit
# code; the cluster costs nothing while no task runs.

locals {
  ops_name = "${local.name}-ops"
  # The migrate task loads the full deployed config (app keys, DATABASE_URL) and connects with
  # DATABASE_MIGRATE_URL as todayz_owner.
  migrate_secret_arns = merge(
    { for n in module.secrets.app_key_names : n => module.secrets.secret_arns[n] },
    {
      DATABASE_URL         = module.secrets.secret_arns["DATABASE_URL"]
      DATABASE_MIGRATE_URL = module.secrets.secret_arns["DATABASE_MIGRATE_URL"]
    },
  )
  # The images task (D-265) runs as todayz_app with the app keys, DATABASE_URL and the R2 keys:
  # `images types --picks builtin` copies the picked photos into the buckets.
  images_secret_arns = {
    for n in concat(module.secrets.app_key_names, ["DATABASE_URL", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY"]) :
    n => module.secrets.secret_arns[n]
  }
  ops_log_options = {
    awslogs-group         = aws_cloudwatch_log_group.ops.name
    awslogs-region        = data.aws_region.current.region
    awslogs-stream-prefix = "ops"
  }
}

resource "aws_ecs_cluster" "ops" {
  name = local.ops_name

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_cloudwatch_log_group" "ops" {
  name              = "/todayz/${var.environment}/ops"
  retention_in_days = var.ops_log_retention_days
  #checkov:skip=CKV_AWS_158:Log groups use the AWS-managed key; a CMK adds cost for no threat-model gain at launch.
  #checkov:skip=CKV_AWS_338:Retention is a variable (30 days); one year is not wanted for one-off task logs.
}

data "aws_iam_policy_document" "ecs_tasks_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }
}

# The execution role pulls the images, writes the logs and reads only the secrets the tasks
# are given. The tasks themselves call no AWS API, so they have no task role.
resource "aws_iam_role" "ops_execution" {
  name_prefix        = "${local.name}-ops-exec-"
  assume_role_policy = data.aws_iam_policy_document.ecs_tasks_assume.json
}

resource "aws_iam_role_policy_attachment" "ops_execution" {
  role       = aws_iam_role.ops_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

data "aws_iam_policy_document" "ops_execution_secrets" {
  statement {
    sid       = "ReadOpsSecrets"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = distinct(concat(values(local.migrate_secret_arns), values(local.images_secret_arns), [module.database.master_secret_arn]))
  }
}

resource "aws_iam_role_policy" "ops_execution_secrets" {
  name   = "ops-secrets"
  role   = aws_iam_role.ops_execution.id
  policy = data.aws_iam_policy_document.ops_execution_secrets.json
}

resource "aws_ecs_task_definition" "db_bootstrap" {
  family                   = "${local.name}-db-bootstrap"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ops_execution.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  container_definitions = jsonencode([{
    name                   = "db-bootstrap"
    image                  = "public.ecr.aws/docker/library/postgres:16-alpine"
    essential              = true
    readonlyRootFilesystem = true
    command                = ["sh", "-c", "printf '%s' \"$BOOTSTRAP_B64\" | base64 -d | sh"]
    environment = [
      { name = "PGHOST", value = module.database.endpoint },
      { name = "PGPORT", value = tostring(module.database.port) },
      { name = "PGDATABASE", value = "todayz" },
      { name = "PGSSLMODE", value = "require" },
      { name = "BOOTSTRAP_B64", value = filebase64("${path.module}/ops/db-bootstrap.sh") },
    ]
    secrets = [
      { name = "PGUSER", valueFrom = "${module.database.master_secret_arn}:username::" },
      { name = "PGPASSWORD", valueFrom = "${module.database.master_secret_arn}:password::" },
      { name = "DATABASE_URL", valueFrom = module.secrets.secret_arns["DATABASE_URL"] },
      { name = "DATABASE_MIGRATE_URL", valueFrom = module.secrets.secret_arns["DATABASE_MIGRATE_URL"] },
    ]
    logConfiguration = { logDriver = "awslogs", options = local.ops_log_options }
  }])
}

# Terraform registers revision 1 on the first release's image; every release after it registers
# a new revision on its own image (scripts/deploy/beanstalk.sh migrate), so the image drift is
# expected and ignored.
resource "aws_ecs_task_definition" "migrate" {
  family                   = "${local.name}-migrate"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ops_execution.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  container_definitions = jsonencode([{
    name                   = "migrate"
    image                  = "${var.ecr_repository_url}:${var.initial_image_tag == "" ? "unreleased" : var.initial_image_tag}"
    essential              = true
    readonlyRootFilesystem = true
    command                = ["migrate", "up"]
    environment = [
      { name = "APP_ENV", value = var.environment },
      { name = "API_PUBLIC_URL", value = "https://${var.api_hostname}" },
      { name = "LOG_LEVEL", value = "info" },
    ]
    secrets          = [for k, v in local.migrate_secret_arns : { name = k, valueFrom = v }]
    logConfiguration = { logDriver = "awslogs", options = local.ops_log_options }
  }])

  lifecycle {
    ignore_changes = [container_definitions]
  }
}

# `images types --picks builtin` (D-265), once after the reference seed and again whenever the
# pick list grows (scripts/deploy/beanstalk.sh images-picks). Like migrate, each run registers a
# revision on the release's image; the rest is copied, so the R2 settings must be known at the
# first apply (r2_account_id).
resource "aws_ecs_task_definition" "images" {
  family                   = "${local.name}-images"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ops_execution.arn

  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  container_definitions = jsonencode([{
    name                   = "images"
    image                  = "${var.ecr_repository_url}:${var.initial_image_tag == "" ? "unreleased" : var.initial_image_tag}"
    essential              = true
    readonlyRootFilesystem = true
    command                = ["images", "types", "--picks", "builtin"]
    environment = concat([
      { name = "APP_ENV", value = var.environment },
      { name = "API_PUBLIC_URL", value = "https://${var.api_hostname}" },
      { name = "LOG_LEVEL", value = "info" },
    ], [for k, v in local.extra_properties : { name = k, value = v } if startswith(k, "R2_")])
    secrets          = [for k, v in local.images_secret_arns : { name = k, valueFrom = v }]
    logConfiguration = { logDriver = "awslogs", options = local.ops_log_options }
  }])

  lifecycle {
    ignore_changes = [container_definitions]
  }
}

# Everything a script needs to start a task, in one place it can read without Terraform.
resource "aws_ssm_parameter" "ops_run_task" {
  #checkov:skip=CKV2_AWS_34:Not a secret: cluster, subnet and security group ids and task families.
  name        = "/todayz/${var.environment}/ops/run-task"
  description = "ECS run-task settings for the ops tasks (scripts/deploy/beanstalk.sh)"
  type        = "String"
  value = jsonencode({
    cluster           = aws_ecs_cluster.ops.name
    subnets           = module.network.private_subnet_ids
    securityGroups    = [aws_security_group.app.id]
    dbBootstrap       = aws_ecs_task_definition.db_bootstrap.family
    migrate           = aws_ecs_task_definition.migrate.family
    images            = aws_ecs_task_definition.images.family
    logGroup          = aws_cloudwatch_log_group.ops.name
    application       = aws_elastic_beanstalk_application.this.name
    apiEnvironment    = "${local.name}-api"
    workerEnvironment = "${local.name}-worker"
    versionsBucket    = aws_s3_bucket.app_versions.id
    imageRepository   = var.ecr_repository_url
  })
}
