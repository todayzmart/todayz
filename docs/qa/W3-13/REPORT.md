# W3-13 Admin AI: QA report

The admin console's three AI assistants (11 §4): AI-A1 catalogue clean-up in A-08, AI-A2 ticket triage in A-14, AI-A3 risk summary in A-13. Branch `claude/great-jackson-f38db4`, from `main` 5beb1ed, with current `main` merged before this report. Everything ran locally with recorded model answers; nothing called a model, a provider or a cloud account.

## What was built

- **Backend** (`internal/ai/{catalogue,triage,riskexplain,assist}`, D-233, D-234):
  - the 04 §2.8 draft envelope on `/v1/admin/ai/{catalogue-cleanup|ticket-triage|risk-summary}/drafts[/{id}[/confirm]]`: create is 202 `{draftId}`, GET polls, confirm takes one decision per line;
  - each route takes its screen's own policy (`admin.catalogue.review`, `admin.support.tickets`, `admin.risk.view`) and is the common 404 while `ai_catalogue_enabled`, `ai_triage_enabled` or `ai_riskexplain_enabled` is off or the key is missing;
  - a draft belongs to the admin who asked; another admin's draft is the common 404 (`assist.Own`);
  - a confirmed line goes through the screen's own writer, audited: A-08 edit, merge and category names (`internal/admin/assist.go`), A-14 triage and reply (`internal/support/assist.go`); the risk summary is only an audit entry on the flag (`risk.flag_summary`), the flag itself never changes;
  - catalogue lines need their A-08 button's permission (normalise and names `admin.catalogue.manage`, merge `admin.catalogue.review`); the draft says `canApply` per line, a line the caller may not apply is `not_permitted` and the draft stays `ready`; normalise and merge together is `422`;
  - grounding (D-230): every number in a drafted name, reason, summary or reply is one the model was given; an edited risk summary with any other number is `422 ungrounded`;
  - privacy: the models see orders by reference (`o1…`), facts with ids left out, free text scrubbed; never a name, phone, address, pickup code or id. Nothing a customer wrote is logged.
- **Admin console** (`admin/src/ai-assist/`, mounted through the existing `<AssistSlot/>` in A-08, A-14, A-13):
  - `draft.ts`: create, poll, confirm with an Idempotency-Key, error mapping (`assistErrorKey`) and per-line refusals from the problem's `body.lines[i]` fields (`lineReasons`);
  - `AssistCard` (the panel: start, working, failed, settled) and `AssistLine` (Use, Edit, Skip per line as a radio group);
  - `CatalogueAssist`, `TriageAssist`, `RiskAssist`; edits are bounded by the contract's lengths and trimmed.
- **Copy:** `shared/i18n/messages/admin/aiAssist.{en,hi,kn,te,ta}.json`, 64 keys; hi, kn, te and ta are machine drafts.

## Evidence

Run on the branch after merging `main` (see the last section for the merge commit).

| Check | Result |
|---|---|
| Go tests, my packages and the touched domains, with the shared test Postgres (`TODAYZ_TEST_PG_URL_FILE`), `-race` | ok: `internal/ai/{catalogue,triage,riskexplain,assist}` and their eval packages, `internal/admin`, `internal/support`, `cmd/todayz` (incl. `TestAdminAICatalogueHTTP`, `TestAdminAITriageHTTP`, `TestAdminAIRiskHTTP`) |
| gosec on the same packages (`-exclude-generated -exclude=G104`) | 0 issues |
| settingslint | clean |
| `scripts/no-egress.sh make gen check-contract` | exit 0: redocly lint, apigen check, api-client tests 42 passed, hooks ok (75 problem codes) |
| Contract smoke (`TestContractSmoke`, part of `check-contract`) | ok (23.9 s); the new routes' 404, 409, 422 and 429 answers are registered problems |
| `@todayz/i18n` tests, message lint | 198 passed; 0 errors (warnings are the unreviewed drafts) |
| Admin typecheck, lint, CSS lint | clean |
| Admin unit tests | 285 passed (6 new in `src/ai-assist/assist.test.tsx`) |
| Admin `build:e2e` and size | ok, 171.85 kB initial JS (budget 250) |
| Admin Playwright (fake API) | 50 passed, of which `e2e/ai-assist.spec.ts` 3; no external request, no console error |

