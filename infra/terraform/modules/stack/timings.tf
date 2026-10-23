# Graceful-shutdown and health-check timings (D-98, 09 §3.3, §3.7). Plain literals on purpose:
# infra/terraform/checks/check.py reads this file and fails if a value drifts (SC-GS-10).
# Keep one `name = number` per line.

locals {
  # ALB / target group
  deregistration_delay_seconds  = 75
  health_check_interval_seconds = 5
  health_check_timeout_seconds  = 4
  unhealthy_threshold           = 2
  healthy_threshold             = 2

  # Process shutdown (backend SHUTDOWN_PRESTOP / SHUTDOWN_DRAIN, Go durations)
  api_prestop_seconds = 15
  api_drain_seconds   = 75

  # Container stop timeouts. Beanstalk's Docker platform takes them from the application
  # bundle (compose stop_grace_period), packaged by W5-04; they are passed to the instances
  # as CONTAINER_STOP_TIMEOUT_SECONDS so the bundle and this file cannot disagree.
  api_stop_timeout_seconds    = 120
  worker_stop_timeout_seconds = 180

  # Worker: River DrainGrace and the StopAndCancel window (backend queue.DrainGrace, HardGrace)
  # Read by infra/terraform/checks/check.py (SC-GS-10), not by Terraform.
  # tflint-ignore: terraform_unused_declarations
  worker_drain_grace_seconds = 90
  # tflint-ignore: terraform_unused_declarations
  worker_hard_cancel_seconds = 30
}
