# W3-10: the AI platform's evals (11 §1 rule 8; backend/internal/ai/eval).
#
# check-ai-eval   every registered suite on its recorded replies (fixtures in each feature's
#                 eval/ folder) through the real client and a loopback replay server: no key,
#                 no database, no network, so it runs inside make check under no-egress. It
#                 fails when a suite scores under its pass bar.
# ai-eval-live    (make ai-eval; opt-in, costs money) the same suites against the live model
#                 with your own ANTHROPIC_API_KEY from .env.local and the models from
#                 platform_settings, so the local stack's database must be up (make dev).
#                 LLM_API_URL in infra/local/local.env points at the mocks; the step points it
#                 at AI_EVAL_LLM_API_URL instead. Without the key it lists the missing names
#                 and stops. AI_EVAL_SUITE picks one suite (default all).

CHECK_STEPS += check-ai-eval
AI_EVAL_STEPS += ai-eval-live

AI_EVAL_SUITE ?= all
AI_EVAL_LLM_API_URL ?= https://api.anthropic.com

.PHONY: check-ai-eval ai-eval-live

check-ai-eval:
	@go -C $(BACKEND) run ./cmd/todayz ai eval all

ai-eval-live: backend-bin
	@source $(BACKEND_LIB); tz_load_app_env; \
	  LLM_API_URL="$(AI_EVAL_LLM_API_URL)" MOCK_LLM=0 $(BACKEND_BIN) ai eval $(AI_EVAL_SUITE) --live
