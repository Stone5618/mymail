// greylist.go 实现 GreylistDAO，对应 greylist 表。
//
// 用途：
//   - 灰名单（greylisting）反垃圾机制的数据持久化
//   - 记录 (IP, sender, recipient) 三元组的首次见到时间与放行状态
//   - 配合 smtp.GreylistChecker 实现首次拒绝、延迟窗口后放行的策略
//
// 表结构（见 migrations/003_add_greylist_spamlog.up.sql）：
//   - key        TEXT PRIMARY KEY  （格式：IP:sender:recipient）
//   - first_seen INTEGER NOT NULL  （Unix 毫秒时间戳）
//   - allowed    INTEGER NOT NULL DEFAULT 0  （0=未放行，1=已放行）
//
// 与原 Node.js greylist-dao.js 行为一致：
//   - key 格式：IP:sender:recipient
//   - cleanup 阈值：2 * ttlMs（清理已过期记录）
package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// GreylistEntry 表示 greylist 表的一行。
type GreylistEntry struct {
	Key       string // IP:sender:recipient
	FirstSeen int64  // Unix 毫秒时间戳
	Allowed   bool   // 是否已通过延迟窗口
}

// GreylistDAO 封装 greylist 表的所有数据库操作。
type GreylistDAO struct {
	db *db.DB
}

// NewGreylistDAO 创建 GreylistDAO。
func NewGreylistDAO(database *db.DB) *GreylistDAO {
	return &GreylistDAO{db: database}
}

// Get 根据 key 查询灰名单记录。
// 返回 (nil, nil) 表示记录不存在（用于首次见到的三元组）。
func (d *GreylistDAO) Get(ctx context.Context, key string) (*GreylistEntry, error) {
	const q = `SELECT key, first_seen, allowed FROM greylist WHERE key = ?`
	row := d.db.QueryRowContext(ctx, q, key)
	var e GreylistEntry
	var allowed int
	err := row.Scan(&e.Key, &e.FirstSeen, &allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询灰名单失败: %w", err)
	}
	e.Allowed = allowed == 1
	return &e, nil
}

// Upsert 插入或更新灰名单记录。
// 若 key 已存在，更新 first_seen 与 allowed（仅在 allowed 未曾为 true 时更新）。
// 返回当前记录（含最新 first_seen / allowed）。
func (d *GreylistDAO) Upsert(ctx context.Context, key string, firstSeen int64, allowed bool) (*GreylistEntry, error) {
	allowedInt := 0
	if allowed {
		allowedInt = 1
	}
	const q = `INSERT INTO greylist (key, first_seen, allowed)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET first_seen = excluded.first_seen`
	_, err := d.db.ExecContext(ctx, q, key, firstSeen, allowedInt)
	if err != nil {
		return nil, fmt.Errorf("upsert 灰名单失败: %w", err)
	}
	return d.Get(ctx, key)
}

// SetAllowed 标记灰名单记录为已放行（通过延迟窗口）。
// 若记录不存在则忽略（no-op）。
func (d *GreylistDAO) SetAllowed(ctx context.Context, key string) error {
	const q = `UPDATE greylist SET allowed = 1 WHERE key = ?`
	res, err := d.db.ExecContext(ctx, q, key)
	if err != nil {
		return fmt.Errorf("标记灰名单放行失败: %w", err)
	}
	// 不强制要求 RowsAffected > 0，调用方可能在不存在的 key 上调用（并发场景）
	_ = res
	return nil
}

// Cleanup 清理已过期的灰名单记录。
// 清理阈值 = 2 * ttlMs（与原 Node.js 一致）：
//   - 已放行的记录：first_seen 超过 ttlMs 即清理（已通过验证，无需保留）
//   - 未放行的记录：first_seen 超过 2 * ttlMs 才清理（给重试更多时间）
//
// 返回清理的记录数。
func (d *GreylistDAO) Cleanup(ctx context.Context, ttlMs int64) (int64, error) {
	if ttlMs <= 0 {
		return 0, fmt.Errorf("ttlMs 必须 > 0")
	}
	now := time.Now().UnixMilli()
	thresholdAllowed := now - ttlMs        // 已放行记录的清理阈值
	thresholdUnallowed := now - 2*ttlMs    // 未放行记录的清理阈值（2 * ttlMs）
	const q = `DELETE FROM greylist
		WHERE (allowed = 1 AND first_seen < ?)
		   OR (allowed = 0 AND first_seen < ?)`
	res, err := d.db.ExecContext(ctx, q, thresholdAllowed, thresholdUnallowed)
	if err != nil {
		return 0, fmt.Errorf("清理灰名单失败: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("获取清理行数失败: %w", err)
	}
	return n, nil
}

// Count 返回灰名单记录总数（监控/测试用）。
func (d *GreylistDAO) Count(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM greylist`
	var count int64
	err := d.db.QueryRowContext(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计灰名单失败: %w", err)
	}
	return count, nil
}
