#!/bin/sh
# Runs inside the pgsty/mc container (docker compose run --rm minio-init <cmd>).
#   buckets  create images and tiles (anonymous GetObject, no listing), private
#            (no anonymous access) and a small probe object in each
#   tiles    upload /pmtiles/*.pmtiles to the tiles bucket when missing or a different size
# Never prints keys: mc reads them from MC_HOST_local, built from the env.
set -eu

: "${MINIO_URL:?}" "${MINIO_USER:?}" "${MINIO_PASSWORD:?}"
scheme="${MINIO_URL%%://*}"
hostport="${MINIO_URL#*://}"
MC_HOST_local="${scheme}://${MINIO_USER}:${MINIO_PASSWORD}@${hostport}"
export MC_HOST_local

probe_body="todayz local probe"

buckets() {
  for b in images private tiles; do
    mc mb --quiet --ignore-existing "local/$b" >/dev/null
  done
  # Anonymous GetObject only, like an R2 public bucket. mc's "download" preset
  # also allows ListBucket, which would let anyone list every key.
  for b in images tiles; do
    printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}\n' "$b" >"/tmp/policy-$b.json"
    mc anonymous set-json "/tmp/policy-$b.json" "local/$b" >/dev/null
  done
  mc anonymous set none local/private >/dev/null
  # Probe objects: the health check reads them anonymously to prove each policy.
  for b in images private tiles; do
    printf '%s\n' "$probe_body" | mc pipe --quiet "local/$b/_health/probe.txt" >/dev/null
  done
  echo "buckets ok: images and tiles (anonymous GetObject, no listing), private (no anonymous access)"
}

tiles() {
  found=0
  for f in /pmtiles/*.pmtiles; do
    [ -f "$f" ] || continue
    found=1
    name="$(basename "$f")"
    want="$(wc -c <"$f" | tr -d ' ')"
    # The mc image has no sed/awk/jq: cut "size":N out of the JSON with parameter expansion.
    have=""
    if json="$(mc stat --json "local/tiles/$name" 2>/dev/null)"; then
      case "$json" in *'"size":'*) have="${json#*\"size\":}"; have="${have%%[!0-9]*}" ;; esac
    fi
    if [ "$have" = "$want" ]; then
      echo "tiles: $name already in bucket"
    else
      mc cp --quiet --attr "Content-Type=application/vnd.pmtiles" "$f" "local/tiles/$name" >/dev/null
      echo "tiles: uploaded $name ($want bytes)"
    fi
  done
  [ "$found" = 1 ] || echo "warning: no .pmtiles files in infra/local/pmtiles (run make setup with network access); maps will have no tiles"
}

case "${1:-buckets}" in
  buckets) buckets ;;
  tiles) tiles ;;
  *) echo "usage: minio-init.sh buckets|tiles" >&2; exit 2 ;;
esac
