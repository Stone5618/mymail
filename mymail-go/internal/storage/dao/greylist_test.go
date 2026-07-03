// greylist_test.go 测试 GreylistDAO 的 CRUD 操作。
// 使用真实 SQLite 临时数据库（复用 newTestDB），保证 SQL 与逻辑的正确性。
//
// 测试覆盖：
//   - Get（不存在/存在）
//   - Upsert（首次插入/更新 first_seen）
//   - SetAllowed（标记放行/不存在 key）
//   - Cleanup（已放行过期/未放行过期/未过期保留）
//   - Count
//   - DB 错误路径
package dao

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// newGreylistTestDB 创建带迁移的测试 DB（独立于 dao 包的 newTestDB，避免命名冲突）。
// 实际复用 newTestDB 即可，这里直接调用。
func newGreylistTestDB(t *testing.T) *db.DB {
	return newTestDB(t)
}

// insertGreylistDirect 直接 INSERT 一条灰名单记录（绕过 Upsert），用于测试指定 first_seen。
func insertGreylistDirect(t *testing.T, database *db.DB, key string, firstSeen int64, allowed bool) {
	t.Helper()
	allowedInt := 0
	if allowed {
		allowedInt = 1
	}
	_, err := database.ExecContext(context.Background(),
		`INSERT INTO greylist (key, first_seen, allowed) VALUES (?, ?, ?)`,
		key, firstSeen, allowedInt)
	if err != nil {
		t.Fatalf("直接插入灰名单记录失败: %v", err)
	}
}

func TestGreylistDAO_Get_NotExist(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()

	entry, err := d.Get(ctx, "1.2.3.4:sender@x.com:rcpt@y.com")
	if err != nil {
		t.Fatalf("Get 不存在记录应返回 nil,nil，实际 err=%v", err)
	}
	if entry != nil {
		t.Errorf("Get 不存在记录应返回 nil，实际 %+v", entry)
	}
}

func TestGreylistDAO_Upsert_Insert(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()
	key := "1.2.3.4:a@x.com:b@y.com"
	now := time.Now().UnixMilli()

	entry, err := d.Upsert(ctx, key, now, false)
	if err != nil {
		t.Fatalf("Upsert 插入失败: %v", err)
	}
	if entry == nil {
		t.Fatal("Upsert 返回 nil")
	}
	if entry.Key != key {
		t.Errorf("Key 不匹配：期望 %s，实际 %s", key, entry.Key)
	}
	if entry.FirstSeen != now {
		t.Errorf("FirstSeen 不匹配：期望 %d，实际 %d", now, entry.FirstSeen)
	}
	if entry.Allowed {
		t.Errorf("新插入记录 Allowed 应为 false")
	}

	// 再次 Get 验证落库
	got, err := d.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got == nil {
		t.Fatal("Get 应返回记录")
	}
	if got.FirstSeen != now {
		t.Errorf("Get FirstSeen 不匹配：期望 %d，实际 %d", now, got.FirstSeen)
	}
}

func TestGreylistDAO_Upsert_Update(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()
	key := "1.2.3.4:a@x.com:b@y.com"

	// 首次插入
	oldTime := time.Now().UnixMilli() - 10000
	if _, err := d.Upsert(ctx, key, oldTime, false); err != nil {
		t.Fatalf("首次 Upsert 失败: %v", err)
	}

	// 二次 Upsert 更新 first_seen
	newTime := time.Now().UnixMilli()
	entry, err := d.Upsert(ctx, key, newTime, false)
	if err != nil {
		t.Fatalf("二次 Upsert 失败: %v", err)
	}
	if entry.FirstSeen != newTime {
		t.Errorf("更新后 FirstSeen 应为 %d，实际 %d", newTime, entry.FirstSeen)
	}
}

func TestGreylistDAO_SetAllowed(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()
	key := "1.2.3.4:a@x.com:b@y.com"

	// 先插入未放行记录
	if _, err := d.Upsert(ctx, key, time.Now().UnixMilli(), false); err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}

	// 验证初始未放行
	entry, _ := d.Get(ctx, key)
	if entry.Allowed {
		t.Error("初始 Allowed 应为 false")
	}

	// 标记放行
	if err := d.SetAllowed(ctx, key); err != nil {
		t.Fatalf("SetAllowed 失败: %v", err)
	}

	// 验证已放行
	entry, _ = d.Get(ctx, key)
	if !entry.Allowed {
		t.Error("SetAllowed 后 Allowed 应为 true")
	}
}

