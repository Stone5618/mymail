// Package dao 提供数据访问对象，封装 SQL 操作。
//
// message.go 实现 MessageDAO，对应 messages 表。
//
// 修复的缺陷：
//   - P0-3：所有 :id 操作强制带 user_id 校验（原 Node.js markRead/markUnread/toggleStar 缺失）
//   - P1-1：关键多步操作支持事务（通过 executor 接口接受 *sql.Tx）
//   - P1-4：GetNextUID 在事务内执行，避免并发竞态产生重复 UID
package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// executor 抽象 *db.DB 与 *sql.Tx 共同具备的查询能力。
// 用于让 DAO 方法既能运行在普通连接上，也能运行在事务内（P1-1 修复）。
type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Message 表示 messages 表的完整行。
// 字段名与数据库列名一致（snake_case），JSON 序列化时保持 snake_case
// 以兼容前端（Vue 前端依赖 user_id / from_addr / is_read 等字段名）。
type Message struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Folder      string `json:"folder"`
	MessageID   string `json:"message_id"`
	UID         *int64 `json:"uid"` // 可空（draft 可无 uid）
	FromAddr    string `json:"from_addr"`
	FromName    string `json:"from_name"`
	ToAddr      string `json:"to_addr"`
	CcAddr      string `json:"cc_addr"`
	BccAddr     string `json:"bcc_addr"`
	ReplyTo     string `json:"reply_to"`
	Subject     string `json:"subject"`
	BodyText    string `json:"body_text"`
	BodyHTML    string `json:"body_html"`
	BodyHTMLRaw string `json:"body_html_raw,omitempty"`
	IsRead      bool   `json:"is_read"`
	IsStarred   bool   `json:"is_starred"`
	IsDeleted   bool   `json:"is_deleted"`
	HasAttach   bool   `json:"has_attach"`
	AttachCount int    `json:"attach_count"`
	SizeBytes   int64  `json:"size_bytes"`
	HeadersRaw  string `json:"headers_raw,omitempty"`
	InReplyTo   string `json:"in_reply_to,omitempty"`
	Flags       string `json:"flags"`
	SpamScore   int    `json:"spam_score"`
	SpamReasons string `json:"spam_reasons,omitempty"`
	ReceivedAt  string `json:"received_at"`
}

// messageColumns 是 messages 表的所有列（顺序须与 scanMessage 一致）。
const messageColumns = `id, user_id, folder, message_id, uid, from_addr, from_name,
	to_addr, cc_addr, bcc_addr, reply_to, subject, body_text, body_html, body_html_raw,
	is_read, is_starred, is_deleted, has_attach, attach_count, size_bytes,
	headers_raw, in_reply_to, flags, spam_score, spam_reasons, received_at`

// scanMessage 将一行数据扫描到 Message。
// 使用 sql.Null* 类型处理可空列。
func scanMessage(s interface {
	Scan(dest ...any) error
}) (*Message, error) {
	var m Message
	var (
		messageID   sql.NullString
		uid         sql.NullInt64
		fromName    sql.NullString
		ccAddr      sql.NullString
		bccAddr     sql.NullString
		replyTo     sql.NullString
		subject     sql.NullString
		bodyText    sql.NullString
		bodyHTML     sql.NullString
		bodyHTMLRaw sql.NullString
		headersRaw  sql.NullString
		inReplyTo   sql.NullString
		flags       sql.NullString
		spamReasons sql.NullString
		receivedAt  sql.NullString
		isRead      int
		isStarred   int
		isDeleted   int
		hasAttach   int
	)
	err := s.Scan(
		&m.ID, &m.UserID, &m.Folder, &messageID, &uid, &m.FromAddr, &fromName,
		&m.ToAddr, &ccAddr, &bccAddr, &replyTo, &subject, &bodyText, &bodyHTML, &bodyHTMLRaw,
		&isRead, &isStarred, &isDeleted, &hasAttach, &m.AttachCount, &m.SizeBytes,
		&headersRaw, &inReplyTo, &flags, &m.SpamScore, &spamReasons, &receivedAt,
	)
	if err != nil {
		return nil, err
	}
	m.MessageID = messageID.String
	if uid.Valid {
		v := uid.Int64
		m.UID = &v
	}
	m.FromName = fromName.String
	m.CcAddr = ccAddr.String
	m.BccAddr = bccAddr.String
	m.ReplyTo = replyTo.String
	m.Subject = subject.String
	m.BodyText = bodyText.String
	m.BodyHTML = bodyHTML.String
	m.BodyHTMLRaw = bodyHTMLRaw.String
	m.HeadersRaw = headersRaw.String
	m.InReplyTo = inReplyTo.String
	m.Flags = flags.String
	if m.Flags == "" {
		m.Flags = "[]"
	}
	m.SpamReasons = spamReasons.String
	m.ReceivedAt = receivedAt.String
	m.IsRead = isRead == 1
	m.IsStarred = isStarred == 1
	m.IsDeleted = isDeleted == 1
	m.HasAttach = hasAttach == 1
	return &m, nil
}

