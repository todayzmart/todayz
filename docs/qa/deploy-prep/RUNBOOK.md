# Prod first deploy: runbook

For the lead, who runs every command here with `.env.cloud.local` loaded. The founder supplies the inputs in §2 and gives an explicit yes on the plan summary before the first prod apply (D-210).

Nothing in this branch has been planned or applied. It was checked offline only:
- `scripts/ci/terraform.sh static` for `infra/terraform` and `infra/cloudflare`: fmt, validate, tflint, checkov (471 passed, 0 failed, 102 skipped);
- `checks/check.py all`;
- wflint;
- shellcheck.

Decisions: D-254 (two applies, the release pipeline), D-255 (state, account pin), D-256 (secrets), D-257 (Cloudflare imports), D-258 (cost).

| | |
|---|---|
| AWS account | **061292478202**. Every provider has `allowed_account_ids`, so the old 212412893706 is refused |
| Regions | ap-south-1, with DR copies in ap-southeast-1 |
| Repo | `todayzmart/todayz`. A push to `main` deploys (Pages: `deploy.yml`; backend: `deploy-backend.yml`, off until step 13) |
| State | `s3://todayz-tfstate-061292478202`, keys `account/`, `prod/`, `cloudflare/terraform.tfstate`, S3 native lockfile |

## 1. Shell setup (every session)

```bash
set -a; . ./.env.cloud.local; set +a   # never echo these; `make env-check` prints set/missing only
export AWS_REGION=ap-south-1 TODAYZ_WAVE5=1
TF=scripts/ci/terraform.sh             # refuses plan/apply/output unless TODAYZ_WAVE5=1
mkdir -p .cache/deploy
```

- Local `terraform` (1.17) is needed. The Docker fallback in `terraform.sh` does not pass `TF_VAR_*` through.
- Variables go in as `TF_VAR_*` exports (table in §3), never as `*.tfvars` files in the repo, because `*.tfvars` is not git-ignored.
- Scratch files go under `.cache/deploy/`, which is git-ignored.

Init each root once, with the state backend:

```bash
for root in envs/account:account envs/prod:prod; do
  $TF -chdir=infra/terraform/${root%%:*} init -backend-config="bucket=todayz-tfstate-061292478202" \
    -backend-config="key=${root##*:}/terraform.tfstate" -backend-config="region=ap-south-1" \
    -backend-config="use_lockfile=true" -backend-config="encrypt=true"
done
$TF -chdir=infra/cloudflare init -backend-config="bucket=todayz-tfstate-061292478202" \
  -backend-config="key=cloudflare/terraform.tfstate" -backend-config="region=ap-south-1" \
  -backend-config="use_lockfile=true" -backend-config="encrypt=true"
```

## 2. What the founder supplies

Values go into `.env.cloud.local` by name and are never pasted in chat. Items marked **blocking** stop the first deploy. The others only hold back a feature.

| # | What | Where it goes | Blocking? |
|---|---|---|---|
| F1 | **Billing currency.** INR if the account is AWS India (AISPL, Indian card); USD otherwise | `TF_VAR_budget_limit_unit` = `INR` with amount `30000`, or `USD` with `350` (09 §6, D-258) | yes |
| F2 | **Budget and alarm emails**, then click "Confirm subscription" in both AWS mails (Budgets, and SNS `todayz-prod-alarms`) | `BUDGET_ALERT_EMAIL`; alarms use the same address unless `ALARM_EMAIL` is added | yes |
| F3 | **Yes on the prod plan summary** (resources, the cost in §6, the domain cut-over) | chat | yes |
| F4 | **GitHub `production` environment** with the founder as required reviewer. The lead can create it with `GH_TOKEN`; the founder picks the reviewers | GitHub settings | before step 13 |
| F5 | **DLT** (TRAI, 2–7 days): sender ID, entity ID, the OTP template, and the notice templates (D-212, D-224) | `SMS_SENDER_ID`, `SMS_DLT_ENTITY_ID`, `SMS_OTP_TEMPLATE_ID`, `SMS_NOTICE_TEMPLATE_IDS` | real phone logins only |
| F6 | **AWS End User Messaging SMS** in ap-south-1 (`aws-founder-steps.md` part C3): register the sender ID with the DLT ids, **request production access (sandbox exit)**, and **raise the monthly SMS spend limit** from the default | AWS console | real phone logins only |
| F7 | **SES production access**. Today the app sends no email (there is no email provider in `config.go`), so this waits until an email notice ships | AWS console | no |
| F8 | **R2 API token** (Object Read & Write on the three buckets) | `secrets.sh put R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` | image uploads |
| F9 | **Provider keys**: Sentry DSN, Grafana OTLP endpoint and headers, Anthropic and Sarvam (AI), later WhatsApp and the geocoder | `secrets.sh put NAME`; the OTLP endpoint as `TF_VAR_otel_exporter_otlp_endpoint` | no |
| F10 | **AI live or mock at launch.** `MOCK_LLM` defaults to true. Live AI needs F9's keys and `TF_VAR_extra_properties='{"MOCK_LLM":"false"}'` | founder decision | no |
| F11 | **Budget vs estimate.** Prod is about ₹24k a month before GST and about ₹28k with it, so the 80% alert (₹24k) will likely fire every month. Keep ₹30k or raise it | founder decision (money; not changed here) | no |

