# Cloudflare for one environment (prod or staging): DNS, TLS settings, WAF, rate limits, R2,
# Turnstile, cache rules, Pages projects. Nothing here has been planned or applied (Wave 5).
#
# Credentials: CLOUDFLARE_API_TOKEN in the environment (read by the provider; never written
# here). The account and zone ids are variables.
# State: the backend block is a placeholder, filled in Wave 5.

terraform {
  required_version = ">= 1.9"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
  }

  # backend "s3" {}   # Wave 5: bucket, key, region via -backend-config; never committed.
}

provider "cloudflare" {}

variable "environment" {
  type        = string
  default     = "prod"
  description = "prod or staging."

  validation {
    condition     = contains(["prod", "staging"], var.environment)
    error_message = "environment must be prod or staging."
  }
}

variable "account_id" {
  type        = string
  description = "Cloudflare account id (CLOUDFLARE_ACCOUNT_ID)."
}

variable "zone_id" {
  type        = string
  description = "Zone id of todayz.in (CLOUDFLARE_ZONE_ID)."
}

variable "zone_name" {
  type    = string
  default = "todayz.in"
}

variable "api_origin_hostname" {
  type        = string
  description = "ALB / Beanstalk endpoint from the AWS stack output api_origin_hostname."
}

variable "acm_validation_records" {
  type = list(object({
    name  = string
    type  = string
    value = string
  }))
  default     = []
  description = "ACM certificate validation CNAMEs from the AWS stack output certificate_validation_records (DNS only)."
}

variable "waf_managed_ruleset_ids" {
  type        = list(string)
  default     = ["77454fe2d30c4220b5701f6fdfb893ba"] # Cloudflare Free Managed Ruleset
  description = "Managed rulesets to execute. The default is the Free plan's. On Pro add the Cloudflare Managed Ruleset (efb7b8c949ac4650a09736fc376e9aee) and OWASP Core (4814384a9e5d4991b9815dcfc25d2f1f)."
}

# TODO(lead): the plan gives no rate-limit numbers. These are conservative starting values to
# confirm; OTP's exact limit is enforced in Postgres (02 section 2.3), the edge only sheds floods.
variable "rate_limit_auth_per_minute" {
  type    = number
  default = 20
}

variable "rate_limit_api_per_minute" {
  type    = number
  default = 600
}

variable "rate_limit_mitigation_seconds" {
  type    = number
  default = 60
}

variable "r2_location" {
  type    = string
  default = "apac"
}

locals {
  # Hosts. Staging uses a hyphen suffix (app-staging.todayz.in) because Universal SSL covers
  # one wildcard level only; app.staging.todayz.in would need Advanced Certificate Manager.
  suffix = var.environment == "prod" ? "" : "-staging"

  host = {
    site     = var.environment == "prod" ? var.zone_name : "staging.${var.zone_name}"
    customer = "app${local.suffix}.${var.zone_name}"
    business = "business${local.suffix}.${var.zone_name}"
    admin    = "admin${local.suffix}.${var.zone_name}"
    api      = "api${local.suffix}.${var.zone_name}"
    img      = "img${local.suffix}.${var.zone_name}"
    tiles    = "tiles${local.suffix}.${var.zone_name}"
  }

  pages_projects = {
    site     = "todayz-site${local.suffix}"
    customer = "todayz-customer${local.suffix}"
    business = "todayz-business${local.suffix}"
    admin    = "todayz-admin${local.suffix}"
  }

  buckets = {
    images  = "todayz-images${local.suffix}"
    tiles   = "todayz-tiles${local.suffix}"
    private = "todayz-private${local.suffix}"
  }
}

# ---- Zone settings (TLS: Full strict to the ALB) ---------------------------------------

resource "cloudflare_zone_setting" "ssl" {
  zone_id    = var.zone_id
  setting_id = "ssl"
  value      = "strict"
}

