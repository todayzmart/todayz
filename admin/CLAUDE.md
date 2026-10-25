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

- To build a section: add your route file(s) under `src/routes/<area>/` (e.g. `src/routes/businesses/index.tsx`). A static route outranks `/$section`, so your page replaces the placeholder. Edit nothing in `nav.ts` or `$section.tsx` (`src/app/routing.test.ts` proves the precedence).
- Tokens only; i18n keys in `shared/i18n/messages/admin/`. Dev port 5303, Host `admin.todayz.localhost`. Initial JS budget 250 KB gzip. Hidden sourcemaps, checked by `build` as in `customer/`.
- Smoke screenshots go to the git-ignored `test-results/screenshots/`; `TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones.

## Auth and gate (W1-08)

- `src/shell/Gate.tsx` (root route) shows a skeleton while the refresh cookie is tried, sends a signed-out visitor to `/sign-in?next=<where they were going>`, holds a signed-in person at `/two-step` until the authenticator step is done, and renders the sidebar `Shell` only after both. `/sign-in` and `/two-step` are the only open routes.
- `src/app/`: `session.ts` (`@todayz/auth` session for app `admin`; token in memory only), `env.ts`, `labels.ts`, `next.ts`, `update-context.tsx`, `second-factor.tsx` (routing hint kept in `sessionStorage` key `todayz.admin.2fa`; the API checks the token, not this flag), `totp.ts` (thin wrappers over the generated `enrolTotp`, `verifyTotp`, `recoverTotp`, plus `totpError`).
- A-01 is two steps: `/sign-in` (SMS code, `nextStep` from verify says enrol or verify) then `/two-step`: first sign-in shows the setup key and an otpauth link (no QR code yet, no library), asks for the first code, then shows the 10 recovery codes once; later sign-ins ask for the code; a lost phone uses a recovery code. Five wrong codes end the session. Recovery codes live in component state only.
- Step-up (D-64): `src/app/StepUpDialog.tsx` asks for a fresh authenticator code when a † action answers `403 step_up_required`, signs in with the returned token, then the screen retries the same change (A-17 uses it). Role filtering of the nav is in `src/app/nav/access.ts`.
- `src/test/render.tsx` renders `<App>` signed in and past the second step by default.
- Stack journeys: `e2e/sign-in.stack.spec.ts` via `playwright.stack.config.ts` against `make dev`: `pnpm --filter @todayz/admin exec playwright test -c playwright.stack.config.ts`. Set `TZ_ADMIN_PHONE` to a seeded staff number to get past the SMS step.

## Status

W0-05: shell, routing, i18n, PWA manifest, telemetry stub, size budget and smoke tests in place. Screens are placeholders.

W1-08: sign-in gate, A-01 SMS step and two-step check (TOTP calls on the generated client since W1-01b), offline banner, update toast, error boundary and SW precache check.

Offline first load: the gate shows `SessionPending` (offline notice and "Try again") while the session is `unknown` and the API cannot be reached.

W3-05: role-aware sidebar (7 roles), A-06 customers (block, unblock, strike reset, session revoke), A-07 orders, list and detail (admin cancel with reason), A-19 audit log and the shared audit side panel (`src/audit/`), A-17 settings (`src/settings/`: groups, values by unit, ranges, provider keys set/missing, edit with reason, step-up, change history; money keys super_admin only).
