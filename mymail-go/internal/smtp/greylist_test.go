// greylist_test.go 测试 GreylistChecker 的 5 状态判断与降级逻辑。
//
// 测试覆盖：
//   - Disabled（功能关闭）
//   - NilDAO（DAO 未初始化）
//   - FirstAttempt（首次见到拒绝）
//   - Cached（已放行缓存命中）
//   - Passed（通过延迟窗口放行）
//   - Wait（等待中拒绝）
//   - Expired（过期重置拒绝）
//   - DBError_Degrade（DB 错误降级放行）
package smtp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// newSMTPTestDB 创建带迁移的测试 DB（smtp 包共享）。
func newSMTPTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smtp_test.db")
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

// insertGreylistForTest 直接 INSERT 指定 first_seen 的灰名单记录（绕过 Upsert）。
func insertGreylistForTest(t *testing.T, database *db.DB, key string, firstSeen int64, allowed bool) {
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

func TestGreylistChecker_Disabled(t *testing.T) {
	database := newSMTPTestDB(t)
	d := dao.NewGreylistDAO(database)
	c := NewGreylistChecker(d, 300000, 3600000, false)

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	if !dec.Allow {
		t.Error("功能关闭时应放行")
	}
	if dec.Reason != GreylistReasonDisabled {
		t.Errorf("Reason 应为 disabled，实际 %s", dec.Reason)
	}
}

func TestGreylistChecker_NilDAO(t *testing.T) {
	c := NewGreylistChecker(nil, 300000, 3600000, true)

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	if !dec.Allow {
		t.Error("DAO=nil 时应放行")
	}
	if dec.Reason != GreylistReasonDisabled {
		t.Errorf("Reason 应为 disabled，实际 %s", dec.Reason)
	}
}

func TestGreylistChecker_FirstAttempt(t *testing.T) {
	database := newSMTPTestDB(t)
	d := dao.NewGreylistDAO(database)
	delayMs := int64(300000)
	c := NewGreylistChecker(d, delayMs, 3600000, true)

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	if dec.Allow {
		t.Error("首次见到应拒绝")
	}
	if dec.Reason != GreylistReasonFirstAttempt {
		t.Errorf("Reason 应为 first_attempt，实际 %s", dec.Reason)
	}
	expectedRetry := int(delayMs / 1000)
	if dec.RetryAfter != expectedRetry {
		t.Errorf("RetryAfter 应为 %d，实际 %d", expectedRetry, dec.RetryAfter)
	}

	// 验证已记录
	entry, _ := d.Get(context.Background(), "1.2.3.4:a@x.com:b@y.com")
	if entry == nil {
		t.Error("首次拒绝后应写入记录")
	}
	if entry.Allowed {
		t.Error("首次记录 Allowed 应为 false")
	}
}

func TestGreylistChecker_Cached(t *testing.T) {
	database := newSMTPTestDB(t)
	d := dao.NewGreylistDAO(database)
	c := NewGreylistChecker(d, 300000, 3600000, true)
	key := "1.2.3.4:a@x.com:b@y.com"

	// 预先插入已放行记录
	insertGreylistForTest(t, database, key, time.Now().UnixMilli(), true)

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	if !dec.Allow {
		t.Error("已放行缓存应放行")
	}
	if dec.Reason != GreylistReasonCached {
		t.Errorf("Reason 应为 cached，实际 %s", dec.Reason)
	}
}

func TestGreylistChecker_Passed(t *testing.T) {
	database := newSMTPTestDB(t)
	d := dao.NewGreylistDAO(database)
	delayMs := int64(300000)
	c := NewGreylistChecker(d, delayMs, 3600000, true)
	key := "1.2.3.4:a@x.com:b@y.com"

	// 预先插入未放行记录，first_seen 超过 delayMs（通过延迟窗口）
	insertGreylistForTest(t, database, key, time.Now().UnixMilli()-delayMs-1000, false)

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	if !dec.Allow {
		t.Error("通过延迟窗口应放行")
	}
	if dec.Reason != GreylistReasonPassed {
		t.Errorf("Reason 应为 passed，实际 %s", dec.Reason)
	}

	// 验证已标记放行
	entry, _ := d.Get(context.Background(), key)
	if entry == nil || !entry.Allowed {
		t.Error("Passed 后应标记 Allowed=true")
	}
}

func TestGreylistChecker_Wait(t *testing.T) {
	database := newSMTPTestDB(t)
	d := dao.NewGreylistDAO(database)
	delayMs := int64(300000)
	c := NewGreylistChecker(d, delayMs, 3600000, true)
	key := "1.2.3.4:a@x.com:b@y.com"

	// 预先插入未放行记录，first_seen 仅 1 秒前（未超过 delayMs）
	insertGreylistForTest(t, database, key, time.Now().UnixMilli()-1000, false)

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	if dec.Allow {
		t.Error("等待中应拒绝")
	}
	if dec.Reason != GreylistReasonWait {
		t.Errorf("Reason 应为 wait，实际 %s", dec.Reason)
	}
	if dec.RetryAfter < 1 {
		t.Errorf("Wait RetryAfter 应 >= 1，实际 %d", dec.RetryAfter)
	}
}

func TestGreylistChecker_Expired(t *testing.T) {
	database := newSMTPTestDB(t)
	d := dao.NewGreylistDAO(database)
	delayMs := int64(300000)
	ttlMs := int64(3600000)
	c := NewGreylistChecker(d, delayMs, ttlMs, true)
	key := "1.2.3.4:a@x.com:b@y.com"

	// 预先插入未放行记录，first_seen 超过 ttlMs（已过期）
	insertGreylistForTest(t, database, key, time.Now().UnixMilli()-ttlMs-1000, false)

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	if dec.Allow {
		t.Error("过期应拒绝")
	}
	if dec.Reason != GreylistReasonExpired {
		t.Errorf("Reason 应为 expired，实际 %s", dec.Reason)
	}
	expectedRetry := int(delayMs / 1000)
	if dec.RetryAfter != expectedRetry {
		t.Errorf("RetryAfter 应为 %d，实际 %d", expectedRetry, dec.RetryAfter)
	}

	// 验证 first_seen 已重置
	entry, _ := d.Get(context.Background(), key)
	if entry == nil {
		t.Error("过期重置后应仍存在记录")
	}
	if entry.Allowed {
		t.Error("过期重置后 Allowed 应为 false")
	}
	// first_seen 应接近当前时间（已重置）
	if time.Now().UnixMilli()-entry.FirstSeen > 5000 {
		t.Errorf("过期重置后 first_seen 应为当前时间，实际偏移 %d ms", time.Now().UnixMilli()-entry.FirstSeen)
	}
}

func TestGreylistChecker_DBError_Degrade(t *testing.T) {
	// 使用独立 DB 以便关闭
	path := filepath.Join(t.TempDir(), "greylist_err.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	d := dao.NewGreylistDAO(database)
	c := NewGreylistChecker(d, 300000, 3600000, true)

	// 关闭 DB 模拟查询失败
	database.Close()

	dec := c.Check(context.Background(), "1.2.3.4", "a@x.com", "b@y.com")
	// DB 错误应降级放行
	if !dec.Allow {
		t.Error("DB 错误应降级放行")
	}
	if dec.Reason != GreylistReasonError {
		t.Errorf("Reason 应为 error，实际 %s", dec.Reason)
	}
}

func TestGreylistChecker_Defaults(t *testing.T) {
	database := newSMTPTestDB(t)
	d := dao.NewGreylistDAO(database)
	// delayMs=0 和 ttlMs=0 应使用默认值
	c := NewGreylistChecker(d, 0, 0, true)
	if c.delayMs != 300000 {
		t.Errorf("默认 delayMs 应为 300000，实际 %d", c.delayMs)
	}
	if c.ttlMs != 3600000 {
		t.Errorf("默认 ttlMs 应为 3600000，实际 %d", c.ttlMs)
	}
}

func TestBuildGreylistKey(t *testing.T) {
	key := buildGreylistKey("1.2.3.4", "a@x.com", "b@y.com")
	expected := "1.2.3.4:a@x.com:b@y.com"
	if key != expected {
		t.Errorf("key 应为 %s，实际 %s", expected, key)
	}
}
