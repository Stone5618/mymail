// Package middleware
package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestID 为每个请求生成/透传 X-Request-ID，实现全链路日志关联。
// 优先透传上游传入的 ID，否则生成 UUID。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.Request.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		c.Set("request_id", reqID)
		c.Header("X-Request-ID", reqID)

		// 注入到 request context，供 logger 和 tracer 使用
		ctx := WithRequestID(c.Request.Context(), reqID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// Security 设置安全响应头。
// 包括 CSP、HSTS、X-Frame-Options 等，防御常见 Web 攻击。
func Security() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Header

		// 防止 MIME 类型嗅探
		h("X-Content-Type-Options", "nosniff")

		// 禁止被嵌入 iframe（防止点击劫持）
		h("X-Frame-Options", "DENY")

		// 控制 Referrer 信息泄露
		h("Referrer-Policy", "strict-origin-when-cross-origin")

		// 禁用不必要的浏览器特性
		h("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// XSS 保护（旧浏览器兼容）
		h("X-XSS-Protection", "1; mode=block")

		// HSTS：强制 HTTPS（仅对 HTTPS 请求有效）
		h("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

		// CSP：限制资源加载来源，同时允许必要的外部资源
		h("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://static.cloudflareinsights.com; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"img-src 'self' data: blob: https://cravatar.cn https://www.gravatar.com https://fonts.gstatic.com; "+
				"connect-src 'self' wss: https://static.cloudflareinsights.com; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'")

		c.Next()
	}
}

// Recover 捕获 panic，返回 500 而非崩溃。
// 在日志中记录堆栈，便于排查。
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				// 记录堆栈
				stack := debug.Stack()
				c.Error(panicError{rec: rec, stack: stack})

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "服务器内部错误",
				})
			}
		}()
		c.Next()
	}
}

// panicError 用于包装 panic 信息。
type panicError struct {
	rec   any
	stack []byte
}

func (e panicError) Error() string {
	return "panic recovered"
}
