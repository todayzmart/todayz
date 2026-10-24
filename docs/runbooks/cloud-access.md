# Cloud access: what the founder sets up

Put every value in **`.env.cloud.local`** at the repo root. The file already exists with the names and empty values, and it is git-ignored.

- Write each value as `NAME=value`, with no quotes and no spaces around `=`.
- Do not paste any value in chat.

The lead checks the file with `make env-check`, which prints only set or missing. The app processes never read this file: they get their secrets from AWS Secrets Manager, through instance roles. No plugin or connector of yours is used.

Most tokens are **shown only once**. Copy each one straight into the file before closing the page.

## 1. GitHub: `GITHUB_OWNER`, `GH_TOKEN`

1. **Create the organization.** Go to https://github.com/organizations/plan and choose **Free**.
   - Organization name: for example `todayz`. If it is taken, pick another.
   - Contact email: your TodayZ email.
   - "This organization belongs to": **My personal account**. That only makes you the owner; the repos live in the org.
2. **Allow fine-grained tokens.** Go to `https://github.com/organizations/<org>/settings/personal-access-tokens`.
   - Choose **Allow access via fine-grained personal access tokens**.
   - Choose **Do not require administrator approval**. You are the only admin.
   - Save.
3. **Create the token.** Go to https://github.com/settings/personal-access-tokens/new.
   - Token name: `todayz-lead`.
   - Resource owner: **the org**, not your username.
   - Expiration: 90 days.
   - Repository access: **All repositories**.
   - Permissions → Repository permissions. Set each of these to **Read and write**:
     - Administration
     - Actions
     - Contents
     - Environments
     - Pull requests
     - Secrets
     - Variables
     - Workflows

     Metadata turns to Read-only on its own.
   - Click **Generate token**. Copy the token (`github_pat_…`).
4. **Fill in the file:**
   - `GITHUB_OWNER=` the org name;
   - `GH_TOKEN=` the token.

## 2. AWS: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `BUDGET_ALERT_EMAIL`

1. **Create the account.** Go to https://signup.aws.amazon.com.
   - Use a TodayZ email that no other AWS account uses. Account name: `TodayZ`.
   - Enter the address and card, and do the phone check.
   - Support plan: **Basic** (free).

   An Indian address and card gives an AWS India (AISPL) account, which is fine: Mumbai `ap-south-1` is on by default.
2. **Protect the root user.** Sign in as root, then go to the account name at the top right → **Security credentials** → **Assign MFA device**, and use an authenticator app.

   Never create access keys for root.
3. **Create the Terraform user.** Go to https://console.aws.amazon.com/iam → **Users** → **Create user**.
   - User name: `todayz-terraform`.
   - Leave **"Provide user access to the AWS Management Console" unticked**.
   - Click Next → **Attach policies directly** → tick **AdministratorAccess** → Next → **Create user**.

   Terraform creates the VPC, IAM roles, RDS, Beanstalk, ECR, Secrets Manager, Budgets and Backup. A narrower policy breaks in the middle of an apply. The dedicated account is the real limit.
4. **Create the access key.** Open the user → **Security credentials** tab → **Access keys** → **Create access key**.
   - Use case: **Command Line Interface (CLI)**. Tick the confirmation, then Next.
   - Description: `lead-shell`. Then **Create access key**.
   - Copy the **Access key** into `AWS_ACCESS_KEY_ID` and the **Secret access key** into `AWS_SECRET_ACCESS_KEY`.
   - If you download the .csv instead, delete it after copying.
5. **Region and alerts:**
   - Leave `AWS_REGION=ap-south-1`.
   - Set `BUDGET_ALERT_EMAIL=` the address that should get cost alarms.

   The budget is ₹30,000 a month, with an alarm at 80% of actual spend and at 100% of forecast.

**Estimated cost (09 §3.8):** prod only (D-210, no staging), about ₹13–17k a month. The budget alarm is the first resource created. Nothing else is applied until you have seen the `terraform plan` summary and said yes.

## 3. Cloudflare: `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`, `CLOUDFLARE_API_TOKEN`

1. **Sign up** at https://dash.cloudflare.com/sign-up with your TodayZ email.
2. **Add the domain.**
   - Go to **Add a domain** → `todayz.in` → **Free** plan.
   - Cloudflare imports the current DNS records.
   - **Check that the list has every record the current site and email use**, especially MX, TXT/SPF and the `@` and `www` records, before going on. A missing record breaks the old site or email.
   - Cloudflare then shows **two nameservers**. At the registrar where you bought `todayz.in` (GoDaddy, Hostinger, BigRock or another), replace the nameservers with those two.
   - Wait until Cloudflare shows the zone as **Active**. That takes minutes to a few hours, and Cloudflare emails you.
