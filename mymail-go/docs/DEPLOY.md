# MyMail 部署指南

本文档描述 MyMail Go 后端的部署方式，包括开发环境、Docker 部署、裸机部署三种模式。

- 项目根目录：`mymail-go/`
- Go module：`github.com/mymail/mymail-go`
- 前端：Vue 3 SPA，构建产物通过 `web/embed.go` 的 `//go:embed all:dist` 嵌入到 Go 二进制
- 数据库：SQLite（`modernc.org/sqlite`，纯 Go 无 CGO 依赖），路径 `/app/data/mymail.db`

---

## 1. 系统要求

### 1.1 开发环境

| 依赖 | 最低版本 | 验证命令 |
|---|---|---|
| Go | 1.25+ | `go version` |
| Node.js | 18+（构建前端，推荐 22） | `node -v` |
| npm | 9+ | `npm -v` |
| Git | 2.20+ | `git --version` |
| golangci-lint | 1.55+ | `golangci-lint --version` |
| OpenSSL | 1.1.1+ | `openssl version` |

### 1.2 运行环境

| 组件 | 最低版本 | 说明 |
|---|---|---|
| Docker | 24+ | Docker Compose 部署 |
| Docker Compose | v2+ | `docker compose` 命令（非旧版 `docker-compose`） |
| SQLite | 3.35+ | 支持 `ALTER TABLE ... ADD COLUMN`（仅裸机直连时需要；Go 内置 modernc.org/sqlite 自带） |
| Alpine Linux | 3.20+ | Docker 运行时基础镜像 |
| Nginx | 1.18+ | 反向代理 + TLS 终止 |
| Dovecot | 2.3+ | IMAP 服务 |
| Postfix | 3.5+ | 出站 SMTP 中继 |

### 1.3 端口规划

| 端口 | 协议 | 用途 | 服务 |
|---|---|---|---|
| 80 | HTTP | Nginx 入口（重定向到 443） | nginx |
| 443 | HTTPS | Nginx HTTPS 入口 | nginx |
| 25 | SMTP | 接收外部邮件 | app（Go 内置 SMTP Receiver） |
| 3000 | HTTP | Go 后端 API + SPA（内部端口） | app |
| 587 | SMTP | Postfix 出站中继（submission） | postfix |
| 993 | IMAPS | Dovecot IMAP over SSL | dovecot |

---

## 2. 开发环境搭建

### 2.1 克隆与依赖

```bash
cd /path/to/mymail/mymail-go

# 下载 Go 依赖
go mod download

# 构建前端（可选，开发模式下可由 vite dev server 代理）
cd ../mymail-vue
npm ci
npm run build
# 将构建产物复制到 Go embed 目录
cp -r dist/* ../mymail-go/web/dist/

cd ../mymail-go
```

### 2.2 配置环境变量

```bash
cp .env.example .env

# 编辑 .env，必须设置以下项：
#   JWT_SECRET      （openssl rand -hex 32 生成，≥ 32 字符）
#   DOMAIN          （你的域名）
#   ADMIN_PASSWORD  （openssl rand -hex 16 生成）
#   CORS_ALLOWED_ORIGINS （生产环境必填）
```

### 2.3 构建与运行

```bash
# 方式 A：make 一键构建
make build
# 产物：dist/mymail

# 方式 B：go run 开发模式（无需构建）
make dev
# 等价于 go run ./cmd/mymail

# 运行已构建的二进制
make run
```

### 2.4 测试

```bash
# 单元测试 + 覆盖率
make test

# 短测试（跳过集成测试）
make test-short

# 集成测试
make test-integration

# 端到端测试
make test-e2e

# 性能基准测试
make bench
```

### 2.5 代码质量

```bash
# golangci-lint
make lint

# 自动修复
make lint-fix

# 安全扫描（govulncheck + gosec）
make security
```

### 2.6 验证开发环境

