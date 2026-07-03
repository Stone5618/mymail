# MyMail 术语表与 FAQ

> **文档版本**：v1.0  
> **编写日期**：2026-07-04  
> **适用对象**：初级开发人员、新加入团队的工程师  
> **文档目的**：汇总 MyMail 项目中常见的专业术语与高频问题，所有解释均基于真实源码与现有文档。

---

## 一、术语表

### 1. 邮件协议与组件

| 术语 | 解释 | 相关源码/文档 |
|---|---|---|
| **SMTP** | Simple Mail Transfer Protocol，简单邮件传输协议。MyMail 同时用它接收外部邮件（`internal/smtp/receiver.go`）和发送外域邮件（`internal/mailsender/sender.go`）。 | [`internal/smtp/receiver.go`](../internal/smtp/receiver.go)、[`internal/mailsender/sender.go`](../internal/mailsender/sender.go) |
| **IMAP** | Internet Message Access Protocol，互联网邮件访问协议。Dovecot 通过 IMAP 向客户端（如 Outlook/Thunderbird）暴露邮件。 | [`deployments/docker/docker-compose.yml`](../deployments/docker/docker-compose.yml)、[`docs/01-项目规格文档.md`](01-项目规格文档.md) |
| **MTA** | Mail Transfer Agent，邮件传输代理。负责在服务器之间转发邮件，例如 Postfix。MyMail 出站邮件可中继到 Postfix。 | [`internal/mailsender/sender.go`](../internal/mailsender/sender.go)、[`docs/01-项目规格文档.md`](01-项目规格文档.md) |
| **MUA** | Mail User Agent，邮件用户代理。指 Web 前端、Outlook、手机邮件 App 等最终用户使用的邮件客户端。 | [`mymail-vue/src/`](../mymail-vue/src/)、[`docs/03-前端文件详解.md`](03-前端文件详解.md) |
| **Maildir** | 一种邮件文件系统存储格式，每封邮件一个文件，分 `new/`、`cur/`、`tmp/` 三个子目录。Dovecot 原生支持。 | [`internal/storage/maildir/maildir.go`](../internal/storage/maildir/maildir.go)、[`docs/02-后端文件详解/03-业务服务与数据存储.md`](02-后端文件详解/03-业务服务与数据存储.md) |
| **MIME** | Multipurpose Internet Mail Extensions，多用途互联网邮件扩展。定义邮件正文、附件的编码与类型。 | [`internal/service/mail_service.go`](../internal/service/mail_service.go)、[`tests/e2e/send_test.go`](../tests/e2e/send_test.go) |
| **DKIM** | DomainKeys Identified Mail，基于域名的邮件身份验证。通过 DNS TXT 记录中的公钥验证邮件签名。 | [`scripts/gen-dkim.sh`](../scripts/gen-dkim.sh)、[`docs/04-API参考文档.md`](04-API参考文档.md) |
| **SPF** | Sender Policy Framework，发件人策略框架。DNS TXT 记录声明哪些 IP 可以代表本域发信，MyMail 用其反垃圾评分。 | [`internal/spam/spf.go`](../internal/spam/spf.go)、[`internal/config/config.go`](../internal/config/config.go) |
| **DNSBL** | DNS-based Blackhole List，基于 DNS 的黑名单。将发件 IP 反序查询黑名单 zone（如 `zen.spamhaus.org`），命中则视为垃圾源。 | [`internal/spam/dnsbl.go`](../internal/spam/dnsbl.go)、[`internal/config/config.go`](../internal/config/config.go) |
| **Greylist** | 灰名单。首次收到某个 `(IP, sender, recipient)` 三元组时临时拒绝，合法 MTA 会在 5 分钟后重试并放行；垃圾邮件通常不重试。 | [`internal/smtp/greylist.go`](../internal/smtp/greylist.go)、[`internal/smtp/handler.go`](../internal/smtp/handler.go) |

### 2. 认证、安全与限流

