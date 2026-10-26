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

# ---- SMS: AWS End User Messaging, India DLT (D-212) ----------------------------------
# Not secrets. Values come from the founder's DLT registration
# (docs/runbooks/aws-founder-steps.md part C). Empty sender ID: no SMS permission.

variable "sms_sender_id" {
  type        = string
  default     = ""
  description = "The DLT-registered sender ID (header), e.g. TODAYZ."

  validation {
    condition     = can(regex("^([A-Za-z0-9]{1,11})?$", var.sms_sender_id))
    error_message = "sms_sender_id must be 1 to 11 letters or digits (or empty)."
  }
}

variable "sms_dlt_entity_id" {
  type        = string
  default     = ""
  description = "The DLT principal entity id (IN_ENTITY_ID)."

  validation {
    condition     = can(regex("^[0-9]{0,32}$", var.sms_dlt_entity_id))
    error_message = "sms_dlt_entity_id must be a numeric DLT id (or empty)."
  }
}

variable "sms_otp_template_id" {
  type        = string
  default     = ""
  description = "The DLT template id of the login OTP SMS (IN_TEMPLATE_ID)."

  validation {
    condition     = can(regex("^[0-9]{0,32}$", var.sms_otp_template_id))
    error_message = "sms_otp_template_id must be a numeric DLT id (or empty)."
  }
}

variable "sms_notice_template_ids" {
  type        = string
  default     = ""
  description = "name=id pairs for the notice SMS templates, e.g. tz_order_unseen=<id>,tz_order_unseen_hi=<id>."
}

# ---- First deploy and releases (D-254) -----------------------------------------------

variable "app_environments_enabled" {
  type        = bool
  default     = true
  description = "false on the first apply only: everything but the Beanstalk environments (and the scaling, alarms and certificate wait that read them). See docs/qa/deploy-prep/RUNBOOK.md."
}

variable "initial_image_tag" {
  type        = string
  default     = ""
  description = "Image tag of the first release; the environments are created on application versions <tag>-api and <tag>-worker (scripts/deploy/beanstalk.sh release). Later deploys go through the script, not Terraform."
}

variable "provider_secret_names" {
  type        = list(string)
  default     = []
  description = "Provider keys that have a value in Secrets Manager and are injected into both environments. Add a name only after `scripts/deploy/secrets.sh put NAME`."

  validation {
    condition = alltrue([for n in var.provider_secret_names : contains([
      "ANTHROPIC_API_KEY", "SARVAM_API_KEY", "TURNSTILE_SECRET_KEY", "R2_ACCESS_KEY_ID",
      "R2_SECRET_ACCESS_KEY", "SENTRY_DSN", "VAPID_PRIVATE_KEY", "WHATSAPP_API_KEY",
      "OTEL_EXPORTER_OTLP_HEADERS", "GEOCODER_API_KEY",
    ], n)])
    error_message = "provider_secret_names holds only provider key names from modules/secrets (not the app keys or the database URLs)."
  }
}

variable "extra_properties" {
  type        = map(string)
  default     = {}
  description = "Non-secret environment properties for both processes (R2_*, TURNSTILE_SITE_KEY, VAPID_PUBLIC_KEY, OTEL_EXPORTER_OTLP_ENDPOINT, ...). Empty values are left out."
}

variable "github_oidc_provider_arn" {
  type        = string
  default     = ""
  description = "envs/account output github_oidc_provider_arn. Empty: no GitHub deploy role."
}

variable "github_repository" {
  type        = string
  default     = "todayzmart/todayz"
  description = "owner/name of the repository whose production environment may assume the deploy role."
}

variable "ops_log_retention_days" {
  type    = number
  default = 30
}
