# W3-11 Retailer AI and shop coach: QA report

Branch `claude/w3-11-coach-1a5eea`, 25 Oct 2026. There are 22 commits, from `fbe6a53` to `f422d14` (`git log main..HEAD`). Nothing has been pushed or merged.

- **Work package:** W3-11, scenarios SC-AI-10…40 (`e2e/scenarios/AI.md`).
- **Features:** AI-R2 supplier bill scan, AI-R3 shelf photo onboarding, AI-R4 insight sentences, AI-R5 review reply draft, AI-R6 shop coach wording, AI-R9 banner copy and AI-R10 banner image check, plus the coach (11 §2.1).
- **Screens:** B-17 bill scan, B-18 shelf photo, B-19 coach, and the AI pieces in B-03, B-08 and B-11.

Every AI output is a draft that a person confirms. A bill, shelf photo, reply or banner draft changes nothing until the confirm, which applies only the lines the person accepted or edited. The coach and the insight sentences change only text, never stock, money or orders. A card's action is a deep link that opens a prefilled screen.

## What was built

### Backend

- **Coach (`internal/coach/`, migration `31101_w3-11_coach.sql`).** The migration adds `coach_suggestions`, `coach_weights` and `coach_checkpoints`. It adds no new settings key; every number comes from existing `platform_settings` keys (`coach_max_cards_per_day`, `coach_max_push_per_day`, `coach_dismiss_quiet_days`, `ai_coach_enabled`, `ai_daily_calls_store`, the `insight_*` thresholds).
  - **Detectors (`detectors/`).** The facts-mapped detectors read W3-03's `internal/insights/facts` and never recompute an insight number (D-99). There are own-SQL detectors for:
    - orders missed at the peak hour;
    - a record sales day;
    - unanswered reviews;
    - the weekly summary;
    - a rating drop;
    - accept time against nearby shops (k ≥ 10, D-71);
    - setup incomplete;
    - the expiry-penalty nudge.
  - **Chooser.** It ranks by severity × the shop's weight, caps at `coach_max_cards_per_day`, and keeps a dismissed subject quiet for `coach_dismiss_quiet_days`. Only severity-3 cards may push, capped at `coach_max_push_per_day`.
  - **Writer.** Templates in en, hi, kn, te and ta. With `ai_coach_enabled` on, the model may reword a card, but only if every number in the result is one of the fact's; otherwise the template is used.
  - **Jobs and feed.** River jobs `coach_daily` and `coach_weekly` run in batches with a checkpoint; `coach_realtime_rules` runs the expiry nudge. The feed is `GET {S}/coach/cards`, plus `POST …/done` and `…/dismiss`. Policy is `coach.view` and `coach.act` (owner and manager).
- **Retailer AI (`internal/ai/{billscan,shelfscan,replydraft,bannercopy,insighttext,imagecheck}`).** These are built on W3-10's `drafts`, `guard`, `client` and `eval`. Each draft feature has these routes:
  - `POST {S}/ai/<feature>/drafts` (the bill and shelf photos are `x-upload`);
  - `GET …/drafts/{id}`;
  - `POST …/drafts/{id}/confirm` (Idempotency-Key required);
  - `GET {S}/ai/retail` tells the app which features are on.
- **Gates.** Each feature is the common 404 while its flag is off or the model key is missing, and the store's daily AI calls are capped (429 `ai_daily_cap_reached` with `Retry-After`). Permissions reuse existing actions:

  | Feature | Permission |
  |---|---|
  | Bill scan | `stock.restock` |
  | Shelf photo | `item.manage` |
  | Reply draft | `reviews.reply` |
  | Banner copy | `banners.manage` |
  | Insight text | `insights.view` |
- **Bill scan.** It matches each line to the store's items. The confirm refuses an unmatched accepted line, a cost above MRP or a past expiry. Stock goes in through `catalogue/ai_seam.go`.
- **Shelf photo.** It matches products in the catalogue and adds only the ticked ones at MRP or the edited price.
- **Reply draft.** It posts only on confirm, through `reviews/ai_seam.go`.
- **Banner copy.** It drops any option with a price or % the facts do not hold. The confirm returns the copy and writes no banner.
- **Insight text.** It writes `text` on `GET {S}/insights` rows (`insights/service.go`, `list.go`). An ungrounded sentence falls back to its template.
- **Image check.** It returns ok, reject or unsure with reasons. It is not wired into the upload yet (see Questions).
- **Recorded evals for every feature (`*eval/`).** They are registered for `todayz ai eval` and run in `go test` under no-egress. The fixtures are synthetic: generated images and replies, not real shop bills (see "Not verified").

