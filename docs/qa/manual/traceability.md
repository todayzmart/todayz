# Traceability

Every 08 scenario ID, every 01 rule (R-ID), every 06 screen and every problem code, mapped to the steps that test it. Built by scanning each step row of the book (action, expected result and refs). "Whole script" means the ID is in a script's **Covers** line and the script as a whole tests it. "Automated only" gives the reason no manual step exists.

## 1. Coverage

- **Steps:** 1172 in 32 script files (283 marked ★ for the production smoke and short runs). Counted by scanning every step row of the book.
- **08 scenario IDs:** 289 of 317 covered by manual steps (284 numbered IDs and ranges plus 33 SC families); 28 automated only; 0 not covered.
- **R-IDs (01):** 170 of 170 have at least one step (an R-ID is the 01 section number and bullet number, for example R-5.30).
- **06 screens:** 72 of 72.
- **Problem codes and HTTP statuses:** 51 of 51 named by at least one step (39 codes registered in 04 §1.4 and 12 HTTP statuses).

| File | Steps |
|---|---|
| [MQ-01-first-run-customer.md](MQ-01-first-run-customer.md) | 40 |
| [MQ-02-order-pickup.md](MQ-02-order-pickup.md) | 57 |
| [MQ-03-shop-day.md](MQ-03-shop-day.md) | 60 |
| [MQ-04-delivery.md](MQ-04-delivery.md) | 45 |
| [MQ-05-onboarding.md](MQ-05-onboarding.md) | 45 |
| [MQ-06-money-month.md](MQ-06-money-month.md) | 44 |
| [MQ-07-admin-roles.md](MQ-07-admin-roles.md) | 57 |
| [MQ-08-languages.md](MQ-08-languages.md) | 37 |
| [MQ-09-accessibility.md](MQ-09-accessibility.md) | 34 |
| [MQ-10-slow-network.md](MQ-10-slow-network.md) | 20 |
| [MQ-11-ai-features.md](MQ-11-ai-features.md) | 46 |
| [MQ-12-observability.md](MQ-12-observability.md) | 25 |
| [MONEY-wallet-credits-payouts.md](MONEY-wallet-credits-payouts.md) | 63 |
| [TIMER-order-timers.md](TIMER-order-timers.md) | 58 |
| [CANCEL-cancellations-disputes.md](CANCEL-cancellations-disputes.md) | 35 |
| [OFFER-offers-banners.md](OFFER-offers-banners.md) | 31 |
| [STOCK-stock-catalogue.md](STOCK-stock-catalogue.md) | 31 |
| [INSIGHT-demand-insights.md](INSIGHT-demand-insights.md) | 52 |
| [ASK-ask-shops-nearby.md](ASK-ask-shops-nearby.md) | 70 |
| [NOTIFY-notifications.md](NOTIFY-notifications.md) | 32 |
| [PERM-permissions-matrix.md](PERM-permissions-matrix.md) | 32 |
| [SEC-security.md](SEC-security.md) | 40 |
| [PWA-install-offline.md](PWA-install-offline.md) | 15 |
| [I18N-languages-detail.md](I18N-languages-detail.md) | 12 |
| [A11Y-accessibility-detail.md](A11Y-accessibility-detail.md) | 13 |
| [SITE-marketing-site.md](SITE-marketing-site.md) | 18 |
| [ADMIN-console-screens.md](ADMIN-console-screens.md) | 29 |
| [JOBS-ops-console.md](JOBS-ops-console.md) | 34 |
| [ABUSE-abuse-cases.md](ABUSE-abuse-cases.md) | 16 |
| [PERF-performance.md](PERF-performance.md) | 16 |
| [FAIL-failure-modes.md](FAIL-failure-modes.md) | 20 |
| [end-to-end-acceptance.md](end-to-end-acceptance.md) | 45 |
| **Total** | **1172** |

**Recount, 2026-10-22.** The counts above come from scanning the files, not from adding to the old totals. A step is a table row whose first cell is a step ID (`MQ-NN.nn`, `<AREA>-M.nn`, `ACC-nn`, with an optional letter suffix such as 13a). Where the scan and the earlier stated totals differed:

- MQ-07 was stated as 58 steps. It has 57 rows (there is no MQ-07.08), so the old figure over-counted by 1.
- MQ-04 was stated as 43 steps. It has 45 rows: the lettered steps MQ-04.13a and MQ-04.13b were not counted, so the old figure under-counted by 2.
- The stated total of 1015 (before the jobs and insights files) was therefore 1 below the real 1016 (1015 − 1 + 2 = 1016). A scanner whose area pattern was `[A-Z]+-M` finds 991, because it misses every step in I18N-M (12) and A11Y-M (13), whose area names contain digits (1016 − 25 = 991).
- The figure of 1049 in the version that added the jobs file was the stated 1015 plus the 34 JOBS steps; it did not include the INSIGHT file. With the 44 INSIGHT steps the real total was 1094.
- The Demand insights review fixes (D-99) rewrote the INSIGHT script as 52 steps (+8), so the total is 1102 and the starred steps are 245 (the INSIGHT script has 8). The 08 scenario list grew by IN-25 to IN-28 (255 numbered IDs, 287 with the 32 SC families); IN-28 is automated only (23 in all). The INSIGHT step numbers changed throughout, so every row in sections 2 to 6 that cited INSIGHT steps was regenerated from the scan. Round 3 of the same work (a sale skips expired batches, the batch edit, the pair rollup, the named seed cells and the 11th regular customer) changed step text only: the INSIGHT script keeps its 52 steps and 8 starred, the totals above stand, and the only citation that moved is M.51 now naming INSIGHT-M.36, so the IN rows needed no regeneration.
- Eight scenario IDs in 08 had no row in section 2 (BK-61 to BK-65, PO-04, RV-01, AI-01), so the earlier "0 not covered" was not checked against 08. They now have rows, each backed by a step that names the ID. BK-65 is covered only at midnight and by the B-14 toggle (TIMER-M.39), AI-01 only for the customer scope (MQ-11.43).
- Section 5 was recounted too. The four codes `claim_already_used`, `job_not_runnable`, `job_already_running` and `job_paused` had no row; they do now. The HTTP status rows used to list a curated subset of steps; they now list every step that names the status.
- Ask shops nearby (D-101) added the ASK script: 66 steps, 35 of them ★, so the total is 1168 and the starred steps are 280. The 08 scenario list grew by ASK-01 to ASK-20 and E2E-21 (276 numbered IDs, 309 with SC-ASK); ASK-20 is automated only (24 in all). 01 gained §11.1 to §11.3, two §15 rows, four §16 bullets and a §17 key group: 26 new R-IDs, so 170. R-11.8 to R-11.26 continue the §11 numbering through the bullets of §11.1 to §11.3; where two short bullets share one ID the Rule column names both. 06 gained C-25, C-26, B-20, A-23 and S-07 (72), and 04 §1.4 gained six codes (48 with the statuses).
- The D-101 review fixes added section J to the ASK script (ASK-M.67 to ASK-M.70, three of them ★: 70 steps, 38 ★), so the total is 1172 and the starred steps are 283. ASK-M.40, ASK-M.42, ASK-M.49 to ASK-M.51 changed their expected results (a quantity cap instead of a mixed price, one booking per reply, a collected order lifts the narrowing, one refusal for every reason a store is left out). The 08 list grew by ASK-21 to ASK-28 (284 numbered IDs, 317 with the SC families); ASK-22, ASK-24, ASK-27 and ASK-28 are automated only (28 in all). 04 §1.4 gained `ask_reply_used`, `ask_price_expired` and `share_card_unavailable` (51 with the statuses). 01 gained no top-level bullet (the new rules are sub-bullets of R-11.19, R-11.23, R-11.24 and R-16.6), so the R-IDs stay 170. The rows that cite ASK steps were regenerated from the scan.

## 2. 08 scenario IDs

