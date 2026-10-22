# MQ-06 Money month

A seeded month of store credit activity closes on the 1st. Finance reviews the statements, holds and releases payouts, exports the batch and marks one paid. The business sees "Paid" with the UTR. A second month shows the one-open-payout rule and a negative balance carrying forward.

- **Covers:** 08 §5 MQ-06; E2E-09; SC-PO (month close numbers, negative carry-forward, KYC missing → held, mark paid needs UTR + step-up, CSV matches); PO-01, PO-02, PO-03 manual side; CR-16.
- **Screens:** A-01, A-11, A-13, A-19, A-03; B-10, B-14, B-16.
- **Devices:** finance D1440 (profile D); business D1280 (profile B).
- **Time:** about 60 minutes.

The worked example uses October 2026 as month 1 (M1) and November 2026 as month 2 (M2). On another date use the current IST month and the next one.

## Preconditions

1. `make reset`; `clock reset`.
2. Seed `<seed:month-SL>`, `<seed:month-others>`, `<seed:payout-history>` and `<seed:risk-flags>` loaded.
3. FN45 and FN46 (finance), AD47 (admin), SA48 (super_admin), S42 (support), Y44 (analyst) have TOTP enrolled (README §2).
4. Expected M1 numbers for Lakshmi Traders (opening ₹0):

   | Store | Credits in | Cashback out | Closing |
   |---|---|---|---|
   | Sri Lakshmi | ₹1,000 spent + ₹30 referral reward = ₹1,030 | −₹290 | ₹740 |
   | Sri Lakshmi 2 | ₹170 | −₹20 | ₹150 |
   | **Business** | **₹1,200** | **−₹310** | **₹890** |

## Steps

### A. Before the close (profile B, O20)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-06.01 ★ | Sign in as O20, open B-10 Credits and referrals for Lakshmi Traders. | Credit account balance ₹890. Lines grouped by store; "spent at your shop" lines show +, cashback lines show −, the referral reward shows +₹30 on Sri Lakshmi. | `R-9.1` `R-9.2` B-10 | ▢ | |
| MQ-06.02 | Switch the store filter to Sri Lakshmi 2. | Balance part ₹150: +₹170 and −₹20. | `R-9.1` B-10 | | |
| MQ-06.03 | Open Statements. | Past month (two months ago) shows "Paid" with UTR `QAUTR0000000001`. No statement for M1 yet. | `R-9.3` `R-9.5` B-10 | ▢ | |
| MQ-06.04 | Sign in as M21 (manager), open B-14 → Payout details. | PAN, GSTIN and UPI shown masked to the last 4 characters; no edit control. | `R-3.6` `R-3.8` D-44 B-14 | ▢ | |
| MQ-06.05 | Sign in as K22 (cashier). Open the More menu. | No Credits and referrals entry and no payout details. Calling `GET {B}/kyc` with K22's session → 403 (payout details live under `{B}/kyc`). | `R-3.9` AU-10 | | |

### B. The close (profile D, FN45)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-06.06 | `clock set 2026-10-31T23:59:30+05:30`. Sign in as FN45. Open A-11 Payouts, period October 2026. | No statements for October yet (the month has not closed). | `R-9.3` A-11 | | |
| MQ-06.07 ★ | `clock set 2026-11-01T00:30:30+05:30` (statements close at 00:30 IST on the 1st, 01 §9). Due jobs run at once. Refresh A-11 for October 2026. | One statement per business with store credit activity. Lakshmi Traders row: opening ₹0, in ₹1,200, cashback ₹310, closing ₹890, KYC verified, flags none, status `pending`. | `R-9.3` `R-9.4` SC-PO A-11 | ▢ | |
| MQ-06.08 ★ | Open the Lakshmi Traders statement. | Per-store lines exactly as in precondition 4 (₹740 and ₹150); supporting ledger lines listed under each store with the order number or POS sale for each. Bank and UPI details masked (last 4). | `R-9.1` `R-9.3` PO-03 A-11 | ▢ | |
| MQ-06.09 | Read the Kaveri Fresh row. | Opening −₹40 (carried from last month), in ₹100, cashback ₹20, closing ₹40, status `held` with reason "open fraud flag" (the seeded `store_strikes` flag). | `R-9.5` `R-9.6` SC-PO A-11 | ▢ | |
| MQ-06.10 | Read the Auto Mart row. | Closing −₹70 shown in red. No payout created (amount not above ₹0). | `R-9.4` `R-9.5` CR-16 A-11 | ▢ | |
| MQ-06.11 ★ | Read the Annapurna Provisions row. | Closing ₹200, KYC "missing", payout status `held` with `hold_reason` `kyc_incomplete` (a payout row is created and held, 01 §9). | `R-9.4` `R-9.6` PO-02 A-11 | ▢ | |
| MQ-06.12 | Read the Nandi Hardware and Sri Lakshmi 3 data (no credit activity). | No statement row, or a zero row; no payout. Record which. | `R-9.3` | | |
| MQ-06.13 | In profile B (O20) refresh B-10 → Statements. | October 2026 statement: closing ₹890, status "Pending" (not "Paid"). | `R-9.5` B-10 | ▢ | |
| MQ-06.14 | In profile B sign in as O28 (Auto Mart). Open B-10. | Balance −₹70 in red. October statement closing −₹70; no payout. | CR-16 B-10 | ▢ | |

