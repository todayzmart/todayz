# TodayZ manual test book

This book holds the scripts a person runs by hand. Run them at the end of each wave for the screens that wave built, and run the whole book at the Wave 4 exit (08 §8). Before the cloud launch, the founders run the ★ scripts on real phones.

The plan is the source of truth. If a step and the plan disagree, the plan wins: record the step as **B** (blocked by a book error) and add a line to "Questions for the lead" in [traceability.md](traceability.md). Rules come from [01-product-rules.md](../../plan/01-product-rules.md). Where 01 and the decision log disagree, the log ([12-decision-log.md](../../plan/12-decision-log.md)) wins.

## Files

| File | What it covers |
|---|---|
| [MQ-01-first-run-customer.md](MQ-01-first-run-customer.md) ★ | Language, location, home map, radius, categories, shop, product |
| [MQ-02-order-pickup.md](MQ-02-order-pickup.md) ★ | Cart, sign-in mid-checkout, credits, place, tracking, code at the counter, review |
| [MQ-03-shop-day.md](MQ-03-shop-day.md) ★ | Shop sign-in, sound alert, accept, partial and reject, 10 POS sales, 5 offline sales, sync, bill QR |
| [MQ-04-delivery.md](MQ-04-delivery.md) | Delivery settings, radius, out-of-radius customer, rider code, failed delivery |
| [MQ-05-onboarding.md](MQ-05-onboarding.md) ★ | Self-serve wizard; field agent flow with owner OTP; admin verify and store review |
| [MQ-06-money-month.md](MQ-06-money-month.md) | A month of money, close, statement, mark paid, business view |
| [MQ-07-admin-roles.md](MQ-07-admin-roles.md) | Each of the 7 admin roles: sidebar, forbidden actions, two-person approval, provider keys |
| [MQ-08-languages.md](MQ-08-languages.md) | Each language on the 5 main screens |
| [MQ-09-accessibility.md](MQ-09-accessibility.md) | TalkBack and VoiceOver through MQ-01 and MQ-02; keyboard-only admin |
| [MQ-10-slow-network.md](MQ-10-slow-network.md) | Slow 4G and 4× CPU on customer home and search; LCP |
| [MQ-11-ai-features.md](MQ-11-ai-features.md) | AI drafts: nothing applies without a person's confirm |
| [MQ-12-observability.md](MQ-12-observability.md) | Dashboards, traces and logs after MQ-02 and MQ-03 |
| [MONEY-wallet-credits-payouts.md](MONEY-wallet-credits-payouts.md) | Wallet, lots, caps, cashback, referrals, adjustments, reversals, ₹1 rounding, paise boundaries |
| [TIMER-order-timers.md](TIMER-order-timers.md) | Every order timer on both sides of its boundary; IST midnight; month end |
| [CANCEL-cancellations-disputes.md](CANCEL-cancellations-disputes.md) | Cancels by every actor, partial accept, failed delivery, reviews and reports, support tickets |
| [OFFER-offers-banners.md](OFFER-offers-banners.md) | Offer kinds, stacking, limits, schedules, banners, moderation |
| [STOCK-stock-catalogue.md](STOCK-stock-catalogue.md) | Counts, damage, restock, batches, import, voice drafts, bill scan, offline sync conflicts |
| [INSIGHT-demand-insights.md](INSIGHT-demand-insights.md) ★ | Demand insights card and tab: 11 kinds, sentences, actions that apply nothing, settings, permissions, privacy, "Not useful" |
| [ASK-ask-shops-nearby.md](ASK-ask-shops-nearby.md) ★ | Ask shops nearby and search proof: summary line, badges, "Confirmed … ago", refine chips, share, voice search, an ask from sending to booking at the replied price, the shop's Asks inbox and its permissions, limits, mute and report, Demand gaps and the site search strip (D-101) |
| [NOTIFY-notifications.md](NOTIFY-notifications.md) | SMS, WhatsApp, push and inbox through the mocks; opt-outs |
| [PERM-permissions-matrix.md](PERM-permissions-matrix.md) | Every business and admin role against every action |
| [SEC-security.md](SEC-security.md) | IDOR, OTP brute force, code reuse, XSS, CSV injection, tokens, headers |
| [PWA-install-offline.md](PWA-install-offline.md) | Install prompt after the 2nd order, offline, update flow, service worker |
| [I18N-languages-detail.md](I18N-languages-detail.md) | Every language on every main screen, long strings, numerals and ₹, pseudo-locale |
| [A11Y-accessibility-detail.md](A11Y-accessibility-detail.md) | Keyboard, screen reader, 200% zoom, touch targets, contrast |
| [SITE-marketing-site.md](SITE-marketing-site.md) | Zero JS, links, lead form, SEO basics |
| [ADMIN-console-screens.md](ADMIN-console-screens.md) | Every admin screen, A-01 to A-22 (A-21 and A-22 in depth in JOBS) |
| [JOBS-ops-console.md](JOBS-ops-console.md) | A-21 Jobs and A-22 System per role: run now, pause, resume, retry, a forced failure and its alert, a missed schedule; `make stop` mid-request and mid-job; updates that never interrupt a sale or checkout; AWS keys in `.env.local` stay out of the app |
| [ABUSE-abuse-cases.md](ABUSE-abuse-cases.md) | Mass referrals, OTP flooding, code guessing, offline-link abuse |
| [PERF-performance.md](PERF-performance.md) | Slow 4G plus 4× CPU across the apps; budgets |
| [FAIL-failure-modes.md](FAIL-failure-modes.md) | Database down mid-checkout, mock errors, clock skew, two tabs, double clicks, back button, refresh, expired session |
| [end-to-end-acceptance.md](end-to-end-acceptance.md) | A day in the life, from shop onboarding to dashboards, with a sign-off table |
| [traceability.md](traceability.md) | Every 08 scenario ID and every 01 rule mapped to steps; questions for the lead |