| 术语 | 解释 | 相关源码/文档 |
|---|---|---|
| **JWT** | JSON Web Token。MyMail 用它标识登录用户，payload 含 `id`、`email`、`role`，签名算法 HS256。 | [`internal/crypto/jwt.go`](../internal/crypto/jwt.go)、[`internal/httpapi/middleware/auth.go`](../internal/httpapi/middleware/auth.go) |
| **bcrypt** | 自适应密码哈希算法。MyMail 用 `golang.org/x/crypto/bcrypt` 以 cost=12 哈希用户密码和 API Key。 | [`internal/crypto/password.go`](../internal/crypto/password.go)、[`tests/e2e/bench_test.go`](../tests/e2e/bench_test.go) |
| **API Key** | 外部程序调用 `/api/v1/*` 时使用的长期凭证。格式 `mk_<64 hex>`，仅创建时明文返回一次，数据库存储 bcrypt 哈希与前缀索引。 | [`internal/crypto/apikey.go`](../internal/crypto/apikey.go)、[`internal/httpapi/middleware/api_auth.go`](../internal/httpapi/middleware/api_auth.go) |
| **Rate Limit** | 限流。MyMail 在 `/api/v1/send` 对 API Key 使用固定窗口计数器限流；`SEND_RATE_LIMIT_PER_MIN` 限制用户每分钟发信数。 | [`internal/httpapi/middleware/api_rate_limit.go`](../internal/httpapi/middleware/api_rate_limit.go)、[`internal/service/mail_service.go`](../internal/service/mail_service.go) |
| **Circuit Breaker** | 熔断器。出站 SMTP 失败率 > 60% 且请求数 ≥ 10 时进入 Open 状态，快速失败以避免雪崩。 | [`internal/resilience/circuit_breaker.go`](../internal/resilience/circuit_breaker.go)、[`internal/mailsender/sender.go`](../internal/mailsender/sender.go) |
| **XSS** | Cross-Site Scripting，跨站脚本攻击。MyMail 后端用 `bluemonday` 净化 HTML，前端用 `DOMPurify` 渲染前再次过滤。 | [`internal/sanitize/html.go`](../internal/sanitize/html.go)、[`mymail-vue/src/views/MailDetailView.vue`](../mymail-vue/src/views/MailDetailView.vue) |
| **CSRF** | Cross-Site Request Forgery，跨站请求伪造。MyMail 通过 `SameSite` Cookie、CORS 白名单和 JWT 认证降低风险。 | [`internal/httpapi/middleware/security_requestid_recover.go`](../internal/httpapi/middleware/security_requestid_recover.go)、[`internal/httpapi/middleware/cors.go`](../internal/httpapi/middleware/cors.go) |
| **IDOR** | Insecure Direct Object Reference，不安全直接对象引用。MyMail 通过 `middleware/ownership.go` 校验用户只能访问自己的邮件/附件。 | [`internal/httpapi/middleware/ownership.go`](../internal/httpapi/middleware/ownership.go)、[`internal/service/mail_service.go`](../internal/service/mail_service.go) |

### 3. 可观测性与工程实践

