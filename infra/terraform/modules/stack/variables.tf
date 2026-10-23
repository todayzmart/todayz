variable "environment" {
  type        = string
  description = "staging or prod."

  validation {
    condition     = contains(["staging", "prod"], var.environment)
    error_message = "environment must be staging or prod."
  }
}

variable "api_hostname" {
  type        = string
  description = "Public API host (placeholder domain: api.todayz.in, or api-staging.todayz.in)."
}

variable "ecr_repository_arn" {
  type = string
}

variable "ecr_repository_url" {
  type = string
}

variable "vpc_cidr_block" {
  type    = string
  default = "10.20.0.0/16"
}

variable "nat_gateway_count" {
  type    = number
  default = 1
}

# ---- Compute -------------------------------------------------------------------------

variable "api_instance_type" {
  type    = string
  default = "t4g.small"
}

variable "api_min_size" {
  type    = number
  default = 2
}

variable "api_max_size" {
  type    = number
  default = 6
}

variable "worker_instance_type" {
  type    = string
  default = "t4g.small"
}

variable "worker_min_size" {
  type    = number
  default = 1
}

variable "worker_max_size" {
  type    = number
  default = 3
}

variable "requests_per_target_per_minute" {
  type        = number
  default     = 1000
  description = "Target-tracking target for ALBRequestCountPerTarget; tuned from the k6 results (W4-02)."
}

variable "cpu_target_percent" {
  type    = number
  default = 60
}

variable "api_deployment_policy" {
  type    = string
  default = "TrafficSplitting"
}

variable "worker_deployment_policy" {
  type    = string
  default = "Immutable"
}

# ---- Database ------------------------------------------------------------------------

variable "db_instance_class" {
  type    = string
  default = "db.t4g.medium"
}

variable "db_multi_az" {
  type    = bool
  default = false
}

variable "db_allocated_storage_gb" {
  type    = number
  default = 100
}

variable "db_max_allocated_storage_gb" {
  type    = number
  default = 500
}

variable "db_deletion_protection" {
  type    = bool
  default = true
}

variable "db_skip_final_snapshot" {
  type    = bool
  default = false
}

variable "db_cross_region_copy_enabled" {
  type    = bool
  default = true
}

variable "secret_recovery_window_in_days" {
  type    = number
  default = 30
}

# ---- Alarms --------------------------------------------------------------------------

variable "alarm_email" {
  type        = string
  default     = ""
  description = "Founders' alert address; empty creates the topic without a subscription."
}

variable "worker_oldest_job_age_metric_name" {
  type        = string
  default     = "OldestAvailableJobAgeSeconds"
  description = "TODO(lead): metric name is owned by W5-04 (the worker publishes it into TodayZ/Worker)."
}

variable "worker_scale_out_age_seconds" {
  type    = number
  default = 30
}
