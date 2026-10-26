# Founder screenshots, 26 Oct 2026 (end of day)

Seven contact sheets built from today's QA screenshots on `main`. All of today's side branches are now merged into local `main`. None of it is in prod yet: local `main` is 90 commits ahead of `origin/main`, and prod was last deployed about 11 hours ago.

| Sheet | What it shows | Source |
|---|---|---|
| `1-customer-app.png` | Customer app with live demo data: home, search, shop, product, cart, orders, account, map | `docs/qa/demo-2026-10-26/` |
| `2-customer-new-today.png` | Profile photo (add, change, remove; shows in the Home header) and the AI helpers (search assist, voice search, list paste, substitutes) | `docs/qa/D-259/`, `docs/qa/W3-12/` |
| `3-shop-app-bright.png` | Shop app in the bright look: the old look first, then overview, orders, POS, stock list, item sheet, offers, shop coach | `docs/qa/D-261/` |
| `4-shop-and-admin-desktop.png` | Shop app and admin console on desktop: overview, live orders, POS, analytics, pulse, stores, risk, payouts | `docs/qa/demo-2026-10-26/` |
| `5-product-photos.png` | Product photos: the shop's optional photo, stock thumbnails, the admin review queue with bulk approve, admin upload | `docs/qa/product-images/` |
| `6-website-and-languages.png` | todayz.in in the bright look (desktop, phone, the shop-type photos, Hindi), and the apps in Hindi, Telugu, Tamil and Kannada | `docs/qa/landing-v2/`, `docs/qa/W4-05/` |
| `7-design-check.png` | The shop overview next to the design card it was checked against | B-03 demo (untracked, in a worktree) |

## What works (local, on main)

- **Core loop, end to end.** Find, book, pick up or delivery, across the customer app, shop app and admin console.
- **Bright look.** The customer app (D-249), the shop app (D-261) and the landing site (D-260). axe WCAG 2.2 AA is clean on all 18 shop-app shots.
- **Profile photo (D-259).** Add, change and remove. The photo shows on Account and in the Home header, only to that customer.
- **Product photos (D-252, D-253).**
  - A shop can add a photo when it creates a product, or send one from the item sheet.
  - Stock photos come from Open Food Facts and Wikimedia Commons, with licence and credit.
  - Every photo is a draft until an admin approves it.
  - Commons found candidates for 215 of 251 kinds of item.
- **Customer AI helpers (W3-12).** Search assist, voice search, list paste and substitutes. Every AI answer is a draft the customer taps to confirm.
- **Languages (W4-05).** The customer, shop and admin apps and the site in Hindi, Kannada, Telugu and Tamil.
- **Admin sign-in without the authenticator, locally only (D-244).** It refuses to turn on in prod.
- **Merged today:**
  - go-live gaps (D-251, B1–B10);
  - deploy prep (D-254–D-258; nothing applied);
  - load fixes (W4-02);
  - accessibility and visual checks (W4-04).

## In progress or waiting

- **Prod deploy.** The 90 local commits are not pushed yet. The paid-tier AWS plan is ready but waits for your yes on the plan summary; nothing has been applied.
- **Shop-app visual baselines.** They still show the old look. They will be refreshed once you approve the bright look.
- **Shop app on a desktop.** The bright sidebar is built but has no screenshot yet.
- **Product photos.** 36 kinds of item have no Commons candidate yet. Open Food Facts returned 503 for 2 items in the trial.
- **Needs you:**
  - DLT IDs and AWS SMS sandbox exit;
  - SES production access;
  - a live-key AI eval;
  - a manual check on a real phone.
