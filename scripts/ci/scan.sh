#!/usr/bin/env bash
# Security scanners behind `make security` (W0-06; mk/90-ci.mk registers them on
# SECURITY_STEPS). Each uses a local binary when present, else its pinned image from
# versions.env (see TODAYZ_CI_TOOLS in lib.sh). Reports go to ci-reports/<scanner>/.
#
#   scan.sh gitleaks          secrets in the git history (the B1 fix: .gitleaks.toml has
#                             no path allowlist, so a committed .env.local is reported)
#   scan.sh trivy             filesystem scan: vulnerable dependencies, secrets, misconfig
#   scan.sh checkov [dir]     IaC policy scan of infra/ (or dir); skips with a message
#                             when there is nothing scannable yet
#   scan.sh zap [--if-up] [url...]
#                             ZAP baseline (passive) through Caddy on *.todayz.localhost.
#                             --if-up skips with a message when the local stack does not
#                             answer its health check (infra/local/health.sh)
#
# gitleaks in CI (CI or GITHUB_ACTIONS set):
#   - fails when there is no .git or the clone is shallow (no silent fallback to a
#     working-tree scan; the workflow checks out with fetch-depth: 0);
#   - on pull_request with BASE_REF set, reads .gitleaks.toml and .gitleaksignore from
#     origin/$BASE_REF, so a pull request cannot loosen its own scan;
#   - residual risk: an inline `gitleaks:allow` comment still suppresses a finding.
#     On a pull request a second, report-only pass over the new commits ignores those
#     comments and writes ci-reports/gitleaks/pr-allow-comments.sarif for the reviewer.
# Outside CI, a git worktree that a container cannot read falls back to `gitleaks dir`
# with scripts/ci/gitleaks-dir.toml (and says so).
#
# Trivy gates on HIGH and CRITICAL with --ignore-unfixed. On the weekly schedule
# (GITHUB_EVENT_NAME=schedule) or with TODAYZ_TRIVY_REPORT_ALL=1 it also writes a
# report-only pass without --ignore-unfixed.
#
# Needs the network for scanner databases and images, so it is never part of `make check`.
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/todayz-scan.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

# ---------------------------------------------------------------- gitleaks

