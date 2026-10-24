# Credits ledger

Kinds `credits_expire`, `credits_expiry_reminder` and `ledger_reconcile` (owner W2-01, code in
`backend/internal/credits/jobs.go`, declared in `backend/internal/jobs/credits.go`). The general
alert is [Job failing](jobs-job-failing.md). The ledger's own alert is **Ledger invariant**
(05 §6): `tz_ledger_reconcile_drift` above 0 for any `reason`, or a log line with
`event=ledger_invariant`. It is critical.

## What they do

| Kind | Class | Schedule (IST) | Outcome |
|---|---|---|---|
| `credits_expire` | money | hourly at :05 | Every lot whose `expires_at` has passed goes to `platform_expired` (one `expiry` transaction per customer and expiry instant, key `expiry:<customer>:<unix>`). Summary: `customers`, `credits`. |
| `credits_expiry_reminder` | notify | daily 10:00 | One `credits_expiring` notice per customer for the lots that expire within `credit_expiry_reminder_days`; those lots get `reminded_at`, so a lot is reminded once. Summary: `reminded`. |
| `ledger_reconcile` | money | daily 03:30 | Reads only. Counts rows breaking each invariant (03 §6) and records them on `tz_ledger_reconcile_drift{reason}`. Summary: one count per reason. |

Every run is idempotent. A run works at most 20 batches of 100 customers; the next run carries on.
Holds and spends also expire a customer's due lots inline, so a late `credits_expire` never lets
anyone spend an expired credit. Job arguments carry no customer data.

## The reconcile reasons

| `reason` | Means |
|---|---|
| `unbalanced` | A transaction whose entries do not sum to 0. The trigger should make this impossible. |
| `negative` | A customer wallet, held balance or lot amount below 0. |
| `lots_vs_balances` | Σ lot remaining ≠ the wallet, or Σ lot held ≠ the held balance. |
| `holds` | A hold whose amounts break its status (captured + released > amount, a closed hold with credits left). |
| `held_vs_holds` | Σ open holds ≠ the customer's held balance. |
| `cache_drift` | `account_balances` ≠ Σ `ledger_entries` for an account. |
| `lot_drift` | A lot ≠ its original amount + Σ its consumptions. |

## First three checks

1. **A-21 Jobs**: open the kind, then the latest run: the error, the attempt and the summary.
2. **Grafana**: `tz_ledger_reconcile_drift` by `reason`, and the logs with
   `event=ledger_invariant` (they carry counts only, never a customer).
3. **The ledger** (read-only, as `todayz_owner` in a read-only transaction): the latest
   `ledger_transactions` for the account and their `ledger_entries`, `credit_holds`,
   `credit_lots` and `credit_lot_consumptions`.

## Likely causes

- **"credits: not configured"**: the worker started without the database or settings.
- **"no notification sender for the reminder"**: the worker could not build notify (boot log).
- **A setting cannot be read** (`credit_expiry_days`, `credit_expiry_reminder_days`): fix the
  row in `platform_settings` (A-20).
- **Drift above 0**: a bug, or a write that went around the service. The tables are append-only
  for `todayz_app` except the balance, lot and hold rows the service updates. Find the first
  transaction where the account's Σ entries stop matching.

## Fix

- Never edit ledger rows by hand. A correction is an `admin_adjustment` posting (W3-01, two
  people) against `platform_adjustments`, with the reason in the memo.
- `ledger_reconcile` fixes nothing; re-run it (super_admin with step-up, A-21) after the fix to
  bring the gauge back to 0.
- `credits_expire` and `credits_expiry_reminder` catch up on their next run; running them by hand
  is safe.
