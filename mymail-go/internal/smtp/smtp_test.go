// smtp_test.go 测试 SMTP 接收器与邮件解析。
//
// 测试策略：
//   - 启动真实 SMTP 服务器在随机端口
//   - 用 net/smtp 客户端发送 RFC 5322 邮件
//   - 验证邮件已写入 DB（messages 表）+ maildir 文件
//   - 测试 RCPT TO 拒绝场景（外部域名/用户不存在）
//   - 测试附件解析与保存
package smtp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
	"github.com/mymail/mymail-go/internal/storage/maildir"
)

// smtpTestEnv SMTP 测试环境。
type smtpTestEnv struct {
	receiver  *Receiver
	userDAO   *dao.UserDAO
	msgDAO    *dao.MessageDAO
	attachDAO *dao.AttachmentDAO
	queueDAO  *dao.MailQueueDAO
	mailSvc   *service.MailService
	attStore  *attachment.Store
	maildir   *maildir.Maildir
	addr      string // 监听地址（如 127.0.0.1:port）
	cfg       *config.Config
}

// newSMTPTestEnv 启动 SMTP 服务器在随机端口，返回测试环境。
func newSMTPTestEnv(t *testing.T) *smtpTestEnv {
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

	cfg := &config.Config{
		Env:                "test",
		Domain:             "example.com",
		AttachmentPath:     filepath.Join(t.TempDir(), "attachments"),
		MaildirPath:        filepath.Join(t.TempDir(), "maildir"),
		MaxAttachmentSize:  10 * 1024 * 1024,
		SMTPPort:           0, // 0 = 系统分配端口
		JWTSecret:          "test-secret-key-32-chars-min-padding",
	}

	mdir := maildir.New(cfg)
	attStore := attachment.New(cfg)
	mailSvc := service.NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO,
		database, auditLogger, cfg.Domain, 10,
	)

	// 使用 NewReceiver 后修改 Addr 为 ":0" 以让系统分配端口
	recv := NewReceiver(cfg, userDAO, mailSvc, mdir, attStore)
	// 直接构造监听器，端口 0 让系统分配
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()

	// 用 Serve 而非 ListenAndServe（已自行 listen）
	go func() {
		_ = recv.server.Serve(ln)
	}()
	// 关闭时停止服务器
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = recv.Shutdown(ctx)
	})

	// 创建测试用户
	createTestUser(t, userDAO, "alice", "alice@example.com")
	createTestUser(t, userDAO, "bob", "bob@example.com")

	return &smtpTestEnv{
		receiver:  recv,
		userDAO:   userDAO,
		msgDAO:    msgDAO,
		attachDAO: attachDAO,
		queueDAO:  queueDAO,
		mailSvc:   mailSvc,
		attStore:  attStore,
		maildir:   mdir,
		addr:      addr,
		cfg:       cfg,
	}
}

// createTestUser 创建测试用户（直接通过 DAO）。
func createTestUser(t *testing.T, userDAO *dao.UserDAO, username, email string) {
	t.Helper()
	_, err := userDAO.Create(context.Background(), dao.CreateUserInput{
		Username:     username,
		Email:        email,
		PasswordHash: "$2a$10$dummyhashdummyhashdummyhashdummyhashdummyhashdummyhashdumm",
		DisplayName:  username,
	})
	if err != nil {
		t.Fatalf("创建测试用户 %s 失败: %v", username, err)
	}
}

