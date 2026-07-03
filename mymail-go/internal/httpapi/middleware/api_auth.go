// api_auth.go 实现 API Key 认证中间件。
//
// 与 JWT 认证（auth.go）并列，用于 /api/v1/* 端点（外部发信）。
//
// 认证流程（P1-3 修复）：
//  1. 提取 Authorization: Bearer mk_xxx
//  2. crypto.IsAPIKeyFormat 校验格式
//  3. APIKeyVerifier.Verify（内部用 key_prefix 索引查找 + bcrypt 比对）
//  4. UserDAO.FindByID 获取用户，校验 IsActive
//  5. 注入 user + apiKey 到 gin.Context
//
// 依赖倒置：通过 APIKeyVerifier 接口解耦 service 层，避免循环依赖。
package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/metrics"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// context keys
const (
	ContextKeyAPIKey = "api_key"
)

// APIKeyVerifier API Key 验证接口。
// service.APIKeyService 实现此接口。
type APIKeyVerifier interface {
	Verify(ctx context.Context, plaintext string) (*dao.APIKey, error)
}

// APIKeyAuth API Key 认证中间件。
//
// 用于 /api/v1/* 端点。认证成功后注入 user + api_key 到 context。
// 必须在 RequireScope 之前使用。
func APIKeyAuth(verifier APIKeyVerifier, userDAO *dao.UserDAO) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || len(authHeader) < 8 || authHeader[:7] != "Bearer " {
			metrics.AuthAttemptsTotal.WithLabelValues("api_key", "missing").Inc()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 API Key"})
			return
		}
		token := authHeader[7:]

		// 验证 API Key（内部用 prefix 索引查找 + bcrypt 比对）
		apiKey, err := verifier.Verify(c.Request.Context(), token)
		if err != nil {
			metrics.AuthAttemptsTotal.WithLabelValues("api_key", "invalid").Inc()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "无效的 API Key"})
			return
		}

		// 查库验证用户仍然有效
		user, err := userDAO.FindByID(c.Request.Context(), apiKey.UserID)
		if err != nil {
			metrics.AuthAttemptsTotal.WithLabelValues("api_key", "db_error").Inc()
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
			return
		}
		if user == nil || !user.IsActive {
			metrics.AuthAttemptsTotal.WithLabelValues("api_key", "disabled").Inc()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "账号已禁用"})
			return
		}

		// 注入 user + api_key
		c.Set(ContextKeyUser, user)
		c.Set(ContextKeyAPIKey, apiKey)
		metrics.AuthAttemptsTotal.WithLabelValues("api_key", "success").Inc()
		c.Next()
	}
}

// RequireScope 要求当前 API Key 具有指定 scope。
// 必须在 APIKeyAuth 之后使用。
func RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get(ContextKeyAPIKey)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未通过 API Key 认证"})
			return
		}
		apiKey, ok := v.(*dao.APIKey)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "上下文类型错误"})
			return
		}

		// 校验 scope
		hasScope := false
		for _, s := range apiKey.Scopes {
			if s == scope {
				hasScope = true
				break
			}
		}
		if !hasScope {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "API Key 无此操作权限"})
			return
		}
		c.Next()
	}
}

// CurrentAPIKey 从 gin.Context 取出当前 API Key。
// 未认证或非 API Key 认证时返回 nil。
func CurrentAPIKey(c *gin.Context) *dao.APIKey {
	v, ok := c.Get(ContextKeyAPIKey)
	if !ok {
		return nil
	}
	apiKey, _ := v.(*dao.APIKey)
	return apiKey
}