| ID | Level | Steps |
|---|---|---|
| AU-01 | I,E | MQ-02.14, MQ-02.15, PERM-M.02, SEC-M.08, SEC-M.13, FAIL-M.07 |
| AU-02 | I | MQ-02.13, SEC-M.01 |
| AU-03 | I | TIMER-M.46, TIMER-M.47, ABUSE-M.03 |
| AU-04 | I | MQ-02.12, TIMER-M.44, TIMER-M.45, SEC-M.02, ABUSE-M.01 |
| AU-05 | I | SEC-M.03, SEC-M.04 |
| AU-06 | I | SEC-M.05 |
| AU-07 | I | SEC-M.09 |
| AU-08 | E | TIMER-M.48, TIMER-M.49, FAIL-M.08, FAIL-M.09 |
| AU-09 | I | INSIGHT-M.28, PERM-M.04, PERM-M.06, PERM-M.07, SEC-M.14, SEC-M.15, SEC-M.17 |
| AU-10 | I | MQ-03.39, MQ-06.05, OFFER-M.31, STOCK-M.02, INSIGHT-M.27, PERM-M.14, PERM-M.15, PERM-M.18, PERM-M.19 |
| AU-11 | I,E | MQ-07.01, MQ-07.02, ADMIN-M.01 |
| AU-12 | I | MQ-06.26, MQ-07.49, ADMIN-M.27 |
| AU-13 | I | PERM-M.02, SEC-M.10 |
| AU-14 | U | automated only — unit test over the golden (role × action) policy table; the manual matrix is in PERM and MQ-07 |
| AU-15 | I | MQ-12.16, MQ-12.17, MQ-12.20, INSIGHT-M.26, NOTIFY-M.01 |
| AU-16 | I | SEC-M.11 |
| AU-17 | I,E | SEC-M.07, ADMIN-M.17 |
| AU-18 | I,E | MONEY-M.61, MONEY-M.62, MONEY-M.63 |
| AU-19 | U | SEC-M.12 |
| AU-20 | I | SEC-M.06 |
| AU-21…30 | E | MQ-09.14 |
| BK-01 | I,E | MQ-02.20, MQ-02.22, MQ-02.25, MQ-03.07, MQ-03.08, MQ-04.16, TIMER-M.01, NOTIFY-M.02, PERF-M.04 |
| BK-02 | I | STOCK-M.20 |
| BK-03 | I,E | OFFER-M.18, STOCK-M.22 |
| BK-04 | I | MQ-02.19, SEC-M.32, FAIL-M.02, FAIL-M.03, FAIL-M.04, FAIL-M.20 |
| BK-05 | I,E | MQ-02.26, MQ-03.11, TIMER-M.06 |
| BK-06 | I | TIMER-M.02, TIMER-M.03, TIMER-M.05, NOTIFY-M.04 |
| BK-07 | I,E | MQ-03.17, CANCEL-M.08, NOTIFY-M.04, I18N-M.07 |
| BK-08 | I,E | MQ-03.13, MQ-03.14, MQ-03.15, TIMER-M.17, NOTIFY-M.04 |
| BK-09 | I | TIMER-M.18, TIMER-M.19, TIMER-M.20 |
| BK-10 | I | CANCEL-M.13 |
| BK-11 | I,E | MQ-02.36, MQ-02.38, MQ-03.19, ACC-16 |
| BK-12 | I,E | MQ-02.34, MQ-02.35, MQ-04.22, SEC-M.19, SEC-M.20, ABUSE-M.04 |
| BK-13 | I | MQ-02.37 |
| BK-14 | I | TIMER-M.09, TIMER-M.10 |
| BK-15 | I,E | MQ-02.30, MQ-02.31, TIMER-M.11, TIMER-M.12, TIMER-M.13, TIMER-M.14 |
| BK-16 | I,E | TIMER-M.25, TIMER-M.26, TIMER-M.27, TIMER-M.28 |
| BK-17 | I | MQ-02.56, CANCEL-M.02, CANCEL-M.03, CANCEL-M.04, CANCEL-M.05 |
| BK-18 | I | CANCEL-M.09, CANCEL-M.10, CANCEL-M.11, CANCEL-M.12 |
| BK-19 | I | CANCEL-M.20 |
| BK-20 | I | TIMER-M.15 |
| BK-21 | I | TIMER-M.58 |
| BK-22 | I | ACC-25 |
| BK-23 | I,E | MQ-04.06, MQ-04.07, MQ-04.09, MQ-04.10, MQ-04.11, MQ-04.13a, MQ-04.39 |
| BK-24 | I,E | MQ-04.23, MQ-04.26, MQ-04.27, MONEY-M.15, TIMER-M.21, NOTIFY-M.07 |
| BK-25 | I | TIMER-M.23, TIMER-M.24 |
| BK-26 | I | MQ-04.29, MQ-04.31, MQ-04.32, MQ-04.33, MQ-04.34, TIMER-M.40, TIMER-M.41, TIMER-M.42, TIMER-M.43 |
| BK-27 | I | MQ-02.23, MQ-04.17, MQ-04.19, MQ-04.24 |
| BK-28 | I | TIMER-M.33, TIMER-M.34, TIMER-M.36, TIMER-M.37, ADMIN-M.13 |
| BK-29 | I | CANCEL-M.22, CANCEL-M.23, ADMIN-M.10 |
| BK-30 | U | TIMER-M.04 |
| BK-31 | E | MQ-02.39, ADMIN-M.18 |
| BK-32 | I | STOCK-M.21, FAIL-M.19 |
| BK-33 | U | MQ-02.18, OFFER-M.11 |
| BK-34 | U,I | MQ-02.20, MQ-02.53, TIMER-M.39 |
| BK-35 | E | PWA-M.08, FAIL-M.10 |
| BK-36 | I | MQ-02.27, STOCK-M.05 |
| BK-37 | I | MQ-02.38, TIMER-M.10, STOCK-M.06, STOCK-M.07 |
| BK-38 | I | MQ-02.24, SEC-M.16, ADMIN-M.18 |
| BK-39 | I | MQ-07.31, MQ-07.32, CANCEL-M.19 |
| BK-40 | I | CANCEL-M.17, CANCEL-M.18, CANCEL-M.19 |
| BK-41 | I | TIMER-M.22 |
| BK-42 | U,I | CANCEL-M.21 |
| BK-43 | I,E | MQ-01.37, SEC-M.40 |
| BK-44 | I | MONEY-M.61 |
| BK-45 | I | MQ-04.12 |
| BK-46 | I | TIMER-M.29, TIMER-M.30, TIMER-M.31 |
| BK-47…60 | — | whole script: NOTIFY-notifications.md |
| BK-61 | I,E | MQ-04.13b |
| BK-62 | I,E | TIMER-M.26 |
| BK-63 | I,E | TIMER-M.12 |
| BK-64 | I,E | MQ-02.54 |
| BK-65 | I,E | TIMER-M.39 |
| CR-01 | I | MQ-02.21, MQ-03.12 |
| CR-02 | I | MONEY-M.02, MONEY-M.03 |
| CR-03 | U | MONEY-M.01, MONEY-M.04 |
| CR-04 | I | MQ-02.41, MQ-02.42, MQ-03.20 |
| CR-05 | I | MONEY-M.09 |
| CR-06 | U,I | MQ-03.33, MONEY-M.29 |
| CR-07 | U | MONEY-M.28 |
| CR-08 | I | MONEY-M.11, MONEY-M.12, MONEY-M.13 |
| CR-09 | U | MONEY-M.08 |
| CR-10 | I | MONEY-M.24, MONEY-M.25, ABUSE-M.09 |
| CR-11 | I | MONEY-M.19, MONEY-M.20, MONEY-M.21, ABUSE-M.10 |
| CR-12 | I | automated only — 50 concurrent spenders on one wallet needs goroutines; not reproducible by hand |
| CR-13 | U | automated only — property test with 10k random sequences |
| CR-14 | I | automated only — needs a hand-corrupted cache row in the database; integration test owns it |
| CR-15 | I,E | MQ-07.36, MQ-07.37, MQ-07.38, MQ-07.39, MQ-07.40, MQ-07.42, MONEY-M.38 |
| CR-16 | I | MQ-06.10, MQ-06.14, MQ-06.42 |
| CR-17 | I | MONEY-M.60 |
| CR-18 | I | MQ-03.36, MQ-03.37, MQ-03.53 |
| CR-19 | I | automated only — database permission check (UPDATE/DELETE as the app role); integration test |
| CR-20 | E | MQ-08.10, MQ-08.16, MQ-08.22, MQ-08.28, MQ-08.34, I18N-M.02, I18N-M.03 |
| CR-21 | U,I | MONEY-M.03, MONEY-M.05, OFFER-M.12 |
| CR-22 | U,I | MQ-04.14, MONEY-M.06 |
| CR-23 | U | MQ-02.41, MQ-04.25 |
| CR-24 | I | MONEY-M.16, MONEY-M.17, MONEY-M.18 |
| CR-25 | I | MONEY-M.09, MONEY-M.38 |
| CR-26 | I | MONEY-M.22, MONEY-M.23, MONEY-M.26 |
| CR-27 | I | MQ-07.25, MQ-07.41, MQ-07.42, MQ-07.43, MQ-07.44, MQ-07.45, MQ-07.46 |
| CR-28 | I | MONEY-M.62 |
| CR-29…30 | — | automated only — builder-written IDs, not yet defined in 08 |
| RF-01 | I,E | MONEY-M.34 |
| RF-02 | I,E | MONEY-M.40 |
| RF-03 | I | MONEY-M.37, MONEY-M.39 |
| RF-04 | I,E | MONEY-M.50, NOTIFY-M.08, ACC-15, ACC-18 |
| RF-05 | I | MONEY-M.35, ABUSE-M.06 |
| RF-06 | I | MONEY-M.48, MONEY-M.49, ABUSE-M.07, ABUSE-M.08 |
| RF-07 | I | MONEY-M.41 |
| RF-08 | I | MONEY-M.43, MONEY-M.44, MONEY-M.45 |
| RF-09 | I | whole script: MONEY-wallet-credits-payouts.md |
| RF-10 | I | MONEY-M.47 |
| RF-11 | I | MONEY-M.36 |
| RF-12 | I | MONEY-M.42 |
| RF-13…20 | — | automated only — builder-written IDs, not yet defined in 08 (standee manual steps are under ST-nn) |
| POS-01 | I,E | MQ-03.23 |
| POS-02 | I | MQ-03.55, MQ-03.57, TIMER-M.56 |
| POS-03 | E | MQ-03.46, MQ-03.48, MQ-03.49, STOCK-M.29 |
| POS-04 | I | STOCK-M.23, STOCK-M.24 |
| POS-05 | I | MQ-03.51, STOCK-M.25 |
| POS-06 | I | STOCK-M.26 |
| POS-07 | E | MQ-03.34, MQ-03.36, TIMER-M.51, ABUSE-M.16, ACC-26 |
| POS-08 | I,E | MQ-03.52, TIMER-M.52 |
| POS-09 | I | SEC-M.21 |
| POS-10 | U | SEC-M.22 |
| POS-11 | I,E | MQ-03.32 |
| POS-12 | U | automated only — counter-code step-window boundary (60 s steps) is a unit test; manual timing is not reliable. Offline verification is covered in SEC section D and ACC-30/31 |
| POS-13 | I | SEC-M.23 |
| POS-14 | I | MONEY-M.46 |
| POS-15 | E | MQ-03.22, MQ-03.24, MQ-03.26, PERF-M.01, PERF-M.02, PERF-M.03 |
| POS-16 | U | MQ-03.27, OFFER-M.11 |
| POS-17 | I | MQ-03.30, MQ-03.31, MONEY-M.42 |
| POS-18 | I | MQ-03.56 |
| POS-19 | I | MONEY-M.54 |
| POS-20 | I | MONEY-M.57 |
| POS-21 | I | MONEY-M.58 |
| POS-22 | I | SEC-M.24, SEC-M.25 |
| POS-23 | I | ABUSE-M.11 |
| POS-24 | I | STOCK-M.27, STOCK-M.28 |
| POS-25 | I,E | MQ-03.41, TIMER-M.54, SEC-M.18, ABUSE-M.13 |
| POS-26 | I,E | MQ-03.38 |
| POS-27 | I | MQ-03.54 |
| POS-28…35 | — | automated only — builder-written backend IDs, not yet defined in 08 |
| POS-36…55 | — | A11Y-M.02 |
| ST-01 | U | MQ-05.09, MQ-05.25, ACC-11 |
| ST-02 | E | MQ-01.38, ACC-12 |
| ST-03 | I | ACC-12, ACC-13 |
| ST-04 | I,E | ACC-18 |
| SR-01 | I | MQ-01.16, MQ-05.11, MQ-05.21, MQ-05.42, MQ-05.43, MQ-05.45, CANCEL-M.22, ADMIN-M.11 |
| SR-02 | I | OFFER-M.23 |
| SR-03 | I,E | OFFER-M.21 |
| SR-04 | I,E | OFFER-M.22 |
| SR-05 | I,E | MQ-01.18, MQ-01.37 |
| FA-01 | I | MQ-05.38, MQ-05.39, MQ-05.40, MQ-05.41, PERM-M.28, PERM-M.29, PERM-M.30, PERM-M.31, ACC-06 |
| PO-01 | I | MQ-06.40, MQ-06.43 |
| PO-02 | I | MQ-06.11, MQ-06.39, MQ-06.41, MQ-06.43 |
| PO-03 | I | MQ-06.08, MQ-06.15, MQ-06.16, MQ-06.17, MQ-06.18, MQ-06.19, MQ-06.21, MQ-06.22, PERM-M.24 |
| PO-04 | I | MQ-06.24, MQ-06.25 |
| BZ-01 | I | PERM-M.08, PERM-M.09, PERM-M.10, PERM-M.11, PERM-M.12 |
| AD-01 | I | MQ-07.10, MQ-07.11, MQ-07.17, MQ-07.18, MQ-07.19, MQ-07.20, MQ-07.47, MQ-07.48, NOTIFY-M.32, PERM-M.25 (+1 more) |
| AD-02 | E | MQ-05.26, MQ-06.22, MQ-06.23, MQ-07.09, MQ-07.10, MQ-07.12, MQ-07.14, MQ-07.16, MQ-07.19, MQ-07.21 (+9 more) |
| RV-01 | I | MQ-02.50 |
| AI-01 | I | MQ-11.43 |
| VO-01 | I,E | MQ-11.17, MQ-11.18, MQ-11.19, MQ-11.20, PERM-M.20 |
| VO-02 | I | MQ-11.16, MQ-11.21, STOCK-M.30 |
| IN-01 | I,E | MQ-03.04, INSIGHT-M.01, INSIGHT-M.34 |
| IN-02 | I,E | INSIGHT-M.08, INSIGHT-M.09 |
| IN-03 | I | INSIGHT-M.02, INSIGHT-M.03, INSIGHT-M.08, INSIGHT-M.11, INSIGHT-M.29, INSIGHT-M.40, INSIGHT-M.41 |
| IN-04 | I | INSIGHT-M.02, INSIGHT-M.05, INSIGHT-M.12 |
| IN-05 | I | MQ-11.26, OFFER-M.07, STOCK-M.19, INSIGHT-M.02, INSIGHT-M.04, INSIGHT-M.08, INSIGHT-M.13, INSIGHT-M.35 |
| IN-06 | I | INSIGHT-M.07, INSIGHT-M.10, ADMIN-M.05, ACC-45 |
| IN-07 | I | INSIGHT-M.07, INSIGHT-M.10, INSIGHT-M.23, INSIGHT-M.24 |
| IN-08 | I,E | INSIGHT-M.07 |
| IN-09 | I | INSIGHT-M.14 |
| IN-10 | I | INSIGHT-M.08, INSIGHT-M.15, INSIGHT-M.36 |
| IN-11 | I,E | INSIGHT-M.15, INSIGHT-M.36 |
| IN-12 | I | INSIGHT-M.08, INSIGHT-M.16, INSIGHT-M.17, INSIGHT-M.37, INSIGHT-M.38 |
| IN-13 | I,E | INSIGHT-M.02, INSIGHT-M.06, INSIGHT-M.18, INSIGHT-M.30, INSIGHT-M.39 |
| IN-14 | I | INSIGHT-M.19, INSIGHT-M.20, INSIGHT-M.31, INSIGHT-M.32 |
| IN-15 | I,E | INSIGHT-M.21, INSIGHT-M.33 |
| IN-16 | I | INSIGHT-M.43, INSIGHT-M.44, INSIGHT-M.45, INSIGHT-M.46 |
| IN-17 | I,E | INSIGHT-M.03, INSIGHT-M.04, INSIGHT-M.05, INSIGHT-M.06, INSIGHT-M.07, INSIGHT-M.09, INSIGHT-M.11, INSIGHT-M.12, INSIGHT-M.13, INSIGHT-M.14 (+13 more) |
| IN-18 | I,E | INSIGHT-M.02, INSIGHT-M.09, INSIGHT-M.44, INSIGHT-M.47 |
| IN-19 | I | INSIGHT-M.48, INSIGHT-M.49, INSIGHT-M.50, INSIGHT-M.51, INSIGHT-M.52 |
| IN-20 | I | INSIGHT-M.25, INSIGHT-M.26, INSIGHT-M.45, ACC-45 |
| IN-21 | I,E | INSIGHT-M.27, INSIGHT-M.28 |
| IN-22 | I | automated only — a compile-and-test rule: the coach detector and the insights package must both import `internal/insights/facts`, and the test fails if the detector holds its own SQL for the shared kinds. By hand you can only compare the two numbers on screen, which MQ-11.34 does |
| IN-23 | I | automated only — a nightly job and its backfill command: idempotency, the 100-pair cap and the `todayz rollup --from --to` backfill are integration tests; the seed runs the rollups once, so every manual step already depends on them (`<seed:insights-SL2>`) |
| IN-24 | I | INSIGHT-M.29, INSIGHT-M.30, INSIGHT-M.31, INSIGHT-M.32, INSIGHT-M.33, INSIGHT-M.34, INSIGHT-M.41, INSIGHT-M.46 |
| IN-25 | I,E | INSIGHT-M.35, INSIGHT-M.36 |
| IN-26 | I | INSIGHT-M.20, INSIGHT-M.32 |
| IN-27 | I,E | INSIGHT-M.17, INSIGHT-M.38 |
| IN-28 | I | automated only — the write path of `search_log.product_id` has no screen: its only visible effect is the Wanted nearby and Missed sales counts after the nightly rollup, and the seed already runs the rollups once (`<seed:insights-SL2>`); the match rules (exact barcode, exact normalised name or a synonym) are integration tests |
| ASK-01 | I,E | ASK-M.01, ASK-M.02, ASK-M.03, ASK-M.43, ASK-M.45, ASK-M.66 |
| ASK-02 | I | ASK-M.02, ASK-M.26, ASK-M.37, ASK-M.43 |
| ASK-03 | I,E | ASK-M.04, ASK-M.06, ASK-M.50, ASK-M.65, ASK-M.66 |
| ASK-04 | U,I | ASK-M.05, ASK-M.27, ASK-M.28 |
| ASK-05 | I | ASK-M.02, ASK-M.30, ASK-M.38, ASK-M.48 |
| ASK-06 | I,E | ASK-M.29, ASK-M.34, ASK-M.35, ASK-M.36, ASK-M.44 |
| ASK-07 | I | ASK-M.31, ASK-M.33, ASK-M.47 |
| ASK-08 | I,E | ASK-M.32, ASK-M.39, ASK-M.40, ASK-M.41, ASK-M.42, ASK-M.67 |
| ASK-09 | I | ASK-M.07, ASK-M.08, ASK-M.10 |
| ASK-10 | U,I | ASK-M.07, ASK-M.09 |
| ASK-11 | U | ASK-M.12, ASK-M.13, ASK-M.14 |
| ASK-12 | I,E | ASK-M.07, ASK-M.10, ASK-M.11, ASK-M.33 |
| ASK-13 | I | ASK-M.46, ASK-M.49, ASK-M.50, ASK-M.51 |
| ASK-14 | I,E | ASK-M.52, ASK-M.53, ASK-M.54 |
| ASK-15 | I,E | ASK-M.19, ASK-M.20, ASK-M.21, ASK-M.23, ASK-M.25 |
| ASK-16 | I | ASK-M.22, ASK-M.70 |
| ASK-17 | I,E | ASK-M.15, ASK-M.16, ASK-M.17 |
| ASK-18 | I | ASK-M.56, ASK-M.57, ASK-M.58 |
| ASK-19 | I,E | ASK-M.59, ASK-M.60, ASK-M.61, ASK-M.62 |
| ASK-20 | I | automated only — an ask with no Yes adds to the nightly Wanted nearby and Missed sales counts behind the k ≥ 10 floor, so one tester's search and ask never show on a screen; counting a search and an ask once within the hour is an integration test of `rollup_ask_demand` (08 ASK-20) |
| ASK-21 | I | ASK-M.42 |
| ASK-22 | I | automated only — the purge runs at `ask_retention_days` (90 days) under the test clock and is checked in the database (cascades, the cleared `ask_reply_id`); no screen shows it |
| ASK-23 | I | ASK-M.68 |
| ASK-24 | I,E | automated only — needs a shop to add the item after a Yes without a price, with an item offer on it; the book's NH Yes without a price is in-store only (ASK-M.38, ASK-M.68) |
| ASK-25 | I,E | ASK-M.69 |
| ASK-26 | I | ASK-M.70 |
| ASK-27 | I,E | automated only — the timeout, model error, invalid output and cap need the mock model driven per case, and the latency budget needs the integration harness |
| ASK-28 | I | automated only — the asks part of account deletion is checked in the database inside AU-18; the manual deletion steps are MONEY-M.61 to MONEY-M.63 |
| PL-01 | U | SEC-M.37 |
| PL-02 | U,I | SEC-M.35, SEC-M.38, I18N-M.08, FAIL-M.12 |
| PL-03 | I | MQ-12.21, MQ-12.22 |
| PL-04 | I | automated only — runs inside `make check`/`make e2e` under `scripts/no-egress.sh`; nothing to do by hand |
| PL-05 | I,E | MQ-07.53, MQ-07.54, MQ-07.55, NOTIFY-M.19, SEC-M.39 |
| PL-06 | U,I | MONEY-M.45, TIMER-M.57, OFFER-M.23, NOTIFY-M.16 |
| E2E-01 | W3 | ACC-14, ACC-15, ACC-16, ACC-17, ACC-18 |
| E2E-02 | W2 | whole script: MQ-02-order-pickup.md |
| E2E-03 | W2 | CANCEL-M.15 |
| E2E-04 | W2 | ACC-19 |
| E2E-05 | W2 | ACC-20, ACC-21, ACC-22, ACC-24 |
| E2E-06 | W2 | ACC-26, ACC-27 |
| E2E-07 | W2 | MQ-03.47, MQ-03.50, ACC-30, ACC-31 |
| E2E-08 | W3 | MQ-05.20, MQ-05.32, MQ-05.35, MQ-05.36, ACC-01, ACC-05 |
| E2E-09 | W3 | MQ-06.28, MQ-06.32, MQ-06.44, ACC-33, ACC-35, ACC-36 |
| E2E-10 | W2, again at W3 | CANCEL-M.28, ACC-38 |
| E2E-11 | W3 | MQ-11.20, STOCK-M.30, STOCK-M.31 |
| E2E-12 | W2, again at W3 | MQ-07.51, ACC-37 |
| E2E-13 | W3 | MQ-11.09, MQ-11.10 |
| E2E-14 | W3 | MQ-11.25, MQ-11.26, MQ-11.27, OFFER-M.07 |
| E2E-15…20 |  | I18N-M.12 |
| E2E-21 | W3 | ASK-M.26, ASK-M.28, ASK-M.29, ASK-M.32, ASK-M.34, ASK-M.36, ASK-M.38, ASK-M.39, ASK-M.41 |
| OB-01 | I | MQ-12.11, MQ-12.12, MQ-12.13, MQ-12.14, PERF-M.14, ACC-43 |
| OB-02 | I | MQ-10.19, MQ-11.11, MQ-12.16, MQ-12.17, MQ-12.18, MQ-12.19, MQ-12.20, INSIGHT-M.26, PERF-M.14, ACC-43 |
| OB-03 | I | MQ-12.15, PERF-M.11 |
| OB-04 | I | MQ-12.15, PERF-M.10 |
| OB-05 | I | JOBS-M.15, JOBS-M.17, PERF-M.13, FAIL-M.11 |
| OB-06 | I | MQ-10.17, MQ-10.18, MQ-12.04, PERF-M.15, ACC-44 |
| OB-11 | I | PERF-M.16, ACC-41, ACC-42 |
| SC-JB-01 | U | automated only — registry lint (W1-11) |
| SC-JB-02 | I | JOBS-M.01, JOBS-M.03, JOBS-M.04, JOBS-M.05 |
| SC-JB-03 | I | JOBS-M.06 |
| SC-JB-04 | I | JOBS-M.14, JOBS-M.15 |
| SC-JB-05 | I | JOBS-M.18 |
| SC-JB-06 | I | JOBS-M.07, JOBS-M.08, JOBS-M.09 |
| SC-JB-07 | I | JOBS-M.11 |
| SC-JB-08 | I | JOBS-M.19, JOBS-M.20 |
| SC-JB-09 | I | JOBS-M.12, JOBS-M.13 |
| SC-JB-10 | I | JOBS-M.16 |
| SC-JB-11 | I | JOBS-M.09, JOBS-M.10 |
| SC-JB-12 | I | automated only — partition retention (needs 90 days of clock) |
| SC-JB-13 | I | automated only — label allow-list |
| SC-JB-14 | I | automated only — AI kind writes drafts only |
| SC-JB-15 | U | automated only — the 60 s cap lint |
| SC-OPS-01 | I,E | JOBS-M.22 |
| SC-OPS-02 | I | JOBS-M.23 |
| SC-OPS-03 | I,E | JOBS-M.21 |
| SC-OPS-04 | U,I | JOBS-M.24 |
| SC-OPS-05 | U | automated only — Terraform policy check |
| SC-OPS-06 | E | JOBS-M.01, JOBS-M.02 |
| SC-OPS-07 | I,E | JOBS-M.21 |
| SC-OPS-08 | I | automated only — fake AWS adapter error |
| SC-OPS-09 | I,E | JOBS-M.34 |
| SC-GS-01 | I | JOBS-M.26 |
| SC-GS-02 | I | JOBS-M.27 |
| SC-GS-03 | U | automated only — drain versus `WriteTimeout` |
| SC-GS-04 | I | JOBS-M.28 |
| SC-GS-05 | I | JOBS-M.29 |
| SC-GS-06 | I | automated only — hard cancel and idempotency keys |
| SC-GS-07 | I,E | JOBS-M.30 |
| SC-GS-08 | E | JOBS-M.31, JOBS-M.32 |
| SC-GS-09 | E | JOBS-M.33 |
| SC-GS-10 | U | automated only — infra timings check |

