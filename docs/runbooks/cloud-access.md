# Cloud access: what the founder sets up

Put every value in **`.env.cloud.local`** at the repo root. The file already exists with the names and empty values, and it is git-ignored. Do not paste any value in chat.

The lead checks the file with `make env-check`, which prints only set or missing. The app processes never read this file: they get their secrets from AWS Secrets Manager, through instance roles.

All three accounts below are TodayZ accounts, separate from your personal ones. No plugin or connector of yours is used.

## 1. GitHub

1. Create a free GitHub organization for TodayZ (for example `todayz`), owned by you.
2. Under Organization settings → Personal access tokens, allow fine-grained tokens.
3. Under your profile → Developer settings → Fine-grained tokens → Generate:
   - **Resource owner:** the TodayZ org.
   - **Repository access:** All repositories.
   - **Expiry:** 90 days.
   - **Repository permissions:** set these to Read and write:
     - Administration
     - Contents
     - Workflows
     - Actions
     - Secrets
     - Variables
     - Environments
     - Pull requests

     Metadata is Read.
4. Fill in `GITHUB_OWNER` (the org name) and `GH_TOKEN`.

Repos are created **private**. The lead then sets up:
- branch protection;
- CODEOWNERS (you);
- required checks;
- the AWS OIDC deploy role.

After that, CI deploys run without long-lived AWS keys (09 §4).

## 2. AWS

1. Use a **dedicated AWS account** for TodayZ, either a new account or a member account in AWS Organizations.
2. Turn on MFA for the root user. Never create root access keys.
3. Under IAM → Users → Create user `todayz-terraform`:
   - no console access;
   - attach **AdministratorAccess**.

   Terraform creates the VPC, IAM roles, RDS, Beanstalk, ECR, Secrets Manager, Budgets and Backup. A narrower policy would break in the middle of an apply. The account boundary is the real limit.
4. Under Security credentials → Create access key → "Command Line Interface", copy the key pair into `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`.
5. Keep `AWS_REGION=ap-south-1` (Mumbai).
6. Set `BUDGET_ALERT_EMAIL` to the address that gets the budget alarms. The default budget is ₹30,000 a month: an alarm at 80% of actual spend and at 100% of forecast.

The first thing created is the budget alarm, then the S3 bucket and lock that hold the Terraform state.

**Estimated cost (09 §3.8):** prod only (D-210, no staging), about ₹13–17k a month.

Nothing is applied until you have seen the `terraform plan` summary and said yes.

## 3. Cloudflare

1. The `todayz.in` zone must be in the Cloudflare account, with its nameservers pointed at Cloudflare.
2. Copy the **Account ID** and the **Zone ID** from the zone's Overview page into `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_ZONE_ID`.
3. Under My Profile → API Tokens → Create Custom Token:
   - **Account:**
     - Cloudflare Pages: Edit
     - Workers R2 Storage: Edit
     - Turnstile: Edit
     - Account Settings: Read
   - **Zone:**
     - Zone: Read
     - Zone Settings: Edit
     - DNS: Edit
     - SSL and Certificates: Edit
     - Zone WAF: Edit
     - Cache Rules: Edit
   - **Zone resources:** Include → Specific zone → `todayz.in`.
   - **Account resources:** the TodayZ account only.
4. Fill in `CLOUDFLARE_API_TOKEN`.

Terraform creates the rest: DNS, WAF, rate limits, the R2 buckets and their custom domains, the Turnstile widget, and the four Pages projects.

The R2 S3 keys the app uses are created later, scoped to the buckets once they exist. The lead will ask for them then.

## 4. Providers (prod can go up without these; real logins need MSG91)

| Names | What | Note |
|---|---|---|
| `MSG91_AUTH_KEY`, `MSG91_SENDER_ID`, `MSG91_OTP_TEMPLATE_ID` | SMS OTP | Real logins need DLT registration of the sender ID and the template, which can take days in India. Start it now. |
| `SENTRY_DSN`, `SENTRY_AUTH_TOKEN` | Errors and source maps | Free tier. Make one org with one project per app, or a single project. |
| `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS` | Grafana Cloud telemetry | Free tier. Use Grafana Cloud → OpenTelemetry → the OTLP endpoint and the Authorization header. |

The lead generates the app keys (JWT, order-code, PII, TOTP and so on) fresh for each environment, straight into Secrets Manager. They are never written to a file.

WhatsApp, Anthropic and Sarvam stay on the mocks until you add their keys.

## When it is filled in

Tell the lead "env is ready". The lead then:
1. Runs `make env-check` and reports only set or missing.
2. Creates the repos and pushes.
3. Applies the AWS budget and Terraform state.
4. Shows you the prod plan and cost, and applies it after your yes.
5. Deploys the API and the four apps, and runs the smoke checks against prod.
6. From then on, every merge or push to `main` deploys to prod through GitHub Actions (D-210).