```bash
# 健康检查
curl http://localhost:3000/healthz
# {"status":"alive","time":"..."}

# 就绪检查
curl http://localhost:3000/readyz
# {"status":"ready","checks":{"startup":"ready","db":"up"}}

# 指标
curl http://localhost:3000/metrics
# Prometheus 格式输出
```

---

## 3. Docker 部署

### 3.1 架构

Docker Compose 编排 4 个服务：

| 服务 | 镜像 | 端口 | 数据卷 | 说明 |
|---|---|---|---|---|
| `app` | 自构建（3 阶段） | 3000, 25 | `mymail-data` | Go 后端 + SMTP Receiver + SPA |
| `dovecot` | `dovecot/dovecot:latest` | 993 | `mymail-data`, `dovecot-ssl` | IMAP 服务，共享数据库与 maildir |
| `postfix` | `boky/postfix` | 587 | `postfix-spool` | 出站 SMTP 中继 |
| `nginx` | `nginx:alpine` | 80, 443 | `nginx-ssl` | 反向代理 + TLS 终止 |

### 3.2 Dockerfile（3 阶段构建）

```
Stage 1: node:22-alpine       构建前端 → /web/dist/
Stage 2: golang:1.25-alpine   构建后端（嵌入前端）→ /mymail
Stage 3: alpine:3.20          运行时，UID/GID 1000
```

关键设计：

- **CGO_ENABLED=0**：纯静态链接，适配 alpine 运行时
- **UID 1000**：与 Dovecot `user_query` 中的 `uid=1000 gid=1000` 一致，确保 maildir 文件权限匹配
- **tini**：作为 PID 1 处理信号，支持优雅关闭
- **HEALTHCHECK**：用 `wget -qO- http://localhost:3000/healthz`（无需认证）

### 3.3 构建与启动

> ⚠️ 构建上下文为项目根目录（`mymail/`），以同时访问 `mymail-go/` 和 `mymail-vue/`。

```bash
cd /path/to/mymail

# 准备环境变量
cp mymail-go/.env.example .env
# 编辑 .env 填入真实值

# 构建并启动（后台）
make -C mymail-go docker-up
# 等价于：docker compose -f mymail-go/deployments/docker/docker-compose.yml up -d

# 查看日志
make -C mymail-go docker-logs

# 查看状态
docker compose -f mymail-go/deployments/docker/docker-compose.yml ps

# 停止
make -C mymail-go docker-down
```

### 3.4 首次启动验证

```bash
# 1. 等待 app 健康检查通过（约 30 秒）
docker inspect --format='{{.State.Health.Status}}' mymail-app
# 预期：healthy

# 2. 健康检查
curl http://localhost:3000/healthz

# 3. 数据库迁移日志
docker logs mymail-app 2>&1 | grep -i "migrate\|migration"
# 预期：执行迁移 version=1..4

# 4. SMTP 端口监听
docker exec mymail-app wget -qO- http://localhost:3000/healthz
docker port mymail-app
# 25/tcp -> 0.0.0.0:25

# 5. 登录验证
curl -X POST http://localhost:3000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"<ADMIN_PASSWORD>"}'
```

---

## 4. 环境变量说明

完整环境变量见 `.env.example`。下表按类别说明所有变量。

### 4.1 基本信息

| 变量 | 默认值 | 说明 |
|---|---|---|
| `ENV` | `dev` | 环境标识，`prod` 时启用额外校验 |
| `DEBUG` | `true` | 调试模式（生产环境必须 `false`） |
| `PORT` | `3000` | HTTP 监听端口 |
| `HOST` | `0.0.0.0` | HTTP 监听地址 |

### 4.2 域名（必填）

| 变量 | 说明 |
|---|---|
| `DOMAIN` | 主域名，如 `example.com`（必填，校验） |
| `MAIL_HOST` | 邮件主机名，如 `mail.example.com` |

### 4.3 JWT 认证