| 术语 | 解释 | 相关源码/文档 |
|---|---|---|
| **OpenTelemetry** | 分布式追踪标准。MyMail 通过 `internal/tracing/` 初始化 tracer，将 trace_id 注入日志。 | [`internal/tracing/tracing.go`](../internal/tracing/tracing.go)、[`internal/logger/logger.go`](../internal/logger/logger.go) |
| **Prometheus** | 指标采集与暴露系统。MyMail 在 `/metrics` 暴露 HTTP、SMTP、WebSocket、熔断器等指标。 | [`internal/metrics/metrics.go`](../internal/metrics/metrics.go)、[`deployments/prometheus/prometheus.yml`](../deployments/prometheus/prometheus.yml) |
| **WebSocket** | 全双工通信协议。MyMail 用 `/ws` 端点推送新邮件到达通知，支持子协议与 URL query 两种 token 传递方式。 | [`internal/ws/hub.go`](../internal/ws/hub.go)、[`internal/ws/auth.go`](../internal/ws/auth.go) |
| **Subprotocol** | WebSocket 子协议。MyMail 约定 `auth.<token>` 格式通过 `Sec-WebSocket-Protocol` 传递 JWT，避免 token 出现在 URL。 | [`internal/ws/auth.go`](../internal/ws/auth.go)、[`mymail-vue/src/stores/ws.js`](../mymail-vue/src/stores/ws.js) |
| **Pinia** | Vue 3 官方推荐状态管理库。MyMail 前端用 Pinia 管理 `auth`、`ws` 等全局状态。 | [`mymail-vue/src/stores/`](../mymail-vue/src/stores/)、[`docs/03-前端文件详解.md`](03-前端文件详解.md) |
| **Vue Router** | Vue 3 路由库。MyMail 前端用 history 模式，后端通过 SPA fallback 返回 `index.html`。 | [`mymail-vue/src/router/index.js`](../mymail-vue/src/router/index.js)、[`internal/httpapi/router.go`](../internal/httpapi/router.go) |
| **Feature Flag** | 特性开关。MyMail 通过 `.env` 中的 `FEATURE_*` 配置动态启用/禁用功能（如反垃圾、WebSocket、API Key、规则引擎）。 | [`internal/config/config.go`](../internal/config/config.go)、[`internal/httpapi/router.go`](../internal/httpapi/router.go) |
| **ADR** | Architecture Decision Record，架构决策记录。用结构化方式记录关键选型（为什么用 Go/SQLite/Gin 等）。 | [`docs/05-架构决策记录.md`](05-架构决策记录.md) |
| **Runbook** | 运维操作手册。MyMail 在 `docs/runbooks/` 中定义 SMTP 中断、DB 锁、磁盘满、安全事件等应急流程。 | [`docs/runbooks/`](../docs/runbooks/)、[`docs/OPERATIONS.md`](OPERATIONS.md) |
| **CI/CD** | Continuous Integration / Continuous Deployment。MyMail 通过 GitHub Actions 实现 lint、test、build、security-scan 等门禁。 | [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)、[`.github/workflows/security.yml`](../.github/workflows/security.yml) |
| **Canary** | 金丝雀发布。先向小部分流量发布新版本，观察无异常后再全量。 | [`scripts/deploy.sh`](../scripts/deploy.sh)、[`docs/DEPLOY.md`](DEPLOY.md) |
| **Blue-Green** | 蓝绿部署。同时维护两套环境，切换流量实现零停机回滚。 | [`scripts/deploy.sh`](../scripts/deploy.sh)、[`scripts/rollback.sh`](../scripts/rollback.sh) |

### 4. 数据库与 Go 语言相关

| 术语 | 解释 | 相关源码/文档 |
|---|---|---|
| **DAO** | Data Access Object，数据访问对象。MyMail 为每张核心表封装一个 DAO，手写 SQL，不使用 ORM。 | [`internal/storage/dao/`](../internal/storage/dao/)、[`docs/02-后端文件详解/03-业务服务与数据存储.md`](02-后端文件详解/03-业务服务与数据存储.md) |
| **WAL** | Write-Ahead Logging。SQLite 的日志模式，支持读写并发，MyMail 启动时通过 PRAGMA 启用。 | [`internal/storage/db/db.go`](../internal/storage/db/db.go)、[`docs/05-架构决策记录.md`](05-架构决策记录.md) |
| **Context** | Go 的 `context.Context`，用于传递截止时间、取消信号和请求级元数据（如 request_id）。 | [`internal/httpapi/middleware/context.go`](../internal/httpapi/middleware/context.go)、[`internal/server/server.go`](../internal/server/server.go) |
| **Goroutine** | Go 轻量级线程。MyMail 的 WebSocket Hub、QueueWorker、SMTP Receiver 均使用 goroutine 并发处理。 | [`internal/ws/hub.go`](../internal/ws/hub.go)、[`internal/mailsender/queue.go`](../internal/mailsender/queue.go) |
| **表驱动测试** | Table-Driven Test。Go 推荐的测试写法：把输入/预期输出放在切片里循环断言。 | [`internal/smtp/greylist_test.go`](../internal/smtp/greylist_test.go)、[`docs/07-测试指南.md`](07-测试指南.md) |

---

## 二、常见问题（FAQ）

### 1. 为什么登录接口 QPS 只有 4？

**答案**：登录接口需要执行 `bcrypt.CompareHashAndPassword` 校验密码。项目使用 cost=12（`internal/crypto/password.go`），单次哈希比较约 200ms+，因此单 goroutine 下 QPS 约为 4–5。这是安全性与性能的权衡。

