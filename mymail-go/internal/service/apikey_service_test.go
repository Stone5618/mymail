// Package service
// apikey_service_test.go 测试 APIKeyService 完整业务逻辑。
// 使用真实 SQLite 临时数据库，覆盖所有成功与错误路径。
//
// 测试覆盖：
//   - Create：成功（返回明文+前缀）、达上限、Name 为空
//   - ListByUser
//   - Delete：成功、校验归属（403）、不存在（404）
//   - Verify：成功、错误明文（401）、格式错误、不存在（401）
package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// newTestDBForService 创建临时 SQLite 数据库并执行迁移（service 包测试辅助）。
// 参考 dao/user_test.go 的 newTestDB 实现。
func newTestDBForService(t *testing.T) *db.DB {
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
	return database
}

// createTestUserForService 创建测试用户，返回 user_id（service 包测试辅助）。
// 复用 UserDAO.Create 简化测试前置数据准备。
func createTestUserForService(t *testing.T, database *db.DB, username string) int64 {
	t.Helper()
	ud := dao.NewUserDAO(database)
	id, err := ud.Create(context.Background(), dao.CreateUserInput{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return id
}

// newTestAuditLogger 创建审计日志器（disabled 模式，no-op）。
func newTestAuditLogger(t *testing.T, database *db.DB) *audit.Logger {
	t.Helper()
	logger, err := audit.NewLogger(database, false, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}
	return logger
}

func TestAPIKeyService_Create(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	result, err := svc.Create(ctx, CreateAPIKeyInput{
		UserID: uid,
		Name:   "my-key",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if result.ID <= 0 {
		t.Error("ID 应 > 0")
	}
	if result.PlainText == "" {
		t.Error("PlainText 不应为空")
	}
	if result.KeyPrefix == "" {
		t.Error("KeyPrefix 不应为空")
	}
	if result.Name != "my-key" {
		t.Errorf("Name 期望 my-key，实际 %q", result.Name)
	}
	// 默认 scopes
	if len(result.Scopes) != 1 || result.Scopes[0] != "send" {
		t.Errorf("默认 Scopes 期望 [send]，实际 %v", result.Scopes)
	}
	// 默认 rate limit
	if result.RateLimit != 60 {
		t.Errorf("默认 RateLimit 期望 60，实际 %d", result.RateLimit)
	}
	if result.CreatedAt == "" {
		t.Error("CreatedAt 不应为空")
	}
}

func TestAPIKeyService_Create_WithScopesAndRateLimit(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	result, err := svc.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "full-key",
		Scopes:    []string{"send", "read"},
		RateLimit: 100,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if len(result.Scopes) != 2 || result.Scopes[0] != "send" || result.Scopes[1] != "read" {
		t.Errorf("Scopes 期望 [send read]，实际 %v", result.Scopes)
	}
	if result.RateLimit != 100 {
		t.Errorf("RateLimit 期望 100，实际 %d", result.RateLimit)
	}
}

func TestAPIKeyService_Create_MaxLimit(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 2)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	// 创建 2 个（达上限）
	for i := 0; i < 2; i++ {
		if _, err := svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "key"}); err != nil {
			t.Fatalf("第 %d 个创建失败: %v", i+1, err)
		}
	}
	// 第 3 个应报错
	_, err := svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "key3"})
	if err == nil {
		t.Fatal("达上限应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T: %v", err, err)
	}
	if ae.Status != 400 {
		t.Errorf("Status 期望 400，实际 %d", ae.Status)
	}
	if ae.Message != "API Key 数量已达上限" {
		t.Errorf("Message 期望 'API Key 数量已达上限'，实际 %q", ae.Message)
	}
}

func TestAPIKeyService_Create_EmptyName(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	_, err := svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: ""})
	if err == nil {
		t.Fatal("Name 为空应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T", err)
	}
	if ae.Status != 400 {
		t.Errorf("Status 期望 400，实际 %d", ae.Status)
	}
}

