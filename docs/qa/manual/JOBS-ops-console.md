# JOBS Jobs, System and graceful shutdown

A-21 Jobs and A-22 System opened and used locally as every admin role, then the stack stopped on purpose in the middle of a request and a job, to prove nothing is cut off. A-21 shows every background job (D-95), every run and what it did. A-22 shows database and queue health, and says "Not configured (local)" for the AWS panels (D-97). The shutdown steps check D-98: no deploy, scale-in or restart cuts off an API request or a running job.

All jobs run under the test clock locally (README §3). After every `clock` call, refresh the screen being checked; due jobs run at once.

- **Covers:** SC-JB-01 to SC-JB-15 (manual side), SC-OPS-01 to SC-OPS-09, SC-GS-01, SC-GS-02, SC-GS-04 to SC-GS-09; OB-05; AD-01, AD-02; D-95, D-97, D-98; 01 §13 (background jobs), 05 §4 dashboard 15, §5, §8.
- **Screens:** A-21, A-22, A-19, A-17; Grafana dashboard 15, Alerting, Explore; C-11, C-12, B-05.
- **Devices:** D1440 (profile D, AD47); a second window (profile E) for the other roles; P360 (profile A) and D1280 (profile B) for section G.
- **Time:** about 2 hours 45 minutes.

## Preconditions

1. `make reset`, then `make dev`, then `make demo` (it runs jobs, so A-21 has data). Let it run for 10 minutes, or `clock +10m` a few times.
2. Grafana at https://grafana.todayz.localhost, folder "TodayZ", `env = local`. Dashboard 15 "Jobs" and the alert rules "Job failing", "Cron missed" and "Job hard-cancelled at shutdown" exist.
3. Admins: AD47 (admin), SA48 (super_admin, TOTP from the mocks "TOTP now" panel), S42 (support), Y44 (analyst), FN45 (finance), G43 (catalogue), F41 (field agent). Customer C10, shop cashier K22 at Sri Lakshmi (SL).
4. Defaults in force: `job_runs_retention_days` 90.
5. Three kinds are used by name: `rollup_today` (class `rollup`, every 5 minutes), `ledger_reconcile` (class `money`) and any order timer kind (class `timer`). For the failure section you also need one scheduled `notify` or `ai` kind that calls a mock; call it **K-mock** and write its name in the run notes. If A-21 has none, mark JOBS-M.14 to M.17 **B** and add a line to "Questions for the lead" in [traceability.md](traceability.md).
6. Never save a phone number, token or code from a log or an error in a screenshot or run file. Blur or crop it.
7. Section G needs production-style builds of the customer and business apps, not the Vite dev server (it hot-reloads on every file change and has no service worker). Stop the dev processes (`infra/local/proc.sh stop customer`, `infra/local/proc.sh stop business`), run `pnpm build` in `customer/` and in `business/`, then serve each with `pnpm preview` on its fixed port (5301 customer, 5302 business) so the Caddy hosts keep working. To ship a "new version", change any visible string, run `pnpm build` again and let `preview` serve the new files; the old hashed chunks are then gone.
8. Section H needs two throwaway lines in `.env.local` (see JOBS-M.34). They hold the placeholder text `qa-not-a-key`, never a real AWS key. Remove them in Cleanup.

## Steps

### A. Reading A-21

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.01 ★ | As AD47 open A-21 Jobs. | One row per registered kind with name, class, owner (a WP id), schedule as readable IST text (for example "Every 5 minutes", "Every day at 00:30 IST"), next run, last run status and time, 7-day success rate, average and maximum duration, and paused state. `rollup_today` shows a recent `succeeded` run. Filters for class and status exist. | SC-JB-02 SC-OPS-06 `R-13.4` A-21 | ▢ | |
| JOBS-M.02 | Filter by class `money`, then by status `failed`, then clear both. | Only `money` kinds show (at least `ledger_reconcile`). The `failed` filter shows kinds whose last run failed, or an empty state. Clearing restores the full list. | SC-OPS-06 A-21 | | |
| JOBS-M.03 ★ | Open `rollup_today`. Note the next run time and the last run in the history. `clock +5m`. Refresh. | A new row in the history: trigger `schedule`, status `succeeded`, a period key, started and finished times, duration, lateness under a few seconds, and summary counts (for example rows written). The list's last run time moved. | SC-JB-02 D-95 A-21 | ▢ | |
| JOBS-M.04 | Open that run. | Summary counts, note, attempts (1), a timeline, "Triggered by: schedule", no error. A "Logs" link and a "Trace" link. | SC-JB-02 A-21 | ▢ | |
| JOBS-M.05 | Click "Logs", then go back and click "Trace". | Grafana Explore opens in Loki with the run's `run_id` and shows its start and finish lines with `kind`, `run_id` and `trace_id`. The trace link opens the same run in Tempo. No phone, token or code in the lines. | SC-JB-02 SC-OB D-95 | ▢ | |
| JOBS-M.06 | Find a `timer` kind (for example the order accept-by expiry). As C10 place an order at SL and leave it unaccepted. `clock +11m` (past the 10-minute accept-by time, `order_accept_timeout_min`; the D-80 test clock). Reopen the timer kind's page. | The order has expired and the timer kind's row shows the hourly counts and success rate rising by one, but the history holds no row for the successful run: event-kind successes are counted, not logged (the table stays small). | SC-JB-03 D-95 A-21 | | |