## 3. Variables and where each comes from

| Root | Variable | Source |
|---|---|---|
| account | `aws_account_id` | default `061292478202` |
| account | `budget_alert_emails` | `export TF_VAR_budget_alert_emails="[\"$BUDGET_ALERT_EMAIL\"]"` (F2) |
| account | `budget_limit_unit`, `monthly_budget_amount` | F1 (defaults INR / 30000) |
| prod | `ecr_repository_arn`, `ecr_repository_url` | `envs/account` outputs (step 2) |
| prod | `github_oidc_provider_arn` | `envs/account` output `github_oidc_provider_arn` |
| prod | `initial_image_tag` | `cat .cache/backend/tag` (step 3) |
| prod | `app_environments_enabled` | `false` on apply #1, `true` from apply #2 on (D-254) |
| prod | `alarm_email` | `$BUDGET_ALERT_EMAIL` or `$ALARM_EMAIL` (F2) |
| prod | `sms_sender_id`, `sms_dlt_entity_id`, `sms_otp_template_id`, `sms_notice_template_ids` | `SMS_*` (F5). They can be empty at first: the app starts, and only SMS sends fail |
| prod | `provider_secret_names` | JSON list of the provider secrets `secrets.sh status` shows as set, e.g. `'["TURNSTILE_SECRET_KEY","SENTRY_DSN"]'` (D-256) |
| prod | `r2_account_id` | `$CLOUDFLARE_ACCOUNT_ID` (R2 lives in the same account) |
| prod | `otel_exporter_otlp_endpoint` | `$OTEL_EXPORTER_OTLP_ENDPOINT` (F9) |
| prod | `turnstile_site_key` | default `0x4AAAAAAFj6Q6j9e1vnXIkj` (the hand-made `todayz-prod` widget, D-214) |
| prod | `vapid_public_key` | empty; web push is a later slice |
| prod | `extra_properties` | `{}` or F10 |
| prod | `api_hostname`, `region`, `dr_region`, `github_repository` | defaults: `api.todayz.in`, ap-south-1, ap-southeast-1, `todayzmart/todayz` |
| cloudflare | `account_id`, `zone_id` | `$CLOUDFLARE_ACCOUNT_ID`, `$CLOUDFLARE_ZONE_ID` (the token comes from `CLOUDFLARE_API_TOKEN`) |
| cloudflare | `import_existing_pages`, `import_r2_buckets` | `true` on the first Cloudflare apply only |
| cloudflare | `import_turnstile_sitekey` | `0x4AAAAAAFj6Q6j9e1vnXIkj` on the first apply only |
| cloudflare | `import_dns_record_ids` | map of `site`/`www`/`customer`/`business`/`admin` to the existing record ids. List them with `wrangler` or the dashboard; the lead looks them up in step 5 |
| cloudflare | `acm_validation_records` | prod output `certificate_validation_records` (step 5) |
| cloudflare | `api_origin_hostname` | empty until step 11, then prod output `api_origin_hostname` |

## 4. First deploy, in order

Each `plan` is reviewed before its `apply`. For prod, the summary goes to the founder (F3) first.

