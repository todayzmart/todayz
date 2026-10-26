# W3-12 Consumer AI: QA report

W3-12 adds the customer's AI helpers:

- AI-C1 list parse (C-22 basket editor)
- AI-C2 search assist (C-05)
- AI-C5 voice search (C-23 mic on C-05)
- AI-C3 substitutes, rule-based (C-07, C-08, C-09)
- the deterministic refine-chip parser that search reads (D-101)
- recorded evals for the three model or speech features

Every AI answer is a draft the customer confirms. Nothing joins a basket, a cart or a search until the customer taps it.

- **Branch:** `claude/agitated-goodall-8937ce` on base `5beb1ed6`. Current `main` is merged in (see the end). Not pushed.
- **Local only:** testcontainers or the shared test Postgres, the recorded model and speech replies, and Playwright against a faked API. Nothing called a model, a speech provider or a cloud account.
- **Decisions:** D-231 (the features call, refine chips in search, voice search) and D-235 (rule-based substitutes and `substitute_pack_tolerance_pct`), numbers given by the lead.
- **Migration:** `31201_w3-12_substitutes.sql` adds `substitute_pack_tolerance_pct` (count, default 25, min 5, max 100; 01 §17, admin group "asks" beside `refine_ai_min_words`). The min and max are my choice.
- **Rules:** 11 §AI-C1, C2, C3, C5; 01 §11, §11.1, §17.
- **Scenarios:** SC-AI-41…55.
- **Tests named in the brief:** ASK-16, ASK-26, ASK-27, BSK-15.

## What was built

### Contract and routes

`backend/api/paths/ai.yaml` and `schemas/ai.yaml` add these routes. They need `me.ai.use` (`platform/authz/policy/ai.go`):

| Route | What it does |
|---|---|
| `GET /v1/me/ai/features` | Which customer AI features are on, so the app shows only those. While a feature is off or its key is missing, its own routes give the common 404. |
| `POST /v1/me/ai/list-parse/drafts` | List parse: create a draft |
| `GET …/drafts/{draftId}` | List parse: read a draft |
| `POST …/drafts/{draftId}/confirm` | List parse: confirm a draft |
| `POST /v1/me/ai/voice-search` | Voice search: multipart audio and an optional language |
| `POST /v1/me/ai/search-assist` | Search assist |

`backend/api/paths/discover.yaml` adds `GET /v1/discover/stores/{slug}/substitutes?itemId&lat&lng&radius` (public, like the rest of discover). It returns `DiscoverSubstitutes {sameShop ≤ 3, nearby ≤ 3, serverNow}`, at most 6 rows.

### Backend

**`internal/ai/listparse` (AI-C1)**

- A typed, pasted or spoken list in en, hi, kn, te, ta or a mix becomes a draft of basket lines.
- The list reaches the model with phones, codes and ids removed, and the list itself is never stored.
- Grounding: a line may carry no number the list does not have (number words in the five languages count). A line that does is dropped.
- At most `basket_max_items` lines.
- Confirm writes nothing. It answers the accepted and edited lines, the app adds them to the editor, and the customer saves the basket.
- BSK-15: parsing creates no basket, no `search_log` row and no catalogue lookup.

**`internal/ai/querynorm`**

- `chips.go` is the deterministic refine-chip parser, with no model. It reads:
  - price caps ("under 100", "₹100 से कम", …)
  - open now, pickup, delivery, offers and near me
  - master-catalogue brands, in the five languages
- `read.go` is the `search.ChipReader` that search uses.
- Search assist (AI-C2) only suggests:
  - It is called by the app after results render, only when search answered `refineHint`, and only when signed in.
  - It has a 3 s timeout (`SearchAssistTimeout`, an 01 §17 fixed constant).
  - Any failure, invalid output or empty suggestion is 204, and the results stay as they are.
  - Only allowed chip kinds, one of each. A price must be a number in the query, and a brand a catalogue slug.
  - It stores nothing: no `ai_drafts` row (D-101), no `search_log` row, and the query is not logged.

**`internal/ai/voicesearch` (AI-C5)**

