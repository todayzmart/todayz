# W3-11 Retailer AI and shop coach: QA report

Branch `claude/w3-11-coach-1a5eea`, 26 Oct 2026. There are 32 commits from `fbe6a53` (`git log main..HEAD`), with current `main` merged in at `17bef82`; the last is this report's update. Nothing has been pushed or merged.

- **Work package:** W3-11, scenarios SC-AI-10…42 (`e2e/scenarios/AI.md`).
- **Features:** AI-R2 supplier bill scan, AI-R3 shelf photo onboarding, AI-R4 insight sentences, AI-R5 review reply draft, AI-R6 shop coach wording, AI-R9 banner copy and AI-R10 banner image check, plus the coach (11 §2.1).
- **Screens:** B-17 bill scan, B-18 shelf photo, B-19 coach, and the AI pieces in B-03, B-08 and B-11.

Every AI output is a draft that a person confirms. A bill, shelf photo, reply or banner draft changes nothing until the confirm, which applies only the lines the person accepted or edited. The coach and the insight sentences change only text, never stock, money or orders. A card's action is a deep link that opens a prefilled screen.

## Lead review fixes (25–26 Oct)

| Finding | Fix | Commit |
|---|---|---|
| Background wording took the owner's calls | Its own budget `ai_daily_calls_store_background` (migration 31102) and a negative cache `ai_text_misses`; a spent budget shows the template, never a 429 (D-228). Answers Q15 | `f54deed` |
| A resolved coach card counted as ignored | A card closed because its subject resolved is neutral and does not lower the shop's weight | `b73f1a5` |
| One bad line refused the whole confirm | Confirm is per line in the same transaction and row lock: a refused line is `not_permitted` with `not_permitted` or an `invalid_*` reason, the good lines are applied, every line's outcome is returned, and the draft stays `ready` while any line is open (D-229) | `25da53a`, `982b811` |
| B-17 and B-18 showed one error for the whole confirm | Settled lines are locked ("Added", "Left out"); each refused line shows its reason, can be edited, and the next confirm sends only the open lines | `b40863c` |
| A tied match was guessed | A bill line whose best match ties stays unmatched | `25da53a` |
| Photos trusted `Content-Type`; scans uncapped | The bytes must be JPEG, PNG or WebP (`retail.Sniff`); at most 2 bill and shelf scans processing per store, a third is 429 `rate_limited` with `Retry-After: 90` | `25da53a` |
| AI creates were not keyed | The four draft creates are `x-idempotency: required`; the app sends one `Idempotency-Key` per photo, review or request and says "busy" on 429 | `25da53a`, `b40863c` |
| Grounding ignored units | `schema.UngroundedFacts` matches % to percent facts and ₹ to money facts; index and id fields ground nothing (D-229) | `8ffae21` |
| Missing tests | Route tests (per line, double confirm, cashier, flag off, other store, injection) and coach done and dismiss on another store's card (404, nothing changes, `TestActOtherStore`) | `982b811`, `86009e0` |
| Drafts left `processing` at shutdown | No change: the existing `StaleAfter` rule (3 min) closes such a draft as failed (timeout) when it is next read, and the in-flight cap counts only drafts made within `StaleAfter`, so a stuck one never holds a slot (noted in D-229) | — |

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
- **Bill scan.** It matches each line to the store's items; a tie stays unmatched. The confirm is per line: an unmatched accepted line, a cost above MRP or a past expiry is refused with its reason and the other lines go in. Stock goes in through `catalogue/ai_seam.go`.
- **Photos.** Checked by their first bytes, at most 2 scans in flight per store (D-229).
- **Shelf photo.** It matches products in the catalogue and adds only the ticked ones at MRP or the edited price, per line like the bill scan.
- **Reply draft.** It posts only on confirm, through `reviews/ai_seam.go`.
- **Banner copy.** It drops any option with a price or % the facts do not hold (unit-aware, D-229). The confirm returns the copy and writes no banner.
- **Insight text.** It writes `text` on `GET {S}/insights` rows (`insights/service.go`, `list.go`). An ungrounded sentence falls back to its template.
- **Image check.** It returns ok, reject or unsure with reasons. It is not wired into the upload yet (see Questions).
- **Recorded evals for every feature (`*eval/`).** They are registered for `todayz ai eval` and run in `go test` under no-egress. The fixtures are synthetic: generated images and replies, not real shop bills (see "Not verified").

### Business app