gl_is_git() { git -C "$ci_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; }

# gl_config: write the config and ignore file for this run into $tmp; print the mode.
gl_prepare() {
  local base="${BASE_REF:-}"
  if [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" && -n "$base" ]]; then
    [[ "$base" =~ ^[A-Za-z0-9._/-]+$ && "$base" != *..* ]] || ci_die "gitleaks: BASE_REF is not a plain branch name"
    git -C "$ci_root" rev-parse --verify --quiet "origin/$base^{commit}" >/dev/null \
      || ci_die "gitleaks: origin/$base is missing (check out with fetch-depth: 0)"
    if ! git -C "$ci_root" show "origin/$base:.gitleaks.toml" >"$tmp/gitleaks.toml" 2>/dev/null; then
      printf '[extend]\nuseDefault = true\n' >"$tmp/gitleaks.toml"
      ci_log "gitleaks: origin/$base has no .gitleaks.toml; default rules"
    fi
    git -C "$ci_root" show "origin/$base:.gitleaksignore" >"$tmp/.gitleaksignore" 2>/dev/null || : >"$tmp/.gitleaksignore"
    ci_log "gitleaks: config and ignore list from origin/$base (the pull request cannot change its own scan)"
  else
    cp "$ci_root/.gitleaks.toml" "$tmp/gitleaks.toml"
    if [[ -f "$ci_root/.gitleaksignore" ]]; then cp "$ci_root/.gitleaksignore" "$tmp/.gitleaksignore"; else : >"$tmp/.gitleaksignore"; fi
  fi
}

scan_gitleaks() {
  local out
  out="$(ci_out gitleaks)"
  if ci_in_ci; then
    gl_is_git || ci_die "gitleaks: no git repository; CI scans the history only (no working-tree fallback)"
    [[ "$(git -C "$ci_root" rev-parse --is-shallow-repository)" == "false" ]] \
      || ci_die "gitleaks: shallow clone; check out with fetch-depth: 0 so the whole history is scanned"
  fi
  if ! gl_is_git; then
    ci_log "gitleaks: not a git repository; scanning the working tree (local only)"
    (cd "$ci_root/scripts/ci" && gitleaks dir "$ci_root" --config gitleaks-dir.toml --redact --no-banner \
      --report-format sarif --report-path "$out/gitleaks.sarif")
    return
  fi
  gl_prepare
  local -a args=(--redact --no-banner --report-format sarif)
  if ci_use_docker gitleaks; then
    if [[ -f "$ci_root/.git" ]]; then
      ci_in_ci && ci_die "gitleaks: a git worktree cannot be scanned from the image in CI"
      ci_log "gitleaks: git worktree; the image cannot read its history, so scanning the working tree"
      (cd "$ci_root/scripts/ci" && ci_docker_run "$GITLEAKS_IMAGE" -v "$ci_root:/repo:ro" -v "$out:/out" -w /repo/scripts/ci -- \
        dir /repo --config gitleaks-dir.toml "${args[@]}" --report-path /out/gitleaks.sarif)
      return
    fi
    ci_docker_run "$GITLEAKS_IMAGE" -v "$ci_root:/repo:ro" -v "$tmp:/cfg:ro" -v "$out:/out" \
      -e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0=/repo -- \
      git /repo --config /cfg/gitleaks.toml --gitleaks-ignore-path /cfg/.gitleaksignore "${args[@]}" \
      --report-path /out/gitleaks.sarif
  else
    gitleaks git "$ci_root" --config "$tmp/gitleaks.toml" --gitleaks-ignore-path "$tmp/.gitleaksignore" \
      "${args[@]}" --report-path "$out/gitleaks.sarif"
  fi
  ci_log "gitleaks: history clean"
  gl_allow_report "$out"
}

# gl_allow_report: on a pull request, list findings in its own commits that only an
# inline gitleaks:allow comment hides. Report only (legitimate test sentinels use it);
# the reviewer reads ci-reports/gitleaks/pr-allow-comments.sarif.
gl_allow_report() {
  local out="$1" base="${BASE_REF:-}"
  [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" && -n "$base" ]] || return 0
  local -a args=(--redact --no-banner --ignore-gitleaks-allow --exit-code 0 --report-format sarif
                 --log-opts "origin/$base..HEAD")
  if ci_use_docker gitleaks; then
    ci_docker_run "$GITLEAKS_IMAGE" -v "$ci_root:/repo:ro" -v "$tmp:/cfg:ro" -v "$out:/out" \
      -e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0=/repo -- \
      git /repo --config /cfg/gitleaks.toml --gitleaks-ignore-path /cfg/.gitleaksignore "${args[@]}" \
      --report-path /out/pr-allow-comments.sarif
  else
    gitleaks git "$ci_root" --config "$tmp/gitleaks.toml" --gitleaks-ignore-path "$tmp/.gitleaksignore" \
      "${args[@]}" --report-path "$out/pr-allow-comments.sarif"
  fi
  ci_log "gitleaks: gitleaks:allow comments in this pull request are listed in ci-reports/gitleaks/pr-allow-comments.sarif"
}

# ---------------------------------------------------------------- trivy

trivy_run() { # trivy_run <report file> <args...>
  local report="$1" cache="$ci_root/.cache/trivy"; shift
  mkdir -p "$cache"
  if ci_use_docker trivy; then
    ci_docker_run "$TRIVY_IMAGE" -v "$ci_root:/src:ro" -v "$cache:/cache" -e TRIVY_CACHE_DIR=/cache -w /src -- \
      "$@" . | tee "$report"
  else
    (cd "$ci_root" && TRIVY_CACHE_DIR="$cache" trivy "$@" .) | tee "$report"
  fi
}

scan_trivy() {
  local out
  out="$(ci_out trivy)"
  local -a common=(fs --scanners "vuln,secret,misconfig" --severity "HIGH,CRITICAL" --skip-version-check --no-progress
                   --skip-dirs '**/node_modules' --skip-dirs .tools --skip-dirs .cache --skip-dirs ci-reports
                   --skip-dirs .claude --skip-dirs .git --skip-dirs infra/local/data
                   --skip-dirs scripts/ci/testdata --skip-files .env.local)
  ci_log "trivy fs: gate on HIGH, CRITICAL with a fix available"
  trivy_run "$out/trivy.txt" "${common[@]}" --ignore-unfixed --exit-code 1
  if [[ "${GITHUB_EVENT_NAME:-}" == "schedule" || "${TODAYZ_TRIVY_REPORT_ALL:-0}" == "1" ]]; then
    ci_log "trivy fs: report-only pass including unfixed HIGH, CRITICAL"
    trivy_run "$out/trivy-all.txt" "${common[@]}" --exit-code 0
  fi
}

# ---------------------------------------------------------------- checkov

scan_checkov() {
  local dir="${1:-infra}" out
  dir="${dir%/}"
  [[ -d "$ci_root/$dir" ]] || { ci_log "checkov: $dir does not exist yet; nothing to scan"; return 0; }
  if [[ -z "$(find "$ci_root/$dir" -type f \( -name '*.tf' -o -name '*.tf.json' -o -name 'Dockerfile*' \
          -o -name '*.yml' -o -name '*.yaml' -o -name '*.json' \) -not -path '*/.terraform/*' \
          -not -path '*/data/*' -print -quit)" ]]; then
    ci_log "checkov: no IaC files under $dir yet; nothing to scan"
    return 0
  fi
  out="$(ci_out checkov)"
  ci_log "checkov -d $dir"
  local docker=0 report="$out"
  if ci_use_docker checkov; then docker=1; report=/out; fi
  local -a args=(-d "$dir" --quiet --compact --skip-download --skip-path '\.terraform' --skip-path 'infra/local/data'
                 --output cli --output junitxml --output-file-path "console,$report/checkov-junit.xml")
  if [[ $docker -eq 1 ]]; then
    ci_docker_run "$CHECKOV_IMAGE" -v "$ci_root:/src:ro" -v "$out:/out" -w /src -- "${args[@]}"
  else
    (cd "$ci_root" && checkov "${args[@]}")
  fi
}

# ---------------------------------------------------------------- zap

ZAP_DEFAULT_TARGETS="https://api.todayz.localhost https://app.todayz.localhost https://business.todayz.localhost https://admin.todayz.localhost https://todayz.localhost"

# zap_target_ok <url>: only the local stack. Loopback or *.todayz.localhost, http(s),
# an optional port and path; nothing else is ever scanned (S5).
zap_target_ok() {
  [[ "$1" =~ ^https?://(127\.0\.0\.1|localhost|\[::1\]|todayz\.localhost|[a-z0-9-]+\.todayz\.localhost)(:[0-9]{1,5})?(/[^[:space:]]*)?$ ]]
}

zap_host() { sed -E 's#^[a-z]+://##; s#[:/].*$##' <<<"$1"; }

caddy_ca() { # the Caddy local CA, from the same per-OS default infra/local/lib.sh uses
  (TODAYZ_ROOT="$ci_root" bash -c '. "$TODAYZ_ROOT/infra/local/lib.sh" >/dev/null 2>&1; printf "%s" "$TZ_CADDY_ROOT_CERT"')
}

scan_zap() {
  local if_up=0
  if [[ "${1:-}" == "--if-up" ]]; then if_up=1; shift; fi
  local -a targets
  if [[ $# -gt 0 ]]; then targets=("$@"); else read -r -a targets <<<"${ZAP_TARGETS:-$ZAP_DEFAULT_TARGETS}"; fi
  local t
  for t in "${targets[@]}"; do zap_target_ok "$t" || ci_die "zap: $t is not the local stack (loopback or *.todayz.localhost only)"; done

  if [[ $if_up -eq 1 ]] && ! "$ci_root/infra/local/health.sh" --no-html >/dev/null 2>&1; then
    ci_log "zap: skipped; the local stack does not answer its health check (make dev first)"
    return 0
  fi

  local out ca rc=0 any=0 name host
  out="$(ci_out zap)"
  ca="$(caddy_ca)"
  local -a curl_ca=() mount_ca=() net=() conf=()
  if [[ -f "$ca" ]]; then curl_ca=(--cacert "$ca"); mount_ca=(-v "$ca:/zap/caddy-root.crt:ro"); fi
  if [[ -f "$ci_root/e2e/security/zap-baseline.conf" ]]; then
    cp "$ci_root/e2e/security/zap-baseline.conf" "$out/zap-baseline.conf"
    conf=(-c zap-baseline.conf)
  fi
  # Linux: the host network, with each *.todayz.localhost name mapped to loopback.
  # macOS (Colima, Docker Desktop): each name mapped to the host gateway, where Caddy listens.
  local gw=127.0.0.1
  if [[ "$(uname -s)" == "Linux" ]]; then net=(--network host); else gw="host-gateway"; fi
  # Run as the calling user, never root, and without loosening the report folder (S5).
  # That user has no home in the image, so ZAP keeps its state in /tmp (-z "-dir ...").
  local -a user=(--user "$(id -u):$(id -g)" -e HOME=/tmp)

  for t in "${targets[@]}"; do
    # Any HTTP answer counts (curl without -f exits 0 on 4xx/5xx).
    if ! curl -sS -o /dev/null -m 5 ${curl_ca[@]+"${curl_ca[@]}"} "$t" 2>/dev/null; then
      ci_log "zap: $t is not answering; skipped"
      continue
    fi
    any=1
    host="$(zap_host "$t")"
    local -a hosts=()
    case "$host" in
      *todayz.localhost) hosts=(--add-host "$host:$gw") ;;
      127.0.0.1|localhost|"[") if [[ "$gw" == host-gateway ]]; then t="$(sed -E 's#//(127\.0\.0\.1|localhost|\[::1\])#//host.docker.internal#' <<<"$t")"; hosts=(--add-host host.docker.internal:host-gateway); fi ;;
    esac
    name="$(sed -E 's#^[a-z]+://##; s#[^A-Za-z0-9]+#_#g' <<<"$t")"
    ci_log "zap baseline: $t"
    set +e
    docker run --rm ${net[@]+"${net[@]}"} ${hosts[@]+"${hosts[@]}"} "${user[@]}" ${mount_ca[@]+"${mount_ca[@]}"} \
      -v "$out:/zap/wrk:rw" "$ZAP_IMAGE" \
      zap-baseline.py -t "$t" ${conf[@]+"${conf[@]}"} -r "$name.html" -J "$name.json" -m 2 -T 10 \
      -z "-dir /tmp/zap-home"
    local zrc=$?
    set -e
    # 0 pass, 2 warnings only (reported, triaged in zap-baseline.conf), 1 failures, 3 other error.
    case "$zrc" in
      0) ci_log "zap: $t clean" ;;
      2) ci_log "zap: $t has warnings; see ci-reports/zap/$name.html" ;;
      *) ci_log "zap: $t failed (exit $zrc)"; rc=1 ;;
    esac
  done
  [[ $any -eq 1 ]] || ci_die "zap: no target answered; start the stack with make dev"
  return $rc
}

cmd="${1:-}"; shift || true
case "$cmd" in
  gitleaks) scan_gitleaks ;;
  trivy) scan_trivy ;;
  checkov) scan_checkov "$@" ;;
  zap) scan_zap "$@" ;;
  zap-target-ok) for t in "$@"; do zap_target_ok "$t" || { echo "refused: $t"; exit 1; }; done; echo "ok" ;;
  *) echo "usage: scan.sh gitleaks|trivy|checkov [dir]|zap [--if-up] [url...]" >&2; exit 2 ;;
esac
