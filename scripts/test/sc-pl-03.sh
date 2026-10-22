#!/usr/bin/env bash
# SC-PL-03 (D-18): secrets are never printed.
# Fresh env file -> `make setup-keys` prints exactly "generated: NAME" per D-18 key;
# a second run prints nothing; `make env-check` prints only "NAME set|missing";
# no generated value appears (raw, base64 or hex) in any captured stdout/stderr.
# Extra logs to scan (e.g. `make dev` start-up) can be passed as arguments.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
envtool="$root/.tools/bin/envtool"
work="$(mktemp -d "${TMPDIR:-/tmp}/tz-pl03.XXXXXX")"
trap 'rm -rf "$work"' EXIT
export TODAYZ_ENV_FILE="$work/.env.local"
keys=(JWT_SIGNING_KEYS JWT_CURRENT_KID ORDER_CODE_ENC_KEY ORDER_CODE_HMAC_KEY BILL_QR_SIGNING_KEY PII_ENC_KEY PII_HASH_KEY DEVICE_SECRET_ENC_KEY TOTP_ENC_KEY)
fail=0

make -C "$root" -s setup-keys >"$work/run1.out" 2>"$work/run1.err"
make -C "$root" -s setup-keys >"$work/run2.out" 2>"$work/run2.err"
make -C "$root" -s env-check >"$work/check.out" 2>"$work/check.err" || true

expected="$(printf 'generated: %s\n' "${keys[@]}")"
[[ "$(cat "$work/run1.out")" == "$expected" ]] || { echo "FAIL: first setup output is not exactly 'generated: NAME' x${#keys[@]}"; fail=1; }
[[ ! -s "$work/run1.err" ]] || { echo "FAIL: first setup wrote to stderr"; fail=1; }
[[ ! -s "$work/run2.out" && ! -s "$work/run2.err" ]] || { echo "FAIL: second setup printed something"; fail=1; }
if grep -vE '^[A-Z][A-Z0-9_]* (set|missing)$' "$work/check.out" >/dev/null; then
  echo "FAIL: env-check printed a line that is not 'NAME set|missing'"; fail=1
fi
for k in "${keys[@]}"; do
  grep -qx "$k set" "$work/check.out" || { echo "FAIL: env-check does not show $k set"; fail=1; }
done
[[ "$(stat -f %Lp "$TODAYZ_ENV_FILE")" == "600" ]] || { echo "FAIL: env file is not mode 600"; fail=1; }

if ! "$envtool" leakscan -env "$TODAYZ_ENV_FILE" "$work"/*.out "$work"/*.err "$@"; then
  echo "FAIL: a generated value appeared in output"; fail=1
fi
[[ $fail -eq 0 ]] && echo "ok: SC-PL-03 secrets never printed"
exit $fail
