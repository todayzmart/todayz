# W1-02: @todayz/ui (shared/ui), the React UI kit and its Storybook.
#
# check: lint (TS + CSS), types, unit tests with axe on every story and the 90% coverage gate,
#        and a Storybook build (the stories must compile).
# e2e:   Chromium against the built Storybook (08-testing: visual and a11y run in make e2e):
#        every story compared with its design/source card at 0.1% tolerance, and axe with
#        colour contrast on every story. Local files only (visual/serve.mjs on 127.0.0.1).
#
# QA screenshots on purpose: TZ_QA_DIR=docs/qa/W1-02/screens pnpm --filter @todayz/ui visual screens.spec.ts

CHECK_STEPS += check-ui
E2E_STEPS += e2e-ui-visual

UI_PKG := @todayz/ui

.PHONY: check-ui e2e-ui-visual

check-ui:
	@pnpm -C $(ROOT) --filter $(UI_PKG) run lint
	@pnpm -C $(ROOT) --filter $(UI_PKG) run lint:css
	@pnpm -C $(ROOT) --filter $(UI_PKG) run typecheck
	@pnpm -C $(ROOT) --filter $(UI_PKG) run test
	@pnpm -C $(ROOT) --filter $(UI_PKG) run build-storybook

e2e-ui-visual:
	@pnpm -C $(ROOT) --filter $(UI_PKG) run build-storybook
	@pnpm -C $(ROOT) --filter $(UI_PKG) run visual design.spec.ts a11y.spec.ts
