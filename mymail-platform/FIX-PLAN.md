# MyMail Platform — 完整修复与优化计划

> 最后更新：2026-05-02
> 基于 commit `447d30b` 的代码审计结果

---

## 目录

- [S0 — 安全加固（阻塞上线）](#s0--安全加固阻塞上线)
- [S1 — Docker 容器化部署](#s1--docker-容器化部署)
- [S2 — 工程地基](#s2--工程地基)
- [S3 — 差异化功能](#s3--差异化功能)
- [附录：文件变更清单](#附录文件变更清单)

---

## S0 — 安全加固（阻塞上线）

> 优先级：**最高**。以下任何一项未完成前，项目不应暴露在公网上。

### S0-1：清理硬编码凭据

**问题**：

- `DEPLOY.md` 第 68 行暴露真实 DuckDNS token `56b1583c-ad9b-4696-95b5-0892d59f9637`
- `scripts/setup.sh` 硬编码域名 `x5618.duckdns.org` 和管理员密码 `admin123`
- `.env.example` 中 `ADMIN_PASSWORD=admin123` 会被用户直接沿用

**修复方案**：

```
1. DEPLOY.md：所有 token/密码替换为占位符 {YOUR_TOKEN}、{YOUR_PASSWORD}
2. scripts/setup.sh：
   - 域名改为从 .env 读取，不硬编码
   - 管理员密码改为部署时交互式输入或从 .env 读取
   - 添加首次运行检测，强制用户修改默认密码
3. .env.example：ADMIN_PASSWORD 改为 CHANGE_ME_$(openssl rand -hex 8)
4. .gitignore：确认 .env 已在忽略列表中（当前已有 ✓）
```

**涉及文件**：`DEPLOY.md`、`scripts/setup.sh`、`.env.example`

---

### S0-2：SMTP 接收器安全加固

**问题**：

- `src/services/smtp-receiver.js` 设置 `authOptional: true`，任何人可以向你的域发送邮件
- `disabledCommands: ['STARTTLS']` 禁用了传输加密
- 无发件人验证（SPF/DKIM 检查），垃圾邮件零门槛

**修复方案**：

```
1. 启用 STARTTLS：
   - 移除 disabledCommands: ['STARTTLS']
   - 提供 TLS 证书路径（从 .env 读取）

2. 添加发件人验证（拒绝明显伪造）：
   - 检查 RCPT TO 是否为本地已知用户（当前已有 ✓，但可加固）
   - 添加 SPF 查询：对发件人域名做 DNS TXT 查询验证
   - 添加 DNSBL 查询：检查发件人 IP 是否在黑名单

3. 添加灰名单（Greylisting）：
   - 首次见到的 (sender_ip, sender, recipient) 三元组临时拒绝
   - 一定时间后重试则放行
   - 实现：用 SQLite 表记录，5-10 行代码

4. 连接级速率限制：
   - 单 IP 每分钟最多 N 次连接
   - 已有 express-rate-limit 用于 HTTP，SMTP 端需要独立实现
```

**新增文件**：`src/services/smtp-validator.js`（SPF/DNSBL 检查）
**修改文件**：`src/services/smtp-receiver.js`、`src/config.js`

---

### S0-3：出站邮件 TLS 强制

**问题**：

- `src/services/smtp-sender.js` 中 `rejectUnauthorized: false` 允许自签名证书，存在 MITM 风险

**修复方案**：

```js
// src/services/smtp-sender.js
// 修改前：
tls: { rejectUnauthorized: false }

// 修改后：
tls: { rejectUnauthorized: true }
// 开发环境可通过 .env 中 SMTP_TLS_REJECT=false 临时关闭
```

**涉及文件**：`src/services/smtp-sender.js`、`src/config.js`、`.env.example`

---

### S0-4：输入校验与安全头

**问题**：

- 注册接口只校验了用户名格式，未校验 displayName 长度
- 邮件发送接口 to/cc/bcc 未做邮箱格式校验
- 未配置 CSP（Content-Security-Policy）头
- WebSocket 连接仅靠 URL 参数 token 认证

**修复方案**：

```
1. 输入校验：
   - auth.js /register：displayName 限制 1-50 字符
   - mail.js /send：to/cc/bcc 用正则校验邮箱格式
   - mail.js /send：subject 限制 500 字符，body 限制 1MB

2. 安全头（helmet 配置）：
   - 启用 CSP：default-src 'self'; script-src 'self' cdn.quilljs.com; style-src 'self' 'unsafe-inline' fonts.googleapis.com cdn.quilljs.com
   - 保留当前 contentSecurityPolicy: false 仅作为临时措施

3. WebSocket 加固：
   - 连接后 10 秒内必须完成认证，否则断开
   - 添加心跳检测（30 秒 ping/pong），清理死连接
```

**涉及文件**：`src/routes/auth.js`、`src/routes/mail.js`、`src/server.js`、`src/services/ws-service.js`

---

### S0-5：管理员密码强制修改

**问题**：首次部署后管理员密码为默认值，无强制修改机制

**修复方案**：

```
1. 登录后检查 is_default_password 标志
2. 如果为 true，API 返回 403 + { requirePasswordChange: true }
3. 前端跳转到强制改密页面
4. users 表新增 is_default_password INTEGER DEFAULT 0 字段
5. setup.sh / init-db.js 创建管理员时设为 1
```

**涉及文件**：`src/dao/user-dao.js`、`src/routes/auth.js`、`scripts/init-db.js`、`public/js/app.js`

---

## S1 — Docker 容器化部署

### S1-1：Dockerfile

**方案**：多阶段构建，最终镜像只包含运行时依赖。

```dockerfile
# Stage 1: 构建 CSS
FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY tailwind.config.js postcss.config.js ./
COPY public/css/input.css ./public/css/input.css
RUN npx tailwindcss -i ./public/css/input.css -o ./public/css/style.css --minify

# Stage 2: 生产镜像
FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY package*.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY src/ ./src/
COPY scripts/ ./scripts/
COPY public/ ./public/
COPY --from=builder /app/public/css/style.css ./public/css/style.css
RUN mkdir -p data/maildir data/attachments data

EXPOSE 3000 25
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=5s \
  CMD wget -qO- http://localhost:3000/api/auth/me || exit 1

CMD ["node", "src/server.js"]
```

**注意**：
- Node 22 Alpine 基础镜像，最终约 150MB
- 非 root 用户运行（需要调整 data 目录权限）
- 只暴露 3000（HTTP）和 25（SMTP），IMAP 由 Dovecot sidecar 处理

---

### S1-2：docker-compose.yml

**方案**：三个服务编排 —— app、dovecot、nginx（可选）

```yaml
version: '3.8'

services:
  app:
    build: .
    container_name: mymail-app
    restart: unless-stopped
    ports:
      - "3000:3000"
      - "25:25"
    volumes:
      - mymail-data:/app/data
    env_file:
      - .env
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:3000/api/auth/me"]
      interval: 30s
      timeout: 5s
      retries: 3

  dovecot:
    image: dovecot/dovecot:latest
    container_name: mymail-dovecot
    restart: unless-stopped
    ports:
      - "993:993"
    volumes:
      - mymail-data:/data
      - ./config/dovecot:/etc/dovecot:ro
    depends_on:
      app:
        condition: service_healthy

  # 可选：用 Nginx 做反向代理 + SSL 终止
  nginx:
    image: nginx:alpine
    container_name: mymail-nginx
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./config/nginx:/etc/nginx/conf.d:ro
      - /etc/letsencrypt:/etc/letsencrypt:ro
    depends_on:
      - app

volumes:
  mymail-data:
```

**关键设计**：
- Dovecot 作为独立 sidecar 容器，共享数据卷
- Nginx 可选（如果用户已有反向代理可以去掉）
- 数据卷持久化，`docker compose down` 不丢数据

---

### S1-3：GitHub Actions 自动构建

**方案**：push 到 main 或打 tag 时自动构建并推送到 ghcr.io

```yaml
# .github/workflows/docker.yml
name: Build & Push Docker Image

on:
  push:
    branches: [main]
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/build-push-action@v5
        with:
          push: true
          tags: |
            ghcr.io/${{ github.repository }}:latest
            ghcr.io/${{ github.repository }}:${{ github.sha }}
```

**新增文件**：`.github/workflows/docker.yml`

---

### S1-4：compose 配置模板

为不熟悉 Docker 的用户提供开箱即用的配置：

```
config/
├── nginx/
│   └── default.conf        # Nginx 反向代理配置模板
├── dovecot/
│   ├── dovecot.conf         # Dovecot 主配置
│   └── dovecot-sql.conf     # Dovecot SQLite 认证配置
└── ssl/
    └── README.md            # SSL 证书获取说明
```

**新增目录**：`config/`、`.github/workflows/`

---

## S2 — 工程地基

### S2-1：核心链路测试

**目标**：用最小测试集覆盖邮件系统的完整生命周期。

**技术选型**：Jest + Supertest

**测试矩阵**：

| 测试场景 | 覆盖路径 | 优先级 |
|---------|---------|--------|
| 用户注册 | POST /api/auth/register → 201 | P0 |
| 用户登录 | POST /api/auth/login → 200 + token | P0 |
| 登录锁定 | 连续 5 次失败 → 423 | P1 |
| 发送邮件 | POST /api/mail/send → 200 + SENT 文件夹有记录 | P0 |
| 接收邮件 | 模拟 SMTP 连接发信 → INBOX 有记录 + WebSocket 通知 | P0 |
| 附件上传 | POST /api/mail/send (multipart) → 附件可下载 | P1 |
| 邮件操作 | 标已读/星标/删除/恢复 | P1 |
| 管理员操作 | 创建用户/禁用用户/修改配置 | P2 |
| WebSocket | 连接认证 + 新邮件推送 | P1 |

**文件结构**：

```
tests/
├── setup.js          # 全局 setup（内存 SQLite、临时目录）
├── auth.test.js      # 认证流程测试
├── mail.test.js      # 邮件收发测试
├── admin.test.js     # 管理员操作测试
└── helpers.js        # 测试工具函数
```

**package.json 新增**：

```json
{
  "devDependencies": {
    "jest": "^29.0.0",
    "supertest": "^6.0.0"
  },
  "scripts": {
    "test": "jest --forceExit --detectOpenHandles",
    "test:coverage": "jest --coverage"
  }
}
```

**CI 集成**：GitHub Actions 在每次 PR 自动跑测试（见 S1-3 的 workflow 补充 test job）。

**新增文件**：`tests/` 目录、`.github/workflows/test.yml`

---

### S2-2：结构化日志

**问题**：当前全用 `console.log/error`，出问题时无法按级别过滤、无法追踪请求链路。

**方案**：使用 `pino`（比 winston 快 5-10 倍，零依赖）

```
改造步骤：
1. 安装 pino + pino-pretty（开发环境美化输出）
2. 创建 src/logger.js 统一日志实例
3. 替换所有 console.log → logger.info
4. 替换所有 console.error → logger.error
5. 添加请求日志中间件（请求 ID、耗时、状态码）
6. 生产环境 JSON 格式输出，方便后续接入日志系统
```

**日志级别设计**：

```js
// src/logger.js
const pino = require('pino');
module.exports = pino({
  level: process.env.LOG_LEVEL || 'info',
  transport: process.env.NODE_ENV !== 'production'
    ? { target: 'pino-pretty', options: { colorize: true } }
    : undefined,
});
```

**新增文件**：`src/logger.js`
**修改文件**：几乎所有 `src/` 下的文件（替换 console 调用）

---

### S2-3：数据库迁移（knex）

**问题**：`init-db.js` 用原始 SQL 建表，未来改表结构需要手工 ALTER，无法回滚。

**方案**：引入 `knex` 的迁移系统，保留 SQLite + better-sqlite3 作为运行时驱动。

```
改造步骤：
1. 安装 knex（仅用于迁移，不用作 query builder）
2. 初始化 knexfile.js 配置
3. 将 init-db.js 中的 CREATE TABLE 转为迁移文件
4. 保留 better-sqlite3 作为运行时数据库驱动（性能更好）
5. 添加 npm scripts：
   - db:migrate — 执行迁移
   - db:migrate:rollback — 回滚
   - db:migrate:make — 创建新迁移
```

**迁移文件示例**：

```
migrations/
├── 001_initial_schema.js      # 现有 5 张表
├── 002_add_default_password.js # S0-5 的 is_default_password 字段
└── 003_smtp_greylist.js        # S0-2 的灰名单表
```

**新增文件**：`knexfile.js`、`migrations/` 目录
**修改文件**：`package.json`、`src/dao/database.js`

---

### S2-4：Graceful Shutdown & 进程健壮性

**问题**：当前无优雅退出逻辑，进程崩溃后依赖 systemd 重启，可能丢失正在处理的 SMTP 数据。

**修复方案**：

```js
// src/server.js 末尾添加
function gracefulShutdown(signal) {
  logger.info({ signal }, 'Received shutdown signal');

  // 1. 停止接受新连接
  server.close(() => {
    logger.info('HTTP server closed');
  });

  // 2. 关闭 SMTP 服务器
  if (smtpServer) {
    smtpServer.close(() => {
      logger.info('SMTP server closed');
    });
  }

  // 3. 关闭 WebSocket 连接
  wsService.shutdown();

  // 4. 关闭数据库连接
  const db = require('./dao/database');
  db.close();

  // 5. 超时强制退出
  setTimeout(() => {
    logger.error('Forced shutdown after timeout');
    process.exit(1);
  }, 10000);
}

process.on('SIGTERM', () => gracefulShutdown('SIGTERM'));
process.on('SIGINT', () => gracefulShutdown('SIGINT'));
process.on('uncaughtException', (err) => {
  logger.fatal({ err }, 'Uncaught exception');
  gracefulShutdown('uncaughtException');
});
process.on('unhandledRejection', (reason) => {
  logger.error({ reason }, 'Unhandled rejection');
});
```

**修改文件**：`src/server.js`、`src/services/ws-service.js`（添加 shutdown 方法）、`src/services/smtp-receiver.js`（返回 server 实例用于关闭）

---

### S2-5：README 优化

**当前 README 已经不错**，但可以微调：

```
1. 在最顶部添加 Mermaid 架构图（15 行代码，视觉效果好）
2. "快速开始" 章节拆分为：
   - Docker 部署（推荐）— 3 步搞定
   - 手动部署 — 现有内容
3. 添加 "安全注意事项" 章节（提醒用户改密码、配 SSL）
4. 添加项目状态 badges（测试覆盖、Docker 镜像、License）
```

**修改文件**：`README.md`

---

## S3 — 差异化功能

### S3-1：API Key 发信接口

**目标**：让外部程序（脚本、CI/CD、告警系统）通过 API 发送邮件，类似 SendGrid 的轻量替代。

**设计方案**：

```
1. 数据模型：
   - api_keys 表：id, user_id, name, key_hash, scopes(JSON), rate_limit, is_active, created_at, last_used_at
   - scopes: ["send"] / ["send", "read"] / ["send", "read", "admin"]

2. API 端点：
   - POST /api/auth/api-keys        — 创建 API Key（返回明文 key，仅此一次）
   - GET  /api/auth/api-keys         — 列出自己的 Keys
   - DELETE /api/auth/api-keys/:id   — 删除 Key
   - POST /api/v1/send               — API Key 发信（Bearer token 认证）

3. 认证方式：
   - Authorization: Bearer mk_xxxxxxxxxxxxxxxx
   - 独立于 JWT，中间件检测 Bearer 前缀 mk_ 走 API Key 验证

4. 安全约束：
   - API Key 只显示一次，存储 bcrypt hash
   - 独立速率限制（可配置，比用户登录更严格）
   - 最小权限原则：创建时默认只有 send scope
```

**新增文件**：`src/dao/api-key-dao.js`、`src/routes/api-v1.js`、`src/middleware/api-auth.js`
**修改文件**：`scripts/init-db.js`（新增表）、`src/server.js`（挂载路由）

---

### S3-2：系统级反垃圾邮件

**目标**：在用户看到邮件之前，系统自动过滤明显垃圾。

**方案**（按实现难度递增）：

```
Phase 1 — 基础过滤（1-2 天）：
  - SPF 查询验证：对发件人域名查询 DNS TXT，检查发件 IP 是否被授权
  - 发件人域名存在性检查：MX/A 记录是否存在
  - 基于评分的过滤：累计分数 > 阈值则标记为垃圾

Phase 2 — 灰名单 + DNSBL（2-3 天）：
  - 灰名单：首次 (IP, sender, recipient) 临时拒绝，5 分钟后重试放行
  - DNSBL 查询：检查发件 IP 是否在 zen.spamhaus.org 等黑名单
  - 结果记录到数据库，支持后续分析

Phase 3 — 内容过滤（可选）：
  - 基于规则的评分系统（类似 SpamAssassin 规则）
  - 关键词匹配、URL 数量、HTML/文本比例等
  - 用户可配置阈值（宽松/标准/严格）
```

**新增文件**：`src/services/spam-filter.js`、`src/services/spf-checker.js`、`src/dao/spam-log-dao.js`
**修改文件**：`src/services/smtp-receiver.js`

---

### S3-3：用户级邮件规则

**目标**：用户自定义邮件处理规则（自动分类、转发、标记等）。

**数据模型**：

```sql
CREATE TABLE mail_rules (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL,
  name        TEXT NOT NULL,
  priority    INTEGER DEFAULT 0,
  conditions  TEXT NOT NULL,  -- JSON: [{"field": "from", "op": "contains", "value": "spam"}]
  actions     TEXT NOT NULL,  -- JSON: [{"type": "move", "folder": "TRASH"}, {"type": "mark_read"}]
  is_active   INTEGER DEFAULT 1,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id)
);
```

**支持的操作**：

| 条件字段 | 操作符 | 动作类型 |
|---------|--------|---------|
| from | contains / equals / ends_with | move (移动到文件夹) |
| to | contains / equals | mark_read / mark_unread |
| subject | contains / equals / regex | star / delete |
| has_attachment | true / false | forward (转发) |
| size | gt / lt | flag (标记) |

**新增文件**：`src/dao/rule-dao.js`、`src/routes/rules.js`、`src/services/rule-engine.js`
**修改文件**：`src/services/smtp-receiver.js`（收信后执行规则）

---

## 附录：文件变更清单

### S0 安全加固

| 文件 | 操作 | 说明 |
|------|------|------|
| `DEPLOY.md` | 修改 | 清理硬编码凭据 |
| `scripts/setup.sh` | 修改 | 移除硬编码域名/密码 |
| `.env.example` | 修改 | 默认密码改为随机值 |
| `src/services/smtp-receiver.js` | 修改 | 启用 TLS、加认证 |
| `src/services/smtp-sender.js` | 修改 | 强制 TLS 验证 |
| `src/config.js` | 修改 | 新增 TLS/SMTP 认证配置项 |
| `src/services/smtp-validator.js` | **新增** | SPF/DNSBL 验证 |
| `src/routes/auth.js` | 修改 | 输入校验增强 |
| `src/routes/mail.js` | 修改 | 邮箱格式校验 |
| `src/server.js` | 修改 | CSP 头配置 |
| `src/services/ws-service.js` | 修改 | 心跳 + 认证超时 |

### S1 Docker

| 文件 | 操作 | 说明 |
|------|------|------|
| `Dockerfile` | **新增** | 多阶段构建 |
| `docker-compose.yml` | **新增** | 服务编排 |
| `.dockerignore` | **新增** | 构建排除规则 |
| `.github/workflows/docker.yml` | **新增** | 自动构建推送 |
| `config/nginx/default.conf` | **新增** | Nginx 配置模板 |
| `config/dovecot/dovecot.conf` | **新增** | Dovecot 配置模板 |
| `config/dovecot/dovecot-sql.conf` | **新增** | Dovecot 认证模板 |

### S2 工程地基

| 文件 | 操作 | 说明 |
|------|------|------|
| `package.json` | 修改 | 新增 test/lint scripts + devDependencies |
| `src/logger.js` | **新增** | pino 日志实例 |
| `src/server.js` | 修改 | graceful shutdown |
| `knexfile.js` | **新增** | 迁移配置 |
| `migrations/` | **新增** | 数据库迁移文件 |
| `tests/` | **新增** | 测试文件 |
| `.github/workflows/test.yml` | **新增** | CI 测试 |
| `README.md` | 修改 | 架构图 + Docker 部署说明 |

### S3 差异化功能

| 文件 | 操作 | 说明 |
|------|------|------|
| `src/dao/api-key-dao.js` | **新增** | API Key 数据层 |
| `src/routes/api-v1.js` | **新增** | API Key 发信端点 |
| `src/middleware/api-auth.js` | **新增** | API Key 认证中间件 |
| `src/services/spam-filter.js` | **新增** | 垃圾邮件过滤引擎 |
| `src/services/spf-checker.js` | **新增** | SPF 查询验证 |
| `src/dao/spam-log-dao.js` | **新增** | 垃圾邮件日志 |
| `src/dao/rule-dao.js` | **新增** | 邮件规则数据层 |
| `src/routes/rules.js` | **新增** | 规则 CRUD API |
| `src/services/rule-engine.js` | **新增** | 规则执行引擎 |

---

## 执行建议

### 预估工时

| 阶段 | 预估 | 说明 |
|------|------|------|
| S0 安全加固 | 2-3 天 | 最紧急，建议立即开始 |
| S1 Docker | 1-2 天 | 独立于 S0，可以并行 |
| S2 工程地基 | 3-5 天 | 测试和迁移需要时间打磨 |
| S3 差异化功能 | 5-8 天 | 每个功能独立 PR，按需推进 |

### PR 策略

```
PR #1: S0 安全加固（一个大 PR，原子性提交）
PR #2: S1 Docker 容器化
PR #3: S2-1 核心链路测试
PR #4: S2-2 结构化日志
PR #5: S2-3 数据库迁移
PR #6: S2-4 Graceful shutdown
PR #7: S3-1 API Key 发信
PR #8: S3-2 系统级反垃圾
PR #9: S3-3 用户级邮件规则
```

每个 PR 独立可合并，不互相阻塞。
