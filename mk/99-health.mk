# W0-02: the last dev step. Checks the whole local stack (containers, Caddy and
# every process registered with infra/local/proc.sh), prints a table and writes
# https://todayz.localhost/_health. Fails make dev when a required check is red.

DEV_STEPS += dev-health

.PHONY: dev-health

dev-health:
	@$(ROOT)/infra/local/health.sh --wait $${TODAYZ_HEALTH_WAIT:-90}
