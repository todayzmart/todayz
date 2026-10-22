# business/

Business PWA with offline-first POS (business.todayz.in; locally https://business.todayz.localhost). Phone bottom tabs, desktop sidebar.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §4.1, §4.3; 06-screens.md (B-xx); 01-product-rules.md.

## Run

- `make dev`, then open https://business.todayz.localhost
- `pnpm --filter @todayz/business test` / `build`

## Rules for this folder

- Import only from `shared/*` and `design/`, never from a sibling app.
- Tokens only, no raw hex; Lucide icons; sentence case; light theme only.
- Every local write gets a client ULID and device sequence; the server is the real permission gate.
- All user-facing text through i18n keys in `shared/i18n/messages/business/<area>.en.json`.
- Initial JS ≤ 250 KB gzip (size-limit).

## Status

Stub from W0-01. The WP that builds this folder fills in layout and conventions.
