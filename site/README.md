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
