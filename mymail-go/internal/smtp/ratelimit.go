// ratelimit.go 实现 SMTP 连接级限流。
//
// 机制（与原 Node.js smtp-receiver.js 一致）：
//   - 固定时间窗口 + 每 IP 计数（内存 Map）
//   - 每个窗口内单 IP 最多 maxConnectionsPerIp 次连接
//   - 超限拒绝（返回 421 等临时错误）
//   - 窗口过期后自动重置计数
//
// 默认配置（来自 config）：
//   - maxConnectionsPerIp = 10
//   - rateWindowMs = 60000（1 分钟窗口）
//
// 设计要点：
//   - 纯内存实现，进程重启即清空（与原 Node.js 一致）
//   - sync.Mutex 保护 counts Map
//   - 后台 goroutine 定期清理过期窗口（默认 60 秒）
//   - Stop() 优雅关闭后台清理
package smtp

import (
	"sync"
	"time"
)

// ipCounter 单 IP 的连接计数。
type ipCounter struct {
	count       int   // 当前窗口内连接数
	windowStart int64 // 当前窗口起始时间（Unix 毫秒）
}

// RateLimiter SMTP 连接级限流器。
//
// 固定窗口算法：每个时间窗口内单 IP 最多 maxPerIP 次连接。
type RateLimiter struct {
	mu       sync.Mutex
	maxPerIP int                  // 每 IP 最大连接数
	windowMs int64                // 时间窗口（毫秒）
	counts   map[string]*ipCounter // IP → 计数器
	stopCh   chan struct{}        // 停止后台清理
	stopped  bool                 // 是否已停止
}

// NewRateLimiter 创建限流器。
//
// 参数：
//   - maxPerIP: 每 IP 最大连接数（<=0 用默认 10）
//   - windowMs: 时间窗口毫秒（<=0 用默认 60000）
func NewRateLimiter(maxPerIP int, windowMs int64) *RateLimiter {
	if maxPerIP <= 0 {
		maxPerIP = 10
	}
	if windowMs <= 0 {
		windowMs = 60000
	}
	return &RateLimiter{
		maxPerIP: maxPerIP,
		windowMs: windowMs,
		counts:   make(map[string]*ipCounter),
		stopCh:   make(chan struct{}),
	}
}

// Check 检查指定 IP 是否允许连接。
// 返回 true=允许，false=超限。
//
// 副作用：若允许则计数 +1，可能重置窗口。
func (r *RateLimiter) Check(ip string) bool {
	if ip == "" {
		return true // 无 IP 信息时放行（兼容性）
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()
	c, ok := r.counts[ip]
	if !ok {
		// 首次见到该 IP
		r.counts[ip] = &ipCounter{count: 1, windowStart: now}
		return true
	}

	// 窗口已过期：重置
	if now-c.windowStart >= r.windowMs {
		c.count = 1
		c.windowStart = now
		return true
	}

	// 窗口内计数
	if c.count >= r.maxPerIP {
		return false // 超限
	}
	c.count++
	return true
}

// CurrentCount 返回指定 IP 的当前窗口计数（测试用）。
func (r *RateLimiter) CurrentCount(ip string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.counts[ip]
	if !ok {
		return 0
	}
	// 若窗口已过期，返回 0
	if time.Now().UnixMilli()-c.windowStart >= r.windowMs {
		return 0
	}
	return c.count
}

// Cleanup 清理所有过期窗口。
// 返回清理的 IP 数量。
func (r *RateLimiter) Cleanup() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()
	cleaned := 0
	for ip, c := range r.counts {
		if now-c.windowStart >= r.windowMs {
			delete(r.counts, ip)
			cleaned++
		}
	}
	return cleaned
}

// StartCleanup 启动后台定期清理 goroutine。
// interval 为清理间隔（<=0 用默认 60 秒）。
// 调用 Stop() 停止。
func (r *RateLimiter) StartCleanup(interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.Cleanup()
			case <-r.stopCh:
				return
			}
		}
	}()
}

// Stop 停止后台清理 goroutine。
// 幂等：多次调用安全。
func (r *RateLimiter) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.stopped = true
	close(r.stopCh)
}

// Stats 返回当前跟踪的 IP 数量（监控/测试用）。
func (r *RateLimiter) Stats() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.counts)
}
