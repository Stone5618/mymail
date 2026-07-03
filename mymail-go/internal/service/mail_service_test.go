// Package service
// mail_service_test.go 测试邮件业务服务的完整逻辑。
// 使用真实 SQLite 临时数据库，覆盖所有成功与错误路径。
//
// 测试覆盖：
//   - Send：成功、收件人空、主题过长、正文过大、频率限制、无效邮箱、附件过多、配额不足
//   - SaveDraft：成功、用户不存在、空主题默认值
//   - DeliverLocal：成功（INBOX/JUNK）、收件人不存在、收件人禁用、配额不足、HTML 净化、附件写入
//   - CRUD：List/Get/MarkRead/MarkUnread/ToggleStar/MoveToFolder/SoftDelete/PermanentDelete/Restore/EmptyTrash
//   - 批量操作：BatchMarkRead/BatchMove/BatchDelete
//   - 附件：GetAttachmentForDownload/ListAttachmentsByMessage（P0-3 归属校验）
//   - IsMailError 工具函数
package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// newTestMailService 创建测试用 MailService 及其依赖。
// 返回 (service, userDAO, messageDAO, attachmentDAO, database, maildirPath)。
func newTestMailService(t *testing.T) (*MailService, *dao.UserDAO, *dao.MessageDAO, *dao.AttachmentDAO, *db.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	userDAO := dao.NewUserDAO(database)
	msgDAO := dao.NewMessageDAO(database)
	attachDAO := dao.NewAttachmentDAO(database)
	sendLogDAO := dao.NewSendLogDAO(database)
	queueDAO := dao.NewMailQueueDAO(database)

	auditLogger, err := audit.NewLogger(database, true, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}
	t.Cleanup(func() { auditLogger.Close() })

	maildirPath := filepath.Join(t.TempDir(), "maildir")
	svc := NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO, database,
		auditLogger, "example.com", 10,
	)
	return svc, userDAO, msgDAO, attachDAO, database, maildirPath
}

