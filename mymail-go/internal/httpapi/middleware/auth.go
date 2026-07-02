// Package middleware
// auth.go 实现 JWT 认证中间件。
//
// 兼容性：与原 Node.js middleware/auth.js 完全一致：
//   - Header 格式：Authorization: Bearer <token>
//   - 缺失/格式错 → 401 {"error":"未登录"}
//   - token 无效/过期 → 401 {"error":"Token 已过期，请重新登录"}
//   - 用户不存在/禁用 → 401 {"error":"账号已禁用"}
//
// 企业级增强：
//   - 注入 user 到 gin.Context（key="user"），后续 handler 可直接取用
//   - 记录认证失败指标
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/metrics"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// context keys
const (
	ContextKeyUser = "user"
)

// Authenticate 校验 JWT，注入 user 到 context。
func Authenticate(jwtMgr *crypto.JWTManager, userDAO *dao.UserDAO) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || len(authHeader) < 8 || authHeader[:7] != "Bearer " {
			metrics.AuthAttemptsTotal.WithLabelValues("jwt", "missing").Inc()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		token := authHeader[7:]

		claims, err := jwtMgr.Verify(token)
		if err != nil {
			metrics.AuthAttemptsTotal.WithLabelValues("jwt", "invalid").Inc()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 已过期，请重新登录"})
			return
		}

		// 查库验证用户仍然有效
		user, err := userDAO.FindByID(c.Request.Context(), claims.ID)
		if err != nil {
			metrics.AuthAttemptsTotal.WithLabelValues("jwt", "db_error").Inc()
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
			return
		}
		if user == nil || !user.IsActive {
			metrics.AuthAttemptsTotal.WithLabelValues("jwt", "disabled").Inc()
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "账号已禁用"})
			return
		}

		c.Set(ContextKeyUser, user)
		c.Next()
	}
}

// RequireAdmin 要求当前用户是管理员。
// 必须在 Authenticate 之后使用。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get(ContextKeyUser)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		user, ok := v.(*dao.User)
		if !ok || user.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
			return
		}
		c.Next()
	}
}

// CurrentUser 从 gin.Context 取出当前用户。
// 未认证时返回 nil。
func CurrentUser(c *gin.Context) *dao.User {
	v, ok := c.Get(ContextKeyUser)
	if !ok {
		return nil
	}
	user, _ := v.(*dao.User)
	return user
}