1. **State bucket:** `scripts/deploy/state-bucket.sh`. Then run the inits in §1.
2. **Account root:** budget, ECR repository, GitHub OIDC provider.
   ```bash
   $TF -chdir=infra/terraform/envs/account plan -out=../../../../.cache/deploy/account.plan
   $TF -chdir=infra/terraform/envs/account apply ../../../../.cache/deploy/account.plan
   export TF_VAR_ecr_repository_arn="$($TF -chdir=infra/terraform/envs/account output -raw ecr_repository_arn)"
   export TF_VAR_ecr_repository_url="$($TF -chdir=infra/terraform/envs/account output -raw ecr_repository_url)"
   export TF_VAR_github_oidc_provider_arn="$($TF -chdir=infra/terraform/envs/account output -raw github_oidc_provider_arn)"
   ```
   Confirm the budget email subscription (F2).
3. **First image:** `scripts/deploy/beanstalk.sh build && scripts/deploy/beanstalk.sh push`. Needs Docker with buildx, about 5 minutes. Then `export TF_VAR_initial_image_tag="$(cat .cache/backend/tag)"`.
4. **Prod apply #1** with `export TF_VAR_app_environments_enabled=false`. It creates the network, RDS, empty secrets, ops tasks, alarms and the ACM request.
   - The plan should show only creates, and no Beanstalk environments.
   - The RDS create takes about 15 minutes.
   ```bash
   $TF -chdir=infra/terraform/envs/prod plan -out=../../../../.cache/deploy/prod-1.plan   # summary → founder → yes
   $TF -chdir=infra/terraform/envs/prod apply ../../../../.cache/deploy/prod-1.plan
   ```
5. **Cloudflare apply #1:** imports, ACM validation and `www`, but no `api` yet.
   ```bash
   $TF -chdir=infra/terraform/envs/prod output -json certificate_validation_records > .cache/deploy/acm.json
   export TF_VAR_acm_validation_records="$(cat .cache/deploy/acm.json)"
   export TF_VAR_import_existing_pages=true TF_VAR_import_r2_buckets=true \
     TF_VAR_import_turnstile_sitekey=0x4AAAAAAFj6Q6j9e1vnXIkj TF_VAR_import_dns_record_ids='{...}'
   $TF -chdir=infra/cloudflare plan -out=../../.cache/deploy/cf-1.plan
   $TF -chdir=infra/cloudflare apply ../../.cache/deploy/cf-1.plan
   ```
   - The plan must show **imports** for the Pages projects, domains, records, R2 buckets and Turnstile, not creates or replaces.
   - The one expected replacement is the apex A record, which becomes the flattened Pages CNAME (D-257).
   - Unset the `import_*` variables afterwards; the import blocks are no-ops once the resources are in state.
6. **Secrets** (D-256). The scripts print names only.
   ```bash
   scripts/deploy/secrets.sh app-keys
   scripts/deploy/secrets.sh database
   $TF -chdir=infra/cloudflare output -raw turnstile_secret | scripts/deploy/secrets.sh put TURNSTILE_SECRET_KEY
   scripts/deploy/secrets.sh put R2_ACCESS_KEY_ID; scripts/deploy/secrets.sh put R2_SECRET_ACCESS_KEY   # typed, not echoed (F8)
   scripts/deploy/secrets.sh put SENTRY_DSN                                                            # and the other F9 keys present
   scripts/deploy/secrets.sh share-preview-to-pages      # SHARE_PREVIEW_KEY on the todayz-customer Pages project (go-live C7)
   scripts/deploy/secrets.sh status
   ```
7. **Database bootstrap:** `scripts/deploy/beanstalk.sh db-bootstrap`. This Fargate task creates the roles with the passwords from step 6, sets `REVOKE TEMPORARY`, the extensions and the default privileges. It is safe to repeat.
8. **Migrate:** `scripts/deploy/beanstalk.sh migrate`. The task's log is printed, and a non-zero exit stops here.
9. **Application versions:** `scripts/deploy/beanstalk.sh release`. It creates `<tag>-api` and `<tag>-worker`.
10. **Prod apply #2:** `export TF_VAR_app_environments_enabled=true TF_VAR_provider_secret_names='[…set ones…]'`, then plan and apply as in step 4.
    - This creates the api and worker environments on the first release, the scaling policies and the environment alarms, and waits for the ACM validation.
    - It takes about 15–20 minutes.