### C. Hold, unhold and the fraud flag (profile D)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-06.15 | As FN45 click Hold on Lakshmi Traders with reason "QA check". | Status `held`, reason shown. Audit row (A-19) with FN45 as actor. | `R-9.6` PO-03 A-11 A-19 | ▢ | |
| MQ-06.16 | Click Unhold. | Status `pending`. Audit row. | PO-03 A-11 | | |
| MQ-06.17 | Sign out. Sign in as AD47 (admin). Open A-11 and the Lakshmi Traders payout. | The screen opens. No Hold, Unhold, Mark paid or Export buttons: export is for finance and super_admin only, with step-up (D-85). | `R-13.2` PO-03 A-11 | ▢ | |
| MQ-06.18 | As AD47 call `POST /v1/admin/payouts/<id>/mark-paid` with `{"utr":"QAUTR0000000002"}` and an Idempotency-Key. | 403. Payout still `pending`. | PO-03 | | |
| MQ-06.19 | As AD47 call `POST /v1/admin/payouts/<id>/hold` with a reason. | 403. | PO-03 | | |
| MQ-06.20 | As AD47 open A-13 Risk → the Kaveri Fresh `store_strikes` flag → Clear with reason "QA reviewed". | Flag cleared; audit row. | `R-13.1` A-13 | ▢ | |
| MQ-06.21 | Sign in as FN45, open the Kaveri Fresh payout, Unhold. | Status `pending`, amount ₹40. | `R-9.6` PO-03 A-11 | | |
| MQ-06.22 | Sign in as S42 (support) and open https://admin.todayz.localhost/payouts. | "You don't have access to this". No payout data loads. API `GET /v1/admin/payouts` → 403. | `R-13.2` AD-02 PO-03 | ▢ | |
| MQ-06.23 | Sign in as Y44 (analyst) and open the same URL. | "You don't have access to this". | `R-13.2` AD-02 | | |

