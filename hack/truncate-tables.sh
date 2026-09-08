#!/usr/bin/env bash
# Empty the riptide event tables. Destructive and not undoable.
#
# Truncates the append-only event tables and restarts their id sequences.
# `alembic_version` is never touched — wiping it would strand the schema
# between migrations. The schema itself is left in place, so the collector
# keeps ingesting into empty tables; no migration re-run is needed.
#
# Usage:
#   hack/truncate-tables.sh [-t TABLE]... [-b] [-y] [-n] [DB_URL]
#
#   DB_URL      postgresql://user:pass@host:5432/riptide
#               SQLAlchemy form works too, the +asyncpg / +psycopg driver
#               suffix is stripped. Falls back to $RIPTIDE_DB_URL, then to
#               RIPTIDE_DB_URL= in the repo .env, then to the compose default.
#   -t TABLE    truncate only this table (repeatable; default: all event tables)
#   -b          take a CSV backup first via hack/export-tables.sh, and abort
#               if that export fails
#   -y          skip the confirmation prompt (for non-interactive use)
#   -n          dry run: report what would be deleted, change nothing
#
# Password: in the URL, or out of band via PGPASSWORD / ~/.pgpass (a password
# on the command line is visible in `ps`); psql prompts if neither is set.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# alembic_version is deliberately absent: it is schema state, not event data.
ALL_TABLES=(
  bitbucket_events
  pipeline_events
  argocd_events
  noergler_events
)

TABLES=()
BACKUP=0
ASSUME_YES=0
DRY_RUN=0

usage() { sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

while getopts ":t:bynh" opt; do
  case "$opt" in
    t) TABLES+=("$OPTARG") ;;
    b) BACKUP=1 ;;
    y) ASSUME_YES=1 ;;
    n) DRY_RUN=1 ;;
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
DB_URL="$(printf '%s' "$DB_URL" | sed -E 's#^(postgres(ql)?)\+[a-z0-9_]+://#\1://#')"

PSQL=(psql "$DB_URL" -v ON_ERROR_STOP=1 --no-psqlrc -q)

"${PSQL[@]}" -Atc 'select 1' >/dev/null || {
  echo "error: cannot connect with RIPTIDE_DB_URL" >&2; exit 1; }

DB_NAME="$("${PSQL[@]}" -Atc 'select current_database()')"
DB_HOST="$("${PSQL[@]}" -Atc "select coalesce(inet_server_addr()::text, 'local')")"

# --- what would go -----------------------------------------------------------
PRESENT=()
TOTAL=0
echo "database $DB_NAME on $DB_HOST"
for t in "${TABLES[@]}"; do
  if [ "$("${PSQL[@]}" -Atc "select to_regclass('public.$t') is not null")" != "t" ]; then
    echo "  skip $t (not present)"
    continue
  fi
  rows="$("${PSQL[@]}" -Atc "select count(*) from public.$t")"
  printf '  %-20s %10s rows\n' "$t" "$rows"
  PRESENT+=("$t")
  TOTAL=$(( TOTAL + rows ))
done

if [ ${#PRESENT[@]} -eq 0 ]; then
  echo "nothing to truncate"
  exit 0
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry run: $TOTAL rows would be deleted, nothing changed"
  exit 0
fi

# --- backup ------------------------------------------------------------------
if [ "$BACKUP" -eq 1 ]; then
  echo "backing up first…"
  "$REPO_ROOT/hack/export-tables.sh" "$DB_URL" || {
    echo "error: backup failed, refusing to truncate" >&2; exit 1; }
fi

# --- confirm -----------------------------------------------------------------
# Ingestion is append-only and forward-only: there is no backfill worker, so
# rows deleted here are gone for good unless -b took a copy.
if [ "$ASSUME_YES" -ne 1 ]; then
  if [ ! -t 0 ]; then
    echo "error: refusing to truncate without -y when stdin is not a terminal" >&2
    exit 1
  fi
  echo
  echo "This deletes $TOTAL rows from ${#PRESENT[@]} table(s) in '$DB_NAME'. There is no undo"
  echo "and no backfill — events not re-sent by a webhook are gone."
  printf "Type the database name to continue: "
  read -r answer
  [ "$answer" = "$DB_NAME" ] || { echo "aborted"; exit 1; }
fi

# --- truncate ----------------------------------------------------------------
# One statement: all-or-nothing, and RESTART IDENTITY resets the id sequences
# so a fresh run starts at 1 instead of continuing past the deleted rows.
LIST="$(printf 'public.%s, ' "${PRESENT[@]}")"
"${PSQL[@]}" -c "truncate table ${LIST%, } restart identity"

echo "truncated: ${PRESENT[*]}"
for t in "${PRESENT[@]}"; do
  printf '  %-20s %10s rows\n' "$t" "$("${PSQL[@]}" -Atc "select count(*) from public.$t")"
done
echo "alembic_version left untouched ($("${PSQL[@]}" -Atc 'select version_num from alembic_version'))"