// MessageDAO 封装 messages 表的所有数据库操作。
type MessageDAO struct {
	db *db.DB
}

// NewMessageDAO 创建 MessageDAO。
func NewMessageDAO(database *db.DB) *MessageDAO {
	return &MessageDAO{db: database}
}

// CreateMessageInput 创建邮件的输入参数。
type CreateMessageInput struct {
	UserID      int64
	Folder      string // 空则 "INBOX"
	MessageID   string // 可空
	UID         *int64 // 可空
	FromAddr    string
	FromName    string
	ToAddr      string
	CcAddr      string // 可空
	BccAddr     string // 可空
	ReplyTo     string // 可空
	Subject     string
	BodyText    string
	BodyHTML    string
	BodyHTMLRaw string // 原始 HTML（净化前），可空
	HasAttach   bool
	AttachCount int
	SizeBytes   int64
	HeadersRaw  string // 可空
	InReplyTo   string // 可空
}

// Create 插入新邮件，返回新邮件 ID。
// 不在事务内执行；如需事务，用 CreateTx。
func (d *MessageDAO) Create(ctx context.Context, in CreateMessageInput) (int64, error) {
	return d.CreateOn(ctx, d.db, in)
}

// CreateOn 在指定 executor 上插入新邮件（可用于事务）。
func (d *MessageDAO) CreateOn(ctx context.Context, ex executor, in CreateMessageInput) (int64, error) {
	if in.Folder == "" {
		in.Folder = "INBOX"
	}
	const q = `INSERT INTO messages
		(user_id, folder, message_id, uid, from_addr, from_name, to_addr,
		 cc_addr, bcc_addr, reply_to, subject, body_text, body_html, body_html_raw,
		 has_attach, attach_count, size_bytes, headers_raw, in_reply_to)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	var cc, bcc, replyTo, headers, inReplyTo any
	if in.CcAddr != "" {
		cc = in.CcAddr
	}
	if in.BccAddr != "" {
		bcc = in.BccAddr
	}
	if in.ReplyTo != "" {
		replyTo = in.ReplyTo
	}
	if in.HeadersRaw != "" {
		headers = in.HeadersRaw
	}
	if in.InReplyTo != "" {
		inReplyTo = in.InReplyTo
	}
	hasAttach := 0
	if in.HasAttach {
		hasAttach = 1
	}
	res, err := ex.ExecContext(ctx, q,
		in.UserID, in.Folder, nullable(in.MessageID), in.UID, in.FromAddr, nullable(in.FromName),
		in.ToAddr, cc, bcc, replyTo, nullable(in.Subject), nullable(in.BodyText), nullable(in.BodyHTML),
		nullable(in.BodyHTMLRaw), hasAttach, in.AttachCount, in.SizeBytes, headers, inReplyTo,
	)
	if err != nil {
		return 0, fmt.Errorf("创建邮件失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取邮件 ID 失败: %w", err)
	}
	return id, nil
}

// nullable 空字符串转 nil（用于插入 NULL）。
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// FindByID 根据 ID 查询邮件（不校验归属，仅用于内部场景如 SMTP 接收）。
// 业务侧应使用 FindByIDForUser。
func (d *MessageDAO) FindByID(ctx context.Context, id int64) (*Message, error) {
	q := fmt.Sprintf("SELECT %s FROM messages WHERE id = ?", messageColumns)
	row := d.db.QueryRowContext(ctx, q, id)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询邮件 by id 失败: %w", err)
	}
	return m, nil
}

// FindByIDForUser 根据 ID 查询邮件，并校验归属于指定用户。
// 修复 P0-3：所有 :id 端点必须校验归属。
// 返回 (nil, nil) 表示邮件不存在或不属于该用户（不区分，防止枚举）。
func (d *MessageDAO) FindByIDForUser(ctx context.Context, id, userID int64) (*Message, error) {
	q := fmt.Sprintf("SELECT %s FROM messages WHERE id = ? AND user_id = ?", messageColumns)
	row := d.db.QueryRowContext(ctx, q, id, userID)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询邮件 by id+user 失败: %w", err)
	}
	return m, nil
}

// ListResult 列表查询结果。
type ListResult struct {
	Messages    []*Message `json:"messages"`
	Total       int64      `json:"total"`
	Page        int        `json:"page"`
	Limit       int        `json:"limit"`
	UnreadCount int64      `json:"unreadCount"` // 当前文件夹未读数（兼容前端）
}

// ListByUser 分页查询用户的邮件。
// 支持文件夹、搜索、仅未读过滤。
// 排序：received_at DESC（与原 Node.js 一致）。
func (d *MessageDAO) ListByUser(ctx context.Context, userID int64, folder string, page, limit int, search string, unreadOnly bool) (*ListResult, error) {
	if folder == "" {
		folder = "INBOX"
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	offset := (page - 1) * limit

	// 构造 WHERE
	var sb strings.Builder
	sb.WriteString("WHERE user_id = ? AND folder = ? AND is_deleted = 0")
	args := []any{userID, folder}
	if unreadOnly {
		sb.WriteString(" AND is_read = 0")
	}
	if search != "" {
		sb.WriteString(" AND (from_addr LIKE ? OR from_name LIKE ? OR subject LIKE ? OR body_text LIKE ?)")
		s := "%" + search + "%"
		args = append(args, s, s, s, s)
	}
	where := sb.String()

	// 总数
	var total int64
	countQ := "SELECT COUNT(*) FROM messages " + where
	if err := d.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("统计邮件总数失败: %w", err)
	}

	// 列表
	listQ := fmt.Sprintf("SELECT %s FROM messages %s ORDER BY received_at DESC LIMIT ? OFFSET ?",
		messageColumns, where)
	listArgs := append(args, limit, offset)
	rows, err := d.db.QueryContext(ctx, listQ, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("查询邮件列表失败: %w", err)
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描邮件行失败: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历邮件行失败: %w", err)
	}

	// 当前文件夹未读数
	unread, err := d.UnreadCount(ctx, userID, folder)
	if err != nil {
		return nil, err
	}

	return &ListResult{
		Messages:    messages,
		Total:       total,
		Page:        page,
		Limit:       limit,
		UnreadCount: unread,
	}, nil
}

// UnreadCount 返回指定用户、文件夹的未读邮件数。
// folder 为空则查 INBOX。
func (d *MessageDAO) UnreadCount(ctx context.Context, userID int64, folder string) (int64, error) {
	if folder == "" {
		folder = "INBOX"
	}
	const q = `SELECT COUNT(*) FROM messages
		WHERE user_id = ? AND folder = ? AND is_read = 0 AND is_deleted = 0`
	var count int64
	err := d.db.QueryRowContext(ctx, q, userID, folder).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计未读数失败: %w", err)
	}
	return count, nil
}

// UnreadCountByFolders 批量返回多个文件夹的未读数。
// 与原 Node.js /api/mail/unread-count 端点契约一致：
// 仅查 INBOX/SENT/DRAFTS/TRASH（不含 JUNK）。
func (d *MessageDAO) UnreadCountByFolders(ctx context.Context, userID int64, folders []string) (map[string]int64, error) {
	result := make(map[string]int64, len(folders))
	for _, f := range folders {
		c, err := d.UnreadCount(ctx, userID, f)
		if err != nil {
			return nil, err
		}
		result[f] = c
	}
	return result, nil
}

// MarkRead 标记邮件为已读。
// 修复 P0-3：必须校验 user_id 归属。
func (d *MessageDAO) MarkRead(ctx context.Context, id, userID int64) error {
	const q = `UPDATE messages SET is_read = 1 WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("标记已读失败: %w", err)
	}
	return nil
}

