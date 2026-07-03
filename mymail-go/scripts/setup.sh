#!/bin/bash
# MyMail Go 后端一键部署脚本
# P1-12 修复：Dovecot 用 vmail 用户（UID 5000）而非 root
#
# 适配 Go 版本：构建 Go 二进制 + Vue 前端（//go:embed 嵌入）
set -e

echo "📧 MyMail Go 后端 - 一键部署"
echo "======================================"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Check root
if [ "$EUID" -ne 0 ]; then
  echo -e "${RED}请以 root 运行 (sudo ./setup.sh)${NC}"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
PARENT_DIR="$(dirname "$PROJECT_DIR")"
VUE_DIR="$PARENT_DIR/mymail-vue"

echo -e "\n${YELLOW}[1/8] 安装系统依赖...${NC}"
apt-get update -qq
apt-get install -y -qq nginx dovecot-core dovecot-imapd sqlite3 openssl curl golang-go > /dev/null 2>&1
echo -e "${GREEN}✓ 系统依赖已安装${NC}"

echo -e "\n${YELLOW}[2/8] 创建 vmail 用户（UID 5000）...${NC}"
# P1-12 修复：创建专用 vmail 用户替代 root 运行 Dovecot
if ! id -u vmail >/dev/null 2>&1; then
  useradd -u 5000 -d /var/mail -s /usr/sbin/nologin vmail
  echo -e "${GREEN}✓ vmail 用户已创建 (UID 5000)${NC}"
else
  echo -e "${GREEN}✓ vmail 用户已存在${NC}"
fi

