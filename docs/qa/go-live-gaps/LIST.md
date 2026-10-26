# Go-live gaps (26 Oct 2026)

What is still open before TodayZ goes live, apart from work other sessions own. Built on branch
`claude/go-live-gaps-c0eea0` from main `876b5ed6`.

Sources:
- `docs/plan/README.md` (Definition of done), `07-work-packages.md` (W4, W5), `08-testing.md` and `e2e/scenarios/`;
- the open sections of `docs/qa/W4-01`…`W4-07` and `docs/qa/rc-2026-10-26/summary.md`;
- the lead's `.claude/lead-notes/followups.md` and `pending-decisions.md`;
- the `TODO` and `test.fixme` markers in the code.

Tags: **(a)** fix now on this branch; **(b)** needs a founder decision (the question is written out);
**(c)** owned elsewhere. Size is S (under an hour), M (a few hours) or L (a day or more).

## (a) Fix now, smallest first

| # | Size | Item | Source | Status |
|---|---|---|---|---|
| A1 | S | `e2e/fixtures/admins.yaml` says the mocks have a "TOTP now" panel. They don't: the journeys compute the code from the file (`e2e/fixtures/api.ts`). Fix the comment | lead | **done** (comment names the real readers) |
| A2 | S | `.tools/bin/envtool` is shared by worktrees through the `.tools` symlink. A build in one checkout is newer than another's sources, so make never rebuilds it, and main's SC-PL-03 smoke saw the wrong key list. Rebuild it on every make run (the Go build cache makes that ~0.3 s) | lead notes 26 Oct | **done**: `mk/00-repo.mk` rebuilds envtool on every run; checked with a binary stamped in 2030 (rebuilt) |
| A3 | S | `cmd/todayz` TestPOSOfflineOfferMatchesEngine flake ("entry 9: linked, got failed"). The cause is not load. `codeAt` binds a 4-digit code to the *earliest* step in ±1 that matches. When a code also matches the step before (1 in 10,000), that sale takes the neighbour's step, and the neighbour's own sale later fails as "code used". 19 consecutive minutes make it about 0.2 % a run. Prefer the exact step, then the drift steps (the set of codes accepted is unchanged) | lead notes (load flakes) | **done**: `pos/totp.go` codeAt; `TestTOTPExactStepWins` fails before, passes after; the POS offline HTTP tests pass 3× |
| A4 | S | `internal/search` TestSearchAtScale p95 goes just over 300 ms when 2–3 sessions run checks at once. Keep the 300 ms budget; when the machine's load per CPU is high, log the p95 and skip only the latency gate (the index checks still gate). The lead asked not to change the budget | lead notes (load flakes) | open |
| A5 | M | OB-30: `shared/telemetry/events/asks.yaml` now exists (W3-16), so dashboard 07's "Asks panels — TODO(lead)" text panel can become the ask funnel, reply rate and time to reply | `e2e/scenarios/OB.md` OB-30 | open |
| A6 | M | Dashboard 11's "Statements, payouts and approvals — TODO(lead)" panel waits on tables W3-01 has since built (`statements`, `payouts`, approvals). Fill it from Postgres | `infra/grafana/dashboards/11-money-payouts.json` | open |
| A7 | M | W4-01 #6: the seed has no customer credits or payout fixtures, so E2E-09 makes its own spend first. Seed one statement with a positive closing balance so E2E-09 stands alone | W4-01 report | open, lowest priority (changes the seed every journey runs on) |

## (b) Needs a founder decision

| # | Size once decided | Question | Source |
|---|---|---|---|
| B1 | M | **Customer home on mobile scores 74 in Lighthouse** (the target is 90) because MapLibre loads when the map is near the screen. Should the home show a static "Tap to see the map" card that loads the map only on a tap? | W4-04 left open 1 |
| B2 | — | **Search speed target.** 08 §6 says "search <300 ms". Measured on the laptop: search plus suggest together 285 ms (pass); the search call alone 590 ms (fail). Which reading is the target, and does the staging run decide it? The same question for a 20-sale POS sync batch: 678 ms against 400 ms | W4-02 open 1, 2 |
| B3 | S | **Bank payout CSV.** Names that start with `=`, `+`, `-` or `@` get a leading `'` so a spreadsheet cannot run them as formulas. A bank may then fail to match the name. Keep the `'` or not? (A question for whoever uploads the file to the bank) | W3-01 money review |
| B4 | L | **Referral farming (W4-03 F3, F7).** The same-device rule trusts the device id the phone sends, and a session with no device row skips the check. Today a fresh device id gets the referral `pending`; nothing pays before a real purchase, and the OTP caps limit how many phones one person can use. Build a server-issued, signed device id before launch, or launch as is and watch the risk queue? | W4-03 F3, F7 |
| B5 | M | **Sessions page.** People cannot see or sign out their other sessions, and there is no "new sign-in" notice (ASVS 3.3.3, 3.3.4). Before launch or later? | W4-03 F8 |
| B6 | M | **Translations.** (1) The admin console (1,965 keys) and the site's legal pages (50 keys) are English only: draft them, or keep English? Who approves? (2) Which one word per language for "cancel"? (3) 8 glossary terms are still open in `shared/i18n/glossary.md` | W4-05 follow-ups |
| B7 | S | **Dashboard 10 "stores needing attention".** A store is flagged when more than half its stock is stale, more than 20 % of its orders expire unaccepted, or it has had no activity for 7 days. Are these the right lines? | W3-07 |
| B8 | L | **Changing the encryption keys.** The four sealed-data keys (order codes, phones, device secrets, admin TOTP) cannot be changed today without making the stored rows unreadable; the runbook says "do not rotate" unless there is a leak. OK to launch like this and build the re-encrypt job later? | W4-07 open 2 |
| B9 | M | **Admin goodwill credits.** D-237 sums one admin's adjustments to *one customer* per day for the two-person rule. One admin giving many customers small credits in a day is not summed. Add a risk flag when one admin's day total across all customers passes a number? If yes, what number? | W3-01 money review |
| B10 | — | **About 60 small defaults in code** marked `TODO(lead)` where the plan gives no number (for example: staff invite expiry 7 days, business document size 10 MB, 5 shop photos, session records kept 30 days, coach card weights, AI eval pass bars). Each was the most conservative reading. Accept them all as launch defaults? `git grep -n "TODO(lead)"` lists them | code markers |