11. **Cloudflare apply #2:** `export TF_VAR_api_origin_hostname="$($TF -chdir=infra/terraform/envs/prod output -raw api_origin_hostname)"`, then plan and apply. This adds the proxied `api.todayz.in`.
12. **Smoke test:**
    - `curl -fsS https://api.todayz.in/readyz` and `/version`, which must show the tag;
    - `aws elasticbeanstalk describe-environments` shows both environments `Green`;
    - the worker log shows River started;
    - the SMS dry run from 09 §6 (D-213), once F5/F6 are done;
    - sign in to admin, then seed reference data (09 §5 step 6).
13. **Turn on CI:**
    - create the GitHub `production` environment (F4);
    - set the repository variable `BACKEND_DEPLOY_ENABLED=true`;
    - push to `main` (Pages deploy through `deploy.yml`);
    - the next backend change deploys through `deploy-backend.yml`.

Later releases are CI, or by hand: `build`, `push`, `migrate`, `release`, then `deploy` (`scripts/deploy/beanstalk.sh`). Terraform ignores the environments' version label from then on.

## 5. Rollback

- **App:** `scripts/deploy/beanstalk.sh deploy <older tag>`. It moves the worker, then the api, back to that tag's existing application versions.
  - Tags are listed with `aws elasticbeanstalk describe-application-versions --application-name todayz-prod`.
  - Beanstalk itself rolls the api back if traffic splitting sees health degrade.
- **Database:** migrations only move forward (expand/contract), so the older app runs on the newer schema.
  - Data repair: PITR to a side instance (14 days), then copy back only the affected rows (09 §7).
  - DR: the weekly AWS Backup copy in ap-southeast-1.
- **Pages:** roll back in the Cloudflare dashboard, or redeploy an older commit.
- **Features:** the `platform_settings` flags (09 §7).
- **Infrastructure:**
  - state is versioned, so a bad state write is undone from an older object version;
  - RDS has deletion protection;
  - never `destroy` prod.
- **A failed first deploy:**
  - before step 10, nothing serves traffic; fix the cause and re-run that step;
  - if apply #2 fails on an environment, read its events (`aws elasticbeanstalk describe-events`), fix it, and apply again; the release is not rebuilt.

## 6. Monthly cost, prod (D-258)

Estimated from the ap-south-1 list prices at 730 hours a month, in USD, before GST. Re-check with the AWS Pricing Calculator; the first month's bill is the real number.

| Resource | Sizing | $/month |
|---|---|---|
| RDS PostgreSQL db.t4g.medium, Single-AZ | about $0.073/h | 53 |
| RDS storage gp3 100 GB, backups beyond free (PITR 14 d), Enhanced Monitoring logs | | 19 |
| EC2 3 × t4g.small (api 2, worker 1) and their gp3 root volumes | about $0.0168/h each | 40 |
| Application Load Balancer | hours and LCUs | 24 |
| NAT gateway (one) | hours and about 50 GB processed, plus ECR and Secrets Manager traffic (no interface endpoints, D-263) | 47 |
| Public IPv4 (NAT EIP, 2 ALB addresses) | $0.005/h each | 11 |
| Secrets Manager | 22 secrets (21 app plus the RDS master; `TOTP_ENC_KEY` went with D-264) | 9 |
| CloudWatch | Beanstalk, flow, RDS and ops logs about 20 GB; 11 alarms; one custom metric | 18 |
| DR: AWS Backup weekly snapshot copy to ap-southeast-1 | storage and cross-region transfer | 6 |
| ECR, S3 (ALB logs, app versions, state), ECS ops tasks, data out | | 5 |
| **Total** | | **about 225–255** |
| With 18% GST (AISPL account) | | about 265–300 (≈ ₹23–26k) |

Usage-billed and not in the total:
- SMS through End User Messaging, per message;
- SES, if it is ever used;
- Anthropic and Sarvam;
- Cloudflare (Free or Pro), Grafana Cloud and Sentry (free tiers at first).

The brief expected $220–250. The interface endpoints ($48) were dropped on the founder's word (D-263), which brings the estimate into that range. Staging is not deployed (D-210).

