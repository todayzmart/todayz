# W0-05: frontend shells (customer, business, admin, site) and the shared frontend packages
# (@todayz/config, tokens, i18n, telemetry). Smoke screenshots go to test-results/screenshots/.
#
# Every step goes through pnpm workspace filters, so each app still builds on its own.
# check and e2e run under scripts/no-egress.sh (root Makefile): nothing here may need the network.

SETUP_STEPS += setup-frontend
GEN_STEPS += gen-frontend
CHECK_STEPS += check-frontend
E2E_STEPS += e2e-frontend-smoke
DEV_STEPS += dev-frontend

FE_SHARED := @todayz/config @todayz/tokens @todayz/i18n @todayz/telemetry
FE_VITE := @todayz/customer @todayz/business @todayz/admin
FE_APPS := $(FE_VITE) @todayz/site
FE_ALL := $(FE_SHARED) $(FE_APPS)

# fe_run <packages> <script> [extra pnpm flags]: run a package script in each package, stop on failure.
fe_run = pnpm -C $(ROOT) $(3) $(foreach p,$(1),--filter $(p)) --if-present run $(2)

# app:port:package for the dev servers (fixed ports, strictPort; Caddy fronts them on 443).
FE_DEV := customer:5301:@todayz/customer business:5302:@todayz/business admin:5303:@todayz/admin site:5304:@todayz/site

.PHONY: setup-frontend gen-frontend check-frontend e2e-frontend-smoke dev-frontend \
        check-frontend-lint check-frontend-types check-frontend-unit check-frontend-build check-frontend-size

# Playwright's Chromium (headless shell) for the smoke tests. This is the only download.
setup-frontend:
	@pnpm -C $(ROOT) --filter @todayz/customer exec playwright install --only-shell chromium

# Tokens and fonts from design/source, compiled message catalogues + pseudo-locale,
# TanStack route trees, Astro types. All output is git-ignored.
gen-frontend:
	@$(call fe_run,@todayz/tokens,gen)
	@$(call fe_run,@todayz/i18n,gen)
	@$(call fe_run,$(FE_VITE),gen)
	@$(call fe_run,@todayz/site,gen)

check-frontend: check-frontend-lint check-frontend-types check-frontend-unit check-frontend-build check-frontend-size

check-frontend-lint:
	@$(call fe_run,$(FE_ALL),lint)
	@$(call fe_run,$(FE_APPS),lint:css)

check-frontend-types:
	@$(call fe_run,$(FE_ALL),typecheck)

check-frontend-unit:
	@$(call fe_run,$(FE_ALL),test)

check-frontend-build:
	@$(call fe_run,$(FE_APPS),build)

# Initial JS budgets: customer 170 KB, business 250 KB, admin 250 KB gzip (each app's .size-limit.js).
check-frontend-size:
	@$(call fe_run,$(FE_VITE),size)

# Optimised builds under preview on random free ports, Chromium, phone and desktop sizes.
# The Vite apps build with `--mode e2e` (production output, but `?locale=pseudo` stays on;
# a `production` build ignores it). Fails on console errors, non-local requests, unloaded
# fonts or a broken pseudo-locale. Screenshots go to the git-ignored test-results/screenshots/;
# TZ_QA_DIR=docs/qa/W0-05 refreshes the committed ones. One app at a time so the preview
# servers never compete.
e2e-frontend-smoke:
	@$(call fe_run,$(FE_VITE),build:e2e)
	@$(call fe_run,@todayz/site,build)
	@$(call fe_run,$(FE_APPS),e2e:smoke,--workspace-concurrency=1)

# Dev servers under the local stack's process supervisor (W0-02).
dev-frontend:
	@if [ ! -x "$(ROOT)/infra/local/proc.sh" ]; then \
	  echo "dev-frontend: skipped: infra/local/proc.sh not present yet"; exit 0; \
	fi; \
	for entry in $(FE_DEV); do \
	  IFS=: read -r name port pkg <<<"$$entry"; \
	  "$(ROOT)/infra/local/proc.sh" start "$$name" --port "$$port" --health "http://127.0.0.1:$$port/" \
	    -- pnpm -C "$(ROOT)" --filter "$$pkg" dev; \
	done
