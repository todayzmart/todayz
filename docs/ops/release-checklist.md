# Release checklist

D-210: there is no staging. **A push or merge to `main` deploys to prod.** The four Cloudflare Pages
sites deploy from `.github/workflows/deploy.yml` (D-214). The API and worker deploy to Elastic
Beanstalk from their own workflow once the AWS account is open (W5-01). Only the lead pushes.

This list is the gate. If a box cannot be ticked, do not push.

## Before the push

- [ ] `make check` passed on the exact commit you will push (not an earlier one). It runs offline (SC-PL-04).
- [ ] For a change to a user flow: `make e2e` passed locally, or the change has its own e2e spec that passed.
- [ ] The branch is up to date with `main` and the merge is clean.
- [ ] Migrations, if any:
  - [ ] Expand only (new tables, new nullable columns, new indexes created concurrently). The current prod app must keep working on the new schema (09 §7).
  - [ ] No drop, rename or type change in the same release as the code that stops using it. That is a later "contract" release.
  - [ ] Ran on a copy of local data: `make reset`, then the app smoke.
- [ ] New `platform_settings` keys have defaults that are safe in prod. Risky features ship behind their flag, off (`feature_*_enabled`, `ai_<feature>_enabled`).
- [ ] No secret in the diff. `make check` runs gitleaks; also read the diff yourself.
- [ ] New env var names are in `.env.example` and, for prod, in Secrets Manager (`todayz/prod/<NAME>`) **before** the deploy. `make env-check` shows names only.
- [ ] Not on a Friday evening, not during a festival peak, not 18:00–21:00 IST (evening order peak), unless it is a fix.
- [ ] Someone can watch for 30 minutes after the push.
- [ ] Write the release note: commit range, what changes for users, migrations, flags, how to roll back.

## Push and watch

- [ ] Push to `main` (the lead). Note the commit sha.
- [ ] GitHub Actions: `ci`, `security` and `deploy` go green. A red deploy job means prod did not change for that part; read the log before retrying.
- [ ] Beanstalk (when live): api deploys with traffic splitting (10 % for 10 minutes, auto rollback on failed health). Worker deploys immutably. Do not start a second deploy while one runs.

## After the push (first 30 minutes)

- [ ] `https://api.todayz.in/readyz` answers 200 and the version matches the sha (A-22 System shows it too).
- [ ] Each site loads: todayz.in, app., business., admin.todayz.in. Hard reload; check the console has no errors.
- [ ] Smoke the core loop with the founders' test accounts: sign in, search nearby, book, shop accepts, pickup code works.
- [ ] Dashboard [00 · What to fix today](https://grafana.todayz.localhost/d/todayz-00) and [01 · Service overview](https://grafana.todayz.localhost/d/todayz-01) (Grafana Cloud in prod): 5xx and p95 flat against the hour before.
- [ ] [15 · Jobs](https://grafana.todayz.localhost/d/tz-jobs): no kind *Failing*; timers not late.
- [ ] [05 · Customer RUM](https://grafana.todayz.localhost/d/todayz-05): JS error rate of the new version not above the old one.
- [ ] No new alert fired. If one did, open its runbook before anything else.

## Rollback

Roll back first, debug after. A rollback is the right call when a release causes a SEV1 or SEV2
([Severity levels](severity.md)) and the fix is not one obvious line.

### Cloudflare Pages (site, customer, business, admin)

Each site is its own project: `todayz-site`, `todayz-customer`, `todayz-business`, `todayz-admin`.

1. Cloudflare dashboard → Workers & Pages → the project → Deployments.
2. Find the last good production deployment (by commit sha and time).
3. Its menu → **Rollback to this deployment**. It is live in seconds; no build runs.
4. The next push to `main` deploys again. Revert the bad commit on `main` before that push, or the bad build comes back.

API route for the same thing (lead's token in `.env.cloud.local`, never printed):
`POST /accounts/{CLOUDFLARE_ACCOUNT_ID}/pages/projects/{project}/deployments/{deployment_id}/rollback`.

Old browser tabs may still hold the bad bundle. The service worker prompts for an update; chunk-load
errors fade as tabs reload ([Client errors](../runbooks/client-errors.md)).

### Elastic Beanstalk (api and worker)

Application `todayz-prod`; environments `todayz-prod-api` and `todayz-prod-worker`.

1. Find the previous version label:
   `aws elasticbeanstalk describe-environments --environment-names todayz-prod-api` (current label) and
   `aws elasticbeanstalk describe-application-versions --application-name todayz-prod --max-records 5`.
2. Deploy it: `aws elasticbeanstalk update-environment --environment-name todayz-prod-api --version-label <previous>`.
   Do the same for `todayz-prod-worker` if the worker changed. Console: Beanstalk → Application versions → select → Deploy.
3. Watch `describe-environment-health --attribute-names All`. The api drains old instances behind the
   load balancer (D-98); no request is cut. The worker hands periodic jobs over through River leader election.
4. Revert the bad commit on `main` so the next push does not redeploy it.

### Database

- Migrations are forward-only. Never run a down migration in prod.
- Because releases are expand/contract, the previous app version runs on the new schema. Rolling back the app is enough.
- If data is wrong, do not edit rows by hand. Restore to a side instance from PITR and copy back only the affected rows ([Backup and restore](backup-restore.md)). Money fixes are `admin_adjustment` postings by two people.

### Without a deploy

Turn a risky feature off in `platform_settings` (A-17, audited, D-25): `feature_delivery_enabled`,
`feature_bill_qr_enabled`, `feature_voice_enabled`, `feature_offers_enabled`, `feature_banners_enabled`,
one `ai_<feature>_enabled` per AI feature, or `ai_daily_cost_cap_paise` to stop AI calls for the day.

## After a rollback

- [ ] Open an incident note if it was SEV1 or SEV2 ([template](incident-template.md)).
- [ ] Revert on `main`, with the reason in the commit message.
- [ ] Add the test that would have caught it before the fix goes out again.
