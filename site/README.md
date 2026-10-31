# Marketing site

The Astro static site: pages for shops and customers, in English, Hindi, Kannada, Telugu and Tamil.

| | |
|---|---|
| Local | https://todayz.localhost (Astro on port 5304, through Caddy) |
| Prod | https://todayz.in and www (Cloudflare Pages project `todayz-site`) |
| Package | `@todayz/site` |

## Run it

```bash
make dev                              # whole stack
pnpm --filter @todayz/site dev
pnpm --filter @todayz/site typecheck
pnpm --filter @todayz/site lint
pnpm --filter @todayz/site test       # Vitest
pnpm --filter @todayz/site build      # also runs verify-dist and check-site
pnpm --filter @todayz/site e2e:smoke  # Playwright, then Lighthouse
```

Run `make gen` first on a fresh checkout. Production builds need `PUBLIC_API_URL` and `PUBLIC_TURNSTILE_SITE_KEY`.

> As built on main at a40f889d (31 Oct 2026).

## Branding

The header and footer keep the original primary logo unchanged. `src/styles/brand-accents.css` derives green-black glass tones from the original green and silver tokens. The homepage replaces the decorative circle with `DiscoveryPreview.astro`: example product photos, stock freshness, shop names, prices and walking distances, linked to customer-app searches. Shelf rows also show their existing product imagery. The compact dark-glass footer has shopper and retailer entry points, company/legal navigation and language links; the oversized decorative wordmark is removed. All existing forms, search destinations and calculator behavior are retained.

## Layout

| Folder under `src/` | What |
|---|---|
| `pages` | One file per route (`trailingSlash` is always) |
| `views`, `components`, `layouts` | Page bodies and shared pieces |
| `i18n` | Strings per language; Hindi, Kannada, Telugu and Tamil approved by the founder (D-262) |
| `lib`, `styles`, `assets` | Helpers, CSS, images |

`scripts/headers.mjs` writes `dist/_headers`. The lead form posts to `POST /v1/public/leads` with a Turnstile token.

## Quality gates

Lighthouse at least 0.95 in every category, mobile and desktop. Exception (D-102, widened to `/kn/` by D-262): mobile performance on `/hi/`, `/kn/` and `/te/` is gated at 0.85. Details: [testing guide](../docs/guide/testing.md).

## More

- [Features](../docs/features/README.md)
- [Local development](../docs/guide/local-dev.md), [testing](../docs/guide/testing.md), [deployment](../docs/guide/deployment.md)
- [Working rules for this folder](CLAUDE.md)

## Liquid Silver presentation — 30 Oct 2026

As built on base commit e986a157, with this presentation change. The founder requested the customer app's selected Liquid Silver direction on todayz.in. Base.astro loads styles/silver.css on every route: silver page and neutral semantic tokens, graphite actions, frosted header and search, translucent cards, and silver image wells. Original product photos and semantic status/category colours remain available. Home's existing scroll reveals use a slower easing curve; hero elements enter in sequence, the hero light drifts once, and cards and photos respond to hover. All new motion respects reduced motion. No additional client script or dependency is added. Locale typography, content, forms, navigation and SEO use the existing implementation.

The stylesheet uses the opt-in silver-* shared tokens introduced by the customer design. Deployment uses the existing main-branch Cloudflare Pages workflow; automatic CI remains paused as requested separately. Static token checks and whitespace validation are performed locally; production compilation is verified by the deployment workflow. Browser visual review remains pending.

## Editorial UX rebuild — 31 Oct 2026

The founder requested a structural redesign beyond palette changes. HomeView now starts with a split editorial introduction, a query-entry search and a product-preview panel built from the existing example catalogue. Search navigates to the configured customer app /search?q= URL; a no-script link preserves access, and the CSP stays intact. A proof strip and sticky chapter navigation lead into discovery, journey, nearby-shop exploration, cost comparison, weekly benefits, savings, retailer signup and FAQ. Every existing feature component, endpoint, lead-form source, translation and SEO input is retained. The shared frame has more spacious navigation, editorial page headings and a revised footer/signup layout. styles/editorial.css owns responsive structure and motion. Product examples retain freshness labels and the example notice. Motion respects the global reduced-motion policy.

Validation: all eleven homepage feature components remain present, CSS token references resolve and whitespace checks pass. Production build is delegated to the existing deployment workflow. Browser visual review remains pending.


## Independent premium homepage — 31 Oct 2026

The homepage now imports `premium.css` and `components/premium`, without bright.css or landing.css. It replaces the ticker and floating secondary navigation with a finite category directory, replaces the radar with a keyboard-operable shop list/detail explorer, and rebuilds product presentation, comparison disclosures, journey layout, calculator, FAQ and lead-form layout. Shared navigation uses a compact wordmark frame; the announcement bar is removed. Existing translations, example data, walking replay, filtering, savings math, lead endpoint and app URLs remain. Motion is bounded and respects reduced motion. The original bright components remain for other routes. Production build and live review are required for this change.