### B. Actions: run now, pause, resume, retry

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.07 ★ | On `rollup_today` click "Run now". Leave the reason empty and submit. | Refused with a field error on the reason. No run starts. | SC-JB-06 `validation_failed` A-21 | | |
| JOBS-M.08 ★ | Run now with the reason "QA JOBS-M.08". Open A-19 Audit log, filter by actor AD47. | A new run with trigger `manual` and "Triggered by: AD47". An audit row for the run with the reason and the kind. | SC-JB-06 D-95 `R-13.2` `R-13.4` A-21 A-19 | ▢ | |
| JOBS-M.09 | Run `rollup_today` twice in a row with the same `Idempotency-Key`: copy the "Run now" request as cURL (README §5) and send it twice. Record the status and the run id of each. | Both calls return the same run. The history shows one new run, not two. | SC-JB-06 SC-JB-11 | | |
| JOBS-M.10 | Pick the kind with the largest "maximum duration" in the list. Send its run-now request twice within a second (cURL, two different `Idempotency-Key` values). | If the first run is still going: the second returns `409` with `job_already_running` and the UI says a run is already in progress. If the first finished too fast, record **NA** and the kind's maximum duration. | SC-JB-11 `job_already_running` A-21 | | |
| JOBS-M.11 ★ | Open the timer kind. Pick a run id from its history on A-21 (failed or retried attempts are logged; if it has none, take the id of any timer-class row in `job_runs` from the Postgres datasource in Grafana Explore, and record **NA** for the retry call if there is no row at all). As SA48 call run and pause for the kind, and retry for that run id, with cURL. | No Run now, Pause or Retry buttons. The note reads "Order timers can't be paused or run by hand". Each of the three calls returns `409 job_not_runnable` and nothing changes. | SC-JB-07 `job_not_runnable` D-95 A-21 | ▢ | |
| JOBS-M.12 ★ | On `rollup_today` click Pause with the reason "QA JOBS-M.12". `clock +10m`. Refresh. Click Run now. | The row shows "Paused" and the reason. No new scheduled run after the 10 minutes. Run now is refused (`409 job_paused`). The audit log has the pause row. | SC-JB-09 `job_paused` D-95 A-21 A-19 | ▢ | |
| JOBS-M.13 | Click Resume with a reason. `clock +5m`. | "Paused" disappears. The next tick runs and succeeds. The audit log has the resume row. | SC-JB-09 A-21 A-19 | | |

### C. Failures, retry and a missed schedule

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.14 ★ | Stop the mocks: `infra/local/proc.sh stop mocks`. `clock +` the interval of **K-mock**, three times, refreshing after each. | Three `failed` runs in its history, each with an error text and attempts. The error holds no phone number, token or code (scrubbed, at most 2 kB). After the third run the list shows a "Failing" badge. | SC-JB-04 `R-13.4` D-95 A-21 | ▢ | |
| JOBS-M.15 ★ | In Grafana open Alerting → Alert rules → "Job failing". Wait up to 2 minutes. Open dashboard 15 "Jobs". | The alert is firing for K-mock (warning, or critical if its class is `timer` or `money`) and links to its runbook. Dashboard 15 shows the failed runs, the failure rate and the kind in its failing table. | OB-05 SC-JB-04 05 §5 | ▢ | |
| JOBS-M.16 | Start the mocks again: `make dev` (or `infra/local/proc.sh start mocks`). On the last failed run click "Retry" with a reason. | A new run with trigger `retry` and the same period key, status `succeeded`. The "Failing" badge clears at the next list refresh. The audit log names the original run. Retry on a `succeeded` run is not offered (`409 invalid_transition` by cURL). | SC-JB-10 D-95 A-21 A-19 | | |
| JOBS-M.17 | Check the alert again after 2 minutes. | "Job failing" is back to normal. | OB-05 05 §5 | | |
| JOBS-M.18 | `rollup_today` also runs when the worker starts, so first confirm on A-21 that it has a recent `succeeded` run. Stop the worker: `infra/local/proc.sh stop worker`. `clock +15m`. Start everything again with `make dev` (it starts only what is stopped). Wait 6 minutes of real time. Refresh A-21. | The watchdog (runs every 5 minutes) flags `rollup_today`: a "Missed" badge and one run row with status `missed` for the period (not one per tick). Grafana "Cron missed" fires (warning for this class). After the next succeeded run the badge clears. If the start-up run or River's catch-up clears the miss before the watchdog ticks and no `missed` row appears, record what you saw. | SC-JB-05 D-95 05 §5 | ▢ | |