- It reads one recording through voice stock update's form reader and limits (`voice.ReadRecording`, `MaxSeconds`, `MaxAudioBytes`).
- It sends the recording to the speech provider and returns the transcript (at most 200 characters) plus the chips the parser read.
- It never searches: the transcript fills the box, and the customer searches with `source=voice`.
- It keeps nothing: no draft, object, audio or transcript in a log, trace, event or metric label (ASK-16, ASK-26). The provider call is metered by outcome only.
- Each use counts against `ai_daily_calls_customer`; past the cap it answers 429 `ai_daily_cap_reached` and does not call the speech provider.
- A provider failure is 503, and C-23 then hides the mic for the session.

**`internal/search`**

- **Picked up W1-06's TODO(lead)** (the lead's answer of 26 Oct; W1-06 is closed): `refine.go` makes search read the chips from `q` unless `parse=false`, and answers them with the remaining words. The change to `search_run.go` is the `Parse` field only.
- The parser itself is `internal/ai/querynorm/chips.go` (deterministic; en, hi, kn, te, ta and their digits), not `internal/search/chips.go` as the answer named. Voice search and search assist use the same parser, and domain packages do not import each other's internals, so search reads it through the `search.ChipReader` interface that `cmd/todayz` wires. `refine_ai_min_words` is read through `settings.Cache`.
- `refineHint` is true when parsing was on, no chip was read and the query has at least `refine_ai_min_words` words. This is the parser half of 01 §11.1; see Questions.
- `substitutes.go` holds the AI-C3 rules (D-235). There is no model and no flag.
  - **Same shop:** in-stock items of other products in the item's category, with a pack within `substitute_pack_tolerance_pct` (default 25%) either way, in base units (g, ml, pc, pack). Same brand first, then nearest pack, then price. At most 3.
  - **Nearby:** the same product in stock at other live shops of verified businesses inside the search radius (the contract's `radius`, default 2 km, as in search), nearest first, one item (the cheapest) per shop. At most 3.
  - Not logged.
- New queries are in `db/queries/search/search.sql`.

**`cmd/todayz`**

- `ai.go`: features, flags and the recorded model.
- `routes_ai.go`, `routes_search.go`: wiring the routes.
- `ai_consumer_http_test.go`: the HTTP tests.

**`internal/voice/handlers.go`**

- Two exported wrappers, `ReadRecording` and `Filename`, so voice search reuses the same form rules (see Questions).

**Evals:** `listparseeval`, `querynormeval` and `voicesearcheval` replay the recorded fixtures.

### Customer app

- `src/ai/features.ts` reads `GET /v1/me/ai/features`.
- `src/ai/events.ts` emits the events `voice_search_used`, `search_assist_used` and `list_parse_used` (`shared/telemetry/events/ai.yaml`).
  - They carry outcome, language and counts only, never the words.
  - `main.tsx` connects them to telemetry.
- **`src/voice-search/` (C-23)**
  - `VoiceSearch` mic in the C-05 search box, `recorder.ts` (MediaRecorder) and `session.ts` (hides the mic for the session after a 503).
  - States: listening, the transcript in the box, capped ("Type instead") and failed.
- **`src/search-assist/` (C-05)**
  - "Did you mean" chips under the results, only for filters that are not already on.
  - A tap applies the chip, and nothing applies without a tap.
- **`src/list-parse/` (C-22)**
  - "Paste a list" in `BasketEditor`, with a review screen: each line can be accepted, edited or dropped.
  - "Add" puts the chosen lines into the editor, and the basket autosaves as before.
- `src/search/SearchPage.tsx` and `slots.tsx`:
  - The refine chips from the search answer are shown, and removing one re-runs the search.
  - The voice and assist slots are filled.
- **`src/substitutes/` (AI-C3)**
  - "See similar items" shows on an out-of-stock item:
    - the C-07 item card (`store/ItemCard.tsx`)
    - C-08 (`product/ProductPage.tsx`)
    - an `out_of_stock` cart line (`cart/CartPage.tsx`)
  - It opens a sheet with two sections, "In this shop" and "Same item at other shops".
  - A row opens that item's page and the sheet closes. The sheet changes nothing in the cart.
  - It is hidden until the place has coordinates, and it asks the API only when the sheet opens.
- **Copy:** in en, hi, kn, te and ta, marked `reviewed`, with a row in `docs/qa/W4-05-hindi-customer/REVIEW.md`. 44 keys:
  - 9 `search.voice.*`
  - 5 `search.assist.*`
  - 21 `basket.paste.*` (new keys, none renamed)
  - 9 `substitutes.*`
- **Mocks:** LLM fixtures in `e2e/mocks/fixtures/llm/{list_parse,search_assist}`.

## Tests

### Backend

All Go tests ran with `-p 2` against the shared test Postgres.

| Test | Covers |
|---|---|
| `TestAIConsumerListParseBSK15` | BSK-15: parse and confirm create no basket, no `search_log` row and no catalogue call |
| `TestAIConsumerListParseOwnership` | Another customer's draft is the common 404 |
| `TestAIConsumerListParseEmpty` | Nothing found, and grounding failures |
| `TestAIConsumerVoiceSearchKeepsNothing` | ASK-16, ASK-26: no audio or words in a log, a row or an object |
| `TestAIConsumerVoiceSearchProviderFailure` | 503 |
| `TestAIConsumerVoiceSearchCap` | 429 at the daily cap; speech is not called past it |
| `TestAIConsumerSearchAssist` | Allowed kinds only; nothing stored |
| `TestAIConsumerSearchAssistFailuresLeaveSearchAlone` | ASK-27: a timeout, a provider error or invalid output is 204 |
| `TestAIConsumerSearchReadsRefineChips` | Search answers chips and `refineHint` |
| `TestAIConsumerFlags` | Each feature off is the common 404, and `/v1/me/ai/features` agrees |
| `querynorm` `TestParse` | The chip parser, one table per language (en, hi with Hinglish, kn, te, ta), with each script's digits |
| `search` `TestRefineChips`, `TestRefineWithoutReader` | Search with and without the chip reader |
| `search` `TestSubstitutes` | Pack tolerance (25 and, changed, 5), same brand first, the 3-row cap, out-of-stock and other-shop exclusions, nearby nearest first |
| `platform/settings` `TestSettingsMatchProductRules`, `TestKeysMatchMigration` | The new key is in 01 §17, the migration and `keys.go` |

Static checks:

- gosec on `./internal/ai/...` is clean.
- The settings lint is clean (the list limit carries `//settings:ok`).
- golangci-lint is clean on the changed packages.
- `make gen`, `scripts/no-egress.sh make gen check-contract` and `scripts/no-egress.sh make contract-smoke` are green.
- Rerun on 26 Oct after the lead's answers: the Go tests for search, ai, voice, platform/settings, admin, migrate, db and cmd/todayz pass, except one run of W2-04's `TestPOSOfflineOfferMatchesEngine` (`pos_round2_http_test.go:328`, entry 4 "failed" instead of "linked"). It passes 3/3 alone; it signs TOTP codes at minute offsets, so it looks like a time-window flake, not W3-12. gosec, settingslint, golangci, check-contract, contract-smoke, the customer checks and the mocked Playwright specs (`ai`, `smoke`: 11/11) pass again.
- Then `main` at `5c371423` (W3-13 admin AI, W4-07, the zod bump) was merged in. The conflicts were in `sqlc.yaml`, `ai.yaml` (paths and schemas), `cmd/todayz/ai.go`, `routes_ai.go`, backend `CLAUDE.md` and the decision log; each side only added, so both sides were kept. After the merge everything passes again: `make gen`, the Go tests above plus `internal/support`, golangci, gosec, settingslint, check-contract, contract-smoke, customer lint, tsc, vitest 195/195, `build:e2e`, size 143.84 kB, every mocked Playwright spec 28/28, and `shared/i18n` 198/198.

### Recorded evals

These replay recorded model and speech replies:

| Suite | Fixtures | Gate | Result |
|---|---|---|---|
| list parse | 200 | 95% | 200/200 exact |
| search assist | 100 | 90%, allowed kinds only | 100/100 exact |
| voice search | 100 speech replies | 100% chip reading | 100/100 exact |

The p95 figures in the run are replay times in microseconds. The real latency target (2.5 s) needs the live, opt-in `make ai-eval`, which W3-12 did not run.

### Customer app

- tsc and eslint are clean.
- vitest: 21 files, 195 tests.
  - `src/test/ai.test.tsx` covers the mic states, the assist chips and the list review.
  - `src/test/substitutes.test.tsx` (7 tests) covers:
    - C-08: open, ask only on open, the query, navigate and close, empty, error with retry, in stock, no place
    - C-07: only the out-of-stock card shows the button
    - C-09: an out-of-stock cart line; after Close the cart is unchanged and "Remove item" is still there
- `build:e2e`, the precache check and the dist check are green.
- size-limit: 143.84 kB of 170 kB initial JS, 9.68 kB of 20 kB CSS.
- Playwright, every mocked spec: 28/28. `e2e/ai.spec.ts` has six tests at 360 px.
- `shared/i18n` vitest is green; `lint:messages` reports 0 errors for customer.

### Screenshots

All at 360 px, in this folder:

| Screen | Files |
|---|---|
| C-23 voice search | `customer-c23-mic-360.png`, `-listening-`, `-transcript-`, `-capped-` |
| C-05 search assist | `customer-c05-assist-360.png`, `-assist-applied-` |
| C-22 list parse | `customer-c22-list-360.png`, `-list-review-`, `-list-added-` |
| AI-C3 substitutes | `customer-c07-substitutes-360.png`, `customer-c08-substitutes-button-360.png`, `customer-c08-substitutes-sheet-360.png` |

## Open points

- **Production build:** `pnpm run build` fails locally on `VITE_TURNSTILE_SITE_KEY must be a real Turnstile site key`, as designed. `build:e2e` and `tsc` pass.
- **Live latency:** p95 and model quality on real providers are unmeasured until someone runs `make ai-eval` with real keys.
- **Voice:**
  - The chips voice search reads are returned, but the UI ignores them: the transcript goes into the box and search reads the chips again.
  - A spoken list is not yet a path into list parse.
  - `source=voice` stays in the URL after the customer edits the query.
- **Search:**
  - A brand chip's label shows the brand slug, not its display name.
  - Removing one chip re-runs the search with `parse=false`, so every chip goes (`SearchPage.tsx:388`, TODO(lead)): the API does not say which words made which chip.
  - While the transcript sits in the box, the "Type at least two letters" hint can show for a moment.
- **Substitutes:** a category can be broad (cheese can be offered for butter when both sit in one category).

## Questions for the lead

Answered by the lead on 26 Oct and done:

- **Decision numbers:** D-231 and D-235 (was D-233, which is W3-13's).
- **Refine chips:** picked up W1-06's TODO(lead), with parser tests in one table per language (see Backend for why the parser sits in `querynorm`).
- **Substitutes:** the placement and the query-parameter path are approved. The pack tolerance is now the `substitute_pack_tolerance_pct` setting, the rows are at most 6 (3 and 3), and the nearby half uses the search radius and only live stores of verified businesses.

Still open:

1. **refineHint.** It is set only by the parser half of 01 §11.1: no chip was read in a query of at least `refine_ai_min_words` words. The other half, "the synonym table had no match", is not wired: search does not yet report whether its synonym lookup missed. Is it fine to wire that half later?
2. **Setting bounds.** `substitute_pack_tolerance_pct` min 5 and max 100 are my choice; the admin group is "asks", beside `refine_ai_min_words` in the same 01 §17 table.
3. **Files outside my paths.** I touched these files outside W3-12's paths. Please review them at merge:
   - `customer/src/ai/` and `customer/src/voice-search/session.ts` (new folders)
   - `customer/src/main.tsx` (`setAiEventSink`)
   - `customer/src/test/render.tsx` (FormData bodies)
   - W1-06's `internal/search` (`refine.go`, `substitutes.go`, the `Parse` field in `search_run.go`), `db/queries/search/search.sql` and `api/*/discover.yaml`
   - `platform/settings/keys.go`, `admin/settings.go` (`groupPrefixes`) and 01 §17 for the new setting
   - W2-02's `internal/jobs/orders.go:25`: `//settings:ok` on `ordersTimerAttempts = 25`, a River attempt cap that settingslint now reads as the new setting's default
   - W3-04's `internal/voice/handlers.go` (two exported wrappers, no change in behaviour)
4. **Translations.** The 44 keys are machine-written and marked `reviewed` under the founder's standing instruction, as for W3-08 and W3-16. The REVIEW.md row asks you to confirm.
5. **Not mine, seen in passing.** `search_run.go:188` (W1-06's TODO(lead)): a filter naming an unknown brand or category is not logged as a zero-result search.
