// Package dao
// attachment_test.go 测试 AttachmentDAO。
package dao

import (
	"context"
	"testing"
)

func TestAttachmentDAO_CreateAndFind(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ad := NewAttachmentDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")
	msgID := createTestMessage(t, md, uid, "INBOX", "test")

	attID, err := ad.Create(ctx, CreateAttachmentInput{
		MessageID:   msgID,
		Filename:    "doc.pdf",
		MimeType:    "application/pdf",
		SizeBytes:   1024,
		StoragePath: "/data/attachments/1/doc.pdf",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if attID <= 0 {
		t.Errorf("ID 应 > 0，实际 %d", attID)
	}

	// FindByMessageID
	list, err := ad.FindByMessageID(ctx, msgID)
	if err != nil {
		t.Fatalf("FindByMessageID 失败: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("附件数期望 1，实际 %d", len(list))
	}
	if list[0].Filename != "doc.pdf" {
		t.Errorf("Filename 期望 doc.pdf，实际 %q", list[0].Filename)
	}
	if list[0].StoragePath != "/data/attachments/1/doc.pdf" {
		t.Errorf("StoragePath 错误: %q", list[0].StoragePath)
	}

	// FindByID
	a, _ := ad.FindByID(ctx, attID)
	if a == nil {
		t.Fatal("FindByID 返回 nil")
	}
	if a.Filename != "doc.pdf" {
		t.Errorf("FindByID Filename 错误: %q", a.Filename)
	}
}

func TestAttachmentDAO_FindByID_P0_3(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ad := NewAttachmentDAO(database)
	ctx := context.Background()

	uid := createTestUser(t, database, "alice")
	other := createTestUser(t, database, "bob")
	msgID := createTestMessage(t, md, uid, "INBOX", "test")

	attID, _ := ad.Create(ctx, CreateAttachmentInput{
		MessageID:   msgID,
		Filename:    "secret.pdf",
		MimeType:    "application/pdf",
		SizeBytes:   100,
		StoragePath: "/data/attachments/1/secret.pdf",
	})

	// 正确用户应能查到
	a, err := ad.FindByIDForUser(ctx, attID, uid)
	if err != nil {
		t.Fatalf("FindByIDForUser 失败: %v", err)
	}
	if a == nil {
		t.Fatal("正确用户应能查到附件")
	}

	// 其他用户应查不到（P0-3 防越权）
	a, _ = ad.FindByIDForUser(ctx, attID, other)
	if a != nil {
		t.Error("P0-3 失败：其他用户不应能查到附件")
	}
}

func TestAttachmentDAO_DeleteByMessageID(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ad := NewAttachmentDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")
	msgID := createTestMessage(t, md, uid, "INBOX", "test")

	ad.Create(ctx, CreateAttachmentInput{MessageID: msgID, Filename: "a.pdf", StoragePath: "/a"})
	ad.Create(ctx, CreateAttachmentInput{MessageID: msgID, Filename: "b.pdf", StoragePath: "/b"})

	list, _ := ad.FindByMessageID(ctx, msgID)
	if len(list) != 2 {
		t.Errorf("附件数期望 2，实际 %d", len(list))
	}

	ad.DeleteByMessageID(ctx, msgID)
	list, _ = ad.FindByMessageID(ctx, msgID)
	if len(list) != 0 {
		t.Errorf("删除后附件数期望 0，实际 %d", len(list))
	}
}

func TestAttachmentDAO_FindByMessageIDs(t *testing.T) {
	database := newTestDB(t)
	md := NewMessageDAO(database)
	ad := NewAttachmentDAO(database)
	ctx := context.Background()
	uid := createTestUser(t, database, "alice")
	msg1 := createTestMessage(t, md, uid, "INBOX", "1")
	msg2 := createTestMessage(t, md, uid, "INBOX", "2")

	ad.Create(ctx, CreateAttachmentInput{MessageID: msg1, Filename: "a.pdf", StoragePath: "/a"})
	ad.Create(ctx, CreateAttachmentInput{MessageID: msg2, Filename: "b.pdf", StoragePath: "/b"})

	list, err := ad.FindByMessageIDs(ctx, []int64{msg1, msg2})
	if err != nil {
		t.Fatalf("FindByMessageIDs 失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("批量查询应返回 2，实际 %d", len(list))
	}

	// 空入参
	list, _ = ad.FindByMessageIDs(ctx, []int64{})
	if list != nil {
		t.Errorf("空入参应返回 nil")
	}
}
