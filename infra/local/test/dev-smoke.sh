#!/usr/bin/env bash
# ok() and bad() always return 0, so "A && ok || bad" is a safe if/else here.
# shellcheck disable=SC2015
# Local stack smoke test (W0-02), run by `make e2e` (under scripts/no-egress.sh).
#   1. make dev (output captured), then make dev again: it must be idempotent
#   2. health.sh passes; /_health serves only its own files
#   3. every *.todayz.localhost host answers over HTTPS; http redirects; a client
#      on the LAN address is refused on all 10 HTTPS hosts and on :80
#   4. Grafana: only its own domain, CSRF-checked, anonymous Editor (not Admin)
#   5. Postgres roles: todayz_app reads and writes todayz_owner's tables, no DDL
#   6. an image uploaded through s3. (SigV4 via Caddy) reads back through img.;
#      img. refuses writes, listings and path tricks (.., encoded dots/slashes, //)
#   7. a ranged GET on a tile answers 206 with the PMTiles magic; CORS for the app
#      origins only, Vary: Origin always
#   8. no secret value from .env.local appears in the captured output (SC-PL-03)
# The stack is stopped at the end (make stop, on any exit), so ports are free again.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=infra/local/lib.sh
source "$here/../lib.sh"

out="$TZ_DEV/smoke"
rm -rf "$out"; mkdir -p "$out"
fail=0
ok() { echo "ok: $*"; }
bad() { echo "FAIL: $*" >&2; fail=1; }
code() { local url="$1"; shift; tz_curl -s -o /dev/null -m 10 -w '%{http_code}' "$@" "$url" 2>/dev/null || true; }

# shellcheck disable=SC2329 # invoked by the EXIT trap
finish() {
  local rc=$?
  [[ $fail -eq 0 ]] || rc=1
  if make -C "$TZ_ROOT" stop >"$out/make-stop.log" 2>&1; then
    echo "ok: make stop (stack stopped)"
  else
    echo "FAIL: make stop failed; see .cache/dev/smoke/make-stop.log" >&2
    rc=1
  fi
  echo
  [[ $rc -eq 0 ]] && echo "dev-smoke: all checks pass (logs in .cache/dev/smoke/)" || echo "dev-smoke: FAILED (logs in .cache/dev/smoke/)" >&2
  exit "$rc"
}
trap finish EXIT

HOSTS=(todayz.localhost app.todayz.localhost business.todayz.localhost admin.todayz.localhost
  api.todayz.localhost mocks.todayz.localhost grafana.todayz.localhost s3.todayz.localhost
  img.todayz.localhost tiles.todayz.localhost)

[[ -f "$TZ_CADDY_ROOT_CERT" ]] || echo "note: no Caddy root certificate yet; it is created on the first make dev"

# --- 1. make dev, twice --------------------------------------------------------------
for run in 1 2; do
  if make -C "$TZ_ROOT" dev >"$out/make-dev-$run.log" 2>&1; then
    ok "make dev (run $run)"
  else
    bad "make dev (run $run) failed; last lines:"
    tail -n 25 "$out/make-dev-$run.log" >&2
    exit 1
  fi
done
grep -q 'caddy reloaded' "$out/make-dev-2.log" && ok "second make dev reloaded Caddy instead of starting another" ||
  bad "second make dev did not reload Caddy"
[[ -f "$TZ_CADDY_ROOT_CERT" ]] || bad "Caddy did not create its local CA"

# --- 2. health -------------------------------------------------------------------------
if "$TZ_LOCAL/health.sh" >"$out/health.log" 2>&1; then ok "health.sh"
else bad "health.sh failed:"; cat "$out/health.log" >&2; fi
[[ "$(code https://todayz.localhost/_health)" == 200 ]] && grep -q 'tz-table' "$TZ_DEV/health.html" &&
  ok "https://todayz.localhost/_health serves the health page" || bad "/_health page not served"
[[ "$(code https://todayz.localhost/_health/registry.tsv)" != 200 &&
  "$(code https://todayz.localhost/_health/../caddy.log --path-as-is)" != 200 &&
  "$(code https://todayz.localhost/_health/%2e%2e/caddy.log --path-as-is)" != 200 ]] &&
  ok "/_health serves only the page and its CSS (no other .cache/dev file, no ..)" || bad "/_health exposes other .cache/dev files"

# --- 3. hosts ------------------------------------------------------------------------------
for h in "${HOSTS[@]}"; do
  c="$(code "https://$h/")"
  if [[ "$c" == 000 || -z "$c" ]]; then bad "https://$h: TLS/connection failed"
  else ok "https://$h answers ($c)"; fi
  echo "$h $c" >>"$out/hosts.txt"
