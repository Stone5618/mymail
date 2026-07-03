// greylist.go 实现灰名单（greylisting）反垃圾机制。
//
// 灰名单原理：
//   - 首次见到 (IP, sender, recipient) 三元组时临时拒绝（4xx）
//   - 正常 MTA 会在延迟窗口（默认 5 分钟）后重试，此时放行
//   - 大多数垃圾邮件不重试，从而被过滤
//
// 5 状态判断（与原 Node.js smtp-receiver.js 一致）：
//   - first_attempt: 首次见到，记录 first_seen，拒绝
//   - expired:       first_seen 已超过 ttlMs（记录过期），重置 first_seen，拒绝
//   - passed:        first_seen 超过 delayMs 但未超过 ttlMs，标记 allowed=true，放行
//   - cached:        已 allowed=true，直接放行（缓存命中）
//   - wait:          first_seen 未超过 delayMs，拒绝（等待中）
//
// key 规则：IP:sender:recipient（与原 Node.js 一致）
//
// 失败降级（验收标准 6）：
//   - DB 查询/写入失败时放行（不阻断投递），记录 reason="error"
package smtp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// 灰名单决策原因。
const (
	GreylistReasonFirstAttempt = "first_attempt" // 首次见到，拒绝
	GreylistReasonExpired      = "expired"       // 记录已过期，重置后拒绝
	GreylistReasonPassed       = "passed"        // 通过延迟窗口，放行
	GreylistReasonCached       = "cached"        // 已放行缓存命中
	GreylistReasonWait         = "wait"          // 等待延迟窗口
	GreylistReasonError        = "error"         // DB 错误降级放行
	GreylistReasonDisabled     = "disabled"      // 灰名单功能关闭
)

// GreylistDecision 灰名单决策结果。
type GreylistDecision struct {
	Allow      bool   // 是否放行
	Reason     string // 决策原因（GreylistReason* 常量）
	RetryAfter int    // 建议重试时间（秒），仅当 Allow=false 时有意义
}

// GreylistChecker 灰名单检查器。
//
// 依赖 GreylistDAO 持久化三元组状态。
// delayMs/ttlMs 来自 config.GreylistDelayMs/GreylistTtlMs。
type GreylistChecker struct {
	dao     *dao.GreylistDAO
	delayMs int64 // 延迟窗口（毫秒）
	ttlMs   int64 // 记录有效期（毫秒）
	enabled bool  // 功能开关
}

// NewGreylistChecker 创建灰名单检查器。
//
// 参数：
//   - greylistDAO: 灰名单 DAO
//   - delayMs: 延迟窗口（<=0 用默认 5 分钟）
//   - ttlMs: 记录有效期（<=0 用默认 1 小时）
//   - enabled: 功能开关（false 时直接放行）
func NewGreylistChecker(greylistDAO *dao.GreylistDAO, delayMs, ttlMs int64, enabled bool) *GreylistChecker {
	if delayMs <= 0 {
		delayMs = 300000 // 5 分钟
	}
	if ttlMs <= 0 {
		ttlMs = 3600000 // 1 小时
	}
	return &GreylistChecker{
		dao:     greylistDAO,
		delayMs: delayMs,
		ttlMs:   ttlMs,
		enabled: enabled,
	}
}

// Check 检查 (ip, sender, recipient) 三元组是否应放行。
//
// 返回 GreylistDecision：
//   - Allow=true: 放行（cached/passed/error/disabled）
//   - Allow=false: 拒绝（first_attempt/expired/wait），含 RetryAfter
func (c *GreylistChecker) Check(ctx context.Context, ip, sender, recipient string) GreylistDecision {
	if !c.enabled || c.dao == nil {
		return GreylistDecision{Allow: true, Reason: GreylistReasonDisabled}
	}

	key := buildGreylistKey(ip, sender, recipient)
	now := time.Now().UnixMilli()

	// 1. 查询现有记录
	entry, err := c.dao.Get(ctx, key)
	if err != nil {
		// DB 错误降级：放行
		slog.Warn("灰名单查询失败，降级放行", "key", key, "error", err)
		return GreylistDecision{Allow: true, Reason: GreylistReasonError}
	}

	// 2. 首次见到：记录并拒绝
	if entry == nil {
		if _, err := c.dao.Upsert(ctx, key, now, false); err != nil {
			slog.Warn("灰名单首次记录写入失败，降级放行", "key", key, "error", err)
			return GreylistDecision{Allow: true, Reason: GreylistReasonError}
		}
		slog.Info("灰名单首次见到，拒绝", "key", key, "retry_after_sec", c.delayMs/1000)
		return GreylistDecision{
			Allow:      false,
			Reason:     GreylistReasonFirstAttempt,
			RetryAfter: int(c.delayMs / 1000),
		}
	}

	// 3. 已放行：缓存命中
	if entry.Allowed {
		return GreylistDecision{Allow: true, Reason: GreylistReasonCached}
	}

	// 4. 未放行：判断是否通过延迟窗口
	elapsed := now - entry.FirstSeen

	// 4.1 已过期：重置 first_seen，拒绝
	if elapsed >= c.ttlMs {
		if _, err := c.dao.Upsert(ctx, key, now, false); err != nil {
			slog.Warn("灰名单过期重置写入失败，降级放行", "key", key, "error", err)
			return GreylistDecision{Allow: true, Reason: GreylistReasonError}
		}
		slog.Info("灰名单记录已过期，重置后拒绝", "key", key, "elapsed_ms", elapsed, "retry_after_sec", c.delayMs/1000)
		return GreylistDecision{
			Allow:      false,
			Reason:     GreylistReasonExpired,
			RetryAfter: int(c.delayMs / 1000),
		}
	}

	// 4.2 通过延迟窗口：标记放行
	if elapsed >= c.delayMs {
		if err := c.dao.SetAllowed(ctx, key); err != nil {
			slog.Warn("灰名单标记放行失败，降级放行", "key", key, "error", err)
			return GreylistDecision{Allow: true, Reason: GreylistReasonError}
		}
		slog.Info("灰名单通过延迟窗口，放行", "key", key, "elapsed_ms", elapsed)
		return GreylistDecision{Allow: true, Reason: GreylistReasonPassed}
	}

	// 4.3 等待中：拒绝
	remaining := c.delayMs - elapsed
	retryAfter := int(remaining / 1000)
	if retryAfter < 1 {
		retryAfter = 1
	}
	slog.Info("灰名单等待中，拒绝", "key", key, "elapsed_ms", elapsed, "retry_after_sec", retryAfter)
	return GreylistDecision{
		Allow:      false,
		Reason:     GreylistReasonWait,
		RetryAfter: retryAfter,
	}
}

// buildGreylistKey 构造灰名单 key。
// 格式：IP:sender:recipient（与原 Node.js 一致）。
func buildGreylistKey(ip, sender, recipient string) string {
	return fmt.Sprintf("%s:%s:%s", ip, sender, recipient)
}
