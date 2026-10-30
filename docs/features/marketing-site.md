# Marketing site

The public site at todayz.in: pages, languages, lead form, the demand marquee and the performance gates.

> As built on base commit e986a157 (30 Oct 2026), with Liquid Silver in this change.

## What users can do

| App | What |
|---|---|
| Site visitor | Read the landing page, how it works, pricing, about, contact, legal pages. Switch language on the home and for-retailers pages. Send a lead (a shop asking to join). |
| Backend | The public lead form posts to `public/leads` ([admin-leads](../../backend/api/paths/admin-leads.yaml) lists them). The marquee reads `public/site-marquee` ([share.yaml](../../backend/api/paths/share.yaml)). |

## Pages

| Path | Page id | Languages |
|---|---|---|
| `/` | home | all five |
| `/for-retailers/` | retailers | all five |
| `/how-it-works/`, `/pricing/`, `/about/`, `/contact/` | how, pricing, about, contact | English |
| `/legal/privacy/`, `/legal/terms-for-shoppers/`, `/legal/terms-for-shops/`, `/legal/refunds-and-credits/` | privacy, terms-shoppers, terms-shops, refunds | English |

The page registry is [site/src/lib/pages.ts](../../site/src/lib/pages.ts). Views are in [site/src/views](../../site/src/views/) (Home, Retailers, Info, Legal, NotFound); components in [site/src/components](../../site/src/components/); styles in `site/src/styles/bright.css`. Pages render from `src/pages/[...slug].astro`, plus `robots.txt` and `sitemap.xml` endpoints.

## Rules

- Astro builds static pages. Draft locales are dropped from the prod build (`drop-draft-locales`, D-102).
- The landing follows the founder's bright trial on every page (D-262). The hero has no photo: dark text on the mint wash, with a smaller title (D-267). Shop types are pastel tiles with a cut-out CC0 or public-domain photo on the type's tint (D-269, replacing D-260's round photos), styled by `.bp .b-types` in `bright.css`; sources in `site/src/assets/photos/SOURCES.md`.
- Lighthouse gates are in `lighthouserc.json` (D-102). The `/kn/` pages have a recorded exception (D-262, see commit history).
- The marquee shows searches from at least k=10 people in a window (`site_marquee_min_searchers`, `site_marquee_window_days`, `site_marquee_max_items`).

## How it works

```mermaid
flowchart LR
    Pages[pages.ts + views] --> Build[astro build]
    Msg[shared/i18n site messages] --> Build
    Build --> Dist[dist: static HTML]
    Dist --> Chk[check-site, verify-dist, lighthouse]
    Dist --> CF[Cloudflare Pages on push to main]
    Visitor[Visitor browser] --> CF
    Visitor -->|lead, marquee| API[API public endpoints]
```

## Code

| Part | Where |
|---|---|
| Frontend | [site](../../site/) with scripts in [site/scripts](../../site/scripts/) (check-site, verify-dist, lighthouse, og, headers) |
| Backend | `backend/internal/share` ([folder](../../backend/internal/share/)) builds the marquee (`site_marquee_build`); `backend/internal/business` stores leads |
| Deploy | Cloudflare Pages from GitHub Actions on push to main (D-214); see the lead's deploy docs |

## Tests

Unit tests in `site/src/lib/*.test.ts`, Playwright in [site/e2e](../../site/e2e/), the check-site and Lighthouse scripts. Reports: [W1-09](../qa/W1-09/), [W4-04](../qa/W4-04/REPORT.md), [customer-redesign](../qa/customer-redesign/REPORT.md) (shop-type tiles).

## Status and known gaps

Live in code. Legal pages are English only (B6). The Kannada phone Lighthouse score was merged as is on the founder's word.

## Decisions

D-102, D-103, D-214, D-247, D-248, D-260, D-262, D-269.

## Liquid Silver presentation — 30 Oct 2026

As built on base commit e986a157, with this presentation change. The founder requested the customer app's selected Liquid Silver direction on todayz.in. Base.astro loads styles/silver.css on every route: silver page and neutral semantic tokens, graphite actions, frosted header and search, translucent cards, and silver image wells. Original product photos and semantic status/category colours remain available. Home's existing scroll reveals use a slower easing curve; hero elements enter in sequence, the hero light drifts once, and cards and photos respond to hover. All new motion respects reduced motion. No additional client script or dependency is added. Locale typography, content, forms, navigation and SEO use the existing implementation.

The stylesheet uses the opt-in silver-* shared tokens introduced by the customer design. Deployment uses the existing main-branch Cloudflare Pages workflow; automatic CI remains paused as requested separately. Static token checks and whitespace validation are performed locally; production compilation is verified by the deployment workflow. Browser visual review remains pending.
