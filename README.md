# MyMail

自托管邮件平台，Go 后端 + Vue 3 前端，Docker 一键部署。支持 SMTP 收发、IMAP 访问、反垃圾邮件、多用户管理、API 发信和实时通知。

## 技术栈

### 后端
- **语言/框架**：Go 1.25 + Gin
- **数据库**：SQLite（modernc.org/sqlite，纯 Go 实现，无 CGO 依赖）
- **认证**：JWT（golang-jwt/v5）+ bcrypt 密码哈希
- **邮件协议**：go-smtp（接收）、go-mail（发送）、Dovecot（IMAP）
- **反垃圾**：SPF、DNSBL（Spamhaus/Spamcop/Barracuda）、灰名单、评分引擎
- **弹性机制**：熔断器（gobreaker）、指数退避重试
- **可观测性**：Prometheus 指标、OpenTelemetry 链路追踪、结构化日志（slog）
- **HTML 安全**：bluemonday 白名单过滤 + DOMPurify 前端净化

### 前端
- **框架**：Vue 3.5 + Vue Router 4.5 + Pinia 3
- **构建**：Vite 8
- **样式**：Tailwind CSS 4 + 深色/浅色主题切换
- **图标**：Heroicons
- **富文本**：Quill 2
- **安全**：DOMPurify HTML 净化

### 基础设施
- **容器**：Docker + Docker Compose（三容器架构）
- **IMAP**：Dovecot 2.3（BLF-CRYPT 密码认证）
- **MTA**：Postfix（boky/postfix 镜像）
- **反代**：Nginx / Cloudflare

## 功能特性

### 邮件收发
- SMTP 接收（端口 25）+ SMTP 发送（端口 587，STARTTLS）
- IMAP 访问（端口 993，SSL）
- Maildir 格式存储，附件独立存储
- 草稿、星标、回收站、批量操作
- MIME 解析（支持非标准编码的 From 头）

### 反垃圾邮件
- **SPF**：发件方策略框架校验（失败 → 评分 +5）
- **DNSBL**：实时黑名单查询（命中 → 评分 +10，直接拒绝）
- **灰名单**：首次延迟 5 分钟（可关闭，避免大型 MTA IP 轮换导致误杀）
- **评分引擎**：综合评分，阈值可配（可疑 ≥5 标记，垃圾 ≥10 拒绝）
- **降级模式**：查询失败时放行，保证可用性

### 用户管理
- 用户注册/登录（JWT + Remember Me）
- 头像上传（本地存储，外部邮箱使用 Cravatar）
- 存储配额管理
- 个人偏好持久化（主题、语言等）
- 修改密码时同步 Dovecot 密码哈希

### 管理后台
- 用户统计（总数、今日收发量）
- DNS 状态检测（MX、SPF、DMARC）
- 用户 CRUD + 软删除
- 全局设置管理
- 审计日志（ActorType + Result 记录）

### API 发信
- API Key 管理（创建/列表/删除，明文仅返回一次）
- `POST /api/v1/send` 外部发信端点
- Bearer Token 认证（`mk_` 前缀）
- Per-Key 固定窗口限流
- Scope 权限控制（`send`）

### 实时通知
- WebSocket 连接（JWT 认证）
- 新邮件推送
- 指数退避重连（最大 5 次后降级为轮询）
- 页面可见性感知（后台暂停，前台恢复）

### 安全特性
- **CSP**：Content-Security-Policy 白名单
- **CORS**：基于白名单的跨域控制
- **HSTS**：Strict-Transport-Security
- **X-Frame-Options**：DENY（防点击劫持）
- **X-Content-Type-Options**：nosniff
- **安全头**：Referrer-Policy、Permissions-Policy
- **gzip 压缩**：文本类资源实时压缩（421KB → 84KB）

### 规则引擎
- 用户自定义过滤规则
- ReDoS 防护（模式长度限制 + 编译超时）
- 支持动作：标记已读、移动、删除、星标

## 项目结构

