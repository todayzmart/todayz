# W4-01 every E2E journey green on a fresh reset: QA report

This report covers the end of W3-14 (the Wave 3 journeys) and W4-01 (the whole journey suite and the contract suite, green twice on a fresh `make reset`).

- **Branch:** `claude/w4-01-e2e-79f46b`. The W3-14 draft (`worktree-agent-a7d6ff86722d24ea4`, 3abbbeb9) was merged in as e4929d78. Main was merged in as 3f0046c9 (17b5a67e backend-risk-fixes D-237, migrations 49801/49802; 3894636a W4-05 translations), so the green runs cover what ships.
- **Where it ran:** locally, against this worktree's own `make dev` stack, under `scripts/no-egress.sh`. Nothing touched a cloud account; nothing was pushed.
- **Product code:** none changed. Seed data changed (below). The bugs and questions found are listed for the lead to route.
- **Tools:** this worktree's `.tools/bin` is its own copy (envtool rebuilt from this checkout), so main's envtool was never touched.

## What changed

By commit (all on this branch):

- **f0da1478, seed (`backend/internal/seed`, no migration):** Annapurna Provisions and Nandi Hardware get owners, `staff.ap_owner` and `staff.nh_owner` (reserved test numbers), in `identity.go` and `seedStaff` in `business.go`. E2E-21 signs in as AP's owner; the seed comments say so.
- **16dfee5b, E2E-21 (`22-e2e-21-asks`):** the booking half is no longer a fixme. AP's owner replies Yes at ₹100 (below the ₹110 listing, under the ₹120 MRP), the customer prices and books the reply with `askReplyId` (`priceSource: ask_reply`, unit ₹100), AP accepts and readies it, and the customer collects it with the code. The reply then points at the order. Fixtures: `PEOPLE.apOwner`, `STORES.AP`, `ANNAPURNA` in `e2e/fixtures/seed.ts`; globalSetup opens Annapurna all day too and globalTeardown restores its hours.
- **d8bd503f, fixes after run 1:**
  - E2E-01: the standee scan comes before the sign-up, as at the counter (a click after the account was made is not a referral, 01 §8, `referrals/claim.go` `pick`).
  - E2E-10 through A-12: opened with `openAdmin` (admin session and step-up), not a plain app page.
  - CR-15: a customer's ledger accounts are made at their first posting, so the walk-in first collects a ₹100 order (its cashback opens the wallet) before finance looks the wallet up on A-09.
  - E2E-08: weekly hours only in the field store form (holidays are the owner's, `FieldStoreHours`).
  - E2E-11: reads the whole catalogue in one page (SL2 lists 22 items).
  - E2E-21: the ask and reply response shapes.
  - `principals.ts`: an admin whose 12 h refresh token has expired under the moved test clock signs in again (01 §2), keeping the last authenticator step used.
- **36ebf36a, fixes after run 2:**
  - `principals.ts`: the admin TOTP is computed at the server's test-clock time, not wall time (E2E-09 signs in again after the clock moved).
  - E2E-08: the owner signs in only after the owner-confirm code. A sign-in code to the same number inside the 30 s resend gap made the agent's owner-confirm send answer 429 (codes are counted per number, 01 §2; `backend/internal/business/agent_owner.go:145`). The owner then submits the store (draft → pending_review): a field agent has no submit, and admin review needs pending_review (`backend/internal/admin/stores.go:294`).
  - E2E-11: the mock LLM fixtures `e2e/mocks/fixtures/llm/voice_stock/{stock-update-hinglish,fallback}.json` were still the W0 placeholder shape. They now answer with the `record_voice_stock_lines` tool in the shape of `backend/internal/voice/tool.go`. The mocks' Go tests (`anthropic_test.go`, `review_test.go`) and `e2e/mocks/README.md` follow the rename.
- **ecfd24a1, E2E-09:** before the month closes, a customer spends 30 credits per ₹100 order at SL until Lakshmi's credit account is above 0 (finance tops the customer up through a credit adjustment when its wallet has fewer than 30). Without it the October close of Lakshmi Traders was −21 credits (credits in 60, cashback out 111, referral in 30): no payout, nothing to mark paid.
- **03e2de40, E2E-09:** mark-paid answers with `AdminPayoutDetail` (`{ payout, lines, causes, … }`); the spec reads `payout.status` and `payout.utr`.
- **993303a8, E2E-09:** B-10 Credits needs a current shop; the owner picks Sri Lakshmi on `/choose-shop` first (as E2E-03 does), and the spec reads "Paid" and "UTR …" on that month's statement row (`statement-<period>`).

- **5b5befc6, `customerFirstRun`:** a signed-in customer page refreshes its session on load; the helper now waits for that refresh to land before the journey navigates on. In run 6 the next `goto` aborted the refresh (API 499) after the server had rotated the token, so the page kept the old cookie: 401 inside `RotationGrace`, and in the next test, past it, "refresh token reuse; session family revoked". Every later step as Asha then failed with 401.

## Evidence

Each run starts from a fresh `make reset` (migrated to head, dev seed). The gate is:

```
scripts/no-egress.sh make gen e2e-journeys
```

39 tests, one worker, `retries: 0`.

| Run | Code | Passed | Failed | Fixme | Skipped | Time | Gate rc | Failed journeys |
|---|---|---|---|---|---|---|---|---|
| 1 | 16dfee5b | 32 | 7 | 0 | 0 | 3.8m | 2 | E2E-01, E2E-10 A-12, CR-15, E2E-08, E2E-11, E2E-21, E2E-09 |
| 2 | d8bd503f | 36 | 3 | 0 | 0 | 6.3m | 2 | E2E-08 (owner-confirm 429), E2E-11 (voice draft invalid), E2E-09 (TOTP 422) |
| 3 | 36ebf36a + main (3f0046c9) | 38 | 1 | 0 | 0 | 7.2m | 2 | E2E-09 (no payout: the close was −21) |
| 4 | ecfd24a1 | 38 | 1 | 0 | 0 | 6.7m | 2 | E2E-09 (mark-paid response shape) |
| 5 | 03e2de40 | 38 | 1 | 0 | 0 | 6.7m | 2 | E2E-09 (B-10 without a current shop) |
| 6 | 993303a8 | 31 | 8 | 0 | 0 | 5.8m | 2 | E2E-13 C-07 and C-08, then every later step as Asha (E2E-15 ×2, E2E-18, E2E-20, E2E-11, E2E-14): Asha's session family was revoked (below) |
| **7** | 5b5befc6 | **39** | 0 | 0 | 0 | 6.5m | **0** | none |
| **8** | 5b5befc6 | **39** | 0 | 0 | 0 | 6.8m | **0** | none |

- **Contract (full):** `scripts/no-egress.sh make e2e-contract` (`run.sh full` through Caddy), after run 8: examples 271 passed, 51 skipped; coverage 322 passed; fuzzing 322 passed. 322 of 325 operations selected (`POST /v1/test/clock` and the two slow test routes excluded by `run.sh`), 27,544 cases generated and passed, "No issues found" in 209 s, rc 0. The stack was reset afterwards (`make reset`, rc 0).
- **No secrets in the output:** every run log (1–8), both targeted logs and the contract log were grepped for the reserved test-number prefix and `+91`: zero matches. The Playwright JSON report matched `+91` only inside base64 screenshot data, not as a phone. No OTP, pickup or counter code, bill token or phone is printed by the helpers.

## Results per journey

Runs 7 and 8, both on a fresh reset:

| # | Spec | Journey | Run 7 | Run 8 |
|---|---|---|---|---|
| 1 | `01-e2e-02-referral-credits` | E2E-02 referral credits, ₹300 order using 30, cashback 13, wallet 83, credit account +30 −13 | pass (4.2s) | pass (4.2s) |
| 2 | `02-e2e-03-partial` | E2E-03 partial accept → customer confirms the new total → collected | pass (807ms) | pass (2.7s) |
| 3 | `02-e2e-03-partial` | E2E-03 shop side in the business app: Accept part → Send to customer | pass (1.1s) | pass (3.9s) |
| 4 | `03-e2e-06-bill-qr` | E2E-06 POS walk-in with bill QR credits | pass (205ms) | pass (303ms) |
| 5 | `04-e2e-01-standee` | E2E-01 standee scan → search → pickup order → collect → review → store referral +30 | pass (2.4s) | pass (7.9s) |
| 6 | `05-e2e-10-review-hide` | E2E-10 admin hides a review → the public rating drops; restore brings it back (admin API) | pass (219ms) | pass (2.1s) |
| 7 | `05-e2e-10-review-hide` | E2E-10 the customer shop page (C-07) shows the rating | pass (640ms) | pass (3.5s) |
| 8 | `05-e2e-10-review-hide` | E2E-10 through A-12: support hides and restores a review on the Reviews screen; the public rating follows | pass (1.5s) | pass (4.2s) |
| 9 | `06-e2e-12-credit-cap` | E2E-12 before the change: 30 credits are used, 40 are refused with credit_max_exceeded | pass (84ms) | pass (86ms) |
| 10 | `06-e2e-12-credit-cap` | E2E-12 super admin sets credit_max_per_order to 40 with step-up; checkout allows 40; audit entry visible | pass (19.2s) | pass (13.4s) |
| 11 | `06-e2e-12-credit-cap` | E2E-12 through A-17: the super admin changes the cap on the settings screen; the history panel shows it | pass (1.2s) | pass (1.5s) |
| 12 | `07-e2e-13-baskets` | E2E-13 a basket one shop has in full → Fill cart → order → pickup; a split basket → two orders | pass (3.3s) | pass (2.3s) |
| 13 | `07-e2e-13-baskets` | E2E-13 build a basket from search results (C-05 → Add to basket) | pass (1.3s) | pass (1.2s) |
| 14 | `07-e2e-13-baskets` | E2E-13 build a basket from a shop page (C-07) | pass (2.3s) | pass (1.1s) |
| 15 | `07-e2e-13-baskets` | E2E-13 build a basket from a product page (C-08) | pass (2.2s) | pass (974ms) |
| 16 | `08-e2e-15-language` | E2E-15 language switch mid-journey keeps the cart and finishes the order | pass (3.7s) | pass (2.5s) |
| 17 | `08-e2e-15-language` | E2E-15 the screens read in Hindi after the switch | pass (1.6s) | pass (1.9s) |
| 18 | `08-e2e-15-language` | E2E-15 Home reads in Telugu after the switch | pass (1.0s) | pass (1.2s) |
| 19 | `09-e2e-16-offline` | E2E-16 offline customer sees the offline banner and keeps the page; it clears when back online | pass (597ms) | pass (562ms) |
| 20 | `10-e2e-17-owner` | E2E-17 multi-store owner sees both shops and can open each one’s orders | pass (1.2s) | pass (550ms) |
| 21 | `11-e2e-18-cashier` | E2E-18 cashier permissions: hand over yes; cancel, settings and the other shop no | pass (766ms) | pass (176ms) |
| 22 | `12-e2e-19-blocked` | E2E-19 blocked customer cannot sign in or order; unblock restores them | pass (1.0m) | pass (1.0m) |
| 23 | `13-e2e-20-suspended` | E2E-20 suspended shop: not in discover, no shop page, orders refused | pass (55ms) | pass (167ms) |
| 24 | `14-e2e-05-delivery` | E2E-05 delivery › customer books delivery to a saved address | pass (1.0s) | pass (1.8s) |
| 25 | `14-e2e-05-delivery` | E2E-05 delivery › shop accepts, dispatches with a rider, and the order is delivered | pass (1.6s) | pass (3.3s) |
| 26 | `14-e2e-05-delivery` | E2E-05 delivery › the full phone shows to the shop only while out for delivery | pass (155ms) | pass (137ms) |
| 27 | `15-cr-15-adjustments` | CR-15 +500 credits posts at once; +600 waits for a second finance admin, who approves it on A-09 | pass (7.7s) | pass (11.5s) |
| 28 | `16-e2e-08-field-onboarding` | E2E-08 field agent onboards a shop → owner confirms → admin verifies → live in search → first order | pass (397ms) | pass (1.6s) |
| 29 | `17-e2e-11-voice-update` | E2E-11 voice update: draft → edit → confirm → stock changes → customer sees the item confirmed | pass (1.5m) | pass (1.4m) |
| 30 | `18-e2e-13-list-parse` | E2E-13 AI: a typed list becomes basket lines, confirmed line by line | pass (59.9s) | pass (59.9s) |
| 31 | `19-e2e-14-bill-scan` | E2E-14 supplier bill scan → stock and batch → going to waste → flash offer → customer sees the offer | pass (1.0m) | pass (1.0m) |
| 32 | `20-e2e-04-no-show` | E2E-04 three no-shows → strikes → the open-order limit and its banner | pass (16.7s) | pass (17.0s) |
| 33 | `21-e2e-07-offline-code` | E2E-07 offline counter code → synced within 12 h → linked, cashback posted | pass (1.4s) | pass (301ms) |
| 34 | `21-e2e-07-offline-code` | E2E-07 variant: the same code used again → sale kept, link failed, no credits | pass (3.2s) | pass (3.1s) |
| 35 | `21-e2e-07-offline-code` | E2E-07 variant: device clock off by more than 2 min at its last check → link failed, no credits | pass (3.8s) | pass (3.2s) |
| 36 | `21-e2e-07-offline-code` | E2E-07 variant: synced after 12 h → link failed, no credits | pass (3.3s) | pass (3.4s) |
| 37 | `21-e2e-07-offline-code` | E2E-07 the shop sees "Customer link failed" on the sale in the business app | pass (2.0s) | pass (1.5s) |
| 38 | `22-e2e-21-asks` | E2E-21 stale result → ask nearby → shops reply → book AP’s Yes at ₹100 → collect with the code | pass (1.7s) | pass (591ms) |
| 39 | `30-e2e-09-statement-payout` | E2E-09 month-end statement → finance marks the payout paid → the business sees Paid with the UTR | pass (28.3s) | pass (32.6s) |

## Flaky-test quarantine

- **The list is empty.** There is no quarantine list or mechanism in the repo; the policy is text only (`docs/plan/08-testing.md:39`).
- Every Playwright config (`e2e/journeys`, and the customer, business and admin stack configs) has `retries: 0`: no test is retried into a pass.
- **Fixme: none.** No `test.fixme` is left in `e2e/journeys/specs` (E2E-21's booking half was the last one).
- Conditional skips (each with its reason in the spec), which are not quarantine and did not fire in runs 3–8:
  - `06-e2e-12-credit-cap` lines 14, 59, 124: the referee needs 40 credits (E2E-02 and E2E-06 run first).
  - `21-e2e-07-offline-code` lines 68, 104: the linked sale did not run.
  - `03-e2e-06-bill-qr` line 12: the referee has no credits (E2E-02 did not complete).
- `site/e2e/i18n.shots.spec.ts` (from main, W4-05) is not in the journeys package; it skips unless `TZ_LANG` is set.

## Bugs found and questions for the lead

None needed a product fix to get green; each is for the lead to route.

1. **The voice stock mock fixtures were stale** (fixed here, e2e only). `e2e/mocks/fixtures/llm/voice_stock/*.json` still used the W0 placeholder tool, so every mocked voice update ended `invalid` (no tool call matching `record_voice_stock_lines`, `backend/internal/ai/client/metered.go:41`). W3-04 changed the tool and did not update the mocks.
2. **A-09 cannot find or adjust a customer who has never had a ledger posting.** Accounts are created lazily at the first posting (`backend/db/queries/credits/credits.sql:12-19` `InsertAccount`), and the ledger browser only finds existing accounts (`backend/internal/credits/adjustments.go:107` `LedgerAccounts`). A finance adjustment for a customer who has never ordered has no account to find or post to. CR-15 works around it with a first order.
3. **Question: who submits a store a field agent onboarded?** The agent creates the store in `draft`; only an owner or manager can submit it (`backend/internal/business/stores.go:550` `submitStore`, with a `TODO(lead)` above it), and admin review requires `pending_review` (`backend/internal/admin/stores.go:294`). 01 §14 does not say. E2E-08 has the owner submit after the confirm; if the field flow should hand the store straight to review, that is a backend change.
4. **The owner-confirm code shares the sign-in OTP limits for the number** (`backend/internal/business/agent_owner.go:145`, `ResendGap`). If the shopkeeper asked for a sign-in code in the last 30 s, the agent's confirm send answers 429. Probably intended (01 §2 counts codes per number); noting it because it surprised the journey.
5. **E2E-21 scenario text vs seed:** the scenario says Nandi Hardware gets the ask, but the seed mutes asks at NH (`backend/internal/seed/asks.go:103`). The spec checks "at least 3 shops" (AP, SL, SL2). One of the two should change.
6. **E2E-09 depends on earlier journeys' credits.** The seed gives no customer credits and no payout fixtures, and Lakshmi pays its own cashback (SL 5 %, SL2 2 %), so the month close is often negative (`backend/internal/payouts/close.go:301`, no payout when the amount is ≤ 0). The spec now makes its own spend first. A seeded statement with a positive closing balance would make E2E-09 independent.
7. **A refresh whose response is lost signs the person out, and a later retry revokes the session family as theft.** If the browser aborts `POST /v1/auth/refresh` (a reload or navigation while it is in flight), the server has still rotated the token (`backend/internal/identity/session.go:181-192`, `RotationGrace` at `:44`), so the browser keeps the old cookie. Inside 30 s it gets 401 without the cookie being cleared; after 30 s the same cookie is treated as theft: that sign-in's session family is revoked and audited as reuse. A real customer who reloads at the wrong moment is signed out and raises a false theft alarm. The comment at `session.go:36-43` accepts this cost for a thief racing the owner, but not for a lost response. Options for the lead: answer a presentation within the grace window with the successor token (idempotent replay), or commit the rotation only after the response is written. Seen in run 6; the journeys now wait for the refresh (5b5befc6).
8. During E2E-19 (which passes) the API log shows 409 `invalid_transition` on block/unblock calls. Not investigated; probably a repeated call the spec tolerates.

## Notes for the lead

- The Colima VM's overlayfs failed during this WP (`overlayfs: failed to get metacopy (-5)`), which took down the main checkout's Postgres and MinIO and the shared test Postgres. The runs above started after the user restarted Colima.
- The contract fuzzer runs after the journeys (as in `make e2e`, `mk/22-journeys.mk:10-13`): it sends OTPs from the same IP and writes data the journeys do not expect. The stack was reset afterwards.