### SC families

Family coverage counts every step that names the family or any ID with its prefix.

| Family | Steps | First steps |
|---|---|---|
| SC-AU | 61 | MQ-02.12, MQ-02.13, MQ-02.14, MQ-02.15, MQ-03.39, MQ-06.05 (+55 more) |
| SC-BK | 141 | MQ-01.37, MQ-02.18, MQ-02.19, MQ-02.20, MQ-02.22, MQ-02.23 (+135 more) |
| SC-CR | 63 | MQ-02.21, MQ-02.41, MQ-02.42, MQ-03.12, MQ-03.20, MQ-03.33 (+57 more) |
| SC-RF | 21 | MONEY-M.34, MONEY-M.35, MONEY-M.36, MONEY-M.37, MONEY-M.39, MONEY-M.40 (+15 more) |
| SC-POS | 52 | MQ-03.22, MQ-03.23, MQ-03.24, MQ-03.26, MQ-03.27, MQ-03.30 (+46 more) |
| SC-ST | 7 | MQ-01.38, MQ-05.09, MQ-05.25, ACC-11, ACC-12, ACC-13 (+1 more) |
| SC-BZ | 13 | MQ-05.04, MQ-05.05, MQ-05.12, MQ-05.13, MQ-05.18, MQ-05.21 (+7 more) |
| SC-CT | 6 | STOCK-M.04, STOCK-M.11, STOCK-M.16, STOCK-M.17, STOCK-M.18, STOCK-M.19 |
| SC-SR | 16 | SEC-M.29, I18N-M.09, ADMIN-M.19, MQ-01.16, MQ-01.18, MQ-01.37 (+10 more) |
| SC-NT | 7 | NOTIFY-M.20, NOTIFY-M.21, NOTIFY-M.22, NOTIFY-M.23, NOTIFY-M.28, NOTIFY-M.29 (+1 more) |
| SC-RV | 2 | SEC-M.27, MQ-02.50 |
| SC-OF | 1 | OFFER-M.27 |
| SC-PO | 20 | MQ-06.07, MQ-06.09, MQ-06.24, MQ-06.27, SEC-M.28, MQ-06.08 (+14 more) |
| SC-RK | 2 | ADMIN-M.23, ABUSE-M.08 |
| SC-IN | 58 | MQ-03.04, MQ-11.26, OFFER-M.07, STOCK-M.19, INSIGHT-M.01, INSIGHT-M.02 (+52 more) |
| SC-VO | 8 | MQ-11.16, MQ-11.17, MQ-11.18, MQ-11.19, MQ-11.20, MQ-11.21 (+2 more) |
| SC-AD | 28 | MQ-05.26, MQ-06.22, MQ-06.23, MQ-07.09, MQ-07.10, MQ-07.11 (+22 more) |
| SC-FA | 9 | MQ-05.38, MQ-05.39, MQ-05.40, MQ-05.41, PERM-M.28, PERM-M.29 (+3 more) |
| SC-SP | 1 | ADMIN-M.25 |
| SC-OB | 30 | MQ-10.17, MQ-10.18, MQ-10.19, MQ-11.11, MQ-12.04, MQ-12.11 (+24 more) |
| SC-AI | 2 | FAIL-M.15, MQ-11.43 |
| SC-UI | 5 | A11Y-M.05, A11Y-M.06, A11Y-M.07, A11Y-M.08, A11Y-M.12 |
| SC-I18N | 5 | I18N-M.04, I18N-M.05, I18N-M.07, I18N-M.08, A11Y-M.13 |
| SC-SITE | 3 | SITE-M.02, SITE-M.03, SITE-M.15 |
| SC-INF | 2 | SEC-M.30, SEC-M.33 |
| SC-CU | 9 | PWA-M.01, PWA-M.03, PWA-M.04, PWA-M.13, A11Y-M.09, PERF-M.05 (+3 more) |
| SC-BU | 9 | PWA-M.15, A11Y-M.01, A11Y-M.03, A11Y-M.04, A11Y-M.11, PERF-M.03 (+3 more) |
| SC-PL | 16 | MQ-07.53, MQ-07.54, MQ-07.55, MQ-12.21, MQ-12.22, MONEY-M.45 (+10 more) |
| SC-JB | 18 | JOBS-M.01, JOBS-M.03, JOBS-M.04, JOBS-M.05, JOBS-M.06, JOBS-M.07 (+12 more) |
| SC-OPS | 7 | JOBS-M.01, JOBS-M.02, JOBS-M.21, JOBS-M.22, JOBS-M.23, JOBS-M.24 (+1 more) |
| SC-GS | 8 | JOBS-M.26, JOBS-M.27, JOBS-M.28, JOBS-M.29, JOBS-M.30, JOBS-M.31 (+2 more) |
| SC-ASK | 65 | ASK-M.01, ASK-M.02, ASK-M.03, ASK-M.04, ASK-M.05, ASK-M.06 (+59 more) |
| SC-E2E | 43 | MQ-03.47, MQ-03.50, MQ-05.20, MQ-05.32, MQ-05.35, MQ-05.36 (+37 more) |

