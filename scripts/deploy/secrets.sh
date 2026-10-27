#!/usr/bin/env bash
# Put values into the Secrets Manager secrets Terraform created empty (modules/secrets, D-256).
# The lead runs it with .env.cloud.local loaded. No value is ever printed, put on a command line
# or written outside a 0600 temp file that is removed on exit; output is names only.
#
#   scripts/deploy/secrets.sh status                  # NAME set|missing for every secret
#   scripts/deploy/secrets.sh app-keys                # generate the 10 app keys (envtool); fills only missing ones
#   scripts/deploy/secrets.sh database                # DATABASE_URL (todayz_app) and DATABASE_MIGRATE_URL (todayz_owner)
#   scripts/deploy/secrets.sh put NAME                # value from stdin (a pipe, or typed without echo)
#   scripts/deploy/secrets.sh share-preview-to-pages  # copy SHARE_PREVIEW_KEY to the customer Pages project
#
# A secret that already has a value is never replaced: the app keys encrypt stored data (PII,
# order codes, device secrets), so a new value would make that data unreadable. Rotation is a
# runbook of its own (docs/runbooks/), not this script. `put` is the exception: it is how a
# provider key is set or changed on purpose.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_NAME="${TODAYZ_ENV:-prod}"
PREFIX="todayz/$ENV_NAME"
export AWS_REGION="${AWS_REGION:-ap-south-1}"
WRANGLER_VERSION=4.152.0

APP_KEYS=(JWT_SIGNING_KEYS JWT_CURRENT_KID ORDER_CODE_ENC_KEY ORDER_CODE_HMAC_KEY BILL_QR_SIGNING_KEY
  PII_ENC_KEY PII_HASH_KEY DEVICE_SECRET_ENC_KEY SHARE_PREVIEW_KEY)
OTHER=(DATABASE_URL DATABASE_MIGRATE_URL ANTHROPIC_API_KEY SARVAM_API_KEY TURNSTILE_SECRET_KEY
  R2_ACCESS_KEY_ID R2_SECRET_ACCESS_KEY SENTRY_DSN VAPID_PRIVATE_KEY WHATSAPP_API_KEY
  OTEL_EXPORTER_OTLP_HEADERS GEOCODER_API_KEY)

usage() {
  sed -n '5,9p' "${BASH_SOURCE[0]}" | sed 's/^# *//' >&2
  exit 2
}

umask 077
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

known() {
  local n
  for n in "${APP_KEYS[@]}" "${OTHER[@]}"; do [[ "$n" == "$1" ]] && return 0; done
  return 1
}

has_value() {
  local v
  v="$(aws secretsmanager describe-secret --secret-id "$PREFIX/$1" --query 'VersionIdsToStages' --output text)"
  [[ -n "$v" && "$v" != "None" ]]
}

# put_file NAME FILE: the value travels as file://, never as an argument.
put_file() {
  aws secretsmanager put-secret-value --secret-id "$PREFIX/$1" --secret-string "file://$2" >/dev/null
  rm -f "$2"
}

cmd_status() {
  local n
  for n in "${APP_KEYS[@]}" "${OTHER[@]}"; do
    if has_value "$n"; then echo "$n set"; else echo "$n missing"; fi
  done
}

cmd_app_keys() {
  local missing=() n line val
  for n in "${APP_KEYS[@]}"; do has_value "$n" || missing+=("$n"); done
  if [[ ${#missing[@]} -eq 0 ]]; then
    echo "app-keys: all 10 set; nothing to do"
    return
  fi
  # The signing key set and its kid go together; a half-set pair needs a person.
  local s=0 k=0
  has_value JWT_SIGNING_KEYS && s=1
  has_value JWT_CURRENT_KID && k=1
  if [[ $s -ne $k ]]; then
    echo "app-keys: only one of JWT_SIGNING_KEYS and JWT_CURRENT_KID is set; fix that pair by hand (docs/runbooks)" >&2
    exit 1
  fi
  go -C "$ROOT/scripts/envtool" build -o "$WORK/envtool" .
  "$WORK/envtool" generate -env "$WORK/keys.env" >/dev/null
  for n in "${missing[@]}"; do
    line="$(grep -m1 "^$n=" "$WORK/keys.env")"
    val="${line#*=}"
    val="${val#\'}"
    val="${val%\'}"
    printf '%s' "$val" >"$WORK/value"
    put_file "$n" "$WORK/value"
    echo "app-keys: $n set"
  done
  val=""
  rm -f "$WORK/keys.env"
}

cmd_database() {
  local id="todayz-$ENV_NAME" host port name user pw
  read -r host port < <(aws rds describe-db-instances --db-instance-identifier "$id" \
    --query 'DBInstances[0].Endpoint.[Address,Port]' --output text)
  [[ -n "$host" && "$host" != "None" ]] || {
    echo "database: $id has no endpoint yet" >&2
    exit 1
  }
  for name in DATABASE_URL DATABASE_MIGRATE_URL; do
    if has_value "$name"; then
      echo "database: $name already set; kept"
      continue
    fi
    user=todayz_app
    [[ "$name" == DATABASE_MIGRATE_URL ]] && user=todayz_owner
    # Hex only: no URL escaping, and the bootstrap task reads it back out of the URL.
    pw="$(openssl rand -hex 32)"
    printf 'postgres://%s:%s@%s:%s/todayz?sslmode=require' "$user" "$pw" "$host" "$port" >"$WORK/value"
    pw=""
    put_file "$name" "$WORK/value"
    echo "database: $name set ($user)"
  done
  echo "database: run scripts/deploy/beanstalk.sh db-bootstrap so the roles get these passwords"
}

cmd_put() {
  local n="$1" val
  known "$n" || {
    echo "put: $n is not a TodayZ secret name" >&2
    exit 1
  }
  if [[ -t 0 ]]; then
    read -rs -p "value for $n (not echoed): " val
    echo >&2
  else
    val="$(cat)"
  fi
  [[ -n "$val" ]] || {
    echo "put: empty value for $n; nothing written" >&2
    exit 1
  }
  printf '%s' "$val" >"$WORK/value"
  val=""
  put_file "$n" "$WORK/value"
  echo "put: $n set"
}

cmd_share_preview_to_pages() {
  for v in CLOUDFLARE_API_TOKEN CLOUDFLARE_ACCOUNT_ID; do
    [[ -n "${!v:-}" ]] || {
      echo "share-preview-to-pages: $v is not set" >&2
      exit 1
    }
  done
  aws secretsmanager get-secret-value --secret-id "$PREFIX/SHARE_PREVIEW_KEY" \
    --query SecretString --output text |
    npx --yes "wrangler@$WRANGLER_VERSION" pages secret put SHARE_PREVIEW_KEY --project-name todayz-customer >/dev/null
  echo "share-preview-to-pages: SHARE_PREVIEW_KEY set on todayz-customer"
}

[[ $# -ge 1 ]] || usage
case "$1" in
  status) cmd_status ;;
  app-keys) cmd_app_keys ;;
  database) cmd_database ;;
  put)
    [[ $# -eq 2 ]] || usage
    cmd_put "$2"
    ;;
  share-preview-to-pages) cmd_share_preview_to_pages ;;
  *) usage ;;
esac
