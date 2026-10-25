# ADMIN Admin console screens

Each admin screen A-01 to A-22 opened and its main actions run once, as the role that owns them. MQ-07 covers sign-in, TOTP, roles, credit adjustments, settings and admin users; MONEY covers A-09 and A-10; CANCEL covers A-07 cancel, A-12 and A-14; OFFER covers A-20; JOBS covers A-21 and A-22; NOTIFY covers A-16. This file covers what is left, plus the privacy rules on aggregated data.

- **Covers:** SC-AD, SC-BZ (admin side), SC-RK, SC-SP; AU-11, AU-12; R-3.3, R-3.4, R-13.1 to R-13.3, R-16.1, R-16.3, R-16.5; D-40, D-41, D-64, D-81.
- **Screens:** A-01 to A-22.
- **Devices:** D1440 (profile D); D1280 for the business side (profile B, O20).
- **Time:** about 2 hours.

## Preconditions

1. `make reset`. Seeds `<seed:admins>` (TOTP enrolled), `<seed:stores>`, `<seed:risk-flags>`, `<seed:tickets>`, `<seed:history-SL>`, `<seed:wanted-nearby>`, `<seed:pending-product>`.
2. AD47 for most steps; the role is named where it differs.

## Steps

### A. Sign-in, Pulse, privacy

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ADMIN-M.01 | Sign in as AD47 with OTP only (stop at the TOTP prompt). In another tab call `GET /v1/admin/pulse?range=7d` with that session. | Refused (`401` or `403`): no admin endpoint without TOTP. After TOTP the same call returns 200. | AU-11 `R-2.5` A-01 | | |
| ADMIN-M.02 ★ | Open A-02 Pulse, range 7 days. | KPI strip: GMV, orders, success rate, active customers, live stores, new stores. Trend charts, funnel, D7 retention, city map, zero-result searches, stores needing attention. | `R-13.1` A-02 | ▢ | |
| ADMIN-M.03 | Zoom the A-02 city map into Indiranagar. | Demand heat shows as area cells (H3 resolution 8), never as single points or addresses. No customer name or phone anywhere on Pulse. | `R-16.1` `R-16.3` A-02 | ▢ | |
| ADMIN-M.04 | Read the zero-result searches list. | Search terms and counts only; no person. A term searched by fewer than 10 people in an area is not shown with its area (record the rule the build applies). | `R-16.3` `R-11.7` | | |
| ADMIN-M.05 | `set insight_wanted_nearby_radius_m = 500` (AD47; unit meters). As O20 open B-03 → Demand insights → Wanted nearby. Restore 2000. | With 500 m, Horlicks shows (as "within 500 m") only if the cells inside 500 m add up to 10 or more; otherwise no row. At 2000 m: "**Horlicks 500 g**: 12 searches within 2 km this week. No shop nearby has it." and no Bournvita row (9). The count is summed over cells first, then k ≥ 10 applies to the total. | `R-16.5` `R-12.8` IN-06 D-81 B-03 | ▢ | |

