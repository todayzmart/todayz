# TodayZ end of day, 24 Oct 2026

Everything new since the 14:00 set (`docs/qa/founder-2026-10-24/`), which stays as it was. The screens come from local runs on faked APIs or seed data. No real shop or customer is in them.

| Sheet | What it shows |
|---|---|
| `1-shop-counter-pos.png` | The shop counter (POS). Build a bill, take cash, UPI or card, show the receipt. Sales are saved on the device while offline and sync later. Desktop and phone layouts. |
| `2-shop-app-stock-offers-setup.png` | The shop app. Stock list, offers (list and new offer), banners, credits, first-time setup, stores, settings and the QR standee. |
| `3-orders-partial-and-limits.png` | Orders. A shop that is short on stock accepts part of an order and the customer takes the new total or cancels. A customer who missed pickups is limited to one open order. Search says when no shop has confirmed its stock lately. |
| `4-website-landing-v2.png` | The new landing page, live on todayz.in (desktop hero plus the mobile sections). |

## What works

- **Customer core loop and baskets** (in the 14:00 set): search nearby, book, pick up, the wallet.
- **Shop counter (POS)**: billing, payment choice, receipt, offline sales queue.
- **Shop app**: stock, offers, banners, credits and the setup screens.
- **Order rules**: partial accept with a timer, the no-show limit, auto-accept, the timer sweep, the stale-stock notice. Merged to `main` today.
- **Website**: the new landing page is live on todayz.in. The four sites run on Cloudflare Pages.
- **Wave 2 gate**: 21 end-to-end journeys pass, 0 fail, 7 are marked to do.

## In progress

- **Delivery and returns** (W2-02 delivery session). Being built. I review it before it merges because it touches money and timers.
- **Wave 2 gate merge** (W2-13). Waiting on one last full end-to-end run, then I review and merge it.
- **API on AWS**. Not up yet because the AWS account's SCP blocks it, and you are handling that. Until then the live apps load but sign-in fails.
- **CI hardening on GitHub**. The contract fuzzing fixes (D-217) are pushed and their CI runs are queued.
- **Map tiles**.
