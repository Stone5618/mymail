// Package audit 提供审计日志能力。
// 企业级要求：记录谁（who）在何时（when）从哪里（where）对什么（what）做了什么操作（action）结果如何（result）。
//
// 双写策略：
//  1. 写入 SQLite audit_log 表（便于查询、保留期管理）
//  2. 写入 JSONL 文件（便于离线归档、防篡改）
//
// 仅记录安全相关操作：登录、密码修改、用户管理、API Key 操作、管理员操作等。
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// ActorType 操作者类型。
type ActorType string

const (
	ActorUser  ActorType = "user"
	ActorAdmin ActorType = "admin"
	ActorAPI   ActorType = "api"
	ActorSMTP  ActorType = "smtp"
	ActorSystem ActorType = "system"
)

// Result 操作结果。
type Result string

const (
	ResultSuccess Result = "success"
	ResultFailure Result = "failure"
	ResultDenied  Result = "denied"
)

// Entry 表示一条审计日志。
type Entry struct {
	Timestamp    time.Time  `json:"timestamp"`
	ActorType    ActorType  `json:"actor_type"`
	ActorID      *int64     `json:"actor_id,omitempty"`
	ActorIP      string     `json:"actor_ip,omitempty"`
	Action       string     `json:"action"`
	ResourceType string     `json:"resource_type,omitempty"`
	ResourceID   string     `json:"resource_id,omitempty"`
	Result       Result     `json:"result"`
	Detail       string     `json:"detail,omitempty"`
	RequestID    string     `json:"request_id,omitempty"`
}

// Logger 是审计日志器。
type Logger struct {
	dao      *AuditDAO
	jsonlMu  sync.Mutex
	jsonlFile *os.File
	jsonlPath string
	enabled  bool
}

// NewLogger 创建审计日志器。
// 若 auditEnabled=false，记录操作将变为 no-op（仍可调用，但不写入）。
// jsonlPath 为空则不写 JSONL 文件。
func NewLogger(database *db.DB, auditEnabled bool, jsonlPath string) (*Logger, error) {
	l := &Logger{
		dao:      NewAuditDAO(database),
		enabled:  auditEnabled,
		jsonlPath: jsonlPath,
	}
	if auditEnabled && jsonlPath != "" {
		if err := os.MkdirAll(filepath.Dir(jsonlPath), 0755); err != nil {
			return nil, fmt.Errorf("创建审计日志目录失败: %w", err)
		}
		f, err := os.OpenFile(jsonlPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
		if err != nil {
			return nil, fmt.Errorf("打开审计日志文件失败: %w", err)
		}
		l.jsonlFile = f
	}
	return l, nil
}

// Log 记录一条审计日志（同步写入 DB + JSONL）。
// 推荐用 Record（异步）以避免阻塞业务请求。
func (l *Logger) Log(ctx context.Context, e Entry) {
	if !l.enabled {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	// 1. 写 DB
	if err := l.dao.Insert(ctx, e); err != nil {
		slog.Error("审计日志写入 DB 失败", "error", err, "action", e.Action)
	}
	// 2. 写 JSONL
	if l.jsonlFile != nil {
		l.jsonlMu.Lock()
		defer l.jsonlMu.Unlock()
		line, err := json.Marshal(e)
		if err != nil {
			slog.Error("审计日志序列化失败", "error", err, "action", e.Action)
			return
		}
		line = append(line, '\n')
		if _, err := l.jsonlFile.Write(line); err != nil {
			slog.Error("审计日志写入 JSONL 失败", "error", err, "action", e.Action)
		}
	}
}

// Record 异步记录审计日志（不阻塞调用方）。
// 适用于业务流程中的审计埋点。
func (l *Logger) Record(ctx context.Context, e Entry) {
	if !l.enabled {
		return
	}
	go l.Log(ctx, e)
}

// Close 关闭 JSONL 文件句柄。
func (l *Logger) Close() error {
	if l.jsonlFile != nil {
		return l.jsonlFile.Close()
	}
	return nil
}