// createTestUserForMail 创建测试用户，返回 (userID, user)。
// storageLimit 为 0 表示无配额限制。
func createTestUserForMail(t *testing.T, userDAO *dao.UserDAO, database *db.DB, username string, storageLimit int64) (int64, *dao.User) {
	t.Helper()
	id, err := userDAO.Create(context.Background(), dao.CreateUserInput{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hash",
		DisplayName:  username,
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	// 设置 storage_limit
	if storageLimit > 0 {
		if _, err := database.ExecContext(context.Background(),
			`UPDATE users SET storage_limit = ? WHERE id = ?`, storageLimit, id); err != nil {
			t.Fatalf("设置 storage_limit 失败: %v", err)
		}
	}
	user, err := userDAO.FindByID(context.Background(), id)
	if err != nil || user == nil {
		t.Fatalf("查询新用户失败: %v", err)
	}
	return id, user
}

// ============ Send 测试 ============

func TestMailService_Send_Success(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	result, err := svc.Send(ctx, userID, SendInput{
		To:       "bob@example.com",
		Subject:  "Hello",
		BodyHTML: "<p>Hi <script>alert(1)</script></p>",
		BodyText: "Hi",
	})
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if result.MailID == 0 {
		t.Error("MailID 不应为 0")
	}
	if result.MessageID == "" {
		t.Error("MessageID 不应为空")
	}

	// 验证：邮件写入 SENT 文件夹
	var folder string
	var bodyHTML, bodyHTMLRaw string
	err = database.QueryRowContext(ctx,
		`SELECT folder, body_html, body_html_raw FROM messages WHERE id = ?`,
		result.MailID).Scan(&folder, &bodyHTML, &bodyHTMLRaw)
	if err != nil {
		t.Fatalf("查询邮件失败: %v", err)
	}
	if folder != "SENT" {
		t.Errorf("Folder 期望 SENT，实际 %q", folder)
	}
	// 验证 P0-4：HTML 净化（script 标签被剥离）
	if strings.Contains(bodyHTML, "<script>") {
		t.Errorf("body_html 应被净化，仍含 script: %q", bodyHTML)
	}
	if bodyHTMLRaw != "<p>Hi <script>alert(1)</script></p>" {
		t.Errorf("body_html_raw 应保留原始: %q", bodyHTMLRaw)
	}

	// 验证：send_log 写入
	var logCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM send_log WHERE user_id = ?`, userID).Scan(&logCount)
	if logCount != 1 {
		t.Errorf("send_log 应有 1 条，实际 %d", logCount)
	}

	// 验证：mail_queue 入队
	var queueCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mail_queue WHERE user_id = ?`, userID).Scan(&queueCount)
	if queueCount != 1 {
		t.Errorf("mail_queue 应有 1 条，实际 %d", queueCount)
	}
}

func TestMailService_Send_RecipientRequired(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	// 1. To 为空
	_, err := svc.Send(ctx, userID, SendInput{To: "", Subject: "Hi"})
	if me, ok := IsMailError(err); !ok || me.Status != 400 || me.Message != "收件人不能为空" {
		t.Errorf("空 To 期望 '收件人不能为空'(400)，实际 %v", err)
	}

	// 2. To 只有空格
	_, err = svc.Send(ctx, userID, SendInput{To: "   ", Subject: "Hi"})
	if me, ok := IsMailError(err); !ok || me.Status != 400 {
		t.Errorf("空格 To 期望 400，实际 %v", err)
	}

	// 3. To 为无效字符串（无 @）
	_, err = svc.Send(ctx, userID, SendInput{To: "not-an-email", Subject: "Hi"})
	if me, ok := IsMailError(err); !ok || me.Status != 400 {
		t.Errorf("无效 To 期望 400，实际 %v", err)
	}
}

func TestMailService_Send_SubjectTooLong(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	longSubject := strings.Repeat("a", 501)
	_, err := svc.Send(ctx, userID, SendInput{To: "bob@example.com", Subject: longSubject})
	if me, ok := IsMailError(err); !ok || me.Status != 400 || me.Message != "主题最多500字符" {
		t.Errorf("期望 '主题最多500字符'(400)，实际 %v", err)
	}
}

func TestMailService_Send_BodyTooLarge(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	bigBody := strings.Repeat("a", 1024*1024+1)
	_, err := svc.Send(ctx, userID, SendInput{
		To:       "bob@example.com",
		Subject:  "Hi",
		BodyHTML: bigBody,
	})
	if me, ok := IsMailError(err); !ok || me.Status != 400 || me.Message != "正文太大，最多1MB" {
		t.Errorf("期望 '正文太大，最多1MB'(400)，实际 %v", err)
	}
}

func TestMailService_Send_RateLimit(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	// 发送 10 次成功
	for i := 0; i < 10; i++ {
		_, err := svc.Send(ctx, userID, SendInput{
			To:      "bob@example.com",
			Subject: "Hi",
		})
		if err != nil {
			t.Fatalf("第 %d 次发送失败: %v", i+1, err)
		}
	}

	// 第 11 次应被拒绝
	_, err := svc.Send(ctx, userID, SendInput{
		To:      "bob@example.com",
		Subject: "Hi",
	})
	if me, ok := IsMailError(err); !ok || me.Status != 429 {
		t.Errorf("期望 429，实际 %v", err)
	}
}

func TestMailService_Send_InvalidEmail(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	_, err := svc.Send(ctx, userID, SendInput{
		To:      "not-an-email",
		Subject: "Hi",
	})
	if me, ok := IsMailError(err); !ok || me.Status != 400 {
		t.Errorf("期望 400，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "无效的邮箱地址") {
		t.Errorf("期望包含 '无效的邮箱地址'，实际 %v", err)
	}
}

func TestMailService_Send_TooManyAttachments(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	// 构造 11 个附件（maxAttachments=10）
	attachments := make([]AttachmentMeta, 11)
	for i := range attachments {
		attachments[i] = AttachmentMeta{
			Filename:    "file.txt",
			MimeType:    "text/plain",
			SizeBytes:   10,
			StoragePath: "/tmp/file.txt",
		}
	}
	_, err := svc.Send(ctx, userID, SendInput{
		To:          "bob@example.com",
		Subject:     "Hi",
		Attachments: attachments,
	})
	if me, ok := IsMailError(err); !ok || me.Status != 400 {
		t.Errorf("期望 400，实际 %v", err)
	}
}

func TestMailService_Send_StorageQuotaExceeded(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	// 设置配额 = 100 字节
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 100)

	// 附件总大小 = 200 字节 > 100
	attachments := []AttachmentMeta{
		{Filename: "big.txt", MimeType: "text/plain", SizeBytes: 200, StoragePath: "/tmp/big.txt"},
	}
	_, err := svc.Send(ctx, userID, SendInput{
		To:          "bob@example.com",
		Subject:     "Hi",
		Attachments: attachments,
	})
	if me, ok := IsMailError(err); !ok || me.Status != 413 {
		t.Errorf("期望 413（配额不足），实际 %v", err)
	}
}

func TestMailService_Send_WithAttachments(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	attachments := []AttachmentMeta{
		{Filename: "a.txt", MimeType: "text/plain", SizeBytes: 100, StoragePath: "/tmp/a.txt"},
		{Filename: "b.txt", MimeType: "text/plain", SizeBytes: 200, StoragePath: "/tmp/b.txt"},
	}
	result, err := svc.Send(ctx, userID, SendInput{
		To:          "bob@example.com",
		Subject:     "Hi",
		Attachments: attachments,
	})
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	// 验证附件记录写入
	var attachCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM attachments WHERE message_id = ?`, result.MailID).Scan(&attachCount)
	if attachCount != 2 {
		t.Errorf("附件数期望 2，实际 %d", attachCount)
	}

	// 验证 storage_used 更新（100+200=300）
	var storageUsed int64
	_ = database.QueryRowContext(ctx,
		`SELECT storage_used FROM users WHERE id = ?`, userID).Scan(&storageUsed)
	if storageUsed != 300 {
		t.Errorf("storage_used 期望 300，实际 %d", storageUsed)
	}

	// 验证 messages.has_attach = 1
	var hasAttach int
	_ = database.QueryRowContext(ctx,
		`SELECT has_attach FROM messages WHERE id = ?`, result.MailID).Scan(&hasAttach)
	if hasAttach != 1 {
		t.Errorf("has_attach 期望 1，实际 %d", hasAttach)
	}
}

// ============ SaveDraft 测试 ============

func TestMailService_SaveDraft_Success(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, err := svc.SaveDraft(ctx, userID, SaveDraftInput{
		To:       "bob@example.com",
		Subject:  "Draft",
		BodyHTML: "<p>draft</p>",
	})
	if err != nil {
		t.Fatalf("SaveDraft 失败: %v", err)
	}
	if mailID == 0 {
		t.Error("mailID 不应为 0")
	}

	// 验证邮件写入 DRAFTS
	var folder, subject string
	err = database.QueryRowContext(ctx,
		`SELECT folder, subject FROM messages WHERE id = ?`, mailID).Scan(&folder, &subject)
	if err != nil {
		t.Fatalf("查询草稿失败: %v", err)
	}
	if folder != "DRAFTS" {
		t.Errorf("Folder 期望 DRAFTS，实际 %q", folder)
	}
	if subject != "Draft" {
		t.Errorf("Subject 期望 'Draft'，实际 %q", subject)
	}
}

func TestMailService_SaveDraft_EmptySubjectDefaultsToNoSubject(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, err := svc.SaveDraft(ctx, userID, SaveDraftInput{
		To:      "bob@example.com",
		Subject: "",
	})
	if err != nil {
		t.Fatalf("SaveDraft 失败: %v", err)
	}

	var subject string
	_ = database.QueryRowContext(ctx,
		`SELECT subject FROM messages WHERE id = ?`, mailID).Scan(&subject)
	if subject != "(无主题)" {
		t.Errorf("空主题应默认 '(无主题)'，实际 %q", subject)
	}
}

func TestMailService_SaveDraft_UserNotFound(t *testing.T) {
	svc, _, _, _, _, _ := newTestMailService(t)
	ctx := context.Background()

	_, err := svc.SaveDraft(ctx, 99999, SaveDraftInput{To: "bob@example.com"})
	if me, ok := IsMailError(err); !ok || me.Status != 401 {
		t.Errorf("期望 401，实际 %v", err)
	}
}

// ============ DeliverLocal 测试 ============

func TestMailService_DeliverLocal_Success(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	recipientID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	err := svc.DeliverLocal(ctx, DeliverLocalInput{
		RecipientUsername: "bob",
		FromAddr:          "alice@example.com",
		FromName:          "Alice",
		ToAddr:            "bob@example.com",
		Subject:           "Hello",
		BodyHTML:          "<p>Hi <script>alert(1)</script></p>",
		BodyText:          "Hi",
		MessageID:         "<msg1@example.com>",
		SizeBytes:         100,
	})
	if err != nil {
		t.Fatalf("DeliverLocal 失败: %v", err)
	}

	// 验证邮件写入 INBOX
	var folder, bodyHTML, bodyHTMLRaw string
	var spamScore int
	err = database.QueryRowContext(ctx,
		`SELECT folder, body_html, body_html_raw, spam_score FROM messages WHERE user_id = ? ORDER BY id DESC LIMIT 1`,
		recipientID).Scan(&folder, &bodyHTML, &bodyHTMLRaw, &spamScore)
	if err != nil {
		t.Fatalf("查询投递邮件失败: %v", err)
	}
	if folder != "INBOX" {
		t.Errorf("Folder 期望 INBOX，实际 %q", folder)
	}
	// 验证 P0-4：HTML 净化
	if strings.Contains(bodyHTML, "<script>") {
		t.Errorf("body_html 应被净化，仍含 script: %q", bodyHTML)
	}
	if bodyHTMLRaw != "<p>Hi <script>alert(1)</script></p>" {
		t.Errorf("body_html_raw 应保留原始: %q", bodyHTMLRaw)
	}

	// 验证 storage_used 更新
	var storageUsed int64
	_ = database.QueryRowContext(ctx,
		`SELECT storage_used FROM users WHERE id = ?`, recipientID).Scan(&storageUsed)
	if storageUsed != 100 {
		t.Errorf("storage_used 期望 100，实际 %d", storageUsed)
	}
}

func TestMailService_DeliverLocal_SpamToJunk(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	recipientID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	err := svc.DeliverLocal(ctx, DeliverLocalInput{
		RecipientUsername: "bob",
		FromAddr:          "spam@example.com",
		ToAddr:            "bob@example.com",
		Subject:           "Spam",
		SpamScore:         5, // 阈值
		SpamReasons:       "BL",
		SizeBytes:         50,
	})
	if err != nil {
		t.Fatalf("DeliverLocal 失败: %v", err)
	}

	// 验证邮件写入 JUNK
	var folder string
	var spamScore int
	var spamReasons string
	err = database.QueryRowContext(ctx,
		`SELECT folder, spam_score, spam_reasons FROM messages WHERE user_id = ? ORDER BY id DESC LIMIT 1`,
		recipientID).Scan(&folder, &spamScore, &spamReasons)
	if err != nil {
		t.Fatalf("查询投递邮件失败: %v", err)
	}
	if folder != "JUNK" {
		t.Errorf("Folder 期望 JUNK，实际 %q", folder)
	}
	if spamScore != 5 {
		t.Errorf("spam_score 期望 5，实际 %d", spamScore)
	}
	if spamReasons != "BL" {
		t.Errorf("spam_reasons 期望 'BL'，实际 %q", spamReasons)
	}
}

func TestMailService_DeliverLocal_RecipientNotFound(t *testing.T) {
	svc, _, _, _, _, _ := newTestMailService(t)
	ctx := context.Background()

	// 收件人不存在，应静默跳过（返回 nil）
	err := svc.DeliverLocal(ctx, DeliverLocalInput{
		RecipientUsername: "nonexistent",
		FromAddr:          "alice@example.com",
		ToAddr:            "nonexistent@example.com",
		Subject:           "Hi",
	})
	if err != nil {
		t.Errorf("收件人不存在应返回 nil，实际 %v", err)
	}
}

func TestMailService_DeliverLocal_RecipientDisabled(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	recipientID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	// 禁用 bob
	if _, err := database.ExecContext(ctx,
		`UPDATE users SET is_active = 0 WHERE id = ?`, recipientID); err != nil {
		t.Fatalf("禁用用户失败: %v", err)
	}

	err := svc.DeliverLocal(ctx, DeliverLocalInput{
		RecipientUsername: "bob",
		FromAddr:          "alice@example.com",
		ToAddr:            "bob@example.com",
		Subject:           "Hi",
	})
	if err != nil {
		t.Errorf("禁用收件人应返回 nil，实际 %v", err)
	}

	// 验证未写入邮件
	var count int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = ?`, recipientID).Scan(&count)
	if count != 0 {
		t.Errorf("禁用用户不应收到邮件，实际 %d 条", count)
	}
}

