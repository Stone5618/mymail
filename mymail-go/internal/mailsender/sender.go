// sender.go 实现外部 SMTP 发送器。
//
// 职责：
//   - 通过 go-mail (wneessen/go-mail) 连接外部 SMTP 服务器发送邮件
//   - 集成熔断器（resilience.CircuitBreaker）：失败率 > 60% 时熔断
//   - 集成指数退避重试（resilience.Retry）：最多 3 次
//
// 调用链路：
//   Send → 熔断器.Execute → 重试.Retry → 实际 SMTP 发送
//   即：熔断器统计的是"整体发送（含重试）是否成功"
//
// 验收标准：
//   - 出站 SMTP 熔断器在失败率 > 60% 时触发（返回 ErrCircuitOpen）
//   - 失败自动重试（指数退避）
package mailsender

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mail "github.com/wneessen/go-mail"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/resilience"
)

// SendInput 外发邮件输入参数。
type SendInput struct {
	From     string   // 发件人邮箱
	To       []string // 收件人列表
	Cc       []string // 抄送列表
	Bcc      []string // 密送列表
	Subject  string   // 主题
	BodyHTML string   // HTML 正文
	BodyText string   // 纯文本正文
	ReplyTo  string   // 回复地址
}

// SMTPSender 外部 SMTP 发送器。
//
// 集成熔断器 + 指数退避重试，保护出站 SMTP。
// 线程安全：go-mail Client 可复用，但为简化每次发送创建新 Client。
type SMTPSender struct {
	host      string
	port      int
	username  string
	password  string
	tlsReject bool
	domain    string

	cb        *resilience.CircuitBreaker
	retryCfg  resilience.RetryConfig
	cbEnabled bool
	timeout   time.Duration
}

// NewSMTPSender 创建外部 SMTP 发送器。
//
// 配置来源（config.Config）：
//   - SMTPSendHost / SMTPSendPort / SMTPSendUsername / SMTPSendPassword
//   - SMTPTLSRejectUnauthorized
//   - SMTPCircuitBreaker* / SMTPRetry*
func NewSMTPSender(cfg *config.Config) *SMTPSender {
	cbEnabled := cfg.SMTPCircuitBreakerEnabled
	var cb *resilience.CircuitBreaker
	if cbEnabled {
		cb = resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			Name:          "smtp-sender",
			MaxRequests:   cfg.SMTPCircuitBreakerMaxRequests,
			Interval:      time.Duration(cfg.SMTPCircuitBreakerIntervalMs) * time.Millisecond,
			Timeout:       time.Duration(cfg.SMTPCircuitBreakerTimeoutMs) * time.Millisecond,
			FailureRatio:  cfg.SMTPCircuitBreakerFailureRatio,
			MinRequests:   cfg.SMTPCircuitBreakerMinRequests,
		})
	}

	retryCfg := resilience.RetryConfig{
		MaxAttempts:     cfg.SMTPRetryMaxAttempts,
		InitialInterval: time.Duration(cfg.SMTPRetryInitialIntervalMs) * time.Millisecond,
		MaxInterval:     time.Duration(cfg.SMTPRetryMaxIntervalMs) * time.Millisecond,
		MaxElapsedTime:  time.Duration(cfg.SMTPRetryMaxElapsedTimeMs) * time.Millisecond,
		Enabled:         cfg.SMTPRetryEnabled,
	}

	return &SMTPSender{
		host:      cfg.SMTPSendHost,
		port:      cfg.SMTPSendPort,
		username:  cfg.SMTPSendUsername,
		password:  cfg.SMTPSendPassword,
		tlsReject: cfg.SMTPTLSRejectUnauthorized,
		domain:    cfg.Domain,
		cb:        cb,
		retryCfg:  retryCfg,
		cbEnabled: cbEnabled,
		timeout:   30 * time.Second,
	}
}

// Send 发送邮件。
//
// 流程：
//  1. 若熔断器启用，通过熔断器 Execute 调用
//  2. 熔断器内部通过重试机制调用实际 SMTP 发送
//  3. 熔断器开启时返回 ErrCircuitOpen
//
// 参数：
//   - ctx: 上下文（用于超时/取消）
//   - in: 发送参数
func (s *SMTPSender) Send(ctx context.Context, in SendInput) error {
	if s.cbEnabled && s.cb != nil {
		return s.cb.Execute(func() error {
			return s.sendWithRetry(ctx, in)
		})
	}
	return s.sendWithRetry(ctx, in)
}

