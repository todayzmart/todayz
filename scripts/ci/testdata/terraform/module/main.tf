# Fixture for a manual check of scripts/ci/terraform.sh (TF_DIR=scripts/ci/testdata/terraform).
# No providers, so init -backend=false needs no downloads.
terraform {
  required_version = ">= 1.9"
}

variable "name" {
  type    = string
  default = "todayz"
}

output "greeting" {
  value = "hello ${var.name}"
}