## Book size

Scanned from the files on 2026-10-22: 1168 steps in 32 scripts, 280 of them marked ★. The traceability file maps 285 of 309 08 scenario IDs to steps (276 numbered IDs and 33 SC families; 24 are automated only and say why), all 170 rules, all 72 screens and all 48 problem codes and HTTP statuses. The per-file counts and how the totals were scanned are in [traceability.md](traceability.md) section 1.

## 1. Set up the local stack

The stack runs on one laptop and never touches the internet or a cloud account.

1. Start Docker: `colima start`.
2. Reset the data: `make reset`. This recreates the database, migrates and seeds it. There is no `make seed`.
3. Start everything: `make dev`. It ends with a health table. Every row you need must be green.
4. Check the build: open `https://api.todayz.localhost/version` and note the git sha in your run header.
5. Check the env without printing values: `make env-check`. It prints `NAME set` or `NAME missing` only. Never print, paste or screenshot a secret value.
6. Run `caddy trust` once per machine if the browser says the certificate is not trusted.

| Host | What |
|---|---|
| https://app.todayz.localhost | Customer app (C-xx screens) |
| https://business.todayz.localhost | Business app and POS (B-xx screens) |
| https://admin.todayz.localhost | Admin console (A-xx screens) |
| https://api.todayz.localhost | API (`/v1/...`, `/healthz`, `/readyz`, `/version`) |
| https://mocks.todayz.localhost | Provider mocks: SMS, WhatsApp, push, Turnstile, speech-to-text, LLM |
| https://grafana.todayz.localhost | Grafana dashboards 00–15, logs (Loki), traces (Tempo) |
| https://todayz.localhost | Marketing site (S-xx screens); `/_health` is the stack health page |

Useful commands:

| Command | Use |
|---|---|
| `make stop` / `make dev` | Stop and start the whole stack. `make stop` sends SIGTERM and waits for the API and worker to drain (at most 180 s for the worker) before SIGKILL (D-98), so it can take a while when a job is running |
| `make logs` | Follow container and app logs |
| `docker compose -p todayz stop postgres` / `start postgres` | Take the database down and bring it back (FAIL script) |
| `infra/local/proc.sh status` | See which app processes are up |
| `infra/local/proc.sh stop mocks` / `make dev` | Take the mocks server down and bring it back |

## 2. Sign-in and OTP

- **Reserved numbers.** `+91 90000 00000` to `+91 90000 00099` accept the OTP `000000` in `local` and `test` only (01 §2). OTP and notification SMS to these numbers still go to the mock and show in the mocks UI; only verification is short-circuited (`000000`).
- **Mock-route numbers.** To see a real OTP pass through the SMS mock, use a synthetic number from `+91 90000 00100` to `+91 90000 00199`. These are not reserved, so the backend sends the SMS to the mock. Read the code in the mocks UI (SMS tab, click the OTP to copy it), or call `GET https://mocks.todayz.localhost/_mock/sms/latest-otp?to=919000000101&since=<RFC 3339 time>&wait=5`. A missing code returns 404 `{"error":"no OTP for that phone"}`. They are outside the reserved block (01 §2, D-15).
- **Admin sign-in** needs the phone OTP and a TOTP code. Seed admins are pre-enrolled with TOTP, except AD47, the one unenrolled seed admin (§6), who enrols on first sign-in (A-01): scan the QR into an authenticator app on a test phone. Store the 10 recovery codes in a password manager, never in a run file. TOTP codes for the other seed admins come from the "TOTP now" panel in the mocks UI (local and test only).
- **Turnstile** in the mock passes every token except `XXXX.DUMMY.TOKEN.FAIL`, which fails with `invalid-input-response`.

## 3. The test clock

