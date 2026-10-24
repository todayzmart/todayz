#!/usr/bin/env bash
# Build and upload the prod Pages sites (D-214): site → todayz-site, customer → todayz-customer,
# business → todayz-business, admin → todayz-admin. .github/workflows/deploy.yml runs `build`
# and `deploy` in separate jobs on every push to main; the lead can run both by hand.
#
#   scripts/deploy/pages.sh build  [site|customer|business|admin ...]   (default: all four)
#   scripts/deploy/pages.sh deploy [site|customer|business|admin ...]
#
# build needs the generated shared code (`make gen`) and node_modules; it writes .cache/pages/<app>
# with the public settings in infra/cloudflare/pages.env. deploy needs Node 22+, npx and
# CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID in the environment (never printed).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="$ROOT/.cache/pages"
WRANGLER_VERSION=4.152.0

usage() {
  echo "usage: scripts/deploy/pages.sh build|deploy [site|customer|business|admin ...]" >&2
  exit 2
}

# Security headers for the Vite apps (the site writes its own, with a CSP, in scripts/headers.mjs).
# No CSP here yet: the apps load Turnstile, images and tiles from other hosts; a CSP comes with
# its own e2e run. No includeSubDomains on HSTS: not every todayz.in host is ours to commit.
app_headers() {
  cat <<'EOF'
/*
  X-Content-Type-Options: nosniff
  Referrer-Policy: strict-origin-when-cross-origin
  X-Frame-Options: DENY
  Strict-Transport-Security: max-age=31536000
  Permissions-Policy: camera=(self), geolocation=(self), microphone=(), payment=()
/sw.js
  Cache-Control: no-cache
/assets/*
  Cache-Control: public, max-age=31536000, immutable
EOF
}

build() {
  local app="$1"
  echo "==> pages: build $app"
  (
    set -a
    # shellcheck disable=SC1091 # public build settings, checked in
    . "$ROOT/infra/cloudflare/pages.env"
    set +a
    pnpm -C "$ROOT" --filter "./$app" run build
  )
  rm -rf "${OUT:?}/$app"
  mkdir -p "$OUT"
  cp -R "$ROOT/$app/dist" "$OUT/$app"
  # Hidden source maps are for error symbolication, not for the public.
  find "$OUT/$app" -name '*.map' -delete
  if [[ "$app" != site ]]; then
    app_headers >"$OUT/$app/_headers"
  fi
}

deploy() {
  local app="$1" commit dirty=false
  [[ -f "$OUT/$app/index.html" ]] || {
    echo "pages: $OUT/$app has no build; run build first" >&2
    exit 1
  }
  commit="$(git -C "$ROOT" rev-parse HEAD)"
  if [[ -n "$(git -C "$ROOT" status --porcelain --untracked-files=no)" ]]; then
    dirty=true
  fi
  echo "==> pages: deploy $app"
  npx --yes "wrangler@$WRANGLER_VERSION" pages deploy "$OUT/$app" \
    --project-name "todayz-$app" --branch main \
    --commit-hash "$commit" --commit-dirty="$dirty"
}

[[ $# -ge 1 ]] || usage
cmd="$1"
shift
case "$cmd" in
  build | deploy) ;;
  *) usage ;;
esac
if [[ "$cmd" == deploy ]]; then
  for v in CLOUDFLARE_API_TOKEN CLOUDFLARE_ACCOUNT_ID; do
    if [[ -z "${!v:-}" ]]; then
      echo "pages: $v is not set" >&2
      exit 1
    fi
  done
fi

apps=("$@")
if [[ ${#apps[@]} -eq 0 ]]; then
  apps=(site customer business admin)
fi
for app in "${apps[@]}"; do
  case "$app" in
    site | customer | business | admin) "$cmd" "$app" ;;
    *)
      echo "pages: unknown app $app" >&2
      exit 1
      ;;
  esac
done