3. **Copy the IDs.**
   - Open `todayz.in` in the dashboard and go to the **Overview** page.
   - Scroll the right-hand column to the **API** box.
   - Copy the **Zone ID** into `CLOUDFLARE_ZONE_ID` and the **Account ID** into `CLOUDFLARE_ACCOUNT_ID`.
4. **Create the token.** Go to https://dash.cloudflare.com/profile/api-tokens → **Create Token** → **Create Custom Token** → **Get started**.
   - Token name: `todayz-terraform`.
   - Permissions: add one row each with **+ Add more**. Each row is type → item → level.

     | Type | Item | Level |
     |---|---|---|
     | Account | Cloudflare Pages | Edit |
     | Account | Workers R2 Storage | Edit |
     | Account | Turnstile (may read "Turnstile Sites") | Edit |
     | Account | Account Settings | Read |
     | Zone | Zone | Read |
     | Zone | Zone Settings | Edit |
     | Zone | DNS | Edit |
     | Zone | SSL and Certificates | Edit |
     | Zone | Zone WAF | Edit |
     | Zone | Cache Rules | Edit |
   - Account Resources: Include → your account.
   - Zone Resources: Include → Specific zone → `todayz.in`.
   - Leave Client IP filtering and TTL empty.
   - Click **Continue to summary** → **Create Token**. Copy the token into `CLOUDFLARE_API_TOKEN`.

   If the first `terraform plan` reports a missing permission, the lead tells you which row to add. You edit the token without changing its value.

Terraform creates the rest: DNS, WAF, rate limits, the R2 buckets and their custom domains, the Turnstile widget, and the four Pages projects. The R2 S3 keys the app uses are created later, scoped to the buckets once they exist; the lead will ask for them then.

## 4. Providers (prod can go up without these; real logins need MSG91)

### SMS OTP (MSG91): `MSG91_AUTH_KEY`, `MSG91_SENDER_ID`, `MSG91_OTP_TEMPLATE_ID`

In India every business SMS must be registered on DLT first. This takes **days**, so start it now.

1. **Register on a DLT portal** as a **Principal Entity**: Jio TrueConnect, Airtel DLT or Vi DLT. Use the company documents: PAN, and the GST or incorporation certificate. Once approved you get an **Entity ID**.
2. **Register a Header** (the sender ID). Use 6 letters, for example `TODAYZ`, category Transactional/Service.
3. **Register a Content Template** (Service Implicit, OTP), for example: `{#var#} is your TodayZ login code. Do not share it.` Once approved you get a DLT template ID.
4. **Set up MSG91.**
   - Sign up at https://msg91.com.
   - Add the DLT Entity ID and the sender ID.
   - Create an **OTP template** with the DLT template ID.
   - Copy MSG91's template id into `MSG91_OTP_TEMPLATE_ID` and the sender ID into `MSG91_SENDER_ID`.
   - Copy the **Authkey** into `MSG91_AUTH_KEY`. It is under the account menu → **Authkey**, or API → Authkey.

### Errors (Sentry): `SENTRY_DSN`, `SENTRY_AUTH_TOKEN`

1. Sign up at https://sentry.io (free Developer plan) and create the org `todayz`.
2. **Create a project:** platform **Go**, name `todayz-api`. Go to Settings → Projects → todayz-api → **Client Keys (DSN)** and copy the DSN into `SENTRY_DSN`.

   The lead adds the frontend projects later with the auth token.
3. **Create the auth token:** Settings → **Developer Settings** → **Organization Tokens** → **Create New Token**, named `todayz-ci`. Copy it into `SENTRY_AUTH_TOKEN`.

### Telemetry (Grafana Cloud): `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`

1. Sign up at https://grafana.com (free tier) and create a stack. Pick the region closest to India.
2. **Get the OTLP settings.**
   - Go to the stack's **Connections** (or **My Account → your stack**) → **OpenTelemetry** → **Configure**.
   - Generate an API token there.
   - The page then shows the environment variables. Copy `OTEL_EXPORTER_OTLP_ENDPOINT` as it is.
   - Copy `OTEL_EXPORTER_OTLP_HEADERS` as it is. It looks like `Authorization=Basic …`.

### Generated by the lead, never by you

- The app keys (JWT, order-code, PII, TOTP and the rest) and the VAPID keys are generated fresh for prod, straight into Secrets Manager. They are never written to a file.
- WhatsApp, Anthropic and Sarvam stay on the mocks until you add their keys.

## When it is filled in

You can fill in the file in parts: GitHub, AWS and Cloudflare are enough to start.

Tell the lead "env is ready". The lead then:
1. Runs `make env-check` and reports only set or missing.
2. Creates the `todayz` repo and pushes `main`.
3. Applies the AWS budget and the Terraform state bucket.
4. Shows you the prod plan and cost, and applies it after your yes.
5. Deploys the API and the four apps, and runs the smoke checks against prod.
6. From then on, every merge or push to `main` deploys to prod through GitHub Actions (D-210).
