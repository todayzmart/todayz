# tz-mocks: provider mocks (W0-07)

One Go service (stdlib only) that stands in for every outside provider the
backend calls in local and test: AWS End User Messaging SMS (and the legacy
MSG91 SMS routes), WhatsApp (MSG91 and Gupshup), Web
Push, Cloudflare Turnstile, Sarvam speech-to-text and the Anthropic Messages API.
It answers in each provider's own request and response shapes, keeps what it
received in memory, and shows it in a small web UI and a JSON API, so tests can
read the OTP that was "sent" or the push that was "delivered".

The default suite needs no provider keys and reaches nothing outside the
machine (SC-PL-04). The only path that can leave the machine is LLM record
mode, which needs `MOCK_LLM=0` **and** `ANTHROPIC_API_KEY` set (see below).

## Run

```
make setup        # builds .tools/bin/tz-mocks (setup-mocks)
make dev          # starts it on 127.0.0.1:4010 through infra/local/proc.sh (dev-mocks)
make check        # gofmt, go vet, go test -race (check-mocks)
```

By hand: `go run .` in `e2e/mocks`, or `MOCKS_ADDR=127.0.0.1:0 .tools/bin/tz-mocks`
for a free port (the chosen address is in the "tz-mocks listening" log line).

UI: http://127.0.0.1:4010/ (https://mocks.todayz.localhost once Caddy routes it).

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `MOCKS_ADDR` | `127.0.0.1:4010` | Listen address. |
| `MOCKS_CAPACITY` | `500` | Records kept per kind (a ring: the oldest drop first). |
| `MOCKS_FIXTURES_DIR` | unset | Read fixtures from this directory (must exist). |
| `TODAYZ_ROOT` | unset | If set and `$TODAYZ_ROOT/e2e/mocks/fixtures` exists, fixtures come from there. |
| `MOCK_LLM` | unset (= fixtures) | `0` asks for live/record mode; anything else is fixtures only. |
| `ANTHROPIC_API_KEY` | unset | Used only when `MOCK_LLM=0`. Never logged, never stored, never echoed. |
| `MOCK_LLM_REPLAY_RECORDED` | unset | `1` also serves recordings from `fixtures/llm/_recorded/`. Off by default, so a recording never changes fixture-mode results. |
| `MOCK_TURNSTILE_HOSTNAME` | `localhost` | `hostname` in a passing siteverify reply (set it to the app's host if the backend checks it). |

The record-mode upstream is fixed at `https://api.anthropic.com`; there is no
variable to point it elsewhere (tests set `Config.AnthropicUpstream` in code).

Fixture source order: `MOCKS_FIXTURES_DIR`, then `$TODAYZ_ROOT/e2e/mocks/fixtures`,
then the copy embedded in the binary. Fixtures are read on every request, so
edits to a fixtures directory apply without a restart. `/healthz` reports which
source is in use. Files and folders starting with `_` or `.` are never embedded
(so `_recorded/` stays out of the binary and of `make` build inputs).

**A broken fixture breaks only its own feature.** Each file is loaded on its
own. A file that does not parse or validate is skipped and listed in
`/healthz` as `"fixture_errors": ["<path>: <reason>", …]`. An LLM request
returns 500 `api_error` naming the file only when the broken file belongs to
the request's feature (its `match.feature`, else its folder); other features
keep working. An STT miss while some STT file is broken is a 500 naming it.

**Host check.** The mock answers only requests whose `Host` is `127.0.0.1`,
`localhost`, `[::1]` or `mocks.todayz.localhost` (any port). Anything else gets
421, so a page on another origin that DNS-rebinds its name to 127.0.0.1 cannot
read captured OTPs. Caddy keeps the original Host when it proxies
`https://mocks.todayz.localhost`, which is why that name is allowed.

## Base URLs for the backend

| Backend variable | Value in local/test | Real provider |
|---|---|---|
| `SMS_API_URL` | `http://127.0.0.1:4010/sms-voice` | none: deployed, the SDK uses the AWS regional endpoint for `AWS_REGION` and the instance role (D-212) |
| `WHATSAPP_API_URL` | `http://127.0.0.1:4010/whatsapp` | MSG91 WhatsApp: `https://api.msg91.com` (not `control.msg91.com`, which is SMS/OTP); Gupshup: `https://api.gupshup.io` |
| `PUSH_API_URL` | `http://127.0.0.1:4010/push` | the subscription's own endpoint |
| `TURNSTILE_API_URL` | `http://127.0.0.1:4010/turnstile` | `https://challenges.cloudflare.com` |
| `STT_API_URL` | `http://127.0.0.1:4010/sarvam` | `https://api.sarvam.ai` |
| `LLM_API_URL` | `http://127.0.0.1:4010/anthropic` | `https://api.anthropic.com` |

The backend appends the provider's own path to the base URL, exactly as it would
against the real host.

**Upgrading from MSG91 (D-212).** An old `.env.local` with
`SMS_API_URL=http://127.0.0.1:4010/msg91` still wins over `infra/local/local.env`, and the
backend now posts the AWS `SendTextMessage` call to that base. The mocks answer 404, so
login fails. Change the line to `SMS_API_URL=http://127.0.0.1:4010/sms-voice`, or delete
it, since `local.env` already has that value. Also delete any `MSG91_*` lines.

## Provider endpoints

All responses are JSON unless noted. Auth headers are checked for presence only;
their values are never read further, logged or stored.

**Phone numbers.** Provider routes are strict, like the real APIs: a number is
the country code and number as digits (`919000000001`; a leading `+` is
allowed), 11 to 15 digits, no leading 0, and a `91` number has exactly 12
digits. A bare 10-digit number is refused with 400, so a backend that drops the
`91` fails here and not in production. The `/_mock` filters stay lenient
(`9000000001`, `+91…`, `091…` all work).

**Body limits.** Over a route's limit is 413 in the provider's error shape;
any other unreadable body is 400 (Turnstile: 200 with `bad-request`).

### AWS End User Messaging SMS (`/sms-voice`)

Docs: https://docs.aws.amazon.com/pinpoint/latest/apireference_smsvoicev2/API_SendTextMessage.html

| Route | Notes |
|---|---|
| `POST /sms-voice` (and `/sms-voice/`) | `SendTextMessage`, AWS JSON 1.0: `X-Amz-Target: PinpointSMSVoiceV2.SendTextMessage`, `Content-Type: application/x-amz-json-1.0`, a SigV4 `Authorization` header (presence only; the backend signs with dummy credentials locally). Body `DestinationPhoneNumber` (E.164 with `+`), `MessageBody` (1 to 1600 characters), `OriginationIdentity`, `MessageType` (`TRANSACTIONAL`/`PROMOTIONAL`), `TimeToLive` (5 to 259200 s), `DestinationCountryParameters`, `DryRun`. 200 `{"MessageId"}`; `DryRun` records nothing. |

A `+91` number also needs `OriginationIdentity` (the DLT sender ID) and
`IN_ENTITY_ID` plus `IN_TEMPLATE_ID`, as India's DLT rules require. When
`IN_TEMPLATE_ID` is listed in `fixtures/sms/dlt-templates.json` (the local ids
from `infra/local/local.env`), `MessageBody` must be that template with each
`{#var#}` filled by at most 30 characters, or the mock answers 400 (the real
operator would drop it silently after AWS accepted it). Unlisted ids are accepted.
Errors use the AWS JSON 1.0 shape: `X-Amzn-ErrorType` and
`{"__type","Message"}`; 403 `MissingAuthenticationTokenException` without a
signature, 400 `UnknownOperationException`, `SerializationException` or
`ValidationException`. Messages never repeat the phone or the text. Each send
is captured with `api: "send_text_message"`, `sender`, `entity_id`,
`template_id` and `body`; the OTP is extracted from the body (below).

### Legacy MSG91 SMS (`/msg91`)

The backend no longer calls these routes (D-212); they stay until the lead
removes them.

Docs: https://docs.msg91.com/otp/sendotp, https://docs.msg91.com/otp/verify-otp,
https://docs.msg91.com/otp/retry-otp, https://docs.msg91.com/sms/send-sms

| Route | Notes |
|---|---|
| `POST /msg91/api/v5/otp` | `authkey` header (or query). `mobile`, `template_id`, `otp`, `otp_length`, `otp_expiry` in the query, or the JSON body as a fallback; other body keys are template variables. Without `otp` the mock makes one (`otp_length`, default 4). `otp_expiry` fills `##expiry##`. 200 `{"request_id","type":"success"}`. |
| `GET\|POST /msg91/api/v5/otp/retry` | Resends the last OTP to `mobile`. 200 `{"type":"success","message":"retry send successfully"}`; 400 when none was sent or it was already verified. |
| `GET /msg91/api/v5/otp/verify` | `otp`, `mobile`. Checks the last code sent to that mobile. 200 `{"type":"success","message":"OTP verified success"}`, else 400 with `OTP not match`, `OTP expired` (after `otp_expiry` minutes, default one day) or `Mobile no. already verified` (a code works once). A new send replaces the code; `DELETE /_mock/sms` or `/_mock/reset` forgets all codes. |
| `POST /msg91/api/v5/flow` | Template SMS. `{"template_id","short_url","recipients":[{"mobiles":"91…","VAR":"…"}]}` (`flow_id` also accepted). One record per recipient. 200 `{"type":"success","message":"<request id>"}`. |

Errors use MSG91's envelope `{"type":"error","message":"…"}`: 401 without an
authkey, 400 for a missing or invalid mobile, a non-digit OTP or bad JSON.
Template text comes from `fixtures/sms/templates.json` (`##VAR##` placeholders);
an unknown OTP template falls back to "##OTP## is your TodayZ verification code. Do not share it with anyone."

**OTP extraction.** Each captured SMS stores `otp`: the MSG91 `otp` parameter if
given (4 to 9 digits), else a template variable whose name contains "otp",
else the only 6-digit run in a text that mentions an OTP, code or verification. `GET /_mock/sms/latest-otp?to=` reads it back.

### WhatsApp (`/whatsapp`)

MSG91: https://docs.msg91.com/whatsapp. Gupshup: https://docs.gupshup.io/reference/msg

| Route | Notes |
|---|---|
| `POST /whatsapp/api/v5/whatsapp/whatsapp-outbound-message/bulk/` | MSG91 template message (`integrated_number`, `content_type:"template"`, `payload.template.{name,language,to_and_components[]}`). `authkey` header. 200 `{"status":"success","hasError":false,"data":{"message":"Message sent successfully","request_id"},"errors":null}`. |
| `POST /whatsapp/api/v5/whatsapp/whatsapp-outbound-message/` | MSG91 session (free-form) message. Same envelope. |
| `POST /whatsapp/wa/api/v1/msg` | Gupshup, form body (`source`, `destination`, `message`, `src.name`). `apikey` header. 202 `{"status":"submitted","messageId":"<uuid>"}`. |
| `POST /whatsapp/wa/api/v1/template/msg` | Gupshup template, `template={"id","params":[…]}`. 202 as above. |

Errors: MSG91 `{"status":"fail","hasError":true,"data":null,"errors":"…"}`; Gupshup
`{"status":"error","message":"…"}`. Template text comes from
`fixtures/whatsapp/templates.json` (`{{1}}`, `{{2}}` placeholders, filled in one
pass: a parameter that contains `{{2}}` is kept as text).

The MSG91 WhatsApp API lives on `https://api.msg91.com` (its docs use that host
for `/api/v5/whatsapp/...`), while SMS and OTP use `https://control.msg91.com`.
The backend therefore has a separate `WHATSAPP_API_URL`.

### Web Push (`/push`)

Docs: RFC 8030 (https://www.rfc-editor.org/rfc/rfc8030), RFC 8291 (message
encryption), RFC 8292 (VAPID).

`POST /push/{subscriptionId...}`, where the subscription id is any path.

- Requires `TTL` (a non-negative integer). Optional `Urgency` (very-low, low,
  normal or high), `Topic` (at most 32 base64url characters) and
  `Content-Encoding` (aes128gcm or aesgcm; required when there is a body).
- `Authorization` (VAPID) is recorded as present or absent only.
- Success returns 201 Created with a `Location` header.
- A body over 4096 bytes returns 413. A bad header returns 400.
- A subscription id starting `gone-404-` returns 404; any other `gone-` prefix
  returns 410. Use these to test that the backend prunes dead subscriptions.
- The real payload is encrypted, so the mock cannot read it. In local and test
  the backend also sends the notification JSON in the header
  `X-TZ-Mock-Plaintext`, either raw or base64url. The mock records its
  `title` and `body`.

### Cloudflare Turnstile (`/turnstile`)

Docs: https://developers.cloudflare.com/turnstile/get-started/server-side-validation/,
https://developers.cloudflare.com/turnstile/troubleshooting/testing/

`POST /turnstile/turnstile/v0/siteverify` (the real path under the base URL;
`/turnstile/v0/siteverify` and `/turnstile/siteverify` also work). Form or JSON
`secret`, `response`, `remoteip`, `idempotency_key`. Always HTTP 200.

- Success: `{"success":true,"challenge_ts","hostname":"localhost","error-codes":[],"action":"","cdata":"","metadata":{"interactive":false}}`.
  `hostname` is `MOCK_TURNSTILE_HOSTNAME` (default `localhost`).
- A body over 64 KB is 413 `{"success":false,"error-codes":["bad-request"],…}`.
- Failure: `{"success":false,"error-codes":[…],"messages":[]}`.
- `missing-input-secret` when there is no secret; `missing-input-response` when the token is empty.
- `invalid-input-response` for the magic token `XXXX.DUMMY.TOKEN.FAIL`, for
  Cloudflare's always-fail test secret `2x0000000000000000000000000000000AA`,
  or for a token over 2048 characters.
- `timeout-or-duplicate` for the "spent" test secret `3x0000000000000000000000000000000AA`.
- Any other non-empty token passes. Cloudflare's client test sitekeys produce
  `XXXX.DUMMY.TOKEN.XXXX`, which therefore passes.
- The mock records only the first 24 characters of the token and whether a
  secret was present (or which test secret it was), never the secret's value.

### Sarvam speech-to-text (`/sarvam`)

Docs: https://docs.sarvam.ai/api-reference-docs/speech-to-text/transcribe

`POST /sarvam/speech-to-text` and `POST /sarvam/speech-to-text-translate`,
multipart with `file` (plus optional `model`, `language_code`,
`with_timestamps`, `with_diarization`, `prompt`). The audio's sha256 selects a
fixture in `fixtures/stt/*.json`. 200
`{"request_id","transcript","timestamps":null,"diarized_transcript":null,"language_code"}`
(`with_timestamps=true` fills word timestamps; translate returns the fixture's
`translation` and has no `timestamps`). An unknown hash is a 400
`{"error":{"message":"tz-mock: no STT fixture for audio sha256 …","code":"invalid_request_error","request_id"}}`,
so a missing fixture never looks like silence. Missing or empty `file` is 400; more than
32 MB is 413. `api-subscription-key` is optional here.

Sample: `fixtures/stt/stock-update-hinglish.wav` (a 0.25 s tone; its hash is the
key) transcribes to "do kilo atta aur paanch Amul doodh bik gaye".

To add one: drop the audio next to a JSON file with
`{"sha256","language_code","transcript","translation"}` (`shasum -a 256 file.wav`).

### Anthropic Messages (`/anthropic`)

Docs: https://docs.claude.com/en/api/messages,
https://docs.claude.com/en/api/messages-streaming, https://docs.claude.com/en/api/errors

| Route | Notes |
|---|---|
| `POST /anthropic/v1/messages` | Requires `anthropic-version`. `x-api-key` is not checked in fixture mode. JSON or SSE (`"stream": true`). |
| `POST /anthropic/v1/messages/count_tokens` | Always local: `{"input_tokens": <len/4>}`. |

The request is validated like the API: `model`, `max_tokens > 0`, `messages`
non-empty with string or block content. Errors use Anthropic's shape
`{"type":"error","error":{"type","message"},"request_id"}` with a `request-id`
header.

- **Feature tag.** The backend sets `metadata.user_id` to `tz:<feature>:<id>`,
  for example `tz:voice_stock:store-123`. The feature picks the fixture folder.
  With no tag the mock returns 400 `invalid_request_error`, and the message says
  how to set the tag.
- **Fixtures.** Each fixture lives at `fixtures/llm/<feature>/<name>.json`:

  ```json
  {
    "description": "why this fixture exists",
    "match": {"contains": ["do kilo atta"], "tool": "record_stock_lines"},
    "response": {
      "content": [{"type": "tool_use", "name": "record_stock_lines", "input": {"lines": []}}],
      "stop_reason": "tool_use",
      "usage": {"input_tokens": 0, "output_tokens": 0}
    }
  }
  ```

  - `match.feature` defaults to the folder name.
  - `contains` is case-insensitive and is checked against all the request text:
    the system prompt, every message, tool results and tool inputs.
  - `tool` must be one of the request's `tools[].name`.
  - Instead of `response`, a fixture may set
    `"error": {"status": 529, "type": "overloaded_error", "message": "Overloaded"}`.
  - `stop_reason` and `usage` are filled in when left out: `tool_use` if any
    block is a tool_use, else `end_turn`, and token counts estimated from the
    text length.
  - Block types: `text`, `tool_use`, `thinking` (with `thinking` and optional
    `signature`) and `redacted_thinking`. Unknown fixture fields are refused.
  - `"stream_error_after": N` (with a `response`) makes a streamed reply stop
    after N events with `event: error`
    `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
    as the API does mid-stream. A non-streamed request is unaffected.
- **Precedence.** The most specific match wins: the number of `contains`
  strings, plus one for `tool`. Ties go to the first path in sorted order. The
  chosen file is in the `X-TZ-Mock-Fixture` response header and the LLM tab.
- **No match** returns 400 `invalid_request_error` with the message
  "tz-mock: no LLM fixture matches feature "<f>" (tools [...]); add one under
  e2e/mocks/fixtures/llm/<f>/". It never returns an invented answer.
- **The reply** is a full Message: `id` (`msg_…`), `type`, `role`, `model` (the
  requested one), `content`, `stop_reason`, `stop_sequence`, and `usage` with
  `input_tokens`, `output_tokens`, `cache_creation_input_tokens`,
  `cache_read_input_tokens` and `service_tier`. The headers are `request-id`
  and `anthropic-ratelimit-*`.
- **Streaming** sends `message_start`, then for each block
  `content_block_start`, its deltas (`text_delta`, `input_json_delta`, or
  `thinking_delta` then `signature_delta`) and `content_block_stop`, then
  `message_delta` and `message_stop`. One `ping` follows the first
  `content_block_start`, as in the API. A thinking block starts as
  `{"type":"thinking","thinking":"","signature":""}`.
- **Example fixtures:**
  - `voice_stock/stock-update-hinglish.json` gives `record_stock_lines` tool_use
    lines for the sample utterance.
  - `voice_stock/fallback.json` returns one unmatched line for anything else.
  - `coach_card/default.json` returns a short text card.
  - `mock_overloaded/default.json` returns a 529, for retry tests.

**Live and record mode.** This mode is on only when `MOCK_LLM=0` and
`ANTHROPIC_API_KEY` is non-empty. If `MOCK_LLM` is unset or any other value, the
mock never forwards a request, even when a key is set. In live mode:

- The request goes to the upstream with `stream: false`. The mock sends its own
  key as `x-api-key`; the caller's key is never forwarded. It passes on
  `anthropic-version` and `anthropic-beta`.
- The upstream's `request-id`, `retry-after` and rate-limit headers are copied
  back to the caller.
- Non-200 replies pass through unchanged and are not recorded.
- A 200 reply is saved as a fixture, `fixtures/llm/_recorded/<feature>-<time>-<rand>.json`.
  Its match is the feature, a snippet of the last user message of up to 120
  characters, and the forced tool if any. The path is in `X-TZ-Mock-Recorded`.
- A recording is validated like any fixture before it is written. If it would
  not load (for example a block type the loader does not know, such as
  `server_tool_use`), nothing is written, the reply still passes through, and
  the LLM tab shows `not recorded: …`.
- Recordings are not served in fixture mode unless `MOCK_LLM_REPLAY_RECORDED=1`,
  and they are never embedded in the binary, so recording cannot change what
  `make check` sees.
- Recording happens only when fixtures come from a directory; the embedded copy
  is read-only.
- If the caller asked for a stream, the reply is replayed to it as SSE.
- If the upstream cannot be reached, the mock returns 502 `api_error`.
- Recordings hold the request text and the reply, never the key. Review a
  recording before you commit it, and move it into the feature's folder.

`make check` runs under `scripts/no-egress.sh`, which sets `MOCK_LLM=1` and
unsets the keys, so it never reaches the network.

## JSON API (`/_mock`)

| Route | Returns |
|---|---|
| `GET /_mock/{kind}` | `{"items":[…]}`, newest first. Kinds: `sms`, `whatsapp`, `push`, `turnstile`, `stt`, `llm`. Filters: `to` (sms, whatsapp; any phone format, `+91…`, `91…` or 10 digits), `subscription` (push, exact id), `feature` (llm), `limit`. An unknown kind returns 404. |
| `GET /_mock/sms/latest-otp?to=` | `{"otp","at"}` for the newest SMS with a code to that phone, else 404 `{"error":"no OTP for that phone"}`. Optional `since=<RFC 3339>` ignores older codes; `wait=<seconds, ≤10>` polls until one arrives. `to` is required (400). |
| `GET /_mock/summary` | `{"counts":{kind:n},"kinds":[…],"llm_mode":"fixtures"\|"live"}` |
| `DELETE /_mock/{kind}` | Clears one kind. 204, or 404 for an unknown kind. |
| `DELETE /_mock/reset` | Clears everything. 204. |
| `GET /healthz` | `{"status":"ok","llm_mode","fixtures":"embedded"\|"<dir>","replay_recorded":false,"fixture_errors":[]}` |

Every captured string (bodies, variables, prompts) is clipped to 4 KB before it
is stored, ending in `…[clipped]`.

A Playwright login step, for example:

```ts
// `phone` must NOT be a reserved +91 90000 000xx number: in local/test the
// backend accepts 000000 for those and never calls SMS (01-product-rules).
const since = new Date().toISOString();
await page.getByRole('button', { name: 'Send code' }).click();
const r = await request.get(`${MOCKS}/_mock/sms/latest-otp?to=${phone}&since=${since}&wait=5`);
const { otp } = await r.json();
```

## UI

`GET /` shows tabs for SMS, WhatsApp, Push, Turnstile, STT and LLM.

- Rows are newest first. Phones are shown in full ("+91 90000 00001"), and SMS
  has an OTP column; click an OTP to copy it.
- The page refreshes every 2 s, can be paused, and has a clear button per tab.
  The open tab is kept in the URL hash.
- It is built from the TodayZ design system. `internal/ui/assets/tz.css` is
  generated from `design/source/tokens.json` and `components/bundle.css` by
  `go run ./cmd/gencss`, with the `@import` lines removed. It uses system fonts,
  the light theme only, and `tz-` classes. A test fails if `tz.css` drifts from
  the design system.
- The UI loads nothing from the network. The CSP is `default-src 'self'`.
  Captured data is rendered with `textContent` only.

The UI and API show phones and OTPs in full, because they exist to read them.
They are for local use only: the mock binds to 127.0.0.1 by default and stores
nothing on disk except LLM recordings. Use the reserved test numbers
`+91 90000 000xx` and synthetic codes, and never paste what the UI shows into a
snapshot or report (e2e/CLAUDE.md).

## Logging

Each request produces one JSON line holding `method`, `route` (the route
template, such as `/push/{subscriptionId...}`, or `unmatched`), `status` and
`duration_ms`. Nothing else is logged: no bodies, query strings, phones, OTPs,
tokens, keys or auth headers (D-15). A panic is recovered as a 500 and its value
is not logged. A request refused by the Host check logs `route:"host-rejected"`
without the Host. net/http's own messages go through the same JSON logger at
WARN (`http.Server.ErrorLog`). `internal/server/log_test.go` drives every provider with
sentinel values and fails if any of them reaches the log.

## Layout

```
main.go                     env, fixture source, listen, graceful stop
fixtures/                   llm/<feature>/*.json, llm/_recorded/, stt/, sms/, whatsapp/ (embedded)
internal/capture/           generic ring buffer + per-kind records
internal/fixtures/          fixture loading, validation, LLM matching
internal/server/            one file per provider, api.go (/_mock), server.go (routing, logging)
internal/ui/                embedded UI (index.html, app.js, mocks.css, generated tz.css)
internal/ui/cssgen, cmd/gencss   tz.css generator
```