```
mymail/
├── mymail-go/                    # Go 后端
│   ├── cmd/mymail/               # 程序入口
│   ├── internal/
│   │   ├── audit/                # 审计日志
│   │   ├── config/               # 配置加载（Viper）
│   │   ├── crypto/               # JWT/API Key/bcrypt
│   │   ├── httpapi/              # HTTP 路由与中间件
│   │   │   ├── handler/          # 请求处理器
│   │   │   ├── middleware/       # 认证、限流、安全头、gzip
│   │   │   └── dto/              # 数据传输对象
│   │   ├── mailsender/           # SMTP 发送 + 队列
│   │   ├── resilience/           # 熔断器 + 重试
│   │   ├── rules/                # 规则引擎
│   │   ├── sanitize/             # HTML 净化
│   │   ├── service/              # 业务逻辑层
│   │   ├── smtp/                 # SMTP 接收 + 反垃圾
│   │   ├── spam/                 # SPF/DNSBL/评分
│   │   ├── storage/              # 数据访问层（DAO + Maildir）
│   │   └── ws/                   # WebSocket Hub
│   ├── config/                   # Dovecot/Nginx 配置
│   ├── deployments/docker/       # Dockerfile + docker-compose
│   └── web/                      # 前端嵌入（//go:embed）
├── mymail-vue/                   # Vue 3 前端
│   └── src/
│       ├── api/                  # API 封装
│       ├── components/           # 基础组件
│       ├── composables/          # 组合式函数
│       ├── router/               # 路由配置
│       ├── stores/               # Pinia 状态管理
│       └── views/                # 页面视图
└── README.md
```

## API 端点

### 公开端点（无需认证）
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/healthz` | 存活探针 |
| GET | `/readyz` | 就绪探针 |
| GET | `/api/version` | 版本信息 |
| POST | `/api/auth/register` | 用户注册 |
| POST | `/api/auth/login` | 用户登录 |

### 用户端点（JWT 认证）
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/auth/me` | 当前用户信息 |
| PUT | `/api/auth/profile` | 更新资料 |
| PUT | `/api/auth/password` | 修改密码 |
| POST | `/api/auth/avatar` | 上传头像 |
| GET | `/api/mail/list` | 邮件列表 |
| GET | `/api/mail/unread-count` | 未读计数 |
| POST | `/api/mail/send` | 发送邮件 |
| POST | `/api/mail/save-draft` | 保存草稿 |
| GET | `/api/mail/:id` | 邮件详情 |
| DELETE | `/api/mail/:id` | 删除邮件 |
| PUT | `/api/mail/:id/read` | 标记已读 |
| PUT | `/api/mail/:id/star` | 切换星标 |
| GET | `/api/mail/:id/attachments/:aid/download` | 下载附件 |
| GET/POST/PUT/DELETE | `/api/rules` | 规则管理 |
| GET/POST/DELETE | `/api/auth/api-keys` | API Key 管理 |

### 管理员端点（JWT + Admin 角色）
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/stats` | 平台统计 |
| GET | `/api/admin/dns-status` | DNS 状态 |
| GET/POST/PUT/DELETE | `/api/admin/users` | 用户管理 |
| GET/PUT | `/api/admin/settings` | 全局设置 |

### 外部 API（API Key 认证）
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/send` | 外部发信（Scope: send） |

### 实时通信
| 协议 | 路径 | 说明 |
|------|------|------|
| WebSocket | `/ws?token=<JWT>` | 实时通知（新邮件推送） |

## 快速部署

### 环境要求
- Docker 24+ / Docker Compose v2
- 域名 + DNS 控制权（MX、SPF、DMARC 记录）
- 服务器开放端口：25（SMTP）、587（SMTPS）、993（IMAPS）、3000（Web）

### 1. 克隆仓库
```bash
git clone https://github.com/Stone5618/mymail.git
cd mymail
```

### 2. 配置环境变量
```bash
cd mymail-go
cp .env.example .env
# 编辑 .env，必填项：
#   DOMAIN=your-domain.com
#   MAIL_HOST=mail.your-domain.com
#   JWT_SECRET=<openssl rand -hex 32>
#   ADMIN_PASSWORD=<强密码>
```

