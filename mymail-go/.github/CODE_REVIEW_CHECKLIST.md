# MyMail 代码评审清单

> 评审 PR 时逐项核对。并非每项都需强制阻塞合并，但每项都应被 consciously 评估并给出结论。
> 评审目标：**正确性 > 安全 > 可读性 > 性能 > 一致性**。

---

## 一、通用检查

### 命名与可读性
- [ ] 标识符命名清晰、自解释，符合 Go 命名惯例（驼峰、首字母缩写全大写如 `URL`/`ID`）。
- [ ] 导出符号（首字母大写）有明确语义，避免歧义缩写。
- [ ] 包名简短、小写、单数（如 `mailsender` 而非 `mailSenders`）。
- [ ] 接口名按行为命名（`-er` 后缀），如 `Sender`、`Validator`。

### 注释与文档
- [ ] 导出函数/类型/常量有 Go doc 注释，以标识符名称开头。
- [ ] 复杂/非显而易见逻辑有内联注释解释"为什么"，而非"做什么"。
- [ ] 无过时、误导或复制粘贴遗留的注释。
- [ ] TODO/FIXME 带关联 Issue 编号：`// TODO(#123): ...`。

### 错误处理
- [ ] 错误被妥善处理（检查返回值、不忽略 `err`），无 `_ =` 吞掉关键错误。
- [ ] 面向用户的错误有上下文（避免裸返回底层错误）。
- [ ] 不滥用 `panic`（仅用于不可恢复的初始化场景，如 `main`/`MustXxx`）。
- [ ] 校验失败返回明确错误，而非静默返回零值。

---

## 二、安全检查

### 注入防护
- [ ] **SQL 注入**：所有 SQL 查询使用参数化绑定（`?` 占位符），无字符串拼接 SQL。
- [ ] **命令注入**：如需执行外部命令，使用 `exec.Command(name, args...)` 参数形式，禁用 `sh -c` 拼接。
- [ ] **XSS**：渲染用户内容前经 `sanitize/html.go`（bluemonday）过滤；HTML 模板使用自动转义。
- [ ] **路径穿越**：处理文件路径时校验 `..`，使用 `filepath.Clean` + 前缀校验，不直接拼接用户输入。

### 访问控制
- [ ] **认证**：受保护端点经过 `middleware/auth.go` / `api_auth.go` 校验，无未授权可达路径。
- [ ] **授权**：跨用户资源访问经 `middleware/ownership.go` 校验所有权，防 **IDOR**（不安全直接对象引用）。
- [ ] **越权**：管理员接口经 `middleware` 中的角色校验，普通用户不可触达。
- [ ] 邮件收发路径的 `RCPT TO` / `MAIL FROM` 经 `smtp/validator.go` 校验，防开放中继。

### 密钥与敏感信息
- [ ] 无硬编码密钥/Token/密码；密钥来自配置或环境变量（参考 `config.Load` 的 `JWT_SECRET` 强校验）。
- [ ] 日志不输出明文密码、JWT、API Key、邮件正文等敏感字段（必要时脱敏）。
- [ ] 密码使用 `crypto/bcrypt` 哈希存储，API Key 使用 `crypto/apikey.go` 哈希存储，不可逆。
- [ ] JWT 签名密钥长度足够，过期时间合理。

### 输入校验
- [ ] 所有外部输入（HTTP / SMTP / WebSocket）经校验后再使用。
- [ ] 邮件地址、域名、附件大小/类型有上限与白名单校验。
- [ ] 反序列化数据有大小/深度限制，防 DoS。

---

## 三、并发检查

### Goroutine 生命周期
- [ ] goroutine 有明确退出条件，无 **goroutine 泄露**（尤其 `hub.go`、`queue.go`、`receiver.go`）。
- [ ] 长期运行的 goroutine 监听 `ctx.Done()`，支持优雅关闭。
- [ ] 不在循环中无限制启动 goroutine（应有并发上限，如 worker pool / 信号量）。

### 数据竞争
- [ ] 共享可变状态受 `sync.Mutex` / `sync.RWMutex` / channel 保护，无裸访问。
- [ ] 计数/标志位使用 `atomic` 包（如 `HealthHandler.started`）。
- [ ] `go test -race` 通过，无竞态告警。
- [ ] map 并发读写有锁保护（Go map 非并发安全）。

