# W0-06: CI definitions (.github/workflows, run locally with scripts/ci/act.sh).
# Workflows call canonical targets only (D-56); everything they run is registered here.
#
# setup-ci         (network) the pinned actionlint into .tools/bin, the wflint module,
#                  Terraform providers/images when infra/terraform has .tf files, and on
#                  a runner: Caddy's port capability (runner-prep.sh, GitHub-hosted only)
#                  and Playwright's system libraries (when TODAYZ_CI_BROWSER_DEPS=1, CI=true).
#                  Both are no-ops on a laptop, so workflows call make targets only (D-56).
# check-ci         (offline) actionlint, scripts/ci/wflint (the TodayZ workflow policy),
#                  tracked-file and gitleaks working-tree checks, shellcheck, the
#                  Lighthouse D-58 lint, toolchain pins, and a selftest proving each
#                  lint catches a seeded violation. See scripts/ci/check.sh.
# check-tf-static  (offline) terraform fmt -check, validate, tflint, checkov on
#                  infra/terraform; skips with a message while there are no .tf files
# security-*       `make security`: gitleaks (history), Trivy, checkov, ZAP baseline,
#                  the stack-only abuse suite (W4-03, e2e/security/abuse.stack.test.mjs).
#                  security-zap and security-abuse run only when the local stack answers its health
#                  check (make dev first) and otherwise prints why it skipped.

SETUP_STEPS += setup-ci
CHECK_STEPS += check-ci check-tf-static
SECURITY_STEPS += security-gitleaks security-trivy security-checkov security-zap security-abuse

.PHONY: setup-ci check-ci check-tf-static security-gitleaks security-trivy security-checkov security-zap security-abuse

setup-ci:
	@$(ROOT)/scripts/ci/install-tools.sh actionlint
	@cd $(ROOT)/scripts/ci/wflint && go mod download
	@$(ROOT)/scripts/ci/terraform.sh prefetch
	@$(ROOT)/scripts/ci/runner-prep.sh
	@if [ "$${TODAYZ_CI_BROWSER_DEPS:-0}" = 1 ]; then $(ROOT)/scripts/ci/playwright-install.sh; fi

check-ci:
	@$(ROOT)/scripts/ci/check.sh

check-tf-static:
	@$(ROOT)/scripts/ci/terraform.sh static

security-gitleaks:
	@$(ROOT)/scripts/ci/scan.sh gitleaks

security-trivy:
	@$(ROOT)/scripts/ci/scan.sh trivy

security-checkov:
	@$(ROOT)/scripts/ci/scan.sh checkov infra

security-zap:
	@$(ROOT)/scripts/ci/scan.sh zap --if-up

security-abuse:
	@$(ROOT)/scripts/ci/scan.sh abuse --if-up
