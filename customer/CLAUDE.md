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

## Layout (W0-05)

- `src/main.tsx`: tokens CSS, holds back the browser install prompt (`src/pwa/install.ts`), locale, router, telemetry init (no-op locally; labels use the matched route id), render; registers the SW in production only.
- `src/app/`: `App.tsx` (IntlProvider → QueryClientProvider → RouterProvider), `router.ts` (QueryClient + router factory, used by tests too), `i18n.ts` (precompiled catalogues; `?locale=pseudo` loads en-XA in every mode except `production`).
- `src/pwa/install.ts`: `beforeinstallprompt` is prevented and stashed; `getDeferredPrompt()` hands it to the install banner, which shows after the 2nd order (06-screens.md).
- `src/shell/`: `Shell` (skip link, `main#main`, `tz-bnav` bottom nav), `nav.ts` (tabs + `PLACEHOLDER_SECTIONS`), `Icon`, `Placeholder` (SoonCard), `NotFound`, `shell.module.css`.
- `src/routes/`: `__root.tsx`, `index.tsx`, `$section.tsx` (placeholder for nav sections not built yet; unknown sections 404).
- `e2e/smoke.spec.ts`: Playwright against `vite preview` of a `--mode e2e` build on a random port. Screenshots go to the git-ignored `test-results/screenshots/`; `TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones.

## Conventions

- To build a section: add your route file(s) under `src/routes/<area>/` (e.g. `src/routes/search/index.tsx`). A static route outranks `/$section`, so `/search` renders your page and the placeholder stops being used. Edit nothing in `nav.ts` or `$section.tsx` (`src/app/routing.test.ts` proves the precedence). Keep the nav item; it is how users reach the section.
- Production builds use `sourcemap: 'hidden'`; `build` fails if shipped JS or CSS carries a `sourceMappingURL` (`@todayz/config/dist-check.mjs`).
- Icons: `lucide-react` through `shell/Icon` (stroke 1.5, aria-hidden). Text: `intl.formatMessage({ id })`, sentence case, no exclamation marks.
- CSS modules for layout, `tz-` classes for components, only `var(--token)` values (`src/test/tokens.test.ts` fails on an undeclared var).
- ICU messages are precompiled; the runtime parser is aliased away (`intlNoParserAlias`). Run `make gen` after editing a catalogue.
- Dev: port 5301 (strictPort), Host `app.todayz.localhost`, HMR over wss:443 via Caddy; `TODAYZ_HMR=direct` for http://127.0.0.1:5301.

## Auth, shell and onboarding (W1-08)

- `src/app/`: `session.ts` (the `@todayz/auth` session for app `customer`; token in memory only), `env.ts` (`VITE_API_URL`, `VITE_TURNSTILE_SITE_KEY`, defined by `vite.config.ts` through `@todayz/auth/build`; a production build fails without a real site key and an https API URL), `locale.ts` (language choice: stores it, loads the catalogue, PATCHes `preferredLang` when signed in), `place.ts` (C-02 location, this device only), `me.ts` (`patchMe`: PATCH /v1/me with If-Match), `labels.ts` (strings for the shared auth pieces), `next.ts` (`?next=` must be a same-app path; wraps `safeNext` from `@todayz/auth`), `update-context.tsx` (`useUpdateController`; C-11 checkout calls `useHoldUpdates(controller, open)` so the update toast waits).
- Routes: `/sign-in` (C-10), `/language` (C-01, `/` redirects first-run here), `/location` (C-02: GPS after an explainer, PIN code; locality search is `TODO(W1-06)`), `/account`.
- `src/test/render.tsx` renders `<App>` with a fake `fetch`; tests of routes live in `src/test/`, not in `src/routes/` (every file there is a route).
- `build` and `build:e2e` run `precache-check` (SC-GS-09): every chunk must be in the service worker's manifest.
- Stack journeys: `e2e/sign-in.stack.spec.ts` runs from `playwright.stack.config.ts` against `make dev`: `pnpm --filter @todayz/customer exec playwright test -c playwright.stack.config.ts`.

## Status

W0-05: shell, routing, i18n, PWA manifest, telemetry stub, size budget and smoke tests in place. Screens are placeholders.

W1-08: shell with sign-in (C-10), language (C-01), location (C-02), account, offline banner, update toast, error boundary and SW precache check. Account is real; Search, Orders and Wallet are still placeholders.