## (c) Owned elsewhere

| # | Owner | Item |
|---|---|---|
| C1 | lead | Rerun `make release-candidate` from main. The 26 Oct run failed in reset and check only because the W4-07 worktree had no `.tools` (`sqlc: command not found`); load passed |
| C2 | lead | ZAP full scan of business, admin and the site, and an API active scan (W4-03 remaining 1) |
| C3 | lead | Bump undici after 29 Oct (W4-03 D1, the `.trivyignore.yaml` entry expires 30 Oct) |
| C4 | deploy-prep | The report-only CSP on the three Vite apps has no `report-uri`/`report-to`, so prod cannot show the violations it waits for before enforcing (W4-03 F1). It needs a report endpoint (Sentry's security endpoint in W5-03, or an API route). Checked here: the built `index.html` of customer, business and admin has no inline script (main's builds of 26 Oct 10:20, one `<script type="module" src>` each), so `script-src 'self'` will not break them |
| C5 | deploy-prep | W4-03 F5: clientip trusts `CF-Connecting-IP` from any peer; restrict the ALB to Cloudflare ranges (W5) |
| C6 | deploy-prep | Certificate and uptime alert with its runbook; cloud restore drill (W4-07 open 3, 4); W5-01…W5-05 |
| C7 | lead / deploy-prep | `SHARE_PREVIEW_KEY` into Secrets Manager when AWS is ready |
| C8 | lead (stack) | Soak run (`make load-soak`, W4-02 open 3); backup drill on non-zero money after `make demo` (W4-07 open 1); OB-28 demo fills dashboards and OB-29 Grafana evaluates every alert |
| C9 | lead (stack) | W4-04 coverage: Lighthouse signed in, scripted focus-return checks, 1440×900 for business and admin, site en-XA baseline, CI baselines on Linux |
| C10 | founder | W4-06 manual QA on the phones, and the TalkBack / VoiceOver pass |
| C11 | founder | DLT ids (sign-in OTP, the field owner-confirm SMS, D-224 re-registration), SNS SMS activation, SES production access, WhatsApp number |
| C12 | founder | `make ai-eval` with real keys before the voice flag goes on in prod; Hindi/kn/te/ta review of the notify and coach templates |

## Closed since the lead's notes (checked on main `876b5ed6`)

| Item | Closed by |
|---|---|
| EmptyState heading level (W4-04 open 2) | `@todayz/ui` EmptyState has `headingLevel`; search uses h2 (ad717013) |
| No inline scripts in built `index.html` (report-only CSP) | Checked: none in customer, business, admin (see C4) |
| Cumulative wallet adjustments (W3-01 review) | D-237: the two-person threshold is per admin, customer and IST day (the across-customers case is B9) |
| W3-06 field photo sweep batch failure; 30603 `confirm_by` NULL | D-237 (sweep retry, migration 49802 backfill) |
| `cleanModReason` phone check | D-237 |
| `referral.self_like` fires after the reward | D-237 (reward held first) |
| Synonym-miss half of `refineHint` | D-238 |
| Lost refresh response signs people out (W4-01 #7) | D-239 |
| A-09 cannot adjust a customer with no wallet (W4-01 #2) | D-240 |
| Who submits a field-onboarded store (W4-01 #3) | D-241 |
| E2E-21 text vs seed (W4-01 #5) | 4dfdee04 |
| E2E-19 409s on block/unblock (W4-01 #8) | Expected: the spec blocks twice on purpose and its clean-up unblocks an unblocked customer (`12-e2e-19-blocked.spec.ts:45`, `:103`) |
| Generic rate limits (W4-03 F4), POS `sold_at` bounds (F6) | D-242, D-243 |
| Tamil clipping on customer home (W4-05) | f3bd3406 (W4-04) |
| H3 cells for search and asks | W3-16 `internal/platform/geo/h3`; `search_log.geo_cell` is written |
