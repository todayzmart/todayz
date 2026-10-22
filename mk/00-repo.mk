# W0-01: repo bootstrap steps (toolchain check, app keys, hooks, repo checks).

SETUP_STEPS += setup-tools setup-keys setup-node setup-hooks
CHECK_STEPS += check-repo

REQUIRED_TOOLS := go node pnpm git docker
BREW_TOOLS := gitleaks lefthook shellcheck

ENVTOOL_SRC := $(wildcard $(ROOT)/scripts/envtool/*.go) $(ROOT)/scripts/envtool/go.mod

$(ENVTOOL): $(ENVTOOL_SRC)
	@mkdir -p $(TOOLS)
	@go -C $(ROOT)/scripts/envtool build -trimpath -o $(ENVTOOL) .

.PHONY: setup-tools setup-keys setup-node setup-hooks check-repo

setup-tools: $(ENVTOOL)
	@for t in $(REQUIRED_TOOLS); do \
	  command -v $$t >/dev/null || { echo "missing tool: $$t (see docs/plan/09-infra-release.md)"; exit 1; }; \
	done
	@for t in $(BREW_TOOLS); do \
	  command -v $$t >/dev/null || { echo "installing $$t"; brew install $$t >/dev/null; }; \
	done
	@docker info >/dev/null 2>&1 || echo "note: docker is not running (colima start) - needed for make dev and integration tests"
	@echo "tools ok: $(REQUIRED_TOOLS) $(BREW_TOOLS)"

# Generates any missing app key (D-18) into .env.local; prints only "generated: NAME".
setup-keys: $(ENVTOOL)
	@$(ENVTOOL) generate -env "$(ENV_FILE)"

setup-node:
	@if [ -f $(ROOT)/package.json ]; then pnpm install --frozen-lockfile=false --reporter=silent; fi

setup-hooks:
	@if [ -d $(ROOT)/.git ]; then lefthook install >/dev/null && echo "git hooks installed (lefthook)"; fi

check-repo: $(ENVTOOL)
	@go -C $(ROOT)/scripts/envtool vet ./...
	@go -C $(ROOT)/scripts/envtool test -count=1 ./...
	@shellcheck $(ROOT)/scripts/*.sh $(ROOT)/scripts/test/*.sh $(ROOT)/scripts/hooks/*.sh
	@gitleaks dir $(ROOT) --config $(ROOT)/.gitleaks.toml --no-banner --redact --log-level warn
	@$(ROOT)/scripts/test/makefile-targets.sh
	@$(ROOT)/scripts/test/make-fail-fast.sh
	@$(ROOT)/scripts/test/sc-pl-03.sh
	@$(ROOT)/scripts/test/sc-pl-04.sh
	@$(ROOT)/scripts/test/hooks.sh