### D. Export and mark paid (profile D, FN45)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-06.24 ★ | As FN45 click CSV export for October 2026. | A CSV downloads. One row per `pending` payout: Lakshmi Traders ₹890.00 and Kaveri Fresh ₹40.00. The held Annapurna payout is not in the file. Exporting stamps `exported_at` on those rows. | `R-9.4` SC-PO A-11 | ▢ | |
| MQ-06.25 | Open the CSV in a text editor. | Amounts equal the A-11 statement closing amounts. No cell starts with `=`, `+`, `-` or `@` without a leading `'`. Columns are exactly `payout_id, period, business_id, legal_name, beneficiary_name, amount_inr, upi_id, bank_account_number, ifsc`, with `amount_inr` at 2 decimals. | `R-9.4` SEC | | |
| MQ-06.26 | `clock +11m` (the last TOTP is now 11 min old). Click Mark paid on Lakshmi Traders, type UTR `QAUTR0000000003`. | Step-up prompt: the TOTP is asked again. The API without step-up returns `step_up_required`. | `R-9.5` AU-12 `step_up_required` A-11 | ▢ | |
| MQ-06.27 | Cancel the step-up. Click Mark paid again and leave the UTR empty. | Refused: the UTR is required. Status still `pending`. | `R-9.5` SC-PO A-11 | | |
| MQ-06.28 ★ | Enter UTR `QAUTR0000000003`, pass the TOTP step-up, confirm. | Status `paid` with the UTR, paid-by FN45 and the time. Audit row in A-19. | `R-9.5` E2E-09 A-11 A-19 | ▢ | |
| MQ-06.29 | Replay the same mark-paid request with the same Idempotency-Key (DevTools → Copy as cURL). | Same response; one audit row only; status still `paid`. | `R-9.5` | | |
| MQ-06.30 | Replay with the same Idempotency-Key and a different UTR. | `422 idempotency_mismatch`. | `idempotency_mismatch` | | |
| MQ-06.31 | Click Mark paid on the already paid payout (open its URL). | No Mark paid button; the API returns 409 `invalid_transition`. | `R-9.5` `invalid_transition` | | |

### E. The business sees "Paid" (profile B)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-06.32 ★ | As O20 refresh B-10 → Statements. | October 2026: "Paid", UTR `QAUTR0000000003`, the amount ₹890. | `R-9.5` E2E-09 B-10 | ▢ | |
| MQ-06.33 | Open the mocks push tab and B-16. | One push and one inbox item "payout paid" for O20, linking to the statement. | `R-15.8` B-16 | ▢ | |
| MQ-06.34 | Balance on B-10. | ₹0 (₹890 − the ₹890 payout). | `R-9.2` B-10 | | |

### F. Month 2: one open payout, recompute, KYC fixed (both profiles)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-06.35 | `clock set 2026-11-10T11:00:00+05:30`. As C11 place a delivery order at Kaveri Fresh for ₹200 with 30 credits to "Home"; accept, dispatch and deliver it as O26. | Delivered. KF store credit +30, cashback −3 (floor((20000 − 3000) × 200 / 1,000,000) = 3). KF balance ₹67. | `R-7.6` `R-7.7` | | |
| MQ-06.36 | Do not pay the Kaveri Fresh October payout (₹40, `pending`). | | | | |
| MQ-06.37 | As X25 (Annapurna owner) open B-14 → Payout details, enter PAN `ABCPX1234K`, UPI `annapurna.qa@upi`, legal name "Annapurna Provisions". Save. | Saved; masked; UPI status "Pending admin check". | `R-3.6` B-14 | | |
| MQ-06.38 | As AD47 open A-03 → Annapurna → KYC, mark the UPI ID verified. | KYC verified; audit row. | `R-3.6` `R-13.3` A-03 | | |
| MQ-06.39 | As FN45 Unhold the Annapurna payout. | `pending`, ₹200. | `R-9.6` PO-02 | | |
| MQ-06.40 ★ | `clock set 2026-12-01T00:00:30+05:30` (and `clock +6h` if needed). Open A-11 for November 2026. | Kaveri Fresh: November statement closing ₹67. Its October payout is recomputed to ₹67 (balance ₹67 − other unpaid ₹0). There is still exactly one open payout for Kaveri Fresh; no second row. | `R-9.4` PO-01 A-11 | ▢ | |
| MQ-06.41 | Read the Lakshmi Traders November row. | Opening ₹0 (after the ₹890 paid), closing ₹0, no new payout. | `R-9.4` PO-02 | | |
| MQ-06.42 | Read the Auto Mart November row. | Opening −₹70 (carried forward), closing −₹70; no payout. | `R-9.5` CR-16 | ▢ | |
| MQ-06.43 | Read the Annapurna row. | One open payout of ₹200, recomputed; no second row. | PO-01 PO-02 | | |
| MQ-06.44 | Mark the Kaveri Fresh payout paid as FN46 with UTR `QAUTR0000000004` (step-up). | `paid` ₹67. O26 sees "Paid" with the UTR in B-10. | `R-9.5` E2E-09 | ▢ | |

## Cleanup

1. `clock reset`.
2. `make reset` (payouts, KYC and orders from this script remain otherwise).
