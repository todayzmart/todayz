# Local stack (W0-02)

Everything TodayZ needs on a laptop, without the internet once it is set up:
Postgres 16 + PostGIS 3.4, MinIO (stands in for Cloudflare R2), Grafana LGTM
(Grafana, Prometheus, Loki, Tempo, OTLP), postgres_exporter, and Caddy for HTTPS
on `*.todayz.localhost`. Apps (API, PWAs, site, mocks) run on the host through
`proc.sh`; each work package registers its own process in its `mk/*.mk`.

## Run it

```
colima start            # once per boot; Docker runs in Colima
make setup              # pulls images, installs pmtiles, downloads city map tiles
caddy trust             # once, by you: adds Caddy's local CA to the keychain (asks for your password)
make dev                # containers, buckets, tiles, Caddy, apps, then the health check
make stop               # stops apps, Caddy and containers (data is kept)
make reset              # drops and recreates the databases (migrate + seed run after)
make logs               # follows container logs and .cache/dev/*.log
```

`make dev` ends with `infra/local/health.sh`, which prints a table and writes
the page at https://todayz.localhost/_health. Run `infra/local/health.sh` any
time to refresh both. Green is healthy, red is down (make dev fails), grey is a
service no work package has started yet.

**One stack per machine; run it from the main checkout.** The ports are fixed, so
only one `todayz` stack can run. `make dev` and `make reset` refuse when the
running stack was started from another checkout (a worktree, say) and name that
checkout; run make there, or `make stop` there first. `TODAYZ_STACK_FORCE=1`
overrides, for when you know the other checkout is gone.

## Hosts and ports

| URL | Upstream | What |
|---|---|---|
| https://api.todayz.localhost | 127.0.0.1:8180 | Go API (W0-03); SSE is not buffered |
| https://app.todayz.localhost | 127.0.0.1:5301 | Customer PWA (Vite, HMR websocket proxied) |
| https://business.todayz.localhost | 127.0.0.1:5302 | Business PWA |
| https://admin.todayz.localhost | 127.0.0.1:5303 | Admin console |
| https://todayz.localhost | 127.0.0.1:5304 | Astro site; `/_health` is the stack health page |
| https://mocks.todayz.localhost | 127.0.0.1:4010 | Provider mocks (W0-07) |
| https://grafana.todayz.localhost | 127.0.0.1:3300 | Grafana: anonymous Editor (no datasource, user or settings changes); only this host name is served (others redirect here); CSRF-checked |
| https://s3.todayz.localhost | 127.0.0.1:9100 | MinIO S3 API (SigV4 works through Caddy) |
| https://img.todayz.localhost | MinIO bucket `images` | Public reads only: no writes (405), no listing, no path tricks (`..`, `%2e`, `%2f`, `%5c`, `//` give 400); `Cache-Control` on 200/206 only |
| https://tiles.todayz.localhost | MinIO bucket `tiles` | PMTiles; Range requests, CORS for the app origins, `Vary: Origin` always; same read-only rules |

