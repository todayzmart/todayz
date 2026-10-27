# Prod plan summary, 27 Oct 2026 (for the founder's yes, F3)

`terraform plan` was run on main at c5ddd7f4 against AWS account 061292478202 (ap-south-1). It used a throwaway local state, so **nothing was created**. Today the account has no state bucket, no ECR repository, no RDS instance and no Beanstalk application.

## What the first apply creates

| Step | Plan | What it creates |
|---|---|---|
| 1. State bucket | script | `s3://todayz-tfstate-061292478202` (versioned, encrypted) |
| 2. Account root | **4 to add**, 0 change, 0 destroy | A monthly budget alert (₹30,000, mails to `BUDGET_ALERT_EMAIL`), an ECR image repository and its lifecycle rule, and the GitHub OIDC login for `todayzmart/todayz` |
| 3. First image | script | The backend image built and pushed to ECR |
| 4. Prod apply #1 | **134 to add**, 0 change, 0 destroy (139 before D-263) | Details below |

Prod apply #1 creates:
- **Network:** a VPC with 4 subnets in 2 zones, one NAT gateway, the free S3 gateway endpoint, 3 security groups and VPC flow logs. There are no interface endpoints (D-263).
- **Database:** RDS Postgres on db.t4g.medium, Single-AZ, 100 GB gp3, 14-day point-in-time restore, deletion protection on. A weekly backup copy goes to ap-southeast-1.
- **Secrets:** 22 empty Secrets Manager entries, filled later by `secrets.sh` (names only).
- **Ops:** an ECS cluster with three Fargate task definitions (database bootstrap, migrate, item-photo picks), IAM roles, log groups, 4 alarms, and an SNS alarm topic that mails `BUDGET_ALERT_EMAIL`.
- **Beanstalk:** the `todayz-prod` application only. **No environments, no servers and no load balancer yet.** Those come in prod apply #2, after the database is migrated, as the runbook planned (D-254).
- **TLS:** the ACM certificate request for `api.todayz.in`.

Cost: about $225–255 a month before GST once apply #2 adds the servers and load balancer; about ₹23–26k with GST (RUNBOOK §6, D-258, D-263). After apply #1 alone it is roughly $90–110 a month (RDS, NAT, secrets).

## What comes after the yes (RUNBOOK §4)

| Step | Action | Time |
|---|---|---|
| 5 | Cloudflare apply #1: imports the live Pages projects, domains, R2 and Turnstile into Terraform, and adds the ACM validation records | The plan must show imports only. The one expected replacement is the apex record, which becomes the flattened Pages CNAME |
| 6–9 | Secrets, database bootstrap, migrate, application versions | about 20 min |
| 10 | Prod apply #2: api 2 × t4g.small, worker 1 × t4g.small, the load balancer and scaling | about 15–20 min |
| 11 | Cloudflare apply #2: the proxied `api.todayz.in` | |
| 12 | Smoke test: `/readyz`, `/version`, both environments Green | |

Each later plan is checked before its apply. If one shows anything other than the expected creates or imports (any destroy or replace beyond the apex record), it stops and comes back to you.

## Inputs still open

| # | Item | Status | Effect |
|---|---|---|---|
| F1 | Billing currency | Assumed INR, ₹30,000 budget | Say if the account bills in USD (then 350) |
| F11 | Budget vs estimate | ₹30k; the estimate is now ₹23–26k, so the 80% alert (₹24k) may still fire some months | Keep, or adjust |
| D-263 | Interface endpoints | Dropped on your word (27 Oct) | Saves about $45 a month |
| F5/F6 | DLT entity ID and OTP template ID | **Empty** (the sender ID is set) | The app runs, but real phone logins fail until DLT and the SMS sandbox exit are done |
| F9 | Sentry DSN, Grafana OTLP endpoint | Empty | No error tracking or dashboards in prod until they are set |
| F8 | R2 API token | Not in secrets yet | Image uploads fail until it is set |