## 3. Rules (R-IDs from 01)

| R-ID | Rule | Steps |
|---|---|---|
| R-1.1 | surfaces/hosts | MQ-01.40 |
| R-1.2 | languages and light theme | MQ-01.01, MQ-01.02, MQ-01.03, MQ-01.04, MQ-01.05, MQ-08.01, MQ-08.02, MQ-08.03, MQ-08.04, MQ-08.05 (+37 more) |
| R-1.3 | free, no gateway, payment method recorded only | MQ-02.16, MQ-02.36, SITE-M.05 |
| R-2.1 | OTP limits, browsing and cart | MQ-01.36, MQ-01.39, MQ-02.04, MQ-02.09, MQ-02.11, MQ-02.12, MQ-02.13, MQ-02.14, MQ-05.31, MQ-09.15 (+14 more) |
| R-2.2 | OTP never logged, reserved numbers | MQ-02.14, NOTIFY-M.01 |
| R-2.3 | test clock | MQ-01.22, SEC-M.36 |
| R-2.4 | business sign-in, multi-business | MQ-02.01, MQ-03.01, MQ-03.02, MQ-03.03, MQ-05.01, PERM-M.01, PERM-M.03 |
| R-2.5 | admin TOTP and 10 recovery codes | MQ-07.01, MQ-07.02, MQ-07.03, MQ-07.04, MQ-07.05, MQ-07.06, MQ-07.07, MQ-07.58, MQ-09.26 (+2 more) |
| R-2.6 | Turnstile | MQ-02.11, SEC-M.06, SITE-M.07, SITE-M.08 |
| R-2.7 | sessions and sign out all | MQ-02.15, TIMER-M.48, TIMER-M.49, TIMER-M.50, PERM-M.02, SEC-M.09, SEC-M.10, ADMIN-M.16 |
| R-2.8 | one phone, both profiles | PERM-M.01 |
| R-3.1 | per-store data | INSIGHT-M.28, PERM-M.04, PERM-M.05, SEC-M.17 |
| R-3.2 | onboarding fields | MQ-05.02, MQ-05.03, MQ-05.04, MQ-05.05, MQ-05.06 |
| R-3.3 | KYC states | MQ-05.10, MQ-05.17, MQ-05.18, MQ-05.19, MQ-05.20, ADMIN-M.06, ADMIN-M.07, ADMIN-M.10, ACC-05 |
| R-3.4 | store statuses, visibility and review | MQ-01.16, MQ-05.11, MQ-05.20, MQ-05.21, MQ-05.35, MQ-05.42, MQ-05.43, MQ-05.44, MQ-05.45, CANCEL-M.22 (+4 more) |
| R-3.5 | fulfilment flags and In-store only | MQ-01.18, MQ-01.37, MQ-04.04, SEC-M.40 |
| R-3.6 | payout details, encryption, masking | MQ-05.12, MQ-05.13, MQ-05.14, MQ-06.04, MQ-06.37, MQ-06.38, PERM-M.13, PERM-M.14, ACC-04 |
| R-3.7 | owner | PERM-M.08 |
| R-3.8 | manager | MQ-06.04, INSIGHT-M.28, PERM-M.05, PERM-M.09, PERM-M.13, PERM-M.21 |
| R-3.9 | cashier | MQ-03.05, MQ-03.21, MQ-03.39, MQ-06.05, CANCEL-M.09, OFFER-M.31, STOCK-M.02, INSIGHT-M.27, PERM-M.03, PERM-M.11 (+1 more) |
| R-3.10 | manager-and-above list | PERM-M.09, PERM-M.16, PERM-M.17 |
| R-3.11 | no business delete | PERM-M.12 |
| R-3.12 | codes and standee URL | MQ-01.38, MQ-05.09, MQ-05.24, MQ-05.25, MONEY-M.50, ACC-11 |
| R-4.1 | master and store catalogue | MQ-01.35, MQ-05.07, STOCK-M.14, STOCK-M.15 |
| R-4.2 | master adds no stock | STOCK-M.14, ADMIN-M.20, ACC-10 |
| R-4.3 | missing products | MQ-05.29, STOCK-M.16, ACC-07 |
| R-4.4 | movement sources | MQ-02.38, MQ-03.43, MQ-03.57, MQ-05.08, MQ-05.23, MQ-11.18, MQ-11.25, STOCK-M.01, STOCK-M.03, STOCK-M.04 (+7 more) |
| R-4.5 | stock math | MQ-01.34, MQ-02.03, MQ-02.27, STOCK-M.01, STOCK-M.05, STOCK-M.21, STOCK-M.24 |
| R-4.6 | freshness and stale | MQ-01.30, MQ-01.33, MQ-03.43, MQ-08.06, MQ-08.07, MQ-08.12, MQ-08.13, MQ-08.18, MQ-08.19, MQ-08.24 (+13 more) |
| R-4.7 | low stock | STOCK-M.08, STOCK-M.09, STOCK-M.10, ACC-09 |
| R-5.1 | one order one store, fulfilment, `feature_delivery_enabled` | MQ-02.06, MQ-04.41, MQ-04.42 |
| R-5.2 | `store_closed` | TIMER-M.33, TIMER-M.34, TIMER-M.36, TIMER-M.37, TIMER-M.38 |
| R-5.3 | accept timeout | MQ-02.20, MQ-02.54, MQ-02.55, MQ-03.10, MQ-08.09, MQ-08.15, MQ-08.21, MQ-08.27, MQ-08.33, MQ-09.18 (+7 more) |
| R-5.4 | pickup hold | MQ-02.29, TIMER-M.06, TIMER-M.09, TIMER-M.10, TIMER-M.15 |
| R-5.5 | extension | MQ-02.30, MQ-02.31, MQ-09.20, TIMER-M.11, TIMER-M.12, TIMER-M.13, TIMER-M.14 |
| R-5.6 | partial timeout value | MQ-03.14, TIMER-M.17, TIMER-M.18, TIMER-M.19 |
| R-5.7 | dispatch timeout | TIMER-M.21, TIMER-M.23, TIMER-M.24 |
| R-5.8 | timers platform-only and server-run | MQ-02.54, TIMER-M.57, TIMER-M.58 |
| R-5.9 | state machine | MQ-02.19, MQ-03.17, MONEY-M.15, TIMER-M.04, CANCEL-M.13, CANCEL-M.14, CANCEL-M.20 |
| R-5.10 | terminal states | MQ-02.40, CANCEL-M.05 |
| R-5.11 | customer cancel window | MQ-02.56, CANCEL-M.01, CANCEL-M.02, CANCEL-M.03, CANCEL-M.04, CANCEL-M.06 |
| R-5.12 | who acts for the shop | MQ-02.26, MQ-03.11, CANCEL-M.09, CANCEL-M.12 |
| R-5.13 | partial timeout gives no strike | TIMER-M.19, TIMER-M.20 |
| R-5.14 | admin cancel | MQ-07.31, MQ-07.32, MQ-09.30, MONEY-M.18, CANCEL-M.16, CANCEL-M.17, CANCEL-M.18, CANCEL-M.19 |
| R-5.15 | order_events timelines | MQ-02.39, CANCEL-M.02 |
| R-5.16 | order numbers | MQ-02.20, MQ-02.53, TIMER-M.39 |
| R-5.17 | auto-accept | ACC-25 |
| R-5.18 | partial accept re-cap and fee | MQ-03.13, MQ-03.14, MQ-03.15, MONEY-M.60, TIMER-M.17, CANCEL-M.15 |
| R-5.19 | placed reserves nothing, rechecks | MQ-02.25, STOCK-M.05, STOCK-M.20, FAIL-M.20 |
| R-5.20 | accepted creates reservations | MQ-02.27, MQ-03.15, STOCK-M.05 |
| R-5.21 | sale_online | MQ-02.38, STOCK-M.06 |
| R-5.22 | delivery_failed returns | MQ-04.29, MQ-04.30, MQ-04.32, MQ-04.33, MQ-04.34, TIMER-M.40, TIMER-M.41, TIMER-M.42, TIMER-M.43, CANCEL-M.21 |
| R-5.23 | other terminal states release | TIMER-M.03, CANCEL-M.03, STOCK-M.07 |
| R-5.24 | no-show strikes and limit | TIMER-M.10, TIMER-M.14, TIMER-M.22, TIMER-M.25, TIMER-M.26, TIMER-M.27, TIMER-M.28, FAIL-M.06, ACC-19 |
| R-5.25 | strike assignment | TIMER-M.10, TIMER-M.20, TIMER-M.24, CANCEL-M.17, CANCEL-M.18, ACC-39 |
| R-5.26 | store strikes and risk flag | TIMER-M.05, TIMER-M.29, TIMER-M.30, TIMER-M.31, CANCEL-M.12, ADMIN-M.23, ABUSE-M.12 |
| R-5.27 | expiry penalty in ranking and nudge | TIMER-M.32 |
| R-5.28 | strike reset | MQ-07.34, TIMER-M.16, CANCEL-M.33, ADMIN-M.14 |
| R-5.29 | delivery settings | MQ-01.31, MQ-02.07, MQ-04.01, MQ-04.08, MQ-04.13, MQ-04.13a, MQ-04.13b |
| R-5.30 | item subtotal for delivery (D-94) | MQ-04.08, MQ-04.09, MQ-04.10, MQ-04.11, MQ-04.12, MQ-04.13a, MQ-04.14, MQ-04.37, MQ-04.38, MONEY-M.14 |
| R-5.31 | rider | MQ-04.18, MQ-04.21 |
| R-5.32 | Delivery tag and radius | MQ-01.17, MQ-01.19, MQ-04.03, MQ-04.05, MQ-04.06, MQ-04.36, MQ-04.39 |
| R-5.33 | delivery_failed credits and cashback | MQ-04.31 |
| R-5.34 | reason lists | MQ-03.16, MQ-04.28, MQ-07.31, MQ-08.10, MQ-08.16, MQ-08.22, MQ-08.28, MQ-08.34, CANCEL-M.01, CANCEL-M.07 (+5 more) |
| R-6.1 | pickup code | MQ-02.24, MQ-02.29, MQ-02.34, MQ-02.35, MQ-02.36, MQ-02.37, MQ-09.19, SEC-M.16, SEC-M.19, SEC-M.20 (+3 more) |
| R-6.2 | delivery code | MQ-04.20, MQ-04.22, MQ-04.23, ACC-22, ACC-23 |
| R-6.3 | bill QR | MQ-03.34, MQ-03.35, MQ-03.36, MQ-03.52, MQ-03.53, MONEY-M.53, TIMER-M.51, TIMER-M.52, SEC-M.21, SEC-M.22 (+2 more) |
| R-6.4 | counter code | MQ-03.32, SEC-M.23, ACC-30 |
| R-6.5 | phone only | MQ-03.30, MONEY-M.42, CANCEL-M.26, SEC-M.26, ACC-29 |
| R-6.6 | only links touch the ledger | SEC-M.26, ACC-29 |
| R-6.7 | offline counter-code verification and risk flag | MQ-03.47, MQ-03.50, SEC-M.24, SEC-M.25, ABUSE-M.11, ACC-30, ACC-31 |
| R-7.1 | 1 credit = ₹1, not cash | SITE-M.06 |
| R-7.2 | eligible amount | MQ-02.18, MQ-04.10, MONEY-M.01, OFFER-M.12 |
| R-7.3 | spending rules | MQ-02.08, MQ-02.17, MQ-02.18, MQ-03.54, MQ-04.14, MQ-04.15, MQ-07.51, MQ-08.08, MQ-08.14, MQ-08.20 (+13 more) |
| R-7.4 | lots | MONEY-M.07, MONEY-M.08, MONEY-M.38 |
| R-7.5 | hold, capture, release, expiry while held | MQ-02.21, MQ-02.41, MQ-03.12, MONEY-M.08, MONEY-M.09, MONEY-M.14, MONEY-M.16, MONEY-M.17, MONEY-M.18, MONEY-M.56 (+3 more) |
| R-7.6 | spent credits go to the shop | MQ-02.43, MQ-06.35, MONEY-M.17, ACC-24 |
| R-7.7 | cashback | MQ-01.17, MQ-01.19, MQ-02.41, MQ-02.43, MQ-03.20, MQ-03.33, MQ-03.37, MQ-04.25, MQ-04.36, MQ-06.35 (+9 more) |
| R-7.8 | referral credits paid by TodayZ | MONEY-M.40 |
| R-7.9 | expiry | MONEY-M.07, MONEY-M.10, MONEY-M.11, MONEY-M.12, MONEY-M.13 |
| R-7.10 | caps | MONEY-M.19, MONEY-M.20, MONEY-M.21, MONEY-M.22, MONEY-M.23, MONEY-M.24, MONEY-M.25, MONEY-M.26, ABUSE-M.09, ABUSE-M.10 |
| R-7.11 | POS reversal | MQ-03.42, MQ-03.55, MQ-03.56, MONEY-M.54, MONEY-M.57, MONEY-M.58, TIMER-M.55, TIMER-M.56 |
| R-7.12 | manual adjustments | MQ-07.25, MQ-07.36, MQ-07.37, MQ-07.38, MQ-07.39, MQ-07.41, MONEY-M.38 |
| R-7.13 | wallet history copy | MQ-02.42, MQ-08.10, MQ-08.16, MQ-08.22, MQ-08.28, MQ-08.34, MONEY-M.13 |
| R-8.1 | kinds and amounts | MONEY-M.32, MONEY-M.47, MONEY-M.50 |
| R-8.2 | qualification | MONEY-M.32, MONEY-M.36, MONEY-M.37, MONEY-M.39, MONEY-M.40, MONEY-M.46, ACC-15, ACC-18 |
| R-8.3 | attribution | MONEY-M.32, MONEY-M.34, MONEY-M.41, MONEY-M.43, MONEY-M.44, MONEY-M.45 |
| R-8.4 | refusals and same device | MONEY-M.35, MONEY-M.48, MONEY-M.49, MONEY-M.51, ABUSE-M.06, ABUSE-M.08 |
| R-8.5 | shop rewards to primary store | MONEY-M.50 |
| R-9.1 | per-store account | MQ-06.01, MQ-06.02, MQ-06.08, ADMIN-M.22, ACC-32 |
| R-9.2 | in and out | MQ-02.43, MQ-06.01, MQ-06.34, MONEY-M.30, ACC-32 |
| R-9.3 | statement | MQ-06.03, MQ-06.06, MQ-06.07, MQ-06.08, MQ-06.12, ACC-33, ACC-34 |
| R-9.4 | payout amount, one open payout | MQ-06.07, MQ-06.10, MQ-06.11, MQ-06.24, MQ-06.25, MQ-06.40, MQ-06.41 |
| R-9.5 | mark paid, "Paid", negative carries forward | MQ-06.03, MQ-06.09, MQ-06.10, MQ-06.13, MQ-06.26, MQ-06.27, MQ-06.28, MQ-06.29, MQ-06.31, MQ-06.32 (+4 more) |
| R-9.6 | holds | MQ-06.09, MQ-06.11, MQ-06.15, MQ-06.21, MQ-06.39 |
| R-10.1 | review eligibility | MQ-02.45, MQ-02.46, MQ-02.48, MQ-02.49, MQ-02.50, CANCEL-M.25, CANCEL-M.26, ABUSE-M.14, ACC-17 |
| R-10.2 | publish, reply, report | MQ-02.47, MQ-02.51, MQ-02.52, MQ-11.30, MQ-11.32, CANCEL-M.27, CANCEL-M.30, ABUSE-M.15 |
| R-10.3 | hide and restore | MQ-07.33, CANCEL-M.28, CANCEL-M.29, CANCEL-M.30, ACC-38 |
| R-10.4 | rating display | MQ-01.17, MQ-02.47, CANCEL-M.24, CANCEL-M.28, ACC-17 |
| R-11.1 | location and radius | MQ-01.06, MQ-01.07, MQ-01.08, MQ-01.09, MQ-01.10, MQ-01.11, MQ-01.13, MQ-01.14, MQ-01.15, MQ-01.27 (+3 more) |
| R-11.2 | home map | MQ-01.11, MQ-01.12, MQ-01.21, MQ-01.23, TIMER-M.35 |
| R-11.3 | search matching, grouped | MQ-01.29, MQ-01.30, I18N-M.09, ACC-14 |
| R-11.4 | filter first | MQ-01.16, MQ-01.28 |
| R-11.5 | score and sort | TIMER-M.30, OFFER-M.22, OFFER-M.23, STOCK-M.11 |
| R-11.6 | has offer | OFFER-M.21 |
| R-11.7 | zero results | MQ-01.26, ADMIN-M.04 |
| R-11.8 | freshness label "Confirmed … ago" (§11.1) | ASK-M.07, ASK-M.11, ASK-M.33 |
| R-11.9 | badges, at most one per card (§11.1) | ASK-M.07, ASK-M.09 |
| R-11.10 | summary line "Checked N shops nearby" (§11.1) | ASK-M.07, ASK-M.08, ASK-M.10 |
| R-11.11 | refine chips, parser first (§11.1) | ASK-M.12, ASK-M.13, ASK-M.14 |
| R-11.12 | voice search, AI-C5 (§11.1) | ASK-M.19, ASK-M.20, ASK-M.21, ASK-M.22, ASK-M.23, ASK-M.24, ASK-M.70 |
| R-11.13 | share link and preview (§11.1) | ASK-M.15, ASK-M.16, ASK-M.17, ASK-M.18, ASK-M.69 |
| R-11.14 | ask: where it starts, who can ask, the ask (§11.2) | ASK-M.01, ASK-M.02, ASK-M.03, ASK-M.10, ASK-M.11, ASK-M.26, ASK-M.43, ASK-M.45, ASK-M.66 |
| R-11.15 | which shops get an ask (§11.2) | ASK-M.02, ASK-M.04, ASK-M.06, ASK-M.26, ASK-M.43, ASK-M.50, ASK-M.65, ASK-M.66 |
| R-11.16 | what the shop sees: distance band only (§11.2) | ASK-M.05, ASK-M.27, ASK-M.28 |
| R-11.17 | the reply: Yes, No, price, stock confirm (§11.2) | ASK-M.29, ASK-M.30, ASK-M.31, ASK-M.34, ASK-M.35, ASK-M.36, ASK-M.37, ASK-M.44, ASK-M.47, ASK-M.48 |
| R-11.18 | what the customer sees (§11.2) | ASK-M.05, ASK-M.32, ASK-M.38 |
| R-11.19 | booking from a reply (§11.2) | ASK-M.38, ASK-M.39, ASK-M.40, ASK-M.41, ASK-M.42, ASK-M.67, ASK-M.68 |
| R-11.20 | when an ask closes (§11.2) | ASK-M.02, ASK-M.05, ASK-M.38, ASK-M.48 |
| R-11.21 | unanswered demand counts once (§11.2) | ASK-M.48 |
| R-11.22 | demand gaps, A-23 (§11.2) | ASK-M.55, ASK-M.56, ASK-M.57 |
| R-11.23 | abuse controls: low yield, report, mute (§11.2) | ASK-M.46, ASK-M.49, ASK-M.50, ASK-M.51, ASK-M.52, ASK-M.53, ASK-M.54 |
| R-11.24 | kept for ask_retention_days; not in v1 (§11.2) | ASK-M.28 |
| R-11.25 | site hero line and comparison section (§11.3) | ASK-M.59, ASK-M.64 |
| R-11.26 | site search strip (§11.3) | ASK-M.55, ASK-M.59, ASK-M.60, ASK-M.61, ASK-M.62, ASK-M.63 |
| R-12.1 | business feature list | MQ-03.04 |
| R-12.2 | offer kinds | MQ-11.27, OFFER-M.01, OFFER-M.04, OFFER-M.05, OFFER-M.06, OFFER-M.07, OFFER-M.08, INSIGHT-M.19 |
| R-12.3 | offer lifecycle, limits, preview | OFFER-M.01, OFFER-M.14, OFFER-M.15, OFFER-M.16, OFFER-M.17, OFFER-M.18, OFFER-M.19 |
| R-12.4 | pricing rule | MQ-04.13a, OFFER-M.03, OFFER-M.06, OFFER-M.08, OFFER-M.09, OFFER-M.10, OFFER-M.11, OFFER-M.13, STOCK-M.22 |
| R-12.5 | offers shown | MQ-01.20, MQ-01.32, OFFER-M.02, OFFER-M.20 |
| R-12.6 | banners | MQ-01.20, MQ-01.31, OFFER-M.20, OFFER-M.24, OFFER-M.25, OFFER-M.26, OFFER-M.27, OFFER-M.28, OFFER-M.29 |
| R-12.7 | voice | MQ-11.15, MQ-11.16, STOCK-M.30, FAIL-M.16 |
| R-12.8 | demand insights | MQ-11.26, MQ-11.33, OFFER-M.07, STOCK-M.19, INSIGHT-M.01, INSIGHT-M.02, INSIGHT-M.03, INSIGHT-M.04, INSIGHT-M.05, INSIGHT-M.06 (+35 more) |
| R-12.9 | POS | MQ-03.21, MQ-03.22, MQ-03.27, MQ-03.45, MQ-03.46, STOCK-M.23, STOCK-M.27, STOCK-M.29, FAIL-M.18, ACC-28 |
| R-12.10 | receipts | MQ-03.40, MQ-03.41, TIMER-M.53, TIMER-M.54, I18N-M.10, ACC-27 |
| R-12.11 | customer purchases | MQ-03.31, MQ-03.38 |
| R-13.1 | admin features | MQ-05.15, MQ-06.20, MQ-12.10, ADMIN-M.02, ADMIN-M.18, ADMIN-M.23, ADMIN-M.28, ACC-40, ACC-41 |
| R-13.2 | roles | MQ-06.17, MQ-06.22, MQ-06.23, MQ-07.09, MQ-07.10, MQ-07.12, MQ-07.13, MQ-07.15, MQ-07.16, MQ-07.17 (+18 more) |
| R-13.3 | KYC, Pulse, money settings | MQ-06.38, MQ-07.29, MQ-07.30 |
| R-13.4 | background jobs (D-95) and System (D-97) | JOBS-M.01, JOBS-M.08, JOBS-M.14, JOBS-M.19 |
| R-14.1 | field-agent login | MQ-05.26, ACC-01 |
| R-14.2 | create business with owner OTP | MQ-05.27, MQ-05.28, MQ-05.29, MQ-05.30, MQ-05.32, MQ-05.33, ACC-01, ACC-02 |
| R-14.3 | edit window | MQ-05.38, MQ-05.39, MQ-05.40, MQ-05.41, PERM-M.28, PERM-M.30, ACC-06 |
| R-14.4 | stats | MQ-05.37, MQ-07.11, PERM-M.32 |
| R-15.1 | channels, flags, keys | MQ-07.53, MQ-07.55, NOTIFY-M.01, NOTIFY-M.12, NOTIFY-M.16, NOTIFY-M.17, NOTIFY-M.18, NOTIFY-M.19, FAIL-M.13 |
| R-15.2 | new order to business plus SMS at 3 min | MQ-02.02, MQ-02.22, MQ-03.06, MQ-03.08, MQ-03.09, NOTIFY-M.02, NOTIFY-M.13, NOTIFY-M.14, NOTIFY-M.15, NOTIFY-M.24 |
| R-15.3 | accepted, partial, rejected, expired | MQ-02.28, MQ-02.32, TIMER-M.03, CANCEL-M.08, NOTIFY-M.03, NOTIFY-M.04, NOTIFY-M.21 |
| R-15.4 | ready | MQ-02.33, NOTIFY-M.05 |
| R-15.5 | hold ending in 10 min | TIMER-M.07, TIMER-M.08, NOTIFY-M.06 |
| R-15.6 | out for delivery and delivered | MQ-04.20, NOTIFY-M.07 |
| R-15.7 | referral and credits | MONEY-M.11, MONEY-M.40, NOTIFY-M.08, NOTIFY-M.09, NOTIFY-M.25 |
| R-15.8 | payout paid | MQ-06.33, NOTIFY-M.10 |
| R-15.9 | new review | MQ-02.51, NOTIFY-M.11 |
| R-15.10 | new ask to the shop: push and in-app, no SMS | ASK-M.28 |
| R-15.11 | one customer push per ask, at the first Yes | ASK-M.32, ASK-M.38 |
| R-16.1 | minimal data, location | PWA-M.13, PWA-M.14, ADMIN-M.03 |
| R-16.2 | phone masking | MQ-02.23, MQ-04.17, MQ-04.19, MQ-04.24, INSIGHT-M.21, PERM-M.16, ADMIN-M.15, ACC-21 |
| R-16.3 | demand aggregated, k ≥ 10 | ADMIN-M.03, ADMIN-M.04, ACC-45 |
| R-16.4 | coach k ≥ 10 | MQ-11.34, INSIGHT-M.02, INSIGHT-M.07, INSIGHT-M.10, INSIGHT-M.14, INSIGHT-M.23, INSIGHT-M.25 |
| R-16.5 | H3 area count | INSIGHT-M.25, INSIGHT-M.26, INSIGHT-M.34, INSIGHT-M.44, ADMIN-M.05, ACC-45 |
| R-16.6 | account deletion | MONEY-M.61, MONEY-M.62, MONEY-M.63 |
| R-16.7 | asks never carry name, phone, address or exact distance | ASK-M.05, ASK-M.27, ASK-M.28, ASK-M.56 |
| R-16.8 | search proof counts stores; share preview has no distance | ASK-M.07, ASK-M.16 |
| R-16.9 | site strip: master names, at least 10 searchers, approved | ASK-M.58, ASK-M.60 |
| R-16.10 | voice: audio and transcript never stored | ASK-M.22, ASK-M.70 |
| R-17.1 | settings canonical, money keys super_admin only | MQ-07.47, MQ-07.48, MQ-07.49, MQ-07.50, MQ-07.51, MQ-07.52, MQ-12.21, MQ-12.22, INSIGHT-M.29, INSIGHT-M.30 (+9 more) |
| R-17.2 | feature flags | MQ-04.40, MQ-04.41, MQ-04.43, OFFER-M.30 |
| R-17.3 | AI flags, caps, models | MQ-11.01, MQ-11.02, MQ-11.03, MQ-11.04, MQ-11.05, MQ-11.43, MQ-11.44, MQ-11.46, ADMIN-M.21, FAIL-M.15 (+4 more) |
| R-17.4 | client versions | PWA-M.11, PWA-M.12 |
| R-17.5 | asks and search proof keys, floors of 10 | ASK-M.06, ASK-M.45, ASK-M.58, ASK-M.67 |