The server clock can be moved in `local` and `test` only (D-80). Staging and prod return 404 for it.

| Shorthand in steps | Request |
|---|---|
| `clock now` | `GET https://api.todayz.localhost/v1/test/clock` (read the server time) |
| `clock +9m59s` | `POST https://api.todayz.localhost/v1/test/clock` with `{"advance":"9m59s"}`. Due jobs run at once. |
| `clock set 2026-11-30T23:59:59+05:30` | `POST /v1/test/clock` with `{"set":"2026-11-30T23:59:59+05:30"}` |
| `clock reset` | `POST /v1/test/clock` with `{"reset":true}` (back to real time) |

The contract is in 04 §2.9: exactly one of `advance`, `set` or `reset` per request, plus an optional `wait` (default 10s). The response is `{"now","offsetMs","dueJobs"}`. Always `clock reset` in cleanup.

All times in this book are IST (UTC+05:30). IST midnight is 18:30:00 UTC.

## 4. Settings changes during a test

Many steps change a `platform_settings` key and change it back.

| Shorthand | How |
|---|---|
| `set credit_max_per_order = 40` | Sign in to the admin console as super_admin SA48, open A-17 Settings, edit the key, give the reason "QA <step id>", pass the TOTP step-up. Before A-17 exists (Wave 2 gate), call `PATCH https://api.todayz.localhost/v1/admin/settings/credit_max_per_order` with `{"value":40,"reason":"QA <step id>"}` from a super_admin session. |
| `restore defaults` | Set every key the script changed back to its 01 §17 default. The script's cleanup lists them. |

Money settings (unit `paise`, `bp` or `credits`) need super_admin (D-64). Other keys can be changed by `admin` too.

## 5. API steps

Some steps call the API directly, for example to edit an id or to replay a request.

1. Do the action once in the app with DevTools open.
2. In the Network panel, right-click the request and pick "Copy as cURL".
3. Edit the id or body as the step says, and run it in a terminal.
4. Record only the status, the problem `code` and the field names. Never paste the `Authorization` header, a cookie, a token, an OTP or a code into a run file.

Problem responses are RFC 9457 JSON with a stable `code` (04 §1.4). The UI shows the translated message for that code and a "Try again" action (06 G1).

## 6. Seed accounts and stores

The seed (`make reset`) builds the world below. Where the seed is not built yet, steps use `<seed:name>` placeholders; the list in §7 is what the seed must provide. Numbers are written in short form: "C10" means `+91 90000 00010`.

### People

