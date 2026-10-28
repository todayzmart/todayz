# Business app

The business PWA with the offline POS: shops run their counter, stock, offers and customers.

| | |
|---|---|
| Local | https://business.todayz.localhost (Vite on port 5302, through Caddy) |
| Prod | https://business.todayz.in (Cloudflare Pages project `todayz-business`) |
| Package | `@todayz/business` |

## Run it

```bash
make dev                                   # whole stack (needs make setup once)
pnpm --filter @todayz/business dev               # just this app, against the mocks
pnpm --filter @todayz/business typecheck
pnpm --filter @todayz/business lint
pnpm --filter @todayz/business test              # Vitest
pnpm --filter @todayz/business e2e:smoke         # Playwright on the built preview
pnpm --filter @todayz/business exec playwright test -c playwright.stack.config.ts   # against make dev
```

Run `make gen` first on a fresh checkout: the API client and route tree are generated, not committed. The app imports only `shared/`, never a sibling app.

## Layout

Feature folders under `src/`:

| Folder | What |
|---|---|
| `pos`, `sync`, `receipt`, `scanner`, `restock-scan` | Offline counter, sync, bills, scanning |
| `stock`, `ai-drafts`, `voice`, `offers` | Stock and offers |
| `orders`, `customers`, `reviews`, `credits`, `asks` | Orders and customers |
| `overview`, `analytics`, `insights`, `coach`, `statements` | Numbers and payouts |
| `onboarding`, `stores`, `settings`, `standee`, `help`, `shell`, `routes`, `app`, `test` | Setup and app frame (B-02 is a step flow with a green rail; the shell sidebar is dark green, D-274) |

## Tech

React 19, Vite, TanStack Router and Query, `@todayz/ui`, `@todayz/api-client`, `@todayz/i18n`. Light theme only. Bundle budget in `size-limit`.

## More

- [Features](../docs/features/README.md)
- [Local development](../docs/guide/local-dev.md), [testing](../docs/guide/testing.md), [deployment](../docs/guide/deployment.md)
- [Working rules for this folder](CLAUDE.md)