## 4. Screens (06)

| Screen | Name | Steps | First steps |
|---|---|---|---|
| C-01 | Language picker (first run) | 8 | MQ-01.01, MQ-01.02, MQ-01.03, MQ-01.04, MQ-01.05, MQ-09.01 (+2 more) |
| C-02 | Location set | 12 | MQ-01.03, MQ-01.04, MQ-01.06, MQ-01.07, MQ-01.08, MQ-01.09 (+6 more) |
| C-03 | Home | 53 | MQ-01.10, MQ-01.11, MQ-01.12, MQ-01.13, MQ-01.14, MQ-01.15 (+47 more) |
| C-04 | Map full screen | 3 | MQ-01.23, MQ-01.24, MQ-01.25 |
| C-05 | Search | 38 | MQ-01.29, MQ-01.30, MQ-05.21, MQ-08.06, MQ-08.12, MQ-08.18 (+32 more) |
| C-06 | Category | 1 | MQ-01.28 |
| C-07 | Shop page | 47 | MQ-01.31, MQ-01.32, MQ-01.33, MQ-01.34, MQ-01.37, MQ-01.38 (+41 more) |
| C-08 | Product detail | 16 | MQ-01.35, MQ-01.36, MQ-09.11, MQ-09.12, MQ-09.22, OFFER-M.02 (+10 more) |
| C-09 | Cart | 29 | MQ-01.36, MQ-02.04, MQ-02.05, MQ-02.06, MQ-02.07, MQ-02.08 (+23 more) |
| C-10 | Sign-in (at checkout or from Account) | 17 | MQ-02.09, MQ-02.10, MQ-02.11, MQ-02.12, MQ-02.13, MQ-09.14 (+11 more) |
| C-11 | Checkout | 39 | MQ-02.14, MQ-02.16, MQ-02.17, MQ-02.18, MQ-04.13b, MQ-04.14 (+33 more) |
| C-12 | Order placed / tracking | 69 | MQ-02.20, MQ-02.28, MQ-02.29, MQ-02.30, MQ-02.32, MQ-02.33 (+63 more) |
| C-13 | Orders list | 5 | MQ-01.39, MQ-02.44, MQ-03.31, MQ-03.38, CANCEL-M.26 |
| C-14 | Review | 9 | MQ-02.45, MQ-02.46, MQ-02.47, MQ-02.49, MQ-02.50, MQ-02.52 (+3 more) |
| C-15 | Wallet | 32 | MQ-01.39, MQ-02.21, MQ-02.41, MQ-02.42, MQ-03.33, MQ-03.37 (+26 more) |
| C-16 | Refer | 2 | MONEY-M.32, MONEY-M.33 |
| C-17 | Scan bill | 10 | MQ-03.32, MQ-03.35, MQ-03.36, MQ-03.47, MQ-03.52, MQ-03.54 (+4 more) |
| C-18 | Account | 10 | MQ-01.39, MQ-08.01, MQ-08.02, MONEY-M.61, NOTIFY-M.20, NOTIFY-M.25 (+4 more) |
| C-19 | Help | 2 | CANCEL-M.31, CANCEL-M.32 |
| C-20 | Notifications inbox | 9 | MQ-02.32, MONEY-M.11, MONEY-M.40, NOTIFY-M.03, NOTIFY-M.18, NOTIFY-M.21 (+3 more) |
| C-21 | Restricted state | 4 | MQ-01.39, TIMER-M.25, I18N-M.11, ACC-19 |
| C-24 | Receipt (public) | 6 | MQ-03.41, TIMER-M.53, TIMER-M.54, SEC-M.18, I18N-M.10, ACC-27 |
| C-25 | Ask sheet | 10 | ASK-M.01, ASK-M.03, ASK-M.04, ASK-M.06, ASK-M.26, ASK-M.43 (+4 more) |
| C-26 | Ask progress and replies | 15 | ASK-M.01, ASK-M.02, ASK-M.04, ASK-M.05, ASK-M.26, ASK-M.27 (+9 more) |
| B-01 | Sign-in + business picker | 8 | MQ-02.01, MQ-03.01, MQ-03.02, MQ-03.03, MQ-03.05, PERM-M.01 (+2 more) |
| B-02 | Onboarding wizard | 16 | MQ-05.01, MQ-05.02, MQ-05.03, MQ-05.04, MQ-05.05, MQ-05.06 (+10 more) |
| B-03 | Overview | 31 | MQ-02.01, MQ-03.03, MQ-03.04, MQ-03.08, MQ-03.58, MQ-05.22 (+25 more) |
| B-04 | Orders | 55 | MQ-02.02, MQ-02.22, MQ-02.23, MQ-02.26, MQ-02.33, MQ-02.34 (+49 more) |
| B-05 | Sell (POS) | 48 | MQ-03.21, MQ-03.22, MQ-03.23, MQ-03.24, MQ-03.25, MQ-03.26 (+42 more) |
| B-06 | Voice update | 8 | MQ-11.02, MQ-11.15, MQ-11.17, MQ-11.21, MQ-11.22, STOCK-M.30 (+2 more) |
| B-07 | Stock and products | 57 | MQ-02.03, MQ-02.25, MQ-02.27, MQ-02.38, MQ-03.43, MQ-03.51 (+51 more) |
| B-08 | Offers and banners | 14 | OFFER-M.01, OFFER-M.17, OFFER-M.19, OFFER-M.24, OFFER-M.26, OFFER-M.28 (+8 more) |
| B-19 | Shop coach | 7 | MQ-11.33, MQ-11.34, MQ-11.35, MQ-11.36, MQ-11.37, MQ-11.38 (+1 more) |
| B-09 | Customers | 7 | INSIGHT-M.21, INSIGHT-M.26, PERM-M.03, PERM-M.16, PERM-M.17, SEC-M.26 (+1 more) |
| B-10 | Credits and referrals | 24 | MQ-02.43, MQ-05.24, MQ-06.01, MQ-06.02, MQ-06.03, MQ-06.13 (+18 more) |
| B-11 | Reviews | 11 | MQ-02.51, MQ-02.52, MQ-11.02, MQ-11.30, MQ-11.31, CANCEL-M.27 (+5 more) |
| B-12 | Analytics | 22 | MQ-03.59, OFFER-M.19, STOCK-M.26, INSIGHT-M.07, INSIGHT-M.09, INSIGHT-M.10 (+16 more) |
| B-13 | Stores and staff | 9 | MQ-05.42, MQ-05.44, PERM-M.03, PERM-M.08, PERM-M.09, PERM-M.11 (+3 more) |
| B-14 | Settings | 19 | MQ-04.01, MQ-04.02, MQ-04.04, MQ-05.12, MQ-05.13, MQ-05.14 (+13 more) |
| B-15 | Help | 3 | CANCEL-M.34, PERM-M.12, ADMIN-M.25 |
| B-16 | Notifications inbox | 13 | MQ-02.51, MQ-03.60, MQ-04.32, MQ-04.33, MQ-06.33, TIMER-M.41 (+7 more) |
| B-20 | Asks inbox and reply | 12 | ASK-M.05, ASK-M.28, ASK-M.29, ASK-M.34, ASK-M.35, ASK-M.36 (+6 more) |
| A-01 | Sign-in | 7 | MQ-07.01, MQ-07.03, MQ-07.04, MQ-09.25, MQ-09.26, TIMER-M.50 (+1 more) |
| A-02 | Pulse | 10 | MQ-07.04, MQ-07.19, MQ-09.32, MQ-12.10, PERM-M.22, ADMIN-M.02 (+4 more) |
| A-03 | Businesses | 19 | MQ-05.15, MQ-05.16, MQ-05.17, MQ-05.20, MQ-06.38, MQ-07.12 (+13 more) |
| A-04 | Create business for shop (field agent) | 14 | MQ-05.26, MQ-05.27, MQ-05.28, MQ-05.29, MQ-05.30, MQ-05.31 (+8 more) |
| A-05 | Stores | 16 | MQ-05.44, MQ-05.45, TIMER-M.05, TIMER-M.20, TIMER-M.24, TIMER-M.29 (+10 more) |
| A-06 | Customers | 15 | MQ-07.15, MQ-07.34, MQ-09.31, MONEY-M.62, TIMER-M.10, TIMER-M.16 (+9 more) |
| A-07 | Orders | 18 | MQ-07.31, MQ-09.27, MQ-09.28, MQ-09.29, MQ-09.30, MQ-09.34 (+12 more) |
| A-08 | Catalogue | 13 | MQ-07.17, MQ-11.03, MQ-11.41, STOCK-M.14, STOCK-M.16, PERM-M.22 (+7 more) |
| A-09 | Credits | 19 | MQ-07.25, MQ-07.36, MQ-07.39, MQ-07.41, MQ-07.42, MQ-09.34 (+13 more) |
| A-10 | Referrals | 2 | MONEY-M.51, MONEY-M.52 |
| A-11 | Payouts | 21 | MQ-06.06, MQ-06.07, MQ-06.08, MQ-06.09, MQ-06.10, MQ-06.11 (+15 more) |
| A-12 | Reviews | 8 | MQ-07.33, CANCEL-M.27, CANCEL-M.28, CANCEL-M.29, PERM-M.22, SEC-M.27 (+2 more) |
| A-13 | Risk | 20 | MQ-06.20, MQ-07.13, MQ-11.03, MQ-11.42, MONEY-M.21, MONEY-M.48 (+14 more) |
| A-14 | Support | 10 | MQ-11.03, MQ-11.39, MQ-11.40, CANCEL-M.32, CANCEL-M.33, CANCEL-M.35 (+4 more) |
| A-15 | Field agents | 6 | MQ-05.37, MQ-07.11, PERM-M.22, PERM-M.32, ABUSE-M.07, ACC-02 |
| A-16 | Notifications | 8 | MQ-07.53, MQ-07.55, NOTIFY-M.17, NOTIFY-M.19, NOTIFY-M.30, NOTIFY-M.31 (+2 more) |
| A-17 | Settings | 21 | MQ-04.40, MQ-07.47, MQ-07.49, MQ-07.53, MQ-07.54, MQ-09.33 (+15 more) |
| A-18 | Admin users | 8 | MQ-07.23, MQ-07.24, MQ-07.26, MQ-07.56, MQ-07.57, MQ-07.58 (+2 more) |
| A-19 | Audit log | 19 | MQ-06.15, MQ-06.28, MQ-07.06, MQ-07.29, MQ-07.42, TIMER-M.16 (+13 more) |
| A-20 | Banner moderation | 4 | OFFER-M.24, OFFER-M.25, OFFER-M.28, PERM-M.22 |
| A-21 | Jobs | 21 | PERM-M.22, JOBS-M.01, JOBS-M.02, JOBS-M.03, JOBS-M.04, JOBS-M.06 (+15 more) |
| A-22 | System | 6 | JOBS-M.21, JOBS-M.22, JOBS-M.23, JOBS-M.24, JOBS-M.25, JOBS-M.34 |
| A-23 | Demand | 3 | ASK-M.56, ASK-M.57, ASK-M.60 |
| S-01 | Home | 8 | A11Y-M.05, SITE-M.01, SITE-M.02, SITE-M.13, SITE-M.16, SITE-M.18 (+2 more) |
| S-02 | For retailers | 6 | SITE-M.02, SITE-M.04, SITE-M.07, SITE-M.09, SITE-M.13, SITE-M.16 |
| S-03 | How it works / Pricing ("Free for now") / About / Contact | 1 | SITE-M.05 |
| S-04 | Legal | 3 | SITE-M.02, SITE-M.06, SITE-M.13 |
| S-05 | Localised home + retailers | 1 | SITE-M.11 |
| S-06 | SEO | 3 | SITE-M.12, SITE-M.13, SITE-M.14 |
| S-07 | Home search sections | 3 | ASK-M.59, ASK-M.61, ASK-M.63 |

