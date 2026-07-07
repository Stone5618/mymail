// Package audit
// cleanup.go 实现审计日志定时清理（AC-8）。
//
// 设计：
//   - 按 AUDIT_RETENTION_DAYS 配置的保留期清理过期日志
//   - 启动时立即执行一次，之后每 24 小时执行一次
//   - 分批删除（每批 500 条），避免长事务锁表
//   - retentionDays <= 0 表示不清理
package audit

import (
	"context"
	"log/slog"
	"time"
)

// StartCleanup 启动审计日志定时清理 goroutine（阻塞，需在 goroutine 中调用）。
//
// 参数：
//   - ctx: 用于优雅停止
//   - dao: 审计日志 DAO
//   - retentionDays: 保留天数（<=0 表示不清理，函数直接返回）
//   - batchSize: 每批删除条数（推荐 500）
func StartCleanup(ctx context.Context, dao *AuditDAO, retentionDays int, batchSize int) {
	if retentionDays <= 0 {
		slog.Info("审计日志清理任务未启动（retentionDays<=0）", "retention_days", retentionDays)
		return
	}
	if batchSize <= 0 {
		batchSize = 500
	}

	slog.Info("审计日志清理任务已启动", "retention_days", retentionDays, "batch_size", batchSize)

	// 启动时先执行一次（清理已累积的过期日志）
	runCleanup(ctx, dao, retentionDays, batchSize)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("审计日志清理任务已停止")
			return
		case <-ticker.C:
			runCleanup(ctx, dao, retentionDays, batchSize)
		}
	}
}

// runCleanup 执行一次清理：删除 timestamp < now - retentionDays 的所有日志。
func runCleanup(ctx context.Context, dao *AuditDAO, retentionDays int, batchSize int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	start := time.Now()
	deleted, err := dao.DeleteBefore(ctx, cutoff, batchSize)
	if err != nil {
		slog.Error("审计日志清理失败",
			"error", err,
			"cutoff", cutoff.Format("2006-01-02 15:04:05"),
			"retention_days", retentionDays,
		)
		return
	}
	if deleted > 0 {
		slog.Info("审计日志清理完成",
			"deleted", deleted,
			"retention_days", retentionDays,
			"cutoff", cutoff.Format("2006-01-02 15:04:05"),
			"elapsed_ms", time.Since(start).Milliseconds(),
		)
	}
}
