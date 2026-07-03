# 后端文件详解：HTTP 接口层

> 本文档详解 MyMail Go 后端的「HTTP 接口层」模块，覆盖 `internal/httpapi/` 目录下的路由注册、处理器（handler）、中间件（middleware）以及数据传输对象（dto）。该层基于 [Gin](https://gin-gonic.com/) 框架构建，承接前端 / 第三方程序的 HTTP 请求，完成认证、限流、参数校验后调用 service 层完成业务逻辑，并以 JSON 形式返回响应。

## 模块整体结构

```
internal/httpapi/
├── router.go                      # 路由总入口，聚合依赖、注册中间件与端点
├── handler/                       # HTTP 请求处理器
│   ├── auth.go                    # 认证：注册/登录/资料/密码
│   ├── mail.go                    # 邮件：列表/详情/发送/标记/附件/批量
│   ├── admin.go                   # 管理员后台 + API Key 管理
│   ├── apiv1.go                   # /api/v1 外部发信（API Key 认证）
│   ├── rules.go                   # 用户邮件规则 CRUD
│   ├── ws.go                      # WebSocket 升级端点
│   └── health.go                  # 健康检查（liveness/readiness/startup）
├── middleware/                    # Gin 中间件
│   ├── auth.go                    # JWT 认证 + RequireAdmin
│   ├── api_auth.go                # API Key 认证 + RequireScope
│   ├── api_rate_limit.go          # API Key 维度固定窗口限流
│   ├── context.go                 # request_id 注入/提取 context
│   ├── cors.go                    # CORS 跨域（白名单）
│   ├── ownership.go               # 邮件归属校验（防 IDOR）
│   ├── request_logger.go          # 访问日志 + Prometheus 指标
│   └── security_requestid_recover.go  # RequestID/安全头/Recover
└── dto/                           # 请求/响应数据传输对象
    ├── auth.go
    ├── mail.go
    └── admin.go
```

调用层次：`HTTP Request → 全局中间件链 → 路由组中间件 → 端点中间件 → Handler → Service → DAO`。Handler 不直接操作数据库，所有业务逻辑下沉到 service 层。

---

## 1. 路由注册 internal/httpapi/router.go

**所属包**：`httpapi`
**核心职责**：聚合全部依赖、构建 `gin.Engine`、注册中间件链与所有路由端点，并提供前端 SPA 静态文件回退服务。

### 1.1 Deps 依赖聚合结构

`Deps` 结构体以「构造参数聚合」的方式集中声明路由注册所需的全部依赖，避免 `NewRouter` 出现一长串函数参数。`nil` 字段表示对应路由组不注册（特性开关）：

```go
type Deps struct {
    Cfg         *config.Config
    DB          *db.DB
    UserDAO     *dao.UserDAO
    JWTManager  *crypto.JWTManager
    AuthService *service.AuthService
    MailService *service.MailService
    AttachStore *attachment.Store

    // 阶段 5 新增（nil 表示不注册对应路由组）
    APIKeyService *service.APIKeyService // /api/auth/api-keys + /api/v1/send
    AdminService  *service.AdminService  // /api/admin/*
    RuleService   *service.RuleService   // /api/rules/*

    // 阶段 6 新增
    WSHub *ws.Hub // /ws WebSocket 升级
}
```

依赖来源：`config`（配置）、`crypto`（JWT 签发/校验）、`service`（业务逻辑）、`storage/dao`（数据访问）、`storage/attachment`（附件存储）、`ws`（WebSocket Hub）、`web`（//go:embed 嵌入的前端 dist 目录）。

### 1.2 NewRouter() 路由构建

`NewRouter(deps Deps) *gin.Engine` 是整个 HTTP 层的入口，主要步骤：

1. 生产环境设置 `gin.ReleaseMode`，禁用调试日志。
2. `gin.New()` 创建不带默认中间件的引擎（手动注册中间件，避免 `Logger`/`Recovery` 重复）。
3. 依次注册全局中间件链。
4. 注册基础设施端点（健康检查、metrics）。
5. 按「特性开关 + service 非空」条件注册业务端点。
6. 注册 SPA 静态文件回退。

### 1.3 中间件链

全局中间件按下列顺序注册，**顺序即执行顺序**，对每个请求都生效：

```go
r.Use(middleware.Recover())         // 1. panic 恢复（最外层，兜底所有 panic）
r.Use(middleware.Security())        // 2. 安全响应头（CSP/HSTS/X-Frame-Options 等）
r.Use(middleware.RequestID())       // 3. 生成/透传 X-Request-ID，注入 context
r.Use(middleware.RequestLogger())   // 4. 访问日志（依赖 request_id）
r.Use(middleware.CORS(deps.Cfg))    // 5. CORS 跨域处理
r.Use(middleware.Metrics())         // 6. Prometheus 指标埋点
```

设计要点：
- `Recover` 必须最外层，保证后续中间件/handler 的 panic 都能被捕获。
- `RequestID` 在 `RequestLogger` 之前，确保日志能带上 `request_id`。
- `Metrics` 在最内层，使 `HTTPRequestsInFlight` 指标准确反映在途请求数。

### 1.4 路由组注册

`NewRouter` 调用一系列 `register*Routes` 私有函数完成分组注册，每个函数对应一个业务域：

| 函数 | 路由前缀 | 鉴权方式 | 触发条件 |
|------|---------|---------|---------|
| `registerAuthRoutes` | `/api/auth` | 部分公开 + 部分 JWT | 始终注册 |
| `registerMailRoutes` | `/api/mail` | JWT | 始终注册 |
| `registerAPIKeyRoutes` | `/api/auth/api-keys` | JWT | `FeatureAPIKey && APIKeyService != nil` |
| `registerAdminRoutes` | `/api/admin` | JWT + RequireAdmin | `AdminService != nil` |
| `registerAPIV1Routes` | `/api/v1` | APIKeyAuth + 限流 + RequireScope | `FeatureAPIKey && APIKeyService != nil` |
| `registerRuleRoutes` | `/api/rules` | JWT | `FeatureRules && RuleService != nil` |
| `/ws` 端点 | `/ws` | WebSocket 子协议/query token | `WSHub != nil` |

各路由组在注册时即绑定对应的鉴权中间件，例如管理员路由链为：

```go
g := r.Group("/api/admin")
g.Use(middleware.Authenticate(deps.JWTManager, deps.UserDAO))
g.Use(middleware.RequireAdmin())
```

`/api/mail/:id/*` 动态路径端点统一前置 `RequireOwnedMail`，修复 P0-3 IDOR 越权漏洞：

```go
owned := mail.Group("/:id", middleware.RequireOwnedMail(deps.MailService))
```

### 1.5 SPA 路由处理

`registerSPARoutes` 通过 `//go:embed web/dist` 嵌入前端构建产物，运行时由 Go 直接提供静态资源服务。SPA 回退策略：

1. `/api/*`、`/ws`、`/healthz`、`/readyz`、`/startupz`、`/health`、`/metrics*` → 返回 JSON `{"error":"not found"}`，避免 SPA 劫持 API 404。
2. 静态文件存在（如 `/assets/index-xxx.js`）→ 直接返回文件内容。
3. 其他路径 → 返回 `index.html`，交由 Vue Router 处理前端路由。
4. 开发环境下 `dist/` 仅有 `.gitkeep`，`index.html` 为空 → 回退到 JSON 404。

```go
r.NoRoute(func(c *gin.Context) {
    path := c.Request.URL.Path
    if strings.HasPrefix(path, "/api/") || path == "/ws" || /* ... */ {
        c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
        return
    }
    // 尝试静态文件 → SPA fallback → JSON 404
})
```

---

## 2. 认证处理器 internal/httpapi/handler/auth.go

**所属包**：`handler`
**核心职责**：实现 `/api/auth/*` 认证相关端点，与原 Node.js 后端 API 契约 100% 兼容。

### 2.1 AuthHandler 结构体

```go
type AuthHandler struct {
    svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler
```

Handler 仅持有 `AuthService` 引用，所有业务逻辑（密码哈希、JWT 签发、登录失败计数等）由 service 完成。

### 2.2 注册/登录/资料/密码接口

#### Register — POST /api/auth/register

公开端点，无需认证。绑定 `dto.RegisterRequest`，调用 `svc.Register` 完成注册并签发 token。

请求示例：
```http
POST /api/auth/register
Content-Type: application/json

{"username":"alice","password":"P@ssw0rd","displayName":"Alice"}
```

响应（201）：
```json
{"message":"注册成功","token":"<jwt>","user":{"id":1,"username":"alice","email":"alice@mymail","displayName":"Alice","role":"user"}}
```

#### Login — POST /api/auth/login

公开端点。`service.SanitizeEmail` 规范化邮箱后调用 `svc.Login`，传入 `c.ClientIP()` 用于登录日志。若返回 `RequirePasswordChange=true`（默认密码未改），响应中携带该字段提示前端强制改密。

请求示例：
```http
POST /api/auth/login
Content-Type: application/json

{"email":"alice@mymail","password":"P@ssw0rd","remember":true}
```

响应（200）：
```json
{"message":"登录成功","token":"<jwt>","user":{...}}
```

需改密时响应：
```json
{"message":"需要修改默认密码","token":"<jwt>","user":{...},"requirePasswordChange":true}
```

#### Me — GET /api/auth/me

需 JWT 认证。通过 `middleware.CurrentUser(c)` 取已注入的 `*dao.User`，返回完整个人资料（含 `signature`、`storageLimit`、`storageUsed`）。

#### UpdateProfile — PUT /api/auth/profile

需 JWT 认证。绑定 `dto.UpdateProfileRequest`（指针字段，nil 不更新），调用 `svc.UpdateProfile` 更新昵称/签名。

#### ChangePassword — PUT /api/auth/password

需 JWT 认证。绑定 `dto.ChangePasswordRequest`，需校验当前密码，调用 `svc.ChangePassword`。

#### ChangeDefaultPassword — POST /api/auth/change-default-password

需 JWT 认证。强制改密（不校验当前密码），用于首次登录默认密码场景。

### 2.3 请求/响应格式约定

- 成功响应：`{"message":"...", "token":"...", "user":{...}}`
- 失败响应：`{"error":"..."}`
- 错误处理统一由 `writeAuthError` 完成：若 service 返回 `*service.AuthError` 则用其携带的 `Status`/`Message`，否则返回 500。

```go
func writeAuthError(c *gin.Context, err error) {
    var ae *service.AuthError
    if errors.As(err, &ae) {
        c.JSON(ae.Status, dto.ErrorResponse{Error: ae.Message})
        return
    }
    c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "服务器内部错误"})
}
```

---

## 3. 邮件处理器 internal/httpapi/handler/mail.go

**所属包**：`handler`
**核心职责**：实现 `/api/mail/*` 邮件相关端点，覆盖列表、详情、发送、草稿、标记、删除、附件下载、批量操作。

### 3.1 MailHandler 结构体

```go
type MailHandler struct {
    svc         *service.MailService
    attachStore *attachment.Store
}
```

`attachStore` 用于附件落盘（`SaveFromMultipartFile`）与流式读取（`OpenFile`）。

### 3.2 列表与详情

#### List — GET /api/mail/list

查询参数：`folder`（默认 `INBOX`）、`page`（默认 1）、`limit`（默认 20）、`search`、`unread`（字符串 `"true"`）。调用 `svc.List` 返回分页列表，将 `dao.Message` 转为 `dto.MailDetailResponse`。

响应示例：
```json
{
  "messages": [{...}],
  "total": 100,
  "page": 1,
  "limit": 20,
  "unreadCount": 5
}
```

#### UnreadCount — GET /api/mail/unread-count

返回 INBOX/SENT/DRAFTS/TRASH 四个文件夹的未读数：
```json
{"INBOX":3,"SENT":0,"DRAFTS":1,"TRASH":0}
```

#### Get — GET /api/mail/:id

`RequireOwnedMail` 中间件已校验归属并注入 `*dao.Message`，handler 通过 `middleware.OwnedMail(c)` 取出。**副作用**：若邮件未读，获取详情后自动调用 `svc.MarkRead` 标记为已读（与原 Node.js 行为一致）。

### 3.3 发送与草稿

#### Send — POST /api/mail/send

`Content-Type: multipart/form-data`，字段使用 camelCase（与前端一致）：`to`、`cc`、`bcc`、`subject`、`bodyHtml`、`bodyText`、`replyTo` + `attachments`（多文件）。

附件处理流程：
1. `c.MultipartForm()` 取 `form.File["attachments"]`。
2. 每个 `*multipart.FileHeader` 调用 `attachStore.SaveFromMultipartFile` 落盘，内部含 MIME 白名单 + 魔数校验（P0-7）。
3. 组装 `service.AttachmentMeta` 切片传给 `svc.Send`。

响应示例：
```json
{"message":"发送成功","messageId":"<msgid>"}
```

#### SaveDraft — POST /api/mail/save-draft

`Content-Type: application/json`，绑定 `dto.SaveDraftRequest`，调用 `svc.SaveDraft` 返回草稿 ID。

### 3.4 标记/星标/删除/恢复

所有 `:id` 端点均前置 `RequireOwnedMail`：

| 方法 | 路径 | 说明 |
|------|------|------|
| PUT | `/api/mail/:id/read` | 标记已读 |
| PUT | `/api/mail/:id/unread` | 标记未读 |
| PUT | `/api/mail/:id/star` | 切换星标 |
| DELETE | `/api/mail/:id` | 已在 TRASH 则永久删除，否则软删除 |
| PUT | `/api/mail/:id/restore` | 从 TRASH 恢复到 INBOX |
| POST | `/api/mail/empty-trash` | 清空回收站 |

`Delete` 的分支逻辑：
```go
if msg.Folder == "TRASH" {
    err = h.svc.PermanentDelete(ctx, user.ID, msg.ID)
} else {
    err = h.svc.SoftDelete(ctx, user.ID, msg.ID)
}
```

### 3.5 附件下载

#### DownloadAttachment — GET /api/mail/:id/attachments/:aid/download

`:id` 由 `RequireOwnedMail` 校验，`:aid` 由 handler 二次校验（`att.MessageID == msg.ID`）防 IDOR。使用 32KB 缓冲流式写入，设置 `Content-Type: application/octet-stream` 与 `Content-Disposition`。

#### DownloadAllAttachments — GET /api/mail/:id/attachments/download-all

将所有附件打包为 ZIP 流式下载，调用 `util.ZipAttachments` 处理重名。响应头一旦发送则无法回滚状态码，错误仅记日志。

### 3.6 批量操作

补齐前端期望的批量端点，绑定 `dto.BatchOperationRequest`（`ids` 必填，`folder` 仅 `batch/move` 用）：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/mail/batch/mark-read` | 批量标记已读 |
| POST | `/api/mail/batch/move` | 批量移动到指定 folder |
| POST | `/api/mail/batch/delete` | 批量删除 |

响应：
```json
{"message":"ok","affected":5}
```

### 3.7 错误处理

`writeMailError` 与 `writeAuthError` 模式一致：识别 `*service.MailError` 取其 `Status`/`Message`，否则 500。

---

## 4. 管理员处理器 internal/httpapi/handler/admin.go

**所属包**：`handler`
**核心职责**：实现 `/api/admin/*` 管理员后台端点 + `/api/auth/api-keys/*` API Key 管理端点。

### 4.1 AdminHandler 结构体

```go
type AdminHandler struct {
    adminSvc *service.AdminService
}
```

权限校验在路由层完成（`Authenticate` + `RequireAdmin`），handler 不再重复校验。

### 4.2 用户管理接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/stats` | 用户统计（total_users/active_users） |
| GET | `/api/admin/users` | 用户列表 |
| GET | `/api/admin/users/:id` | 用户详情 |
| POST | `/api/admin/users` | 创建用户 |
| PUT | `/api/admin/users/:id` | 更新用户（role/storage_limit/is_active/password） |
| DELETE | `/api/admin/users/:id` | 软删除用户 |
| GET | `/api/admin/settings` | 查询全局设置 |
| PUT | `/api/admin/settings` | 更新全局设置 |

`:id` 解析使用辅助函数 `parseUserID`，失败返回 400 `"无效的用户 ID"`。

创建用户请求示例：
```http
POST /api/admin/users
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{"username":"bob","email":"bob@mymail","password":"P@ssw0rd","display_name":"Bob","role":"user","storage_limit":104857600}
```

`UpdateUserRequest` 所有字段为指针，nil 表示不更新：
```go
type UpdateUserRequest struct {
    Role         *string `json:"role"`
    StorageLimit *int64  `json:"storage_limit"`
    IsActive     *bool   `json:"is_active"`
    Password     *string `json:"password"`
}
```

### 4.3 APIKeyHandler 结构体

```go
type APIKeyHandler struct {
    svc *service.APIKeyService
}
```

用户维度（非管理员），JWT 认证。端点：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/api-keys` | 创建 API Key（明文仅返回一次） |
| GET | `/api/auth/api-keys` | 列出当前用户的 API Key |
| DELETE | `/api/auth/api-keys/:id` | 删除 API Key（service 校验归属） |

创建响应示例（201）：
```json
{
  "id":1,
  "plain_text":"mk_xxxxxxxxxxxxxxxxxxxxxxxx",
  "name":"send-only",
  "key_prefix":"mk_abc1",
  "scopes":["send"],
  "rate_limit":60,
  "created_at":"2026-07-04T10:00:00Z"
}
```

`plain_text` 字段仅在创建时返回一次（数据库只存 bcrypt hash + `key_prefix`），后续列表接口返回的 `APIKeyResponse` 不含明文。

---

## 5. API v1 处理器 internal/httpapi/handler/apiv1.go

**所属包**：`handler`
**核心职责**：实现 `/api/v1/*` 端点，供外部程序通过 API Key 发信。

### 5.1 APIV1Handler 结构体

```go
type APIV1Handler struct {
    mailSvc *service.MailService
}
```

### 5.2 Send — POST /api/v1/send

鉴权链：`APIKeyAuth` → `APIKeyRateLimiter.RateLimit()` → `RequireScope("send")`（路由层配置）。

请求体（JSON，snake_case）：
```json
{
  "to":["bob@example.com"],
  "cc":[],
  "bcc":[],
  "subject":"Hello",
  "body_html":"<p>Hi</p>",
  "body_text":"Hi",
  "reply_to":""
}
```

handler 内部将 `[]string` 用 `strings.Join(.., ", ")` 拼成逗号分隔字符串以适配 `service.SendInput`。响应：

```json
{"message":"发送成功","queue_id":42}
```

`queue_id` 实际为 `MailService.Send` 返回的 `MailID`（邮件记录主键），适配说明见文件头注释。handler 还做防御性检查：`CurrentUser`/`CurrentAPIKey` 任一为 nil 直接 401。

---

## 6. 规则处理器 internal/httpapi/handler/rules.go

**所属包**：`handler`
**核心职责**：实现 `/api/rules/*` 用户邮件规则 CRUD。

### 6.1 RuleHandler 结构体与 DTO

```go
type RuleHandler struct {
    svc *service.RuleService
}
```

规则 DTO 内联在本文件（未放 dto 包），便于与 `dao.RuleCondition`/`dao.RuleAction` 对齐：

```go
type RuleConditionDTO struct {
    Field string `json:"field"`
    Op    string `json:"op"`
    Value any    `json:"value"`
}
type RuleActionDTO struct {
    Type    string `json:"type"`
    Folder  string `json:"folder,omitempty"`
    Address string `json:"address,omitempty"`
    Flag    string `json:"flag,omitempty"`
}
```

### 6.2 规则 CRUD 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/rules` | 列出当前用户的所有规则 |
| POST | `/api/rules` | 创建规则 |
| PUT | `/api/rules/:id` | 更新规则（校验归属） |
| DELETE | `/api/rules/:id` | 删除规则（校验归属） |

`UpdateRuleRequest` 所有字段为指针，nil 不更新；`Conditions`/`Actions` 为 `*[]` 便于区分「不更新」与「清空」。

创建请求示例：
```http
POST /api/rules
Authorization: Bearer <jwt>
Content-Type: application/json

{
  "name":"转发 boss 邮件",
  "priority":10,
  "conditions":[{"field":"from","op":"contains","value":"boss@company.com"}],
  "actions":[{"type":"mark_flag","flag":"important"}]
}
```

归属校验在 service 层（`svc.Update(id, user.ID, ...)` 传入 `user.ID`），非本人规则返回 403/404。

---

## 7. WebSocket 处理器 internal/httpapi/handler/ws.go

**所属包**：`handler`
**核心职责**：处理 `GET /ws` WebSocket 升级请求，建立长连接用于实时推送新邮件通知。

### 7.1 WSHandler 结构体

```go
type WSHandler struct {
    hub     *wspkg.Hub
    jwtMgr  *crypto.JWTManager
    userDAO *dao.UserDAO
}
```

### 7.2 Upgrade 端点 — GET /ws

认证流程（P1-18：子协议优先 + URL query 兼容）：

1. `wspkg.Authenticate` 从 `Sec-WebSocket-Protocol` 头提取 `auth.<token>`（推荐）；回退到 `/ws?token=<token>`（兼容）。
2. JWT 验证 + 用户查找；失败返回 401 `{"error":"WebSocket 认证失败"}`，并递增 `metrics.WSConnectionsTotal{reason="auth_failed"}`。

升级成功后的生命周期：

```go
conn, err := websocket.Accept(c.Writer, c.Request, acceptOpts)
defer conn.Close(websocket.StatusInternalError, "内部错误")

client := wspkg.NewClient(userID, conn, h.hub)
h.hub.Register(userID, client)
defer h.hub.Unregister(userID, client)

// 发送欢迎消息
welcome := wspkg.Message{Type: "connected", Message: "WebSocket 已连接"}
client.Send(welcomePayload)

// 启动 WritePump（阻塞直到连接关闭）
ctx, cancel := context.WithCancel(c.Request.Context())
defer cancel()
client.WritePump(ctx)
```

`OriginPatterns: []string{"*"}` 允许任意来源（生产环境应由反向代理限制）。子协议回显保证与原 Node.js 行为一致。连接随 HTTP 服务器关闭而关闭（使用 `c.Request.Context()`）。

---

## 8. 健康检查处理器 internal/httpapi/handler/health.go

**所属包**：`handler`
**核心职责**：实现 P1-8 修复的三探针分离，供 Kubernetes 探针使用。

### 8.1 HealthHandler 结构体

```go
type HealthHandler struct {
    db      *db.DB
    started atomic.Bool
}
```

构造时启动一个 goroutine，延迟 2 秒后将 `started` 置 true（模拟预热完成）。

### 8.2 三探针端点

| 端点 | 用途 | 检查项 | 失败行为 |
|------|------|--------|---------|
| GET /healthz | liveness 存活探针 | 进程存活 | 重启容器 |
| GET /readyz | readiness 就绪探针 | startup + DB 连通性 | 从负载均衡移除 |
| GET /startupz | startup 启动探针 | started 标志 | 避免慢启动被 liveness 杀死 |
| GET /health | 兼容别名 | 同 liveness | — |

`Readiness` 使用 2 秒超时 context 调用 `db.PingContext`，返回各项检查明细：

```json
{
  "status":"ready",
  "checks":{"startup":"ready","db":"up"}
}
```

未就绪时返回 503 `{"status":"not_ready","checks":{...}}`。

---

## 9. 认证中间件 internal/httpapi/middleware/auth.go

**所属包**：`middleware`
**核心职责**：JWT 认证 + 管理员鉴权，与原 Node.js `middleware/auth.js` 行为一致。

### 9.1 Authenticate 中间件

```go
func Authenticate(jwtMgr *crypto.JWTManager, userDAO *dao.UserDAO) gin.HandlerFunc
```

执行流程：

1. 取 `Authorization` 头，校验 `Bearer ` 前缀；缺失 → 401 `"未登录"`。
2. `jwtMgr.Verify(token)` 校验签名/过期；失败 → 401 `"Token 已过期，请重新登录"`。
3. `userDAO.FindByID(claims.ID)` 查库验证用户仍有效；不存在/禁用 → 401 `"账号已禁用"`。
4. 通过 `c.Set(ContextKeyUser, user)` 注入用户，`c.Next()`。

每步失败均递增 `metrics.AuthAttemptsTotal{type="jwt", reason=...}`。

### 9.2 RequireAdmin 中间件

```go
func RequireAdmin() gin.HandlerFunc
```

必须在 `Authenticate` 之后。取 `ContextKeyUser`，若 `user.Role != "admin"` → 403 `"需要管理员权限"`。

### 9.3 CurrentUser 辅助函数

```go
func CurrentUser(c *gin.Context) *dao.User
```

从 context 取用户，未认证返回 nil。所有 handler 通过此函数获取当前用户。

---

## 10. API Key 认证中间件 internal/httpapi/middleware/api_auth.go

**所属包**：`middleware`
**核心职责**：API Key 认证 + scope 鉴权，用于 `/api/v1/*` 外部发信端点。

### 10.1 APIKeyAuth 中间件

```go
func APIKeyAuth(verifier APIKeyVerifier, userDAO *dao.UserDAO) gin.HandlerFunc
```

`APIKeyVerifier` 是接口（`Verify(ctx, plaintext) (*dao.APIKey, error)`），由 `service.APIKeyService` 实现，依赖倒置避免循环依赖。

流程：提取 `Bearer mk_xxx` → `verifier.Verify`（内部 prefix 索引 + bcrypt 比对）→ `userDAO.FindByID` 校验用户有效 → 注入 `ContextKeyUser` + `ContextKeyAPIKey`。

### 10.2 RequireScope 中间件

```go
func RequireScope(scope string) gin.HandlerFunc
```

取 `ContextKeyAPIKey`，遍历 `apiKey.Scopes` 校验是否包含指定 scope；不匹配 → 403 `"API Key 无此操作权限"`。

### 10.3 CurrentAPIKey 辅助函数

```go
func CurrentAPIKey(c *gin.Context) *dao.APIKey
```

供 handler 防御性检查使用。

---

## 11. API Key 限流中间件 internal/httpapi/middleware/api_rate_limit.go

**所属包**：`middleware`
**核心职责**：基于 API Key 维度的固定窗口限流，满足验收标准 9.6 第 4 项。

### 11.1 APIKeyRateLimiter 结构体

```go
type APIKeyRateLimiter struct {
    mu      sync.Mutex
    buckets map[int64]*rateBucket // key: API Key ID
}
type rateBucket struct {
    count   int
    resetAt time.Time
}
```

内存存储，无需外部依赖；`sync.Mutex` 保证线程安全。

### 11.2 RateLimit 中间件

```go
func (l *APIKeyRateLimiter) RateLimit() gin.HandlerFunc
```

必须在 `APIKeyAuth` 之后。逻辑：

- `apiKey == nil` → 跳过（由前置中间件处理 401）。
- `apiKey.RateLimit <= 0` → 不限流。
- `allow(keyID, limit)` 判断：新窗口或窗口过期则重置；`count > limit` 返回 false。
- 超限 → 429 `{"error":"API Key 请求频率超限，请稍后重试"}`，并设置 `Retry-After: 60`。

懒清理设计：检查时若窗口已过期则重置，无需后台 goroutine。`Cleanup()` 方法可选调用清理所有过期 bucket。

---

## 12. Context 中间件 internal/httpapi/middleware/context.go

**所属包**：`middleware`
**核心职责**：提供 request_id 在 `context.Context` 中的注入与提取，用于全链路日志关联。

```go
func WithRequestID(ctx context.Context, reqID string) context.Context
func GetRequestID(ctx context.Context) string
```

使用 `logger.RequestIDKey` 作为 context key。`RequestID` 中间件调用 `WithRequestID` 将 ID 注入 `c.Request.Context()`，供 service 层通过 `GetRequestID` 取用。

---

## 13. CORS 中间件 internal/httpapi/middleware/cors.go

**所属包**：`middleware`
**核心职责**：P1-2 修复，基于白名单的跨域控制（非全开放）。

```go
func CORS(cfg *config.Config) gin.HandlerFunc
```

构建允许来源集合：`cfg.CORSAllowedOrigins` + 自动添加 `https://<Domain>` 与 `https://<MailHost>`。请求处理：

- Origin 命中白名单 → 设置 `Access-Control-Allow-Origin`（回显具体 origin，不发 `*`）、`Allow-Methods`、`Allow-Headers`（含 `X-Request-ID`、`Idempotency-Key`）、`Allow-Credentials: true`、`Max-Age: 86400`、`Vary: Origin`。
- OPTIONS 预检：origin 不允许 → 403；允许 → 204 No Content。

---

## 14. 归属校验中间件 internal/httpapi/middleware/ownership.go

**所属包**：`middleware`
**核心职责**：修复 P0-3 IDOR 越权，统一在 `/api/mail/:id/*` 前置校验邮件归属。

### 14.1 RequireOwnedMail 中间件

```go
func RequireOwnedMail(mailSvc *service.MailService) gin.HandlerFunc
```

必须在 `Authenticate` 之后。流程：

1. `CurrentUser(c)` 取用户；nil → 401。
2. 解析 `:id`；失败/非正 → 400 `"无效的邮件 ID"`。
3. `mailSvc.Get(ctx, user.ID, id)` 校验归属；返回 `*MailError(404)` → 404；其他错误 → 500。
4. `msg == nil` → 404 `"邮件不存在"`（不区分不存在/无权，防枚举）。
5. 通过 `c.Set(ContextKeyMail, msg)` 注入邮件，`c.Next()`。

### 14.2 OwnedMail 辅助函数

```go
func OwnedMail(c *gin.Context) *dao.Message
```

handler 通过此函数取出已校验归属的邮件，避免重复查询数据库。

---

## 15. 请求日志中间件 internal/httpapi/middleware/request_logger.go

**所属包**：`middleware`
**核心职责**：访问日志 + Prometheus 指标埋点。

### 15.1 RequestLogger 中间件

记录每个请求的 `method/path/status/latency_ms/ip/request_id`，可选 `query` 与 `errors`。根据状态码选择日志级别：

- `status >= 500` → `slog.Error`
- `status >= 400` → `slog.Warn`
- 其他 → `slog.Info`

### 15.2 Metrics 中间件

```go
func Metrics() gin.HandlerFunc
```

- 请求进入：`HTTPRequestsInFlight.Inc()`；defer `Dec()`。
- 请求结束：`HTTPRequestsTotal{method,path,status}.Inc()`、`HTTPRequestDuration{method,path}.Observe(latency)`。
- `c.FullPath()` 为空（NoRoute）时记为 `"unknown"`。

---

## 16. 安全/RequestID/Recover 中间件 internal/httpapi/middleware/security_requestid_recover.go

**所属包**：`middleware`
**核心职责**：三个全局基础中间件集中实现。

### 16.1 RequestID 中间件

```go
func RequestID() gin.HandlerFunc
```

优先透传上游 `X-Request-ID` 头，否则生成 UUID。设置到 `c.Set("request_id", ...)`、响应头 `X-Request-ID`，并通过 `WithRequestID` 注入 `c.Request.Context()` 供下游使用。

### 16.2 Security 中间件

设置安全响应头：

| 头 | 值 | 作用 |
|----|----|------|
| `X-Content-Type-Options` | `nosniff` | 防 MIME 嗅探 |
| `X-Frame-Options` | `DENY` | 防点击劫持 |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | 控制 Referrer 泄露 |
| `Permissions-Policy` | `geolocation=(), microphone=(), camera=()` | 禁用不必要的浏览器特性 |
| `X-XSS-Protection` | `1; mode=block` | 旧浏览器 XSS 保护 |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains; preload` | HSTS 强制 HTTPS |
| `Content-Security-Policy` | `default-src 'self'; ...` | CSP 资源加载限制 |

### 16.3 Recover 中间件

```go
func Recover() gin.HandlerFunc
```

`defer recover()` 捕获 panic，记录堆栈到 `c.Error(panicError{...})`，返回 500 `{"error":"服务器内部错误"}`，避免进程崩溃。必须作为最外层中间件。

---

## 17. 认证 DTO internal/httpapi/dto/auth.go

**所属包**：`dto`
**核心职责**：定义认证相关请求/响应 DTO，JSON 标签与原 Node.js 后端 100% 兼容。

| DTO | 用途 | 关键字段 |
|-----|------|---------|
| `RegisterRequest` | 注册请求 | `username/password`（required）、`displayName` |
| `LoginRequest` | 登录请求 | `email/password`（required）、`remember` |
| `UpdateProfileRequest` | 更新资料 | `displayName/Signature`（指针，nil 不更新） |
| `ChangePasswordRequest` | 修改密码 | `currentPassword/newPassword` |
| `ChangeDefaultPasswordRequest` | 强制改密 | `newPassword` |
| `UserPublic` | 公开用户信息 | 5 字段 camelCase |
| `UserMe` | `/me` 完整资料 | 含 `signature/storageLimit/storageUsed/createdAt` |
| `AuthResponse` | 认证成功响应 | `message/token/user` + `requirePasswordChange,omitempty` |
| `MessageResponse` | 通用消息响应 | `message` |
| `ErrorResponse` | 错误响应 | `error` |

注意 `UserMe.Signature` 为 `*string`，nil 时 JSON 输出 `null`，与原后端一致。

---

## 18. 邮件 DTO internal/httpapi/dto/mail.go

**所属包**：`dto`
**核心职责**：定义邮件相关 DTO，遵循「请求 body camelCase，响应字段 snake_case」约定（与前端 `MailView.vue` / `ComposeView.vue` 对齐）。

请求 DTO：
- `SaveDraftRequest`：草稿请求（camelCase）。
- `BatchOperationRequest`：批量操作（`ids` required，`folder` 仅 move 用）。

响应 DTO：
- `MailDetailResponse`：邮件详情，字段全 snake_case（`is_read/from_addr/...`），`Attachments` 为可选切片。
- `AttachmentResponse`：附件信息，**不暴露 `storage_path`**（安全考虑）。
- `ListResponse`：列表包装，`unreadCount` 为 camelCase（与原 Node.js 一致）。
- `UnreadCountResponse`：四文件夹未读数，键名 `INBOX/SENT/DRAFTS/TRASH`。
- `SendResponse`/`SaveDraftResponse`/`BatchOperationResponse`：操作响应。
- `UploadResponse`/`UploadedFileDTO`：上传附件响应（供 upload 端点使用）。

---

## 19. 管理员 DTO internal/httpapi/dto/admin.go

**所属包**：`dto`
**核心职责**：定义管理员后台与 API Key 相关 DTO，管理员视角使用 snake_case（含敏感字段）。

管理员 DTO：
- `AdminStatsResponse`：`total_users/active_users`。
- `AdminUserResponse`：含 `role/storage_limit/is_active` 等敏感字段。
- `CreateUserRequest`/`UpdateUserRequest`：`UpdateUserRequest` 全指针，nil 不更新。
- `SettingsResponse`/`SettingItem`/`UpdateSettingsRequest`：全局设置 CRUD。

API Key DTO：
- `APIKeyResponse`：列表展示，不含明文。
- `CreateAPIKeyRequest`：`name/scopes/rate_limit`。
- `CreateAPIKeyResponse`：含 `plain_text`（仅创建时返回一次）。

---

## 附录：中间件链顺序总览

以 `PUT /api/mail/:id/read` 为例，完整执行链：

```
1. Recover               (全局)   panic 兜底
2. Security              (全局)   安全响应头
3. RequestID             (全局)   X-Request-ID
4. RequestLogger         (全局)   访问日志（开始计时）
5. CORS                  (全局)   跨域
6. Metrics               (全局)   InFlight++
7. Authenticate          (路由组) JWT → 注入 user
8. RequireOwnedMail      (端点)   :id 归属校验 → 注入 mail
9. MailHandler.MarkRead  (端点)   调用 svc.MarkRead
   ↓ 响应回写 ↓
8'. (反向) OwnedMail 释放
7'. (反向) Authenticate 释放
6'. Metrics              InFlight--, 记录 latency/status
5'. CORS                 无操作
4'. RequestLogger        输出日志（按状态码选级别）
3'. RequestID            无操作
2'. Security             无操作
1'. Recover              无操作
```

API v1 外部发信端点 `POST /api/v1/send` 链：

```
全局链(1-6) → APIKeyAuth → APIKeyRateLimiter.RateLimit → RequireScope("send") → APIV1Handler.Send
```

## 附录：与其他模块的依赖关系

| 依赖方向 | 依赖模块 | 用途 |
|---------|---------|------|
| httpapi → config | `internal/config` | 读取配置（特性开关、CORS、Domain） |
| httpapi → crypto | `internal/crypto` | JWT 签发/校验 |
| httpapi → service | `internal/service` | 业务逻辑（Auth/Mail/Admin/APIKey/Rule） |
| httpapi → storage/dao | `internal/storage/dao` | 数据访问（User/Message/...） |
| httpapi → storage/db | `internal/storage/db` | DB 连接（健康检查） |
| httpapi → storage/attachment | `internal/storage/attachment` | 附件落盘/读取 |
| httpapi → ws | `internal/ws` | WebSocket Hub/Client |
| httpapi → metrics | `internal/metrics` | Prometheus 指标 |
| httpapi → logger | `internal/logger` | request_id context key |
| httpapi → util | `internal/util` | ZIP 打包 |
| httpapi → web | `web` | //go:embed 前端 dist |

依赖方向严格自上而下，service 层不反向依赖 httpapi，DTO 仅在 httpapi 层内流转（service 层使用自己的 input/output 结构体，handler 负责转换）。
