# Instructions for AI agents

This file is for any AI coding agent working in this repo: Claude Code, Codex, Cursor, Gemini, Copilot or others.

**The rules are in [CLAUDE.md](CLAUDE.md).** They bind every agent, whatever its name. Each folder also has its own `CLAUDE.md` with rules for that folder. Read the root one, then the one in the folder you change.

## Start here

1. [README.md](README.md): what TodayZ is, the apps, how to run it.
2. [docs/README.md](docs/README.md): the map of every doc.
3. [docs/guide/architecture.md](docs/guide/architecture.md) and [docs/guide/code-structure.md](docs/guide/code-structure.md): how the system is built and where the code lives.
4. [docs/features/README.md](docs/features/README.md), then the feature file for your area.
5. [docs/plan/01-product-rules.md](docs/plan/01-product-rules.md): every product rule and number.
6. [docs/guide/working-on-todayz.md](docs/guide/working-on-todayz.md): the workflow, the Definition of done and the docs rule.

## The rules most often missed

- **Update the docs in the same change as the code.** This covers the feature doc, the guides and diagrams, the folder README and CLAUDE.md, the root README, and the decision log. `make check` fails when the docs fall behind the code. The commit-msg hook refuses a code commit with no doc change unless the message says `Docs: none (<reason>)`.
- Never print, log or commit secret values, OTPs, phone numbers or tokens. Refer to env vars by name only.
- Generated code is never committed. Run `make gen` before you build or test.
- Database changes only through new migration files.
- Configurable numbers come from `platform_settings`, never from hard-coded values.
- AI output is always a draft that a person confirms.
- Only the lead pushes. A push to `main` deploys to prod.
- Ask before you guess on money, codes, timers, permissions or user-visible behaviour.
