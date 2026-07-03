// Package smtp 实现 SMTP 接收服务器。
//
// 接收外部 MTA 投递到本域的邮件，解析 RFC 5322 邮件格式，
// 将附件保存到磁盘，然后调用 MailService.DeliverLocal 完成本地投递。
//
// 设计要点：
//   - 基于 emersion/go-smtp 实现 Backend + Session 接口
//   - 兼容原 Node.js smtp-receiver.js 的行为（域名校验、收件人存在校验）
//   - 反垃圾（SPF/DNSBL/灰名单）在阶段 4 实现，本阶段仅做基础接收
//   - 支持优雅关闭（Shutdown）
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	smtpsrv "github.com/emersion/go-smtp"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/maildir"
)

// Receiver SMTP 接收服务器。
//
// 包装 emersion/go-smtp.Server，注入自实现 Backend。
// 与原 Node.js smtp-receiver.js 对应。
type Receiver struct {
	server  *smtpsrv.Server
	backend *backend
	closed  chan struct{}
	closeMu sync.Mutex
}

// NewReceiver 创建 SMTP 接收器。
//
// 参数：
//   - cfg: 配置（使用 SMTPPort/Domain/MaxAttachmentSize/SMTPTLSCert/SMTPTLSKey）
//   - userDAO: 用于校验收件人是否存在
//   - mailSvc: 调用 DeliverLocal 完成投递
//   - maildir: Maildir 文件存储（按原 Node.js 兼容，写一份到 new/）
//   - attStore: 附件存储（保存解析出的附件到磁盘）
func NewReceiver(
	cfg *config.Config,
	userDAO *dao.UserDAO,
	mailSvc *service.MailService,
	maildir *maildir.Maildir,
	attStore *attachment.Store,
) *Receiver {
	b := &backend{
		cfg:      cfg,
		userDAO:  userDAO,
		mailSvc:  mailSvc,
		maildir:  maildir,
		attStore: attStore,
	}
	s := smtpsrv.NewServer(b)
	s.Addr = fmt.Sprintf("0.0.0.0:%d", cfg.SMTPPort)
	s.Domain = cfg.Domain
	s.MaxRecipients = 50
	s.MaxMessageBytes = cfg.MaxAttachmentSize
	s.ReadTimeout = 60 * time.Second
	s.WriteTimeout = 60 * time.Second
	s.AllowInsecureAuth = true // 兼容原 Node.js authOptional: true

	// 配置 TLS（如有证书）
	if cfg.SMTPTLSCert != "" && cfg.SMTPTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.SMTPTLSCert, cfg.SMTPTLSKey)
		if err != nil {
			slog.Warn("加载 SMTP TLS 证书失败，回退到明文", "error", err)
		} else {
			s.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
			slog.Info("SMTP TLS 已启用", "cert", cfg.SMTPTLSCert)
		}
	}

	return &Receiver{
		server:  s,
		backend: b,
		closed:  make(chan struct{}),
	}
}

// SetAntiSpam 注入阶段 4 反垃圾组件（灰名单/限流/验证器）。
// 各参数可为 nil（nil 表示禁用对应功能）。
// 在 NewReceiver 之后、Start 之前调用。
func (r *Receiver) SetAntiSpam(greylist *GreylistChecker, rateLimiter *RateLimiter, validator *MessageValidator) {
	r.backend.greylist = greylist
	r.backend.rateLimiter = rateLimiter
	r.backend.validator = validator
}

// RateLimiter 返回限流器实例（server.go 用于启动后台清理 + Shutdown 停止）。
func (r *Receiver) RateLimiter() *RateLimiter {
	return r.backend.rateLimiter
}

// Start 启动 SMTP 服务（阻塞调用）。
func (r *Receiver) Start() error {
	slog.Info("SMTP 接收服务监听中", "addr", r.server.Addr, "domain", r.server.Domain)
	if err := r.server.ListenAndServe(); err != nil && !errors.Is(err, smtpsrv.ErrServerClosed) {
		// 优雅关闭时 server.Close 已关闭，可能返回 ErrServerClosed
		select {
		case <-r.closed:
			return nil
		default:
		}
		return err
	}
	return nil
}

// Shutdown 优雅关闭 SMTP 服务。
func (r *Receiver) Shutdown(ctx context.Context) error {
	r.closeMu.Lock()
	defer r.closeMu.Unlock()
	select {
	case <-r.closed:
		return nil
	default:
		close(r.closed)
	}
	slog.Info("SMTP 接收服务关闭中...")
	return r.server.Shutdown(ctx)
}

// Addr 返回监听地址（用于测试）。
func (r *Receiver) Addr() string {
	return r.server.Addr
}
