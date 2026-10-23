# W1-08: @todayz/auth (session, token handling, sign-in flow, shell pieces): lint, types, tests.

CHECK_STEPS += check-auth

.PHONY: check-auth

check-auth:
	@pnpm -C $(ROOT) --filter @todayz/auth lint
	@pnpm -C $(ROOT) --filter @todayz/auth typecheck
	@pnpm -C $(ROOT) --filter @todayz/auth test
