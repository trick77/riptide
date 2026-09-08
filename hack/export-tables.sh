#!/usr/bin/env bash
# Export all riptide tables to plain CSV and pack them into a .tar.gz.
#
# CSV (RFC4180, UTF-8, header row) instead of pg_dump -Fc so the archive can be
# read by any Postgres version, by pandas/DuckDB/Excel, or by hand.
#
# Usage:
#   hack/export-tables.sh [-o OUTDIR] [-t TABLE]... [-k] [-n] [DB_URL]
#
#   DB_URL      postgresql://user:pass@host:5432/riptide
#               SQLAlchemy form works too, the +asyncpg / +psycopg driver
#               suffix is stripped. Falls back to $RIPTIDE_DB_URL, then to
#               RIPTIDE_DB_URL= in the repo .env, then to the compose default.
#   -o OUTDIR   where the archive lands (default: hack/)
#   -t TABLE    export only this table (repeatable; default: all riptide tables)
#   -k          keep the unpacked staging directory next to the archive
#   -n          no compression, plain .tar
#
# Password: in the URL, or out of band via PGPASSWORD / ~/.pgpass (a password
# on the command line is visible in `ps`); psql prompts if neither is set.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ALL_TABLES=(
  bitbucket_events
  pipeline_events
  argocd_events
  noergler_events
  alembic_version
)

OUTDIR="$REPO_ROOT/hack"
TABLES=()
KEEP=0
COMPRESS=1

usage() { sed -n '2,21p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

while getopts ":o:t:knh" opt; do
  case "$opt" in
    o) OUTDIR="$OPTARG" ;;
    t) TABLES+=("$OPTARG") ;;
    k) KEEP=1 ;;
    n) COMPRESS=0 ;;
    h) usage 0 ;;
    *) usage 1 ;;
  esac
