# Payouts

Kind `payouts_statement_close` (owner W3-01, code in `backend/internal/payouts/close.go`, declared
in `backend/internal/jobs/payouts.go`), and the admin money actions on A-11 Payouts (hold, unhold,
mark paid, export) and A-09 Credits (adjustments). The general alert is
[Job failing](jobs-job-failing.md); a missed 1st-of-month run is [Cron missed](jobs-cron-missed.md).
The ledger side (store_credit, `platform_payouts_cash`, the reconcile) is in
[Credits ledger](credits-ledger.md).

## What it does

| Kind | Class | Schedule (IST) | Outcome |
|---|---|---|---|
| `payouts_statement_close` | money, batched | 1st of the month, 00:30 | Closes the month before for every business with a store_credit account and no statement for it: one `payout_statements` row with its lines per store and kind (opening, credits in, referral in, cashback out, reversals, adjustments, payouts out, closing), then the business's single open payout recomputed. Summary: `closed`, `skipped`, `empty`, `payouts`, `held`, `cancelled`. |

- **Idempotent.** `ux_payout_statements_business_period` allows one statement per business and
  month; a rerun lists only businesses without one, and a race on the same business rolls back and
  counts as `skipped`. The job never moves money: only **mark paid** posts the `payout`
  transaction, keyed `payout:<payout id>`, so a retried mark paid can never pay twice.
- **Batched.** Each business commits on its own and the run yields between businesses while the
  worker drains; the continuation carries on with the businesses still unclosed.
- **One open payout per business.** The close recomputes the open (pending or held) payout to
  what is payable now: per store `min(closing, current balance)` above 0, total at most the
  statement's closing. Nothing payable cancels it. A negative balance carries forward to the next
  statement's opening; nothing is written off.
- **Backfill.** Closing an older month after a later one exists writes the statement but leaves
  the payout alone.
- **Holds.** A payout is held while the business is suspended, its shop is not checked yet
  (D-274), it has no UPI or bank to pay to, or it has an open risk flag (`business_suspended` →
  `shop_unchecked` → `payout_details_missing` → `risk_flag`), or by hand
  (`manual`, with a note). The close never lifts a hold. Mark paid and export check the causes
  live, so a cause opened after the close still blocks the money.

## Admin money actions

All are finance or super_admin, need step-up auth, a reason, and are audited (`payouts.hold`,
`payouts.unhold`, `payouts.mark_paid`, `payouts.export`, `credits.adjustment_*`).

- **Export** (`GET /v1/admin/payout-exports?period=`): CSV of pending payouts without an open
  cause, with the decrypted UPI or bank details. Formula cells are escaped. The audit row carries
  ids and the count only, never bank details. Treat the file as secret: do not paste or mail it.
- **Mark paid** sends the UTR and the amount the admin sees. A different amount (the payout was
  recomputed since the screen loaded) is `payout_amount_changed`; reload and check. The same UTR
  again returns the paid payout; another payout's UTR is `utr_in_use`.
- **Unhold** is refused with `payout_hold_cause_open` while a cause is open: fix the cause (shop
  check on A-24, payout details, risk flag, suspension) first.
- **Adjustments** above `admin_adjust_dual_approval_above_paise` need a second finance or
  super_admin to approve; the requester cannot approve their own (`approver_is_requester`).

## First three checks

1. **A-21 Jobs**: open `payouts_statement_close`, then the latest run: the error, the attempt and
   the summary.
2. **A-11 Payouts** for the period: statements count against businesses with store credit,
   held payouts and their causes.
3. **The ledger** (read-only, as `todayz_owner` in a read-only transaction):
   `payout_statements` and `payout_statement_lines` for the business and period, `payouts` and
   `payout_lines`, and the store_credit `ledger_entries` in the month.

## Likely causes

- **"payouts: <month> has not ended"**: the run's period is the current month (a manual run with
  the wrong date); run it for the month before.
- **"payouts: statement of <business> does not tie"**: the per-kind lines do not sum to closing minus
  opening. A bug or a ledger write around the service; stop and run `ledger_reconcile`. Nothing
  for that business was written.
- **A payout stays held**: read its causes on A-11. A shop is checked on A-24 (Shop checks), risk
  flags are cleared on A-13, payout details are the owner's; the payout follows at unhold or the next close.
- **Mark paid refused with `payout_not_payable`**: the payout is held or cancelled.
