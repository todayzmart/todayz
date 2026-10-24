# Credits ledger

Kinds `credits_expire`, `credits_expiry_reminder` and `ledger_reconcile` (owner W2-01, code in
`backend/internal/credits/jobs.go`, declared in `backend/internal/jobs/credits.go`), and the
referral rewards that `referral_sweep` posts (owner W2-03, see [Referral rewards](#referral-rewards)). The general
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
| `negative` | A customer wallet or held balance below 0. |
| `lots_vs_balances` | Per customer, Σ lot (remaining + held) ≠ wallet + held. |
| `holds` | An open hold whose lots do not hold exactly amount − released, or a settled hold whose lots still hold credits. |
| `held_vs_holds` | Σ (amount − released) of a customer's open holds ≠ their held balance. |
| `cache_drift` | `account_balances` ≠ Σ `ledger_entries` for an account. |
| `lot_drift` | A lot's remaining ≠ original + Σ its remaining deltas, or its held ≠ Σ its held deltas. |

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
- **Drift above 0**: a bug, or a write that went around the service. `todayz_app` cannot update or
  delete accounts, transactions, entries or consumptions, cannot set a balance or a lot's
  remaining or held (only the triggers move them, D-123), and updates only hold rows and a lot's
  `expired_at` and `reminded_at`. A lot inserted with no consumption behind it shows as
  `lots_vs_balances`. Find the first transaction where the account's Σ entries stop matching.
- **An order collected with no cashback during a deploy**: an order written by an old pod in a
  rolling deploy of migration 20202 gets `orders.cashback_bp` 0, so it earns no cashback (D-124).
  This is by design (no rate the customer was not shown); a goodwill top-up is an
  `admin_adjustment`.
- **`tz_credits_risk_flag_failures_total` rising**: cap-refusal risk flags are not being written
  (the 2 s timeout or a database error; the log line `credits: risk flag not written` carries
  the rule). Orders are not affected (D-126). Usually the API pool is exhausted; check
  `tz_db_pool_*` and the slow-query panel.

## Fix

- Never edit ledger rows by hand. A correction is an `admin_adjustment` posting (W3-01, two
  people) against `platform_adjustments`, with the reason in the memo.
- `ledger_reconcile` fixes nothing; re-run it (super_admin with step-up, A-21) after the fix to
  bring the gauge back to 0.
- `credits_expire` and `credits_expiry_reminder` catch up on their next run; running them by hand
  is safe.

## Referral rewards

Each side of a qualified referral is one `referral_reward` transaction (D-169):
- `platform_marketing` −N to the customer's `customer_wallet` (with a lot) or the store's `store_credit`, +N;
- `ref_type` is `referral` and `ref_id` the referral;
- the key is `referral:<referral id>:<referrer|referee>`.

It posts in the purchase's transaction. If it fails, the purchase still commits and the side waits.
`referral_sweep` (every 10 minutes, `backend/internal/jobs/referrals.go`) posts what is waiting. Its summary `rewarded` counts the referrals it finished.

- **Rewards waiting a long time** (`referrals.status = 'qualified'` with a `*_rewarded_at` still empty):
  - Look for the log line `referrals: reward not posted`. It carries the referral id, the side and the error. A database or ledger outage clears itself on the next sweep.
  - A blocked customer referrer waits by design (D-168).
  - A store referrer whose business has no live store waits by design too.
- **`referral.reward_refused` risk flag** (the log line `referrals: reward refused by the ledger`, at error level): the ledger will never accept this posting as it is. Usually the key is already used by a transaction with other legs (`credits: idempotency key reused`).
  1. Read the existing transaction for that key and its entries, read-only, as `todayz_owner`.
  2. Compare it with the referral's `referrer_credits`/`referee_credits` and its beneficiary.
  3. The sweep skips the whole referral while the flag is `open` or `confirmed`. Never edit or delete the ledger rows. If money is owed or was overpaid, correct it with an `admin_adjustment` (two people).
  4. Then either:
     - set the flag to `confirmed` to keep the referral parked for good; or
     - fix the cause and clear the flag, and the next sweep tries once more. If it is refused again, a new flag is raised; the sweep does not loop.
