# TodayZ build progress, 23 October 2026

TodayZ lets neighbourhood shops put their stock online so people nearby can find an item, book it and pick it up. The foundation is done and the first screens are working. Everything runs locally; nothing is deployed yet.

## What we can show today

- **Shop owner's dashboard (business app).** Desktop and phone. It runs on sample data and is labelled that way. Its buttons show a notice and change nothing. It shows:
  - today's sales against last week;
  - new orders with a countdown to accept;
  - demand insights (what sells fast, what is about to expire, what people nearby want);
  - top items;
  - shop setup progress.
- **New marketing site.**
  - Pages: home, for retailers, pricing, how it works, contact and the legal pages.
  - A lead form for shops.
  - English is final; Hindi, Kannada, Telugu and Tamil are machine drafts awaiting native review.
- **Design system.** 32 reusable components that every app is built from, for example:
  - product and store cards;
  - the map of nearby shops;
  - the till cart and pickup codes;
  - the order timeline;
  - dashboard tiles.

Screenshots:
- `docs/qa/B-03-demo/`: dashboard
- `docs/qa/W1-09/screens/`: site
- `docs/qa/W1-02/screens/`: components

## Progress by stage

| Stage | What it delivers | Packages | Status |
|---|---|---|---|
| 0 · Foundation | Local servers, backend skeleton, API contract, safe shutdown, test and release tooling, stand-ins for SMS, WhatsApp and AI | 8 of 8 | Done |
| 1 · Platform | Login, shop sign-up, catalogue and stock, nearby search, design system, languages, monitoring, website | 3 of 11 | In progress |
| 2 · Core flows | Bookings, pickup codes, the shop till, credits, referrals, notifications, delivery, customer and shop screens | 0 of 13 | Next |
| 3 · Money, insights, AI | Payouts, fraud rules, demand insights, voice search, AI drafts, admin console, dashboards, alerts | 0 of 16 | Later |
| 4 · Hardening | End-to-end journeys, load, security and accessibility testing, translations, manual QA | 0 of 7 | Later |
| 5 · Cloud and launch | AWS and Cloudflare, staging, production | 0 of 5 | After stage 4 |

The dashboard was built ahead of its stage for this demo; it moves to live data in stage 3.

## Quality so far

- **Full check on main:** every step passes (backend, contract, mocks, frontend, i18n, UI kit and CI). It found one false positive in the secret scan, now fixed.
- **Automated tests:** over 700 pass across the parts built:
  - UI kit: 354
  - Site: 172
  - Translations: 197
  - Business app: 41
  - plus the Go backend tests
- **Accessibility:** no problems found on the dashboard or on 156 component screens.
- **Speed:** Google Lighthouse scores the English home page 100 on mobile.
- **Data:** no real customer data is used anywhere; all screens show sample shops and products.

## Not real yet

- There is no login, ordering or payment yet; those arrive in stages 1 and 2.
- The dashboard shows sample numbers.
- The Indian-language copy is machine translated.
- Nothing is online; the site and apps run on the build machine only.

## Decisions we need

- Legal sign-off on the privacy and terms pages, and a named grievance officer.
- Native reviewers for Hindi, Kannada, Telugu and Tamil.
- The old website's page list, so its links keep working after the switch.
- The real anti-bot key (Turnstile) and the API address for the site form.
- Cloud accounts (AWS and Cloudflare) when we reach stage 5.

## Plan for this weekend (chosen 23 October)

We cut the weekend to the core loop: the smallest version of TodayZ that works from start to finish.

**Goal by Sunday night, 25 October, running locally:**
1. A customer signs in with an OTP and searches what is in stock nearby.
2. The customer books an item and gets a pickup code.
3. The shop sees the new order straight away, accepts it, and hands it over at the counter after checking the code.

**How we get there:**

| When | Work |
|---|---|
| Friday evening (started) | Sign-in core, background jobs, monitoring, cloud setup files (written, not applied) |
| Saturday | Admin sign-in and account deletion, shop sign-up and stores, catalogue and stock, nearby search, app shells with sign-in |
| Saturday night to Sunday | Booking and pickup codes, live order alerts to the shop, the customer's search, shop and checkout screens, the shop's order screen, one end-to-end test of the whole loop |

**Not in the weekend cut:**
- The shop till (POS) and offline sales.
- Credits, cashback, referrals, offers and reviews. Booking uses simple stand-ins for credits and offer pricing until these are built.
- Payouts, fraud rules, AI features, voice search and the admin console.
- The full test suite.

**Afterwards:** the rest of stage 2, then stages 3 and 4, take about 2 to 3 weeks. Cloud launch follows about a week after that, once the accounts are ready.

**Risks:**
- The sign-in, shop and catalogue packages run one after another, so a delay in sign-in pushes everything behind it.
- Usage limits paused work once today.

Every piece is still reviewed and tested before it joins the main build; we are cutting scope, not checks.

## Full plan timeline (for reference)

The first target was stages 0 to 4 within a day, which was not realistic: stage 0 alone took about a day and a half, because each piece is reviewed and fixed before the next one builds on it.

| Stage | Estimate |
|---|---|
| 1 · Platform (8 packages left) | 3 to 4 days |
| 2 · Core flows | about 1 week |
| 3 · Money, insights, AI | 1 to 1.5 weeks |
| 4 · Hardening | 4 to 5 days |
| 5 · Cloud and launch | about 1 week, once the accounts are ready |

A fully tested product running locally (end of stage 4) is about **3 to 4 weeks** away. Launch in the cloud follows about a week after that.
