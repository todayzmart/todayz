#!/usr/bin/env bash
# Build and release the backend to Elastic Beanstalk (D-247). One image, `todayz`, runs the api,
# the worker and the migrate task. A release is: the image in ECR (immutable tag), a migrate
# task on that image, and two application versions <tag>-api and <tag>-worker whose bundle is a
# small docker-compose file. Terraform creates the environments once; every release after that
# goes through this script (by hand or from .github/workflows/deploy-backend.yml).
#
#   scripts/deploy/beanstalk.sh build                 # generate code, build linux/arm64, save .cache/backend/image.tar
#   scripts/deploy/beanstalk.sh push                  # push that image to ECR (skipped if the tag is there)
#   scripts/deploy/beanstalk.sh db-bootstrap          # ops task: roles, extensions, privileges (once; safe to repeat)
#   scripts/deploy/beanstalk.sh migrate  [TAG]        # ops task: todayz migrate up on that image
#   scripts/deploy/beanstalk.sh release  [TAG]        # upload the bundles, create the application versions
#   scripts/deploy/beanstalk.sh deploy   [TAG]        # worker, then api, to that release; waits for each
#   scripts/deploy/beanstalk.sh ci-deploy             # GitHub Actions: OIDC role, then push, migrate, release, deploy
#
# TAG defaults to the one `build` wrote (.cache/backend/tag). Rollback is `deploy OLD_TAG`
# (docs/qa/deploy-prep/RUNBOOK.md); migrations only move forward, so a rollback keeps the newer
# schema. Needs the aws CLI v2, docker with buildx, jq and zip. Prints names, never values.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="$ROOT/.cache/backend"
ENV_NAME="${TODAYZ_ENV:-prod}"
export AWS_REGION="${AWS_REGION:-ap-south-1}"
ACCOUNT="${TODAYZ_AWS_ACCOUNT_ID:-061292478202}"
REPOSITORY=todayz-backend
REGISTRY="$ACCOUNT.dkr.ecr.$AWS_REGION.amazonaws.com"
IMAGE="$REGISTRY/$REPOSITORY"
DEPLOY_ROLE_ARN="${TODAYZ_DEPLOY_ROLE_ARN:-arn:aws:iam::$ACCOUNT:role/todayz-$ENV_NAME-github-deploy}"

usage() {
  sed -n '8,14p' "${BASH_SOURCE[0]}" | sed 's/^# *//' >&2
  exit 2
}

log() { echo "==> beanstalk: $*"; }
die() {
  echo "beanstalk: $*" >&2
  exit 1
}

tag_arg() {
  if [[ -n "${1:-}" ]]; then
    echo "$1"
  elif [[ -f "$OUT/tag" ]]; then
    cat "$OUT/tag"
  else
    die "no TAG given and no $OUT/tag; run build first"
  fi
}

# The ops settings Terraform writes (modules/stack/ops.tf). load_cfg runs in the main shell (not
# inside $(...)), so the parameter is read once per command.
CFG=""
load_cfg() {
  [[ -n "$CFG" ]] || CFG="$(aws ssm get-parameter --name "/todayz/$ENV_NAME/ops/run-task" --query Parameter.Value --output text)"
}
cfg() { jq -r ".$1" <<<"$CFG"; }

check_account() {
  local caller
  caller="$(aws sts get-caller-identity --query Account --output text)"
  [[ "$caller" == "$ACCOUNT" ]] || die "these credentials are for another account, not $ACCOUNT"
}

cmd_build() {
  local commit tag
  commit="$(git -C "$ROOT" rev-parse HEAD)"
  tag="g$(git -C "$ROOT" rev-parse --short=12 HEAD)"
  if [[ -n "$(git -C "$ROOT" status --porcelain --untracked-files=no)" ]]; then
    [[ "${TODAYZ_ALLOW_DIRTY:-}" == 1 ]] || die "the work tree has changes; commit first (TODAYZ_ALLOW_DIRTY=1 builds a -dirty tag)"
    tag="$tag-dirty-$(date -u +%Y%m%d%H%M%S)"
  fi
  # Only the generated code the image needs (event map, contract, sqlc, problem codes).
  log "generate"
  make -C "$ROOT" setup-node setup-contract-codegen
  mkdir -p "$ROOT/.tools/bin"
  go -C "$ROOT/backend/tools" build -trimpath -o "$ROOT/.tools/bin/" github.com/sqlc-dev/sqlc/cmd/sqlc
  make -C "$ROOT" gen-events gen-contract gen-backend
  log "build $REPOSITORY:$tag (linux/arm64)"
  mkdir -p "$OUT"
  docker buildx build --platform linux/arm64 \
    --build-arg "VERSION=$tag" --build-arg "COMMIT=$commit" \
    --build-arg "BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --tag "$IMAGE:$tag" --output "type=docker,dest=$OUT/image.tar" "$ROOT/backend"
  echo "$tag" >"$OUT/tag"
  log "built $tag ($OUT/image.tar)"
}

