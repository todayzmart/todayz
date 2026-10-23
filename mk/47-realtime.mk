# W2-05: @todayz/realtime (SSE client, resume, backoff, polling fallback, query invalidation hook): lint, types, tests.

CHECK_STEPS += check-realtime

.PHONY: check-realtime

check-realtime:
	@pnpm -C $(ROOT) --filter @todayz/realtime lint
	@pnpm -C $(ROOT) --filter @todayz/realtime typecheck
	@pnpm -C $(ROOT) --filter @todayz/realtime test
