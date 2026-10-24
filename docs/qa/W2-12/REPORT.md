# W2-12 Business stock, products, offers, reviews, credits: QA report

Branch `claude/wp-w2-12-biz-stock-b2b5e9`, in `business/`. Everything ran locally and offline; no cloud account, no `make dev`, no stack e2e. API calls in the tests and screenshots are faked at `fetch`.

## What landed

| Screen | Route | State |
|---|---|---|
| B-07 Stock | `/stock` | Generated client (`catalogue`). Create-product and bulk import still on an adapter. |
| B-08 Offers and banners | `/offers` | Generated client (`offers`, `banners`, `catalogue` batches), per D-155. |
| B-11 Reviews | `/reviews` | Generated client (`reviews`), per D-141. |
| B-10 Credits and referrals | `/credits` | Adapter (`credits/api.ts`): W2-01 is not merged. |

- **Stock (B-07).** Desk with search, filter chips and sortable table. Item sheet with price, listing and stock edits (If-Match), movements, and count mode. Add item and import sheets. Deep links per D-99 are prefill-only; unknown ids are ignored.
- **Offers (B-08).**
  - Offer list with status filter, redemptions, pause, resume and a two-step End (If-Match `"v{version}"`; a 412 reloads the list).
  - New offer for all five kinds: percent off (cap 90), rupees off per unit, buy and get free, bill discount (whole shop only), and expiring stock (one offer per ticked batch, one idempotency key each).
  - Preview comes from the server's `previewOffer`, debounced, with a phone-frame card.
  - Banners: template banners with headline, subline, tint, target and schedule. States shown: live, checking ("Checking image…"), paused, ended, "Removed by TodayZ: {reason}". Views, taps and orders per banner. The server's `banner_limit_reached` is shown, not a hard-coded 3.
- **Reviews (B-11).** Owner and manager only: a cashier is told to ask a manager and nothing is loaded. List, one reply (edit), report.
- **Credits (B-10).** Balance (can be negative), lines newest first with a cursor, payout explainer (01 §9), a statements slot (W3-01), the standee card (short code, preview and download of the A5 PDF fetched with the bearer token, so no token is in a URL), and the business referral card (copy, share). The referral code is never put in a URL, query key or log (test).
- **Fix found by the screenshots.** Item names in the Stock table were unstyled buttons (grey box); now a plain text button.

## Evidence

Run from `business/` unless noted, after the last code change.

| Check | Result |
|---|---|
| `pnpm exec eslint src` | clean |
| `pnpm exec tsc --noEmit` | clean |
| `pnpm run lint:css` | clean |
| `pnpm exec vitest run` | 163 tests, 22 files, all pass (stock 33, offers 27, credits 17, reviews, shell 15) |
| `pnpm --filter @todayz/i18n test` | 197 passed (includes the sentence-case lint on the new keys) |
| `pnpm run build:e2e` | dist ok, precache-check and dist-check pass |
| `pnpm run size` | initial JS 150.42 kB gzip (limit 250 kB); initial CSS 10.35 kB (limit 20 kB) |
| `pnpm run build` (production mode) | not run: it needs a real `VITE_TURNSTILE_SITE_KEY`; `build:e2e` covers the same code |

## Screenshots

`docs/qa/W2-12/`, 390 px wide, from `vite preview --mode e2e` on a random port with a faked API: `offers-list-390.png`, `offers-banners-390.png`, `offers-new-sheet-390.png`, `credits-390.png`, `stock-390.png`. Reviews has route tests only, no screenshot.

On a phone the Stock table scrolls sideways (the shared `DataTable`), so the on-hand and price columns are off-screen until swiped.

## Decision log

No new D-180 to D-189 entries. D-141 and D-155 were followed as written.

## Adapters still pending

Delete the file and swap to the generated client when the operation lands. The shapes below are this app's assumption.

- **Credits** (`business/src/credits/api.ts`, `TODO(merge)`: W2-01, W2-03, W2-10):
  - `GET /v1/biz/businesses/{bid}/credit-account?limit=&cursor=` → `{balance, items: [{id, kind, credits, at, ref: {kind: 'order'|'sale', label}|null}], nextCursor}`. Kinds: `spent`, `cashback`, `referral_reward`, `pos_reversal`, `payout`, `adjustment` (signed).
  - `GET /v1/biz/businesses/{bid}/referrals` → `{code, link, stats: {pending, rewarded, pendingCredits, rewardedCredits}}`.
  - `GET /v1/biz/businesses/{bid}/stores/{sid}/qr-standee.pdf` → PDF blob.
- **Stock** (`business/src/stock/api.ts`, `TODO(merge)`): `createProduct` (D-37, a shop-made master product) and the bulk item import (`createItemImport`, `applyItemImport`). Not in the generated client after the last `make gen`.
- **Statements slot** on B-10 is a "coming soon" card until W3-01 (`business/src/statements/`).

## Questions (`TODO(lead)`)

1. **Two-item deal deep link.** `offer=pair&items=` has no offer kind of its own (D-155 lists five). It opens a percent offer on both items. Say if that is right or the link should be dropped.
2. **Report reasons.** The contract takes the review report reason as free text (≤ 500). The screen offers `abusive`, `spam`, `fake`, `not_about_shop`, `other` and sends the code, then the optional note. Confirm the list.
3. **Standee link.** The "Standee options" link on B-10 is a plain `<a href="/standee">` because W2-10 owns that screen and the route may not exist on this branch.

## Contract gaps worth a backend look

- Stock: `add=<masterId>` deep link cannot be resolved (no get-product endpoint); count mode with `count=` ids does one GET per id; the movement contract has no actor field, so "who changed it" is not shown.
- Reviews: no server-side filters, no per-star distribution, no hidden state on a review, no ETag on a reply.
- Banners: no image upload operation (no presign), so only template banners can be made; the sheet says photo banners are not available yet. D-155's "uploaded banners held for review" is shown for banners the server holds, but this app cannot upload one.

## Outside the owned paths

- **`business/src/shell/nav.ts` (not edited, needs the lead):** Reviews, Credits and Offers have routes but no entry in the nav. Stock is already there.
- **`business/src/shell/Shell.test.tsx` (edited):** the "shows the placeholder for /$section" test now skips sections with a real page (`stock`, `offers`, `reviews`, `credits`). Same pattern as W2-10 used for `orders`. Drop or fold into the nav change if you prefer.
- `shared/i18n/messages/business/{stock,offers,reviews,credits}.en.json` are new files in the shared folder (the plan names this path for each area).

## Not verified

- No run against the real API or the dev stack; every API answer is a fake that follows the generated client's shapes.
- No physical-device check; the layout was seen at 390 px in Chromium only.
- Camera barcode scanning in Stock is deferred.
- Screen reader behaviour was not tried by hand (roles and labels are covered by the tests).
