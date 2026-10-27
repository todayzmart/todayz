# admin/

Admin console (admin.todayz.in; locally https://admin.todayz.localhost). Desktop-first; field-agent pages responsive. RBAC with 7 roles.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §3.3, §4.1, §4.4; 06-screens.md (A-xx); 01-product-rules.md.

## Run

- `make dev`, then open https://admin.todayz.localhost
- `pnpm --filter @todayz/admin test` / `build`

## Rules for this folder

- Import only from `shared/*` and `design/`, never from a sibling app.
- Every list is server-paginated with filters in the URL; every mutation shows the audit side panel.
- Tokens only, no raw hex; light theme only; i18n keys for all text.
- Never show a provider key value; integrations show set/missing only.

## Layout (W0-05)

Same structure as `customer/` (see its CLAUDE.md): `src/main.tsx`, `src/app/`, `src/shell/`, `src/routes/` (`index.tsx` is Pulse, `$section.tsx` placeholders), `e2e/smoke.spec.ts`.

- `src/shell/Shell.tsx`: white `tz-side` sidebar with the `tz-side-tag` "Admin" and the Growth and Access groups. Below 1024px it collapses to an icon rail of 44px links; each keeps its label as the accessible name and a `title` tooltip.
- `src/shell/nav.ts`: one list (`NAV`) named after 06-screens.md (A-xx). Role filtering (D-64) is added with sign-in.
- `index.html` sets `robots: noindex`.

## Conventions

- To build a section: add your route file(s) under `src/routes/<area>/` (e.g. `src/routes/businesses/index.tsx`). A static route outranks `/$section`, so your page replaces the placeholder. Edit nothing in `$section.tsx` (`src/app/routing.test.ts` proves the precedence). Since W3-05 a new section adds its `NAV` entry and `BUILT_SECTIONS` in `nav.ts` and its roles in `src/app/nav/access.ts`, and updates `e2e/roles.spec.ts` from 06 §3.
- Tokens only; i18n keys in `shared/i18n/messages/admin/`. Dev port 5303, Host `admin.todayz.localhost`. Initial JS budget 250 KB gzip. Hidden sourcemaps, checked by `build` as in `customer/`.
- Smoke screenshots go to the git-ignored `test-results/screenshots/`; `TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones.

## Auth and gate (W1-08)

- `src/shell/Gate.tsx` (root route) shows a skeleton while the refresh cookie is tried, sends a signed-out visitor to `/sign-in?next=<where they were going>`, and renders the sidebar `Shell` once the person is signed in. `/sign-in` is the only open route. There is no `/two-step` page any more (D-264): it is a plain "Page not found".
- `src/app/`: `session.ts` (`@todayz/auth` session for app `admin`; token in memory only), `env.ts`, `labels.ts`, `next.ts`, `update-context.tsx`, `StepUpDialog.tsx`.
- A-01 is one step (D-264): `/sign-in` takes the SMS code, and the verify's token opens the console; there is no authenticator, enrolment or recovery code. After too many wrong codes for one number (01 §2, `admin_signin_*` settings) the API answers `429 signin_locked`; the shared `PhoneOtpFlow` maps it to the `signinLocked` kind and shows `auth.error.signinLocked` with no countdown. Another super admin unlocks it on A-18.
- Step-up (D-64, D-264): `src/app/StepUpDialog.tsx` opens when a † action answers `403 step_up_required`. The admin taps "Send code" (nothing is sent before), `POST /v1/auth/step-up/send` texts a code to their own number, and the 6-digit code goes to `POST /v1/auth/step-up/verify`; the session is stamped for 10 minutes and the screen retries the same change with the same token. The resend button counts down from `resendAfter` or `Retry-After`; errors are translated (`auth.stepUp.*`): wrong code with tries left, expired code, too many codes, unavailable, and after the fifth wrong code only "Sign in again" (the API has ended the admin's sessions). The dialog stays mounted while closed, so a sent code and the countdown survive closing it. Used by A-09 credits, A-11 payouts, A-17 settings, A-18 admin users and A-21 money jobs. Role filtering of the nav is in `src/app/nav/access.ts`.
- `src/test/render.tsx` renders `<App>` signed in by default. `src/test/step-up.ts` fakes the step-up routes (`stepUpHandler`, `passStepUp`).
- Stack journeys: `e2e/sign-in.stack.spec.ts` via `playwright.stack.config.ts` against `make dev`: `pnpm --filter @todayz/admin exec playwright test -c playwright.stack.config.ts`. Set `TZ_ADMIN_PHONE` to a seeded staff number; the SMS code alone then opens the console (the test skips without it).

## Status

W0-05: shell, routing, i18n, PWA manifest, telemetry stub, size budget and smoke tests in place. Screens are placeholders.

W1-08: sign-in gate, A-01 SMS step, offline banner, update toast, error boundary and SW precache check. D-264 (27 Oct) removed the two-step check (`/two-step`, `second-factor.tsx`, `totp.ts`, the authenticator reset on A-18) and moved step-up to SMS.

Offline first load: the gate shows `SessionPending` (offline notice and "Try again") while the session is `unknown` and the API cannot be reached.

W3-05: role-aware sidebar (7 roles), A-06 customers (phone search is `POST /v1/admin/customer-search` so the number never sits in a URL; block, unblock, strike reset, session revoke; an admin account is read-only here and a staff-only account cannot be blocked, both 409 from the API), A-07 orders, list and detail (admin cancel with reason), A-19 audit log and the shared audit side panel (`src/audit/`), A-17 settings (`src/settings/`: groups, values by unit, ranges, provider keys set/missing, edit with reason, step-up, change history; money keys and, since D-264, the "Access" group (admin sign-in limits) super_admin only: the row's `superAdminOnly` picks the "Super admin only" label for a non-editable row). A-12 reviews (`src/reviews/`: reported first, hide and restore with a reason, report queue). A-03 businesses (`src/businesses/`: queues by status, verify, ask for info, reject, suspend, reinstate, KYC review and audited one-field reveal kept in component state only, document review) with the Leads tab (field agents see only Leads; phones masked for every role). A-05 stores (`src/stores/`: status and city filters, review queue, approve only for a verified business, send back, edit pin and hours, force-close until an IST time, reopen, strike reset for support and above). A-08 catalogue (`src/catalogue/`: products, review queue with merge, categories, search words, `<AssistSlot/>` when `assistEnabled`). A-20 banners (`src/banners/`). A-17 stacks into cards below 720px. A-18 admin users (`src/admin-users/`, super_admin only, step-up, no self-change; since D-264 a locked admin shows "Locked until …" and an "Unlock sign-in" button, `useUnlockSignIn` → `POST /v1/admin/users/{adminId}/unlock-sign-in` with a reason). A-02 Pulse (`src/pulse/`: range in the URL, KPIs, trend, funnel, leaks, retention, credits, attention, stores by city; no map yet). A-16 notifications (`src/notifications/`, read-only, D-224: channels with keys set/missing and last failure, kinds × languages with `{param}` chips, test send to your own in-app inbox for customer kinds; no edit). Pulse queries take `enabled: canOpen(roles, '')`, because during the redirect for a role without Pulse the old `/` page mounts briefly.

W3-06: A-04 field onboarding (`src/field/`, routes `/field`, `/field/new`, `/field/$businessId`): a phone-first wizard (details, store with location and hours, photos straight to the presigned URL and then confirmed, products one by one from search or new, owner's 6-digit code with tries left and a resend countdown). `submit_incomplete` sends the agent to the step that fixes it. Items, store and photos are read-only once the edit window closes (the server decides, D-39). A-15 (`/field-agents`): an agent sees their own numbers and an 8-week trend (`/field/me/stats`); admins see every agent (`fieldAgentsAll`), disabled ones marked. Both screens are in the sidebar for field agents and admins. Built for 360px: `e2e/field.spec.ts` runs there and fails on any sideways scroll.

Playwright (no API): `e2e/fake-api.ts` answers the calls (`signIn(page, roles)`, `answer`, `answerWith`; register catch-alls first, the newest route wins). `e2e/roles.spec.ts` is AD-02: per role and one union, the exact sidebar from 06 §3, every screen opened with no 403 (the fake refuses what the policy goldens refuse), a hidden URL shows no access. A role without Pulse starts at its first screen. A page that throws shows `RouteError` (`src/app/CrashFallback.tsx`) inside the shell. Rebuild with `pnpm run build:e2e` before `playwright test`.

W3-08: A-14 support inbox (`src/support/`, routes `src/routes/support/`): tabs by status with soonest due first, SLA timers from `serverNow`, assign, reply, internal notes, change the priority (audited; the due time moves with it), resolve and close with a reason, attachments through the presigned PUT, and `<AssistSlot/>` for W3-13. Playwright: `e2e/support.spec.ts`; READS entries in `e2e/roles.spec.ts`. Report: `docs/qa/W3-08/REPORT.md`.

W3-15: A-21 jobs (`src/jobs/`, routes `src/routes/jobs/`) on W1-11's admin-jobs API: list with status badges (Missed, Failing, Paused) next to the job, IST schedule text (`schedule.ts`, same shapes as the API's `scheduleText`), class and status filters in the URL; kind page with run history filters, run detail (counts, note, scrubbed error, attempts, Grafana Explore links from `VITE_GRAFANA_URL`, hidden when empty), and Run now (optional period), Pause, Resume, Retry, each with a reason and audited. `gate.ts` decides the buttons: none for `timer` kinds or read-only roles; `money` kinds only for super_admin after step-up. Two audit panels (job and run) take a `title`. A-22 system health (`src/system/`, admin and super_admin): one `GET /v1/admin/system`, panels for Beanstalk, ALB, RDS, alarms, database (top 10 SQL) and River, each with its age; locally the AWS panels say "Not configured (local)". Playwright: `e2e/jobs.spec.ts`, `e2e/system.spec.ts`; `localSystem` in `e2e/fake-api.ts`. Report: `docs/qa/W3-15/REPORT.md`.

W3-13: the AI assistants in `<AssistSlot/>` (`src/ai-assist/`, D-233, D-234): A-08 catalogue clean-up (normalise, merge into a look-alike, category names), A-14 ticket triage (classify, read-only summary, reply draft) and A-13 risk summary. `draft.ts` holds the create, poll and confirm (keyed by an Idempotency-Key) and maps errors (`assistErrorKey`, per-line `lineReasons`); `AssistCard` is the panel and `AssistLine` one line with Use, Edit, Skip (every line starts as Skip). A line the caller may not apply says so (`canApply`). Copy in `shared/i18n/messages/admin/aiAssist.*.json`. Tests: `src/ai-assist/assist.test.tsx`, `e2e/ai-assist.spec.ts`. Report: `docs/qa/W3-13/REPORT.md`.
