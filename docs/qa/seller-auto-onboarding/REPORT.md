# Seller auto-onboarding (D-274): report

> As built on main at d641cbe6 (27 Oct 2026), on branch `claude/seller-auto-onboarding-fa62d0`. A record of what was checked and when; it is not kept current.

## What the founder asked (27 Oct)

Anyone can sign up a shop, fill in the details (shop photos and GST included) and start selling. Nobody approves it first. A field agent checks new shops every day: if a shop is not right, they visit it or reject it. Copy only the layout of Blinkit's seller onboarding (the step list on a green bar on the left), and use the same dark green bar for the sidebar inside the business app ("our one is too normal white").

The rules, as the lead recorded them (D-274):

1. A business and its stores go live as soon as onboarding is filled in.
2. Payouts wait until a field agent or admin marks the shop checked. The shop keeps selling and earning credits; the monthly close holds the payout with the cause `shop_unchecked`.
3. field_agent, admin and super_admin can mark a shop checked, or reject it with a reason. Both are audited. A-24 is the daily queue and works on a phone.
4. A reject hides the shop at once and cancels its open online orders with `shop_rejected`: no customer strike, held credits released, each customer told in their language (en, hi, kn, te, ta).

## What was built

| Part | Where |
|---|---|
| Migration 49301 | `backend/db/migrations/49301_w4-93_shop_check.sql`: `businesses.check_status`, `checked_at`, `checked_by`, `check_note`, the queue index; `shop_rejected` as an order cancel reason; `shop_unchecked` as a payout hold reason |
| Go live on submit | `backend/internal/business` (`businesses.go`, `stores.go`, `agent_owner.go`): submit and the owner's code from a field agent put the business at `verified` (no `verified_by`) and its complete stores `live`, audited `store.go_live`. Uploaded documents and photos are accepted on upload |
| Shop check | `backend/internal/admin/shop_checks.go`: `GET /v1/admin/shop-checks`, `POST /v1/admin/businesses/{id}/check`, `POST /v1/admin/businesses/{id}/check-reject`; policies `admin.shop_check.view` and `admin.shop_check.decide` |
| Reject cascade | `backend/internal/orders/shop_reject.go` (`CancelShopOrders`), in the reject's transaction |
| Payout hold | `backend/internal/payouts`: cause order suspended → `shop_unchecked` → `payout_details_missing` → `risk_flag`; KYC no longer holds a payout; unhold is refused while a cause is open |
| B-02 | `business/src/onboarding/` (`OnboardingPage.tsx`, `steps.ts`, `onboarding.module.css`): the step flow with a dark green rail |
| Sidebar | `business/src/shell/shell.module.css`: the shell sidebar on the `rail` tokens |
| Tokens | `design/source/tokens.json`: `rail`, `rail-raised`, `rail-line`, `on-rail`, `on-rail-2`, `rail-accent`; `shared/tokens/test/tokens.test.ts` checks their contrast |
| A-24 | `admin/src/shop-checks/`, route `/shop-checks`; A-03 shows the check pill, A-11 the cause |
| Copy | `shared/i18n/messages/business/onboarding.*`, `credits.*`, `statements.*` and `customer/orders.*` in en, hi, kn, te, ta; `admin/shopChecks.en.json` |

## Existing data (migration 49301)

- A business an admin had already verified (now verified or suspended) counts as checked by that admin. A rejected one stays rejected.
- A `submitted` or `needs_info` business goes live with its stores that were waiting for review; uploaded documents become accepted.
- Down drops the check columns and restores the constraints. It does not put the promoted businesses back in review, because nothing records which ones moved.

## Design notes

- Only the layout follows the founder's reference: the step list on a dark bar on the left, the step card on white. The colours, icons, type and copy are TodayZ's (Anek and Figtree, Lucide, sentence case).
- The rail colours are new tokens. On `rail`, `on-rail` is 12.6:1, `on-rail-2` 8.3:1 and `rail-accent` 8.2:1 (AA). The blue `focus` ring is too dark on the green, so on the rail it is `rail-accent`.
- On a phone the rail is a dark header with the steps as a row of chips that scrolls on its own, so the page does not scroll sideways. From 1024px the rail is a full-height column with a hint under each step.
- The calm note "Payouts start after our team checks your shop." shows on B-02 once live and on B-10 Credits while the shop is unchecked. It is an info banner, not a warning.
- B-02 renders without the shell, so its root sets `data-skin="bright"` itself (the screenshots caught black, underlined buttons without it). The sidebar's shop setup card stays a white card with an ink link (the screenshots caught a white-on-white "Finish setup").
- This overrides D-261's white sidebar for the business app only. The admin console keeps its white sidebar.

## Checks run (27 Oct, on this branch)

