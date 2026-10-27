# Customer redesign and landing shop-type tiles (D-269)

> As built on main at d641cbe6 (27 Oct 2026), branch `claude/customer-redesign-fb6725`.

The customer app's discovery screens now use common quick-commerce patterns, and the landing page's shop types are pastel photo tiles. Only the patterns are borrowed: no other app's images, logos, icons or words. Everything is drawn with TodayZ tokens: white page, mint wash, pastel tints, Lucide icons, Anek and Figtree, light theme only. Decision: D-269 in `docs/plan/12-decision-log.md`.

## What changed

### Customer app (`customer/`)

| Pattern | Where | Notes |
|---|---|---|
| Compact sticky header with location, a big search bar and a pickup/delivery mode | `home/Home.tsx`, `home/home.module.css` | All / Pickup / Delivery is a `Segmented` switch under the search. It sends `pickup` or `delivery` to `listDiscoverStores`, the same parameters the old filter chips sent. It is kept for the visit. The cart still asks pickup or delivery for each order (C-09), so no order rule changes. On a phone the three options share the row; from 640 px the switch sits beside the search. |
| Dense pastel category grid with product photos | `home/CategoryTiles.tsx`, `home/photos.ts` | 3 columns on a phone, 6 on desktop. A cut-out photo is multiplied onto the category's tint. Each category's subcategories follow as small tiles that link to C-06. |
| Horizontal product rails | `home/Rails.tsx`, `home/railShops.ts` | "From {shop}" for the two nearest open, bookable shops, "Deals near you" (biggest saving first) and "Under ₹99" (cheapest first). Each rail shows up to 12 tiles, with skeleton tiles while loading and "See all" to the shop page. |
| Product card with a clear price and ADD that turns into − n + | `store/ItemCard.tsx`, `store/tile.module.css`, `store/itemUi.ts` | One tile for the shop page, search results and rails. It has a square photo on a pastel tint (`tintOf(productId)`), an offer badge, a basket icon, and ADD on the photo's corner (`QuantityStepper`). Below the photo come the price with the struck MRP, the name, the pack and the stock proof (D-101). Out-of-stock tiles keep "See similar items". In-store-only shops keep directions. |
| Floating cart bar | `store/store.module.css`, existing `CartBarLink` | It is capped at 560 px and centred on wide pages. |
| Fast skeletons | `store/StorePage.tsx`, `home/Home.tsx`, `home/Rails.tsx` | The skeletons are shaped like the tiles: a square photo, then two lines of text. |
| Clean shop page | `store/store.module.css` | The grid is 2, 3 or 5 columns (phone, 640 px, 1024 px), and the page is 1120 px wide on desktop. |
| Clean product page | `product/ProductPage.tsx`, `product/product.module.css` | The price is large and the offer shows as a pill. From 768 px the page has two columns with a sticky photo. |
| Search results | `search/search.module.css` | Each shop's items scroll sideways as tiles. The page is 960 px wide on desktop. |

New text: `home.mode.label`, `home.mode.all`, `home.rail.from`, `home.rail.deals`, `home.rail.under`, `home.rail.seeAll` and `home.rail.seeAllAt` are in en, hi, kn, ta and te. The four translations are machine drafts (`review: machine`) and wait for the same native review as the rest of the catalogue. The switch reuses `home.filter.pickup` and `home.filter.delivery`.

Accessibility:
- The switch is a radio group named "Show shops for".
- Each rail is a `section` labelled by its heading.
- "See all" carries "See all items at {shop}".
- Stepper buttons are named "Add, {item}", "Add one, {item}" and "Remove one, {item}".
- Decorative photos have empty `alt`.
- `e2e/redesign.shots.spec.ts` runs axe on every screen at both widths and passes.

### Landing page (`site/`)

The For retailers "For every kind of shop" section had small round photos (D-260). It now has pastel tiles: each has a 16:10 tinted photo box and a cut-out photo, with the name and line below.
- **Code:** `components/home/ShopTypes.astro` wraps the list in a `.bp`. The styles are `.bp .b-types` and `.bp .b-type-photo` in `styles/bright.css`, tokens only, with 2 columns at 760 px and below. The old `.lp .types` and `.lp .t-photo` rules are removed from `landing.css`.
- **Hero:** the D-267 hero is untouched.

## Photos