**相关代码/文档**：
- [`internal/crypto/password.go`](../internal/crypto/password.go)
- [`tests/e2e/bench_test.go`](../tests/e2e/bench_test.go)
- [`docs/05-架构决策记录.md`](05-架构决策记录.md)

---

### 2. 为什么第一次发邮件被灰名单拒绝？

**答案**：灰名单会针对首次出现的 `(IP, sender, recipient)` 三元组返回 `450 4.7.1 greylisted`，要求发件方 MTA 在 `GREYLIST_DELAY_MS`（默认 5 分钟）后重试。合法 MTA 会重试，垃圾邮件大多不会。测试环境可设置 `FEATURE_GREYLIST=false` 关闭。

**相关代码/文档**：
- [`internal/smtp/greylist.go`](../internal/smtp/greylist.go)
- [`internal/config/config.go`](../internal/config/config.go) 第 177–179 行
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 4.2 节

---

### 3. API Key 为什么只显示一次？

**答案**：API Key 明文仅在创建时通过 `crypto.GenerateAPIKey()` 生成一次，数据库存储的是 bcrypt 哈希和前缀。这是为了防止 Key 泄露后攻击者直接拿到原始凭证。如果丢失，只能删除旧 Key 重新创建。

**相关代码/文档**：
- [`internal/crypto/apikey.go`](../internal/crypto/apikey.go)
- [`internal/service/apikey_service.go`](../internal/service/apikey_service.go)
- [`internal/storage/dao/api_key.go`](../internal/storage/dao/api_key.go)

---

### 4. 如何关闭反垃圾过滤？

**答案**：在 `.env` 中设置 `FEATURE_SPAM_FILTER=false`。关闭后 SMTP 接收器不再执行 SPF、DNSBL 和评分。灰名单可单独通过 `FEATURE_GREYLIST=false` 关闭。

**相关代码/文档**：
- [`internal/config/config.go`](../internal/config/config.go) 第 230–238 行
- [`internal/smtp/handler.go`](../internal/smtp/handler.go)
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 4.3 节

---

### 5. 如何查看审计日志？

**答案**：审计日志双写：
1. SQLite `audit_log` 表，可直接用 SQL 查询；
2. JSONL 文件，默认路径由配置 `AUDIT_LOG_PATH` 决定。

示例查询：
```sql
SELECT timestamp, actor_type, action, resource_type, result, detail
FROM audit_log
ORDER BY timestamp DESC
LIMIT 50;
```

**相关代码/文档**：
- [`internal/audit/audit.go`](../internal/audit/audit.go)
- [`internal/audit/dao.go`](../internal/audit/dao.go)
- [`docs/OPERATIONS.md`](OPERATIONS.md)

---

### 6. 前端刷新 404 怎么办？

**答案**：生产环境需要先有前端构建产物（`mymail-vue/dist/`），并且 Go 后端已通过 `//go:embed` 打包。开发环境 `dist/` 只含 `.gitkeep`，会返回 JSON 404。解决步骤：
1. 在 `mymail-vue/` 执行 `npm run build`；
2. 重新构建/启动 Go 二进制，让新 `index.html` 被嵌入；
3. 确保后端 `registerSPARoutes` 对非 API 路径 fallback 到 `index.html`。

**相关代码/文档**：
- [`internal/httpapi/router.go`](../internal/httpapi/router.go) 第 101–130 行
- [`web/embed.go`](../web/embed.go)
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 6.3 节

---

### 7. 如何添加一个新的管理员账号？

**答案**：首个注册的用户自动成为 `admin`（`internal/service/auth_service.go`）。后续可通过管理员后台将普通用户角色改为 `admin`，或直接在数据库 `users` 表修改 `role='admin'`。

**相关代码/文档**：
- [`internal/service/auth_service.go`](../internal/service/auth_service.go)
- [`internal/httpapi/handler/admin.go`](../internal/httpapi/handler/admin.go)

---

### 8. 外域邮件发送全部失败，日志显示 `circuit breaker is open`，怎么办？

**答案**：说明最近 60 秒内出站 SMTP 失败率超过 60%，熔断器进入 Open 状态。处理步骤：
1. 检查下游 SMTP（Postfix/第三方中继）是否可用；
2. 等待 30 秒（`SMTP_CIRCUIT_BREAKER_TIMEOUT_MS=30000`）后自动进入 Half-Open；
3. 查看 `/metrics` 中的 `smtp_circuit_breaker_state`。

