# W4-07 Ops readiness: QA report

Runbooks for every alert, a local backup/restore drill, the `docs/ops/` pages, and the
`make release-candidate` evidence pack. Branch `claude/ecstatic-kowalevski-c5802a`, with `main`
(403b64d6) merged in on 2026-10-26. Everything ran locally. Nothing touched AWS or Cloudflare.

## What was built

- **`check-runbooks`, a `make check` step** (`mk/60-ops.mk`, `scripts/ops/runbooks_check.py`, 15 unit tests). It fails when:
  - a Grafana alert in `infra/grafana/alerts/` has no `runbook_url`, or it points at a missing file;
  - a CloudWatch alarm that emails (`alarm_actions` on `local.alarm_actions`) names no runbook;
  - an alert runbook lacks one of the six sections, or does not name its alert uid or alarm in backticks;
  - a dashboard uid or a relative link in a runbook or `docs/ops/` page does not resolve;
  - a runbook is missing from `docs/runbooks/README.md`.

  The step also shellchecks `scripts/ops/` and runs the unit tests. It needs no network and no stack.
- **Runbooks** (`docs/runbooks/`). There are 32 alert runbooks, each with six sections: What it means, First five minutes, Diagnosis, Mitigation, Escalation (with a [severity level](../../ops/severity.md)), Follow-up.
  - The 26 existing alert runbooks are restructured into that shape. Their content is kept: "First three checks" became Diagnosis, "Fix" became Mitigation, "Who to tell" became Escalation. Each one gets a new First five minutes and Follow-up.
  - Six new runbooks cover the CloudWatch alarms that email: `rds-cpu`, `rds-storage`, `api-5xx`, `api-unhealthy`, `worker-unhealthy`, `job-queue-backlog`. `api-latency` now also covers `alb-latency-p95-high`.
  - `README.md` is a new index: Grafana alerts, CloudWatch alarms, job-kind runbooks, cloud setup.
- **Ops pages** (`docs/ops/`):
  - `severity.md`: SEV1–SEV4.
  - `on-call.md`: on-call basics.
  - `incident-template.md`: the incident template.
  - `release-checklist.md`: D-210, a push to `main` deploys prod. It covers before, push and watch, the first 30 minutes after, and rollback for Pages and Beanstalk.
  - `secret-rotation.md`: names only, and which keys cannot be rotated yet.
  - `backup-restore.md`: the local drill, a cloud PITR restore to a side instance, and the weekly cross-region copy.
  - `README.md`: an index of these pages.
- **Restore drill:** `make ops-restore-drill` runs `scripts/ops/restore-drill.sh` and `restore-drill.sql`. It is not part of `make check`.
  1. It opens a read-only exported snapshot on `todayz` (port 5433).
  2. It runs `pg_dump -Fc --snapshot` and the source reconcile on that same snapshot.
  3. It runs `pg_restore --exit-on-error` into the scratch database `todayz_restore_drill`.
  4. It reconciles the restored copy and compares it with the source. The checks are:
     - each table's row count and an order-free md5;
     - the `ledger_reconcile` invariants and the sum of ledger entries;
     - money sums;
     - partitions;
     - goose and River versions;
     - schema object counts.
  5. It writes `docs/qa/ops/restore-drill-<date>.md`, drops the scratch database, and exits 1 on any mismatch.

  It never prints a URL or password, and it changes nothing in `todayz`.

## Evidence

