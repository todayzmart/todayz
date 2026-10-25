# W3-01 🔒 Payouts and admin money: QA report

The work covers the backend in `backend/internal/payouts` and `backend/internal/credits/adjustments*.go` and the screens A-11 Payouts, A-09 Credits (admin) and B-10 Monthly statements (business). Branch `claude/w3-01-payouts-4e85e1`, commits 0be7309 … 2ef0e32 plus this report, not merged or pushed. Everything ran locally (testcontainers Postgres); nothing touched a cloud account.

## What was built

- **Schema (`30101_w3-01_payouts.sql`):**
  - `payout_statements` and `payout_statement_lines`, one statement per business and month (`ux_payout_statements_business_period`);
  - `payouts` and `payout_lines`, at most one open (pending or held) payout per business (`ux_payouts_open_business`), and one payout per UTR (`ux_payouts_utr`);
  - `admin_adjustment_requests` for the two-person flow. A request posts at most one transaction (`ux_admin_adjustment_requests_transaction`).
- **Statement close (`payouts_statement_close`):**
  - It runs on the 1st at 00:30 IST and closes the IST month before.
  - It is a money, Batched kind: each business commits on its own, and a rerun skips businesses that already have a statement.
  - Per store and kind it writes opening, credits in, referral in, cashback out, reversals, adjustments, payouts out and closing. Every statement must tie out (opening + in − out = closing) or the close fails.
  - It then recomputes the business's single open payout. The payable amount per store is `min(closing, balance now)` above 0, and the total is at most the closing. If nothing is payable, the open payout is cancelled.
  - A negative balance carries forward to the next opening and is never written off.
  - The job never moves money.
- **Holds:**
  - A payout is held for incomplete KYC or payout details, any open risk flag, a suspended business, or a manual hold by finance (reason required).
  - Hold and unhold need finance or super_admin, a reason and step-up. A close never lifts a manual hold.
- **Mark paid:**
  - It needs finance or super_admin, step-up, a UTR (`^[A-Z0-9]{12,22}$`), the amount the screen loaded (`payout_amount_changed` if a close moved it) and an `Idempotency-Key`.
  - It posts the `payout` transaction through the existing ledger functions (keyed `payout:<id>`, store_credit → `platform_payouts_cash`; it balances to zero and is audited).
  - A retry answers the first result and never pays twice.
- **Export:** a CSV of pending payouts with no open cause, for the bank or UPI batch.
  - Formula cells are escaped.
  - `exported_at` is stamped.
  - The audit row carries no bank details.
- **Statement CSV:** one month by store, with numbers raw (`TestStatementCSVKeepsNumbersRaw`).
- **Admin adjustments (A-09):**
  - Whole credits only, with a reason and an optional link. Finance and super_admin only, with step-up.
  - At or under `admin_adjust_dual_approval_above_paise` (read from `platform_settings`) the adjustment posts at once.
  - Above it, a pending request waits for a second finance or super_admin. Approver ≠ requester (`approver_is_requester`). The requester can withdraw.
  - Approve posts exactly once. Taking away more than the wallet holds is refused (`adjustment_insufficient_balance`).
- **Contract:**
  - `admin-payouts.yaml`: statements, payouts, hold/unhold, mark paid, export, and the B-10 business statements list and detail (JSON or CSV).
  - `admin-credits.yaml`: ledger accounts and entries, expiry preview, adjustments, approve and reject.
  - Problem codes are registered in `problems/admin-*.yaml`.
  - Authz rows have golden files (`policy/testdata/{payouts,credits}.golden`).
- **A-11 Payouts** (`admin/src/payouts/`):
  - It has a period picker (the last closed IST month by default) and statements and payouts tables with filters in the URL.
  - Mark paid uses step-up and an Idempotency-Key, and handles `payout_amount_changed`.
  - Hold and release ask for a reason. Export downloads the CSV.
  - Finance and super_admin act on payouts; the other roles that can see the page only read it.
- **A-09 Credits** (`admin/src/credits/`):
  - Ledger tab: account search by phone, store code or id. The phone is sent in a mutation and never goes into a query key or the URL.
  - Each account shows its ledger lines, and the page shows an expiry preview.
  - "Adjust credits" on a customer wallet: the toast says whether the adjustment was posted or is waiting for a second person.
  - Pending approvals tab: approve or reject with a note. Your own request shows "Needs a second person" and "Withdraw".