| 变量 | 默认值 | 说明 |
|---|---|---|
| `JWT_SECRET` | （必填） | JWT 签名密钥，≥ 32 字符，不能是占位符（P0-8 强制校验） |
| `JWT_EXPIRES_IN` | `24h` | access token 有效期 |
| `JWT_REMEMBER_EXPIRES_IN` | `720h` | refresh token 有效期（30 天） |

### 4.4 数据库与存储

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DB_PATH` | `./data/mymail.db` | SQLite 数据库路径 |
| `MAILDIR_PATH` | `./data/maildir` | Maildir 邮件存储 |
| `ATTACHMENT_PATH` | `./data/attachments` | 附件存储 |
| `MAX_ATTACHMENT_SIZE` | `26214400`（25MB） | 附件大小上限 |

### 4.5 管理员（必填）

| 变量 | 默认值 | 说明 |
|---|---|---|
| `ADMIN_USERNAME` | `admin` | 管理员用户名 |
| `ADMIN_PASSWORD` | （必填） | 管理员初始密码 |
| `ADMIN_EMAIL` | （必填） | 管理员邮箱 |

### 4.6 SMTP

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SMTP_PORT` | `25` | 入站 SMTP 监听端口 |
| `SMTP_SEND_HOST` | `postfix` | 出站 SMTP 中继主机 |
| `SMTP_SEND_PORT` | `587` | 出站 SMTP 端口 |
| `SMTP_SEND_USERNAME` | （空） | 出站 SASL 用户名 |
| `SMTP_SEND_PASSWORD` | （空） | 出站 SASL 密码 |
| `SMTP_TLS_REJECT_UNAUTHORIZED` | `true` | TLS 证书校验（P1-11 修复：默认 true） |
| `SMTP_TLS_CERT` | （空） | TLS 证书路径 |
| `SMTP_TLS_KEY` | （空） | TLS 私钥路径 |
| `SMTP_MAX_CONNECTIONS_PER_IP` | `10` | 单 IP 连接数上限 |
| `SMTP_RATE_WINDOW_MS` | `60000` | 连接限流窗口（毫秒） |

### 4.7 反垃圾

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SPAM_THRESHOLD` | `10` | 拒收评分阈值 |
| `SPAM_SUSPICIOUS_THRESHOLD` | `5` | 可疑评分阈值 |
| `SPF_ENABLED` | `true` | SPF 校验开关 |
| `SPF_MAX_DEPTH` | `10` | SPF include 递归深度上限（RFC 7208） |
| `DNSBL_ENABLED` | `true` | DNSBL 校验开关 |
| `DNSBL_ZONES` | `zen.spamhaus.org,...` | DNSBL zone 列表（逗号分隔） |
| `DNSBL_QUERY_TIMEOUT_MS` | `3000` | DNSBL 单次查询超时 |
| `SPF_QUERY_TIMEOUT_MS` | `3000` | SPF 单次查询超时 |

### 4.8 灰名单

| 变量 | 默认值 | 说明 |
|---|---|---|
| `GREYLIST_DELAY_MS` | `300000`（5 分钟） | 灰名单延迟重试时间 |
| `GREYLIST_TTL_MS` | `3600000`（1 小时） | 灰名单条目有效期 |

### 4.9 限流

| 变量 | 默认值 | 说明 |
|---|---|---|
| `RATE_LIMIT_MAX` | `100` | HTTP 通用限流 |
| `SEND_RATE_LIMIT_PER_MIN` | `10` | 每用户每分钟发信上限 |

### 4.10 出站 SMTP 熔断器（gobreaker/v2）

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SMTP_CIRCUIT_BREAKER_ENABLED` | `true` | 熔断器开关 |
| `SMTP_CIRCUIT_BREAKER_MAX_REQUESTS` | `5` | 半开状态最大请求数 |
| `SMTP_CIRCUIT_BREAKER_INTERVAL_MS` | `60000` | closed 状态计数窗口 |
| `SMTP_CIRCUIT_BREAKER_TIMEOUT_MS` | `30000` | open 状态持续时间 |
| `SMTP_CIRCUIT_BREAKER_FAILURE_RATIO` | `0.6` | 失败率阈值 |
| `SMTP_CIRCUIT_BREAKER_MIN_REQUESTS` | `10` | 触发熔断最小请求数 |

