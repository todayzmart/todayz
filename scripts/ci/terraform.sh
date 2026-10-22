#!/usr/bin/env bash
# Terraform static checks (W0-06; D-16). Only static steps exist here: fmt -check,
# validate (after `init -backend=false`), tflint and checkov. Nothing touches a cloud
# account; that waits for Wave 5.
#
#   terraform.sh prefetch   `make setup` (setup-ci): pull the images, `init -backend=false`
#                           each module and `tflint --init`, while the network is open
#   terraform.sh static     `make check` (check-tf-static), offline: fmt, validate,
#                           tflint, checkov
#   terraform.sh fmt|validate|tflint|checkov   one step
#   terraform.sh <anything else>   refused (see tf below)
#
# Works on infra/terraform (TF_DIR overrides). With no .tf files every step prints why
# it skipped and exits 0. Uses local terraform/tflint/checkov when present, else their
# pinned images (TODAYZ_CI_TOOLS in lib.sh).
#
# tf() is the only way this repo runs terraform. It allows fmt, validate, version,
# `init -backend=false` and `providers lock`; it refuses plan, apply, destroy, import,
# state, taint, untaint, refresh, force-unlock, workspace, output, console, login,
# test and anything else unless TODAYZ_WAVE5=1 (W0-06 S7). scripts/ci/wflint enforces
# the same list on every script and workflow.
set -euo pipefail
# shellcheck source=scripts/ci/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

tf_rel="${TF_DIR:-infra/terraform}"
tf_rel="${tf_rel%/}"
tf_dir="$ci_root/$tf_rel"
plugin_cache="$ci_root/.cache/terraform-plugins"

present() {
  [[ -d "$tf_dir" ]] && [[ -n "$(find "$tf_dir" -name '*.tf' -not -path '*/.terraform/*' -print -quit)" ]]
}

skip() { ci_log "terraform: $tf_rel has no .tf files yet; skipping $1 (static checks only before Wave 5, D-16)"; }

# Module directories: every folder holding .tf files, relative to the repo root.
modules() {
  (cd "$ci_root" && find "$tf_rel" -name '*.tf' -not -path '*/.terraform/*' -exec dirname {} \; | sort -u)
}

# tf_allowed <args...>: succeed when the subcommand is static. Global options
# (-chdir=..., -help, -version) come before the subcommand.
tf_allowed() {
  local a sub="" rest=()
  for a in "$@"; do
    if [[ -z "$sub" ]]; then
      case "$a" in -*) continue ;; *) sub="$a"; continue ;; esac
    fi
    rest+=("$a")
  done
  case "$sub" in
    fmt|validate|version) return 0 ;;
    init) for a in ${rest[@]+"${rest[@]}"}; do [[ "$a" == "-backend=false" ]] && return 0; done; return 1 ;;
    providers) [[ "${rest[0]:-}" == "lock" ]] ;;
    *) return 1 ;;
  esac
}

tf() { # tf <args...>: terraform from the repo root
  if [[ "${TODAYZ_WAVE5:-0}" != "1" ]] && ! tf_allowed "$@"; then
    ci_die "terraform: refused \`terraform $*\`: only fmt, validate, version, init -backend=false and providers lock before Wave 5 (D-16; TODAYZ_WAVE5=1 lifts this)"
  fi
  mkdir -p "$plugin_cache"
  if ci_use_docker terraform; then
    ci_docker_run "$TERRAFORM_IMAGE" -v "$ci_root:/work" -w /work \
      -e TF_IN_AUTOMATION=1 -e TF_INPUT=0 -e TF_PLUGIN_CACHE_DIR=/work/.cache/terraform-plugins -- "$@"
  else
    (cd "$ci_root" && TF_IN_AUTOMATION=1 TF_INPUT=0 TF_PLUGIN_CACHE_DIR="$plugin_cache" terraform "$@")
  fi
}

tflint_run() { # tflint_run <args...>
  local -a cfg=()
  if ci_use_docker tflint; then
    [[ -f "$tf_dir/.tflint.hcl" ]] && cfg=(--config "/work/$tf_rel/.tflint.hcl")
    ci_docker_run "$TFLINT_IMAGE" -v "$ci_root:/work" -w /work -e TFLINT_PLUGIN_DIR=/work/.cache/tflint-plugins -- \
      --chdir="$tf_rel" "$@" ${cfg[@]+"${cfg[@]}"}
  else
    # An absolute --config path, so every module in a recursive run uses the same rules.
    [[ -f "$tf_dir/.tflint.hcl" ]] && cfg=(--config "$tf_dir/.tflint.hcl")
    (cd "$ci_root" && TFLINT_PLUGIN_DIR="$ci_root/.cache/tflint-plugins" tflint --chdir="$tf_rel" "$@" ${cfg[@]+"${cfg[@]}"})
  fi
}

do_prefetch() {
  present || { skip prefetch; return 0; }
  local img m
  for img in "$TERRAFORM_IMAGE" "$TFLINT_IMAGE" "$CHECKOV_IMAGE"; do
    if command -v docker >/dev/null 2>&1; then docker pull --quiet "$img" >/dev/null; fi
  done
  while IFS= read -r m; do
    ci_log "terraform init -backend=false $m (providers into .terraform, offline later)"
    tf -chdir="$m" init -backend=false -input=false -no-color >/dev/null
  done < <(modules)
  tflint_run --init
}

do_fmt() {
  present || { skip fmt; return 0; }
  ci_group "terraform fmt -check"
  tf fmt -check -recursive -diff "$tf_rel"
  ci_endgroup
}

do_validate() {
  present || { skip validate; return 0; }
  local m
  while IFS= read -r m; do
    ci_group "terraform validate $m"
    if [[ ! -d "$ci_root/$m/.terraform" ]]; then
      if [[ "${TODAYZ_NO_EGRESS:-}" == "1" ]]; then ci_die "terraform: $m is not initialised; run make setup (it prefetches providers)"; fi
      tf -chdir="$m" init -backend=false -input=false -no-color >/dev/null
    fi
    tf -chdir="$m" validate -no-color
    ci_endgroup
  done < <(modules)
}

do_tflint() {
  present || { skip tflint; return 0; }
  ci_group "tflint --recursive $tf_rel"
  if [[ "${TODAYZ_NO_EGRESS:-}" != "1" ]]; then tflint_run --init; fi
  tflint_run --recursive --format compact
  ci_endgroup
}

do_checkov() {
  present || { skip checkov; return 0; }
  "$ci_root/scripts/ci/scan.sh" checkov "$tf_rel"
}

case "${1:-}" in
  prefetch) do_prefetch ;;
  static) do_fmt; do_validate; do_tflint; do_checkov ;;
  fmt) do_fmt ;;
  validate) do_validate ;;
  tflint) do_tflint ;;
  checkov) do_checkov ;;
  "") echo "usage: terraform.sh prefetch|static|fmt|validate|tflint|checkov" >&2; exit 2 ;;
  *) tf "$@" ;;
esac
