# Secret rotation

Names only. This page never holds a value, and neither does any note, log, commit or chat about a
rotation. `make env-check` shows `NAME set|missing`; nothing else should ever print a secret.

## Where secrets live

| Where | Names | Who changes them |
|---|---|---|
| `.env.local` (local, git-ignored) | The app keys and provider keys in `.env.example` | Each developer; `make setup` generates missing app keys |
| `.env.cloud.local` (lead's shell only, git-ignored) | `GH_TOKEN`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`, `SENTRY_AUTH_TOKEN` and the other names in `.env.cloud.example` | The founder creates; the lead stores |
| AWS Secrets Manager `todayz/prod/<NAME>` | App keys, `DATABASE_URL`, `DATABASE_MIGRATE_URL`, provider keys (list in `infra/terraform/modules/secrets/main.tf`) | The lead; Beanstalk injects them at deploy |
| GitHub repository secrets | `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` (allow-list: `scripts/ci/wflint/secrets-allow.txt`) | The lead |
| Cloudflare Pages project settings | `SHARE_PREVIEW_KEY` on `todayz-customer` | The lead |

## The general steps (any Secrets Manager entry)

1. Create the new value at its source (provider console, `openssl rand` on your own machine, the RDS role). Never in a shared terminal that logs.
2. Put it in Secrets Manager: console → Secrets Manager → `todayz/prod/<NAME>` → Retrieve → Edit, or `aws secretsmanager put-secret-value --secret-id todayz/prod/<NAME> --secret-string file://<tmpfile>`, then delete the temp file. Do not pass the value on the command line (shell history).
3. Redeploy the current version so instances read it: Beanstalk → environment → Actions → Restart app server(s) does **not** re-read injected secrets; deploy the same version label again (`aws elasticbeanstalk update-environment --environment-name todayz-prod-api --version-label <current>`), api first, then `todayz-prod-worker`.
4. Check: `/readyz` 200, the dashboards flat, the feature that uses the key works.
5. Revoke the old value at its source.
6. Note the rotation (name, date, why, who) in the ops log. No values.

## Per secret

### App keys (09 §2.1, D-18)

| Name | Can rotate without data work? | How |
|---|---|---|
| `JWT_SIGNING_KEYS`, `JWT_CURRENT_KID` | Yes, by kid rollover | 1. Add a new Ed25519 key with a new `kid` to the set; deploy (the API now verifies both). 2. Set `JWT_CURRENT_KID` to the new kid; deploy (new access tokens use it). 3. Wait at least one hour (access tokens live 15 min; refresh tokens are opaque and not signed with this key). 4. Remove the old key from the set; deploy. |
| `BILL_QR_SIGNING_KEY` | Yes | Bill QRs expire in 120 s. Rotate outside shop hours; a QR shown during the deploy fails and the POS refreshes it. |
| `SHARE_PREVIEW_KEY` | Yes, both sides together | Set the same new value on the API (Secrets Manager) and on the `todayz-customer` Pages project, then deploy both. Between the two, previews are counted by Cloudflare IP (the API still starts). |
| `ORDER_CODE_HMAC_KEY`, `PII_HASH_KEY` | **No** | Stored lookups (`code_hmac`, PII hashes) are keyed with it. A new key makes existing rows unfindable. Needs a re-hash job that does not exist yet. |
| `ORDER_CODE_ENC_KEY`, `PII_ENC_KEY`, `DEVICE_SECRET_ENC_KEY` | **No** | Stored ciphertext (order codes, phones, PAN, bank details, device secrets) is sealed with one key, with no key version. A new key makes existing rows unreadable. Needs a re-encrypt job that does not exist yet. |

If a **no** key leaks, it is SEV1 ([Severity levels](severity.md)). Tell the lead and the founder at
once. Do not swap the key in place. The fix is a dual-key release (read old and new, write new), a
re-encrypt or re-hash job, then removal of the old key. That work is not built (open point in
`docs/qa/W4-07/REPORT.md`).

### Database

| Name | How |
|---|---|
| `DATABASE_URL` (role `todayz_app`) | `ALTER ROLE todayz_app PASSWORD …` as the owner from an SSM session (never in a logged shell); update the secret; redeploy api then worker. Old connections keep working until they recycle, so there is no gap. |
| `DATABASE_MIGRATE_URL` (role `todayz_owner`) | Same, for the owner role. Only migrations use it. |
| RDS master user `todayz` | RDS console → Modify → new master password, or move it to Secrets Manager-managed rotation. Nothing in the app uses it. |

### Providers

| Name | Notes |
|---|---|
| `ANTHROPIC_API_KEY`, `SARVAM_API_KEY` | New key in the provider console, update, redeploy, revoke old. AI features fall back to rules during the gap. |
| `WHATSAPP_API_KEY`, `GEOCODER_API_KEY` | Same steps. |
| `TURNSTILE_SECRET_KEY` | Cloudflare → Turnstile → widget `todayz-prod` → rotate secret. The site key (`TURNSTILE_SITE_KEY`, public) does not change. Cloudflare keeps the old secret valid for a short overlap. |
| `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | New R2 API token scoped to the buckets; update both names together; redeploy; delete the old token. |
| `VAPID_PRIVATE_KEY` (with `VAPID_PUBLIC_KEY`) | Rotating breaks every push subscription; users must allow notifications again. Rotate only if it leaked. |
| `SENTRY_DSN`, `SENTRY_AUTH_TOKEN` | Sentry → project → Client keys / auth tokens. The DSN is not very secret, but rotate it if it is abused. |
| `OTEL_EXPORTER_OTLP_HEADERS` | Grafana Cloud → access policy token. Update and redeploy; telemetry gaps during the switch are expected. |
| SMS | No key (D-212): the instance role signs requests. Rotate nothing; check the IAM policy instead. |

### Cloud admin (the lead's shell, D-210)

| Name | How |
|---|---|
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | IAM → the user → Security credentials → create a second key, update `.env.cloud.local`, test with a read call, deactivate then delete the old key. |
| `CLOUDFLARE_API_TOKEN` | Cloudflare → My Profile → API Tokens → Roll. Update `.env.cloud.local` **and** the GitHub repository secret `CLOUDFLARE_API_TOKEN`, or the next Pages deploy fails. |
| `GH_TOKEN` | GitHub → Settings → Developer settings → tokens. Regenerate, update `.env.cloud.local`. |

## When to rotate

- At once: a value was printed, pasted, committed, screenshotted, or sent to the wrong place; a laptop with `.env.cloud.local` is lost; a person with access leaves.
- Every 90 days: cloud admin keys (`AWS_*`, `CLOUDFLARE_API_TOKEN`, `GH_TOKEN`).
- Yearly: provider keys and `JWT_SIGNING_KEYS`.
- A committed secret is rotated even after the commit is removed; history and caches keep it.