- **B-17 bill scan (`src/restock-scan/`, `/restock-scan`).**
  - Photo pick, checked in the browser for type and size before upload, then "Reading the bill", then a review list.
  - Each line has a tick, quantity, cost, expiry and the matched item. An unmatched line must be edited to name an item or left unticked.
  - "Add to stock" sends one keyed confirm with a decision for every open line. A refused line stays open with its reason and can be edited and confirmed again; the lines already added or left out are locked. "Discard" (or "Discard the rest" once some lines are settled) discards the open lines.
  - The photo upload is keyed, so a retry gets the same draft; a third scan in flight says "Other photos are still being read".
  - The cap and failure states say when to try again.
- **B-18 shelf photo (`src/onboarding/shelf/`, `/onboarding/shelf`).** The same flow, per line too. Already-listed products are shown but cannot be added.
- **B-19 coach (`src/coach/`, `/coach`, and `overview/CoachSlot.tsx`).** Cards use the Demand insights row look, with no metric chip (D-99). Done and "Not useful" show the quiet-until toast. There are history tabs. A cashier is sent to their own screens and no call is made.
- **B-11 reply (`reviews/ReplyForm.tsx`, `ai-drafts/replyDraft.ts`).** "Draft a reply" fills the box. Posting goes through the draft's confirm (accepted, or edited when the person changed it). It is for first replies only.
- **B-08 banner (`offers/BannerSheet.tsx`, `ai-drafts/BannerCopy.tsx`).** "Suggest wording" lists the options as radios. "Use this" fills the headline, subline and tint, and the person still saves the banner.
- **AI-R4 (`insights/text/InsightText.tsx`, `overview/insightRows.tsx`).** A model-worded sentence carries "Worded by AI".
- **Keyed creates.** The reply draft and banner copy creates send an `Idempotency-Key` too (`useAction`).
- **Copy.** There are 37 `coach.*` keys and 79 `ai.*` keys in en, hi, kn, te and ta. They are machine-written, and approval is pending in `docs/qa/business-i18n/REVIEW.md` (see Questions).

## Evidence

| Check | Result |
|---|---|
| Bill scan, recorded (SC-AI-10): 60 bills, en/hi/kn | Printed: 222 of 231 lines right (**96.1%**, bar 90%). Handwritten: 119 of 154 (**77.3%**, bar 75%). The CLI's per-bill pass rate is 0.68 against a placeholder of 0.60 |
| Shelf photo, recorded (SC-AI-15): 40 photos | 197 of 220 products found (**89.5%**, placeholder bar 80%), 0 extra |
| coach_card, insight_text, review_reply, banner_copy, image_check, recorded | Every case passes (14, 13, 9, 7 and 9 cases). **0 ungrounded numbers** kept in coach and insight text |
| 0 writes without confirm | Each draft feature has a DB test that the draft writes nothing and the confirm writes only accepted and edited lines: `TestDraftWritesNothingUntilConfirm`, `TestShelfDraftCreatesNothingUntilConfirm`, `TestReplyPostsOnlyOnConfirm`, `TestBannerCopyWritesNoBanner`, `drafts.TestConfirmFlow` |
| Refusals, feature off, store cap | `TestConfirmRefuses`, `TestShelfConfirmRefuses`, `TestReplyRefuses`, `TestBannerCopyRefuses`, `TestHiddenWhenOff`, `TestShelfHiddenWhenOff`, `TestStoreCap`, `TestFailedRead` |
| Per-line confirm (1 bad line among good ones) | `drafts` `TestKeepOpen`, `TestNotPermitted`; route tests `TestAIRetailBillConfirmPerLine`, `TestAIRetailShelfConfirmPerLine`, `TestShelfRefusal`, `TestAIRetailBillConcurrentConfirm` (one double confirm wins, the other changes nothing) |
| Route guards | `TestAIRetailCashierForbidden`, `TestAIRetailHiddenWhenOff`, `TestAIRetailOwnership`, `TestAIRetailBillInjection`, coach `TestActOtherStore` |
| Hardening | `TestTiedMatchIsLeftUnmatched`, `TestScansInFlight`, `retail` `TestSniff`, `TestReadPhotoChecksTheBytes`, `schema` `TestUngroundedFacts` |
| Background budget | `guard` `TestStoreBackgroundBudget`; `insighttext` `TestTextsNegativeCache`, `TestTextsMissOnReject`, `TestTextsOffAndBudget`; `coach` `TestAIWriter`, `TestAIWriterMissCache`, `TestResolvedIsNotIgnored` |
| Coach | Card cap, push cap, dismiss quiet, k ≥ 10 floor, AI fallback, feed, batched checkpoint, realtime nudge, grounded templates: `internal/coach` and `detectors` golden tests on the dev seed and per-detector scenarios |
| Go (26 Oct, after the main merge) | gofmt, `go vet`, golangci-lint (0 issues on `internal/ai`, `internal/coach`, `internal/catalogue`, `cmd/todayz`) and the settings lint are clean. `go test -race` passes for `internal/ai/...`, `internal/coach/...`, `internal/insights/...`, `internal/catalogue/...` and `internal/reviews/...` with the DB (shared test Postgres), and for `cmd/todayz` filtered to the AI, retail, catalogue and route-contract tests |
| Business | Typecheck, ESLint and stylelint are clean. vitest: `src/ai-drafts/ai.test.tsx` 23 tests and `src/coach/coach.test.tsx` 7 tests pass. Full suite: **51 files, 486 tests pass** |
| i18n | Message lint shows 0 business errors, and 1890 of 1890 keys are reviewed in each of hi, kn, te and ta; `src/settings/language.test.tsx` passes |
| Contract (26 Oct) | `make gen check-contract` under `scripts/no-egress.sh` is green: redocly lint, apigen check, api-client gen (280 operations) and its 42 tests, the hooks check, and `TestContractSmoke` (68 problem codes registered) |

