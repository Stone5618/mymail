// smtp_test.go — E2E 测试：SMTP 接收 → 本地投递 → HTTP API 验证。
//
// 测试流程：
//  1. 启动真实 SMTP 服务器（随机端口）
//  2. 通过 SMTP 客户端发送邮件到本地用户
//  3. 通过 HTTP API 验证邮件出现在收件人 INBOX
package e2e

import (
	"context"
	"fmt"
	"net"
	netsmtp "net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/maildir"
	"github.com/mymail/mymail-go/internal/smtp"
)

// freePort 获取一个可用 TCP 端口（监听 :0 后取端口再关闭）。
func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("获取空闲端口失败: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// startSMTPReceiver 启动 SMTP 接收器（随机端口），返回地址与关闭函数。
func (e *testEnv) startSMTPReceiver(t testing.TB) (addr string) {
	t.Helper()
	port := freePort(t)
	e.cfg.SMTPPort = port

	mdir := maildir.New(e.cfg)
	receiver := smtp.NewReceiver(e.cfg, e.userDAO, e.mailSvc, mdir, e.attachStore)

	go func() {
		if err := receiver.Start(); err != nil {
			t.Logf("SMTP receiver 退出: %v", err)
		}
	}()

	addr = fmt.Sprintf("127.0.0.1:%d", port)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = receiver.Shutdown(ctx)
	})

	// 等待 SMTP 就绪（重试连接）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("SMTP 服务器未在 2 秒内就绪")
	return addr
}

// TestE2E_SMTPReceive 测试 SMTP 接收完整流程：
// 注册收件人 → 启动 SMTP → 发送邮件 → HTTP API 验证 INBOX。
func TestE2E_SMTPReceive(t *testing.T) {
	env := newTestEnv(t)

	// 1. 注册收件人（本地用户）
	recipientToken := env.registerAndLogin(t, "smtprecv", "SmtpRecvPass123!")

	// 2. 启动 SMTP 服务器
	smtpAddr := env.startSMTPReceiver(t)

	// 3. 通过 SMTP 客户端发送邮件
	from := "external@example.org"
	to := "smtprecv@example.com"
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: SMTP E2E 测试邮件",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"这是通过 SMTP 发送的 E2E 测试邮件正文。",
	}, "\r\n")

	err := smtpSendMail(smtpAddr, from, []string{to}, []byte(msg))
	if err != nil {
		t.Fatalf("SMTP 发送失败: %v", err)
	}
	t.Logf("SMTP 发送成功: from=%s to=%s", from, to)

	// 4. 等待投递完成（异步处理）
	time.Sleep(200 * time.Millisecond)

	// 5. 通过 HTTP API 验证 INBOX
	status, resp := env.doJSON(t, "GET", "/api/mail/list?folder=INBOX&page=1&limit=20", nil, recipientToken)
	if status != 200 {
		t.Fatalf("查询 INBOX 失败: status=%d, resp=%v", status, resp)
	}
	items, _ := resp["messages"].([]any)
	if len(items) != 1 {
		t.Fatalf("INBOX 期望 1 封邮件，实际 %d", len(items))
	}
	mail := items[0].(map[string]any)
	if subject, _ := mail["subject"].(string); subject != "SMTP E2E 测试邮件" {
		t.Fatalf("subject 期望 'SMTP E2E 测试邮件'，实际 %v", mail["subject"])
	}
	if fromAddr, _ := mail["from_addr"].(string); fromAddr != from {
		t.Fatalf("from_addr 期望 %s，实际 %v", from, mail["from_addr"])
	}
	t.Logf("SMTP 接收 + 本地投递验证通过: subject=%v", mail["subject"])
}

// TestE2E_SMTPRejectUnknownRecipient 测试 SMTP 拒绝未知收件人。
func TestE2E_SMTPRejectUnknownRecipient(t *testing.T) {
	env := newTestEnv(t)
	env.registerAndLogin(t, "known_user", "KnownPass123!")
	smtpAddr := env.startSMTPReceiver(t)

	// 向不存在的用户发邮件 → SMTP 应在 RCPT TO 阶段拒绝
	from := "external@example.org"
	to := "nonexistent@example.com"
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: 应被拒绝",
		"",
		"此邮件应被拒绝",
	}, "\r\n")

	err := smtpSendMail(smtpAddr, from, []string{to}, []byte(msg))
	if err == nil {
		t.Fatal("向未知收件人发信应失败，但 SMTP 接受了")
	}
	t.Logf("未知收件人正确被拒绝: %v", err)
}

// smtpSendMail 简单 SMTP 客户端：连接 → AUTH（如有）→ MAIL → RCPT → DATA → 退出。
func smtpSendMail(addr, from string, to []string, msg []byte) error {
	c, err := netsmtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("连接 SMTP 失败: %w", err)
	}
	defer c.Close()

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM 失败: %w", err)
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s 失败: %w", r, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA 失败: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("写邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("关闭 DATA 失败: %w", err)
	}

	return c.Quit()
}