### Business app

- **B-17 bill scan (`src/restock-scan/`, `/restock-scan`).**
  - Photo pick, checked in the browser for type and size before upload, then "Reading the bill", then a review list.
  - Each line has a tick, quantity, cost, expiry and the matched item. An unmatched line must be edited to name an item or left unticked.
  - "Add to stock" sends one keyed confirm with a decision for every line. "Discard" discards them all.
  - The cap and failure states say when to try again.
- **B-18 shelf photo (`src/onboarding/shelf/`, `/onboarding/shelf`).** The same flow. Already-listed products are shown but cannot be added.
- **B-19 coach (`src/coach/`, `/coach`, and `overview/CoachSlot.tsx`).** Cards use the Demand insights row look, with no metric chip (D-99). Done and "Not useful" show the quiet-until toast. There are history tabs. A cashier is sent to their own screens and no call is made.
- **B-11 reply (`reviews/ReplyForm.tsx`, `ai-drafts/replyDraft.ts`).** "Draft a reply" fills the box. Posting goes through the draft's confirm (accepted, or edited when the person changed it). It is for first replies only.
- **B-08 banner (`offers/BannerSheet.tsx`, `ai-drafts/BannerCopy.tsx`).** "Suggest wording" lists the options as radios. "Use this" fills the headline, subline and tint, and the person still saves the banner.
- **AI-R4 (`insights/text/InsightText.tsx`, `overview/insightRows.tsx`).** A model-worded sentence carries "Worded by AI".
- **Copy.** There are 37 `coach.*` keys and 73 `ai.*` keys in en, hi, kn, te and ta. They are machine-written, and approval is pending in `docs/qa/business-i18n/REVIEW.md` (see Questions).

## Evidence

| Check | Result |
|---|---|
| Bill scan, recorded (SC-AI-10): 60 bills, en/hi/kn | Printed: 222 of 231 lines right (**96.1%**, bar 90%). Handwritten: 119 of 154 (**77.3%**, bar 75%). The CLI's per-bill pass rate is 0.68 against a placeholder of 0.60 |
| Shelf photo, recorded (SC-AI-15): 40 photos | 197 of 220 products found (**89.5%**, placeholder bar 80%), 0 extra |
| coach_card, insight_text, review_reply, banner_copy, image_check, recorded | Every case passes (14, 13, 9, 7 and 9 cases). **0 ungrounded numbers** kept in coach and insight text |
| 0 writes without confirm | Each draft feature has a DB test that the draft writes nothing and the confirm writes only accepted and edited lines: `TestDraftWritesNothingUntilConfirm`, `TestShelfDraftCreatesNothingUntilConfirm`, `TestReplyPostsOnlyOnConfirm`, `TestBannerCopyWritesNoBanner`, `drafts.TestConfirmFlow` |
| Refusals, feature off, store cap | `TestConfirmRefuses`, `TestShelfConfirmRefuses`, `TestReplyRefuses`, `TestBannerCopyRefuses`, `TestHiddenWhenOff`, `TestShelfHiddenWhenOff`, `TestStoreCap`, `TestFailedRead` |
| Coach | Card cap, push cap, dismiss quiet, k ≥ 10 floor, AI fallback, feed, batched checkpoint, realtime nudge, grounded templates: `internal/coach` and `detectors` golden tests on the dev seed and per-detector scenarios |
| Go | `go vet`, golangci-lint, settings lint and gofmt are clean. `go test -race` passes for `internal/ai/...` and `internal/coach/...` with the DB (shared test Postgres) |
| Business | Typecheck, ESLint and stylelint are clean. vitest: `src/ai-drafts/ai.test.tsx` 19 tests and `src/coach/coach.test.tsx` 7 tests pass. Full suite: **48 files, 436 tests pass** |
| i18n | Message lint shows 0 business errors, and 1657 of 1657 keys are reviewed in each of hi, kn, te and ta; `src/settings/language.test.tsx` passes |
| Contract | `make gen check-contract` under `scripts/no-egress.sh` is green: redocly lint, apigen check, api-client 42 tests, the hooks' 14 tests, and `TestContractSmoke` (67 problem codes registered) |

Run: `scripts/no-egress.sh make gen check-contract`, `go test -race ./internal/ai/... ./internal/coach/...` with `TODAYZ_TEST_PG_URL_FILE`, and `pnpm --filter @todayz/business test|lint|typecheck`. The full `make check` was not run, as the work package says.

## Files outside W3-11's own paths

