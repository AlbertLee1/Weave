#!/usr/bin/env bash
# scripts/e2e-teardown.sh — idempotent shutdown for Playwright E2E gates.
#
# Counterpart to scripts/e2e-setup.sh. Stops (in order):
#   1. Vite dev server on :5173
#   2. bin/weave on :9117
#   3. docker compose services (postgres + nats)
#
# Safe to run repeatedly and safe to run when nothing is up — missing PID
# files and already-dead processes are reported but never fail the script.
#
# Part of Phase 6 gate US-029.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PID_DIR="$ROOT/.e2e-pids"
WEAVE_PID_FILE="$PID_DIR/weave.pid"
VITE_PID_FILE="$PID_DIR/vite.pid"

log()  { printf '[e2e-teardown] %s\n' "$*"; }
warn() { printf '[e2e-teardown] WARN: %s\n' "$*" >&2; }

kill_pid_file() {
  local label="$1" file="$2" pid i
  if [ ! -f "$file" ]; then
    log "$label: no PID file, nothing to stop"
    return 0
  fi
  pid="$(tr -d '[:space:]' < "$file")"
  if [ -z "$pid" ]; then
    warn "$label: PID file was empty, removing"
    rm -f "$file"
    return 0
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    log "$label: pid=$pid already gone"
    rm -f "$file"
    return 0
  fi

  log "$label: sending TERM to pid=$pid"
  kill "$pid" 2>/dev/null || true

  # Wait up to 10s for graceful shutdown
  for i in $(seq 1 20); do
    if ! kill -0 "$pid" 2>/dev/null; then
      break
    fi
    sleep 0.5
  done

  if kill -0 "$pid" 2>/dev/null; then
    warn "$label: pid=$pid still alive after 10s, sending KILL"
    kill -9 "$pid" 2>/dev/null || true
  fi
  rm -f "$file"
  log "$label: stopped"
}

# ── 1. Vite first so it stops proxying requests to the API ──────────
kill_pid_file "vite"  "$VITE_PID_FILE"

# ── 2. Weave API ────────────────────────────────────────────────────
kill_pid_file "weave" "$WEAVE_PID_FILE"

# ── 3. Docker services ──────────────────────────────────────────────
log "Stopping Docker services..."
docker compose down

# ── 4. Clean up empty PID directory ─────────────────────────────────
if [ -d "$PID_DIR" ]; then
  rmdir "$PID_DIR" 2>/dev/null || true
fi

log "Stack is down."
