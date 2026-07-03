// Package dao
// queue.go 实现 MailQueueDAO，对应 mail_queue 表（新增表，原 Node.js 无）。
//
// 用途：
//   - 持久化外发邮件队列，避免 SMTP 发送失败导致邮件丢失
//   - 支持重试（attempts / max_attempts / next_retry_at）
//   - worker 后台轮询 pending 队列并投递
//
// 修复的缺陷：
//   - 原 Node.js 无队列，sendMail 失败即丢；本 DAO 配合 mailsender/queue.go 实现持久化重试。
package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// QueueStatus 队列状态。
type QueueStatus string

const (
	QueuePending   QueueStatus = "pending"
	QueueSending   QueueStatus = "sending"
	QueueSent      QueueStatus = "sent"
	QueueFailed    QueueStatus = "failed"
	QueueCancelled QueueStatus = "cancelled"
)

// MailQueueItem 表示 mail_queue 表的完整行。
type MailQueueItem struct {
	ID           int64        `json:"id"`
	UserID       int64        `json:"user_id"`
	FromAddr     string       `json:"from_addr"`
	ToAddrs      string       `json:"to_addrs"`      // 逗号分隔
	CcAddrs      string       `json:"cc_addrs"`       // 可空，逗号分隔
	BccAddrs     string       `json:"bcc_addrs"`      // 可空，逗号分隔
	Subject      string       `json:"subject"`
	BodyHTML     string       `json:"body_html"`
	BodyText     string       `json:"body_text"`
	ReplyTo      string       `json:"reply_to"`
	Attachments  string       `json:"attachments"`   // JSON 数组（附件文件路径元信息）
	Status       QueueStatus  `json:"status"`
	Attempts     int          `json:"attempts"`
	MaxAttempts  int          `json:"max_attempts"`
	NextRetryAt  sql.NullTime `json:"-"`
	ErrorMsg     string       `json:"error_msg,omitempty"`
	CreatedAt    string       `json:"created_at"`
	SentAt       string       `json:"sent_at,omitempty"`
}

// MailQueueDAO 封装 mail_queue 表的所有数据库操作。
type MailQueueDAO struct {
	db *db.DB
}

// NewMailQueueDAO 创建 MailQueueDAO。
func NewMailQueueDAO(database *db.DB) *MailQueueDAO {
	return &MailQueueDAO{db: database}
}

// EnqueueInput 入队参数。
type EnqueueInput struct {
	UserID      int64
	FromAddr    string
	ToAddrs     string // 逗号分隔
	CcAddrs     string
	BccAddrs    string
	Subject     string
	BodyHTML    string
	BodyText    string
	ReplyTo     string
	Attachments string // JSON 字符串
	MaxAttempts int    // 0 则默认 3
}

// Enqueue 插入一条待发送队列项，返回队列 ID。
// 不立即发送，由 worker 异步处理。
func (d *MailQueueDAO) Enqueue(ctx context.Context, in EnqueueInput) (int64, error) {
	return d.EnqueueOn(ctx, d.db, in)
}

// EnqueueOn 在指定 executor 上入队（可用于事务）。
func (d *MailQueueDAO) EnqueueOn(ctx context.Context, ex executor, in EnqueueInput) (int64, error) {
	if in.MaxAttempts <= 0 {
		in.MaxAttempts = 3
	}
	const q = `INSERT INTO mail_queue
		(user_id, from_addr, to_addrs, cc_addrs, bcc_addrs, subject,
		 body_html, body_text, reply_to, attachments, status, max_attempts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)`
	var cc, bcc, replyTo, attachments any
	if in.CcAddrs != "" {
		cc = in.CcAddrs
	}
	if in.BccAddrs != "" {
		bcc = in.BccAddrs
	}
	if in.ReplyTo != "" {
		replyTo = in.ReplyTo
	}
	if in.Attachments != "" {
		attachments = in.Attachments
	}
	res, err := ex.ExecContext(ctx, q,
		in.UserID, in.FromAddr, in.ToAddrs, cc, bcc, nullable(in.Subject),
		nullable(in.BodyHTML), nullable(in.BodyText), replyTo, attachments, in.MaxAttempts,
	)
	if err != nil {
		return 0, fmt.Errorf("入队失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取队列 ID 失败: %w", err)
	}
	return id, nil
}