### B. Businesses and stores

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ADMIN-M.06 ★ | Open A-03. Read the tabs Submitted, Needs info, Verified, Suspended. Open the SB business (submitted). | SB in Submitted, NI in Needs info. Detail: profile, stores, staff, KYC masked, documents, activity, credit account, orders, notes. | `R-3.3` A-03 | ▢ | |
| ADMIN-M.07 | On SB pick Request info, tick "Store photo" and "GSTIN", message "QA: please re-upload". As SB's owner open the business app. | SB moves to Needs info. The owner sees exactly those two fields and the message. | `R-3.3` SC-BZ | ▢ | |
| ADMIN-M.08 | Review SB's PAN document: approve; storefront photo: reject with reason. | Each document shows status and reviewer. Audit rows. | A-03 | | |
| ADMIN-M.09 | Verify NI after its owner resubmits (or verify SB directly if the build allows from Needs info; record). | Business `verified`. Its live store appears in C-03 search within 1 minute at the QA point. | `R-3.4` SC-BZ D-40 | | |
| ADMIN-M.10 | Suspend Annapurna Provisions with reason "QA ADMIN-M.10". Reinstate it. | Suspended: gone from search; open orders still complete (CANCEL-M.22). Reinstated: back in search. Both audited with reason. | `R-3.3` BK-29 A-03 | | |
| ADMIN-M.11 ★ | Open A-05 → Review tab. Open PR (new branch, `pending_review`). Send it back with note "QA: blurry photo". Then, after the owner re-submits, approve. | Send back: store stays hidden, owner sees the note. Approve: `live`, visible at the QA point within 1 minute. | `R-3.4` SR-01 D-40 A-05 | ▢ | |
| ADMIN-M.12 | A-05 filters: status `paused`, then "stale". | PS listed under paused. Stale lists stores whose stock is older than 72 h (Sri Lakshmi's I7 makes it partly stale; record the rule). | A-05 `R-4.6` | | |
| ADMIN-M.13 | Edit Sri Lakshmi's hours in A-05 to close at 21:00 today. Force-close Kaveri Fresh. Then undo both. | C-07 shows the new hours. Kaveri Fresh shows closed and refuses orders with `store_closed`. Audited. | A-05 BK-28 `store_closed` | | |
| ADMIN-M.14 | A-05 → Kaveri Fresh (has the `store_strikes` flag) → Reset strikes, reason "QA ADMIN-M.14". | Strike count 0; reliability recalculates on the next run; audit row. | `R-5.28` D-41 A-05 | | |

### C. Customers, orders, catalogue

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ADMIN-M.15 | A-06: search `9000000010` (C10 without +91), then `+91 90000 00010`, then `90000 000`. | Exact match only: the full number finds C10 (record which formats are accepted); a partial number finds nothing. | A-06 `R-16.2` | | |
| ADMIN-M.16 | Open C10 → Devices/sessions. Revoke the session on profile A. | Profile A is signed out at its next call. Audit row. | `R-2.7` A-06 | | |
| ADMIN-M.17 | Block C10 with reason "QA". Try to sign in as C10. Unblock. | `account_blocked` with the support link. After unblock C10 signs in. | AU-17 A-06 `account_blocked` | | |
| ADMIN-M.18 | A-07: search `TZ-` plus a known order number, then filter by store Sri Lakshmi and status `collected`, date today. Open one. | Detail: full timeline with actors and reasons, code attempts (count only, never the code), credit holds, notifications sent. | `R-13.1` BK-31 BK-38 A-07 | ▢ | |
| ADMIN-M.19 | A-08 (G43): open Categories, rename "Dairy" in Kannada, change its tint. Open Synonyms, add "makhan" → butter. | Customer app in Kannada shows the new name. Search "makhan" finds Amul Butter. | A-08 SC-SR | ▢ | |
| ADMIN-M.20 | A-08 bulk import (AD47; G43 should not see it): a 10-row product file with 1 bad row. | Preview lists the bad row; apply adds 9 master products, no stock anywhere. G43 sees no Import action and gets `403` on `POST /v1/admin/product-imports`. | `R-4.2` A-08 D-64 | | |
| ADMIN-M.21 | With `ai_catalogue_enabled` false open the A-08 review queue detail. | No assist panel (the `<AssistSlot/>` is hidden). Same for A-13 with `ai_riskexplain_enabled` false and A-14 with `ai_triage_enabled` false. | `R-17.3` A-08 A-13 A-14 | | |

### D. Payouts, risk, support, settings, audit

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| ADMIN-M.22 | A-11 (FN45): pick last month. Open Kaveri Fresh's statement. | Columns: business, opening, in, cashback, closing, KYC, flags, status. Detail: per-store lines and supporting ledger. Bank details masked. | `R-9.1` A-11 | ▢ | |
| ADMIN-M.23 ★ | A-13 Risk (AD47). The seed has no flags (D-225), so raise them the way the rules do, from the rule fixtures' conditions or `make demo` traffic: at Kaveri Fresh, enter wrong pickup codes until more than `risk_code_lockouts_store_per_day` orders lock today, or let it miss orders until `store_strikes` fires; wait for the next `risk_scan` (5 min). Open each flag. Clear one with a reason. Confirm the Kaveri Fresh flag with a reason, then press **Suspend business** as its own step. Finally clear the confirmed flag with a reason. | Evidence shown per flag; a business or store flag says "Holds payout". Clear closes it. Confirm records the verdict, suspends no one and keeps holding Kaveri Fresh's payout (A-11 shows the risk-flag hold). **Suspend business** suspends Kaveri Fresh (gone from search), audited on the business. Clearing the confirmed flag lifts its hold: A-11 then lets finance Unhold the payout once nothing else holds it. Every step is audited. Undo the suspension in A-03. | `R-13.1` `R-5.26` SC-RK A-13 | ▢ | |
| ADMIN-M.24 | A-13 as S42. | Queue and evidence visible; no Clear or Confirm (read-only). API `403` on clear. | D-64 A-13 | | |
| ADMIN-M.25 | A-14: open the seeded business ticket. Read the SLA timer. Reply, then close. | SLA timer counts from creation (record the target). The business sees the reply in B-15. | SC-SP A-14 B-15 | | |
| ADMIN-M.26 | A-17 (SA48): open `credit_max_per_order`. Enter 0, then a value above its max, then 40. Read the History tab. Restore 30. | Out-of-range values refused with the min/max shown. Units: credits for this key, ₹ for `_paise` keys, % for `_bp` keys. History lists each change with actor, reason and time. | `R-17.1` D-03 A-17 | ▢ | |
| ADMIN-M.27 | Wait 11 minutes after the last TOTP (`clock +11m`), then change `order_accept_timeout_min` as AD47. | `step_up_required`; after re-entering TOTP the change saves. | AU-12 `step_up_required` A-17 | | |
| ADMIN-M.28 ★ | A-19: filter by actor AD47, action "settings.update", today. Open one row. Then filter subject "Kaveri Fresh". | Before/after diff of the value. The subject filter lists the strikes reset, force-close and suspension. No secret or code in any diff. | `R-13.1` A-19 | ▢ | |
| ADMIN-M.29 | As SA48 open A-18 and open A-19 filtered by actor SA48 after inviting and disabling a user (MQ-07.56–57). | Both actions in the audit log. | `R-13.2` A-18 A-19 | | |

## Cleanup

1. Reinstate any business or store suspended here. Revert the category name and remove the synonym.
2. `restore defaults`: `insight_wanted_nearby_radius_m` 2000, `credit_max_per_order` 30, `order_accept_timeout_min` 10.
3. `clock reset`; `make reset`.
