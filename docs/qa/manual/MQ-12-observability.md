# MQ-12 Observability

Run a pickup order (MQ-02) and a shop day (MQ-03), then check that the activity shows on the Grafana dashboards, that one slow request can be followed from a dashboard to its trace and logs, and that no phone, OTP, code or token appears anywhere in logs or traces.

- **Covers:** 08 §5 MQ-12; OB-01, OB-02, OB-03, OB-04, OB-06 manual side; AU-15 manual side; 05 §2, §4, §5.
- **Screens:** Grafana dashboards 00, 02, 05, 06, 07, 08, 10, 11, 12; Explore (Loki, Tempo); A-02 Pulse.
- **Devices:** D1440.
- **Time:** about 60 minutes after MQ-02 and MQ-03.

## Preconditions

1. MQ-02 and MQ-03 were run within the last hour on this stack, without `make reset` after them. Write down the order numbers from MQ-02 (A-1) and MQ-03, and the IST time of each.
2. Grafana at https://grafana.todayz.localhost, folder "TodayZ". Time range "Last 1 hour", `env = local`.
3. Know C10's phone, K22's phone and the A-1 pickup code from your MQ-02 run notes (not saved in a run file). You will search the logs for them.

## Steps

### A. Dashboards show the activity

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-12.01 ★ | Open 00 "What to fix today". | All 9 panels render without "No data" errors: slowest endpoints by impact, top errors by users affected, slowest SQL, worst client screens, biggest funnel leak, stores hurting customers, zero-result searches nearby, jobs and notifications failing, SLO burn. | 05 §4 dashboard 00 | ▢ | |
| MQ-12.02 | In 00 "Top errors by users affected". | The errors you caused on purpose (wrong OTP, wrong code) are 4xx and are not listed as errors; only 5xx and error log lines are. Record any row that shows. | 05 §4 dashboard 00 | | |
| MQ-12.03 ★ | Open 02 "API endpoints". Pick route `POST /v1/me/orders`. | RPS, p50/p95/p99 and error % show for the time of your orders. The error code breakdown lists any `code` you hit (for example `invalid_transition` from MQ-02). | 05 §4 dashboard 02 | ▢ | |
| MQ-12.04 | Open 05 "Customer app experience". | LCP, INP and CLS by route for `/`, `/search`, `/s/:storeSlug`, `/checkout`, `/orders/:id`. | 05 §4 dashboard 05 OB-06 | ▢ | |
| MQ-12.05 ★ | Open 06 "Business app and POS". | POS sales online vs offline match MQ-03 (10 online, 5 offline). Offline outbox sync lag shows the sync from MQ-03. Order alert latency p95 is under 3 s. | 05 §4 dashboard 06 02 §11 | ▢ | |
| MQ-12.06 | Open 07 "Customer funnel and usage". | Your MQ-02 session shows in the funnel from app opened to collected. Search panel shows your searches ("amul", "maggi"). | 05 §4 dashboard 07 | ▢ | |
| MQ-12.07 ★ | Open 08 "Marketplace health". | Orders placed in the last hour match your runs. The outcome breakdown includes collected and rejected (with the reason from MQ-03) and partial acceptance. | 05 §4 dashboard 08 | ▢ | |
| MQ-12.08 | Open 10 "Retailer health". | Sri Lakshmi shows POS sales for today, the stock-update activity by source (`sale_pos`, `sale_online`, `count_correction` if MQ-03 counted), and the share of linked sales by `bill_qr`. | 05 §4 dashboard 10 | ▢ | |
| MQ-12.09 | Open 11 "Money and payouts" and 12 "Security and abuse". | 11: ledger reconcile status "OK". 12: OTP sends and verifies counts (counts only, no numbers or phones), code lockouts by store if MQ-02 locked a code. | 05 §4 dashboards 11 12 | ▢ | |
| MQ-12.10 | Sign in to the admin console as Y44 (analyst) and open A-02 Pulse. Compare today's orders and GMV with dashboard 08. | Same numbers within rounding. | `R-13.1` A-02 | ▢ | |

