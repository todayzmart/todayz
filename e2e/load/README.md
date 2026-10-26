# e2e/load (W4-02)

Load and performance scenarios for 08-testing.md §6, run against the local stack only.

```
make reset load-data     # once per fresh database (about 3 min)
make load                # every scenario; or one: make load-browse | load-orders | load-pos | load-otp | load-sse | load-soak
```

| Path | What |
|---|---|
| `run.sh` | Runs one scenario. Refuses an API or mocks origin that is not loopback or `*.todayz.localhost`. |
| `data/generate.sql` | 1,000 shops (Bengaluru 400, Chennai 350, Hyderabad 250) with about 210 stocked items each on average (20 to about 300), 50,000 customers, 70k collected orders over 14 days and 350k POS sales over 7 days (`-v history=0` skips the history). Refuses unless `platform_environment.env` is `local` or `test`; a rerun is a no-op. Runs as the owner (`DATABASE_MIGRATE_URL`) because it uses temp tables. |
| `scenarios/bootstrap.js` | Signs in the load shop people through the API (mock SMS) and names them, so `data/link_staff.sql` can make them owners and managers of the load shops. Phone identities are keyed and sealed, so SQL cannot create them. |
| `data/export_fixture.sql` | Writes `out/fixture.json`: shop ids, coordinates, in-stock items, cities, search terms. No phones or codes. |
| `scenarios/*.js` | k6: browse_peak, order_burst, pos_rush, otp_abuse, soak. Each writes `out/<scenario>-summary.json` (p50/p95/p99, error rates, threshold verdicts). |
| `ssefanout/` | Go driver for sse_fanout (k6 cannot hold SSE streams): signs in, opens the streams, publishes events through the outbox and `pg_notify`, measures delivery and the api's RSS. |
| `checks/*.sql` | Post-run verdicts that need the database: accept-timer lateness and the ledger reconcile (order_burst), duplicate sales (pos_rush). |
| `sample.sh` | CSV of RSS, Go heap, goroutines, pool and Postgres connections every 30 s, run alongside the soak. |

Sign-ins use the mocks' latest OTP and a fake `CF-Connecting-IP` per sign-in, which the api
believes only from a loopback peer outside a deployed environment, so the suite cannot run
against a deployed api. Nothing here prints or stores an OTP, code, token or phone.

Local limits: one stack per machine; sse_fanout's default is 2,000 shop streams and 3,000
customer streams (`-store-streams`, `-customers`, `-per-customer`).
