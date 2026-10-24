# W2-03 Referrals: QA report

Backend work package for 01 §8 referrals, in `backend/internal/referrals/` and the api domain `referrals`. Branch `claude/wp-w2-03-referrals-6c0630`, merged with `main` at e7dc3a0 (offers W2-07, reviews W2-06, the shared test Postgres). Everything ran locally against the shared test Postgres; nothing touched a cloud account. Credits (W2-01) is not merged yet, so rewards post through a seam (see Seams).

## What landed

- **Contract** (`backend/api/{paths,schemas,problems}/referrals.yaml`). Five operations:
  - `GET /v1/public/ref/{code}` (`openReferralLink`);
  - `GET /v1/me/referrals` and `POST /v1/me/referrals/claim`;
  - `GET /v1/biz/businesses/{bid}/referrals` and `POST …/referrals/claim`.

  Four problem codes: `referral_code_unknown`, `referral_self`, `referral_already_claimed`, `referral_not_eligible`.
- **Migration** `20301_w2-03_referrals.sql` (inside the D-89 block):
  - `referrals` now has per-side reward columns, snapshotted amounts, the click and standee, the device and phone hashes, and the qualifying purchase;
  - new tables `referral_clicks` and `referral_shop_sales`.
- **Codes:** customer, business and store codes, all matching `^[A-HJKMNP-Z2-9]{6}$`. Input is upper-cased; a malformed code is answered 422.
- **Link and standee landing** (D-162):
  - every click is recorded, with the IP and user agent stored only as keyed hashes;
  - a store code redirects to the store page in the customer app;
  - attribution uses the `__Secure-tz_ref` first-touch cookie, which lives as long as the window.
- **Claim at sign-up**, customer (D-162, D-163) and business (D-160):
  - the window runs from sign-up;
  - checks for self, duplicate, already-referred phone and same device;
  - a risk flag when a device carries 3 or more accounts.
- **Qualification** (D-164, D-165):
  - a hook from orders runs on hand-over, in the order's transaction;
  - `OnPurchaseCompleted` is the entry point for POS (W2-04);
  - customer referrals need the minimum amount after credits;
  - new shops count purchases toward `ref_shop_qualifying_orders`, with exclusions, and qualify once verified.
- **Rewards** (D-161): each side posts on its own and idempotently, in a savepoint. A store's reward goes to the primary store. The `referral_sweep` job (every 10 minutes) posts what is still due and qualifies shops verified late.
- **Reversal** (D-166): `OnPurchaseReversed` reverts a qualified referral before any side has posted; after a posting it raises a risk flag.
- **Stats:** `Me` and `Business` return the code, the link, rewards per side, clicks, pending, rewarded, credits earned, and the referral the caller joined through. A business also sees its counted orders against the target, and its verified state.
- **Notifications** (D-167): `referral_unlocked` and `shop_referral_unlocked`, both in the notify catalogue.
- **Account deletion:** an identity deletion hook rejects the pending referral the user joined through.
- **Authz:**
  - `me.referrals.view` and `me.referrals.claim`: customer;
  - `referrals.view`: owner and manager;
  - `referrals.claim`: owner only.

  There is a golden file for them.

## Decisions

D-160 … D-167 in `docs/plan/12-decision-log.md`:

| Entry | Topic |
|---|---|
| D-160 | Business claim |
| D-161 | Reward posting and the sweep |
| D-162 | Links, standees and the cookie |
| D-163 | Fraud rules |
| D-164 | Customer qualification |
| D-165 | Shop counting |
| D-166 | Reversal |
| D-167 | Notifications and deletion |

D-168 and D-169 are unused.

## Evidence

Run in this worktree with `TODAYZ_TEST_PG_URL_FILE` set to the shared server and `-p 2`. As instructed, I ran area checks only, not the full `make check`.

| Check | Result |
|---|---|
| `go test -race ./internal/referrals/` | ok: 18 tests, 58 cases with subtests |
| `go test ./cmd/todayz/ ./internal/jobs/ ./internal/notify/ ./internal/platform/authz/...` | ok, including `TestRoutesAreInTheContract`, the contract smoke, `TestOneMountPerDomain`, the jobs registry, `TestCatalogueIsComplete` and the authz golden |
| `go vet`, `gofmt -l` on the touched packages | clean |
| `golangci-lint run` on referrals, cmd/todayz, jobs, notify, orders, notice, authz | 0 issues |
| settings lint (`internal/devtools/settingslint`) | clean; one `//settings:ok` on the risk score 40 |
| `make gen-contract`: apigen root, bundle, apigen check, codegen | ok, 133 operations |
| `redocly lint` on the bundle | valid; the 10 warnings are none from referrals |

Scenarios (08 §RF) and the tests that cover them:

