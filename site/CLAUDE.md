# site/

Astro marketing site (todayz.in; locally https://todayz.localhost). Static output, zero JS by default.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §4.5; design/source.

## Run

- `make dev`, then open https://todayz.localhost
- `pnpm --filter @todayz/site build`

## Rules for this folder

- Real HTML per route (no iframe wrapper); sitemap, robots, Open Graph, JSON-LD.
- Tokens only, no raw hex; light theme only.
- The lead form posts to `POST /v1/public/leads` (Turnstile protected).

## Layout (W0-05)

- `astro.config.mjs`: static output, dev port 5304 (strictPort), Host `todayz.localhost`, dev toolbar off. It also adds a small integration that serves and copies the `@todayz/tokens` icons and exposes the favicon and theme-color tags as `virtual:todayz-head`.
- `src/layouts/Base.astro`: `<html lang="en-IN">`, head tags, skip link, footer. It imports `@todayz/tokens/styles.css`, which brings the self-hosted fonts.
- `src/lib/messages.ts`: formats `shared/i18n/messages/site` at build time. It supports literals and `{args}` only, and throws on anything else.
- `scripts/verify-dist.mjs`: runs after `astro build` and fails on any client JS, `<script>`, font CDN, remote `url(…)`/`@import` in CSS, or remote `src`/`href`/`srcset` in HTML (only `https://todayz.in/` canonical links may be absolute). `src/lib/verify-dist.test.ts` covers each rule.
- `e2e/smoke.spec.ts`: `astro preview` on a random port; screenshots go to the git-ignored `test-results/screenshots/` (`TZ_QA_DIR=docs/qa/W0-05 make e2e` refreshes the committed ones).

## Conventions

- Always run Astro through the package scripts. They set `ASTRO_TELEMETRY_DISABLED` and `ASTRO_DISABLE_UPDATE_CHECK`, and pass `--ignore-lock` so dev and preview stay in the foreground. Astro 7 otherwise backgrounds itself when it detects an agent.
- Astro's prerender bundle inlines `cookie`, `html-escaper` and `zod` (`vite.environments.prerender.resolve.noExternal`). Under pnpm they are not resolvable from `dist/`, and Node would otherwise walk up the tree and load whatever copy it finds.
- Styles live in `<style>` blocks and use tokens only. Stylelint checks `.astro` files through postcss-html.

## Status

W0-05: placeholder home, tokens and fonts, zero client JS, smoke tests. Still to come from the site WP: sitemap, robots, Open Graph, JSON-LD and the lead form.
