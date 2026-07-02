// Package audit
// dao.go 提供 audit_log 表的数据访问。
package audit

import (
	"context"
	"fmt"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// AuditDAO 封装 audit_log 表操作。
type AuditDAO struct {
	db *db.DB
}

// NewAuditDAO 创建 AuditDAO。
func NewAuditDAO(database *db.DB) *AuditDAO {
	return &AuditDAO{db: database}
}

// Insert 插入一条审计日志。
// detail 为详细描述（JSON 字符串），用于事后追溯。
func (d *AuditDAO) Insert(ctx context.Context, e Entry) error {
	const q = `INSERT INTO audit_log
		(timestamp, actor_type, actor_id, actor_ip, action, resource_type, resource_id, result, detail, request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	var actorID any
	if e.ActorID != nil {
		actorID = *e.ActorID
	}
	_, err := d.db.ExecContext(ctx, q,
		e.Timestamp.UTC().Format("2006-01-02 15:04:05"),
		string(e.ActorType),
		actorID,
		e.ActorIP,
		e.Action,
		e.ResourceType,
		e.ResourceID,
		string(e.Result),
		e.Detail,
		e.RequestID,
	)
	if err != nil {
		return fmt.Errorf("插入审计日志失败: %w", err)
	}
	return nil
}