// MarkUnread 标记邮件为未读。
// 修复 P0-3：必须校验 user_id 归属。
func (d *MessageDAO) MarkUnread(ctx context.Context, id, userID int64) error {
	const q = `UPDATE messages SET is_read = 0 WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("标记未读失败: %w", err)
	}
	return nil
}

// ToggleStar 切换星标状态。
// 修复 P0-3：必须校验 user_id 归属。
func (d *MessageDAO) ToggleStar(ctx context.Context, id, userID int64) error {
	const q = `UPDATE messages SET is_starred = CASE WHEN is_starred = 1 THEN 0 ELSE 1 END
		WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("切换星标失败: %w", err)
	}
	return nil
}

// MoveToFolder 移动邮件到指定文件夹。
// 修复 P0-3：必须校验 user_id 归属。
func (d *MessageDAO) MoveToFolder(ctx context.Context, id, userID int64, folder string) error {
	const q = `UPDATE messages SET folder = ? WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, folder, id, userID)
	if err != nil {
		return fmt.Errorf("移动邮件失败: %w", err)
	}
	return nil
}

// Restore 从回收站恢复邮件到 INBOX，并清除软删除标志。
// 修复 P0-3：必须校验 user_id 归属。
// 注意：与 MoveToFolder 不同，Restore 必须同时清除 is_deleted 标志，
// 否则 ListByUser 的 `WHERE is_deleted = 0` 过滤会排除已恢复的邮件。
func (d *MessageDAO) Restore(ctx context.Context, id, userID int64) error {
	const q = `UPDATE messages SET is_deleted = 0, folder = 'INBOX' WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("恢复邮件失败: %w", err)
	}
	return nil
}

