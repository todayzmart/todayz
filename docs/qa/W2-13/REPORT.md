# W2-13 Wave 2 E2E gate: QA report

This report covers the Playwright multi-context journeys for Wave 2 and the gate that runs them.

- **Branch:** `claude/wp-w2-13-gate-a2a139`, cut from `main` at 0e61d1e. Local `main` 573be3e (W2-02 finish, which carries the B-1 fix feeadca) was merged in as c600b10. Runs 5–8 test that merged code.
- **Where it ran:** locally, against the `make dev` stack, under `scripts/no-egress.sh`. Nothing touched a cloud account.
- **Product code:** none changed. The bugs found are listed below for the lead to route.

## What was built

- **`e2e/fixtures/`** (`@todayz/e2e-fixtures`)
  - `seed.ts`: seed ids, slugs, prices and test people.
  - `api.ts`: a JSON client whose failure messages carry only the method, path, status and problem code. Also ULID and TOTP helpers.
  - `principals.ts`: each person signs in once per run through the mocks' `/_mock/sms/latest-otp`, plus TOTP for an admin.
    - The session is kept in a 0600 file under the OS temp dir, and the teardown deletes it.
    - Sessions are lent to browser contexts through the refresh cookie.
  - `journey.ts`: `Shop`, `Customer`, the test clock, the first-run language and place, the saved-cart wait, checkout, and `test` with `who`.
- **`e2e/journeys/`** (`@todayz/e2e-journeys`)
  - Specs are named `specs/NN-e2e-XX-*`. They run with one worker, in file order. The specs that move the test clock run last (20-*, 21-*).
  - `globalSetup` opens SL and SL2 all day. `globalTeardown` restores their hours and resets the clock.
  - Traces and videos are off.
- **Gate:** `mk/60-journeys.mk` adds `E2E_STEPS += e2e-journeys`, which depends on `e2e-stack-up`. It also adds `CHECK_STEPS += check-journeys` (a typecheck). The root Makefile is untouched.
- **Admin steps** go through the admin API (D-83).

## Evidence

Each run started from a fresh `TODAYZ_STACK_FORCE=1 make reset`, which ended with `reset rc=0`. The gate itself is:

```
TODAYZ_STACK_FORCE=1 scripts/no-egress.sh make gen e2e-journeys
```

| Run | Code | Passed | Failed | Fixme | Gate rc |
|---|---|---|---|---|---|
| 1 | f4f1d1d (first cut) | 10 | 10 | 9 | 2 |
| 2 | 264d67d (fixes after run 1) | 14 | 5 | 10 | 2 |
| 3 | d30cdab (before the merge) | 16 | 3 | 10 | 2 |
| 4 | d30cdab (before the merge) | 16 | 3 | 10 | 2 |
| 5 | c600b10 + E2E-03/04/12 now strict (uncommitted) | 18 | 3 | 7 | 2 |
| 6 | 85d115a | 20 | 1 | 7 | 2 |
| **7** | **14a2fdb (final)** | **21** | **0** | **7** | **0** |
| **8** | **14a2fdb (final)** | **21** | **0** | **7** | **0** |

- **Final code:** runs 7 and 8 are the two runs on the final code. They agree test for test: 21 pass, 0 fail, 7 fixme, and the gate exits 0.
- **Runs 3–4:** their 3 failures were all B-1, which is fixed on the merged code.
- **Runs 5–6:** every failure was a test-side matcher or step order (see Test problems), not a product bug.
- **API log:** no 5xx in runs 5–8. The last 5xx in `.cache/dev/api.log` is a B-1 500 from before the merge.
- **No secrets in the output:** across every run log and reset log (runs 1–8), a grep for the test phone prefix and `+91` found 0 matches. No OTP, pickup code, counter code, bill token or phone is printed or attached.

## Results per journey (runs 7 and 8, merged code)

