# W3-05 Admin console: QA report

This report covers the admin console work package so far. It lists the backend and the screens that are built, the journeys, and what is still open.

- **Branch:** `claude/determined-meninsky-950b06`. Main (W2-15 delivery, migrations 21501 and up) was merged in as f73eca2.
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

## Evidence

### Area checks (on 70bc5ae)

- **Admin:** 99 unit tests pass, plus lint, tsc, `build:e2e`, the size budget and the message lint.
- **Backend:** the admin and settings packages pass their tests. `make gen` is clean.
- **Journeys:** the typecheck (`pnpm run typecheck` in `e2e/journeys`) is clean on f73eca2.

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
  - A-02 Pulse;
  - A-03 businesses, with a Leads tab that is read-only for support;
  - A-05 stores;
  - A-08 catalogue, with the `<AssistSlot/>`;
  - A-12 reviews (restore with a reason);
  - A-16 notifications;
  - A-18 admin users;
  - A-20 banner moderation.
- **Tests:**
  - the role-matrix E2E (AD-01, AD-02);
  - Playwright for each area.

## Notes and choices for the lead

1. **Audit permissions:** read under the conservative rule.
   - The audit log needs `audit.view`.
   - The admin_user subject panel also needs `users.view`.
   - The business subject audit is refused for now.
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
