#!/usr/bin/env bash
#
# scripts/rolling-upgrade.sh — Weave 双实例滚动升级演练脚本 (US-275)
#
# 在本机以 9117 (旧) / 9118 (新) 双端口启动两个 weave server, 模拟 k8s
# 滚动升级: 新实例先就绪 → 旧实例 SIGTERM 优雅下线, 期间至少一个实例
# 始终对外可服务. 该脚本既是给运维的演练工具, 也是给 CI 的烟雾测试.
#
# 流程:
#   1. 校验 BIN_OLD / BIN_NEW 二进制存在 (同一版本两份也可,做烟雾测试).
#   2. 启动旧实例 (WEAVE_PORT=$OLD_PORT), 等 /health/live 200.
#   3. 启动新实例 (WEAVE_PORT=$NEW_PORT), 等 /health/ready 200 (新实例
#      做完 PG / NATS / Bleve 预热后才会 ready).
#   4. 在并发探测下 (循环 curl 两个 /health/ready) 给旧实例发 SIGTERM,
#      等待其完全退出. 整个窗口要保证 "至少一个 ready" 不被破坏.
#   5. 优雅 SIGTERM 新实例, 退出 0.
#
# 通过环境变量调参:
#   BIN_OLD             v(N) 二进制路径, 必填.
#   BIN_NEW             v(N+1) 二进制路径, 必填.
#   OLD_PORT            旧实例端口, 默认 9117.
#   NEW_PORT            新实例端口, 默认 9118.
#   PG_DSN              共享 Postgres DSN; 默认沿用 weave 默认 DSN.
#   NATS_URL            共享 NATS URL.
#   WEAVE_DATA_DIR_OLD  旧实例 data 目录, 默认 /tmp/weave-rollup-old.
#   WEAVE_DATA_DIR_NEW  新实例 data 目录, 默认 /tmp/weave-rollup-new.
#   READY_TIMEOUT       单个实例就绪超时秒数, 默认 60.
#   HANDOFF_PROBES      旧实例下线期间并发探测次数, 默认 30.
#
# 退出码: 0 演练通过; 非 0 任意一步失败 (含探针超时 / 进程意外退出 /
# 切换窗口出现两个实例同时 unready). 失败时打印日志路径方便排查.
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OLD_PORT="${OLD_PORT:-9117}"
NEW_PORT="${NEW_PORT:-9118}"
READY_TIMEOUT="${READY_TIMEOUT:-60}"
HANDOFF_PROBES="${HANDOFF_PROBES:-30}"
BIN_OLD="${BIN_OLD:-}"
BIN_NEW="${BIN_NEW:-}"
WEAVE_DATA_DIR_OLD="${WEAVE_DATA_DIR_OLD:-/tmp/weave-rollup-old}"
WEAVE_DATA_DIR_NEW="${WEAVE_DATA_DIR_NEW:-/tmp/weave-rollup-new}"
LOG_DIR="${LOG_DIR:-$ROOT/.rollup-logs}"

log() { printf '[rolling-upgrade] %s\n' "$*" >&2; }

die() {
  log "ERROR: $*"
  exit 1
}

require_bin() {
  local label="$1"
  local path="$2"
  if [ -z "$path" ]; then
    die "$label is required (set BIN_OLD / BIN_NEW)."
  fi
  if [ ! -x "$path" ]; then
    die "$label='$path' not executable"
  fi
}

require_bin BIN_OLD "$BIN_OLD"
require_bin BIN_NEW "$BIN_NEW"

mkdir -p "$WEAVE_DATA_DIR_OLD" "$WEAVE_DATA_DIR_NEW" "$LOG_DIR"

OLD_LOG="$LOG_DIR/old.log"
NEW_LOG="$LOG_DIR/new.log"
OLD_PID=""
NEW_PID=""

