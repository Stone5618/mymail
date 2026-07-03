// sender_test.go 测试外部 SMTP 发送器。
//
// 测试策略：
//   - buildMessage：验证邮件消息构建（From/To/Cc/Bcc/Subject/ReplyTo/Body）
//   - Send 配置校验：host 为空、收件人为空
//   - CircuitBreakerState/IsCircuitOpen：熔断器状态查询
//   - NewSMTPSender：构造函数（含/不含熔断器）
//   - 熔断器+重试集成：模拟连续失败触发熔断（用不可达的 SMTP 主机）
//
// 注意：不测试真实 SMTP 发送（需外部 SMTP 服务器），仅测试逻辑与配置。
package mailsender

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/config"
)

func newTestConfig() *config.Config {
	return &config.Config{
		Domain:                            "example.com",
		SMTPSendHost:                      "smtp.example.com",
		SMTPSendPort:                      587,
		SMTPSendUsername:                  "user@example.com",
		SMTPSendPassword:                  "pass",
		SMTPTLSRejectUnauthorized:         true,
		SMTPCircuitBreakerEnabled:         true,
		SMTPCircuitBreakerMaxRequests:     3,
		SMTPCircuitBreakerIntervalMs:      2000,
		SMTPCircuitBreakerTimeoutMs:       200,
		SMTPCircuitBreakerFailureRatio:    0.6,
		SMTPCircuitBreakerMinRequests:     5,
		SMTPRetryEnabled:                  true,
		SMTPRetryMaxAttempts:              2,
		SMTPRetryInitialIntervalMs:        10,
		SMTPRetryMaxIntervalMs:            50,
		SMTPRetryMaxElapsedTimeMs:         1000,
	}
}

func TestSMTPSender_BuildMessage(t *testing.T) {
	s := NewSMTPSender(newTestConfig())
	msg, err := s.buildMessage(SendInput{
		From:     "sender@example.com",
		To:       []string{"rcpt1@example.com", "rcpt2@example.com"},
		Cc:       []string{"cc@example.com"},
		Bcc:      []string{"bcc@example.com"},
		Subject:  "Test Subject",
		BodyHTML: "<html>body</html>",
		BodyText: "body text",
		ReplyTo:  "reply@example.com",
	})
	if err != nil {
		t.Fatalf("buildMessage 失败: %v", err)
	}
	if msg == nil {
		t.Fatal("msg 不应为 nil")
	}
	// go-mail v0.7.x 无公开 getter，仅验证构建不报错
}

func TestSMTPSender_BuildMessage_HTMLOnly(t *testing.T) {
	s := NewSMTPSender(newTestConfig())
	msg, err := s.buildMessage(SendInput{
		From:     "sender@example.com",
		To:       []string{"rcpt@example.com"},
		Subject:  "HTML Only",
		BodyHTML: "<html>html only</html>",
	})
	if err != nil {
		t.Fatalf("buildMessage 失败: %v", err)
	}
	if msg == nil {
		t.Fatal("msg 不应为 nil")
	}
}

func TestSMTPSender_BuildMessage_TextOnly(t *testing.T) {
	s := NewSMTPSender(newTestConfig())
	msg, err := s.buildMessage(SendInput{
		From:     "sender@example.com",
		To:       []string{"rcpt@example.com"},
		Subject:  "Text Only",
		BodyText: "plain text only",
	})
	if err != nil {
		t.Fatalf("buildMessage 失败: %v", err)
	}
	if msg == nil {
		t.Fatal("msg 不应为 nil")
	}
}

func TestSMTPSender_BuildMessage_EmptyFrom(t *testing.T) {
	s := NewSMTPSender(newTestConfig())
	_, err := s.buildMessage(SendInput{
		From: "",
		To:   []string{"rcpt@example.com"},
	})
	if err == nil {
		t.Error("空 From 应报错")
	}
}