| Command | Result |
|---|---|
| `python3 -I scripts/ops/runbooks_check.py .` | `ok: runbooks (32 Grafana alerts, 10 CloudWatch alarms, 32 alert runbooks)` |
| `make check-runbooks` (after the merge of `main`) | shellcheck clean, 15 tests OK, runbooks ok |
| Checker sensitivity (unit tests) | fails on a missing runbook, a bad `runbook_url`, a missing section, an alert not named, a bad dashboard uid, a bad relative link, a paging alarm with no runbook, and a runbook missing from the index |
| `scripts/test/makefile-targets.sh` | `ok: canonical make targets (14)` |
| `make check-repo` | exit 0 (shellcheck, envtool tests, SC-PL-03/04, git hooks) |
| `make check-tf-static` (I touched `stack/main.tf`) | exit 0; checkov 374 passed, 0 failed |
| `terraform fmt -check` on `stack/main.tf` | clean |
| `make ops-restore-drill` (first run at `5beb1ed6`, again at `2af8d501` after the reseed) | **PASS** both times. Last run: 138 of 138 tables match, 14608 rows; dump 0.2 s, restore 1.2 s; ledger invariants 0; goose 31601 (53 applied), River 8. Evidence: [restore-drill-2026-10-26.md](../ops/restore-drill-2026-10-26.md) |
| `make release-candidate` (once, from this worktree, 05:46–05:47 IST) | Target works: it ran every step, wrote [rc-2026-10-26/](../rc-2026-10-26/summary.md) and a log per step. Steps: reset FAIL, check FAIL, e2e FAIL, load pass (no steps), security FAIL. See below |
| `envtool leakscan` on the rc logs and drill evidence, against the main checkout's `.env.local` | exit 0, no findings |

There is no `make lint` target in the repo. For lint I ran the steps that cover the files I
touched: `check-runbooks`, `check-repo`, `check-tf-static` and `terraform fmt`.

## `make release-candidate` (evidence pack)

It ran once, from this worktree at `2af8d501`, after the lead's `make e2e` had finished and the
stack was down. The target itself is not broken, so I did not change it (it lives in the root
Makefile). Each failure, with its output:

| Step | Result | Cause | Owner |
|---|---|---|---|
| `make reset` | FAIL | `reset-db` passed (databases recreated). `reset-backend` → `gen-backend`: `/bin/bash: sqlc: command not found` (exit 127) | Worktree setup: this worktree's `.tools/bin` has only `envtool`. Not a code bug |
| `make check` | FAIL | Same: `make gen` stops at `gen-backend`, `sqlc: command not found` | Worktree setup |
| `make e2e` | FAIL | Same: `make gen` stops at `gen-backend`, `sqlc: command not found`. No stack step ran | Worktree setup |
| `make load` | pass | `make load: no steps registered yet`. The pass means nothing | k6 owner: no load step is registered |
| `make security` | FAIL | gitleaks: history clean (753 commits). **trivy**: `pnpm-lock.yaml`: `zod` 4.6.5, CVE-2023-54404, HIGH, fixed in 4.6.6 ("Denial of service via unbounded array validation"). checkov and ZAP did not run (the step stops at the first failure) | **Real finding.** The owner of the frontends' `zod` dependency. Not fixed here (no dependency changes, D-88) |