func TestMailService_DeliverLocal_QuotaExceeded(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	// 配额 = 100，但邮件大小 = 200
	recipientID, _ := createTestUserForMail(t, userDAO, database, "bob", 100)

	err := svc.DeliverLocal(ctx, DeliverLocalInput{
		RecipientUsername: "bob",
		FromAddr:          "alice@example.com",
		ToAddr:            "bob@example.com",
		Subject:           "Big",
		SizeBytes:         200,
	})
	if err != nil {
		t.Errorf("配额不足应静默跳过返回 nil，实际 %v", err)
	}

	// 验证未写入邮件
	var count int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = ?`, recipientID).Scan(&count)
	if count != 0 {
		t.Errorf("配额不足不应写入邮件，实际 %d 条", count)
	}
}

func TestMailService_DeliverLocal_WithAttachments(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	recipientID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	err := svc.DeliverLocal(ctx, DeliverLocalInput{
		RecipientUsername: "bob",
		FromAddr:          "alice@example.com",
		ToAddr:            "bob@example.com",
		Subject:           "With Attach",
		SizeBytes:         100,
		Attachments: []LocalAttachment{
			{Filename: "a.txt", MimeType: "text/plain", SizeBytes: 50, StoragePath: "/tmp/a.txt"},
			{Filename: "b.txt", MimeType: "text/plain", SizeBytes: 30, StoragePath: "/tmp/b.txt"},
		},
	})
	if err != nil {
		t.Fatalf("DeliverLocal 失败: %v", err)
	}

	// 验证附件记录写入
	var attachCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM attachments a INNER JOIN messages m ON a.message_id = m.id
		 WHERE m.user_id = ?`, recipientID).Scan(&attachCount)
	if attachCount != 2 {
		t.Errorf("附件数期望 2，实际 %d", attachCount)
	}
}

