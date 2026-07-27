#!/usr/bin/env bash
# Bash equivalent of migrate-docker-naming.ps1 for start-server.sh (Linux/macOS
# hosts, no PowerShell dependency). Keep both in sync; start-server.bat uses
# the .ps1 version instead.
#
# Resolves (and, once, offers to migrate) the Docker project/container/volume
# naming used by deploy/docker-compose.yml.
#
# The project used to be named "telesrv" in Docker (containers telesrv-postgres
# / telesrv-redis, volumes telesrv_pgdata / telesrv_redisdata). It's now
# "owpengram" by default, so a gramsrv instance running on the same machine
# can't be confused with this one at a glance in `docker ps` / Docker Desktop.
#
# Existing installs may still have telesrv_* volumes with real data in them.
# This script asks, at most once, whether to copy that data over to owpengram_*
# naming. The answer is cached in a state file next to .secrets/.public_ip:
#   - accepted -> volumes are copied (old telesrv_* volumes are left in place,
#     untouched, as a backup), state becomes "owpengram", never asked again.
#   - declined -> state becomes "telesrv", the install keeps the old naming
#     forever, never asked again.
#   - fresh install (no telesrv_* volumes found) -> silently uses "owpengram",
#     nothing to migrate.
#
# Prints exactly two lines to stdout: the resolved project name, then the
# resolved container/volume prefix. Everything else (prompts, progress) goes
# to stderr, so a caller can safely capture just stdout.
set -euo pipefail

COMPOSE_FILE="${1:-deploy/docker-compose.yml}"
STATE_FILE="${2:-.docker_naming}"

info() { echo "$@" >&2; }
result() { printf '%s\n%s\n' "$1" "$2"; }

volume_exists() { docker volume inspect "$1" >/dev/null 2>&1; }

state=""
[[ -f "$STATE_FILE" ]] && state="$(tr -d '[:space:]' < "$STATE_FILE")"

if [[ "$state" == "owpengram" ]]; then
  result "owpengram" "owpengram"
  exit 0
fi
if [[ "$state" == "telesrv" ]]; then
  result "telesrv" "telesrv"
  exit 0
fi

old_pg=false
old_redis=false
volume_exists "telesrv_pgdata" && old_pg=true
volume_exists "telesrv_redisdata" && old_redis=true

if [[ "$old_pg" == false && "$old_redis" == false ]]; then
  echo -n "owpengram" > "$STATE_FILE"
  result "owpengram" "owpengram"
  exit 0
fi

if [[ ! -t 0 ]]; then
  info "[cfg] old 'telesrv_*' Docker volumes found but running non-interactively; keeping old naming for now"
  result "telesrv" "telesrv"
  exit 0
fi

info ""
info "== Docker container/volume naming =="
info "Found existing Docker volumes named 'telesrv_*' (from before the project was"
info "renamed to OwpenGram). Renaming them to 'owpengram_*' avoids confusing this"
info "server with a gramsrv instance running on the same machine. Recommended."
read -rp "Migrate Docker containers/volumes from 'telesrv' to 'owpengram' naming now? [Y/n] " reply

if [[ "$reply" =~ ^[Nn] ]]; then
  echo -n "telesrv" > "$STATE_FILE"
  info "[cfg] keeping 'telesrv' Docker naming (won't ask again)"
  result "telesrv" "telesrv"
  exit 0
fi

info "[cfg] migrating Docker volumes: telesrv_* -> owpengram_*"

TELESRV_DOCKER_PROJECT=telesrv TELESRV_DOCKER_PREFIX=telesrv \
  docker compose -f "$COMPOSE_FILE" -p telesrv stop postgres redis >/dev/null 2>&1 || true

if [[ "$old_pg" == true ]]; then
  docker volume create owpengram_pgdata >/dev/null
  if ! docker run --rm -v telesrv_pgdata:/from -v owpengram_pgdata:/to alpine sh -c "cp -a /from/. /to/"; then
    info "[ERROR] failed to copy pgdata volume"
    exit 1
  fi
fi
if [[ "$old_redis" == true ]]; then
  docker volume create owpengram_redisdata >/dev/null
  if ! docker run --rm -v telesrv_redisdata:/from -v owpengram_redisdata:/to alpine sh -c "cp -a /from/. /to/"; then
    info "[ERROR] failed to copy redisdata volume"
    exit 1
  fi
fi

TELESRV_DOCKER_PROJECT=telesrv TELESRV_DOCKER_PREFIX=telesrv \
  docker compose -f "$COMPOSE_FILE" -p telesrv down >/dev/null 2>&1 || true

echo -n "owpengram" > "$STATE_FILE"
info "[ok] migration complete - old 'telesrv_*' volumes were left in place, untouched, as a backup"
result "owpengram" "owpengram"
