#!/usr/bin/env bash
# One shared PostGIS server for Go integration tests (opt-in).
#
#   scripts/testpg.sh up      start it (idempotent) and write .cache/testpg.url
#   scripts/testpg.sh down    remove it (its data is tmpfs, so every leftover database goes too)
#   scripts/testpg.sh status
#
# Then, in any checkout or worktree whose testkit has shared mode:
#   export TODAYZ_TEST_PG_URL_FILE=<main checkout>/.cache/testpg.url
# and `go test` uses this server instead of a container per test package.
# The URL holds a random local password: the file is mode 600 and nothing here
# prints it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAME=todayz-testpg
PORT="${TODAYZ_TESTPG_PORT:-5439}"
URL_FILE="$ROOT/.cache/testpg.url"

if [[ -z "${DOCKER_HOST:-}" ]]; then
  DOCKER_HOST="$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)"
  [[ -n "$DOCKER_HOST" ]] && export DOCKER_HOST
fi

running() { [[ "$(docker inspect -f '{{.State.Running}}' "$NAME" 2>/dev/null || true)" == "true" ]]; }

case "${1:-}" in
  up)
    if running && [[ -s "$URL_FILE" ]]; then
      echo "testpg: already up on 127.0.0.1:$PORT"
    else
      docker rm -f "$NAME" >/dev/null 2>&1 || true
      # shellcheck source=../infra/local/lib.sh
      img="${TODAYZ_POSTGIS_IMAGE:-$(source "$ROOT/infra/local/lib.sh" && echo "$TODAYZ_POSTGIS_IMAGE")}"
      pw="$(openssl rand -hex 16)"
      docker run -d --name "$NAME" --label in.todayz.testkit=shared \
        -e POSTGRES_PASSWORD="$pw" -p "127.0.0.1:$PORT:5432" \
        --tmpfs /var/lib/postgresql/data:rw --shm-size 256m \
        "$img" -c fsync=off -c synchronous_commit=off -c full_page_writes=off \
        -c max_connections=1000 -c shared_buffers=256MB >/dev/null
      for _ in $(seq 1 60); do
        docker exec "$NAME" pg_isready -U postgres -q 2>/dev/null && break
        sleep 1
      done
      docker exec "$NAME" pg_isready -U postgres -q || { echo "testpg: server did not become ready" >&2; exit 1; }
      mkdir -p "$ROOT/.cache"
      (umask 077 && printf 'postgres://postgres:%s@127.0.0.1:%s/postgres?sslmode=disable\n' "$pw" "$PORT" >"$URL_FILE")
      echo "testpg: up on 127.0.0.1:$PORT"
    fi
    echo "export TODAYZ_TEST_PG_URL_FILE=$URL_FILE"
    ;;
  down)
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    rm -f "$URL_FILE"
    echo "testpg: down"
    ;;
  status)
    if running; then
      echo "testpg: up on 127.0.0.1:$PORT, $(docker exec "$NAME" psql -U postgres -Atc "select count(*) from pg_database where datname like 'tz\_%'") test databases"
    else
      echo "testpg: down"
    fi
    ;;
  *)
    echo "usage: scripts/testpg.sh up|down|status" >&2
    exit 2
    ;;
esac
