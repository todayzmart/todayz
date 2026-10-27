# Payouts and money

How shops are paid: monthly statements, payout holds, exports, and what "mark paid" means.

> As built on main at d641cbe6 (27 Oct 2026). Update this file in the same change as the code it describes.

## What users can do

| App | What |
|---|---|
| Shop app | See statements and payout status. While the shop is not checked yet, Credits and statements say "Payouts start after our team checks your shop" (`awaitingCheck`, D-274). Folders: [statements](../../business/src/statements/), [credits](../../business/src/credits/). |
| Admin console | List statements and payouts, hold or unhold a payout, export payout files, mark a payout paid by recording a bank reference (UTR). Folder: [admin/src/payouts](../../admin/src/payouts/). |
| Backend jobs | `payouts_statement_close` closes statements. |

## Rules

Source: [01-product-rules.md](../plan/01-product-rules.md) section 9.

- A statement closes on the 1st at 00:30 IST.
- Payout holds, the first that applies: business_suspended, shop_unchecked (the shop check of D-274 not done yet), payout_details_missing (no UPI or bank), risk_flag (D-225); manual from finance. KYC verification no longer holds a payout; kyc_incomplete stays only on payouts held before D-274.
- A close never lifts a hold: a payout held `shop_unchecked` stays held after the check until finance releases it on A-11, and later closes do not hold for it.
- Payout amount is defined in D-09. The export format is D-85.
- "Mark paid" only records a UTR. TodayZ makes no bank call; a person pays outside the system.

## How it works

```mermaid
flowchart LR
    Close[payouts_statement_close] --> St[payout_statements + lines]
    St --> Pay[payouts + payout_lines]
    Pay --> Hold{Hold?}
    Hold -- yes --> Wait[Held]
    Hold -- no --> Exp[Admin export]
    Exp --> Paid[Mark paid with UTR]
```

The causes are read in one query (`payouts.sql`, `checked` from `businesses.check_status`) and the reason order is in `causes.reason()`. A-11 shows each cause ("Shop not checked yet"), and the business payout state is `awaiting_check` while the open payout is held `shop_unchecked`.

Runbook: [payouts](../runbooks/payouts.md).

## Code

| Part | Where |
|---|---|
| Backend | `backend/internal/payouts` ([folder](../../backend/internal/payouts/)) |
| API paths | [admin-payouts.yaml](../../backend/api/paths/admin-payouts.yaml) (statements, payouts, exports, shop `B/statements`) |
| Tables | `payouts`, `payout_lines`, `payout_statements`, `payout_statement_lines` |

## Tests

Package tests, journey `30-e2e-09-statement-payout`. Reports: [W3-07](../qa/W3-07/REPORT.md).

## Status and known gaps

Live in code. No bank or payment-gateway integration.

## Decisions

D-09, D-85, D-93, D-225, D-274.