echo -e "\n${YELLOW}[3/8] 构建前端...${NC}"
if [ -d "$VUE_DIR" ]; then
  cd "$VUE_DIR"
  if command -v npm >/dev/null 2>&1; then
    npm ci --no-audit --no-fund 2>&1 | tail -3
    npm run build 2>&1 | tail -3
    # 复制前端构建产物到 Go embed 目录
    mkdir -p "$PROJECT_DIR/web/dist"
    cp -r dist/* "$PROJECT_DIR/web/dist/"
    echo -e "${GREEN}✓ 前端已构建并复制到 web/dist/${NC}"
  else
    echo -e "${YELLOW}⚠ npm 未安装，跳过前端构建。Go 二进制将使用占位 dist/${NC}"
  fi
else
  echo -e "${YELLOW}⚠ 未找到 mymail-vue 目录，跳过前端构建${NC}"
fi

echo -e "\n${YELLOW}[4/8] 构建 Go 二进制...${NC}"
cd "$PROJECT_DIR"
CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo release)" \
  -o /usr/local/bin/mymail ./cmd/mymail
echo -e "${GREEN}✓ Go 二进制已构建: /usr/local/bin/mymail${NC}"

echo -e "\n${YELLOW}[5/8] 初始化数据库...${NC}"
# 创建数据目录
mkdir -p "$PROJECT_DIR/data/maildir" "$PROJECT_DIR/data/attachments"
# 数据库迁移由 Go 二进制启动时自动执行
echo -e "${GREEN}✓ 数据目录已创建${NC}"

echo -e "\n${YELLOW}[6/8] 生成 SSL 证书...${NC}"
if [ -f "$PROJECT_DIR/.env" ]; then
  DOMAIN=$(grep -E '^DOMAIN=' "$PROJECT_DIR/.env" | cut -d'=' -f2 | tr -d '[:space:]')
fi
DOMAIN="${DOMAIN:-localhost}"
if [ ! -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  apt-get install -y -qq certbot python3-certbot-nginx > /dev/null 2>&1
  echo -e "${YELLOW}正在申请 $DOMAIN 的 SSL 证书...${NC}"
  certbot certonly --standalone -d "$DOMAIN" --non-interactive --agree-tos --email admin@$DOMAIN || {
    echo -e "${YELLOW}SSL 申请失败，使用自签名证书${NC}"
    mkdir -p /etc/ssl/mymail
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
      -keyout /etc/ssl/mymail/privkey.pem \
      -out /etc/ssl/mymail/fullchain.pem \
      -subj "/CN=$DOMAIN" 2>/dev/null
    echo -e "${GREEN}✓ 自签名证书已生成${NC}"
  }
else
  echo -e "${GREEN}✓ SSL 证书已存在${NC}"
fi

echo -e "\n${YELLOW}[7/8] 配置 Dovecot（vmail UID 5000）...${NC}"
# 确定 SSL 证书路径
if [ -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  SSL_CERT="/etc/letsencrypt/live/$DOMAIN/fullchain.pem"
  SSL_KEY="/etc/letsencrypt/live/$DOMAIN/privkey.pem"
else
  SSL_CERT="/etc/ssl/mymail/fullchain.pem"
  SSL_KEY="/etc/ssl/mymail/privkey.pem"
fi

# P1-12 修复：Dovecot 使用 vmail 用户（UID 5000）而非 root
cat > /etc/dovecot/dovecot.conf << DOVECOT_EOF
protocols = imap
listen = *, ::
ssl = required
ssl_cert = <$SSL_CERT
ssl_key = <$SSL_KEY
mail_location = maildir:$PROJECT_DIR/data/maildir/%d/%n
mail_home = $PROJECT_DIR/data/maildir/%d/%n
auth_mechanisms = plain login
userdb {
  driver = static
  args = uid=5000 gid=5000 home=$PROJECT_DIR/data/maildir/%d/%n
}
passdb {
  driver = sql
  args = /etc/dovecot/dovecot-sql.conf
}
service imap {
  process_min_avail = 1
}
protocol imap {
  mail_max_userip_connections = 20
  imap_idle_notify_interval = 2 mins
}
log_path = /var/log/dovecot.log
DOVECOT_EOF

cat > /etc/dovecot/dovecot-sql.conf << SQL_EOF
driver = sqlite
connect = $PROJECT_DIR/data/mymail.db
default_pass_scheme = BLF-CRYPT
password_query = SELECT email AS user, password_hash, '%w' AS userdb_home, '$PROJECT_DIR/data/maildir/%d/%n' AS userdb_mail FROM users WHERE email = '%u' AND is_active = 1
user_query = SELECT '$PROJECT_DIR/data/maildir/%d/%n' AS home, 'maildir:$PROJECT_DIR/data/maildir/%d/%n' AS mail, 5000 AS uid, 5000 AS gid FROM users WHERE email = '%u' AND is_active = 1
SQL_EOF

# 设置 maildir 目录所有权为 vmail
chown -R vmail:vmail "$PROJECT_DIR/data/maildir"
systemctl restart dovecot 2>/dev/null || true
echo -e "${GREEN}✓ Dovecot 已配置（vmail UID 5000）${NC}"

echo -e "\n${YELLOW}[8/8] 配置 Nginx + systemd...${NC}"
cat > /etc/nginx/sites-available/mymail << NGINX_EOF
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl http2;
    server_name $DOMAIN;
    ssl_certificate $SSL_CERT;
    ssl_certificate_key $SSL_KEY;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
    client_max_body_size 30M;
}
NGINX_EOF

ln -sf /etc/nginx/sites-available/mymail /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t 2>&1 && systemctl reload nginx
echo -e "${GREEN}✓ Nginx 已配置${NC}"

# systemd 服务（以 vmail 用户运行 Go 二进制）
cat > /etc/systemd/system/mymail.service << SERVICE_EOF
[Unit]
Description=MyMail Go Backend
After=network.target

[Service]
Type=simple
User=vmail
Group=vmail
WorkingDirectory=$PROJECT_DIR
ExecStart=/usr/local/bin/mymail
Restart=always
RestartSec=5
Environment=NODE_ENV=production

[Install]
WantedBy=multi-user.target
SERVICE_EOF

systemctl daemon-reload
systemctl enable mymail
systemctl start mymail
echo -e "${GREEN}✓ systemd 服务已创建并启动${NC}"

echo -e "\n${GREEN}======================================"
echo -e "📧 MyMail 部署完成！"
echo -e "======================================${NC}"
echo ""
echo "  Web:     https://$DOMAIN"
echo "  IMAP:    $DOMAIN:993 (SSL)"
echo "  SMTP:    $DOMAIN:25"
echo ""
echo -e "${YELLOW}⚠️  请确保：${NC}"
echo "  1. 配置 DNS: MX, SPF, DKIM, DMARC 记录"
echo "  2. 运行 ./scripts/gen-dkim.sh 生成 DKIM 密钥"
echo "  3. 在 $PROJECT_DIR/.env 中设置所有 CHANGE_ME 值"
echo ""
