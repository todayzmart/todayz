#!/usr/bin/env bash
# Local backup/restore drill (W4-07). `make ops-restore-drill` runs it; it is not
# part of `make check`. It needs the local Postgres (port 5433) up: `make dev`.
#
# 1. Opens a read-only snapshot on the source database and dumps exactly that
#    snapshot (pg_dump --snapshot), so a running app does not skew the compare.
# 2. Runs the reconcile pass (restore-drill.sql) on the source in that snapshot.
# 3. Restores the dump into a scratch database and runs the same pass there.
# 4. Compares the two and writes docs/qa/ops/restore-drill-<date>.md.
# 5. Drops the scratch database and the dump.
#
# It never connects by URL and never prints a password: psql runs inside the
# postgres container as the local superuser. Exit 1 on any mismatch or a
# non-zero ledger invariant.
set -euo pipefail

# shellcheck source=infra/local/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../infra/local" && pwd)/lib.sh"

SRC_DB="${TODAYZ_DRILL_SOURCE_DB:-todayz}"
DRILL_DB="todayz_restore_drill"
DUMP="/tmp/todayz-restore-drill.dump"
SRC_OUT="/tmp/todayz-restore-drill-source.tsv"
SQL="$TZ_ROOT/scripts/ops/restore-drill.sql"
DAY="$(date +%F)"
EVIDENCE_DIR="$TZ_ROOT/docs/qa/ops"
EVIDENCE="$EVIDENCE_DIR/restore-drill-$DAY.md"

die() {
  echo "restore-drill: $*" >&2
  exit 1
}
say() { echo "restore-drill: $*"; }
now_ms() { perl -MTime::HiRes=time -e 'printf "%d\n", time() * 1000'; }
secs() { awk -v ms="$1" 'BEGIN { printf "%.1f", ms / 1000 }'; }

pg() { tz_compose exec -T -e PGOPTIONS="-c client_min_messages=warning" postgres "$@"; }
psql_db() { pg psql -X -q -At -F $'\t' -v ON_ERROR_STOP=1 -U todayz -d "$1" "${@:2}"; }

command -v docker >/dev/null || die "docker CLI not found"
tz_compose ps --status running --services 2>/dev/null | grep -qx postgres ||
  die "the local postgres service is not running; start it with 'make dev'"
[[ "$(psql_db postgres -c "SELECT count(*) FROM pg_database WHERE datname = '$SRC_DB'")" == 1 ]] ||
  die "database $SRC_DB not found; run 'make reset' first"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/todayz-drill.XXXXXX")"
