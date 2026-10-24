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

- **Credits (W2-01):** wired, see the addendum below (D-169).
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
9. **Shop counting.** *Decided (founder, D-168):* distinct customers, not purchases. Built in the follow-up below.
10. **Blocked referrer.** *Decided (founder, D-168):* not paid while blocked; the reward stays qualified and the sweep pays it once they are unblocked. A deleted one stays void. Built in the follow-up below. Still open: should a blocked *referee*, or a suspended referrer business, be held the same way? Today both are paid.
11. **Sweep batch.** The sweep takes 200 per list per pass; a backlog drains over several 10-minute ticks, oldest first.
12. **Typed code vs click.** A typed code that names someone other than a live click is ignored (first touch), not refused with an error.
13. **Delivered orders** are not hooked, because orders has no delivered transition yet.
14. **Code constants.** The sweep cadence (10 minutes), its batch (200) and the reversal risk score (40) are code constants, not settings, since 01 §17 does not list them. The 90-day device window and the 3-account threshold are fixed by 01 §17 and §8.

## Follow-up: founder decisions (24 Oct, D-168)

Built after the branch merged into main (main at 6787251, fast-forwarded):
- **Distinct customers.**
  - Migration `20302_w2-03_shop_sale_customer.sql` adds `referral_shop_sales.customer_id`, backfilled from the order for existing order rows.
  - `ShopSaleCount` counts distinct buyers, and `ordersCounted` reports that count.
- **Blocked referrer.**
  - A blocked customer referrer's side waits; the referee is still paid.
  - The sweep's `QualifiedReferrals` skips a referral waiting only on a blocked referrer.
  - It pays the referrer once they are unblocked.
- **Docs:**
  - 01 §8 states both rules;
  - D-161, D-164 and D-165 now point to D-168;
  - D-164 now names `counter_code`, as the code and 01 do, where it wrongly said `phone_verified`.

New tests:
- `TestShopCountingDistinctCustomers`: repeat buyers do not count, and a third buyer qualifies the shop.
- `TestBlockedReferrerWaits`: the referee is paid while the referrer is blocked; the sweep skips the referral and then pays it after the unblock.
- `TestShopCounting` passes unchanged, since it already used a different buyer for each counted purchase.

## Addendum: rewards post to the credits ledger (24 Oct, D-169)

Built after main (9ddd0c0) merged in, with both W2-01 credits and W2-03 referrals on it.
- **Adapter.** `jobs.ReferralsLedger(credits.Poster)` is in `backend/internal/jobs/referrals.go`, so `internal/referrals` still imports no other domain.
  - One `TxReferralReward` posting per side: ref `referral`/the referral id, the reward's `referral:<id>:<side>` key, legs `platform_marketing` −N → `customer_wallet` or `store_credit` +N.
  - A duplicate counts as posted, with the first transaction id.
  - `ErrKeyReused` and `ErrInvalid` wrap the new `referrals.ErrRewardRefused`; other errors pass through.
- **Units.** `Reward.Credits` and `Leg.Amount` are both whole credits:
  - 01 line 7 says "1 credit = ₹1";
  - 03 says `bigint` whole credits;
  - `ledger_entries` holds signed amounts in credits.
  So the amount moves as `int64(r.Credits)`, with no conversion.
- **One constructor.** `jobs.ReferralsService(Deps, notice.Sender)` builds the service for both the API (`newReferrals` in `cmd/todayz/routes_referrals.go`) and the `referral_sweep` job. It took over `referralsDeps` (hasher, public URL, app origins). The sweep had a nil ledger before. All three TODO(merge) comments are gone.
- **Transaction.** `credits.Service.Post` uses `s.db.InTx`, which joins the caller's transaction from ctx as a savepoint (`outer.Begin`) and opens a pool transaction only when there is none. So the posting is part of the purchase. The referrals savepoint around it means a failure rolls back the posting only.
- **No retry loop.** A refused posting raises a `referral.reward_refused` risk flag. `QualifiedReferrals` leaves out referrals with that flag `open` or `confirmed`, and clearing it allows one more try. A transient error is still logged and retried by the sweep, as before. No migration was needed: `risk_flags` and its subject index already exist.
- **Runbook.** There is a new "Referral rewards" section in `docs/runbooks/credits-ledger.md`, and `referral_sweep` now names that runbook.

New tests, in `backend/internal/jobs/referrals_test.go`, run against the real `credits.Service` on the shared test Postgres:
- `TestReferralRewardsPostToCreditsDB` builds through `ReferralsService`. Both sides post once. Both wallets are credited, `platform_marketing` is debited by the sum, and the referee's lot equals the reward. The same purchase again and a re-sweep post nothing.
- `TestReferralRewardStoreCreditDB`: a store referrer's reward lands in the primary store's `store_credit`, with no lot.
- `TestReferralRewardFailedPostSweepsDB`: the ledger fails both postings inside an outer purchase transaction that has a write of its own. That write and the qualification commit and nothing posts. The next sweep posts both sides once, and a second sweep posts nothing.
- `TestReferralReversedBeforePostingDB` (D-166): the purchase is reversed while both sides are unposted. The referral goes back to `pending`, the sweep posts nothing and no flag is raised. A later qualifying purchase pays.
- `TestReferralRewardRefusedIsParkedDB`: the referee's key already exists with other legs.
  - The referrer is still paid and the referee is flagged once.
  - Three sweeps add nothing.
  - After the flag is cleared, one more sweep tries once, raises one new flag, and the next sweep adds nothing.
  - A mutation check confirmed the test catches a missing exclusion: with the `QualifiedReferrals` exclusion removed it fails with 4 flags, one per sweep.

