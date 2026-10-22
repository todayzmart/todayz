# W0-03: backend (Go API + workers, one binary `todayz`). See backend/CLAUDE.md.
#
#   setup  go modules, the Go tools from backend/tools/go.mod into .tools/bin,
#          the test images (PostGIS, Ryuk) and the govulncheck DB (.cache/govulndb)
#   gen    sqlc (backend/gen/sqlc) and problem codes (backend/gen/problems)
#   check  gofmt, vet, golangci-lint, go test -race (testcontainers), gosec,
#          govulncheck (offline DB), the SC-PL-06 settings lint
#   reset  migrate up (todayz_owner), seed dev (todayz_app)
#   dev    migrate up, then the api (:8180) and the worker (:8181) under proc.sh

SETUP_STEPS += setup-backend
GEN_STEPS += gen-backend
CHECK_STEPS += check-backend
RESET_STEPS += reset-backend
DEV_STEPS += dev-backend

BACKEND := $(ROOT)/backend
BACKEND_BIN := $(TOOLS)/todayz
BACKEND_LIB := $(ROOT)/infra/local/lib.sh
BACKEND_PROC := $(ROOT)/infra/local/proc.sh
GOVULNDB := $(ROOT)/.cache/govulndb
API_PORT := 8180
WORKER_PORT := 8181
# Called vulnerabilities in go.mod modules always fail check-backend. Ones in
# the standard library come from the toolchain go.mod pins (D-88) and fail
# too by default; GOVULN_STDLIB=warn only prints the fixing Go version.
GOVULN_STDLIB ?= fail
# testcontainers-go v0.44.0's reaper (internal/config.ReaperDefaultImage).
RYUK_IMAGE := testcontainers/ryuk:0.14.0
BACKEND_TOOL_PKGS := \
  github.com/golangci/golangci-lint/v2/cmd/golangci-lint \
  github.com/pressly/goose/v3/cmd/goose \
  github.com/securego/gosec/v2/cmd/gosec \
  github.com/sqlc-dev/sqlc/cmd/sqlc \
  golang.org/x/vuln/cmd/govulncheck

# Docker for testcontainers: the current docker context when DOCKER_HOST is
# unset; on colima/lima Ryuk must mount the socket inside the VM.
BACKEND_DOCKER_ENV = \
  docker_host="$${DOCKER_HOST:-$$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)}"; \
  if [ -n "$$docker_host" ]; then export DOCKER_HOST="$$docker_host"; fi; \
  case "$$docker_host" in *colima* | *lima*) export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE="$${TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE:-/var/run/docker.sock}" ;; esac

# The PostGIS image: TODAYZ_POSTGIS_IMAGE, else the pinned ref from
# infra/local/lib.sh (testkit defaults to the same digests).
BACKEND_POSTGIS_IMAGE = \
  img="$${TODAYZ_POSTGIS_IMAGE:-}"; \
  if [ -z "$$img" ] && [ -f $(BACKEND_LIB) ]; then img="$$(source $(BACKEND_LIB) && echo "$$TODAYZ_POSTGIS_IMAGE")"; fi

.PHONY: setup-backend setup-backend-vulndb gen-backend check-backend backend-bin reset-backend dev-backend

setup-backend:
	@echo "backend toolchain: $$(GOTOOLCHAIN=auto go -C $(BACKEND) version)"
	@GOTOOLCHAIN=auto go -C $(BACKEND)/tools version >/dev/null
	@go -C $(BACKEND) mod download
	@go -C $(BACKEND)/tools mod download
	@mkdir -p $(TOOLS)
	@go -C $(BACKEND)/tools build -trimpath -o $(TOOLS)/ $(BACKEND_TOOL_PKGS)
	@echo "backend tools built: golangci-lint goose gosec sqlc govulncheck"
	@if docker info >/dev/null 2>&1; then \
	  $(BACKEND_POSTGIS_IMAGE); \
	  if [ -n "$$img" ]; then docker pull -q "$$img" >/dev/null; fi; \
	  docker pull -q $(RYUK_IMAGE) >/dev/null; \
	  echo "backend test images pulled: $${img:-testkit default} $(RYUK_IMAGE)"; \
	else \
	  echo "note: docker is not running (colima start); backend test images not pulled"; \
	fi
	@$(MAKE) setup-backend-vulndb