// sendSMTPRaw 发送 SMTP 邮件，返回错误。
// from/to 为邮箱地址，data 为完整 RFC 5322 邮件内容（CRLF 换行）。
func sendSMTPRaw(addr, from string, to []string, data []byte) error {
	host, port, _ := net.SplitHostPort(addr)
	_ = host
	_ = port

	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Hello("sender.example.org"); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildRFC5322 构造一个简单的 RFC 5322 邮件。
func buildRFC5322(from, to, subject, body string) []byte {
	return []byte("From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <test@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		body + "\r\n")
}

// ============ 基础投递测试 ============

// TestSMTP_DeliverToLocalUser 验证 SMTP 接收邮件后写入 DB 与 maildir。
func TestSMTP_DeliverToLocalUser(t *testing.T) {
	env := newSMTPTestEnv(t)

	raw := buildRFC5322("sender@external.org", "alice@example.com", "Hello Alice", "这是测试内容")
	if err := sendSMTPRaw(env.addr, "sender@external.org", []string{"alice@example.com"}, raw); err != nil {
		t.Fatalf("发送 SMTP 失败: %v", err)
	}

	// 等待投递完成（异步处理）
	time.Sleep(100 * time.Millisecond)

	// 验证 DB
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	if user == nil {
		t.Fatal("alice 用户应存在")
	}
	list, err := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if err != nil {
		t.Fatalf("查询邮件列表失败: %v", err)
	}
	if len(list.Messages) == 0 {
		t.Fatal("alice 的 INBOX 应至少有 1 封邮件")
	}
	msg := list.Messages[0]
	if msg.FromAddr != "sender@external.org" {
		t.Errorf("from_addr 期望 sender@external.org，实际 %s", msg.FromAddr)
	}
	if msg.Subject != "Hello Alice" {
		t.Errorf("subject 期望 'Hello Alice'，实际 %s", msg.Subject)
	}
	if msg.BodyText == "" || !strings.Contains(msg.BodyText, "这是测试内容") {
		t.Errorf("body_text 应含 '这是测试内容'，实际 %q", msg.BodyText)
	}
	if msg.Folder != "INBOX" {
		t.Errorf("folder 期望 INBOX，实际 %s", msg.Folder)
	}

	// 验证 maildir 文件已写入
	if !env.maildir.Exists("alice") {
		t.Error("alice 的 maildir 目录应存在")
	}
}

// TestSMTP_MultipleRecipients 验证多收件人投递。
func TestSMTP_MultipleRecipients(t *testing.T) {
	env := newSMTPTestEnv(t)

	raw := buildRFC5322("sender@external.org", "alice@example.com, bob@example.com", "群发", "Hi all")
	if err := sendSMTPRaw(env.addr, "sender@external.org",
		[]string{"alice@example.com", "bob@example.com"}, raw); err != nil {
		t.Fatalf("发送 SMTP 失败: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	// 两个收件人都应收到
	for _, username := range []string{"alice", "bob"} {
		user, _ := env.userDAO.FindByUsername(context.Background(), username)
		list, err := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
		if err != nil {
			t.Fatalf("查询 %s 邮件失败: %v", username, err)
		}
		if len(list.Messages) == 0 {
			t.Errorf("%s 应收到邮件", username)
		}
	}
}

// ============ RCPT TO 拒绝测试 ============

// TestSMTP_RcptTo_ExternalDomain 验证拒绝外部域名。
func TestSMTP_RcptTo_ExternalDomain(t *testing.T) {
	env := newSMTPTestEnv(t)

	raw := buildRFC5322("sender@external.org", "someone@other.com", "x", "y")
	err := sendSMTPRaw(env.addr, "sender@external.org",
		[]string{"someone@other.com"}, raw)
	if err == nil {
		t.Error("发送到外部域名应被拒绝")
	} else if !strings.Contains(err.Error(), "not our domain") &&
		!strings.Contains(err.Error(), "550") {
		t.Errorf("期望 'not our domain' 错误，实际 %v", err)
	}
}

// TestSMTP_RcptTo_UserNotFound 验证拒绝不存在的本域用户。
func TestSMTP_RcptTo_UserNotFound(t *testing.T) {
	env := newSMTPTestEnv(t)

	raw := buildRFC5322("sender@external.org", "nobody@example.com", "x", "y")
	err := sendSMTPRaw(env.addr, "sender@external.org",
		[]string{"nobody@example.com"}, raw)
	if err == nil {
		t.Error("发送到不存在的用户应被拒绝")
	} else if !strings.Contains(err.Error(), "user not found") &&
		!strings.Contains(err.Error(), "550") {
		t.Errorf("期望 'user not found' 错误，实际 %v", err)
	}
}

// TestSMTP_RcptTo_InvalidAddress 验证 RCPT TO 地址格式无效被拒绝。
func TestSMTP_RcptTo_InvalidAddress(t *testing.T) {
	env := newSMTPTestEnv(t)

	// 无 @ 符号的无效地址
	err := sendSMTPRaw(env.addr, "sender@external.org",
		[]string{"not-an-email"}, []byte("From: a\r\n\r\nbody\r\n"))
	if err == nil {
		t.Error("无效地址应被拒绝")
	}
}

// ============ 邮件解析测试 ============

// TestIsUnknownCharset 验证 unknown charset 错误识别。
func TestIsUnknownCharset(t *testing.T) {
	if !isUnknownCharset(errors.New("unknown charset: weird-encoding")) {
		t.Error("应识别 'unknown charset' 错误")
	}
	if isUnknownCharset(errors.New("other error")) {
		t.Error("不应识别其他错误为 unknown charset")
	}
	if isUnknownCharset(nil) {
		t.Error("nil 不应识别为 unknown charset")
	}
}

// TestParseMail_SimpleText 验证解析简单文本邮件。
func TestParseMail_SimpleText(t *testing.T) {
	data := buildRFC5322("sender@example.com", "alice@example.com", "测试主题", "正文内容")
	parsed, err := parseMail(data)
	if err != nil {
		t.Fatalf("解析邮件失败: %v", err)
	}
	if parsed.subject != "测试主题" {
		t.Errorf("subject 期望 '测试主题'，实际 %q", parsed.subject)
	}
	if parsed.fromAddr != "sender@example.com" {
		t.Errorf("fromAddr 期望 sender@example.com，实际 %q", parsed.fromAddr)
	}
	if !strings.Contains(parsed.bodyText, "正文内容") {
		t.Errorf("bodyText 应含 '正文内容'，实际 %q", parsed.bodyText)
	}
	if parsed.messageID != "test@example.com" {
		t.Errorf("messageID 期望 test@example.com，实际 %q", parsed.messageID)
	}
	if len(parsed.attachments) != 0 {
		t.Errorf("附件数应为 0，实际 %d", len(parsed.attachments))
	}
}

// TestParseMail_WithAttachment 验证解析带附件的 multipart 邮件。
func TestParseMail_WithAttachment(t *testing.T) {
	// 构造 multipart/mixed 邮件
	boundary := "BOUNDARY123"
	raw := "From: sender@example.com\r\n" +
		"To: alice@example.com\r\n" +
		"Subject: 带附件\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"正文\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; name=\"note.txt\"\r\n" +
		"Content-Disposition: attachment; filename=\"note.txt\"\r\n" +
		"\r\n" +
	"附件内容 hello\r\n" +
		"--" + boundary + "--\r\n"

	parsed, err := parseMail([]byte(raw))
	if err != nil {
		t.Fatalf("解析邮件失败: %v", err)
	}
	if parsed.subject != "带附件" {
		t.Errorf("subject 期望 '带附件'，实际 %q", parsed.subject)
	}
	if !strings.Contains(parsed.bodyText, "正文") {
		t.Errorf("bodyText 应含 '正文'，实际 %q", parsed.bodyText)
	}
	if len(parsed.attachments) != 1 {
		t.Fatalf("附件数应为 1，实际 %d", len(parsed.attachments))
	}
	att := parsed.attachments[0]
	if att.filename != "note.txt" {
		t.Errorf("附件名期望 note.txt，实际 %q", att.filename)
	}
	if att.mimeType != "text/plain" {
		t.Errorf("附件 MIME 期望 text/plain，实际 %q", att.mimeType)
	}
	if !strings.Contains(string(att.content), "附件内容") {
		t.Errorf("附件内容应含 '附件内容'，实际 %q", string(att.content))
	}
}

// TestParseMail_EmptySubject 验证空主题时的兜底处理。
func TestParseMail_EmptySubject(t *testing.T) {
	raw := []byte("From: sender@example.com\r\n" +
		"To: alice@example.com\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"\r\n" +
		"正文\r\n")
	parsed, err := parseMail(raw)
	if err != nil {
		t.Fatalf("解析邮件失败: %v", err)
	}
	if parsed.subject != "" {
		t.Errorf("空主题应保持空，实际 %q", parsed.subject)
	}
}

// ============ 附件端到端 + Receiver 方法测试 ============

// TestSMTP_WithAttachment 验证带附件邮件的端到端投递（覆盖 saveAttachment/headBytes）。
func TestSMTP_WithAttachment(t *testing.T) {
	env := newSMTPTestEnv(t)

	boundary := "TESTBOUNDARY456"
	raw := []byte("From: sender@external.org\r\n" +
		"To: alice@example.com\r\n" +
		"Subject: 带附件邮件\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <attach-test@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"正文内容\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; name=\"note.txt\"\r\n" +
		"Content-Disposition: attachment; filename=\"note.txt\"\r\n" +
		"\r\n" +
		"附件内容 hello world\r\n" +
		"--" + boundary + "--\r\n")

	if err := sendSMTPRaw(env.addr, "sender@external.org",
		[]string{"alice@example.com"}, raw); err != nil {
		t.Fatalf("发送 SMTP 失败: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// 验证邮件入库且标记为有附件
	user, _ := env.userDAO.FindByUsername(context.Background(), "alice")
	list, err := env.msgDAO.ListByUser(context.Background(), user.ID, "INBOX", 1, 50, "", false)
	if err != nil {
		t.Fatalf("查询邮件失败: %v", err)
	}
	if len(list.Messages) == 0 {
		t.Fatal("alice 应收到邮件")
	}
	msg := list.Messages[0]
	if !msg.HasAttach {
		t.Error("邮件应标记为有附件")
	}
	if msg.AttachCount != 1 {
		t.Errorf("附件数期望 1，实际 %d", msg.AttachCount)
	}

	// 验证 maildir 文件已写入
	if !env.maildir.Exists("alice") {
		t.Error("alice 的 maildir 目录应存在")
	}
}

// TestReceiver_Addr 验证 Addr() 返回配置的地址。
func TestReceiver_Addr(t *testing.T) {
	env := newSMTPTestEnv(t)
	// Addr() 返回 NewReceiver 中设置的 server.Addr
	addr := env.receiver.Addr()
	if addr == "" {
		t.Error("Addr() 不应为空")
	}
	// 配置中 SMTPPort=0，所以 Addr 应为 "0.0.0.0:0"
	if !strings.Contains(addr, "0.0.0.0") {
		t.Errorf("Addr 期望包含 '0.0.0.0'，实际 %s", addr)
	}
}

// TestReceiver_StartShutdown 验证 Start() + Shutdown() 生命周期（覆盖 Start 方法）。
// 不使用 newSMTPTestEnv（它已启动 Serve），而是独立创建 Receiver 测试 Start/ListenAndServe。
func TestReceiver_StartShutdown(t *testing.T) {
	// 最小化依赖：创建 DB + 用户 + Receiver
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开 DB 失败: %v", err)
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
	auditLogger, _ := audit.NewLogger(database, false, "")
	t.Cleanup(func() { auditLogger.Close() })

	// 创建测试用户
	if _, err := userDAO.Create(context.Background(), dao.CreateUserInput{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "$2a$10$dummyhashdummyhashdummyhashdummyhashdummyhashdummyhashdumm",
		DisplayName:  "alice",
	}); err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}

	cfg := &config.Config{
		Env:               "test",
		Domain:            "example.com",
		AttachmentPath:    filepath.Join(t.TempDir(), "attachments"),
		MaildirPath:       filepath.Join(t.TempDir(), "maildir"),
		MaxAttachmentSize: 10 * 1024 * 1024,
		SMTPPort:          0, // 占位，下面会被覆盖
		JWTSecret:         "test-secret-key-32-chars-min-padding",
	}
	mdir := maildir.New(cfg)
	attStore := attachment.New(cfg)
	mailSvc := service.NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO,
		database, auditLogger, cfg.Domain, 10,
	)

	// 分配随机端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("分配端口失败: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // 关闭，让 Start 重新监听

	recv := NewReceiver(cfg, userDAO, mailSvc, mdir, attStore)
	recv.server.Addr = addr // 覆盖为可用端口

	// 启动 Start（阻塞）
	errCh := make(chan error, 1)
	go func() {
		errCh <- recv.Start()
	}()

	// 等待服务就绪
	time.Sleep(150 * time.Millisecond)

	// 验证可用：发送一封简单邮件
	raw := buildRFC5322("sender@external.org", "alice@example.com", "Start 测试", "内容")
	if err := sendSMTPRaw(addr, "sender@external.org",
		[]string{"alice@example.com"}, raw); err != nil {
		t.Fatalf("发送 SMTP 失败: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 关闭
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := recv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}

	// Start 应返回 nil（正常关闭）
	if err := <-errCh; err != nil {
		t.Errorf("Start 返回非 nil: %v", err)
	}
}

// TestParseMail_MultipartAlternative 验证 multipart/alternative 邮件解析。
func TestParseMail_MultipartAlternative(t *testing.T) {
	boundary := "ALTBOUNDARY789"
	raw := []byte("From: sender@example.com\r\n" +
		"To: alice@example.com\r\n" +
		"Subject: 多部分邮件\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"纯文本版本\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>HTML 版本</p>\r\n" +
		"--" + boundary + "--\r\n")

	parsed, err := parseMail(raw)
	if err != nil {
		t.Fatalf("解析邮件失败: %v", err)
	}
	if !strings.Contains(parsed.bodyText, "纯文本版本") {
		t.Errorf("bodyText 应含 '纯文本版本'，实际 %q", parsed.bodyText)
	}
	if !strings.Contains(parsed.bodyHTML, "<p>HTML 版本</p>") {
		t.Errorf("bodyHTML 应含 HTML 内容，实际 %q", parsed.bodyHTML)
	}
}

// TestParseMail_SmallAttachment 验证小附件（< 16 字节）解析（覆盖 headBytes 短分支）。
func TestParseMail_SmallAttachment(t *testing.T) {
	boundary := "SMALLBOUNDARY"
	raw := []byte("From: sender@example.com\r\n" +
		"To: alice@example.com\r\n" +
		"Subject: 小附件\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"正文\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; name=\"tiny.txt\"\r\n" +
		"Content-Disposition: attachment; filename=\"tiny.txt\"\r\n" +
		"\r\n" +
		"hi\r\n" + // 仅 3 字节（< 16），覆盖 headBytes 短分支
		"--" + boundary + "--\r\n")

	parsed, err := parseMail(raw)
	if err != nil {
		t.Fatalf("解析邮件失败: %v", err)
	}
	if len(parsed.attachments) != 1 {
		t.Fatalf("附件数应为 1，实际 %d", len(parsed.attachments))
	}
	if parsed.attachments[0].filename != "tiny.txt" {
		t.Errorf("附件名期望 tiny.txt，实际 %q", parsed.attachments[0].filename)
	}
	if !strings.HasPrefix(string(parsed.attachments[0].content), "hi") {
		t.Errorf("附件内容应以 'hi' 开头，实际 %q", string(parsed.attachments[0].content))
	}
}

// TestParseMail_HTMLOnly 验证仅 HTML 正文（无 text/plain）的邮件。
func TestParseMail_HTMLOnly(t *testing.T) {
	boundary := "HTMLONLY"
	raw := []byte("From: sender@example.com\r\n" +
		"To: alice@example.com\r\n" +
		"Subject: 仅HTML\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>只有HTML</p>\r\n" +
		"--" + boundary + "--\r\n")

	parsed, err := parseMail(raw)
	if err != nil {
		t.Fatalf("解析邮件失败: %v", err)
	}
	// bodyText 应被兜底为 bodyHTML
	if parsed.bodyHTML == "" {
		t.Error("bodyHTML 不应为空")
	}
	if parsed.bodyText == "" {
		t.Error("bodyText 应被兜底填充")
	}
}

// TestNewReceiver_WithTLS 验证 NewReceiver 配置 TLS 证书分支。
func TestNewReceiver_WithTLS(t *testing.T) {
	// 生成自签名测试证书
	certPath := filepath.Join(t.TempDir(), "cert.pem")
	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := generateTestCert(certPath, keyPath); err != nil {
		t.Fatalf("生成测试证书失败: %v", err)
	}

	cfg := &config.Config{
		Env:               "test",
		Domain:            "example.com",
		AttachmentPath:    t.TempDir(),
		MaildirPath:       t.TempDir(),
		MaxAttachmentSize: 10 * 1024 * 1024,
		SMTPPort:          0,
		SMTPTLSCert:       certPath,
		SMTPTLSKey:        keyPath,
		JWTSecret:         "test-secret-key-32-chars-min-padding",
	}

	recv := NewReceiver(cfg, nil, nil, nil, nil)
	if recv == nil {
		t.Fatal("NewReceiver 不应返回 nil")
	}
	if recv.server.TLSConfig == nil {
		t.Error("TLSConfig 应已配置")
	}
}

// TestNewReceiver_InvalidTLS 验证 NewReceiver 加载无效 TLS 证书时回退明文。
func TestNewReceiver_InvalidTLS(t *testing.T) {
	cfg := &config.Config{
		Env:               "test",
		Domain:            "example.com",
		AttachmentPath:    t.TempDir(),
		MaildirPath:       t.TempDir(),
		MaxAttachmentSize: 10 * 1024 * 1024,
		SMTPPort:          0,
		SMTPTLSCert:       "/nonexistent/cert.pem",
		SMTPTLSKey:        "/nonexistent/key.pem",
		JWTSecret:         "test-secret-key-32-chars-min-padding",
	}

	recv := NewReceiver(cfg, nil, nil, nil, nil)
	if recv == nil {
		t.Fatal("NewReceiver 不应返回 nil")
	}
	// 加载失败时应回退明文，TLSConfig 为 nil
	if recv.server.TLSConfig != nil {
		t.Error("无效证书时 TLSConfig 应为 nil（回退明文）")
	}
}

// generateTestCert 生成自签名 ECDSA 证书写入文件。
func generateTestCert(certPath, keyPath string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"test.example.com"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certFile.Close()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	return pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}