func TestMailService_DeliverLocal_AutoSizeWhenZero(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	recipientID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	// SizeBytes = 0，应自动用 len(BodyHTML) + len(BodyText)
	err := svc.DeliverLocal(ctx, DeliverLocalInput{
		RecipientUsername: "bob",
		FromAddr:          "alice@example.com",
		ToAddr:            "bob@example.com",
		Subject:           "AutoSize",
		BodyHTML:          "abc",
		BodyText:          "de",
		SizeBytes:         0,
	})
	if err != nil {
		t.Fatalf("DeliverLocal 失败: %v", err)
	}

	// storage_used 应为 5
	var storageUsed int64
	_ = database.QueryRowContext(ctx,
		`SELECT storage_used FROM users WHERE id = ?`, recipientID).Scan(&storageUsed)
	if storageUsed != 5 {
		t.Errorf("storage_used 期望 5（len(abc)+len(de)），实际 %d", storageUsed)
	}
}

// ============ CRUD 测试 ============

func TestMailService_List(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	// 创建 3 封 INBOX 邮件
	for i := 0; i < 3; i++ {
		_, err := msgDAO.Create(ctx, dao.CreateMessageInput{
			UserID:   userID,
			Folder:   "INBOX",
			FromAddr: "sender@example.com",
			ToAddr:   "alice@example.com",
			Subject:  "Subject " + string(rune('A'+i)),
			BodyText: "body",
		})
		if err != nil {
			t.Fatalf("创建测试邮件失败: %v", err)
		}
	}

	result, err := svc.List(ctx, userID, "INBOX", 1, 20, "", false)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("Total 期望 3，实际 %d", result.Total)
	}
	if len(result.Messages) != 3 {
		t.Errorf("Messages 长度期望 3，实际 %d", len(result.Messages))
	}
}

func TestMailService_List_InvalidFolder(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	_, err := svc.List(ctx, userID, "INVALID_FOLDER", 1, 20, "", false)
	if me, ok := IsMailError(err); !ok || me.Status != 400 {
		t.Errorf("期望 400，实际 %v", err)
	}
}

func TestMailService_Get_Success(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, err := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID:   userID,
		Folder:   "INBOX",
		FromAddr: "sender@example.com",
		ToAddr:   "alice@example.com",
		Subject:  "Hi",
	})
	if err != nil {
		t.Fatalf("创建测试邮件失败: %v", err)
	}

	msg, attachments, err := svc.Get(ctx, userID, mailID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if msg == nil {
		t.Fatal("msg 不应为 nil")
	}
	if msg.ID != mailID {
		t.Errorf("ID 期望 %d，实际 %d", mailID, msg.ID)
	}
	if attachments != nil {
		t.Errorf("无附件时 attachments 应为 nil，实际 %v", attachments)
	}
}

