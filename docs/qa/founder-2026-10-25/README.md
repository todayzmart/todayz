# Founder update, Sun 25 Oct 2026 (end of day)

Screens added today. They come from the QA screenshots on `main` and from the worker branches still being built.

## What works (live on todayz.in after each push, or merged and going live)

| Sheet | What it shows | State |
|---|---|---|
| `1-customer-delivery-languages.png` | Home delivery for customers: choose delivery in the cart, saved addresses, out-for-delivery tracking, a "shop not delivering now" state. The customer app in Hindi and Telugu. | Live |
| `2-shop-app-insights.png` | Shop app on desktop: Overview (today's sales, orders, new orders to accept), Demand insights (selling fast, going to waste, sitting on the shelf, wanted nearby), Analytics (sales and all insights), Customers (regulars slipping away), QR standee campaigns | Demand insights pushed today (W3-03); the rest live |
| `3-shop-app-phone.png` | The same shop screens on a 360 px phone, plus the counter sales list | Live |
| `4-admin-risk.png` | Admin console: risk flag queue, a flag that holds a shop's payout until staff clear it, referral review | Merged; it goes live once its checks pass |

Also live today: the AI platform base (W3-10). It has no screens of its own. It provides the shared AI plumbing: a cost guard, kill switches, drafts a person confirms, and recorded tests.

## In progress

| Sheet | What it shows | State |
|---|---|---|
| `5-in-progress-voice.png` | Update stock by voice. The shopkeeper holds to talk ("sold 2 Maggi, 10 Amul milk came in"). The app shows a draft line by line, asks which item was meant, and saves only after the shopkeeper confirms. Lines that change a price need a manager. | Being built (W3-04) |
| `6-in-progress-support.png` | Support inbox for staff: ticket list and thread | Being built (W3-08); its shop-app help screens are being redone |

Also being built: the shop coach and shop AI (W3-11: bill scan, shelf photo, coach cards).

## Still to come

- Wave 3: field agents, consumer AI, ask shops nearby, ops console, the AI work that depends on support, then the Wave 3 sign-off gate.
- Wave 4 after that. AWS (API hosting) waits for your go-ahead.
