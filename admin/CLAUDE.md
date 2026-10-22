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

## Status

W0-05: shell, routing, i18n, PWA manifest, telemetry stub, size budget and smoke tests in place. Screens are placeholders.
