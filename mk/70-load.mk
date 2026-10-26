# W4-02: load and performance (08 §6). k6 scenarios in e2e/load/scenarios, the SSE fan-out
# driver in e2e/load/ssefanout (Go; k6 cannot hold SSE streams), the data generator and the
# post-run SQL checks in e2e/load/data and e2e/load/checks. e2e/load/README.md has the details.
#
#   make reset load-data    1,000 shops, 50,000 customers, 14 days of orders and 7 of POS sales,
#                           the load shop staff signed in through the API, out/fixture.json.
#                           Refuses unless platform_environment.env is local or test.
#   make load               every scenario below, in order, stopping at the first failure.
#   make load-<scenario>    one scenario (load-browse, load-orders, load-pos, load-otp, load-sse,
#                           load-soak). LOAD_SOAK_DURATION (default 20m) shortens the 2 h soak.
#
# Every URL is local: run.sh refuses an API or mocks origin that is not loopback or
# *.todayz.localhost, and these targets refuse a database URL that is not the local stack's
# (127.0.0.1:5433). Reports go to the git-ignored e2e/load/out and hold numbers only.

LOAD_STEPS += load-browse load-orders load-pos load-otp load-sse load-soak

LOAD_DIR := $(ROOT)/e2e/load
LOAD_RUN := $(LOAD_DIR)/run.sh
LOAD_OUT ?= $(LOAD_DIR)/out
LOAD_SOAK_DURATION ?= 20m
export LOAD_OUT LOAD_SOAK_DURATION

# load_env: the local stack's env (DATABASE_URL as todayz_app, DATABASE_MIGRATE_URL as the
# owner, which the generator needs for temp tables), then refuse any database that is not it.
define load_env
source $(ROOT)/infra/local/lib.sh; tz_load_app_env; \
case "$${DATABASE_URL:-}" in *@127.0.0.1:5433/* | *@localhost:5433/*) ;; *) echo "load: DATABASE_URL is not the local stack" >&2; exit 2 ;; esac; \
case "$${DATABASE_MIGRATE_URL:-}" in *@127.0.0.1:5433/* | *@localhost:5433/*) ;; *) echo "load: DATABASE_MIGRATE_URL is not the local stack" >&2; exit 2 ;; esac; \
mkdir -p $(LOAD_OUT)
endef

LOAD_PSQL = psql "$$DATABASE_MIGRATE_URL" -X -v ON_ERROR_STOP=1

.PHONY: load-data load-browse load-orders load-pos load-otp load-sse load-soak

load-data:
	@$(load_env); \
	$(LOAD_PSQL) -q -v history=$${LOAD_HISTORY:-1} -f $(LOAD_DIR)/data/generate.sql; \
	$(LOAD_RUN) bootstrap; \
	$(LOAD_PSQL) -q -f $(LOAD_DIR)/data/link_staff.sql; \
	$(LOAD_PSQL) -At -f $(LOAD_DIR)/data/export_fixture.sql -o $(LOAD_OUT)/fixture.json; \
	echo "load-data: fixture at $(LOAD_OUT)/fixture.json"

load-browse:
	@$(LOAD_RUN) browse_peak

# The k6 verdict (latency, errors) and the SQL verdict (timers, ledger) are both reported; the
# step fails if either fails.
load-orders:
	@$(load_env); since="$$(date -u +%Y-%m-%dT%H:%M:%SZ)"; rc=0; \
	$(LOAD_RUN) order_burst || rc=$$?; \
	$(LOAD_PSQL) -v since="$$since" -f $(LOAD_DIR)/checks/order_burst.sql || rc=1; \
	exit $$rc

load-pos:
	@$(load_env); since="$$(date -u +%Y-%m-%dT%H:%M:%SZ)"; rc=0; \
	$(LOAD_RUN) pos_rush || rc=$$?; \
	sent="$$(jq -r '.metrics.n_sales_unique_sent.count // 0' $(LOAD_OUT)/pos_rush-summary.json)"; \
	$(LOAD_PSQL) -v since="$$since" -v sent="$$sent" -f $(LOAD_DIR)/checks/pos_rush.sql || rc=1; \
	exit $$rc

load-otp:
	@$(LOAD_RUN) otp_abuse

load-sse:
	@$(load_env); $(LOAD_RUN) sse_fanout

# The soak samples memory, goroutines and connections every 30 s alongside k6
# (out/soak-samples.csv) for the no-growth and no-leak verdict.
load-soak:
	@$(load_env); rc=0; \
	$(LOAD_DIR)/sample.sh $(LOAD_OUT)/soak-samples.csv & sampler=$$!; \
	$(LOAD_RUN) soak || rc=$$?; \
	kill $$sampler 2>/dev/null || true; \
	exit $$rc
