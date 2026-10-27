# Languages and design

The five languages, the shared design tokens and components, and the "bright" skin used by the customer and shop apps.

> As built on main at 2c2f23d6 (27 Oct 2026). Update this file in the same change as the code it describes.

## What users can do

| App | What |
|---|---|
| Customer app | Pick a language (route `language`), the choice is remembered. Folder: [shared/i18n](../../shared/i18n/), [customer/src/shell](../../customer/src/shell/). |
| Shop app | Same picker; screens translated ([business-i18n report](../qa/business-i18n/)). |
| Admin console | English with partial catalogue ([admin-i18n report](../qa/admin-i18n/)). |
| Site | Home and for-retailers in all five languages; other pages English only. See [marketing-site](marketing-site.md). |

## Rules

Source: [01-product-rules.md](../plan/01-product-rules.md); design source is the artifact named in the root [CLAUDE.md](../../CLAUDE.md).

- Languages: English (default and fallback), Hindi, Kannada, Telugu, Tamil. A pseudo-locale `en-XA` is for testing only.
- Messages are ICU MessageFormat, sentence case, no exclamation marks. Numbers use Latin digits in every language. Amounts and dates arrive pre-formatted.
- Machine drafts and stale translations never ship to prod. Only reviewed translations compile into the prod catalogue ([translation workflow](../../shared/i18n/docs/translation-workflow.md)).
- Light theme only: white page, mint wash, pastel tints, Lucide icons, Anek and Figtree fonts.
- The bright, Zomato-style look moved from a trial into the customer app as a skin (D-249) and into the shop app (D-261). The site follows the same look (D-262).

## How it works

```mermaid
flowchart LR
    Src[messages/app/area.en.json] --> Gen[make gen: compile ICU]
    Tr[hi, kn, te, ta files with review state] --> Gen
    Gen --> Cat[compiled catalogues per app]
    Cat --> App[react-intl in the app]
    Tok[shared/tokens] --> Css[CSS variables]
    UI[shared/ui components] --> App
    Css --> App
```

`shared/tokens` generates CSS variables, fonts and icons. `shared/ui` holds the components (StoreCard, ProductCard, BottomNav, PosCart, StatusPill and more). Design source and generators live in [design](../../design/).

## Code

| Part | Where |
|---|---|
| Shared | [shared/i18n](../../shared/i18n/), [shared/tokens](../../shared/tokens/), [shared/ui](../../shared/ui/) |
| Design | [design/source](../../design/source/), [design/generators](../../design/generators/), [design/trials](../../design/trials/) |
| Backend | None. Language is stored in the profile; templates are code-owned. |

## Tests

Pseudo-locale checks, the `check-i18n-messages` lint, visual baselines in [e2e/visual](../../e2e/visual/), accessibility checks in [e2e/a11y](../../e2e/a11y/). Journey `08-e2e-15-language`. Reports: [W4-04](../qa/W4-04/REPORT.md), [W4-05](../qa/W4-05/REPORT.md), [W4-05-hindi-customer](../qa/W4-05-hindi-customer/), [D-261](../qa/D-261/).

## Status and known gaps

- Hindi, Kannada, Telugu, Tamil copy awaits review by native speakers before it counts as reviewed.
- Admin and most site pages are English only.

## Decisions

D-58, D-87, D-103, D-247, D-248, D-249, D-250, D-260, D-261, D-262.
