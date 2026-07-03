// local_test.go 测试本地投递器与辅助函数。
//
// 测试覆盖：
//   - splitAddresses / splitAddress / dedupStrings 单元测试
//   - LocalDeliverer.DeliverFromQueue 集成测试（真实 DB）
//     - 本域 + 外域混合收件人
//     - 附件元信息解析
//     - 去重（to + cc + bcc）
package mailsender

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// ============ 辅助函数单元测试 ============

// TestSplitAddresses 验证地址拆分。
func TestSplitAddresses(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a@b.com", 1},
		{"a@b.com,c@d.com", 2},
		{" a@b.com , , c@d.com ", 2},
	}
	for _, c := range cases {
		got := splitAddresses(c.in)
		if len(got) != c.want {
			t.Errorf("splitAddresses(%q) 期望 %d 个，实际 %d (%v)", c.in, c.want, len(got), got)
		}
	}
}

// TestSplitAddress 验证单地址拆分。
func TestSplitAddress(t *testing.T) {
	cases := []struct {
		in     string
		user   string
		domain string
		ok     bool
	}{
		{"alice@example.com", "alice", "example.com", true},
		{"bob@sub.example.com", "bob", "sub.example.com", true},
		{"invalid", "", "", false},
		{"@example.com", "", "", false},
		{"alice@", "", "", false},
	}
	for _, c := range cases {
		user, domain, ok := splitAddress(c.in)
		if user != c.user || domain != c.domain || ok != c.ok {
			t.Errorf("splitAddress(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, user, domain, ok, c.user, c.domain, c.ok)
		}
	}
}

// TestDedupStrings 验证去重。
func TestDedupStrings(t *testing.T) {
	in := []string{"a@b.com", "c@d.com", "a@b.com", "e@f.com"}
	out := dedupStrings(in)
	if len(out) != 3 {
		t.Errorf("去重后应 3 个，实际 %d (%v)", len(out), out)
	}
}

// ============ LocalDeliverer 集成测试 ============

// mailsenderTestEnv mailsender 测试环境。
type mailsenderTestEnv struct {
	domain   string
	database *db.DB
	userDAO  *dao.UserDAO
	msgDAO   *dao.MessageDAO
	queueDAO *dao.MailQueueDAO
	mailSvc  *service.MailService
	local    *LocalDeliverer
}

// newMailsenderTestEnv 创建测试环境：临时 DB + 已注册用户。
func newMailsenderTestEnv(t *testing.T) *mailsenderTestEnv {
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

	auditLogger, err := audit.NewLogger(database, false, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}
	t.Cleanup(func() { auditLogger.Close() })

	domain := "example.com"
	mailSvc := service.NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO,
		database, auditLogger, domain, 10,
	)

	// 创建两个本域测试用户
	for _, name := range []string{"alice", "bob"} {
		if _, err := userDAO.Create(context.Background(), dao.CreateUserInput{
			Username:     name,
			Email:        name + "@" + domain,
			PasswordHash: "$2a$10$dummyhashdummyhashdummyhashdummyhashdummyhashdummyhashdumm",
			DisplayName:  name,
		}); err != nil {
			t.Fatalf("创建测试用户 %s 失败: %v", name, err)
		}
	}

	return &mailsenderTestEnv{
		domain:   domain,
		database: database,
		userDAO:  userDAO,
		msgDAO:   msgDAO,
		queueDAO: queueDAO,
		mailSvc:  mailSvc,
		local:    NewLocalDeliverer(mailSvc, domain),
	}
}

// TestLocalDeliverer_Domain 验证 Domain() 返回配置域名。
func TestLocalDeliverer_Domain(t *testing.T) {
	env := newMailsenderTestEnv(t)
	if env.local.Domain() != env.domain {
		t.Errorf("Domain() 期望 %s，实际 %s", env.domain, env.local.Domain())
	}
}

