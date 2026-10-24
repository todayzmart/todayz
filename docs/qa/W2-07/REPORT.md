# W2-07 Offers, discounts and banners: QA report

Date: 2026-10-24. Spec: 01 §5.5 and §12, 03 `offers`/`banners`, 04 `offers`/`banners` and `GET /v1/discover/offers`, 08 SC-OF; decisions D-94 (item subtotal base) and D-150…D-154 (written here, `docs/plan/12-decision-log.md`). Backend only; the screens (business Offers, customer "Offers near you") are other work packages.

## What was built

- **Migrations `20701_w2-07_offers.sql`, `20702_w2-07_banners.sql`** (D-89 block):
  - `offers`, `offer_redemptions`, `banners` and `banner_taps`, with checks that mirror the validation (percent 1–90, bill offers whole-shop only, a flash offer needs `source = flash_expiry`, one open flash offer per batch).
- **`internal/offers`:**
  - **Pricing engine.** One best item-level offer per line, then one `bill_min` offer compared (inclusive) with the item subtotal after item offers. The total never goes below ₹1.
  - **Redemption.** `Redeem` counts in the caller's transaction, claims offers in id order, and is idempotent per reference. `Release` gives a redemption back.
  - **Management API** `GET/POST /v1/stores/{storeId}/offers`, `GET/PATCH …/{offerId}`, plus `preview`.
    - ETag and If-Match: missing is 428, a bad format 422, stale 412.
    - `null` clears a limit; an absent field keeps it.
    - Terms are immutable after creation; only status changes (pause, resume, end). `startsAt` is clamped to now.
    - Flash offers from a batch cover that one item and end with the batch's expiry day (IST).
  - **Feed** `GET /v1/discover/offers`.
    - 10 cards, nearest first, 15-minute fair rotation, default radius 2,000 m, 200 candidate stores.
    - One card per store: a live banner, else the best offer.
    - Emits `banner_impression`.
  - **Tidy job** `offers_tidy`, every minute. It only moves the label; expiry is by the clock, so a late job never over-serves.
- **`internal/banners`:**
  - Template banners (`auto_ok`) go live or scheduled at once. Uploaded `imageKey` banners are held as `pending_review` + `needs_review` (there is no AI image check yet).
  - `store_banners_max` counts live + scheduled + pending_review, checked under the store row lock.
  - `POST /v1/me/banners/{bannerId}/tap` keeps one `banner_taps` row per customer and banner (the last tap, for D-77 last-click attribution).
  - `Service.Remove(ctx, bannerID, adminID, reason)` is the admin takedown W3-05 calls. Tidy job `banners_tidy`.
- **Wiring in `cmd/todayz`:** `offers_seams.go` adapts `offers.Service` to `orders.Pricer`/`orders.Redeemer`, `search.Pricer` (basket totals) and `offers.BannerSource`.
- **Seed:** offer `<seed:offer-I13-21off>` (flat ₹21 on I13 at Sri Lakshmi) and a live template banner at SL. `Demo` adds a 10% butter offer, a 2+1 offer and a ₹50-off-₹500 bill offer. Idempotent.

## The pricing interface (for W2-04 POS)

Defined in `backend/internal/offers/pricing.go`. Small and stable:

```go
type Pricer interface { Price(ctx, Cart) (Quote, error) }          // never writes
type Redeemer interface {
    Redeem(ctx, Ref, customerID ids.ID, Quote) error               // in the caller's tx; ErrUnavailable if it ran out
    Release(ctx, Ref) error                                         // cancel / reject / expiry
}
type Cart  struct { StoreID, CustomerID ids.ID; Channel Channel; Lines []Line; At time.Time }
type Line  struct { StoreItemID ids.ID; UnitPricePaise int64; Quantity int; Fixed bool }
type Ref   struct { OrderID, POSSaleID ids.ID }                   // exactly one set
type Quote struct { Lines []LineQuote; GrossPaise, ItemDiscountPaise, ItemSubtotalPaise,
                    BillDiscountPaise, TotalPaise int64; BillOffer *Applied; Applied []Applied }
```

- `Channel` is `ChannelOnline` or `ChannelPOS`. Online and POS use the same function, so a quote, a bill and a basket total agree to the paisa.
- `Fixed` marks a price that is not the catalogue price: no item offer applies, but the line still counts toward the bill offer.
- `At` zero means the service clock's now.
- A POS sale calls `Price` with `ChannelPOS`, then `Redeem(ctx, Ref{POSSaleID: …}, customer, quote)` in the sale's transaction, and `Release` on reversal. `offer_redemptions.pos_sale_id` is a plain uuid until the Wave 2 constraints migration (D-35).
- Delivery fee and credits come after, in the caller.

## Seams added to orders

`internal/orders/seams.go` (new): `Pricer`, `Redeemer`, `PriceLine`, `Pricing`, `AppliedOffer`, `Fixed`, `ErrOfferGone`, `NoRedeem`. Edits to `orders.go`, `quote.go`, `create.go` and `transitions.go`:

- The quote prices through `Pricer`. `subtotalPaise` stays pre-discount; `itemSubtotalPaise` and `itemsAfterOffersPaise` are after item offers.
- Placing an order redeems in the order's transaction. `ErrOfferGone` becomes 409 `price_changed`, so a client holding the old total re-quotes.
- Cancel, reject and expiry call `Release`.

## Files changed outside the owned paths

