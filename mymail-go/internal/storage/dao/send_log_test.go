// Package dao
// send_log_test.go 测试 SendLogDAO。
package dao

import (
	"context"
	"testing"
	"time"
)

func TestSendLogDAO_CreateAndUpdate(t *testing.T) {
	database := newTestDB(t)
	sd := NewSendLogDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	id, err := sd.Create(ctx, CreateSendLogInput{
		UserID:  uid,
		ToAddr:  "recipient@example.com",
		Subject: "Test",
		Status:  SendLogPending,
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if id <= 0 {
		t.Errorf("ID 应 > 0")
	}

	// 更新状态
	if err := sd.UpdateStatus(ctx, id, SendLogSent, ""); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}

	// 更新为失败
	if err := sd.UpdateStatus(ctx, id, SendLogFailed, "connection refused"); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}
}

func TestSendLogDAO_CreateDefaultStatus(t *testing.T) {
	database := newTestDB(t)
	sd := NewSendLogDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 不传 Status，应默认 pending
	id, err := sd.Create(ctx, CreateSendLogInput{
		UserID: uid,
		ToAddr: "r@example.com",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if id <= 0 {
		t.Errorf("ID 应 > 0")
	}
}

func TestSendLogDAO_RecentSentCount(t *testing.T) {
	database := newTestDB(t)
	sd := NewSendLogDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	// 创建 3 条 sent + 1 条 failed + 1 条 pending
	for i := 0; i < 3; i++ {
		id, _ := sd.Create(ctx, CreateSendLogInput{
			UserID: uid, ToAddr: "r@x.com", Status: SendLogPending,
		})
		sd.UpdateStatus(ctx, id, SendLogSent, "")
	}
	sd.Create(ctx, CreateSendLogInput{UserID: uid, ToAddr: "r@x.com", Status: SendLogFailed})
	sd.Create(ctx, CreateSendLogInput{UserID: uid, ToAddr: "r@x.com", Status: SendLogPending})

	count, err := sd.RecentSentCount(ctx, uid, 60*time.Second)
	if err != nil {
		t.Fatalf("RecentSentCount 失败: %v", err)
	}
	if count != 3 {
		t.Errorf("近期 sent 数期望 3，实际 %d", count)
	}
}

func TestSendLogDAO_RecentSentCount_Empty(t *testing.T) {
	database := newTestDB(t)
	sd := NewSendLogDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")

	count, err := sd.RecentSentCount(ctx, uid, 60*time.Second)
	if err != nil {
		t.Fatalf("RecentSentCount 失败: %v", err)
	}
	if count != 0 {
		t.Errorf("无记录应返回 0，实际 %d", count)
	}
}
