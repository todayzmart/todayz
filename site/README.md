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