### 4.11 出站 SMTP 重试（backoff）

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SMTP_RETRY_ENABLED` | `true` | 重试开关 |
| `SMTP_RETRY_MAX_ATTEMPTS` | `3` | 最大重试次数 |
| `SMTP_RETRY_INITIAL_INTERVAL_MS` | `500` | 初始退避间隔 |
| `SMTP_RETRY_MAX_INTERVAL_MS` | `10000` | 最大退避间隔 |
| `SMTP_RETRY_MAX_ELAPSED_TIME_MS` | `60000` | 最大总耗时 |

### 4.12 规则引擎（ReDoS 防护）

| 变量 | 默认值 | 说明 |
|---|---|---|
| `RULES_MAX_PATTERN_LENGTH` | `500` | 正则 pattern 最大长度 |
| `RULES_COMPILE_TIMEOUT_MS` | `2000` | 正则编译超时 |

### 4.13 CORS

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CORS_ALLOWED_ORIGINS` | （生产必填） | 允许的源（逗号分隔） |

### 4.14 可观测性

| 变量 | 默认值 | 说明 |
|---|---|---|
| `LOG_LEVEL` | `info` | 日志级别（debug/info/warn/error） |
| `LOG_FORMAT` | `json` | 日志格式（json/text） |
| `METRICS_ENABLED` | `true` | Prometheus 指标开关 |
| `METRICS_PATH` | `/metrics` | 指标端点路径 |
| `TRACING_ENABLED` | `false` | OpenTelemetry 追踪开关 |
| `TRACING_ENDPOINT` | （空） | OTLP exporter 端点 |
| `TRACING_SAMPLE_RATE` | `0.1` | 采样率 |

### 4.15 审计

| 变量 | 默认值 | 说明 |
|---|---|---|
| `AUDIT_ENABLED` | `true` | 审计日志开关 |
| `AUDIT_RETENTION_DAYS` | `365` | 审计日志保留天数 |

### 4.16 特性开关

| 变量 | 默认值 | 说明 |
|---|---|---|
| `FEATURE_SPAM_FILTER` | `true` | 反垃圾过滤 |
| `FEATURE_WS_NOTIFY` | `true` | WebSocket 实时通知 |
| `FEATURE_GREYLIST` | `true` | 灰名单 |
| `FEATURE_API_KEY` | `true` | API Key 认证 |
| `FEATURE_RULES` | `true` | 邮件规则引擎 |
| `FEATURE_AUDIT` | `true` | 审计日志 |
| `FEATURE_REGISTRATION` | `true` | 用户注册 |

---

## 5. 裸机部署（systemd）

### 5.1 一键部署脚本

```bash
cd /path/to/mymail/mymail-go
sudo bash scripts/setup.sh
```

`setup.sh` 执行 8 个步骤：

1. 安装系统依赖（nginx、dovecot、sqlite3、openssl、golang-go）
2. 创建 `vmail` 用户（UID 5000）
3. 构建前端 → 复制到 `web/dist/`
4. 构建 Go 二进制 → `/usr/local/bin/mymail`
5. 初始化数据目录
6. 申请 SSL 证书（certbot 或自签名）
7. 配置 Dovecot（vmail UID 5000）
8. 配置 Nginx + systemd 服务

### 5.2 手动部署

