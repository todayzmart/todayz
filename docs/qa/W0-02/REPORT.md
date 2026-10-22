# W0-02 Local stack: QA report

- **Branch:** `wp/W0-02-local-stack`
- **Machine:** macOS arm64, Docker via Colima, GNU Make 3.81
- **Date:** 2026-10-22
- Every run was local; nothing touched a cloud account.
- Built by the W0-02 agent. The lead saved this report and re-ran `make check` after rebasing onto `e2c2c21` (fail-fast make): pass.

## What was built

- **Compose** (project `todayz`, loopback-only ports, pinned images, a healthcheck on every service)

  | Service | Image | Port(s) | Notes |
  |---|---|---|---|
  | Postgres 16 + PostGIS 3.4 | `imresamu/postgis:16-3.4` on arm64, `postgis/postgis:16-3.4` on amd64 (`lib.sh` picks; override with `TODAYZ_POSTGIS_IMAGE`) | 5433 | pg_stat_statements; databases `todayz` and `todayz_test`; read-only roles `grafana_ro` and `todayz_exporter` |
  | MinIO | `pgsty/minio` (MinIO stopped publishing images) | 9100 / 9101 | |
  | otel-lgtm 0.35.0 | `grafana/otel-lgtm` | Grafana 3300, OTLP 4317/4318, Prometheus 9390 | read-only Postgres datasource, uid `todayz-postgres` |
  | postgres_exporter v0.20.1 | `prometheuscommunity/postgres-exporter` | 9187 | scraped by Prometheus |

- **Buckets**
  - `images` and `tiles`: anonymous GetObject only, no listing.
  - `private`: no anonymous access.
  - Setup is idempotent.
- **Caddy** (on the host)
  - `tls internal` with default storage; trust install is skipped because the user runs `caddy trust`.
  - 10 `*.todayz.localhost` hosts; http redirects with 308; requests not from loopback are aborted on every site and on :80 (`order abort first`; see the correction below).
  - `img.` and `tiles.` are read-only bucket views: 405 on writes, 404 on listing, CORS for the app origins, Range support.
  - `/_health` serves the health page.
- **Tiles**
  - pmtiles v1.31.2, Protomaps build 20260811, z0–14.
  - Bengaluru 18 MB, Hyderabad 13 MB, Chennai 9 MB.
  - Extracted by `make setup`; uploaded by `make dev`.
- **`infra/local/proc.sh`**: host process manager.
  - Commands: start, stop, stop-all, status, logs.
  - Checks a port is free first and names the process holding it.
  - Waits for the health URL.
  - Runs each process in its own process group, with a pid-reuse guard.
  - Passes in env from `local.env` and then `.env.local`, never printing a value.
- **`infra/local/health.sh`**: a table plus the `/_health` page; red means down (exit 1), grey means not registered yet.
- **Make**
  - `mk/10-stack.mk`: setup, dev, stop, logs, reset, check (`test/static.sh`: no network, no stack) and e2e (`test/dev-smoke.sh`).
  - `mk/99-health.mk`: dev-health runs last.

## Verification (all PASS)

1. `make setup`: images pulled and 3 tile files extracted; a re-run skips existing files.
2. `make dev` from cold: containers healthy, buckets created, tiles uploaded, Caddy up, health green (apps grey).
3. `make dev` again: idempotent.
4. `static.sh` checks:
   - compose parses, ports are loopback-only, images pinned, healthchecks present;
   - Caddy adapts and validates, all 10 hosts present, every certificate from the internal CA, trust install off;
   - shellcheck clean;
   - local.env names are all in `.env.example`, with no D-18 key and no provider secret;
   - launch.json is valid.
5. `make e2e` (dev-smoke):
   - Every host answers over TLS with the Caddy CA, and http redirects with 308.
   - A request via the LAN IP is refused, and the internal files are not served. (Correction: this first run only tried `grafana.`; see below.)
   - Images: a SigV4 PUT through `s3.` reads back through `img.`; PUT on `img.` gives 405; no bucket listing; `private` gives 403.
   - Tiles: a Range GET returns 206 with the PMTiles magic bytes; CORS preflight works for app origins and refuses a foreign origin.
   - Secret scans of every log are clean (sc-pl-03 plus `envtool leakscan -env .env.local`).
6. proc.sh with a fake app on 5301:
   - start waits for health; a second start says it is already running.
   - A different name on the same port is refused, and the owner is named.
   - Only env names were checked in the child.
   - A websocket upgrade through `app.todayz.localhost` returns 101 (the HMR path).
   - Health goes red when the process is killed behind proc.sh's back.
7. Process-group stop: no child processes left behind.
8. `make reset`: both databases recreated.
9. `make logs`: all 4 services plus caddy.log.
10. `make stop`: containers exit, Caddy is down, and health exits 1.
11. `make dev` after stop works.
12. Grafana datasource health OK; a write as `grafana_ro` is refused (read-only transaction).
13. Prometheus scrapes the exporter (`pg_up 1`); OTLP 4318 accepts a POST; 4317 is open.
14. `make check` with the stack stopped: rc 0.
15. The stack was left stopped.

## Screenshots

- `health-page.png`: stack rows green, app rows grey.
- `grafana-datasources.png`: shows "TodayZ Postgres (local, read-only)".
- Both were taken with headless Chrome using `--ignore-certificate-errors`, because `caddy trust` has not been run yet.

## Known gaps

- Vite HMR through Caddy has been tested with a fake websocket server only. Real HMR will be verified when W0-05 merges.
- `R2_ENDPOINT` is `http://127.0.0.1:9100`, so presigned URLs point at loopback http. Browsers treat that as a secure context.
- The health page is a snapshot written by health.sh; it does not refresh itself.
- The browser pane cannot open `*.todayz.localhost` until the user runs `caddy trust` once.
- `infra/grafana/dashboards` does not exist until W1-07, so Grafana logs one "cannot read directory" line.

## Correction after the security review

The claim above that every site aborts non-loopback requests was wrong. Caddy
sorts `abort` after `handle` and `redir`, so `todayz.localhost`, `img.`, `tiles.`
and the :80 redirect answered LAN clients; the first smoke test only probed
`grafana.` (a plain `reverse_proxy` site, where abort did run first). Fixed with
`order abort first`; `static.sh` now asserts abort is the first handler of every
site in the adapted JSON, and `dev-smoke.sh` calls all 10 HTTPS hosts and :80 on
the LAN address and expects every one refused (passes).

The same review round also added: Grafana enforce_domain, CSRF always-check and
anonymous Editor; Vary: Origin always; pid and Caddy identity guards; one stack
per machine; literal MinIO credentials; digest-pinned images; path-trick 400s on
`img.`/`tiles.`; tile sha256 pins; `todayz_app` / `todayz_owner` roles; and
dev-smoke now stops the stack when it ends. `make check` and `make e2e` pass.

## Requests to lead, with decisions

- **Make 3.81 fail-fast:** fixed on main in `e2c2c21`.
- **09 §1.1 still says Postgres 5432 and `minio/minio`:** the lead is updating the plan.
- **App processes register with `proc.sh --health`:** passed on to W0-03, W0-05 and W0-07 (W0-07 already does).
- **check-repo shellcheck could also cover `infra/local`:** `static.sh` covers it today; this stays as-is.
- **Status lines in `infra/CLAUDE.md` and `e2e/CLAUDE.md`:** the lead updates them after the Wave 0 merges.