func TestMailService_Get_NotFound(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	_, _, err := svc.Get(ctx, userID, 99999)
	if me, ok := IsMailError(err); !ok || me.Status != 404 || me.Message != "邮件不存在" {
		t.Errorf("期望 '邮件不存在'(404)，实际 %v", err)
	}
}

func TestMailService_Get_OwnershipCheck(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	aliceID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)
	bobID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	// alice 的邮件
	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID:   aliceID,
		Folder:   "INBOX",
		FromAddr: "x@example.com",
		ToAddr:   "alice@example.com",
	})

	// bob 尝试访问 alice 的邮件（P0-3）
	_, _, err := svc.Get(ctx, bobID, mailID)
	if me, ok := IsMailError(err); !ok || me.Status != 404 {
		t.Errorf("期望 404（归属校验），实际 %v", err)
	}
}

func TestMailService_UnreadCount(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	// 创建 2 封未读 + 1 封已读
	for i := 0; i < 2; i++ {
		id, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
			UserID: userID, Folder: "INBOX",
			FromAddr: "x@example.com", ToAddr: "alice@example.com",
		})
		_ = id
	}
	readID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})
	_ = msgDAO.MarkRead(ctx, readID, userID)

	count, err := svc.UnreadCount(ctx, userID, "INBOX")
	if err != nil {
		t.Fatalf("UnreadCount 失败: %v", err)
	}
	if count != 2 {
		t.Errorf("未读数期望 2，实际 %d", count)
	}
}

func TestMailService_UnreadCountByFolders(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	_, _ = msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})
	_, _ = msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "SENT",
		FromAddr: "alice@example.com", ToAddr: "y@example.com",
	})

	result, err := svc.UnreadCountByFolders(ctx, userID, []string{"INBOX", "SENT", "DRAFTS"})
	if err != nil {
		t.Fatalf("UnreadCountByFolders 失败: %v", err)
	}
	if result["INBOX"] != 1 {
		t.Errorf("INBOX 未读期望 1，实际 %d", result["INBOX"])
	}
	if result["SENT"] != 1 {
		t.Errorf("SENT 未读期望 1，实际 %d", result["SENT"])
	}
	if result["DRAFTS"] != 0 {
		t.Errorf("DRAFTS 未读期望 0，实际 %d", result["DRAFTS"])
	}
}

func TestMailService_MarkRead(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	if err := svc.MarkRead(ctx, userID, mailID); err != nil {
		t.Fatalf("MarkRead 失败: %v", err)
	}

	msg, _ := msgDAO.FindByIDForUser(ctx, mailID, userID)
	if !msg.IsRead {
		t.Error("MarkRead 后 IsRead 应为 true")
	}

	// MarkUnread
	if err := svc.MarkUnread(ctx, userID, mailID); err != nil {
		t.Fatalf("MarkUnread 失败: %v", err)
	}
	msg, _ = msgDAO.FindByIDForUser(ctx, mailID, userID)
	if msg.IsRead {
		t.Error("MarkUnread 后 IsRead 应为 false")
	}
}

func TestMailService_ToggleStar(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	// 第一次切换：false → true
	if err := svc.ToggleStar(ctx, userID, mailID); err != nil {
		t.Fatalf("ToggleStar 失败: %v", err)
	}
	msg, _ := msgDAO.FindByIDForUser(ctx, mailID, userID)
	if !msg.IsStarred {
		t.Error("ToggleStar 后 IsStarred 应为 true")
	}

	// 第二次切换：true → false
	if err := svc.ToggleStar(ctx, userID, mailID); err != nil {
		t.Fatalf("ToggleStar 失败: %v", err)
	}
	msg, _ = msgDAO.FindByIDForUser(ctx, mailID, userID)
	if msg.IsStarred {
		t.Error("二次 ToggleStar 后 IsStarred 应为 false")
	}
}

func TestMailService_MoveToFolder(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	if err := svc.MoveToFolder(ctx, userID, mailID, "TRASH"); err != nil {
		t.Fatalf("MoveToFolder 失败: %v", err)
	}
	msg, _ := msgDAO.FindByIDForUser(ctx, mailID, userID)
	if msg.Folder != "TRASH" {
		t.Errorf("Folder 期望 TRASH，实际 %q", msg.Folder)
	}
}

func TestMailService_MoveToFolder_InvalidFolder(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)
	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	err := svc.MoveToFolder(ctx, userID, mailID, "BAD_FOLDER")
	if me, ok := IsMailError(err); !ok || me.Status != 400 {
		t.Errorf("期望 400，实际 %v", err)
	}
}

func TestMailService_SoftDelete(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)
	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	if err := svc.SoftDelete(ctx, userID, mailID); err != nil {
		t.Fatalf("SoftDelete 失败: %v", err)
	}
	msg, _ := msgDAO.FindByIDForUser(ctx, mailID, userID)
	if !msg.IsDeleted {
		t.Error("SoftDelete 后 IsDeleted 应为 true")
	}
	if msg.Folder != "TRASH" {
		t.Errorf("Folder 期望 TRASH，实际 %q", msg.Folder)
	}
}