// SoftDelete 软删除（移到 TRASH）。
// 修复 P0-3：必须校验 user_id 归属。
func (d *MessageDAO) SoftDelete(ctx context.Context, id, userID int64) error {
	const q = `UPDATE messages SET is_deleted = 1, folder = 'TRASH' WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("软删除邮件失败: %w", err)
	}
	return nil
}

// PermanentDelete 永久删除。
// 修复 P0-3：必须校验 user_id 归属。
func (d *MessageDAO) PermanentDelete(ctx context.Context, id, userID int64) error {
	const q = `DELETE FROM messages WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("永久删除邮件失败: %w", err)
	}
	return nil
}

// EmptyTrash 清空用户垃圾箱。
// 修复 P1-1：使用事务保证原子性（删除附件记录 + 邮件记录）。
// 修复 P0-7 附件文件删除由服务层负责（DAO 不处理文件系统）。
func (d *MessageDAO) EmptyTrash(ctx context.Context, userID int64) error {
	return d.db.RunInTransaction(ctx, func(tx *sql.Tx) error {
		// 1. 查询所有待删除邮件的 ID（供服务层删除附件文件）
		//    这里只删除 DB 记录，文件由服务层在调用前后处理
		const delMsg = `DELETE FROM messages WHERE user_id = ? AND folder = 'TRASH'`
		if _, err := tx.ExecContext(ctx, delMsg, userID); err != nil {
			return fmt.Errorf("删除垃圾箱邮件失败: %w", err)
		}
		// 附件记录由 ON DELETE CASCADE 自动删除（schema 已配置）
		return nil
	})
}

