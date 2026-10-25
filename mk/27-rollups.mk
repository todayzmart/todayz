# W3-07: `make demo` (09 §36, 05 §333): synthetic traffic against the running
# local stack (`make dev`), so the dashboards, Pulse and the business app show
# activity. It searches, sends RUM and app events and books pickup orders at
# the seeded store as customer.ravi, which the seeded owner collects (or the
# customer cancels), through the API on loopback; the worker runs the timers
# and the rollup jobs. It prints counts and problem codes only.
#
#   make demo                 until Ctrl-C, a tick every 10 s
#   make demo DEMO_FOR=10m    stop after ten minutes
#
# Recompute today's rollups afterwards with
# `todayz rollup --from <YYYY-MM-DD>` (IST day). `make reset` puts the stock
# and the customer's wallet back.

DEMO_STEPS += demo-rollups
DEMO_FOR ?=
DEMO_EVERY ?= 10s

.PHONY: demo-rollups

demo-rollups: backend-bin
	@if [ ! -f $(BACKEND_LIB) ]; then echo "demo-rollups skipped: infra/local/lib.sh not present yet"; exit 0; fi; \
	  source $(BACKEND_LIB); tz_load_app_env; \
	  $(BACKEND_BIN) rollup demo --api http://127.0.0.1:$(API_PORT) --mocks http://127.0.0.1:$(MOCKS_PORT) \
	    --every $(DEMO_EVERY) $(if $(DEMO_FOR),--for $(DEMO_FOR),)
