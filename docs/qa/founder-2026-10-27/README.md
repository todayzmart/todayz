# Founder screenshots, 27 Oct 2026 (end of day)

Five contact sheets built from today's QA screenshots. Sheets 1 to 4 come from local `main`. Sheet 5 comes from the seller-onboarding branch, which is not merged yet. None of it is in prod: local `main` is 15 commits ahead of `origin/main`, and prod was last deployed about 12 hours ago.

| Sheet | What it shows | Source |
|---|---|---|
| `1-customer-new-look-phone.png` | Customer app in the quick-commerce look on a phone: home, search, shop, shop with cart, product, and the pastel shop types on todayz.in | `docs/qa/customer-redesign/after/` |
| `2-customer-new-look-desktop.png` | The same look on desktop | `docs/qa/customer-redesign/after/` |
| `3-demo-catalogue.png` | Demo catalogue with real product photos and credits | `docs/qa/demo-catalogue/` |
| `4-shop-app-counter.png` | Shop app: counter sales in Orders; the POS stops at the stock count and asks for a shelf count | `docs/qa/D-268-counter-sales/`, `docs/qa/D-270-pos-stock-cap/` |
| `5-seller-onboarding-branch.png` | **In progress.** Seller self-onboarding on a dark green rail, the dark green shop-app sidebar, and the admin's daily shop-check queue with reject | branch `claude/seller-auto-onboarding-fa62d0`, `docs/qa/seller-auto-onboarding/` |

## What works (local, on main)

- **Counter sales in Orders (D-268).** Counter sales sit next to online orders, with filters, totals, CSV export and a bill you can reverse.
- **No item goes below zero at the counter (D-270).** The POS stops at the stock count. Instead of a short-stock warning, the shop types what is on the shelf now.
- **Demo catalogue (D-271).** A local catalogue built from Open Food, Beauty and Products Facts. Every photo carries its credit and licence.
- **Quick-commerce look for customer discovery (D-269).** Home, search, shop and product pages, plus the pastel shop-type tiles on the landing page.
- **Your phone walk-through fixes (D-272).**
  - Even padding.
  - Photos fill their frame at the same size.
  - The map always shows.
  - Cart lines have photos.
  - A total with no rows above it has no rule over it.

## In progress or waiting

- **Seller self-onboarding (D-274).**
  - A shop goes live as soon as onboarding is filled in.
  - Payouts wait until a field agent or admin checks the shop.
  - A reject hides the shop and cancels its open orders.
  - Built and tested on its branch, with your two answers recorded: payee edits don't re-hold, and an agent may check a shop they onboarded.
  - Merged into local `main` on 28 Oct (`b118c86a`). The rail counts all 7 steps (`f8c91be0`).
- **Credits wording.** Next, as you chose: the credits row is hidden when the balance is 0, and the copy says items of ₹100 or more.
- **DeepSeek's quick-commerce theme (D-273).** Committed as `748eb5f6` with your choices: crop-to-fill (D-272) and no retailer photos. 344 demo products now show initials until open or owned photos are added.
- **Firebase phone sign-in (D-275).** Merged into local `main` on 28 Oct (`2eafaeb1`). `OTP_PROVIDER` defaults to `sms`. Real SMS waits for billing on `todayz-prod`.
- **Customer Home speed.** Lighthouse on a phone gives 58–59 with the map and 81 without it, against a target of 90. The new look's photo rails are the main cost (3.4 s LCP, 0.13 CLS), so this is follow-up work. The target stays at 90.
- **Prod deploy.** Not pushed, as you asked.
