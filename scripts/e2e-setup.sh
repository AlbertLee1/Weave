#!/usr/bin/env bash
# scripts/e2e-setup.sh — idempotent stack startup for Playwright E2E gates.
#
# Brings up the full Weave stack required by `cd web && npm run test:e2e`:
#   1. docker compose postgres + nats (healthy)
#   2. bin/weave listening on :9117 (/health returning 200)
#   3. Vite dev server on :5173 (serving index.html)
#
# Safe to run repeatedly: re-uses processes that are already healthy and
# short-circuits instead of starting duplicates. PID files live under
# .e2e-pids/ and log output under .e2e-logs/. Stop everything with
# scripts/e2e-teardown.sh (or `make e2e-down`).
#
# Part of Phase 6 gate US-029.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PID_DIR="$ROOT/.e2e-pids"
LOG_DIR="$ROOT/.e2e-logs"
mkdir -p "$PID_DIR" "$LOG_DIR"

WEAVE_PID_FILE="$PID_DIR/weave.pid"
VITE_PID_FILE="$PID_DIR/vite.pid"
WEAVE_LOG="$LOG_DIR/weave.log"
VITE_LOG="$LOG_DIR/vite.log"

WEAVE_HEALTH_URL="http://localhost:9117/health"
VITE_URL="http://localhost:5173"

log()  { printf '[e2e-setup] %s\n' "$*"; }
warn() { printf '[e2e-setup] WARN: %s\n' "$*" >&2; }
fail() { printf '[e2e-setup] ERROR: %s\n' "$*" >&2; exit 1; }

pid_alive() {
  local pid="${1:-}"
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

read_pid() {
  local file="$1"
  [ -f "$file" ] || { printf ''; return; }
  tr -d '[:space:]' < "$file"
}

http_ok() {
  curl -sfo /dev/null --max-time 2 "$1"
}

wait_http_ok() {
  local url="$1" timeout="${2:-60}" i
  for i in $(seq 1 "$timeout"); do
    if http_ok "$url"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# ── 1. Docker services ──────────────────────────────────────────────
log "Ensuring Docker services (postgres + nats) are up..."
docker compose up -d --wait postgres nats

# ── 2. Build weave server binary ────────────────────────────────────
log "Building bin/weave..."
go build -o bin/weave ./cmd/server

# ── 3. Weave API server on :9117 ────────────────────────────────────
existing_weave_pid="$(read_pid "$WEAVE_PID_FILE")"
if pid_alive "$existing_weave_pid" && http_ok "$WEAVE_HEALTH_URL"; then
  log "weave server already healthy (pid=$existing_weave_pid) — reusing"
else
  rm -f "$WEAVE_PID_FILE"
  if http_ok "$WEAVE_HEALTH_URL"; then
    fail "Port 9117 already serving /health but no tracked PID file. Run scripts/e2e-teardown.sh or free the port, then retry."
  fi

  log "Starting weave server on :9117 (logs: $WEAVE_LOG)"
  : > "$WEAVE_LOG"
  nohup ./bin/weave >> "$WEAVE_LOG" 2>&1 &
  echo $! > "$WEAVE_PID_FILE"

  if ! wait_http_ok "$WEAVE_HEALTH_URL" 60; then
    warn "weave server did not become healthy within 60s — dumping tail"
    tail -50 "$WEAVE_LOG" >&2 || true
    fail "weave server failed to start"
  fi
  log "weave server ready (pid=$(read_pid "$WEAVE_PID_FILE"))"
fi

# ── 4. Vite dev server on :5173 ─────────────────────────────────────
existing_vite_pid="$(read_pid "$VITE_PID_FILE")"
if pid_alive "$existing_vite_pid" && http_ok "$VITE_URL"; then
  log "Vite dev server already up (pid=$existing_vite_pid) — reusing"
else
  rm -f "$VITE_PID_FILE"
  if http_ok "$VITE_URL"; then
    fail "Port 5173 already serving but no tracked PID file. Run scripts/e2e-teardown.sh or free the port, then retry."
  fi

  if [ ! -d web/node_modules ]; then
    log "Installing frontend dependencies..."
    (cd web && npm install --silent)
  fi

  log "Starting Vite dev server on :5173 (logs: $VITE_LOG)"
  : > "$VITE_LOG"
  (
    cd web
    nohup npx vite --clearScreen false --host 127.0.0.1 --port 5173 >> "$VITE_LOG" 2>&1 &
    echo $! > "$VITE_PID_FILE"
  )

  if ! wait_http_ok "$VITE_URL" 60; then
    warn "Vite dev server did not respond within 60s — dumping tail"
    tail -50 "$VITE_LOG" >&2 || true
    fail "Vite dev server failed to start"
  fi
  log "Vite dev server ready (pid=$(read_pid "$VITE_PID_FILE"))"
fi

cat <<EOF
[e2e-setup] Stack is up.
    API    http://localhost:9117
    Web    http://localhost:5173
    Logs   $LOG_DIR/{weave,vite}.log
Run tests:   (cd web && npm run test:e2e)
Stop stack:  scripts/e2e-teardown.sh   (or: make e2e-down)
EOF
