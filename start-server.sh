#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

ENV_FILE=".env"
COMPOSE_FILE="deploy/docker-compose.yml"
LOG_DIR="logs"

NO_BUILD=false
for arg in "$@"; do
  case "$arg" in
    --no-build) NO_BUILD=true ;;
    -h|--help)
      echo "Usage: $0 [--no-build]"
      echo ""
      echo "  --no-build   Skip Go compilation (reuse existing binaries in bin/)"
      exit 0
      ;;
  esac
done

# --- Helpers ----------------------------------------------------------------
log()  { echo "[cfg] $*"; }
step() { echo; echo "== $* =="; }
die()  { echo "[ERROR] $*" >&2; exit 1; }

# This script only starts the server — it never writes or edits .env. Set up
# .env yourself (from .env.example) before running it.
[[ -f "$ENV_FILE" ]] || die "${ENV_FILE} not found — copy .env.example to ${ENV_FILE} and configure it first"

# --- Docker container/volume naming (telesrv -> owpengram), one-time, opt-in
# Resolves whether this install uses the new "owpengram" Docker naming or (if
# telesrv_* volumes exist and the user declined migrating them) keeps the old
# "telesrv" naming permanently. See deploy/migrate-docker-naming.sh.
NAMING_OUT="$(bash deploy/migrate-docker-naming.sh "$COMPOSE_FILE" .docker_naming)" \
  || die "docker naming resolution failed"
DOCKER_PROJECT="$(sed -n '1p' <<<"$NAMING_OUT")"
DOCKER_PREFIX="$(sed -n '2p' <<<"$NAMING_OUT")"
export TELESRV_DOCKER_PROJECT="$DOCKER_PROJECT"
export TELESRV_DOCKER_PREFIX="$DOCKER_PREFIX"
log "docker naming = ${DOCKER_PREFIX} (project ${DOCKER_PROJECT})"

# --- Start infrastructure (PostgreSQL + Redis) -----------------------------
step "[1/4] Starting infrastructure (PostgreSQL + Redis)"
docker compose -f "$COMPOSE_FILE" up -d

# --- Wait for PostgreSQL ---------------------------------------------------
step "[2/4] Waiting for PostgreSQL"
for i in $(seq 1 30); do
  if docker exec "${DOCKER_PREFIX}-postgres" pg_isready -U telesrv -d telesrv >/dev/null 2>&1; then
    echo "[ok] PostgreSQL is ready"
    break
  fi
  if [ "$i" -eq 30 ]; then
    die "PostgreSQL not ready after 60s"
  fi
  sleep 2
done

# --- Postgres role/database naming (telesrv -> owpengram), one-time, opt-in
# Only runs for installs that already accepted the Docker naming migration
# above; a "telesrv"-naming install already said no to this rename theme once
# and isn't asked again. See deploy/migrate-db-naming.sh.
if [ "$DOCKER_PREFIX" = "owpengram" ]; then
  bash deploy/migrate-db-naming.sh "${DOCKER_PREFIX}-postgres" "$ENV_FILE" .db_naming \
    || die "postgres naming migration failed"
fi

# --- Build ------------------------------------------------------------------
step "[3/4] Building server binaries"
if [ "$NO_BUILD" = true ]; then
  log "skipping build (--no-build)"
  if [[ ! -f "bin/owpengram-server" ]] && [[ ! -f "bin/owpengram-server.exe" ]]; then
    die "no binaries found in bin/ — run without --no-build first"
  fi
else
  mkdir -p bin
  echo "  building owpengram-server ..."
  go build -o bin/owpengram-server ./cmd/telesrv
  echo "  building owpengram-admin-panel ..."
  go build -o bin/owpengram-admin-panel ./cmd/telesrv-admin
  echo "[ok] binaries built in bin/"
fi

# --- Start servers ----------------------------------------------------------
step "[4/4] Starting owpengram-server + owpengram-admin-panel"

mkdir -p "$LOG_DIR"
SERVER_LOG="$LOG_DIR/owpengram-server.log"
ADMIN_LOG="$LOG_DIR/owpengram-admin-panel.log"

cleanup() {
  echo
  echo "[stop] stopping owpengram-server and owpengram-admin-panel ..."
  kill "$SERVER_PID" 2>/dev/null || true
  kill "$ADMIN_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
  wait "$ADMIN_PID" 2>/dev/null || true
  echo "[ok] stopped."
}
trap cleanup EXIT INT TERM

# Start owpengram-server (main server)
BIN="./bin/owpengram-server"
[[ -f "bin/owpengram-server.exe" ]] && BIN="./bin/owpengram-server.exe"
$BIN >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
echo "[ok] owpengram-server started (PID ${SERVER_PID}), logs -> ${SERVER_LOG}"

# Start owpengram-admin-panel (admin panel)
ADMIN_BIN="./bin/owpengram-admin-panel"
[[ -f "bin/owpengram-admin-panel.exe" ]] && ADMIN_BIN="./bin/owpengram-admin-panel.exe"
$ADMIN_BIN >>"$ADMIN_LOG" 2>&1 &
ADMIN_PID=$!
echo "[ok] owpengram-admin-panel started (PID ${ADMIN_PID}), logs -> ${ADMIN_LOG}"

echo
echo "============================================"
echo " OwpenGram server is running"
echo "============================================"
echo ""
echo " Logs:"
echo "   owpengram-server:       tail -f ${SERVER_LOG}"
echo "   owpengram-admin-panel:  tail -f ${ADMIN_LOG}"
echo "============================================"

# --- Interactive menu -------------------------------------------------------
show_menu() {
  echo
  echo "  [1] View owpengram-server logs (last 50 lines)"
  echo "  [2] View owpengram-admin-panel logs (last 50 lines)"
  echo "  [3] View both logs (last 50 lines)"
  echo "  [4] Tail owpengram-server logs (live)"
  echo "  [5] Tail owpengram-admin-panel logs (live)"
  echo "  [q] Stop server and exit"
  echo
}

while true; do
  # Check if processes are still alive
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "[WARN] owpengram-server (PID ${SERVER_PID}) exited unexpectedly"
    echo "       Check ${SERVER_LOG} for details"
    kill "$ADMIN_PID" 2>/dev/null || true
    break
  fi
  if ! kill -0 "$ADMIN_PID" 2>/dev/null; then
    echo "[WARN] owpengram-admin-panel (PID ${ADMIN_PID}) exited unexpectedly"
    echo "       Check ${ADMIN_LOG} for details"
    kill "$SERVER_PID" 2>/dev/null || true
    break
  fi

  show_menu
  read -rp "  Choice: " choice
  case "$choice" in
    1) tail -n 50 "$SERVER_LOG" 2>/dev/null || echo "  (no logs yet)" ;;
    2) tail -n 50 "$ADMIN_LOG" 2>/dev/null || echo "  (no logs yet)" ;;
    3) echo "  --- owpengram-server ---" ; tail -n 50 "$SERVER_LOG" 2>/dev/null || echo "  (no logs yet)"
       echo "  --- owpengram-admin-panel ---" ; tail -n 50 "$ADMIN_LOG" 2>/dev/null || echo "  (no logs yet)" ;;
    4) echo "  Press Ctrl+C to stop tailing"; tail -f "$SERVER_LOG" ;;
    5) echo "  Press Ctrl+C to stop tailing"; tail -f "$ADMIN_LOG" ;;
    q|Q) break ;;
    *) echo "  Invalid choice" ;;
  esac
done
