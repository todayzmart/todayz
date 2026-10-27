# Admin console

The admin console for staff. Seven RBAC roles; sign-in is a code by SMS alone, and a † action (money, settings, admin users, `money` jobs) asks for a fresh SMS code in the step-up dialog (D-264). Too many wrong sign-in codes lock a number's admin sign-in; another super admin unlocks it on A-18 Admin users.

| | |
|---|---|
| Local | https://admin.todayz.localhost (Vite on port 5303, through Caddy) |
| Prod | https://admin.todayz.in (Cloudflare Pages project `todayz-admin`) |
| Package | `@todayz/admin` |

## Run it

```bash
make dev                                   # whole stack (needs make setup once)
pnpm --filter @todayz/admin dev               # just this app, against the mocks
pnpm --filter @todayz/admin typecheck
pnpm --filter @todayz/admin lint
pnpm --filter @todayz/admin test              # Vitest
pnpm --filter @todayz/admin e2e:smoke         # Playwright on the built preview
pnpm --filter @todayz/admin exec playwright test -c playwright.stack.config.ts   # against make dev
```

Run `make gen` first on a fresh checkout: the API client and route tree are generated, not committed. The app imports only `shared/`, never a sibling app.

## Layout

Feature folders under `src/`:

| Folder | What |
|---|---|
| `pulse`, `demand`, `system`, `jobs`, `audit` | Health and history |
| `businesses`, `stores`, `customers`, `orders`, `reviews`, `risk` | Marketplace operations |
| `credits`, `payouts`, `referrals`, `banners`, `notifications` | Money and campaigns |
| `catalogue`, `field`, `support`, `ai-assist` | Catalogue, field agents, support |
| `admin-users`, `settings`, `shell`, `routes`, `app`, `test` | Staff, roles, sign-in unlock, `platform_settings` |

## Tech

React 19, Vite, TanStack Router and Query, `@todayz/ui`, `@todayz/api-client`, `@todayz/i18n`. Light theme only. Bundle budget in `size-limit`.

## More

- [Features](../docs/features/README.md)
- [Local development](../docs/guide/local-dev.md), [testing](../docs/guide/testing.md), [deployment](../docs/guide/deployment.md)
- [Working rules for this folder](CLAUDE.md)
