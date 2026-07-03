// admin_audit_test.go 验证管理员操作的审计日志记录。
//
// 验收标准 9.6 第 5 项：审计日志记录所有管理员操作。
//
// 测试覆盖：
//   - CreateUser → audit_log 记录 admin.user.create
//   - UpdateUser → audit_log 记录 admin.user.update
//   - DeleteUser → audit_log 记录 admin.user.delete
//   - UpdateSettings → audit_log 记录 admin.settings.update
//   - ActorType 为 "admin"
package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// auditEntry 简化审计日志记录（仅用于测试断言）。
type auditEntry struct {
	Action       string
	ActorType    string
	ResourceType string
	Result       string
}

// queryAuditLog 查询 audit_log 表中的所有记录。
func queryAuditLog(t *testing.T, database *db.DB) []auditEntry {
	t.Helper()
	rows, err := database.QueryContext(context.Background(),
		`SELECT action, actor_type, resource_type, result FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatalf("查询 audit_log 失败: %v", err)
	}
	defer rows.Close()
	var entries []auditEntry
	for rows.Next() {
		var e auditEntry
		if err := rows.Scan(&e.Action, &e.ActorType, &e.ResourceType, &e.Result); err != nil {
			t.Fatalf("扫描 audit_log 行失败: %v", err)
		}
		entries = append(entries, e)
	}
	return entries
}

// findAction 在 audit_log 记录中查找指定 action，返回是否找到。
func findAction(entries []auditEntry, action string) (auditEntry, bool) {
	for _, e := range entries {
		if e.Action == action {
			return e, true
		}
	}
	return auditEntry{}, false
}

// newAdminServiceWithAudit 创建启用审计的 AdminService（auditEnabled=true）。
func newAdminServiceWithAudit(t *testing.T) (*AdminService, *db.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// 关键：auditEnabled=true，使审计日志实际写入 DB
	auditLogger, err := audit.NewLogger(database, true, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}

	userDAO := dao.NewUserDAO(database)
	settingsDAO := dao.NewSettingsDAO(database)
	svc := NewAdminService(userDAO, settingsDAO, auditLogger)
	return svc, database
}

func TestAdminService_AuditLog_CreateUser(t *testing.T) {
	svc, database := newAdminServiceWithAudit(t)
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, CreateUserAdminInput{
		Username: "newuser", Email: "new@example.com",
		Password: "pass123", Role: "user",
	})
	if err != nil {
		t.Fatalf("CreateUser 失败: %v", err)
	}

	// 等待异步审计日志写入（Record 用 goroutine）
	time.Sleep(100 * time.Millisecond)

	entries := queryAuditLog(t, database)
	e, found := findAction(entries, "admin.user.create")
	if !found {
		t.Fatalf("audit_log 应包含 admin.user.create，实际记录: %+v", entries)
	}
	if e.ActorType != "admin" {
		t.Errorf("ActorType 期望 'admin'，实际 %q", e.ActorType)
	}
	if e.ResourceType != "user" {
		t.Errorf("ResourceType 期望 'user'，实际 %q", e.ResourceType)
	}
	if e.Result != "success" {
		t.Errorf("Result 期望 'success'，实际 %q", e.Result)
	}
}

func TestAdminService_AuditLog_UpdateUser(t *testing.T) {
	svc, database := newAdminServiceWithAudit(t)
	ctx := context.Background()

	// 先创建用户
	u, _ := svc.CreateUser(ctx, CreateUserAdminInput{
		Username: "updateme", Email: "upd@example.com",
		Password: "pass123", Role: "user",
	})
	time.Sleep(100 * time.Millisecond) // 等待 create 审计

	// 更新
	role := "admin"
	if err := svc.UpdateUser(ctx, u.ID, UpdateUserAdminInput{Role: &role}); err != nil {
		t.Fatalf("UpdateUser 失败: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	entries := queryAuditLog(t, database)
	if _, found := findAction(entries, "admin.user.update"); !found {
		t.Errorf("audit_log 应包含 admin.user.update，实际记录: %+v", entries)
	}
}

func TestAdminService_AuditLog_DeleteUser(t *testing.T) {
	svc, database := newAdminServiceWithAudit(t)
	ctx := context.Background()

	u, _ := svc.CreateUser(ctx, CreateUserAdminInput{
		Username: "deleteme", Email: "del@example.com",
		Password: "pass123", Role: "user",
	})
	time.Sleep(100 * time.Millisecond)

	if err := svc.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser 失败: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	entries := queryAuditLog(t, database)
	if _, found := findAction(entries, "admin.user.delete"); !found {
		t.Errorf("audit_log 应包含 admin.user.delete，实际记录: %+v", entries)
	}
}

func TestAdminService_AuditLog_UpdateSettings(t *testing.T) {
	svc, database := newAdminServiceWithAudit(t)
	ctx := context.Background()

	if err := svc.UpdateSettings(ctx, map[string]string{
		"site_name": "MyMail",
		"max_users": "100",
	}); err != nil {
		t.Fatalf("UpdateSettings 失败: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	entries := queryAuditLog(t, database)
	if _, found := findAction(entries, "admin.settings.update"); !found {
		t.Errorf("audit_log 应包含 admin.settings.update，实际记录: %+v", entries)
	}
}
