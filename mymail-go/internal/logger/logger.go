// Package logger 提供结构化日志能力，基于标准库 slog。
// 自动注入 trace_id 和 request_id，实现全链路日志关联。
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// contextKey 是 context 中存取值的类型，避免键冲突。
type contextKey string

// RequestIDKey 用于从 context 中存取 request_id。
const RequestIDKey contextKey = "request_id"

// traceHandler 包装 slog.Handler，自动注入 trace_id 和 request_id。
type traceHandler struct {
	slog.Handler
}

// Handle 实现 slog.Handler 接口，在每条日志记录中注入追踪字段。
func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	// 注入 trace_id 和 span_id（来自 OpenTelemetry）
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	// 注入 request_id（来自中间件）
	if reqID, ok := ctx.Value(RequestIDKey).(string); ok && reqID != "" {
		r.AddAttrs(slog.String("request_id", reqID))
	}
	return h.Handler.Handle(ctx, r)
}

// L 是全局 logger 实例。
var L *slog.Logger

// Init 初始化全局 logger。
// level: debug/info/warn/error
// format: json/text
func Init(level string, format string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: true,
	}

	var h slog.Handler
	if strings.ToLower(format) == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}

	// 包装为支持 trace_id 的 handler
	L = slog.New(&traceHandler{Handler: h})
	slog.SetDefault(L)
}

// WithContext 返回带有 context 的 logger，自动提取 trace_id 和 request_id。
func WithContext(ctx context.Context) *slog.Logger {
	if L == nil {
		// 兜底：未初始化时用默认 logger
		return slog.Default()
	}
	return L.With() // slog 通过 context 传递，handler 自动提取
}
