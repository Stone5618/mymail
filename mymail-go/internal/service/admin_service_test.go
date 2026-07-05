// Package service
// admin_service_test.go 测试 AdminService 完整业务逻辑。
// 使用真实 SQLite 临时数据库，覆盖所有成功与错误路径。
//
// 测试覆盖：
//   - Stats：用户统计
//   - ListUsers：列出所有用户
//   - GetUser：存在 / 不存在（404）
//   - CreateUser：P1-14 验证 StorageLimit 正确设置（200MB）
//   - UpdateUser：role / storage_limit / isActive / password 各字段
//   - DeleteUser：软删除（IsActive 变 false）
//   - GetSettings / UpdateSettings
package service

import (
	"context"
	"testing"

	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// newTestAdminService 创建测试用 AdminService，返回 (service, userDAO)。
func newTestAdminService(t *testing.T) (*AdminService, *dao.UserDAO) {
	t.Helper()
	database := newTestDBForService(t)
	userDAO := dao.NewUserDAO(database)
	settingsDAO := dao.NewSettingsDAO(database)
	msgDAO := dao.NewMessageDAO(database)
	svc := NewAdminService(userDAO, settingsDAO, msgDAO, newTestAuditLogger(t, database), "example.com")
	return svc, userDAO
}

func TestAdminService_Stats(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	// 初始应为 0
	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats 失败: %v", err)
	}
	if stats.TotalUsers != 0 {
		t.Errorf("初始 TotalUsers 期望 0，实际 %d", stats.TotalUsers)
	}

	// 创建 2 个用户
	userDAO.Create(ctx, dao.CreateUserInput{Username: "u1", Email: "u1@x.com", PasswordHash: "h"})
	userDAO.Create(ctx, dao.CreateUserInput{Username: "u2", Email: "u2@x.com", PasswordHash: "h"})

	stats, err = svc.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats 失败: %v", err)
	}
	if stats.TotalUsers != 2 {
		t.Errorf("TotalUsers 期望 2，实际 %d", stats.TotalUsers)
	}
	if stats.ActiveUsers != 2 {
		t.Errorf("ActiveUsers 期望 2，实际 %d", stats.ActiveUsers)
	}
}

func TestAdminService_ListUsers(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	userDAO.Create(ctx, dao.CreateUserInput{Username: "u1", Email: "u1@x.com", PasswordHash: "h"})
	userDAO.Create(ctx, dao.CreateUserInput{Username: "u2", Email: "u2@x.com", PasswordHash: "h"})

	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers 失败: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("期望 2 个用户，实际 %d", len(users))
	}
}

func TestAdminService_GetUser_Exist(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	id, _ := userDAO.Create(ctx, dao.CreateUserInput{Username: "alice", Email: "a@x.com", PasswordHash: "h"})

	u, err := svc.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("GetUser 失败: %v", err)
	}
	if u.ID != id {
		t.Errorf("ID 期望 %d，实际 %d", id, u.ID)
	}
	if u.Username != "alice" {
		t.Errorf("Username 期望 alice，实际 %q", u.Username)
	}
}

func TestAdminService_GetUser_NotExist(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	_, err := svc.GetUser(ctx, 99999)
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
	if ae.Message != "用户不存在" {
		t.Errorf("Message 期望 '用户不存在'，实际 %q", ae.Message)
	}
}

func TestAdminService_CreateUser_P1_14_StorageLimit(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	// P1-14：传入 200MB 验证 StorageLimit 正确设置
	const limit200MB int64 = 200 * 1024 * 1024 // 209715200
	u, err := svc.CreateUser(ctx, CreateUserAdminInput{
		Username:     "alice",
		Email:        "alice@example.com",
		Password:     "password123",
		DisplayName:  "Alice",
		Role:         "user",
		StorageLimit: limit200MB,
	})
	if err != nil {
		t.Fatalf("CreateUser 失败: %v", err)
	}
	if u.StorageLimit != limit200MB {
		t.Errorf("P1-14 失败：StorageLimit 期望 %d，实际 %d", limit200MB, u.StorageLimit)
	}
	if u.Username != "alice" {
		t.Errorf("Username 期望 alice，实际 %q", u.Username)
	}
	if u.Role != "user" {
		t.Errorf("Role 期望 user，实际 %q", u.Role)
	}

	// 验证密码已哈希（用 crypto.ComparePassword 校验）
	if err := crypto.ComparePassword(u.PasswordHash, "password123"); err != nil {
		t.Errorf("密码哈希校验失败: %v", err)
	}
}