```bash
# 1. 构建前端
cd /path/to/mymail/mymail-vue
npm ci && npm run build
cp -r dist/* /path/to/mymail/mymail-go/web/dist/

# 2. 构建 Go 二进制
cd /path/to/mymail/mymail-go
CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/mymail ./cmd/mymail

# 3. 创建 vmail 用户
useradd -u 5000 -d /var/mail -s /usr/sbin/nologin vmail

# 4. 创建数据目录
mkdir -p /var/lib/mymail/data/maildir /var/lib/mymail/data/attachments
chown -R vmail:vmail /var/lib/mymail

# 5. 创建 systemd 服务
cat > /etc/systemd/system/mymail.service << 'EOF'
[Unit]
Description=MyMail Go Backend
After=network.target

[Service]
Type=simple
User=vmail
Group=vmail
WorkingDirectory=/var/lib/mymail
EnvironmentFile=/var/lib/mymail/.env
ExecStart=/usr/local/bin/mymail
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now mymail
```

---

## 6. Nginx 反向代理配置

完整配置见 `config/nginx/default.conf`。要点：

```nginx
# Rate limiting
limit_req_zone $binary_remote_addr zone=general:10m rate=10r/s;
limit_req_zone $binary_remote_addr zone=login:10m rate=5r/m;

# HTTP -> HTTPS
server {
    listen 80;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}

# HTTPS
server {
    listen 443 ssl http2;
    ssl_certificate     /etc/nginx/ssl/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    # 安全头
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    client_max_body_size 30M;

    # WebSocket（长连接，86400s 读超时）
    location /ws {
        proxy_pass http://app:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }

    # 登录端点（更严格限流）
    location /api/auth/login {
        proxy_pass http://app:3000;
        limit_req zone=login burst=3 nodelay;
    }

    # 默认：反代到 Go app
    location / {
        proxy_pass http://app:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        limit_req zone=general burst=20 nodelay;
    }
}
```

> Docker 环境下 Nginx 通过容器名 `app:3000` 访问后端；裸机环境改为 `127.0.0.1:3000`。

---

## 7. Dovecot 配置

### 7.1 dovecot.conf（`config/dovecot/dovecot.conf`）

```
protocols = imap
disable_plaintext_auth = yes
ssl = required
ssl_cert = </etc/dovecot/ssl/fullchain.pem
ssl_key = </etc/dovecot/ssl/privkey.pem
ssl_min_protocol = TLSv1.2
auth_mechanisms = plain login

passdb {
    driver = sql
    args = /etc/dovecot/dovecot-sql.conf
}
userdb {
    driver = sql
    args = /etc/dovecot/dovecot-sql.conf
}

mail_location = maildir:/app/data/maildir/%d/%n
mail_home = /app/data/maildir/%d/%n

namespace inbox {
    separator = /
    inbox = yes
    mailbox Drafts { special_use = \Drafts; auto = subscribe; }
    mailbox Sent   { special_use = \Sent;   auto = subscribe; }
    mailbox Trash  { special_use = \Trash;  auto = subscribe; }
    mailbox Junk   { special_use = \Junk;   auto = subscribe; }
}

service imap-login {
    inet_listener imap { port = 0 }          # 禁用明文 IMAP
    inet_listener imaps { port = 993 }
}

default_process_limit = 100
default_client_limit = 1000
```

### 7.2 dovecot-sql.conf（`config/dovecot/dovecot-sql.conf`）

```
driver = sqlite
connect = /app/data/mymail.db

# P0-1 修复：列名 password_hash（Go 版 schema）
password_query = SELECT \
    email AS user, \
    password_hash, \
    '%w' AS userdb_home, \
    '/app/data/maildir/%d/%n' AS userdb_mail \
    FROM users \
    WHERE email = '%u' AND is_active = 1

# P1-10 修复：uid/gid 固定 1000（与 Dockerfile USER 1000 一致）
user_query = SELECT \
    '/app/data/maildir/%d/%n' AS home, \
    'maildir:/app/data/maildir/%d/%n' AS mail, \
    1000 AS uid, \
    1000 AS gid \
    FROM users \
    WHERE email = '%u' AND is_active = 1

# Go 版用 bcrypt，Dovecot 用 BLF-CRYPT 兼容
default_pass_scheme = BLF-CRYPT
```

