# W0-04: the API contract (backend/api/, one paths and schemas file per domain, D-50) and
# everything generated from it. The bundle and all generated code are git-ignored.
#
# setup-contract   (network) oapi-codegen from backend/tools into .tools/bin; the pinned uv
#                  release into .tools/bin (SHA-256 checked, scripts/install-uv.sh); the pinned
#                  CPython into .tools/python (uv-managed, never a system Python); the
#                  Schemathesis venv in .tools/schemathesis (every wheel hash-pinned in
#                  e2e/contract/requirements.txt, D-88, D-100 L8).
# gen-contract     apigen root (the domain files -> backend/gen/api/root.yaml), Redocly bundle
#                  -> backend/api/openapi.yaml (the bundler lives in shared/api-client and is
#                  told every backend path explicitly, D-100 D5), apigen check (SC-PL-01: unique
#                  method+path, allowed prefixes, the operation count, the D-100 lints),
#                  oapi-codegen -> backend/gen/api/ (spec/ embeds the bundle; <domain>/ the chi
#                  strict-server stubs plus register.gen.go), orval + the MSW generator ->
#                  shared/api-client/src/gen and src/mocks/handlers.gen.ts.
# check-contract   redocly lint (rules in backend/api/redocly.yaml), the api-client lint, types
#                  and tests, the hooks' unit tests, then the Schemathesis smoke (examples and
#                  coverage phases) against an in-process api on a fresh database
#                  (TestContractSmoke) and the SC-PL-02 fixture (TestContractProblemCheckFires).
# e2e-contract     the full Schemathesis run (examples, coverage, fuzzing) against the running
#                  stack through Caddy, verified with Caddy's local root certificate. It brings
#                  the stack up itself (e2e-stack-up: make dev is idempotent), because the stack
#                  smoke before it ends with make stop (D-100 D6).
# contract-lock    (network) re-pins e2e/contract/requirements.txt from requirements.in.

SETUP_STEPS += setup-contract
GEN_STEPS += gen-contract
CHECK_STEPS += check-contract
E2E_STEPS += e2e-contract

CONTRACT_API := $(BACKEND)/api
CONTRACT_OPENAPI := $(CONTRACT_API)/openapi.yaml
CONTRACT_ROOT := $(BACKEND)/gen/api/root.yaml
CONTRACT_PROBLEMS := $(CONTRACT_API)/problems
CONTRACT_DIR := $(ROOT)/e2e/contract
CONTRACT_VENV := $(ROOT)/.tools/schemathesis
CONTRACT_PY := $(CONTRACT_VENV)/bin/python
CONTRACT_ST := $(CONTRACT_VENV)/bin/schemathesis
CONTRACT_API_URL ?= https://api.todayz.localhost
# Newest release date a Python dependency may have when re-locking (D-88).
CONTRACT_EXCLUDE_NEWER := 2026-10-08
# 04 §2.11 lists 307 operations once every domain has landed. 0 only prints the count per
# namespace; the lead sets 307 when the last domain WP merges (SC-PL-01).
CONTRACT_EXPECT ?= 0

# The Python toolchain, pinned to the patch (D-100 L8). uv is installed by scripts/install-uv.sh
# (SHA-256 per platform recorded there); its managed interpreters live under .tools/python and
# a system Python is never picked (UV_PYTHON_PREFERENCE=only-managed). Re-pin uv by bumping
# UV_VERSION and adding the release's sums to the script.
UV_VERSION := 0.10.6
UV := $(TOOLS)/uv
CONTRACT_PYTHON := 3.12.12
CONTRACT_UV_ENV := UV_PYTHON_INSTALL_DIR=$(ROOT)/.tools/python UV_PYTHON_PREFERENCE=only-managed \
                   UV_NO_CONFIG=1 UV_NO_PROGRESS=1
uv = $(CONTRACT_UV_ENV) $(UV)

apigen = go -C $(BACKEND) run ./internal/devtools/apigen
REDOCLY_BIN := $(ROOT)/shared/api-client/node_modules/.bin/redocly

# make check: apigen's codegen round trip never skips (D-100 L9); the tools it needs are named
# here, and the api-client tests see the same flag.
_check: export TODAYZ_CHECK = 1
_check: export TODAYZ_OAPI_CODEGEN = $(TOOLS)/oapi-codegen
_check: export TODAYZ_REDOCLY = $(REDOCLY_BIN)

.PHONY: setup-contract gen-contract check-contract check-contract-lint check-contract-client \
        check-contract-hooks contract-smoke e2e-stack-up e2e-contract contract-lock

