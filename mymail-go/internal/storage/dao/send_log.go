// Package dao
// send_log.go 实现 SendLogDAO，对应 send_log 表。
//
// 用途：
//   - 记录每封外发邮件的状态（pending/sent/failed）
//   - 实现发送频率限制（recentCount 滑动窗口）
//
// 修复的缺陷：
//   - P1-1：CreateOn 支持事务（与 message 创建同事务）
package dao

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// SendLogStatus 发送日志状态。
type SendLogStatus string

const (
	SendLogPending SendLogStatus = "pending"
	SendLogSent    SendLogStatus = "sent"
	SendLogFailed  SendLogStatus = "failed"
)

// SendLog 表示 send_log 表的完整行。
type SendLog struct {
	ID        int64          `json:"id"`
	UserID    int64          `json:"user_id"`
	ToAddr    string         `json:"to_addr"`
	Subject   string         `json:"subject"`
	Status    SendLogStatus  `json:"status"`
	ErrorMsg  sql.NullString `json:"-"`
	SentAt    string         `json:"sent_at"`
}

// SendLogDAO 封装 send_log 表的所有数据库操作。
type SendLogDAO struct {
	db *db.DB
}

// NewSendLogDAO 创建 SendLogDAO。
func NewSendLogDAO(database *db.DB) *SendLogDAO {
	return &SendLogDAO{db: database}
}

// CreateSendLogInput 创建发送日志的输入参数。
type CreateSendLogInput struct {
	UserID   int64
	ToAddr   string
	Subject  string
	Status   SendLogStatus // 空则 "pending"
	ErrorMsg string        // 可空
}

// Create 插入发送日志，返回日志 ID。
func (d *SendLogDAO) Create(ctx context.Context, in CreateSendLogInput) (int64, error) {
	return d.CreateOn(ctx, d.db, in)
}

// CreateOn 在指定 executor 上插入发送日志（可用于事务）。
func (d *SendLogDAO) CreateOn(ctx context.Context, ex executor, in CreateSendLogInput) (int64, error) {
	if in.Status == "" {
		in.Status = SendLogPending
	}
	const q = `INSERT INTO send_log (user_id, to_addr, subject, status, error_msg)
		VALUES (?, ?, ?, ?, ?)`
	var errMsg any
	if in.ErrorMsg != "" {
		errMsg = in.ErrorMsg
	}
	res, err := ex.ExecContext(ctx, q, in.UserID, in.ToAddr, nullable(in.Subject), string(in.Status), errMsg)
	if err != nil {
		return 0, fmt.Errorf("创建发送日志失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取发送日志 ID 失败: %w", err)
	}
	return id, nil
}

// UpdateStatus 更新发送日志状态。
func (d *SendLogDAO) UpdateStatus(ctx context.Context, id int64, status SendLogStatus, errorMsg string) error {
	const q = `UPDATE send_log SET status = ?, error_msg = ? WHERE id = ?`
	var errMsg any
	if errorMsg != "" {
		errMsg = errorMsg
	}
	_, err := d.db.ExecContext(ctx, q, string(status), errMsg, id)
	if err != nil {
		return fmt.Errorf("更新发送日志状态失败: %w", err)
	}
	return nil
}

// RecentSentCount 返回用户在指定时间窗口内已发送成功的邮件数。
// 用于发送频率限制（默认 60 秒窗口）。
func (d *SendLogDAO) RecentSentCount(ctx context.Context, userID int64, window time.Duration) (int64, error) {
	since := time.Now().Add(-window).UTC().Format("2006-01-02 15:04:05")
	const q = `SELECT COUNT(*) FROM send_log
		WHERE user_id = ? AND sent_at >= ? AND status = 'sent'`
	var count int64
	err := d.db.QueryRowContext(ctx, q, userID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("查询近期发送数失败: %w", err)
	}
	return count, nil
}