To see how far the worktree setup goes, I ran `make gen` once more with the main checkout's
`.tools/bin` on `PATH`, without rebuilding anything. `gen-backend` then passes. `gen-contract`
fails next: `bundle.mjs: Cannot find module '@redocly/cli/package.json'` ("node_modules
missing"). After that, the worktree also has no `.env.local`. So a real end-to-end rc needs a
fully set-up checkout. A second full run (setup, install, full stack) is outside "run it once" and
the RAM rule, so I stopped there.

**Recommendation:** after the merge, the lead runs `make release-candidate` once from the main
checkout, which has the tools, `node_modules` and `.env.local`. Bump `zod` to 4.6.6 first, or the
security step stays red.

## Files changed outside `docs/runbooks/` and `docs/ops/`

- `mk/60-ops.mk` (new): my own mk file.
- `scripts/ops/` (new): the checker, its tests, and the drill script and SQL.
- `docs/qa/ops/`, `docs/qa/rc-<date>/`, `docs/qa/W4-07/`: evidence.
- `infra/terraform/modules/stack/main.tf`, one line. The `worker-oldest-job-age-high` alarm description now ends `Runbook: docs/runbooks/job-queue-backlog.md.`, like the other alarms. It is a description change only; no resource is replaced.

## Conservative readings

1. **Runbook shape.** I used the brief's six sections. 05 §5 (line 259) still lists the old five ("first 3 checks, likely causes, fix, who to tell"). I did not edit 05; see question 1.
2. **Which CloudWatch alarms need a runbook.** Only alarms that email someone, meaning they have `alarm_actions` on `local.alarm_actions`. `worker-oldest-job-age-low` only scales in. It has no runbook, and the index says so.
3. **Shared local stack.** It was started from the main checkout by another session. The drill only reads `todayz` and creates and drops its own scratch database. I did not run `make reset` for the drill.
4. **`make release-candidate` and the shared stack.** The rc runs `make reset`, and its `e2e-stack` step runs `make dev` and then `make stop`, which stops the shared stack. I waited until no other `make e2e`, `reset`, `dev`, `load`, `security` or `release-candidate` run was active before starting it. The lead's `make e2e` was running at the time.
5. **`make stop`.** The lead's e2e had already stopped the stack. My rc's `reset-db` started Postgres again, so I ran `make stop` at the end. All compose containers are stopped (volumes kept). `todayz-testpg` (port 5439, opt-in) was already up and is not mine, so I left it.
6. **The rc target is in the root Makefile**, which nobody edits (D-56). It was not broken, so I left it.
7. **The shared database after my rc.** The rc's `reset-db` dropped `todayz` and `todayz_test`. Then `reset-backend` failed (no `sqlc`), so `todayz` was left empty. I put it back with the main checkout's existing binary, built at 04:32 after the last backend commit at 04:27, and its env loader: `todayz migrate up` then `todayz seed dev`. Both exit 0; now at goose 31601, with 26 stores. Nothing was printed and nothing rebuilt. `todayz_test` stays empty, as after any `make reset`.
8. **The common brief says workers do not run `make reset` or `make e2e`.** The W4-07 brief asks for one rc run, so I ran it once, and only when no other stack run was active.

## Open points

1. **Money in the drill data.** The local database had just been reset by another session, so the money sums in the drill evidence are all 0. The invariants and row checksums still compare every row. I ran it again after the reseed; still 0, because `seed dev` writes no ledger postings. The money columns are compared, but on zero values. A drill after `make demo` or e2e journeys would test non-zero sums.
2. **Rotation of the sealed keys.** `ORDER_CODE_ENC_KEY`, `PII_ENC_KEY`, `DEVICE_SECRET_ENC_KEY` and `TOTP_ENC_KEY` have no key version and no re-encrypt job. A rotation makes stored rows unreadable. `secret-rotation.md` says "do not rotate" and how to handle a leak. The re-encrypt job is a follow-up for the identity and orders owners.
3. **Certificate and uptime alert (05 §5, cloud).** It is planned with Grafana Synthetic Monitoring and does not exist yet. When it is added, it needs a runbook, and `check-runbooks` will ask for one.
4. **The cloud restore drill (W5-04).** It is written down in `backup-restore.md` and waits for the AWS account.

## Questions for the lead

1. Will you run `make release-candidate` from the main checkout after the merge (see above), and who bumps `zod`?
2. Should 05 §5 line 259 change to the six sections, with a decision-log entry? I left 05 alone because it is not in my paths.
3. Should `worker-oldest-job-age-low` keep no runbook? It pages nobody.

## Not verified

- **Every cloud step in the runbooks and in `docs/ops/`.** That covers the `aws` CLI lines, console paths, Beanstalk and Pages rollback, PITR timing, and the AWS Backup copy. They are written from the Terraform in `infra/terraform/` and from 09. None was run (no cloud calls).
- **Grafana Cloud links.** The dashboard links point at the local `grafana.todayz.localhost`. The checker checks the uids against `infra/grafana/dashboards/`, not a live Grafana.
- **The drill on a large database.** The local data is small (31 MB), so the timings say nothing about prod.