| Check | Result |
|---|---|
| `make gen` | ok |
| Backend: gofmt, go vet, `go test -count=1 -p 2` on admin, business, payouts, orders, referrals, authz policy, seed, notify, migrate, cmd/todayz | ok (re-run on referrals and cmd/todayz after the review fixes) |
| Business: tsc, eslint, vitest | ok, 56 files and 564 tests |
| Business: stylelint | 1 error in `src/orders/sales.module.css` (D-268's file, unchanged here; on main) |
| Admin: tsc, eslint, stylelint, vitest | ok, 304 tests |
| `shared/tokens` vitest | ok, 12 tests |
| i18n `lint:messages` | 0 errors |
| e2e journeys typecheck | ok |
| Playwright: `business/e2e/onboarding.shots.spec.ts`, `admin/e2e/shop-checks.spec.ts` on `build:e2e` | ok, 3 and 2 tests |
| `make check-docs` | 1 failure, not from this change: `docs/features/marketing-site.md` links `../qa/landing-v2/`, which is missing on main (D-267) |
| Review | `claude-reviewer` (Sonnet) on the payout hold, the reject cascade and the role checks; findings and fixes below |

New tests: `backend/cmd/todayz/shop_check_http_test.go` (queue order and paging, 403 for support, finance, catalogue and analyst, check, reject with the cascade, audit rows), the D-274 cases in `business_http_test.go`, `field_http_test.go` and `payouts_*_test.go`, `business/src/onboarding/onboarding.test.tsx`, `business/src/shell/sidebar.test.ts`, `admin/src/shop-checks/shopChecks.test.tsx`, and the A-24 rows in `admin/e2e/roles.spec.ts`. Journey E2E-08 (`e2e/journeys/specs/16-e2e-08-field-onboarding.spec.ts`) now expects the shop live after the owner's code and the agent to check it from A-24.

## Review findings and fixes

`claude-reviewer` (Sonnet, read only) went through the payout hold, the reject cascade, the role checks and the migration. It confirmed that the cancels run in the reject's transaction, that `end` gives no strike and releases stock, credits and offers, that the hold order and the unhold refusal are right, that only field_agent, admin and super_admin pass the new policies, and that the migration's Up and Down are safe.

| Finding | What changed |
|---|---|
| An order placed while a reject runs could commit after it, leaving an open order at a rejected shop | `StoreForBooking` holds the business `FOR SHARE OF b` for the whole checkout, so the order either waits and is cancelled by the reject, or sees the shop rejected |
| The reject locked one order's stock while waiting for the next order, so it could deadlock with a shop accept | `OpenOrdersOfBusiness` locks every open order `FOR UPDATE`, in id order, before any `end` |
| A shop referral (D-168) qualified as soon as the shop was live, so a fake shop could earn the reward before anyone checked it | `BusinessVerified` (referrals) needs `check_status = 'checked'`; the sweep qualifies the shop after the check. `TestShopCounting` (referrals) covers the wait while unchecked |
| A rejected shop's payout stays held as `shop_unchecked` with no way to release it | Kept as is (the money stays held, which is the safe side). What happens to it is a founder decision, raised with the lead |
| A rejected owner can sign up a new business; a field agent can check a shop they onboarded; a suspended, unchecked shop can be neither checked nor rejected | Recorded as accepted gaps in D-274 |

## Screenshots

Taken with a faked API from the production-shaped builds (`business/e2e/onboarding.shots.spec.ts`, `admin/e2e/shop-checks.spec.ts`, `TZ_QA_DIR=docs/qa/seller-auto-onboarding`):

- `onboarding-desktop.png`, `onboarding-live-desktop.png`, `onboarding-phone.png`: B-02
- `sidebar-desktop.png`: the dark green business sidebar
- `shop-checks-desktop.png`, `shop-checks-reject-desktop.png`, `shop-checks-phone.png`: A-24

## Accepted gaps

- A rejected shop's payout stays held as `shop_unchecked`; the founder decides what happens to that money.
- A rejected owner can sign up a new business, which goes live at once. A field agent can check a shop they onboarded.
- Checking a shop does not release a payout that was already held as `shop_unchecked`. Finance releases it on A-11, because a close never lifts a hold. Later closes do not hold it.
- A checked shop that goes wrong later is suspended on A-03. Reject from A-24 is only for unchecked shops.
- The pre-D-274 verify, request-info and store review paths stay for old rows. Nothing new reaches them.
- The new hi, kn, te and ta strings are machine-written and approved under the founder's release instruction, with no native-speaker review (`docs/qa/business-i18n/REVIEW.md`).
- No `W4-93` row in `docs/plan/07-work-packages.md`. The migration uses that block, and the lead may add the row.
