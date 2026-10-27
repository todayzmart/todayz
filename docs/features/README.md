# TodayZ feature catalogue

One page per feature area, written from the code on main: what each app lets people do, the rules, how it works, where the code and tests are, and what is missing.

> As built on main at 2c2f23d6 (27 Oct 2026). Update this file in the same change as the code it describes.

## How to read a feature doc

Every file has the same sections.

1. Purpose: one line.
2. What users can do: per app, with links to the route folders.
3. Rules: which sections of [01-product-rules.md](../plan/01-product-rules.md) apply, and the `platform_settings` keys. Numbers that are settings are never written here; read them from the rules file or the settings table.
4. How it works: short prose and a diagram.
5. Code: backend packages, API path files, tables, jobs, frontend folders.
6. Tests: package tests, journeys, QA reports.
7. Status and known gaps.
8. Decisions: D-nnn entries from the [decision log](../plan/12-decision-log.md), with the reason for each choice.

The code on main is the truth. The plan in `docs/plan/` says what was intended. Where they differ, these docs follow the code.

Related starting points: [root README](../../README.md), [AGENTS.md](../../AGENTS.md), [docs index](../README.md), [working on TodayZ](../guide/working-on-todayz.md).

## Status legend

| Label | Meaning |
|---|---|
| Live in code | Built on main and covered by tests. Not the same as proven in prod. |
| Partly built | Some parts built; the gap is named in the doc. |
| Planned only | In the plan, no code. |

## Feature by app

| Feature | Customer app | Shop app (business) | Admin console | Site | Backend jobs |
|---|---|---|---|---|---|
| [Auth and accounts](auth-and-accounts.md) | Live | Live | Live (SMS step-up) | - | Live |
| [Stores and onboarding](stores-and-onboarding.md) | Live (shop page) | Live | Live | - | Live |
| [Catalogue and stock](catalogue-and-stock.md) | Live (view) | Live | Live | - | Live |
| [Search and discovery](search-and-discovery.md) | Live (map is a card) | Live (reviews) | Live (moderation) | - | Live |
| [Orders, pickup and delivery](orders-pickup-delivery.md) | Live | Live | Live | - | Live (timers) |
| [POS and offline](pos-and-offline.md) | Live (scan, receipt) | Live | - | - | Live |
| [Credits](credits-and-wallet.md) | Partly built (no wallet screen) | Live | Live | - | Live |
| [Payouts and money](payouts-and-money.md) | - | Live | Live | - | Live |
| [Offers, banners, referrals](offers-banners-referrals.md) | Partly built (no refer screen) | Live | Live | - | Live |
| [Asks](asks.md) | Live | Live | Live (demand) | Marquee | Live |
| [Customers, CRM, insights](customers-crm-and-insights.md) | - | Live | Live (pulse) | - | Live |
| [AI features](ai-features.md) | Live | Live | Live | - | Live |
| [Notifications](notifications.md) | Partly built (no inbox) | Partly built (no inbox) | Live (read-only) | - | Partly built (SMS only) |
| [Admin console](admin-console.md) | - | Help screens | Live | - | Live |
| [Languages and design](languages-and-design.md) | Live | Live | English, partial | Live | - |
| [Marketing site](marketing-site.md) | - | - | Leads list | Live | Live (marquee) |

## Backend packages

Each package under `backend/internal` appears in at least one feature doc. Not covered here (platform or tooling): `devtools`, `testkit`, `seed`, `telemetry`, `platform`, `jobs`, `ops` (the System screen's cloud readouts are mentioned in the admin doc).

| Package | Feature doc |
|---|---|
| `backend/internal/identity` | [auth-and-accounts](auth-and-accounts.md) |
| `backend/internal/business` | [stores-and-onboarding](stores-and-onboarding.md) |
| `backend/internal/catalogue`, `backend/internal/imageimport` | [catalogue-and-stock](catalogue-and-stock.md) |
| `backend/internal/search`, `backend/internal/baskets`, `backend/internal/reviews` | [search-and-discovery](search-and-discovery.md) |
| `backend/internal/orders` | [orders-pickup-delivery](orders-pickup-delivery.md) |
| `backend/internal/pos` | [pos-and-offline](pos-and-offline.md) |
| `backend/internal/credits` | [credits-and-wallet](credits-and-wallet.md) |
| `backend/internal/payouts` | [payouts-and-money](payouts-and-money.md) |
| `backend/internal/offers`, `backend/internal/banners`, `backend/internal/referrals` | [offers-banners-referrals](offers-banners-referrals.md) |
| `backend/internal/asks`, `backend/internal/share` | [asks](asks.md) |
| `backend/internal/insights`, `backend/internal/coach`, `backend/internal/rollups` | [customers-crm-and-insights](customers-crm-and-insights.md) |
| `backend/internal/ai`, `backend/internal/voice` | [ai-features](ai-features.md) |
| `backend/internal/notify` | [notifications](notifications.md) |
| `backend/internal/admin`, `backend/internal/risk`, `backend/internal/support` | [admin-console](admin-console.md) |

## Keeping this current

Change a feature, change its doc in the same pull request. If you add a package under `backend/internal`, add it to a feature doc and to the table above.