- **Backend:**
  - `cmd/todayz/ai.go`: the eval CLI registers the new suites.
  - `cmd/todayz/routes_ai.go`: mounts the retailer AI routes. W3-04 voice will mount here too.
  - `cmd/todayz/routes_insights.go`: passes the insight-text writer.
  - `api/paths/ai.yaml`, `api/schemas/ai.yaml`: the ai domain is shared with W3-10, W3-04 and W3-12.
  - `db/sqlc.yaml`: the `coach` and `airetail` entries only.
  - `internal/catalogue/ai_seam.go`, `internal/reviews/ai_seam.go`: the seams the confirms call.
  - `internal/insights/service.go`, `list.go`: they fill `text`.
- **Business:**
  - `overview/CoachSlot.tsx` and `overview/insightRows.tsx`: they fill W3-03's slot and add the mark.
  - `reviews/ReplyForm.tsx`.
  - `offers/BannerSheet.tsx`.
  - `routes/onboarding_.shelf.tsx`.
- **Docs:** `docs/qa/business-i18n/REVIEW.md` (the approval row and the strings to check) and `docs/runbooks/coach.md`.
- **Not touched:** `nav.ts`, `shared/ui`, `internal/ai/voice*`, `internal/support`.

## Not verified

- **Real bills and photos.** The fixtures are synthetic: generated invoices and shelf photos with recorded model answers. The 96% and 77% show that the scoring and matching work; they do not show how the live model does on real bills. `make ai-eval` with real keys and real photos is still to be done.
- **Screenshots.** None were taken. They need `make dev`, which this worker may not run. No stack E2E journey was written for B-17, B-18 or B-19.
- **Push.** No push is sent: notify (W2-05) has no coach kind yet, so `Pusher` is nil. The push cap is tested with a fake.
- **Image check.** It is not called from the banner upload; it is tested only through its recorded eval.
- **Insight-text flag off.** There is no DB test of the insight-text service with its flag off. The fallback to the template is covered by the eval and the UI test.

## Questions (`TODO(lead):` in the code)

**Coach**

1. The weight steps (Done +1500, Not useful −2500, ignored −500) and the detector thresholds have no key in 01:
   - `recordMinWeeks`: 4 of 6 weeks;
   - `ratingDropTenths`: half a star;
   - `slowFactor`: 2× the median;
   - one expiry is enough to fire the missed-orders detector.

   Should they become `platform_settings` keys in a 311xx migration?
2. **Push.** Should notify get a coach kind?
3. **The weekly summary's day.** It runs on the schedule the weekly job uses. Is that right?
4. **The coach_card bar.** It is set to "every case", since 11 names none.
5. **Translations.** The coach templates in hi, kn, te and ta are a first draft.
6. **Navigation.** `/coach` has no nav entry (`nav.ts` is the lead's), and the `reply_reviews` action opens `/reviews` without a filter.

**Bill scan**

7. **Unmatched lines.** AI-R2 says unmatched lines are "offered as create product". Today they link to Stock instead of creating a product inline. The add-product sheet needs to take a prefill first.
8. **The CLI bar.** It is a 0.6 placeholder per bill. The plan's per-line bars are asserted in the test.
9. **No low-confidence threshold.** Every line is shown for review.

**Shelf photo**

10. **The recall bar.** It is 0.8; the plan names none.

**Insight text**

11. **Language.** The sentence is in the store's language, not the viewer's.
12. **The suggested %.** The offer % for slow items is not written, because 01 has no limits for it.

**Image check**

13. **Wiring.** Should an "ok" verdict clear a banner without a person, and where is the verdict stored for the admin queue (W3-05)? Today every banner still waits for a person.
14. **Its bar.** It is "every case".

**All AI features**

15. **Background runs and the cap.** Do the coach's and the insight text's model calls count against `ai_daily_calls_store`? Today they do.
16. **Permissions.** The features reuse existing permissions (table above). Does any need its own?
17. **Other eval bars.** The insight_text, review_reply and banner_copy bars are "every case".
18. **Small numbers.** The grounding check ignores numbers 0–1 (a "one" in "one item").
19. **Translations.** The `ai.*` and `coach.*` translations are machine-written and marked reviewed so the language test passes. They need the lead's confirmation under the founder's 25 Oct instruction (REVIEW.md).

**App**

20. **No entry points.** `/restock-scan` and `/onboarding/shelf` have no link from Stock or nav yet (`nav.ts`).
21. **Reply draft.** It is for first replies only, not edits.
22. **Banner copy.** It only fills the fields; the image is the person's.
23. **Polling.** The draft poll interval is 1500 ms (`POLL_MS`), a UI constant and not a product rule.
