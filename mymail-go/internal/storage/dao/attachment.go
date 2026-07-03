// Package dao
// attachment.go 实现 AttachmentDAO，对应 attachments 表。
//
// 修复的缺陷：
//   - P1-1：关键多步操作支持事务（CreateOn / DeleteByMessageIDOn）
//   - P0-3：FindByIDForUser 校验邮件归属
package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// Attachment 表示 attachments 表的完整行。
// 字段名与数据库列名一致（snake_case），JSON 序列化保持 snake_case。
type Attachment struct {
	ID          int64  `json:"id"`
	MessageID   int64  `json:"message_id"`
	Filename    string `json:"filename"`
	MimeType    string `json:"mime_type"`
	SizeBytes   int64  `json:"size_bytes"`
	StoragePath string `json:"-"` // 不暴露给前端（防止泄露服务器路径）
	CreatedAt   string `json:"created_at"`
}

// attachmentColumns 是 attachments 表的所有列（顺序须与 scanAttachment 一致）。
const attachmentColumns = `id, message_id, filename, mime_type, size_bytes, storage_path, created_at`

// scanAttachment 将一行数据扫描到 Attachment。
func scanAttachment(s interface {
	Scan(dest ...any) error
}) (*Attachment, error) {
	var a Attachment
	var (
		mimeType sql.NullString
		size     sql.NullInt64
	)
	err := s.Scan(&a.ID, &a.MessageID, &a.Filename, &mimeType, &size, &a.StoragePath, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	a.MimeType = mimeType.String
	a.SizeBytes = size.Int64
	return &a, nil
}

// AttachmentDAO 封装 attachments 表的所有数据库操作。
type AttachmentDAO struct {
	db *db.DB
}

// NewAttachmentDAO 创建 AttachmentDAO。
func NewAttachmentDAO(database *db.DB) *AttachmentDAO {
	return &AttachmentDAO{db: database}
}

// CreateAttachmentInput 创建附件的输入参数。
type CreateAttachmentInput struct {
	MessageID   int64
	Filename    string
	MimeType    string
	SizeBytes   int64
	StoragePath string
}

// Create 插入新附件，返回新附件 ID。
func (d *AttachmentDAO) Create(ctx context.Context, in CreateAttachmentInput) (int64, error) {
	return d.CreateOn(ctx, d.db, in)
}

// CreateOn 在指定 executor 上插入新附件（可用于事务）。
func (d *AttachmentDAO) CreateOn(ctx context.Context, ex executor, in CreateAttachmentInput) (int64, error) {
	const q = `INSERT INTO attachments (message_id, filename, mime_type, size_bytes, storage_path)
		VALUES (?, ?, ?, ?, ?)`
	res, err := ex.ExecContext(ctx, q,
		in.MessageID, in.Filename, nullable(in.MimeType), in.SizeBytes, in.StoragePath,
	)
	if err != nil {
		return 0, fmt.Errorf("创建附件失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取附件 ID 失败: %w", err)
	}
	return id, nil
}

// FindByMessageID 查询某邮件的所有附件。
func (d *AttachmentDAO) FindByMessageID(ctx context.Context, messageID int64) ([]*Attachment, error) {
	q := fmt.Sprintf("SELECT %s FROM attachments WHERE message_id = ? ORDER BY id", attachmentColumns)
	rows, err := d.db.QueryContext(ctx, q, messageID)
	if err != nil {
		return nil, fmt.Errorf("查询附件 by message_id 失败: %w", err)
	}
	defer rows.Close()
	var list []*Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描附件行失败: %w", err)
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// FindByID 根据 ID 查询附件（不校验归属，仅用于内部场景）。
func (d *AttachmentDAO) FindByID(ctx context.Context, id int64) (*Attachment, error) {
	q := fmt.Sprintf("SELECT %s FROM attachments WHERE id = ?", attachmentColumns)
	row := d.db.QueryRowContext(ctx, q, id)
	a, err := scanAttachment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询附件 by id 失败: %w", err)
	}
	return a, nil
}

// FindByIDForUser 根据 ID 查询附件，并校验邮件归属于指定用户。
// 修复 P0-3：附件下载必须校验邮件归属。
// 返回 (nil, nil) 表示附件不存在或不属于该用户。
func (d *AttachmentDAO) FindByIDForUser(ctx context.Context, id, userID int64) (*Attachment, error) {
	const q = `SELECT a.id, a.message_id, a.filename, a.mime_type, a.size_bytes, a.storage_path, a.created_at
		FROM attachments a INNER JOIN messages m ON a.message_id = m.id
		WHERE a.id = ? AND m.user_id = ?`
	row := d.db.QueryRowContext(ctx, q, id, userID)
	a, err := scanAttachment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询附件 by id+user 失败: %w", err)
	}
	return a, nil
}

// DeleteByMessageID 删除某邮件的所有附件记录。
// 注意：仅删除 DB 记录，文件由服务层负责。
func (d *AttachmentDAO) DeleteByMessageID(ctx context.Context, messageID int64) error {
	const q = `DELETE FROM attachments WHERE message_id = ?`
	_, err := d.db.ExecContext(ctx, q, messageID)
	if err != nil {
		return fmt.Errorf("删除附件 by message_id 失败: %w", err)
	}
	return nil
}

// DeleteByMessageIDOn 在指定 executor 上删除某邮件的所有附件记录。
func (d *AttachmentDAO) DeleteByMessageIDOn(ctx context.Context, ex executor, messageID int64) error {
	const q = `DELETE FROM attachments WHERE message_id = ?`
	_, err := ex.ExecContext(ctx, q, messageID)
	if err != nil {
		return fmt.Errorf("删除附件 by message_id 失败: %w", err)
	}
	return nil
}

// FindByMessageIDs 批量查询多个邮件的附件（用于 EmptyTrash 清理文件）。
func (d *AttachmentDAO) FindByMessageIDs(ctx context.Context, messageIDs []int64) ([]*Attachment, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf("SELECT %s FROM attachments WHERE message_id IN (%s)",
		attachmentColumns, strings.Join(placeholders, ","))
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("批量查询附件失败: %w", err)
	}
	defer rows.Close()
	var list []*Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描附件行失败: %w", err)
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
