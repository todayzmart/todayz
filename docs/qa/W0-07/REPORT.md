# W0-07 Mock integrations: QA report

Date: 2026-10-22. Branch `wp/W0-07-mocks`. Built by the W0-07 agent; saved and verified by the lead.

## What was built

`e2e/mocks` is one Go service (stdlib only) on `MOCKS_ADDR` (default `127.0.0.1:4010`). It emulates these providers in their real shapes, so a production client works by swapping only the base URL:

| Provider | Base | Emulated |
|---|---|---|
| MSG91 SMS | `/msg91` | OTP send/retry/verify, flow v5; 401 without `authkey` |
| WhatsApp | `/whatsapp` | MSG91 bulk template and session; Gupshup message and template (202) |
| Web Push | `/push/{subscriptionId}` | 201 with Location; TTL/Urgency/Topic checks; 413 over 4096 B; `X-TZ-Mock-Plaintext` for readable title and body; `gone-404-*` gives 404, other `gone-*` gives 410 |
| Turnstile | `/turnstile/turnstile/v0/siteverify` | Cloudflare JSON; `XXXX.DUMMY.TOKEN.FAIL` gives `invalid-input-response`; an empty token gives `missing-input-response`; Cloudflare test secrets 1x/2x/3x |
| Sarvam STT | `/sarvam/speech-to-text` | Transcript from a fixture keyed by the audio's sha256; an unknown hash gets a deterministic 400 |
| Anthropic | `/anthropic/v1/messages` | JSON and SSE; fixtures chosen by the `metadata.user_id` tag `tz:<feature>:…`; no match gives a 400 naming the feature; record mode only when `MOCK_LLM=0` and a key is set |

Capture is an in-memory ring per kind (500). The JSON API is under `/_mock`: `GET /_mock/{kind}`, `GET /_mock/sms/latest-otp?to=`, `DELETE /_mock/reset`, and `GET /healthz`. The design-system UI is at `/`. Logs record method, route template, status and duration only (D-15). Full endpoint list with provider doc links: `e2e/mocks/README.md`.

`mk/30-mocks.mk` adds `setup-mocks` (builds `.tools/bin/tz-mocks`), `check-mocks` (gofmt, vet, `go test -race`, build) and `dev-mocks` (via `infra/local/proc.sh`).

## Tests

**78 top-level tests and 98 subtests pass under `-race`; 0 skipped, 0 failed** (after the review fixes below; before them: 62 and 75).

| Package | Tests | What they cover |
|---|---|---|
| main | 5 | fixtures load, WAV hash, `_recorded` never embedded, real `http.Server` on :0 with sentinels (no phone/OTP/key/Host in logs, every line JSON, 421 for a foreign Host) |
| capture | 6 | ring cap, filters, concurrency, 4 KB clipping without touching the caller's values |
| fixtures | 7 | match precedence, per-file errors, attribution by feature, `_` folders skipped unless replay |
| server | 55 | every route: shapes, errors, OTP extraction, MOCK_LLM gating with a fake upstream, record mode, log redaction, Host allowlist, 413/400 per provider, thinking/signature/ping/stream_error_after, broken-fixture isolation, invalid recording not written, strict phones, OTP single-use and expiry |
| ui | 5 | css drift, offline assets, CSS vars, no innerHTML/eval, `Object.hasOwn` tab check |

### Review fixes (second pass)

- Recordings never change fixture-mode results: `_recorded/` is skipped unless `MOCK_LLM_REPLAY_RECORDED=1`, not embedded (`//go:embed fixtures`), and not a make input.
- One bad fixture breaks only its own feature (500 naming the file); `/healthz` lists `fixture_errors`. Recordings are validated before they are written.
- `MOCK_LLM_UPSTREAM_URL` removed; the upstream is fixed at `https://api.anthropic.com`.
- Host allowlist (127.0.0.1, localhost, [::1], mocks.todayz.localhost, any port), else 421.
- Captured strings clipped to 4 KB; `http.Server.ErrorLog` goes to the JSON logger at WARN.
- Fidelity: ping after the first `content_block_start`, thinking start carries `"signature":""`, fixture field `stream_error_after`, `MOCK_TURNSTILE_HOSTNAME`, strict country-code phones on provider routes (lenient `/_mock` filters), MSG91 verify is single-use and honours `otp_expiry`. MSG91 WhatsApp base is `https://api.msg91.com`; SMS/OTP stays `https://control.msg91.com`.
- 413 only for an over-limit body, 400 for other read errors; WhatsApp templates render in one pass; `saveRecording` returns real errors.

The log redaction test was mutation-checked by logging `RequestURI`, which made it fail. No test reaches the internet.

## make check

The agent's run and the lead's re-run after rebasing onto `e2c2c21`, which makes make fail fast on 3.81, both pass: check-repo and check-mocks.

## Manual QA

The agent ran the mocks on a random free port with `MOCK_LLM=1` and no key, using synthetic numbers (+91 90000 000xx) and made-up codes. Results:

- MSG91: OTP and flow return 200; no authkey returns 401.
- WhatsApp: MSG91 returns 200; Gupshup returns 202.
- Push: 201 with Location; `gone-` returns 410.
- Turnstile: pass, fail token and empty token each give the right result.
- STT: the sample WAV returns the Hinglish transcript; unknown audio returns 400.
- LLM: voice_stock returns a tool_use; coach_card returns text; an unknown feature returns 400.

The 201 log lines contained no sensitive values, and SIGTERM stopped the server cleanly.

Screenshots in this folder: `ui-sms`, `ui-whatsapp`, `ui-push`, `ui-turnstile`, `ui-stt` and `ui-llm` (.jpg), plus `bug-stale-rows-before-fix.jpg`. They show synthetic values only.

### Bugs found and fixed

1. Switching tabs left stale rows on screen, and late replies from the old tab could still land.
2. The OTP template hard-coded 10 minutes. It now uses `##expiry##` from `otp_expiry`, because the expiry is configurable (01 §OTP).
3. The UI note about reserved numbers was misleading.

## Known gaps

- Push payloads are not decrypted; the readable content comes from the plaintext header.
- The MSG91 WhatsApp shape comes from public docs only; confirm it in Wave 5.
- Sarvam: sync STT only.
- Anthropic: Messages and count_tokens only (count_tokens is an estimate).
- Record mode is tested against a fake upstream only.
- Captures are in memory.
- `/_mock` has no auth; it relies on the 127.0.0.1 bind and the Host allowlist.
- The LLM table scrolls sideways inside its card below about 1300 px.

## Lead decisions on the agent's requests

- **Make 3.81 did not stop at a failing step.** Fixed on main in `e2c2c21`: the recipe shell wrapper plus a fail-fast `run_steps`, guarded by `scripts/test/make-fail-fast.sh`.
- **Synthetic OTPs in QA screenshots** are allowed: made-up codes on reserved numbers. Real codes never appear anywhere.
- **SMS/WhatsApp keys locally.** W1 (identity/notify) decides; the lead's default is placeholder keys written by `make setup` for local/test only.
- **E2E phone range, push contract, Turnstile base path and the `tz:<feature>:` tag** go to the plan (01/02/11) when W1-01 and W3-04 start.