// TestLocalDeliverer_DeliverFromQueue_LocalOnly 验证纯本域收件人投递。
func TestLocalDeliverer_DeliverFromQueue_LocalOnly(t *testing.T) {
	env := newMailsenderTestEnv(t)

	item := &dao.MailQueueItem{
		ID:       1,
		FromAddr: "sender@external.org",
		ToAddrs:  "alice@example.com",
		Subject:  "测试本地投递",
		BodyText: "正文内容",
	}

	local, external, err := env.local.DeliverFromQueue(context.Background(), item)
	if err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if len(local) != 1 {
		t.Errorf("本域收件人应为 1，实际 %d (%v)", len(local), local)
	}
	if len(external) != 0 {
		t.Errorf("外域收件人应为 0，实际 %d (%v)", len(external), external)
	}

	// 验证 alice 收到邮件
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	list, err := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if err != nil {
		t.Fatalf("查询邮件失败: %v", err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("alice 应收到 1 封邮件，实际 %d", len(list.Messages))
	}
	if list.Messages[0].Subject != "测试本地投递" {
		t.Errorf("subject 期望 '测试本地投递'，实际 %s", list.Messages[0].Subject)
	}
}

// TestLocalDeliverer_DeliverFromQueue_Mixed 验证本域 + 外域混合收件人。
func TestLocalDeliverer_DeliverFromQueue_Mixed(t *testing.T) {
	env := newMailsenderTestEnv(t)

	item := &dao.MailQueueItem{
		ID:       1,
		FromAddr: "sender@external.org",
		ToAddrs:  "alice@example.com,remote@other.com",
		CcAddrs:  "bob@example.com",
		BccAddrs: "secret@secret.org",
		Subject:  "混合投递",
		BodyText: "内容",
	}

	local, external, err := env.local.DeliverFromQueue(context.Background(), item)
	if err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if len(local) != 2 {
		t.Errorf("本域收件人应为 2（alice+bob），实际 %d (%v)", len(local), local)
	}
	if len(external) != 2 {
		t.Errorf("外域收件人应为 2（remote+secret），实际 %d (%v)", len(external), external)
	}

	// alice 与 bob 都应收到
	for _, name := range []string{"alice", "bob"} {
		user, _ := env.userDAO.FindByUsername(context.Background(), name)
		list, _ := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
		if len(list.Messages) != 1 {
			t.Errorf("%s 应收到 1 封邮件，实际 %d", name, len(list.Messages))
		}
	}
}

// TestLocalDeliverer_DeliverFromQueue_Dedup 验证 to/cc/bcc 合并去重。
func TestLocalDeliverer_DeliverFromQueue_Dedup(t *testing.T) {
	env := newMailsenderTestEnv(t)

	item := &dao.MailQueueItem{
		ID:       1,
		FromAddr: "sender@external.org",
		ToAddrs:  "alice@example.com",
		CcAddrs:  "alice@example.com", // 重复
		Subject:  "去重测试",
		BodyText: "x",
	}

	local, _, err := env.local.DeliverFromQueue(context.Background(), item)
	if err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if len(local) != 1 {
		t.Errorf("去重后本域收件人应为 1，实际 %d (%v)", len(local), local)
	}

	// alice 只应收到 1 封邮件（不是 2 封）
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	list, _ := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if len(list.Messages) != 1 {
		t.Errorf("alice 应收到 1 封邮件（去重），实际 %d", len(list.Messages))
	}
}

// TestLocalDeliverer_DeliverFromQueue_WithAttachments 验证附件元信息解析。
func TestLocalDeliverer_DeliverFromQueue_WithAttachments(t *testing.T) {
	env := newMailsenderTestEnv(t)

	// 构造附件 JSON
	atts := []service.QueueAttachment{
		{Filename: "a.txt", MimeType: "text/plain", SizeBytes: 100, StoragePath: "/tmp/a.txt"},
		{Filename: "b.png", MimeType: "image/png", SizeBytes: 2048, StoragePath: "/tmp/b.png"},
	}
	attJSON, _ := json.Marshal(atts)

	item := &dao.MailQueueItem{
		ID:          1,
		FromAddr:    "sender@external.org",
		ToAddrs:     "alice@example.com",
		Subject:     "带附件",
		BodyText:    "正文",
		Attachments: string(attJSON),
	}

	local, _, err := env.local.DeliverFromQueue(context.Background(), item)
	if err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if len(local) != 1 {
		t.Errorf("本域收件人应为 1，实际 %d", len(local))
	}

	// 验证邮件入库（附件存储路径已不存在，但邮件本身应入库）
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	list, _ := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if len(list.Messages) != 1 {
		t.Errorf("alice 应收到 1 封邮件，实际 %d", len(list.Messages))
	}
}

// TestLocalDeliverer_DeliverFromQueue_InvalidAddress 验证无效地址被跳过。
func TestLocalDeliverer_DeliverFromQueue_InvalidAddress(t *testing.T) {
	env := newMailsenderTestEnv(t)

	item := &dao.MailQueueItem{
		ID:       1,
		FromAddr: "sender@external.org",
		ToAddrs:  "invalid-address, alice@example.com",
		Subject:  "无效地址测试",
		BodyText: "x",
	}

	local, _, err := env.local.DeliverFromQueue(context.Background(), item)
	if err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	// 无效地址被跳过，只 alice 入库
	if len(local) != 1 {
		t.Errorf("本域收件人应为 1（无效地址跳过），实际 %d (%v)", len(local), local)
	}
}

// TestLocalDeliverer_DeliverFromQueue_EmptyRecipients 验证空收件人不报错。
func TestLocalDeliverer_DeliverFromQueue_EmptyRecipients(t *testing.T) {
	env := newMailsenderTestEnv(t)

	item := &dao.MailQueueItem{
		ID:       1,
		FromAddr: "sender@external.org",
		ToAddrs:  "",
		Subject:  "无收件人",
		BodyText: "x",
	}

	local, external, err := env.local.DeliverFromQueue(context.Background(), item)
	if err != nil {
		t.Fatalf("空收件人不应报错: %v", err)
	}
	if len(local) != 0 || len(external) != 0 {
		t.Errorf("空收件人应返回空列表，实际 local=%d external=%d", len(local), len(external))
	}
}
