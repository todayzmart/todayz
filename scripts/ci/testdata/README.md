# scripts/ci/testdata

Seeded violations for `scripts/ci/selftest.sh` (run by `make check` through `check-ci`).
GitHub never runs these: it only reads `.github/workflows/`.

| Folder | Checked by | Rule |
|---|---|---|
| `workflows/bad-<rule>-*.yml` | `scripts/ci/wflint` | exit 1 and `[<rule>]`, with no unrelated noise tags |
| `workflows/good*.yml` | wflint | clean (`good-4space.yml` is the 4-space-indented variant) |
| `actions/bad-<rule>-*/action.yml`, `actions/good/` | wflint | composite actions |
| `dependabot/bad-*/`, `dependabot/good/` | wflint | `[dependabot]`: ecosystems, weekly, 14-day cooldown, every go.mod |
| `scripts/bad-forbidden-*`, `scripts/good-terraform.sh` | wflint | Terraform subcommands in scripts and make files |
| `actionlint/bad.yml` | actionlint | `[expression]` |
| `shellcheck/bad.sh` | shellcheck | SC2086 |
| `lighthouse/bad*`, `lighthouse/good*` | `lighthouse.sh lint` | D-58: results stay on the filesystem (configs and package.json scripts) |
| `gitleaks/loosened.toml` | `scan.sh gitleaks` | a pull request's own config cannot hide its committed `.env.local` |
| `terraform/module` | `terraform.sh static` | a minimal module for the static checks |

Every wflint rule tag has at least one fixture; the selftest fails if one is missing.
The selftest also builds scratch git repos (a committed `.env.local`, tracked keys and
build output, a shallow clone) and mutates temp copies of the real workflows; those
are generated at run time and never written here.

Each fixture's file name says the rule it proves (`bad-<rule>-<case>`). To add a case,
add a file; the selftest picks it up by name.