| Short | Phone | Who | State |
|---|---|---|---|
| C01–C05 | +91 90000 00001–05 | New customers | No account. `make reset` restores this. |
| C10 | +91 90000 00010 | Customer "Ananya" | Regular. Wallet 100 credits in one lot (cashback, earned 30 days ago). No open orders. |
| C11 | +91 90000 00011 | Customer "Ravi" | High wallet: 500 credits in 3 lots |
| C12 | +91 90000 00012 | Customer "Meena" | Wallet 60: lot L1 of 40 expiring tomorrow 23:59 IST, lot L2 of 20 expiring in 200 days |
| C13 | +91 90000 00013 | Customer "Kiran" | 3 no-show strikes in the last 30 days (limited) |
| C14 | +91 90000 00014 | Customer "Divya" | Referred by C10, referral pending |
| C15 | +91 90000 00015 | Customer "Arjun" | Blocked |
| C16 | +91 90000 00016 | Customer "Sana" | One order in `placed` at Sri Lakshmi |
| C17 | +91 90000 00017 | Customer "Vikram" | 9 credit-using orders this IST month; wallet 200 |
| C18 | +91 90000 00018 | Customer "Lata" | Wallet 0, two past collected orders |
| C19 | +91 90000 00019 | Customer "Imran" | Wallet 0, one past collected order |
| O20 | +91 90000 00020 | "Suresh", owner of Lakshmi Traders (stores Sri Lakshmi and Sri Lakshmi 2) | |
| M21 | +91 90000 00021 | Manager at Lakshmi Traders, all stores | |
| K22 | +91 90000 00022 | Cashier at Lakshmi Traders, Sri Lakshmi only | |
| R23 | +91 90000 00023 | Cashier with the delivery flag (rider), Sri Lakshmi only | |
| M24 | +91 90000 00024 | Manager at Lakshmi Traders, Sri Lakshmi 2 only | |
| X25 | +91 90000 00025 | Owner of Annapurna Provisions and cashier at Lakshmi Traders | Two businesses |
| O26 | +91 90000 00026 | Owner of Kaveri Fresh | |
| O27 | +91 90000 00027 | Owner of Nandi Hardware | |
| O28 | +91 90000 00028 | Owner of Auto Mart | |
| M29 | +91 90000 00029 | Owner of Marina Mart (Chennai) | Used only by INSIGHT-M.23, M.25, M.43 and M.44 |
| N30–N35 | +91 90000 00030–35 | New shop owners | No account |
| F40 | +91 90000 00040 | Field agent "Rahul" | Onboarded Annapurna Provisions |
| F41 | +91 90000 00041 | Field agent "Priya" | Onboarded nothing yet |
| S42 | +91 90000 00042 | Admin role `support` | |
| G43 | +91 90000 00043 | Admin role `catalogue` | |
| Y44 | +91 90000 00044 | Admin role `analyst` | |
| FN45 | +91 90000 00045 | Admin role `finance` (A) | |
| FN46 | +91 90000 00046 | Admin role `finance` (B) | |
| AD47 | +91 90000 00047 | Admin role `admin` | The one seed admin without TOTP: enrols in MQ-07.01 to .07 |
| SA48 | +91 90000 00048 | Admin role `super_admin` (A) | |
| SA49 | +91 90000 00049 | Admin role `super_admin` (B) | |
| D50 | +91 90000 00050 | Admin roles `support` and `catalogue` | Two roles |
| H01–H11 | +91 90000 00070–80 | Customers of Sri Lakshmi 2 (insights seed) | No wallet, no open orders. H01–H03: 3 purchases in the last 60 days, the last 22, 25 and 30 days ago. H04–H08: 4 purchases, the last 22 to 40 days ago. H09: 4 purchases, the last 20 days ago. H10: 2 purchases, the last 25 days ago. H11 (+91 90000 00080): 4 purchases, the last 3 days ago, a regular who is not slipping. So "regulars slipping away" counts H01–H08 (8). H01–H11 are the only phone-linked customers of Sri Lakshmi 2: 11, one more than `insight_regular_min_linked_customers` (10), so the default does not sit on the boundary; its walk-in sales carry no phone and are not counted. |
| C51 | +91 90000 00051 | Customer "Asha" (asks seed) | No asks, no wallet, no orders. Used by ASK sections D to F and J. |
| C52 | +91 90000 00052 | Customer "Farhan" (asks seed) | 4 asks today, all closed, each sent to KF between 07:30 and 08:30 IST, so one more fits `ask_daily_limit_customer` (5) and a sixth does not |
| C53 | +91 90000 00053 | Customer "Neha" (asks seed) | Her last 20 asks, all before today, closed with no Yes; no collected or delivered online order since, so her asks go to at most `ask_low_yield_max_stores` (3) stores |
| C54 | +91 90000 00054 | Customer "Rohit" (asks seed) | Reported by Sri Lakshmi with "Report this ask" 3 days ago: an open `ask_reported` flag (A-13) and an `ask_store_blocks` row, so SL never gets his asks |
| C55 | +91 90000 00055 | Customer "Zoya" (asks seed) | One open ask, "Extension board 4 socket", quantity 1, sent to NH alone at 10:58 IST today, so its window ends at 11:13 |
| U01–U12 | +91 90000 00081–92 | Customers behind the Demand gaps seed | No wallet, no orders. Each asked once in Bengaluru in the last 7 days (`<seed:ask-gaps>`) and got no Yes |

### Stores (Bengaluru unless noted)

Distances are from the **QA point**: PIN 560038, label "Indiranagar, 560038", at 12.9719, 77.6412. Use this point whenever a step says "location QA point".

| Short | Name | State | Distance | Hours (IST) | Fulfilment | Notes |
|---|---|---|---|---|---|---|
| SL | Sri Lakshmi (business Lakshmi Traders) | business verified, store live | 0.6 km | 08:00–22:00 daily | pickup and delivery | cashback 500 bp (5%); delivery radius 3 km, fee ₹30, free delivery on ₹500+, minimum ₹150; auto-accept off; live since 2026-01-10 (primary store); 14 items I1–I14 |
| SL2 | Sri Lakshmi 2 (Lakshmi Traders), HAL 2nd Stage | live | 1.8 km | 08:00–22:00 | pickup only | cashback 0; live since 2026-05-01; 22 items J1–J22 (the insights shop, table below) |
| AP | Annapurna Provisions | live, business verified 10 days ago | 1.2 km | 09:00–21:00 | pickup only | cashback 0; onboarded by F40; 20 items |
| NH | Nandi Hardware | live, verified | 0.9 km | 10:00–20:00 | both off ("In-store only") | 15 items |
| KF | Kaveri Fresh | live, verified | 3.5 km | 07:00–22:00 | delivery only, radius 5 km, fee ₹0, minimum ₹100 | cashback 200 bp; 12 items |
| AM | Auto Mart | live, verified | 4.2 km | 08:00–22:00 | pickup | auto-accept on; 10 items |
| PS | Paused store | `paused` | 1.0 km | | | |
| DS | Draft store | `draft` | 1.1 km | | | |
| PR | New branch of a verified business | `pending_review` | 1.4 km | | | |
| SS | Suspended store | `suspended` | 1.5 km | | | |
| NI | Store of a business in `needs_info` | business `needs_info` | 1.6 km | | | |
| SB | Store of a business in `submitted` | business `submitted` | 1.7 km | | | |
| HY | Hyderabad store | live, verified | about 500 km | 08:00–22:00 | pickup and delivery | second city |
| MM | Marina Mart (business Marina Traders), Chennai | live, verified | far from every Bengaluru shop | 08:00–22:00 | pickup only | owner M29; 2 items; twelve other live verified stores of twelve different businesses in Chennai (`<seed:price-compare>`) |

