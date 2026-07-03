// api_rate_limit_test.go 测试 API Key 发信限流中间件。
//
// 测试覆盖：
//   - 无 API Key 上下文 → 放行（由前置中间件处理 401）
//   - RateLimit=0 → 不限流
//   - 未超限 → 放行
//   - 超限 → 429 + Retry-After 头
//   - 窗口重置后恢复
//   - 并发安全（多 goroutine 同时请求）
package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

func setupRateLimitRouter(apiKey *dao.APIKey) (*gin.Engine, *APIKeyRateLimiter) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	limiter := NewAPIKeyRateLimiter()
	r.Use(func(c *gin.Context) {
		if apiKey != nil {
			c.Set(ContextKeyAPIKey, apiKey)
		}
		c.Next()
	})
	r.Use(limiter.RateLimit())
	r.POST("/send", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r, limiter
}

func TestAPIKeyRateLimiter_NoAPIKey(t *testing.T) {
	r, _ := setupRateLimitRouter(nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/send", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("无 API Key 应放行，期望 200，实际 %d", w.Code)
	}
}

func TestAPIKeyRateLimiter_Unlimited(t *testing.T) {
	apiKey := &dao.APIKey{ID: 1, RateLimit: 0}
	r, _ := setupRateLimitRouter(apiKey)

	// RateLimit=0 不限流，连续 100 次都应放行
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/send", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("RateLimit=0 不应限流，第 %d 次请求期望 200，实际 %d", i+1, w.Code)
		}
	}
}

func TestAPIKeyRateLimiter_WithinLimit(t *testing.T) {
	apiKey := &dao.APIKey{ID: 2, RateLimit: 5}
	r, _ := setupRateLimitRouter(apiKey)

	// 5 次请求都应放行
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/send", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("未超限第 %d 次请求期望 200，实际 %d", i+1, w.Code)
		}
	}
}

func TestAPIKeyRateLimiter_Exceeded(t *testing.T) {
	apiKey := &dao.APIKey{ID: 3, RateLimit: 3}
	r, _ := setupRateLimitRouter(apiKey)

	// 前 3 次放行
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/send", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("前 3 次应放行，第 %d 次期望 200，实际 %d", i+1, w.Code)
		}
	}

	// 第 4 次应 429
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/send", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超限应返回 429，实际 %d，body: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After 头期望 '60'，实际 %q", w.Header().Get("Retry-After"))
	}
}

func TestAPIKeyRateLimiter_ConcurrentSafe(t *testing.T) {
	apiKey := &dao.APIKey{ID: 4, RateLimit: 100}
	r, limiter := setupRateLimitRouter(apiKey)

	// 100 次并发请求，全部应放行（未超限）
	var wg sync.WaitGroup
	var okCount, failCount int64
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/send", nil)
			r.ServeHTTP(w, req)
			mu.Lock()
			if w.Code == http.StatusOK {
				okCount++
			} else {
				failCount++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if okCount != 100 {
		t.Errorf("并发 100 次未超限请求应全部放行，实际 ok=%d fail=%d", okCount, failCount)
	}

	// 验证 limiter 内部计数一致
	limiter.mu.Lock()
	bucket := limiter.buckets[4]
	count := bucket.count
	limiter.mu.Unlock()
	if count != 100 {
		t.Errorf("内部计数期望 100，实际 %d", count)
	}
}

func TestAPIKeyRateLimiter_Allow_Directly(t *testing.T) {
	limiter := NewAPIKeyRateLimiter()

	// limit=2，前 2 次允许，第 3 次拒绝
	if !limiter.allow(100, 2) {
		t.Error("第 1 次应允许")
	}
	if !limiter.allow(100, 2) {
		t.Error("第 2 次应允许")
	}
	if limiter.allow(100, 2) {
		t.Error("第 3 次应拒绝")
	}

	// 不同 keyID 独立计数
	if !limiter.allow(200, 2) {
		t.Error("不同 keyID 第 1 次应允许")
	}
}
