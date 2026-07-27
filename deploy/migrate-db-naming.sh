#!/usr/bin/env bash
# Bash equivalent of migrate-db-naming.ps1 for start-server.sh (Linux/macOS
# hosts, no PowerShell dependency). Keep both in sync; start-server.bat uses
# the .ps1 version instead.
#
# One-time, opt-in migration of the Postgres role/database name inside an
# existing data volume from "telesrv" to "owpengram".
#
# docker-compose.yml's POSTGRES_USER/POSTGRES_DB env vars only take effect
# when Postgres bootstraps a brand-new, empty data volume -- they're silently
# ignored against a pre-existing volume, so changing the compose file alone
# does nothing for an install that already has data on disk. This script
# talks to the already-running Postgres container directly (run it after
# "docker compose up" plus a readiness wait, not before) and renames the
# role/database in place via ALTER ROLE / ALTER DATABASE.
#
# The existing password is left untouched -- renaming doesn't change it. Only
# the TELESRV_POSTGRES_DSN line in .env is patched (never any other line, and
# never if that line was already customized away from the plain telesrv
# defaults) so the app can still connect afterwards.
#
# Prints nothing when there's nothing to do. Prompts interactively at most
# once; the decision is cached in a state file next to .secrets/.public_ip, so
# this is never asked again. Declining is permanent: the install keeps the
# "telesrv" role/database name forever.
set -euo pipefail

CONTAINER_NAME="${1:-owpengram-postgres}"
ENV_FILE="${2:-.env}"
STATE_FILE="${3:-.db_naming}"

info() { echo "$@" >&2; }

pg_role_exists() {
  local role="$1"
  local out
  out="$(docker exec "$CONTAINER_NAME" psql -U telesrv -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='${role}'" 2>/dev/null || true)"
  [[ "$out" == *1* ]]
}

state=""
[[ -f "$STATE_FILE" ]] && state="$(tr -d '[:space:]' < "$STATE_FILE")"
if [[ "$state" == "owpengram" || "$state" == "telesrv" ]]; then
  exit 0
fi

old_exists=false
new_exists=false
pg_role_exists "telesrv" && old_exists=true
pg_role_exists "owpengram" && new_exists=true

if [[ "$old_exists" == false || "$new_exists" == true ]]; then
  # Nothing to migrate: either already "owpengram"-named (fresh install or
  # already renamed), or connecting as "telesrv" didn't work at all (custom
  # credentials already in place) -- leave it alone either way.
  echo -n "owpengram" > "$STATE_FILE"
  exit 0
fi

if [[ ! -t 0 ]]; then
  info "[cfg] Postgres role/database still named 'telesrv' but running non-interactively; keeping old naming for now"
  exit 0
fi

info ""
info "== Postgres role/database naming =="
info "The Postgres role and database inside this install's data volume are still"
info "named 'telesrv' (renaming docker-compose.yml alone doesn't affect existing"
info "data). Renaming them to 'owpengram' keeps everything consistent. Recommended."
read -rp "Rename Postgres role/database from 'telesrv' to 'owpengram' now? [Y/n] " reply

if [[ "$reply" =~ ^[Nn] ]]; then
  echo -n "telesrv" > "$STATE_FILE"
  info "[cfg] keeping 'telesrv' Postgres role/database naming (won't ask again)"
  exit 0
fi

info "[cfg] renaming Postgres role/database: telesrv -> owpengram"

# Postgres refuses "ALTER ROLE <session user> RENAME" (session user cannot be
# renamed), so telesrv can't rename itself. Do the database rename directly
# as telesrv, then use a short-lived second superuser to rename the telesrv
# role, then drop that temporary role from the now-renamed "owpengram" role's
# own session. Each step is a separate connection/transaction on purpose:
# combining the ALTER DATABASE and ALTER ROLE in one multi-statement call
# rolls the database rename back too when the role rename fails (Postgres
# treats a semicolon-separated -c string as one implicit transaction).
if ! docker exec "$CONTAINER_NAME" psql -U telesrv -d postgres -c \
  "ALTER DATABASE telesrv RENAME TO owpengram; CREATE ROLE _telesrv_migrate SUPERUSER LOGIN PASSWORD 'telesrv_migrate';" \
  >/dev/null 2>&1; then
  info "[ERROR] failed to rename Postgres database / create temporary migration role"
  exit 1
fi
if ! docker exec "$CONTAINER_NAME" psql -U _telesrv_migrate -d owpengram -c \
  "ALTER ROLE telesrv RENAME TO owpengram;" >/dev/null 2>&1; then
  info "[ERROR] failed to rename Postgres role"
  exit 1
fi
if ! docker exec "$CONTAINER_NAME" psql -U owpengram -d owpengram -c \
  "DROP ROLE _telesrv_migrate;" >/dev/null 2>&1; then
  info "[WARN] renamed role/database but failed to drop the temporary _telesrv_migrate role - remove it by hand"
fi

echo -n "owpengram" > "$STATE_FILE"
info "[ok] Postgres role/database renamed to 'owpengram'"

if [[ -f "$ENV_FILE" ]]; then
  if grep -qE '^TELESRV_POSTGRES_DSN=postgres://telesrv:[^@]*@[^/]+/telesrv(\?.*)?$' "$ENV_FILE"; then
    sed -i -E 's#^TELESRV_POSTGRES_DSN=postgres://telesrv:([^@]*)@([^/]+)/telesrv(\?.*)?$#TELESRV_POSTGRES_DSN=postgres://owpengram:\1@\2/owpengram\3#' "$ENV_FILE"
    info "[ok] updated the TELESRV_POSTGRES_DSN line in ${ENV_FILE} (only that one line)"
  else
    info "[WARN] TELESRV_POSTGRES_DSN in ${ENV_FILE} doesn't match the plain telesrv defaults - update it by hand:"
    info "       TELESRV_POSTGRES_DSN=postgres://owpengram:<your-password>@<host>:<port>/owpengram?sslmode=disable"
  fi
else
  info "[WARN] ${ENV_FILE} not found - if you create one, point TELESRV_POSTGRES_DSN at the 'owpengram' role/database"
fi