done
[[ "$(code https://grafana.todayz.localhost/api/health)" == 200 ]] && ok "grafana through Caddy" || bad "grafana through Caddy"
[[ "$(code http://img.todayz.localhost/x)" == 308 ]] && ok "http redirects to https" || bad "http does not redirect"
# macOS names the LAN interface en0/en1; on Linux (the CI runner) take the source address of
# the default route.
lan_ip="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)"
if [[ -z "$lan_ip" ]] && command -v ip >/dev/null; then
  lan_ip="$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -n1)"
fi
if [[ -n "$lan_ip" ]]; then
  served=""
  for h in "${HOSTS[@]}"; do
    c="$(tz_curl -s -o /dev/null -m 5 -w '%{http_code}' --resolve "$h:443:$lan_ip" "https://$h/" 2>/dev/null || true)"
    [[ "$c" == 000 || -z "$c" ]] || served+="https://$h=$c "
  done
  c="$(tz_curl -s -o /dev/null -m 5 -w '%{http_code}' --resolve "todayz.localhost:80:$lan_ip" "http://todayz.localhost/" 2>/dev/null || true)"
  [[ "$c" == 000 || -z "$c" ]] || served+="http://todayz.localhost=$c "
  [[ -z "$served" ]] && ok "a client on the LAN address ($lan_ip) is refused on all 10 HTTPS hosts and on :80" ||
    bad "non-loopback client was served: $served"
else
  bad "no LAN address (en0/en1, or the default route's source) to test the loopback-only rule"
fi

# --- 4. Grafana hardening ---------------------------------------------------------------------
c="$(tz_curl -s -o /dev/null -m 5 -w '%{http_code}' http://127.0.0.1:3300/login 2>/dev/null || true)"
[[ "$c" == 301 || "$c" == 302 ]] && ok "Grafana redirects other Host names to grafana.todayz.localhost ($c)" ||
  bad "Grafana served Host 127.0.0.1:3300 directly ($c); enforce_domain is off"
# Anonymous is Editor: admin-only calls are refused, even from Grafana's own origin.
g_same=(-H 'Content-Type: application/json' -H 'Origin: https://grafana.todayz.localhost')
c1="$(code https://grafana.todayz.localhost/api/admin/settings)"
c2="$(code https://grafana.todayz.localhost/api/datasources -X POST "${g_same[@]}" --data '{"name":"smoke","type":"prometheus","access":"proxy","url":"http://127.0.0.1:1"}')"
c3="$(code https://grafana.todayz.localhost/api/org/users)"
[[ "$c1" == 403 && "$c2" == 403 && ( "$c3" == 403 || "$c3" == 401 ) ]] &&
  ok "anonymous Grafana user is not Admin (settings, datasource create, org users refused)" ||
  bad "anonymous Grafana admin calls: settings=$c1 datasource-create=$c2 org-users=$c3"
c="$(code https://grafana.todayz.localhost/api/folders -X POST -H 'Content-Type: application/json' -H 'Origin: https://evil.example' --data '{"title":"smoke-csrf"}')"
[[ "$c" == 403 ]] && ok "cross-origin POST to Grafana is refused (CSRF, 403)" || bad "cross-origin POST to Grafana gave $c"
# The same request from Grafana's own origin works (Editor), so the 403 above is the CSRF check.
uid="smoke-$$"
c="$(code https://grafana.todayz.localhost/api/folders -X POST "${g_same[@]}" --data "{\"uid\":\"$uid\",\"title\":\"smoke $uid\"}")"
[[ "$c" == 200 ]] && ok "same-origin Editor request works (folder created)" || bad "same-origin folder create gave $c"
code "https://grafana.todayz.localhost/api/folders/$uid" -X DELETE "${g_same[@]}" >/dev/null

