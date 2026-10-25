#!/usr/bin/env bash
# bad() always returns 0, so "A && B || bad" only runs bad when A or B fails.
# shellcheck disable=SC2015
# Static checks for the local stack (W0-02), run by `make check`.
# Needs no network and no running stack: it runs under scripts/no-egress.sh.
#   - docker-compose.yml parses; exactly 4 long-running services and the 8 contract
#     ports, all on 127.0.0.1; every image (and lib.sh's PostGIS refs) pinned by digest;
#     no credential interpolated from the environment
#   - the Caddyfile validates (in a throwaway HOME: no CA lands in your Caddy storage),
#     serves every *.todayz.localhost host, and every site aborts non-loopback
#     clients as its first handler
#   - shellcheck on every infra/local script
#   - local.env: every name is in .env.example, no D-18 app key, no secret-looking
#     names; DATABASE_URL / DATABASE_MIGRATE_URL match the init.sql roles; R2_* match MinIO
#   - .claude/launch.json is valid; stack-health is the bare origin
# Each section prints one "ok" only when nothing in it failed.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=infra/local/lib.sh
source "$here/../lib.sh"

fail=0
sec=0
bad() { echo "FAIL: $*" >&2; fail=1; sec=1; }
section() { sec=0; }
done_ok() { if [[ $sec -eq 0 ]]; then echo "ok: $*"; fi; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
compose="$TZ_ROOT/docker-compose.yml"

# --- compose ---------------------------------------------------------------------
section
if ! out="$(tz_compose config -q 2>&1)"; then bad "docker compose config: $out"; fi
done_ok "docker-compose.yml parses"

section
if out="$(tz_compose --profile tools config --format json 2>/dev/null)"; then
  [[ "$(jq -r '.name' <<<"$out")" == todayz ]] || bad "compose project must be named todayz"
  # Exactly the contract ports, each bound to loopback.
  ports="$(jq -r '.services[] | (.ports // [])[] | "\(.host_ip // "")|\(.published)"' <<<"$out" | sort -u)"
  allowed=" 5433 9100 9101 3300 4317 4318 9390 9187 "
  n=0
  while IFS='|' read -r ip p; do
    [[ -n "$p" ]] || continue
    n=$((n + 1))
    [[ "$ip" == 127.0.0.1 ]] || bad "port $p must bind to 127.0.0.1 (got '${ip:-all interfaces}')"
    [[ "$allowed" == *" $p "* ]] || bad "port $p is not in the local port contract"
  done <<<"$ports"
  [[ $n -eq 8 ]] || bad "compose publishes $n ports; the contract has 8"
  # Exactly 4 long-running services, each health-checked.
  svcs="$(jq -r '[.services | to_entries[] | select((.value.profiles // []) | length == 0) | .key] | sort | join(" ")' <<<"$out")"
  [[ "$svcs" == "lgtm minio postgres postgres-exporter" ]] || bad "long-running services are '$svcs'; expected lgtm minio postgres postgres-exporter"
  while IFS='|' read -r svc hc; do
    [[ "$hc" == true ]] || bad "service $svc has no healthcheck"
  done < <(jq -r '.services | to_entries[] | select((.value.profiles // []) | length == 0) | "\(.key)|\(.value.healthcheck != null)"' <<<"$out")
  # Every image, tools profile included, is tag@sha256.
  while IFS='|' read -r svc img; do
    [[ "$img" =~ ^[^@[:space:]]+:[^@:[:space:]]+@sha256:[0-9a-f]{64}$ && "$img" != *:latest@* ]] ||
      bad "service $svc image '$img' is not pinned as tag@sha256:digest"
  done < <(jq -r '.services | to_entries[] | "\(.key)|\(.value.image)"' <<<"$out")
else
  bad "docker compose config --format json failed"
fi
for img in "$TZ_POSTGIS_AMD64" "$TZ_POSTGIS_ARM64"; do
  [[ "$img" =~ ^[^@[:space:]]+:[^@:[:space:]]+@sha256:[0-9a-f]{64}$ ]] || bad "lib.sh PostGIS image '$img' is not pinned by digest"
done
grep -n 'image:' "$compose" | grep -v '@sha256:' >/dev/null && bad "an image: line in docker-compose.yml has no @sha256 digest"
done_ok "compose: 4 services, 8 loopback contract ports, health checks, every image pinned by digest"

# Credentials are literals: nothing from the shell or .env.local is interpolated.
section
grep -nE '\$\{?(R2_|AWS_|DATABASE_|.*(KEY|SECRET|TOKEN|PASSWORD))' "$compose" >/dev/null &&
  bad "docker-compose.yml interpolates a credential from the environment"
grep -qE '^[[:space:]]+MINIO_ROOT_USER: todayz$' "$compose" && grep -qE '^[[:space:]]+MINIO_ROOT_PASSWORD: todayz-local$' "$compose" &&
  grep -qE '^[[:space:]]+MINIO_USER: todayz$' "$compose" && grep -qE '^[[:space:]]+MINIO_PASSWORD: todayz-local$' "$compose" ||
  bad "MinIO credentials in docker-compose.yml must be the literal local values"
grep -qx 'R2_ACCESS_KEY_ID=todayz' "$TZ_LOCAL_ENV" && grep -qx 'R2_SECRET_ACCESS_KEY=todayz-local' "$TZ_LOCAL_ENV" ||
  bad "R2_ACCESS_KEY_ID / R2_SECRET_ACCESS_KEY in local.env do not match MinIO in docker-compose.yml"
done_ok "compose credentials are literal and match local.env"

# --- Caddy -------------------------------------------------------------------------
# A throwaway HOME and XDG dirs, so adapt/validate never create a CA (or anything
# else) in the user's real Caddy storage.
section
if command -v caddy >/dev/null; then
  mkdir -p "$tmp/root/.cache/dev" "$tmp/home"
  caddy_tmp() { HOME="$tmp/home" XDG_DATA_HOME="$tmp/home/data" XDG_CONFIG_HOME="$tmp/home/config" TODAYZ_ROOT="$tmp/root" caddy "$@"; }
  if out="$(caddy_tmp adapt --config "$TZ_LOCAL/Caddyfile" --adapter caddyfile 2>"$tmp/adapt.err")"; then
    hosts="$(jq -r '[.apps.http.servers[].routes[]?.match[]?.host[]?] | unique | join(" ")' <<<"$out")"
    for h in todayz.localhost app.todayz.localhost business.todayz.localhost admin.todayz.localhost \
      api.todayz.localhost mocks.todayz.localhost grafana.todayz.localhost s3.todayz.localhost \
      img.todayz.localhost tiles.todayz.localhost; do
      [[ " $hosts " == *" $h "* ]] || bad "Caddyfile has no site for $h"
    done
    [[ "$(jq -r '.apps.pki.certificate_authorities.local.install_trust | tostring' <<<"$out")" == false ]] ||
      bad "Caddyfile must set skip_install_trust"
    [[ "$(jq -r '[.apps.tls.automation.policies[].issuers[].module] | unique | join(",")' <<<"$out")" == internal ]] ||
      bad "a site is not on tls internal"
    # local_only must be the first handler of every site, :80 included (order abort first).
    noabort="$(jq -c '[.apps.http.servers[].routes[] | select(.handle[0].routes[0].handle[0].abort != true) | .match[0].host[0]]' <<<"$out")"
    [[ "$noabort" == "[]" ]] || bad "these sites do not abort non-loopback clients first: $noabort"
    nsites="$(jq '[.apps.http.servers[].routes[]] | length' <<<"$out")"
    [[ "$nsites" -eq 11 ]] || bad "Caddyfile has $nsites site routes; expected 10 HTTPS hosts plus the :80 redirect"
  else
    bad "caddy adapt: $(tail -n 3 "$tmp/adapt.err")"
  fi
  caddy_tmp validate --config "$TZ_LOCAL/Caddyfile" --adapter caddyfile >/dev/null 2>"$tmp/validate.err" ||
    bad "caddy validate: $(tail -n 3 "$tmp/validate.err")"
else
  bad "caddy not installed (brew install caddy)"
fi
done_ok "Caddyfile validates; 10 hosts, all tls internal, no trust install, every site aborts non-loopback clients first"

# --- shellcheck --------------------------------------------------------------------
section
if command -v shellcheck >/dev/null; then
  shellcheck -x -P "$TZ_ROOT" "$TZ_LOCAL"/*.sh "$TZ_LOCAL"/test/*.sh && shellcheck -s sh "$TZ_LOCAL"/minio/*.sh ||
    bad "shellcheck infra/local"
else
  bad "shellcheck not installed"
fi
done_ok "shellcheck infra/local"

# --- local.env -----------------------------------------------------------------------
section
names() { grep -E '^[[:space:]]*(export[[:space:]]+)?[A-Z][A-Z0-9_]*=' "$1" | sed -E 's/^[[:space:]]*(export[[:space:]]+)?([A-Z][A-Z0-9_]*)=.*/\2/' | sort -u; }
example="$(names "$TZ_ROOT/.env.example")"
missing="$(comm -23 <(names "$TZ_LOCAL_ENV") <(echo "$example"))"
[[ -z "$missing" ]] || bad "local.env names not in .env.example: $(echo "$missing" | tr '\n' ' ')"
d18=(JWT_SIGNING_KEYS JWT_CURRENT_KID ORDER_CODE_ENC_KEY ORDER_CODE_HMAC_KEY BILL_QR_SIGNING_KEY PII_ENC_KEY PII_HASH_KEY DEVICE_SECRET_ENC_KEY TOTP_ENC_KEY SHARE_PREVIEW_KEY)
found=""
for k in "${d18[@]}"; do grep -qE "^[[:space:]]*(export[[:space:]]+)?$k=" "$TZ_LOCAL_ENV" && found+="$k "; done
[[ -z "$found" ]] || bad "local.env must not set D-18 app keys: $found"
# Provider secrets belong in .env.local only (R2_* here are MinIO's local stand-ins).
risky="$(names "$TZ_LOCAL_ENV" | grep -E '(_API_KEY|_AUTH_KEY|_TOKEN|_DSN|_PRIVATE_KEY|SECRET_KEY$)' | grep -vx 'R2_SECRET_ACCESS_KEY' || true)"
[[ -z "$risky" ]] || bad "local.env must not set provider secrets: $(echo "$risky" | tr '\n' ' ')"
if grep -qE '^[A-Z][A-Z0-9_]*=.*[[:space:]]#' "$TZ_LOCAL_ENV"; then bad "local.env has an inline comment (envtool does not strip them)"; fi
done_ok "local.env: names in .env.example, no D-18 app key, no provider secrets"

# The app connects as todayz_app, migrations as todayz_owner (both from init.sql), on 5433.
section
init="$TZ_LOCAL/postgres/init.sql"
url() { grep -E "^$1=" "$TZ_LOCAL_ENV" | head -n 1 | cut -d= -f2-; }
[[ "$(url DATABASE_URL)" == postgres://todayz_app:todayz_app@127.0.0.1:5433/todayz\?* ]] ||
  bad "DATABASE_URL must be postgres://todayz_app:todayz_app@127.0.0.1:5433/todayz?..."
[[ "$(url DATABASE_MIGRATE_URL)" == postgres://todayz_owner:todayz_owner@127.0.0.1:5433/todayz\?* ]] ||
  bad "DATABASE_MIGRATE_URL must be postgres://todayz_owner:todayz_owner@127.0.0.1:5433/todayz?..."
grep -q "CREATE ROLE todayz_app LOGIN PASSWORD 'todayz_app' NOSUPERUSER" "$init" &&
  grep -q "CREATE ROLE todayz_owner LOGIN PASSWORD 'todayz_owner' NOSUPERUSER" "$init" ||
  bad "init.sql must create todayz_app and todayz_owner (non-superuser) with the local.env passwords"
grep -q '127.0.0.1:5433:5432' "$compose" || bad "Postgres must publish 127.0.0.1:5433"
done_ok "DATABASE_URL (todayz_app) and DATABASE_MIGRATE_URL (todayz_owner) match init.sql and the compose port"

# --- launch.json -----------------------------------------------------------------------
section
lj="$TZ_ROOT/.claude/launch.json"
if [[ -f "$lj" ]] && jq -e '.configurations | length > 0' "$lj" >/dev/null 2>&1; then
  jq -e 'all(.configurations[]; has("name") and has("url"))' "$lj" >/dev/null ||
    bad ".claude/launch.json: every configuration needs name and url"
  # A preview url must be a bare origin (no path).
  [[ "$(jq -r '.configurations[] | select(.name == "stack-health") | .url' "$lj")" == https://todayz.localhost ]] ||
    bad ".claude/launch.json stack-health url must be the bare origin https://todayz.localhost"
else
  bad ".claude/launch.json missing or not valid JSON"
fi
done_ok ".claude/launch.json is valid"

exit $fail