### B. From a slow request to its trace

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-12.11 ★ | In 02 pick the route with the highest p95 in the last hour. Click an exemplar dot on its latency graph. | Tempo opens the trace. Record the trace id. | OB-01 05 §2.3 | ▢ | |
| MQ-12.12 | Read the trace. | One root span for the HTTP request, child spans for the handler, SQL queries (named, not raw SQL with values) and any outbound call. The time in DB is visible. | OB-01 05 §2.3 | ▢ | |
| MQ-12.13 | From the trace, open "Logs for this span" (or search Loki for `{app="api"} |= "<trace id>"`). | The log lines of that request carry the same `trace_id`. | OB-01 05 §2.2 | ▢ | |
| MQ-12.14 | In Explore → Prometheus, query the API request-duration histogram (`tz_` prefix, `_seconds` unit, 05 §2.1) filtered to that route. | The count moved at the same time. Metric, log and trace agree on one request. Record the metric name. | OB-01 05 §2.1 | | |
| MQ-12.15 | Force a slow route and a slow query the way the OB-03 and OB-04 tests do. Use `GET /v1/test/slow?ms=` and `GET /v1/test/slow-sql?ms=` (04 §2.9). Call it 20 times, wait 2 minutes, open 00. | The route ranks first in "Slowest endpoints by impact"; "Slowest SQL" shows the slow statement by its query name. | OB-03 OB-04 | ▢ | |

### C. No PII in logs or traces

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-12.16 ★ | In Explore → Loki, query `{env="local"} |= "9000000010"` over the last hour, then `|= "90000 00010"` and `|= "+919000000010"`. | No line contains C10's phone in any form. The mocks server's own captured SMS is not in Loki. | OB-02 AU-15 05 §2.2 | ▢ | |
| MQ-12.17 | Search Loki for `000000` near your sign-in times, and for your A-1 pickup code. | No OTP and no order code in any log line. | OB-02 AU-15 | | |
| MQ-12.18 | Search Loki for `Bearer`, `__Secure-tz_rt`, `authorization` and `cookie`. | No token or cookie value. A key name with a masked value (for example `"authorization":"[redacted]"`) is fine. | OB-02 05 §2.2 | | |
| MQ-12.19 | Search Loki for the C11 "Home" address text and for the PAN typed in MQ-05 if run on this stack. | Not found. | OB-02 05 §2.2 | | |
| MQ-12.20 ★ | In Tempo search traces by attribute: any attribute value containing `9000000010`, the pickup code, or `Bearer`. Open three traces of `POST /v1/auth/otp/verify` and read every span attribute. | No phone, OTP, code or token in span names, attributes or events. | OB-02 AU-15 05 §2.3 | ▢ | |
| MQ-12.21 | Run `make env-check`. | Prints `NAME set` or `NAME missing` only, for every key. No value. | `R-17.1` PL-03 | | |
| MQ-12.22 | Search Loki and `make logs` output for each secret key name followed by `=` (for example `PII_ENC_KEY=`, `JWT_SIGNING_KEYS=`). Do not open `.env.local`. | No line shows a key name with a value. Lines that name a key with `set` or `missing` are fine. | `R-17.1` PL-03 | | |

### D. Alerts

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-12.23 | Open Grafana → Alerting → Alert rules, folder TodayZ. | The rules from 05 §5 exist: API availability (fast and slow burn), API latency, Order timers late, Job backlog, Ledger invariant, DB connections, Order alerts slow, SSE down, SMS failures, OTP abuse, Refresh token reuse, Store not accepting, Zero-result spike, Client errors, RUM regression, POS sync lag, AI cost, Store strikes, Unconfirmed returns. Cloud-only rules may be absent locally. | 05 §5 | ▢ | |
| MQ-12.24 | Open each rule's runbook link for three of them. | The link opens `docs/runbooks/<alert>.md` (local file or repo view) with what it means and what to do. | 05 §5 | | |
| MQ-12.25 | Stop the mocks server (`infra/local/proc.sh stop mocks`) for 12 minutes while signing in with mock-route numbers (+91 90000 00101 and on) every 30 s. Start it again. | "SMS failures" goes to firing in the local contact point after 10 minutes over 5%. It resolves after the mocks are back. | 05 §5 | ▢ | |

## Cleanup

1. `make dev` (brings the mocks back if still stopped).
2. Nothing else to reset.
