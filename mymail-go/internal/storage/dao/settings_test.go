// settings_test.go 测试 SettingsDAO 的 CRUD 操作。
//
// 测试覆盖：
//   - Set / Get（写入后读取值正确）
//   - Get_NotExist（不存在的 key 返回 nil, nil）
//   - Set_Upsert（同一 key 写两次，Get 返回最新值）
//   - GetAll（多条返回，按 key 升序）
//   - Delete（删除后 Get 返回 nil）
//   - Delete_NotExist（删除不存在的 key 不报错）
//
// 使用真实 SQLite 临时数据库（复用 dao 包的 newTestDB）。
package dao

import (
	"context"
	"testing"
)

func TestSettingsDAO_SetAndGet(t *testing.T) {
	database := newTestDB(t)
	dao := NewSettingsDAO(database)
	ctx := context.Background()

	if err := dao.Set(ctx, "domain", "example.com"); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	s, err := dao.Get(ctx, "domain")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if s == nil {
		t.Fatal("Get 返回 nil")
	}
	if s.Key != "domain" {
		t.Errorf("Key 期望 domain，实际 %q", s.Key)
	}
	if s.Value != "example.com" {
		t.Errorf("Value 期望 example.com，实际 %q", s.Value)
	}
}

func TestSettingsDAO_Get_NotExist(t *testing.T) {
	database := newTestDB(t)
	dao := NewSettingsDAO(database)
	ctx := context.Background()

	s, err := dao.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 不存在不应报错: %v", err)
	}
	if s != nil {
		t.Errorf("Get 不存在应返回 nil，实际 %+v", s)
	}
}

func TestSettingsDAO_Set_Upsert(t *testing.T) {
	database := newTestDB(t)
	dao := NewSettingsDAO(database)
	ctx := context.Background()

	if err := dao.Set(ctx, "port", "25"); err != nil {
		t.Fatalf("第一次 Set 失败: %v", err)
	}
	if err := dao.Set(ctx, "port", "587"); err != nil {
		t.Fatalf("第二次 Set 失败: %v", err)
	}
	s, err := dao.Get(ctx, "port")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if s == nil {
		t.Fatal("Get 返回 nil")
	}
	if s.Value != "587" {
		t.Errorf("Value 期望 587（最新值），实际 %q", s.Value)
	}
}

func TestSettingsDAO_GetAll(t *testing.T) {
	database := newTestDB(t)
	dao := NewSettingsDAO(database)
	ctx := context.Background()

	if err := dao.Set(ctx, "domain", "example.com"); err != nil {
		t.Fatalf("Set domain 失败: %v", err)
	}
	if err := dao.Set(ctx, "port", "587"); err != nil {
		t.Fatalf("Set port 失败: %v", err)
	}
	if err := dao.Set(ctx, "name", "mymail"); err != nil {
		t.Fatalf("Set name 失败: %v", err)
	}

	settings, err := dao.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll 失败: %v", err)
	}
	if len(settings) != 3 {
		t.Fatalf("应返回 3 条，实际 %d 条", len(settings))
	}
	// 按 key 升序：domain < name < port
	expected := []string{"domain", "name", "port"}
	for i, want := range expected {
		if settings[i].Key != want {
			t.Errorf("第 %d 条 Key 期望 %q，实际 %q", i, want, settings[i].Key)
		}
	}
}

func TestSettingsDAO_Delete(t *testing.T) {
	database := newTestDB(t)
	dao := NewSettingsDAO(database)
	ctx := context.Background()

	if err := dao.Set(ctx, "domain", "example.com"); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	if err := dao.Delete(ctx, "domain"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	s, err := dao.Get(ctx, "domain")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if s != nil {
		t.Errorf("Delete 后 Get 应返回 nil，实际 %+v", s)
	}
}

func TestSettingsDAO_Delete_NotExist(t *testing.T) {
	database := newTestDB(t)
	dao := NewSettingsDAO(database)
	ctx := context.Background()

	if err := dao.Delete(ctx, "nonexistent"); err != nil {
		t.Errorf("Delete 不存在的 key 不应报错: %v", err)
	}
}
