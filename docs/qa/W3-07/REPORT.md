# W3-07 Analytics rollups and dashboards: QA report

The rollups, Pulse API, Grafana dashboards 00 and 05–13, alert rules with runbooks, and the `make demo` traffic generator. Branch `claude/w3-07-rollups-15028a`, cut from `main` 3f16f74; not rebased onto the newer `main`. Everything ran locally (testcontainers PostGIS); nothing touched a cloud account, and no stack was started (`make dev` is the lead's).

## What was built

- **Rollup tables (migration 30701):**
  - `rollup_daily_platform`, `rollup_daily_store`, `rollup_daily_search_demand` (with `product_id`, D-99), `rollup_cohort_weekly` and `rollup_item_pairs` (`bills_total`, `lift_bp`);
  - the views `v_metric_orders`, `v_metric_transactions`, `v_metric_pos_sales`, `v_metric_searches`, `v_metric_app_opens`, `v_metric_store_activity` and `v_metric_store_supply`, which the rollups and the dashboards both read, so one definition gives each number;
  - the banner rollup writes W3's existing `banner_daily`.
- **Service (`internal/rollups`):**
  - Each day is recomputed from the raw tables per IST day, so a rerun writes the same rows and a late offline POS sale is picked up.
  - Today's row is marked not final. `computed_at` comes from the injected clock.
- **Job kinds (`internal/jobs/rollups.go`):** six kinds, each registered with `jobs.Define`, class `rollup`, owner W3-07, so they show on A-21 and dashboard 15:
  - `rollup today` every 5 min;
  - `rollup day` at 00:30;
  - `demand` hourly;
  - `banners` every 5 min;
  - `cohorts` at 01:15;
  - `item pairs` at 02:00.
- **Backfill:** `todayz rollup --from D [--to D]` (at most 400 days per call), on the test clock locally.
- **Pulse API (`admin-pulse`):**
  - `GET /v1/admin/pulse?range=` returns KPIs with the previous period, the trend, stores needing attention and zero-result searches.
  - `GET /v1/admin/pulse/map` returns stores and H3 demand cells.
  - Both need `admin.pulse.view`, which every admin role except `field_agent` has.
  - k = 10 applies to zero-result queries and demand cells.
  - The city filter was dropped, because searches carry only an H3 cell (631ca8d).
- **Dashboards:** files `00-what-to-fix-today`, `05-customer-rum`, `06-business-pos`, `07-customer-funnel`, `08-marketplace`, `09-growth-retention`, `10-retailer-health`, `11-money-payouts`, `12-security-abuse` and `13-cost`.
  - All are IST, read-only, with "How to read this" first.
  - They show counts only: no person, phone, address or code, and no id as a metric label.
- **Alerts:** 24 rules in 11 files (`api`, `auth`, `client`, `database`, `ledger`, `marketplace`, `pos-sync`, `queue`, `realtime`, `risk`, `search`) for 05 §5.
  - Each has a runbook in `docs/runbooks/` (20 new), with these sections: what it means, first three checks, likely causes, fix, who to tell.
  - `jobs.yaml`, `orders.yaml` and `pos.yaml` belong to other WPs and were not touched.
- **`make demo`:** `todayz rollup demo`, registered from `mk/27-rollups.mk` (`DEMO_STEPS += demo-rollups`, `DEMO_FOR=10m` to stop).
  - It signs in `customer.ravi` and the seeded owner through the mocks' OTP.
  - Every 10 s it runs 1–3 searches, sends a RUM batch and app events, and books a pickup order at SL (Maggi or a QA pack).
  - The owner accepts the order, marks it ready and enters the customer's code. 15 % of orders are cancelled by the customer instead.
  - It reaches loopback only and refuses any URL that is not loopback or `*.todayz.localhost`, and any `APP_ENV` other than local or test.
  - It prints counts and problem codes, never a body, a phone, an OTP, a code or a token.
- **Scenarios:** `e2e/scenarios/OB.md`, OB-11…OB-30, each mapped to its test.

## Evidence

| Check | Result |
|---|---|
| `go test -race` `internal/rollups`, `internal/jobs`, `internal/platform/authz/policy` | ok (rollups 12 s) |
| `go test -race ./cmd/todayz -run 'Rollup\|Pulse\|TestRoutesAreInTheContract\|TestOneMountPerDomain'` | ok |
| OB-11 `TestRollupsMatchRawCountsForASeededDay` | every platform, store and demand count equals the raw count and the fixture's own number |
| `TestDashboardsLint` / `TestDashboardSQLRuns` | 10 dashboards linted; every Postgres query and variable (over 100) runs read-only against the fixture |
| `TestAlertRulesLint` | 24 rules: unique uids, labels, runbooks, dashboard uids, thresholds, provisioned datasources, every `tz_` metric registered in Go |
| `TestAlertRulesFire` | the 9 Postgres-only rules stay quiet on clean data and fire on their fixtures; the mixed SMS rule returns rows |
| `TestRollupDemoTraffic` | against the real router: 4 ticks, 3 collected, 1 cancelled, RUM, events and searches stored, one token renewal, no phone, OTP, code or token in output or API logs |
| golangci-lint (`./cmd/todayz/`, `./internal/rollups/`) | 0 issues |
| settings lint | clean |
| `make check` (36a2b19) | check-repo, check-stack ok. check-backend: gofmt, vet, golangci, the grep and `go test -race ./...` pass for every package except `internal/admin` (see Questions 4); `cmd/todayz` 198 s, `internal/rollups` 18 s, `internal/jobs` 24 s. gosec 0 issues, govulncheck no called vulnerabilities (run against the main checkout's `.cache/govulndb`: the worktree has none), settings lint clean |
| remaining steps, each run by hand under `scripts/no-egress.sh` (make stops at the first failing step) | check-contract (TestContractSmoke), check-journeys, check-ui, check-mocks, check-i18n-messages, check-ci, check-auth, check-frontend, check-realtime: ok. check-infra: its own tests and `infra checks (all): ok`, then `terraform validate infra/cloudflare` refused, and check-tf-static likewise, because this worktree has no `terraform init` (setup gap, not code) |

## Open items (TODO(lead))

**Alerts approximated until a metric exists:**
- DB pool alert: uses mean wait rather than p95, because there is no histogram.
- OTP "one address": counts per `ip_hash`, labelled by a 12-hex prefix. Grouping by /24 needs a prefix hash.
- SMS failure rate: a Loki log line ÷ `otp_requests`. Identity sends SMS directly and has no metric.
- Client "error sessions": read as errors per page view (LCP samples), with a floor of 100.
- SSE business hours: fixed at 04–16 UTC. Per-store hours are still to do.
- POS outbox age: approximated by devices that synced in the last 24 h but not the last hour, IST 10–20.
- Search zero-result: platform-wide, since searches carry no city.
- `tz_idempotency_complete_failures`: matched by regex, in case the OTLP export adds `_total`.

**Deferred to W5:**
- DB CPU and storage, certificate and uptime alerts: cloud only.
- Contact points and routing.
- Wiring the alert files into the local Grafana.

**Panels with no data source yet:**
- Statements, payouts, approvals, Turnstile, WAF, Cost Explorer, Sentry.
- Funnel events, checkout quote failures, SW adoption, insight events.
- The Asks panels on dashboard 07: a TODO text panel until W3-16 lands `asks.yaml`.

**Other:**
- The backfill command writes no `job_runs` rows.
- The `needs_attention` thresholds on dashboard 10 need confirmation.
- `make demo` leaves out store rejects and accept-timer expiries. They put strikes on SL (01 §8), which would pause it for the business app and the journeys.
- `make demo` uses up SL's QA-pack and Maggi stock and changes Ravi's wallet. `make reset` restores both.

## Questions

1. **Possible W1-11 bug:** dashboard 15's lateness panel filters `tz_job_lateness_seconds{class="timer"}`, but that metric carries only `kind`. W3-07's queue alert joins on `tz_job_runs_total` instead.
2. Should `make demo` also exercise store rejects and expiries on a dedicated demo store, so the attention and strike panels move? A new seed store is outside W3-07's paths.
3. **Dashboard 10 thresholds:** do the `needs_attention` thresholds (orders expired unaccepted, stale stock, rating) match what ops wants?
4. **Not W3-07, base branch:** `internal/admin` `TestSettingGroupCoversEveryKey` fails on `customer_max_saved_addresses` (W2-15's 21501, D-222), which has no group in W3-05's settings groups. One line in W3-05's group map fixes it; it may already be fixed on `main`.

## Not verified

- No stack run. `make demo` against the real stack, dashboards with live data (OB-28) and Grafana evaluating the alert rules (OB-29) all need the lead's `make dev`. The generator is tested only in-process against the real router.
- PromQL and LogQL get static checks only (balanced brackets, registered metric names); there is no promtool or Loki parser offline.
- Dashboard rendering and layout in Grafana: no screenshots, because no Grafana ran.

## Commits

99406b9 contract, 16c57fe policy, 631ca8d contract without city, aa2bfdd tables, views and queries, d711202 service, jobs, Pulse handlers, backfill, 9f39c31 dashboards, 5d11c11 alerts and runbooks, fc12b3c `make demo`, b479088 scenarios, 0f9c846 demo `Do` interface (the http.Client grep), 36a2b19 `#nosec` annotations (gosec), then this report. The Pulse contract (99406b9 + 631ca8d) was sent to the lead for a cherry-pick to `main` for W3-05.

## Files changed outside the owned paths

- `backend/cmd/todayz/main.go`: the `case "rollup"` dispatch and two usage lines.
- `backend/cmd/todayz/rollup.go`, `rollup_test.go`, `rollup_demo.go`, `rollup_demo_test.go`: the backfill and demo subcommands, under the `rollup` command.
- `mk/27-rollups.mk`: registers `DEMO_STEPS += demo-rollups`.
- `e2e/scenarios/OB.md`: new, OB-11…OB-30.
