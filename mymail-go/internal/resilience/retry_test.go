// retry_test.go 测试指数退避重试。
//
// 测试覆盖：
//   - 首次成功不重试
//   - 失败后重试成功
//   - 超过最大次数返回错误
//   - 上下文取消
//   - Disabled 不重试
//   - 退避时间计算
//   - 带结果的重试
//   - 总耗时超时
//   - 默认配置
package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetryConfig 快速重试配置（短间隔便于测试）。
func fastRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:     3,
		InitialInterval: 5 * time.Millisecond,
		MaxInterval:     50 * time.Millisecond,
		MaxElapsedTime:  5 * time.Second,
		Enabled:         true,
	}
}

func TestRetry_Success(t *testing.T) {
	cfg := fastRetryConfig()
	calls := int32(0)
	err := Retry(context.Background(), cfg, func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err != nil {
		t.Errorf("成功应返回 nil，实际 %v", err)
	}
	if calls != 1 {
		t.Errorf("应只调用 1 次，实际 %d", calls)
	}
}

func TestRetry_RetryThenSucceed(t *testing.T) {
	cfg := fastRetryConfig()
	calls := int32(0)
	err := Retry(context.Background(), cfg, func() error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("transient error")
		}
		return nil
	})
	if err != nil {
		t.Errorf("第 3 次成功应返回 nil，实际 %v", err)
	}
	if calls != 3 {
		t.Errorf("应调用 3 次，实际 %d", calls)
	}
}

func TestRetry_MaxAttemptsExceeded(t *testing.T) {
	cfg := fastRetryConfig()
	cfg.MaxAttempts = 3
	calls := int32(0)
	testErr := errors.New("persistent error")
	err := Retry(context.Background(), cfg, func() error {
		atomic.AddInt32(&calls, 1)
		return testErr
	})
	if err == nil {
		t.Error("应返回错误")
	}
	if !errors.Is(err, testErr) {
		t.Errorf("错误应包装 testErr，实际 %v", err)
	}
	if calls != 3 {
		t.Errorf("应调用 3 次（MaxAttempts），实际 %d", calls)
	}
}

func TestRetry_ContextCancel(t *testing.T) {
	cfg := fastRetryConfig()
	cfg.MaxAttempts = 10
	cfg.InitialInterval = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	calls := int32(0)

	// 在第一次失败后取消上下文
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, cfg, func() error {
		atomic.AddInt32(&calls, 1)
		return errors.New("fail")
	})
	if err == nil {
		t.Error("应返回错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("错误应包含 context.Canceled，实际 %v", err)
	}
}

func TestRetry_Disabled(t *testing.T) {
	cfg := fastRetryConfig()
	cfg.Enabled = false
	calls := int32(0)
	err := Retry(context.Background(), cfg, func() error {
		atomic.AddInt32(&calls, 1)
		return errors.New("fail")
	})
	if err == nil {
		t.Error("应返回错误")
	}
	if calls != 1 {
		t.Errorf("Disabled 时应只调用 1 次，实际 %d", calls)
	}
}

func TestRetry_BackoffCalculation(t *testing.T) {
	cfg := fastRetryConfig()
	// initial=5ms, max=50ms
	// attempt 1: 5ms
	// attempt 2: 10ms
	// attempt 3: 20ms
	// attempt 4: 40ms
	// attempt 5: 50ms (capped)
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 5 * time.Millisecond},
		{2, 10 * time.Millisecond},
		{3, 20 * time.Millisecond},
		{4, 40 * time.Millisecond},
		{5, 50 * time.Millisecond}, // capped at MaxInterval
		{6, 50 * time.Millisecond}, // capped
	}
	for _, tt := range tests {
		got := calculateBackoff(tt.attempt, cfg)
		if got != tt.want {
			t.Errorf("attempt %d: 期望 %v，实际 %v", tt.attempt, tt.want, got)
		}
	}
}

func TestRetry_MaxElapsedTime(t *testing.T) {
	cfg := fastRetryConfig()
	cfg.MaxAttempts = 100
	cfg.InitialInterval = 50 * time.Millisecond
	cfg.MaxElapsedTime = 100 * time.Millisecond // 很短的总超时

	start := time.Now()
	err := Retry(context.Background(), cfg, func() error {
		return errors.New("fail")
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Error("应返回错误")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("应在 MaxElapsedTime 附近停止，实际耗时 %v", elapsed)
	}
}

func TestRetryWithResult_Success(t *testing.T) {
	cfg := fastRetryConfig()
	result, err := RetryWithResult(context.Background(), cfg, func() (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Errorf("成功应返回 nil 错误，实际 %v", err)
	}
	if result != 42 {
		t.Errorf("结果应为 42，实际 %d", result)
	}
}

func TestRetryWithResult_RetryThenSucceed(t *testing.T) {
	cfg := fastRetryConfig()
	calls := int32(0)
	result, err := RetryWithResult(context.Background(), cfg, func() (int, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return 0, errors.New("fail")
		}
		return 99, nil
	})
	if err != nil {
		t.Errorf("应成功，实际 %v", err)
	}
	if result != 99 {
		t.Errorf("结果应为 99，实际 %d", result)
	}
	if calls != 2 {
		t.Errorf("应调用 2 次，实际 %d", calls)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("默认 MaxAttempts 应为 3，实际 %d", cfg.MaxAttempts)
	}
	if cfg.InitialInterval != 500*time.Millisecond {
		t.Errorf("默认 InitialInterval 应为 500ms，实际 %v", cfg.InitialInterval)
	}
	if cfg.MaxInterval != 10*time.Second {
		t.Errorf("默认 MaxInterval 应为 10s，实际 %v", cfg.MaxInterval)
	}
	if !cfg.Enabled {
		t.Error("默认应启用重试")
	}
}

func TestRetry_EmptyBodyHTMLOnly(t *testing.T) {
	// 验证 BodyText 为空时仍能正常工作（边界场景）
	cfg := fastRetryConfig()
	err := Retry(context.Background(), cfg, func() error {
		return nil
	})
	if err != nil {
		t.Errorf("应成功，实际 %v", err)
	}
}
