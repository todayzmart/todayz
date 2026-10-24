# TodayZ screenshots for the founder, Saturday 24 October 2026

Everything runs locally on the build machine with synthetic seed data. Nothing is deployed yet.

| Sheet | What it shows | State |
|---|---|---|
| `1-customer-core-loop.png` | Sign in, home, search "atta", shop, checkout, waiting, pickup code, collected | Works end to end against the real backend |
| `2-customer-baskets-maps.png` | Saved baskets; one shop that has everything; a split across two shops; fill cart. New: the map card, the full map, the category page | Baskets done. Maps are in progress; their tiles are blank in test mode |
| `3-shop-app-orders.png` | A new order arrives live; short stock blocks Accept; ready; handed over with the code; store picker; admin console | Works end to end |
| `4-website.png` | Marketing site: home, for retailers, pricing (also in Hindi, Tamil, Telugu, Kannada) | Done |

The individual screenshots are in `docs/qa/demo-2026-10-25/`, `docs/qa/W2-14/` and the W2-08/09 worktree.

**Building now:**

- credits and wallet
- the shop counter (POS) with offline billing
- offers and banners
- reviews
- referrals
- the shop's stock, products, onboarding and settings screens

**Plan:**

- Wave 2 is done around Tue 27 Oct.
- Go-live is around 22–28 Nov, once cloud credentials and the SMS (DLT) and WhatsApp approvals are in.