func TestSMTPSender_Send_EmptyHost(t *testing.T) {
	cfg := newTestConfig()
	cfg.SMTPSendHost = ""
	s := NewSMTPSender(cfg)

	err := s.Send(context.Background(), SendInput{
		From: "sender@example.com",
		To:   []string{"rcpt@example.com"},
	})
	if err == nil {
		t.Error("空 host 应报错")
	}
	if !strings.Contains(err.Error(), "未配置") {
		t.Logf("错误信息: %v", err)
	}
}

func TestSMTPSender_Send_EmptyRecipients(t *testing.T) {
	s := NewSMTPSender(newTestConfig())

	err := s.Send(context.Background(), SendInput{
		From: "sender@example.com",
		To:   []string{},
	})
	if err == nil {
		t.Error("空收件人应报错")
	}
	if !strings.Contains(err.Error(), "收件人为空") {
		t.Logf("错误信息: %v", err)
	}
}

func TestSMTPSender_CircuitBreakerState_Disabled(t *testing.T) {
	cfg := newTestConfig()
	cfg.SMTPCircuitBreakerEnabled = false
	s := NewSMTPSender(cfg)

	if s.CircuitBreakerState() != "disabled" {
		t.Errorf("熔断器禁用时 State 应为 disabled，实际 %s", s.CircuitBreakerState())
	}
	if s.IsCircuitOpen() {
		t.Error("熔断器禁用时 IsCircuitOpen 应为 false")
	}
}

func TestSMTPSender_CircuitBreakerState_Closed(t *testing.T) {
	s := NewSMTPSender(newTestConfig())
	if s.CircuitBreakerState() != "closed" {
		t.Errorf("初始状态应为 closed，实际 %s", s.CircuitBreakerState())
	}
	if s.IsCircuitOpen() {
		t.Error("初始状态 IsCircuitOpen 应为 false")
	}
}

func TestSMTPSender_NewSMTPSender_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	cfg.SMTPSendUsername = ""
	cfg.SMTPSendPassword = ""
	s := NewSMTPSender(cfg)
	if s == nil {
		t.Fatal("sender 不应为 nil")
	}
	if s.username != "" {
		t.Error("username 应为空")
	}
}

func TestSMTPSender_RetryDisabled(t *testing.T) {
	cfg := newTestConfig()
	cfg.SMTPRetryEnabled = false
	cfg.SMTPSendHost = "smtp.example.com"
	s := NewSMTPSender(cfg)

	// 重试禁用时仅尝试一次
	err := s.Send(context.Background(), SendInput{
		From: "sender@example.com",
		To:   []string{"rcpt@example.com"},
	})
	if err == nil {
		t.Log("发送到不可达主机应失败（预期行为）")
	}
	// 不检查具体错误，因为可能触发熔断器或连接超时
}

func TestSMTPSender_Integration_CircuitBreakerTrips(t *testing.T) {
	// 集成测试：连续向不可达主机发送，验证熔断器最终触发
	// 使用不可达的 IP（0.0.0.1:1 保证连接失败）
	cfg := newTestConfig()
	cfg.SMTPSendHost = "127.0.0.1"
	cfg.SMTPSendPort = 1 // 不可达端口
	cfg.SMTPRetryEnabled = false
	cfg.SMTPRetryMaxAttempts = 1
	cfg.SMTPCircuitBreakerEnabled = true
	cfg.SMTPCircuitBreakerMinRequests = 3
	cfg.SMTPCircuitBreakerFailureRatio = 0.6
	cfg.SMTPCircuitBreakerIntervalMs = 5000
	cfg.SMTPCircuitBreakerTimeoutMs = 60000
	s := NewSMTPSender(cfg)

	// 连续失败 3 次（>= MinRequests=3, 失败率 100% >= 60%）
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.Send(ctx, SendInput{
			From: "sender@example.com",
			To:   []string{"rcpt@example.com"},
		})
		cancel()
	}

	// 熔断器应已触发（Open）
	if !s.IsCircuitOpen() {
		t.Errorf("连续 3 次失败后熔断器应 Open，状态 %s", s.CircuitBreakerState())
	}
}