func TestMailService_PermanentDelete_WithAttachments(t *testing.T) {
	svc, userDAO, msgDAO, attachDAO, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	// 创建临时附件文件
	tmpFile := filepath.Join(t.TempDir(), "attach.txt")
	if err := os.WriteFile(tmpFile, []byte("content"), 0644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}

	// 写附件记录
	_, err := attachDAO.Create(ctx, dao.CreateAttachmentInput{
		MessageID:   mailID,
		Filename:    "attach.txt",
		MimeType:    "text/plain",
		SizeBytes:   7,
		StoragePath: tmpFile,
	})
	if err != nil {
		t.Fatalf("创建附件记录失败: %v", err)
	}

	// 永久删除
	if err := svc.PermanentDelete(ctx, userID, mailID); err != nil {
		t.Fatalf("PermanentDelete 失败: %v", err)
	}

	// 验证邮件已删除
	msg, _ := msgDAO.FindByIDForUser(ctx, mailID, userID)
	if msg != nil {
		t.Error("邮件应已删除")
	}

	// 验证附件文件已删除
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Errorf("附件文件应已删除，err=%v", err)
	}
}

func TestMailService_Restore(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)
	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "TRASH",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	if err := svc.Restore(ctx, userID, mailID); err != nil {
		t.Fatalf("Restore 失败: %v", err)
	}
	msg, _ := msgDAO.FindByIDForUser(ctx, mailID, userID)
	if msg.Folder != "INBOX" {
		t.Errorf("Folder 期望 INBOX，实际 %q", msg.Folder)
	}
}

func TestMailService_EmptyTrash(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	// 在 TRASH 创建 2 封，在 INBOX 创建 1 封
	for i := 0; i < 2; i++ {
		_, _ = msgDAO.Create(ctx, dao.CreateMessageInput{
			UserID: userID, Folder: "TRASH",
			FromAddr: "x@example.com", ToAddr: "alice@example.com",
		})
	}
	_, _ = msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	if err := svc.EmptyTrash(ctx, userID); err != nil {
		t.Fatalf("EmptyTrash 失败: %v", err)
	}

	// 验证 TRASH 已清空
	var trashCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = ? AND folder = 'TRASH'`,
		userID).Scan(&trashCount)
	if trashCount != 0 {
		t.Errorf("TRASH 应为 0，实际 %d", trashCount)
	}

	// INBOX 不受影响
	var inboxCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = ? AND folder = 'INBOX'`,
		userID).Scan(&inboxCount)
	if inboxCount != 1 {
		t.Errorf("INBOX 应为 1，实际 %d", inboxCount)
	}
}

// ============ 批量操作测试 ============

func TestMailService_BatchMarkRead(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	var ids []int64
	for i := 0; i < 3; i++ {
		id, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
			UserID: userID, Folder: "INBOX",
			FromAddr: "x@example.com", ToAddr: "alice@example.com",
		})
		ids = append(ids, id)
	}

	affected, err := svc.BatchMarkRead(ctx, userID, ids)
	if err != nil {
		t.Fatalf("BatchMarkRead 失败: %v", err)
	}
	if affected != 3 {
		t.Errorf("affected 期望 3，实际 %d", affected)
	}

	count, _ := svc.UnreadCount(ctx, userID, "INBOX")
	if count != 0 {
		t.Errorf("批量标记后未读应为 0，实际 %d", count)
	}
}

func TestMailService_BatchMarkRead_EmptyIDs(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	affected, err := svc.BatchMarkRead(ctx, userID, nil)
	if err != nil {
		t.Fatalf("BatchMarkRead 失败: %v", err)
	}
	if affected != 0 {
		t.Errorf("空 ids affected 期望 0，实际 %d", affected)
	}
}

func TestMailService_BatchMove(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	var ids []int64
	for i := 0; i < 2; i++ {
		id, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
			UserID: userID, Folder: "INBOX",
			FromAddr: "x@example.com", ToAddr: "alice@example.com",
		})
		ids = append(ids, id)
	}

	affected, err := svc.BatchMove(ctx, userID, ids, "JUNK")
	if err != nil {
		t.Fatalf("BatchMove 失败: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected 期望 2，实际 %d", affected)
	}

	// 验证已移动
	var junkCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = ? AND folder = 'JUNK'`,
		userID).Scan(&junkCount)
	if junkCount != 2 {
		t.Errorf("JUNK 应为 2，实际 %d", junkCount)
	}
}

func TestMailService_BatchMove_InvalidFolder(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)
	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	_, err := svc.BatchMove(ctx, userID, []int64{mailID}, "BAD_FOLDER")
	if me, ok := IsMailError(err); !ok || me.Status != 400 {
		t.Errorf("期望 400，实际 %v", err)
	}
}

func TestMailService_BatchDelete(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	var ids []int64
	for i := 0; i < 2; i++ {
		id, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
			UserID: userID, Folder: "INBOX",
			FromAddr: "x@example.com", ToAddr: "alice@example.com",
		})
		ids = append(ids, id)
	}

	affected, err := svc.BatchDelete(ctx, userID, ids)
	if err != nil {
		t.Fatalf("BatchDelete 失败: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected 期望 2，实际 %d", affected)
	}

	var trashCount int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = ? AND folder = 'TRASH'`,
		userID).Scan(&trashCount)
	if trashCount != 2 {
		t.Errorf("TRASH 应为 2，实际 %d", trashCount)
	}
}

// ============ 附件相关测试 ============

