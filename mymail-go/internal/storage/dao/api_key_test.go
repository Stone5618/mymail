// api_key_test.go 测试 APIKeyDAO 的 CRUD 操作。
//
// 测试覆盖：
//   - Create / FindByID（字段验证）
//   - Create 默认值（Scopes=nil 默认 ["send"]、RateLimit=0 默认 60）
//   - FindByPrefix（命中与不命中）
//   - FindByUserID（多条返回）
//   - FindByPrefix_OnlyActive（停用后不返回）
//   - UpdateLastUsed
//   - Deactivate
//   - Delete（删除后 FindByID 返回 sql.ErrNoRows）
//   - CountByUserID
//   - FindByID_NotExist
//
// 使用真实 SQLite 临时数据库（复用 dao 包的 newTestDB / createTestUser）。
package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func TestAPIKeyDAO_Create(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := dao.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "test-key",
		KeyHash:   "$2a$12$somehash",
		KeyPrefix: "mk_a1b2c3d4e5",
		Scopes:    []string{"send"},
		RateLimit: 60,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if id <= 0 {
		t.Errorf("ID 应 > 0，实际 %d", id)
	}

	k, err := dao.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if k.Name != "test-key" {
		t.Errorf("Name 期望 test-key，实际 %q", k.Name)
	}
	if k.KeyPrefix != "mk_a1b2c3d4e5" {
		t.Errorf("KeyPrefix 期望 mk_a1b2c3d4e5，实际 %q", k.KeyPrefix)
	}
	if len(k.Scopes) != 1 || k.Scopes[0] != "send" {
		t.Errorf("Scopes 期望 [send]，实际 %v", k.Scopes)
	}
	if k.RateLimit != 60 {
		t.Errorf("RateLimit 期望 60，实际 %d", k.RateLimit)
	}
	if !k.IsActive {
		t.Error("IsActive 应为 true")
	}
	if k.LastUsedAt != nil {
		t.Errorf("LastUsedAt 应为 nil，实际 %v", k.LastUsedAt)
	}
}

func TestAPIKeyDAO_Create_DefaultScopes(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := dao.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "default-key",
		KeyHash:   "$2a$12$somehash",
		KeyPrefix: "mk_a1b2c3d4e5",
		Scopes:    nil,
		RateLimit: 0,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	k, err := dao.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if len(k.Scopes) != 1 || k.Scopes[0] != "send" {
		t.Errorf("Scopes 默认应为 [send]，实际 %v", k.Scopes)
	}
	if k.RateLimit != 60 {
		t.Errorf("RateLimit 默认应为 60，实际 %d", k.RateLimit)
	}
}

func TestAPIKeyDAO_FindByPrefix(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	prefix := "mk_a1b2c3d4e5"
	id, err := dao.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "key1",
		KeyHash:   "$2a$12$somehash",
		KeyPrefix: prefix,
		Scopes:    []string{"send"},
		RateLimit: 60,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	// 用 KeyPrefix 查找，返回非空列表且包含该 key
	keys, err := dao.FindByPrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("FindByPrefix 失败: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("FindByPrefix 应返回非空列表")
	}
	found := false
	for _, k := range keys {
		if k.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("FindByPrefix 返回列表应包含刚创建的 key")
	}

	// 查不存在的 prefix 返回空列表
	keys2, err := dao.FindByPrefix(ctx, "mk_noprefix99")
	if err != nil {
		t.Fatalf("FindByPrefix 不存在不应报错: %v", err)
	}
	if len(keys2) != 0 {
		t.Errorf("不存在的 prefix 应返回空列表，实际 %d 条", len(keys2))
	}
}

func TestAPIKeyDAO_FindByUserID(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	for i := 0; i < 2; i++ {
		_, err := dao.Create(ctx, CreateAPIKeyInput{
			UserID:    uid,
			Name:      fmt.Sprintf("key%d", i),
			KeyHash:   "$2a$12$somehash",
			KeyPrefix: fmt.Sprintf("mk_prefix%02d", i),
			Scopes:    []string{"send"},
			RateLimit: 60,
		})
		if err != nil {
			t.Fatalf("Create 第 %d 个失败: %v", i, err)
		}
	}

	keys, err := dao.FindByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByUserID 失败: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("应返回 2 条，实际 %d 条", len(keys))
	}
}