func TestAdminService_CreateUser_DefaultStorageLimit(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	// StorageLimit=0 应默认 100MB
	u, err := svc.CreateUser(ctx, CreateUserAdminInput{
		Username: "bob",
		Email:    "bob@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateUser 失败: %v", err)
	}
	if u.StorageLimit != dao.DefaultStorageLimit {
		t.Errorf("默认 StorageLimit 期望 %d，实际 %d", dao.DefaultStorageLimit, u.StorageLimit)
	}
}

func TestAdminService_CreateUser_DefaultRole(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	// Role 为空应默认 "user"
	u, err := svc.CreateUser(ctx, CreateUserAdminInput{
		Username: "bob",
		Email:    "bob@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateUser 失败: %v", err)
	}
	if u.Role != "user" {
		t.Errorf("默认 Role 期望 user，实际 %q", u.Role)
	}
}

func TestAdminService_UpdateUser_Role(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	id, _ := userDAO.Create(ctx, dao.CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	newRole := "admin"
	if err := svc.UpdateUser(ctx, id, UpdateUserAdminInput{Role: &newRole}); err != nil {
		t.Fatalf("UpdateUser Role 失败: %v", err)
	}
	u, _ := userDAO.FindByID(ctx, id)
	if u.Role != "admin" {
		t.Errorf("Role 期望 admin，实际 %q", u.Role)
	}
}

func TestAdminService_UpdateUser_StorageLimit(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	id, _ := userDAO.Create(ctx, dao.CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	newLimit := int64(500 * 1024 * 1024)
	if err := svc.UpdateUser(ctx, id, UpdateUserAdminInput{StorageLimit: &newLimit}); err != nil {
		t.Fatalf("UpdateUser StorageLimit 失败: %v", err)
	}
	u, _ := userDAO.FindByID(ctx, id)
	if u.StorageLimit != newLimit {
		t.Errorf("StorageLimit 期望 %d，实际 %d", newLimit, u.StorageLimit)
	}
}

func TestAdminService_UpdateUser_IsActive(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	id, _ := userDAO.Create(ctx, dao.CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	active := false
	if err := svc.UpdateUser(ctx, id, UpdateUserAdminInput{IsActive: &active}); err != nil {
		t.Fatalf("UpdateUser IsActive 失败: %v", err)
	}
	u, _ := userDAO.FindByID(ctx, id)
	if u.IsActive {
		t.Error("IsActive 期望 false")
	}
}

func TestAdminService_UpdateUser_Password(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	id, _ := userDAO.Create(ctx, dao.CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "oldhash"})

	newPwd := "newpass123"
	if err := svc.UpdateUser(ctx, id, UpdateUserAdminInput{Password: &newPwd}); err != nil {
		t.Fatalf("UpdateUser Password 失败: %v", err)
	}
	u, _ := userDAO.FindByID(ctx, id)
	// 验证新密码可校验通过
	if err := crypto.ComparePassword(u.PasswordHash, "newpass123"); err != nil {
		t.Errorf("新密码校验失败: %v", err)
	}
	// 旧密码应失败
	if err := crypto.ComparePassword(u.PasswordHash, "oldpass"); err == nil {
		t.Error("旧密码不应校验通过")
	}
}

func TestAdminService_UpdateUser_NotExist(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	role := "admin"
	err := svc.UpdateUser(ctx, 99999, UpdateUserAdminInput{Role: &role})
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

func TestAdminService_UpdateUser_AllFields(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	id, _ := userDAO.Create(ctx, dao.CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	// 一次性更新所有字段
	role := "admin"
	limit := int64(300 * 1024 * 1024)
	active := true
	pwd := "allnewpass"
	if err := svc.UpdateUser(ctx, id, UpdateUserAdminInput{
		Role:         &role,
		StorageLimit: &limit,
		IsActive:     &active,
		Password:     &pwd,
	}); err != nil {
		t.Fatalf("UpdateUser 全字段失败: %v", err)
	}
	u, _ := userDAO.FindByID(ctx, id)
	if u.Role != "admin" {
		t.Errorf("Role 期望 admin，实际 %q", u.Role)
	}
	if u.StorageLimit != limit {
		t.Errorf("StorageLimit 期望 %d，实际 %d", limit, u.StorageLimit)
	}
	if !u.IsActive {
		t.Error("IsActive 期望 true")
	}
	if err := crypto.ComparePassword(u.PasswordHash, "allnewpass"); err != nil {
		t.Errorf("密码校验失败: %v", err)
	}
}

func TestAdminService_DeleteUser_SoftDelete(t *testing.T) {
	svc, userDAO := newTestAdminService(t)
	ctx := context.Background()

	id, _ := userDAO.Create(ctx, dao.CreateUserInput{Username: "u", Email: "u@x.com", PasswordHash: "h"})

	// 删除前应 active
	u, _ := userDAO.FindByID(ctx, id)
	if !u.IsActive {
		t.Fatal("删除前 IsActive 应为 true")
	}

	if err := svc.DeleteUser(ctx, id); err != nil {
		t.Fatalf("DeleteUser 失败: %v", err)
	}

	// 软删除：IsActive 应变 false，但用户记录仍存在
	u, _ = userDAO.FindByID(ctx, id)
	if u == nil {
		t.Fatal("软删除后用户记录应仍存在")
	}
	if u.IsActive {
		t.Error("软删除后 IsActive 应为 false")
	}
}

func TestAdminService_DeleteUser_NotExist(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	err := svc.DeleteUser(ctx, 99999)
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

func TestAdminService_GetSettings_Empty(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	settings, err := svc.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings 失败: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("初始应 0 个设置，实际 %d", len(settings))
	}
}

func TestAdminService_UpdateSettings(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	settings := map[string]string{
		"domain":      "example.com",
		"smtp_port":   "25",
		"feature_flag": "on",
	}
	if err := svc.UpdateSettings(ctx, settings); err != nil {
		t.Fatalf("UpdateSettings 失败: %v", err)
	}

	// 验证已写入
	result, _ := svc.GetSettings(ctx)
	if len(result) != 3 {
		t.Errorf("期望 3 个设置，实际 %d", len(result))
	}

	// 用 SettingsDAO.Get 验证单个值
	got := make(map[string]string)
	for _, s := range result {
		got[s.Key] = s.Value
	}
	if got["domain"] != "example.com" {
		t.Errorf("domain 期望 example.com，实际 %q", got["domain"])
	}
	if got["smtp_port"] != "25" {
		t.Errorf("smtp_port 期望 25，实际 %q", got["smtp_port"])
	}
	if got["feature_flag"] != "on" {
		t.Errorf("feature_flag 期望 on，实际 %q", got["feature_flag"])
	}
}

func TestAdminService_UpdateSettings_Upsert(t *testing.T) {
	svc, _ := newTestAdminService(t)
	ctx := context.Background()

	// 先写
	svc.UpdateSettings(ctx, map[string]string{"domain": "old.com"})

	// 再覆盖（UPSERT）
	svc.UpdateSettings(ctx, map[string]string{"domain": "new.com"})

	result, _ := svc.GetSettings(ctx)
	if len(result) != 1 {
		t.Errorf("UPSERT 后应仍 1 个设置，实际 %d", len(result))
	}
	if result[0].Value != "new.com" {
		t.Errorf("UPSERT 后 domain 期望 new.com，实际 %q", result[0].Value)
	}
}
