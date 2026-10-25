# W3-05 Admin console: QA report

This report covers the admin console work package so far. It lists the backend and the screens that are built, the journeys, and what is still open.

- **Branch:** `claude/determined-meninsky-950b06`. Main (W2-15 delivery, migrations 21501 and up) was merged in as f73eca2, and the lead's local main 3f16f74 (the first W3-05 set) after that.
- **Where it ran:** locally only, against the `make dev` stack, with `scripts/no-egress.sh` around the journey. Nothing was pushed and no cloud account was touched.

## What was built

| Commit | What |
|---|---|
| d0f0c97 | Backend `internal/admin`: customers (search, block, unblock, strike reset, session revoke), admin order reads, the audit log and the per-subject audit panel |
| 78dd3c1 | A role-aware sidebar for the 7 roles; no-access and load-failed states; nav entries for reviews, banners, audit, notifications, jobs (A-21), system (A-22) and demand (A-23) |
| d192fdb | A-06 customers, A-07 orders (list and detail) with admin cancel and a reason, and the A-19 audit log screen |
| eefefb1 | Journey E2E-19, the blocked customer, with admin steps through the admin API (D-83) |
| 8a8394d | Settings API: a list with groups and provider key states (set or missing), `PATCH /v1/admin/settings/{key}` with step-up and a reason (money keys are super_admin only), history, and an audit entry for every change |
| 70bc5ae | The A-17 settings screen: groups, values by unit, ranges, provider key states, edit with a reason, the step-up prompt (`StepUpDialog`) and a history panel |
| aa9ae2e | Journey E2E-12 through the admin API: the cap goes from 30 to 40 with step-up; checkout allows 40; history and audit are checked; the cap is restored to 30 |
| 0bbb545 | Journey E2E-12 through the A-17 screen, with a screenshot; `Principal.totpCode` serves the dialog |
| 6747cf9 | A-12 reviews: reported first, state and store filters in the URL, hide and restore (review and reply) with a required reason, the report queue (dismissed or upheld), the audit panel |
| 4300035 | API for A-03 businesses, A-05 stores and the Leads tab; migration 30502 (lead status columns, `stores.review_note`); A-17 gets an addresses group |
| db0cf55 | Screens A-03 (queues by status, verify, ask for info, reject, suspend, reinstate, KYC review and one-field reveal, documents) with the Leads tab, and A-05 (filters, review queue, pin and hours, force-close, reopen, strike reset) |
| 9894128 | AD-02 role-matrix Playwright; the admin smoke fixed for the role-aware nav; shell fixes (below) |
| 31f372b | Playwright journeys for A-03 verify, A-05 force-close and A-12 hide and restore |

## Evidence

### Area checks (on 70bc5ae)

- **Admin:** 99 unit tests pass, plus lint, tsc, `build:e2e`, the size budget and the message lint.
- **Backend:** the admin and settings packages pass their tests. `make gen` is clean.
- **Journeys:** the typecheck (`pnpm run typecheck` in `e2e/journeys`) is clean on f73eca2.

### Area checks (on 31f372b)

- **Admin:** 118 unit tests pass, plus tsc, eslint, stylelint and `build:e2e`.
- **Admin Playwright (no API, the calls are faked):** 17 pass: 6 smoke, 8 AD-02 role cases, 3 area journeys.
  - **AD-02** checks 7 roles and one union (catalogue + finance). Each case checks the exact 06 §3 sidebar, opens every screen and asserts no 403, then opens a hidden URL and checks that it shows no access. The fake refuses what the policy goldens refuse, so a wrong role map fails the test; a deliberately broken field-agent entry did fail it.
  - **Area journeys:** an admin verifies a submitted business (the page sends `POST …/verify {}`); an admin force-closes a store until 09:00 IST (sent as 03:30 UTC); support hides a reported review and restores it, each with a reason.
- **Backend:** `cmd/todayz` admin HTTP tests (`admin_shops_http_test.go`) and `internal/admin` pass. `make gen` is clean.
- **Screenshots** (git-ignored, in `admin/test-results/screenshots/`): `admin-a03-queue.png`, `admin-a03-verified.png`, `admin-a05-list.png`, `admin-a05-force-close.png`, `admin-a12-detail.png`.

### E2E-19 on a fresh stack (f73eca2)

The run started from `make reset && make dev` (rc 0, all health checks pass). The journey ran on its own:

```
scripts/no-egress.sh pnpm --filter @todayz/e2e-journeys run journeys specs/12-e2e-19-blocked.spec.ts
```

