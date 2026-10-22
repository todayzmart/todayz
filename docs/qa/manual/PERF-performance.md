# PERF Performance by hand

What a person can feel and measure: POS scan speed, screen speed on a slow phone, the new-order alert, timer lateness, and the dashboards that show it. The numbers come from 02 §11 (performance budgets), 05 and 08 §6. k6 (`make load`) proves throughput; this file proves the experience and that the dashboards tell the truth.

- **Covers:** POS-15; OB-01, OB-03, OB-04, OB-05, OB-06, OB-11; SC-OB; BK-01 (alert time); 02 §11 budgets.
- **Screens:** C-03, C-05, C-07; B-03, B-04, B-05; A-02, A-07.
- **Devices:** P360 real mid Android (about ₹10k) for M.01 and M.06 if available; otherwise Chrome DevTools "Slow 4G" with 4× CPU slowdown. D1280. Grafana at https://grafana.todayz.localhost.
- **Time:** about 2 hours (k6 runs in M.12 add about 30 minutes).

## Preconditions

1. `make reset`. `make demo` running in a second terminal for live traffic (stop it for M.01 to M.03).
2. Grafana open on dashboards 00 What to fix today, 02 API endpoints, 04 Jobs, 05 Customer app experience, 06 Business app and POS experience.
3. Record each measured number in the run notes. A step fails if the number is outside the budget on 3 tries out of 5.

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERF-M.01 ★ | As K22 on B-05 (the phone, or D1280 with 4× CPU), use a USB or Bluetooth scanner to scan I1 (8901262010016) 10 times. Record a DevTools Performance trace for 3 scans. | The line appears or its quantity steps up in under 100 ms from the Enter key on every scan (trace: keydown to paint). | POS-15 02 §11 | ▢ | |
| PERF-M.02 | Scan a barcode that is not in the store (`8901058000177` if not seeded at SL). | The "Add product" sheet opens in under 100 ms. | POS-15 | | |
| PERF-M.03 | Sell 30 lines in one bill quickly (scan, scan, scan). | No dropped scans; quantity totals match the scans; no visible jank (no long task over 200 ms in the trace). | POS-15 SC-BU | | |
| PERF-M.04 ★ | C10 places a pickup order at SL while O20 has B-04 open on D1280. Use a stopwatch, or read `order_alert_latency_seconds` on dashboard 04. Repeat 5 times. | The new-order alert (sound and card) shows within 3 s each time; p95 on the dashboard under 3 s. | BK-01 02 §11 | ▢ | |
| PERF-M.05 | Customer app on P360 (Slow 4G, 4× CPU), cold load of C-03 Home, then C-05 Search "atta", then C-07 a shop page. Run Lighthouse mobile on C-03 and C-07. | LCP under 2.5 s; CLS under 0.1; no layout jump when the map or images load. Record the Lighthouse scores. | 02 §11 SC-CU | ▢ | |
| PERF-M.06 | On C-03 change the radius chip from 2 km to 5 km. | The list and map update without a reload; on Slow 4G a skeleton shows, never a spinner for more than 300 ms (06 G1). Record the time on both connections (06 sets no number). | `R-11.1` C-03 | | |
| PERF-M.07 | Business app on P360 (Slow 4G, 4× CPU): open B-03 Overview, B-04 Orders and B-05 Sell. | Each screen is usable (data shown, buttons respond) within 3 s; INP under 200 ms on button taps (DevTools Performance → Interactions). | 02 §11 SC-BU | | |
| PERF-M.08 | DevTools → Network, first load of the customer app and the business app; filter JS, read the transferred size of the initial bundle. | Customer initial JS under 170 KB gzip; business under 250 KB gzip. The map and the scanner load only when opened. | 02 §11 | | |
| PERF-M.09 | Admin: A-07 Orders with no filter, then filter by store and date; A-06 search by phone; A-19 audit filter by actor. | Each list returns within 1 s with seeded data (record the times; 06 gives no admin budget, so record against the API read p95 200 ms on dashboard 02). | 02 §11 A-07 | | |
| PERF-M.10 ★ | Forced slow route: use the slow-route test hook (Q-37) or `make demo` with its slow flag on one search route for 5 minutes. | Dashboard 00 "Slowest endpoints by impact" ranks that route first; the impact score uses the 200 ms read budget. A trace link opens the slow request. | OB-04 | ▢ | |
| PERF-M.11 | Forced slow query: run a `pg_sleep(2)` inside one request through the documented hook (or a psql session with `SELECT pg_sleep(2)` under the app role while the API runs). | It shows on dashboard 00 "Slowest SQL" within the scrape interval. | OB-03 | | |
| PERF-M.12 | Run `make load` (k6) on the local stack and read its summary. | `browse_peak` reads p95 under 200 ms (search under 300 ms); `order_burst` writes p95 under 400 ms and timers late under 10 s p99; `pos_rush` no duplicate sales; `otp_abuse` blocked after limits. Record each. 02 §11 names the search run `search_peak`, 08 §6 `browse_peak` (Q-48). | 08 §6 OB | | |
| PERF-M.13 ★ | Timer lateness: stop the worker (`infra/local/proc.sh stop worker`) with 3 placed orders due to expire in 1 minute. Wait 3 minutes. Start it. | The 3 orders expire as soon as the worker starts (late, not lost). Dashboard 04 timer lateness shows the gap; the "Order timers late" alert (p99 over 30 s for 5 min) fires if the gap lasted 5 minutes. | OB-05 | ▢ | |
| PERF-M.14 | One request end to end: place an order, copy its `trace_id` from the response header. Find it in Grafana Tempo, Loki and the metric exemplar. | Same trace id in all three. No phone, OTP or code in the log line or span attributes. | OB-01 OB-02 | ▢ | |
| PERF-M.15 | RUM: run C-03 on Slow 4G for 2 minutes. Open dashboard 05. | The session shows within 2 minutes with LCP, INP, CLS by route. Connection type shows the slow bucket. | OB-06 | | |
| PERF-M.16 | Rollups: with the seeded day `<seed:history-SL>`, compare A-02 Pulse orders and GMV for that day with a psql count on `orders` for the same IST day. | Equal. Retailer B-03 for SL equals the per-store count. | OB-11 | | |

## Cleanup

1. Turn off any slow hook. Start the worker if stopped. Stop `make demo`.
2. Reset DevTools throttling. `make reset`.