cmd_push() {
  local tag
  tag="$(tag_arg "${1:-}")"
  check_account
  if aws ecr describe-images --repository-name "$REPOSITORY" --image-ids "imageTag=$tag" >/dev/null 2>&1; then
    log "$REPOSITORY:$tag is already in ECR"
    return
  fi
  if ! docker image inspect "$IMAGE:$tag" >/dev/null 2>&1; then
    [[ -f "$OUT/image.tar" ]] || die "no local image $tag and no $OUT/image.tar"
    docker load --input "$OUT/image.tar" >/dev/null
  fi
  aws ecr get-login-password | docker login --username AWS --password-stdin "$REGISTRY" >/dev/null
  log "push $REPOSITORY:$tag"
  docker push --quiet "$IMAGE:$tag" >/dev/null
}

# run_task FAMILY_OR_ARN CONTAINER: start an ops task, wait, print its log, fail on a non-zero exit.
run_task() {
  local taskdef="$1" container="$2" net task_arn task_id code reason
  net="$(jq -c '{awsvpcConfiguration: {subnets: .subnets, securityGroups: .securityGroups, assignPublicIp: "DISABLED"}}' <<<"$CFG")"
  task_arn="$(aws ecs run-task --cluster "$(cfg cluster)" --launch-type FARGATE \
    --task-definition "$taskdef" --network-configuration "$net" \
    --started-by "beanstalk.sh" --query 'tasks[0].taskArn' --output text)"
  [[ -n "$task_arn" && "$task_arn" != "None" ]] || die "run-task started nothing for $taskdef"
  task_id="${task_arn##*/}"
  log "task $container $task_id started; waiting"
  aws ecs wait tasks-stopped --cluster "$(cfg cluster)" --tasks "$task_arn"
  read -r code reason < <(aws ecs describe-tasks --cluster "$(cfg cluster)" --tasks "$task_arn" \
    --query 'tasks[0].[containers[0].exitCode, stoppedReason]' --output text)
  aws logs get-log-events --log-group-name "$(cfg logGroup)" \
    --log-stream-name "ops/$container/$task_id" --start-from-head \
    --query 'events[].message' --output text 2>/dev/null | tr '\t' '\n' || true
  [[ "$code" == 0 ]] || die "task $container exited ${code:-without a code} ($reason)"
  log "task $container done"
}

cmd_db_bootstrap() {
  check_account && load_cfg
  run_task "$(cfg dbBootstrap)" db-bootstrap
}

cmd_migrate() {
  local tag family current revision
  tag="$(tag_arg "${1:-}")"
  check_account && load_cfg
  family="$(cfg migrate)"
  # A new revision of the migrate task on this release's image; everything else is copied.
  current="$(aws ecs describe-task-definition --task-definition "$family" --query taskDefinition --output json)"
  jq --arg image "$IMAGE:$tag" '{
      family, executionRoleArn, networkMode, requiresCompatibilities, cpu, memory, runtimePlatform,
      containerDefinitions: (.containerDefinitions | map(.image = $image))
    }' <<<"$current" >"$OUT/migrate-taskdef.json"
  revision="$(aws ecs register-task-definition --cli-input-json "file://$OUT/migrate-taskdef.json" \
    --query 'taskDefinition.taskDefinitionArn' --output text)"
  log "migrate on $tag"
  run_task "$revision" migrate
}

bundle() {
  local role="$1" tag="$2" dir="$3" command stop
  command="$role"
  # Literal ${...}: compose fills it from .env, where Terraform's timings.tf value lands.
  # shellcheck disable=SC2016
  stop='${CONTAINER_STOP_TIMEOUT_SECONDS:-120}'
  # shellcheck disable=SC2016
  [[ "$role" == worker ]] && stop='${CONTAINER_STOP_TIMEOUT_SECONDS:-180}'
  mkdir -p "$dir/.platform/hooks/prebuild" "$dir/.platform/confighooks/prebuild"
  # Beanstalk writes the environment properties and secrets into .env next to this file.
  # network_mode host: no nginx (ProxyServer none), the container binds the app port itself and
  # reaches the instance metadata service for the SMS signer's credentials (D-213).
  cat >"$dir/docker-compose.yml" <<EOF
services:
  $role:
    image: $IMAGE:$tag
    command: ["$command"]
    network_mode: host
    env_file: [.env]
    environment:
      APP_VERSION: $tag
    stop_grace_period: ${stop}s
    restart: unless-stopped
EOF
  cat >"$dir/.platform/hooks/prebuild/01-ecr-login.sh" <<EOF
#!/bin/bash
# Log the instance's Docker into ECR with the instance role before the image is pulled.
set -euo pipefail
aws ecr get-login-password --region $AWS_REGION | docker login --username AWS --password-stdin $REGISTRY >/dev/null
EOF
  chmod 755 "$dir/.platform/hooks/prebuild/01-ecr-login.sh"
  cp "$dir/.platform/hooks/prebuild/01-ecr-login.sh" "$dir/.platform/confighooks/prebuild/"
}