# --- 5. Postgres roles -------------------------------------------------------------------------
psql_as() { # role sql: runs inside the container over TCP, as the given role
  tz_compose exec -T -e PGPASSWORD="$1" -e PGOPTIONS="-c client_min_messages=warning" postgres \
    psql -X -q -t -A -v ON_ERROR_STOP=1 -h 127.0.0.1 -U "$1" -d todayz -c "$2" 2>>"$out/roles.log"
}
tbl="smoke_roles_$$"
if psql_as todayz_owner "DROP TABLE IF EXISTS $tbl; CREATE TABLE $tbl (id serial PRIMARY KEY, v text NOT NULL);" >/dev/null; then
  r="$(psql_as todayz_app "INSERT INTO $tbl (v) VALUES ('a'), ('b'); UPDATE $tbl SET v = 'c' WHERE v = 'a'; DELETE FROM $tbl WHERE v = 'b'; SELECT string_agg(v, ',') FROM $tbl;" || true)"
  [[ "$r" == c ]] && ok "todayz_app can read and write todayz_owner's table (sequence included)" || bad "todayz_app read/write on $tbl gave '${r}'"
  psql_as todayz_app "CREATE TABLE smoke_app_$$ (x int);" >/dev/null 2>&1 && bad "todayz_app could CREATE TABLE" ||
    ok "todayz_app cannot CREATE TABLE"
  psql_as todayz_app "DROP TABLE $tbl;" >/dev/null 2>&1 && bad "todayz_app could DROP todayz_owner's table" ||
    ok "todayz_app cannot DROP TABLE"
  r="$(psql_as todayz_app "SELECT rolsuper FROM pg_roles WHERE rolname = current_user;" || true)"
  [[ "$r" == f ]] && ok "todayz_app is not a superuser" || bad "todayz_app rolsuper=$r"
  psql_as todayz_owner "DROP TABLE IF EXISTS $tbl;" >/dev/null || bad "todayz_owner could not drop $tbl"
else
  bad "todayz_owner could not create a table in todayz; see .cache/dev/smoke/roles.log"
fi