> ⚠️ UID/GID 关键约束：
> - Docker 环境：`1000:1000`（与 Dockerfile `USER mymail` 一致）
> - 裸机 setup.sh 环境：`5000:5000`（与 `vmail` 用户一致）
>
> 两者不可混用，否则 maildir 文件权限不匹配导致 Dovecot 无法读取邮件。

### 7.3 Dovecot 数据卷

Docker 环境下，`dovecot` 容器与 `app` 容器共享 `mymail-data` 数据卷，确保两者访问同一份 `mymail.db` 和 `maildir/`。

---

## 8. Postfix 配置要点

Postfix 容器（`boky/postfix`）仅用于出站邮件中继，不处理入站。配置要点：

```yaml
# docker-compose.yml 中的 postfix 服务
postfix:
  image: boky/postfix
  environment:
    - HOSTNAME=${DOMAIN:-mail.example.com}
    - ALLOWED_SENDER_DOMAINS=${DOMAIN:-example.com}
    - INBOUND_ENABLED=false          # 关闭入站（入站由 Go SMTP Receiver 处理）
  ports:
    - "587:587"
```

Go 后端通过 `SMTP_SEND_HOST=postfix` + `SMTP_SEND_PORT=587` 将外域邮件交给 Postfix 中继发送。Postfix 负责最终的 MX 投递与重试。

---

## 9. TLS 证书配置

### 9.1 Let's Encrypt（推荐，生产环境）

```bash
# 申请证书
certbot certonly --standalone -d mail.example.com -d example.com \
  --non-interactive --agree-tos --email admin@example.com

# 证书路径：
#   /etc/letsencrypt/live/mail.example.com/fullchain.pem
#   /etc/letsencrypt/live/mail.example.com/privkey.pem

# 自动续期（crontab）
0 3 * * * certbot renew --quiet --post-hook "systemctl reload nginx dovecot"
```

### 9.2 Docker 环境

将证书挂载到 nginx 与 dovecot 容器：

```yaml
# docker-compose.yml 追加
nginx:
  volumes:
    - /etc/letsencrypt/live/mail.example.com/fullchain.pem:/etc/nginx/ssl/fullchain.pem:ro
    - /etc/letsencrypt/live/mail.example.com/privkey.pem:/etc/nginx/ssl/privkey.pem:ro

dovecot:
  volumes:
    - /etc/letsencrypt/live/mail.example.com/fullchain.pem:/etc/dovecot/ssl/fullchain.pem:ro
    - /etc/letsencrypt/live/mail.example.com/privkey.pem:/etc/dovecot/ssl/privkey.pem:ro
```

### 9.3 自签名证书（仅测试）

```bash
mkdir -p /etc/ssl/mymail
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/ssl/mymail/privkey.pem \
  -out /etc/ssl/mymail/fullchain.pem \
  -subj "/CN=mail.example.com"
```

---

## 10. DKIM 生成

```bash
cd /path/to/mymail/mymail-go
bash scripts/gen-dkim.sh
```

脚本执行：

1. 生成 2048 位 RSA 密钥对到 `/etc/ssl/dkim/`：
   - `default.private`（私钥，Postfix 挂载使用）
   - `default.public`（公钥）
2. 输出 DNS TXT 记录配置：

```dns
# DKIM
default._domainkey.example.com.  TXT  "v=DKIM1; k=rsa; p=<公钥内容>"

# SPF
example.com.  TXT  "v=spf1 ip4:<服务器IP> mx ~all"

# DMARC
_dmarc.example.com.  TXT  "v=DMARC1; p=none; rua=mailto:admin@example.com"

# MX
example.com.  MX  10  mail.example.com.
```

> ⚠️ DNS 记录生效需要时间（TTL），建议提前 24 小时配置并验证后再切换流量。

验证 DNS 记录：

```bash
# 验证 MX
dig MX example.com +short

# 验证 SPF
dig TXT example.com +short

# 验证 DKIM
dig TXT default._domainkey.example.com +short

# 验证 DMARC
dig TXT _dmarc.example.com +short
```