resource "cloudflare_zone_setting" "min_tls" {
  zone_id    = var.zone_id
  setting_id = "min_tls_version"
  value      = "1.2"
}

resource "cloudflare_zone_setting" "always_https" {
  zone_id    = var.zone_id
  setting_id = "always_use_https"
  value      = "on"
}

# ---- DNS -------------------------------------------------------------------------------

# api.: proxied to the ALB (which accepts Cloudflare ranges only).
resource "cloudflare_dns_record" "api" {
  zone_id = var.zone_id
  name    = local.host.api
  type    = "CNAME"
  content = var.api_origin_hostname
  proxied = true
  ttl     = 1
  comment = "API origin: ALB, Cloudflare ranges only"
}

# ACM validation records must NOT be proxied.
resource "cloudflare_dns_record" "acm_validation" {
  for_each = { for r in var.acm_validation_records : r.name => r }
  zone_id  = var.zone_id
  name     = trimsuffix(each.value.name, ".")
  type     = each.value.type
  content  = trimsuffix(each.value.value, ".")
  proxied  = false
  ttl      = 300
  comment  = "ACM DNS validation for the API certificate"
}

# Pages custom domains (the CNAME to <project>.pages.dev is what makes the domain resolve).
resource "cloudflare_dns_record" "pages" {
  for_each = local.pages_projects
  zone_id  = var.zone_id
  name     = local.host[each.key]
  type     = "CNAME"
  content  = "${each.value}.pages.dev"
  proxied  = true
  ttl      = 1
  comment  = "Cloudflare Pages: ${each.key}"
}

# ---- Pages projects (direct upload from CI with wrangler; no Git integration) -----------

resource "cloudflare_pages_project" "app" {
  for_each          = local.pages_projects
  account_id        = var.account_id
  name              = each.value
  production_branch = "main"
}

resource "cloudflare_pages_domain" "app" {
  for_each     = local.pages_projects
  account_id   = var.account_id
  project_name = cloudflare_pages_project.app[each.key].name
  name         = local.host[each.key]
}

# ---- R2 ---------------------------------------------------------------------------------

resource "cloudflare_r2_bucket" "this" {
  for_each   = local.buckets
  account_id = var.account_id
  name       = each.value
  location   = var.r2_location
}

# Public buckets get custom domains (cached by Cloudflare); `private` has none.
resource "cloudflare_r2_custom_domain" "img" {
  account_id  = var.account_id
  bucket_name = cloudflare_r2_bucket.this["images"].name
  domain      = local.host.img
  zone_id     = var.zone_id
  enabled     = true
  min_tls     = "1.2"
}

resource "cloudflare_r2_custom_domain" "tiles" {
  account_id  = var.account_id
  bucket_name = cloudflare_r2_bucket.this["tiles"].name
  domain      = local.host.tiles
  zone_id     = var.zone_id
  enabled     = true
  min_tls     = "1.2"
}

# ---- Turnstile (OTP send, lead form) --------------------------------------------------------

resource "cloudflare_turnstile_widget" "this" {
  account_id = var.account_id
  name       = "todayz-${var.environment}"
  domains    = [local.host.customer, local.host.business, local.host.site, local.host.api]
  mode       = "managed"
  region     = "world"
}

# ---- WAF: managed rules and custom rules ----------------------------------------------------

resource "cloudflare_ruleset" "waf_managed" {
  zone_id     = var.zone_id
  name        = "todayz-${var.environment}-managed-waf"
  description = "Cloudflare managed rulesets"
  kind        = "zone"
  phase       = "http_request_firewall_managed"

  rules = [for i, id in var.waf_managed_ruleset_ids : {
    action = "execute"
    action_parameters = {
      id = id
    }
    expression  = "true"
    description = "Execute managed ruleset ${i + 1}"
    enabled     = true
  }]
}