# --- 6. image round trip -----------------------------------------------------------------------
tz_export_env_file "$TZ_LOCAL_ENV"
key="smoke/$(date +%s)-$$.png"
# 1x1 PNG
printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==' | base64 -d >"$out/pixel.png"
s3() { tz_curl -s -m 10 --aws-sigv4 "aws:amz:us-east-1:s3" --user "$R2_ACCESS_KEY_ID:$R2_SECRET_ACCESS_KEY" "$@"; }
c="$(s3 -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: image/png' --data-binary "@$out/pixel.png" "https://s3.todayz.localhost/images/$key" || true)"
if [[ "$c" == 200 ]]; then
  ok "PUT through https://s3.todayz.localhost (SigV4 over Caddy)"
  tz_curl -s -m 10 -D "$out/img.headers" -o "$out/pixel.back.png" "https://img.todayz.localhost/$key" || true
  cmp -s "$out/pixel.png" "$out/pixel.back.png" && ok "image reads back through https://img.todayz.localhost" || bad "img. read-back differs"
  grep -qi '^cache-control: public, max-age=300' "$out/img.headers" && ok "img. 200 is cacheable" || bad "img. 200 has no Cache-Control"
  tz_curl -s -m 10 -D "$out/img404.headers" -o /dev/null "https://img.todayz.localhost/smoke/missing-$$.png" || true
  grep -qi '^cache-control: public' "$out/img404.headers" && bad "img. made a 404 cacheable" || ok "img. errors are not marked cacheable"
  [[ "$(code "https://img.todayz.localhost/$key" -X PUT --data-binary x)" == 405 ]] && ok "img. refuses writes (405)" || bad "img. did not answer 405 to a write"
  [[ "$(code https://img.todayz.localhost/)" != 200 && "$(code https://tiles.todayz.localhost/)" != 200 ]] &&
    ok "img. and tiles. do not list their buckets" || bad "a bucket listing is public through Caddy"
  [[ "$(code http://127.0.0.1:9100/images/)" != 200 && "$(code "http://127.0.0.1:9100/tiles/?list-type=2")" != 200 ]] &&
    ok "anonymous policy allows GetObject only (no ListBucket)" || bad "anonymous ListBucket is allowed on MinIO"
  [[ "$(code "http://127.0.0.1:9100/private/$key")" == 403 && "$(code "http://127.0.0.1:9100/private/_health/probe.txt")" == 403 ]] &&
    ok "private bucket refuses anonymous reads" || bad "private bucket readable anonymously"
  s3 -o /dev/null -X DELETE "https://s3.todayz.localhost/images/$key" || true
else
  bad "PUT through s3. gave ${c:-nothing}"
fi
# Path tricks: each must be refused (400) before it reaches MinIO.
tricks=(
  "https://img.todayz.localhost/../private/_health/probe.txt"
  "https://img.todayz.localhost/%2e%2e/private/_health/probe.txt"
  "https://img.todayz.localhost/%2E%2E%2Fprivate%2F_health%2Fprobe.txt"
  "https://img.todayz.localhost/..%2fprivate/_health/probe.txt"
  "https://img.todayz.localhost/..%5cprivate/_health/probe.txt"
  "https://img.todayz.localhost//?list-type=2"
  "https://img.todayz.localhost//private/_health/probe.txt"
  "https://tiles.todayz.localhost/../images/_health/probe.txt"
  "https://tiles.todayz.localhost/../private/_health/probe.txt"
)
leaked=""
for u in "${tricks[@]}"; do
  c="$(code "$u" --path-as-is)"
  [[ "$c" == 400 ]] || leaked+="$u=$c "
done
[[ -z "$leaked" ]] && ok "img./tiles. refuse ${#tricks[@]} path tricks with 400" || bad "path tricks not refused: $leaked"
[[ "$(code https://img.todayz.localhost/_health/probe.txt)" == 200 ]] && ok "a plain object still reads after the trick filters" ||
  bad "img. probe object not readable"

# --- 7. tiles: range + CORS ------------------------------------------------------------------------
tile=""
for f in "$TZ_LOCAL"/pmtiles/*.pmtiles; do [[ -e "$f" ]] && { tile="$(basename "$f")"; break; }; done
if [[ -n "$tile" ]]; then
  c="$(tz_curl -s -m 10 -H 'Range: bytes=0-126' -D "$out/tile.headers" -o "$out/tile.head" -w '%{http_code}' "https://tiles.todayz.localhost/$tile" || true)"
  if [[ "$c" == 206 && "$(head -c 7 "$out/tile.head")" == PMTiles && "$(wc -c <"$out/tile.head" | tr -d ' ')" == 127 ]]; then
    ok "ranged GET on $tile -> 206, 127 bytes, PMTiles header"
  else
    bad "ranged GET on $tile gave $c"
  fi
else
  echo "skip: no local tiles (make setup); ranged GET checked on the probe object"
  [[ "$(code https://tiles.todayz.localhost/_health/probe.txt -H 'Range: bytes=0-1')" == 206 ]] && ok "ranged GET -> 206" || bad "ranged GET on tiles."
  tile="_health/probe.txt"
fi
hdr="$(tz_curl -s -m 5 -o /dev/null -D - -X OPTIONS -H 'Origin: https://app.todayz.localhost' \
  -H 'Access-Control-Request-Method: GET' -H 'Access-Control-Request-Headers: range' "https://tiles.todayz.localhost/$tile" | tr -d '\r')"
grep -qi '^HTTP/[0-9.]* 204' <<<"$hdr" && grep -qi '^access-control-allow-origin: https://app.todayz.localhost$' <<<"$hdr" &&
  grep -qi '^access-control-allow-headers:.*range' <<<"$hdr" && grep -qi '^vary:.*origin' <<<"$hdr" &&
  ok "CORS preflight for the customer app" || bad "CORS preflight"
hdr="$(tz_curl -s -m 5 -o /dev/null -D - -H 'Origin: https://evil.example' -H 'Range: bytes=0-1' "https://tiles.todayz.localhost/$tile" | tr -d '\r')"
grep -qi '^HTTP/[0-9.]* 206' <<<"$hdr" && ! grep -qi '^access-control-allow-origin' <<<"$hdr" && grep -qi '^vary:.*origin' <<<"$hdr" &&
  ok "foreign origin: 206 without CORS headers, Vary: Origin set" || bad "CORS for a foreign origin"
hdr="$(tz_curl -s -m 5 -o /dev/null -D - -H 'Origin: https://business.todayz.localhost' -H 'Range: bytes=0-1' "https://tiles.todayz.localhost/$tile" | tr -d '\r')"
grep -qi '^access-control-expose-headers:.*content-range' <<<"$hdr" && [[ "$(grep -ci '^vary:' <<<"$hdr")" -eq 1 ]] &&
  ok "CORS exposes Content-Range; one Vary header" || bad "CORS expose headers / Vary"

# --- 8. no secrets in the output -----------------------------------------------------------------------
logs=("$out"/*.log)
if [[ -x "$TZ_ROOT/scripts/test/sc-pl-03.sh" ]]; then
  "$TZ_ROOT/scripts/test/sc-pl-03.sh" "${logs[@]}" >"$out/sc-pl-03.txt" 2>&1 && ok "SC-PL-03 on the captured logs" ||
    { bad "SC-PL-03"; cat "$out/sc-pl-03.txt" >&2; }
else
  bad "scripts/test/sc-pl-03.sh is missing; the secret scan did not run"
fi
# sc-pl-03 scans with fresh temp keys; scan with the real .env.local too.
if [[ ! -f "$TZ_ENV_LOCAL" ]]; then
  bad ".env.local is missing (make setup); cannot scan for its values"
elif [[ ! -x "$TZ_ROOT/.tools/bin/envtool" ]]; then
  bad ".tools/bin/envtool is missing (make setup); cannot scan for .env.local values"
else
  "$TZ_ROOT/.tools/bin/envtool" leakscan -env "$TZ_ENV_LOCAL" "${logs[@]}" "$TZ_DEV"/*.log >/dev/null 2>&1 &&
    ok "no .env.local value in make dev output or process logs" || bad "a .env.local value appeared in a log (envtool leakscan)"
fi

exit $fail
