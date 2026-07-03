// api_rate_limit.go 实现 API Key 维度的发信限流中间件。
//
// 验收标准 9.6 第 4 项：API Key 发信限流生效。
//
// 设计：
//   - 基于 APIKey.RateLimit 字段（每分钟允许请求数，0 表示不限流）
//   - 固定窗口计数器（每分钟重置），内存存储，无需外部依赖
//   - 懒清理：检查时若窗口已过期则重置，无需后台 goroutine
//   - 必须在 APIKeyAuth 之后使用（依赖 CurrentAPIKey）
package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// APIKeyRateLimiter 基于 API Key ID 的固定窗口限流器。
type APIKeyRateLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*rateBucket // key: API Key ID
}

// rateBucket 单个 API Key 的计数桶。
type rateBucket struct {
	count   int
	resetAt time.Time
}

// NewAPIKeyRateLimiter 创建限流器。
func NewAPIKeyRateLimiter() *APIKeyRateLimiter {
	return &APIKeyRateLimiter{buckets: make(map[int64]*rateBucket)}
}

// RateLimit 返回 gin 中间件，根据当前 API Key 的 RateLimit 字段限流。
// 必须在 APIKeyAuth 之后使用。RateLimit<=0 表示不限流。
func (l *APIKeyRateLimiter) RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := CurrentAPIKey(c)
		if apiKey == nil {
			// 未通过 API Key 认证，跳过（由前置中间件处理 401）
			c.Next()
			return
		}
		if apiKey.RateLimit <= 0 {
			// RateLimit<=0 表示不限流
			c.Next()
			return
		}

		allowed := l.allow(apiKey.ID, apiKey.RateLimit)
		if !allowed {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "API Key 请求频率超限，请稍后重试",
			})
			return
		}
		c.Next()
	}
}

// allow 判断指定 API Key 是否允许通过。线程安全。
func (l *APIKeyRateLimiter) allow(keyID int64, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	bucket, ok := l.buckets[keyID]
	if !ok || now.After(bucket.resetAt) {
		// 新窗口或窗口已过期，重置
		l.buckets[keyID] = &rateBucket{
			count:   1,
			resetAt: now.Add(time.Minute),
		}
		return true
	}

	bucket.count++
	if bucket.count > limit {
		return false
	}
	return true
}

// Cleanup 清理所有过期 bucket（可选调用，通常由懒清理覆盖）。
func (l *APIKeyRateLimiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for id, b := range l.buckets {
		if now.After(b.resetAt) {
			delete(l.buckets, id)
		}
	}
}