**相关代码/文档**：
- [`internal/resilience/circuit_breaker.go`](../internal/resilience/circuit_breaker.go)
- [`internal/mailsender/sender.go`](../internal/mailsender/sender.go)
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 3.2 节

---

### 9. 数据库出现 `database is locked` 怎么办？

**答案**：SQLite 写操作串行，多个并发写会触发锁等待。排查方向：
1. 检查是否有长时间事务未提交；
2. 确认 `busy_timeout` 是否足够；
3. 关闭其他占用 `.db` 文件的客户端（如 DBeaver）；
4. 高并发场景需考虑迁移到 PostgreSQL/MySQL。

**相关代码/文档**：
- [`internal/storage/db/db.go`](../internal/storage/db/db.go) 第 45–71 行
- [`docs/runbooks/RB-002-DB-Lock.md`](runbooks/RB-002-DB-Lock.md)

---

### 10. 如何配置 CORS 让前端开发环境正常访问？

**答案**：在 `.env` 中添加前端地址：
```dotenv
CORS_ALLOWED_ORIGINS=http://localhost:5173
```
生产环境必须配置白名单，否则 `config.Validate()` 会失败。

**相关代码/文档**：
- [`internal/httpapi/middleware/cors.go`](../internal/httpapi/middleware/cors.go)
- [`internal/config/config.go`](../internal/config/config.go) 第 265–272 行
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 6.1 节

---

### 11. JWT_SECRET 有什么要求？

**答案**：`JWT_SECRET` 必须：
- 长度 ≥ 32 字符；
- 不能是 `change-me-*`、`change_me`、`changeme` 等占位符；
- 生产环境建议用 `openssl rand -hex 32` 生成 64 字符十六进制串。

**相关代码/文档**：
- [`internal/config/config.go`](../internal/config/config.go) 第 241–257 行
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 1.1 节

---

### 12. WebSocket 连接立即返回 401，如何排查？

**答案**：WebSocket 认证优先从 `Sec-WebSocket-Protocol: auth.<token>` 读取，其次从 `/ws?token=<token>` 读取。请检查：
1. token 是否已过期；
2. 用户是否被禁用（`is_active=0`）；
3. 子协议格式是否为 `auth.` 前缀。

**相关代码/文档**：
- [`internal/ws/auth.go`](../internal/ws/auth.go)
- [`internal/ws/client.go`](../internal/ws/client.go)
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 2.4 节

---

### 13. 如何限制某个 API Key 的发信频率？

**答案**：创建 API Key 时可指定 `rate_limit`（每分钟请求数，0 表示不限流）。中间件 `APIKeyRateLimiter` 使用固定窗口计数器，超过限制返回 429。

**相关代码/文档**：
- [`internal/httpapi/middleware/api_rate_limit.go`](../internal/httpapi/middleware/api_rate_limit.go)
- [`internal/httpapi/handler/apikey.go`](../internal/httpapi/handler/apikey.go)

---

### 14. 项目如何运行测试？

**答案**：进入 `mymail-go/` 目录执行：
```bash
make test      # 单元测试 + 覆盖率
make test-e2e  # E2E 测试（需 -tags=e2e）
make bench     # 基准测试
```
CI 要求总覆盖率 ≥ 80%，且通过 `-race` 竞态检测。

**相关代码/文档**：
- [`Makefile`](../Makefile)
- [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)
- [`docs/07-测试指南.md`](07-测试指南.md)

---

### 15. 如何开启或关闭某个功能模块？

**答案**：通过 `.env` 中的 `FEATURE_*` 开关：
```dotenv
FEATURE_SPAM_FILTER=true
FEATURE_GREYLIST=true
FEATURE_WS_NOTIFY=true
FEATURE_API_KEY=true
FEATURE_RULES=true
FEATURE_AUDIT=true
FEATURE_REGISTRATION=true
```
`router.go` 会根据开关决定是否注册对应路由组。

**相关代码/文档**：
- [`internal/config/config.go`](../internal/config/config.go) 第 113–120 行、第 230–238 行
- [`internal/httpapi/router.go`](../internal/httpapi/router.go) 第 76–99 行

