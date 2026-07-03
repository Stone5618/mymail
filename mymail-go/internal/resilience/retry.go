// retry.go 实现指数退避重试。
//
// 自实现（不依赖 cenkalti/backoff），逻辑简单可控。
//
// 算法：
//   - 首次执行不等待
//   - 失败后等待 initialInterval * 2^(attempt-1)
//   - 等待时间不超过 maxInterval
//   - 总耗时不超过 maxElapsedTime（<=0 表示不限）
//   - 最大尝试次数（含首次）= maxAttempts
//
// 配置（来自 config.SMTPRetry*）：
//   - maxAttempts = 3
//   - initialInterval = 500ms
//   - maxInterval = 10s
//   - maxElapsedTime = 60s
package resilience

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RetryConfig 重试配置。
type RetryConfig struct {
	MaxAttempts     int           // 最大尝试次数（含首次，<=0 用默认 3）
	InitialInterval time.Duration // 初始间隔（<=0 用默认 500ms）
	MaxInterval     time.Duration // 最大间隔（<=0 用默认 10s）
	MaxElapsedTime  time.Duration // 最大总耗时（<=0 表示不限）
	Enabled         bool          // 是否启用重试
}

// DefaultRetryConfig 返回默认重试配置。
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:     3,
		InitialInterval: 500 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		MaxElapsedTime:  60 * time.Second,
		Enabled:         true,
	}
}

// Retry 执行带指数退避重试的函数。
//
// 行为：
//   - fn 返回 nil 视为成功，立即返回
//   - fn 返回 error 视为失败，按指数退避等待后重试
//   - 超过 MaxAttempts 或 MaxElapsedTime 后返回最后一次错误
//   - ctx 取消时立即返回 ctx.Err()
//   - cfg.Enabled=false 时仅执行一次（不重试）
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	if !cfg.Enabled {
		return fn()
	}

	cfg = normalizeRetryConfig(cfg)
	start := time.Now()
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// 检查上下文取消
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("重试被取消: %w", err)
		}

		// 检查总耗时
		if cfg.MaxElapsedTime > 0 && time.Since(start) >= cfg.MaxElapsedTime {
			if lastErr != nil {
				return fmt.Errorf("重试超时（%s），最后错误: %w", cfg.MaxElapsedTime, lastErr)
			}
			return fmt.Errorf("重试超时（%s）", cfg.MaxElapsedTime)
		}

		// 执行函数
		err := fn()
		if err == nil {
			if attempt > 1 {
				slog.Info("重试成功", "attempt", attempt)
			}
			return nil
		}

		lastErr = err
		slog.Warn("重试失败",
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"error", err,
		)

		// 最后一次尝试不再等待
		if attempt >= cfg.MaxAttempts {
			break
		}

		// 计算退避时间（指数退避）
		backoff := calculateBackoff(attempt, cfg)
		if err := sleepWithContext(ctx, backoff); err != nil {
			return fmt.Errorf("重试等待被取消: %w", err)
		}
	}

	return fmt.Errorf("重试 %d 次后仍失败: %w", cfg.MaxAttempts, lastErr)
}

// RetryWithResult 执行带指数退避重试的函数（返回结果）。
func RetryWithResult[T any](ctx context.Context, cfg RetryConfig, fn func() (T, error)) (T, error) {
	var zero T
	if !cfg.Enabled {
		return fn()
	}

	cfg = normalizeRetryConfig(cfg)
	start := time.Now()
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, fmt.Errorf("重试被取消: %w", err)
		}

		if cfg.MaxElapsedTime > 0 && time.Since(start) >= cfg.MaxElapsedTime {
			if lastErr != nil {
				return zero, fmt.Errorf("重试超时（%s），最后错误: %w", cfg.MaxElapsedTime, lastErr)
			}
			return zero, fmt.Errorf("重试超时（%s）", cfg.MaxElapsedTime)
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		slog.Warn("重试失败",
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"error", err,
		)

		if attempt >= cfg.MaxAttempts {
			break
		}

		backoff := calculateBackoff(attempt, cfg)
		if err := sleepWithContext(ctx, backoff); err != nil {
			return zero, fmt.Errorf("重试等待被取消: %w", err)
		}
	}

	return zero, fmt.Errorf("重试 %d 次后仍失败: %w", cfg.MaxAttempts, lastErr)
}

// calculateBackoff 计算指数退避时间。
// attempt 从 1 开始，退避 = initialInterval * 2^(attempt-1)，不超过 maxInterval。
func calculateBackoff(attempt int, cfg RetryConfig) time.Duration {
	if attempt <= 1 {
		return cfg.InitialInterval
	}
	// 指数退避：initial * 2^(attempt-1)
	backoff := cfg.InitialInterval
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= cfg.MaxInterval {
			return cfg.MaxInterval
		}
	}
	if backoff > cfg.MaxInterval {
		return cfg.MaxInterval
	}
	return backoff
}

// sleepWithContext 带上下文取消的 sleep。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// normalizeRetryConfig 规范化重试配置（填充默认值）。
func normalizeRetryConfig(cfg RetryConfig) RetryConfig {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.InitialInterval <= 0 {
		cfg.InitialInterval = 500 * time.Millisecond
	}
	if cfg.MaxInterval <= 0 {
		cfg.MaxInterval = 10 * time.Second
	}
	return cfg
}
