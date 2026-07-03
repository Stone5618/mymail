// spam_log_test.go 测试 SpamLogDAO 的 CRUD 操作。
// 使用真实 SQLite 临时数据库（复用 newTestDB），保证 SQL 与逻辑的正确性。
//
// 测试覆盖：
//   - Create（含空字段）
//   - FindByID（存在/不存在）
//   - FindRecent（分页 + 倒序）
//   - FindByIP
//   - StatsByAction（含 since 过滤）
//   - Count
//   - DB 错误路径
package dao

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mymail/mymail-go/internal/storage/db"
)

func TestSpamLogDAO_Create(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	id, err := d.Create(ctx, CreateSpamLogInput{
		Sender:    "spammer@evil.com",
		Recipient: "victim@example.com",
		IP:        "1.2.3.4",
		Score:     15,
		Reasons:   "SPF hard fail; IP blacklisted",
		Action:    "reject",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if id <= 0 {
		t.Errorf("ID 应 > 0，实际 %d", id)
	}

	// 验证查询
	sl, err := d.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if sl == nil {
		t.Fatal("FindByID 应返回记录")
	}
	if sl.Sender != "spammer@evil.com" {
		t.Errorf("Sender 不匹配：%s", sl.Sender)
	}
	if sl.Score != 15 {
		t.Errorf("Score 不匹配：%d", sl.Score)
	}
	if sl.Action != "reject" {
		t.Errorf("Action 不匹配：%s", sl.Action)
	}
	if sl.Reasons != "SPF hard fail; IP blacklisted" {
		t.Errorf("Reasons 不匹配：%s", sl.Reasons)
	}
}

func TestSpamLogDAO_Create_NullFields(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	// sender/recipient/ip 为空时应插入 NULL
	id, err := d.Create(ctx, CreateSpamLogInput{
		Sender:    "",
		Recipient: "",
		IP:        "",
		Score:     0,
		Reasons:   "",
		Action:    "allow",
	})
	if err != nil {
		t.Fatalf("Create 空字段失败: %v", err)
	}
	sl, _ := d.FindByID(ctx, id)
	if sl == nil {
		t.Fatal("FindByID 应返回记录")
	}
	if sl.Sender != "" {
		t.Errorf("空 Sender 应为空字符串，实际 %s", sl.Sender)
	}
	if sl.IP != "" {
		t.Errorf("空 IP 应为空字符串，实际 %s", sl.IP)
	}
}

func TestSpamLogDAO_FindByID_NotExist(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	sl, err := d.FindByID(ctx, 99999)
	if err != nil {
		t.Fatalf("FindByID 不存在应返回 nil,nil，实际 err=%v", err)
	}
	if sl != nil {
		t.Errorf("FindByID 不存在应返回 nil")
	}
}

func TestSpamLogDAO_FindRecent(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	// 插入 5 条记录
	actions := []string{"allow", "mark", "reject", "allow", "mark"}
	for i, a := range actions {
		_, err := d.Create(ctx, CreateSpamLogInput{
			Sender: "s@test.com",
			IP:     "1.2.3.4",
			Score:  i,
			Action: a,
		})
		if err != nil {
			t.Fatalf("Create %d 失败: %v", i, err)
		}
	}

	// 查询最近 3 条
	list, err := d.FindRecent(ctx, 3, 0)
	if err != nil {
		t.Fatalf("FindRecent 失败: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("应返回 3 条，实际 %d", len(list))
	}
	// 验证倒序（最后插入的在前）
	if list[0].Score != 4 {
		t.Errorf("倒序第一个 Score 应为 4，实际 %d", list[0].Score)
	}

	// 分页：offset=3 取剩余
	list2, _ := d.FindRecent(ctx, 10, 3)
	if len(list2) != 2 {
		t.Errorf("offset=3 后应返回 2 条，实际 %d", len(list2))
	}

	// limit 默认值
	list3, _ := d.FindRecent(ctx, 0, 0)
	if len(list3) != 5 {
		t.Errorf("limit=0 应默认返回全部 5 条，实际 %d", len(list3))
	}
}

func TestSpamLogDAO_FindByIP(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	// 插入不同 IP 的记录
	d.Create(ctx, CreateSpamLogInput{IP: "1.1.1.1", Score: 10, Action: "reject"})
	d.Create(ctx, CreateSpamLogInput{IP: "2.2.2.2", Score: 5, Action: "mark"})
	d.Create(ctx, CreateSpamLogInput{IP: "1.1.1.1", Score: 8, Action: "reject"})

	list, err := d.FindByIP(ctx, "1.1.1.1", 10)
	if err != nil {
		t.Fatalf("FindByIP 失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("IP 1.1.1.1 应返回 2 条，实际 %d", len(list))
	}
	// 验证都是该 IP
	for _, sl := range list {
		if sl.IP != "1.1.1.1" {
			t.Errorf("IP 不匹配：%s", sl.IP)
		}
	}

	// 不存在的 IP
	list2, _ := d.FindByIP(ctx, "9.9.9.9", 10)
	if len(list2) != 0 {
		t.Errorf("不存在的 IP 应返回 0 条")
	}
}

func TestSpamLogDAO_StatsByAction(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	// 插入不同 action 的记录
	d.Create(ctx, CreateSpamLogInput{IP: "1.1.1.1", Action: "allow"})
	d.Create(ctx, CreateSpamLogInput{IP: "1.1.1.1", Action: "allow"})
	d.Create(ctx, CreateSpamLogInput{IP: "1.1.1.1", Action: "mark"})
	d.Create(ctx, CreateSpamLogInput{IP: "1.1.1.1", Action: "reject"})

	stats, err := d.StatsByAction(ctx, "")
	if err != nil {
		t.Fatalf("StatsByAction 失败: %v", err)
	}
	// 应有 3 个 action（allow/mark/reject），allow=2
	m := make(map[string]int64)
	for _, s := range stats {
		m[s.Action] = s.Count
	}
	if m["allow"] != 2 {
		t.Errorf("allow 应为 2，实际 %d", m["allow"])
	}
	if m["mark"] != 1 {
		t.Errorf("mark 应为 1，实际 %d", m["mark"])
	}
	if m["reject"] != 1 {
		t.Errorf("reject 应为 1，实际 %d", m["reject"])
	}
}

func TestSpamLogDAO_StatsByAction_WithSince(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	// 插入记录
	d.Create(ctx, CreateSpamLogInput{IP: "1.1.1.1", Action: "allow"})

	// 用未来时间过滤（应返回空）
	future := "2999-01-01 00:00:00"
	stats, err := d.StatsByAction(ctx, future)
	if err != nil {
		t.Fatalf("StatsByAction 带 since 失败: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("未来时间过滤应返回 0 条，实际 %d", len(stats))
	}

	// 用过去时间过滤（应返回全部）
	past := "2000-01-01 00:00:00"
	stats2, _ := d.StatsByAction(ctx, past)
	if len(stats2) == 0 {
		t.Errorf("过去时间过滤应返回记录")
	}
}

func TestSpamLogDAO_Count(t *testing.T) {
	database := newTestDB(t)
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	count, _ := d.Count(ctx)
	if count != 0 {
		t.Errorf("空表 Count 应为 0，实际 %d", count)
	}

	d.Create(ctx, CreateSpamLogInput{Action: "allow"})
	d.Create(ctx, CreateSpamLogInput{Action: "reject"})

	count, _ = d.Count(ctx)
	if count != 2 {
		t.Errorf("Count 应为 2，实际 %d", count)
	}
}

func TestSpamLogDAO_DBError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "err.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	d := NewSpamLogDAO(database)
	ctx := context.Background()

	// 先正常插入一条
	if _, err := d.Create(ctx, CreateSpamLogInput{Action: "allow"}); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	// 关闭 DB
	database.Close()

	// 各方法应返回错误
	if _, err := d.Create(ctx, CreateSpamLogInput{Action: "allow"}); err == nil {
		t.Error("Create 关闭后应报错")
	}
	if _, err := d.FindByID(ctx, 1); err == nil {
		t.Error("FindByID 关闭后应报错")
	}
	if _, err := d.FindRecent(ctx, 10, 0); err == nil {
		t.Error("FindRecent 关闭后应报错")
	}
	if _, err := d.FindByIP(ctx, "1.1.1.1", 10); err == nil {
		t.Error("FindByIP 关闭后应报错")
	}
	if _, err := d.StatsByAction(ctx, ""); err == nil {
		t.Error("StatsByAction 关闭后应报错")
	}
	if _, err := d.Count(ctx); err == nil {
		t.Error("Count 关闭后应报错")
	}
}