---

### 16. 附件大小上限在哪里配置？

**答案**：通过 `.env` 中的 `MAX_ATTACHMENT_SIZE` 配置，默认 25MB（26214400 字节）。上传时后端会校验单文件大小，前端 `UploadZone.vue` 也会做提示。

**相关代码/文档**：
- [`internal/config/config.go`](../internal/config/config.go) 第 165 行
- [`internal/service/mail_service.go`](../internal/service/mail_service.go)
- [`mymail-vue/src/components/UploadZone.vue`](../mymail-vue/src/components/UploadZone.vue)

---

### 17. 为什么 SPF 失败只标记可疑，DNSBL 命中却直接拒绝？

**答案**：反垃圾评分规则中，SPF fail 加 5 分（达到 `SPAM_SUSPICIOUS_THRESHOLD` 时标记），DNSBL listed 加 10 分（达到 `SPAM_THRESHOLD` 时拒绝）。该策略基于真实源码中的评分映射。

**相关代码/文档**：
- [`internal/spam/spf.go`](../internal/spam/spf.go)
- [`internal/spam/dnsbl.go`](../internal/spam/dnsbl.go)
- [`internal/smtp/handler.go`](../internal/smtp/handler.go)
- [`docs/09-故障排查指南.md`](09-故障排查指南.md) 4.3 节

---

### 18. 如何备份 MyMail 数据？

**答案**：运行 `scripts/backup.sh`，它会：
1. 备份 `data/` 目录；
2. 排除 `.env`；
3. 使用 AES-256-CBC PBKDF2 加密；
4. 生成 SHA256 校验文件。

**相关代码/文档**：
- [`scripts/backup.sh`](../scripts/backup.sh)
- [`docs/OPERATIONS.md`](OPERATIONS.md)

---

### 19. 如何从零部署 MyMail？

**答案**：参考 `docs/DEPLOY.md`：
1. 准备 `.env`（复制 `.env.example` 并替换密钥、域名）；
2. 执行 `scripts/setup.sh` 创建 vmail 用户和目录；
3. 运行 `make docker-build` 构建镜像；
4. 执行 `make docker-up` 启动 4 服务（app/dovecot/postfix/nginx）。

**相关代码/文档**：
- [`docs/DEPLOY.md`](DEPLOY.md)
- [`scripts/setup.sh`](../scripts/setup.sh)
- [`deployments/docker/docker-compose.yml`](../deployments/docker/docker-compose.yml)

---

### 20. 升级 Node.js 旧版本数据需要注意什么？

**答案**：参考 `docs/MIGRATION.md` 和 `scripts/migrate-data.go`，迁移脚本会：
1. 新增列（如 `body_html_raw`、`password_hash`）；
2. 将旧邮件 HTML 经过 `bluemonday` 净化；
3. 邮箱地址小写化；
4. 重命名 `spam_log` 列；
5. 创建新表（`audit_log`、`greylist`、`mail_queue`）；
6. 旧 API Key 标记为 inactive。

**相关代码/文档**：
- [`docs/MIGRATION.md`](MIGRATION.md)
- [`scripts/migrate-data.go`](../scripts/migrate-data.go)
- [`scripts/rollback-migrate.go`](../scripts/rollback-migrate.go)

---

## 三、速查索引

| 我想了解 | 先看哪里 |
|---|---|
| 项目整体架构 | [`docs/01-项目规格文档.md`](01-项目规格文档.md) |
| 后端文件详解 | [`docs/02-后端文件详解/`](02-后端文件详解/) |
| 前端文件详解 | [`docs/03-前端文件详解.md`](03-前端文件详解.md) |
| API 接口列表 | [`docs/04-API参考文档.md`](04-API参考文档.md) |
| 测试方法 | [`docs/07-测试指南.md`](07-测试指南.md) |
| 部署操作 | [`docs/DEPLOY.md`](DEPLOY.md)、[`docs/OPERATIONS.md`](OPERATIONS.md) |
| 故障排查 | [`docs/09-故障排查指南.md`](09-故障排查指南.md)、[`docs/runbooks/`](runbooks/) |
| 开发规范 | [`docs/11-贡献指南.md`](11-贡献指南.md) |