| Journey | Result | Time |
|---|---|---|
| E2E-19: a blocked customer cannot sign in or order; unblocking restores them | **pass** | 1.0 min |

- **API log:** no 5xx. The 401, 403, 409 and 429 answers are the refusals the journey asserts.
- **No secrets in the output:** a grep of the run log and the reset log for the test phone prefix and `+91` found 0 matches.
- **Stack:** `make stop` ran afterwards (rc 0; data volumes are kept).

### E2E-12: not run yet

Both forms (API and A-17 UI) are written and they typecheck. The journey needs referee credits from E2E-02 and E2E-06 (specs 01- and 03-), so it has to run inside the full journey suite, not on its own. The A-17 screenshot (`a17-settings-1280.png`) is written to `$TZ_QA_DIR` on that run.

## Still to build in W3-05

- **Screens:**
  - A-02 Pulse: waits on the W3-07 Pulse API (`getAdminPulse`, `getAdminPulseMap`, `admin.pulse.view`), which is on the unmerged branch `claude/w3-07-rollups-15028a`;
  - A-08 catalogue, with the `<AssistSlot/>`;
  - A-16 notifications;
  - A-18 admin users;
  - A-20 banner moderation.
- **Tests:** AD-01 as a Go integration test (every admin route against every role); Playwright for the screens above.
- **Parts of the built screens left out:** document view links; the A-03 notes, credit and orders sections; the A-05 map and the stale and category filters; telling the owner when a store is sent back (the business app does not show the note yet).

## Notes and choices for the lead

1. **Audit permissions:** read under the conservative rule.
   - The audit log needs `audit.view`.
   - The admin_user subject panel also needs `users.view`.
   - Each subject panel needs that subject's own view action (a business needs `admin.business.view`, a review `admin.review.moderate`, and so on); an unknown subject type is refused.
2. **Blocks:**
   - a block is global, not per shop;
   - blocking an admin is refused with 409.
3. **Customer phone search:** the phone goes in the API query string but never in the UI URL.
4. **Where code lives:** admin order reads are in the orders package, not in `internal/admin`.
5. **Nav role map:** inferred from 06-screens.md, because the plan has no explicit table.
6. **Settings:**
   - Groups come from the key prefix.
   - The value is untyped in the contract.
   - A PATCH that changes nothing writes no audit row.
   - Text values are trimmed and capped at 200 characters, and `min_client_version_*` must be semver.
   - The UI builds the action name `settings.update` from parts, so the message lint does not read it as a message id.
7. **E2E-19 test phone:** moved to 9000000056, because main gave 046 to `delivery`.
8. **TOTP in the journeys:** codes use the local `Date.now()`, as the login does, not the test-clock offset.
9. **Worktree setup:** the worktree's `.env.local` is a symlink to the main checkout's. It is git-ignored.
10. **Follow-up outside W3-05:** the business i18n test fails on `pos.storeSales.offline` (the title case of "Sales"). This comes from W2-16.
11. **A-03 businesses:**
    - Verify works only from submitted, and it makes the stores waiting for review live.
    - Reject works from submitted or needs_info, suspend from verified, and reinstate from suspended.
    - KYC is reviewed as one record, and a rejection needs a note.
    - A reveal shows one field, is audited by field name only, is sent with no-store, and is kept in component state only.
    - A document rejection needs a reason; accepting one is direct.
12. **A-05 stores:**
    - Approve needs a verified business. Send back moves the store to draft with a note.
    - Force-close works only on live or paused stores, until a future time entered in IST. Reopen works only while a closure is running.
    - A strike reset does not recount.
13. **Leads:** the phone is masked for every role, and field agents see only the Leads tab.
14. **A-17:** a new addresses group for `customer_max_saved_addresses` (W2-15).
15. **Found and fixed in the shell (user-visible):**
    - The admin smoke had been broken on main by the role-aware nav (5 of 6 tests failed).
    - A field agent landed on "no access" at `/`. A role without Pulse now starts at its first screen (Businesses).
    - The router had no `defaultErrorComponent`, so a page crash showed TanStack's untranslated text. `RouteError` now shows the translated fallback inside the shell, with a retry.
    - The gate briefly mounted a staff page right after sign-out, and its API calls were refused.
    - The phone rail links shrank to 37px; they now keep 44px and the rail scrolls.
    - The audit panel was narrow in A-12.
16. **Follow-ups for the lead:**
    - In-text links (store and business names) show the browser's default blue across admin. This needs a global link style in `shared/ui`.
    - The reviews table clips its last column ("Open reports") at 1280px.
