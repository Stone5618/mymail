// circuit_breaker_test.go 测试熔断器。
//
// 核心验收标准：失败率 > 60% 时触发熔断。
//
// 测试覆盖：
//   - Closed 状态正常放行
//   - 失败率 > 60% 触发 Open（核心验收）
//   - 请求数不足 MinRequests 时不触发
//   - 失败率不足时不触发
//   - Open 状态拒绝请求
//   - HalfOpen 恢复（成功转 Closed）
//   - HalfOpen 失败转 Open
//   - 默认配置
//   - 状态字符串
package resilience

import (
	"errors"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
)

// newTestCB 创建测试用熔断器（短超时便于快速测试）。
func newTestCB() *CircuitBreaker {
	return NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "test",
		MaxRequests:  3,
		Interval:     2 * time.Second,
		Timeout:      200 * time.Millisecond,
		FailureRatio: 0.6,
		MinRequests:  5,
	})
}

// errTest 测试用错误。
var errTest = errors.New("test error")

func TestCircuitBreaker_AllowWhenClosed(t *testing.T) {
	cb := newTestCB()
	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Errorf("Closed 状态应放行，err=%v", err)
	}
	if !called {
		t.Error("函数应被调用")
	}
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("状态应为 Closed，实际 %v", cb.State())
	}
}

func TestCircuitBreaker_OpenOnHighFailureRate(t *testing.T) {
	// 验收标准：失败率 > 60% 时触发熔断
	// 配置 MinRequests=5, FailureRatio=0.6
	cb := newTestCB()

	// 连续失败 5 次（5/5 = 100% >= 60%），第 5 次后应触发 Open
	for i := 0; i < 5; i++ {
		err := cb.Execute(func() error {
			return errTest
		})
		// 前 4 次应返回 errTest（closed 状态），第 5 次后可能返回 ErrOpenState
		_ = err
	}

	// 第 6 次应被熔断器拒绝（Open 状态）
	err := cb.Execute(func() error {
		t.Error("Open 状态不应调用函数")
		return nil
	})
	if err == nil {
		t.Error("Open 状态应返回错误")
	}
	if !errors.Is(err, gobreaker.ErrOpenState) {
		t.Errorf("应返回 ErrOpenState，实际 %v", err)
	}
	if cb.State() != gobreaker.StateOpen {
		t.Errorf("状态应为 Open，实际 %v", cb.State())
	}
}

func TestCircuitBreaker_NoTripBelowMinRequests(t *testing.T) {
	// MinRequests=5，仅 4 次失败不应触发（不足最小请求数）
	cb := newTestCB()
	for i := 0; i < 4; i++ {
		cb.Execute(func() error { return errTest })
	}
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("4 次失败（不足 MinRequests=5）不应触发 Open，状态 %v", cb.State())
	}

	// 第 5 次失败后应触发
	cb.Execute(func() error { return errTest })
	if cb.State() != gobreaker.StateOpen {
		t.Errorf("5 次失败后应触发 Open，状态 %v", cb.State())
	}
}

func TestCircuitBreaker_NoTripLowFailureRate(t *testing.T) {
	// 失败率 < 60% 不应触发
	// 5 次请求中 2 次失败（40% < 60%）
	cb := newTestCB()
	failCount := 0
	for i := 0; i < 5; i++ {
		cb.Execute(func() error {
			failCount++
			if failCount <= 2 {
				return errTest
			}
			return nil
		})
	}
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("40%% 失败率不应触发 Open，状态 %v", cb.State())
	}
}

func TestCircuitBreaker_RejectWhenOpen(t *testing.T) {
	cb := newTestCB()
	// 触发 Open
	for i := 0; i < 5; i++ {
		cb.Execute(func() error { return errTest })
	}
	if cb.State() != gobreaker.StateOpen {
		t.Fatalf("应为 Open 状态")
	}

	// Open 状态下 Execute 应立即返回 ErrOpenState
	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})
	if !errors.Is(err, gobreaker.ErrOpenState) {
		t.Errorf("应返回 ErrOpenState，实际 %v", err)
	}
	if called {
		t.Error("Open 状态函数不应被调用")
	}
}

func TestCircuitBreaker_HalfOpen_Recovery(t *testing.T) {
	// Timeout=200ms，Open 200ms 后转 HalfOpen
	cb := newTestCB()
	for i := 0; i < 5; i++ {
		cb.Execute(func() error { return errTest })
	}
	if cb.State() != gobreaker.StateOpen {
		t.Fatalf("应为 Open 状态")
	}

	// 等待 Timeout 后转 HalfOpen
	time.Sleep(250 * time.Millisecond)

	// HalfOpen 状态下成功应转 Closed
	// MaxRequests=3，需连续成功 3 次才转 Closed
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error { return nil })
		if err != nil {
			t.Logf("HalfOpen 第 %d 次错误: %v（可能是 HalfOpen 限流）", i+1, err)
		}
	}
	// 最终应恢复 Closed
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("HalfOpen 成功后应转 Closed，状态 %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpen_FailureBackToOpen(t *testing.T) {
	cb := newTestCB()
	for i := 0; i < 5; i++ {
		cb.Execute(func() error { return errTest })
	}
	if cb.State() != gobreaker.StateOpen {
		t.Fatalf("应为 Open 状态")
	}

	// 等待 Timeout
	time.Sleep(250 * time.Millisecond)

	// HalfOpen 状态下失败应转回 Open
	err := cb.Execute(func() error { return errTest })
	_ = err
	if cb.State() != gobreaker.StateOpen {
		t.Errorf("HalfOpen 失败应转回 Open，状态 %v", cb.State())
	}
}

func TestCircuitBreaker_Defaults(t *testing.T) {
	// 全部用默认值
	cb := NewCircuitBreaker(CircuitBreakerConfig{Name: "defaults"})
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("初始状态应为 Closed")
	}
	// 默认 MinRequests=10，9 次失败不应触发
	for i := 0; i < 9; i++ {
		cb.Execute(func() error { return errTest })
	}
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("9 次失败（不足默认 MinRequests=10）不应触发，状态 %v", cb.State())
	}
}

func TestCircuitBreaker_StateString(t *testing.T) {
	cb := newTestCB()
	if cb.StateString() != "closed" {
		t.Errorf("初始 StateString 应为 closed，实际 %s", cb.StateString())
	}

	// 触发 Open
	for i := 0; i < 5; i++ {
		cb.Execute(func() error { return errTest })
	}
	if cb.StateString() != "open" {
		t.Errorf("Open 状态 StateString 应为 open，实际 %s", cb.StateString())
	}
}

func TestCircuitBreaker_Counts(t *testing.T) {
	cb := newTestCB()
	cb.Execute(func() error { return nil })
	cb.Execute(func() error { return nil })
	cb.Execute(func() error { return errTest })

	counts := cb.Counts()
	if counts.Requests != 3 {
		t.Errorf("Requests 应为 3，实际 %d", counts.Requests)
	}
	if counts.TotalSuccesses != 2 {
		t.Errorf("TotalSuccesses 应为 2，实际 %d", counts.TotalSuccesses)
	}
	if counts.TotalFailures != 1 {
		t.Errorf("TotalFailures 应为 1，实际 %d", counts.TotalFailures)
	}
}
