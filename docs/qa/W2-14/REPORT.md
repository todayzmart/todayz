# W2-14 Basket search: QA report

Date: 2026-10-24. Spec: D-110 and D-111 (`docs/plan/12-decision-log.md`), 01 §11.4 and §17, 03 `baskets`/`basket_lines`, 04 `/v1/me/baskets…` and `POST /v1/discover/basket-search`, 06 C-22, 08 SC-BSK. Non-AI only; AI-C1 list parse is W3-12.

## What was built

**Backend**

- **Migration `21401_w2-14_baskets.sql`:**
  - `baskets` has soft delete, `version`, and `last_split` jsonb (an object, at most 8 KB).
  - `basket_lines` has the checks one-of, quantity 1–99 and text 1–120, and is unique on (basket, position).
- **`internal/baskets`:**
  - `GET/POST /v1/me/baskets` and `GET/PATCH/DELETE /v1/me/baskets/{id}`.
  - PATCH needs `If-Match "v{n}"`: `428` without it, `412` when it is stale.
  - `lines` are replaced as a set in one transaction.
  - Another customer's basket is `404`.
  - One basket over the limit is `422 basket_limit_reached`.
  - The account-deletion hook deletes the customer's baskets.
  - The authz policy has a golden file.
- **`internal/search/basket*.go`, `POST /v1/discover/basket-search`:**
  - Public and read-only, sharing discover search's per-IP limit.
  - A line is covered when `available` reaches the quantity.
  - Free text is normalised as search does.
  - Ranking is: more lines covered, then the lower total (item offers applied through `offers.Price`), then the nearer store, then the store id.
  - Badges (Has everything, Lowest price, Closest) are labels only.
  - A split across at most 2 shops appears only when no store has everything and `basket_split_max_shops` ≥ 2.
  - Each search logs one `search_log` row per line, with `source = basket`.
  - It never calls a model.
- **Settings:** `basket_max_saved`, `basket_max_items`, `basket_results_max` and `basket_split_max_shops` are read from `platform_settings`.
- **Seed:** Asha has two baskets.
  - "Weekly groceries": Sri Lakshmi has every line.
  - "Sunday breakfast": the lines are split between Sri Lakshmi and Sri Lakshmi 2.

**Customer app (`customer/src/basket`, routes `/baskets`, `/baskets/device` and `/baskets/$basketId`)**

- **C-22 editor:**
  - Add by text, with product suggestions; repeated adds merge.
  - Quantity stepper, remove, and rename.
  - A saved basket autosaves with If-Match. On a `412` the app says so and shows the latest version.
- **Results:**
  - The proof line "Checked {N} shops nearby · {k} has/have everything", using a plural.
  - Store cards with the covered lines, totals, freshness, what is missing, the badge, and "Fill cart".
  - The split card "Get it all from two shops", with "Fill cart from {shop}".
  - The radius chips are the same as on search.
- **Fill cart:** asks before replacing a cart for another shop. On a split, it remembers the second shop. C-12 then shows "Next: the rest from {shop}", but not on a failed order.
- **"Add to basket":**
  - Appears on search and shop item cards (compact, labelled "Add to basket: {name}") and on the product page.
  - Signed out, the line goes into the basket on this device.
  - Signed in, a sheet lists the saved baskets. The chosen basket is read fresh and patched with If-Match, retrying once on a `412`.
- **`/baskets`:**
  - Saved list with "Search again", "New basket", and delete with an 8 s undo.
  - Signed out, the device basket with "Sign in to save it". After sign-in, an offer to save it.
- **Entry points:** a Baskets card on Home and a row on Account.

## Checks

| Check | Result |
|---|---|
| Backend unit and integration (`internal/baskets`, `internal/search` basket tests, authz golden, routes, seed, jobs) | pass |
| Customer vitest | 95/95 (14 in `src/test/basket.test.tsx`) |
| Business vitest | 78/78 |
| Customer tsc, ESLint, Stylelint | clean |
| `build:e2e` + precache check | pass |
| Size: initial JS | 136.2 KB of 170 KB gzip; CSS 9.7 KB of 20 KB |
| Stack journey `customer/e2e/basket.stack.spec.ts` on a fresh `make reset` + `make dev` | 1 passed (also passes on a rerun) |
| `scripts/no-egress.sh make check` | every W2-14 part passes; three failures outside W2-14 remain (see below) |