cmd_release() {
  local tag app bucket role label dir
  tag="$(tag_arg "${1:-}")"
  check_account && load_cfg
  app="$(cfg application)"
  bucket="$(cfg versionsBucket)"
  for role in api worker; do
    label="$tag-$role"
    if [[ "$(aws elasticbeanstalk describe-application-versions --application-name "$app" \
      --version-labels "$label" --query 'length(ApplicationVersions)' --output text)" != 0 ]]; then
      log "application version $label exists"
      continue
    fi
    dir="$OUT/bundle-$role"
    rm -rf "$dir" "$OUT/$label.zip"
    bundle "$role" "$tag" "$dir"
    (cd "$dir" && zip -qr "$OUT/$label.zip" docker-compose.yml .platform)
    aws s3 cp --quiet "$OUT/$label.zip" "s3://$bucket/$tag/$label.zip"
    aws elasticbeanstalk create-application-version --application-name "$app" \
      --version-label "$label" --description "TodayZ $role $tag" \
      --source-bundle "S3Bucket=$bucket,S3Key=$tag/$label.zip" --process >/dev/null
    log "application version $label created"
  done
}

deploy_env() {
  local env="$1" label="$2" app="$3" current health
  current="$(aws elasticbeanstalk describe-environments --application-name "$app" --environment-names "$env" \
    --query 'Environments[0].VersionLabel' --output text)"
  if [[ "$current" == "$label" ]]; then
    log "$env already runs $label"
    return
  fi
  log "deploy $label to $env"
  aws elasticbeanstalk update-environment --application-name "$app" --environment-name "$env" \
    --version-label "$label" >/dev/null
  aws elasticbeanstalk wait environment-updated --application-name "$app" --environment-names "$env" \
    --version-label "$label"
  read -r current health < <(aws elasticbeanstalk describe-environments --application-name "$app" \
    --environment-names "$env" --query 'Environments[0].[VersionLabel, HealthStatus]' --output text)
  [[ "$current" == "$label" ]] || die "$env is on $current, not $label (Beanstalk rolled back; see its events)"
  log "$env on $label, health $health"
}

cmd_deploy() {
  local tag app
  tag="$(tag_arg "${1:-}")"
  check_account && load_cfg
  app="$(cfg application)"
  # Worker first: it is the one that runs jobs written by the new code's schema.
  deploy_env "$(cfg workerEnvironment)" "$tag-worker" "$app"
  deploy_env "$(cfg apiEnvironment)" "$tag-api" "$app"
}

# GitHub Actions only: exchange the job's OIDC token for the deploy role (no stored AWS key, no
# third-party action), then run the release.
cmd_ci_deploy() {
  local token_file creds key secret session
  [[ -n "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" && -n "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]] ||
    die "ci-deploy needs the job's id-token: write permission"
  umask 077
  token_file="$OUT/oidc-token"
  trap 'rm -f "$OUT/oidc-token"' EXIT
  curl -fsS -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
    "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=sts.amazonaws.com" | jq -r .value >"$token_file"
  creds="$(aws sts assume-role-with-web-identity --role-arn "$DEPLOY_ROLE_ARN" \
    --role-session-name "github-${GITHUB_RUN_ID:-local}" --web-identity-token "file://$token_file" \
    --duration-seconds 3600 --query 'Credentials.[AccessKeyId, SecretAccessKey, SessionToken]' --output text)"
  read -r key secret session <<<"$creds"
  creds=""
  echo "::add-mask::$secret"
  echo "::add-mask::$session"
  export AWS_ACCESS_KEY_ID="$key" AWS_SECRET_ACCESS_KEY="$secret" AWS_SESSION_TOKEN="$session"
  rm -f "$token_file"
  local tag
  tag="$(tag_arg)"
  cmd_push "$tag"
  cmd_migrate "$tag"
  cmd_release "$tag"
  cmd_deploy "$tag"
}

[[ $# -ge 1 ]] || usage
mkdir -p "$OUT"
cmd="$1"
shift
case "$cmd" in
  build) cmd_build ;;
  push) cmd_push "$@" ;;
  db-bootstrap) cmd_db_bootstrap ;;
  migrate) cmd_migrate "$@" ;;
  release) cmd_release "$@" ;;
  deploy) cmd_deploy "$@" ;;
  ci-deploy) cmd_ci_deploy ;;
  *) usage ;;
esac
