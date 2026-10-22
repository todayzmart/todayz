# customer/

Customer PWA (app.todayz.in; locally https://app.todayz.localhost). Phone-first. React 19, Vite, TanStack Router/Query.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §4.1–4.2; 06-screens.md (C-xx); 01-product-rules.md.

## Run

- `make dev`, then open https://app.todayz.localhost
- `pnpm --filter @todayz/customer test` / `build`

## Rules for this folder

- Import only from `shared/*` and `design/`, never from a sibling app.
- Use tokens and `tz-` classes or `@todayz/ui`; no raw hex (Stylelint fails it). Light theme only.
- All user-facing text through i18n keys in `shared/i18n/messages/customer/<area>.en.json`.
- Own only your area's `src/routes/<area>/`; `routeTree.gen.ts` is generated and git-ignored.
- Initial JS ≤ 170 KB gzip (size-limit).

## Status

Stub from W0-01. The WP that builds this folder fills in layout and conventions.