---

## 11. 备份策略

### 11.1 备份脚本

```bash
# 设置加密密码
export BACKUP_PASSWORD='your-strong-backup-password'

# 执行备份
cd /path/to/mymail/mymail-go
./scripts/backup.sh
```

`backup.sh` 执行：

1. 打包 `data/mymail.db` + `data/maildir/` + `data/attachments/`
2. **排除 `.env`**（P1-6 修复，避免 JWT_SECRET/ADMIN_PASSWORD 泄露）
3. 生成 SHA256 完整性校验和
4. AES-256-CBC + PBKDF2 加密
5. 删除明文备份，仅保留加密版本
6. 保留最近 7 份备份，自动清理旧备份

### 11.2 定时备份

```bash
# crontab 每日凌晨 3 点备份
0 3 * * * BACKUP_PASSWORD='your-password' /path/to/mymail/mymail-go/scripts/backup.sh >> /var/log/mymail-backup.log 2>&1
```

### 11.3 恢复

```bash
# 1. 解密
openssl enc -d -aes-256-cbc -pbkdf2 \
  -in backups/mymail_backup_YYYYMMDD_HHMMSS.tar.gz.enc \
  -out /tmp/backup.tar.gz \
  -pass env:BACKUP_PASSWORD

# 2. 校验完整性
sha256sum -c backups/mymail_backup_YYYYMMDD_HHMMSS.tar.gz.sha256

# 3. 停服后恢复
systemctl stop mymail
tar -xzf /tmp/backup.tar.gz -C /path/to/mymail/mymail-go/
systemctl start mymail

# 4. 清理
rm /tmp/backup.tar.gz
```

---

## 12. 首次部署 Checklist

部署完成后，逐项确认：

### 12.1 服务健康

- [ ] `curl http://localhost:3000/healthz` 返回 200
- [ ] `curl http://localhost:3000/readyz` 返回 `{"status":"ready"}`
- [ ] `docker ps` 显示所有容器 `Up` 且 `healthy`
- [ ] 数据库迁移日志显示 version 1-4 全部完成

### 12.2 网络与端口

- [ ] `telnet mail.example.com 25` 可连接（SMTP 入站）
- [ ] `telnet mail.example.com 443` 可连接（HTTPS）
- [ ] `telnet mail.example.com 993` 可连接（IMAPS）
- [ ] 防火墙放行 25/443/993 端口

### 12.3 DNS

- [ ] MX 记录指向 `mail.example.com`
- [ ] SPF 记录包含服务器 IP
- [ ] DKIM 记录已生效（`dig TXT default._domainkey.example.com`）
- [ ] DMARC 记录已配置
- [ ] PTR 反向解析已配置（避免外发邮件被拒）

### 12.4 功能验证

- [ ] Web 界面可访问（`https://mail.example.com`）
- [ ] 管理员可登录
- [ ] IMAP 客户端可登录并收邮件
- [ ] 发送测试邮件到外部邮箱并确认收到
- [ ] 从外部邮箱发送邮件到本域并确认收到
- [ ] WebSocket 实时通知工作（新邮件到达时前端刷新）
- [ ] `/metrics` 端点返回 Prometheus 格式数据

### 12.5 安全

- [ ] `.env` 中所有 `CHANGE_ME` 已替换
- [ ] `JWT_SECRET` 为随机 32+ 字符串
- [ ] `ADMIN_PASSWORD` 已更改（非默认值）
- [ ] `CORS_ALLOWED_ORIGINS` 已正确配置
- [ ] 备份密码 `BACKUP_PASSWORD` 已妥善保管
- [ ] 防火墙仅放行必要端口
- [ ] TLS 证书有效（非自签名，或自签名仅用于测试）

### 12.6 监控

- [ ] `/metrics` 已接入 Prometheus 采集
- [ ] 告警规则已配置（SMTP 中断、DB 锁、磁盘满）
- [ ] 日志已配置轮转（避免磁盘占满）
