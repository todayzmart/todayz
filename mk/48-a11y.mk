# W4-04: accessibility (axe, WCAG 2.2 AA), visual regression and Lighthouse for the four web apps.
#
# check: types only (no stack).
# e2e:   axe over every route at 360, 390 and 1280 px (and the phone again in hi, kn, te, ta), then the
#        visual baselines (e2e/visual/baselines), against the running stack through Caddy. Both sign
#        in through the mocks' OTP (20 sends per IP per hour): start from a fresh make reset.
#        Lighthouse is separate (needs build:e2e of each app): `make lighthouse-apps`; the site has its
#        own gate (`pnpm --filter @todayz/site run lighthouse`).
# Refresh baselines on purpose: TZ_VISUAL_APP=<app> pnpm -C e2e/visual exec playwright test
#        -c playwright.config.ts --update-snapshots

CHECK_STEPS += check-a11y
E2E_STEPS += e2e-a11y e2e-visual

.PHONY: check-a11y e2e-a11y e2e-visual lighthouse-apps

check-a11y:
	@pnpm -C $(ROOT) --filter @todayz/e2e-a11y run typecheck

e2e-a11y: e2e-stack-up
	@pnpm -C $(ROOT) --filter @todayz/e2e-a11y run a11y

e2e-visual: e2e-stack-up
	@pnpm -C $(ROOT) --filter @todayz/e2e-visual run visual

lighthouse-apps:
	@pnpm -C $(ROOT) --filter @todayz/customer run build:e2e
	@pnpm -C $(ROOT) --filter @todayz/business run build:e2e
	@pnpm -C $(ROOT) --filter @todayz/admin run build:e2e
	@node $(ROOT)/e2e/a11y/lighthouse.mjs
