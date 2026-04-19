#!/usr/bin/env bash
#
# scripts/test-restore.sh — 端到端验证 backup.sh + restore.sh 闭环
#
# 流程：
#   1. 在 PG_DSN 上的临时数据库 (weave_restore_test_<pid>) 上跑 backup.sh
#   2. drop 临时数据库重新建空库，在新库上跑 restore.sh
#   3. 比对几个核心表的 row count，确保数据一致；返回 0 即视为通过
#
# 该脚本以来源 DSN 为基准，把生成的备份恢复到一个隔离 DSN，避免污染源库。
# CI 中跑的轻量验证：只校验 schema_migrations 与若干核心表的行数。
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PG_DSN="${PG_DSN:-postgres://weave:weave@localhost:5432/weave?sslmode=disable}"
BACKUP_DIR="${BACKUP_DIR:-$ROOT/backups/test-restore}"

log() { printf '[test-restore] %s\n' "$*" >&2; }

if ! command -v psql >/dev/null 2>&1; then
  log "ERROR: psql not found in PATH"
  exit 2
fi

# 解析出 DSN 的各部分供 createdb / dropdb 使用
SRC_DB="$(printf '%s' "$PG_DSN" | sed -E 's#.*/([^?]+).*#\1#')"
TEST_DB="weave_restore_test_$$"
RESTORED_DSN="$(printf '%s' "$PG_DSN" | sed "s#/$SRC_DB\(?\\|\$\)#/$TEST_DB\1#")"

log "source DB:    $SRC_DB"
log "restored DB:  $TEST_DB"

cleanup() {
  log "drop $TEST_DB"
  psql "$PG_DSN" -v ON_ERROR_STOP=1 \
    -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='$TEST_DB';" \
    >/dev/null 2>&1 || true
  psql "$PG_DSN" -v ON_ERROR_STOP=1 \
    -c "DROP DATABASE IF EXISTS \"$TEST_DB\";" >/dev/null 2>&1 || true
  rm -rf "$BACKUP_DIR"
}
trap cleanup EXIT INT TERM

# 1. 备份源库
log "running backup.sh → $BACKUP_DIR"
mkdir -p "$BACKUP_DIR"
BACKUP_DIR="$BACKUP_DIR" "$ROOT/scripts/backup.sh" >/dev/null

# 2. 创建空目标库
log "create empty $TEST_DB"
psql "$PG_DSN" -v ON_ERROR_STOP=1 \
  -c "CREATE DATABASE \"$TEST_DB\";" >/dev/null

# 3. 恢复到目标库
log "running restore.sh latest → $TEST_DB"
PG_DSN="$RESTORED_DSN" BACKUP_DIR="$BACKUP_DIR" "$ROOT/scripts/restore.sh" latest >/dev/null

# 4. 行数比对 — 取 schema_migrations 与公开表里若干已知表
TABLES="schema_migrations ontologies object_types properties link_types"
PASS=1
for T in $TABLES; do
  EXISTS_SRC="$(psql "$PG_DSN" -tAc "SELECT to_regclass('public.$T') IS NOT NULL;" || echo f)"
  EXISTS_DST="$(psql "$RESTORED_DSN" -tAc "SELECT to_regclass('public.$T') IS NOT NULL;" || echo f)"
  if [ "$EXISTS_SRC" != "t" ]; then
    log "skip $T (missing in source)"
    continue
  fi
  if [ "$EXISTS_DST" != "t" ]; then
    log "FAIL: $T missing in restored DB"
    PASS=0
    continue
  fi
  SRC_COUNT="$(psql "$PG_DSN" -tAc "SELECT count(*) FROM \"$T\";")"
  DST_COUNT="$(psql "$RESTORED_DSN" -tAc "SELECT count(*) FROM \"$T\";")"
  if [ "$SRC_COUNT" = "$DST_COUNT" ]; then
    log "ok   $T: src=$SRC_COUNT dst=$DST_COUNT"
  else
    log "FAIL $T: src=$SRC_COUNT dst=$DST_COUNT"
    PASS=0
  fi
done

if [ "$PASS" -eq 1 ]; then
  log "test-restore PASSED"
  exit 0
fi
log "test-restore FAILED"
exit 1
