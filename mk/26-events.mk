# W1-07: the product event catalogue (shared/telemetry/events/<domain>.yaml, 05 §2.6, D-50).
#
# gen-events   eventgen validates the files (an event name in two files fails the build) and
#              writes backend/gen/events (the files + embed.go, loaded by eventcat) and
#              shared/telemetry/src/gen/events.ts (the typed emitter's event map). Both are
#              git-ignored.

GEN_STEPS += gen-events

.PHONY: gen-events

# The binary embeds backend/gen/events, so make dev and make reset generate it first.
backend-bin: gen-events

gen-events:
	@go -C $(BACKEND) run ./internal/devtools/eventgen -in ../shared/telemetry/events -go gen/events -ts ../shared/telemetry/src/gen/events.ts