// sendWithRetry 带重试的实际发送。
func (s *SMTPSender) sendWithRetry(ctx context.Context, in SendInput) error {
	return resilience.Retry(ctx, s.retryCfg, func() error {
		return s.sendOnce(ctx, in)
	})
}

// sendOnce 执行一次 SMTP 发送。
func (s *SMTPSender) sendOnce(ctx context.Context, in SendInput) error {
	if s.host == "" {
		return errors.New("SMTP 发送主机未配置")
	}
	if len(in.To) == 0 {
		return errors.New("收件人为空")
	}

	// 构建邮件消息
	msg, err := s.buildMessage(in)
	if err != nil {
		return fmt.Errorf("构建邮件失败: %w", err)
	}

	// 构建 SMTP 客户端
	clientOpts := []mail.Option{
		mail.WithPort(s.port),
		mail.WithTimeout(s.timeout),
	}
	if s.username != "" && s.password != "" {
		clientOpts = append(clientOpts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(s.username),
			mail.WithPassword(s.password),
		)
	}
	// TLS 策略
	if s.tlsReject {
		clientOpts = append(clientOpts, mail.WithTLSPolicy(mail.TLSMandatory))
	} else {
		clientOpts = append(clientOpts, mail.WithTLSPolicy(mail.TLSOpportunistic))
		// 不强制校验证书：兼容 Postfix 自签名 / snakeoil 证书（与旧 Node.js 行为一致）。
		clientOpts = append(clientOpts, mail.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	}

	client, err := mail.NewClient(s.host, clientOpts...)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}

	// 发送
	if err := client.DialAndSend(msg); err != nil {
		slog.Warn("SMTP 发送失败",
			"host", s.host,
			"port", s.port,
			"from", in.From,
			"to", strings.Join(in.To, ","),
			"error", err,
		)
		return fmt.Errorf("SMTP 发送失败: %w", err)
	}

	slog.Info("SMTP 发送成功",
		"host", s.host,
		"from", in.From,
		"to_count", len(in.To),
	)
	return nil
}

// buildMessage 构建 go-mail 邮件消息。
func (s *SMTPSender) buildMessage(in SendInput) (*mail.Msg, error) {
	msg := mail.NewMsg()
	if err := msg.From(in.From); err != nil {
		return nil, fmt.Errorf("设置 From 失败: %w", err)
	}
	if err := msg.To(in.To...); err != nil {
		return nil, fmt.Errorf("设置 To 失败: %w", err)
	}
	if len(in.Cc) > 0 {
		if err := msg.Cc(in.Cc...); err != nil {
			return nil, fmt.Errorf("设置 Cc 失败: %w", err)
		}
	}
	if len(in.Bcc) > 0 {
		if err := msg.Bcc(in.Bcc...); err != nil {
			return nil, fmt.Errorf("设置 Bcc 失败: %w", err)
		}
	}
	if in.Subject != "" {
		msg.Subject(in.Subject)
	}
	if in.ReplyTo != "" {
		if err := msg.ReplyTo(in.ReplyTo); err != nil {
			return nil, fmt.Errorf("设置 ReplyTo 失败: %w", err)
		}
	}

	// 设置正文（HTML + 纯文本）
	if in.BodyHTML != "" && in.BodyText != "" {
		msg.SetBodyString(mail.TypeTextHTML, in.BodyHTML)
		msg.AddAlternativeString(mail.TypeTextPlain, in.BodyText)
	} else if in.BodyHTML != "" {
		msg.SetBodyString(mail.TypeTextHTML, in.BodyHTML)
	} else if in.BodyText != "" {
		msg.SetBodyString(mail.TypeTextPlain, in.BodyText)
	}

	return msg, nil
}

// CircuitBreakerState 返回熔断器状态字符串（监控用）。
// 熔断器未启用时返回 "disabled"。
func (s *SMTPSender) CircuitBreakerState() string {
	if !s.cbEnabled || s.cb == nil {
		return "disabled"
	}
	return s.cb.StateString()
}

// IsCircuitOpen 判断熔断器是否开启。
func (s *SMTPSender) IsCircuitOpen() bool {
	if !s.cbEnabled || s.cb == nil {
		return false
	}
	return s.cb.State() == 2 // gobreaker.StateOpen（gobreaker v2: Closed=0, HalfOpen=1, Open=2）
}