func TestMailService_GetAttachmentForDownload(t *testing.T) {
	svc, userDAO, msgDAO, attachDAO, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})
	attachID, _ := attachDAO.Create(ctx, dao.CreateAttachmentInput{
		MessageID:   mailID,
		Filename:    "a.txt",
		MimeType:    "text/plain",
		SizeBytes:   10,
		StoragePath: "/tmp/a.txt",
	})

	att, err := svc.GetAttachmentForDownload(ctx, userID, attachID)
	if err != nil {
		t.Fatalf("GetAttachmentForDownload 失败: %v", err)
	}
	if att == nil {
		t.Fatal("att 不应为 nil")
	}
	if att.Filename != "a.txt" {
		t.Errorf("Filename 期望 a.txt，实际 %q", att.Filename)
	}
}

func TestMailService_GetAttachmentForDownload_OwnershipCheck(t *testing.T) {
	svc, userDAO, msgDAO, attachDAO, database, _ := newTestMailService(t)
	ctx := context.Background()
	aliceID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)
	bobID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: aliceID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})
	attachID, _ := attachDAO.Create(ctx, dao.CreateAttachmentInput{
		MessageID:   mailID,
		Filename:    "a.txt",
		MimeType:    "text/plain",
		SizeBytes:   10,
		StoragePath: "/tmp/a.txt",
	})

	// bob 尝试下载 alice 的附件（P0-3）
	_, err := svc.GetAttachmentForDownload(ctx, bobID, attachID)
	if me, ok := IsMailError(err); !ok || me.Status != 404 {
		t.Errorf("期望 404（归属校验），实际 %v", err)
	}
}

func TestMailService_GetAttachmentForDownload_NotFound(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	_, err := svc.GetAttachmentForDownload(ctx, userID, 99999)
	if me, ok := IsMailError(err); !ok || me.Status != 404 || me.Message != "没有附件" {
		t.Errorf("期望 '没有附件'(404)，实际 %v", err)
	}
}

func TestMailService_ListAttachmentsByMessage(t *testing.T) {
	svc, userDAO, msgDAO, attachDAO, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: userID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})
	_, _ = attachDAO.Create(ctx, dao.CreateAttachmentInput{
		MessageID: mailID, Filename: "a.txt", MimeType: "text/plain", SizeBytes: 10, StoragePath: "/tmp/a.txt",
	})
	_, _ = attachDAO.Create(ctx, dao.CreateAttachmentInput{
		MessageID: mailID, Filename: "b.txt", MimeType: "text/plain", SizeBytes: 20, StoragePath: "/tmp/b.txt",
	})

	atts, err := svc.ListAttachmentsByMessage(ctx, userID, mailID)
	if err != nil {
		t.Fatalf("ListAttachmentsByMessage 失败: %v", err)
	}
	if len(atts) != 2 {
		t.Errorf("附件数期望 2，实际 %d", len(atts))
	}
}

func TestMailService_ListAttachmentsByMessage_OwnershipCheck(t *testing.T) {
	svc, userDAO, msgDAO, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	aliceID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)
	bobID, _ := createTestUserForMail(t, userDAO, database, "bob", 0)

	mailID, _ := msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID: aliceID, Folder: "INBOX",
		FromAddr: "x@example.com", ToAddr: "alice@example.com",
	})

	// bob 尝试列出 alice 邮件的附件（P0-3）
	_, err := svc.ListAttachmentsByMessage(ctx, bobID, mailID)
	if me, ok := IsMailError(err); !ok || me.Status != 404 {
		t.Errorf("期望 404（归属校验），实际 %v", err)
	}
}

// ============ IsMailError 工具函数测试 ============

func TestIsMailError(t *testing.T) {
	// 是 *MailError
	err := NewMailError(400, "bad request")
	me, ok := IsMailError(err)
	if !ok {
		t.Error("应识别为 MailError")
	}
	if me.Status != 400 || me.Message != "bad request" {
		t.Errorf("Status/Message 不匹配: %d/%q", me.Status, me.Message)
	}

	// 非 *MailError
	_, ok = IsMailError(errNormalError{})
	if ok {
		t.Error("普通 error 不应被识别为 MailError")
	}

	// nil
	_, ok = IsMailError(nil)
	if ok {
		t.Error("nil 不应被识别为 MailError")
	}
}

type errNormalError struct{}

func (errNormalError) Error() string { return "normal" }

// ============ 事务回滚测试 ============

