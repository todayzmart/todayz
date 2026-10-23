# W1-10: infrastructure as code (not applied). See infra/terraform/README.md.
#
#   setup-infra-cloudflare  (network) init -backend=false for infra/cloudflare and tflint --init,
#                           the same prefetch setup-ci does for infra/terraform
#   check-infra             (offline) the policy checks: the A-22 read-only IAM action
#                           allow-list and the worker's PutMetricData namespace (SC-OPS-05), the
#                           shutdown and health-check timings (SC-GS-10), with their own tests;
#                           and fmt, validate, tflint, checkov on infra/cloudflare
#                           (infra/terraform is covered by check-tf-static in 90-ci.mk)

SETUP_STEPS += setup-infra-cloudflare
CHECK_STEPS += check-infra

.PHONY: setup-infra-cloudflare check-infra

setup-infra-cloudflare:
	@TF_DIR=infra/cloudflare $(ROOT)/scripts/ci/terraform.sh prefetch

check-infra:
	@cd $(ROOT)/infra/terraform/checks && python3 -I -m unittest discover -s . -p 'test_*.py'
	@python3 -I $(ROOT)/infra/terraform/checks/check.py all
	@TF_DIR=infra/cloudflare $(ROOT)/scripts/ci/terraform.sh static