## Scenarios

| BSK | Where |
|---|---|
| 01 limit, soft delete | `internal/baskets` TestSavedLimitBSK01 |
| 02, 03 line limits, free text | TestLinesBSK02BSK03, TestBasketFreeTextBSK03 |
| 04 coverage | TestBasketCoverageBSK04 |
| 05, 06 ranking and ties | TestBasketRankOrder, TestBasketRankTies (BSK-05's order fixed in 08, D-111) |
| 07 badges | TestBasketBadges; the summary in basket.test.tsx |
| 08, 09 split, `basket_split_max_shops` | TestBasketSplit, TestBasketSplitBSK08BSK09 |
| 10 Fill cart, next from split | basket.test.tsx; the stack journey |
| 11 signed out, save offer | basket.test.tsx |
| 12 ownership, account deletion | TestOwnershipBSK12 |
| 13 If-Match | TestConcurrencyBSK13; autosave conflict in basket.test.tsx |
| 14 never calls a model, logging | TestBasketLogsAndValidationBSK14 (run under no-egress in `make check`) |
| 15 AI parse | W3-12 |
| 16–21 | `e2e/scenarios/BSK.md` |

## Screenshots (390 px, seed data)

- `basket-list-390.png`: Asha's saved baskets.
- `basket-results-390.png`: "Weekly groceries": Sri Lakshmi, Has everything, 4 of 4.
- `basket-cart-390.png`: the cart after Fill cart.
- `basket-split-390.png`: "Sunday breakfast": the two-shop split card.
- `basket-add-390.png`: Add to basket from the shop page, with the "Added … to Sunday breakfast" toast.

The stack journey adds a line to "Sunday breakfast", so after one run its totals differ from the seed until the next `make reset`. The journey passes either way.

## Notes and follow-ups

- On the first search, progress reads "Checking shops nearby…", because N is only known from the response.
- "Add to basket" also shows on in-store-only items. A basket is a list, and the card says "In store only".
- On a narrow phone, the toast sits over the cart bar for its 4 s, as other toasts do.
- **Open, defaults kept:**
  - Q-59: a split never beats a full match.
  - Q-60: no basket reminders.
  - Q-61: baskets stay private.
  - E2E-13 (placing both orders of a split end to end) needs W2-02 checkout on two shops in one journey. It is not in this stack spec, which stops at the cart.

## `make check` (2026-10-24, under `scripts/no-egress.sh`)

- **Passes:** check-repo, check-stack, check-backend, check-contract, check-mocks, the frontend lint, the build and precache check, size, check-i18n-messages, check-ui, check-auth, check-realtime, check-ci, check-tf-static.
- **Fixed on the way:**
  - gosec G602 in `basket_rank.go`: pointers instead of indexes.
  - The contract smoke:
    - A line is now a `oneOf` of `required: [productId]` / `required: [text]` in both line schemas.
    - `searchBasket` may answer 422 to a schema-valid basket longer than `basket_max_items`, a setting the contract can only carry as its maximum of 100 (`e2e/contract/schemathesis.toml`, D-111).
  - Two customer catalogue strings broke sentence case ("Dolo", "Excel"). They are now "Add atta, milk, bread…" and "Tata salt 1 kg".
- **Failing, not W2-14:**
  - `business/e2e/orders.stack.spec.ts:226`: `headers` may be undefined under `exactOptionalPropertyTypes` (check-frontend-types).
  - `shared/i18n` business copy: `orders.card.shortHint` has "Reject" mid-sentence (check-frontend-unit).
  - Both come from ebfbf90.
  - check-infra: `infra/cloudflare` is not initialised on this machine. `make setup` online prefetches the providers.
- **Flaky under load:** `internal/platform/realtime` `TestStreamLiveThenReplayAfterReconnect` failed once in a full run. It passes alone with `-race -count=3`, and check-backend passed in the next two full runs.
