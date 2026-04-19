#!/usr/bin/env bash
#
# scripts/restore.sh <timestamp> — 恢复 Weave 备份；可选 PITR
#
# 用法：
#   scripts/restore.sh                           # 恢复最近一次备份
#   scripts/restore.sh 20260419T030000Z          # 恢复 ≤ 该时间点的最近备份
#   scripts/restore.sh latest                    # 同上：最近一次
#
# 行为：
#   1. 在 BACKUP_DIR 下挑选符合 timestamp 的备份目录 (≤ 指定时间的最新)
#   2. pg_restore 到目标 DSN (--clean --if-exists 让目标库幂等重建)
#   3. tar -xzf 把 media tarball 还原到 WEAVE_DATA_DIR/media
#   4. 如果备份内有 WAL 段且 PITR_TARGET_TIME 指定，则将 WAL 复制到
#      WAL_RESTORE_DIR 并提示用户配置 recovery_target_time（PITR 需要 Postgres
#      停机 + 写 standby.signal + recovery.signal，本脚本只负责把素材
#      就位，不主动重启数据库以避免误操作）。
#
# 退出码：0 成功；1 找不到匹配备份；2 缺工具；3 校验失败。
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PG_DSN="${PG_DSN:-postgres://weave:weave@localhost:5432/weave?sslmode=disable}"
WEAVE_DATA_DIR="${WEAVE_DATA_DIR:-$ROOT/data}"
BACKUP_DIR="${BACKUP_DIR:-$ROOT/backups}"
WAL_RESTORE_DIR="${WAL_RESTORE_DIR:-$WEAVE_DATA_DIR/wal_restore}"
PITR_TARGET_TIME="${PITR_TARGET_TIME:-}"

TARGET="${1:-latest}"

log() { printf '[restore] %s\n' "$*" >&2; }

if [ ! -d "$BACKUP_DIR" ]; then
  log "ERROR: BACKUP_DIR=$BACKUP_DIR does not exist"
  exit 1
fi

# 选择匹配的备份目录：listing 升序，过滤 ≤ TARGET，取最后一项
pick_backup() {
  local target="$1"
  local choice=""
  # shellcheck disable=SC2012
  for d in $(ls -1 "$BACKUP_DIR" 2>/dev/null | sort); do
    [ -d "$BACKUP_DIR/$d" ] || continue
    [ -f "$BACKUP_DIR/$d/db.dump" ] || continue
    if [ "$target" = "latest" ] || [ "$d" \< "$target" ] || [ "$d" = "$target" ]; then
      choice="$d"
    fi
  done
  if [ -z "$choice" ]; then
    return 1
  fi
  echo "$choice"
}

CHOICE="$(pick_backup "$TARGET" || true)"
if [ -z "$CHOICE" ]; then
  log "ERROR: no backup matches '$TARGET' under $BACKUP_DIR"
  exit 1
fi
SRC="$BACKUP_DIR/$CHOICE"
log "selected backup: $SRC"

# 校验 manifest sha256 (best-effort — 老备份可能没有 manifest)
if [ -f "$SRC/manifest.json" ] && command -v shasum >/dev/null 2>&1; then
  WANT="$(grep -o '"sha256": *"[a-f0-9]*"' "$SRC/manifest.json" | head -1 | sed 's/.*"\([a-f0-9]*\)"/\1/')"
  GOT="$(shasum -a 256 "$SRC/db.dump" | awk '{print $1}')"
  if [ -n "$WANT" ] && [ "$WANT" != "$GOT" ]; then
    log "ERROR: db.dump sha256 mismatch — backup corrupted"
    log "  want $WANT"
    log "  got  $GOT"
    exit 3
  fi
fi

# 1. pg_restore
if ! command -v pg_restore >/dev/null 2>&1; then
  log "ERROR: pg_restore not found in PATH"
  exit 2
fi
log "pg_restore → $PG_DSN"
pg_restore --dbname="$PG_DSN" --clean --if-exists --no-owner --no-privileges \
  --exit-on-error "$SRC/db.dump"

# 2. media — 解压前先备份现有目录避免覆盖性事故
if [ -f "$SRC/media.tar.gz" ]; then
  if [ -d "$WEAVE_DATA_DIR/media" ]; then
    BAK="$WEAVE_DATA_DIR/media.pre-restore.$(date -u +%Y%m%dT%H%M%SZ)"
    log "moving existing media → $BAK"
    mv "$WEAVE_DATA_DIR/media" "$BAK"
  fi
  mkdir -p "$WEAVE_DATA_DIR"
  log "tar -xzf $SRC/media.tar.gz → $WEAVE_DATA_DIR"
  tar -C "$WEAVE_DATA_DIR" -xzf "$SRC/media.tar.gz"
elif [ -f "$SRC/media.tar.gz.skipped" ]; then
  log "media archive was skipped at backup time — nothing to restore"
fi

# 3. PITR 准备 — 只把 WAL 段拷到 WAL_RESTORE_DIR，并打印操作指引
if [ -n "$PITR_TARGET_TIME" ]; then
  if [ ! -d "$SRC/wal" ] || [ -z "$(ls -A "$SRC/wal" 2>/dev/null)" ]; then
    log "ERROR: PITR_TARGET_TIME=$PITR_TARGET_TIME 指定，但备份不含 WAL 段"
    exit 3
  fi
  mkdir -p "$WAL_RESTORE_DIR"
  log "stage WAL segments → $WAL_RESTORE_DIR"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a "$SRC/wal/" "$WAL_RESTORE_DIR/"
  else
    cp -R "$SRC/wal/." "$WAL_RESTORE_DIR/"
  fi
  cat <<EOF >&2
[restore] PITR 准备就绪。后续手动步骤：
  1. 在 postgresql.conf 设置：
       restore_command = 'cp $WAL_RESTORE_DIR/%f %p'
       recovery_target_time = '$PITR_TARGET_TIME'
       recovery_target_action = 'promote'
  2. 在数据目录创建空的 recovery.signal 文件
  3. 重启 Postgres；恢复完成后 recovery.signal 被自动删除
EOF
fi

log "DONE — restored $CHOICE to $PG_DSN"
echo "$SRC"
