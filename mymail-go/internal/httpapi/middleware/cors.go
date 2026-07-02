// Package middleware 提供 HTTP 中间件。
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mymail/mymail-go/internal/config"
)

// CORS 实现 P1-2 修复：基于白名单的跨域控制，而非全开放。
// 仅允许配置中 CORS_ALLOWED_ORIGINS 列出的来源。
func CORS(cfg *config.Config) gin.HandlerFunc {
	// 构建允许来源集合
	allowed := make(map[string]bool, len(cfg.CORSAllowedOrigins)+2)
	for _, o := range cfg.CORSAllowedOrigins {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = true
		}
	}
	// 自动添加本机域名（开发环境友好）
	if cfg.Domain != "" {
		allowed["https://"+cfg.Domain] = true
		allowed["https://"+cfg.MailHost] = true
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, Idempotency-Key")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Max-Age", "86400")
			c.Header("Vary", "Origin")
		}

		// 预检请求直接返回
		if c.Request.Method == http.MethodOptions {
			if origin == "" || !allowed[origin] {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
