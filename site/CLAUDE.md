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

## Layout (W1-09)

- `astro.config.mjs`: static output, dev port 5304 (strictPort), Host `todayz.localhost`, dev toolbar off. A small integration serves and copies the `@todayz/tokens` icons and exposes the favicon and theme-color tags as `virtual:todayz-head`.
- `src/lib/pages.ts`: the page registry (id, English path, locales, title and description keys, lead-form flag). Routes (`src/pages/[...slug].astro`), sitemap, hreflang, footer and tests all read it. Home and retailers exist in en, hi, kn, te and ta; every other page is English only.
- `src/i18n/`: site-local messages (en, hi, kn, te, ta). English files hold `{defaultMessage}` descriptors; other locales hold flat strings plus `$meta.status: "machine-draft"` and fall back to English. Only literals and `{name}` arguments. The site does not import `shared/i18n` or `@todayz/ui`.
- `src/layouts/Base.astro`: head tags (canonical, hreflang, Open Graph, Twitter, JSON-LD), skip link, header, machine-draft banner, footer. The Turnstile script is added only when the page holds the lead form.
- `src/views/`: one view per page kind (Home, Retailers, Info, Legal, NotFound). `src/components/`: Header, Footer, LangSwitch, Faq, Section, Steps, FeatureGrid, Icon (Lucide, inline SVG), LeadForm.
- `src/lib/leads.ts`: typed `POST /v1/public/leads` (`TODO(W1-04)`: switch to `@todayz/api-client`). Env: `PUBLIC_API_URL`, `PUBLIC_TURNSTILE_SITE_KEY` (a test key is the default and must not ship), `PUBLIC_CUSTOMER_APP_URL`, `PUBLIC_BUSINESS_APP_URL`.
- `src/lib/company.ts`: facts the pages show; anything undecided is null and the pages say so.
- `src/lib/seo.ts`: sitemap, robots and the old-URL redirect map; `public/_redirects` must equal `redirectsFile()` (tested).
- `public/og/`: committed Open Graph PNGs, one per page and language. Refresh with `pnpm build && pnpm og`.
- `scripts/verify-dist.mjs` (after `astro build`): fails on any script except same-origin hashed `/_astro/*.js`, inline JSON-LD, and the Turnstile URL on pages that hold the lead form; also on font CDNs, remote `url()`/`@import`, and remote `src`/`href`/`srcset` (only `https://todayz.in/` and `<a href>` to the app origins are allowed). `scripts/check-site.mjs` then checks internal links, fragments and runs the HTML validator.
- `lighthouserc.json` + `scripts/lighthouse.mjs`: Lighthouse CI on dist/ (>= 95 in every category, filesystem output in `.lighthouseci/`, Turnstile host blocked). It runs at the end of `e2e:smoke`.
- `e2e/`: `smoke.spec.ts`, `site.spec.ts` (SC-SITE-01..06; Turnstile and the leads API are stubbed in `helpers.ts`), `screens.spec.ts` (only with `TZ_QA_DIR`).

## Conventions

- Always run Astro through the package scripts. They set `ASTRO_TELEMETRY_DISABLED` and `ASTRO_DISABLE_UPDATE_CHECK`, and pass `--ignore-lock` so dev and preview stay in the foreground. Astro 7 otherwise backgrounds itself when it detects an agent.
- Astro's prerender bundle inlines `cookie`, `html-escaper` and `zod` (`vite.environments.prerender.resolve.noExternal`). Under pnpm they are not resolvable from `dist/`.
- Styles live in `<style>` blocks and use tokens only (no raw hex). Stylelint checks `.astro` files; `src/lib/tokens.test.ts` checks every variable used exists in the tokens.
- All visible text goes through message keys. Never state a figure, price or claim the product rules do not hold.

## Status

W1-09: all pages (S-01..S-06), lead form, SEO, localisation scaffolding, checks. Legal text is a draft awaiting legal review; non-English text is machine-drafted awaiting human review.
