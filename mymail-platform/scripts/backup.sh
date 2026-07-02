#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="$PROJECT_DIR/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/mymail_backup_$TIMESTAMP.tar.gz"

mkdir -p "$BACKUP_DIR"

echo "📧 Creating backup: $BACKUP_FILE"

tar -czf "$BACKUP_FILE" \
  -C "$PROJECT_DIR" \
  data/mymail.db \
  data/maildir \
  data/attachments \
  .env \
  2>/dev/null || true

echo "✅ Backup created: $BACKUP_FILE"
echo "   Size: $(du -h "$BACKUP_FILE" | cut -f1)"

# Clean old backups (keep last 7)
cd "$BACKUP_DIR"
ls -t mymail_backup_*.tar.gz 2>/dev/null | tail -n +8 | xargs -r rm
echo "   Old backups cleaned (keeping last 7)"