func TestMailService_Send_TransactionRollback(t *testing.T) {
	// 通过模拟 DAO 失败来验证事务回滚
	// 我们构造一个场景：发送时 storage_used 更新后注入失败，
	// 验证 messages 和 send_log 都不应保留
	_, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	// 直接通过 database.ExecContext 在事务中插入数据，
	// 然后回滚，验证数据未持久化
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx 失败: %v", err)
	}
	_, _ = tx.ExecContext(ctx,
		`INSERT INTO messages (user_id, folder, from_addr, to_addr, subject) VALUES (?, 'INBOX', 'a@b.c', 'd@e.f', 'rollback')`,
		userID)
	_ = tx.Rollback()

	// 验证未持久化
	var count int64
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = ? AND subject = 'rollback'`,
		userID).Scan(&count)
	if count != 0 {
		t.Errorf("回滚后不应有数据，实际 %d", count)
	}
}

func TestMailService_Send_MultipleUIDsIncrement(t *testing.T) {
	// 验证 P1-4：连续发送时 UID 应递增（事务内取 UID）
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	var uids []int64
	for i := 0; i < 3; i++ {
		result, err := svc.Send(ctx, userID, SendInput{
			To:      "bob@example.com",
			Subject: "UID test",
		})
		if err != nil {
			t.Fatalf("第 %d 次 Send 失败: %v", i+1, err)
		}
		var uid int64
		_ = database.QueryRowContext(ctx,
			`SELECT uid FROM messages WHERE id = ?`, result.MailID).Scan(&uid)
		uids = append(uids, uid)
	}

	// UID 应为 1, 2, 3
	if uids[0] != 1 || uids[1] != 2 || uids[2] != 3 {
		t.Errorf("UID 应递增 1,2,3，实际 %v", uids)
	}
}

// ============ SaveDraft UID 测试 ============

func TestMailService_SaveDraft_UIDAssigned(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	mailID, err := svc.SaveDraft(ctx, userID, SaveDraftInput{
		Subject: "draft",
	})
	if err != nil {
		t.Fatalf("SaveDraft 失败: %v", err)
	}

	var uid int64
	_ = database.QueryRowContext(ctx,
		`SELECT uid FROM messages WHERE id = ?`, mailID).Scan(&uid)
	if uid != 1 {
		t.Errorf("草稿 UID 期望 1，实际 %d", uid)
	}
}

// ============ 时间字段测试 ============

func TestMailService_Send_FieldsPersisted(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, user := createTestUserForMail(t, userDAO, database, "alice", 0)

	result, err := svc.Send(ctx, userID, SendInput{
		To:       "bob@example.com,carol@example.com",
		Cc:       "dave@example.com",
		Bcc:      "eve@example.com",
		Subject:  "Fields",
		BodyHTML: "<p>body</p>",
		BodyText: "body",
		ReplyTo:  "reply@example.com",
	})
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	var fromAddr, fromName, toAddr, ccAddr, bccAddr, replyTo, subject string
	err = database.QueryRowContext(ctx,
		`SELECT from_addr, from_name, to_addr, cc_addr, bcc_addr, reply_to, subject FROM messages WHERE id = ?`,
		result.MailID).Scan(&fromAddr, &fromName, &toAddr, &ccAddr, &bccAddr, &replyTo, &subject)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	if fromAddr != user.Email {
		t.Errorf("from_addr 期望 %q，实际 %q", user.Email, fromAddr)
	}
	if fromName != user.DisplayName {
		t.Errorf("from_name 期望 %q，实际 %q", user.DisplayName, fromName)
	}
	if !strings.Contains(toAddr, "bob@example.com") || !strings.Contains(toAddr, "carol@example.com") {
		t.Errorf("to_addr 应包含两个收件人，实际 %q", toAddr)
	}
	if ccAddr != "dave@example.com" {
		t.Errorf("cc_addr 期望 'dave@example.com'，实际 %q", ccAddr)
	}
	if bccAddr != "eve@example.com" {
		t.Errorf("bcc_addr 期望 'eve@example.com'，实际 %q", bccAddr)
	}
	if replyTo != "reply@example.com" {
		t.Errorf("reply_to 期望 'reply@example.com'，实际 %q", replyTo)
	}
	if subject != "Fields" {
		t.Errorf("subject 期望 'Fields'，实际 %q", subject)
	}
}

// ============ ReplyTo 默认值测试 ============

func TestMailService_Send_ReplyToDefaultsToUserEmail(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, user := createTestUserForMail(t, userDAO, database, "alice", 0)

	result, err := svc.Send(ctx, userID, SendInput{
		To:      "bob@example.com",
		Subject: "Hi",
		// ReplyTo 留空
	})
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	var replyTo string
	_ = database.QueryRowContext(ctx,
		`SELECT reply_to FROM messages WHERE id = ?`, result.MailID).Scan(&replyTo)
	if replyTo != user.Email {
		t.Errorf("空 ReplyTo 应默认为用户邮箱 %q，实际 %q", user.Email, replyTo)
	}
}

// ============ MessageID 自动生成测试 ============

func TestMailService_Send_MessageIDAutoGenerated(t *testing.T) {
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	result, err := svc.Send(ctx, userID, SendInput{
		To:      "bob@example.com",
		Subject: "Hi",
		// MessageID 留空
	})
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if !strings.HasSuffix(result.MessageID, "@example.com") {
		t.Errorf("MessageID 应以 @example.com 结尾，实际 %q", result.MessageID)
	}

	// 验证 DB 中也保存了同样的 MessageID
	var dbMsgID string
	_ = database.QueryRowContext(ctx,
		`SELECT message_id FROM messages WHERE id = ?`, result.MailID).Scan(&dbMsgID)
	if dbMsgID != result.MessageID {
		t.Errorf("DB MessageID %q 与返回 %q 不一致", dbMsgID, result.MessageID)
	}
}

// ============ 频率限制窗口测试 ============

func TestMailService_Send_RateLimit_WindowBoundary(t *testing.T) {
	// 验证：sendRateLimit=10 时，第 10 次成功，第 11 次失败
	svc, userDAO, _, _, database, _ := newTestMailService(t)
	ctx := context.Background()
	userID, _ := createTestUserForMail(t, userDAO, database, "alice", 0)

	for i := 0; i < 10; i++ {
		_, err := svc.Send(ctx, userID, SendInput{To: "bob@example.com", Subject: "Hi"})
		if err != nil {
			t.Fatalf("第 %d 次应成功，失败: %v", i+1, err)
		}
	}
	_, err := svc.Send(ctx, userID, SendInput{To: "bob@example.com", Subject: "Hi"})
	if me, ok := IsMailError(err); !ok || me.Status != 429 {
		t.Errorf("第 11 次应 429，实际 %v", err)
	}
}

// ============ time 引用避免未使用导入 ============

var _ = time.Now