So at the QA point, between 10:00 and 20:00 IST: radius 1 km shows 2 shops (SL, NH); 2 km shows 4 (SL, NH, AP, SL2); 5 km shows 6 (adds KF, AM); 20 km shows 6. Shops PS, DS, PR, SS, NI, SB are inside 2 km and must never show (D-40).

For the insights scripts the seed also fixes distances and places every seeded search in a named H3 resolution-8 cell (analytics stores searches as cells, D-81). SL and SL2 lie on opposite sides of the QA point, 0.6 km and 1.8 km from it, so about 2.4 km apart (neither is inside the other's 2 km radius, and the midpoint is 1.2 km from each). AP is 1.2 km from the QA point on the SL2 side: within 1 km of SL2 and within 2 km of SL.

| Cell | Where its centre is | To SL | To SL2 | Counts for | Holds |
|---|---|---|---|---|---|
| W | the midpoint of the SL–SL2 line | about 1.2 km | about 1.2 km | SL and SL2 | Horlicks 500 g (12), Bournvita 500 g (9) |
| S2 | on the SL–SL2 line, 0.2 km beyond SL2 | about 2.6 km | about 0.2 km | SL2 only | Idli rice 1 kg (24), Boost 500 g (11) |
| M | Chennai, about 0.5 km from MM | not in Bengaluru | not in Bengaluru | MM only | Horlicks 500 g (14) |

Each search is stored with its cell only. When it builds, the seed computes the distance from every cell's centre to the shops and **fails the build, naming the cell, the shop and the distance, if a bound is broken**: a cell meant for one shop is at most 1.5 km from it and at least 2.5 km from the other (S2); a shared cell is at most 1.5 km from both (W); cell M is at most 1.5 km from MM. So every seeded search is at least 0.5 km inside or outside the 2 km radius, never on it.

### Items at Sri Lakshmi (SL)

| Short | Item | Price | MRP | On hand | Notes |
|---|---|---|---|---|---|
| I1 | Amul Butter 100 g | ₹58 | ₹60 | 10 | barcode `8901262010016`; 56 sold in the last 7 days (8 a day, 1.25 days of stock) |
| I2 | Maggi 2-Minute Noodles 70 g | ₹14 | ₹15 | 50 | barcode `8901058000177` (POS-15) |
| I3 | Nandini Toned Milk 500 ml | ₹24 | ₹24 | 30 | |
| I4 | Wheat atta 5 kg | ₹245 | ₹260 | 6 | |
| I5 | Coca-Cola 750 ml | ₹40 | ₹40 | 12 | |
| I6 | Dolo 650 strip | ₹30 | ₹32 | 1 | last unit |
| I7 | Toor dal 1 kg | ₹160 | ₹170 | 8 | stock last updated 84 h ago (stale; `stock_stale_after_hours` is 72) |
| I8 | Curd 400 g (dahi) | ₹35 | ₹35 | 20 | batch of 10 expiring in 3 days |
| I9 | QA pack ₹99.99 | ₹99.99 | ₹99.99 | 50 | boundary item, 9999 paise |
| I10 | QA pack ₹100.00 | ₹100.00 | ₹100.00 | 50 | boundary item, 10000 paise |
| I11 | QA pack ₹100.01 | ₹100.01 | ₹100.01 | 50 | boundary item, 10001 paise |
| I12 | QA pack ₹0.50 | ₹0.50 | ₹0.50 | 100 | for paise rounding |
| I13 | QA pack ₹120 | ₹120 | ₹120 | 50 | offer "₹21 off" (CR-21) |
| I14 | QA pack ₹487 | ₹487 | ₹487 | 20 | CR-06 cashback example |

### Items at Sri Lakshmi 2 (SL2), the insights shop

Used by [INSIGHT-demand-insights.md](INSIGHT-demand-insights.md). Dates are for the QA clock (2026-10-22 11:00 IST, a Thursday); `seed test` builds them relative to the test clock.

| Short | Item | Price | On hand | Notes |
|---|---|---|---|---|
| J1 | Tata Tea Gold 250 g | ₹145 | 12 | 64 sold in the last 7 days, in 64 bills (9.14 a day), so 1.3 days of stock; in 90 of the 600 bills in the last 60 days |
| J2 | Curd 200 g | ₹20 | 12 | one batch, expires 23 Oct |
| J3 | Buttermilk 200 ml | ₹15 | 10 | one batch, expires 24 Oct |
| J4 | Cheese slices 200 g | ₹120 | 8 | one batch, expires 25 Oct |
| J5 | Fresh cream 200 ml | ₹55 | 6 | one batch, expires 26 Oct (4 days) |
| J6 | Paneer 200 g | ₹90 | 10 | two batches: 6 expired on 21 Oct, 4 expire on 28 Oct (6 days) |
| J7 | Sunflower oil 1 L | ₹140 | 20 | MRP ₹155; two batches: the older costs ₹150, the latest (received later) costs ₹146 |
| J8 | Mango pickle 500 g | ₹65 | 12 | batch cost ₹40 (₹480 in all); last sale 23 days ago |
| J9 | Agarbatti pack | ₹30 | 5 | no cost known; last sale 40 days ago |
| J10 | Papad 200 g | ₹45 | 9 | cost ₹30; last sale 19 days ago |
| J11 | Idli rice 1 kg | ₹70 | 0 | closes at 0 on 20 and 21 Oct (2 whole days; today does not count); 4 sold, one each on 16, 17, 18 and 20 Oct; 24 nearby searches matched it in the last 7 days, 18 on 20 and 21 Oct and 6 on days it was in stock |
| J12–J17 | Ragi flour 500 g (₹45), Poha 500 g (₹38), Jaggery 500 g (₹60), Sabudana 500 g (₹55), Vermicelli 400 g (₹40), Rice flour 1 kg (₹60) | as listed | 10 each | last movement (a sale of 1) 9 days ago, so `stock_updated_at` is 9 days ago |
| J18 | Bread 400 g | ₹45 | 60 | in 84 of the 600 bills in the last 60 days |
| J19 | Eggs tray 12 | ₹84 | 50 | in 70 bills in the last 60 days; 21 bills hold both J18 and J19 (A is Bread, share 25%, lift 2.14) |
| J20 | Nandini Toned Milk 500 ml | ₹24 | 120 | in 480 of the 600 bills in the last 60 days (the "milk with everything" decoy); 56 sold in the last 7 days, in 56 bills (8 a day), so 15.0 days of stock |
| J21 | Parle-G | ₹10 | 80 | in 300 of the 600 bills, 240 of them with J20 (A is milk, share 50%, lift 1.0): must not fire at the default lift; 35 sold in the last 7 days, in 35 bills (5 a day), so 16.0 days of stock |
| J22 | Salt 1 kg | ₹24 | 30 | MRP ₹28; its latest batch cost is ₹240 (a case price entered as a unit price) |

Rules for this shop: every in-stock item except J12–J17 has `stock_updated_at` at the clock time; J1, J20 and J21 are the only items that sold 5 or more units in the last 7 days (64, 56 and 35), and J20 and J21 hold 15.0 and 16.0 days of stock, so J1 is the only Selling fast row at the default `insight_cover_days` (7) and also at 14; every other item is in fewer than 120 of the 600 bills and the bills are otherwise independent, so no pair other than J18 + J19 and J20 + J21 reaches a 20% share, and at `insight_pair_min_lift_pct` 100 only J20 + J21 joins J18 + J19; no price except J7's is below its latest batch cost, and no batch cost reaches its MRP except J22's; for each item with batches the remaining quantities add up to the on hand shown; no other item, pair or customer meets a kind's rule. Every seeded date and count is at least half a day (or about 5 percent) away from the threshold it tests. Annapurna Provisions (AP) lists Boost 500 g with 8 on hand; no store lists Horlicks 500 g or Bournvita 500 g. Fewer than 10 other live verified stores of distinct businesses are near SL or SL2, so "No shop nearby has it." never shows there.

## 7. Seed data the book needs

Steps use these placeholders until the seed provides them. The seed WP should create each one with the values in §6.

| Placeholder | What it must be |
|---|---|
| `<seed:customers>` | C01–C19 as in §6, including wallets, lots, strikes, referral and open-order states |
| `<seed:staff>` | O20, M21, K22, R23, M24, X25, O26, O27, O28, M29 with the roles and store scopes in §6 |
| `<seed:admins>` | F40, F41, S42, G43, Y44, FN45, FN46, AD47, SA48, SA49, D50 pre-enrolled with TOTP (codes in the mocks "TOTP now" panel), except AD47, who is not enrolled |
| `<seed:stores>` | SL, SL2, AP, KF, NH, AM, PS, DS, PR, SS, NI, SB, HY, MM as in §6 |
| `<seed:items-SL>` | I1–I14 at SL with prices, stock, barcodes, the stale timestamp and the expiring batch |
| `<seed:offer-I13-21off>` | ₹21 flat offer on I13 at SL, live |
| `<seed:banner-SL>` | One live banner at SL, headline "Fresh stock every morning", target the I13 offer |
| `<seed:history-SL>` | 30 days of POS sales and 300 historical orders across end states (03 §12), so dashboards, insights and the coach have data |
| `<seed:month-SL>` | Ledger lines for Lakshmi Traders dated in the current IST month: credits spent at SL ₹1,000 and at SL2 ₹170, one `any_shop` referral reward ₹30 to SL, cashback SL −₹290 and SL2 −₹20. Opening balance ₹0. The month closes to ₹890 (MQ-06). KYC verified. |
| `<seed:standee-SL>` | One printed standee at SL with a known `standee_id` |
| `<seed:pending-product>` | One pending master product from SL with a duplicate candidate |
| `<seed:risk-flags>` | One open flag of each rule kind; the `store_strikes` flag is on Kaveri Fresh |
| `<seed:tickets>` | One open customer ticket linked to an order, one business ticket |
| `<seed:wanted-nearby>` | 12 searches for "Horlicks 500 g" and 9 for "Bournvita 500 g" (below k ≥ 10) in the last 7 days, all in cell W (§6: about 1.2 km from SL and from SL2), so both shops count them. SL stocks neither, and no store near SL lists either. Fewer than 10 other live verified stores of distinct businesses are near either shop, so the Wanted nearby row never carries "No shop nearby has it." there |
| `<seed:insights-SL>` | On top of `<seed:history-SL>` (D-99): I1 Amul Butter 100 g sold 56 in the last 7 days (8 a day, 10 on hand, 1.25 days of stock, `high`); I7 Toor dal 1 kg last sold 26 days ago, its latest batch cost ₹128 and its stock was last updated 84 h ago (so Stock check due fires for it alone and customers see "Stock not confirmed recently"); I8 Curd 400 g has one batch of 10 expiring in 3 days. Every other SL item was updated at the clock time. Nothing else at SL meets a kind's rule: every other item sold fewer than 5 units in the last 7 days, no other batch expires within 5 days, no item is at 0 with matching searches, no price is below cost, no pair of items reaches 20 bills together, and no customer with 3 or more purchases in 60 days has been silent for 21 days |
| `<seed:insights-SL2>` | The item table "Items at Sri Lakshmi 2" above, with the bills, searches and customers behind it: H01–H11 (§6, the only phone-linked customers) with their purchases; 600 bills in the 60-day window, including the Bread and Eggs bills and the Milk and Parle-G decoy; 24 searches for Idli rice 1 kg and 11 for Boost 500 g, both in cell S2 (about 0.2 km from SL2 and about 2.6 km from SL, so they count for SL2 only); Horlicks 500 g and Bournvita 500 g as in `<seed:wanted-nearby>`; Boost 500 g listed at AP with 8 on hand. Batches are received and sold in the order of 03 stock rule 8 (a sale takes unexpired dated batches first, so a sale of Paneer takes the 28 Oct batch, not the expired one), so the batch remainders match the on hand in the table. The rollups (`rollup_item_pairs`, search demand) run at the end of the seed; `rollup_item_pairs` stores every pair with at least `insight_pair_min_bills` bills together, so the decoy J20 + J21 (240 bills together, lift 10000 bp) is stored and appears as soon as `insight_pair_min_lift_pct` is lowered to 100. The seed fails if a cell is outside its distance bounds (§6) Every store other than SL, SL2, AP and MM (Auto Mart included) has no sales history and no search within 2 km, and its items are stamped as updated at the clock time |
| `<seed:price-compare>` | Marina Mart (MM, Chennai, owner M29) lists Sugar 1 kg at ₹62 and Jaggery 1 kg at ₹95, fresh stock, no sales history. Twelve other live, verified Chennai stores within 2 km of MM, each of a different business ("Chennai Store 01" to "Chennai Store 12"), list Sugar 1 kg, with a median price of ₹58. Nine list Jaggery 1 kg, with a median of ₹90 (under the 10-store floor, D-71). 14 searches for "Horlicks 500 g" were made in the last 7 days, all in cell M (§6: about 0.5 km from MM) and none of the twelve lists it, so M29 sees "No shop nearby has it." (INSIGHT-M.23). `insight_price_compare_enabled` stays false until a step switches it on (Q-52) |
| `<seed:addresses>` | C11 saved addresses: "Home" 0.6 km from SL and 3.5 km from KF; "Office" 4.0 km from SL and 4.5 km from KF; "Far" 6.0 km from KF |
| `<seed:payout-history>` | Lakshmi Traders: two months ago a payout was marked paid with UTR `QAUTR0000000001`, so this month opens at ₹0. Kaveri Fresh: last month closed at −₹40 (no payout) |
| `<seed:month-others>` | Current IST month. Kaveri Fresh: credits in ₹100, cashback −₹20, so it closes at ₹40 (opening −₹40). Auto Mart: credits in ₹50, cashback −₹120, closes at −₹70. Annapurna Provisions: credits in ₹200, cashback 0, closes at ₹200, and no payout details entered (PAN, UPI empty) |
| `<seed:ai-fixtures>` | Recorded model replies for voice, bill scan, shelf scan, coach, reply drafts and insight wording, served by the mocks server. Insight wording: for the SL "Going to waste" row "Your Curd 400 g expires within 5 days, so a flash offer can save it."; for the SL "Sitting on the shelf" row a sentence that contains ₹999, which is not in the row's facts |
| `<seed:pseudo>` | Pseudo-locale bundle reachable with `?locale=pseudo` (en-XA, D-87) |
| `<seed:asks>` | Used by [ASK-ask-shops-nearby.md](ASK-ask-shops-nearby.md), relative to the QA clock (2026-10-22 11:00 IST). The master product "LED bulb 9 W" (Electricals, MRP ₹120) is listed only at AP: ₹110, 2 on hand, 0 reserved, stock last confirmed 2026-10-17 11:00 (5 days, so stale). NH lists "Extension board 4 socket" (Electricals) and no bulb. SL is the only Bengaluru store that lists Amul Butter 100 g (I1). C51–C55 in the states in §6. NH has received 5 asks today, C55's included; KF has received C52's 4. The speech-to-text mock answers the mic with the recorded transcript "amul butter under 200". |
| `<seed:ask-gaps>` | U01–U12 (§6) each asked once for "Kodo millet 1 kg" in Bengaluru in the last 7 days, and U01–U09 for "Foxtail millet 500 g"; none got a Yes. `rollup_ask_demand` has not run since. No Bengaluru store lists either product. |
| `<seed:site-marquee>` | `search_performed` events with a `product_id`, in Bengaluru, in the last 7 days, by distinct signed-in users: Amul Butter 100 g by 15, Maggi 2-Minute Noodles 70 g by 12, Dolo 650 by 20 (a denylisted category) and Toor dal 1 kg by 9, plus Toor dal 1 kg by 6 anonymous devices, which never count towards the floor (01 §11.3). None in Hyderabad. No list is approved for any city and `site_marquee_build` has not run. |

## 8. Devices

Run in the browser pane with device emulation unless the script says "real phone".

| Short | Emulation |
|---|---|
| P360 | 360×740, Android Chrome user agent, touch |
| P390 | 390×844, iOS Safari user agent, touch |
| D1280 | 1280×800 desktop |
| D1440 | 1440×900 desktop |

Real devices for the cloud-launch pass (08 §5): a Redmi or Realme Android Go-class phone (2 GB RAM), a Samsung A-series mid-range phone, an iPhone SE or 11 on iOS 17+, a Windows laptop with Chrome, a MacBook with Safari, and an Android tablet at the counter. Use a USB barcode scanner that types as a keyboard for MQ-03.

Two-device scripts need two browser profiles or two windows, so the customer and the shop have separate cookies.

## 9. Recording results

1. Create `docs/qa/manual/runs/<YYYY-MM-DD>/`.
2. Copy each script you run into it. Fill the **P/F** cell with one of: `P` pass, `F` fail, `B` blocked (cannot run: say why), `NA` not built yet.
3. Save a screenshot for every step whose Shot cell has ▢, named `<step id>.png` (for example `MQ-02.14.png`), in the same folder.
4. Add a header to each copied script:

   ```
   Tester:            Build sha (GET /version):
   Date (IST):        Seed: make reset at HH:MM
   Device / browser:  Viewport:
   ```

5. For every `F`, add a row to `runs/<date>/bugs.md`: step id, what you saw, what the step expected, screenshot name.
6. Commit the run folder with the results.

**Mask before you save.** Never store an OTP, an order code, a counter code, a TOTP code, a recovery code, a token, a receipt token or a full phone number in a screenshot, a run file or a bug (e2e/CLAUDE.md). Blur or crop them. Masked phones as the app shows them (`98•••••521`) are fine. The mocks UI shows phones and codes in full: crop it before saving.

## 10. Step format

Every script starts with preconditions and ends with cleanup. Each step is one row:

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-xx.nn | What to do | Exact copy, number, state or timer | Rule (R-x.y from [traceability.md](traceability.md)), scenario ID, decision ID, screen ID, problem `code` | ▢ = save a screenshot | |

- Numbers in expected results are today's defaults from 01 §17, with the key named. If a key has been changed for the run, use the run's value.
- "★" after a step ID marks the shortened production smoke (08 §8). Run all ★ steps after each deploy.
- Never write "works" as a result. Write what you saw.
