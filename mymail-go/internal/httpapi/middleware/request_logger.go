// Package middleware
package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger 记录每个 HTTP 请求的访问日志。
// 包含 method/path/status/latency/ip/request_id。
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"ip", c.ClientIP(),
			"request_id", c.GetString("request_id"),
		}
		if query != "" {
			attrs = append(attrs, "query", query)
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}

		// 根据状态码选择日志级别
		switch {
		case status >= 500:
			slog.Error("HTTP 请求", attrs...)
		case status >= 400:
			slog.Warn("HTTP 请求", attrs...)
		default:
			slog.Info("HTTP 请求", attrs...)
		}
	}
}

// Metrics 提供 Prometheus 指标埋点。
// 指标定义见 internal/metrics 包。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 指标埋点在 internal/metrics 中实现
		// 这里仅做转发，保持中间件链顺序
		c.Next()
	}
}
