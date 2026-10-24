# infra/cloudflare

Cloudflare for one environment (`environment = prod|staging`): zone TLS settings, DNS (proxied api.,
Pages hosts, ACM validation records), WAF (managed ruleset plus three custom blocks), rate limits,
R2 buckets with custom domains for `img.` and `tiles.`, a Turnstile widget, cache rules and four
Pages projects (site, customer, business, admin; direct upload from CI).

**Not applied.** The provider reads `CLOUDFLARE_API_TOKEN` from the environment; account and zone ids
are variables (`CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`). Only `fmt -check`, `validate`, tflint
and checkov run before Wave 5 (`make check-infra`).

- Staging hosts use a hyphen (`app-staging.todayz.in`): Universal SSL covers one wildcard level, so
  `app.staging.todayz.in` would need Advanced Certificate Manager.
- The default managed ruleset is the Free plan's; see `waf_managed_ruleset_ids` for Pro.
- Rate-limit numbers are starting values (`TODO(lead)`); the plan gives none.
- The Turnstile secret is a sensitive output: put it in Secrets Manager as `TURNSTILE_SECRET_KEY`.
- Apply order with AWS: see `infra/terraform/README.md`.

**Live parts made by hand (D-214).** The four Pages projects, their custom domains (including
`www.todayz.in` on the site project, which this module does not have yet), the Pages DNS records
and the `todayz-prod` Turnstile widget were created through the API on 2026-10-24. Import them
before the first apply. `scripts/deploy/pages.sh` and `.github/workflows/deploy.yml` deploy the
sites; `pages.env` holds the public build settings.