## 7. Known gaps (not blocking the first deploy)

- **Worker tier.** Beanstalk's Worker tier makes an SQS queue and `sqsd` that the River worker does not use. The environment's health is the container's, not `/readyz` on 8181 (09 §3.2). W5-04 decides whether to move it to a load-balancer-less WebServer environment.
- **Cloudflare-only ALB (go-live C5).** The ALB security group takes only Cloudflare ranges (`modules/edge`), but the app still trusts `CF-Connecting-IP` from any peer.
- **Not in this branch** (W5-03): CSP report endpoint (C4), the certificate and uptime alerts, and the cloud restore drill (C6).

## 8. Break glass: unlock an admin's sign-in (D-264)

Use this only when every super admin is locked out of admin sign-in (10 wrong codes in 24 h lock a number for 24 h) and nobody is left to press "Unlock sign-in" on A-18. Otherwise another super admin unlocks on A-18.

`todayz admin unlock-sign-in` lifts that number's admin sign-in lock and clears its failure count. It writes audit `admin_user.unlock_sign_in` with the system actor, the reason and `via: break_glass`, and prints only the admin's user id. It runs as `todayz_app` on the migrate ops task: that task already holds `DATABASE_URL` and the app keys, and its latest revision is the last migrated release. That release must include D-264.

The image has no shell, and a Fargate task has no stdin, so the number goes in as a container override environment value (`--phone-env`), never as a command argument:

```bash
# §1 shell setup first.
CFG="$(aws ssm get-parameter --name /todayz/prod/ops/run-task --query Parameter.Value --output text)"
CLUSTER="$(jq -r .cluster <<<"$CFG")"
NET="$(jq -c '{awsvpcConfiguration: {subnets: .subnets, securityGroups: .securityGroups, assignPublicIp: "DISABLED"}}' <<<"$CFG")"
read -rs -p 'Admin number (+91 and 10 digits): ' PHONE; echo   # typed, not echoed, not in the history
REASON='every super admin locked out; <who asked, ticket>'      # 1-500 characters, no phone number
umask 077
PHONE="$PHONE" jq -n --arg reason "$REASON" '{containerOverrides: [{name: "migrate",
  command: ["admin", "unlock-sign-in", "--phone-env", "BREAK_GLASS_PHONE", "--reason", $reason],
  environment: [{name: "BREAK_GLASS_PHONE", value: $ENV.PHONE}]}]}' > .cache/deploy/unlock.json
unset PHONE
TASK="$(aws ecs run-task --cluster "$CLUSTER" --launch-type FARGATE --task-definition "$(jq -r .migrate <<<"$CFG")" \
  --network-configuration "$NET" --overrides file://.cache/deploy/unlock.json --started-by break-glass \
  --query 'tasks[0].taskArn' --output text)"
rm -f .cache/deploy/unlock.json
aws ecs wait tasks-stopped --cluster "$CLUSTER" --tasks "$TASK"
aws ecs describe-tasks --cluster "$CLUSTER" --tasks "$TASK" --query 'tasks[0].containers[0].exitCode' --output text
aws logs get-log-events --log-group-name "$(jq -r .logGroup <<<"$CFG")" --log-stream-name "ops/migrate/${TASK##*/}" \
  --start-from-head --query 'events[].message' --output text | tr '\t' '\n'
```

- **Exit 0:** done. The log line names `user_id` and `lock_lifted` (false when there was no live lock; the count is still cleared). The admin can sign in at once.
- **Exit 1:** the number belongs to no admin account; nothing changed. A disabled admin is unlocked too, but sign-in still refuses them until they are enabled again.
- **Exit 2:** a bad number (it must be +91 and 10 digits) or a bad reason; nothing changed.
- **Where the number is kept.** It is not in the shell history, the process list, the task's command or any log line. AWS does keep the RunTask request, overrides included, in CloudTrail (90 days of event history) and on the stopped task for about an hour. Only account administrators can read either. If that is not acceptable, the alternative is a Terraform change: give the ops execution role read access to a short-lived S3 environment file, or turn on ECS Exec. That is the lead's call.
- Locally, or anywhere with a shell: `todayz admin unlock-sign-in --phone-file FILE --reason TEXT`, or `--phone-file -` to read stdin.