## 5. Problem codes and HTTP statuses

A step counts for a code or status when its text names it (currency amounts such as ₹403 do not count). Counts and first steps come from scanning the step rows, in book order.

| Code or status | Steps | First steps |
|---|---|---|
| `invalid_transition` | 18 | MQ-02.31, MQ-02.37, MQ-06.31, MQ-12.03, TIMER-M.04, TIMER-M.14 (+12 more) |
| `idempotency_mismatch` | 3 | MQ-06.30, SEC-M.32, FAIL-M.04 |
| `insufficient_stock` | 3 | STOCK-M.20, STOCK-M.21, I18N-M.08 |
| `store_closed` | 7 | TIMER-M.34, TIMER-M.36, TIMER-M.37, TIMER-M.38, TIMER-M.39, I18N-M.08 (+1 more) |
| `store_not_bookable` | 3 | MQ-04.42, ASK-M.68, SEC-M.40 |
| `delivery_closed` | 1 | MQ-04.13b |
| `ordering_suspended` | 1 | TIMER-M.26 |
| `extension_already_used` | 1 | TIMER-M.12 |
| `review_edit_window_passed` | 1 | MQ-02.50 |
| `store_credit_cap` | 1 | MONEY-M.21 |
| `ai_daily_cap_reached` | 1 | MQ-11.43 |
| `code_locked` | 3 | SEC-M.20, I18N-M.08, ABUSE-M.04 |
| `account_has_active_order` | 3 | MONEY-M.61, FAIL-M.06, ACC-19 |
| `resync_required` | 1 | STOCK-M.27 |
| `reversal_window_passed` | 1 | TIMER-M.56 |
| `approver_is_requester` | 1 | MQ-07.41 |
| `otp_locked` | 2 | SEC-M.01, ABUSE-M.03 |
| `otp_expired` | 1 | TIMER-M.47 |
| `account_blocked` | 3 | SEC-M.07, ADMIN-M.17, ASK-M.03 |
| `step_up_required` | 4 | MQ-06.26, MQ-07.49, ADMIN-M.27, JOBS-M.20 |
| `price_changed` | 3 | CANCEL-M.07, OFFER-M.18, STOCK-M.22 |
| `below_credit_minimum` | 4 | MONEY-M.03, MONEY-M.04, OFFER-M.12, I18N-M.08 |
| `below_minimum` | 3 | MQ-04.09, MQ-04.10, MQ-04.37 |
| `credit_monthly_cap` | 2 | MONEY-M.25, ABUSE-M.09 |
| `credit_max_exceeded` | 1 | MONEY-M.02 |
| `claim_expired` | 2 | MQ-03.52, TIMER-M.52 |
| `claim_already_used` | 1 | SEC-M.21 |
| `job_not_runnable` | 1 | JOBS-M.11 |
| `job_already_running` | 1 | JOBS-M.10 |
| `job_paused` | 1 | JOBS-M.12 |
| `ask_limit_reached` | 1 | ASK-M.45 |
| `ask_already_open` | 1 | ASK-M.01 |
| `ask_no_shops` | 1 | ASK-M.65 |
| `ask_store_unavailable` | 3 | ASK-M.06, ASK-M.50, ASK-M.51 |
| `ask_closed` | 1 | ASK-M.48 |
| `price_above_mrp` | 2 | ASK-M.29, ASK-M.34 |
| `ask_reply_used` | 1 | ASK-M.42 |
| `ask_price_expired` | 1 | ASK-M.67 |
| `share_card_unavailable` | 1 | ASK-M.69 |
| 401 | 5 | MQ-07.02, SEC-M.12, SEC-M.13, SEC-M.35, ADMIN-M.01 |
| 403 | 60 | MQ-05.40, MQ-05.41, MQ-06.05, MQ-06.18, MQ-06.19, MQ-06.22 (+54 more) |
| 404 | 7 | MQ-11.04, CANCEL-M.22, NOTIFY-M.22, SEC-M.14, SEC-M.15, SEC-M.36 (+1 more) |
| 405 | 1 | PERM-M.12 |
| 409 | 42 | MQ-02.31, MQ-02.37, MQ-02.48, MQ-02.50, MQ-04.13b, MQ-04.42 (+36 more) |
| 410 | 3 | STOCK-M.27, ASK-M.69, NOTIFY-M.22 |
| 412 | 2 | SEC-M.31, SEC-M.38 |
| 413 | 2 | NOTIFY-M.23, SEC-M.33 |
| 421 | 1 | SEC-M.34 |
| 422 | 15 | MQ-06.30, MQ-07.40, CANCEL-M.11, CANCEL-M.19, CANCEL-M.25, STOCK-M.04 (+9 more) |
| 429 | 10 | MQ-11.43, TIMER-M.44, SEC-M.02, SEC-M.03, SEC-M.04, SEC-M.18 (+4 more) |
| 503 | 3 | JOBS-M.27, FAIL-M.01, FAIL-M.12 |