func TestGreylistDAO_SetAllowed_NotExist(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()

	// 对不存在的 key 调用 SetAllowed 应 no-op 不报错
	if err := d.SetAllowed(ctx, "nonexistent:key"); err != nil {
		t.Errorf("SetAllowed 不存在 key 应 no-op，实际 err=%v", err)
	}
}

func TestGreylistDAO_Cleanup(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	ttlMs := int64(3600000) // 1 小时

	// 插入测试数据：
	// 1. 已放行 + 未过期 → 保留
	insertGreylistDirect(t, database, "keep:allowed:recent", now-1000, true)
	// 2. 已放行 + 已过期（first_seen < now - ttlMs）→ 清理
	insertGreylistDirect(t, database, "del:allowed:expired", now-ttlMs-1000, true)
	// 3. 未放行 + 未过期 → 保留
	insertGreylistDirect(t, database, "keep:unallowed:recent", now-1000, false)
	// 4. 未放行 + 已过期（first_seen < now - 2*ttlMs）→ 清理
	insertGreylistDirect(t, database, "del:unallowed:expired", now-2*ttlMs-1000, false)
	// 5. 未放行 + 刚好 1*ttlMs 前（未超过 2*ttlMs）→ 保留
	insertGreylistDirect(t, database, "keep:unallowed:boundary", now-ttlMs+1000, false)

	cleaned, err := d.Cleanup(ctx, ttlMs)
	if err != nil {
		t.Fatalf("Cleanup 失败: %v", err)
	}
	if cleaned != 2 {
		t.Errorf("清理数量应为 2，实际 %d", cleaned)
	}

	// 验证保留的记录
	count, _ := d.Count(ctx)
	if count != 3 {
		t.Errorf("清理后剩余记录应为 3，实际 %d", count)
	}

	// 验证具体保留的 key
	for _, key := range []string{"keep:allowed:recent", "keep:unallowed:recent", "keep:unallowed:boundary"} {
		entry, err := d.Get(ctx, key)
		if err != nil {
			t.Errorf("查询 %s 失败: %v", key, err)
		}
		if entry == nil {
			t.Errorf("%s 应被保留，但已被清理", key)
		}
	}

	// 验证清理的 key
	for _, key := range []string{"del:allowed:expired", "del:unallowed:expired"} {
		entry, _ := d.Get(ctx, key)
		if entry != nil {
			t.Errorf("%s 应被清理，但仍存在", key)
		}
	}
}

func TestGreylistDAO_Cleanup_InvalidTtl(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()

	_, err := d.Cleanup(ctx, 0)
	if err == nil {
		t.Error("ttlMs=0 应报错")
	}
	_, err = d.Cleanup(ctx, -1)
	if err == nil {
		t.Error("ttlMs<0 应报错")
	}
}

func TestGreylistDAO_Count(t *testing.T) {
	database := newGreylistTestDB(t)
	d := NewGreylistDAO(database)
	ctx := context.Background()

	count, err := d.Count(ctx)
	if err != nil {
		t.Fatalf("Count 失败: %v", err)
	}
	if count != 0 {
		t.Errorf("空表 Count 应为 0，实际 %d", count)
	}

	d.Upsert(ctx, "k1", time.Now().UnixMilli(), false)
	d.Upsert(ctx, "k2", time.Now().UnixMilli(), true)

	count, _ = d.Count(ctx)
	if count != 2 {
		t.Errorf("Count 应为 2，实际 %d", count)
	}
}

func TestGreylistDAO_DBError(t *testing.T) {
	// 使用独立 DB 以便关闭
	path := filepath.Join(t.TempDir(), "err.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	d := NewGreylistDAO(database)
	ctx := context.Background()

	// 先正常插入一条
	if _, err := d.Upsert(ctx, "k", time.Now().UnixMilli(), false); err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}

	// 关闭 DB
	database.Close()

	// 各方法应返回错误
	if _, err := d.Get(ctx, "k"); err == nil {
		t.Error("Get 关闭后应报错")
	}
	if _, err := d.Upsert(ctx, "k", time.Now().UnixMilli(), false); err == nil {
		t.Error("Upsert 关闭后应报错")
	}
	if err := d.SetAllowed(ctx, "k"); err == nil {
		t.Error("SetAllowed 关闭后应报错")
	}
	if _, err := d.Cleanup(ctx, 3600000); err == nil {
		t.Error("Cleanup 关闭后应报错")
	}
	if _, err := d.Count(ctx); err == nil {
		t.Error("Count 关闭后应报错")
	}
}
