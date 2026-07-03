// ratelimit_test.go 测试 RateLimiter 的限流逻辑。
//
// 测试覆盖：
//   - Check（窗口内允许/超限拒绝/窗口重置/空 IP 放行）
//   - CurrentCount
//   - Cleanup
//   - StartCleanup + Stop（后台清理）
//   - 并发安全（多 goroutine）
//   - 默认值
package smtp

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiter_Check_AllowWithinLimit(t *testing.T) {
	r := NewRateLimiter(5, 60000)
	ip := "1.2.3.4"

	for i := 0; i < 5; i++ {
		if !r.Check(ip) {
			t.Errorf("第 %d 次连接应允许", i+1)
		}
	}
}

func TestRateLimiter_Check_RejectOverLimit(t *testing.T) {
	r := NewRateLimiter(3, 60000)
	ip := "1.2.3.4"

	// 前 3 次允许
	for i := 0; i < 3; i++ {
		if !r.Check(ip) {
			t.Errorf("第 %d 次连接应允许", i+1)
		}
	}
	// 第 4 次拒绝
	if r.Check(ip) {
		t.Error("第 4 次连接应拒绝")
	}
	// 第 5 次仍拒绝
	if r.Check(ip) {
		t.Error("第 5 次连接应拒绝")
	}
}

func TestRateLimiter_Check_WindowReset(t *testing.T) {
	// 使用很短的窗口便于测试
	r := NewRateLimiter(2, 100) // 100ms 窗口
	ip := "1.2.3.4"

	r.Check(ip)
	r.Check(ip)
	// 超限
	if r.Check(ip) {
		t.Error("第 3 次应拒绝")
	}

	// 等待窗口过期
	time.Sleep(120 * time.Millisecond)

	// 窗口重置后应允许
	if !r.Check(ip) {
		t.Error("窗口重置后应允许")
	}
}

func TestRateLimiter_Check_EmptyIP(t *testing.T) {
	r := NewRateLimiter(1, 60000)
	// 空 IP 应放行
	if !r.Check("") {
		t.Error("空 IP 应放行")
	}
	if !r.Check("") {
		t.Error("空 IP 第二次也应放行")
	}
}

func TestRateLimiter_Check_DifferentIPs(t *testing.T) {
	r := NewRateLimiter(2, 60000)
	// 不同 IP 独立计数
	if !r.Check("1.1.1.1") {
		t.Error("1.1.1.1 第 1 次应允许")
	}
	if !r.Check("2.2.2.2") {
		t.Error("2.2.2.2 第 1 次应允许")
	}
	if !r.Check("1.1.1.1") {
		t.Error("1.1.1.1 第 2 次应允许")
	}
	if r.Check("1.1.1.1") {
		t.Error("1.1.1.1 第 3 次应拒绝")
	}
	if !r.Check("2.2.2.2") {
		t.Error("2.2.2.2 第 2 次应允许")
	}
}

func TestRateLimiter_CurrentCount(t *testing.T) {
	r := NewRateLimiter(10, 60000)
	ip := "1.2.3.4"

	if r.CurrentCount(ip) != 0 {
		t.Errorf("初始计数应为 0，实际 %d", r.CurrentCount(ip))
	}
	r.Check(ip)
	r.Check(ip)
	if r.CurrentCount(ip) != 2 {
		t.Errorf("计数应为 2，实际 %d", r.CurrentCount(ip))
	}
	// 不存在的 IP
	if r.CurrentCount("9.9.9.9") != 0 {
		t.Error("不存在的 IP 计数应为 0")
	}
}

func TestRateLimiter_CurrentCount_ExpiredWindow(t *testing.T) {
	r := NewRateLimiter(10, 50) // 50ms 窗口
	ip := "1.2.3.4"
	r.Check(ip)
	r.Check(ip)
	time.Sleep(60 * time.Millisecond)
	// 窗口过期后计数应返回 0
	if r.CurrentCount(ip) != 0 {
		t.Errorf("过期窗口计数应为 0，实际 %d", r.CurrentCount(ip))
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	r := NewRateLimiter(10, 50) // 50ms 窗口
	r.Check("1.1.1.1")
	r.Check("2.2.2.2")
	r.Check("3.3.3.3")

	if r.Stats() != 3 {
		t.Errorf("Stats 应为 3，实际 %d", r.Stats())
	}

	// 等待窗口过期
	time.Sleep(60 * time.Millisecond)

	cleaned := r.Cleanup()
	if cleaned != 3 {
		t.Errorf("应清理 3 个，实际 %d", cleaned)
	}
	if r.Stats() != 0 {
		t.Errorf("清理后 Stats 应为 0，实际 %d", r.Stats())
	}
}

func TestRateLimiter_StartCleanup_Stop(t *testing.T) {
	r := NewRateLimiter(10, 50)
	r.StartCleanup(20 * time.Millisecond) // 20ms 清理一次
	defer r.Stop()

	r.Check("1.1.1.1")
	time.Sleep(80 * time.Millisecond) // 等待窗口过期 + 后台清理触发

	// 后台清理应已清理过期记录
	if r.Stats() != 0 {
		t.Errorf("后台清理后 Stats 应为 0，实际 %d", r.Stats())
	}

	// Stop 后再次调用应幂等
	r.Stop()
	r.Stop()
}

func TestRateLimiter_ConcurrentSafe(t *testing.T) {
	r := NewRateLimiter(1000, 60000) // 高限额避免影响并发测试
	ip := "1.2.3.4"

	var wg sync.WaitGroup
	var allowed int64
	goroutines := 10
	perGoroutine := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if r.Check(ip) {
					atomic.AddInt64(&allowed, 1)
				}
			}
		}()
	}
	wg.Wait()

	// 1000 次并发 Check，1000 个应允许（限额 1000）
	total := goroutines * perGoroutine
	if int(allowed) > 1000 {
		t.Errorf("允许数应 <= 1000，实际 %d", allowed)
	}
	if int(allowed) != 1000 {
		t.Errorf("允许数应为 1000（限额），实际 %d", allowed)
	}
	_ = total

	// 计数应为 1000
	if r.CurrentCount(ip) != 1000 {
		t.Errorf("CurrentCount 应为 1000，实际 %d", r.CurrentCount(ip))
	}
}

func TestRateLimiter_Defaults(t *testing.T) {
	r := NewRateLimiter(0, 0)
	if r.maxPerIP != 10 {
		t.Errorf("默认 maxPerIP 应为 10，实际 %d", r.maxPerIP)
	}
	if r.windowMs != 60000 {
		t.Errorf("默认 windowMs 应为 60000，实际 %d", r.windowMs)
	}
}

func TestRateLimiter_Stop_Idempotent(t *testing.T) {
	r := NewRateLimiter(10, 60000)
	r.Stop()
	r.Stop()
	r.Stop()
	// 多次 Stop 不应 panic
}
