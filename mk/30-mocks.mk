# W0-07: provider mocks (MSG91 SMS, WhatsApp, Web Push, Turnstile, Sarvam STT,
# Anthropic) as one stdlib Go service in e2e/mocks. See e2e/mocks/README.md.

SETUP_STEPS += setup-mocks
CHECK_STEPS += check-mocks
DEV_STEPS += dev-mocks

MOCKS_DIR := $(ROOT)/e2e/mocks
MOCKS_BIN := $(TOOLS)/tz-mocks
MOCKS_PORT := 4010
MOCKS_SRC := $(MOCKS_DIR)/go.mod \
  $(wildcard $(MOCKS_DIR)/*.go $(MOCKS_DIR)/*/*/*.go $(MOCKS_DIR)/*/*/*/*.go) \
  $(wildcard $(MOCKS_DIR)/internal/ui/assets/*) \
  $(shell find $(MOCKS_DIR)/fixtures -type f -not -path '*/_*' -not -name '.*' 2>/dev/null)
# Local recordings (fixtures/llm/_recorded) are not embedded, so they are not
# build inputs either: recording never triggers a rebuild.

$(MOCKS_BIN): $(MOCKS_SRC)
	@mkdir -p $(TOOLS)
	@go -C $(MOCKS_DIR) build -trimpath -o $(MOCKS_BIN) .

.PHONY: setup-mocks check-mocks dev-mocks

setup-mocks:
	@go -C $(MOCKS_DIR) mod download
	@$(MAKE) --no-print-directory $(MOCKS_BIN)
	@echo "mocks built: .tools/bin/tz-mocks"

check-mocks:
	@unformatted="$$(gofmt -l $(MOCKS_DIR))"; \
	  if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	@go -C $(MOCKS_DIR) vet ./...
	@go -C $(MOCKS_DIR) test -race -count=1 ./...
	@go -C $(MOCKS_DIR) build -trimpath -o /dev/null .

dev-mocks: $(MOCKS_BIN)
	@if [ -x $(ROOT)/infra/local/proc.sh ]; then \
	  $(ROOT)/infra/local/proc.sh start mocks --port $(MOCKS_PORT) --health http://127.0.0.1:$(MOCKS_PORT)/healthz -- $(MOCKS_BIN); \
	else \
	  echo "dev-mocks skipped: infra/local/proc.sh not present yet"; \
	fi