### D. Roles on A-21 and the money class

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.19 ★ | In profile E sign in as AD47. Open `ledger_reconcile` (class `money`). Try Run now and Pause (cURL too). | The buttons are not offered. The API refuses with `403` (money jobs are `super_admin` only). | SC-JB-08 `R-13.2` `R-13.4` D-95 A-21 | ▢ | |
| JOBS-M.20 ★ | Sign in as SA48. Click Run now on `ledger_reconcile` with a reason, without step-up (cURL). Then through the UI. | Without step-up: `403 step_up_required`. The UI asks for the admin TOTP, then runs it with the reason. A manual run appears; an audit row names SA48. Pause then Resume it the same way (TOTP not asked again inside 10 minutes). | SC-JB-08 `step_up_required` `R-2.5` A-21 A-19 | ▢ | |
| JOBS-M.21 ★ | Sign in as S42, then Y44, then FN45, then G43, then F41, one after another. For each, write down whether A-21 and A-22 are in the sidebar and what A-21 shows. | S42 and Y44: A-21 with every kind, read-only (no Run now, Pause, Resume or Retry), A-22 hidden. FN45: A-21 shows only `money` kinds, read-only, A-22 hidden. G43 and F41: neither screen in the sidebar; opening `/jobs` or `/system` by URL shows the no-access state, and the API returns 403. | SC-OPS-03 SC-OPS-07 AD-02 D-64 A-21 A-22 | ▢ | |

### E. A-22 System

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.22 ★ | As AD47 open A-22 System. | The Beanstalk, ALB and CloudWatch alarm panels read "Not configured (local)". The RDS panel shows "Multi-AZ: off" as a plain fact (no warning colour) and the top SQL statements. The River panel shows queue depth and the age of the oldest job. The page shows how old its data is. No error toast, no blank panel. | SC-OPS-01 D-96 D-97 A-22 | ▢ | |
| JOBS-M.23 | In a terminal call `GET https://api.todayz.localhost/v1/test/slow-sql?ms=500` five times. Wait 61 seconds. Reload A-22. | The top-10 SQL list holds the slow statement with its call count and mean time of about 500 ms. The text has placeholders, never literal values. | SC-OPS-02 D-97 A-22 | ▢ | |
| JOBS-M.24 | Note the "data age" on A-22. Reload twice within 60 seconds. Reload after 61 seconds. | The age keeps growing and the figures do not change inside 60 seconds, then refresh. | SC-OPS-04 D-97 A-22 | | |
| JOBS-M.25 | On A-22 click each Grafana link. | Each opens the right dashboard in the "TodayZ" folder. | D-97 A-22 | | |

### F. Stopping the stack without cutting anything off