**Recorded evals** (`make check` runs them; no live model):

| Suite | Cases | Measure | Bar |
|---|---|---|---|
| SC-AI-57 catalogue clean-up | 16, 15 pass (1 seeded wrong merge, `wrong_merge_pack_size`) | merge precision 10 of 11 = 90.9% | at least 90% |
| SC-AI-60 ticket triage | 16, 15 pass (1 seeded wrong category, `wrong_category_credits`) | category accuracy 12 of 13 = 92.3% | at least 85% |
| SC-AI-63 risk summary | 11, 11 pass | grounded 11 of 11 | every summary |

The seeded failures prove each scorer counts a wrong answer; the bars still hold with them in.

SC-AI-56…65 are in 08-testing (Admin AI row).

## Screenshots

`docs/qa/W3-13/`, from `e2e/ai-assist.spec.ts` with `TZ_QA_DIR=docs/qa/W3-13`:

- `admin-w313-a08-cleanup.png`: A-08 review detail with the clean-up draft (normalise, merge into a look-alike, category names).
- `admin-w313-a14-triage.png`: A-14 ticket with classify, the read-only summary and the reply draft.
- `admin-w313-a13-explain.png`: A-13 flag with the summary draft.
- `admin-w313-a13-recorded.png`: after confirm, the summary recorded.

## Shared changes (outside my packages)

- `backend/db/queries/support/support.sql` (+8 lines) and `backend/internal/support/assist.go`: the A-14 writes the triage confirm calls; same checks as the inbox's own writes.
- `backend/internal/admin/assist.go`: the A-08 writes the catalogue confirm calls.
- `backend/cmd/todayz/{ai.go,ai_admin.go,routes_ai.go}`: wiring and routes; `backend/db/sqlc.yaml`: the `aiadmin` query package.
- `backend/api/paths/ai.yaml`, `backend/api/schemas/ai.yaml`: the three draft routes and their schemas.
- `admin/src/{catalogue,support,risk}/AssistSlot.tsx`: the slot bodies now render the panels; their comments and props are unchanged.
- `shared/i18n/messages/admin/aiAssist.*.json`: new namespace.
- Policy goldens unchanged: no new actions, each route reuses its screen's.
- No migration (the three flags were already seeded), no new setting, no new Go or npm module.
- Plan: D-233 and D-234 in 12-decision-log; SC-AI-56…65 in 08-testing; status lines in `admin/CLAUDE.md` and `backend/CLAUDE.md`.

## Open points

- `TODO(lead)` `triage.SummaryMinReplies` = 4: 11 §4 says "long threads" with no number. A constant, not a setting, because 01 does not mark it configurable (D-234).
- The risk summary lives only as an audit entry (`risk.flag_summary`); W3-02's flag tables are untouched. If A-13 should show it as a field on the flag, that is a W3-02 change.
- Risk facts that are a bare count or amount (`bareNumber`, up to 9 digits) skip the free-text scrub, because the PIN rule would cut a six-digit paise amount; such a value cannot identify a person.
- Conservative defaults taken: every line starts as Skip, so a confirm with nothing chosen applies nothing; the triage summary takes no edit; the slot comments are unchanged.
- The production `build` of the admin app needs a real `VITE_TURNSTILE_SITE_KEY` (pre-existing, noted in the W3-15 report); `build:e2e`, which `make check` uses, is green.
- hi, kn, te and ta copy are machine drafts awaiting review.

## Branch

- `aae618bb` backend, `c0cf3d68` console, tests, copy, plan and this report, `2b29bec0` merge of `main` at `403b64d6` (clean, no conflicts).
- After the merge: Go tests with `-race` on the shared test Postgres for `internal/ai/{catalogue,triage,riskexplain,assist}` (with the evals), `internal/admin`, `internal/support`, `internal/identity` (changed on `main`) and `cmd/todayz`: all ok. Admin typecheck, lint, CSS lint, 285 unit tests, `build:e2e`, size 171.85 kB, Playwright 50 passed; i18n 198 passed.
- Not pushed.
