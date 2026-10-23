# W1-03: message lint for the four apps. The lint script is told where each app's source is
# (shared/i18n never reads sibling folders itself) and fails on a missing or empty folder.

CHECK_STEPS += check-i18n-messages

.PHONY: check-i18n-messages

check-i18n-messages:
	@pnpm -C $(ROOT) --filter @todayz/i18n exec node scripts/lint.mjs \
		--src customer=$(ROOT)/customer/src --src business=$(ROOT)/business/src \
		--src admin=$(ROOT)/admin/src --src site=$(ROOT)/site/src