The API listens on 127.0.0.1:8180 and the worker's health port is 8181 (loopback). Use the direct port so Caddy cannot hide what the API does. `make stop` runs `proc.sh stop-all`: it sends SIGTERM to `api` and `worker` first, waits for the drain (at most 180 s for the worker) before SIGKILL, and only then stops the rest, mocks last, so a draining job can still reach its mock (D-98). Run each step in two terminals: **T1** for the long call, **T2** for `make stop`.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.26 ★ | T1: `curl -s -o /dev/null -w "%{http_code} %{time_total}\n" "http://127.0.0.1:8180/v1/test/slow?ms=20000"`. Two seconds later, in T2: `make stop`. | T1 prints `200` after about 20 seconds: the request was not cut off. `make stop` did not kill the API before it answered; it waits and then stops the rest. Start it again with `make dev`. | SC-GS-01 D-98 | | |
| JOBS-M.27 ★ | Repeat M.26. In T3, immediately after `make stop` starts, run in a fast loop (every 100 ms): `curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8180/readyz` and the same for `/healthz`. | `/readyz` answers `503` and `/healthz` answers `200` only during the 1 s pre-stop (`SHUTDOWN_PRESTOP` locally). Then both refuse connections (`curl` prints `000`), while T1 still completes with `200`. | SC-GS-02 D-98 04 §2.9 | | |
| JOBS-M.28 ★ | As AD47 on A-21 run now the kind with the largest maximum duration (reason "QA JOBS-M.28"). While it is running, run `make stop`. Then `make dev` and open the run. | `make stop` stops the API and the worker first and waits for the worker (it prints that it is waiting); the mocks stop last. The run ends `succeeded`, or `yielded` if the kind is batched, and never `cancelled`. Dashboard 15 shows `tz_job_hard_cancelled_total` 0. If the kind finishes in under a second, record **NA** and its duration. | SC-GS-04 D-98 A-21 | ▢ | |
| JOBS-M.29 | If M.28 ended `yielded`: after `make dev`, open that kind's history. | A continuation run for the same period follows and ends `succeeded`. The two runs' summary counts add up to the total of an uninterrupted run of the same period (run it again after `make reset` and compare). No item was processed twice. | SC-GS-05 D-98 A-21 | ▢ | |
| JOBS-M.30 | Open C-12 (order tracking) as C10 with an order in progress, so the live connection is open (DevTools → Network → the event stream). Run `make stop`, wait for it to finish, then `make dev`. As K22 accept the order. | The stream closes with a `reconnect` event (retry 1000 ms), with no error screen and no page reload. After the API is back, C-12 updates by itself to "Accepted" and nothing is missed. | SC-GS-07 D-98 06 G9 C-12 | ▢ | |

### G. Updates never interrupt work

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.31 ★ | As C10 put an item in the cart and stay on C-11 Checkout. Build a new version of the customer app (precondition 7: change a visible string, `pnpm build` in `customer/`, let `pnpm preview` serve it). Wait a minute. | No update toast and no reload while the checkout is open. After placing the order or leaving checkout, the toast appears and the user chooses when to reload. | SC-GS-08 D-98 06 G9 C-11 | ▢ | |
| JOBS-M.32 ★ | As K22 on B-05 start a sale and keep it open. Ship a new version of the business app the same way (`pnpm build` in `business/`, `pnpm preview`). | No update prompt and no reload while the sale is open. After the sale is closed or cancelled, the prompt appears. | SC-GS-08 D-98 06 G9 B-05 | ▢ | |
| JOBS-M.33 | Keep the customer app open on C-03 Home in one tab without visiting Search or Wallet. Ship a new customer build as in precondition 7 so the old chunk files are gone. In that tab open Search, Wallet and a store page. | Every screen loads from the old version's service worker cache. No white screen, no "failed to load module" error, no forced reload. | SC-GS-09 D-98 06 G9 | ▢ | |

### H. AWS keys stay out of the app

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| JOBS-M.34 | Add two lines to `.env.local` with the placeholder value `qa-not-a-key`: `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. `make stop`, then `make dev`. Find the API and worker process ids (`pgrep -f "todayz api"`, `pgrep -f "todayz worker"`) and count the AWS variables each sees: `ps eww -p <pid> | grep -c AWS_`. Open A-22 as AD47. Then `make logs` and count lines naming an Amazon host: `make logs | grep -c amazonaws` (stop it after a minute). | Both counts of `AWS_` are `0`: `proc.sh` strips every `AWS_*` variable from app processes. A-22 still reads "Not configured (local)" on the AWS panels, with no error. No line names an Amazon host (the count is `0`) and the outbound-traffic guard reports nothing. | SC-OPS-09 D-97 A-22 | | |

## Cleanup

1. Resume every kind you paused (`rollup_today`, `ledger_reconcile`). Check A-21 shows no "Paused" kind.
2. Start the mocks and the worker if stopped (`infra/local/proc.sh status`). `clock reset`.
3. Remove the two `AWS_*` lines from `.env.local`. Stop the `pnpm preview` processes and start the dev processes again (`infra/local/proc.sh start customer`, `infra/local/proc.sh start business`).
4. `restore defaults`: no setting was changed in this script.
5. `make reset`.
