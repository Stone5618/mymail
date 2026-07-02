# MyMail 后端 Go 重构完整方案

> **方案版本**：v1.0
> **制定日期**：2026-07-03
> **目标**：用 Go 重写 mymail-platform 后端，同时修复评估报告中识别的全部 P0/P1 Bug
> **原则**：保持前端 Vue 项目零改动，API 契约完全兼容；借此机会修复所有已知 Bug，不留技术债

---

## 目录

- [一、总体策略与原则](#一总体策略与原则)
- [二、技术选型](#二技术选型)
- [三、Go 项目结构](#三go-项目结构)
- [四、模块映射表](#四模块映射表node-js--go)
- [五、数据库设计与迁移](#五数据库设计与迁移)
- [六、API 契约保持与改进](#六api-契约保持与改进)
- [七、P0 阻断性 Bug 修复方案](#七p0-阻断性-bug-修复方案)
- [八、P1 高危 Bug 修复方案](#八p1-高危-bug-修复方案)
- [九、P2 工程化改进](#九p2-工程化改进)
- [十、分阶段实施计划](#十分阶段实施计划)
- [十一、测试策略](#十一测试策略)
- [十二、部署方案](#十二部署方案)
- [十三、风险评估与回滚](#十三风险评估与回滚)
- [十四、关键参数清单](#十四关键参数清单)

---

## 一、总体策略与原则

### 1.1 核心目标

1. **零前端改动**：Vue 前端（mymail-vue）零修改，所有 API 路径、请求/响应格式、错误结构完全兼容
2. **借此修复全部 Bug**：所有 P0/P1 Bug 在新架构中一并修复，不带入新代码
3. **架构升级**：解决 Node.js 单进程同步阻塞、无事务、无队列等硬伤
4. **保持轻量**：MyMail 定位自托管轻量邮件平台，避免企业级过度设计

### 1.2 五项原则

| 原则 | 说明 |
|---|---|
| **API 兼容** | 所有现有端点路径、HTTP 方法、请求体、响应 JSON 结构保持不变 |
| **数据兼容** | SQLite 数据库结构保持向前兼容，支持现有数据迁移 |
| **配置兼容** | 环境变量名与 `.env.example` 保持一致，便于平滑切换 |
| **Docker 兼容** | docker-compose.yml 服务编排保持不变，仅替换 app 镜像 |
| **测试先行** | 每个 Bug 修复必须有对应测试用例，避免回归 |

### 1.3 不做的事

- ❌ 不重写前端（Vue 项目零改动）
- ❌ 不更换数据库（继续用 SQLite，但驱动改为异步 CGO-free 的 `modernc.org/sqlite`）
- ❌ 不引入微服务架构（保持单二进制部署）
- ❌ 不引入 Kafka/RabbitMQ 等重型中间件（用 Go channel + 持久化队列实现轻量异步）
- ❌ 不引入 gRPC（继续用 REST + WebSocket）

---

## 二、技术选型

### 2.1 选型对比

| 维度 | 选型 | 备选 | 选择理由 |
|---|---|---|---|
| Web 框架 | **Gin** | Echo / Fiber / 标准库 | 生态最成熟、中间件丰富、性能优秀、文档齐全 |
| SMTP 接收 | **emersion/go-smtp** | bradfitz/go-smtp | 维护活跃、API 简洁、支持 TLS |
| SMTP 发送 | **go-mail/mail** | gomail-1.0 | 现代化 API、支持 TLS、附件处理友好 |
| IMAP 解析 | **emersion/go-imap** | - | 与 go-smtp 同作者，生态一致 |
| 邮件解析 | **emersion/go-message** | net/mail | 支持 MIME 多部分、附件提取 |
| 数据库驱动 | **modernc.org/sqlite** | mattn/go-sqlite3 | 纯 Go 实现，无需 CGO，跨平台编译 |
| SQL 工具 | **sqlx + 自写 SQL** | GORM / ent | 邮件查询复杂，手写 SQL 更可控；sqlx 提供 struct scan |
| 数据库迁移 | **golang-migrate/migrate** | goose / atlas | 支持 SQLite，CLI + 库双模式，业界标准 |
| 配置 | **viper** | envconfig | 支持 .env + 环境变量 + 配置文件多层回退 |
| 日志 | **log/slog**（标准库） | zap / zerolog | Go 1.21+ 标准库，结构化日志，零依赖 |
| JWT | **golang-jwt/jwt/v5** | - | 业界标准，维护活跃 |
| 密码哈希 | **golang.org/x/crypto/bcrypt** | argon2id | 与现有数据兼容（bcrypt `$2b$` 格式） |
| WebSocket | **coder/websocket**（原 nhooyr/websocket） | gorilla/websocket | API 现代化，标准库兼容，无需 flatter |
| 限流 | **ulule/limiter** | 自己实现 | 支持内存/Redis 后端，HTTP + SMTP 通用 |
| 验证 | **go-playground/validator** | - | Gin 集成，结构体 tag 校验 |
| 测试 | **标准 testing + testify** | - | 标准组合，社区主流 |
| HTTP 测试 | **net/http/httptest** | - | 标准库，与 Gin 兼容 |
| 容器化 | **多阶段 Dockerfile** | - | 最终镜像 scratch 或 alpine |

### 2.2 Go 版本

- **最低版本**：Go 1.22（需 `log/slog`、泛型、`for range int` 等特性）
- **推荐版本**：Go 1.23+

### 2.3 关键设计决策

#### 决策 1：纯 Go SQLite 驱动（无 CGO）

**选择**：`modernc.org/sqlite`
**理由**：
- 无需 CGO，跨平台编译简单（`GOOS=linux GOARCH=amd64 go build`）
- Docker 镜像可用 `scratch` 或 `alpine`，体积 < 50MB
- 性能略低于 `mattn/go-sqlite3`（CGO），但对小规模邮件系统足够
- 避免 CGO 带来的构建复杂度

#### 决策 2：sqlx + 手写 SQL，不用 ORM

**理由**：
- 邮件查询复杂（多条件搜索、分页、文件夹过滤），ORM 生成的 SQL 难以优化
- 手写 SQL 更容易排查性能问题
- sqlx 提供 `StructScan` 自动映射，减少样板代码
- 迁移成本最低（直接复用现有 SQL）

#### 决策 3：单二进制 + 内嵌静态资源

**选择**：用 `embed` 包将前端 dist/ 内嵌到二进制
**理由**：
- 真正单二进制部署，无外部依赖
- Docker 镜像可缩至 < 50MB
- 简化部署：一个文件 + 一个 .env 即可运行

---

## 三、Go 项目结构

```
mymail-go/
├── cmd/
│   └── mymail/
│       └── main.go                    # 入口
├── internal/
│   ├── config/                        # 配置加载
│   │   ├── config.go                  # 配置结构 + 加载
│   │   └── config_test.go
│   ├── logger/                        # 日志封装
│   │   └── logger.go
│   ├── server/                        # HTTP + SMTP + WS 服务器编排
│   │   ├── server.go                  # 启动 + graceful shutdown
│   │   └── server_test.go
│   ├── httpapi/                       # HTTP API 层
│   │   ├── router.go                  # 路由注册
│   │   ├── middleware/                # 中间件
│   │   │   ├── auth.go                # JWT 认证
│   │   │   ├── api_auth.go            # API Key 认证
│   │   │   ├── cors.go                # CORS 白名单
│   │   │   ├── ratelimit.go           # 速率限制
│   │   │   ├── recover.go             # panic 恢复
│   │   │   ├── request_logger.go      # 请求日志
│   │   │   └── security.go            # 安全头
│   │   ├── handler/                   # 请求处理器（对应 routes）
│   │   │   ├── auth.go
│   │   │   ├── mail.go
│   │   │   ├── admin.go
│   │   │   ├── apiv1.go
│   │   │   ├── rules.go
│   │   │   ├── health.go              # 新增 /health 端点
│   │   │   └── spa.go                 # SPA 静态资源 + fallback
│   │   └── dto/                       # 请求/响应结构体
│   │       ├── auth.go
│   │       ├── mail.go
│   │       └── admin.go
│   ├── smtp/                          # SMTP 接收器
│   │   ├── receiver.go                # SMTPServer 包装
│   │   ├── handler.go                 # onConnect/onMailFrom/onRcptTo/onData
│   │   ├── validator.go               # SPF/DNSBL 检查
│   │   ├── greylist.go                # 灰名单
│   │   └── ratelimit.go               # 连接级限流
│   ├── mailsender/                    # SMTP 发送器
│   │   ├── sender.go                  # 外部发送（go-mail）
│   │   ├── local.go                   # 本地投递（同域）
│   │   └── queue.go                   # 持久化发送队列
│   ├── ws/                            # WebSocket 服务
│   │   ├── hub.go                     # 用户维度连接管理
│   │   ├── client.go                  # 单连接处理
│   │   └── auth.go                    # WS 认证（子协议方式）
│   ├── spam/                          # 反垃圾模块（重写）
│   │   ├── filter.go                  # 评分主流程
│   │   ├── spf.go                     # SPF 完整实现（含 MX/IPv6）
│   │   ├── dnsbl.go                   # DNSBL 查询
│   │   └── scorer.go                  # 评分规则
│   ├── rules/                         # 用户级规则引擎（重写）
│   │   ├── engine.go                  # 规则执行
│   │   ├── matcher.go                 # 条件匹配（含 ReDoS 防护）
│   │   └── action.go                  # 动作执行
│   ├── storage/                       # 存储层
│   │   ├── db/                        # 数据库
│   │   │   ├── db.go                  # 连接池 + PRAGMA
│   │   │   ├── migrations/            # 迁移文件（嵌入）
│   │   │   │   ├── 001_initial_schema.up.sql
│   │   │   │   ├── 001_initial_schema.down.sql
│   │   │   │   ├── 002_add_apikeys_rules.up.sql
│   │   │   │   ├── 002_add_apikeys_rules.down.sql
│   │   │   │   ├── 003_add_greylist_spamlog.up.sql
│   │   │   │   ├── 003_add_greylist_spamlog.down.sql
│   │   │   │   ├── 004_add_indexes.up.sql              # 新增缺失索引
│   │   │   │   └── 004_add_indexes.down.sql
│   │   │   └── migrate.go             # 迁移执行器
│   │   ├── dao/                       # 数据访问对象
│   │   │   ├── user.go
│   │   │   ├── message.go
│   │   │   ├── attachment.go
│   │   │   ├── send_log.go
│   │   │   ├── settings.go
│   │   │   ├── api_key.go
│   │   │   ├── rule.go
│   │   │   ├── greylist.go
│   │   │   └── spam_log.go
│   │   ├── maildir/                   # Maildir 文件操作
│   │   │   └── maildir.go
│   │   └── attachment/                # 附件文件操作
│   │       └── attachment.go
│   ├── service/                       # 业务服务层
│   │   ├── auth_service.go            # 登录注册/锁定
│   │   ├── mail_service.go            # 邮件 CRUD/发送/接收编排
│   │   ├── admin_service.go           # 管理员操作
│   │   ├── rule_service.go            # 规则 CRUD
│   │   └── apikey_service.go          # API Key 管理
│   ├── crypto/                        # 加密工具
│   │   ├── jwt.go                     # JWT 生成/校验
│   │   ├── bcrypt.go                  # 密码哈希
│   │   └── apikey.go                  # API Key 生成/哈希
│   ├── sanitize/                      # HTML 净化（新增）
│   │   └── html.go                    # bluemonday 封装
│   └── util/                          # 工具函数
│       ├── validator.go               # 邮箱/用户名校验
│       ├── token.go                   # 随机 token 生成
│       └── archive.go                 # zip 打包
├── web/                               # 前端资源（嵌入）
│   └── dist/                          # Vue 构建产物（git submodule 或软链）
├── config/                            # 部署配置
│   ├── dovecot/
│   │   ├── dovecot.conf
│   │   └── dovecot-sql.conf           # 修复列名 bug
│   └── nginx/
│       └── default.conf
├── scripts/
│   ├── gen-dkim.sh
│   └── backup.sh                      # 修复含密钥 bug
├── deployments/
│   └── docker/
│       ├── Dockerfile                 # 多阶段构建
│       └── docker-compose.yml         # 修复路径不一致
├── .github/
│   └── workflows/
│       ├── test.yml                   # 增强：加 lint + 安全扫描
│       ├── docker.yml
│       └── release.yml                # 新增：发布带二进制
├── docs/
│   ├── API.md                        # API 契约文档
│   └── DEPLOY.md
├── .env.example                       # 修复 bug
├── .golangci.yml                      # 新增：lint 配置
├── go.mod
├── go.sum
├── Makefile                           # 构建/测试/部署命令
└── README.md
```

### 3.1 设计要点

1. **`internal/` 强制封装**：所有业务代码用 `internal/`，防止外部包误引用
2. **`cmd/mymail/main.go` 极简**：仅加载配置 → 启动 server，无业务逻辑
3. **handler/service/dao 三层分离**：解决原 Node.js 项目"业务逻辑写路由里"的问题
4. **`web/dist/` 内嵌**：用 `//go:embed` 将前端打包进二进制
5. **`storage/migrations/` 嵌入**：迁移 SQL 文件用 `//go:embed` 内嵌，无需外部文件

---

## 四、模块映射表（Node.js → Go）

| 原 Node.js 模块 | Go 对应模块 | 说明 |
|---|---|---|
| src/server.js | cmd/mymail/main.go + internal/server/server.go | 入口 + 服务器编排 |
| src/app.js | internal/httpapi/router.go + middleware/ | Gin 路由 + 中间件 |
| src/config.js | internal/config/config.go | viper 加载 |
| src/logger.js | internal/logger/logger.go | slog 封装 |
| src/routes/auth.js | internal/httpapi/handler/auth.go + service/auth_service.go | 拆分 handler/service |
| src/routes/mail.js | internal/httpapi/handler/mail.go + service/mail_service.go | 拆分 handler/service |
| src/routes/admin.js | internal/httpapi/handler/admin.go + service/admin_service.go | 拆分 handler/service |
| src/routes/api-v1.js | internal/httpapi/handler/apiv1.go + service/apikey_service.go | API Key 发信 |
| src/routes/rules.js | internal/httpapi/handler/rules.go + service/rule_service.go | 规则 CRUD |
| src/middleware/auth.js | internal/httpapi/middleware/auth.go + internal/crypto/jwt.go | JWT |
| src/middleware/api-auth.js | internal/httpapi/middleware/api_auth.go + internal/crypto/apikey.go | API Key |
| src/services/smtp-receiver.js | internal/smtp/receiver.go + handler.go | SMTP 接收 |
| src/services/smtp-sender.js | internal/mailsender/sender.go + local.go | SMTP 发送 |
| src/services/smtp-validator.js | internal/smtp/validator.go + internal/spam/{spf,dnsbl}.go | SPF/DNSBL |
| src/services/spam-filter.js | internal/spam/filter.go（重写） | 删除原死代码，统一到 spam 包 |
| src/services/rule-engine.js | internal/rules/engine.go（重写） | 含 ReDoS 防护 |
| src/services/ws-service.js | internal/ws/hub.go + client.go | WebSocket |
| src/dao/database.js | internal/storage/db/db.go | 连接池 + PRAGMA |
| src/dao/*.js | internal/storage/dao/*.go | 数据访问层 |
| migrations/*.js | internal/storage/db/migrations/*.sql | golang-migrate 格式 |
| scripts/init-db.js | （废弃，统一用 migrations） | 二选一 |
| scripts/backup.sh | scripts/backup.sh（修复） | 排除 .env |
| scripts/setup.sh | scripts/setup.sh（修复） | 移除 root |
| scripts/gen-dkim.sh | scripts/gen-dkim.sh（保留） | - |
| Dockerfile | deployments/docker/Dockerfile（修复） | 多阶段 + 健康检查 |
| docker-compose.yml | deployments/docker/docker-compose.yml（修复） | 路径一致化 |
| config/dovecot/* | config/dovecot/*（修复列名） | - |
| config/nginx/* | config/nginx/*（保留） | - |
| knexfile.js | （废弃，迁移配置内嵌） | - |
| jest.config.js | （废弃，用 go test） | - |
| tests/*.test.js | internal/**/*_test.go + tests/e2e/* | Go 标准 testing |

---

## 五、数据库设计与迁移

### 5.1 数据库策略

- **保持 SQLite**：与现有数据兼容，迁移零成本
- **驱动更换**：`better-sqlite3`（同步）→ `modernc.org/sqlite`（异步，纯 Go）
- **连接池**：用 `database/sql` 标准连接池，最大并发数可配置
- **迁移工具**：`golang-migrate`，SQL 文件嵌入二进制

### 5.2 完整 Schema（修复所有缺陷）

#### 5.2.1 用户表（修复 is_default_password 默认值）

```sql
-- migrations/001_initial_schema.up.sql
CREATE TABLE users (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    username            TEXT NOT NULL UNIQUE,
    email               TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,                          -- 修复：列名统一
    display_name        TEXT,
    role                TEXT NOT NULL DEFAULT 'user',
    storage_limit       INTEGER NOT NULL DEFAULT 104857600,
    storage_used        INTEGER NOT NULL DEFAULT 0,
    is_active           INTEGER NOT NULL DEFAULT 1,
    login_fails         INTEGER NOT NULL DEFAULT 0,
    locked_until        DATETIME,
    is_default_password INTEGER NOT NULL DEFAULT 0,             -- 修复：默认 0
    signature           TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
```

#### 5.2.2 邮件表（修复外键级联）

```sql
CREATE TABLE messages (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,  -- 修复：加级联
    folder        TEXT NOT NULL DEFAULT 'INBOX',
    message_id    TEXT,
    uid           INTEGER,
    from_addr     TEXT NOT NULL,
    from_name     TEXT,
    to_addr       TEXT NOT NULL,
    cc_addr       TEXT,
    bcc_addr      TEXT,
    reply_to      TEXT,
    subject       TEXT,
    body_text     TEXT,
    body_html     TEXT,                    -- 净化后的 HTML（修复 XSS）
    body_html_raw TEXT,                    -- 原始 HTML（备份，仅管理员可看）
    is_read       INTEGER NOT NULL DEFAULT 0,
    is_starred    INTEGER NOT NULL DEFAULT 0,
    is_deleted    INTEGER NOT NULL DEFAULT 0,
    has_attach    INTEGER NOT NULL DEFAULT 0,
    attach_count  INTEGER NOT NULL DEFAULT 0,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    headers_raw   TEXT,
    in_reply_to   TEXT,
    flags         TEXT NOT NULL DEFAULT '[]',
    spam_score    INTEGER NOT NULL DEFAULT 0,    -- 新增：垃圾评分
    spam_reasons  TEXT,                          -- 新增：评分原因 JSON
    received_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 修复：补全索引
CREATE INDEX idx_msg_user_folder ON messages(user_id, folder);
CREATE INDEX idx_msg_received ON messages(received_at);
CREATE INDEX idx_msg_user_read ON messages(user_id, is_read);              -- 新增
CREATE INDEX idx_msg_user_received ON messages(user_id, received_at DESC); -- 新增
CREATE INDEX idx_msg_message_id ON messages(message_id);                   -- 新增：去重用
```

#### 5.2.3 附件表（保持不变）

```sql
CREATE TABLE attachments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id   INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    mime_type    TEXT,
    size_bytes   INTEGER,
    storage_path TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

#### 5.2.4 API Key 表（修复索引缺失）

```sql
CREATE TABLE api_keys (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    key_hash      TEXT NOT NULL,
    key_prefix    TEXT NOT NULL,                  -- 新增：前 8 字符，用于索引查找
    scopes        TEXT NOT NULL DEFAULT '["send"]',
    rate_limit    INTEGER NOT NULL DEFAULT 60,
    is_active     INTEGER NOT NULL DEFAULT 1,
    last_used_at  DATETIME,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 修复：加索引（解决 O(n) bcrypt 性能瓶颈）
CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix) WHERE is_active = 1;
CREATE INDEX idx_api_keys_user ON api_keys(user_id);
```

#### 5.2.5 邮件规则表（修复索引缺失）

```sql
CREATE TABLE mail_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    priority    INTEGER NOT NULL DEFAULT 0,
    conditions  TEXT NOT NULL,
    actions     TEXT NOT NULL,
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 修复：加复合索引
CREATE INDEX idx_rules_user_active ON mail_rules(user_id, is_active);
```

#### 5.2.6 灰名单表（修复 init 与 migration 不一致）

```sql
-- migrations/003_add_greylist_spamlog.up.sql
CREATE TABLE greylist (
    key         TEXT PRIMARY KEY,        -- 格式: ip:sender:recipient
    first_seen  INTEGER NOT NULL,        -- Unix 毫秒时间戳
    allowed     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_greylist_first_seen ON greylist(first_seen);
```

#### 5.2.7 垃圾邮件日志表（修复索引缺失）

```sql
CREATE TABLE spam_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    sender      TEXT,
    recipient   TEXT,
    ip          TEXT,
    score       INTEGER NOT NULL,
    reasons     TEXT,
    action      TEXT NOT NULL,           -- rejected / flagged / passed
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 修复：加索引
CREATE INDEX idx_spam_log_created ON spam_log(created_at);
CREATE INDEX idx_spam_log_sender ON spam_log(sender);
```

#### 5.2.8 发送日志表（修复外键级联）

```sql
CREATE TABLE send_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,  -- 修复：加级联
    to_addr    TEXT NOT NULL,
    subject    TEXT,
    status     TEXT NOT NULL DEFAULT 'pending',
    error_msg  TEXT,
    sent_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_send_log_user ON send_log(user_id);
CREATE INDEX idx_send_log_sent ON send_log(sent_at);
```

#### 5.2.9 系统配置表（保持不变）

```sql
CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       TEXT,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

#### 5.2.10 发送队列表（新增，解决无队列问题）

```sql
-- migrations/003_add_greylist_spamlog.up.sql
CREATE TABLE mail_queue (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_addr   TEXT NOT NULL,
    to_addrs    TEXT NOT NULL,           -- JSON 数组
    cc_addrs    TEXT,
    bcc_addrs   TEXT,
    subject     TEXT,
    body_html   TEXT,
    body_text   TEXT,
    reply_to    TEXT,
    attachments TEXT,                    -- JSON 数组，附件路径
    status      TEXT NOT NULL DEFAULT 'pending',  -- pending/sending/sent/failed
    attempts    INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    next_retry_at DATETIME,
    error_msg   TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at     DATETIME
);

CREATE INDEX idx_queue_status ON mail_queue(status, next_retry_at);
CREATE INDEX idx_queue_user ON mail_queue(user_id);
```

### 5.3 数据迁移策略

**场景**：用户已有 Node.js 版本的 SQLite 数据库，需迁移到 Go 版本

**策略**：
1. Go 版本启动时检测数据库版本（用 `settings` 表的 `schema_version` 键）
2. 若版本低于当前，自动执行增量迁移
3. 迁移过程：
   - 备份原数据库到 `mymail.db.backup.{timestamp}`
   - 执行迁移 SQL
   - 更新 `schema_version`
4. 关键迁移：
   - 添加 `messages.body_html_raw` 列，将原 `body_html` 复制过去
   - 用 `bluemonday` 净化 `body_html`（一次性脚本）
   - 添加 `api_keys.key_prefix`，从现有 key_hash 无法反推，需用户重新生成 Key
   - 添加 `messages.spam_score/spam_reasons`，默认 0/null

**回滚**：
- 迁移失败自动恢复备份
- 提供独立脚本 `scripts/rollback-migrate.go`

---

## 六、API 契约保持与改进

### 6.1 端点清单（完全保持兼容）

所有现有端点路径、HTTP 方法、请求体格式、响应 JSON 结构保持不变。

#### 6.1.1 认证（/api/auth/*）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | /api/auth/register | 注册 |
| POST | /api/auth/login | 登录 |
| GET | /api/auth/me | 获取当前用户 |
| PUT | /api/auth/profile | 更新资料 |
| PUT | /api/auth/password | 修改密码 |
| POST | /api/auth/change-default-password | 强制改密 |
| POST | /api/auth/api-keys | 创建 API Key |
| GET | /api/auth/api-keys | 列出 API Key |
| DELETE | /api/auth/api-keys/:id | 删除 API Key |

#### 6.1.2 邮件（/api/mail/*）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/mail/list | 邮件列表（支持 folder/search/page/limit/starredOnly） |
| GET | /api/mail/unread-count | 未读数 |
| GET | /api/mail/:id | 邮件详情 |
| POST | /api/mail/send | 发送邮件（multipart，含附件） |
| POST | /api/mail/save-draft | 保存草稿 |
| PUT | /api/mail/:id/read | 标记已读 |
| PUT | /api/mail/:id/unread | 标记未读 |
| PUT | /api/mail/:id/star | 切换星标 |
| DELETE | /api/mail/:id | 删除（移到垃圾箱） |
| PUT | /api/mail/:id/restore | 恢复 |
| POST | /api/mail/empty-trash | 清空垃圾箱 |
| GET | /api/mail/:id/attachments/:aid/download | 下载单个附件 |
| GET | /api/mail/:id/attachments/download-all | 批量下载 zip |
| POST | /api/mail/upload | 独立附件上传 |
| DELETE | /api/mail/upload/:id | 删除已上传附件 |
| GET | /api/mail/upload/:id/preview | 附件预览 |
| POST | /api/mail/batch/delete | 批量删除 |
| POST | /api/mail/batch/mark-read | 批量标已读 |
| POST | /api/mail/batch/move | 批量移动 |

#### 6.1.3 管理员（/api/admin/*）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/admin/stats | 统计 |
| GET | /api/admin/users | 用户列表 |
| GET | /api/admin/users/:id | 用户详情 |
| POST | /api/admin/users | 创建用户 |
| PUT | /api/admin/users/:id | 更新用户（isActive/password/storageLimit） |
| DELETE | /api/admin/users/:id | 删除用户 |
| GET | /api/admin/settings | 系统配置 |
| PUT | /api/admin/settings | 更新配置 |
| GET | /api/admin/dns-status | DNS 状态 |
| POST | /api/admin/smtp-port | SMTP 端口管理 |

#### 6.1.4 API v1（/api/v1/*）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | /api/v1/send | API Key 发信 |

#### 6.1.5 规则（/api/rules/*）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/rules | 列出规则 |
| POST | /api/rules | 创建规则 |
| PUT | /api/rules/:id | 更新规则 |
| DELETE | /api/rules/:id | 删除规则 |

#### 6.1.6 新增端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /health | 健康检查（无认证，返回 DB/SMTP/WS 状态） |
| GET | /metrics | Prometheus 指标（可选） |

### 6.2 响应格式

**成功**：直接返回业务数据 JSON

**错误**（保持原格式）：

```json
{ "error": "错误描述信息" }
```

**HTTP 状态码**（修复不一致）：
- 200：成功（GET/PUT/DELETE）
- 201：创建成功（POST 注册/发送邮件/创建规则）
- 400：请求参数错误
- 401：未认证
- 403：无权限
- 404：资源不存在
- 413：文件过大
- 423：账户锁定
- 429：请求过频
- 500：服务器错误

### 6.3 WebSocket 协议

**保持兼容**：前端 `ws://host/ws?token=xxx` 仍可用

**改进**：同时支持子协议认证（推荐前端后续切换）：
- URL query: `?token=xxx`（保持兼容）
- 子协议: `Sec-WebSocket-Protocol: bearer.xxx`（推荐）

**消息格式**（保持不变）：

```json
{ "type": "new_mail", "data": { "id": 1, "from": "...", "subject": "...", "time": "..." } }
{ "type": "queue_complete", "data": { ... } }
```

---

## 七、P0 阻断性 Bug 修复方案

### P0-1：Dovecot SQL 配置列名错误

**原 Bug**：`config/dovecot/dovecot-sql.conf` 使用 `password` 列，实际是 `password_hash`

**修复**：

```sql
-- config/dovecot/dovecot-sql.conf
password_query = SELECT \
    email AS user, \
    password_hash AS password, \        -- 修复：列名改为 password_hash
    '/data/maildir/%d/%n' AS userdb_mail \
    FROM users \
    WHERE email = '%u'

user_query = SELECT \
    '/data/maildir/%d/%n' AS home, \
    'maildir:/data/maildir/%d/%n' AS mail, \
    1000 AS uid, \
    1000 AS gid \
    FROM users \
    WHERE email = '%u'

default_pass_scheme = BLF-CRYPT
```

**额外验证**：
- 在 `scripts/setup.sh` 中添加测试：用 `doveadm auth test user@domain password` 验证认证
- 在文档中说明：bcrypt `$2b$` 与 Dovecot BLF-CRYPT 兼容性（Dovecot 2.3+ 支持）

**验证测试**：

```go
// tests/e2e/dovecot_auth_test.go
func TestDovecotSQLConfig(t *testing.T) {
    // 读取 dovecot-sql.conf
    // 解析 password_query
    // 验证 SQL 在测试数据库上可执行
    // 验证返回的 password 字段对应 users.password_hash 列
}
```

---

### P0-2：spam-filter.js 完全不可用

**原 Bug**：导入不存在的函数、函数签名错误、返回值比较错误

**修复**：**直接删除原 spam-filter.js**，在新 Go 项目中重新实现统一的 `internal/spam/` 包

**新实现**：

```go
// internal/spam/filter.go
package spam

type Filter struct {
    spfChecker   *SPFChecker
    dnsblChecker *DNSBLChecker
    scorer       *Scorer
    threshold    int
    suspicious   int
}

type Result struct {
    Score   int      `json:"score"`
    Reasons []string `json:"reasons"`
    Action  string   `json:"action"` // rejected / flagged / passed
}

func (f *Filter) Check(senderIP, senderEmail, heloDomain string) (*Result, error) {
    var score int
    var reasons []string

    // SPF 检查（完整实现，含 MX/IPv6）
    spfResult, err := f.spfChecker.Check(senderIP, senderEmail, heloDomain)
    if err != nil {
        // SPF 查询失败不阻断，但加分
        score += 2
        reasons = append(reasons, "spf_query_failed")
    } else {
        switch spfResult.Result {
        case "fail":
            score += 5
            reasons = append(reasons, "spf_fail")
        case "softfail":
            score += 2
            reasons = append(reasons, "spf_softfail")
        case "neutral", "none":
            score += 1
            reasons = append(reasons, "spf_neutral")
        }
    }

    // DNSBL 检查
    dnsblResult, err := f.dnsblChecker.Check(senderIP)
    if err == nil && dnsblResult.Listed {
        score += 5
        reasons = append(reasons, fmt.Sprintf("dnsbl_listed:%s", dnsblResult.Zones))
    }

    // 决定动作
    action := "passed"
    if score >= f.threshold {
        action = "rejected"
    } else if score >= f.suspicious {
        action = "flagged"
    }

    return &Result{Score: score, Reasons: reasons, Action: action}, nil
}
```

**SPF 完整实现**（修复原 Node.js 版本不完整）：

```go
// internal/spam/spf.go
package spam

import (
    "net"
    "strings"
    "golang.org/x/net/dns/dnsmessage"
)

type SPFChecker struct {
    maxDepth int // 防止 include 递归过深
}

type SPFResult struct {
    Result string // pass / fail / softfail / neutral / none / temperror / permerror
    Detail string
}

// Check 完整实现 SPF，支持 ip4/ip6/mx/a/include/exists/all
func (c *SPFChecker) Check(senderIP, senderEmail, heloDomain string) (*SPFResult, error) {
    ip := net.ParseIP(senderIP)
    if ip == nil {
        return &SPFResult{Result: "permerror", Detail: "invalid sender IP"}, nil
    }

    domain := strings.Split(senderEmail, "@")[1]
    return c.checkDomain(ip, domain, heloDomain, 0)
}

func (c *SPFChecker) checkDomain(ip net.IP, domain, heloDomain string, depth int) (*SPFResult, error) {
    if depth > c.maxDepth {
        return &SPFResult{Result: "permerror", Detail: "too many include"}, nil
    }

    // 查询 DNS TXT 记录
    txt, err := c.querySPFRecord(domain)
    if err != nil || txt == "" {
        return &SPFResult{Result: "none"}, nil
    }

    // 解析并匹配
    return c.matchMechanisms(ip, txt, domain, heloDomain, depth)
}

// matchMechanisms 支持 ip4/ip6/a/mx/include/exists/all
// IPv4 + IPv6 双栈支持
// include 递归深度限制
```

**测试**：

```go
// internal/spam/filter_test.go
func TestFilter_Check(t *testing.T) {
    tests := []struct{
        name        string
        senderIP    string
        senderEmail string
        heloDomain  string
        wantAction  string
        wantScore   int
    }{
        {"正常邮件", "8.8.8.8", "test@gmail.com", "gmail.com", "passed", 0},
        {"SPF fail", "1.2.3.4", "test@gmail.com", "gmail.com", "flagged", 5},
        {"DNSBL 命中", "127.0.0.2", "test@spam.com", "spam.com", "rejected", 10},
    }
    // ...
}
```

---

### P0-3：mail.js IDOR 越权漏洞

**原 Bug**：`/:id/read`、`/:id/unread`、`/:id/star` 未校验邮件归属

**修复**：在所有邮件操作前加归属校验

```go
// internal/httpapi/handler/mail.go

func (h *MailHandler) MarkRead(c *gin.Context) {
    userID := c.GetInt("user_id")
    mailID, _ := strconv.Atoi(c.Param("id"))

    // 修复：校验邮件归属
    msg, err := h.messageDAO.GetByID(c.Request.Context(), mailID)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "邮件不存在"})
        return
    }
    if msg.UserID != userID {
        c.JSON(http.StatusForbidden, gin.H{"error": "无权操作此邮件"})
        return
    }

    if err := h.messageDAO.MarkRead(c.Request.Context(), mailID); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
```

**通用化**：抽出一个 `requireOwnedMail(c, mailID)` 中间件

```go
// internal/httpapi/middleware/ownership.go
func (m *Middleware) RequireOwnedMail(messageDAO dao.MessageDAO) gin.HandlerFunc {
    return func(c *gin.Context) {
        userID := c.GetInt("user_id")
        mailID, _ := strconv.Atoi(c.Param("id"))

        msg, err := messageDAO.GetByID(c.Request.Context(), mailID)
        if err != nil {
            c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "邮件不存在"})
            return
        }
        if msg.UserID != userID {
            c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权操作此邮件"})
            return
        }
        c.Set("mail", msg)
        c.Next()
    }
}
```

**应用范围**：所有 `:id` 路径的端点（read/unread/star/delete/restore/attachments）

**测试**：

```go
func TestMailHandler_MarkRead_Forbidden(t *testing.T) {
    // 用户 A 尝试标记用户 B 的邮件
    // 期望 403
}

func TestMailHandler_MarkRead_Own(t *testing.T) {
    // 用户 A 标记自己的邮件
    // 期望 200
}
```

---

### P0-4：前端 MailDetailView/ComposeView XSS

**原 Bug**：前端 v-html 渲染未净化的 body_html

**修复策略**：**前端 + 后端双层防御**

#### 前端修复（虽然原则是零改动，但这是安全 bug 必须改）

```vue
<!-- MailDetailView.vue -->
<script setup>
import DOMPurify from 'dompurify'

const sanitizedHtml = computed(() => {
  return DOMPurify.sanitize(mail.body_html || escapeHtml(mail.body_text), {
    ALLOWED_TAGS: ['p', 'br', 'div', 'span', 'a', 'img', 'table', 'tr', 'td', 'th',
                   'ul', 'ol', 'li', 'b', 'i', 'u', 'strong', 'em', 'blockquote', 'pre', 'code'],
    ALLOWED_ATTR: ['href', 'src', 'alt', 'title', 'style', 'class', 'target'],
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick', 'onmouseover'],
  })
})
</script>

<template>
  <div v-html="sanitizedHtml"></div>
</template>
```

#### 后端防御（Go 版本新增）

```go
// internal/sanitize/html.go
package sanitize

import (
    "github.com/microcosm-cc/bluemonday"
)

var policy = bluemonday.UGCPolicy().
    AllowElements("p", "br", "div", "span", "a", "img", "table", "tr", "td", "th",
                  "ul", "ol", "li", "b", "i", "u", "strong", "em", "blockquote", "pre", "code").
    AllowAttrs("href").OnElements("a").
    AllowAttrs("src", "alt", "title").OnElements("img").
    AllowAttrs("style", "class").Globally()

func SanitizeHTML(input string) string {
    return policy.Sanitize(input)
}
```

**应用时机**：在 SMTP 接收邮件时立即净化，原始 HTML 存入 `body_html_raw`，净化后的存入 `body_html`

```go
// internal/smtp/handler.go
func (h *Handler) onData(session *smtp.Session) error {
    // ...解析邮件...
    bodyHTMLRaw := parsed.HTML
    bodyHTMLSanitized := sanitize.SanitizeHTML(bodyHTMLRaw)

    msg := &domain.Message{
        BodyHTML:     bodyHTMLSanitized,    // 净化后，前端直接用
        BodyHTMLRaw:  bodyHTMLRaw,          // 原始，仅管理员可查
        // ...
    }
    return h.messageDAO.Create(ctx, msg)
}
```

**API 端点**：默认返回 `body_html`（净化后）。新增管理员端点 `GET /api/admin/mail/:id/raw` 返回 `body_html_raw`

---

### P0-5：前端 /admin 路由无权限守卫

**原 Bug**：路由守卫仅判断 token 存在，未校验角色

**修复**：前端加 `meta.requiresAdmin` + 守卫校验

```js
// src/router/index.js
const routes = [
  // ...
  {
    path: '/admin',
    name: 'admin',
    component: () => import('@/views/AdminView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true }   // 新增
  }
]

router.beforeEach(async (to) => {
  const auth = useAuthStore()

  if (to.meta.requiresAuth && !auth.isAuthenticated) {
    return { name: 'login' }
  }

  // 修复：管理员路由校验
  if (to.meta.requiresAdmin && auth.user?.role !== 'admin') {
    return { name: 'inbox' }   // 非管理员重定向到收件箱
  }
})
```

**后端兜底**：所有 `/api/admin/*` 端点必须经过 `requireAdmin` 中间件（已有，保持）

---

### P0-6：前端 MailView batchDelete toast 未定义

**原 Bug**：调用了未导入的 `toast`

**修复**：

```vue
<!-- MailView.vue -->
<script setup>
import { useToast } from '@/composables/useToast'
const { toast } = useToast()  // 修复：导入并解构

async function batchDelete() {
  // ...
  toast('删除成功', 'success')
}
</script>
```

---

### P0-7：前端 UploadZone 上传逻辑错乱

**原 Bug**：先标记 done 再后台上传，isUploading 恒 false

**修复策略**：彻底重写上传逻辑，去掉"fire-and-forget"

```vue
<!-- UploadZone.vue -->
<script setup>
const uploadingCount = ref(0)

function isUploading() {
  return uploadingCount.value > 0  // 修复：真实状态
}

async function uploadOne(item) {
  uploadingCount.value++
  item.status = 'uploading'
  try {
    const fd = new FormData()
    fd.append('files', item.file)
    const res = await uploadFiles(fd)
    item.attId = res.files[0].id
    item.status = 'done'
    item.progress = 100
  } catch (err) {
    item.status = 'error'
    item.error = err.message
    toast(`上传失败: ${item.file.name}`, 'error')
  } finally {
    uploadingCount.value--
  }
  emitUpdate()
}

function getAttachmentIds() {
  return items.value
    .filter(item => item.status === 'done' && item.attId)
    .map(item => item.attId)
}
</script>
```

**ComposeView 同步修改**：发送前检查 `isUploading()`，等待所有上传完成

```vue
<!-- ComposeView.vue -->
async function handleSend() {
  if (uploadZone.value.isUploading()) {
    toast('附件正在上传，请稍候', 'warning')
    return
  }
  // ...发送逻辑
}
```

---

### P0-8：JWT 默认密钥

**原 Bug**：`config.js` 提供 `'change-me-in-production'` 默认值

**修复**：启动时强制校验，未设置则 panic

```go
// internal/config/config.go
func Load() (*Config, error) {
    cfg := &Config{
        JWTSecret: os.Getenv("JWT_SECRET"),
        // ...
    }

    // 修复：强制要求 JWT_SECRET
    if cfg.JWTSecret == "" || cfg.JWTSecret == "change-me-in-production" {
        return nil, errors.New("JWT_SECRET must be set to a random string (use `openssl rand -hex 32`)")
    }
    if len(cfg.JWTSecret) < 32 {
        return nil, errors.New("JWT_SECRET must be at least 32 characters")
    }

    return cfg, nil
}
```

```go
// cmd/mymail/main.go
func main() {
    cfg, err := config.Load()
    if err != nil {
        slog.Error("config load failed", "error", err)
        os.Exit(1)
    }
    // ...
}
```

---

### P0-9：init-db.js 与 migrations 不一致

**原 Bug**：两套初始化方式各自缺表

**修复**：**废弃 init-db.js**，统一用 `golang-migrate` 迁移

```go
// internal/storage/db/migrate.go
package db

import (
    "embed"
    "github.com/golang-migrate/migrate/v4"
    "github.com/golang-migrate/migrate/v4/source/iofs"
    "github.com/golang-migrate/migrate/v4/database/sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(db *sql.DB) error {
    source, err := iofs.New(migrationsFS, "migrations")
    if err != nil {
        return err
    }

    driver, err := sqlite3.WithInstance(db, &sqlite3.Config{})
    if err != nil {
        return err
    }

    m, err := migrate.NewWithInstance("iofs", source, "sqlite3", driver)
    if err != nil {
        return err
    }
    defer m.Close()

    if err := m.Up(); err != nil && err != migrate.ErrNoChange {
        return err
    }
    return nil
}
```

**启动时自动迁移**：

```go
// cmd/mymail/main.go
db, err := db.Open(cfg.DBPath)
if err != nil { /* ... */ }

if err := db.Migrate(db); err != nil {
    slog.Error("migration failed", "error", err)
    os.Exit(1)
}
```

**迁移文件清单**（参见第五章）：
- 001_initial_schema（users/messages/attachments/send_log/settings）
- 002_add_apikeys_rules（api_keys/mail_rules + is_default_password 列）
- 003_add_greylist_spamlog（greylist/spam_log/mail_queue）
- 004_add_indexes（补全所有缺失索引）

---

## 八、P1 高危 Bug 修复方案

### P1-1：DAO 层无事务

**修复**：用 `database/sql` 标准事务 + 服务层封装

```go
// internal/service/mail_service.go
func (s *MailService) SendMail(ctx context.Context, userID int64, req *SendMailRequest) error {
    return s.db.RunInTransaction(ctx, func(tx *sql.Tx) error {
        // 1. 创建邮件
        msgID, err := s.messageDAO.CreateWithTx(tx, ctx, msg)
        if err != nil { return err }

        // 2. 创建附件记录
        for _, att := range attachments {
            if err := s.attachmentDAO.CreateWithTx(tx, ctx, msgID, att); err != nil {
                return err  // 自动回滚
            }
        }

        // 3. 写发送日志
        if err := s.sendLogDAO.CreateWithTx(tx, ctx, log); err != nil { return err }

        // 4. 更新存储配额
        if err := s.userDAO.IncrStorageUsedWithTx(tx, ctx, userID, size); err != nil {
            return err
        }

        // 5. 入发送队列
        if err := s.queueDAO.EnqueueWithTx(tx, ctx, queueItem); err != nil {
            return err
        }

        return nil  // 自动提交
    })
}
```

**事务辅助函数**：

```go
// internal/storage/db/tx.go
func (d *DB) RunInTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
    tx, err := d.BeginTx(ctx, nil)
    if err != nil { return err }

    defer func() {
        if p := recover(); p != nil {
            tx.Rollback()
            panic(p)
        }
    }()

    if err := fn(tx); err != nil {
        tx.Rollback()
        return err
    }

    return tx.Commit()
}
```

**应用范围**：
- 邮件发送（创建消息+附件+日志+配额+队列）
- SMTP 接收（写文件+创建消息+附件+配额+规则触发）
- 清空垃圾箱（删附件+删邮件）
- 删除用户（级联删所有数据）
- API Key 创建/删除

---

### P1-2：CORS 全开放

**修复**：

```go
// internal/httpapi/middleware/cors.go
func CORS(cfg *config.Config) gin.HandlerFunc {
    allowedOrigins := map[string]bool{
        fmt.Sprintf("https://%s", cfg.Domain):  true,
        fmt.Sprintf("https://%s", cfg.MailHost): true,
    }
    // 允许配置多个来源
    for _, o := range cfg.CORSAllowedOrigins {
        allowedOrigins[o] = true
    }

    return func(c *gin.Context) {
        origin := c.Request.Header.Get("Origin")
        if allowedOrigins[origin] {
            c.Header("Access-Control-Allow-Origin", origin)
            c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
            c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
            c.Header("Access-Control-Allow-Credentials", "true")
            c.Header("Access-Control-Max-Age", "86400")
        }
        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(204)
            return
        }
        c.Next()
    }
}
```

**配置**：

```bash
# .env.example
CORS_ALLOWED_ORIGINS=https://mail.example.com,https://example.com
```

---

### P1-3：API Key O(n) bcrypt 性能瓶颈

**修复**：用 key_prefix 索引 + 单次 bcrypt 验证

```go
// internal/crypto/apikey.go
package crypto

import (
    "crypto/rand"
    "encoding/hex"
    "golang.org/x/crypto/bcrypt"
)

const (
    keyPrefix = "mk_"
    prefixLen = 8  // 存储 key 明文前 8 字符作为索引
)

// Generate 生成新的 API Key
// 返回明文（仅一次）和存储用的 hash + prefix
func Generate() (plain string, hash string, prefix string, err error) {
    bytes := make([]byte, 32)
    if _, err := rand.Read(bytes); err != nil {
        return "", "", "", err
    }
    plain = keyPrefix + hex.EncodeToString(bytes)
    prefix = plain[:prefixLen]

    hashBytes, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
    if err != nil {
        return "", "", "", err
    }
    return plain, string(hashBytes), prefix, nil
}

// Verify 验证 API Key
func Verify(plain, hash string) bool {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
```

```go
// internal/httpapi/middleware/api_auth.go
func (m *Middleware) APIAuth(apiKeyDAO dao.APIKeyDAO) gin.HandlerFunc {
    return func(c *gin.Context) {
        auth := c.Request.Header.Get("Authorization")
        if !strings.HasPrefix(auth, "Bearer mk_") {
            c.Next()  // 不是 API Key，交给 JWT 中间件
            return
        }

        plain := strings.TrimPrefix(auth, "Bearer ")
        prefix := plain[:8]  // mk_xxxx

        // 修复：用 prefix 索引精确查询（O(1)），而非全表扫描
        key, err := apiKeyDAO.FindByPrefix(c.Request.Context(), prefix)
        if err != nil || key == nil {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "无效的 API Key"})
            return
        }

        // 单次 bcrypt 验证
        if !crypto.Verify(plain, key.KeyHash) {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "无效的 API Key"})
            return
        }

        // 校验 scope
        if !hasScope(key.Scopes, "send") {
            c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "API Key 无发信权限"})
            return
        }

        c.Set("user_id", key.UserID)
        c.Set("api_key_id", key.ID)
        c.Next()
    }
}
```

---

### P1-4：getNextUid 竞态条件

**修复**：用事务 + 行锁

```go
// internal/storage/dao/message.go
func (d *MessageDAO) GetNextUID(ctx context.Context, tx *sql.Tx, userID int64) (int64, error) {
    // 用事务保证原子性
    var uid int64
    err := tx.QueryRowContext(ctx,
        `SELECT COALESCE(MAX(uid), 0) + 1 FROM messages WHERE user_id = ? FOR UPDATE`,
        userID,
    ).Scan(&uid)
    // SQLite 不支持 FOR UPDATE，但事务隔离足够（WAL 模式下）
    return uid, err
}
```

**SQLite 特殊处理**：因为 SQLite 的写锁是数据库级别，事务内 MAX+INSERT 自动原子；若用 PostgreSQL，加 `FOR UPDATE`。

**更稳妥方案**：用 `INSERT ... RETURNING uid`，让数据库生成 UID

```sql
-- 用触发器或 AUTOINCREMENT 保证唯一
-- 实际上 messages 表的 id 已是 AUTOINCREMENT，uid 可单独维护
CREATE TABLE user_uid_seq (
    user_id INTEGER PRIMARY KEY,
    next_uid INTEGER NOT NULL DEFAULT 1
);

-- 获取 UID 时
UPDATE user_uid_seq SET next_uid = next_uid + 1 WHERE user_id = ? RETURNING next_uid - 1;
```

---

### P1-5：storage_limit 未校验

**修复**：上传/接收前校验配额

```go
// internal/service/mail_service.go
func (s *MailService) SendMail(ctx context.Context, userID int64, req *SendMailRequest) error {
    // 计算总大小
    totalSize := int64(len(req.BodyHTML) + len(req.BodyText))
    for _, att := range req.Attachments {
        totalSize += att.Size
    }

    // 修复：校验配额
    user, err := s.userDAO.GetByID(ctx, userID)
    if err != nil { return err }

    if user.StorageUsed+totalSize > user.StorageLimit {
        return ErrStorageQuotaExceeded
    }

    // ...发送逻辑
}
```

```go
// internal/httpapi/handler/mail.go
if errors.Is(err, service.ErrStorageQuotaExceeded) {
    c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "存储空间不足"})
    return
}
```

**SMTP 接收同样校验**：

```go
// internal/smtp/handler.go
user, err := h.userDAO.GetByUsername(ctx, username)
if user.StorageUsed+int64(len(data)) > user.StorageLimit {
    return errors.New("user storage quota exceeded")
}
```

---

### P1-6：backup.sh 备份含 .env 密钥

**修复**：

```bash
# scripts/backup.sh
#!/bin/bash
BACKUP_DIR="./backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/mymail_backup_$TIMESTAMP.tar.gz"

mkdir -p "$BACKUP_DIR"

# 修复：明确排除 .env
tar -czf "$BACKUP_FILE" \
    --exclude='.env' \
    --exclude='node_modules' \
    --exclude='backups' \
    data/ \
    scripts/ \
    config/ \
    2>&1 | tee -a "$BACKUP_DIR/backup.log"  # 修复：日志而非吞错

if [ $? -ne 0 ]; then
    echo "[ERROR] Backup failed at $(date)" >> "$BACKUP_DIR/backup.log"
    exit 1
fi

# 单独备份 .env（加密）
if [ -f .env ]; then
    gpg --symmetric --cipher-algo AES256 .env -o "$BACKUP_DIR/env_$TIMESTAMP.gpg" 2>/dev/null
    echo "[INFO] .env encrypted backup created"
fi

# 保留最近 7 份
ls -t "$BACKUP_DIR"/mymail_backup_*.tar.gz | tail -n +8 | xargs -r rm
```

---

### P1-7：rule-engine ReDoS 风险

**修复**：用 Go 标准库 `regexp`（基于 RE2，天然防 ReDoS）

```go
// internal/rules/matcher.go
package rules

import (
    "regexp"
    "time"
)

// Go 标准库 regexp 使用 RE2 引擎，不支持回溯，天然防 ReDoS
// 但仍需限制编译时间
func compileRegex(pattern string) (*regexp.Regexp, error) {
    // 限制 pattern 长度
    if len(pattern) > 500 {
        return nil, errors.New("regex pattern too long")
    }

    // 限制编译时间（Go regexp 不支持超时，但 RE2 编译快）
    done := make(chan struct {
        re  *regexp.Regexp
        err error
    }, 1)
    go func() {
        re, err := regexp.Compile(pattern)
        done <- struct {
            re  *regexp.Regexp
            err error
        }{re, err}
    }()

    select {
    case result := <-done:
        return result.re, result.err
    case <-time.After(2 * time.Second):
        return nil, errors.New("regex compile timeout")
    }
}
```

**注意**：Go 的 `regexp` 包基于 RE2，**不支持反向引用**（如 `\1`），但支持大部分常用语法。若需完整 PCRE，用 `github.com/dlclark/regexp2`（支持超时）。

---

### P1-8：Dockerfile HEALTHCHECK 无效

**修复**：用专用 `/health` 端点

```dockerfile
# deployments/docker/Dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:3000/health || exit 1
```

```go
// internal/httpapi/handler/health.go
func (h *HealthHandler) Health(c *gin.Context) {
    ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
    defer cancel()

    status := gin.H{
        "status": "ok",
        "time":   time.Now().UTC(),
    }

    // 检查 DB
    if err := h.db.PingContext(ctx); err != nil {
        status["status"] = "degraded"
        status["db"] = "down"
        c.JSON(503, status)
        return
    }
    status["db"] = "up"

    // 检查 SMTP（监听端口）
    // ...

    c.JSON(200, status)
}
```

---

### P1-9：Docker compose 数据卷路径不一致

**修复**：统一为 `/app/data`

```yaml
# deployments/docker/docker-compose.yml
services:
  app:
    build: .
    volumes:
      - mymail-data:/app/data       # 修复：统一路径
    # ...

  dovecot:
    volumes:
      - mymail-data:/app/data       # 修复：与 app 一致
      - ./config/dovecot/dovecot.conf:/etc/dovecot/dovecot.conf:ro
      - ./config/dovecot/dovecot-sql.conf:/etc/dovecot/dovecot-sql.conf:ro
    # ...

  postfix:
    # ...
```

**dovecot-sql.conf 同步修改**：

```sql
connect = /app/data/mymail.db                  -- 修复：与 app 路径一致
user_query = SELECT '/app/data/maildir/%d/%n' AS home, ...
```

---

### P1-10：Dovecot uid/gid 与 Dockerfile 不匹配

**修复**：在 Dockerfile 中固定 mymail 用户 UID/GID 为 1000

```dockerfile
# deployments/docker/Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /mymail ./cmd/mymail

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tini wget
# 修复：固定 UID/GID 为 1000，与 dovecot 配置一致
RUN addgroup -g 1000 -S mymail && adduser -u 1000 -S mymail -G mymail

WORKDIR /app
COPY --from=builder /mymail /app/mymail
RUN mkdir -p /app/data/maildir /app/data/attachments && \
    chown -R mymail:mymail /app

USER mymail
EXPOSE 3000 25
VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:3000/health || exit 1

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/app/mymail"]
```

---

### P1-11：docker-compose 强制 SMTP_TLS_REJECT_UNAUTHORIZED=false

**修复**：移除该环境变量，让它走 config.go 默认值 true

```yaml
# deployments/docker/docker-compose.yml
services:
  app:
    environment:
      - NODE_ENV=production
      - MAIL_HOST=postfix
      - SMTP_SEND_PORT=587
      # 修复：移除 SMTP_TLS_REJECT_UNAUTHORIZED=false
      # 默认 true，对内部 Postfix 也强制 TLS（用 STARTTLS）
```

**Postfix 配置**：让内部 Postfix 也用 STARTTLS

```yaml
postfix:
  image: boky/postfix
  environment:
    - HOSTNAME=${DOMAIN}
    - ALLOWED_SENDER_DOMAINS=${DOMAIN}
    - INBOUND_ENABLED=false
    - ENABLE_TLS=true          # 新增
```

---

### P1-12：setup.sh 以 root 运行 Dovecot

**修复**：用专用 vmail 用户（UID 5000）

```bash
# scripts/setup.sh
# 创建 vmail 用户（而非用 root）
groupadd -g 5000 vmail
useradd -u 5000 -g vmail -s /usr/sbin/nologin -d /var/mail vmail

# Dovecot 配置使用 vmail
sed -i 's/uid=root/uid=vmail/g' /etc/dovecot/dovecot-sql.conf
sed -i 's/gid=root/gid=vmail/g' /etc/dovecot/dovecot-sql.conf
sed -i 's/1000/5000/g' /etc/dovecot/dovecot-sql.conf  # 修复：与 vmail UID 一致

# Maildir 所有者改为 vmail
chown -R vmail:vmail /app/data/maildir
```

---

### P1-13：smtp-sender.js sendLocal 未保存 DB / 未处理附件

**修复**：完整重写本地投递逻辑

```go
// internal/mailsender/local.go
package mailsender

func (s *LocalSender) SendLocal(ctx context.Context, req *SendRequest) error {
    return s.db.RunInTransaction(ctx, func(tx *sql.Tx) error {
        for _, recipient := range req.To {
            username, domain := splitAddress(recipient)
            if domain != s.cfg.Domain {
                continue  // 非本地域，跳过
            }

            user, err := s.userDAO.GetByUsernameWithTx(tx, ctx, username)
            if err != nil {
                return fmt.Errorf("recipient not found: %s", recipient)
            }

            // 1. 写入 Maildir
            maildirPath := filepath.Join(s.cfg.Mail.MaildirPath, domain, username, "new")
            if err := os.MkdirAll(maildirPath, 0755); err != nil {
                return err
            }

            filename := fmt.Sprintf("%d.%s.%s", time.Now().UnixNano(),
                randomString(8), s.cfg.Domain)
            fullPath := filepath.Join(maildirPath, filename)

            rawEmail := buildRawEmail(req)  // 含完整 MIME 头
            if err := os.WriteFile(fullPath, []byte(rawEmail), 0644); err != nil {
                return err
            }

            // 2. 保存附件
            var attachRecords []*domain.Attachment
            if len(req.Attachments) > 0 {
                attachDir := filepath.Join(s.cfg.Mail.AttachmentPath, strconv.FormatInt(user.ID, 10))
                if err := os.MkdirAll(attachDir, 0755); err != nil {
                    return err
                }
                for _, att := range req.Attachments {
                    attachPath := filepath.Join(attachDir, randomFilename(att.Filename))
                    if err := os.WriteFile(attachPath, att.Content, 0644); err != nil {
                        return err
                    }
                    attachRecords = append(attachRecords, &domain.Attachment{
                        Filename:    att.Filename,
                        MimeType:    att.MimeType,
                        SizeBytes:   int64(len(att.Content)),
                        StoragePath: attachPath,
                    })
                }
            }

            // 3. 写入数据库
            msgID, err := s.messageDAO.CreateWithTx(tx, ctx, &domain.Message{
                UserID:      user.ID,
                Folder:      "INBOX",
                FromAddr:    req.From.Address,
                FromName:    req.From.Name,
                ToAddr:      recipient,
                Subject:     req.Subject,
                BodyHTML:    sanitize.SanitizeHTML(req.HTML),
                BodyHTMLRaw: req.HTML,
                BodyText:    req.Text,
                HasAttach:   len(attachRecords) > 0,
                AttachCount: len(attachRecords),
                SizeBytes:   int64(len(rawEmail)),
                MessageID:   generateMessageID(s.cfg.Domain),
                UID:         s.messageDAO.GetNextUID(tx, ctx, user.ID),
            })
            if err != nil {
                return err
            }

            // 4. 保存附件记录
            for _, att := range attachRecords {
                att.MessageID = msgID
                if err := s.attachmentDAO.CreateWithTx(tx, ctx, att); err != nil {
                    return err
                }
            }

            // 5. 更新存储配额（含校验）
            newUsed := user.StorageUsed + int64(len(rawEmail))
            if newUsed > user.StorageLimit {
                return ErrStorageQuotaExceeded
            }
            if err := s.userDAO.UpdateStorageUsedWithTx(tx, ctx, user.ID, int64(len(rawEmail))); err != nil {
                return err
            }

            // 6. 触发用户规则
            if err := s.ruleEngine.RunForMessageWithTx(tx, ctx, user.ID, msgID); err != nil {
                // 规则失败不阻断投递，仅记录日志
                slog.Warn("rule engine failed", "error", err, "msg_id", msgID)
            }

            // 7. WebSocket 通知
            s.wsHub.NotifyNewMail(user.ID, map[string]any{
                "id":      msgID,
                "from":    req.From.Address,
                "subject": req.Subject,
                "time":    time.Now().UTC().Format(time.RFC3339),
            })
        }
        return nil
    })
}
```

---

### P1-14：admin.js POST /users 调用错误方法

**修复**：

```go
// internal/service/admin_service.go
func (s *AdminService) CreateUser(ctx context.Context, req *CreateUserRequest) (*domain.User, error) {
    hash, err := crypto.HashPassword(req.Password)
    if err != nil { return nil, err }

    user := &domain.User{
        Username:           req.Username,
        Email:              req.Email,
        PasswordHash:       hash,
        DisplayName:        req.DisplayName,
        Role:               "user",
        StorageLimit:       req.StorageLimit,    // 修复：直接设置 storage_limit
        IsDefaultPassword:  1,                    // 修复：标记需改密
        IsActive:           1,
    }

    if err := s.userDAO.Create(ctx, user); err != nil {
        return nil, err
    }

    // 修复：移除错误的 updateStorageUsed(userId, 0) 调用
    return user, nil
}
```

---

### P1-15：前端 401 处理用 window.location.href

**修复**：

```js
// src/api/index.js
import router from '@/router'

if (res.status === 401) {
    setToken(null)
    // 修复：用 router.replace 而非 window.location.href
    if (router.currentRoute.value.name !== 'login') {
        router.replace({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } })
    }
    throw new Error('请重新登录')
}
```

---

### P1-16：前端 AdminView 重置密码明文 toast

**修复**：改为弹窗 + 复制按钮

```vue
<!-- AdminView.vue -->
<script setup>
const resetPwModal = ref({ show: false, password: '', userId: null })

async function resetPassword(id) {
    if (!confirm('确认重置该用户的密码？')) return  // 修复：加确认

    const pw = generateRandomPassword(16)
    await resetPassword(id, pw)
    resetPwModal.value = { show: true, password: pw, userId: id }  // 修复：弹窗显示
}

function copyPassword() {
    navigator.clipboard.writeText(resetPwModal.value.password)
    toast('已复制到剪贴板', 'success')
}
</script>

<template>
  <Modal v-if="resetPwModal.show" @close="resetPwModal.show = false">
    <div class="p-6">
      <h3 class="text-lg font-bold mb-4">新密码（仅显示一次）</h3>
      <div class="flex items-center gap-2 bg-dark-800 p-3 rounded">
        <code class="flex-1 font-mono">{{ resetPwModal.password }}</code>
        <button @click="copyPassword" class="btn-secondary">复制</button>
      </div>
      <p class="text-sm text-dark-500 mt-2">请立即告知用户并让其登录后修改密码</p>
    </div>
  </Modal>
</template>
```

---

### P1-17：前端 AppLayout 移动端退出按钮不可点击

**修复**：

```vue
<!-- AppLayout.vue -->
<template>
  <aside>
    <!-- ... -->
    <button
      @click="logout"
      class="sidebar-item flex items-center gap-3 w-full"
      aria-label="退出登录"  <!-- 修复：始终可见 + aria-label -->
    >
      <span>↩</span>
      <span class="hidden sm:inline">退出</span>  <!-- 移动端仅显示图标 -->
    </button>
  </aside>
</template>
```

---

### P1-18：前端 WS token 走 URL query

**修复**：保持 URL query 兼容，但优先用子协议

```js
// src/stores/ws.js
function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host
    const token = auth.token

    // 修复：用子协议传 token（推荐）
    const ws = new WebSocket(`${protocol}//${host}/ws`, [`bearer.${token}`])

    // 兼容旧方式：URL query（仅当子协议不被支持时）
    // const ws = new WebSocket(`${protocol}//${host}/ws?token=${encodeURIComponent(token)}`)

    ws.value = ws
    // ...
}
```

```go
// internal/ws/auth.go
func (a *Authenticator) Authenticate(c *websocket.Conn) (int64, error) {
    // 优先从子协议读 token
    protocols := c.Subprotocol()
    if strings.HasPrefix(protocols, "bearer.") {
        token := strings.TrimPrefix(protocols, "bearer.")
        return a.verifyJWT(token)
    }

    // 兼容 URL query（向后兼容）
    token := c.Request().URL.Query().Get("token")
    if token != "" {
        return a.verifyJWT(token)
    }

    return 0, errors.New("no token provided")
}
```

---

### P1-19：前端 escapeHtml 未转义 `"` `'`

**修复**：用更安全的实现

```js
// src/composables/useFormat.js
export function escapeHtml(str) {
    if (!str) return ''
    const div = document.createElement('div')
    div.textContent = str  // 修复：用 textContent 自动转义所有特殊字符
    return div.innerHTML
}
```

**或更明确的实现**：

```js
export function escapeHtml(str) {
    if (!str) return ''
    return str
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')   // 修复
        .replace(/'/g, '&#39;')    // 修复
}
```

**注意**：HTML 净化应优先用 DOMPurify（见 P0-4），escapeHtml 仅用于纯文本展示场景。

---

### P1-20：.env.example `$(openssl rand -hex 8)` 不生效

**修复**：

```bash
# .env.example
# 修复：用占位符，文档说明生成方式
ADMIN_PASSWORD=CHANGE_ME_TO_RANDOM_STRING
# 生成命令：openssl rand -hex 16

JWT_SECRET=CHANGE_ME_TO_RANDOM_STRING
# 生成命令：openssl rand -hex 32
```

---

### P1-21：knexfile.js 未加载 dotenv

**修复**：Go 版本废弃 knexfile.js，配置加载由 viper 统一管理

```go
// internal/config/config.go
func Load() (*Config, error) {
    viper.SetConfigFile(".env")
    viper.AutomaticEnv()
    viper.SetEnvPrefix("")  // 不加前缀

    // 设置默认值
    viper.SetDefault("PORT", 3000)
    viper.SetDefault("DB_PATH", "./data/mymail.db")
    // ...

    if err := viper.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            // .env 不存在也行，用环境变量
        }
    }

    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}
```

---

## 九、P2 工程化改进

### P2-1：引入 ESLint + Prettier + Husky（前端）

```json
// mymail-vue/package.json
"devDependencies": {
    "eslint": "^9.0.0",
    "eslint-plugin-vue": "^9.0.0",
    "@vue/eslint-config-typescript": "^13.0.0",
    "prettier": "^3.0.0",
    "husky": "^9.0.0",
    "lint-staged": "^15.0.0"
},
"scripts": {
    "lint": "eslint src --ext .vue,.js --fix",
    "format": "prettier --write src"
}
```

### P2-2：Go lint 配置

```yaml
# .golangci.yml
linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - typecheck
    - unused
    - gosec        # 安全检查
    - gocritic
    - revive
    - misspell
  disable:
    - lll

linters-settings:
  gosec:
    excludes:
      - G104  # unhandled error (我们用 errcheck)
  gocyclo:
    min-complexity: 20

run:
  timeout: 5m
  tests: true
```

### P2-3：补全测试（见第十一章）

### P2-4：前端迁移 TypeScript（渐进式）

- 第一步：`jsconfig.json` → `tsconfig.json`
- 第二步：核心文件改 `.ts`：`api/index.ts`、`stores/auth.ts`
- 第三步：View 组件改 `.vue` + `<script setup lang="ts">`

### P2-5：清理脚手架残留

删除：`HelloWorld.vue`、`TheWelcome.vue`、`WelcomeItem.vue`、`icons/`、`base.css`

### P2-6：抽离复用组件

新增：`PageHeader.vue`、`useFileIcon.js`、`useFormatSize.js`、`Modal.vue`

### P2-7：添加 CSP meta + lang + theme-color

```html
<!-- index.html -->
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta name="theme-color" content="#020617">
    <meta name="description" content="MyMail 自托管邮件平台">
    <meta http-equiv="Content-Security-Policy"
          content="default-src 'self'; script-src 'self' cdn.quilljs.com; style-src 'self' 'unsafe-inline' fonts.googleapis.com cdn.quilljs.com; img-src 'self' data: blob:; connect-src 'self' ws: wss:;">
    <title>MyMail - 邮件平台</title>
</head>
```

### P2-8：Quill 改为 npm 打包

```bash
npm install quill @vueup/vue-quill
```

```vue
<script setup>
import { QuillEditor } from '@vueup/vue-quill'
import '@vueup/vue-quill/dist/vue-quill.snow.css'
</script>
```

---

## 十、分阶段实施计划

### 阶段 1：基础架构搭建（无业务功能）

**目标**：搭建可运行的 Go 项目骨架，能启动 HTTP 服务

**任务**：
1. 初始化 Go module：`go mod init github.com/yourname/mymail-go`
2. 实现配置加载（viper）
3. 实现日志（slog）
4. 实现数据库连接（modernc.org/sqlite + sqlx）
5. 实现迁移系统（golang-migrate + 嵌入 SQL）
6. 实现 Gin 路由 + 中间件（CORS/Recover/Logger/Security）
7. 实现 `/health` 端点
8. 实现 graceful shutdown
9. 实现多阶段 Dockerfile
10. 编写最小测试

**交付物**：能启动的 Go 二进制，访问 `/health` 返回 200

### 阶段 2：认证模块（P0-8 + P0-9）

**目标**：完成用户注册、登录、JWT 认证

**任务**：
1. 实现 user DAO（含事务接口）
2. 实现 bcrypt 密码哈希（兼容现有数据）
3. 实现 JWT 生成/校验（P0-8：强制要求密钥）
4. 实现 auth handler + service
5. 实现 auth middleware（JWT）
6. 实现登录锁定逻辑
7. 实现 is_default_password 强制改密
8. 编写完整测试

**对应 Bug**：P0-8（JWT 默认密钥）、P0-9（migrations 统一）

### 阶段 3：邮件核心模块（P0-3 + P1-1 + P1-4 + P1-5 + P1-13）

**目标**：完成邮件 CRUD + 收发

**任务**：
1. 实现 message/attachment DAO
2. 实现 maildir 文件操作
3. 实现 mail handler + service
4. **P0-3：所有 :id 端点加归属校验**
5. **P1-1：所有多步操作用事务**
6. **P1-4：getNextUid 用事务**
7. **P1-5：storage_limit 校验**
8. 实现 SMTP 接收器（go-smtp）
9. **P1-13：sendLocal 完整重写**
10. 实现 multer 等价的附件上传（multipart + MIME 校验 + 文件头魔数）
11. 实现批量下载 zip
12. 编写完整测试

**对应 Bug**：P0-3、P1-1、P1-4、P1-5、P1-13

### 阶段 4：SMTP + 反垃圾（P0-2 + P1-7）

**目标**：完成 SMTP 收发 + 反垃圾

**任务**：
1. 实现 SMTP 发送器（go-mail）
2. **P0-2：实现完整 spam 包（SPF + DNSBL + 评分）**
   - SPF 完整实现（ip4/ip6/mx/a/include/exists，递归深度限制）
   - DNSBL 并行查询
   - 评分规则
3. 实现灰名单
4. 实现连接级限流
5. **P1-7：规则引擎用 Go regexp（RE2，防 ReDoS）**
6. 实现规则引擎（含 forward/flag 动作完整实现）
7. 实现发送队列（mail_queue 表 + 后台 worker）
8. 编写完整测试

**对应 Bug**：P0-2、P1-7

### 阶段 5：API Key + 管理员（P1-2 + P1-3 + P1-14）

**目标**：完成 API Key 发信 + 管理后台

**任务**：
1. 实现 api_key DAO
2. **P1-3：实现 key_prefix 索引 + 单次 bcrypt**
3. 实现 api_auth middleware
4. 实现 /api/v1/send 端点
5. 实现 admin handler + service
6. **P1-14：修复 admin POST /users 逻辑**
7. 实现 DNS 状态检测
8. 实现系统配置 CRUD
9. 编写完整测试

**对应 Bug**：P1-2、P1-3、P1-14

### 阶段 6：WebSocket（P1-18）

**目标**：完成实时推送

**任务**：
1. 实现 WebSocket hub（用户维度连接管理）
2. **P1-18：支持子协议认证 + URL query 兼容**
3. 实现心跳（30s ping/pong）
4. 实现认证超时（10s）
5. 实现消息大小限制（maxPayload）
6. 实现消息频率限制
7. 实现优雅关闭
8. 编写完整测试

**对应 Bug**：P1-18

### 阶段 7：前端 Bug 修复（P0-4 + P0-5 + P0-6 + P0-7 + P1-15/16/17/19）

**目标**：修复前端所有 P0/P1 Bug

**任务**：
1. **P0-4：引入 DOMPurify，净化 v-html**
   - MailDetailView 修改
   - ComposeView 回复/转发修改
2. **P0-5：/admin 路由加 meta.requiresAdmin + 守卫**
3. **P0-6：MailView 导入 useToast**
4. **P0-7：UploadZone 重写上传逻辑**
5. **P1-15：401 处理改 router.replace**
6. **P1-16：AdminView 重置密码改弹窗**
7. **P1-17：AppLayout 退出按钮始终可见**
8. **P1-19：escapeHtml 用 textContent 实现**
9. 前端引入 ESLint + Prettier
10. 清理脚手架残留

**对应 Bug**：P0-4、P0-5、P0-6、P0-7、P1-15、P1-16、P1-17、P1-19

### 阶段 8：部署修复（P0-1 + P1-6 + P1-8/9/10/11/12 + P1-20/21）

**目标**：修复所有部署相关 Bug

**任务**：
1. **P0-1：修复 dovecot-sql.conf 列名**
2. **P1-6：backup.sh 排除 .env + 加密备份**
3. **P1-8：Dockerfile HEALTHCHECK 用 /health**
4. **P1-9：docker-compose 数据卷路径一致化**
5. **P1-10：Dockerfile 固定 UID/GID 1000**
6. **P1-11：移除 SMTP_TLS_REJECT_UNAUTHORIZED=false**
7. **P1-12：setup.sh 用 vmail 用户**
8. **P1-20：.env.example 用占位符**
9. **P1-21：废弃 knexfile.js**

**对应 Bug**：P0-1、P1-6、P1-8、P1-9、P1-10、P1-11、P1-12、P1-20、P1-21

### 阶段 9：端到端集成测试

**目标**：用真实数据验证完整流程

**任务**：
1. 编写 E2E 测试脚本（Docker compose 启动完整环境）
2. 测试用例：
   - 用户注册 → 登录 → 收邮件 → 发邮件 → 删除
   - SMTP 外部邮件接收 → SPF/DNSBL → 灰名单 → 投递 → WebSocket 通知
   - API Key 创建 → 用 API Key 发信
   - 规则创建 → 触发 → 动作执行
   - 管理员创建用户 → 强制改密
   - Dovecot IMAP 登录 → 读取邮件
3. 性能测试（用 Vegeta 或 k6）

### 阶段 10：数据迁移 + 上线

**目标**：从 Node.js 平滑迁移到 Go

**任务**：
1. 编写数据迁移脚本（备份 + schema 升级 + body_html 净化）
2. 在测试环境完整演练
3. 选择低峰期切换
4. 保留 Node.js 版本 7 天作为回滚备份

---

## 十一、测试策略

### 11.1 测试金字塔

```
        ┌───────────┐
        │  E2E (5%) │  ← Docker compose 完整流程
        └───────────┘
       ┌─────────────┐
       │ 集成 (20%)   │  ← HTTP API + 真实 SQLite
       └─────────────┘
      ┌───────────────┐
      │  单元 (75%)    │  ← 纯函数 / DAO / Service
      └───────────────┘
```

### 11.2 测试覆盖目标

| 模块 | 单元测试 | 集成测试 | E2E | 总覆盖率目标 |
|---|---|---|---|---|
| config | ✓ | - | - | 90% |
| crypto (jwt/bcrypt/apikey) | ✓ | - | - | 100% |
| storage/dao | ✓（mock DB） | ✓（真实 SQLite） | - | 85% |
| service | ✓（mock DAO） | ✓（真实 DAO） | - | 85% |
| httpapi/handler | - | ✓（httptest） | - | 80% |
| httpapi/middleware | ✓ | ✓ | - | 90% |
| smtp | ✓ | ✓ | ✓ | 75% |
| spam | ✓ | ✓ | - | 90% |
| rules | ✓ | ✓ | - | 85% |
| ws | ✓ | - | ✓ | 70% |
| sanitize | ✓ | - | - | 100% |
| **总计** | | | | **80%+** |

### 11.3 测试工具

| 工具 | 用途 |
|---|---|
| 标准 `testing` | 单元测试框架 |
| `testify/assert` | 断言 |
| `testify/require` | 必须断言 |
| `testify/mock` | Mock |
| `httptest` | HTTP 集成测试 |
| `testcontainers-go` | Docker 容器测试（可选） |
| `stretchr/testify/suite` | 测试套件 |

### 11.4 关键测试用例

#### 11.4.1 安全测试

```go
// internal/httpapi/handler/mail_test.go
func TestMailHandler_AllOperations_OwnershipCheck(t *testing.T) {
    tests := []struct{
        name   string
        method string
        path   string
    }{
        {"read", "PUT", "/api/mail/2/read"},
        {"unread", "PUT", "/api/mail/2/unread"},
        {"star", "PUT", "/api/mail/2/star"},
        {"delete", "DELETE", "/api/mail/2"},
        {"restore", "PUT", "/api/mail/2/restore"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 用户 A 的 token
            // 邮件 2 属于用户 B
            // 期望 403
        })
    }
}

func TestMailHandler_Send_SQLInjection(t *testing.T) {
    // 主题含 SQL 注入 payload
    // 期望：邮件正常创建，无 SQL 执行
}

func TestAPIAuth_BruteForce(t *testing.T) {
    // 连续 5 次失败密码
    // 期望：第 6 次返回 423
}
```

#### 11.4.2 SMTP 测试

```go
// internal/smtp/receiver_test.go
func TestSMTPReceiver_ReceiveExternal(t *testing.T) {
    // 启动 SMTP 服务器
    // 用 go-mail 连接发送一封邮件
    // 期望：邮件出现在收件人 INBOX
    // 期望：WebSocket 收到通知
}

func TestSMTPReceiver_Greylist(t *testing.T) {
    // 首次连接发送
    // 期望：临时拒绝
    // 等待 delayMs
    // 第二次连接
    // 期望：放行
}

func TestSMTPReceiver_SpamRejection(t *testing.T) {
    // 模拟 SPF fail
    // 期望：邮件被拒
}
```

#### 11.4.3 数据完整性测试

```go
func TestMailService_SendMail_TransactionRollback(t *testing.T) {
    // 模拟 attachment 创建失败
    // 期望：邮件记录也回滚，无孤儿数据
}

func TestMessageDAO_GetNextUID_Concurrent(t *testing.T) {
    // 并发 100 个 goroutine 同时创建邮件
    // 期望：所有 UID 唯一
}
```

### 11.5 CI/CD 集成

```yaml
# .github/workflows/test.yml
name: Tests
on: [push, pull_request]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4

  test:
    runs-on: ubuntu-latest
    strategy:
      matrix: { go-version: ['1.22', '1.23'] }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: ${{ matrix.go-version }} }
      - run: go test -race -coverprofile=coverage.out ./...
      - name: Upload coverage
        uses: codecov/codecov-action@v4
      - name: Check coverage
        run: |
          COV=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | tr -d '%')
          if [ $(echo "$COV < 80" | bc) -eq 1 ]; then
            echo "Coverage $COV% < 80%"
            exit 1
          fi

  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Run Gosec Security Scanner
        uses: securego/gosec@master
        with:
          args: ./...
```

---

## 十二、部署方案

### 12.1 单二进制部署（最简）

```bash
# 构建
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o mymail ./cmd/mymail

# 部署
scp mymail user@server:/opt/mymail/
scp .env user@server:/opt/mymail/
scp -r config/ user@server:/opt/mymail/

# 启动
ssh user@server
cd /opt/mymail
./mymail
```

### 12.2 Docker 部署（推荐）

```yaml
# deployments/docker/docker-compose.yml
services:
  app:
    build: .
    image: mymail-go:latest
    container_name: mymail-app
    restart: unless-stopped
    ports:
      - "3000:3000"
      - "25:25"
    volumes:
      - mymail-data:/app/data       # 修复：统一路径
    env_file: .env
    environment:
      - NODE_ENV=production
      - MAIL_HOST=postfix
      - SMTP_SEND_PORT=587
      # 修复：移除 SMTP_TLS_REJECT_UNAUTHORIZED=false
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:3000/health"]
      interval: 30s
      timeout: 5s
      retries: 3
    depends_on:
      - dovecot
      - postfix

  dovecot:
    image: dovecot/dovecot:latest
    container_name: mymail-dovecot
    restart: unless-stopped
    ports:
      - "993:993"
    volumes:
      - mymail-data:/app/data       # 修复：与 app 一致
      - ./config/dovecot/dovecot.conf:/etc/dovecot/dovecot.conf:ro
      - ./config/dovecot/dovecot-sql.conf:/etc/dovecot/dovecot-sql.conf:ro
    # 修复：固定 uid/gid 1000
    user: "1000:1000"

  postfix:
    image: boky/postfix
    container_name: mymail-postfix
    restart: unless-stopped
    ports:
      - "587:587"
    environment:
      - HOSTNAME=${DOMAIN}
      - ALLOWED_SENDER_DOMAINS=${DOMAIN}
      - INBOUND_ENABLED=false
      - ENABLE_TLS=true             # 新增：启用 TLS
    volumes:
      - postfix-spool:/var/spool/postfix

  nginx:
    image: nginx:alpine
    container_name: mymail-nginx
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./config/nginx/default.conf:/etc/nginx/conf.d/default.conf:ro
      - nginx-ssl:/etc/nginx/ssl
    depends_on:
      - app

volumes:
  mymail-data:
  nginx-ssl:
  postfix-spool:
```

### 12.3 Dockerfile（多阶段 + 嵌入前端）

```dockerfile
# deployments/docker/Dockerfile

# Stage 1: 构建前端
FROM node:22-alpine AS frontend
WORKDIR /web
COPY mymail-vue/package*.json ./
RUN npm ci
COPY mymail-vue/ ./
RUN npm run build

# Stage 2: 构建后端
FROM golang:1.22-alpine AS backend
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 复制前端构建产物到嵌入目录
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /mymail ./cmd/mymail

# Stage 3: 运行时
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tini wget tzdata
# 修复：固定 UID/GID 1000
RUN addgroup -g 1000 -S mymail && adduser -u 1000 -S mymail -G mymail

WORKDIR /app
COPY --from=backend /mymail /app/mymail
COPY --from=backend /app/config /app/config
COPY --from=backend /app/scripts /app/scripts
RUN mkdir -p /app/data/maildir /app/data/attachments && \
    chown -R mymail:mymail /app

USER mymail
EXPOSE 3000 25
VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:3000/health || exit 1

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/app/mymail"]
```

### 12.4 嵌入前端资源

```go
// cmd/mymail/main.go
package main

import (
    "embed"
    "io/fs"
)

//go:embed all:web/dist
var webFS embed.FS

func getWebFS() fs.FS {
    sub, _ := fs.Sub(webFS, "web/dist")
    return sub
}

// 在 router 中
router.StaticFS("/", getWebFS())  // SPA 静态资源
```

---

## 十三、风险评估与回滚

### 13.1 风险矩阵

| 风险 | 概率 | 影响 | 缓解措施 |
|---|---|---|---|
| API 契约不兼容导致前端报错 | 中 | 高 | 用 OpenAPI 文档对照测试，E2E 覆盖 |
| 数据迁移损坏现有数据 | 低 | 极高 | 自动备份 + 演练 + 可回滚 |
| Go SMTP 库不如 Node.js smtp-server 稳定 | 中 | 中 | 压测 + 灰度 + 保留 Node.js 版本 7 天 |
| bcrypt 跨语言兼容性问题 | 低 | 高 | 测试 `$2b$` 格式互通 |
| modernc.org/sqlite 性能不足 | 低 | 中 | 压测 + 可切换 mattn/go-sqlite3 |
| 前端 DOMPurify 误杀正常 HTML | 中 | 低 | 配置允许标签白名单，可调整 |
| Dovecot 配置变更导致 IMAP 失败 | 中 | 高 | 单独测试 Dovecot 认证 |
| CGO_ENABLED=0 导致 SQLite 性能问题 | 低 | 中 | modernc.org/sqlite 是纯 Go，无需 CGO |

### 13.2 回滚方案

#### 13.2.1 数据库回滚

```bash
# 迁移前自动备份
cp data/mymail.db data/mymail.db.backup.$(date +%Y%m%d%H%M%S)

# 回滚
cp data/mymail.db.backup.{timestamp} data/mymail.db
```

#### 13.2.2 应用回滚

```bash
# Docker
docker tag mymail-go:latest mymail-go:rollback
docker tag mymail-node:old mymail-go:latest
docker compose up -d app

# 二进制
systemctl stop mymail
mv /opt/mymail/mymail /opt/mymail/mymail.go
mv /opt/mymail/mymail.node.bak /opt/mymail/mymail
systemctl start mymail
```

#### 13.2.3 DNS 回滚

保持 DNS 不变，仅切换后端服务，DNS 无需回滚。

### 13.3 灰度策略

1. **第 1 天**：测试环境部署，跑完整 E2E
2. **第 2-3 天**：生产环境并行部署（不同端口），用 Nginx 灰度 10% 流量
3. **第 4-5 天**：观察日志，无异常则提升到 50%
4. **第 6-7 天**：100% 切换
5. **第 8-14 天**：保留 Node.js 版本可回滚
6. **第 15 天**：下线 Node.js 版本

---

## 十四、关键参数清单

### 14.1 必须修改的配置

| 参数 | 默认值 | 必须修改为 | 生成命令 |
|---|---|---|---|
| JWT_SECRET | change-me | 32+ 字符随机串 | `openssl rand -hex 32` |
| ADMIN_PASSWORD | CHANGE_ME | 强密码 | `openssl rand -hex 16` |
| DOMAIN | your-domain.com | 实际域名 | - |
| MAIL_HOST | mail.your-domain.com | 实际邮件域名 | - |

### 14.2 性能相关参数

| 参数 | 默认值 | 调优建议 |
|---|---|---|
| PORT | 3000 | Nginx 反代后无需改 |
| DB_PATH | ./data/mymail.db | SSD 磁盘 |
| MAX_ATTACHMENT_SIZE | 26214400 (25MB) | 根据需求调整 |
| SMTP_PORT | 25 | 云厂商需解封 |
| SMTP_MAX_CONNECTIONS_PER_IP | 10 | 视流量调整 |
| SMTP_RATE_WINDOW_MS | 60000 | - |
| GREYLIST_DELAY_MS | 300000 (5min) | - |
| RATE_LIMIT_MAX | 100 | - |
| SEND_RATE_LIMIT_PER_MIN | 10 | - |
| SPAM_THRESHOLD | 10 | - |
| SPAM_SUSPICIOUS_THRESHOLD | 5 | - |

### 14.3 Go 特有参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| GOMAXPROCS | CPU 核数 | Go 调度器线程数 |
| DB_MAX_OPEN_CONNS | 1（SQLite） | SQLite 写串行，读并发 |
| DB_MAX_IDLE_CONNS | 1 | - |
| WS_MAX_PAYLOAD | 1MB | WebSocket 消息上限 |
| WS_WRITE_WAIT | 10s | 写超时 |
| WS_PONG_WAIT | 60s | pong 等待 |
| WS_PING_PERIOD | 30s | ping 频率 |
| QUEUE_WORKER_COUNT | 2 | 发送队列 worker 数 |
| QUEUE_RETRY_BASE_DELAY | 60s | 重试基础延迟 |
| QUEUE_MAX_ATTEMPTS | 3 | 最大重试次数 |

---

## 附录：执行检查清单

### 重构启动前

- [ ] 备份现有数据库
- [ ] 备份现有 .env
- [ ] 记录现有 API 调用基线（用 Nginx 日志统计）
- [ ] 通知所有用户预计停机时间

### 每个 Bug 修复后

- [ ] 编写对应测试用例
- [ ] 测试用例覆盖正常 + 异常路径
- [ ] 在 CI 中跑通
- [ ] Code Review

### 上线前

- [ ] 所有 P0 Bug 已修复并有测试
- [ ] 所有 P1 Bug 已修复并有测试
- [ ] 测试覆盖率 ≥ 80%
- [ ] E2E 测试全通过
- [ ] 性能压测达标（参考：100 并发，P99 < 500ms）
- [ ] 安全扫描通过（gosec + npm audit）
- [ ] 文档更新（README + DEPLOY + API）
- [ ] 回滚方案已演练

### 上线后

- [ ] 监控日志 24 小时
- [ ] 监控错误率
- [ ] 监控资源占用（CPU/内存/磁盘）
- [ ] 7 天内可回滚
- [ ] 14 天后下线旧版本

---

> **方案结束**
>
> 本方案基于 MyMail 项目评估报告（2026-07-03）制定，涵盖全部 9 项 P0 + 21 项 P1 Bug 的修复策略，以及 Go 重构的完整实施路径。建议按阶段顺序推进，每个阶段完成后做一轮 Code Review 与回归测试。