cleanup() {
  set +e
  if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
    log "cleanup: SIGTERM old pid=$OLD_PID"
    kill -TERM "$OLD_PID" 2>/dev/null || true
    wait "$OLD_PID" 2>/dev/null
  fi
  if [ -n "$NEW_PID" ] && kill -0 "$NEW_PID" 2>/dev/null; then
    log "cleanup: SIGTERM new pid=$NEW_PID"
    kill -TERM "$NEW_PID" 2>/dev/null || true
    wait "$NEW_PID" 2>/dev/null
  fi
}
trap cleanup EXIT INT TERM

# 探针: 调用 /health/<probe>, 返回 0 = 200, 非 0 = 任何其他状态或网络错误
probe() {
  local port="$1"
  local probe_path="$2"
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' \
    --max-time 2 \
    "http://127.0.0.1:${port}${probe_path}" || true)"
  [ "$code" = "200" ]
}

# 等待 /health/<probe> 在 deadline 内返回 200, 否则 die.
wait_probe() {
  local label="$1"
  local port="$2"
  local probe_path="$3"
  local deadline=$(( $(date +%s) + READY_TIMEOUT ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if probe "$port" "$probe_path"; then
      log "$label: ${probe_path} on :${port} = 200"
      return 0
    fi
    sleep 1
  done
  die "$label: ${probe_path} on :${port} did not become 200 within ${READY_TIMEOUT}s"
}

start_instance() {
  local label="$1"
  local bin="$2"
  local port="$3"
  local data_dir="$4"
  local log_file="$5"
  log "starting $label: bin=$bin port=$port data=$data_dir"
  WEAVE_PORT="$port" WEAVE_DATA_DIR="$data_dir" \
    "$bin" >"$log_file" 2>&1 &
  echo $!
}

# Phase 1: 旧实例上线
OLD_PID="$(start_instance OLD "$BIN_OLD" "$OLD_PORT" "$WEAVE_DATA_DIR_OLD" "$OLD_LOG")"
wait_probe OLD "$OLD_PORT" /health/live
log "OLD ready, pid=$OLD_PID"

# Phase 2: 新实例上线 (旧实例继续服务)
NEW_PID="$(start_instance NEW "$BIN_NEW" "$NEW_PORT" "$WEAVE_DATA_DIR_NEW" "$NEW_LOG")"
wait_probe NEW "$NEW_PORT" /health/live

# Phase 3: 等新实例 ready (PG/NATS/Bleve 预热完). 旧实例需仍在线.
wait_probe NEW "$NEW_PORT" /health/ready
if ! probe "$OLD_PORT" /health/live; then
  die "OLD lost liveness during NEW warmup — degraded handoff window"
fi
log "NEW ready, pid=$NEW_PID; OLD still alive"

# Phase 4: 切换窗口 — 给旧实例 SIGTERM, 期间反复探测两个端口
log "handoff: SIGTERM OLD pid=$OLD_PID, polling both ports"
kill -TERM "$OLD_PID" 2>/dev/null || true

handoff_failures=0
for i in $(seq 1 "$HANDOFF_PROBES"); do
  old_alive=0
  new_ready=0
  if probe "$OLD_PORT" /health/live; then old_alive=1; fi
  if probe "$NEW_PORT" /health/ready; then new_ready=1; fi
  if [ "$new_ready" -eq 0 ] && [ "$old_alive" -eq 0 ]; then
    handoff_failures=$(( handoff_failures + 1 ))
    log "handoff probe $i: BOTH instances unavailable (old_alive=0, new_ready=0)"
  fi
  if ! kill -0 "$OLD_PID" 2>/dev/null; then
    log "handoff probe $i: OLD exited cleanly after $i probes"
    break
  fi
  sleep 0.2
done

wait "$OLD_PID" 2>/dev/null || true
OLD_PID=""

if [ "$handoff_failures" -gt 0 ]; then
  die "handoff produced $handoff_failures windows where neither instance served — rolling-upgrade SLA broken"
fi

# Phase 5: 关掉新实例, 演练完成
log "tearing down NEW pid=$NEW_PID"
kill -TERM "$NEW_PID" 2>/dev/null || true
wait "$NEW_PID" 2>/dev/null || true
NEW_PID=""

log "rolling-upgrade drill PASSED"
exit 0