### 3. 配置 DNS 记录
```
# MX 记录
your-domain.com.    MX    10 mail.your-domain.com.

# SPF 记录
your-domain.com.    TXT   "v=spf1 mx a -all"

# DMARC 记录
_dmarc.your-domain.com.    TXT   "v=DMARC1; p=quarantine; rua=mailto:admin@your-domain.com"
```

### 4. 构建并启动
```bash
cd deployments/docker

# 注入版本信息
export VERSION="1.0.0"
export BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
export COMMIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建并启动
docker compose build app
docker compose up -d
```

### 5. 验证部署
```bash
# 健康检查
curl http://localhost:3000/healthz

# 版本信息
curl http://localhost:3000/api/version

# 访问 Web 界面
# 浏览器打开 http://your-domain.com
```

## 配置说明

关键环境变量（完整列表见 `.env.example`）：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DOMAIN` | - | 邮件域名（必填） |
| `JWT_SECRET` | - | JWT 签名密钥，≥32 字符（必填） |
| `ADMIN_PASSWORD` | - | 管理员初始密码（必填） |
| `SMTP_PORT` | 25 | SMTP 接收端口 |
| `SPAM_THRESHOLD` | 10 | 垃圾评分拒绝阈值 |
| `SPF_ENABLED` | true | 启用 SPF 校验 |
| `DNSBL_ENABLED` | true | 启用 DNSBL 查询 |
| `FEATURE_GREYLIST` | true | 启用灰名单 |
| `FEATURE_API_KEY` | true | 启用 API Key 发信 |
| `RATE_LIMIT_MAX` | 100 | API 限流（次/窗口） |
| `SEND_RATE_LIMIT_PER_MIN` | 10 | 发信限流（封/分钟） |

## 架构设计

### 三容器架构
```
                    ┌─────────────┐
  用户 ──HTTPS──→   │   Cloudflare  │ ──→ Nginx ──→ ┌───────────────┐
                    │   (CDN/SSL)  │               │  mymail-app   │
                    └─────────────┘               │  (Go :3000)   │
                                                  └───┬───────┬───┘
                         ┌──────────────────────────┘       │
                         ▼                                  ▼
                  ┌──────────────┐                   ┌──────────────┐
                  │ mymail-postfix│                   │ mymail-dovecot│
                  │ (SMTP :25/587)│                   │ (IMAP :993)   │
                  └──────────────┘                   └──────────────┘
                         │                                  │
                         └──────────┬───────────────────────┘
                                    ▼
                           ┌────────────────┐
                           │  mymail-data   │
                           │  (Docker Volume)│
                           │  - SQLite DB    │
                           │  - Maildir      │
                           │  - Attachments  │
                           │  - Avatars      │
                           └────────────────┘
```

### 前端嵌入
前端构建产物通过 `//go:embed` 嵌入 Go 二进制，运行时由 Go 直接服务静态资源，无需额外 Web 服务器。带 hash 的资源（`/assets/*`）设置长期缓存（`immutable`），`index.html` 设置不缓存。

### 安全链路
- 所有 API 端点经过中间件链：Recover → Security → RequestID → RequestLogger → CORS → Metrics → Gzip
- 用户端点增加 `Authenticate`（JWT 校验）
- 管理员端点增加 `RequireAdmin`（角色校验）
- 外部 API 使用 `APIKeyAuth`（Bearer Token）+ `RateLimit`（限流）+ `RequireScope`（权限）

## 开发指南

### 本地开发
```bash
# 后端
cd mymail-go
go mod download
go run ./cmd/mymail

# 前端（开发服务器，代理 API 到生产环境）
cd mymail-vue
npm install
npm run dev
```

### 构建
```bash
# 前端构建
cd mymail-vue
npm run build

# 后端构建
cd mymail-go
go build -o mymail ./cmd/mymail
```

### 代码检查
```bash
# Go 静态检查
cd mymail-go
golangci-lint run

# Vue 检查
cd mymail-vue
npm run lint
```

## 许可证

私有项目，保留所有权利。
