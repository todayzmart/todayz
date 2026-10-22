#!/usr/bin/env bash
# tiles.sh: local map tiles for `make dev` (W0-02).
#
#   tiles.sh install   install the pinned pmtiles CLI into .tools/bin
#   tiles.sh fetch     install, then extract the city archives (keeps files that verify)
#   tiles.sh status    list the local archives, their sizes and whether they verify
#
# Runs from `make setup` only (needs the internet), never from `make check`.
# `make dev` uploads infra/local/pmtiles/*.pmtiles to the MinIO `tiles` bucket.
# A failed download, a file that does not verify, or a missing Go toolchain
# prints a warning and exits 0: the stack runs without maps.
set -euo pipefail
# shellcheck source=infra/local/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

# Pinned versions. Protomaps keeps old daily builds for a while, but not
# forever: the build below answered HTTP 200 on 2026-10-22. When it is gone,
# fetch warns; pick a newer build from https://maps.protomaps.com/builds/,
# bump TILES_BUILD and every sha256 below together (extract is deterministic:
# the same build, bbox and maxzoom give the same bytes), then re-run make setup.
PMTILES_MODULE="github.com/protomaps/go-pmtiles"
PMTILES_VERSION="v1.31.2"
TILES_BUILD="20260811"
TILES_SOURCE="${TODAYZ_TILES_SOURCE:-https://build.protomaps.com/$TILES_BUILD.pmtiles}"
TILES_MAXZOOM="${TODAYZ_TILES_MAXZOOM:-14}"
TILES_MAX_MB=150

TOOLS="$TZ_ROOT/.tools/bin"
PMTILES="$TOOLS/pmtiles"
OUT="$TZ_LOCAL/pmtiles"

# name  bbox (min_lon,min_lat,max_lon,max_lat): the metro area plus a margin,
# and the sha256 of its extract from TILES_BUILD at z0-14.
CITIES=(
  "bengaluru 77.30,12.70,77.95,13.25 f17460b2184f42556625e3335d972aa06f27cf652f806f0afe06b88e77295ea0"
  "hyderabad 78.15,17.20,78.75,17.65 33bdb12db0e3f3785aab1c9fe03fde40e9cadf97880c7985a82a76e828a2bf77"
  "chennai 79.95,12.75,80.35,13.30 a037337c1d84cbd28a8d2a279e200934893e2fe062a03571d4dffbae51019a0b"
)

warn() { echo "tiles: WARNING: $*" >&2; }

# verify FILE SHA256: a PMTiles v3 archive within budget, and the pinned bytes
# when the default build and zoom are used (an override is checked by header only).
verify() {
  local file="$1" want="$2" mb
  [[ -s "$file" ]] || return 1
  [[ "$(head -c 7 "$file")" == PMTiles && "$(head -c 8 "$file" | tail -c 1 | od -An -tu1 | tr -d ' ')" == 3 ]] || {
    warn "$(basename "$file") is not a PMTiles v3 archive"; return 1; }
  mb="$(size_mb "$file")"
  [[ $mb -le $TILES_MAX_MB ]] || { warn "$(basename "$file") is $mb MB, above the $TILES_MAX_MB MB budget"; return 1; }
  if [[ -z "${TODAYZ_TILES_SOURCE:-}" && -z "${TODAYZ_TILES_MAXZOOM:-}" ]]; then
    [[ "$(shasum -a 256 "$file" | cut -d' ' -f1)" == "$want" ]] || {
      warn "$(basename "$file") does not match its pinned sha256"; return 1; }
  fi
}

installed_version() {
  # go install leaves "dev" in `pmtiles version`; the module version is in the build info.
  local bin=""
  for bin in "$PMTILES" "$TOOLS/go-pmtiles"; do [[ -x "$bin" ]] && break; bin=""; done
  [[ -n "$bin" ]] || return 0
  if [[ "$bin" != "$PMTILES" ]]; then mv -f "$bin" "$PMTILES"; fi
  go version -m "$PMTILES" 2>/dev/null | awk '$1 == "mod" { print $3; exit }'
}

cmd_install() {
  local have; have="$(installed_version)"
  if [[ -n "$have" && "${have#v}" == "${PMTILES_VERSION#v}" ]]; then
    echo "tiles: pmtiles $PMTILES_VERSION already installed"
    return 0
  fi
  if ! command -v go >/dev/null; then
    warn "go not found, so the pmtiles CLI cannot be installed; maps stay blank until it is (make setup)"
    return 0
  fi
  mkdir -p "$TOOLS"
  echo "tiles: installing pmtiles $PMTILES_VERSION into .tools/bin"
  if ! GOBIN="$TOOLS" go install "$PMTILES_MODULE@$PMTILES_VERSION"; then
    warn "go install $PMTILES_MODULE@$PMTILES_VERSION failed"
    return 1
  fi
  # The module's binary is called go-pmtiles; the docs and this script call it pmtiles.
  mv -f "$TOOLS/go-pmtiles" "$PMTILES"
}

size_mb() { local b; b="$(wc -c <"$1" | tr -d ' ')"; echo $(((b + 1048575) / 1048576)); }

cmd_fetch() {
  if ! cmd_install || [[ ! -x "$PMTILES" ]]; then
    warn "skipping map tiles; the apps run without a base map"
    return 0
  fi
  mkdir -p "$OUT"
  local entry name bbox sum rest file tmp failed=0
  for entry in "${CITIES[@]}"; do
    name="${entry%% *}"; rest="${entry#* }"
    bbox="${rest%% *}"; sum="${rest#* }"
    file="$OUT/$name.pmtiles"
    if [[ -s "$file" ]]; then
      if verify "$file" "$sum"; then
        echo "tiles: $name.pmtiles present and verified ($(size_mb "$file") MB), skipping"
        continue
      fi
      warn "re-extracting $name.pmtiles"
    fi
    tmp="$file.part"
    rm -f "$tmp"
    echo "tiles: extracting $name (bbox $bbox, z0-$TILES_MAXZOOM) from $TILES_SOURCE"
    if "$PMTILES" extract "$TILES_SOURCE" "$tmp" --bbox="$bbox" --maxzoom="$TILES_MAXZOOM" --quiet && verify "$tmp" "$sum"; then
      mv -f "$tmp" "$file"
      echo "tiles: $name.pmtiles done and verified ($(size_mb "$file") MB)"
    else
      rm -f "$tmp" "$file"
      warn "no verified $name tiles (offline, or build $TILES_BUILD moved; see the note at the top of tiles.sh); re-run 'make setup' later"
      failed=1
    fi
  done
  [[ $failed -eq 0 ]] || warn "some city tiles are missing; maps show only the cities that downloaded"
  return 0
}

cmd_status() {
  local entry name rest f any=0 state
  for entry in "${CITIES[@]}"; do
    name="${entry%% *}"; rest="${entry#* }"
    f="$OUT/$name.pmtiles"
    [[ -e "$f" ]] || continue
    any=1
    if verify "$f" "${rest#* }" 2>/dev/null; then state=verified; else state="DOES NOT VERIFY"; fi
    printf '%-20s %5s MB  %s\n' "$(basename "$f")" "$(size_mb "$f")" "$state"
  done
  [[ $any -eq 1 ]] || echo "tiles: no local archives in infra/local/pmtiles (run 'make setup')"
}

case "${1:-fetch}" in
  install) cmd_install ;;
  fetch) cmd_fetch ;;
  status) cmd_status ;;
  *) sed -n '2,9p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 2 ;;
esac