// FindTrashMessageIDs 返回用户垃圾箱中所有邮件 ID。
// 用于 EmptyTrash 前获取附件文件路径以清理文件系统。
func (d *MessageDAO) FindTrashMessageIDs(ctx context.Context, userID int64) ([]int64, error) {
	const q = `SELECT id FROM messages WHERE user_id = ? AND folder = 'TRASH'`
	rows, err := d.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("查询垃圾箱邮件 ID 失败: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CleanOldTrash 清理指定天数前的垃圾箱邮件。
// 修复 P1-1：使用事务。
func (d *MessageDAO) CleanOldTrash(ctx context.Context, userID int64, days int) error {
	return d.db.RunInTransaction(ctx, func(tx *sql.Tx) error {
		const q = `DELETE FROM messages
			WHERE user_id = ? AND folder = 'TRASH'
			AND received_at < datetime('now', ?)`
		daysNeg := fmt.Sprintf("-%d days", days)
		if _, err := tx.ExecContext(ctx, q, userID, daysNeg); err != nil {
			return fmt.Errorf("清理旧垃圾箱失败: %w", err)
		}
		return nil
	})
}

// TotalSize 返回用户所有邮件占用的总字节数。
func (d *MessageDAO) TotalSize(ctx context.Context, userID int64) (int64, error) {
	const q = `SELECT COALESCE(SUM(size_bytes), 0) FROM messages WHERE user_id = ?`
	var total int64
	err := d.db.QueryRowContext(ctx, q, userID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("统计邮件总大小失败: %w", err)
	}
	return total, nil
}

// TodaySentCount 返回今日已发送邮件数（全局）。
func (d *MessageDAO) TodaySentCount(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM messages WHERE folder = 'SENT' AND received_at >= date('now')`
	var count int64
	err := d.db.QueryRowContext(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计今日发送数失败: %w", err)
	}
	return count, nil
}

// TodayReceivedCount 返回今日已接收邮件数（全局）。
func (d *MessageDAO) TodayReceivedCount(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM messages WHERE folder = 'INBOX' AND received_at >= date('now')`
	var count int64
	err := d.db.QueryRowContext(ctx, q).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计今日接收数失败: %w", err)
	}
	return count, nil
}

// GetNextUID 返回用户下一个 UID（在事务内执行，修复 P1-4 竞态）。
// 必须在事务内调用，否则并发场景下会产生重复 UID。
// 用法：
//
//	err := db.RunInTransaction(ctx, func(tx *sql.Tx) error {
//	    uid, err := messageDAO.GetNextUIDTx(ctx, tx, userID)
//	    // ... 使用 uid 创建邮件
//	})
func (d *MessageDAO) GetNextUID(ctx context.Context, userID int64) (int64, error) {
	return d.GetNextUIDOn(ctx, d.db, userID)
}

// GetNextUIDOn 在指定 executor 上获取下一个 UID。
// 修复 P1-4：在事务内执行 SELECT MAX(uid) + 1，配合事务隔离避免竞态。
// SQLite 的写事务是串行的，故不会出现重复。
func (d *MessageDAO) GetNextUIDOn(ctx context.Context, ex executor, userID int64) (int64, error) {
	const q = `SELECT COALESCE(MAX(uid), 0) + 1 FROM messages WHERE user_id = ?`
	var next int64
	err := ex.QueryRowContext(ctx, q, userID).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("获取 next_uid 失败: %w", err)
	}
	return next, nil
}

// FindByUID 根据 UID 查询邮件（Dovecot 集成用）。
func (d *MessageDAO) FindByUID(ctx context.Context, userID, uid int64) (*Message, error) {
	q := fmt.Sprintf("SELECT %s FROM messages WHERE user_id = ? AND uid = ?", messageColumns)
	row := d.db.QueryRowContext(ctx, q, userID, uid)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询邮件 by uid 失败: %w", err)
	}
	return m, nil
}

// UpdateFlags 更新邮件的 IMAP flags（JSON 字符串）。
// 修复 P0-3：必须校验 user_id 归属。
func (d *MessageDAO) UpdateFlags(ctx context.Context, id, userID int64, flags string) error {
	const q = `UPDATE messages SET flags = ? WHERE id = ? AND user_id = ?`
	_, err := d.db.ExecContext(ctx, q, flags, id, userID)
	if err != nil {
		return fmt.Errorf("更新 flags 失败: %w", err)
	}
	return nil
}

// BatchMarkRead 批量标记已读。
// 修复 P0-3：限定 user_id。
// 返回受影响行数。
func (d *MessageDAO) BatchMarkRead(ctx context.Context, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := []any{userID}
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	q := fmt.Sprintf("UPDATE messages SET is_read = 1 WHERE user_id = ? AND id IN (%s)",
		strings.Join(placeholders, ","))
	res, err := d.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("批量标记已读失败: %w", err)
	}
	return res.RowsAffected()
}

// BatchMove 批量移动到指定文件夹。
// 修复 P0-3：限定 user_id。
func (d *MessageDAO) BatchMove(ctx context.Context, userID int64, ids []int64, folder string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := []any{folder, userID}
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	q := fmt.Sprintf("UPDATE messages SET folder = ? WHERE user_id = ? AND id IN (%s)",
		strings.Join(placeholders, ","))
	res, err := d.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("批量移动失败: %w", err)
	}
	return res.RowsAffected()
}

// BatchSoftDelete 批量软删除（移到 TRASH）。
// 修复 P0-3：限定 user_id。
func (d *MessageDAO) BatchSoftDelete(ctx context.Context, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := []any{userID}
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	q := fmt.Sprintf("UPDATE messages SET is_deleted = 1, folder = 'TRASH' WHERE user_id = ? AND id IN (%s)",
		strings.Join(placeholders, ","))
	res, err := d.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("批量软删除失败: %w", err)
	}
	return res.RowsAffected()
}

// DB 返回底层 *db.DB（供服务层做事务编排）。
func (d *MessageDAO) DB() *db.DB {
	return d.db
}