### 锁使用
- [ ] 锁的粒度合理，临界区短小，无长耗时 I/O 持锁。
- [ ] 锁顺序一致，避免死锁（多锁时统一加锁次序）。
- [ ] `defer mu.Unlock()` 紧跟 `Lock()`，不遗漏解锁。
- [ ] 避免在持有锁时调用会反向获取同一锁的函数（重入死锁）。

---

## 四、性能检查

### 数据库
- [ ] 无 **N+1 查询**（循环内查 DB），批量操作用 `IN` / 批量预加载。
- [ ] 高频查询字段有索引（参考 `migrations/004_add_indexes`）。
- [ ] 大结果集分页查询（`LIMIT`/`OFFSET` 或游标），不全量加载。
- [ ] 事务范围最小化，长事务不放数据库连接。

### 内存与分配
- [ ] 热路径避免不必要的堆分配（预分配 slice、复用 buffer、`strings.Builder`）。
- [ ] 大文件/附件流式处理（`io.Copy`），不全量读入内存。
- [ ] 缓存策略合理（如 `golang-lru/v2`），有上限与淘汰策略。
- [ ] 避免在热循环中反复 `fmt.Sprintf` / 反射。

### 网络
- [ ] HTTP/SOCKS 连接配置合理超时与连接池，复用 `http.Client`。
- [ ] DNS 查询（如 `spam/dnsbl.go`、`spam/spf.go`）有超时与缓存，避免阻塞。
- [ ] SMTP/HTTP 客户端有超时，不使用默认零值超时。

---

## 五、Go 特定检查

### 错误处理（Go 风格）
- [ ] 错误使用 `fmt.Errorf("xxx: %w", err)` **wrap** 上下文，保留错误链。
- [ ] 自定义错误类型实现 `Error()`，必要时实现 `Is`/`As` 以支持 `errors.Is`/`errors.As`。
- [ ] 不重复 wrap（避免 `%w` 嵌套层数过多导致错误信息冗长）。
- [ ] `io.EOF` 等哨兵错误判断使用 `errors.Is`，而非 `==`。

### Context 传递
- [ ] 函数链路传递 `ctx context.Context`，且作为第一个参数（非结构体字段）。
- [ ] I/O / DB / 网络调用使用 `ctx` 派生的超时（如 `db.PingContext`、`http.Request.Context()`）。
- [ ] 不在 `ctx` 中存储可变状态，context value key 使用未导出类型。
- [ ] 不传递 `context.Background()` 到需要取消/超时的下游（仅顶层/后台任务可用）。

### defer 顺序
- [ ] 资源（文件/连接/锁）获取后立即 `defer Close()`/`defer Unlock()`。
- [ ] 多个 `defer` 按 LIFO 释放，顺序正确（后获取的先释放）。
- [ ] 循环内的 `defer` 注意不会延迟到函数结束才释放（必要时提取为独立函数）。
- [ ] `defer cancel()` 紧跟 `context.WithCancel`/`WithTimeout`，避免 context 泄露。

### 其他 Go 惯例
- [ ] 接口定义在**使用方**而非实现方（消费者驱动），避免过早抽象。
- [ ] 零值可用：结构体尽量支持零值直接使用，必要时提供 `NewXxx` 构造函数。
- [ ] 接收者类型一致：同一类型的所有方法统一用值接收者或指针接收者。
- [ ] `go fmt` / `goimports` 格式化，import 分组（标准库 / 第三方 / 本地）。
- [ ] 不暴露未导出字段的导出方法（避免逃逸封装）。
- [ ] 测试覆盖核心逻辑与边界条件，测试名遵循 `TestXxx_场景` 规范。

---

## 六、提交前自检

- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 通过
- [ ] `golangci-lint run` 通过
- [ ] `go test ./... -race -cover -timeout=180s` 通过，覆盖率不下降
- [ ] `govulncheck ./...` 无已知高危漏洞
- [ ] 无调试用的 `fmt.Println` / `log.Printf` 残留（使用 `slog`）
- [ ] 提交信息遵循约定式提交（`feat:` / `fix:` / `refactor:` 等）

---

> **评审礼仪**：对事不对人；指出问题时尽量给出建议方案；对优秀实现给予肯定。
> 被 review 者应将评审意见视为改进机会，而非个人批评。
