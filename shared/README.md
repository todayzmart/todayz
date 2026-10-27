# Shared packages

Code every app uses. It becomes the versioned `todayz-shared` package when the repos split. Apps import from here and from nowhere else in the repo.

| Package | What | Docs |
|---|---|---|
| `tokens` (`@todayz/tokens`) | Design tokens, generated into CSS | |
| `ui` (`@todayz/ui`) | Components, Storybook, visual tests | [CLAUDE.md](ui/CLAUDE.md) |
| `api-client` (`@todayz/api-client`) | Typed client, Zod schemas, MSW handlers; generated from the contract and not committed | [CLAUDE.md](api-client/CLAUDE.md) |
| `i18n` (`@todayz/i18n`) | English, Hindi, Kannada, Telugu, Tamil and a pseudo-locale; message lint | [README](i18n/README.md) |
| `auth` (`@todayz/auth`) | Session core: sign-in, refresh, step-up | |
| `realtime` (`@todayz/realtime`) | Live updates over SSE with a polling fallback | [README](realtime/README.md) |
| `telemetry` (`@todayz/telemetry`) | Browser traces, errors and metrics | |
| `config` (`@todayz/config`) | Shared TypeScript, ESLint and Vitest presets | |

## Run

```bash
make gen                                   # generates api-client, tokens and catalogues
pnpm --filter './shared/*' test            # every package
pnpm --filter @todayz/ui test
pnpm --filter @todayz/ui storybook
```

Tests run as part of `make check`. Roughly 450 cases across the packages, `ui` the largest.

## Rules

- Change the API contract in `backend/api/`, never the generated client.
- No text in code: strings go through `@todayz/i18n`.
- Design comes from `design/`; tokens are not edited by hand.

## More

- [Features](../docs/features/README.md)
- [Testing](../docs/guide/testing.md), [local development](../docs/guide/local-dev.md)
- [Working rules for this folder](CLAUDE.md)