func TestAPIKeyService_ListByUser(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")
	other := createTestUserForService(t, database, "bob")

	svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "k1"})
	svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "k2"})
	svc.Create(ctx, CreateAPIKeyInput{UserID: other, Name: "other-key"})

	keys, err := svc.ListByUser(ctx, uid)
	if err != nil {
		t.Fatalf("ListByUser 失败: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("期望 2 个 key，实际 %d", len(keys))
	}

	// other 用户只有 1 个
	otherKeys, _ := svc.ListByUser(ctx, other)
	if len(otherKeys) != 1 {
		t.Errorf("other 期望 1 个 key，实际 %d", len(otherKeys))
	}
}

func TestAPIKeyService_Delete_Success(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	result, _ := svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "k1"})

	if err := svc.Delete(ctx, uid, result.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}

	// 验证已删除
	keys, _ := svc.ListByUser(ctx, uid)
	if len(keys) != 0 {
		t.Errorf("删除后应 0 个 key，实际 %d", len(keys))
	}
}

func TestAPIKeyService_Delete_OtherUserForbidden(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")
	other := createTestUserForService(t, database, "bob")

	result, _ := svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "k1"})

	err := svc.Delete(ctx, other, result.ID)
	if err == nil {
		t.Fatal("其他用户删除应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T", err)
	}
	if ae.Status != 403 {
		t.Errorf("Status 期望 403，实际 %d", ae.Status)
	}
	if ae.Message != "无权操作此 API Key" {
		t.Errorf("Message 期望 '无权操作此 API Key'，实际 %q", ae.Message)
	}

	// 验证未被删除
	keys, _ := svc.ListByUser(ctx, uid)
	if len(keys) != 1 {
		t.Errorf("归属校验失败后应保留 key，实际 %d 个", len(keys))
	}
}

func TestAPIKeyService_Delete_NotExist(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	err := svc.Delete(ctx, uid, 99999)
	if err == nil {
		t.Fatal("不存在应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T", err)
	}
	if ae.Status != 404 {
		t.Errorf("Status 期望 404，实际 %d", ae.Status)
	}
}

func TestAPIKeyService_Verify_Success(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")

	result, _ := svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "k1"})

	key, err := svc.Verify(ctx, result.PlainText)
	if err != nil {
		t.Fatalf("Verify 失败: %v", err)
	}
	if key.ID != result.ID {
		t.Errorf("ID 期望 %d，实际 %d", result.ID, key.ID)
	}
	if key.UserID != uid {
		t.Errorf("UserID 期望 %d，实际 %d", uid, key.UserID)
	}
	if key.Name != "k1" {
		t.Errorf("Name 期望 k1，实际 %q", key.Name)
	}
}

func TestAPIKeyService_Verify_WrongPlaintext(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()
	uid := createTestUserForService(t, database, "alice")
	svc.Create(ctx, CreateAPIKeyInput{UserID: uid, Name: "k1"})

	// 生成另一个合法格式但不在 DB 的 key
	other, err := crypto.GenerateAPIKey()
	if err != nil {
		t.Fatalf("生成 key 失败: %v", err)
	}

	_, err = svc.Verify(ctx, other.PlainText)
	if err == nil {
		t.Fatal("错误明文应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T: %v", err, err)
	}
	if ae.Status != 401 {
		t.Errorf("Status 期望 401，实际 %d", ae.Status)
	}
}

func TestAPIKeyService_Verify_BadFormat(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()

	_, err := svc.Verify(ctx, "not-a-valid-key")
	if err == nil {
		t.Fatal("格式错误应报错")
	}
	// 格式错误返回普通 error（非 AuthError）
	if _, ok := IsAuthError(err); ok {
		t.Errorf("格式错误不应是 AuthError")
	}
}

func TestAPIKeyService_Verify_NotExist(t *testing.T) {
	database := newTestDBForService(t)
	svc := NewAPIKeyService(dao.NewAPIKeyDAO(database), newTestAuditLogger(t, database), 10)
	ctx := context.Background()

	// 生成合法格式但不在 DB 的 key
	other, err := crypto.GenerateAPIKey()
	if err != nil {
		t.Fatalf("生成 key 失败: %v", err)
	}

	_, err = svc.Verify(ctx, other.PlainText)
	if err == nil {
		t.Fatal("不存在的 key 应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("应为 AuthError，实际 %T: %v", err, err)
	}
	if ae.Status != 401 {
		t.Errorf("Status 期望 401，实际 %d", ae.Status)
	}
}