setup-contract:
	@mkdir -p $(TOOLS)
	@go -C $(BACKEND)/tools build -trimpath -o $(TOOLS)/ github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
	@$(ROOT)/scripts/install-uv.sh $(UV_VERSION) $(UV)
	@$(uv) python install -q $(CONTRACT_PYTHON)
	@if [ -f $(CONTRACT_VENV)/pyvenv.cfg ] && ! { grep -q "^version_info = $(CONTRACT_PYTHON)$$" $(CONTRACT_VENV)/pyvenv.cfg \
	    && grep -q "^uv = $(UV_VERSION)$$" $(CONTRACT_VENV)/pyvenv.cfg \
	    && grep -q "^home = $(ROOT)/.tools/python/" $(CONTRACT_VENV)/pyvenv.cfg; }; then \
	  echo "recreating .tools/schemathesis for Python $(CONTRACT_PYTHON) / uv $(UV_VERSION)"; rm -rf $(CONTRACT_VENV); fi
	@$(uv) venv -q --python $(CONTRACT_PYTHON) --allow-existing $(CONTRACT_VENV)
	@$(uv) pip install -q --python $(CONTRACT_PY) --require-hashes -r $(CONTRACT_DIR)/requirements.txt

gen-contract:
	@$(apigen) root -api $(CONTRACT_API) -out $(CONTRACT_ROOT)
	@TODAYZ_API_ROOT=$(CONTRACT_ROOT) TODAYZ_OPENAPI=$(CONTRACT_OPENAPI) TODAYZ_REDOCLY_CONFIG=$(CONTRACT_API)/redocly.yaml \
	  $(call fe_run,@todayz/api-client,bundle)
	@$(apigen) check -api $(CONTRACT_API) -bundle $(CONTRACT_OPENAPI) -expect-count $(CONTRACT_EXPECT)
	@$(apigen) codegen -api $(CONTRACT_API) -bundle $(CONTRACT_OPENAPI) -out $(BACKEND)/gen/api -tool $(TOOLS)/oapi-codegen
	@TODAYZ_API_ROOT=$(CONTRACT_ROOT) TODAYZ_OPENAPI=$(CONTRACT_OPENAPI) TODAYZ_REDOCLY_CONFIG=$(CONTRACT_API)/redocly.yaml \
	  TODAYZ_PROBLEMS_DIR=$(CONTRACT_PROBLEMS) $(call fe_run,@todayz/api-client,gen)

# The binary embeds the bundle (gen/api/spec), so it needs the contract step too.
backend-bin: gen-contract

check-contract: check-contract-lint check-contract-client check-contract-hooks contract-smoke

check-contract-lint:
	@TODAYZ_OPENAPI=$(CONTRACT_OPENAPI) TODAYZ_REDOCLY_CONFIG=$(CONTRACT_API)/redocly.yaml $(call fe_run,@todayz/api-client,lint:spec)

check-contract-client:
	@$(call fe_run,@todayz/api-client,lint)
	@$(call fe_run,@todayz/api-client,typecheck)
	@$(call fe_run,@todayz/api-client,test)

check-contract-hooks:
	@if [ ! -x $(CONTRACT_ST) ]; then echo "schemathesis missing (.tools/schemathesis): run make setup online"; exit 1; fi
	@cd $(CONTRACT_DIR) && $(CONTRACT_PY) -I -B -m unittest discover -s . -p 'test_*.py'

contract-smoke:
	@$(BACKEND_DOCKER_ENV); $(BACKEND_POSTGIS_IMAGE); \
	  if [ -n "$$img" ]; then export TODAYZ_POSTGIS_IMAGE="$$img"; fi; \
	  TODAYZ_SCHEMATHESIS=$(CONTRACT_ST) TODAYZ_CONTRACT_DIR=$(CONTRACT_DIR) TODAYZ_PROBLEMS_DIR=$(CONTRACT_PROBLEMS) \
	  go -C $(BACKEND) test -count=1 -run 'TestContract(Smoke|ProblemCheckFires)' ./cmd/todayz

# make dev is idempotent (dev-stack, dev-buckets, dev-caddy all re-run safely), so this is the
# stack-up step e2e-contract needs after e2e-stack's smoke stopped everything (D-100 D6).
e2e-stack-up:
	@$(MAKE) dev

e2e-contract: e2e-stack-up
	@ca="$${TODAYZ_CA_FILE:-}"; \
	  if [ -z "$$ca" ] && [ -f $(BACKEND_LIB) ]; then ca="$$(source $(BACKEND_LIB) && echo "$$TZ_CADDY_ROOT_CERT")"; fi; \
	  TODAYZ_CA_FILE="$$ca" TODAYZ_SCHEMATHESIS=$(CONTRACT_ST) TODAYZ_PROBLEMS_DIR=$(CONTRACT_PROBLEMS) \
	  $(CONTRACT_DIR)/run.sh full $(CONTRACT_OPENAPI) $(CONTRACT_API_URL)

contract-lock:
	@cd $(CONTRACT_DIR) && $(uv) pip compile --generate-hashes --exclude-newer $(CONTRACT_EXCLUDE_NEWER) \
	  --python-version $(CONTRACT_PYTHON) requirements.in -o requirements.txt