- **B-10 Monthly statements** (`business/src/statements/`, replaces W2-12's `StatementsSlot`):
  - Each closed month shows its closing balance and the payout state: processing, on hold for payout details, or paid with date and UTR.
  - Each month has a breakdown, with cashback and payouts shown as taken away.
  - A negative month shows a carry-forward note.
  - "Load older statements" pages on the cursor.
  - A CSV download is fetched with the session token (no token in a URL).
  - Owner and manager only, like the rest of B-10.
- **Runbook:** `docs/runbooks/payouts.md` (the close, reruns, holds, mark paid, export, alerts).

## Evidence

| Check | Result |
|---|---|
| gofmt, `go vet ./...` | clean |
| golangci-lint (`.tools/bin`) | 0 issues |
| `go test -race -count=1` (testcontainers) | pass: `internal/payouts` 13.7 s, `internal/credits` 65.7 s, `cmd/todayz` 249 s, `internal/platform/authz` and `/policy`, `internal/jobs` |
| Statement golden over a seeded month (`TestStatementGoldenDB`) | exact numbers per store and kind; ledger tie-out: Σ statement movements = Σ store_credit movements in the month |
| SC-PO-01, -02 (`TestOnePayoutPerBusinessDB`) | one open payout per business, recomputed by a later close; mark paid (UTR, amount, idempotent replay); the `payouts_out` line; carry forward |
| SC-PO-03 (`TestPayoutsHTTP`) | A-11 is readable by finance, admin and super_admin; only finance and super_admin act; step-up, reason, UTR and Idempotency-Key are enforced |
| SC-PO-04 (`TestExportDB`) | export columns, pending and uncaused payouts only, `exported_at`, escaped formula cells, audit without bank details |
| Holds (`TestHoldCausesAndCarryForwardDB`) | suspended, risk flag and KYC hold; a negative balance carries forward and pays nothing |
| Close job (`TestCloseJobDB`) | the registered kind closes the IST month before its run; a rerun closes nothing again |
| SC-CR-15 (`TestAdjustmentThresholdDB`, `TestAdjustmentTwoPersonApprovalDB`, `TestAdminCreditAdjustmentsHTTP`) | at or under the threshold posts; above waits; approver ≠ requester; approve posts once; roles and step-up over HTTP |
| Adjustment idempotency and balance (`TestAdjustmentIdempotencyKeyDB`, `TestAdjustmentInsufficientBalanceDB`) | a replay never posts twice; taking away more than the balance is refused |
| Unit (`payouts_test.go`) | IST month bounds and close period, INR formatting, CSV cell safety, hold reason order, tie-out, payable, reason cleaning, UTR pattern |
| Contract (`make gen`, bundle and lint) | CONTRACT |
| Admin vitest | 13 files, 113 tests pass (A-11: 8, A-09: 8) |
| Admin tsc, eslint | clean |
| Business vitest | B-10 7 tests; `language.test.tsx` passes after the translation approval (see Open 1) |
| Business tsc, eslint, stylelint | clean |
| `@todayz/i18n` `lint:messages` | 0 errors in every app; business hi/kn/te/ta 1433/1433 reviewed |
| `make check` | MAKE_CHECK |

## Screenshots

None. The screens need the local stack (`make dev`), which this session was told not to run. The component tests render each state: A-11 finance and read-only, mark paid, holds and export; A-09 ledger, adjust and approvals; B-10 paid, processing, on hold, negative, breakdown, CSV and errors.

## Files outside the owned paths

- `backend/db/queries/credits/adjustments.sql` and `backend/db/queries/payouts/payouts.sql` (sqlc queries for the owned packages).
- `backend/internal/platform/authz/policy/{credits,payouts}.go` and their golden files (authz rows).
- `backend/internal/jobs/payouts.go` (declares the close kind) and `backend/cmd/todayz/routes_payouts.go` (mounts the routes), plus `payouts_http_test.go`.
- `docs/runbooks/payouts.md`.
- `admin/src/app/nav/access.ts` (`payoutManage` and `creditAdjust`: finance and super_admin), `admin/src/shell/nav.ts` (`BUILT_SECTIONS` gains credits and payouts; admin CLAUDE.md says not to edit nav.ts), and `admin/src/routes/{credits,payouts}/index.tsx`.
- `business/src/credits/CreditsPage.tsx` (renders `StatementsCard`), `StatementsSlot.tsx` deleted, and one test in `credits.test.tsx` updated.
- i18n: `shared/i18n/messages/admin/{payouts,credits}.en.json`, `shared/i18n/messages/business/statements.{en,hi,kn,te,ta}.json`, and `credits.statements.soon` removed from `business/credits.*.json`.
- `docs/qa/business-i18n/REVIEW.md` (the approval row for the statement keys).

## Open

1. **Business translations: approved, with no native-speaker review yet.**
   - The lead approved the 30 new `statements.*` keys in hi/kn/te/ta under the founder's 25 Oct release instruction (2ef0e32), the same way as W3-09. They are recorded in `docs/qa/business-i18n/REVIEW.md` with a table of money words to spot-check.
   - Terms reuse `credits.*`: स्टेटमेंट, पेआउट, बैलेंस; ಸ್ಟೇಟ್‌ಮೆಂಟ್, ಪೇಔಟ್; స్టేట్‌మెంట్, పేఅవుట్; ஸ்டேட்மென்ட், பணம் வழங்கல். Placeholders and ₹ are unchanged. Hindi "paid" says "sent to you" (भेजा गया), so it can't read as the shop paying.
   - For a native speaker to read later:
      - Keys: `statements.loading`, `.error`, `.retry`, `.empty`, `.list`, `.closing`, `.status.processing`, `.status.needsKyc`, `.status.paid`, `.payout.processing`, `.payout.needsKyc`, `.payout.paid`, `.payout.utr`, `.payout.none`, `.negative`, `.totals.title`, `.totals.opening`, `.totals.creditsIn`, `.totals.referralIn`, `.totals.cashbackOut`, `.totals.reversals`, `.totals.adjustments`, `.totals.payoutsOut`, `.totals.closing`, `.download`, `.downloadFor`, `.downloading`, `.downloadError`, `.more`, `.moreError`.
2. **Questions for the lead (conservative reading taken; `TODO(lead)` in code where it applies):**
   - Hold causes (KYC, risk flag, suspended, manual) are read live in the payouts domain. Any open risk flag holds, whatever its kind.
   - Adjustments on `store_credit` are refused; A-09 adjusts customer wallets only. 03 puts `admin_adjustment_requests` on the customer's wallet.
   - The UTR format is `^[A-Z0-9]{12,22}$`, covering NEFT/RTGS UTRs (16–22) and UPI RRNs (12).
   - Periods are IST calendar months. The close on the 1st closes the month before in IST, not UTC.
   - The 03 data-model additions (manual hold, adjustment link) are not yet written into 03.
   - SC-PO numbering: 07 lists SC-PO-01…20, but 08 has one Payouts bullet. Tests are named SC-PO-01…04 as in the brief.
   - Hold and unhold require step-up and a reason; every admin money action requires a reason.
   - An adjustment replay with the same Idempotency-Key but a different body is a conflict, not a second posting.
   - Payable is per store `min(closing, balance now)` above 0, and the total is at most the closing.
   - Mark paid carries the loaded `amountPaise` and is refused if a later close changed it.
   - A backfilled close does not touch a payout that is already paid. A close never lifts a manual hold.
   - A store-limited business member sees only those stores' totals and no payout (the payout is the whole business's).
   - The masked payee (last 4 digits) is shown to every A-11 reader.
   - Mark paid may take a store's store_credit balance negative if cashback was given after the close. That balance carries forward.
   - The close is a Batched kind.
   - The audit side panel cannot show `payout` or `credit_adjustment` subjects yet: the admin-audit subject enum belongs to W3-05.
   - The `BUILT_SECTIONS` edit to `admin/src/shell/nav.ts` is outside the owned paths.
   - A-11's default period comes from the device clock (the last closed IST month), not `serverNow`.
   - The A-09 decision note reuses the shared "Reason" label.
   - The expiry preview uses the server's default window.
   - A-09 shows credits as ₹ (1 credit = ₹1; the contract's paise are ×100).

## Not verified

- Real bank or UPI payouts. Mark paid only records a UTR that finance types in; nothing calls a bank.
- The screens in a browser against the stack (no `make dev` in this session); see Screenshots.
