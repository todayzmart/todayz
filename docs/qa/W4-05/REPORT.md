# W4-05 translations, what is left: report

Date: 26 Oct 2026. Branch `worktree-agent-ae20693cd0a077cc2`. Nothing pushed.

## 1. Coverage (keys with a `reviewed` value per language)

Before: keys missing or still machine-only; after: what is left. All four languages (hi, kn, te, ta) have the same counts except where stated.

| App | English keys | Before | After | Left |
|---|---|---|---|---|
| customer | 775 | hi 692, kn/te/ta 691 | 775 in all four | none |
| business | 1955 | 1955 | 1955 | none |
| site | 349 | 261 | 299 | `legal` (50 keys) |
| admin | 2030 | 65 | 65 | 1965 keys per language |

Drafted here (machine-written, then approved with `translations.mjs approve`, see "Approval"):

- customer `address` (43 keys, new files), `delivery` (29, new), `checkout` (+3: `checkout.fulfilment.label`, `checkout.delivery.title`, `checkout.payNote.delivery`), `orders` (+8 hi, +9 kn, te, ta; the extra key is `orders.callShop`).
- site `info` (28, new) and `shell` (10, new).

Not drafted, English shows:

- admin (1965 keys x 4 languages). It is the internal console and the volume is large; the lead should decide if it is worth a pass. The admin screenshots below are therefore English in every language folder.
- site `legal` (50 keys x 4). Legal text needs a proper review, so I did not machine-write it.

No key is `machine` any more in customer, business or site; the only gaps are the two above.

## 2. Glossary

`shared/i18n/glossary.md`: one table of 26 terms across hi, kn, te, ta, the rules for choosing a term, the changes made, and the open questions.

Inconsistencies fixed (majority use was clear), 148 messages edited by script plus 1 by hand (`site/home` `home.pos.bill`):

| Change | Messages |
|---|---|
| te shop: షాపు/షాప్ to దుకాణం (site) | 53 |
| te shop: షాప్ to దుకాణం (business) | 14 |
| te bill: బిల్ to బిల్లు (business `ai.bill.*` 9, plus `home.pos.bill`) | 9 + 1 |
| te plural: క్రెడిట్లు to క్రెడిట్‌లు (ZWNJ, majority) | 15 |
| te plural: ఆర్డర్‌లు to ఆర్డర్లు | 31 |
| te plural: కస్టమర్‌లు to కస్టమర్లు | 22 |
| te plural: ఆఫర్‌లు to ఆఫర్లు | 3 |
| ta pickup: பிக்கப் to பிக்அப் (`business/help` `help.faq.code.q`) | 1 |

One message can match more than one rule. The edits touch 29 tracked files under `shared/i18n/messages/` (mostly `*.te.json`). Review state and hashes were not changed by these edits (the English did not change); `lint:messages` is clean.

