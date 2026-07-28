#!/usr/bin/env bash
# Checks prerequisites (Go, Python 3, and its packages), then launches the
# interactive server-panel TUI (tui-panel/server-panel.py). Run this instead
# of the TUI directly so missing prerequisites get a clear message instead of
# a Python traceback.
set -uo pipefail
cd "$(dirname "$0")"

ok()   { echo "[ok] $*"; }
warn() { echo "[WARN] $*"; }
die()  { echo "[ERROR] $*" >&2; exit 1; }

echo "== Checking prerequisites =="

PROBLEMS=0

# --- Go -----------------------------------------------------------------
if command -v go >/dev/null 2>&1; then
  ok "Go found: $(go version)"
else
  warn "Go is not installed (needed to build owpengram-server / owpengram-admin-panel)"
  echo "       Install it from: https://go.dev/dl/"
  PROBLEMS=1
fi

# --- Python ---------------------------------------------------------------
PYTHON=""
for cand in python3 python; do
  if command -v "$cand" >/dev/null 2>&1; then
    PYTHON="$cand"
    break
  fi
done

if [[ -z "$PYTHON" ]]; then
  warn "Python 3 is not installed (needed to run the server-panel TUI)"
  echo "       Install it from: https://www.python.org/downloads/"
  PROBLEMS=1
else
  ok "Python found: $("$PYTHON" --version 2>&1) (${PYTHON})"
fi

# --- Python dependencies ----------------------------------------------------
if [[ -n "$PYTHON" ]]; then
  MISSING="$("$PYTHON" tui-panel/check_deps.py)"
  if [[ -n "$MISSING" ]]; then
    warn "Missing Python packages: $(echo "$MISSING" | tr '\n' ' ')"
    echo "       Install them with: $PYTHON -m pip install -r tui-panel/requirements-panel.txt"
    PROBLEMS=1
  else
    ok "Python dependencies OK (textual, psutil, cryptography)"
  fi
fi

if [[ "$PROBLEMS" -ne 0 ]]; then
  echo
  die "missing prerequisites above -- install them and re-run this script"
fi

echo
echo "[cfg] All prerequisites OK, launching server panel..."
echo
exec "$PYTHON" tui-panel/server-panel.py "$@"