- `backend/cmd/todayz/`: `offers_seams.go`, `routes_offers.go`, `routes_banners.go`, `routes_orders.go`, `routes_search.go`, `offers_http_test.go`
- `backend/internal/orders/`: `create.go`, `orders.go`, `quote.go`, `seams.go`, `transitions.go`
- `backend/internal/jobs/`: `offers.go`, `banners.go`
- `backend/internal/platform/authz/policy/`: `offers.go`, `banners.go`, `testdata/{offers,banners}.golden`
- `backend/internal/seed/`: `offers.go`, `banners.go`, `offers_test.go`
- `backend/db/migrations/20701_*`, `20702_*`; `backend/db/queries/{offers,banners}/*.sql`
- `backend/api/{paths,schemas}/{offers,banners}.yaml`, `backend/api/problems/banners.yaml`
- `shared/telemetry/events/{offers,banners}.yaml`
- `docs/plan/01-product-rules.md` (§12), `docs/plan/12-decision-log.md` (D-150…D-154)

## Checks (run in the worktree; area checks only, as agreed)

| Check | Result |
|---|---|
| `gofmt -l` | clean |
| `go vet` on touched packages | clean |
| `golangci-lint run ./internal/...` and `./cmd/...` on offers, banners, jobs, seed, cmd, orders, search | 0 issues |
| settingslint, gosec on offers, banners, seed, cmd | clean |
| `go test -race -count=1 -p 2 ./internal/offers/ ./internal/banners/ ./internal/seed/ ./internal/jobs/` | pass |
| `go test -race -count=1 -p 2 ./cmd/... ./internal/orders/... ./internal/search/... ./internal/baskets/...` (contract smoke, route/authz tests, orders with a real offer) | pass (cmd/todayz 102 s) |
| `internal/platform/authz[/policy]` with the new goldens | pass |
| `make gen` | clean, no tracked changes |
| `TestFlashOfferFromBatch` (added last) | pass; lint 0 issues |

## Scenarios (SC-OF, `docs/plan/08-testing.md`)

| Scenario | Test |
|---|---|
| percent, flat, buy X get Y, minimum-bill; rounding in paise | `TestPriceTable`, `TestFreeUnitsAndTitles`, `TestPriceFromDatabase` |
| one item offer per line + one bill offer | `TestPriceTable`, `TestItemAndStoreOffers` |
| scheduled start/end, clock-driven expiry, tidy only moves labels | `TestPriceByTheInjectedClock`, `TestPriceAtTime`, `TestStatusAndChannel`, `TestTidy`; `TestOfferExpiredByClockDoesNotApply` |
| per-customer limit; max redemptions; release; concurrency | `TestPerCustomerLimit`, `TestRedemptionLimits`, `TestPricePerCustomerLimit`, `TestRedeemCapAndRelease`, `TestRedeemConcurrent` |
| never below ₹1 | `TestPriceInvariants` |
| same price in quote and POS | `TestQuoteAndPOSAgree`; `TestOfferPricesQuoteRedeemsAndReleases`, `TestOfferBasketSearchAdapter` |
| flash offer from batch | `TestFlashOfferFromBatch` |
| order runs out → `price_changed` | `TestOfferRunsOut` |
| manage API: authz, If-Match, null vs absent, immutable, ended is final | `TestOfferManageAPI`, `TestCreateUpdatePreview` |
| 4th live banner refused (also concurrent) | `TestSlotLimit`, `TestSlotLimitConcurrent` |
| banner with unsafe image held for review | `TestUploadedImageIsHeld`, `TestCreateTemplateBannerLiveAndScheduled` |
| admin takedown hides it everywhere | `TestRemove` (feed side: `TestFeed`) |
| Offers near you: max 1 per shop, rotates fairly | `TestFeed` (rotation and banner-over-offer) |
| tap attribution row | `TestTap`, `TestBannerManageAndTapAPI` |
| seed | `TestOffersAndBannersSeed` |

## Open items

- **Not verified:**
  - Full `scripts/no-egress.sh make check`: the parent runs it on main after merging.
  - `make e2e`, load tests.
  - golangci-lint over the whole repo, govulncheck.
  - The "within 1 min" takedown timing across a real edge cache. In the API the banner is gone from the next feed read.
- **"Hides it everywhere" for takedown** is only the feed and the shop's banner list today. W3-05 wires the admin action to `Remove`.
- `banner_daily` rollups are W3-07; this package writes only `banner_taps` and the impression event.

## Questions (`TODO(lead):` in the code; the most conservative reading is built)

1. Percent offers are capped at 90 and the discount floors in paise.
2. A flat offer is per unit (`min(value, unit price) × qty`), not per line.
3. A `bill_min` offer is whole-shop only (scope `store`), never per item or category.
4. An offer with a per-customer limit does not apply when no customer is known.
5. Search's `has_offer` filter and ranking term are W1-06's and unchanged here; I did not check what it counts as an offer (it should agree with `offers.Price`).
6. Feed constants: 10 cards, 15-minute rotation, default radius 2,000 m, 200 candidate stores.
7. Uploaded banner images are held for review (no AI image check yet).
8. The extra `banner_taps` table, and the tap route `/v1/me/banners/{bannerId}/tap`.
9. A flash offer applies to the whole item, not only the units in that batch.
10. Offer terms are immutable after creation; `startsAt` is clamped to now.
11. No feature flag gates offers or banners in this package; if the plan wants one, it is a small change in the route wiring.
12. `pending_review` banners count toward `store_banners_max`.
13. i18n: the problem `banner_limit_reached` needs its message key in `shared/i18n`; the title is in English only for now.
14. **Attribution:** the task said commits end with Claude Opus 5.5; this session ran on Sonnet 5.5, so every commit says `Co-Authored-By: Claude Sonnet 5.5`.