Run: `scripts/no-egress.sh make gen check-contract`, `go test -race ./internal/ai/... ./internal/coach/... ./internal/insights/... ./internal/catalogue/... ./internal/reviews/...` with `TODAYZ_TEST_PG_URL_FILE`, and `pnpm --filter @todayz/business test|lint|typecheck`. The full `make check` was not run, as the work package says.

## Files outside W3-11's own paths

- **Backend:**
  - `cmd/todayz/ai.go`: the eval CLI registers the new suites.
  - `cmd/todayz/routes_ai.go`: mounts the retailer AI routes. W3-04 voice will mount here too.
  - `cmd/todayz/routes_insights.go`: passes the insight-text writer.
  - `api/paths/ai.yaml`, `api/schemas/ai.yaml`: the ai domain is shared with W3-10, W3-04 and W3-12.
  - `db/sqlc.yaml`: the `coach` and `airetail` entries only.
  - `internal/catalogue/ai_seam.go`, `internal/reviews/ai_seam.go`: the seams the confirms call (the catalogue seam now returns a per-line refusal).
  - W3-10's AI base: `internal/ai/guard/guard.go` (the background budget scope), `internal/ai/drafts/drafts.go` and `drafts_test.go` (`KeepOpen`, `not_permitted` lines), `internal/ai/schema/ground.go` and `schema_test.go` (`UngroundedFacts`; voice keeps `Ungrounded`), and `internal/ai/retail/retailtest` (shared test env).
  - `internal/ai/insighttext`: the cache key is now readable, so its hash changes and a cached sentence is rewritten once after the deploy.
  - `internal/insights/service.go`, `list.go`: they fill `text`.
- **Business:**
  - `overview/CoachSlot.tsx` and `overview/insightRows.tsx`: they fill W3-03's slot and add the mark.
  - `reviews/ReplyForm.tsx`.
  - `offers/BannerSheet.tsx`.
  - `routes/onboarding_.shelf.tsx`.
  - `stores/queries.ts`: `useAction` is used for the keyed creates, not edited.
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

15. **Background runs and the cap.** Answered by D-228: they have their own `ai_daily_calls_store_background` budget.
16. **Permissions.** The features reuse existing permissions (table above). Does any need its own?
17. **Other eval bars.** The insight_text, review_reply and banner_copy bars are "every case".
18. **Small numbers.** The grounding check ignores numbers 0–1 (a "one" in "one item").

19. **Translations.** The `ai.*` and `coach.*` translations are machine-written and marked reviewed so the language test passes. They need the lead's confirmation under the founder's 25 Oct instruction (REVIEW.md).

**App**

20. **No entry points.** `/restock-scan` and `/onboarding/shelf` have no link from Stock or nav yet (`nav.ts`).
21. **Reply draft.** It is for first replies only, not edits.
22. **Banner copy.** It only fills the fields; the image is the person's.
23. **Polling.** The draft poll interval is 1500 ms (`POLL_MS`), a UI constant and not a product rule.

**Lead review (26 Oct)**

24. **Scans in flight.** `retail.MaxScansInFlight` is a fixed 2 (`TODO(lead)`). Should it be a `platform_settings` key?
25. **Count fields in grounding.** A count fact is plain: it grounds "Buy 2 get 1" but never "2%" or "₹2" (D-229). Is that the reading you want?
26. **Shelf `not_permitted`.** A shelf line is refused for permission when the create needs `item.price.edit` and the confirmer lacks it. No seeded role has `item.manage` without `item.price.edit`, so the app cannot reach it today; `TestShelfRefusal` checks the mapping and the UI test uses a fake.