resource "cloudflare_ruleset" "waf_custom" {
  zone_id     = var.zone_id
  name        = "todayz-${var.environment}-custom-waf"
  description = "Custom firewall rules"
  kind        = "zone"
  phase       = "http_request_firewall_custom"

  rules = [
    {
      # Test-only routes exist only in local and test (backend/CLAUDE.md); never reachable here.
      action      = "block"
      expression  = "(http.host eq \"${local.host.api}\" and starts_with(http.request.uri.path, \"/v1/test/\"))"
      description = "Block /v1/test/* at the edge"
      enabled     = true
    },
    {
      # /version lives on the worker's internal port in staging and prod, not on the API.
      action      = "block"
      expression  = "(http.host eq \"${local.host.api}\" and http.request.uri.path eq \"/version\")"
      description = "Block /version on the public API host"
      enabled     = true
    },
    {
      # Health endpoints are for the load balancer; the public does not need them.
      action      = "block"
      expression  = "(http.host eq \"${local.host.api}\" and http.request.uri.path in {\"/readyz\" \"/healthz\"})"
      description = "Block health endpoints on the public API host"
      enabled     = true
    },
  ]
}

# ---- Rate limits -------------------------------------------------------------------------------

resource "cloudflare_ruleset" "rate_limits" {
  zone_id     = var.zone_id
  name        = "todayz-${var.environment}-rate-limits"
  description = "Edge rate limits per path group"
  kind        = "zone"
  phase       = "http_ratelimit"

  rules = [
    {
      action      = "block"
      expression  = "(http.host eq \"${local.host.api}\" and starts_with(http.request.uri.path, \"/v1/auth/\"))"
      description = "Auth endpoints (OTP send and verify, refresh) per IP"
      enabled     = true
      ratelimit = {
        characteristics     = ["cf.colo.id", "ip.src"]
        period              = 60
        requests_per_period = var.rate_limit_auth_per_minute
        mitigation_timeout  = var.rate_limit_mitigation_seconds
      }
    },
    {
      action      = "block"
      expression  = "(http.host eq \"${local.host.api}\" and starts_with(http.request.uri.path, \"/v1/\"))"
      description = "All API calls per IP"
      enabled     = true
      ratelimit = {
        characteristics     = ["cf.colo.id", "ip.src"]
        period              = 60
        requests_per_period = var.rate_limit_api_per_minute
        mitigation_timeout  = var.rate_limit_mitigation_seconds
      }
    },
  ]
}

# ---- Cache rules --------------------------------------------------------------------------------

resource "cloudflare_ruleset" "cache" {
  zone_id     = var.zone_id
  name        = "todayz-${var.environment}-cache"
  description = "Cache rules: images and tiles cached hard, the API never"
  kind        = "zone"
  phase       = "http_request_cache_settings"

  rules = [
    {
      action = "set_cache_settings"
      action_parameters = {
        cache = false
      }
      expression  = "(http.host eq \"${local.host.api}\")"
      description = "API responses are never cached at the edge"
      enabled     = true
    },
    {
      action = "set_cache_settings"
      action_parameters = {
        cache = true
        edge_ttl = {
          mode    = "override_origin"
          default = 2592000 # 30 days; image and tile keys are content-addressed
        }
        browser_ttl = {
          mode = "respect_origin"
        }
      }
      expression  = "(http.host in {\"${local.host.img}\" \"${local.host.tiles}\"})"
      description = "Images and tiles: cache at the edge for 30 days"
      enabled     = true
    },
  ]
}

output "turnstile_site_key" {
  description = "Public site key (TURNSTILE_SITE_KEY / PUBLIC_TURNSTILE_SITE_KEY)."
  value       = cloudflare_turnstile_widget.this.sitekey
}

output "turnstile_secret" {
  description = "Secret key: put it in Secrets Manager as TURNSTILE_SECRET_KEY; never print it."
  value       = cloudflare_turnstile_widget.this.secret
  sensitive   = true
}

output "pages_project_names" {
  value = local.pages_projects
}

output "hosts" {
  value = local.host
}
