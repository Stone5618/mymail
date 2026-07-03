#!/bin/bash
# MyMail DKIM 密钥生成脚本
# 保留自原 mymail-platform 版本，无修改
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Read domain from .env if available
if [ -f "$PROJECT_DIR/.env" ]; then
  DOMAIN=$(grep -E '^DOMAIN=' "$PROJECT_DIR/.env" | cut -d'=' -f2 | tr -d '[:space:]')
fi
DOMAIN="${DOMAIN:-localhost}"
DKIM_DIR="/etc/ssl/dkim"
SELECTOR="default"

echo "🔑 Generating DKIM key pair..."

mkdir -p "$DKIM_DIR"

# Generate DKIM private key
openssl genrsa -out "$DKIM_DIR/$SELECTOR.private" 2048 2>/dev/null

# Generate DKIM public key
openssl rsa -in "$DKIM_DIR/$SELECTOR.private" -pubout -out "$DKIM_DIR/$SELECTOR.public" 2>/dev/null

# Extract public key for DNS record (remove headers and newlines)
PUB_KEY=$(grep -v "PUBLIC KEY" "$DKIM_DIR/$SELECTOR.public" | tr -d '\n')

echo ""
echo "✅ DKIM keys generated at $DKIM_DIR/"
echo ""
echo "📋 Add this DNS TXT record:"
echo ""
echo "  Name:  $SELECTOR._domainkey.$DOMAIN"
echo "  Value: v=DKIM1; k=rsa; p=${PUB_KEY}"
echo ""
echo "📋 Also add these DNS records:"
echo ""
echo "  SPF:   $DOMAIN  TXT  v=spf1 ip4:$(curl -s ifconfig.me) mx ~all"
echo "  DMARC: _dmarc.$DOMAIN  TXT  v=DMARC1; p=none; rua=mailto:admin@$DOMAIN"
echo ""
