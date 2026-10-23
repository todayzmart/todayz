#!/usr/bin/env bash
# Installs one pinned uv release into the repo's tool dir (D-100 L8, security L-5):
#   install-uv.sh <version> <dest>      e.g. install-uv.sh 0.10.6 .tools/bin/uv
# The tarball is fetched from the uv release for this platform and checked against
# the SHA-256 recorded below before anything is extracted; the uv binary inside it
# is checked against its own recorded SHA-256 before it is installed; nothing is
# installed system-wide and no package manager is involved. Idempotent, and
# checked on every run (D-100 round 2, Low 9): a dest that reports the version AND
# matches the binary's recorded SHA-256 is kept; anything else is reinstalled.
set -euo pipefail

version="${1:?usage: install-uv.sh <version> <dest>}"
dest="${2:?usage: install-uv.sh <version> <dest>}"

# SHA-256 of uv-<target>.tar.gz per release (github.com/astral-sh/uv/releases/download/<version>/).
sha_for() {
  case "$1:$2" in
    0.10.6:aarch64-apple-darwin) echo 3993249d8f51deaf34cfce037e57e294e82267ff1f9dc45b7983a17afaf065b4 ;;
    0.10.6:x86_64-apple-darwin) echo d7647571fb17a5107d4d23cc190418039c157fd7361ddb59bc6f8127a49e3eac ;;
    0.10.6:x86_64-unknown-linux-gnu) echo aaa402e19d14a6b9a4267fcf4ec35380f804c68923525cea67cd6ee05bb4e930 ;;
    0.10.6:aarch64-unknown-linux-gnu) echo 9380705294a85e3e634570abddd5b2577900c1873c29b790c7abc56a81dce4bc ;;
    *) echo "" ;;
  esac
}

# SHA-256 of the `uv` binary inside uv-<target>.tar.gz (uv-<target>/uv), computed from the
# tarballs above. The installed file is compared with this on every run, so a replaced or
# patched binary that still prints the version is reinstalled.
bin_sha_for() {
  case "$1:$2" in
    0.10.6:aarch64-apple-darwin) echo 240a5881367c38cbdfac25cad5d8cff2459a730339225e9373028d4453bebe05 ;;
    0.10.6:x86_64-apple-darwin) echo e88f039b4a8ceeb867e256627734898ebaaddc712b7fe16271960b24ab25eeea ;;
    0.10.6:x86_64-unknown-linux-gnu) echo 1cec0cda74b129b3e15ed7ff80d0ce303c5e3446cd0dbc47675e908052587419 ;;
    0.10.6:aarch64-unknown-linux-gnu) echo 7644a9b798e80c5b448c212d5051435ca978d3fc2210c6be9c0af7f997108ef7 ;;
    *) echo "" ;;
  esac
}

case "$(uname -s):$(uname -m)" in
  Darwin:arm64) target=aarch64-apple-darwin ;;
  Darwin:x86_64) target=x86_64-apple-darwin ;;
  Linux:x86_64) target=x86_64-unknown-linux-gnu ;;
  Linux:aarch64 | Linux:arm64) target=aarch64-unknown-linux-gnu ;;
  *) echo "install-uv: no pinned uv build for $(uname -s) $(uname -m)" >&2; exit 1 ;;
esac

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

# uv --version prints "uv <version> (<commit> <date>)".
reports() {
  case "$("$1" --version 2>/dev/null || true)" in
    "uv $version" | "uv $version "*) return 0 ;;
    *) return 1 ;;
  esac
}

want="$(sha_for "$version" "$target")"
want_bin="$(bin_sha_for "$version" "$target")"
if [[ -z "$want" || -z "$want_bin" ]]; then
  echo "install-uv: no SHA-256 recorded for uv $version on $target; add both sums to scripts/install-uv.sh" >&2
  exit 1
fi

if [[ -f "$dest" ]]; then
  if [[ -x "$dest" ]] && [[ "$(sha256 "$dest")" == "$want_bin" ]] && reports "$dest"; then
    exit 0
  fi
  echo "install-uv: $dest is not the pinned uv $version binary; reinstalling" >&2
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/tz-uv.XXXXXX")"
# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup() { rm -rf "$work"; }
trap cleanup EXIT

url="https://github.com/astral-sh/uv/releases/download/$version/uv-$target.tar.gz"
echo "installing uv $version ($target) into $dest"
curl -fsSL --proto '=https' --tlsv1.2 -o "$work/uv.tar.gz" "$url"

if [[ "$(sha256 "$work/uv.tar.gz")" != "$want" ]]; then
  echo "install-uv: SHA-256 mismatch for uv-$target.tar.gz (the pinned sum is in scripts/install-uv.sh)" >&2
  exit 1
fi

tar -xzf "$work/uv.tar.gz" -C "$work"
if [[ "$(sha256 "$work/uv-$target/uv")" != "$want_bin" ]]; then
  echo "install-uv: SHA-256 mismatch for the uv binary inside uv-$target.tar.gz (the pinned sum is in scripts/install-uv.sh)" >&2
  exit 1
fi
mkdir -p "$(dirname "$dest")"
install -m 0755 "$work/uv-$target/uv" "$dest"
reports "$dest" || { echo "install-uv: $dest does not report uv $version" >&2; exit 1; }
