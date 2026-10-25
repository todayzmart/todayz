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
| 4300035 | API for A-03 businesses, A-05 stores and the Leads tab; migration 30502 (lead status columns, `stores.review_note`) |
| db0cf55 | Screens A-03 (queues by status, verify, ask for info, reject, suspend, reinstate, KYC review and one-field reveal, documents) with the Leads tab, and A-05 (filters, review queue, pin and hours, force-close, reopen, strike reset) |
| 9894128 | AD-02 role-matrix Playwright; the admin smoke fixed for the role-aware nav; shell fixes (below) |
| 31f372b | Playwright journeys for A-03 verify, A-05 force-close and A-12 hide and restore |
| ea7d5f0, 869fc6b | A-08 catalogue: API and screen. Products (filters in the URL, add, edit only the changed fields with a reason, retire), the review queue oldest first with duplicate suggestions (approve, merge into, reject), categories (two levels) and search words. The queue answers `assistEnabled` so the `<AssistSlot/>` for W3-13 mounts without the catalogue role reading settings |
| 1609f3e, 05f65bb | A-20 banners: API and screen. Review, live and taken-down tabs with a store filter in the URL; clear a held banner with an optional note, take one down with a required reason; support and up |
| 1c5517c | A-17 on a phone: below 720px each setting stacks (key and description, then value, range and last change as label and value lines, then the button) |
| e9d3cb4, c698eb7 | A-18 admin users: API and screen. Active and Disabled tabs, invite by phone with roles, change roles, disable, enable, reset the authenticator; super_admin only, step-up, a reason, the version read, the audit panel. No self-change, and no change that leaves no active super_admin |
| e12fb27, 3cc4b46, 3b0cc8b | Cherry-picked from W3-07 (the lead's call): the Pulse contract and the `admin.pulse.view` policy, so A-02 builds against them |
| a97da6b | A-02 Pulse: range (today, 7, 30, 90 days) in the URL, two KPI strips with the change on the period before, trend chart with a metric switch, funnel, where orders leak (with a reason filter), retention with a cohort heat table, searches that found nothing, credits, stores needing attention (a link only for roles that can open stores), and stores by city |
| 4fbf2fa | AD-01 as a Go test over the registered policy tables (`authz/policy/roles_test.go`) |

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

### Review fixes (6b9658b, f3ef175)

The lead's review items 1–5 and 7. Item 6 (30501 `CREATE INDEX CONCURRENTLY`) is skipped: 30501 is already on main, `db/migrations/README.md` forbids editing a migration on main, and prod's first apply runs it on an empty table, so a plain index build takes no real lock time.

- **Backend:** `cmd/todayz` `TestAdmin*` and `TestRoutesAreInTheContract`, `internal/admin`, the settings package, `internal/platform/migrate` (`TestUpDownUp`), the settings lint, vet, gofmt and golangci-lint (0 issues) all pass.
- **Contract:** lint, client and hooks checks pass; `make contract-smoke` passes (rc 0).
- **Admin:** 120 unit tests and tsc pass; Playwright (smoke, AD-02 roles, area journeys) 17 pass. The journeys typecheck is clean.
- **What the tests prove:**
  - a setting outside its min/max is refused (422), never clamped, and a stale step-up on a money key is refused;
  - the old GET search answers 404, and the new POST body never shows up in a path or query string;
  - three phone-shaped reasons each get 422 on `body.reason`, and the reply never echoes the number;
  - an admin account cannot be blocked, strike-reset or signed out (409), and its detail lists no sessions;
  - a staff-only account (no customer profile) cannot be blocked (409).

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

#### Area checks (on 4fbf2fa)

- **Admin:** 159 unit tests pass, plus tsc and eslint. The admin message lint has 0 errors (5 warnings, the same as before).
- **Admin Playwright (no API, the calls are faked):** 22 pass (smoke, AD-02 roles, area journeys). AD-02 now covers the A-08, A-20, A-18 and Pulse reads; new journeys cover A-08, A-20, A-18, A-17 at 375px and A-02.
- **AD-01:** `TestAdminRoleMatrix` passes and checks these rules:
  - catalogue reviews the queue and is denied every order, credit, payout, settings and risk action;
  - analyst reads Pulse and is denied every admin action that is not a read;
  - Pulse is open to every role except field_agent;
  - KYC verify is admin and super_admin only;
  - money settings are super_admin only, with step-up;
  - any `admin.field.*` action is own-only for a field agent.

  Payouts, credit adjust, risk and field stats have no actions registered yet. The test matches them by prefix, so they are checked from the day they land. The 403 on each route comes from the handler's `authz.Authorize` call, which the admin HTTP tests exercise.
- **Backend:** `go test ./internal/platform/authz/...` passes, and so do the admin and banners packages.
- **Screenshots** (git-ignored, in `admin/test-results/screenshots/`): `admin-a02-pulse.png`, plus the A-08, A-20, A-18 and A-17-phone shots.

### E2E-12: not run yet

Both forms (API and A-17 UI) are written and they typecheck. The journey needs referee credits from E2E-02 and E2E-06 (specs 01- and 03-), so it has to run inside the full journey suite, not on its own. The A-17 screenshot (`a17-settings-1280.png`) is written to `$TZ_QA_DIR` on that run.

## Still to build in W3-05

- **A-16 notifications:** waiting for the lead's answer on scope. The proposal is: templates read-only, integrations shown as set or missing, a test send only to the caller's own phone, and no PATCH.
- **A-02 map:** the page shows stores by city and a count of demand areas. A real map with demand heat needs h3-js and maplibre in admin, or a map layer in `shared/ui`; that is the lead's call.
- **Parts of the built screens left out:** document view links; the A-03 notes, credit and orders sections; the A-05 map and the stale and category filters; telling the owner when a store is sent back (the business app does not show the note yet); A-08 bulk edit and import, and telling stores about a merge (nothing creates `pending_review` products yet).

## Notes and choices for the lead

1. **Audit permissions:** read under the conservative rule.
   - The audit log needs `audit.view`.
   - The admin_user subject panel also needs `users.view`.
   - Each subject panel needs that subject's own view action (a business needs `admin.business.view`, a review `admin.review.moderate`, and so on); an unknown subject type is refused.
2. **Blocks:**
   - a block is global, not per shop;
   - blocking an admin, or an account with no customer profile, is refused with 409;
   - unblock stays open for any account, so one blocked before the fix can be recovered;
   - phone-shaped text in any admin reason or note is refused (422), not redacted.
3. **Customer phone search:** since f3ef175 it is `POST /v1/admin/customer-search` with the phone in the JSON body, so it is in no URL, UI or API. The path is not `/customers/search` because apigen refuses a literal segment beside `{customerId}`.
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
14. **A-17:** `customer_max_saved_addresses` (W2-15) is in the lead's "customers" group (915a8fb); my own addresses group was dropped in the merge.
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
    - Banners (W2-07): `RemoveBanner` has no status guard, and the take-down audit row does not carry the reason.
    - An A-18 invite creates the phone identity with `verified_at` set to the invite time.
    - The last-super-admin guard is a backstop; the advisory lock serialises changes.
    - DataTable shows sort arrows on lists that are paginated on the server.
    - During a redirect, the shell can briefly mount the previous route's page. Pulse guards its queries with `enabled`; a general fix in the shell is a follow-up, because deciding from route matches looped in tests.
