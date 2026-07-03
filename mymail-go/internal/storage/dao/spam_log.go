// spam_log.go 实现 SpamLogDAO，对应 spam_log 表。
//
// 用途：
//   - 持久化 SMTP 接收邮件的反垃圾评估结果
//   - 修复原 Node.js 缺陷：spam-log-dao.js 是死代码，spam 评分只输出到日志未落库
//   - 配合 smtp.MessageValidator 在 DATA 阶段记录每封邮件的评分
//   - 支持按 IP / action / 时间范围查询，用于管理员审计与监控
//
// 表结构（见 migrations/003_add_greylist_spamlog.up.sql）：
//   - id         INTEGER PRIMARY KEY AUTOINCREMENT
//   - sender     TEXT
//   - recipient  TEXT
//   - ip         TEXT
//   - score      INTEGER NOT NULL
//   - reasons    TEXT
//   - action     TEXT NOT NULL  （allow / mark / reject）
//   - created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
//
// 索引：idx_spam_log_created / idx_spam_log_sender / idx_spam_log_action
package dao

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// SpamLog 表示 spam_log 表的一行。
type SpamLog struct {
	ID        int64  `json:"id"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	IP        string `json:"ip"`
	Score     int    `json:"score"`
	Reasons   string `json:"reasons"`
	Action    string `json:"action"`
	CreatedAt string `json:"created_at"`
}

// SpamLogDAO 封装 spam_log 表的所有数据库操作。
type SpamLogDAO struct {
	db *db.DB
}

// NewSpamLogDAO 创建 SpamLogDAO。
func NewSpamLogDAO(database *db.DB) *SpamLogDAO {
	return &SpamLogDAO{db: database}
}

// CreateSpamLogInput 创建 spam_log 记录的输入参数。
type CreateSpamLogInput struct {
	Sender    string
	Recipient string
	IP        string
	Score     int
	Reasons   string // 格式化后的原因字符串（分号分隔）
	Action    string // allow / mark / reject
}

// Create 插入一条 spam_log 记录，返回记录 ID。
func (d *SpamLogDAO) Create(ctx context.Context, in CreateSpamLogInput) (int64, error) {
	const q = `INSERT INTO spam_log (sender, recipient, ip, score, reasons, action)
		VALUES (?, ?, ?, ?, ?, ?)`
	res, err := d.db.ExecContext(ctx, q,
		nullable(in.Sender), nullable(in.Recipient), nullable(in.IP),
		in.Score, nullable(in.Reasons), in.Action,
	)
	if err != nil {
		return 0, fmt.Errorf("插入 spam_log 失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取 spam_log ID 失败: %w", err)
	}
	return id, nil
}

// spamLogColumns 是 spam_log 表的所有列（顺序须与 scanSpamLog 一致）。
const spamLogColumns = `id, sender, recipient, ip, score, reasons, action, created_at`

// scanSpamLog 将一行数据扫描到 SpamLog。
func scanSpamLog(s interface {
	Scan(dest ...any) error
}) (*SpamLog, error) {
	var sl SpamLog
	var sender, recipient, ip, reasons sql.NullString
	err := s.Scan(&sl.ID, &sender, &recipient, &ip, &sl.Score, &reasons, &sl.Action, &sl.CreatedAt)
	if err != nil {
		return nil, err
	}
	sl.Sender = sender.String
	sl.Recipient = recipient.String
	sl.IP = ip.String
	sl.Reasons = reasons.String
	return &sl, nil
}

// FindByID 按 ID 查询 spam_log 记录。
func (d *SpamLogDAO) FindByID(ctx context.Context, id int64) (*SpamLog, error) {
	q := fmt.Sprintf("SELECT %s FROM spam_log WHERE id = ?", spamLogColumns)
	row := d.db.QueryRowContext(ctx, q, id)
	sl, err := scanSpamLog(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 spam_log 失败: %w", err)
	}
	return sl, nil
}

// FindRecent 按时间倒序查询最近的 spam_log 记录（分页）。
// limit <= 0 时默认 50，上限 500。
func (d *SpamLogDAO) FindRecent(ctx context.Context, limit, offset int) ([]*SpamLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf("SELECT %s FROM spam_log ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", spamLogColumns)
	rows, err := d.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查询最近 spam_log 失败: %w", err)
	}
	defer rows.Close()
	return scanSpamLogs(rows)
}

// FindByIP 查询指定 IP 的 spam_log 记录（按时间倒序）。
// 用于管理员审计某 IP 的垃圾邮件历史。
func (d *SpamLogDAO) FindByIP(ctx context.Context, ip string, limit int) ([]*SpamLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := fmt.Sprintf("SELECT %s FROM spam_log WHERE ip = ? ORDER BY created_at DESC, id DESC LIMIT ?", spamLogColumns)
	rows, err := d.db.QueryContext(ctx, q, ip, limit)
	if err != nil {
		return nil, fmt.Errorf("按 IP 查询 spam_log 失败: %w", err)
	}
	defer rows.Close()
	return scanSpamLogs(rows)
}

// SpamLogStat spam_log 按 action 分组的统计结果。
type SpamLogStat struct {
	Action string
	Count  int64
}

// StatsByAction 按 action 分组统计最近一段时间内的 spam_log 数量。
// since 为空字符串时统计全部。
func (d *SpamLogDAO) StatsByAction(ctx context.Context, since string) ([]SpamLogStat, error) {
	var q string
	var args []any
	if since == "" {
		q = `SELECT action, COUNT(*) FROM spam_log GROUP BY action ORDER BY action`
	} else {
		q = `SELECT action, COUNT(*) FROM spam_log WHERE created_at >= ? GROUP BY action ORDER BY action`
		args = append(args, since)
	}
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("统计 spam_log 失败: %w", err)
	}
	defer rows.Close()
	var stats []SpamLogStat
	for rows.Next() {
		var s SpamLogStat
		if err := rows.Scan(&s.Action, &s.Count); err != nil {
			return nil, fmt.Errorf("扫描统计行失败: %w", err)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// Count 返回 spam_log 记录总数（监控/测试用）。
func (d *SpamLogDAO) Count(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM spam_log`
	var count int64
	err := d.db.QueryRowContext(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计 spam_log 失败: %w", err)
	}
	return count, nil
}

// scanSpamLogs 扫描多行 spam_log 数据。
func scanSpamLogs(rows *sql.Rows) ([]*SpamLog, error) {
	var list []*SpamLog
	for rows.Next() {
		sl, err := scanSpamLog(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描 spam_log 行失败: %w", err)
		}
		list = append(list, sl)
	}
	return list, rows.Err()
}
