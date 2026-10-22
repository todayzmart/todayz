# W0-05 Frontend shells: QA report

- **Branch:** `wp/W0-05-frontend-shells`
- **Machine:** macOS arm64, Node 25.9, pnpm 10.32, GNU Make 3.81
- **Date:** 2026-10-22
- Every run was local; nothing touched a cloud account.
- Built by the W0-05 agent. The lead saved this report, rebased the branch onto `669e414` and re-ran `make check` and `make e2e`.

## Results

- **`make check`** under `scripts/no-egress.sh`: exit 0, with no egress attempts recorded. The repo checks pass too: canonical targets, fail-fast make, SC-PL-03, SC-PL-04 and hooks.
- **`make e2e`** under no-egress: exit 0.
- **Unit tests (Vitest), 94 passing:**

  | Package | Tests |
  |---|---|
  | config | 9 |
  | tokens | 11 |
  | i18n | 18 |
  | telemetry | 12 |
  | customer | 9 |
  | business | 14 |
  | admin | 14 |
  | site | 7 |

- **Smoke tests (Playwright Chromium), 17 passing:** customer 5, business 5, admin 4, site 3.
  - Each test fails on any console error, page error or request to a non-local host.
  - They cover phone (375×812) and desktop (1280×800) sizes, Figtree served from the app's own origin, nav state with `aria-current`, the pseudo-locale (`lang="en-XA"` and bracketed text), and that the manifest and icons are served locally.
  - For the site they also check there is no `<script>` and no script request.
- **Lint and types:** ESLint on 8 packages and Stylelint on 4 apps (including `.astro` style blocks) give 0 warnings. `tsc` on 7 packages and `astro check` give 0 errors.
- **Undeclared CSS vars:** each app has a test that fails if its CSS uses a var that `@todayz/tokens` does not declare.
- **Manual check:** the dev servers at 127.0.0.1:5301–5304 were opened at phone and desktop sizes, including the pseudo-locale. There were no console errors and every request was local. The servers were stopped afterwards.

## Bundle sizes (gzip, size-limit)

"Initial" is the entry script plus its modulepreloads and stylesheets.

| App | Initial JS | Budget | CSS | CSS budget |
|---|---|---|---|---|
| customer | 112.07 KB | 170 KB | 7.68 KB | 20 KB |
| business | 112.93 KB | 250 KB | 7.62 KB | 20 KB |
| admin | 113.2 KB | 250 KB | 7.57 KB | 20 KB |
| site | 0 KB JS (enforced by `verify-dist`) | 0 | n/a | n/a |

## What was built

### Shared packages

- **`@todayz/tokens`** (`shared/tokens`): `make gen` turns `design/source` into git-ignored output:
  - `tokens.css` (115 vars on `:root`, light theme only) and `tokens.ts`.
  - `bundle.css`, the `tz-` component CSS with the Google Fonts `@import` removed.
  - `fonts.css`, 10 self-hosted families: Anek, Figtree and Noto for Devanagari, Kannada, Tamil and Telugu.
  - Favicons and PWA icons.

  It also exports `styles.css`, `./vite` (serves and emits the icons, head tags, and a manifest built from the token colours) and `./css-vars` (used by the undeclared-var test).
- **`@todayz/config`** (`shared/config`):
  - ESLint flat preset: `tseslint.configs.strict` (not type-aware), react-hooks, jsx-a11y, react-refresh, and no imports from a sibling app.
  - tsconfig base and a React variant.
  - Vitest preset.
  - Playwright preset: random free port on 127.0.0.1, and a `guardPage` helper that aborts and records non-local requests.
  - Vite preset: 127.0.0.1, strictPort, `*.todayz.localhost` hosts, HMR over wss on 443, `TODAYZ_HMR=direct` for plain 127.0.0.1, and an alias that drops the ICU parser.
  - Stylelint preset: no hex, named or function colours outside `shared/tokens`.
  - Fixtures that prove each lint rule fails when it should.
- **`@todayz/i18n`**:
  - Compiles the catalogues to ICU ASTs.
  - Builds the en-XA pseudo-locale: accented, at least 1.4× longer, bracketed, placeholders kept.
  - `?locale=pseudo` switches to it. Formatting always uses the en-IN locale.
- **`@todayz/telemetry`**: `initTelemetry` does nothing in local and test environments or when no RUM endpoint is set. When it is on, it uses a beacon queue and loads web-vitals lazily. Sentry stays off (D-17).

### The three Vite apps

- All three use React 19.3, TanStack Router file routes (route trees generated and git-ignored, with automatic code splitting), TanStack Query, react-intl with precompiled messages, vite-plugin-pwa (precache plus a CacheFirst cache for fonts; the service worker runs in production builds only) and the telemetry stub.
- **customer** (port 5301): a `tz-bnav` bottom nav (Home, Search, Orders, Wallet, Account) and a home page with the hero wash.
- **business** (port 5302): bottom tabs below 1024px; a `tz-side` sidebar from 1024px up, with the "Business" tag and the Grow and Shop groups.
- **admin** (port 5303): a `tz-side` sidebar with the "Admin" tag and the Growth and Access groups. Below 1024px it becomes an icon rail. Pages carry `robots: noindex`.
- Unknown sections return a 404.

### The site

Port 5304: static Astro 7 with zero client JS, the same tokens, CSS and fonts, and a placeholder home page. A `verify-dist` check fails the build on any JS, `<script>`, font CDN reference or remote asset.

