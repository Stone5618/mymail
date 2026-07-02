#!/bin/bash
set -e

echo "📧 MyMail Platform - One-Click Setup"
echo "======================================"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Check root
if [ "$EUID" -ne 0 ]; then
  echo -e "${RED}Please run as root (sudo ./setup.sh)${NC}"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo -e "\n${YELLOW}[1/7] Installing system dependencies...${NC}"
apt-get update -qq
apt-get install -y -qq nginx dovecot-core dovecot-imapd sqlite3 openssl curl > /dev/null 2>&1
echo -e "${GREEN}✓ System dependencies installed${NC}"

echo -e "\n${YELLOW}[2/7] Installing Node.js dependencies...${NC}"
cd "$PROJECT_DIR"
npm install --production 2>&1 | tail -3
echo -e "${GREEN}✓ Node.js dependencies installed${NC}"

echo -e "\n${YELLOW}[3/7] Initializing database...${NC}"
node scripts/init-db.js
echo -e "${GREEN}✓ Database initialized${NC}"

echo -e "\n${YELLOW}[4/7] Building CSS...${NC}"
npx tailwindcss -i ./public/css/input.css -o ./public/css/style.css --minify 2>&1 | tail -1
echo -e "${GREEN}✓ CSS built${NC}"

echo -e "\n${YELLOW}[5/7] Generating SSL certificate...${NC}"
# Read domain from .env if available
if [ -f "$PROJECT_DIR/.env" ]; then
  DOMAIN=$(grep -E '^DOMAIN=' "$PROJECT_DIR/.env" | cut -d'=' -f2 | tr -d '[:space:]')
fi
DOMAIN="${DOMAIN:-localhost}"
if [ ! -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  # Install certbot
  apt-get install -y -qq certbot python3-certbot-nginx > /dev/null 2>&1
  
  # For DuckDNS, use standalone mode
  echo -e "${YELLOW}Requesting SSL certificate for $DOMAIN...${NC}"
  certbot certonly --standalone -d "$DOMAIN" --non-interactive --agree-tos --email admin@$DOMAIN || {
    echo -e "${YELLOW}SSL certificate request failed. Using self-signed cert for now.${NC}"
    mkdir -p /etc/ssl/mymail
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
      -keyout /etc/ssl/mymail/privkey.pem \
      -out /etc/ssl/mymail/fullchain.pem \
      -subj "/CN=$DOMAIN" 2>/dev/null
    echo -e "${GREEN}✓ Self-signed certificate generated${NC}"
  }
else
  echo -e "${GREEN}✓ SSL certificate already exists${NC}"
fi

echo -e "\n${YELLOW}[6/7] Configuring Nginx...${NC}"
# Determine SSL cert path
if [ -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  SSL_CERT="/etc/letsencrypt/live/$DOMAIN/fullchain.pem"
  SSL_KEY="/etc/letsencrypt/live/$DOMAIN/privkey.pem"
else
  SSL_CERT="/etc/ssl/mymail/fullchain.pem"
  SSL_KEY="/etc/ssl/mymail/privkey.pem"
fi

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

    # Security headers
    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header X-XSS-Protection "1; mode=block";

    # Web app
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

    # WebSocket
    location /ws {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
    }

    # Upload limit
    client_max_body_size 30M;
}
NGINX_EOF

ln -sf /etc/nginx/sites-available/mymail /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t 2>&1 && systemctl reload nginx
echo -e "${GREEN}✓ Nginx configured${NC}"

echo -e "\n${YELLOW}[7/7] Configuring Dovecot...${NC}"
cat > /etc/dovecot/dovecot.conf << 'DOVECOT_EOF'
protocols = imap

listen = *, ::

ssl = required
ssl_cert = </etc/letsencrypt/live/$DOMAIN/fullchain.pem
ssl_key = </etc/letsencrypt/live/$DOMAIN/privkey.pem

# If using self-signed cert, uncomment:
# ssl_cert = </etc/ssl/mymail/fullchain.pem
# ssl_key = </etc/ssl/mymail/privkey.pem

mail_location = maildir:$PROJECT_DIR/data/maildir/%d/%n

auth_mechanisms = plain login

userdb {
  driver = static
  args = uid=root gid=root
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
default_pass_scheme = bcrypt
password_query = SELECT email as user, password_hash as password FROM users WHERE email = '%u' AND is_active = 1
user_query = SELECT '$PROJECT_DIR/data/maildir/' || substr(email, instr(email, '@') + 1) || '/' || username AS home, 'root' AS uid, 'root' AS gid FROM users WHERE email = '%u'
SQL_EOF

systemctl restart dovecot 2>/dev/null || true
echo -e "${GREEN}✓ Dovecot configured${NC}"

# Create systemd service
echo -e "\n${YELLOW}Creating systemd service...${NC}"
cat > /etc/systemd/system/mymail.service << SERVICE_EOF
[Unit]
Description=MyMail Platform
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$PROJECT_DIR
ExecStart=/usr/bin/node src/server.js
Restart=always
RestartSec=5
Environment=NODE_ENV=production

[Install]
WantedBy=multi-user.target
SERVICE_EOF

systemctl daemon-reload
systemctl enable mymail
systemctl start mymail

echo -e "\n${GREEN}======================================"
echo -e "📧 MyMail Platform Setup Complete!"
echo -e "======================================${NC}"
echo ""

# Read admin password from .env or prompt
ADMIN_PASS=""
if [ -f "$PROJECT_DIR/.env" ]; then
  ADMIN_PASS=$(grep -E '^ADMIN_PASSWORD=' "$PROJECT_DIR/.env" | cut -d'=' -f2- | tr -d '[:space:]')
fi
if [ -z "$ADMIN_PASS" ] || [ "$ADMIN_PASS" = "CHANGE_ME" ]; then
  echo -e "${YELLOW}Admin password not set in .env. Please set ADMIN_PASSWORD in $PROJECT_DIR/.env${NC}"
  ADMIN_PASS="{YOUR_PASSWORD}"
fi

echo "  Web:     https://$DOMAIN"
echo "  Admin:   stone@${DOMAIN} / ${ADMIN_PASS}"
echo "  IMAP:    $DOMAIN:993 (SSL)"
echo "  SMTP:    $DOMAIN:465 (SSL)"
echo ""
echo -e "${YELLOW}⚠️  Don't forget to:${NC}"
echo "  1. Update DuckDNS IP: curl 'https://www.duckdns.org/update?domains=$DOMAIN&token={YOUR_TOKEN}&ip='"
echo "  2. Configure DNS: MX, SPF, DKIM, DMARC records"
echo "  3. Open port 25 in cloud firewall if needed"
echo ""
