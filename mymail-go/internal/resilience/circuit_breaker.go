// Package resilience 实现弹性容错机制：熔断器 + 指数退避重试。
//
// 用于出站 SMTP 发送的高可用保障：
//   - circuit_breaker.go：基于 sony/gobreaker v2 的熔断器封装
//   - retry.go：指数退避重试（自实现，无额外依赖）
//
// 设计依据：
//   - 验收标准：出站 SMTP 熔断器在失败率 > 60% 时触发
//   - config: SMTPCircuitBreaker* / SMTPRetry* 配置项
package resilience

import (
	"errors"
	"time"

	"github.com/sony/gobreaker/v2"
)

// ErrCircuitOpen 熔断器开启时返回的错误。
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreakerConfig 熔断器配置。
type CircuitBreakerConfig struct {
	Name          string        // 熔断器名称（日志/监控用）
	MaxRequests   uint32        // 半开状态最大请求数（<=0 用默认 5）
	Interval      time.Duration // closed 状态计数窗口（<=0 用默认 60s）
	Timeout       time.Duration // open 状态持续时间（<=0 用默认 30s）
	FailureRatio  float64       // 失败率阈值（0-1，<=0 用默认 0.6）
	MinRequests   uint32        // 触发熔断最小请求数（<=0 用默认 10）
}

// CircuitBreaker 熔断器封装。
//
// 基于 sony/gobreaker v2（泛型版本），内部用 struct{} 作为结果类型，
// 对外暴露 Execute(fn func() error) error 简化调用。
//
// 状态机：
//   - Closed：正常放行，统计失败率
//   - Open：拒绝所有请求（返回 ErrCircuitOpen），等待 Timeout 后转 HalfOpen
//   - HalfOpen：放行 MaxRequests 个请求，成功则转 Closed，失败则转 Open
type CircuitBreaker struct {
	cb *gobreaker.CircuitBreaker[struct{}]
}

// NewCircuitBreaker 创建熔断器。
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.MaxRequests <= 0 {
		cfg.MaxRequests = 5
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.FailureRatio <= 0 {
		cfg.FailureRatio = 0.6
	}
	if cfg.MinRequests <= 0 {
		cfg.MinRequests = 10
	}
	minReqs := cfg.MinRequests
	failureRatio := cfg.FailureRatio

	settings := gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// 仅当请求数 >= MinRequests 且失败率 >= FailureRatio 时熔断
			if counts.Requests < minReqs {
				return false
			}
			ratio := float64(counts.TotalFailures) / float64(counts.Requests)
			return ratio >= failureRatio
		},
	}

	return &CircuitBreaker{
		cb: gobreaker.NewCircuitBreaker[struct{}](settings),
	}
}

// Execute 执行函数，受熔断器保护。
// 熔断器开启时返回 ErrCircuitOpen。
func (cb *CircuitBreaker) Execute(fn func() error) error {
	_, err := cb.cb.Execute(func() (struct{}, error) {
		if err := fn(); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	return err
}

// State 返回当前熔断器状态。
func (cb *CircuitBreaker) State() gobreaker.State {
	return cb.cb.State()
}

// StateString 返回当前熔断器状态的字符串表示。
func (cb *CircuitBreaker) StateString() string {
	switch cb.cb.State() {
	case gobreaker.StateClosed:
		return "closed"
	case gobreaker.StateHalfOpen:
		return "half-open"
	case gobreaker.StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// Counts 返回当前计数（监控用）。
func (cb *CircuitBreaker) Counts() gobreaker.Counts {
	return cb.cb.Counts()
}
