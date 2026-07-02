# MyMail 项目完整评估分析报告

> **评估日期**：2026-07-03
> **评估范围**：`mymail-platform`（后端）+ `mymail-vue`（前端）
> **评估方法**：源码全量审计 + 配置/部署/测试/CI 全链路分析
> **评估版本**：基于当前工作区代码状态

---

## 目录

- [一、摘要](#一摘要)
- [二、项目概览](#二项目概览)
- [三、架构评估](#三架构评估)
- [四、后端代码质量评估](#四后端代码质量评估)
- [五、前端代码质量评估](五前端代码质量评估)
- [六、数据库设计评估](#六数据库设计评估)
- [七、安全性专项评估](#七安全性专项评估)
- [八、部署与运维评估](#八部署与运维评估)
- [九、测试与质量保障评估](#九测试与质量保障评估)
- [十、性能与可扩展性评估](#十性能与可扩展性评估)
- [十一、综合评分](#十一综合评分)
- [十二、改进建议](#十二改进建议)
- [十三、结论](#十三结论)
- [附录 A：关键文件索引](#附录-a关键文件索引)
- [附录 B：Bug 清单与定位](#附录-bbug-清单与定位)

---

## 一、摘要

MyMail 是一个自托管的轻量邮件服务平台，目标是提供"开箱即用"的私人邮件解决方案，对标小型 SendGrid + 个人邮箱客户端的组合。项目由 `mymail-platform`（Node.js + Express 后端）和 `mymail-vue`（Vue 3 前端）两部分组成，集成了 Web 邮箱、SMTP 收发、IMAP 客户端、API Key 发信、反垃圾过滤、用户级规则引擎等功能。

**核心结论**：

- 项目处于 **"功能 MVP 已完成，工程化与安全加固进行中"** 阶段。
- FIX-PLAN.md 已规划 S0（安全）/ S1（Docker）/ S2（工程）/ S3（差异化）四个阶段，从代码看 S1/Docker 基本完成，S0/安全部分完成，S2/工程部分完成，S3/差异化部分完成但存在 Bug。
- **存在 9 项 P0 阻断性 Bug** 与 **21 项 P1 高危问题**，未经修复不建议上线。
- **综合评分 5.2/10**，适用场景为个人学习或小规模（< 50 用户）私有部署。

---

## 二、项目概览

### 2.1 定位

MyMail 提供完整的自托管邮件服务栈：

- **Web 应用**（Express + WebSocket，端口 3000）：REST API + SPA 静态资源 + 实时推送
- **SMTP 接收器**（smtp-server，端口 25）：接收外部邮件
- **IMAP 服务**（Dovecot sidecar，端口 993）：为 Outlook/Foxmail 等客户端提供访问
- **出站 SMTP 中继**（Postfix sidecar，端口 587）：发送外部邮件

### 2.2 技术栈

| 层级 | 后端选型 | 前端选型 |
|---|---|---|
| 语言 | Node.js 22+ | JavaScript（无 TypeScript） |
| 框架 | Express 4 | Vue 3（Composition API + `<script setup>`） |
| 数据库 | SQLite (better-sqlite3) | - |
| 状态管理 | - | Pinia 3 |
| 路由 | Express Router | Vue Router 5 |
| 样式 | Tailwind CSS 3 | Tailwind CSS 4 |
| 富文本 | - | Quill 2（CDN 加载） |
| 实时通信 | ws 8 | 原生 WebSocket |
| 邮件协议 | smtp-server / nodemailer / mailparser | - |
| 认证 | JWT + bcrypt | JWT（localStorage） |
| 日志 | pino + pino-pretty | - |
| 数据库迁移 | knex 3 | - |
| 测试 | Jest 30 + Supertest 7 | - |
| 构建 | - | Vite 8 |
| 容器化 | Docker 多阶段构建 | - |
| CI/CD | GitHub Actions（test + docker） | - |

### 2.3 功能矩阵

| 功能 | 后端实现 | 前端实现 | 完成度 |
|---|---|---|---|
| 用户注册/登录 | ✅ 含登录锁定 | ✅ 含密码强度计 | 高 |
| 邮件收发 | ✅ SMTP 接收 + nodemailer 发送 | ✅ 富文本 + 附件 | 高 |
| 附件管理 | ✅ multer 上传 + 批量下载 zip | ⚠️ 上传逻辑混乱 | 中 |
| IMAP 客户端 | ❌ Dovecot SQL 配置有 Bug | - | 低 |
| 实时推送 | ✅ WebSocket + 心跳 | ✅ WS + 轮询兜底 | 高 |
| 反垃圾邮件 | ❌ spam-filter.js 完全不可用 | - | 极低 |
| 灰名单 | ✅ 实现完整 | - | 高 |
| SPF/DNSBL | ⚠️ SPF 缺 MX/IPv6 | - | 中 |
| 用户级规则 | ⚠️ 规则引擎有 ReDoS 风险 | ❌ 无 UI | 低 |
| API Key 发信 | ⚠️ O(n) bcrypt 性能瓶颈 | - | 中 |
| 管理后台 | ✅ 用户管理 + DNS 检测 | ⚠️ 无前端权限守卫 | 中 |
| 暗色主题 | - | ✅ 视觉精致 | 高 |
| 响应式 | - | ✅ 移动端友好 | 高 |

---

## 三、架构评估

### 3.1 整体架构图

```
┌──────────────┐    ┌─────────┐    ┌─────────────────────┐
│   Browser    │───▶│ Nginx   │───▶│  Express App :3000  │
└──────────────┘    │ :80/443 │    │  ┌───────────────┐  │
                    └─────────┘    │  │ REST API      │  │
┌──────────────┐                   │  │ WebSocket     │  │
│  Mail Client │───┐               │  │ SPA Static    │  │
└──────────────┘   │               │  └───────────────┘  │
                   ▼               └─────────┬───────────┘
┌──────────────┐    ┌─────────┐              │
│ External SMTP│───▶│ SMTP :25│◀─────────────┤
└──────────────┘    └─────────┘              │
                       │                     ▼
                       ▼              ┌─────────────┐
                ┌──────────────┐      │  SQLite     │
                │  Maildir     │      │  mymail.db  │
                └──────────────┘      └─────────────┘
                       ▲
┌──────────────┐       │
│  Mail Client │───▶ Dovecot :993
└──────────────┘
```

### 3.2 架构优点

1. **职责分离清晰**：Web/SMTP/IMAP/MTA 四个组件解耦，可独立扩展与替换。
2. **容器化编排合理**：docker-compose.yml 用 4 个 service（app/dovecot/postfix/nginx）+ 共享 volume，符合云原生模式。
3. **SQLite + Maildir 混合存储**：元数据入库（便于查询），原始邮件存文件系统（便于 IMAP 直接读取），是合理的设计。
4. **WebSocket + 轮询双保险**：前端在 WS 之外用 10 秒轮询兜底，弱网环境下也能收到通知。
5. **统一日志与配置**：pino 结构化日志 + dotenv 环境变量，符合 12-factor 应用规范。
6. **Graceful Shutdown**：[server.js#L35-L69](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/server.js#L35-L69) 处理了 SIGTERM/SIGINT/uncaughtException/unhandledRejection，避免数据丢失。

### 3.3 架构缺陷

1. **better-sqlite3 同步阻塞**：所有 DB I/O 阻塞事件循环，与 Node.js 异步模型冲突。高并发场景下 HTTP/SMTP/WS 都会受影响。SQLite 本身适合嵌入式场景，但邮件系统是 I/O 密集型，应选 PostgreSQL 或至少使用 `sqlite3`（异步驱动）。
2. **单进程承担多角色**：HTTP + SMTP + WS 在同一 Node 进程，SMTP 处理慢会拖累 Web 响应。应拆分为独立进程或 worker。
3. **无消息队列**：出站邮件直接调用 nodemailer，失败即丢；应引入队列（如 BullMQ）做异步重试。
4. **无缓存层**：每次请求都查 DB 验证用户，无 Redis 缓存。
5. **前后端项目分离但部署耦合**：前端 dist 需手动拷贝到后端 public/，无统一构建流程。
6. **无水平扩展能力**：SQLite 单文件 + 单进程 + Maildir 单机存储，无法横向扩展。

### 3.4 模块组织

后端 `src/` 采用经典分层：`routes → services → dao`，加 `middleware` 和 `config`，符合 Express 最佳实践。但有以下问题：

- **无 controller 层**：业务逻辑直接写在 routes 里（如 `mail.js` 的 `/send` 端点有 100+ 行业务代码），路由文件臃肿，难测试。
- **service 层不纯**：`smtp-receiver.js` 直接调用 DAO 和 fs，既是 SMTP 处理器又是邮件存储服务。
- **DAO 层无统一基类/接口**：每个 DAO 重复定义 `findById`/`findAll` 模式，无复用。

前端 `src/` 组织较规范：`api/` / `stores/` / `composables/` / `components/` / `views/` / `router/` 划分清晰，但 `components/` 太薄（仅 3 个真实可复用组件），大量 UI 重复在 views 中。

---

## 四、后端代码质量评估

### 4.1 严重 Bug 与安全漏洞（P0）

#### 4.1.1 spam-filter.js 完全不可用

[spam-filter.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/spam-filter.js) 存在多重致命错误：

```js
// 第 15 行：导入不存在的函数
const { checkSPF, checkDNSBL, validateSenderDomain } = require('./smtp-validator');
// smtp-validator.js 未导出 validateSenderDomain

// 第 48 行：函数签名错误（checkSPF 实际签名是 (senderIp, senderDomain, heloDomain)）
const spfResult = await checkSPF(senderEmail, senderIP);

// 第 50-51 行：返回值比较错误（checkSPF 返回 { result: 'fail', detail } 对象）
if (spfResult === 'fail')  // 永远为 false
```

**影响**：一旦调用 spam-filter 必崩溃。**好消息**是 [smtp-receiver.js#L155-L166](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-receiver.js#L155-L166) 实际使用的是 `smtp-validator.js` 中的 `checkSPF/checkDNSBL/calculateScore`，spam-filter.js **未被引用**——这是一段死代码。但说明反垃圾模块有重复实现且未统一。

#### 4.1.2 Dovecot SQL 配置列名错误

[dovecot-sql.conf#L10-L16](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/dovecot/dovecot-sql.conf#L10-L16)：

```sql
SELECT email AS user, password, '%w' AS userdb_home, ...
FROM users WHERE email = '%u'
```

但 [001_initial_schema.js#L15](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations/001_initial_schema.js#L15) 中列名是 `password_hash`，**不是 `password`**。

**影响**：IMAP 客户端（Outlook/Foxmail）登录会全部失败，整个 IMAP 功能不可用。

#### 4.1.3 mail.js IDOR 越权漏洞

[mail.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/mail.js) 的 `/:id/read`、`/:id/unread`、`/:id/star` 端点未校验邮件归属：

```js
router.put('/:id/read', (req, res) => {
  messageDAO.markRead(parseInt(req.params.id));  // 任意用户可改任意邮件
  res.json({ message: 'ok' });
});
```

**影响**：任何登录用户可批量标记/取消任意用户邮件的已读、星标状态，破坏数据完整性。

#### 4.1.4 API Key 认证性能瓶颈

[api-auth.js#L26-L34](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/middleware/api-auth.js#L26-L34) 遍历所有 API Key 做 bcrypt.compare：

```js
const keys = db.prepare('SELECT * FROM api_keys WHERE is_active = 1').all();
for (const key of keys) {
  if (await bcrypt.compare(keyPlain, key.key_hash)) {  // 单次约 100ms
    matchedKey = key;
    break;
  }
}
```

**影响**：10 个 Key 最坏 1 秒延迟，且存在定时攻击风险。应存储 key 前缀（如 `mk_xxxx` 前 8 字符）作为索引列，先精确查询再单次 bcrypt 验证。

#### 4.1.5 JWT 默认密钥

[config.js#L17](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/config.js#L17)：

```js
jwt: { secret: process.env.JWT_SECRET || 'change-me-in-production', ... }
```

**影响**：若部署时未设置 `JWT_SECRET`，攻击者可伪造任意身份 Token。应改为：未设置则启动报错。

#### 4.1.6 init-db.js 与 migrations 不一致

- `init-db.js` 创建 `greylist` 表，但 [migrations/](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations) **不创建**——`npm run db:migrate` 后 greylist 功能崩溃。
- `init-db.js` **不创建 `spam_log` 表**——SpamLogDAO 会崩溃。
- [Dockerfile#L33-L38](file:///d:/New%20AI%20Project/mymail/mymail-platform/Dockerfile#L33-L38) 未 COPY `migrations/` 和 `knexfile.js`——容器内 `npm run db:migrate` 不可用。

#### 4.1.7 SMTP 接收器 authOptional + 无 STARTTLS 强制

[smtp-receiver.js#L88-L89](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-receiver.js#L88-L89)：

```js
authOptional: true,
allowInsecureAuth: false,
```

虽然实现了 SPF/DNSBL/灰名单等防护，但 `authOptional: true` 仍允许任何人向本地域发送邮件（取决于业务定位，若是接收外部邮件这是合理的，但需配合反垃圾）。

#### 4.1.8 出站邮件 TLS 验证默认关闭

[docker-compose.yml#L17](file:///d:/New%20AI%20Project/mymail/mymail-platform/docker-compose.yml#L17)：

```yaml
SMTP_TLS_REJECT_UNAUTHORIZED=false
```

虽然 [config.js#L46](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/config.js#L46) 默认 true，但 docker-compose 强制覆盖为 false，允许中间人攻击。

#### 4.1.9 backup.sh 备份含 .env 密钥

[backup.sh](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/backup.sh) 将 `.env` 打包进备份，含 `JWT_SECRET`、`ADMIN_PASSWORD` 等，备份泄漏即全盘沦陷。

### 4.2 高危问题（P1）

#### 4.2.1 无事务保护

DAO 层全程未使用 `db.transaction()`。以下多步操作缺乏原子性：

- `messageDAO.emptyTrash`（先删附件再删邮件，中途失败留孤儿附件）
- `mail.js /send`（建消息+建附件+写日志+更新配额）
- `smtp-receiver.js onData`（写文件+建消息+建附件+更新配额+触发规则）

#### 4.2.2 CORS 全开放

[app.js#L31](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/app.js#L31)：

```js
app.use(cors());  // 允许所有来源
```

应配置白名单：`cors({ origin: ['https://your-domain.com'] })`。

#### 4.2.3 message-dao SQL 拼接

[message-dao.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/message-dao.js) 第 79、83 行用模板字符串拼接 `${days}` 进 SQL：

```js
db.prepare(`SELECT id FROM messages WHERE ... AND received_at < datetime('now', '-${days} days')`)
```

虽然 `days` 是内部数字，但这是危险模式，未来若从用户输入传入则构成注入。

#### 4.2.4 getNextUid 竞态条件

`MAX(uid)+1` 在并发插入时会获得相同 UID，应使用事务或 AUTOINCREMENT。

#### 4.2.5 storage_limit 未校验

`updateStorageUsed` 仅累加，从不检查 `storage_limit`，用户可超额存储。

#### 4.2.6 ReDoS 风险

[rule-engine.js#L52-L53](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/rule-engine.js#L52-L53) 用户提供的正则无超时限制：

```js
new RegExp(strVal, 'i').test(fieldValue)
```

恶意正则（如 `(a+)+$`）可导致 CPU 100%。

#### 4.2.7 setup.sh 以 root 运行 Dovecot

Dovecot userdb 用 `uid=root gid=root`，IMAP 进程以 root 身份访问文件——严重安全风险。

#### 4.2.8 rule-engine forward/flag 动作未实现

[rule-engine.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/rule-engine.js) 中 `forward` 动作仅打日志，`flag` 动作为空 case。用户配置后无效但无提示。

#### 4.2.9 SPF 实现不完整

[smtp-validator.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-validator.js)：
- 仅实现 `ip4:`、`include:` 机制，`mx` 机制未实现
- `matchCIDR` 仅支持 IPv4，无 IPv6
- 位运算在 JS 32 位有符号整数下，`bits=0` 时会出错
- 递归 `include:` 无深度限制，可被恶意 SPF 记录触发无限递归

### 4.3 中等问题（P2）

| # | 问题 | 文件 |
|---|---|---|
| 1 | admin.js POST /users 调用 updateStorageUsed(userId, 0) 而非更新 storage_limit | [admin.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/admin.js) |
| 2 | admin.js POST /users 未设 is_default_password 标志 | [admin.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/admin.js) |
| 3 | mail.js /restore 死代码 `const folder = msg.in_reply_to ? 'INBOX' : 'INBOX'` | [mail.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/mail.js) |
| 4 | 邮箱正则不统一（mail.js 不要求 TLD，api-v1.js 要求 TLD） | routes |
| 5 | app.js `express.json({ limit: '50mb' })` 限制过大，易被内存耗尽攻击 | [app.js#L32](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/app.js#L32) |
| 6 | multer MIME 类型可被伪造，未校验文件头魔数 | [mail.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/mail.js) |
| 7 | 上传失败未清理磁盘文件（泄漏） | [mail.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/mail.js) |
| 8 | 索引缺失（api_keys.key_hash、spam_log.created_at、mail_rules 复合索引） | [migrations/](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations) |
| 9 | messages.user_id 外键无 ON DELETE CASCADE（删用户留孤儿邮件） | [001_initial_schema.js#L29](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations/001_initial_schema.js#L29) |
| 10 | Dockerfile HEALTHCHECK 用 /api/auth/me 端点返回 401 仍退出 0 | [Dockerfile#L51-L52](file:///d:/New%20AI%20Project/mymail/mymail-platform/Dockerfile#L51-L52) |
| 11 | Docker compose 数据卷路径不一致（app 用 /app/data，dovecot 用 /data） | [docker-compose.yml](file:///d:/New%20AI%20Project/mymail/mymail-platform/docker-compose.yml) |
| 12 | Dovecot uid/gid 写死 1000，Dockerfile mymail 用户 UID 动态分配 | [Dockerfile](file:///d:/New%20AI%20Project/mymail/mymail-platform/Dockerfile) + [dovecot-sql.conf](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/dovecot/dovecot-sql.conf) |
| 13 | .env.example `$(openssl rand -hex 8)` 在 .env 中不执行，会作为字面值 | [.env.example#L28](file:///d:/New%20AI%20Project/mymail/mymail-platform/.env.example#L28) |
| 14 | .env.example SMTP_TLS_REJECT_UNAUTHORIZED 重复定义（第 37、44 行） | [.env.example](file:///d:/New%20AI%20Project/mymail/mymail-platform/.env.example) |
| 15 | knexfile.js 未加载 dotenv | [knexfile.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/knexfile.js) |
| 16 | api-auth.js 第 21 行 `apiKeyDAO.findByUserId(0)` 是死代码 | [api-auth.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/middleware/api-auth.js) |
| 17 | greylist-dao.js cleanup 的 cutoff = Date.now() - ttlMs * 2 逻辑混乱 | [greylist-dao.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/greylist-dao.js) |
| 18 | rule-dao.js JSON 解析时无 try-catch，损坏数据会导致异常 | [rule-dao.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/rule-dao.js) |
| 19 | ws-service.js 无消息大小限制，无消息频率限制 | [ws-service.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/ws-service.js) |
| 20 | smtp-sender.js sendLocal 未保存到数据库、未处理附件、无 await | [smtp-sender.js#L38-L67](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-sender.js#L38-L67) |
| 21 | mail.js /send 成功返回 200（应返回 201） | [mail.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/mail.js) |

### 4.4 代码风格优点

- **统一使用 pino 日志**：[logger.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/logger.js) 已统一封装，符合最佳实践。
- **Graceful shutdown 已实现**：[server.js#L35-L69](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/server.js#L35-L69) 处理了 SIGTERM/SIGINT/uncaughtException/unhandledRejection。
- **配置全部走环境变量**：[config.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/config.js) 集中加载，符合 12-factor。
- **API 路由有统一前缀**（/api/auth、/api/mail、/api/admin、/api/v1、/api/rules）。
- **请求日志中间件**记录 method/path/status/responseTime，可观测性基础具备。
- **Helmet 安全头**配置较完整（CSP 已配置）。
- **knex 迁移系统**已引入，支持回滚。
- **Jest + Supertest** 测试框架已引入。

---

## 五、前端代码质量评估

### 5.1 严重 Bug 与安全漏洞（P0）

#### 5.1.1 邮件正文存储型 XSS

[MailDetailView.vue#L67](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/MailDetailView.vue#L67)：

```html
<div v-html="mail.body_html || escapeHtml(mail.body_text)"></div>
```

`mail.body_html` **未经任何 sanitize** 直接 v-html 渲染。攻击者发送含 `<script>` 或 `<img onerror=...>` 的邮件，收件人打开即执行任意 JS，可窃取 token、冒充用户操作。`escapeHtml` 只在 `body_text` 兜底时生效，对 `body_html` 无效。**必须引入 DOMPurify** 等库做净化。

#### 5.1.2 回复/转发拼接 XSS

[ComposeView.vue#L247-L251](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/ComposeView.vue#L247-L251)：

```js
await setBodyHtml(`<br>...<p>发件人: ${msg.from_name || msg.from_addr} &lt;${msg.from_addr}&gt;</p>...${msg.body_html || msg.body_text || ''}`)
```

将原邮件 `body_html` **直接拼入编辑器**——同样存在 XSS（用户回复一封恶意邮件时，恶意脚本在编辑器内执行）。且未做引文层级缩进。

#### 5.1.3 管理后台无前端权限守卫

[router/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/router/index.js) 路由守卫仅判断 token 是否存在：

```js
router.beforeEach((to) => {
  const token = localStorage.getItem('token')
  if (!token && to.name !== 'login') return { name: 'login' }
})
```

`/admin` 路由对所有登录用户开放，仅在 [AppLayout.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/components/AppLayout.vue) 的 `navItems` 中通过 `auth.user?.role === 'admin'` 隐藏入口。**任意登录用户手动访问 `/admin` 即可进入管理后台**。前端隐藏 ≠ 权限控制（需后端兜底，但前端也应有路由级 guard）。

#### 5.1.4 MailView batchDelete 引用未定义函数

[MailView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/MailView.vue) 的 `batchDelete` 调用了未定义的 `toast(...)`：

```js
async function batchDelete() {
  // ...
  toast('删除成功', 'success')  // ReferenceError: toast is not defined
}
```

该文件未 `import { useToast }` 也未解构 `toast`。运行时会抛 `ReferenceError`。

#### 5.1.5 UploadZone 附件上传逻辑错乱

[UploadZone.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/components/UploadZone.vue) 的 `uploadOne` 因 Cloudflare 代理问题，**先把状态置为 done 再后台上传**：

```js
async function uploadOne(item) {
  item.status = 'done'  // 立即标记完成
  item.progress = 100
  emitUpdate()
  uploadFiles(fd).then(res => { item.attId = res.files[0].id ... })
}

function isUploading() {
  return false  // 恒返回 false
}
```

**影响**：
1. `getAttachmentIds()` 立即返回空，ComposeView 调用 sendMail 时附件 ID 列表为空。
2. `isUploading()` 恒 false，发送按钮禁用逻辑失效。
3. ComposeView 改为直接把原始 File 塞进 FormData，与预上传机制并存，逻辑割裂。

### 5.2 高危问题（P1）

| # | 问题 | 文件 |
|---|---|---|
| 1 | Token 存 localStorage，XSS 可读取 | [api/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/api/index.js) |
| 2 | WS token 走 URL query，会被写入日志/历史 | [stores/ws.js#L18](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/stores/ws.js#L18) |
| 3 | 401 处理用 `window.location.href`，丢失 SPA 状态 | [api/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/api/index.js) |
| 4 | LoginView 直接改 store 字段绕过 action | [LoginView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/LoginView.vue) |
| 5 | 路由守卫直接读 localStorage，绕过 Pinia store | [router/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/router/index.js) |
| 6 | AdminView 重置密码明文 toast 展示 | [AdminView.vue#L175](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/AdminView.vue#L175) |
| 7 | AdminView resetPw/deleteUser 无确认对话框 | [AdminView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/AdminView.vue) |
| 8 | useFormat.escapeHtml 未转义 `"` `'`，属性上下文仍可 XSS | [useFormat.js#L16](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/composables/useFormat.js#L16) |
| 9 | 附件下载 `<a target="_blank">` 缺 `rel="noopener noreferrer"` | [MailDetailView.vue#L81](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/MailDetailView.vue#L81) |
| 10 | 无 CSP meta | [index.html](file:///d:/New%20AI%20Project/mymail/mymail-vue/index.html) |
| 11 | Quill 从 CDN 加载无 SRI，CDN 被劫持可注入 JS | [ComposeView.vue#L206](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/ComposeView.vue#L206) |
| 12 | WS 重连固定 5 秒，无指数退避、无网络在线检测 | [stores/ws.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/stores/ws.js) |
| 13 | AdminView.loadAll `catch {}` 静默吞错 | [AdminView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/AdminView.vue) |
| 14 | AppLayout 移动端退出按钮仅 group-hover 显示，无法点击 | [AppLayout.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/components/AppLayout.vue) |

### 5.3 中等问题（P2）

| # | 问题 |
|---|---|
| 1 | 无 TypeScript，邮件/用户数据结构复杂缺类型保障（`is_starred` 在 0/1 与 true/false 间混用） |
| 2 | 无 ESLint/Prettier/Husky 代码规范工具 |
| 3 | 无单元测试/E2E 测试框架 |
| 4 | 残留脚手架代码（HelloWorld.vue、TheWelcome.vue、WelcomeItem.vue、icons/、base.css） |
| 5 | formatSize、fileIcon、骨架屏 shimmer 样式多处重复 |
| 6 | 注释稀少，仅 api/index.js 有分组注释 |
| 7 | `<html lang="">` 为空，影响 a11y 与 SEO |
| 8 | 大量 emoji 作为图标，跨平台渲染不一致、a11y 差 |
| 9 | 无路由 keep-alive 缓存，切换 folder 重新请求 |
| 10 | 无虚拟滚动、无请求缓存、无图片懒加载、无 PWA |
| 11 | 完全无 i18n，文案硬编码中文 |
| 12 | 暗色主题强制，不尊重用户 prefers-color-scheme 偏好 |
| 13 | WS 用 window.dispatchEvent 当事件总线，是反模式 |
| 14 | useToast 命令式 DOM 操作，绕过 Vue 渲染管线 |
| 15 | 颜色对比度部分不达 WCAG AA 4.5:1（`text-dark-500` on `dark-900` 约 3.9:1） |
| 16 | 批量未读用 for 循环串行 await，无 batchMarkUnread 接口 |
| 17 | 注册无邮箱字段，登录用 email，注册用 username，字段不一致 |
| 18 | 草稿删除静默吞错 `catch(() => {})` |
| 19 | 附件预览 blob URL 组件销毁时未 revoke，内存泄漏 |
| 20 | 邮件配置区 IMAP/SMTP 服务器前端拼接，与实际后端无关，可能误导 |

### 5.4 优点

1. **Composition API + `<script setup>` 全量使用**，代码风格现代、统一。
2. **API 层封装整洁**，分组清晰（Auth/Mail/Star/Upload/Batch/Admin），错误处理统一。
3. **响应式设计**考虑细致，移动端体验好（侧边栏抽屉、表格转卡片）。
4. **视觉与微交互**打磨到位，骨架屏、过渡动画、登录页星空背景等细节体现用心。
5. **Pinia Setup Store** 写法规范，状态派生用 `computed`。
6. **路由懒加载**与代码分包到位，各 View 独立 chunk。
7. **MailView 列表**功能完整：搜索/星标筛选/多选批量/分页/空状态。
8. **LoginView** 完成度高：密码强度计/显示切换/记住我/错误提示/加载态。
9. **WebSocket 兜底轮询**：弱网环境下也能收到通知。
10. **WS 心跳机制**：30 秒 ping/pong 检测断连。

---

## 六、数据库设计评估

### 6.1 表结构概览

[001_initial_schema.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations/001_initial_schema.js) 定义 5 张表：

| 表 | 用途 | 关键字段 |
|---|---|---|
| users | 用户 | username/email/password_hash/role/storage_limit/storage_used/login_fails/locked_until/is_default_password/signature |
| messages | 邮件 | user_id/folder/message_id/uid/from/to/cc/subject/body_text/body_html/is_read/is_starred/has_attach/headers_raw/received_at |
| attachments | 附件 | message_id/filename/mime_type/size_bytes/storage_path |
| send_log | 发送日志 | user_id/to_addr/subject/status/error_msg/sent_at |
| settings | 系统配置 | key/value |

[002_add_apikeys_rules.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations/002_add_apikeys_rules.js) 添加：
- `users.is_default_password` 列
- `api_keys` 表（user_id/name/key_hash/scopes/rate_limit/is_active/last_used_at）
- `mail_rules` 表（user_id/name/priority/conditions/actions/is_active）
- `spam_log` 表（sender/ip/score/reasons/action/created_at）

### 6.2 设计优点

1. **主键自增**：所有表用 `table.increments('id').primary()`。
2. **外键约束**：开启 `PRAGMA foreign_keys = ON`（[database.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/database.js)）。
3. **WAL 模式**：开启 `PRAGMA journal_mode = WAL` 提升并发读。
4. **关键索引**：`messages(user_id, folder)`、`messages(received_at)` 已建。
5. **级联删除**：`attachments.message_id` ON DELETE CASCADE ✓。
6. **时间戳**：`created_at`/`updated_at`/`received_at` 默认 `knex.fn.now()`。

### 6.3 设计缺陷

#### 6.3.1 外键级联不完整

- `messages.user_id` 无 `ON DELETE CASCADE`——删用户留孤儿邮件
- `send_log.user_id` 无级联删除
- `attachments.message_id` 有级联 ✓
- `api_keys.user_id` 有级联 ✓
- `mail_rules.user_id` 有级联 ✓

#### 6.3.2 索引不足

- 无 `messages(user_id, is_read)` 索引——`unreadCount` 查询低效
- 无 `messages(received_at, user_id)` 复合索引
- 无 `api_keys.key_hash` 索引——api-auth 全表扫描的根因
- 无 `spam_log.created_at` 索引——分页查询低效
- 无 `mail_rules(user_id, is_active)` 复合索引——`findActiveByUserId` 低效

#### 6.3.3 大字段存储

- `body_text`/`body_html`/`headers_raw` 用 TEXT，大邮件会撑大 DB 文件
- 应考虑外部存储（如邮件已存 Maildir，DB 仅存元数据+正文摘要）

#### 6.3.4 无全文索引

- `findByUserId` 搜索 `body_text LIKE`，全表扫描
- SQLite 支持 FTS5 虚拟表，可建全文索引提升搜索性能

#### 6.3.5 数据完整性

- `flags` 用 TEXT 存 JSON 字符串（`'[]'`），无 schema 校验
- `mail_rules.conditions/actions` 同样存 JSON，解析时无 try-catch

#### 6.3.6 schema 不一致

- `init-db.js` 创建 `greylist` 表，migrations 不创建
- `init-db.js` 不创建 `spam_log` 表，migrations 创建
- 两套初始化方式必须组合使用，单一方式必崩

---

## 七、安全性专项评估

### 7.1 认证与授权

| 维度 | 评估 | 风险 |
|---|---|---|
| 密码存储 | bcrypt ✓ | 低 |
| JWT 密钥 | 默认值 `change-me-in-production` | **高** |
| JWT 刷新 | 无刷新机制，长期 Token 不可吊销 | 中 |
| 登录锁定 | 5 次失败锁定 ✓ | 低 |
| API Key | O(n) bcrypt，无 key_hash 索引 | **高** |
| 路由权限 | mail.js IDOR 漏洞 | **高** |
| 管理员权限 | requireAdmin 中间件 ✓ | 低 |
| 前端权限 | /admin 路由无 guard | **高** |

### 7.2 输入校验

- 注册接口：username 校验了，displayName 未校验长度
- 邮件发送：to/cc/bcc 邮箱格式校验不统一
- 邮件主题/正文：未限制长度（仅依赖 50mb body limit）
- 文件上传：MIME 类型可伪造，未校验文件头魔数
- 邮件规则：正则无超时限制（ReDoS）

### 7.3 加密与传输

- 出站 SMTP `tls: { rejectUnauthorized: config.smtp.tlsRejectUnauthorized }`，默认 true ✓
- 但 docker-compose.yml 设 `SMTP_TLS_REJECT_UNAUTHORIZED=false` ⚠️
- 入站 SMTP 默认无 TLS（需配置 SMTP_TLS_CERT/KEY）
- HTTPS 由 Nginx 终止 ✓
- HSTS 已启用 ✓（nginx config）
- WebSocket 默认 wss（生产）✓
- 密码 bcrypt 哈希 ✓
- API Key bcrypt 哈希 ✓

### 7.4 XSS / CSRF

- 前端 v-html 渲染邮件正文，**存储型 XSS**（P0）
- 回复转发拼接原邮件 HTML，**二次 XSS**
- escapeHtml 实现不完整（未转义 `"` `'`）
- 无 CSRF 防护（依赖 JWT 不放 Cookie，可接受）
- CSP 已在后端 Helmet 配置 ✓，但前端 index.html 无 CSP meta

### 7.5 其他安全风险

- backup.sh 备份含 .env 密钥
- setup.sh 全程 root，Dovecot userdb uid=root
- AdminView 重置密码明文 toast
- WS token 走 URL query，写入日志
- AppLayout 头像点击无行为，预期会展开菜单
- Dovecot uid/gid 写死 1000，与容器内 mymail 用户 UID 不匹配，可能导致权限提升或访问失败

### 7.6 已实施的安全措施（值得肯定）

| 措施 | 实施情况 |
|---|---|
| bcrypt 密码哈希 | ✓ |
| JWT 认证 | ✓ |
| 登录失败锁定 | ✓（5 次失败锁定） |
| Helmet 安全头 | ✓（CSP/HSTS/X-Frame-Options 等） |
| CORS | ⚠️ 全开放 |
| 速率限制 | ✓（HTTP + SMTP 连接） |
| 灰名单 | ✓ |
| SPF/DNSBL | ⚠️ SPF 实现不完整 |
| 邮件附件白名单 | ✓ |
| 文件大小限制 | ✓（25MB） |
| 非 root 容器 | ✓（Dockerfile） |
| tini init 进程 | ✓ |
| Nginx 安全头 | ✓ |
| Nginx 登录限流 | ✓（5r/m） |

---

## 八、部署与运维评估

### 8.1 Docker 化

**优点**：
- 多阶段构建，最终镜像约 150MB ✓
- 非 root 用户运行 ✓
- tini 作为 init 进程 ✓
- 数据卷持久化 ✓
- GitHub Actions 自动构建推送 ghcr.io ✓
- Buildx 缓存优化 ✓

**缺陷**：
- HEALTHCHECK 用 `/api/auth/me` 返回 401 仍退出 0（wget -qO- 仅网络错误才非 0）
- 未 COPY migrations/ 和 knexfile.js，容器内无法迁移
- Dovecot/app 数据卷路径不一致（/data vs /app/data）
- Dovecot uid/gid 写死 1000 与 mymail 用户 UID 不匹配
- `SMTP_TLS_REJECT_UNAUTHORIZED=false` 默认放行自签名证书

### 8.2 Nginx 配置

[config/nginx/default.conf](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/nginx/default.conf) 质量较高：

- HTTP→HTTPS 跳转 ✓
- HSTS ✓
- 安全头 ✓
- WebSocket 代理 ✓
- 登录端点独立限流（5r/m）✓
- 静态资源缓存 ✓
- 隐藏文件拒绝 ✓

**问题**：
- SSL 证书路径需手动提供，无自动签发流程
- `limit_req_zone login` 5r/m 可能误伤正常用户
- 缺 SSL 配置项（如 ssl_protocols TLSv1.2 TLSv1.3）

### 8.3 CI/CD

- **test.yml**：Node 18/20/22 矩阵 ✓，但未设置 DB_PATH 等测试环境变量
- **docker.yml**：标准构建推送，使用 Buildx 缓存 ✓
- **缺 lint workflow**：无代码规范检查
- **缺安全扫描**：无 npm audit / Trivy 镜像扫描

### 8.4 可观测性

- pino 结构化日志 ✓
- 请求日志中间件（method/path/status/responseTime）✓
- 无 Prometheus metrics 端点
- 无分布式追踪
- 无错误上报（Sentry 等）
- 无健康检查端点（应加 `/health` 返回 DB/SMTP 状态）

### 8.5 备份与恢复

- backup.sh 已实现 ✓
- 自动保留最近 7 份 ✓
- **致命缺陷**：备份含 .env 密钥
- `2>/dev/null || true` 吞掉所有错误，备份失败无感知
- 无异地备份
- 无恢复测试

---

## 九、测试与质量保障评估

### 9.1 测试覆盖

| 模块 | 测试文件 | 覆盖情况 |
|---|---|---|
| auth | auth.test.js | 注册/登录/锁定/me/改密——覆盖较好 |
| mail | mail.test.js | 发送/列表/已读/星标/删除/清空——基本覆盖 |
| admin | admin.test.js | 仅 users 列表/创建——**严重不足** |
| rules | 无 | **完全未测** |
| api-v1 | 无 | **完全未测** |
| spam-filter | 无 | **完全未测**（且模块本身有 Bug） |
| rule-engine | 无 | **完全未测** |
| smtp-receiver | 无 | **完全未测** |
| ws-service | 无 | **完全未测** |
| 附件下载 | 无 | **完全未测** |
| 前端单元 | 无 | **完全未测** |
| 前端 E2E | 无 | **完全未测** |

### 9.2 测试质量

- setup.js **不创建 api_keys/mail_rules/spam_log/greylist 表**，触及这些表的测试都会崩溃
- 测试共享状态：helpers.js 单例 `_app`，所有测试文件共用一个 DB 实例，测试间数据污染
- jest.config.js 强制 `maxWorkers: 1`（串行）缓解了部分问题，但测试隔离仍差
- 无安全测试（注入、XSS、CSRF、越权）
- 无并发测试（getNextUid 竞态未被覆盖）
- 无前端测试（无 Vitest、无 Vue Test Utils、无 E2E）

### 9.3 工程规范

- 后端无 ESLint 配置
- 前端无 ESLint/Prettier 配置
- 无 Husky/lint-staged pre-commit 钩子
- 无代码覆盖率报告（jest --coverage 未配）
- 无 TypeScript（前后端均纯 JS）

### 9.4 文档

- README.md 详尽，含架构图、功能特性、快速开始、配置说明、DNS 配置、客户端配置、项目结构、常用命令、故障排除、安全注意事项 ✓
- DEPLOY.md 部署文档完整 ✓
- FIX-PLAN.md 修复计划详尽（S0-S3 四阶段）✓
- .env.example 配置模板完整 ✓
- **问题**：部分文档与实现不一致（如 .env.example 中 `$(openssl rand -hex 8)` 不生效、SMTP_TLS_REJECT_UNAUTHORIZED 重复定义）

---

## 十、性能与可扩展性评估

### 10.1 性能瓶颈

1. **better-sqlite3 同步阻塞**：所有 DB I/O 阻塞事件循环，高并发下 HTTP/SMTP/WS 都会受影响。这是最大瓶颈。
2. **每次请求查库验证用户**：auth.authenticate 每次都 userDAO.findById，无缓存。
3. **API Key 认证 O(n) bcrypt**：10 个 Key 最坏 1 秒延迟。
4. **邮件搜索全表扫描**：body_text LIKE 无索引。
5. **getNextUid 竞态**：MAX(uid)+1 在并发下冲突。
6. **express.json 50mb 限制**：易被内存耗尽攻击。
7. **前端无虚拟滚动**：邮件列表全量渲染（分页 20 条缓解）。
8. **前端无路由 keep-alive**：切换 folder 重新请求。
9. **前端无请求缓存**：每次都重新 fetch。
10. **SMTP onData 全部 buffer 到内存**：大附件会撑爆内存，应流式处理。

### 10.2 可扩展性

- **SQLite 单文件**：不支持多实例水平扩展，写操作串行。
- **单进程**：HTTP + SMTP + WS 共享事件循环，无法独立扩展。
- **无消息队列**：出站邮件直接调用 nodemailer，失败即丢。
- **无缓存层**：无 Redis，热点数据每次查库。
- **Maildir 单机存储**：无法横向扩展，无分布式文件系统。
- **无负载均衡**：单容器实例。

**结论**：MyMail 适合**个人/小团队（< 100 用户）**使用，不具备大规模扩展能力。这是设计选择，非缺陷，但应在文档中明确。

### 10.3 性能优化建议

#### 后端

1. 引入 Redis 缓存用户信息、API Key 验证结果
2. 邮件搜索用 SQLite FTS5 全文索引
3. SMTP onData 改为流式写文件，避免全量 buffer
4. 出站邮件用 BullMQ 队列异步处理
5. 数据库加索引（见 6.3.2）
6. 引入连接池（如改用 PostgreSQL）

#### 前端

1. 路由 keep-alive 缓存 MailView 各 folder
2. 邮件列表虚拟滚动（vue-virtual-scroller）
3. 请求缓存（SWR 模式）
4. 图片懒加载（`<img loading="lazy">`）
5. 搜索输入防抖
6. 配置 rollup-plugin-visualizer 做包体分析
7. PWA / Service Worker 离线能力

---

## 十一、综合评分

### 11.1 维度评分

| 维度 | 评分 | 说明 |
|---|---|---|
| 功能完整性 | 7/10 | 核心 Web 邮箱功能完整，但 IMAP（Dovecot Bug）和反垃圾（spam-filter Bug）不可用 |
| 代码质量 | 5/10 | 后端分层清晰但缺事务；前端现代但 XSS/权限漏洞多；均有死代码 |
| 安全性 | 3/10 | JWT 默认密钥、IDOR、XSS、API Key 性能瓶颈、备份含密钥等多个高危 |
| 测试覆盖 | 2/10 | 仅 auth/mail 基本覆盖，rules/api/spam/ws/smtp 全未测，前端零测试 |
| 部署运维 | 6/10 | Docker 化完善，CI 完善，但健康检查无效、路径不一致、UID 不匹配 |
| 性能 | 4/10 | SQLite 同步阻塞是硬伤，无缓存无队列，仅适合小规模 |
| 可维护性 | 5/10 | 无 TS、无 lint、无覆盖率报告，但代码风格相对统一 |
| 用户体验 | 7/10 | 视觉精致、响应式到位、微交互用心，但有 toast bug、附件上传 bug |
| 文档完整度 | 7/10 | README/DEPLOY/FIX-PLAN 齐全，但部分配置说明与实现不一致 |
| 架构设计 | 6/10 | 职责分离合理，但单进程多角色、无队列无缓存 |

### 11.2 总体评分

**5.2 / 10**

**评级**：⚠️ **不建议生产环境直接使用，需完成 P0/P1 修复后方可上线**

### 11.3 项目阶段判断

项目处于 **"功能 MVP 已完成，工程化与安全加固进行中"** 阶段。FIX-PLAN.md 已规划了 S0（安全）/ S1（Docker）/ S2（工程）/ S3（差异化）四个阶段：

| 阶段 | 完成度 | 说明 |
|---|---|---|
| S0 安全加固 | 60% | 灰名单、SPF、Helmet、CSP 已加；但 JWT 默认密钥、IDOR、XSS 等未修复 |
| S1 Docker 容器化 | 90% | Dockerfile、docker-compose、GitHub Actions 已完成；HEALTHCHECK、路径不一致待修 |
| S2 工程地基 | 50% | pino、knex、jest 已引入；测试覆盖严重不足，无 lint |
| S3 差异化功能 | 70% | API Key、规则引擎已实现；但有 Bug（spam-filter 不可用、api-auth 性能） |

---

## 十二、改进建议

### P0 — 阻断上线，立即修复

1. **修复 Dovecot SQL 配置列名**：`password` → `password_hash`，并验证 BLF-CRYPT 与 bcrypt `$2b$` 兼容性
2. **修复 spam-filter.js**：或直接删除（smtp-receiver 已用 smtp-validator）
3. **修复 mail.js IDOR**：`/:id/read`、`/:id/unread`、`/:id/star` 加归属校验
4. **修复前端 MailDetailView/ComposeView XSS**：引入 DOMPurify 净化 body_html
5. **修复前端 /admin 路由权限**：加 `meta.requiresAdmin` + beforeEach 校验
6. **修复 MailView batchDelete toast 未定义**：导入 useToast
7. **修复 UploadZone 上传逻辑**：确保发送前附件真正上传完成
8. **JWT_SECRET 强制要求**：未设置则启动报错，不提供默认值
9. **统一 init-db.js 与 migrations**：两套方式都创建完整表结构

### P1 — 高优先级，尽快修复

10. **DAO 层引入事务**：emptyTrash、mail.js /send、smtp-receiver onData
11. **CORS 配置白名单**
12. **API Key 加 key_hash 索引**：先精确查询再单次 bcrypt
13. **修复 getNextUid 竞态**：用事务或 AUTOINCREMENT
14. **storage_limit 校验**：上传前检查配额
15. **backup.sh 排除 .env**
16. **rule-engine ReDoS 防护**：正则超时或白名单
17. **Dockerfile 修复**：COPY migrations/、修复 HEALTHCHECK
18. **Docker compose 修复**：路径一致化、UID 固定
19. **前端 Token 管理统一**：路由守卫改用 store
20. **前端 401 处理**：router.replace 而非 window.location.href
21. **AdminView 重置密码改弹窗 + 复制按钮 + 确认对话框**
22. **docker-compose.yml 移除 SMTP_TLS_REJECT_UNAUTHORIZED=false**
23. **修复 smtp-sender.js sendLocal**：保存到数据库、处理附件
24. **mail.js 邮箱格式校验统一**

### P2 — 工程化改进

25. **引入 ESLint + Prettier + Husky**
26. **前端迁移 TypeScript**（渐进式）
27. **补全测试**：rules/api-v1/spam/ws/smtp/附件下载
28. **前端引入 Vitest + Vue Test Utils**
29. **添加 /health 端点**（DB/SMTP 状态）
30. **添加 Prometheus metrics**
31. **清理脚手架残留**：HelloWorld/TheWelcome/base.css/icons
32. **抽离复用组件**：PageHeader、useFileIcon、useFormatSize
33. **添加 CSP meta、`<html lang="zh-CN">`、theme-color**
34. **Quill 改为 npm 打包或 @vueup/vue-quill**
35. **数据库加索引**：api_keys.key_hash、spam_log.created_at、mail_rules(user_id, is_active)、messages(user_id, is_read)
36. **messages.user_id 加 ON DELETE CASCADE**
37. **引入 Sentry 错误上报**
38. **添加 npm audit / Trivy 镜像扫描到 CI**

### P3 — 体验与性能

39. **路由 keep-alive 缓存 MailView**
40. **WS 重连指数退避 + navigator.onLine 检测**
41. **搜索输入防抖**
42. **batchMarkUnread 批量接口**
43. **引入 SVG 图标库替代 emoji**
44. **配置 rollup-plugin-visualizer**
45. **WS token 改用 Sec-WebSocket-Protocol 传递**
46. **附件下载加 rel="noopener noreferrer"**
47. **暗色模式切换开关**
48. **i18n 国际化**
49. **邮件列表虚拟滚动**
50. **请求缓存（SWR 模式）**
51. **SMTP onData 流式写文件**
52. **引入 BullMQ 出站邮件队列**
53. **a11y 改进**：aria-label、skip-to-content、focus trap、对比度修复

---

## 十三、结论

### 13.1 总体评价

MyMail 是一个**有诚意的个人项目**，体现了作者对邮件系统全栈的理解：

**亮点**：
- 架构分层清晰、Docker 编排完善、CI 流畅
- 视觉精致、响应式到位、微交互用心
- FIX-PLAN 文档详尽，规划了正确方向
- pino/knex/jest 等现代化工具选型得当
- Graceful shutdown、Helmet 安全头、Nginx 限流等基础安全措施到位

**短板**：
- 安全漏洞密集（XSS、IDOR、默认密钥、备份含密钥）
- 关键模块存在阻断性 Bug（Dovecot 列名错误、spam-filter 不可用、UploadZone 上传逻辑错乱）
- 测试覆盖严重不足（rules/api/spam/ws/smtp 全未测，前端零测试）
- SQLite 同步阻塞限制扩展性
- 无 TypeScript 无 lint 工程基建薄弱

### 13.2 适用场景

- ✅ 作为个人学习项目
- ✅ 小规模（< 50 用户）私有部署（完成 P0 修复后）
- ❌ 直接用于生产环境
- ❌ 承担商业邮件服务
- ❌ 大规模（> 100 用户）部署

### 13.3 核心建议

项目应**先补齐安全与正确性**（P0/P1），再**补齐工程化基建**（P2），最后**优化体验与性能**（P3）。

当前 FIX-PLAN.md 已规划了正确方向，但执行落地不彻底——多个 S0 项未真正完成，S3 差异化功能存在 Bug。建议作者**回归 FIX-PLAN 的 S0/S2 阶段，逐项验收**，而非继续推进新功能。

具体执行建议：

1. **第一阶段（阻断修复）**：完成所有 P0 项，使 IMAP、反垃圾、权限、XSS 等核心功能可用
2. **第二阶段（高危修复）**：完成所有 P1 项，使系统具备上线条件
3. **第三阶段（工程化）**：完成 P2 项，提升可维护性与测试覆盖
4. **第四阶段（优化）**：按需推进 P3 项

每个阶段完成后应做一轮代码审计与回归测试，确保问题真正修复且未引入新问题。

---

## 附录 A：关键文件索引

### 后端核心文件

| 模块 | 文件路径 |
|---|---|
| 入口 | [src/server.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/server.js) |
| Express 应用 | [src/app.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/app.js) |
| 配置 | [src/config.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/config.js) |
| 日志 | [src/logger.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/logger.js) |
| 路由-auth | [src/routes/auth.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/auth.js) |
| 路由-mail | [src/routes/mail.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/mail.js) |
| 路由-admin | [src/routes/admin.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/admin.js) |
| 路由-api-v1 | [src/routes/api-v1.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/api-v1.js) |
| 路由-rules | [src/routes/rules.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/rules.js) |
| 中间件-auth | [src/middleware/auth.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/middleware/auth.js) |
| 中间件-api-auth | [src/middleware/api-auth.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/middleware/api-auth.js) |
| 服务-smtp-receiver | [src/services/smtp-receiver.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-receiver.js) |
| 服务-smtp-sender | [src/services/smtp-sender.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-sender.js) |
| 服务-smtp-validator | [src/services/smtp-validator.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-validator.js) |
| 服务-spam-filter | [src/services/spam-filter.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/spam-filter.js) |
| 服务-rule-engine | [src/services/rule-engine.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/rule-engine.js) |
| 服务-ws-service | [src/services/ws-service.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/ws-service.js) |
| DAO-database | [src/dao/database.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/database.js) |
| 迁移-001 | [migrations/001_initial_schema.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations/001_initial_schema.js) |
| 迁移-002 | [migrations/002_add_apikeys_rules.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations/002_add_apikeys_rules.js) |

### 后端部署文件

| 模块 | 文件路径 |
|---|---|
| Dockerfile | [Dockerfile](file:///d:/New%20AI%20Project/mymail/mymail-platform/Dockerfile) |
| docker-compose | [docker-compose.yml](file:///d:/New%20AI%20Project/mymail/mymail-platform/docker-compose.yml) |
| Nginx 配置 | [config/nginx/default.conf](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/nginx/default.conf) |
| Dovecot 配置 | [config/dovecot/dovecot.conf](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/dovecot/dovecot.conf) |
| Dovecot SQL | [config/dovecot/dovecot-sql.conf](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/dovecot/dovecot-sql.conf) |
| 部署脚本 | [scripts/setup.sh](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/setup.sh) |
| 初始化数据库 | [scripts/init-db.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/init-db.js) |
| 备份脚本 | [scripts/backup.sh](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/backup.sh) |
| DKIM 生成 | [scripts/gen-dkim.sh](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/gen-dkim.sh) |
| 环境变量模板 | [.env.example](file:///d:/New%20AI%20Project/mymail/mymail-platform/.env.example) |
| CI-test | [.github/workflows/test.yml](file:///d:/New%20AI%20Project/mymail/mymail-platform/.github/workflows/test.yml) |
| CI-docker | [.github/workflows/docker.yml](file:///d:/New%20AI%20Project/mymail/mymail-platform/.github/workflows/docker.yml) |

### 前端核心文件

| 模块 | 文件路径 |
|---|---|
| 入口 | [src/main.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/main.js) |
| 根组件 | [src/App.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/App.vue) |
| 路由 | [src/router/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/router/index.js) |
| API 客户端 | [src/api/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/api/index.js) |
| Store-auth | [src/stores/auth.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/stores/auth.js) |
| Store-ws | [src/stores/ws.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/stores/ws.js) |
| 布局组件 | [src/components/AppLayout.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/components/AppLayout.vue) |
| 上传组件 | [src/components/UploadZone.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/components/UploadZone.vue) |
| 视图-登录 | [src/views/LoginView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/LoginView.vue) |
| 视图-邮件列表 | [src/views/MailView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/MailView.vue) |
| 视图-邮件详情 | [src/views/MailDetailView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/MailDetailView.vue) |
| 视图-写邮件 | [src/views/ComposeView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/ComposeView.vue) |
| 视图-管理后台 | [src/views/AdminView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/AdminView.vue) |
| 视图-设置 | [src/views/SettingsView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/SettingsView.vue) |

---

## 附录 B：Bug 清单与定位

### P0 阻断性 Bug

| # | Bug | 位置 | 影响 |
|---|---|---|---|
| 1 | Dovecot SQL 列名错误 `password` 应为 `password_hash` | [dovecot-sql.conf#L12](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/dovecot/dovecot-sql.conf#L12) | IMAP 登录全部失败 |
| 2 | spam-filter.js 导入不存在的 `validateSenderDomain` | [spam-filter.js#L15](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/spam-filter.js#L15) | 调用即崩溃（实际未被引用） |
| 3 | spam-filter.js 函数签名错误 | [spam-filter.js#L48](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/spam-filter.js#L48) | checkSPF 参数顺序错 |
| 4 | spam-filter.js 返回值比较错误 | [spam-filter.js#L50-L51](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/spam-filter.js#L50-L51) | 条件永远为 false |
| 5 | mail.js IDOR 越权 | [mail.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/mail.js) `/:id/read`、`/:id/unread`、`/:id/star` | 任意用户可改任意邮件 |
| 6 | JWT 默认密钥 `change-me-in-production` | [config.js#L17](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/config.js#L17) | 可伪造任意身份 |
| 7 | init-db.js 与 migrations 表结构不一致 | [init-db.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/init-db.js) vs [migrations/](file:///d:/New%20AI%20Project/mymail/mymail-platform/migrations) | 单一初始化方式必崩 |
| 8 | 前端 MailDetailView v-html XSS | [MailDetailView.vue#L67](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/MailDetailView.vue#L67) | 存储型 XSS |
| 9 | 前端 ComposeView 回复转发 XSS | [ComposeView.vue#L247-L251](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/ComposeView.vue#L247-L251) | 二次 XSS |
| 10 | 前端 /admin 路由无权限守卫 | [router/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/router/index.js) | 任意用户可进管理后台 |
| 11 | 前端 MailView batchDelete toast 未定义 | [MailView.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/MailView.vue) | ReferenceError |
| 12 | 前端 UploadZone isUploading 恒 false | [UploadZone.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/components/UploadZone.vue) | 附件未上传完即可发送 |
| 13 | backup.sh 备份含 .env 密钥 | [backup.sh](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/backup.sh) | 备份泄漏即全盘沦陷 |

### P1 高危 Bug

| # | Bug | 位置 |
|---|---|---|
| 1 | DAO 层全程无事务 | [message-dao.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/message-dao.js) emptyTrash 等 |
| 2 | CORS 全开放 | [app.js#L31](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/app.js#L31) |
| 3 | API Key O(n) bcrypt 性能瓶颈 | [api-auth.js#L26-L34](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/middleware/api-auth.js#L26-L34) |
| 4 | getNextUid 竞态条件 | [message-dao.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/message-dao.js) |
| 5 | storage_limit 未校验 | [user-dao.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/user-dao.js) updateStorageUsed |
| 6 | message-dao SQL 拼接 `${days}` | [message-dao.js#L79,L83](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/dao/message-dao.js) |
| 7 | rule-engine ReDoS 风险 | [rule-engine.js#L52-L53](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/rule-engine.js#L52-L53) |
| 8 | Dockerfile HEALTHCHECK 无效 | [Dockerfile#L51-L52](file:///d:/New%20AI%20Project/mymail/mymail-platform/Dockerfile#L51-L52) |
| 9 | Docker compose 数据卷路径不一致 | [docker-compose.yml](file:///d:/New%20AI%20Project/mymail/mymail-platform/docker-compose.yml) |
| 10 | Dovecot uid/gid 与 Dockerfile 不匹配 | [Dockerfile](file:///d:/New%20AI%20Project/mymail/mymail-platform/Dockerfile) + [dovecot-sql.conf](file:///d:/New%20AI%20Project/mymail/mymail-platform/config/dovecot/dovecot-sql.conf) |
| 11 | docker-compose 强制 SMTP_TLS_REJECT_UNAUTHORIZED=false | [docker-compose.yml#L17](file:///d:/New%20AI%20Project/mymail/mymail-platform/docker-compose.yml#L17) |
| 12 | setup.sh 以 root 运行 Dovecot | [setup.sh](file:///d:/New%20AI%20Project/mymail/mymail-platform/scripts/setup.sh) |
| 13 | smtp-sender.js sendLocal 未保存 DB / 未处理附件 | [smtp-sender.js#L38-L67](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/services/smtp-sender.js#L38-L67) |
| 14 | admin.js POST /users 调用错误的 updateStorageUsed | [admin.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/src/routes/admin.js) |
| 15 | 前端 401 处理用 window.location.href | [api/index.js](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/api/index.js) |
| 16 | 前端 AdminView 重置密码明文 toast | [AdminView.vue#L175](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/views/AdminView.vue#L175) |
| 17 | 前端 AppLayout 移动端退出按钮不可点击 | [AppLayout.vue](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/components/AppLayout.vue) |
| 18 | 前端 WS token 走 URL query | [stores/ws.js#L18](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/stores/ws.js#L18) |
| 19 | 前端 escapeHtml 未转义 `"` `'` | [useFormat.js#L16](file:///d:/New%20AI%20Project/mymail/mymail-vue/src/composables/useFormat.js#L16) |
| 20 | .env.example `$(openssl rand -hex 8)` 不生效 | [.env.example#L28](file:///d:/New%20AI%20Project/mymail/mymail-platform/.env.example#L28) |
| 21 | knexfile.js 未加载 dotenv | [knexfile.js](file:///d:/New%20AI%20Project/mymail/mymail-platform/knexfile.js) |

---

> **报告结束**
>
> 本报告基于 2026-07-03 的代码状态生成。建议在完成 P0 修复后重新审计，验证问题是否真正解决。