HELD_PID=""
cleanup() {
  exec 3>&- 2>/dev/null || true
  exec 4<&- 2>/dev/null || true
  [[ -n "$HELD_PID" ]] && kill "$HELD_PID" 2>/dev/null || true
  pg rm -f "$DUMP" "$SRC_OUT" >/dev/null 2>&1 || true
  psql_db postgres -c "DROP DATABASE IF EXISTS $DRILL_DB WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

psql_db postgres -c "DROP DATABASE IF EXISTS $DRILL_DB WITH (FORCE)" >/dev/null
PG_VERSION="$(psql_db postgres -c "SHOW server_version")"
SRC_SIZE="$(psql_db postgres -c "SELECT pg_size_pretty(pg_database_size('$SRC_DB'))")"

# 1. Hold a snapshot open in one session; dump and reconcile the source inside it.
say "opening a snapshot on $SRC_DB"
mkfifo "$WORK/in" "$WORK/out"
psql_db "$SRC_DB" <"$WORK/in" >"$WORK/out" 2>&1 &
HELD_PID=$!
exec 3>"$WORK/in" 4<"$WORK/out"
echo "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT pg_export_snapshot();" >&3
SNAP=""
read -r -t 30 SNAP <&4 || true
[[ "$SNAP" =~ ^[0-9A-F-]+$ ]] || die "could not export a snapshot"

say "dumping $SRC_DB (custom format)"
t0="$(now_ms)"
pg pg_dump -Fc -U todayz -d "$SRC_DB" --snapshot="$SNAP" -f "$DUMP"
DUMP_MS=$(($(now_ms) - t0))
DUMP_BYTES="$(pg sh -c "wc -c < $DUMP" | tr -d ' \r')"
TOC_ENTRIES="$(pg pg_restore --list "$DUMP" | grep -cv '^;')"

say "reconciling the source inside the snapshot"
{
  echo "\\o $SRC_OUT"
  cat "$SQL"
  echo "\\o"
  echo "COMMIT;"
  echo "\\q"
} >&3
exec 3>&-
wait "$HELD_PID" || die "the source reconcile failed: $(cat <&4)"
HELD_PID=""
exec 4<&-
pg cat "$SRC_OUT" >"$WORK/source.tsv"

# 2. Restore into the scratch database.
say "restoring into $DRILL_DB"
t0="$(now_ms)"
pg createdb -U todayz -T template0 "$DRILL_DB"
pg pg_restore --exit-on-error -U todayz -d "$DRILL_DB" "$DUMP"
RESTORE_MS=$(($(now_ms) - t0))

say "reconciling the restored copy"
t0="$(now_ms)"
psql_db "$DRILL_DB" <"$SQL" >"$WORK/restore.tsv"
RECONCILE_MS=$(($(now_ms) - t0))

# 3. Compare. cmp.tsv: section, key, source, restore, ok|DIFF|NONZERO.
awk -F'\t' -v OFS='\t' '
  NR == FNR { k = $1 FS $2; src[k] = $3; if (!(k in seen)) { seen[k] = 1; order[++n] = k }; next }
  { k = $1 FS $2; rst[k] = $3; if (!(k in seen)) { seen[k] = 1; order[++n] = k } }
  END {
    for (i = 1; i <= n; i++) {
      k = order[i]; split(k, p, FS)
      s = (k in src) ? src[k] : "(missing)"; r = (k in rst) ? rst[k] : "(missing)"
      st = (s == r) ? "ok" : "DIFF"
      if (st == "ok" && p[1] == "ledger" && s != "0") st = "NONZERO"
      print p[1], p[2], s, r, st
    }
  }' "$WORK/source.tsv" "$WORK/restore.tsv" >"$WORK/cmp.tsv"

count() { awk -F'\t' -v s="$1" -v st="$2" '$1 == s && (st == "" || $5 == st)' "$WORK/cmp.tsv" | wc -l | tr -d ' '; }
BAD="$(awk -F'\t' '$5 != "ok"' "$WORK/cmp.tsv" | wc -l | tr -d ' ')"
TABLES="$(count rows "")"
TABLES_OK="$(count rows ok)"
ROWS_TOTAL="$(awk -F'\t' '$1 == "rows" { split($3, a, " "); t += a[1] } END { print t + 0 }' "$WORK/cmp.tsv")"
if [[ "$BAD" == 0 ]]; then RESULT="PASS"; else RESULT="FAIL ($BAD mismatches)"; fi

table() { # section heading
  echo "### $2"
  echo
  echo "| Key | Source | Restored | Result |"
  echo "|---|---|---|---|"
  awk -F'\t' -v s="$1" '$1 == s { printf "| `%s` | %s | %s | %s |\n", $2, $3, $4, $5 }' "$WORK/cmp.tsv"
  echo
}

mkdir -p "$EVIDENCE_DIR"
{
  echo "# Restore drill $DAY"
  echo
  echo "Generated by \`make ops-restore-drill\` (\`scripts/ops/restore-drill.sh\`). Do not edit by hand; run it again."
  echo
  echo "**Result: $RESULT.**"
  echo
  echo "| Fact | Value |"
  echo "|---|---|"
  echo "| Run at | $(date '+%Y-%m-%d %H:%M %Z') |"
  echo "| Repo commit | \`$(git -C "$TZ_ROOT" rev-parse --short HEAD)\` |"
  echo "| Source | local Postgres $PG_VERSION (docker compose service \`postgres\`, host port 5433), database \`$SRC_DB\`, $SRC_SIZE |"
  echo "| Snapshot | exported read-only snapshot; dump and source reconcile read the same snapshot |"
  echo "| Dump | \`pg_dump -Fc\`, $DUMP_BYTES bytes, $TOC_ENTRIES TOC entries, $(secs "$DUMP_MS") s |"
  echo "| Restore | \`pg_restore --exit-on-error\` into scratch database \`$DRILL_DB\`, $(secs "$RESTORE_MS") s |"
  echo "| Reconcile (restored copy) | $(secs "$RECONCILE_MS") s |"
  echo "| Tables compared | $TABLES_OK of $TABLES match (row count and checksum), $ROWS_TOTAL rows |"
  echo "| Mismatches | $BAD |"
  echo "| Scratch database | dropped after the run |"
  echo
  echo "## What was compared"
  echo
  echo "- **Rows:** exact row count and an order-free md5 of every row, for every table (partitions one by one)."
  echo "- **Ledger:** the \`ledger_reconcile\` invariants. Each must be 0 on both sides; \`entries_sum\` is Σ of all ledger entries."
  echo "- **Money:** credit sums per account kind, lots, holds, payouts, statements and adjustments."
  echo "- **Partitions:** children per partitioned table, the default partition, and the furthest range bound."
  echo "- **Migrations:** goose version and count, River version."
  echo "- **Schema:** counts of indexes, triggers, functions, constraints, sequences, and the extension versions."
  echo
  echo "## Results"
  echo
  table ledger "Ledger invariants"
  table money "Money sums (credits)"
  table partitions "Partitions"
  table migrations "Migrations"
  table schema "Schema objects"
  echo "### Tables"
  echo
  echo "<details><summary>$TABLES tables: row count and checksum</summary>"
  echo
  echo "| Table | Source | Restored | Result |"
  echo "|---|---|---|---|"
  awk -F'\t' '$1 == "rows" { printf "| `%s` | %s | %s | %s |\n", $2, $3, $4, $5 }' "$WORK/cmp.tsv"
  echo
  echo "</details>"
  echo
  echo "## Not covered here"
  echo
  echo "- The cloud path: RDS point-in-time restore to a side instance. Steps are in [docs/ops/backup-restore.md](../../ops/backup-restore.md); it can only run once the AWS account exists."
  echo "- Objects outside the database: MinIO / R2 images and files."
  echo "- Roles and passwords: they are cluster-wide, so a database dump does not carry them. Terraform and the Secrets Manager entries do."
} >"$EVIDENCE"

say "evidence: ${EVIDENCE#"$TZ_ROOT"/}"
say "dump $(secs "$DUMP_MS") s, restore $(secs "$RESTORE_MS") s, $TABLES_OK/$TABLES tables match, mismatches $BAD"
if [[ "$BAD" != 0 ]]; then
  awk -F'\t' '$5 != "ok" { print "restore-drill: " $5 ": " $1 " " $2 }' "$WORK/cmp.tsv" >&2
  exit 1
fi
say "PASS"