done
shift $(( OPTIND - 1 ))
[ $# -gt 1 ] && usage 1
[ ${#TABLES[@]} -eq 0 ] && TABLES=("${ALL_TABLES[@]}")

command -v psql >/dev/null || { echo "error: psql not found in PATH" >&2; exit 1; }

# --- connection string -------------------------------------------------------
# precedence: positional DB_URL > $RIPTIDE_DB_URL > .env > compose default
DB_URL="${1:-${RIPTIDE_DB_URL:-}}"
if [ -z "$DB_URL" ] && [ -f "$REPO_ROOT/.env" ]; then
  DB_URL="$(grep -E '^[[:space:]]*RIPTIDE_DB_URL=' "$REPO_ROOT/.env" | tail -1 | cut -d= -f2- | sed 's/^["'"'"']//; s/["'"'"']$//')"
fi
DB_URL="${DB_URL:-postgresql://riptide:riptide@localhost:5432/riptide}"
# postgresql+asyncpg:// -> postgresql://   (libpq does not know the driver suffix)
DB_URL="$(printf '%s' "$DB_URL" | sed -E 's#^(postgres(ql)?)\+[a-z0-9_]+://#\1://#')"

PSQL=(psql "$DB_URL" -v ON_ERROR_STOP=1 --no-psqlrc -q)

"${PSQL[@]}" -Atc 'select 1' >/dev/null || {
  echo "error: cannot connect with RIPTIDE_DB_URL" >&2; exit 1; }

# --- staging dir -------------------------------------------------------------
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
NAME="riptide-export-$STAMP"
STAGE="$OUTDIR/$NAME"
mkdir -p "$STAGE"

cleanup() { [ "$KEEP" -eq 1 ] || rm -rf "$STAGE"; }
trap cleanup EXIT

PG_VERSION="$("${PSQL[@]}" -Atc 'show server_version')"
DB_NAME="$("${PSQL[@]}" -Atc 'select current_database()')"

echo "riptide export -> $STAGE (postgres $PG_VERSION, db $DB_NAME)"

# --- per-table CSV -----------------------------------------------------------
MANIFEST_ROWS=()
for t in "${TABLES[@]}"; do
  exists="$("${PSQL[@]}" -Atc "select to_regclass('public.$t') is not null")"
  if [ "$exists" != "t" ]; then
    echo "  skip $t (not present)"
    continue
  fi

  # Generated columns (bitbucket_events.is_automated) are left out: Postgres
  # rejects them on COPY FROM and recomputes them on load anyway.
  sel="$("${PSQL[@]}" -Atc "select string_agg(quote_ident(column_name), ', ' order by ordinal_position) from information_schema.columns where table_schema='public' and table_name='$t' and is_generated='NEVER'")"

  # UTC timestamps and compact jsonb text — stable across versions
  "${PSQL[@]}" -c "set timezone to 'UTC'" \
    -c "\\copy (select $sel from public.$t order by 1) to '$STAGE/$t.csv' with (format csv, header true, quote '\"', escape '\"', encoding 'UTF8')"

  rows="$("${PSQL[@]}" -Atc "select count(*) from public.$t")"
  cols="$("${PSQL[@]}" -Atc "select string_agg(column_name || ':' || data_type, ', ' order by ordinal_position) from information_schema.columns where table_schema='public' and table_name='$t'")"
  bytes="$(wc -c < "$STAGE/$t.csv" | tr -d ' ')"
  printf '  %-20s %10s rows  %12s bytes\n' "$t" "$rows" "$bytes"

  MANIFEST_ROWS+=("$("${PSQL[@]}" -Atc "select json_build_object('table', '$t', 'rows', $rows, 'bytes', $bytes, 'file', '$t.csv', 'columns', \$col\$$cols\$col\$)::text")")
done

# --- schema DDL (plain SQL, readable; best effort) ---------------------------
if command -v pg_dump >/dev/null; then
  if pg_dump "$DB_URL" --schema-only --no-owner --no-privileges -f "$STAGE/schema.sql" 2>/dev/null; then
    echo "  schema.sql written"
  else
    echo "  schema.sql skipped (pg_dump missing or version mismatch)"
    rm -f "$STAGE/schema.sql"
  fi
fi

# --- manifest ----------------------------------------------------------------
{
  printf '{\n'
  printf '  "exported_at": "%s",\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '  "database": "%s",\n' "$DB_NAME"
  printf '  "server_version": "%s",\n' "$PG_VERSION"
  printf '  "format": "CSV RFC4180, UTF-8, header row; timestamptz in UTC, jsonb as JSON text",\n'
  printf '  "tables": [\n'
  for i in "${!MANIFEST_ROWS[@]}"; do
    sep=","; [ "$i" -eq $(( ${#MANIFEST_ROWS[@]} - 1 )) ] && sep=""
    printf '    %s%s\n' "${MANIFEST_ROWS[$i]}" "$sep"
  done
  printf '  ]\n}\n'
} > "$STAGE/manifest.json"

cat > "$STAGE/README.txt" <<'EOF'
riptide table export
====================

One CSV per table: header row, UTF-8, RFC4180 quoting ("" for a literal quote).
No binary dump format, so any Postgres version reads it back, as do pandas,
DuckDB and Excel.

Reload into an empty, migrated riptide DB (schema from `alembic upgrade head`
or from schema.sql), one table at a time:

  psql "$RIPTIDE_DB_URL" -c "\copy bitbucket_events from 'bitbucket_events.csv' with (format csv, header true)"

Generated columns (bitbucket_events.is_automated) are not in the CSV; Postgres
recomputes them. Load with the header's column list if the target table has a
different column order:

  psql "$RIPTIDE_DB_URL" -c "\copy bitbucket_events (id,delivery_id,...) from 'bitbucket_events.csv' with (format csv, header true)"

Fix sequences after loading explicit ids:

  select setval(pg_get_serial_sequence('bitbucket_events','id'), max(id)) from bitbucket_events;

Read with pandas:  pandas.read_csv("bitbucket_events.csv")
Read with duckdb:  select * from read_csv_auto('bitbucket_events.csv');

manifest.json lists row counts, byte sizes and column types per table.
EOF

# --- archive -----------------------------------------------------------------
if [ "$COMPRESS" -eq 1 ]; then
  ARCHIVE="$OUTDIR/$NAME.tar.gz"
  tar -czf "$ARCHIVE" -C "$OUTDIR" "$NAME"
else
  ARCHIVE="$OUTDIR/$NAME.tar"
  tar -cf "$ARCHIVE" -C "$OUTDIR" "$NAME"
fi

echo "archive: $ARCHIVE ($(wc -c < "$ARCHIVE" | tr -d ' ') bytes)"
[ "$KEEP" -eq 1 ] && echo "staging: $STAGE (kept)"
exit 0