| Journey | Test | Result | Note |
|---|---|---|---|
| E2E-02 | Referral credits; a ₹300 order using 30; cashback 13; wallet 83; credit account +30 −13 | **pass** | |
| E2E-03 | Partial accept (API) → the customer confirms the new total → collected | **pass** | Strict: the accept answers 200 with `awaiting_customer`. D-1 closed by W2-02 finish |
| E2E-03 | Shop side in the business app: Accept part → one less → Send to customer → "Waiting for …" banner; the order is `awaiting_customer` with 1 accepted | **pass** | New. D-2 closed |
| E2E-06 | POS walk-in with bill QR credits | **pass** | |
| E2E-10 | Admin hides a review → the public rating drops; restore brings it back (admin API) | **pass** | |
| E2E-10 | The customer shop page (C-07) shows the rating ("3.5 (1)") | **pass** | B-1 fixed |
| E2E-12 | Before the change: 30 credits placed and released; 40 refused `409 credit_max_exceeded`, nothing held | **pass** | Strict (Q-1, D-216). First run against the stack was run 5 |
| E2E-12 | Super admin sets `credit_max_per_order` to 40 with step-up; audit entry | fixme | D-3 |
| E2E-13 | "Weekly groceries" → Fill cart → order → pickup; "Sunday breakfast" split → two orders | **pass** | |
| E2E-13 | Build a basket from search results (C-05) | **pass** | |
| E2E-13 | Build a basket from a shop page (C-07) | **pass** | B-1 fixed |
| E2E-13 | Build a basket from a product page (C-08) | **pass** | B-1 fixed |
| E2E-15 | Language switch mid-journey keeps the cart and finishes the order | **pass** | |
| E2E-15 | The screens read in Hindi after the switch | fixme | D-4 |
| E2E-16 | Offline banner; the page is kept; the banner clears when back online | **pass** | |
| E2E-17 | Multi-store owner sees both shops (B-01) and opens each one's orders | **pass** | |
| E2E-18 | Cashier: hand over yes; cancel, settings and the other shop no | **pass** | |
| E2E-19 | Blocked customer cannot sign in or order | fixme | D-5 |
| E2E-20 | Suspended shop: not in discover, no shop page, orders refused | **pass** | |
| E2E-05 | Delivery to a saved address | fixme | D-6 |
| E2E-05 | Dispatch with a rider → delivered | fixme | D-6 |
| E2E-05 | The full phone shows only while out for delivery | fixme | D-6 |
| E2E-04 | Three no-shows → strikes → the limit → the C-21 banner on Home → one open order; the second refused `409 ordering_suspended` at quote and at place | **pass** | Banner step new. D-7 closed for the customer app |
| E2E-07 | Offline counter code → synced within 12 h → linked, cashback posted | **pass** | |
| E2E-07 | Variant: the same code used again → sale kept, link failed, no credits | **pass** | |
| E2E-07 | Variant: device clock off by more than 2 min → link failed, no credits | **pass** | |
| E2E-07 | Variant: synced after 12 h → link failed, no credits | **pass** | |
| E2E-07 | The shop sees "Customer link failed" in the business app | fixme | D-8 |

## Product bugs (for the lead to route)

### B-1: the shop page and its items return 500 for every seeded store

**Status: resolved.** The lead fixed it on `main` in feeadca, with a regression test. It reached this branch with the merge (c600b10). On the merged code, the three journeys it failed pass in runs 5–8, and the API log shows no more 500s on these routes.

**Where:** `backend/db/queries/search/search.sql:106` (`StoreBySlug`), scanned in `backend/internal/search/shop.go:127`.

**What happens:**
- `GET /v1/discover/stores/{slug}` and `GET /v1/discover/stores/{slug}/items` answer 500.
- The API log shows `can't scan into dest[23] (col: category_name): cannot scan NULL into *string`. All 49 of the 500s in the run logs carry this error: 30 on `/v1/discover/stores/{slug}` and 19 on its `/items` route.

**Cause:**
- The query left-joins `categories` on `b.category_id`.
- With no category, `COALESCE(NULLIF(c.names->>'en',''), c.slug)` is NULL, but sqlc types `category_name` as a non-null `string`.
- All 24 seeded businesses have `category_id` NULL.

**Impact:** the customer shop page (C-07) and product page (C-08) can't load for any shop. This fails E2E-10 (C-07) and two E2E-13 tests.

**Fix direction (backend search owner):** make `category_name` nullable, for example `sqlc.narg`-style or a `::text` that sqlc reads as nullable, or COALESCE it to `''` and map `''` to nil in `shop.go`. Add a test for a business without a category.

## Deferred pieces (fixme, not failures)

| Id | What is missing | Where |
|---|---|---|
| ~~D-1~~ | ~~Partial accept in the backend (BK-08–10)~~ | Closed by W2-02 finish (573be3e); E2E-03 is strict |
| ~~D-2~~ | ~~The business app's partial-accept UI~~ | Closed by W2-02 finish; E2E-03's business-app test passes |
| D-3 | Admin settings API and A-17 (`credit_max_per_order`, step-up, audit) | W3-09 / W3-05 |
| D-4 | Hindi copy | `shared/i18n/compiled/customer/hi.json` has English text for all 485 keys (0 translated) |
| D-5 | Admin customer-block API or screen, and a blocked customer in the seed | W3-05 |
| D-6 | Customer address API (createOrder needs `addressId`; C-11 is pickup only), dispatch UI, and the full-phone reveal (`StoreOrder` has `phoneMasked` only) | W3 delivery |
| ~~D-7~~ | ~~The no-show limit banner (BK-16)~~ | Closed for the customer app (C-21 on Home, asserted). The business-app side of the limit is not asserted at this gate |
| D-8 | The POS sales list with the link status in the business app | business POS |

## Open question for the lead

**Q-1: credits over the per-order cap.**
- **What the plan says:** 08-testing CR-02 says "Use 31 → refused `credit_max_exceeded`".
- **What the server does:** it clamps instead.
  - A quote or order asking for 40 credits gets the 30 that the cap allows (`creditsAllowed` 30).
  - An order sent with the total for 40 is refused as `409 price_changed`.
  - An order sent with the capped total is placed with `creditsUsed` 30.
