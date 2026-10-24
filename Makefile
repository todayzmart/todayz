# TodayZ root Makefile (owned by W0-01; D-56).
#
# The canonical targets below are the only public entry points. Each package adds
# its own mk/<area>.mk and registers its steps on the *_STEPS lists, for example:
#
#   CHECK_STEPS += check-backend
#   check-backend: ; cd backend && go test ./...
#
# Nobody edits this file to add a step. Steps run in the order the mk files sort,
# then the order they were registered. There is no `make seed`: `make reset` seeds.

.DEFAULT_GOAL := help
.DELETE_ON_ERROR:
MAKEFLAGS += --no-print-directory

ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
# Strict bash for every recipe; a wrapper because make 3.81 (macOS) ignores .SHELLFLAGS.
SHELL := $(ROOT)/scripts/make-shell.sh
TOOLS := $(ROOT)/.tools/bin
export PATH := $(TOOLS):$(PATH)
export TODAYZ_ROOT := $(ROOT)

# .env.local holds generated keys; tests point TODAYZ_ENV_FILE at a temp file.
ENV_FILE := $(or $(TODAYZ_ENV_FILE),$(ROOT)/.env.local)
ENVTOOL := $(TOOLS)/envtool
NO_EGRESS := $(ROOT)/scripts/no-egress.sh

CANONICAL := setup dev stop logs reset gen check e2e load security ai-eval demo env-check release-candidate

SETUP_STEPS :=
DEV_STEPS :=
STOP_STEPS :=
LOGS_STEPS :=
RESET_STEPS :=
GEN_STEPS :=
CHECK_STEPS :=
E2E_STEPS :=
LOAD_STEPS :=
SECURITY_STEPS :=
AI_EVAL_STEPS :=
DEMO_STEPS :=

include $(sort $(wildcard $(ROOT)/mk/*.mk))

# run_steps <target> <steps>: run each step as its own make goal, stop at the first failure.
define run_steps
	@steps="$(strip $(2))"; \
	if [ -z "$$steps" ]; then echo "make $(1): no steps registered yet"; exit 0; fi; \
	for s in $$steps; do echo "==> $(1): $$s"; $(MAKE) $$s || { rc=$$?; echo "make $(1): step $$s failed (exit $$rc)"; exit $$rc; }; done
endef

.PHONY: help $(CANONICAL) _check _e2e

help: ## List the canonical targets
	@echo "TodayZ make targets (docs/plan/02-architecture.md §1.2):"
	@echo "  setup              install toolchains and deps; generate missing keys into .env.local (names only)"
	@echo "  dev                local stack + apps behind Caddy HTTPS on *.todayz.localhost"
	@echo "  stop               stop the local stack"
	@echo "  logs               tail the local stack's logs"
	@echo "  reset              recreate the DB, migrate, seed (there is no make seed)"
	@echo "  gen                bundle OpenAPI, generate Go + TS clients, sqlc, route trees"
	@echo "  check              gen, lint, types, unit + integration, contract smoke, builds, security static"
	@echo "  e2e                Playwright journeys, contract full, visual, a11y, Lighthouse"
	@echo "  load               k6"
	@echo "  security           ZAP, Trivy, checkov, abuse suite"
	@echo "  ai-eval            live-model AI evals (only with your own keys in .env.local)"
	@echo "  demo               live order generator for dashboards and the business app"
	@echo "  env-check          env var names with set/missing, never values"
	@echo "  release-candidate  everything above on a clean reset; writes docs/qa/rc-<date>/"

setup:
	$(call run_steps,setup,$(SETUP_STEPS))

dev:
	$(call run_steps,dev,$(DEV_STEPS))

stop:
	$(call run_steps,stop,$(STOP_STEPS))

logs:
	$(call run_steps,logs,$(LOGS_STEPS))

reset:
	$(call run_steps,reset,$(RESET_STEPS))

gen:
	$(call run_steps,gen,$(GEN_STEPS))

# check and e2e run with outbound traffic blocked and provider keys unset (SC-PL-04).
check:
	@$(NO_EGRESS) $(MAKE) _check

_check: gen
	$(call run_steps,check,$(CHECK_STEPS))

e2e:
	@$(NO_EGRESS) $(MAKE) _e2e

_e2e: gen
	$(call run_steps,e2e,$(E2E_STEPS))

load:
	$(call run_steps,load,$(LOAD_STEPS))

security:
	$(call run_steps,security,$(SECURITY_STEPS))

ai-eval:
	$(call run_steps,ai-eval,$(AI_EVAL_STEPS))

demo:
	$(call run_steps,demo,$(DEMO_STEPS))

# The cloud names (D-210) are the lead's only; a missing .env.cloud.local lists them as missing.
env-check: $(ENVTOOL)
	@$(ENVTOOL) check -env "$(ENV_FILE)" -example "$(ROOT)/.env.example"
	@echo "-- cloud (.env.cloud.local, lead only)"
	@$(ENVTOOL) check -env "$(ROOT)/.env.cloud.local" -example "$(ROOT)/.env.cloud.example"

# A clean reset, then every suite; the report lists each step's result (no values).
release-candidate:
	@out="$(ROOT)/docs/qa/rc-$$(date +%Y-%m-%d)"; mkdir -p "$$out"; \
	status=0; : >"$$out/summary.md"; \
	echo "# Release candidate $$(date +%Y-%m-%d) @ $$(git -C "$(ROOT)" rev-parse --short HEAD 2>/dev/null || echo no-git)" >>"$$out/summary.md"; \
	for t in reset check e2e load security; do \
	  if $(MAKE) $$t >"$$out/$$t.log" 2>&1; then r=pass; else r=FAIL; status=1; fi; \
	  echo "- make $$t: $$r" | tee -a "$$out/summary.md"; \
	done; \
	$(ENVTOOL) leakscan -env "$(ENV_FILE)" "$$out"/*.log || status=1; \
	exit $$status
