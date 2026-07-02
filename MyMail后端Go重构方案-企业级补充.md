# MyMail 后端 Go 重构方案 - 企业级补充

> **文档版本**：v1.1（补充）
> **制定日期**：2026-07-03
> **定位**：小型项目，企业级标准
> **关系**：本文档是对《MyMail后端Go重构完整方案.md》的补充，补齐企业级治理要求

---

## 目录

- [一、差距分析：原方案 vs 企业级标准](#一差距分析原方案-vs-企业级标准)
- [二、可观测性体系](#二可观测性体系)
- [三、安全合规增强](#三安全合规增强)
- [四、API 治理](#四api-治理)
- [五、高可用与韧性设计](#五高可用与韧性设计)
- [六、数据治理](#六数据治理)
- [七、CI/CD 与发布工程](#七cicd-与发布工程)
- [八、文档与知识管理](#八文档与知识管理)
- [九、测试体系增强](#九测试体系增强)
- [十、运维与 SRE](#十运维与-sre)
- [十一、项目结构补强](#十一项目结构补强)
- [十二、配置管理增强](#十二配置管理增强)
- [十三、关键参数清单补充](#十三关键参数清单补充)

---

## 一、差距分析：原方案 vs 企业级标准

### 1.1 评估矩阵

| 维度 | 原方案覆盖度 | 企业级标准要求 | 差距 |
|---|---|---|---|
| 功能正确性 | ✅ 100% | 100% | 无 |
| Bug 修复 | ✅ 100% | 100% | 无 |
| 单元/集成测试 | ✅ 80% 覆盖 | 80%+ + 性能 + 混沌 | 缺性能基线、混沌工程 |
| 日志 | ⚠️ slog 结构化 | 结构化 + trace_id 串联 + 审计日志 | 缺 trace_id、审计日志 |
| 指标监控 | ❌ 仅 /health | Prometheus + Grafana 看板 | 缺完整指标体系 |
| 分布式追踪 | ❌ 无 | OpenTelemetry | 完全缺失 |
| API 文档 | ❌ 仅 Markdown | OpenAPI 3.0 + 自动生成 | 完全缺失 |
| API 版本管理 | ❌ 无 | v1/v2 共存策略 | 完全缺失 |
| 请求 ID 透传 | ❌ 无 | X-Request-ID 全链路 | 完全缺失 |
| 幂等性 | ❌ 无 | 关键操作幂等 | 完全缺失 |
| 审计日志 | ❌ 无 | who/what/when/where/result | 完全缺失 |
| 密钥管理 | ⚠️ .env | 轮换机制 + 多源加载 | 缺轮换 |
| 依赖扫描 | ❌ 无 | Snyk/Dependabot + 准入 | 完全缺失 |
| 容器扫描 | ❌ 无 | Trivy + 准入 | 完全缺失 |
| 健康检查 | ⚠️ 单一 /health | liveness + readiness 分离 | 缺分离 |
| 熔断/重试 | ❌ 无 | 出站调用必备 | 完全缺失 |
| 优雅降级 | ❌ 无 | 依赖故障时降级 | 完全缺失 |
| 数据备份 | ⚠️ 单脚本 | 全量+增量+异地+演练 | 缺策略 |
| 数据归档 | ❌ 无 | 邮件生命周期管理 | 完全缺失 |
| SLO/SLA | ❌ 无 | 错误预算 + 告警 | 完全缺失 |
| Runbook | ❌ 无 | 故障处理手册 | 完全缺失 |
| ADR | ❌ 无 | 架构决策记录 | 完全缺失 |
| 代码规范 | ⚠️ golangci | + 评审清单 + PR 模板 | 缺规范文档 |
| 特性开关 | ❌ 无 | Feature Flag | 完全缺失 |
| 多环境配置 | ❌ 无 | dev/staging/prod | 完全缺失 |

### 1.2 核心补充方向

原方案是"能跑、Bug 修复"级别，企业级标准需要补齐为"可观测、可治理、可运维、可审计"。

**小型项目企业级标准的核心特征**：
- **不追求**：微服务、Kubernetes、多机房、Service Mesh（过度设计）
- **必须做到**：可观测、可审计、可回滚、可告警、文档齐全、安全合规

---

## 二、可观测性体系

### 2.1 三支柱架构

```
┌─────────────────────────────────────────────────────────┐
│                      应用进程                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐               │
│  │  日志    │  │  指标    │  │  追踪    │               │
│  │  slog    │  │  prom    │  │  otel    │               │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘               │
│       │             │             │                      │
└───────┼─────────────┼─────────────┼──────────────────────┘
        │             │             │
        ▼             ▼             ▼
   ┌─────────┐  ┌─────────┐  ┌─────────┐
   │  文件    │  │ Push    │  │  OTLP   │
   │  / var   │  │ gateway │  │  export │
   └────┬────┘  └────┬────┘  └────┬────┘
        │            │            │
        ▼            ▼            ▼
   ┌─────────┐  ┌─────────┐  ┌─────────┐
   │  Loki   │  │Prometheus│  │  Jaeger │
   │  日志    │  │  指标    │  │  追踪    │
   └─────────┘  └────┬────┘  └─────────┘
                     │
                     ▼
              ┌─────────────┐
              │   Grafana   │  ← 统一看板
              └─────────────┘
```

### 2.2 日志增强：trace_id 串联

```go
// internal/logger/logger.go
package logger

import (
    "context"
    "log/slog"
    "os"

    "go.opentelemetry.io/otel/trace"
)

type traceHandler struct {
    slog.Handler
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
    // 注入 trace_id 和 span_id
    if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
        sc := span.SpanContext()
        r.AddAttrs(
            slog.String("trace_id", sc.TraceID().String()),
            slog.String("span_id", sc.SpanID().String()),
        )
    }
    // 注入 request_id（从 context 取）
    if reqID, ok := ctx.Value(RequestIDKey).(string); ok {
        r.AddAttrs(slog.String("request_id", reqID))
    }
    return h.Handler.Handle(ctx, r)
}

var L *slog.Logger

func Init(level string, format string) {
    var lvl slog.Level
    switch level {
    case "debug": lvl = slog.LevelDebug
    case "warn":  lvl = slog.LevelWarn
    case "error": lvl = slog.LevelError
    default:      lvl = slog.LevelInfo
    }

    var h slog.Handler
    opts := &slog.HandlerOptions{Level: lvl, AddSource: true}
    if format == "json" {
        h = slog.NewJSONHandler(os.Stdout, opts)
    } else {
        h = slog.NewTextHandler(os.Stdout, opts)
    }

    L = slog.New(&traceHandler{Handler: h})
}
```

### 2.3 指标体系（Prometheus）

```go
// internal/metrics/metrics.go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // HTTP 指标
    HTTPRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mymail_http_requests_total",
            Help: "Total HTTP requests",
        },
        []string{"method", "path", "status"},
    )
    HTTPRequestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "mymail_http_request_duration_seconds",
            Help:    "HTTP request duration",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method", "path"},
    )
    HTTPRequestsInFlight = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "mymail_http_requests_in_flight",
            Help: "Current in-flight HTTP requests",
        },
    )

    // 业务指标
    MailsReceivedTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mymail_mails_received_total",
            Help: "Total mails received via SMTP",
        },
        []string{"folder", "spam_action"},  // folder: inbox/junk, action: passed/flagged/rejected
    )
    MailsSentTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mymail_mails_sent_total",
            Help: "Total mails sent",
        },
        []string{"status"},  // success/failed/queued
    )
    MailsSentDuration = promauto.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "mymail_mail_send_duration_seconds",
            Help:    "Mail send duration",
            Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
        },
    )
    QueueDepth = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "mymail_queue_depth",
            Help: "Current mail queue depth",
        },
    )
    QueueProcessingTime = promauto.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "mymail_queue_process_duration_seconds",
            Help:    "Queue item processing time",
            Buckets: prometheus.DefBuckets,
        },
    )

    // SMTP 指标
    SMTPConnectionsActive = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "mymail_smtp_connections_active",
            Help: "Active SMTP connections",
        },
    )
    SMTPConnectionsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mymail_smtp_connections_total",
            Help: "Total SMTP connections",
        },
        []string{"result"},  // accepted/rejected/greylisted
    )

    // 资源指标
    DBConnectionsActive = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "mymail_db_connections_active",
            Help: "Active DB connections",
        },
    )
    StorageUsedBytes = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "mymail_storage_used_bytes",
            Help: "Storage used by user",
        },
        []string{"user_id"},
    )

    // 认证指标
    AuthAttemptsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mymail_auth_attempts_total",
            Help: "Authentication attempts",
        },
        []string{"method", "result"},  // method: jwt/apikey, result: success/failure/locked
    )
)
```

**指标埋点示例**：

```go
// internal/httpapi/middleware/metrics.go
func Metrics() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        metrics.HTTPRequestsInFlight.Inc()
        defer metrics.HTTPRequestsInFlight.Dec()

        c.Next()

        status := strconv.Itoa(c.Writer.Status())
        path := c.FullPath()  // 用路由模板，避免 cardinality 爆炸
        metrics.HTTPRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
        metrics.HTTPRequestDuration.WithLabelValues(c.Request.Method, path).
            Observe(time.Since(start).Seconds())
    }
}
```

### 2.4 分布式追踪（OpenTelemetry）

```go
// internal/tracing/tracing.go
package tracing

import (
    "context"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/resource"
    "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

func Init(ctx context.Context, endpoint string, serviceName string) (func(), error) {
    exporter, err := otlptracehttp.New(ctx,
        otlptracehttp.WithEndpoint(endpoint),
        otlptracehttp.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName(serviceName),
            semconv.ServiceVersion("1.0.0"),
        )),
        trace.WithSampler(trace.TraceIDRatioBased(0.1)),  // 10% 采样
    )
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})

    return func() { tp.Shutdown(ctx) }, nil
}
```

**HTTP 中间件**：

```go
// internal/httpapi/middleware/tracing.go
import "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

router.Use(otelgin.Middleware("mymail"))
```

**DAO 层 span**：

```go
// internal/storage/dao/message.go
func (d *MessageDAO) GetByID(ctx context.Context, id int64) (*domain.Message, error) {
    ctx, span := otel.Tracer("dao").Start(ctx, "MessageDAO.GetByID")
    defer span.End()
    span.SetAttributes(attribute.Int64("message.id", id))

    // ...查询逻辑
}
```

### 2.5 审计日志（关键！企业级必备）

```go
// internal/audit/audit.go
package audit

type Event struct {
    Timestamp   time.Time `json:"timestamp"`
    ActorType   string    `json:"actor_type"`   // user/admin/system/api_key
    ActorID     int64     `json:"actor_id"`
    ActorIP     string    `json:"actor_ip"`
    Action      string    `json:"action"`       // mail.send/user.create/rule.delete/...
    ResourceType string   `json:"resource_type"` // mail/user/rule/api_key
    ResourceID  string    `json:"resource_id"`
    Result      string    `json:"result"`       // success/failure
    Detail      string    `json:"detail,omitempty"`
    RequestID   string    `json:"request_id"`
}

type Logger interface {
    Log(ctx context.Context, event Event) error
}

// 实现：写入独立 SQLite 表 + 文件（双写）
type DualLogger struct {
    db   *sql.DB
    file *os.File
}

func (l *DualLogger) Log(ctx context.Context, e Event) error {
    e.Timestamp = time.Now().UTC()
    // 1. 写数据库
    _, err := l.db.ExecContext(ctx,
        `INSERT INTO audit_log (timestamp, actor_type, actor_id, actor_ip, action, resource_type, resource_id, result, detail, request_id) VALUES (?,?,?,?,?,?,?,?,?,?)`,
        e.Timestamp, e.ActorType, e.ActorID, e.ActorIP, e.Action,
        e.ResourceType, e.ResourceID, e.Result, e.Detail, e.RequestID,
    )
    if err != nil {
        return err
    }
    // 2. 写文件（JSON Lines）
    json.NewEncoder(l.file).Encode(e)
    return nil
}
```

**审计事件埋点**：

```go
// internal/httpapi/handler/mail.go
func (h *MailHandler) SendMail(c *gin.Context) {
    userID := c.GetInt64("user_id")
    reqID := c.GetString("request_id")
    ip := c.ClientIP()

    // ...发送逻辑...

    // 审计日志
    h.audit.Log(c.Request.Context(), audit.Event{
        ActorType:    "user",
        ActorID:      userID,
        ActorIP:      ip,
        Action:       "mail.send",
        ResourceType: "mail",
        ResourceID:   strconv.FormatInt(mailID, 10),
        Result:       "success",
        RequestID:    reqID,
        Detail:       fmt.Sprintf("to=%s subject=%s", req.To, req.Subject),
    })
}
```

**必须审计的操作**：
- 用户登录/登出（成功+失败）
- 密码修改
- 管理员操作（创建/删除用户、重置密码、改配置）
- API Key 创建/删除
- 规则创建/修改/删除
- 邮件发送
- 邮件删除（批量）
- 配置修改

### 2.6 Grafana 看板

提供预置看板 JSON：

```
deployments/grafana/dashboards/
├── mymail-overview.json       # 总览（QPS/延迟/错误率/队列深度）
├── mymail-smtp.json           # SMTP 详情（连接/灰名单/SPF/DNSBL）
├── mymail-business.json       # 业务（收发量/存储/用户活跃）
└── mymail-infrastructure.json # 基础设施（DB/CPU/内存/磁盘）
```

### 2.7 告警规则

```yaml
# deployments/prometheus/alerts.yml
groups:
  - name: mymail
    rules:
      - alert: HighErrorRate
        expr: |
          rate(mymail_http_requests_total{status=~"5.."}[5m])
          / rate(mymail_http_requests_total[5m]) > 0.05
        for: 5m
        labels: { severity: critical }
        annotations:
          summary: "HTTP 5xx 错误率 > 5%"
          description: "{{ $value }} errors/sec on {{ $labels.path }}"

      - alert: SMTPDown
        expr: mymail_smtp_connections_active == 0 for 5m
        labels: { severity: critical }
        annotations:
          summary: "SMTP 服务无连接（可能宕机）"

      - alert: QueueBacklog
        expr: mymail_queue_depth > 100
        for: 10m
        labels: { severity: warning }
        annotations:
          summary: "发送队列积压 > 100"

      - alert: DBConnectionExhausted
        expr: mymail_db_connections_active > 0.9 * 10
        labels: { severity: critical }
        annotations:
          summary: "DB 连接接近上限"

      - alert: HighAuthFailureRate
        expr: |
          rate(mymail_auth_attempts_total{result="failure"}[5m]) > 1
        for: 5m
        labels: { severity: warning }
        annotations:
          summary: "认证失败率高，可能有暴力破解"
```

---

## 三、安全合规增强

### 3.1 密钥管理

#### 3.1.1 多源加载

```go
// internal/config/secret.go
package config

import (
    "os"
    "strings"
)

// SecretLoader 支持多源加载密钥
// 优先级：环境变量 > 文件（如 /run/secrets/jwt_secret）> Vault
type SecretLoader struct {
    useVault bool
    vaultURL string
}

func (s *SecretLoader) Load(key string) (string, error) {
    // 1. 环境变量
    if v := os.Getenv(key); v != "" {
        return v, nil
    }

    // 2. 文件（Docker secret / Kubernetes secret）
    if v, err := os.ReadFile("/run/secrets/" + strings.ToLower(key)); err == nil {
        return strings.TrimSpace(string(v)), nil
    }

    // 3. Vault（可选）
    if s.useVault {
        // 调用 Vault API
    }

    return "", fmt.Errorf("secret %s not found", key)
}
```

#### 3.1.2 密钥轮换

```go
// internal/crypto/jwt.go
package crypto

import (
    "sync"
    "time"
    "golang-jwt/jwt/v5"
)

type KeyRotator struct {
    current  []byte
    previous []byte  // 旧密钥，仍可用于校验（宽限期）
    rotatedAt time.Time
    interval time.Duration
    mu sync.RWMutex
}

func NewKeyRotator(initialSecret []byte, rotationInterval time.Duration) *KeyRotator {
    return &KeyRotator{
        current:   initialSecret,
        interval:  rotationInterval,
        rotatedAt: time.Now(),
    }
}

// Verify 支持当前 + 旧密钥双校验
func (k *KeyRotator) Verify(tokenString string) (*Claims, error) {
    k.mu.RLock()
    defer k.mu.RUnlock()

    // 先用当前密钥
    claims, err := k.verifyWithKey(tokenString, k.current)
    if err == nil {
        return claims, nil
    }

    // 用旧密钥（宽限期内）
    if k.previous != nil && time.Since(k.rotatedAt) < k.interval {
        return k.verifyWithKey(tokenString, k.previous)
    }

    return nil, err
}

// Rotate 轮换密钥（需管理员触发或定时任务）
func (k *KeyRotator) Rotate(newSecret []byte) {
    k.mu.Lock()
    defer k.mu.Unlock()
    k.previous = k.current
    k.current = newSecret
    k.rotatedAt = time.Now()
}
```

**轮换策略**：
- JWT_SECRET：每 90 天轮换，旧密钥保留 7 天宽限期
- ADMIN_PASSWORD：每 30 天提醒
- API Key：用户可手动轮换，旧 Key 24 小时宽限

### 3.2 依赖扫描

```yaml
# .github/workflows/security.yml
name: Security
on:
  push:
    branches: [main, develop]
  pull_request:
  schedule:
    - cron: '0 0 * * 0'  # 每周日扫描

jobs:
  go-vuln:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - name: Run govulncheck
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./... || exit 1

  gosec:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: securego/gosec@master
        with:
          args: -severity medium -confidence medium ./...

  trivy-fs:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Trivy filesystem scan
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: fs
          scan-ref: .
          severity: CRITICAL,HIGH
          exit-code: 1

  trivy-image:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build image
        run: docker build -t mymail:scan -f deployments/docker/Dockerfile .
      - name: Trivy image scan
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: mymail:scan
          severity: CRITICAL,HIGH
          exit-code: 1

  npm-audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22' }
      - working-directory: mymail-vue
        run: |
          npm ci
          npm audit --audit-level=high
```

### 3.3 准入策略

```yaml
# .github/workflows/gate.yml
name: Quality Gate
on: [pull_request]

jobs:
  gate:
    runs-on: ubuntu-latest
    needs: [lint, test, gosec, govulncheck, trivy]
    if: always()
    steps:
      - name: Check all jobs passed
        run: |
          for job in lint test gosec govulncheck trivy; do
            if [[ "${{ needs.${job}.result }}" != "success" ]]; then
              echo "Job $job failed, blocking merge"
              exit 1
            fi
          done
```

### 3.4 GDPR / 数据保护

#### 3.4.1 用户数据导出

```go
// GET /api/auth/export-data
func (h *AuthHandler) ExportData(c *gin.Context) {
    userID := c.GetInt64("user_id")
    // 导出该用户所有数据为 zip：邮件 + 附件 + 规则 + API Key 信息
    // 返回下载链接
}
```

#### 3.4.2 用户数据删除（被遗忘权）

```go
// DELETE /api/auth/account
func (h *AuthHandler) DeleteAccount(c *gin.Context) {
    userID := c.GetInt64("user_id")
    // 1. 要求二次确认（密码）
    // 2. 7 天宽限期（标记为待删除）
    // 3. 7 天后实际删除：邮件、附件、规则、API Key、登录记录
    // 4. 审计日志保留（脱敏）
}
```

#### 3.4.3 PII 脱敏

```go
// internal/audit/audit.go
func sanitizePII(s string) string {
    // 邮箱地址脱敏：user@domain.com → u***@domain.com
    if strings.Contains(s, "@") {
        parts := strings.SplitN(s, "@", 2)
        if len(parts[0]) > 1 {
            parts[0] = string(parts[0][0]) + "***"
        }
        return strings.Join(parts, "@")
    }
    return s
}
```

### 3.5 安全头部补全

```go
// internal/httpapi/middleware/security.go
func Security() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Header("X-Content-Type-Options", "nosniff")
        c.Header("X-Frame-Options", "DENY")
        c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
        c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
        c.Header("X-XSS-Protection", "1; mode=block")
        c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
        c.Header("Content-Security-Policy",
            "default-src 'self'; "+
            "script-src 'self'; "+
            "style-src 'self' 'unsafe-inline'; "+
            "img-src 'self' data: blob:; "+
            "connect-src 'self' wss:; "+
            "frame-ancestors 'none'; "+
            "base-uri 'self'")
        c.Next()
    }
}
```

---

## 四、API 治理

### 4.1 OpenAPI 文档

```go
// internal/httpapi/openapi/openapi.go
import "github.com/swaggo/swag"

// 用 swaggo/swag 从注解生成 OpenAPI 3.0
// @title MyMail API
// @version 1.0
// @description 自托管邮件平台 API
// @host api.example.com
// @BasePath /api
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

// @Summary 发送邮件
// @Description 发送一封邮件，支持附件
// @Tags mail
// @Accept multipart/form-data
// @Produce json
// @Param to formData string true "收件人"
// @Param subject formData string true "主题"
// @Param body formData string true "正文"
// @Param attachments formData file false "附件"
// @Success 201 {object} dto.SendMailResponse
// @Failure 400 {object} dto.ErrorResponse
// @Failure 413 {object} dto.ErrorResponse
// @Security BearerAuth
// @Router /mail/send [post]
func (h *MailHandler) SendMail(c *gin.Context) { ... }
```

**生成命令**：

```bash
swag init -g cmd/mymail/main.go -o internal/httpapi/openapi --parseDependency --parseInternal
```

**端点**：`GET /api/docs` 提供 Swagger UI，`GET /api/openapi.json` 提供 OpenAPI JSON。

### 4.2 API 版本管理

```go
// internal/httpapi/router.go
func SetupRouter(...) *gin.Engine {
    r := gin.New()

    // v1 路由（当前版本）
    v1 := r.Group("/api/v1")
    {
        v1.POST("/send", ...)
        // 其他 v1 端点
    }

    // v2 路由（未来版本，向前兼容）
    // v2 := r.Group("/api/v2")

    // 兼容旧路径（无版本前缀，等同于 v1）
    r.POST("/api/v1/send", ...)
    // 同时保留 POST /api/auth/login 等无版本前缀路径

    return r
}
```

**版本策略**：
- 主版本在 URL 中：`/api/v1/...`、`/api/v2/...`
- 旧版本至少保留 2 个版本周期
- 弃用端点在响应头加 `Deprecation: true` 和 `Sunset: <date>`

### 4.3 请求 ID 透传

```go
// internal/httpapi/middleware/requestid.go
import "github.com/google/uuid"

func RequestID() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 优先从上游透传
        reqID := c.Request.Header.Get("X-Request-ID")
        if reqID == "" {
            reqID = uuid.NewString()
        }
        c.Set("request_id", reqID)
        c.Header("X-Request-ID", reqID)

        // 注入 context（供日志/追踪/审计使用）
        ctx := context.WithValue(c.Request.Context(), RequestIDKey, reqID)
        c.Request = c.Request.WithContext(ctx)

        c.Next()
    }
}
```

**所有日志/追踪/审计日志都包含 request_id，实现全链路串联**。

### 4.4 幂等性设计

```go
// internal/httpapi/middleware/idempotency.go
// 用于 POST 请求，避免重复提交
func Idempotency(cache *ttlcache.Cache) gin.HandlerFunc {
    return func(c *gin.Context) {
        if c.Request.Method != "POST" {
            c.Next()
            return
        }

        idempotencyKey := c.Request.Header.Get("Idempotency-Key")
        if idempotencyKey == "" {
            c.Next()
            return
        }

        // 检查是否已处理
        if cached, exists := cache.Get(idempotencyKey); exists {
            // 返回缓存的响应
            c.Header("X-Idempotent-Replay", "true")
            c.JSON(cached.Status, cached.Body)
            c.Abort()
            return
        }

        c.Next()

        // 缓存响应（24 小时）
        cache.Set(idempotencyKey, CachedResponse{
            Status: c.Writer.Status(),
            Body:   c.Writer.Bytes(),
        }, 24*time.Hour)
    }
}
```

**应用范围**：
- POST /api/mail/send
- POST /api/mail/save-draft
- POST /api/auth/register
- POST /api/admin/users
- POST /api/rules

### 4.5 速率限制（分布式）

```go
// internal/httpapi/middleware/ratelimit.go
import "github.com/ulule/limiter/v3"
import "github.com/ulule/limiter/v3/drivers/store/memory"

func RateLimit(rate string) gin.HandlerFunc {
    store := memory.NewStore()
    rate, _ := limiter.NewRateFromFormatted(rate)  // "100-M" = 100 per minute
    instance := limiter.New(store, rate)
    return mgin.NewMiddleware(instance)
}
```

**精细化限流策略**：

```go
// 不同端点不同限流
router.POST("/api/auth/login", rateLimit("5-M"), authHandler.Login)
router.POST("/api/auth/register", rateLimit("3-H"), authHandler.Register)
router.POST("/api/mail/send", rateLimit("10-M"), mailHandler.SendMail)
router.POST("/api/v1/send", apiKeyRateLimit, apiv1Handler.Send)  // API Key 独立配额
router.GET("/api/admin/*", adminRateLimit, adminHandler...)
```

---

## 五、高可用与韧性设计

### 5.1 健康检查分离

```go
// internal/httpapi/handler/health.go
type HealthChecker interface {
    Check(ctx context.Context) error
}

type HealthHandler struct {
    db       HealthChecker
    smtp     HealthChecker
    queue    HealthChecker
    readonly bool  // 维护模式
}

// Liveness：进程是否存活（用于 K8s livenessProbe）
// GET /healthz
func (h *HealthHandler) Liveness(c *gin.Context) {
    c.JSON(200, gin.H{"status": "alive"})
}

// Readiness：是否准备好接收流量（用于 K8s readinessProbe）
// GET /readyz
func (h *HealthHandler) Readiness(c *gin.Context) {
    if h.readonly {
        c.JSON(503, gin.H{"status": "maintaining"})
        return
    }

    ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
    defer cancel()

    checks := map[string]string{}
    allOK := true

    if err := h.db.Check(ctx); err != nil {
        checks["db"] = "down"
        allOK = false
    } else {
        checks["db"] = "up"
    }

    if err := h.queue.Check(ctx); err != nil {
        checks["queue"] = "down"
        allOK = false
    } else {
        checks["queue"] = "up"
    }

    if !allOK {
        c.JSON(503, gin.H{"status": "not ready", "checks": checks})
        return
    }

    c.JSON(200, gin.H{"status": "ready", "checks": checks})
}

// 启动期：用于启动探针，判断应用是否已启动
// GET /startupz
func (h *HealthHandler) Startup(c *gin.Context) {
    if !h.started {
        c.JSON(503, gin.H{"status": "starting"})
        return
    }
    c.JSON(200, gin.H{"status": "started"})
}
```

### 5.2 熔断器

```go
// internal/resilience/circuit_breaker.go
import "github.com/sony/gobreaker"

// 用于出站 SMTP 调用
func NewSMTPBreaker() *gobreaker.CircuitBreaker {
    return gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        "smtp-outbound",
        MaxRequests: 5,                  // 半开状态下最大请求数
        Interval:    60 * time.Second,   // 统计窗口
        Timeout:     30 * time.Second,   // 熔断后多久尝试半开
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            // 失败率 > 60% 时熔断
            failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
            return counts.Requests > 10 && failureRatio > 0.6
        },
        OnStateChange: func(name string, from, to gobreaker.State) {
            slog.Warn("circuit breaker state change",
                "name", name, "from", from, "to", to)
        },
    })
}

// 使用
func (s *SMTPSender) Send(ctx context.Context, req *SendRequest) error {
    _, err := s.breaker.Execute(func() (interface{}, error) {
        return nil, s.doSend(ctx, req)
    })
    if errors.Is(err, gobreaker.ErrOpenState) {
        // 熔断中，入队列等待
        return s.enqueueForRetry(req)
    }
    return err
}
```

### 5.3 重试策略

```go
// internal/resilience/retry.go
import "github.com/cenkalti/backoff/v4"

func RetryWithBackoff(ctx context.Context, op func() error) error {
    b := backoff.NewExponentialBackOff()
    b.InitialInterval = 1 * time.Second
    b.MaxInterval = 30 * time.Second
    b.MaxElapsedTime = 5 * time.Minute

    b = backoff.WithContext(b, ctx)
    return backoff.Retry(op, b)
}

// 使用
func (s *SMTPSender) SendWithRetry(ctx context.Context, req *SendRequest) error {
    return RetryWithBackoff(ctx, func() error {
        return s.Send(ctx, req)
    })
}
```

### 5.4 优雅降级

```go
// internal/service/mail_service.go
type MailService struct {
    db          dao.MessageDAO
    spamFilter  *spam.Filter
    wsHub       *ws.Hub
    features    *FeatureFlags
}

func (s *MailService) ReceiveMail(ctx context.Context, msg *domain.Message) error {
    // 反垃圾检查（可降级）
    if s.features.SpamFilterEnabled {
        result, err := s.spamFilter.Check(msg.SenderIP, msg.FromAddr, msg.HeloDomain)
        if err != nil {
            // 反垃圾失败不阻断投递，记录日志
            slog.Warn("spam filter failed, falling back to pass", "error", err)
            // 默认放行
        } else {
            msg.SpamScore = result.Score
            msg.SpamReasons = strings.Join(result.Reasons, ",")
            if result.Action == "rejected" {
                return ErrSpamRejected
            }
            if result.Action == "flagged" {
                msg.Folder = "Junk"
            }
        }
    }

    // WebSocket 通知（可降级）
    if s.features.WSNotificationEnabled {
        s.wsHub.NotifyNewMail(msg.UserID, ...)
        // 通知失败不影响投递
    }

    return s.db.Create(ctx, msg)
}
```

### 5.5 特性开关

```go
// internal/config/features.go
package config

type FeatureFlags struct {
    SpamFilterEnabled       bool `env:"FEATURE_SPAM_FILTER" envDefault:"true"`
    WSNotificationEnabled   bool `env:"FEATURE_WS_NOTIFY" envDefault:"true"`
    GreylistEnabled         bool `env:"FEATURE_GREYLIST" envDefault:"true"`
    APIKeyEnabled           bool `env:"FEATURE_API_KEY" envDefault:"true"`
    RulesEnabled            bool `env:"FEATURE_RULES" envDefault:"true"`
    AuditLogEnabled         bool `env:"FEATURE_AUDIT" envDefault:"true"`
    MaintenanceMode         bool `env:"FEATURE_MAINTENANCE" envDefault:"false"`
    RegistrationEnabled     bool `env:"FEATURE_REGISTRATION" envDefault:"true"`
    MailExportEnabled       bool `env:"FEATURE_MAIL_EXPORT" envDefault:"true"`
}

// 支持运行时切换（通过 admin API 或 SIGHUP 信号重载配置）
func (f *FeatureFlags) Reload() error {
    // 重新读取配置
}
```

**管理端点**：

```go
// GET /api/admin/features
// PUT /api/admin/features
// 仅管理员可访问
```

---

## 六、数据治理

### 6.1 备份策略

```bash
# scripts/backup.sh
#!/bin/bash
# 企业级备份：全量 + 增量 + 异地 + 演练

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-./backups}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"
REMOTE_BUCKET="${REMOTE_BUCKET:-}"  # S3/MinIO bucket
DB_PATH="${DB_PATH:-./data/mymail.db}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
WEEKDAY=$(date +%u)  # 1=Monday

mkdir -p "$BACKUP_DIR"

# 周日做全量，其他日做增量
if [ "$WEEKDAY" = "7" ]; then
    BACKUP_TYPE="full"
    # SQLite 全量备份（VACUUM INTO 不锁库）
    sqlite3 "$DB_PATH" "VACUUM INTO '$BACKUP_DIR/full_$TIMESTAMP.db'"
else
    BACKUP_TYPE="incremental"
    # 用 SQLite 的 WAL 增量（或简单全量，因为 SQLite 不支持真正增量）
    sqlite3 "$DB_PATH" "VACUUM INTO '$BACKUP_DIR/incr_$TIMESTAMP.db'"
fi

# 备份 Maildir（用 rsync 增量）
rsync -a --delete data/maildir/ "$BACKUP_DIR/maildir_$TIMESTAMP/"

# 备份附件
tar -czf "$BACKUP_DIR/attachments_$TIMESTAMP.tar.gz" -C data/ attachments/ 2>&1 | tee -a "$BACKUP_DIR/backup.log"

# 加密备份（含敏感数据）
if [ -f .env ]; then
    tar -czf - .env | gpg --symmetric --cipher-algo AES256 \
        --batch --passphrase-file /run/secrets/backup_passphrase \
        -o "$BACKUP_DIR/env_$TIMESTAMP.gpg"
fi

# 生成备份清单
cat > "$BACKUP_DIR/manifest_$TIMESTAMP.json" <<EOF
{
    "timestamp": "$TIMESTAMP",
    "type": "$BACKUP_TYPE",
    "files": ["$(ls "$BACKUP_DIR"/*"$TIMESTAMP"* | sed "s|$BACKUP_DIR/||g" | tr '\n' ',' | sed 's/,$//')"],
    "db_size": "$(du -h "$DB_PATH" | cut -f1)",
    "maildir_size": "$(du -sh data/maildir | cut -f1)"
}
EOF

# 上传到异地（S3 兼容存储）
if [ -n "$REMOTE_BUCKET" ]; then
    for f in "$BACKUP_DIR"/*"$TIMESTAMP"*; do
        aws s3 cp "$f" "s3://$REMOTE_BUCKET/$(basename "$f")" --no-progress
    done
fi

# 清理过期备份
find "$BACKUP_DIR" -name "*.db" -mtime +$RETENTION_DAYS -delete
find "$BACKUP_DIR" -name "*.tar.gz" -mtime +$RETENTION_DAYS -delete
find "$BACKUP_DIR" -name "*.gpg" -mtime +$RETENTION_DAYS -delete

# 校验最新备份完整性
LATEST_DB=$(ls -t "$BACKUP_DIR"/full_*.db "$BACKUP_DIR"/incr_*.db 2>/dev/null | head -1)
if [ -n "$LATEST_DB" ]; then
    INTEGRITY=$(sqlite3 "$LATEST_DB" "PRAGMA integrity_check;" 2>&1)
    if [ "$INTEGRITY" != "ok" ]; then
        echo "[ALERT] Backup integrity check failed: $INTEGRITY" >&2
        exit 1
    fi
    echo "[INFO] Backup integrity verified"
fi

echo "[INFO] Backup completed: type=$BACKUP_TYPE, timestamp=$TIMESTAMP"
```

### 6.2 数据归档（邮件生命周期）

```go
// internal/service/archive_service.go
package service

type ArchiveService struct {
    db       *sql.DB
    cfg      *config.Config
}

// ArchiveOldMails 归档超过 N 天的已删除邮件
func (s *ArchiveService) ArchiveOldMails(ctx context.Context) error {
    cutoff := time.Now().AddDate(0, 0, -s.cfg.Mail.ArchiveAfterDays)

    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil { return err }
    defer tx.Rollback()

    // 1. 查询待归档邮件
    rows, err := tx.QueryContext(ctx,
        `SELECT id, user_id, FROM messages
         WHERE folder = 'Trash' AND received_at < ? AND is_deleted = 1`,
        cutoff,
    )
    if err != nil { return err }
    defer rows.Close()

    var toArchive []int64
    for rows.Next() {
        var id int64
        var userID int64
        rows.Scan(&id, &userID)
        toArchive = append(toArchive, id)
    }

    // 2. 移动到归档存储（独立 SQLite 文件 archive.db）
    for _, id := range toArchive {
        // 复制到 archive.db
        // 删除原记录
        // 移动附件文件到 archive/ 目录
    }

    return tx.Commit()
}
```

**策略**：
- 已删除邮件 30 天后归档到 archive.db
- 归档邮件 1 年后永久删除（可配置）
- 用户可查询归档邮件（独立端点 `GET /api/mail/archive`）

### 6.3 数据库维护

```go
// internal/service/maintenance_service.go
// 定时任务：每天凌晨 3 点执行

func (s *MaintenanceService) Daily(ctx context.Context) error {
    // 1. 清理过期灰名单记录
    if err := s.greylistDAO.Cleanup(ctx); err != nil {
        slog.Error("greylist cleanup failed", "error", err)
    }

    // 2. 清理过期发送队列
    if err := s.queueDAO.CleanupOld(ctx, 30*24*time.Hour); err != nil {
        slog.Error("queue cleanup failed", "error", err)
    }

    // 3. 清理过期审计日志（保留 1 年）
    if err := s.auditDAO.DeleteOlderThan(ctx, 365*24*time.Hour); err != nil {
        slog.Error("audit cleanup failed", "error", err)
    }

    // 4. 更新存储配额统计（防止漂移）
    if err := s.recalculateStorageUsage(ctx); err != nil {
        slog.Error("storage recalc failed", "error", err)
    }

    // 5. ANALYZE 更新统计信息
    if _, err := s.db.ExecContext(ctx, "ANALYZE"); err != nil {
        slog.Error("ANALYZE failed", "error", err)
    }

    return nil
}

func (s *MaintenanceService) Weekly(ctx context.Context) error {
    // VACUUM 重组数据库（每周日凌晨）
    if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
        slog.Error("VACUUM failed", "error", err)
    }
    return nil
}
```

### 6.4 数据完整性校验

```go
// scripts/check-integrity.go
// 定期运行，检查数据一致性

func CheckIntegrity(db *sql.DB) error {
    // 1. 检查孤儿邮件（user_id 不存在）
    var orphanMails int
    db.QueryRow(`SELECT COUNT(*) FROM messages m LEFT JOIN users u ON m.user_id = u.id WHERE u.id IS NULL`).Scan(&orphanMails)
    if orphanMails > 0 {
        log.Printf("[WARN] %d orphan mails found", orphanMails)
    }

    // 2. 检查孤儿附件
    var orphanAttachs int
    db.QueryRow(`SELECT COUNT(*) FROM attachments a LEFT JOIN messages m ON a.message_id = m.id WHERE m.id IS NULL`).Scan(&orphanAttachs)
    if orphanAttachs > 0 {
        log.Printf("[WARN] %d orphan attachments found", orphanAttachs)
    }

    // 3. 检查存储配额漂移
    rows, _ := db.Query(`SELECT u.id, u.storage_used, COALESCE(SUM(m.size_bytes), 0) AS actual FROM users u LEFT JOIN messages m ON u.id = m.user_id GROUP BY u.id HAVING u.storage_used != actual`)
    for rows.Next() {
        var userID, stored, actual int64
        rows.Scan(&userID, &stored, &actual)
        if abs(stored-actual) > 1024 {  // 容差 1KB
            log.Printf("[WARN] storage mismatch: user=%d stored=%d actual=%d", userID, stored, actual)
        }
    }

    return nil
}
```

---

## 七、CI/CD 与发布工程

### 7.1 完整 CI 流水线

```yaml
# .github/workflows/ci.yml
name: CI
on:
  push:
    branches: [main, develop]
  pull_request:

jobs:
  # 1. 代码规范检查
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest
          args: --timeout=5m

  lint-frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22' }
      - working-directory: mymail-vue
        run: |
          npm ci
          npm run lint

  # 2. 单元测试 + 覆盖率
  test-unit:
    runs-on: ubuntu-latest
    strategy:
      matrix: { go-version: ['1.22', '1.23'] }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: ${{ matrix.go-version }} }
      - run: go test -race -coverprofile=coverage.out -covermode=atomic ./...
      - name: Check coverage
        run: |
          COV=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | tr -d '%')
          echo "Coverage: $COV%"
          if (( $(echo "$COV < 80" | bc -l) )); then
            echo "::error::Coverage $COV% < 80%"
            exit 1
          fi
      - uses: codecov/codecov-action@v4

  test-frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
      - working-directory: mymail-vue
        run: |
          npm ci
          npm run test:unit  # 需要 Vitest

  # 3. 集成测试
  test-integration:
    runs-on: ubuntu-latest
    needs: [test-unit]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go test -tags=integration ./tests/integration/...

  # 4. E2E 测试
  test-e2e:
    runs-on: ubuntu-latest
    needs: [test-integration]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Build Docker images
        run: docker compose -f deployments/docker/docker-compose.test.yml build
      - name: Start services
        run: docker compose -f deployments/docker/docker-compose.test.yml up -d
      - name: Wait for healthy
        run: |
          for i in {1..60}; do
            if curl -sf http://localhost:3000/readyz; then break; fi
            sleep 2
          done
      - name: Run E2E tests
        run: go test -tags=e2e ./tests/e2e/...
      - name: Dump logs on failure
        if: failure()
        run: docker compose -f deployments/docker/docker-compose.test.yml logs

  # 5. 安全扫描
  security:
    runs-on: ubuntu-latest
    needs: [lint]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: govulncheck
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./...
      - name: gosec
        uses: securego/gosec@master
      - name: Trivy filesystem
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: fs
          severity: CRITICAL,HIGH
          exit-code: 1
      - name: npm audit
        working-directory: mymail-vue
        run: |
          npm ci
          npm audit --audit-level=high

  # 6. 构建
  build:
    runs-on: ubuntu-latest
    needs: [test-unit, security]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Build binaries
        run: |
          # 多平台
          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/mymail-linux-amd64 ./cmd/mymail
          CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o dist/mymail-linux-arm64 ./cmd/mymail
          # Docker
          docker build -t mymail:${{ github.sha }} -f deployments/docker/Dockerfile .
      - name: Trivy image scan
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: mymail:${{ github.sha }}
          severity: CRITICAL,HIGH
          exit-code: 1
      - uses: actions/upload-artifact@v4
        with:
          name: binaries
          path: dist/

  # 7. 质量门禁
  gate:
    runs-on: ubuntu-latest
    needs: [lint, lint-frontend, test-unit, test-frontend, test-integration, test-e2e, security, build]
    if: always()
    steps:
      - name: All checks must pass
        run: |
          for job in lint lint-frontend test-unit test-frontend test-integration test-e2e security build; do
            result="${{ needs.${job}.result }}"
            if [[ "$result" != "success" ]]; then
              echo "::error::Job $job failed ($result)"
              exit 1
            fi
          done
          echo "All checks passed"
```

### 7.2 发布工程

```yaml
# .github/workflows/release.yml
name: Release
on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5

      - name: Generate changelog
        id: changelog
        run: |
          # 从 commit 信息生成 changelog
          PREV_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
          if [ -z "$PREV_TAG" ]; then
            CHANGELOG=$(git log --pretty=format:"- %s" HEAD~50..HEAD)
          else
            CHANGELOG=$(git log --pretty=format:"- %s" $PREV_TAG..HEAD)
          fi
          echo "changelog<<EOF" >> $GITHUB_OUTPUT
          echo "$CHANGELOG" >> $GITHUB_OUTPUT
          echo "EOF" >> $GITHUB_OUTPUT

      - name: Build all platforms
        run: |
          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=${{ github.ref_name }}" -o dist/mymail-linux-amd64 ./cmd/mymail
          CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.version=${{ github.ref_name }}" -o dist/mymail-linux-arm64 ./cmd/mymail
          CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w -X main.version=${{ github.ref_name }}" -o dist/mymail-darwin-amd64 ./cmd/mymail
          CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=${{ github.ref_name }}" -o dist/mymail-windows-amd64.exe ./cmd/mymail

          # 生成 SHA256
          cd dist && sha256sum * > checksums.txt

      - name: Build and push Docker image
        run: |
          docker buildx build --platform linux/amd64,linux/arm64 \
            -t ghcr.io/${{ github.repository }}:${{ github.ref_name }} \
            -t ghcr.io/${{ github.repository }}:latest \
            --push \
            -f deployments/docker/Dockerfile \
            .

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          body: |
            ## Changelog
            ${{ steps.changelog.outputs.changelog }}

            ## Docker
            ```
            docker pull ghcr.io/${{ github.repository }}:${{ github.ref_name }}
            ```

            ## Checksums
            See `checksums.txt` for binary integrity verification.
          files: |
            dist/mymail-*
            dist/checksums.txt
```

### 7.3 部署策略：蓝绿 + 金丝雀

```yaml
# deployments/docker/docker-compose.blue.yml
# 蓝环境（当前生产）
services:
  app-blue:
    # ...
    ports: ["3001:3000"]

# deployments/docker/docker-compose.green.yml
# 绿环境（新版本）
services:
  app-green:
    # ...
    ports: ["3002:3000"]
```

```bash
# scripts/deploy.sh
#!/bin/bash
# 金丝雀发布：10% → 50% → 100%

NEW_VERSION=$1
CURRENT=$(docker inspect --format='{{.Config.Image}}' mymail-app 2>/dev/null | awk -F: '{print $2}')

# 1. 启动新版本（绿环境）
docker compose -f deployments/docker/docker-compose.green.yml up -d

# 2. 健康检查
for i in {1..60}; do
    if curl -sf http://localhost:3002/readyz; then break; fi
    sleep 2
done

# 3. Nginx 金丝雀 10%
sed -i 's/server app:3000;/server app-green:3000 weight=1;\nserver app-blue:3000 weight=9;/' config/nginx/upstream.conf
docker exec mymail-nginx nginx -s reload

# 4. 观察 5 分钟
sleep 300
if ! curl -sf http://localhost:3002/readyz; then
    echo "Rolling back: green unhealthy"
    docker compose -f deployments/docker/docker-compose.green.yml down
    exit 1
fi

# 5. 提升 50%
sed -i 's/weight=1;/weight=5;/; s/weight=9;/weight=5;/' config/nginx/upstream.conf
docker exec mymail-nginx nginx -s reload
sleep 300

# 6. 100% 切换
sed -i 's|server app-green:3000 weight=5;|server app-green:3000;|; /app-blue/d' config/nginx/upstream.conf
docker exec mymail-nginx nginx -s reload

# 7. 停止旧版本
docker stop mymail-app 2>/dev/null || true

# 8. 保留 24 小时可回滚
echo "New version $NEW_VERSION deployed. Old version kept for 24h rollback."
```

---

## 八、文档与知识管理

### 8.1 文档体系

```
docs/
├── README.md                    # 项目总览
├── ARCHITECTURE.md              # 架构设计（C4 Model）
├── API.md                       # API 概览
├── DEPLOY.md                    # 部署指南
├── OPERATIONS.md                # 运维手册
├── SECURITY.md                  # 安全说明
├── CONTRIBUTING.md              # 贡献指南
├── CHANGELOG.md                 # 变更日志
├── runbooks/                    # 故障处理手册
│   ├── smtp-down.md
│   ├── db-locked.md
│   ├── high-error-rate.md
│   └── disk-full.md
├── adr/                         # 架构决策记录
│   ├── 0001-use-go-over-node.md
│   ├── 0002-use-sqlite-not-postgres.md
│   ├── 0003-use-gin-not-echo.md
│   └── 0004-embed-frontend.md
└── api/                         # OpenAPI 生成产物
    ├── openapi.json
    └── openapi.yaml
```

### 8.2 ADR 模板

```markdown
# ADR-NNNN: 标题

## 状态
proposed | accepted | deprecated | superseded by [ADR-XXXX](...)

## 日期
YYYY-MM-DD

## 背景
描述问题背景、约束、假设

## 决策
我们选择...

## 理由
1. ...
2. ...

## 备选方案
- 方案 A：...
  - 优点：...
  - 缺点：...
- 方案 B：...

## 后果
- 正面影响：...
- 负面影响：...
- 风险：...
```

### 8.3 Runbook 模板

```markdown
# Runbook: SMTP 服务宕机

## 严重级别
P0 - 紧急

## 症状
- 告警：SMTPDown 触发
- 现象：外部邮件无法接收
- 验证：`nc -zv mail.example.com 25`

## 影响范围
- 所有入站邮件受影响
- 已发送邮件不受影响
- Web 邮箱不受影响

## 排查步骤

### 1. 检查 SMTP 进程
```bash
docker logs mymail-app --tail 100 | grep -i smtp
docker exec mymail-app ss -tlnp | grep :25
```

### 2. 检查端口占用
```bash
sudo lsof -i :25
```

### 3. 检查防火墙
```bash
sudo ufw status
sudo iptables -L -n | grep 25
```

## 处置方案

### 方案 A：重启 SMTP 服务
```bash
docker restart mymail-app
# 等待 30 秒后验证
sleep 30 && nc -zv mail.example.com 25
```

### 方案 B：回滚版本
```bash
./scripts/rollback.sh
```

## 升级条件
- 重启后 5 分钟内仍无响应 → 升级到 P0 + 通知所有用户
- 影响超过 30 分钟 → 启动应急响应

## 事后
- 24 小时内提交事故报告
- 更新本 Runbook
```

### 8.4 PR 模板

```markdown
## 变更类型
- [ ] feat: 新功能
- [ ] fix: Bug 修复
- [ ] refactor: 重构
- [ ] docs: 文档
- [ ] test: 测试
- [ ] chore: 构建/工具

## 变更说明
<!-- 简要描述 -->

## 关联 Issue
Closes #XXX

## 测试
- [ ] 单元测试通过
- [ ] 集成测试通过
- [ ] 手动验证

## 检查清单
- [ ] 代码符合规范（golangci-lint 通过）
- [ ] 无硬编码密钥
- [ ] 新增依赖已审查
- [ ] 文档已更新
- [ ] 测试覆盖率不下降
- [ ] Breaking change 已标注
```

### 8.5 代码评审清单

```markdown
## Code Review Checklist

### 通用
- [ ] 代码是否实现预期功能
- [ ] 命名是否清晰、一致
- [ ] 是否有死代码
- [ ] 是否有 magic number（应抽常量）

### 安全
- [ ] SQL 是否参数化（无注入）
- [ ] 用户输入是否校验
- [ ] 是否有越权风险（IDOR）
- [ ] 敏感信息是否泄露到日志
- [ ] HTML 是否净化（XSS）
- [ ] 密码/Token 是否加密存储

### 性能
- [ ] 是否有 N+1 查询
- [ ] 是否在循环中做 I/O
- [ ] 是否有内存泄漏（如未关闭的 channel/goroutine）
- [ ] 大数据集是否分页

### 并发
- [ ] 共享状态是否加锁
- [ ] goroutine 是否有泄漏风险
- [ ] 是否有死锁可能

### 错误处理
- [ ] 错误是否被正确处理（无吞错）
- [ ] 错误信息是否对用户友好
- [ ] 是否有降级方案

### 可观测性
- [ ] 关键路径是否有日志
- [ ] 是否有指标埋点
- [ ] 是否有追踪 span

### 测试
- [ ] 是否有单元测试
- [ ] 是否覆盖边界情况
- [ ] 测试是否有意义（不是为覆盖率写）
```

---

## 九、测试体系增强

### 9.1 测试分层（增强）

```
            ┌──────────────┐
            │  E2E (5%)    │  ← 完整流程
            └──────────────┘
          ┌──────────────────┐
          │ 契约测试 (5%)     │  ← API 契约校验
          └──────────────────┘
        ┌──────────────────────┐
        │  集成测试 (20%)       │  ← 真实 DB
        └──────────────────────┘
      ┌──────────────────────────┐
      │   单元测试 (60%)          │  ← 纯函数
      └──────────────────────────┘
    ┌──────────────────────────────┐
    │     性能基准 (5%)             │  ← Benchmark
    └──────────────────────────────┘
  ┌──────────────────────────────────┐
  │      混沌/故障注入 (5%)           │  ← Chaos
  └──────────────────────────────────┘
```

### 9.2 性能基准测试

```go
// internal/storage/dao/message_bench_test.go
func BenchmarkMessageDAO_List(b *testing.B) {
    db := setupTestDB(b)
    // 预置 1000 条邮件
    seedMails(b, db, 1000)

    dao := NewMessageDAO(db)
    ctx := context.Background()

    b.Run("list_20", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            dao.List(ctx, 1, "INBOX", 1, 20, "", false)
        }
    })

    b.Run("search", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            dao.List(ctx, 1, "INBOX", 1, 20, "test subject", false)
        }
    })
}

// 基线：list_20 < 5ms, search < 20ms
```

**性能基线**（写入文档）：

| 操作 | 目标 P99 | 目标 QPS |
|---|---|---|
| 邮件列表（20 条） | < 50ms | 200 |
| 邮件详情 | < 30ms | 500 |
| 发送邮件 | < 500ms | 20 |
| SMTP 接收 | < 100ms | 50 |
| 用户登录 | < 100ms | 100 |

### 9.3 混沌测试

```go
// tests/chaos/chaos_test.go
// 模拟各种故障场景

func TestChaos_DBDown(t *testing.T) {
    // 启动应用
    app := startApp(t)
    defer app.Stop()

    // 模拟 DB 故障
    app.BreakDB()

    // 验证：应用不崩溃，返回 503
    resp := app.Request("GET", "/api/mail/list")
    assert.Equal(t, 503, resp.StatusCode)

    // 恢复 DB
    app.RestoreDB()

    // 验证：自动恢复
    time.Sleep(5 * time.Second)
    resp = app.Request("GET", "/api/mail/list")
    assert.Equal(t, 200, resp.StatusCode)
}

func TestChaos_SMTPTimeout(t *testing.T) {
    // 模拟出站 SMTP 超时
    // 验证：熔断器触发，邮件入队列，不丢失
}

func TestChaos_DiskFull(t *testing.T) {
    // 模拟磁盘满
    // 验证：优雅降级，新邮件拒绝，旧邮件可读
}
```

### 9.4 契约测试

```go
// tests/contract/contract_test.go
// 验证 API 响应结构符合契约

func TestContract_MailList(t *testing.T) {
    resp := request("GET", "/api/mail/list?folder=INBOX&page=1&limit=20")

    // 验证响应结构
    assert.Equal(t, "object", typeof(resp.Data))
    assert.HasField(resp.Data, "items")
    assert.HasField(resp.Data, "total")
    assert.HasField(resp.Data, "page")
    assert.HasField(resp.Data, "limit")

    // 验证字段类型
    items := resp.Data.Items
    if len(items) > 0 {
        assert.IsInt(items[0].ID)
        assert.IsString(items[0].From)
        assert.IsString(items[0].Subject)
        assert.IsBool(items[0].IsRead)  // 不再是 0/1
    }
}
```

### 9.5 安全测试

```go
// tests/security/security_test.go
func TestSecurity_SQLInjection(t *testing.T) {
    payloads := []string{
        "' OR '1'='1",
        "'; DROP TABLE users; --",
        "admin'--",
        "1; EXEC xp_cmdshell('dir')",
    }
    for _, p := range payloads {
        // 在登录、注册、邮件搜索等端点测试
    }
}

func TestSecurity_XSS(t *testing.T) {
    payloads := []string{
        `<script>alert(1)</script>`,
        `<img src=x onerror=alert(1)>`,
        `"><script>alert(1)</script>`,
        "javascript:alert(1)",
    }
    // 发送邮件，验证 body_html 被净化
}

func TestSecurity_IDOR(t *testing.T) {
    // 用户 A 尝试访问用户 B 的邮件
    tokenA := login(userA)
    tokenB := login(userB)
    mailB := sendMail(tokenB)

    // A 访问 B 的邮件
    resp := requestWithToken(tokenA, "GET", "/api/mail/"+mailB)
    assert.Equal(t, 403, resp.StatusCode)
}

func TestSecurity_PathTraversal(t *testing.T) {
    // 尝试 ../../../etc/passwd 作为附件名
}

func TestSecurity_CSRF(t *testing.T) {
    // 验证无 CSRF token 的请求被拒
    // （JWT 模式下，需验证 Authorization 头）
}
```

---

## 十、运维与 SRE

### 10.1 SLO 定义

| 服务 | SLI | SLO | 错误预算 |
|---|---|---|---|
| Web API 可用性 | 成功请求率（非 5xx） | 99.9% | 43 分钟/月 |
| Web API 延迟 | P99 < 500ms | 99% | 7 小时/月 |
| SMTP 接收可用性 | 成功接收率 | 99.5% | 3.6 小时/月 |
| 邮件投递成功率 | 成功投递/总发送 | 99% | 7 小时/月 |
| IMAP 可用性 | 成功登录率 | 99.5% | 3.6 小时/月 |

### 10.2 容量规划

```markdown
## 单实例容量上限（参考值）

| 资源 | 配置 | 上限 |
|---|---|---|
| CPU | 2 核 | 100 并发用户 |
| 内存 | 2GB | 500 用户 |
| 磁盘（DB） | 50GB | 100 万封邮件 |
| 磁盘（Maildir） | 500GB | 100 万封邮件（含附件） |
| 网络带宽 | 100Mbps | 50 并发 SMTP |

## 扩容信号
- CPU 使用率 > 70% 持续 5 分钟 → 横向扩容
- 内存使用率 > 80% → 检查泄漏
- 磁盘使用率 > 70% → 扩容或归档
- DB 查询 P99 > 1s → 加索引或优化
- 队列深度 > 100 → 增加 worker
```

### 10.3 监控看板

提供 4 个预置 Grafana 看板（JSON 文件）：

1. **Overview**：QPS、延迟分布、错误率、活跃用户、队列深度
2. **SMTP**：连接数、灰名单命中率、SPF/DNSBL 命中率、收发量
3. **Business**：日活用户、邮件收发趋势、存储使用、配额告警
4. **Infrastructure**：CPU、内存、磁盘、DB 连接、GC

### 10.4 告警接入

```yaml
# deployments/alertmanager/config.yml
route:
  receiver: default
  group_by: [alertname, severity]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h

receivers:
  - name: default
    email_configs:
      - to: ops@example.com
        from: alert@mymail.example.com
        smarthost: mail.example.com:587
        auth_username: alert@mymail.example.com
        auth_password: "{{ .AlertEmailPassword }}"

  - name: critical
    webhook_configs:
      - url: https://hooks.slack.com/services/XXX/YYY/ZZZ
        send_resolved: true
    pagerduty_configs:
      - service_key: "{{ .PagerDutyKey }}"
```

---

## 十一、项目结构补强

```
mymail-go/
├── cmd/
│   └── mymail/
│       └── main.go
├── internal/
│   ├── ...（原方案结构）
│   ├── metrics/                    # 新增：Prometheus 指标
│   │   └── metrics.go
│   ├── tracing/                    # 新增：OpenTelemetry
│   │   └── tracing.go
│   ├── audit/                      # 新增：审计日志
│   │   ├── audit.go
│   │   └── dao.go
│   ├── resilience/                 # 新增：熔断/重试
│   │   ├── circuit_breaker.go
│   │   └── retry.go
│   └── feature/                    # 新增：特性开关
│       └── flags.go
├── deployments/
│   ├── docker/
│   │   ├── Dockerfile
│   │   ├── docker-compose.yml
│   │   ├── docker-compose.test.yml  # 新增：测试环境
│   │   ├── docker-compose.blue.yml  # 新增：蓝绿部署
│   │   └── docker-compose.green.yml
│   ├── prometheus/                 # 新增
│   │   ├── prometheus.yml
│   │   └── alerts.yml
│   ├── alertmanager/               # 新增
│   │   └── config.yml
│   ├── grafana/                    # 新增
│   │   ├── dashboards/
│   │   │   ├── mymail-overview.json
│   │   │   ├── mymail-smtp.json
│   │   │   ├── mymail-business.json
│   │   │   └── mymail-infrastructure.json
│   │   └── datasources/
│   │       └── prometheus.yml
│   └── jaeger/                     # 新增
│       └── docker-compose.yml
├── docs/                           # 新增：完整文档体系
│   ├── ARCHITECTURE.md
│   ├── OPERATIONS.md
│   ├── SECURITY.md
│   ├── CONTRIBUTING.md
│   ├── runbooks/
│   ├── adr/
│   └── api/
├── scripts/
│   ├── backup.sh                   # 增强：企业级备份
│   ├── deploy.sh                   # 新增：蓝绿部署脚本
│   ├── rollback.sh                 # 新增：回滚脚本
│   ├── check-integrity.go          # 新增：数据完整性检查
│   └── gen-dkim.sh
├── .github/
│   ├── workflows/
│   │   ├── ci.yml                  # 增强：完整流水线
│   │   ├── release.yml             # 新增
│   │   └── security.yml            # 新增
│   ├── PULL_REQUEST_TEMPLATE.md    # 新增
│   ├── CODE_REVIEW_CHECKLIST.md    # 新增
│   └── ISSUE_TEMPLATE/
├── .golangci.yml                   # 增强
├── .editorconfig                   # 新增
├── Makefile                        # 增强
└── README.md
```

### 11.1 Makefile 增强

```makefile
# Makefile
.PHONY: all build test lint run dev docker deploy backup

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GOFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

all: lint test build

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -o dist/mymail ./cmd/mymail
	@echo "Built dist/mymail (version: $(VERSION))"

build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o dist/mymail-linux-amd64 ./cmd/mymail
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o dist/mymail-linux-arm64 ./cmd/mymail
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -o dist/mymail-darwin-amd64 ./cmd/mymail

test:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out | grep total

test-integration:
	go test -tags=integration ./tests/integration/...

test-e2e:
	go test -tags=e2e ./tests/e2e/...

bench:
	go test -bench=. -benchmem ./...

lint:
	golangci-lint run --timeout=5m

security:
	govulncheck ./...
	gosec -severity medium -confidence medium ./...

dev:
	go run ./cmd/mymail -config .env.dev

docker-build:
	docker build -t mymail:$(VERSION) -f deployments/docker/Dockerfile .

docker-up:
	docker compose -f deployments/docker/docker-compose.yml up -d

docker-down:
	docker compose -f deployments/docker/docker-compose.yml down

deploy:
	./scripts/deploy.sh $(VERSION)

rollback:
	./scripts/rollback.sh

backup:
	./scripts/backup.sh

migrate:
	go run ./cmd/mymail migrate

generate-docs:
	swag init -g cmd/mymail/main.go -o docs/api --parseDependency --parseInternal

clean:
	rm -rf dist/ coverage.out

help:
	@echo "Available targets:"
	@echo "  build          - Build binary"
	@echo "  test           - Run unit tests with coverage"
	@echo "  test-e2e       - Run E2E tests"
	@echo "  lint           - Run linter"
	@echo "  security       - Run security scanners"
	@echo "  docker-up      - Start with docker compose"
	@echo "  deploy         - Deploy new version"
	@echo "  rollback       - Rollback to previous version"
	@echo "  backup         - Run backup"
```

---

## 十二、配置管理增强

### 12.1 多环境配置

```
configs/
├── config.dev.yaml          # 开发环境
├── config.staging.yaml      # 预发布
├── config.prod.yaml         # 生产
└── config.test.yaml         # 测试
```

```go
// internal/config/config.go
type Config struct {
    Env         string `env:"APP_ENV" envDefault:"dev"`
    Version     string
    Debug       bool   `env:"DEBUG" envDefault:"false"`

    Server    ServerConfig
    DB        DBConfig
    Mail      MailConfig
    SMTP      SMTPConfig
    JWT       JWTConfig
    CORS      CORSConfig
    RateLimit RateLimitConfig
    Spam      SpamConfig
    Features  FeatureFlags
    Observability ObservabilityConfig
    Backup    BackupConfig
    Audit     AuditConfig
}

type ObservabilityConfig struct {
    LogLevel       string `env:"LOG_LEVEL" envDefault:"info"`
    LogFormat      string `env:"LOG_FORMAT" envDefault:"json"`  // json/text
    MetricsEnabled bool   `env:"METRICS_ENABLED" envDefault:"true"`
    MetricsPath    string `env:"METRICS_PATH" envDefault:"/metrics"`
    TracingEnabled bool   `env:"TRACING_ENABLED" envDefault:"false"`
    TracingEndpoint string `env:"TRACING_ENDPOINT"`
    TracingSampleRate float64 `env:"TRACING_SAMPLE_RATE" envDefault:"0.1"`
}

type AuditConfig struct {
    Enabled       bool `env:"AUDIT_ENABLED" envDefault:"true"`
    RetentionDays int  `env:"AUDIT_RETENTION_DAYS" envDefault:"365"`
}

type BackupConfig struct {
    Enabled        bool   `env:"BACKUP_ENABLED" envDefault:"true"`
    Schedule       string `env:"BACKUP_SCHEDULE" envDefault:"0 3 * * *"`  // cron
    RetentionDays  int    `env:"BACKUP_RETENTION_DAYS" envDefault:"30"`
    RemoteBucket   string `env:"BACKUP_REMOTE_BUCKET"`
    RemoteAccessKey string `env:"BACKUP_REMOTE_ACCESS_KEY"`
    RemoteSecretKey string `env:"BACKUP_REMOTE_SECRET_KEY"`
}
```

### 12.2 配置校验

```go
func (c *Config) Validate() error {
    if c.Env == "" {
        return errors.New("env is required")
    }

    if c.JWT.Secret == "" || c.JWT.Secret == "change-me-in-production" {
        return errors.New("JWT_SECRET must be set")
    }
    if len(c.JWT.Secret) < 32 {
        return errors.New("JWT_SECRET must be at least 32 characters")
    }

    if c.Mail.Domain == "" {
        return errors.New("mail domain is required")
    }

    if c.Env == "prod" {
        if c.Debug {
            return errors.New("debug must be false in production")
        }
        if c.CORS.AllowedOrigins == nil {
            return errors.New("CORS allowed origins must be set in production")
        }
        if !c.Observability.MetricsEnabled {
            return errors.New("metrics must be enabled in production")
        }
    }

    return nil
}
```

### 12.3 配置热重载

```go
// internal/config/hotreload.go
func WatchConfig(callback func(*Config)) {
    watcher, _ := fsnotify.NewWatcher()
    watcher.Add("configs/")

    go func() {
        for {
            select {
            case event, ok := <-watcher.Events:
                if !ok { return }
                if event.Has(fsnotify.Write) {
                    slog.Info("config file changed, reloading")
                    cfg, err := Load()
                    if err != nil {
                        slog.Error("config reload failed", "error", err)
                        continue
                    }
                    callback(cfg)
                }
            case err, ok := <-watcher.Errors:
                if !ok { return }
                slog.Error("config watcher error", "error", err)
            }
        }
    }()
}
```

---

## 十三、关键参数清单补充

### 13.1 可观测性参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| LOG_LEVEL | info | debug/info/warn/error |
| LOG_FORMAT | json | json/text |
| METRICS_ENABLED | true | 启用 Prometheus 指标 |
| METRICS_PATH | /metrics | 指标端点路径 |
| TRACING_ENABLED | false | 启用分布式追踪 |
| TRACING_ENDPOINT | - | OTLP collector 地址 |
| TRACING_SAMPLE_RATE | 0.1 | 追踪采样率 |
| AUDIT_ENABLED | true | 启用审计日志 |
| AUDIT_RETENTION_DAYS | 365 | 审计日志保留天数 |

### 13.2 韧性参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| CIRCUIT_BREAKER_THRESHOLD | 0.6 | 熔断失败率阈值 |
| CIRCUIT_BREAKER_TIMEOUT | 30s | 熔断恢复时间 |
| RETRY_MAX_ATTEMPTS | 3 | 最大重试次数 |
| RETRY_INITIAL_INTERVAL | 1s | 重试初始间隔 |
| RETRY_MAX_INTERVAL | 30s | 重试最大间隔 |
| RETRY_MAX_ELAPSED | 5m | 重试总时长 |

### 13.3 备份参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| BACKUP_ENABLED | true | 启用定时备份 |
| BACKUP_SCHEDULE | 0 3 * * * | cron 表达式 |
| BACKUP_RETENTION_DAYS | 30 | 备份保留天数 |
| BACKUP_REMOTE_BUCKET | - | S3 兼容存储 bucket |
| BACKUP_REMOTE_ACCESS_KEY | - | S3 access key |
| BACKUP_REMOTE_SECRET_KEY | - | S3 secret key |

### 13.4 SLO 参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| SLO_API_AVAILABILITY | 0.999 | API 可用性目标 |
| SLO_API_LATENCY_P99_MS | 500 | API P99 延迟目标 |
| SLO_SMTP_AVAILABILITY | 0.995 | SMTP 可用性目标 |

### 13.5 特性开关

| 参数 | 默认值 | 说明 |
|---|---|---|
| FEATURE_SPAM_FILTER | true | 反垃圾模块 |
| FEATURE_WS_NOTIFY | true | WebSocket 通知 |
| FEATURE_GREYLIST | true | 灰名单 |
| FEATURE_API_KEY | true | API Key 发信 |
| FEATURE_RULES | true | 用户规则 |
| FEATURE_AUDIT | true | 审计日志 |
| FEATURE_REGISTRATION | true | 用户注册 |
| FEATURE_MAINTENANCE | false | 维护模式 |

---

## 总结：企业级标准的"做"与"不做"

### 必须做（小型项目企业级标准）

| 维度 | 关键动作 |
|---|---|
| 可观测性 | 日志 + 指标 + 追踪三支柱，trace_id 串联 |
| 审计日志 | 关键操作全记录，独立存储 |
| API 治理 | OpenAPI 文档 + 请求 ID + 幂等 + 版本 |
| 安全合规 | 依赖扫描 + 容器扫描 + 密钥轮换 + GDPR |
| 韧性 | 熔断 + 重试 + 优雅降级 + 特性开关 |
| 数据治理 | 全量+增量备份 + 异地 + 演练 + 归档 |
| CI/CD | 多阶段门禁 + 蓝绿发布 + 自动回滚 |
| 文档 | ADR + Runbook + PR 模板 + 评审清单 |
| 测试 | 单元+集成+E2E+性能+混沌+契约+安全 |
| SRE | SLO + 告警 + 容量规划 + 监控看板 |

### 不做（避免过度设计）

| 维度 | 不做的原因 |
|---|---|
| 微服务拆分 | 单二进制足够，小项目不值得 |
| Kubernetes | Docker Compose 足够，K8s 运维成本高 |
| Service Mesh | 流量小，无需 sidecar |
| 多机房容灾 | 单机+备份足够 |
| 消息队列（Kafka） | 用 SQLite 表 + Go channel 实现轻量队列 |
| 分布式缓存（Redis） | 单机内存缓存足够 |
| API Gateway | Nginx 反代足够 |
| 服务注册发现 | 单实例，配置文件足够 |

---

> **补充方案结束**
>
> 本补充方案与《MyMail后端Go重构完整方案.md》配合使用，共同构成完整的企业级 Go 重构方案。原方案聚焦功能正确性与 Bug 修复，本补充聚焦可观测性、可治理、可运维、可审计的企业级治理能力。建议两份文档一起作为实施手册，按阶段推进。
