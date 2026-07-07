// Package audit
// dao.go 提供 audit_log 表的数据访问。
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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

// AuditFilter 审计日志查询过滤条件（AC-6）。
// 所有字段为空表示不过滤该字段。
type AuditFilter struct {
	ActorType string    // admin/user/api/smtp/system
	Action    string    // 如 admin.user.create（前缀匹配，传 "admin.user" 可匹配所有 admin.user.* 动作）
	Result    string    // success/failure/denied
	StartTime time.Time // 时间范围起点（包含）
	EndTime   time.Time // 时间范围终点（包含）
}

// AuditLogRow 审计日志查询结果行。
type AuditLogRow struct {
	ID           int64
	Timestamp    time.Time
	ActorType    string
	ActorID      sql.NullInt64
	ActorIP      sql.NullString
	Action       string
	ResourceType sql.NullString
	ResourceID   sql.NullString
	Result       string
	Detail       sql.NullString
	RequestID    sql.NullString
}

// List 分页查询审计日志（AC-6）。
// 利用现有索引 idx_audit_timestamp/idx_audit_actor/idx_audit_action。
// 返回 (rows, total, error)，total 为满足条件的总数（用于分页）。
func (d *AuditDAO) List(ctx context.Context, filter AuditFilter, limit, offset int) ([]*AuditLogRow, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// 构造 WHERE
	var (
		sb    strings.Builder
		args  []any
		first = true
	)
	addCond := func(cond string, arg any) {
		if first {
			sb.WriteString(" WHERE ")
			first = false
		} else {
			sb.WriteString(" AND ")
		}
		sb.WriteString(cond)
		args = append(args, arg)
	}
	if filter.ActorType != "" {
		addCond("actor_type = ?", filter.ActorType)
	}
	if filter.Action != "" {
		// 前缀匹配：传 "admin.user" 可匹配 admin.user.create / admin.user.delete 等
		addCond("action LIKE ?", filter.Action+"%")
	}
	if filter.Result != "" {
		addCond("result = ?", filter.Result)
	}
	if !filter.StartTime.IsZero() {
		addCond("timestamp >= ?", filter.StartTime.UTC().Format("2006-01-02 15:04:05"))
	}
	if !filter.EndTime.IsZero() {
		addCond("timestamp <= ?", filter.EndTime.UTC().Format("2006-01-02 15:04:05"))
	}
	where := sb.String()

	// 总数
	var total int64
	countQ := "SELECT COUNT(*) FROM audit_log" + where
	if err := d.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计审计日志总数失败: %w", err)
	}

	// 列表（按时间倒序，最新在前）
	listQ := "SELECT id, timestamp, actor_type, actor_id, actor_ip, action, resource_type, resource_id, result, detail, request_id FROM audit_log" +
		where + " ORDER BY timestamp DESC, id DESC LIMIT ? OFFSET ?"
	listArgs := append(args, limit, offset)
	rows, err := d.db.QueryContext(ctx, listQ, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询审计日志列表失败: %w", err)
	}
	defer rows.Close()

	var result []*AuditLogRow
	for rows.Next() {
		var r AuditLogRow
		var ts string
		if err := rows.Scan(&r.ID, &ts, &r.ActorType, &r.ActorID, &r.ActorIP, &r.Action,
			&r.ResourceType, &r.ResourceID, &r.Result, &r.Detail, &r.RequestID); err != nil {
			return nil, 0, fmt.Errorf("扫描审计日志行失败: %w", err)
		}
		// timestamp 列为 DATETIME，SQLite 返回 "2006-01-02 15:04:05" 字符串
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", ts, time.UTC); err == nil {
			r.Timestamp = t
		} else {
			r.Timestamp = time.Now() // fallback，理论上不会走到
		}
		result = append(result, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历审计日志行失败: %w", err)
	}
	return result, total, nil
}

// DeleteBefore 分批删除 timestamp 早于 before 的审计日志（AC-8）。
// 每批 batchSize 条，批次间休眠 50ms 避免影响业务。
// 返回实际删除的总条数。
//
// 设计要点：
//   - SQLite WAL 模式下 DELETE 不阻塞读写
//   - 分批删除避免长事务锁表
//   - 子查询 LIMIT 避免全表扫描一次性删除大量数据
func (d *AuditDAO) DeleteBefore(ctx context.Context, before time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	cutoff := before.UTC().Format("2006-01-02 15:04:05")
	var totalDeleted int64
	for {
		res, err := d.db.ExecContext(ctx,
			`DELETE FROM audit_log WHERE id IN (
				SELECT id FROM audit_log WHERE timestamp < ? ORDER BY id LIMIT ?
			)`, cutoff, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("删除审计日志失败: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("获取删除行数失败: %w", err)
		}
		totalDeleted += affected
		if affected < int64(batchSize) {
			break // 没有更多数据
		}
		// 批次间短暂休眠，避免影响业务
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return totalDeleted, nil
}