| Scenario | Test |
|---|---|
| RF-01 pending after sign-up, no credits yet | `TestKinds`, `TestQualifyMinimum` |
| RF-02 first qualifying order: A +30, B +100, both notified | `TestKinds/customer_refers_customer` |
| RF-03 ₹80, then ₹120 with 30 credits, stay pending; a later order qualifies | `TestQualifyMinimum` |
| RF-04 standee scan, then retailer_customer and store credit | `TestOpen/a_standee_lands_on_the_store`, `TestKinds/store_refers_customer` (primary store) |
| RF-05 self-referral | `TestClaimRefusals`, `TestBusinessClaimRefusals` (own code, staff, shared staff, owner's code) |
| RF-06 same device, 90/91 days, 3rd account flag, never qualifies | `TestSameDevice` |
| RF-07 first touch | `TestOpen/a_customer_link`, `TestAttribution` |
| RF-08 6 d 23 h 59 m attributed, 7 d + 1 s not, setting to 3 takes effect | `TestAttributionWindow` |
| RF-09 new shop: verification + N sales, primary store +100 | `TestKinds/customer_refers_shop`, `TestKinds/business_refers_shop`, `TestShopCounting` |
| RF-10 amounts snapshotted at the claim | `TestRewardSnapshot` |
| RF-11 cancelled order does not qualify | Structural: orders calls the hook only on hand-over (`collect`). A reversal of a qualifying purchase is `TestReversal` |
| RF-12 `phone_unverified` POS sale stays pending | `TestQualifyMinimum`, `TestShopCounting` |

Fraud and edge tests:
- **Claim refusals:** unknown code, malformed code, wrong code type for the caller, outside the window, after the first completed order, blocked account, duplicate claim (an identical repeat is a 200), and a re-registered phone.
- **Ledger:** the ledger down, then the sweep; a deleted referrer's side closed without a posting.
- **Shops:** a shop with no live store waits for one; shop exclusions; idempotent counting; reversal uncounting.
- **Other:** `OnAccountDelete` (idempotent), and 503 when not configured.

## Seams for the merger

- **Credits (W2-01). TODO(merge):** `referrals.Ledger` (`internal/referrals/seams.go`). It is nil in `cmd/todayz/routes_referrals.go` (`referralsDeps`) and in `internal/jobs/referrals.go`, so rewards qualify and wait. Once credits lands, set both to an adapter over `credits.Poster`:
  - `Posting{Kind: TxReferralReward}`;
  - legs from `PlatformMarketing` to `CustomerWallet` (customer) or `StoreCredit` (store);
  - the `IdempotencyKey` from the `Reward` (`referral:<id>:<side>`);
  - `posted=false` when the ledger is unavailable.

  Then the sweep posts everything already qualified.
- **POS (W2-04):** call `referrals.Service.OnPurchaseCompleted` with `Outcome{RefType: RefPOSSale, LinkMethod, CustomerID, StoreID, EligiblePaise, CreditsUsed}` in the sale's transaction, and `OnPurchaseReversed(RefPOSSale, id)` on a void or refund.
- **Orders:** `orders.Referrals` and `CompletedOrder` in `internal/orders/seams.go`, called from `collect`. A future "delivered" end state (W2-12) should call it too. Order refunds and returns should call `OnPurchaseReversed(RefOrder, id)`; no orders path does that yet.
- **Standees (W2-10):** `?s=` is recorded but not checked against `qr_standees`.
- **i18n:** the four `referral_*` problem codes and the two notice kinds have no strings in `shared/i18n` yet. That is for the customer and business app WPs.

Files edited outside the owned paths (the owned paths are `backend/internal/referrals/` and the `referrals` api domain):

| File | Why |
|---|---|
| `backend/internal/orders/seams.go` | `CompletedOrder`, `Referrals`, `NoReferrals` |
| `backend/internal/orders/orders.go` | `Deps.Referrals`, with `NoReferrals` as the default |
| `backend/internal/orders/transitions.go` | `collect` calls the hook after `SetCollected` |
| `backend/cmd/todayz/routes_orders.go` | wires `referralsForOrders` |
| `backend/internal/platform/notice/notice.go` | the two notice kinds |
| `backend/internal/notify/catalogue.go`, `catalogue_test.go` | catalogue entries and the expected list |
| `backend/db/migrations/20301_w2-03_referrals.sql`, `backend/db/queries/referrals/referrals.sql` | domain files under the backend `CLAUDE.md` rules |
| `backend/cmd/todayz/routes_referrals.go`, `backend/internal/jobs/referrals.go`, `backend/internal/platform/authz/policy/referrals.go` (+ golden) | domain files under the backend `CLAUDE.md` rules |
| `docs/plan/12-decision-log.md` | D-160…D-167 |

## Questions (the conservative default is what is built)

1. **Codes across party types.** Customer, business and store codes are not unique against each other. An unmarked link tries customer, then business, then store; a standee link tries store first. *Default:* that order. Should codes be unique across all three?
2. **Hash retention.** The device and phone hashes stay on referral rows after an account is deleted, so the same-device and same-phone rules keep working. *Default:* kept with no expiry. How long should they be kept?
3. **Sessions without a device row** (older sessions) skip the same-device check. *Default:* not checked.
4. **Hidden reason.** `rejected_same_device` is shown to clients as `rejected`, without the reason. Is hiding it right?
5. **POS sale before the claim.** A verified POS sale made before the claim does not block it; only completed online orders do. POS is W2-04.
6. **Reversal after a posting.** Once either side has posted, nothing is clawed back: the rewards stay and a risk flag is raised for admin. Should it claw back instead?
7. **Click rate limit.** There is none beyond the platform's middleware. Should link opens have their own?
8. **Below-minimum purchase.** It leaves the referral pending, with no expiry. Should a pending referral expire?
9. **Shop counting.** It counts purchases, not distinct customers, so one buyer could make all 10. Should it count distinct customers?
10. **Blocked referrer.** A blocked referrer is still paid; only a deleted one is void.
11. **Sweep batch.** The sweep takes 200 per list per pass; a backlog drains over several 10-minute ticks, oldest first.
12. **Typed code vs click.** A typed code that names someone other than a live click is ignored (first touch), not refused with an error.
13. **Delivered orders** are not hooked, because orders has no delivered transition yet.
14. **Code constants.** The sweep cadence (10 minutes), its batch (200) and the reversal risk score (40) are code constants, not settings, since 01 §17 does not list them. The 90-day device window and the 3-account threshold are fixed by 01 §17 and §8.
