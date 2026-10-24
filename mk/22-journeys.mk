# W2-13: the Wave 2 E2E gate. Playwright multi-context journeys (e2e/journeys/specs, fixtures in
# e2e/fixtures) against the running stack through Caddy: customer, business and admin principals
# side by side, OTPs read from the mocks' /_mock/sms/latest-otp, admin steps through the admin
# API (D-83), the stack's test clock (D-80) for timers. 08-testing.md §E2E lists the journeys.
#
# check: types only (no stack).
# e2e:   the journeys, one worker, after the stack is up (e2e-stack-up: make dev is idempotent).
#        Start a gate run from a fresh make reset: journeys create customers, orders and credits,
#        and the OTP send caps (20 per IP per hour) count every sign-in.
#        This file is 22-, so make e2e runs the journeys after e2e-stack (10-) and before
#        e2e-contract (25-): the fuzzer sends OTPs from the same IP (13 of the 20 in one run) and
#        writes data the journeys do not expect. After the journeys it may meet 429 rate_limited,
#        which otp/send declares, so the contract check still holds.
#        The JSON report goes to the git-ignored e2e/journeys/test-results/report.json; it holds
#        titles, statuses and errors only (helpers never print a code, token or phone).

CHECK_STEPS += check-journeys
E2E_STEPS += e2e-journeys

JOURNEYS_PKG := @todayz/e2e-journeys

.PHONY: check-journeys e2e-journeys

check-journeys:
	@pnpm -C $(ROOT) --filter $(JOURNEYS_PKG) run typecheck

e2e-journeys: e2e-stack-up
	@pnpm -C $(ROOT) --filter $(JOURNEYS_PKG) run journeys
