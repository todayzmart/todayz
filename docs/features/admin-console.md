# Admin console

What TodayZ staff can see and do: pulse, reviews of shops, risk, settings, support tickets, jobs and system health.

> As built on main at 2c2f23d6 (27 Oct 2026). Update this file in the same change as the code it describes.

The console is at admin.todayz.in. It is English only (the console i18n is partial, see [languages-and-design](languages-and-design.md)).

## What users can do

Each screen is a route in [admin/src](../../admin/src/). Screens are gated by role (D-64): field_agent, support, catalogue, analyst, finance, admin, super_admin.

| Screen | Folder | What |
|---|---|---|
| Pulse (home) | [pulse](../../admin/src/pulse/) | Daily platform numbers |
| Businesses, stores | [businesses](../../admin/src/businesses/), [stores](../../admin/src/stores/) | Verify, ask for info, suspend, review store, force-close ([stores-and-onboarding](stores-and-onboarding.md)) |
| Field agents, leads | [field](../../admin/src/field/) | Onboard shops for owners |
| Orders | [orders](../../admin/src/orders/) | Search and cancel ([orders-pickup-delivery](orders-pickup-delivery.md)) |
| Customers | [customers](../../admin/src/customers/) | Block, reset strikes, adjust credits |
| Risk | [risk](../../admin/src/risk/) | Open flags, take actions (D-225) |
| Credits, payouts | [credits](../../admin/src/credits/), [payouts](../../admin/src/payouts/) | See [credits-and-wallet](credits-and-wallet.md), [payouts-and-money](payouts-and-money.md) |
| Reviews, banners, referrals | [reviews](../../admin/src/reviews/), [banners](../../admin/src/banners/), [referrals](../../admin/src/referrals/) | Moderation |
| Catalogue | [catalogue](../../admin/src/catalogue/) | Pending products, categories, synonyms, images |
| Demand | [demand](../../admin/src/demand/) | Demand gaps, site marquee |
| Notifications | [notifications](../../admin/src/notifications/) | Read-only templates, test send |
| Support | [support](../../admin/src/support/) | Tickets with an SLA clock (D-226) |
| Settings | [settings](../../admin/src/settings/) | Edit `platform_settings` with history |
| Admin users, audit | [admin-users](../../admin/src/admin-users/), [audit](../../admin/src/audit/) | Staff accounts, TOTP reset, audit trail |
| Jobs (A-21) | [jobs](../../admin/src/jobs/) | Run state of every background job (D-95) |
| System (A-22) | [system](../../admin/src/system/) | Health and AWS service readouts (D-97, D-227) |

Customers and shops reach support from their own apps: [customer/src/help](../../customer/src/help/) and [business/src/help](../../business/src/help/).

## Rules

Source: [01-product-rules.md](../plan/01-product-rules.md) sections 13 and 14.

- Every sensitive change writes an audit row, and KYC reveal is audited (D-41).
- Support SLA keys: `support_sla_<priority>_min`, `support_open_tickets_max`, `support_tickets_per_hour`, `support_uploads_per_hour`, `support_upload_ttl_hours`.
- Risk rules and thresholds: D-225. Store strikes do not auto-pause (D-67). Keys include `store_strike_risk_threshold`.
- The permission matrix is D-38. Admin AI drafts apply by the screen's own permissions (D-233).

## How it works

```mermaid
flowchart LR
    Admin[Admin signs in + TOTP] --> Perm[Permission check by role]
    Perm --> Act[Action in domain package]
    Act --> Aud[audit_log]
    Act --> Flag[risk_flags if suspicious]
    Flag --> Risk[Risk screen]
```

Every domain keeps its own admin handlers; `backend/internal/admin` holds the shared admin pieces (businesses, stores, users, settings, audit, catalogue, banners, leads, notifications, AI assist). `backend/internal/risk` raises flags (`risk_scan` job). `backend/internal/support` runs tickets with presigned uploads and a retention sweep.

## Code

| Part | Where |
|---|---|
| Backend | `backend/internal/admin` ([folder](../../backend/internal/admin/)), `backend/internal/risk` ([folder](../../backend/internal/risk/)), `backend/internal/support` ([folder](../../backend/internal/support/)); also `backend/internal/ops` (the System screen's cloud readouts) and `backend/internal/platform` (jobs framework, settings, authz) |
| API paths | `admin-*.yaml` in [backend/api/paths](../../backend/api/paths/), plus [support.yaml](../../backend/api/paths/support.yaml), [biz-support.yaml](../../backend/api/paths/biz-support.yaml) |
| Tables | `platform_settings`, `audit_log`, `risk_flags`, `admin_users`, `support_tickets`, `support_messages`, `support_attachments`, `job_runs`, `job_stats_hourly`, `job_controls` |
| Jobs | `risk_scan`, `support_uploads_sweep`, `jobs_watchdog`, `job_runs_retention`, `platform_heartbeat`, `platform_maintenance` |

## Tests

Package tests; journeys `13-e2e-20-suspended`, `12-e2e-19-blocked`, `05-e2e-10-review-hide`. Reports: [W3-02](../qa/W3-02/REPORT.md), [W3-08](../qa/W3-08/REPORT.md), [W3-15](../qa/W3-15/REPORT.md), [W3-16](../qa/W3-16/REPORT.md), [admin-i18n](../qa/admin-i18n/). Runbooks: [risk-store-strikes](../runbooks/risk-store-strikes.md), [jobs-job-failing](../runbooks/jobs-job-failing.md).

## Status and known gaps

Live in code. Admin and its legal text are English only (B6). A sessions page for staff is not built.

## Decisions

D-38, D-41, D-47, D-64, D-95, D-97, D-106, D-224, D-225, D-226, D-227, D-233, D-240, D-244.