func TestAPIKeyDAO_FindByPrefix_OnlyActive(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	prefix := "mk_a1b2c3d4e5"
	id, err := dao.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "key1",
		KeyHash:   "$2a$12$somehash",
		KeyPrefix: prefix,
		Scopes:    []string{"send"},
		RateLimit: 60,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	// 创建后应能查到
	keys, _ := dao.FindByPrefix(ctx, prefix)
	if len(keys) != 1 {
		t.Fatalf("停用前 FindByPrefix 应返回 1 条，实际 %d 条", len(keys))
	}

	// 停用后 FindByPrefix 不返回
	if err := dao.Deactivate(ctx, id); err != nil {
		t.Fatalf("Deactivate 失败: %v", err)
	}
	keys, err = dao.FindByPrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("FindByPrefix 失败: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("停用后 FindByPrefix 应返回空列表，实际 %d 条", len(keys))
	}
}

func TestAPIKeyDAO_UpdateLastUsed(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := dao.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "key1",
		KeyHash:   "$2a$12$somehash",
		KeyPrefix: "mk_a1b2c3d4e5",
		Scopes:    []string{"send"},
		RateLimit: 60,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	// 初始 LastUsedAt 为 nil
	k, _ := dao.FindByID(ctx, id)
	if k.LastUsedAt != nil {
		t.Errorf("初始 LastUsedAt 应为 nil，实际 %v", k.LastUsedAt)
	}

	// UpdateLastUsed 后 LastUsedAt 非 nil
	if err := dao.UpdateLastUsed(ctx, id); err != nil {
		t.Fatalf("UpdateLastUsed 失败: %v", err)
	}
	k, _ = dao.FindByID(ctx, id)
	if k.LastUsedAt == nil {
		t.Error("UpdateLastUsed 后 LastUsedAt 应非 nil")
	}
}

func TestAPIKeyDAO_Deactivate(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := dao.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "key1",
		KeyHash:   "$2a$12$somehash",
		KeyPrefix: "mk_a1b2c3d4e5",
		Scopes:    []string{"send"},
		RateLimit: 60,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	if err := dao.Deactivate(ctx, id); err != nil {
		t.Fatalf("Deactivate 失败: %v", err)
	}
	k, err := dao.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if k.IsActive {
		t.Error("Deactivate 后 IsActive 应为 false")
	}
}

func TestAPIKeyDAO_Delete(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := dao.Create(ctx, CreateAPIKeyInput{
		UserID:    uid,
		Name:      "key1",
		KeyHash:   "$2a$12$somehash",
		KeyPrefix: "mk_a1b2c3d4e5",
		Scopes:    []string{"send"},
		RateLimit: 60,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	if err := dao.Delete(ctx, id); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	_, err = dao.FindByID(ctx, id)
	if err == nil {
		t.Fatal("Delete 后 FindByID 应返回 error")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("应返回 sql.ErrNoRows（或其包装），实际 %v", err)
	}
}

func TestAPIKeyDAO_CountByUserID(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	for i := 0; i < 3; i++ {
		_, err := dao.Create(ctx, CreateAPIKeyInput{
			UserID:    uid,
			Name:      fmt.Sprintf("key%d", i),
			KeyHash:   "$2a$12$somehash",
			KeyPrefix: fmt.Sprintf("mk_count%02d", i),
			Scopes:    []string{"send"},
			RateLimit: 60,
		})
		if err != nil {
			t.Fatalf("Create 第 %d 个失败: %v", i, err)
		}
	}

	count, err := dao.CountByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("CountByUserID 失败: %v", err)
	}
	if count != 3 {
		t.Errorf("CountByUserID 应为 3，实际 %d", count)
	}
}

func TestAPIKeyDAO_FindByID_NotExist(t *testing.T) {
	database := newTestDB(t)
	dao := NewAPIKeyDAO(database)
	ctx := context.Background()

	_, err := dao.FindByID(ctx, 99999)
	if err == nil {
		t.Fatal("查不存在的 ID 应返回 error")
	}
}