- **Where the code is mapped:** `credit_max_exceeded` is mapped in `backend/internal/orders/create.go:227`, but it only fires when the hold refuses.
- **Is money at risk?** No: more than 30 is never spent either way.
- **What the test does:** E2E-12 asserts the clamp, which is the conservative reading and the current behaviour.
- **Decision needed:** whether clamping is intended, which would mean 08 CR-02 changes, or whether an over-cap request should be refused, which would be a backend change.
- **Answer (lead, 24 Oct):** refuse with 409 `credit_max_exceeded` (04-api, D-544); CR-02 stands. The W2-02 finish session is making the backend change.
- **Closed:** the change came with W2-02 finish (D-216). E2E-12 asserts the refusal strictly and passes in runs 5–8.

## Test problems found and fixed during the runs

**Fixed after run 1** (commit 264d67d):

| Run 1 failure | Cause | Fix |
|---|---|---|
| E2E-13, E2E-15: lost session | The refresh cookie's path is `/v1/auth`, so `context.cookies(apiURL)` missed it on detach, and the next browser context had no session. | `detach()` reads the whole jar and filters by name. |
| E2E-13, E2E-15: "Set your location" | The customer app had no place, so search and baskets showed "Set your location". | `customerFirstRun` seeds `todayz.place` (the QA point) and `todayz.lang`. |
| E2E-04: the clock answered 422 | `wait` was sent as a number. | `POST /v1/test/clock` now gets a duration string. |
| E2E-04: expected 422 | Ordering while suspended is `409 ordering_suspended` (`orders.yaml`, 04-api). | The spec now expects 409. |
| E2E-12 | The server clamps credits (Q-1). | The spec was rewritten to the clamp. |
| E2E-17 | The store picker is at `/choose-shop`, not `/`. | The spec goes to `/choose-shop`. |
| E2E-03 | Runtime fixme on D-1. | The test cancels the order, then marks the fixme. |
| Hangs | A missing control hung for the full 3-minute test timeout. | `actionTimeout` 20 s and `navigationTimeout` 30 s. |

**Fixed after run 2** (commit d30cdab):

| Run 2 failure | Cause | Fix |
|---|---|---|
| E2E-13 and E2E-15: empty cart | The specs did a full `goto`/`reload` a few milliseconds after adding to the cart. That aborted the app's IndexedDB write (`customer/src/cart/storage.ts`), so the cart came back empty. This is a test race, not a product bug: the cart survives a reload once the write lands. | `cartSaved(page)` waits for the saved lines, and `checkoutCart` no longer reloads `/cart`. E2E-15 now also asserts that the cart holds the item before the switch. |
| E2E-15: lang tag | The spec expected `<html lang="en">`. English uses `en-IN` by design (`shared/i18n/src/locales.ts:54`). | The spec expects `en-IN`. |

**After the merge of W2-02 finish** (commits 85d115a, 14a2fdb):

| Change or failure | Cause | Fix |
|---|---|---|
| E2E-03, E2E-12 runtime fixmes | W2-02 finish shipped partial accept and the `credit_max_exceeded` refusal. | Both are strict assertions now. |
| E2E-03 shop side (D-2), E2E-04 banner (D-7) | The screens now exist. | New steps: the business app's Accept part sheet (`AcceptPartSheet`) and the C-21 banner. |
| Run 5: E2E-10 C-07 rating | The spec looked for "N ratings/reviews". The shop page shows the average and count as "3.5 (1)". | The spec matches `d.d (count)`, with the count read from the public reviews API. |
| Run 5: E2E-13 C-08 | The product page uses the full-size Add to basket button, whose name is "Add to basket". Only the compact one on cards and shop pages is "Add to basket: {name}" (`customer/src/basket/AddToBasket.tsx:104`). | The helper accepts both names. |
| Run 5: E2E-04 banner body | The `status` element holds the title and the body as one text, so an anchored `getByText` regex missed it. | `toContainText` on the status element filtered by the title. |
| Run 6: E2E-04 limit step | With one open order, the quote now also refuses with `409 ordering_suspended`, as `backend/api/paths/orders.yaml:30` specifies. The spec quoted after placing the first order. | The spec prices before the first order, then asserts that both the quote and the place refuse the second order. |

## After the runs

None of runs 5–8 found a new product bug. What is left is the 7 fixmes (D-3, D-4, D-5, D-6 ×3, D-8), all waiting on later work packages.

## Notes

- **Files outside the owned area:**
  - `pnpm-lock.yaml` gained importers for the two new workspace packages. They have no new external dependencies, and installs are `--frozen-lockfile --offline`.
  - `e2e/CLAUDE.md` names `fixtures/` and `journeys/`.
- **OTP budget:** the API allows 20 OTP sends per IP per hour and 5 per phone. One run signs in about 12 people, so run the gate from a fresh `make reset`, which clears the counts.
- **Stack ownership:** this session owned the stack for the round (`TODAYZ_STACK_FORCE=1`) and stopped it at the end.