## 6. Questions for the lead

Each question names the steps that depend on it. Q-01 to Q-49 are resolved (D-91 to D-94 in the decision log, and the plan files named in the Status column); the steps now state the expected result. Q-50, Q-51 and Q-52 are new and open (Q-52, added with D-99, blocks no step because the "Priced above nearby" kind is off by default). Q-53 to Q-57 (D-101) are open founder questions; the ASK script tests the plan's default for each, so none blocks a step. Q-58 is open for the lead. If the build differs from the resolution, mark the step **B**.

### A. Disagreements between plan files (01, 02, 04, 05, 06, 08, 12)

| Q | Question | Steps that depend on it | Status |
|---|---|---|---|
| Q-05 | Payout export: 02 gives `admin` the export; 04, 06 A-11 and D-85 say finance and super_admin only. D-85 asks for step-up on export; 02 and 04 do not mark it. | MQ-06.17 | Resolved: D-85, D-64 / 02 §3.3, 04 §2.7 |
| Q-06 | 01 §13 lists the admin role's payout exceptions as "mark paid and hold" only; 04 also gives unhold and export. | MQ-06.16, MQ-06.17, MQ-06.21, MQ-06.39, PERM-M.24 | Resolved: D-64 / 01 §13, 06 §3 |
| Q-07 | 02 and 04 let `support` GET businesses, but the 06 support nav has no A-03. | no step yet; affects the rule named | Resolved: D-93 / 06 §3, A-03 (read-only) |
| Q-08 | Payout details: 08 SC-BZ says owner and finance only; 01, 02 and 06 say a manager sees them masked. MQ-06.05 also uses a `payout-details` path where 04 has `{B}/kyc`. | MQ-06.05, ACC-04 | Resolved: 01 §9, 02 §3.3 / 08 SC-BZ (`{B}/kyc`) |
| Q-09 | 08 BK-18 says a shop cancel "counts against shop reliability"; 01 §5.4, 04 and D-68 give no strike for it. | CANCEL-M.09, CANCEL-M.10, CANCEL-M.11, CANCEL-M.12 | Resolved: D-68 / 08 BK-18 |
| Q-12 | Wallet line copy: 01 "Spent at Sri Lakshmi · order TZ-…" vs 08 CR-04 "Spent at <shop> · TZ-…". | MQ-02.42 | Resolved: 01 §7 / 08 CR-04 |
| Q-17 | Superseded decisions still quoted: D-30 earn caps (withdrawn by D-62); D-01 `picked_up` (now `collected`, D-61); D-45 C-08 (moved to C-13 by D-75); D-38 five roles (now seven, D-64). | MONEY-M.23, STOCK-M.02, PERM-M.13, SEC-M.18 | Resolved: 12 (superseded tags under D-01, D-30, D-38, D-45) |
| Q-19 | 02 names Sentry, but 05 and D-17 keep Sentry off before Wave 5. | MQ-07.53 | Resolved: 02 §2.3 (D-17) |
| Q-24 | Benchmarks use k ≥ 5 stores in 05; coach and demand use k ≥ 10 people in 01. Two different k on purpose? (note only) | no step yet; affects the rule named | Resolved: 01 §16 / 05 §4 |
| Q-30 | 01 §9 creates a payout only when KYC is verified; 08 PO-02 expects a `held` payout for missing KYC. | MQ-06.11 | Resolved: 01 §9 / 03 §8 |
| Q-44 | The SMS escalation "3 min" is a **default** in 01 §15 but has no key in 01 §17 (the canonical key list). Which number gets the SMS (store phone, owner, every manager)? In which language? | NOTIFY-M.16 | Resolved: D-92 (founder) / 01 §15, §17; 07 W2-05 |
| Q-45 | `search_w_*` keys have unit `bp`; the unit rule (R-17.1) makes every `_bp` key a money setting for super_admin only. Intended for ranking weights? (OFFER-M.23, PERM-M.26) | OFFER-M.23, PERM-M.26 | Resolved: D-93 / 01 §17 |
| Q-46 | The site must ship zero client JS (`verify-dist.mjs` refuses any script or remote src), but the lead form uses Turnstile, which needs a script. Which wins? Also: which admin screen shows leads? | SITE-M.09, SITE-M.10 | Resolved: D-93 / 02 §4.5, 04 §2.7, 06 A-03 |
| Q-48 | Load scenario names: 02 §11 says `search_peak`; 08 §6 says `browse_peak` (search inside it). | PERF-M.12 | Resolved: 02 §11 |
| Q-49 | Reviews: 06 A-12 and 08 E2E-10 say "delete"; 01 §10 says hide/restore (soft delete with reason). The book tests hide and restore. | MQ-07.33, CANCEL-M.28, ACC-38 | Resolved: 01 §10 / 06 A-12, 08 E2E-10 |