| Host port | Service |
|---|---|
| 5433 | Postgres (databases `todayz`, `todayz_test`; roles below). 5432 is left alone. |
| 9100 / 9101 | MinIO API / console (http://127.0.0.1:9101, `todayz` / `todayz-local`, the same as `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` in local.env) |
| 3300 | Grafana |
| 4317 / 4318 | OTLP gRPC / HTTP |
| 9390 | Prometheus (scrapes postgres_exporter) |
| 9187 | postgres_exporter |
| 443 / 80 | Caddy (80 only redirects); admin API on localhost:2019 |

All container ports bind to 127.0.0.1. Caddy has to listen on all interfaces
(macOS refuses a non-root bind of 127.0.0.1:443), so every site, and the :80
redirect, aborts requests that do not come from loopback. The Caddyfile sets
`order abort first` so that abort runs before any `handle` or `redir`;
`test/static.sh` checks the adapted config, and `test/dev-smoke.sh` calls all
10 HTTPS hosts and :80 on the LAN address and expects every one refused.

### Postgres roles

| Role | Password | Used by | Can |
|---|---|---|---|
| `todayz_app` | `todayz_app` | API and workers (`DATABASE_URL`) | read and write tables, sequences and functions owned by `todayz_owner`; no DDL; not a superuser |
| `todayz_owner` | `todayz_owner` | migrations (`DATABASE_MIGRATE_URL`) | owns both databases and schema `public` |
| `todayz` | `todayz` | `make reset`, admin | superuser; no app uses it |
| `grafana_ro` | `grafana_ro` | Grafana datasource | read only |
| `todayz_exporter` | `todayz_exporter` | postgres_exporter | `pg_monitor` |

Local-only values, literal in `postgres/init.sql` and `docker-compose.yml`
(compose reads no env file). init.sql also creates `postgis`, `pg_trgm`,
`unaccent`, `pg_stat_statements` and `pgcrypto` in both databases (a
non-superuser owner cannot), sets default privileges for what `todayz_owner`
creates, and revokes EXECUTE on new functions from PUBLIC. It runs on a new
volume, on every `make dev`, and on `make reset`.

## Files

| File | Purpose |
|---|---|
| `../../docker-compose.yml` | Compose project `todayz` (pinned images, healthchecks, named volumes) |
| `local.env` | Committed, non-secret local defaults; `.env.local` overrides it |
| `Caddyfile` | HTTPS routes; `tls internal`; default storage, so `caddy trust` covers it |
| `proc.sh` | Host process manager: `start NAME [--port P] [--health URL] [--cwd DIR] -- CMD`, `stop`, `stop-all`, `status`, `logs` |
| `stack.sh` | Docker and Caddy steps behind `mk/10-stack.mk` |
| `health.sh` | Health table and page |
| `tiles.sh` | Installs `pmtiles` v1.31.2 into `.tools/bin`; extracts Bengaluru, Hyderabad, Chennai (z0-14) from Protomaps build 20260811 |
| `postgres/init.sql` | Roles, databases, extensions and default privileges (idempotent; make dev and make reset re-run it) |
| `minio/minio-init.sh` | Buckets, policies, probe objects, tile upload (runs in the `mc` container) |
| `lgtm/` | Prometheus scrape config, Grafana Postgres datasource, dashboards provider (reads `infra/grafana/dashboards/`, the only repo folder mounted into the container) |
| `test/static.sh` | `make check`: compose config (counts, loopback ports, digests, literal credentials), Caddyfile (abort first on every site; validated in a throwaway HOME), shellcheck, local.env rules (no network) |
| `test/proc-test.sh` | `make check`: no `AWS_*` variable reaches a `proc.sh` child, `stop-all` order, SIGKILL after the stop timeout (throwaway root, dummy values) |
| `test/dev-smoke.sh` | `make e2e`: real `make dev` twice, health, every host, LAN refusal, Grafana hardening, DB roles, image round trip, path tricks, tile range, CORS, leak scan; then `make stop` |

### proc.sh for other work packages

```make
DEV_STEPS += dev-api
dev-api:
	@$(ROOT)/infra/local/proc.sh start api --port 8180 --health http://127.0.0.1:8180/healthz --cwd $(ROOT)/backend -- go run ./cmd/api
```

The child gets `local.env`, then `.env.local`, exported (no values are printed),
then every `AWS_*` variable is removed, whether it came from those files or the
calling shell: the app never sees AWS keys (D-97).
`start` is idempotent, refuses a port that is already taken (and names the
owner), and with `--health` waits up to 90 s. Each process runs in its own
process group, so `stop` also ends `pnpm -> vite` style children. Logs go to
`.cache/dev/NAME.log`; pidfiles to `.cache/dev/pids/`; the registry the health
check reads is `.cache/dev/registry.tsv`.

`stop` sends SIGTERM to the group and waits before SIGKILL: `api` 120 s,
`worker` 180 s, anything else 10 s (`PROC_STOP_TIMEOUT_API`,
`PROC_STOP_TIMEOUT_WORKER` and `PROC_STOP_TIMEOUT` override them). `stop-all`
(and so `make stop`) drains in order (D-98): `api` and `worker` together first,
waiting for both to exit, then everything else, `mocks` last.

Vite apps need `server.allowedHosts: ['.todayz.localhost']` and
`server.hmr: { clientPort: 443, protocol: 'wss' }` to work behind Caddy.

### Images

Every image is pinned as `tag@sha256:digest` (the registry index digest, so it
covers every platform the image has); `test/static.sh` fails on an image
without one. Under `scripts/no-egress.sh` compose runs with `--pull never`.
To bump one: `docker buildx imagetools inspect IMAGE:NEWTAG`, put tag and
digest in `docker-compose.yml` (PostGIS: `TZ_POSTGIS_*` in `lib.sh` and the
compose default), update this table, `make setup`.

| Image | Digest | Why this one |
|---|---|---|
| `postgis/postgis:16-3.4` (amd64) | `sha256:44126d872ac91993766c341e369c539e8196614321765d36a6f1bab0419a5fa5` | The official image is amd64 only. |
| `imresamu/postgis:16-3.4` (arm64, amd64) | `sha256:6da75969915039b7356058b4310d43fde88c275ada982c2dfee29da68445ff4d` | The same docker-postgis build, published multi-arch. `lib.sh` picks by `uname -m` (override with `TODAYZ_POSTGIS_IMAGE`, tag@digest). |
| `pgsty/minio:RELEASE.2026-08-04T00-00-00Z` | `sha256:b6bfe7239bfc83fb90d31612d9704d86039dd714f7904b3f1ad68f211e602372` | MinIO stopped publishing images; pgsty builds the same AGPL source. |
| `pgsty/mc:RELEASE.2026-09-16T00-00-00Z` | `sha256:cfc83108c3abb371f8fb84d99c1fdc88f8c237e022409b0081fb7c0a3be634dd` | As above. |
| `grafana/otel-lgtm:0.36.0` | `sha256:57c04cbd8e7022f32bb84c4ac005d79baf8b08e2c5be44facf11f5f1f9944ba2` | Plugin preinstall disabled so it starts offline. |
| `prometheuscommunity/postgres-exporter:v0.20.1` | `sha256:ac5ec343104fae0e2d84a27bb8d69b38430a11910c5382cad85d478d2bab713e` | |

## Troubleshooting

**"Docker is not running" / `Cannot connect to the Docker daemon`.**
Start Colima: `colima start`. Check with `docker info`.

**"port NNNN is already in use by X".**
Something else holds a port from the table above. If it is an old TodayZ
process, `make stop` (or `infra/local/proc.sh stop NAME`). Otherwise stop that
program. `lsof -nP -iTCP:NNNN -sTCP:LISTEN` shows who it is. Postgres here
uses 5433 on purpose, so a Postgres of your own on 5432 is fine.

**"another Caddy answers on its admin port".**
A Caddy not started by `make dev` is running. `caddy stop`, then `make dev`.

**Browser says the certificate is not trusted (NET::ERR_CERT_AUTHORITY_INVALID).**
Run `caddy trust` once (it asks for your password). Firefox may need
`security.enterprise_roots.enabled = true`. Scripts never need it: they pass
`--cacert ~/Library/Application\ Support/Caddy/pki/authorities/local/root.crt`.

**`*.todayz.localhost` does not resolve.**
Browsers and curl resolve `.localhost` to loopback themselves. Other tools may
not; use `curl --resolve host:443:127.0.0.1` or add a hosts entry.

**502 from an app host.**
The app is not running. `infra/local/proc.sh status` and `.cache/dev/NAME.log`.

**Maps are blank.**
`infra/local/tiles.sh status`; if empty or a file "DOES NOT VERIFY", `make setup`
with internet, then `make dev`. Each city file is checked against a pinned
sha256 (the extract is deterministic). If Protomaps has removed build 20260811,
fetch warns: pick a newer build, bump `TILES_BUILD` and the three sha256s in
`tiles.sh` together. `make setup` without Go only warns (no pmtiles CLI, no maps).

**A new dashboard file does not show up in Grafana.**
Grafana cannot watch files through Colima's mount, so it only reads
`infra/grafana/dashboards/` at start: `docker compose -p todayz restart lgtm`.

**Start over.**
`make stop`, then `docker compose -p todayz down -v` deletes the volumes (all
local data). `make dev` recreates everything.