// FindByID 根据 ID 查询队列项。
func (d *MailQueueDAO) FindByID(ctx context.Context, id int64) (*MailQueueItem, error) {
	q := fmt.Sprintf("SELECT %s FROM mail_queue WHERE id = ?", queueColumns)
	row := d.db.QueryRowContext(ctx, q, id)
	item, err := scanQueueItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询队列项失败: %w", err)
	}
	return item, nil
}

// queueColumns 是 mail_queue 表的所有列（顺序须与 scanQueueItem 一致）。
const queueColumns = `id, user_id, from_addr, to_addrs, cc_addrs, bcc_addrs, subject,
	body_html, body_text, reply_to, attachments, status, attempts, max_attempts,
	next_retry_at, error_msg, created_at, sent_at`

// scanQueueItem 将一行数据扫描到 MailQueueItem。
func scanQueueItem(s interface {
	Scan(dest ...any) error
}) (*MailQueueItem, error) {
	var item MailQueueItem
	var (
		cc, bcc, replyTo, attachments, errorMsg, sentAt sql.NullString
		subject, bodyHTML, bodyText                    sql.NullString
	)
	err := s.Scan(
		&item.ID, &item.UserID, &item.FromAddr, &item.ToAddrs, &cc, &bcc, &subject,
		&bodyHTML, &bodyText, &replyTo, &attachments, &item.Status,
		&item.Attempts, &item.MaxAttempts, &item.NextRetryAt, &errorMsg, &item.CreatedAt, &sentAt,
	)
	if err != nil {
		return nil, err
	}
	item.CcAddrs = cc.String
	item.BccAddrs = bcc.String
	item.Subject = subject.String
	item.BodyHTML = bodyHTML.String
	item.BodyText = bodyText.String
	item.ReplyTo = replyTo.String
	item.Attachments = attachments.String
	item.ErrorMsg = errorMsg.String
	item.SentAt = sentAt.String
	return &item, nil
}

