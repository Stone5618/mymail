// Package dao
// queue_test.go 测试 MailQueueDAO。
package dao

import (
	"context"
	"testing"
	"time"
)

func TestMailQueueDAO_EnqueueAndFind(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := qd.Enqueue(ctx, EnqueueInput{
		UserID:    uid,
		FromAddr:  "alice@example.com",
		ToAddrs:   "bob@example.com",
		Subject:   "Test",
		BodyHTML:  "<p>Hello</p>",
		BodyText:  "Hello",
	})
	if err != nil {
		t.Fatalf("Enqueue 失败: %v", err)
	}
	if id <= 0 {
		t.Errorf("ID 应 > 0")
	}

	item, err := qd.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if item == nil {
		t.Fatal("FindByID 返回 nil")
	}
	if item.Status != QueuePending {
		t.Errorf("Status 期望 pending，实际 %q", item.Status)
	}
	if item.Attempts != 0 {
		t.Errorf("Attempts 期望 0，实际 %d", item.Attempts)
	}
	if item.MaxAttempts != 3 {
		t.Errorf("MaxAttempts 期望 3，实际 %d", item.MaxAttempts)
	}
	if item.ToAddrs != "bob@example.com" {
		t.Errorf("ToAddrs 错误: %q", item.ToAddrs)
	}
}

func TestMailQueueDAO_ClaimNextPending(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 入队 3 条
	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com", Subject: "1"})
	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com", Subject: "2"})
	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com", Subject: "3"})

	// 领取第一条
	item, err := qd.ClaimNextPending(ctx)
	if err != nil {
		t.Fatalf("ClaimNextPending 失败: %v", err)
	}
	if item == nil {
		t.Fatal("应返回一条 pending 项")
	}
	if item.Status != QueueSending {
		t.Errorf("Status 期望 sending，实际 %q", item.Status)
	}
	if item.Attempts != 1 {
		t.Errorf("Attempts 期望 1，实际 %d", item.Attempts)
	}

	// 标记已发送
	if err := qd.MarkSent(ctx, item.ID); err != nil {
		t.Fatalf("MarkSent 失败: %v", err)
	}
	sent, _ := qd.FindByID(ctx, item.ID)
	if sent.Status != QueueSent {
		t.Errorf("Status 期望 sent，实际 %q", sent.Status)
	}
	if sent.SentAt == "" {
		t.Error("SentAt 应非空")
	}

	// 再领取应得到第二条
	item2, _ := qd.ClaimNextPending(ctx)
	if item2 == nil || item2.ID == item.ID {
		t.Error("应领取下一条")
	}

	// 再领取第三条
	item3, _ := qd.ClaimNextPending(ctx)
	if item3 == nil {
		t.Error("应领取第三条")
	}

	// 再领取应返回 nil
	item4, _ := qd.ClaimNextPending(ctx)
	if item4 != nil {
		t.Error("无 pending 时应返回 nil")
	}
}

func TestMailQueueDAO_MarkFailed_Retry(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := qd.Enqueue(ctx, EnqueueInput{
		UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com",
		MaxAttempts: 3,
	})

	// 领取
	item, _ := qd.ClaimNextPending(ctx)
	if item == nil || item.ID != id {
		t.Fatal("领取失败")
	}

	// 标记失败（attempts=1 < max=3，应重新入队）
	if err := qd.MarkFailed(ctx, id, "smtp error", 5); err != nil {
		t.Fatalf("MarkFailed 失败: %v", err)
	}
	item, _ = qd.FindByID(ctx, id)
	if item.Status != QueuePending {
		t.Errorf("失败后应重新入队 pending，实际 %q", item.Status)
	}
	if item.ErrorMsg != "smtp error" {
		t.Errorf("ErrorMsg 期望 'smtp error'，实际 %q", item.ErrorMsg)
	}
	if !item.NextRetryAt.Valid {
		t.Error("NextRetryAt 应有效")
	}
}

func TestMailQueueDAO_MarkFailed_FinalFail(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := qd.Enqueue(ctx, EnqueueInput{
		UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com",
		MaxAttempts: 1,
	})

	// 领取后 attempts=1 >= max=1，应最终失败
	qd.ClaimNextPending(ctx)
	if err := qd.MarkFailed(ctx, id, "permanent error", 5); err != nil {
		t.Fatalf("MarkFailed 失败: %v", err)
	}
	item, _ := qd.FindByID(ctx, id)
	if item.Status != QueueFailed {
		t.Errorf("attempts >= max 时应置为 failed，实际 %q", item.Status)
	}
}

func TestMailQueueDAO_Cancel(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := qd.Enqueue(ctx, EnqueueInput{
		UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com",
	})

	if err := qd.Cancel(ctx, id); err != nil {
		t.Fatalf("Cancel 失败: %v", err)
	}
	item, _ := qd.FindByID(ctx, id)
	if item.Status != QueueCancelled {
		t.Errorf("Status 期望 cancelled，实际 %q", item.Status)
	}
}

func TestMailQueueDAO_PendingCount(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com"})
	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com"})
	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com"})

	count, err := qd.PendingCount(ctx)
	if err != nil {
		t.Fatalf("PendingCount 失败: %v", err)
	}
	if count != 3 {
		t.Errorf("PendingCount 期望 3，实际 %d", count)
	}
}

func TestMailQueueDAO_RequeueStale(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com"})
	// 领取后变 sending
	qd.ClaimNextPending(ctx)

	// 等待 1 秒确保 created_at < now（SQLite 时间精度为秒）
	time.Sleep(1100 * time.Millisecond)

	// 卡死 0 分钟（立即视为卡死）
	affected, err := qd.RequeueStale(ctx, 0)
	if err != nil {
		t.Fatalf("RequeueStale 失败: %v", err)
	}
	if affected != 1 {
		t.Errorf("受影响行数期望 1，实际 %d", affected)
	}

	item, _ := qd.FindByID(ctx, id)
	if item.Status != QueuePending {
		t.Errorf("卡死后应重置为 pending，实际 %q", item.Status)
	}
}

func TestMailQueueDAO_FindByUser(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")
	other := createTestUser(t, database, "bob")

	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com", Subject: "1"})
	qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com", Subject: "2"})
	qd.Enqueue(ctx, EnqueueInput{UserID: other, FromAddr: "a@x.com", ToAddrs: "b@x.com", Subject: "3"})

	list, err := qd.FindByUser(ctx, uid, 10)
	if err != nil {
		t.Fatalf("FindByUser 失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("用户 alice 应有 2 条，实际 %d", len(list))
	}
}

func TestMailQueueDAO_ClaimRespectsNextRetryAt(t *testing.T) {
	database := newTestDB(t)
	qd := NewMailQueueDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, _ := qd.Enqueue(ctx, EnqueueInput{UserID: uid, FromAddr: "a@x.com", ToAddrs: "b@x.com"})
	// 领取并标记失败，设置下次重试 5 分钟后
	qd.ClaimNextPending(ctx)
	qd.MarkFailed(ctx, id, "err", 5)

	// 此时 next_retry_at 在未来，ClaimNextPending 应返回 nil
	item, err := qd.ClaimNextPending(ctx)
	if err != nil {
		t.Fatalf("ClaimNextPending 失败: %v", err)
	}
	if item != nil {
		t.Error("next_retry_at 在未来时不应被领取")
	}
}
