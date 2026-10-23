# shared/ui (`@todayz/ui`)

The React UI kit: one component per design system card, rendering the design's `tz-` classes from `@todayz/tokens/styles.css` plus this package's `src/styles.css` (states and pieces the cards do not draw: focus, loading, overlays, sorting, tabs, the map overlay). Built in W1-02; owned by the lead afterwards (D-84): ask the lead for component changes.

Read first: the root `CLAUDE.md`, `shared/CLAUDE.md`, the design source (`design/source/README.md`, `design/source/components/<Card>/preview.html`), and 08-testing.md (visual and a11y rows).

## Use

```ts
import '@todayz/tokens/styles.css';
import '@todayz/ui/styles.css';           // after the tokens stylesheet
import { Button, ProductCard } from '@todayz/ui';
import { StoreMap } from '@todayz/ui/map'; // separate entry: only map screens bundle MapLibre
```

Entries: `.` (every component, `src/index.ts`), `./map` (StoreMap and its geo and style helpers), `./styles.css`. The package ships source (`exports` point at `src/`); the apps' Vite builds compile it. An app that imports `@todayz/ui/map` needs Vite's client types (`"types": ["vite/client"]` in its tsconfig): the loader imports MapLibre's worker with `?url`.

## Run

| What | Command (in `shared/ui`) |
|---|---|
| Storybook | `pnpm storybook` (127.0.0.1:6006) |
| Unit tests + axe per story + coverage | `pnpm test` |
| Lint, CSS lint, types | `pnpm lint`, `pnpm lint:css`, `pnpm typecheck` |
| Design comparison and Chromium axe | `pnpm build-storybook && pnpm visual design.spec.ts a11y.spec.ts` |
| Modal focus, Escape and toasts over modals (Chromium) | `pnpm build-storybook && pnpm visual overlays.spec.ts` |
| QA screenshots | `TZ_QA_DIR=docs/qa/W1-02/screens pnpm visual screens.spec.ts` |

`make check` runs `check-ui` (lint, types, unit + coverage, Storybook build). `make e2e` runs `e2e-ui-visual` (Storybook build, design comparison, Chromium axe). Both are offline: `visual/serve.mjs` serves only repo files on 127.0.0.1, and every Playwright test fails on an external request or a console error.

## Rules for this folder

- **Strings come in as props.** No i18n import, no English in components (the one default is the map credit `© OpenStreetMap`). Labels for icon buttons, counts read in words, empty states: all props.
- **Money, distance and time come pre-formatted** (`₹289`, `0.6 km`, `Confirmed 12 min ago`). The kit never formats or computes them. Stock freshness follows D-101: "Confirmed {time} ago", or `stale` with "Stock not confirmed recently".
- **Tokens only.** No colour literals in `.ts`, `.tsx` or `.css` (`src/test/styles.test.ts` fails on hex, rgb, hsl, oklch, a dark-theme query, or a `var(--x)` the tokens do not declare). Use `tz-` classes from the design first; add a `tz-ui-` class in `src/styles.css` only for what the design does not draw.
- **Light theme only. Lucide icons only** (`lucide-react` through `internal/Icon`). Sentence case in stories.
- **Every component has a `DesignReference` story** when the design has a card for it: same markup and synthetic names as `preview.html`, tagged with `designRef(card, height, width?)` from `src/test/fixtures.tsx`. `visual/design.spec.ts` compares it with the card at 0.1% (icons masked; the map drawing masked). A design reference story renders at the card's own size: the `frame()` decorator skips it.
- **Every story passes axe** in happy-dom (structure, names) and in Chromium (contrast). Do not disable a rule for a story; the only exceptions are design-owned findings listed in `visual/a11y.spec.ts`, matched by story, rule and node.
- **Coverage gate: 90%** lines, statements, functions and branches.
- **StoreMap must render offline.** MapLibre loads lazily; its worker comes from a bundled `?url` import (`loader.ts`, `setWorkerUrl`), never from MapLibre's runtime guess. Without WebGL, or when MapLibre fails, the static renderer draws the same pins, clusters and ring. Without `height` the map keeps the design's 312:150 ratio.
- No imports from any app folder.

## Layout

```
src/<Component>/   Component.tsx, Component.stories.tsx, Component.test.tsx
src/internal/      Icon, Modal (native <dialog>), VisuallyHidden, cx
src/test/          setup, fixtures (story frames, designRef), stories.test (axe on every story), styles.test
visual/            serve.mjs, catalog.ts (cards from Storybook's index + preview headers), specs
```

## Status

W1-02 done: every component in the WP list, 156 stories, 354 unit tests, 23 design cards matched, 2 Chromium overlay checks, axe clean in Chromium apart from the two CartBar contrast findings that come from the design CSS (see `docs/qa/W1-02/REPORT.md`).