Checks, with `-p 2` on the shared test Postgres:
- `go test -race` passes for `internal/referrals`, `internal/jobs`, `internal/credits` and `internal/notify`.
- `cmd/todayz` passes.
- golangci-lint reports 0 issues on referrals, jobs and cmd/todayz.
- settingslint, gofmt and go vet are clean.

Questions (the conservative reading is what is built):
- `referral_sweep` stays `ClassMaintenance`, though it now moves money. `ClassMoney` would also make it require a runbook, and it names one anyway. Should it change class?
- While a `referral.reward_refused` flag is open, the whole referral is parked, including the other side if that side is unposted for another reason. A per-side park would need a column (a migration).

## Addendum: review fixes, rewards post after the purchase commits (24 Oct, D-169)

The review of ff9a29f found no blockers. These are its fixes.

**1. Deadlock: the reward is out of the purchase transaction.**
- The purchase's transaction only qualifies the referral and enqueues `referral_reward` with `InsertTx` (`EnqueueOpts.UniqueKey` `referral:<id>`), in a savepoint. A failed enqueue is logged and the sweep posts it, since a newly qualified referral is due at once.
- The new `referral_reward` job (ClassMoney, 3 attempts, `backend/internal/jobs/referrals.go`) calls `Service.PostRewards`. It posts each side in its own transaction, so each posting is a single `LockBalances` call. Lock order: ledger accounts, then the referral row (`FOR UPDATE`, rechecked; a change rolls the side back). `PostRewards` refuses to run inside a caller's transaction.
- `referral_sweep` stays the backstop.
- Reproduced first: with the posting put back inside the purchase transaction, `S` given a cashback rate so `Complete` locks it, the race failed with `40P01` in 20 of 20 rounds. With the fix, `TestReferralRewardConcurrentWithReferrersOrderDB` (20 rounds, `-race`, run 3 times) has no errors. The referrer's wallet ends at 1000 + rewards − credits spent + cashback.

**2. Stuck retries.**
- Migration `20303_w2-03_reward_attempts.sql` adds `referrals.attempts` and `next_attempt_at` (with a check and the partial index `ix_referrals_reward_due`). Already-qualified rows are backfilled with `next_attempt_at = qualified_at`.
- The backoff is 10 min after the first failed pass, doubling, capped at 6 h.
- `QualifiedReferrals` reads only due rows, `ORDER BY next_attempt_at, id`.
- A payWait side (a store referrer with no live store) gets the backoff. A blocked referrer and a refused reward stay out of the batch by query until their condition changes.
- There is a new `tz_referral_reward_failed_total{reason}` (`ledger_error`, `refused`, `error`), and `waiting` is in the sweep and job summaries.
- `TestStuckRewardsDoNotStarveFreshOnes`: 220 stuck plus 3 fresh. The first sweep takes 200 stuck; the second takes the other 20 and posts the 3 fresh; the third has nothing due; after the backoff, 200 again.

**3. Tests** (`backend/internal/jobs/referrals_test.go`, real `credits.Service`):
- `TestReferralRewardSQLErrorRollsBackTheSideDB`: the poster posts, then runs `SELECT 1/0` on `db.TxFrom(ctx)`. The side's transaction rolls back (no ledger tx for its key, the wallet unchanged), the other side posts, and the next due sweep posts the failed side once.
- `TestReferralRewardDuplicateDB`: an identical posting made first returns `Duplicate` with the same id. No new tx, one lot, and the referral stores the first tx id.
- `TestReferralReversedAfterPostingDB`: both sides posted, then reversed. The flag is raised, and the wallets, lots and `platform_marketing` are unchanged.
- `TestReferralRewardsPostToCreditsDB` now checks the exact key per side and `ref_type`/`ref_id` on the posted rows.
- `TestReferralRewardStoreRefusedDB`: a refused store beneficiary is parked with a flag.
- `TestShopRewardWaitsForAStore` (referrals package): reversed while the store side waits, the referral stays qualified with one flag, and the store side is paid later.

**4. Guard.** `referrals.New` returns an error with a nil `Ledger`, and `NoLedger` is deleted. The tests use their own fake ledgers. The `seams.go` doc no longer says "switched off".

**5. Docs.** D-161 fixed in place (posts after commit; the ledger is implemented, no longer "a seam until W2-01 merges"). D-169 extended (after-commit design, lock order, backoff, metric, the reversal-while-waiting rule). The runbook's "Referral rewards" section rewritten.

Questions (the conservative reading is what is built):
- **The backoff is code, not a setting.** 10 min, doubling, 6 h cap is operational pacing like the sweep's schedule, so it is not in `platform_settings`. Should it be configurable?
- **A store referrer with no live store backs off.** Its reward can therefore lag up to 6 h after the store goes live. Excluding it from the batch by query, as for a blocked referrer, would need a join on stores. The backoff was the simpler choice the review allowed.
- **Reversal while one side waits.** 01 §7 says "if it has posted, the reward stays". It is read as "if either side has posted", so the waiting side is still paid later and a flag is raised. The other reading would void the waiting side.
- **Without the job queue** (`Deps.Jobs` nil, only in tests), the sweep posts within about 10 min. The app always passes the enqueuer.
