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
# "telesrv" naming permanently. See deploy/migrate-docker-naming.ps1.
NAMING_OUT="$(powershell -NoProfile -ExecutionPolicy Bypass -File deploy/migrate-docker-naming.ps1)" \
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

# --- Build ------------------------------------------------------------------
step "[3/4] Building server binaries"
if [ "$NO_BUILD" = true ]; then
  log "skipping build (--no-build)"
  if [[ ! -f "bin/telesrv" ]] && [[ ! -f "bin/telesrv.exe" ]]; then
    die "no binaries found in bin/ — run without --no-build first"
  fi
else
  mkdir -p bin
  echo "  building telesrv ..."
  go build -o bin/telesrv ./cmd/telesrv
  echo "  building telesrv-admin ..."
  go build -o bin/telesrv-admin ./cmd/telesrv-admin
  echo "[ok] binaries built in bin/"
fi

# --- Start servers ----------------------------------------------------------
step "[4/4] Starting telesrv + telesrv-admin"

mkdir -p "$LOG_DIR"
TELESRV_LOG="$LOG_DIR/telesrv.log"
ADMIN_LOG="$LOG_DIR/telesrv-admin.log"

cleanup() {
  echo
  echo "[stop] stopping telesrv and telesrv-admin ..."
  kill "$TELESRV_PID" 2>/dev/null || true
  kill "$ADMIN_PID" 2>/dev/null || true
  wait "$TELESRV_PID" 2>/dev/null || true
  wait "$ADMIN_PID" 2>/dev/null || true
  echo "[ok] stopped."
}
trap cleanup EXIT INT TERM

# Start telesrv (main server)
BIN="./bin/telesrv"
[[ -f "bin/telesrv.exe" ]] && BIN="./bin/telesrv.exe"
$BIN >>"$TELESRV_LOG" 2>&1 &
TELESRV_PID=$!
echo "[ok] telesrv started (PID ${TELESRV_PID}), logs -> ${TELESRV_LOG}"

# Start telesrv-admin (admin panel)
ADMIN_BIN="./bin/telesrv-admin"
[[ -f "bin/telesrv-admin.exe" ]] && ADMIN_BIN="./bin/telesrv-admin.exe"
$ADMIN_BIN >>"$ADMIN_LOG" 2>&1 &
ADMIN_PID=$!
echo "[ok] telesrv-admin started (PID ${ADMIN_PID}), logs -> ${ADMIN_LOG}"

echo
echo "============================================"
echo " OwpenGram server is running"
echo "============================================"
echo ""
echo " Logs:"
echo "   telesrv:        tail -f ${TELESRV_LOG}"
echo "   telesrv-admin:  tail -f ${ADMIN_LOG}"
echo "============================================"

# --- Interactive menu -------------------------------------------------------
show_menu() {
  echo
  echo "  [1] View telesrv logs (last 50 lines)"
  echo "  [2] View telesrv-admin logs (last 50 lines)"
  echo "  [3] View both logs (last 50 lines)"
  echo "  [4] Tail telesrv logs (live)"
  echo "  [5] Tail telesrv-admin logs (live)"
  echo "  [q] Stop server and exit"
  echo
}

while true; do
  # Check if processes are still alive
  if ! kill -0 "$TELESRV_PID" 2>/dev/null; then
    echo "[WARN] telesrv (PID ${TELESRV_PID}) exited unexpectedly"
    echo "       Check ${TELESRV_LOG} for details"
    kill "$ADMIN_PID" 2>/dev/null || true
    break
  fi
  if ! kill -0 "$ADMIN_PID" 2>/dev/null; then
    echo "[WARN] telesrv-admin (PID ${ADMIN_PID}) exited unexpectedly"
    echo "       Check ${ADMIN_LOG} for details"
    kill "$TELESRV_PID" 2>/dev/null || true
    break
  fi

  show_menu
  read -rp "  Choice: " choice
  case "$choice" in
    1) tail -n 50 "$TELESRV_LOG" 2>/dev/null || echo "  (no logs yet)" ;;
    2) tail -n 50 "$ADMIN_LOG" 2>/dev/null || echo "  (no logs yet)" ;;
    3) echo "  --- telesrv ---" ; tail -n 50 "$TELESRV_LOG" 2>/dev/null || echo "  (no logs yet)"
       echo "  --- telesrv-admin ---" ; tail -n 50 "$ADMIN_LOG" 2>/dev/null || echo "  (no logs yet)" ;;
    4) echo "  Press Ctrl+C to stop tailing"; tail -f "$TELESRV_LOG" ;;
    5) echo "  Press Ctrl+C to stop tailing"; tail -f "$ADMIN_LOG" ;;
    q|Q) break ;;
    *) echo "  Invalid choice" ;;
  esac
done
