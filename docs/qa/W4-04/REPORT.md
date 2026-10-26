# W4-04 accessibility, visual and Lighthouse: report

Branch `worktree-agent-a0a262a1367ccd88f`, commit f3bd3406, merged to main in 119051dd. Run locally on 26 Oct 2026.

## What was built

- **`e2e/a11y` (`@todayz/e2e-a11y`).**
  - axe WCAG 2.0/2.1/2.2 A and AA on every route:

    | App | Routes |
    |---|---|
    | customer | 17 |
    | business | 20 |
    | admin | 24 |
    | site | 14 |

  - Viewports: 360×740, 390×844 and 1280×800, in English.
  - It also fails on sideways scroll (WCAG 1.4.10, which axe cannot see) and on a missing `html[lang]`.
  - The 360 phone run is repeated in hi, kn, te and ta for the three apps. The site's pages are already per language.
  - Signed-in routes run as the seed's asha, the owner and the super admin.
  - Files: `routes.ts`, `harness.ts`, `a11y.spec.ts`, `global-teardown.ts`, `playwright.config.ts`, `lighthouse.mjs`.
- **`e2e/visual` (`@todayz/e2e-visual`).** 56 baselines in `e2e/visual/baselines`, with a 2% tolerance and volatile nodes masked:

  | App | Baselines |
  |---|---|
  | customer: phone en and en-XA | 18 |
  | business: desktop en, desktop en-XA, phone en | 18 |
  | admin: the same three | 12 |
  | site: phone and desktop en | 8 |

- **`e2e/a11y/lighthouse.mjs`.** Lighthouse for the customer, business and admin shells, mobile and desktop. The site keeps its own gate.
- **`mk/48-a11y.mk`.**
  - `check-a11y` joins `make check`.
  - `e2e-a11y` and `e2e-visual` join `make e2e`.
  - `lighthouse-apps` runs on its own.
- **Also changed:**
  - `e2e/CLAUDE.md` (layout);
  - `.gitignore` (`e2e/a11y/out/`);
  - `pnpm-lock.yaml`: two new importers, edited by hand. The lead checked it with `pnpm install --frozen-lockfile --offline`.

## Results

### axe

| App | Before | After |
|---|---|---|
| customer, business, admin | 0 violations | 0 |
| site | 2 failures: `/ta/` at 360 and 390, color-contrast on the decorative order card | 0 |

- The last full run passed 436 of 436.
- After it, the customer CSS fixes for Tamil search, map and product at 360 were re-run, 15 tests, and pass.
- The full customer and site suites were not re-run after that last CSS change. The lead's release-candidate run covers them.
- The apps were already clean on axe. The real bugs were layout overflows that axe cannot see, found by the new scroll check and the language runs.

### Fixes (no restyling)

- `customer/src/home/Home.tsx`, `home.module.css`: the Tamil location chip ran past the edge of a 390 px phone. It now shrinks and truncates, with the full text in `title`.
- `site/src/components/Header.astro`: the Tamil header made `/ta/` 379 px wide on a 360 px phone. The gap, padding and Start button are tighter at 400 px and below.
- `site/src/styles/landing.css`:
  - a long Tamil word blew out the hero grid;
  - the order card's content overflowed its box, which axe reported as a contrast failure.

  Fixed with `minmax(0,1fr)` tracks, an ellipsis on the card head and `overflow-wrap` on h1.
- `customer/src/asks/asks.module.css` (`.cta`): the "ask nearby shops" button was 348 px wide in Tamil at 360. The label now wraps.
- `customer/src/map/map.module.css` (`.title`): the long Tamil title pushed the Map/List switch 11 px off a 360 px phone. The title now wraps.
- `customer/src/home/Home.tsx`, a Lighthouse win:
  - Before: customer home mobile LCP was 2763 ms (budget 2.5 s) and TBT 3.1 s, because MapLibre started on mount.
  - Now the map card loads only within 300 px of the viewport (`useNearView`). Where IntersectionObserver is missing, it loads at once.
  - `customer/src/test/setup.ts` stubs the observer.

### Visual baselines

- All 56 are new.
- A second full run was stable.
- The customer suite passed again after the last CSS changes.

### Lighthouse

Budgets: customer LCP < 2.5 s and CLS < 0.1; site ≥ 95.

| Page | Preset | Perf | A11y | BP | LCP | CLS | TBT |
|---|---|---|---|---|---|---|---|
| customer `/` | mobile | 74 (was 65) | 100 | 96 | 2284 ms (was 2763) | 0.001 | 1114 ms (was 3099) |
| customer `/` | desktop | 94 | 100 | 96 | 689 ms | 0.005 | 200 ms |
| customer `/search?q=atta` | mobile / desktop | 100 / 100 | 98 / 98 | 96 / 96 | 873 / 274 ms | 0 | 19 / 0 ms |
| customer `/sign-in` | mobile / desktop | 100 / 100 | 100 / 100 | 100 / 100 | 58 / 39 ms | 0 | 0 |
| business `/sign-in` | mobile / desktop | 99 / 100 | 100 / 100 | 100 / 100 | 1684 / 544 ms | 0 | 0 |
| admin `/sign-in` | mobile / desktop | 99 / 100 | 100 / 100 | 100 / 100 | 1691 / 423 ms | 0 | 0 |
| site, 9 pages | mobile and desktop | 100 | 100 | 100 | gate passes | | |

Measured on headless Chromium with software WebGL and mobile CPU throttling, so TBT on the home page is pessimistic.

### Checks run

- customer: typecheck, eslint, stylelint and vitest (195/195) pass.
- site: astro check, eslint, stylelint and vitest (172/172) pass.
- `e2e/a11y` typecheck and `make check-a11y` pass.

## Left open

1. **Customer home mobile performance is 74** when the map is near the viewport. A static "tap to load the map" placeholder, or splitting MapLibre, needs a product decision.
2. **Lighthouse heading-order on search (98).** The shared EmptyState renders an h3 with no h2 before it. It needs a heading-level prop in `@todayz/ui`.
3. **Coverage gaps:**
   - Lighthouse covers the signed-out shells only.
   - There is no screen-reader pass (TalkBack, VoiceOver) and no real-device run.
   - There are no scripted focus-order or focus-return checks on sheets and dialogs.
   - There is no 1440×900 run for business and admin.
   - The site has no en-XA visual baseline.
4. **Baselines were made on macOS Chromium.** CI on another OS needs its own set or a larger tolerance.
5. **OTP cap.** Each run that signs in counts against the cap of 20 sends per IP per hour. Run a11y and visual from a fresh `make reset`.
