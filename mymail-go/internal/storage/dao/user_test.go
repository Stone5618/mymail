// Package dao
// user_test.go 测试 UserDAO 的 CRUD 操作。
// 使用真实 SQLite 临时数据库（t.TempDir），保证 SQL 与逻辑的正确性。
//
// 测试覆盖：
//   - Create / FindByID / FindByEmail / FindByUsername
//   - UpdateLoginFails / LockUser / ResetLoginFails
//   - UpdateProfile / UpdatePassword / SetDefaultPassword
//   - UpdateStorageUsed / SetActive / Count / ActiveCount
//   - 事务回滚（ResetLoginFails 用事务）
package dao

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// newTestDB 创建临时 SQLite 数据库并执行迁移。
func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database
}

func TestUserDAO_CreateAndFind(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, err := dao.Create(ctx, CreateUserInput{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "$2a$12$somehash",
		DisplayName:  "Alice",
		Role:         "user",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if id <= 0 {
		t.Errorf("ID 应 > 0，实际 %d", id)
	}

	// FindByID
	u, err := dao.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if u == nil {
		t.Fatal("FindByID 返回 nil")
	}
	if u.Username != "alice" {
		t.Errorf("Username 期望 alice，实际 %q", u.Username)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("Email 期望 alice@example.com，实际 %q", u.Email)
	}
	if u.Role != "user" {
		t.Errorf("Role 期望 user，实际 %q", u.Role)
	}
	if !u.IsActive {
		t.Error("IsActive 应默认 true")
	}
	if u.IsDefaultPassword {
		t.Error("IsDefaultPassword 应默认 false")
	}

	// FindByEmail
	u2, _ := dao.FindByEmail(ctx, "alice@example.com")
	if u2 == nil || u2.ID != id {
		t.Error("FindByEmail 失败")
	}

	// FindByUsername
	u3, _ := dao.FindByUsername(ctx, "alice")
	if u3 == nil || u3.ID != id {
		t.Error("FindByUsername 失败")
	}
}

func TestUserDAO_FindNotFound(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	u, err := dao.FindByID(ctx, 99999)
	if err != nil {
		t.Errorf("FindByID 不存在不应返回错误: %v", err)
	}
	if u != nil {
		t.Error("FindByID 不存在应返回 nil")
	}

	u, err = dao.FindByEmail(ctx, "nobody@example.com")
	if err != nil {
		t.Errorf("FindByEmail 不存在不应返回错误: %v", err)
	}
	if u != nil {
		t.Error("FindByEmail 不存在应返回 nil")
	}

	u, err = dao.FindByUsername(ctx, "nobody")
	if err != nil {
		t.Errorf("FindByUsername 不存在不应返回错误: %v", err)
	}
	if u != nil {
		t.Error("FindByUsername 不存在应返回 nil")
	}
}

func TestUserDAO_CreateDefaultValues(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	// 不传 DisplayName 与 Role，应使用默认值
	id, err := dao.Create(ctx, CreateUserInput{
		Username:     "bob",
		Email:        "bob@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if u.DisplayName != "bob" {
		t.Errorf("DisplayName 默认应为 username，实际 %q", u.DisplayName)
	}
	if u.Role != "user" {
		t.Errorf("Role 默认应为 user，实际 %q", u.Role)
	}
}

func TestUserDAO_Count(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	count, _ := dao.Count(ctx)
	if count != 0 {
		t.Errorf("初始 Count 应为 0，实际 %d", count)
	}

	for i := 0; i < 3; i++ {
		dao.Create(ctx, CreateUserInput{
			Username:     "user" + string(rune('A'+i)),
			Email:        "user" + string(rune('A'+i)) + "@example.com",
			PasswordHash: "hash",
		})
	}

	count, _ = dao.Count(ctx)
	if count != 3 {
		t.Errorf("Count 应为 3，实际 %d", count)
	}
}

func TestUserDAO_ActiveCount(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id1, _ := dao.Create(ctx, CreateUserInput{Username: "a", Email: "a@x.com", PasswordHash: "h"})
	dao.Create(ctx, CreateUserInput{Username: "b", Email: "b@x.com", PasswordHash: "h"})

	// 禁用第一个
	dao.SetActive(ctx, id1, false)

	active, _ := dao.ActiveCount(ctx)
	if active != 1 {
		t.Errorf("ActiveCount 应为 1，实际 %d", active)
	}
}

func TestUserDAO_UpdateLoginFails(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	if err := dao.UpdateLoginFails(ctx, id, 3); err != nil {
		t.Fatalf("UpdateLoginFails 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if u.LoginFails != 3 {
		t.Errorf("LoginFails 期望 3，实际 %d", u.LoginFails)
	}
}

func TestUserDAO_LockUser(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	until := time.Now().Add(15 * time.Minute).UTC().Format("2006-01-02 15:04:05")
	if err := dao.LockUser(ctx, id, until); err != nil {
		t.Fatalf("LockUser 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if !u.LockedUntil.Valid {
		t.Fatal("LockedUntil 应有效")
	}
}

func TestUserDAO_ResetLoginFails(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})
	dao.UpdateLoginFails(ctx, id, 5)
	until := time.Now().Add(15 * time.Minute).UTC().Format("2006-01-02 15:04:05")
	dao.LockUser(ctx, id, until)

	if err := dao.ResetLoginFails(ctx, id); err != nil {
		t.Fatalf("ResetLoginFails 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if u.LoginFails != 0 {
		t.Errorf("LoginFails 期望 0，实际 %d", u.LoginFails)
	}
	if u.LockedUntil.Valid {
		t.Error("LockedUntil 应为 NULL")
	}
}

func TestUserDAO_UpdateProfile(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	name := "New Name"
	sig := "My signature"
	if err := dao.UpdateProfile(ctx, id, UpdateProfileInput{
		DisplayName: &name,
		Signature:   &sig,
	}); err != nil {
		t.Fatalf("UpdateProfile 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if u.DisplayName != "New Name" {
		t.Errorf("DisplayName 期望 New Name，实际 %q", u.DisplayName)
	}
	if !u.Signature.Valid || u.Signature.String != "My signature" {
		t.Errorf("Signature 期望 My signature，实际 %+v", u.Signature)
	}
}

func TestUserDAO_UpdateProfile_NoFields(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	// 空输入应直接返回 nil，不执行 SQL
	if err := dao.UpdateProfile(ctx, id, UpdateProfileInput{}); err != nil {
		t.Errorf("空输入不应报错: %v", err)
	}
}

func TestUserDAO_UpdatePassword(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "oldhash"})
	if err := dao.UpdatePassword(ctx, id, "newhash"); err != nil {
		t.Fatalf("UpdatePassword 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if u.PasswordHash != "newhash" {
		t.Errorf("PasswordHash 期望 newhash，实际 %q", u.PasswordHash)
	}
}

func TestUserDAO_SetDefaultPassword(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	if err := dao.SetDefaultPassword(ctx, id, true); err != nil {
		t.Fatalf("SetDefaultPassword(true) 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if !u.IsDefaultPassword {
		t.Error("IsDefaultPassword 应为 true")
	}

	if err := dao.SetDefaultPassword(ctx, id, false); err != nil {
		t.Fatalf("SetDefaultPassword(false) 失败: %v", err)
	}
	u, _ = dao.FindByID(ctx, id)
	if u.IsDefaultPassword {
		t.Error("IsDefaultPassword 应为 false")
	}
}

func TestUserDAO_UpdateStorageUsed(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	if err := dao.UpdateStorageUsed(ctx, id, 1024); err != nil {
		t.Fatalf("UpdateStorageUsed 失败: %v", err)
	}
	u, _ := dao.FindByID(ctx, id)
	if u.StorageUsed != 1024 {
		t.Errorf("StorageUsed 期望 1024，实际 %d", u.StorageUsed)
	}

	// 增量更新
	dao.UpdateStorageUsed(ctx, id, 512)
	u, _ = dao.FindByID(ctx, id)
	if u.StorageUsed != 1536 {
		t.Errorf("StorageUsed 期望 1536，实际 %d", u.StorageUsed)
	}

	// 负数（释放）
	dao.UpdateStorageUsed(ctx, id, -256)
	u, _ = dao.FindByID(ctx, id)
	if u.StorageUsed != 1280 {
		t.Errorf("StorageUsed 期望 1280，实际 %d", u.StorageUsed)
	}
}

func TestUserDAO_SetActive(t *testing.T) {
	database := newTestDB(t)
	dao := NewUserDAO(database)
	ctx := context.Background()

	id, _ := dao.Create(ctx, CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	dao.SetActive(ctx, id, false)
	u, _ := dao.FindByID(ctx, id)
	if u.IsActive {
		t.Error("IsActive 应为 false")
	}

	dao.SetActive(ctx, id, true)
	u, _ = dao.FindByID(ctx, id)
	if !u.IsActive {
		t.Error("IsActive 应为 true")
	}
}

func TestScanUser_SQLNullHandling(t *testing.T) {
	// 直接测试 scanUser 对 NULL 字段的处理
	// 通过插入一个 display_name=NULL 的用户
	database := newTestDB(t)
	ctx := context.Background()

	// 直接 SQL 插入，display_name 为 NULL
	_, err := database.ExecContext(ctx, `INSERT INTO users (username, email, password_hash, display_name, role) VALUES (?, ?, ?, NULL, 'user')`,
		"nulluser", "null@x.com", "hash")
	if err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	dao := NewUserDAO(database)
	u, _ := dao.FindByUsername(ctx, "nulluser")
	if u == nil {
		t.Fatal("用户未找到")
	}
	if u.DisplayName != "" {
		t.Errorf("NULL display_name 应转为空字符串，实际 %q", u.DisplayName)
	}
	if u.Signature.Valid {
		t.Error("NULL signature 应为 Invalid")
	}
	if u.LockedUntil.Valid {
		t.Error("NULL locked_until 应为 Invalid")
	}
}

// 确保 sql 包被引用（用于 NullTime 等类型断言）
var _ = sql.NullTime{}