### Make, docs and lockfile

- **`mk/40-frontend.mk`** adds the steps `setup-frontend` (Playwright Chromium headless shell), `gen-frontend`, `check-frontend` (lint, lint:css, typecheck, test, build, size), `e2e-frontend-smoke` (builds first, one app at a time) and `dev-frontend`.
- The folder CLAUDE.md files for customer, business, admin and site have Layout, Conventions and Status sections.
- `pnpm-lock.yaml` changed only for these packages' dependencies, all pinned to releases at least 2 weeks old.

## Screenshots

These are in this folder. `make e2e` writes screenshots to the git-ignored `test-results/screenshots/`; `TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones:

- customer: phone, desktop, pseudo-phone
- business: phone, desktop, pseudo-phone, pseudo-desktop
- admin: phone, desktop, pseudo-desktop
- site: phone, desktop

## Known gaps

- Every screen is a placeholder. Sign-in, role filtering of the admin nav and the offline POS service worker strategy belong to later work packages.
- The site has no pseudo-locale (zero JS means no runtime switch). It also has no sitemap, robots, Open Graph or JSON-LD yet; the site work package adds them.
- ESLint does not parse `.astro` files. `astro check` covers their types and Stylelint covers their styles.
- The PWA precache is about 550 KB per app: the app shell and icons (`**/*.{js,css,html,png,svg}`). Fonts are not precached; they use a runtime CacheFirst cache.
- RUM and Sentry are stubs only.
- Hidden sourcemaps still write `.map` files into `dist/` (unreferenced, not precached). Wave 5 deploy uploads them to Sentry and keeps them out of the public bucket.
- No smoke test yet for service-worker registration (later WP).

## Notes for W0-02 (local stack)

- **Astro 7 backgrounds itself** when it detects an agent. The site scripts pass `--ignore-lock` to stay in the foreground, so proc.sh calls the package scripts.
- **A stray `~/node_modules` leaked into the Astro build.** Astro's prerender bundle imports `cookie`, `html-escaper` and `zod` as bare names that pnpm cannot resolve from `site/dist`, so Node walked up to `/Users/sairahul/node_modules`. The site config now bundles all three (`vite.environments.prerender.resolve.noExternal`).
- **Caddy routing:** `app.`, `business.`, `admin.todayz.localhost` and `todayz.localhost` proxy to ports 5301–5304, including WebSocket upgrades for HMR on 443.

## Requests to lead, with decisions

1. **Save this report:** done.
2. **Allow the esbuild and unrs-resolver build scripts:** added `onlyBuiltDependencies` to `pnpm-workspace.yaml` on this branch.
3. **Pin Node 24 LTS:** deferred to W0-06. CI pins Node 24 LTS there, and the local engines range stays `>=22.12` so the user's Node 25.9 still works. Tools whose engine ranges exclude 25 stay on their current pins until then.
4. **`shared/CLAUDE.md` Status:** updated on this branch.
5. **Stray `/Users/sairahul/node_modules`:** reported to the user. It is outside the repo, so the lead does not delete it.

## Review fixes (Code Reviewer, no blockers)

Commit `W0-05: review fixes`. The lead re-ran `make check` and `make e2e` (stack smoke plus 19 Playwright tests) on main `572ca76`: both pass, and the tree stays clean.

1. **Font test was vacuous.** `document.fonts.check()` returns true with no `@font-face` at all. `expectFontLoaded()` now waits for a 200 on the self-hosted Figtree woff2 from the page's own origin and a `loaded` FontFace. Mutation-checked: removing the `fonts.css` import fails customer and site.
2. **Screenshots no longer rewrite committed files.** The default is `test-results/screenshots/`; `TZ_QA_DIR` opts in. Animations are disabled for captures.
3. **D-84:** business `/` redirects (replace, query kept) to `/orders`. Overview moved to `/overview`.
4. **Adding a route:** the folder CLAUDE.md files now say to add a static route file and edit nothing in `nav.ts`. A routing test per app proves static routes win over `/$section`.
5. **Telemetry:** route labels use the router's route id, otherwise id-like segments become `{id}`. `captureError` scrubs JWTs, bearer tokens, emails, phone numbers, long tokens and 4–8 digit codes.
6. **Sourcemaps:** `hidden` in all apps, Workbox maps off. `dist-check.mjs` fails a build that ships a `sourceMappingURL` comment.
7. **Install prompt** (06: only after the 2nd order): `beforeinstallprompt` is prevented and stashed in `customer/src/pwa/install.ts` for the later WP.

Nits done:
- Active bottom-nav icon is ink with a 2px stroke, per `bundle.css`. The design README says brand; the CSS wins.
- Business touch density below 1024px.
- Admin rail links are 44px with tooltips, and the rail has no overflow.
- ESLint bans raw colour literals in TS/TSX.
- Site `verify-dist` rejects any remote asset.
- i18n has a real sentence-case test.
- `isLocalUrl` and `guardPage` have unit tests.
- `?locale=pseudo` works only outside production builds; e2e builds use `--mode e2e`.

Counts after the fixes:

| Suite | Tests |
|---|---|
| Vitest | 176 (config 27, i18n 20, tokens 11, telemetry 33, site 24, customer 18, business 23, admin 20) |
| Playwright smoke | 19 (customer 5, business 7, admin 4, site 3) |

Bundle sizes are unchanged: about 113 KB JS and 7.6 KB CSS (gzip) per app.