# Needs the internet; a failed download keeps the old DB (or warns when there is none).
setup-backend-vulndb:
	@mkdir -p $(ROOT)/.cache
	@tmp="$(ROOT)/.cache/govulndb.zip.tmp"; \
	if curl -fsSL --retry 2 --max-time 120 -o "$$tmp" https://vuln.go.dev/vulndb.zip; then \
	  rm -rf "$(GOVULNDB).new" && mkdir -p "$(GOVULNDB).new" && unzip -q "$$tmp" -d "$(GOVULNDB).new" && \
	  rm -rf "$(GOVULNDB)" && mv "$(GOVULNDB).new" "$(GOVULNDB)" && rm -f "$$tmp" && \
	  echo "govulncheck DB updated: .cache/govulndb"; \
	else \
	  rm -f "$$tmp"; \
	  if [ -d "$(GOVULNDB)" ]; then echo "warning: govulncheck DB not refreshed (offline?); keeping the old one"; \
	  else echo "warning: govulncheck DB not downloaded (offline?); make check needs it: rerun make setup online"; fi; \
	fi

gen-backend:
	@cd $(BACKEND) && sqlc generate -f db/sqlc.yaml
	@go -C $(BACKEND) run ./internal/devtools/problemgen -in api/problems -out gen/problems

check-backend:
	@unformatted="$$(gofmt -l $(BACKEND))"; \
	  if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	@go -C $(BACKEND) vet ./...
	@cd $(BACKEND) && golangci-lint run ./...
	@# forbidigo cannot see composite literals: an outbound client, transport
	@# or dialer is built only in internal/platform/egress (T3).
	@hits="$$(cd $(BACKEND) && grep -rnE --include='*.go' '(http\.(Client|Transport)|net\.Dialer)\{' . \
	    | grep -v '^\./internal/platform/egress/' | grep -v '^\./gen/' | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' || true)"; \
	  if [ -n "$$hits" ]; then echo "build http clients, transports and dialers only in internal/platform/egress:"; echo "$$hits"; exit 1; fi
	@$(BACKEND_DOCKER_ENV); $(BACKEND_POSTGIS_IMAGE); \
	  if [ -n "$$img" ]; then export TODAYZ_POSTGIS_IMAGE="$$img"; fi; \
	  go -C $(BACKEND) test -race -count=1 ./...
	@cd $(BACKEND) && gosec -quiet -exclude-generated -exclude-dir=testdata -exclude=G104 ./...
	@if [ ! -d $(GOVULNDB) ]; then echo "govulncheck DB missing (.cache/govulndb): run make setup online"; exit 1; fi
	@cd $(BACKEND) && govulncheck -db file://$(GOVULNDB) -format json ./... | go run ./internal/devtools/vulngate -stdlib=$(GOVULN_STDLIB)
	@go -C $(BACKEND) run ./internal/devtools/settingslint .

# Always rebuilt (the go build cache makes it quick); gen first, since the
# binary embeds generated code. The commit is stamped from git here because
# Go's own VCS stamp only sees a .git directory, so inside a git worktree
# nested in the main checkout it would report the main checkout's HEAD.
backend-bin: gen-backend
	@mkdir -p $(TOOLS)
	@bi=github.com/todayz/backend/internal/platform/buildinfo; \
	  commit="$$(git -C $(ROOT) rev-parse HEAD 2>/dev/null || true)"; \
	  ctime="$$(TZ=UTC git -C $(ROOT) log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd 2>/dev/null || true)"; \
	  modified=false; if [ -n "$$(git -C $(ROOT) status --porcelain 2>/dev/null)" ]; then modified=true; fi; \
	  go -C $(BACKEND) build -trimpath \
	    -ldflags "-X $$bi.Commit=$$commit -X $$bi.BuildTime=$$ctime -X $$bi.Modified=$$modified" \
	    -o $(BACKEND_BIN) ./cmd/todayz

# Runs after reset-db (mk/10-stack.mk) recreated the databases.
reset-backend: backend-bin
	@if [ ! -f $(BACKEND_LIB) ]; then echo "reset-backend skipped: infra/local/lib.sh not present yet"; exit 0; fi; \
	  source $(BACKEND_LIB); tz_load_env; \
	  $(BACKEND_BIN) migrate up; \
	  $(BACKEND_BIN) seed dev

dev-backend: backend-bin
	@if [ ! -x $(BACKEND_PROC) ]; then echo "dev-backend skipped: infra/local/proc.sh not present yet"; exit 0; fi; \
	  ( source $(BACKEND_LIB); tz_load_env; $(BACKEND_BIN) migrate up ); \
	  $(BACKEND_PROC) start api --port $(API_PORT) --health http://127.0.0.1:$(API_PORT)/healthz -- $(BACKEND_BIN) api; \
	  $(BACKEND_PROC) start worker --port $(WORKER_PORT) --health http://127.0.0.1:$(WORKER_PORT)/healthz -- $(BACKEND_BIN) worker
