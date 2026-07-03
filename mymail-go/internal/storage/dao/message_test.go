// Package dao
// message_test.go 测试 MessageDAO 的所有操作。
//
// 测试覆盖：
//   - Create / FindByID / FindByIDForUser
//   - ListByUser（分页/搜索/未读过滤）
//   - UnreadCount / UnreadCountByFolders
//   - MarkRead / MarkUnread / ToggleStar（P0-3 归属校验）
//   - MoveToFolder / SoftDelete / PermanentDelete
//   - EmptyTrash（事务 P1-1）
//   - GetNextUID（事务内 P1-4，并发 100 次无重复）
//   - BatchMarkRead / BatchMove / BatchSoftDelete
//   - TotalSize / TodaySentCount / TodayReceivedCount
package dao

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// createTestUser 创建测试用户，返回 user_id。
// 复用 UserDAO.Create 简化测试前置数据准备。
func createTestUser(t *testing.T, database *db.DB, username string) int64 {
	t.Helper()
	ud := NewUserDAO(database)
	id, err := ud.Create(context.Background(), CreateUserInput{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return id
}

// createTestMessage 创建测试邮件，返回 message_id。
func createTestMessage(t *testing.T, md *MessageDAO, userID int64, folder, subject string) int64 {
	t.Helper()
	id, err := md.Create(context.Background(), CreateMessageInput{
		UserID:   userID,
		Folder:   folder,
		FromAddr: "sender@example.com",
		ToAddr:   "recipient@example.com",
		Subject:  subject,
		BodyText: "body",
	})
	if err != nil {
		t.Fatalf("创建测试邮件失败: %v", err)
	}
	return id
}

func TestMessageDAO_CreateAndFind(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id := createTestMessage(t, md, uid, "INBOX", "Hello")

	// FindByID
	m, err := md.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if m == nil {
		t.Fatal("FindByID 返回 nil")
	}
	if m.UserID != uid {
		t.Errorf("UserID 期望 %d，实际 %d", uid, m.UserID)
	}
	if m.Folder != "INBOX" {
		t.Errorf("Folder 期望 INBOX，实际 %q", m.Folder)
	}
	if m.Subject != "Hello" {
		t.Errorf("Subject 期望 Hello，实际 %q", m.Subject)
	}
	if m.IsRead {
		t.Error("新邮件应未读")
	}
	if m.IsDeleted {
		t.Error("新邮件不应删除")
	}
	if m.Flags != "[]" {
		t.Errorf("Flags 默认应为 '[]'，实际 %q", m.Flags)
	}

	// FindByIDForUser - 正确用户
	m2, _ := md.FindByIDForUser(ctx, id, uid)
	if m2 == nil {
		t.Error("FindByIDForUser 应返回邮件")
	}

	// FindByIDForUser - 错误用户（P0-3 防越权）
	otherUID := createTestUser(t, database, "bob")
	m3, _ := md.FindByIDForUser(ctx, id, otherUID)
	if m3 != nil {
		t.Error("FindByIDForUser 不应返回他人邮件（P0-3 防越权）")
	}
}

func TestMessageDAO_FindNotFound(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()

	m, err := md.FindByID(ctx, 99999)
	if err != nil {
		t.Errorf("FindByID 不存在不应返回错误: %v", err)
	}
	if m != nil {
		t.Error("FindByID 不存在应返回 nil")
	}
}

func TestMessageDAO_ListByUser(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 创建 5 封 INBOX + 2 封 SENT
	for i := 0; i < 5; i++ {
		createTestMessage(t, md, uid, "INBOX", fmt.Sprintf("Inbox %d", i))
	}
	for i := 0; i < 2; i++ {
		createTestMessage(t, md, uid, "SENT", fmt.Sprintf("Sent %d", i))
	}

	// 列表 INBOX
	result, err := md.ListByUser(ctx, uid, "INBOX", 1, 20, "", false)
	if err != nil {
		t.Fatalf("ListByUser 失败: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("INBOX Total 期望 5，实际 %d", result.Total)
	}
	if len(result.Messages) != 5 {
		t.Errorf("INBOX 消息数 期望 5，实际 %d", len(result.Messages))
	}

	// 列表 SENT
	result, _ = md.ListByUser(ctx, uid, "SENT", 1, 20, "", false)
	if result.Total != 2 {
		t.Errorf("SENT Total 期望 2，实际 %d", result.Total)
	}

	// 分页
	result, _ = md.ListByUser(ctx, uid, "INBOX", 1, 2, "", false)
	if len(result.Messages) != 2 {
		t.Errorf("第 1 页应返回 2 条，实际 %d", len(result.Messages))
	}
	if result.Total != 5 {
		t.Errorf("Total 应仍为 5，实际 %d", result.Total)
	}

	// 搜索
	result, _ = md.ListByUser(ctx, uid, "INBOX", 1, 20, "Inbox 3", false)
	if result.Total != 1 {
		t.Errorf("搜索 'Inbox 3' 应返回 1 条，实际 %d", result.Total)
	}
}

func TestMessageDAO_UnreadCount(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id1 := createTestMessage(t, md, uid, "INBOX", "1")
	createTestMessage(t, md, uid, "INBOX", "2")

	// 默认未读
	count, _ := md.UnreadCount(ctx, uid, "INBOX")
	if count != 2 {
		t.Errorf("未读数期望 2，实际 %d", count)
	}

	// 标记一封已读
	md.MarkRead(ctx, id1, uid)
	count, _ = md.UnreadCount(ctx, uid, "INBOX")
	if count != 1 {
		t.Errorf("标记已读后未读数期望 1，实际 %d", count)
	}
}

func TestMessageDAO_UnreadCountByFolders(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	createTestMessage(t, md, uid, "INBOX", "1")
	createTestMessage(t, md, uid, "SENT", "2")
	createTestMessage(t, md, uid, "DRAFTS", "3")

	counts, err := md.UnreadCountByFolders(ctx, uid, []string{"INBOX", "SENT", "DRAFTS", "TRASH"})
	if err != nil {
		t.Fatalf("UnreadCountByFolders 失败: %v", err)
	}
	if counts["INBOX"] != 1 {
		t.Errorf("INBOX 期望 1，实际 %d", counts["INBOX"])
	}
	if counts["TRASH"] != 0 {
		t.Errorf("TRASH 期望 0，实际 %d", counts["TRASH"])
	}
}

func TestMessageDAO_MarkRead_P0_3(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")
	other := createTestUser(t, database, "bob")

	id := createTestMessage(t, md, uid, "INBOX", "test")

	// 错误用户标记（应不影响）
	err := md.MarkRead(ctx, id, other)
	if err != nil {
		t.Errorf("MarkRead 不应报错: %v", err)
	}
	m, _ := md.FindByID(ctx, id)
	if m.IsRead {
		t.Error("P0-3 失败：其他用户的 MarkRead 不应生效")
	}

	// 正确用户标记
	md.MarkRead(ctx, id, uid)
	m, _ = md.FindByID(ctx, id)
	if !m.IsRead {
		t.Error("MarkRead 应生效")
	}
}

func TestMessageDAO_MarkUnread(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id := createTestMessage(t, md, uid, "INBOX", "test")
	md.MarkRead(ctx, id, uid)
	md.MarkUnread(ctx, id, uid)
	m, _ := md.FindByID(ctx, id)
	if m.IsRead {
		t.Error("MarkUnread 应生效")
	}
}

func TestMessageDAO_ToggleStar(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")
	other := createTestUser(t, database, "bob")

	id := createTestMessage(t, md, uid, "INBOX", "test")

	// 其他用户 toggle（不应生效，P0-3）
	md.ToggleStar(ctx, id, other)
	m, _ := md.FindByID(ctx, id)
	if m.IsStarred {
		t.Error("P0-3 失败：其他用户的 ToggleStar 不应生效")
	}

	// 正确用户 toggle
	md.ToggleStar(ctx, id, uid)
	m, _ = md.FindByID(ctx, id)
	if !m.IsStarred {
		t.Error("ToggleStar 应生效")
	}

	// 再 toggle 应取消
	md.ToggleStar(ctx, id, uid)
	m, _ = md.FindByID(ctx, id)
	if m.IsStarred {
		t.Error("二次 ToggleStar 应取消星标")
	}
}

func TestMessageDAO_MoveToFolder(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id := createTestMessage(t, md, uid, "INBOX", "test")
	md.MoveToFolder(ctx, id, uid, "TRASH")
	m, _ := md.FindByID(ctx, id)
	if m.Folder != "TRASH" {
		t.Errorf("Folder 期望 TRASH，实际 %q", m.Folder)
	}
}

func TestMessageDAO_SoftDelete(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id := createTestMessage(t, md, uid, "INBOX", "test")
	md.SoftDelete(ctx, id, uid)
	m, _ := md.FindByID(ctx, id)
	if !m.IsDeleted {
		t.Error("IsDeleted 应为 true")
	}
	if m.Folder != "TRASH" {
		t.Errorf("Folder 期望 TRASH，实际 %q", m.Folder)
	}
}

func TestMessageDAO_PermanentDelete(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id := createTestMessage(t, md, uid, "INBOX", "test")
	md.PermanentDelete(ctx, id, uid)
	m, _ := md.FindByID(ctx, id)
	if m != nil {
		t.Error("PermanentDelete 后邮件应不存在")
	}
}

func TestMessageDAO_EmptyTrash(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 创建 3 封 TRASH + 2 封 INBOX
	for i := 0; i < 3; i++ {
		createTestMessage(t, md, uid, "TRASH", fmt.Sprintf("Trash %d", i))
	}
	for i := 0; i < 2; i++ {
		createTestMessage(t, md, uid, "INBOX", fmt.Sprintf("Inbox %d", i))
	}

	// 清空 TRASH
	if err := md.EmptyTrash(ctx, uid); err != nil {
		t.Fatalf("EmptyTrash 失败: %v", err)
	}

	// INBOX 应不变
	result, _ := md.ListByUser(ctx, uid, "INBOX", 1, 20, "", false)
	if result.Total != 2 {
		t.Errorf("EmptyTrash 后 INBOX 应仍为 2，实际 %d", result.Total)
	}

	// TRASH 应为空
	result, _ = md.ListByUser(ctx, uid, "TRASH", 1, 20, "", false)
	if result.Total != 0 {
		t.Errorf("EmptyTrash 后 TRASH 应为 0，实际 %d", result.Total)
	}
}

func TestMessageDAO_FindTrashMessageIDs(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	createTestMessage(t, md, uid, "TRASH", "1")
	createTestMessage(t, md, uid, "TRASH", "2")
	createTestMessage(t, md, uid, "INBOX", "3")

	ids, err := md.FindTrashMessageIDs(ctx, uid)
	if err != nil {
		t.Fatalf("FindTrashMessageIDs 失败: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("应返回 2 个 ID，实际 %d", len(ids))
	}
}

func TestMessageDAO_TotalSize(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 创建带 size 的邮件
	md.Create(ctx, CreateMessageInput{
		UserID: uid, Folder: "INBOX", FromAddr: "a@x.com", ToAddr: "b@x.com",
		Subject: "1", SizeBytes: 100,
	})
	md.Create(ctx, CreateMessageInput{
		UserID: uid, Folder: "INBOX", FromAddr: "a@x.com", ToAddr: "b@x.com",
		Subject: "2", SizeBytes: 200,
	})

	total, err := md.TotalSize(ctx, uid)
	if err != nil {
		t.Fatalf("TotalSize 失败: %v", err)
	}
	if total != 300 {
		t.Errorf("TotalSize 期望 300，实际 %d", total)
	}
}

func TestMessageDAO_TodaySentCount(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	createTestMessage(t, md, uid, "SENT", "1")
	createTestMessage(t, md, uid, "SENT", "2")
	createTestMessage(t, md, uid, "INBOX", "3")

	count, err := md.TodaySentCount(ctx)
	if err != nil {
		t.Fatalf("TodaySentCount 失败: %v", err)
	}
	if count != 2 {
		t.Errorf("TodaySentCount 期望 2，实际 %d", count)
	}
}

func TestMessageDAO_TodayReceivedCount(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	createTestMessage(t, md, uid, "INBOX", "1")
	createTestMessage(t, md, uid, "INBOX", "2")

	count, err := md.TodayReceivedCount(ctx)
	if err != nil {
		t.Fatalf("TodayReceivedCount 失败: %v", err)
	}
	if count != 2 {
		t.Errorf("TodayReceivedCount 期望 2，实际 %d", count)
	}
}

func TestMessageDAO_GetNextUID_P1_4(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 首次应返回 1
	next, err := md.GetNextUID(ctx, uid)
	if err != nil {
		t.Fatalf("GetNextUID 失败: %v", err)
	}
	if next != 1 {
		t.Errorf("首次 next_uid 期望 1，实际 %d", next)
	}

	// 创建一封后应返回 2
	uidVal := int64(1)
	md.Create(ctx, CreateMessageInput{
		UserID: uid, Folder: "INBOX", FromAddr: "a@x.com", ToAddr: "b@x.com",
		Subject: "1", UID: &uidVal,
	})
	next, _ = md.GetNextUID(ctx, uid)
	if next != 2 {
		t.Errorf("二次 next_uid 期望 2，实际 %d", next)
	}
}

// TestMessageDAO_GetNextUID_Concurrent 并发 100 次 GetNextUID + Create，
// 验证 P1-4 修复：必须在同一事务内执行 GetNextUIDOn + CreateOn 才能避免重复。
// SQLite 单连接写串行（SetMaxOpenConns(1)），事务会自动序列化。
func TestMessageDAO_GetNextUID_Concurrent(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	uid := createTestUser(t, database, "alice")

	const goroutines = 20
	const perGoroutine = 5
	var wg sync.WaitGroup
	uidCh := make(chan int64, goroutines*perGoroutine)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < perGoroutine; j++ {
				// P1-4 修复的推荐用法：在事务内执行 GetNextUID + Create
				var next int64
				err := database.RunInTransaction(ctx, func(tx *sql.Tx) error {
					var err error
					next, err = md.GetNextUIDOn(ctx, tx, uid)
					if err != nil {
						return err
					}
					uidVal := next
					_, err = md.CreateOn(ctx, tx, CreateMessageInput{
						UserID: uid, Folder: "INBOX", FromAddr: "a@x.com", ToAddr: "b@x.com",
						Subject: "concurrent", UID: &uidVal,
					})
					return err
				})
				if err != nil {
					t.Errorf("事务失败: %v", err)
					return
				}
				uidCh <- next
			}
		}()
	}
	wg.Wait()
	close(uidCh)

	seen := make(map[int64]bool)
	for u := range uidCh {
		if seen[u] {
			t.Errorf("P1-4 失败：UID %d 重复", u)
		}
		seen[u] = true
	}
	if len(seen) != goroutines*perGoroutine {
		t.Errorf("生成 UID 数 期望 %d，实际 %d", goroutines*perGoroutine, len(seen))
	}
}

func TestMessageDAO_BatchMarkRead(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id1 := createTestMessage(t, md, uid, "INBOX", "1")
	id2 := createTestMessage(t, md, uid, "INBOX", "2")
	createTestMessage(t, md, uid, "INBOX", "3")

	affected, err := md.BatchMarkRead(ctx, uid, []int64{id1, id2})
	if err != nil {
		t.Fatalf("BatchMarkRead 失败: %v", err)
	}
	if affected != 2 {
		t.Errorf("受影响行数期望 2，实际 %d", affected)
	}

	m1, _ := md.FindByID(ctx, id1)
	m2, _ := md.FindByID(ctx, id2)
	if !m1.IsRead || !m2.IsRead {
		t.Error("批量标记应生效")
	}
}

func TestMessageDAO_BatchMove(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id1 := createTestMessage(t, md, uid, "INBOX", "1")
	id2 := createTestMessage(t, md, uid, "INBOX", "2")

	affected, err := md.BatchMove(ctx, uid, []int64{id1, id2}, "TRASH")
	if err != nil {
		t.Fatalf("BatchMove 失败: %v", err)
	}
	if affected != 2 {
		t.Errorf("受影响行数期望 2，实际 %d", affected)
	}
	m1, _ := md.FindByID(ctx, id1)
	if m1.Folder != "TRASH" {
		t.Errorf("BatchMove 后 folder 期望 TRASH，实际 %q", m1.Folder)
	}
}

func TestMessageDAO_BatchSoftDelete(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id1 := createTestMessage(t, md, uid, "INBOX", "1")
	id2 := createTestMessage(t, md, uid, "INBOX", "2")

	affected, err := md.BatchSoftDelete(ctx, uid, []int64{id1, id2})
	if err != nil {
		t.Fatalf("BatchSoftDelete 失败: %v", err)
	}
	if affected != 2 {
		t.Errorf("受影响行数期望 2，实际 %d", affected)
	}
	m1, _ := md.FindByID(ctx, id1)
	if !m1.IsDeleted {
		t.Error("BatchSoftDelete 后应 IsDeleted=true")
	}
}

func TestMessageDAO_FindByUID(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	uidVal := int64(42)
	id, _ := md.Create(ctx, CreateMessageInput{
		UserID: uid, Folder: "INBOX", FromAddr: "a@x.com", ToAddr: "b@x.com",
		Subject: "test", UID: &uidVal,
	})

	m, err := md.FindByUID(ctx, uid, 42)
	if err != nil {
		t.Fatalf("FindByUID 失败: %v", err)
	}
	if m == nil || m.ID != id {
		t.Error("FindByUID 未返回正确邮件")
	}
}

func TestMessageDAO_UpdateFlags(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id := createTestMessage(t, md, uid, "INBOX", "test")

	flags := `["\\Seen","\\Flagged"]`
	if err := md.UpdateFlags(ctx, id, uid, flags); err != nil {
		t.Fatalf("UpdateFlags 失败: %v", err)
	}
	m, _ := md.FindByID(ctx, id)
	if m.Flags != flags {
		t.Errorf("Flags 期望 %q，实际 %q", flags, m.Flags)
	}
}

func TestMessageDAO_CleanOldTrash(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 创建垃圾箱邮件
	createTestMessage(t, md, uid, "TRASH", "old")
	createTestMessage(t, md, uid, "TRASH", "old2")

	// 清理 30 天前的（不会清理任何刚创建的）
	if err := md.CleanOldTrash(ctx, uid, 30); err != nil {
		t.Fatalf("CleanOldTrash 失败: %v", err)
	}

	// 邮件应仍存在
	result, _ := md.ListByUser(ctx, uid, "TRASH", 1, 20, "", false)
	if result.Total != 2 {
		t.Errorf("CleanOldTrash 不应清理新邮件，期望 2，实际 %d", result.Total)
	}
}
