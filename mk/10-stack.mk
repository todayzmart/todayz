# W0-02: local stack. Docker (Postgres/PostGIS, MinIO, Grafana LGTM,
# postgres_exporter) through docker-compose.yml, Caddy HTTPS on the host, map tiles.
# Scripts live in infra/local/ (see infra/local/README.md).

STACK := $(ROOT)/infra/local/stack.sh
PROC := $(ROOT)/infra/local/proc.sh

SETUP_STEPS += setup-stack-images setup-tiles
DEV_STEPS += dev-stack dev-buckets dev-caddy
STOP_STEPS += stop-stack
LOGS_STEPS += logs-stack
RESET_STEPS += reset-db
CHECK_STEPS += check-stack
E2E_STEPS += e2e-stack

.PHONY: setup-stack-images setup-tiles dev-stack dev-buckets dev-caddy stop-stack logs-stack reset-db check-stack e2e-stack

setup-stack-images:
	@$(STACK) pull

# Needs the internet; a failed download is a warning (make setup carries on).
setup-tiles:
	@$(ROOT)/infra/local/tiles.sh fetch

dev-stack:
	@$(STACK) up

# Buckets, policies, health probes, then tiles (uploaded only when missing).
dev-buckets:
	@$(STACK) buckets
	@$(STACK) tiles

dev-caddy:
	@$(STACK) caddy-start

# Host processes first (they may hold DB connections), then Caddy, then containers.
stop-stack:
	@$(PROC) stop-all
	@$(STACK) caddy-stop
	@$(STACK) stop

logs-stack:
	@$(STACK) logs

# Drops and recreates todayz and todayz_test; W0-03 (mk/20-backend.mk) migrates and seeds after.
reset-db:
	@$(STACK) reset-db

# Static only: no network, no running stack (runs under scripts/no-egress.sh).
check-stack:
	@$(ROOT)/infra/local/test/static.sh

# Brings the stack up for real and probes every host (make e2e).
e2e-stack:
	@$(ROOT)/infra/local/test/dev-smoke.sh
