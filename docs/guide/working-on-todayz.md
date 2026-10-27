# Working on TodayZ

How to make a change, from first read to merge, and how to keep the docs true. This guide is for people and AI agents alike.

> As built on main at 2c2f23d6 (27 Oct 2026). Update this file in the same change as the code it describes.

## 1. Before you change anything

1. Read the root [CLAUDE.md](../../CLAUDE.md) for the rules, then the `CLAUDE.md` and `README.md` in the folder you will change.
2. Find your area:
   - [docs/features/README.md](../features/README.md) says which feature doc covers it;
   - [code-structure.md](code-structure.md) says where the code lives.
3. Check the rule and the number in [01-product-rules.md](../plan/01-product-rules.md), and the reason in the [decision log](../plan/12-decision-log.md).
4. Ask the lead (or the founder) when a rule about money, codes, timers, permissions or user-visible behaviour is unclear. Do not guess.

## 2. Making the change

| Change | Where | Then |
|---|---|---|
| API endpoint or schema | `backend/api/paths/<domain>.yaml`, `backend/api/schemas/` | `make gen` (bundles OpenAPI, generates the Go server, the TS client and sqlc) |
| Database | a new file in `backend/db/migrations/` (never edit a merged one) | `make reset` locally |
| SQL query | `backend/db/queries/` | `make gen` |
| Backend logic | `backend/internal/<domain>/` | Go tests next to the code |
| Screen | `<app>/src/<feature>/` and `<app>/src/routes/` | vitest; Playwright journey if it is a user flow |
| Text users see | i18n keys in `shared/i18n` (English always; other languages may be pending) | `make check` runs the i18n checks |
| Product number | a `platform_settings` key, never a constant | add the key to 01-product-rules.md |
| New decision | a new `D-nnn` entry in [12-decision-log.md](../plan/12-decision-log.md) | update the plan file it changes |

Generated code (`backend/gen`, `shared/api-client`, the bundled `openapi.yaml`, `routeTree.gen.ts`) is never committed.

## 3. Keep the docs true

Every change updates the docs it makes stale, in the same commit. This rule applies to every AI agent and every person.

| If you changed… | Update |
|---|---|
| What a user can do, a rule, a screen, an endpoint, a table, a job | the feature file in [docs/features/](../features/README.md): its *What users can do*, *Rules*, *Code*, *Tests*, *Status* and *Decisions* sections |
| A new feature area | a new `docs/features/<area>.md` from the same template, plus a row in the matrix in [docs/features/README.md](../features/README.md) |
| How parts talk to each other, a flow, a state machine | [architecture.md](architecture.md) or [flows.md](flows.md), including the Mermaid diagram |
| A new package, folder, mk file or script | [code-structure.md](code-structure.md) and the folder's `README.md` |
| Tests, test commands, gates | [testing.md](testing.md) |
| CI workflows, deploy scripts, infra, env var names | [deployment.md](deployment.md) and [local-dev.md](local-dev.md) |
| The apps, the commands, or what is live | the root [README.md](../../README.md) |
| A rule for agents in a folder | that folder's `CLAUDE.md` |

When you touch a doc, update its `As built on main at <commit>` line. Use the commit your change is based on.

### What enforces it

- **`make check` runs `check-docs`** ([mk/62-docs.mk](../../mk/62-docs.mk), [scripts/docs/docs_check.py](../../scripts/docs/docs_check.py)). It fails when:
  - a required doc is missing;
  - a relative link is broken;
  - a `backend/internal` package, `mk/` file, GitHub workflow or app feature folder is not named in the docs;
  - a guide or feature doc has lost its `As built` line.
- **The commit-msg hook** ([scripts/hooks/docs-gate.sh](../../scripts/hooks/docs-gate.sh), installed by `make setup`) refuses a commit that changes code without changing any `.md` file. When no doc really needs to change, for example a pure refactor or a test-only fix, add this line to the commit message:

  ```
  Docs: none (refactor only, behaviour unchanged)
  ```

The checks catch missing names and broken links. They cannot tell whether a description is still accurate; that is your job. A reviewer treats a stale doc as a bug.

## 4. Definition of done

A change is done when every item in [docs/plan/README.md](../plan/README.md#definition-of-done-every-wp) holds, in short:

1. `make check` passes at the repo root, including `check-docs`.
2. New behaviour has tests at the right levels. Money, codes, timers and permissions have table-driven tests for every branch.
3. The UI uses design tokens and shared components, and all text goes through i18n.
4. No secret, OTP, phone number or token appears in code, logs, snapshots or output.
5. The docs above are updated in the same change.
6. Risky changes (money, auth, secrets, data migrations, deploys) have had a review.

## 5. Commits, branches and deploys

- Workers commit on their own branch or worktree and never push. Only the lead merges into `main` and pushes, because **a push to `main` deploys to prod** (D-210). See [deployment.md](deployment.md).
- Commit messages say what changed and why. Cite the work package or decision (`W3-12`, `D-259`) when there is one.
- Lowering a quality gate (a Lighthouse budget, a coverage bar, a skipped test) needs the founder's own yes, recorded in the decision log.
