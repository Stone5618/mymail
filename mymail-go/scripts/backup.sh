#!/bin/bash
# MyMail 备份脚本
# P1-6 修复：排除 .env 密钥文件 + AES-256 加密 + SHA256 完整性校验
#
# 用法：
#   export BACKUP_PASSWORD='your-secret-password'
#   ./scripts/backup.sh
#
# 恢复：
#   openssl enc -d -aes-256-cbc -pbkdf2 -in <file>.enc -out backup.tar.gz -pass env:BACKUP_PASSWORD
#   sha256sum -c <file>.sha256
#   tar -xzf backup.tar.gz -C /target/path
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="$PROJECT_DIR/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/mymail_backup_$TIMESTAMP.tar.gz"
ENC_FILE="$BACKUP_FILE.enc"
CHECKSUM_FILE="$BACKUP_FILE.sha256"

# 加密密钥从环境变量读取
if [ -z "$BACKUP_PASSWORD" ]; then
  echo "❌ 未设置 BACKUP_PASSWORD 环境变量"
  echo "   请先执行: export BACKUP_PASSWORD='your-secret-password'"
  exit 1
fi

mkdir -p "$BACKUP_DIR"

echo "📧 创建备份（P1-6 修复：不含 .env 密钥文件）..."

# P1-6 修复：从备份中排除 .env 文件，避免 JWT_SECRET、ADMIN_PASSWORD 等密钥泄露
tar -czf "$BACKUP_FILE" \
  -C "$PROJECT_DIR" \
  data/mymail.db \
  data/maildir \
  data/attachments \
  2>/dev/null || true

# 生成完整性校验和（SHA256）
sha256sum "$BACKUP_FILE" > "$CHECKSUM_FILE"

# AES-256-CBC + PBKDF2 加密
openssl enc -aes-256-cbc -pbkdf2 -salt \
  -in "$BACKUP_FILE" \
  -out "$ENC_FILE" \
  -pass env:BACKUP_PASSWORD

# 删除未加密的明文备份（仅保留加密版本）
rm "$BACKUP_FILE"

echo "✅ 加密备份已创建: $ENC_FILE"
echo "   完整性校验和: $CHECKSUM_FILE"
echo "   大小: $(du -h "$ENC_FILE" | cut -f1)"

# 清理旧备份（保留最近 7 个）
cd "$BACKUP_DIR"
ls -t mymail_backup_*.tar.gz.enc 2>/dev/null | tail -n +8 | xargs -r rm
ls -t mymail_backup_*.tar.gz.sha256 2>/dev/null | tail -n +8 | xargs -r rm
echo "   旧备份已清理（保留最近 7 个）"

echo ""
echo "📋 恢复命令："
echo "   openssl enc -d -aes-256-cbc -pbkdf2 -in $ENC_FILE -out backup.tar.gz -pass env:BACKUP_PASSWORD"
echo "   sha256sum -c $CHECKSUM_FILE  # 验证完整性"
echo "   tar -xzf backup.tar.gz -C /target/path"
