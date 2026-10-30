# Customer app

The customer PWA: search what is nearby, book it, then pick it up or get it delivered.

| | |
|---|---|
| Local | https://app.todayz.localhost (Vite on port 5301, through Caddy) |
| Prod | https://app.todayz.in (Cloudflare Pages project `todayz-customer`) |
| Package | `@todayz/customer` |

## Run it

```bash
make dev                                   # whole stack (needs make setup once)
pnpm --filter @todayz/customer dev               # just this app, against the mocks
pnpm --filter @todayz/customer typecheck
pnpm --filter @todayz/customer lint
pnpm --filter @todayz/customer test              # Vitest
pnpm --filter @todayz/customer e2e:smoke         # Playwright on the built preview
pnpm --filter @todayz/customer exec playwright test -c playwright.stack.config.ts   # against make dev
```

Run `make gen` first on a fresh checkout: the API client and route tree are generated, not committed. The app imports only `shared/`, never a sibling app.

## Layout

Feature folders under `src/`:

| Folder | What |
|---|---|
| `home`, `search`, `search-assist`, `voice-search`, `category`, `map` | Find what is nearby |
| `store`, `product`, `substitutes`, `share` | Shop and item pages |
| `cart`, `basket`, `checkout`, `orders`, `receipt` | Book, pay, pick up or delivery |
| `list-parse`, `ai`, `asks`, `scan` | AI drafts, ask shops, scan standees |
| `account`, `address`, `help`, `pwa`, `shell`, `routes`, `app`, `test` | Account, install, app frame, tests |

## Tech

React 19, Vite, TanStack Router and Query, `@todayz/ui`, `@todayz/api-client`, `@todayz/i18n`. Light theme only; discovery screens use quick-commerce patterns on TodayZ tokens (D-269, see [CLAUDE.md](CLAUDE.md)). Bundle budget in `size-limit`.

## More

- [Features](../docs/features/README.md)
- [Local development](../docs/guide/local-dev.md), [testing](../docs/guide/testing.md), [deployment](../docs/guide/deployment.md)
- [Working rules for this folder](CLAUDE.md)

## Liquid Silver customer design (30 Oct 2026)

As built on base commit 7e831f7d; Liquid Silver change in this commit. The customer app now uses a light silver page, graphite actions and text, white product cards, and translucent search and floating navigation. The customer theme maps shared semantic tokens to opt-in `silver-*` palette tokens; other applications retain their palettes. Home has silver category wells, more section spacing and staggered entrance animation; product cards lift and images gently scale on hover. Buttons respond to presses and the cart bar animates into view. Motion is enabled only with `prefers-reduced-motion: no-preference`; navigation transitions are disabled for reduced motion. Glass surfaces have an opaque-enough fallback background when backdrop blur is unavailable. Product photography, translations and shopping flows use the existing implementation.

Palette values live in `design/source/tokens.json`; presentation lives in `customer/src/app/theme.css` and the Home, shell and product tile CSS modules. The token generator output stays untracked. The floating nav retains safe-area padding and the cart bar remains above it.