### B. Rules that are unclear or missing (money, codes, timers, permissions)

| Q | Question | Steps that depend on it | Status |
|---|---|---|---|
| Q-10 | Codes not named: CR-11 "refused and flagged"; POS-09 "already claimed". Codes used in 01/08 but missing from the 04 §1.4 list: `credit_max_exceeded`, `below_credit_minimum`, `credit_monthly_cap`, `below_minimum`, `claim_expired`, `otp_locked`, `otp_expired`, `account_blocked`, `step_up_required`, `price_changed`. | MONEY-M.02, MONEY-M.21, TIMER-M.26 | Resolved: D-91 / 04 §1.4 |
| Q-13 | Does the client countdown use server time? With the test clock the display matches only if it does. | MQ-02.54 | Resolved: D-93 / 04 §1 (`serverNow`) |
| Q-14 | "Install prompt after the 2nd order": after it is placed or after it is completed? | MQ-02.57, PWA-M.02 | Resolved: D-93 (founder) / 06 customer global |
| Q-15 | The refusal code for editing a review after 7 days is not named. | MQ-02.50 | Resolved: D-91 / 04 §1.4 (`review_edit_window_passed`) |
| Q-16 | Fixed numbers with no settings key (are they meant to be constants?): SMS escalation 3 min; "Hold ending in 10 min"; 7-day expiry reminder; review edit 7 days and 1000 chars; bill QR 120 s; counter code 60 s; 5 code tries; receipt 30 days; same device 90 days; Bayesian prior 4.0 weight 5; step-up 10 min; sync cursor 30 days; banner attribution 24 h; OTP limits; device clock 2 min; voice 15 s; `sold_at` clamp 72 h; SSE poll 15 s; ≥ 3 expired in 2 h alert. D-85 confirms 2 h / 24 h / 72 h and the 30-day strike window as constants. | MQ-03.34, ACC-26 | Resolved: D-92 / 01 §17 |
| Q-18 | 01 §15 has no business row for "credits expiring" and no WhatsApp mapping per event. | MONEY-M.11, NOTIFY-M.09 | Resolved: D-93 / 01 §15 |
| Q-23 | `ai_daily_cost_cap_paise` is a money key by unit: super_admin only? Confirm. | MQ-11.46 | Resolved: D-93 / 11 §1 rule 7 |
| Q-25 | POS round-off direction at exactly ₹0.50 is not defined. | MQ-03.27, PERM-M.19, A11Y-M.02 | Resolved: D-94 (founder) / 01 §12, 08 POS-16 |
| Q-27 | Free delivery "above ₹500": is exactly ₹500 free? The same for an offer's minimum bill: inclusive, and measured after item offers? | MQ-04.13, OFFER-M.06 | Resolved: D-94 (founder) / 01 §5.5, §12 |
| Q-28 | `feature_delivery_enabled` off with a delivery-only store: does it become "In-store only"? | MQ-04.42 | Resolved: D-93 / 01 §5 |
| Q-29 | Delivery hours narrower than opening hours: which refusal code? | MQ-04.01 | Resolved: D-91 / 01 §5.5, 04 §1.4 (`delivery_closed`) |
| Q-31 | The statement close time on the 1st is not given (the book waits up to 6 h of test clock). | MQ-06.07, ACC-33 | Resolved: 01 §9 (00:30 IST) |
| Q-32 | Payout CSV: are held payouts in the file? Which payee columns? | MQ-06.24, MQ-06.25 | Resolved: D-93 / 04 §2.7, 03 §8 |
| Q-33 | CR-15 uses ₹500.01: can an admin adjustment carry fractional credits? | MQ-07.42 | Resolved: D-93 / 08 CR-15 |
| Q-34 | Digits in Indic locales (Latin or native) are not specified; the book assumes Latin and records what it sees. | I18N-M.02 | Resolved: D-93 / 02 §4.1, 07 W1-03 |
| Q-35 | Refusal code and copy when an AI per-customer or per-store daily cap is hit are not named. | MQ-11.43 | Resolved: D-91 (founder) / 11 §1 rule 7, 04 §1.4 |
| Q-36 | Does the grounding validator (11 §1 rule 5) apply to review reply drafts? | MQ-11.31 | Resolved: D-93 / 11 §1 rule 5 |
| Q-37 | No local hook is named for forcing a slow route or slow query (OB-03, OB-04). | MQ-12.15, PERF-M.10 | Resolved: D-93 / 04 §2.9 |
| Q-38 | Do `credit_orders_per_month` and `store_credit_redemptions_per_day` count at place (hold) or at capture? Does a cancelled credit order still count? | notes in MONEY-wallet-credits-payouts.md | Resolved: D-93 (founder) / 01 §7 |
| Q-39 | When does a referral reward post after qualification? The POS-21 "not yet posted" branch may only be testable automatically. | notes in MONEY-wallet-credits-payouts.md | Resolved: D-93 / 01 §8, 08 POS-21 |
| Q-40 | Also unclear: lot expiry time of day; the code for the no-show limit and the second extension; no "all day" hours option; whether an order placed before a timer setting change keeps the old timer (TIMER-M.57); whether accepting after closing time is allowed (TIMER-M.38); whether "first N orders" counts at place or at collect. | MONEY-M.10, OFFER-M.15, I18N-M.03 | Resolved: D-91, D-93 / 01 §5.1, §5.2, §7, §12; 03 §2 |
| Q-42 | Low stock: is "at" the threshold low, or only below it (STOCK-M.09)? Is there a per-item threshold (STOCK-M.10)? | ACC-09 | Resolved: D-93 / 01 §4 |
| Q-43 | Which code when accepting with not enough available stock (STOCK-M.21)? Is an offline sale at an old cached price flagged (STOCK-M.29)? | no step yet; affects the rule named | Resolved: D-93 / 04 §2.6, 02 §4.3 |
| Q-47 | Many wrong order codes across orders at one store raise no risk flag in 01. Should they (ABUSE-M.05)? | ABUSE-M.05 | Resolved: D-92 / 01 §17; 07 W3-02 |
| Q-53 | Is a replied price binding for `ask_price_valid_min` (60 min), or only indicative, and does it stack with offers? The plan takes binding with no stacking: booking from a Yes uses the replied price per unit, no item-level offer on that line, at most the asked quantity, one active booking per reply. | ASK-M.39, ASK-M.40, ASK-M.41, ASK-M.42, ASK-M.67 | Open (founder): D-101; the book tests binding |
| Q-54 | Should shops ever pay per ask (a lead-generation fee)? The plan has none. | no step; affects the rule named (R-11.24) | Open (founder): D-101 |
| Q-55 | Should WhatsApp delivery of asks to shops be on at launch? `ask_whatsapp_enabled` stays false. | ASK-M.28 | Open (founder): D-101; the book checks that no WhatsApp message is sent |
| Q-56 | "Ready soonest" on search results needs a pickup prep time or delivery ETA, which no store has. Add one per store, or keep the badge to ask replies? The plan keeps it ask-only. | ASK-M.07, ASK-M.32 | Open (founder): D-101 |
| Q-57 | May a cashier change the price in a reply? The plan says no (the 01 §3 price rule). | ASK-M.36, ASK-M.44 | Open (founder): D-101 |
| Q-58 | A store that already answered replies again after the ask has closed: `409 invalid_transition` or `409 ask_closed`? 01 §11.2 and 04 give both codes but no order. The book tests each alone: a second reply while the ask is open (ASK-M.30) and a first reply after the window (ASK-M.48). | ASK-M.30, ASK-M.48 | Open (lead): W3-16, D-101 |

### C. Book choices to confirm

| Q | Question | Steps that depend on it | Status |
|---|---|---|---|
| Q-01 | Book choice: mock-route numbers `+91 90000 00100`–`00199` deliver real SMS to the mock; all other reserved numbers use OTP 000000. Confirm with W0-07. | MQ-03.22, MQ-12.25, NOTIFY-M.01, NOTIFY-M.13, SEC-M.01, SEC-M.03, SEC-M.05, SITE-M.07 (+1 more) | Resolved: D-15 / 01 §2 |
| Q-02 | The body for `clock set` and `clock reset` on `/v1/test/clock` is not in 04. | MQ-01.21, MQ-01.22, MQ-02.50, MQ-04.35, MQ-05.41, MQ-06.06, MQ-06.07, MQ-06.35 (+57 more) | Resolved: 04 §2.9 (test clock) |
| Q-03 | C-03 "products" stat: distinct products or store listings? (MQ-01.12 expects 57.) | MQ-01.12 | Resolved: D-93 / 04 §2.3 (in-stock listings) |
| Q-04 | The brief's example said 15 min `order_accept_timeout`; 01 says 10 min `order_accept_timeout_min`. The book uses 01. | MQ-07.18, MQ-07.49, MQ-07.50, MQ-09.33, TIMER-M.57, ADMIN-M.27 | Resolved: 01 §5.1 (10 min) |
| Q-11 | C-12 hides Cancel in `out_for_delivery`: confirm. | MQ-04.20 | Resolved: 01 §5.2 |
| Q-20 | How seeded admins get TOTP for QA (fixed seed secret or enrol on first run). | MQ-05.15, MQ-06.26, MQ-06.28, MQ-07.01, MQ-07.02, MQ-07.03, MQ-07.06 (+7 more) | Resolved: D-93 / 08 §2, 07 W1-01 |
| Q-21 | The B-14 example "On ₹500 … 25 credits" assumes 5%; the default cashback is 0. | no step yet; affects the rule named | Resolved: D-93 / 06 B-14 |
| Q-22 | AU-06 (identical timing within 50 ms) is automated only; the manual step only records. | SEC-M.05 | Resolved: 08 AU-06 (book is right) |
| Q-26 | Do notification SMS to reserved numbers reach the mock, or are they dropped? | MQ-03.09 | Resolved: D-93 / 01 §2 |
| Q-41 | The brief mentions coupons; 01 has no coupon codes, so the book tests none. | notes in OFFER-offers-banners.md | Resolved: 10 (Not planned) |
| Q-50 | JOBS-M.14 to M.17 need a scheduled `notify` or `ai` kind that calls a mock, so that stopping the mocks makes it fail. Which kind does the seed and `make demo` provide? The book calls it K-mock and marks the steps **B** if none exists. | JOBS-M.14, JOBS-M.15, JOBS-M.16, JOBS-M.17 | Open: W1-11 / W3-15 to name one |
| Q-51 | JOBS-M.28 and M.29 need a kind that runs for several seconds and is batched (a rollup backfill, payouts, a marketing send), so a stop mid-run can be seen. Which kind and which `periodKey` range does `make demo` give? | JOBS-M.28, JOBS-M.29 | Open: W1-11 to name one |
| Q-52 | Should the Demand insights kind "Priced above nearby" be on at launch? It compares a shop's price with the median price across at least 10 live nearby shops; it is useful, but shops may dislike being compared. The book tests it with the setting switched on, and checks that it is off by default. | INSIGHT-M.43, INSIGHT-M.44, INSIGHT-M.45, INSIGHT-M.46 | Open: the founder decides. Default off (`insight_price_compare_enabled` false, 01 §12 and §17, D-99) |

### Uncovered rules

- R-IDs with no step: none.
- 08 IDs with no step and no automated-only reason: none.
- R-IDs covered only by a whole script (no single step names them) are listed in section 3 as "whole script".
