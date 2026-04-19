#!/usr/bin/env bash
#
# scripts/backup.sh — Weave 单机版每日备份脚本
#
# 三个组成部分：
#   1. pg_dump custom-format dump (postgres 全量逻辑备份)
#   2. data/media 目录 tar.gz (内容寻址 blob)
#   3. WAL archive 复制 (PITR 所需的事务日志增量)
#
# 输出布局 (BACKUP_DIR/<UTC timestamp>/...)：
#   <stamp>/db.dump          pg_restore 兼容的 custom-format
#   <stamp>/media.tar.gz     data/media tarball
#   <stamp>/wal/             archive_command 同步过来的 WAL 段
#   <stamp>/manifest.json    备份元数据 (timestamp / sizes / sha256)
#
# 通过环境变量调参：
#   PG_DSN          libpq DSN，默认 postgres://weave:weave@localhost:5432/weave?sslmode=disable
#   WEAVE_DATA_DIR  Weave data root，默认 ./data
#   BACKUP_DIR      备份输出根目录，默认 ./backups
#   WAL_ARCHIVE_DIR Postgres archive_command 落盘的目录，默认 $WEAVE_DATA_DIR/wal_archive
#   RETAIN_DAYS     仅保留最近 N 天，默认 14
#
# 退出码：
#   0 成功；非 0 任意一步失败。pg_dump / tar 错误立即终止，避免半成品备份。
#
# 配套 PITR 配置 (postgresql.conf, 一次性设置)：
#   wal_level = replica
#   archive_mode = on
#   archive_command = 'test ! -f /var/lib/postgresql/wal_archive/%f && cp %p /var/lib/postgresql/wal_archive/%f'
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PG_DSN="${PG_DSN:-postgres://weave:weave@localhost:5432/weave?sslmode=disable}"
WEAVE_DATA_DIR="${WEAVE_DATA_DIR:-$ROOT/data}"
BACKUP_DIR="${BACKUP_DIR:-$ROOT/backups}"
WAL_ARCHIVE_DIR="${WAL_ARCHIVE_DIR:-$WEAVE_DATA_DIR/wal_archive}"
RETAIN_DAYS="${RETAIN_DAYS:-14}"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DEST="$BACKUP_DIR/$STAMP"

log() { printf '[backup %s] %s\n' "$STAMP" "$*" >&2; }

mkdir -p "$DEST/wal"

# 1. pg_dump — custom format, --create 让 pg_restore 可以独立重建数据库
log "pg_dump → $DEST/db.dump"
if ! command -v pg_dump >/dev/null 2>&1; then
  log "ERROR: pg_dump not found in PATH"
  exit 2
fi
pg_dump --dbname="$PG_DSN" --format=custom --no-owner --no-privileges \
  --file="$DEST/db.dump"

# 2. media tarball
if [ -d "$WEAVE_DATA_DIR/media" ]; then
  log "tar media → $DEST/media.tar.gz"
  tar -C "$WEAVE_DATA_DIR" -czf "$DEST/media.tar.gz" media
else
  log "WARN: $WEAVE_DATA_DIR/media missing, skipping media archive"
  : > "$DEST/media.tar.gz.skipped"
fi

# 3. WAL archive — 用于 PITR；archive_command 必须事先把 WAL 写到 $WAL_ARCHIVE_DIR
if [ -d "$WAL_ARCHIVE_DIR" ]; then
  log "rsync WAL segments from $WAL_ARCHIVE_DIR"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a "$WAL_ARCHIVE_DIR/" "$DEST/wal/"
  else
    cp -R "$WAL_ARCHIVE_DIR/." "$DEST/wal/"
  fi
else
  log "WARN: WAL_ARCHIVE_DIR=$WAL_ARCHIVE_DIR missing — PITR 不可用 (仅有逻辑全量)"
fi

# 4. manifest — sha256 + sizes 便于校验
DB_SHA="$(shasum -a 256 "$DEST/db.dump" | awk '{print $1}')"
DB_SIZE="$(wc -c < "$DEST/db.dump" | tr -d ' ')"
MEDIA_SHA=""
MEDIA_SIZE=0
if [ -f "$DEST/media.tar.gz" ]; then
  MEDIA_SHA="$(shasum -a 256 "$DEST/media.tar.gz" | awk '{print $1}')"
  MEDIA_SIZE="$(wc -c < "$DEST/media.tar.gz" | tr -d ' ')"
fi
WAL_COUNT="$(find "$DEST/wal" -type f 2>/dev/null | wc -l | tr -d ' ')"

cat > "$DEST/manifest.json" <<EOF
{
  "version": 1,
  "timestamp": "$STAMP",
  "pg_dsn": "$(printf '%s' "$PG_DSN" | sed 's#://[^@]*@#://***@#')",
  "db": {"path": "db.dump", "sha256": "$DB_SHA", "size": $DB_SIZE},
  "media": {"path": "media.tar.gz", "sha256": "$MEDIA_SHA", "size": $MEDIA_SIZE},
  "wal": {"dir": "wal", "segment_count": $WAL_COUNT}
}
EOF

# 5. 保留策略 — 删除超过 RETAIN_DAYS 的旧备份
if [ "$RETAIN_DAYS" -gt 0 ] && [ -d "$BACKUP_DIR" ]; then
  log "prune backups older than $RETAIN_DAYS days under $BACKUP_DIR"
  find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -mtime "+$RETAIN_DAYS" \
    -exec rm -rf {} +
fi

log "DONE — $DEST (db=$DB_SIZE bytes, media=$MEDIA_SIZE bytes, wal_segments=$WAL_COUNT)"
echo "$DEST"
