# End-to-end, contract, load and security

Everything that tests TodayZ from the outside. Not shipped; it runs on your machine and in CI.

| Folder | What | Command |
|---|---|---|
| `journeys` | Cross-app Playwright journeys, 23 specs | `make e2e-journeys` |
| `a11y` | axe on every route, plus app-shell Lighthouse | `make e2e` (shells: `make lighthouse-apps`) |
| `visual` | Screenshot baselines | `make e2e` |
| `contract` | Schemathesis against the API; `run.sh smoke` or `full` | `make e2e` |
| `fixtures` | Seeded people, shops and admins, shared by specs | |
| `mocks` | Local stand-ins for SMS, WhatsApp, push, Turnstile, speech and the model API | [README](mocks/README.md) |
| `load` | k6 scenarios and a live-update fan-out driver | `make load` ([README](load/README.md)) |
| `security` | Abuse suite (ZAP and scans run from `make security`) | `make security` |
| `scenarios` | The scenario catalogue (`AI`, `ASK`, `BSK`, `FA`, `OB`, `UI`) | |

## Run

```bash
make reset && make e2e                     # clean data, then every browser and contract check
pnpm --filter @todayz/e2e-journeys exec playwright test specs/04-*.spec.ts
make load                                  # needs make load-data first
make security
```

Start from a fresh `make reset`: journeys create data and sign-in code sends count against a per-hour cap. `make e2e` runs without network access. The contract runner refuses any non-local URL (D-100). Reports never contain sign-in codes, tokens or phone numbers.

Workspace packages: `@todayz/e2e-fixtures`, `@todayz/e2e-journeys`, `@todayz/e2e-a11y`, `@todayz/e2e-visual`.

## More

- [Testing guide](../docs/guide/testing.md): layers, journeys, gates, how to add a test
- [Local development](../docs/guide/local-dev.md)
- [Working rules for this folder](CLAUDE.md)
