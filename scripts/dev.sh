#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Colors
CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
DIM='\033[2m'
RESET='\033[0m'

PIDS=()

cleanup() {
  echo ""
  echo -e "${YELLOW}Shutting down...${RESET}"
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null
  echo -e "${GREEN}All services stopped.${RESET}"
}
trap cleanup EXIT INT TERM

log() {
  echo -e "${CYAN}[weave]${RESET} $1"
}

# ── 1. Docker services ──────────────────────────────────────────────
log "Starting Docker services (PostgreSQL + NATS)..."
docker compose up -d --wait 2>&1 | sed "s/^/  ${DIM}/"
echo -e "${RESET}"
log "${GREEN}Docker services ready${RESET}"

# ── 2. Frontend dependencies ────────────────────────────────────────
if [ ! -d web/node_modules ]; then
  log "Installing frontend dependencies..."
  (cd web && npm install --silent)
fi

# ── 3. Build & start Go server ──────────────────────────────────────
log "Building Go server..."
go build -o bin/weave ./cmd/server

export PG_DSN="postgres://weave:weave@localhost:5432/weave?sslmode=disable"
export NATS_URL="nats://localhost:4222"

log "Starting Go server on ${GREEN}:8080${RESET}"
./bin/weave &
PIDS+=($!)

# Wait for Go server to be ready
for i in $(seq 1 30); do
  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done

if ! curl -sf http://localhost:8080/health >/dev/null 2>&1; then
  echo -e "${RED}Go server failed to start${RESET}"
  exit 1
fi

log "${GREEN}Go server ready${RESET}"

# ── 4. Start Vite dev server ────────────────────────────────────────
log "Starting Vite dev server on ${GREEN}:5173${RESET}"
(cd web && npx vite --clearScreen false) &
PIDS+=($!)

# ── Ready ───────────────────────────────────────────────────────────
sleep 1
echo ""
echo -e "${CYAN}╔══════════════════════════════════════════════╗${RESET}"
echo -e "${CYAN}║${RESET}  Weave Dev Environment Ready                 ${CYAN}║${RESET}"
echo -e "${CYAN}╠══════════════════════════════════════════════╣${RESET}"
echo -e "${CYAN}║${RESET}                                              ${CYAN}║${RESET}"
echo -e "${CYAN}║${RESET}  WebUI:   ${GREEN}http://localhost:5173${RESET}              ${CYAN}║${RESET}"
echo -e "${CYAN}║${RESET}  API:     ${GREEN}http://localhost:8080${RESET}              ${CYAN}║${RESET}"
echo -e "${CYAN}║${RESET}  PG:      ${DIM}localhost:5432${RESET}                     ${CYAN}║${RESET}"
echo -e "${CYAN}║${RESET}  NATS:    ${DIM}localhost:4222${RESET}                     ${CYAN}║${RESET}"
echo -e "${CYAN}║${RESET}                                              ${CYAN}║${RESET}"
echo -e "${CYAN}║${RESET}  Press ${YELLOW}Ctrl+C${RESET} to stop all services         ${CYAN}║${RESET}"
echo -e "${CYAN}╚══════════════════════════════════════════════╝${RESET}"
echo ""

wait