// ClaimNextPending 原子地领取下一个待发送项（worker 用）。
// 通过事务 + UPDATE ... LIMIT 1 实现"领取"语义：
//  1. 查询最早的 pending 项
//  2. 标记为 sending，增加 attempts
//  3. 返回该项
//
// 如果没有待发送项，返回 (nil, nil)。
func (d *MailQueueDAO) ClaimNextPending(ctx context.Context) (*MailQueueItem, error) {
	var item *MailQueueItem
	err := d.db.RunInTransaction(ctx, func(tx *sql.Tx) error {
		const selectQ = `SELECT %s FROM mail_queue
			WHERE status = 'pending'
			AND (next_retry_at IS NULL OR next_retry_at <= CURRENT_TIMESTAMP)
			ORDER BY created_at ASC LIMIT 1`
		q := fmt.Sprintf(selectQ, queueColumns)
		row := tx.QueryRowContext(ctx, q)
		it, err := scanQueueItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("查询待发送项失败: %w", err)
		}
		const updateQ = `UPDATE mail_queue SET status = 'sending', attempts = attempts + 1 WHERE id = ?`
		if _, err := tx.ExecContext(ctx, updateQ, it.ID); err != nil {
			return fmt.Errorf("领取队列项失败: %w", err)
		}
		it.Status = QueueSending
		it.Attempts++
		item = it
		return nil
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

// MarkSent 标记队列项为已发送。
func (d *MailQueueDAO) MarkSent(ctx context.Context, id int64) error {
	const q = `UPDATE mail_queue SET status = 'sent', sent_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("标记队列已发送失败: %w", err)
	}
	return nil
}

// MarkFailed 标记队列项为失败，并设置下次重试时间。
// 若 attempts >= max_attempts，则状态置为 failed（终态），否则置为 pending 等待重试。
func (d *MailQueueDAO) MarkFailed(ctx context.Context, id int64, errorMsg string, retryDelayMinutes int) error {
	// 先查询当前 attempts / max_attempts
	item, err := d.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		return fmt.Errorf("队列项 %d 不存在", id)
	}

	if item.Attempts >= item.MaxAttempts {
		const q = `UPDATE mail_queue SET status = 'failed', error_msg = ? WHERE id = ?`
		_, err := d.db.ExecContext(ctx, q, errorMsg, id)
		if err != nil {
			return fmt.Errorf("标记队列最终失败: %w", err)
		}
		return nil
	}

	// 计算下次重试时间
	const q = `UPDATE mail_queue
		SET status = 'pending', error_msg = ?,
		    next_retry_at = datetime('now', ?)
		WHERE id = ?`
	retryMod := fmt.Sprintf("+%d minutes", retryDelayMinutes)
	_, err = d.db.ExecContext(ctx, q, errorMsg, retryMod, id)
	if err != nil {
		return fmt.Errorf("标记队列重试失败: %w", err)
	}
	return nil
}

// Cancel 取消队列项。
func (d *MailQueueDAO) Cancel(ctx context.Context, id int64) error {
	const q = `UPDATE mail_queue SET status = 'cancelled' WHERE id = ?`
	_, err := d.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("取消队列项失败: %w", err)
	}
	return nil
}

// PendingCount 返回待发送项数量（指标埋点用）。
func (d *MailQueueDAO) PendingCount(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM mail_queue WHERE status = 'pending'`
	var count int64
	err := d.db.QueryRowContext(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计待发送数失败: %w", err)
	}
	return count, nil
}

// FindStaleSending 查询处于 sending 状态过久的项（worker 崩溃恢复用）。
// staleMinutes 表示"多久没更新视为卡死"。
func (d *MailQueueDAO) FindStaleSending(ctx context.Context, staleMinutes int) ([]*MailQueueItem, error) {
	mod := fmt.Sprintf("-%d minutes", staleMinutes)
	q := fmt.Sprintf(`SELECT %s FROM mail_queue
		WHERE status = 'sending' AND created_at < datetime('now', ?)`, queueColumns)
	rows, err := d.db.QueryContext(ctx, q, mod)
	if err != nil {
		return nil, fmt.Errorf("查询卡死队列项失败: %w", err)
	}
	defer rows.Close()
	var list []*MailQueueItem
	for rows.Next() {
		item, err := scanQueueItem(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描队列项失败: %w", err)
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// RequeueStale 将卡死的 sending 项重置为 pending（崩溃恢复）。
func (d *MailQueueDAO) RequeueStale(ctx context.Context, staleMinutes int) (int64, error) {
	mod := fmt.Sprintf("-%d minutes", staleMinutes)
	const q = `UPDATE mail_queue SET status = 'pending'
		WHERE status = 'sending' AND created_at < datetime('now', ?)`
	res, err := d.db.ExecContext(ctx, q, mod)
	if err != nil {
		return 0, fmt.Errorf("重置卡死队列失败: %w", err)
	}
	return res.RowsAffected()
}

// FindByUser 查询用户队列项（管理后台用）。
func (d *MailQueueDAO) FindByUser(ctx context.Context, userID int64, limit int) ([]*MailQueueItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := fmt.Sprintf("SELECT %s FROM mail_queue WHERE user_id = ? ORDER BY created_at DESC LIMIT ?", queueColumns)
	rows, err := d.db.QueryContext(ctx, q, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询用户队列失败: %w", err)
	}
	defer rows.Close()
	var list []*MailQueueItem
	for rows.Next() {
		item, err := scanQueueItem(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描队列项失败: %w", err)
		}
		list = append(list, item)
	}
	return list, rows.Err()
}