Flagged, not fixed (no clear majority, or a native speaker's call): cancel (customer and business differ in hi, kn, te), hi price (दाम and क़ीमत), ta sign in, ta code (கோடு and குறியீடு), kn and ta refund (loanword and native word in the same site messages), ta payout, hi ask (noun), hi accept. Details with counts are at the foot of the glossary.

## 3. Screenshots

`docs/qa/W4-05/screens/<app>/<lang>/<screen>.png`, 95 PNGs, about 6 MB in total (customer 0.95 MB, business 1.5 MB, admin 1.7 MB, site 1.9 MB). The Playwright `shot` helper writes the PNGs; there is no compression step in the tooling.

| App | Screens | Viewport |
|---|---|---|
| customer | home, search, cart, sign-in, help, language | phone 390 |
| business | overview, orders, stock, offers, customers, help (signed in, one shop, no orders) | phone 390 |
| admin | pulse, customers, orders, settings, support (signed in as super admin, empty lists) | desktop |
| site | home, for-retailers (the only pages that exist in hi, kn, te, ta) | phone 390, full page not used |

New specs (no app code changed): `customer/e2e/i18n.shots.spec.ts`, `business/e2e/i18n.shots.spec.ts`, `admin/e2e/i18n.shots.spec.ts`, `site/e2e/i18n.shots.spec.ts`. Each runs only when `TZ_LANG` and `TZ_QA_DIR` are set, so the normal suites skip it. To refresh one language of one app (after `pnpm build:e2e` with `TODAYZ_I18N_DRAFTS=1`):

```
cd customer
TZ_LANG=hi TZ_QA_DIR=docs/qa/W4-05/screens/customer/hi pnpm exec playwright test e2e/i18n.shots.spec.ts
```

The language is chosen with `localStorage 'todayz.lang'` (customer, business, admin) or the path prefix (site). The pages are loaded against `vite preview` or `astro preview` on a random free port, with the API answered by mocks; no stack was started.

Observations from the screenshots I opened (customer home in Tamil, business overview in Telugu): both render in the right script. I did not review every picture; a native-speaker pass on the screens is still needed. In the Tamil home the location chip text is clipped at the right edge on a 390 px phone ("உங்கள் இருப்பிடத்தை அமைக்க"), which looks like a layout limit for longer Tamil; not changed here.

## 4. Checks

- `pnpm -C shared/i18n lint:messages`: 0 errors in customer, business, admin and site (warnings are the untranslated admin and site legal keys, plus unreferenced keys).
- `pnpm -C shared/i18n exec vitest run`: 4 files, 198 tests passed.
- Playwright: the four new specs, 5 languages each, all passed (customer 6 screens, business 6, admin 5, site 2 per language).
- `make gen` could not run in this worktree (`sqlc` and `oapi-codegen` are not installed here), so I ran the frontend generators only (`api-client`, tokens, i18n with `TODAYZ_I18N_DRAFTS=1`, tsr, astro sync). Generated output is git-ignored.

## 5. Approval

The new customer and site drafts were approved with `translations.mjs approve` under the founder's standing 25 Oct instruction ("go live with hindi", then "and all other languages too"), as relayed by the lead. There was no native-speaker review, Hindi included. The row is recorded in `docs/qa/W4-05-hindi-customer/REVIEW.md`. The lead should confirm it before the merge, since I could not ask the founder.

## 6. Follow-ups for the lead

- Decide whether admin (1965 keys) and site `legal` (50 keys) get drafts, and if so who approves them.
- Pick one word per language for "cancel" (see glossary).
- Check the Tamil clipping noted above.

## Appendix: per-message list of glossary fixes

Format: rule | app/area key. The hand edit `site/home` `home.pos.bill` is not in the script log.

```
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common nav.retailers
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common nav.cta
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common footer.terms_shops
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common lead.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common lead.shop
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common lead.err_shop
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common cta.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common cta.body
te shop: షాపు/షాప్ -> దుకాణం (site) | site/common cta.tick.1
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.meta.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.meta.description
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.hero.kicker
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.hero.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.hero.tick.1
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.hero.scene
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.hero.note
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.mock.shops_have
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.problem.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.problem.intro
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.problem.4.body
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.problem.no_shops
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.insight.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.insight.flow.3.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.features.eyebrow
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.features.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.features.intro
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.features.4.body
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.showcase.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.insights.intro
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.insights.kind.1.body
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.shoppers.intro
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.shoppers.2
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.shoppers.body.3
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.shoppers.body.4
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.shoppers.avail
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.shoppers.book
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.steps.1.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.steps.1.body
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.steps.shop_name
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.types.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.types.intro
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.types.strip
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.testimonials.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.faq.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.faq.1.a
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.faq.2.a
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.faq.3.a
te shop: షాపు/షాప్ -> దుకాణం (site) | site/home home.faq.4.q
te shop: షాపు/షాప్ -> దుకాణం (site) | site/retailers retailers.meta.title
te shop: షాపు/షాప్ -> దుకాణం (site) | site/retailers retailers.hero.body
te shop: షాపు/షాప్ -> దుకాణం (site) | site/retailers retailers.faq.1.a
te shop: షాపు/షాప్ -> దుకాణం (site) | site/retailers retailers.faq.2.q
te shop: షాపు/షాప్ -> దుకాణం (site) | site/retailers retailers.faq.3.a
te shop: షాప్ -> దుకాణం (business) | business/asks asks.muted
te shop: షాప్ -> దుకాణం (business) | business/asks asks.reply.priceOptional
te shop: షాప్ -> దుకాణం (business) | business/asks asks.reply.noPriceCashier
te shop: షాప్ -> దుకాణం (business) | business/asks asks.report.body
te shop: షాప్ -> దుకాణం (business) | business/asks asks.error.invalid_transition
te shop: షాప్ -> దుకాణం (business) | business/coach coach.title
te shop: షాప్ -> దుకాణం (business) | business/coach coach.desc
te shop: షాప్ -> దుకాణం (business) | business/coach coach.aiWording
te shop: షాప్ -> దుకాణం (business) | business/voice voice.noStore.title
te shop: షాప్ -> దుకాణం (business) | business/voice voice.noStore.body
te shop: షాప్ -> దుకాణం (business) | business/voice voice.noStore.action
te shop: షాప్ -> దుకాణం (business) | business/voice voice.off.body
te shop: షాప్ -> దుకాణం (business) | business/voice voice.error.ai_daily_cap_reached
te shop: షాప్ -> దుకాణం (business) | business/voice voice.error.forbidden
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.title
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.desc
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.reading
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.failed
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.off
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.lines
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.matched
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.unmatched
te bill: బిల్ -> బిల్లు (business ai, site home.pos.bill) | business/ai ai.bill.another
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.title
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.loading
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.noStore.body
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.manager.body
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.account.credits
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.lines.empty.body
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.line.spent
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.payouts.in
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.payouts.out
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.referral.body
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.referral.earned
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/credits credits.referral.earnedValue
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/settings settings.rewards.example
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/shell shell.nav.credits
te plural spelling: క్రెడిట్లు -> క్రెడిట్‌లు (ZWNJ, majority) | business/statements statements.totals.creditsIn
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/analytics analytics.kpi.bills
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/analytics analytics.funnel.title
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/analytics analytics.funnel.empty
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/analytics analytics.funnel.missed
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/analytics analytics.accept.p50
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/analytics analytics.accept.none
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/help help.how.orders.title
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/help help.how.orders.body
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/help help.how.pickup.body
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/help help.faq.timer.a
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/help help.faq.staff.a
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/help help.new.order.help
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/help help.new.order.failed
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/overview overview.gate.cashier
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | business/overview overview.gate.orders
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/auth account.signedOut.body
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/checkout checkout.problem.store_not_bookable
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/help help.new.order.failed
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/help help.new.order.help
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/help help.new.subject.help
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.title
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.signedOut.title
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.signedOut.body
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.empty.title
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.loading
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.loadError
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.items.label
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.limit.title
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/orders orders.limit.body
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | customer/shell shell.nav.orders
te plural spelling: ఆర్డర్‌లు -> ఆర్డర్లు (no ZWNJ, majority) | site/info info.pricing.point.2
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/analytics analytics.repeat.title
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/analytics analytics.repeat.note
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/analytics analytics.repeat.notEnough
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/analytics analytics.insights.needs.regulars_slipping
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/customers customers.title
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/customers customers.error
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/customers customers.notEnough.title
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/customers customers.notEnough.body
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/customers customers.about.window
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/customers customers.about.slipping
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/customers customers.empty
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/help help.how.stock.body
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/help help.how.counter.body
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/help help.how.credits.title
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/help help.how.credits.body
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/help help.faq.credits.q
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/help help.faq.visible.q
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/overview overview.insights.stock_check_due.sentence
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/overview overview.insights.regulars_slipping.label
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/overview overview.insights.regulars_slipping.sentence
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/overview overview.insights.action.see_customers
te plural spelling: కస్టమర్‌లు -> కస్టమర్లు (no ZWNJ, majority) | business/overview overview.setup.next.products
te plural spelling: ఆఫర్‌లు -> ఆఫర్లు (no ZWNJ, majority) | business/help help.faq.staff.a
te plural spelling: ఆఫర్‌లు -> ఆఫర్లు (no ZWNJ, majority) | customer/cart cart.discount
te plural spelling: ఆఫర్‌లు -> ఆఫర్లు (no ZWNJ, majority) | customer/search search.filter.offers
ta pickup: பிக்கப் -> பிக்அப் | business/help help.faq.code.q
```