All new photos are CC0 or public domain. Each one was found on Wikimedia Commons, with its licence read from the file page, except the pharmacy photo, which comes from rawpixel. Each photo was set on a white background, trimmed and padded to a square. They are saved as WebP at 320 px (customer) and 240 px (site). Both `SOURCES.md` files list them:

| Tile | Source | Licence |
|---|---|---|
| Grocery | https://commons.wikimedia.org/wiki/File:Vegetables_2.jpg | CC0 |
| Pharmacy | https://www.rawpixel.com/image/5920306/photo-image-public-domain-free | CC0 |
| Beauty | https://commons.wikimedia.org/wiki/File:Lapiz_labial.jpg | Public domain |
| Stationery | https://commons.wikimedia.org/wiki/File:Pencils_Standing_On_White_Background_(246574375).jpeg | CC0 |
| Electronics | https://commons.wikimedia.org/wiki/File:Action-Max-Stereo-Headphones.jpg (Evan-Amos) | Public domain |
| Hardware | https://commons.wikimedia.org/wiki/File:AdjustableWrenchWhiteBackground.jpg (Raysonho) | CC0 |

- **Customer:** `customer/src/home/photos/cat-*.webp`. The older scene photos stay for the offers carousel and the `tintPhoto` fallbacks (banner and shop cover), where a cut-out on white would look wrong.
- **Site:** `site/src/assets/photos/*.webp` were replaced in place.

## Screenshots

Before and after, at phone (360 or 390 px) and desktop (1280 or 1440 px):
- [before/](before/) and [after/](after/) hold Home, search, the shop page (with the cart bar), product, and the site's shop types.
- Customer shots come from `customer/e2e/redesign.shots.spec.ts` against the mocks. It runs only with `TZ_QA_DIR` set, for example `TZ_QA_DIR=$PWD/../docs/qa/customer-redesign/after pnpm exec playwright test -c playwright.config.ts e2e/redesign.shots.spec.ts` after `pnpm run build:e2e`.
- The site's before shot is the running `make dev` site on main. The after shot is the `build:e2e` preview.

## Visual baselines

These screens look different by design, so their Playwright visual baselines change:
- customer Home, search, shop and product;
- the site's For retailers page.

Re-take them on the next `make e2e` and review the diff there. No threshold or gate was lowered.

## Checks run

| Check | Result |
|---|---|
| customer `lint`, `lint:css`, `typecheck` | clean |
| customer `test` (Vitest) | 23 files, 218 tests passed |
| customer `build:e2e` + `redesign.shots.spec.ts` (with axe) | 8 of 8 passed |
| site `lint`, `lint:css` | clean |
| site `typecheck` | 0 errors |
| site `test` | 8 files, 172 tests passed |
| site `build:e2e` | passed (verify-dist, check-site) |
| `shared/i18n` `lint:messages` | 0 errors in every app; the warnings are the existing untranslated and run-time-key notes |
| `make check-docs` | ok (36 docs); fixed a link to a missing `landing-v2` report in `marketing-site.md` that was already broken on main |

Not run here: `make check` and `make e2e`. The user's stack is in use, so the lead runs them.

## Backend gaps (listed, not built)

- **No "items near you" or popular-items list.** Rails are built in the browser from the two nearest open, bookable shops' item lists (`GET /v1/discover/stores/{slug}/items`, 24 each). A ranked cross-shop list would need a new discover endpoint.
- **No ETA.** Quick-commerce headers show "in N minutes"; we show the place only. Pickup readiness or delivery time per shop would need data we do not have.
- **No per-category item list.** C-06 lists shops only, so category tiles filter shops on Home rather than open a product shelf.
- **Items carry no category or colour.** A tile's tint is a stable hash of the product id (`itemUi.tintOf`).
- **No photos per subcategory.** Subcategory tiles repeat the parent's photo. About 30 more licensed photos would fix it (follow-up).
- **No shop phone on the shop page.** `store.phone` comes only on an open order (D-220), so the shop page has no call button.

## Notes

- **`.tools` symlink:** `make gen` in this worktree needs the main checkout's `.tools` directory. A `.tools` symlink to it was added locally and is not committed.
- **Openverse rate limits:** the photo search on Openverse hit 429, 401 and 504, so the photos were taken from the Wikimedia Commons API (one call at a time) and rawpixel.
