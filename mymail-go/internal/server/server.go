// Package server 负责服务器编排：初始化 DB、HTTP 路由、启动与优雅关闭。
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/httpapi"
	"github.com/mymail/mymail-go/internal/mailsender"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/smtp"
	"github.com/mymail/mymail-go/internal/spam"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
	"github.com/mymail/mymail-go/internal/storage/maildir"
	"github.com/mymail/mymail-go/internal/tracing"
	"github.com/mymail/mymail-go/internal/ws"
)

// Server 持有应用全部运行时依赖。
type Server struct {
	cfg         *config.Config
	db          *db.DB
	httpSrv     *http.Server
	smtpRecv    *smtp.Receiver
	queue       *mailsender.QueueWorker
	tracer      *tracing.Tracer
	audit       *audit.Logger
	rateLimiter *smtp.RateLimiter       // SMTP 连接限流器（Shutdown 时停止后台清理）
	smtpSender  *mailsender.SMTPSender   // 外部 SMTP 发送器（供 queue 外域发送）
	wsHub       *ws.Hub                  // WebSocket 连接管理器（Shutdown 时优雅关闭）
}

// New 构建服务器实例，初始化 DB、迁移、追踪、路由。
func New(ctx context.Context, cfg *config.Config) (*Server, error) {
	// 1. 初始化数据库连接 + 自动迁移
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := database.Migrate(); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	slog.Info("数据库已就绪", "path", cfg.DBPath)

	// 2. 初始化 OpenTelemetry 追踪（可选）
	var tr *tracing.Tracer
	if cfg.TracingEnabled && cfg.TracingEndpoint != "" {
		tr, err = tracing.Init(ctx, cfg.TracingEndpoint, "mymail", cfg.TracingSampleRate)
		if err != nil {
			slog.Warn("追踪初始化失败，继续运行", "error", err)
		} else {
			slog.Info("追踪已启用", "endpoint", cfg.TracingEndpoint)
		}
	}

	// 3. 初始化 DAO 层
	userDAO := dao.NewUserDAO(database)
	msgDAO := dao.NewMessageDAO(database)
	attachDAO := dao.NewAttachmentDAO(database)
	sendLogDAO := dao.NewSendLogDAO(database)
	queueDAO := dao.NewMailQueueDAO(database)

	// 3.1 阶段 5：初始化 API Key / Settings / Rule DAO
	apiKeyDAO := dao.NewAPIKeyDAO(database)
	settingsDAO := dao.NewSettingsDAO(database)
	ruleDAO := dao.NewRuleDAO(database)

	// 4. 初始化 JWT 管理器（P0-8：强制校验已在 config.Validate 完成）
	jwtMgr, err := crypto.NewJWTManager(cfg.JWTSecret, cfg.JWTExpiresIn, cfg.JWTRememberExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("初始化 JWT 管理器失败: %w", err)
	}

	// 5. 初始化审计日志器（双写 DB + JSONL）
	jsonlPath := ""
	if cfg.AuditEnabled {
		jsonlPath = filepath.Join(filepath.Dir(cfg.DBPath), "audit.log")
	}
	auditLogger, err := audit.NewLogger(database, cfg.AuditEnabled, jsonlPath)
	if err != nil {
		return nil, fmt.Errorf("初始化审计日志器失败: %w", err)
	}

	// 6. 初始化 Service 层
	authSvc := service.NewAuthService(userDAO, jwtMgr, auditLogger, cfg.Domain, cfg.MaildirPath, cfg.AvatarPath)
	authSvc.SetMaxAvatarSize(cfg.MaxAvatarSize)
	mailSvc := service.NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO,
		database, auditLogger, cfg.Domain, cfg.SendRateLimitPerMin,
	)

	// 6.1 阶段 5：初始化 API Key / Admin / Rule Service
	//   - APIKeyService 实现 middleware.APIKeyVerifier 接口，供 /api/v1/* 认证
	//   - AdminService 处理管理员用户管理与全局设置
	//   - RuleService 处理用户邮件规则 CRUD
	//   注：每用户 API Key 上限 10 把
	apiKeySvc := service.NewAPIKeyService(apiKeyDAO, auditLogger, 10)
	adminSvc := service.NewAdminService(userDAO, settingsDAO, msgDAO, auditLogger, cfg.Domain)
	ruleSvc := service.NewRuleService(ruleDAO)

	// 6.2 初始化附件文件存储（P0-7：内部含 MIME 白名单 + 魔数校验）
	attachStore := attachment.New(cfg)

	// 6.3 初始化 Maildir（SMTP 接收器使用，与原 Node.js 兼容）
	mdir := maildir.New(cfg)

	// 7. 构建 HTTP 路由
	// 7.0 阶段 6：初始化 WebSocket Hub
	wsHub := ws.NewHub()

	router := httpapi.NewRouter(httpapi.Deps{
			Cfg:         cfg,
			DB:          database,
			UserDAO:     userDAO,
			JWTManager:  jwtMgr,
			AuthService: authSvc,
			MailService: mailSvc,
			AttachStore: attachStore,
			AvatarPath:  cfg.AvatarPath,
		APIKeyService: apiKeySvc,
		AdminService:  adminSvc,
		RuleService:   ruleSvc,
		WSHub:         wsHub,
	})

	// 8. 构建 HTTP Server
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	// 9. 构建 SMTP 接收器（阶段 3）
	smtpReceiver := smtp.NewReceiver(cfg, userDAO, mailSvc, mdir, attachStore)

	// 9.1 阶段 4：初始化反垃圾组件链路
	//   - 灰名单：GreylistDAO + GreylistChecker（特性开关 FeatureGreylist）
	//   - 反垃圾过滤器：SPFChecker + DNSBLChecker + Scorer + Filter（特性开关 FeatureSpamFilter）
	//   - 验证器：MessageValidator（整合 Filter + SpamLogDAO 落库）
	//   - 连接限流器：RateLimiter + 后台清理 goroutine
	//   - 注入到 SMTP 接收器（SetAntiSpam）
	greylistDAO := dao.NewGreylistDAO(database)
	greylistChecker := smtp.NewGreylistChecker(
		greylistDAO,
		int64(cfg.GreylistDelayMs),
		int64(cfg.GreylistTtlMs),
		cfg.FeatureGreylist,
	)
	slog.Info("灰名单已初始化",
		"enabled", cfg.FeatureGreylist,
		"delay_ms", cfg.GreylistDelayMs,
		"ttl_ms", cfg.GreylistTtlMs,
	)

	// 反垃圾过滤器（SPF + DNSBL + 评分）
	// 仅当 FeatureSpamFilter 开启时初始化 SPF/DNSBL/Scorer；否则 filter=nil，validator 内部直接放行
	var spamFilter *spam.Filter
	if cfg.FeatureSpamFilter {
		spfChecker := spam.NewSPFChecker(nil, cfg.SpfMaxDepth, cfg.SpfQueryTimeoutMs)
		dnsblChecker := spam.NewDNSBLChecker(nil, cfg.DnsblZones, cfg.DnsblQueryTimeoutMs)
		scorer := spam.NewScorer(cfg.SpamSuspiciousThreshold, cfg.SpamThreshold)
		spamFilter = spam.NewFilter(spfChecker, dnsblChecker, scorer, cfg.SpfEnabled, cfg.DnsblEnabled)
		slog.Info("反垃圾过滤器已初始化",
			"spf_enabled", cfg.SpfEnabled,
			"dnsbl_enabled", cfg.DnsblEnabled,
			"dnsbl_zones", cfg.DnsblZones,
			"suspicious_threshold", cfg.SpamSuspiciousThreshold,
			"spam_threshold", cfg.SpamThreshold,
		)
	} else {
		slog.Info("反垃圾过滤器已禁用（FEATURE_SPAM_FILTER=false）")
	}

	// SpamLogDAO（即使反垃圾关闭也创建实例；validator 内部判断是否落库）
	spamLogDAO := dao.NewSpamLogDAO(database)
	validator := smtp.NewMessageValidator(spamFilter, spamLogDAO, cfg.FeatureSpamFilter)

	// 连接限流器 + 启动后台清理（每 60 秒清理过期窗口）
	rateLimiter := smtp.NewRateLimiter(cfg.SMTPMaxConnectionsPerIp, int64(cfg.SMTPRateWindowMs))
	rateLimiter.StartCleanup(60 * time.Second)
	slog.Info("SMTP 连接限流器已启动",
		"max_per_ip", cfg.SMTPMaxConnectionsPerIp,
		"window_ms", cfg.SMTPRateWindowMs,
	)

	// 注入到 SMTP 接收器（在 Start 之前调用）
	smtpReceiver.SetAntiSpam(greylistChecker, rateLimiter, validator)

	// 10. 构建队列 worker（阶段 3 + 阶段 4 增强：外域收件人通过 SMTPSender 发送）
	localDeliverer := mailsender.NewLocalDeliverer(mailSvc, cfg.Domain)
	smtpSender := mailsender.NewSMTPSender(cfg)
	queueWorker := mailsender.NewQueueWorker(localDeliverer, queueDAO, 5*time.Second, 5, smtpSender)

	return &Server{
		cfg:         cfg,
		db:          database,
		httpSrv:     httpSrv,
		smtpRecv:    smtpReceiver,
		queue:       queueWorker,
		tracer:      tr,
		audit:       auditLogger,
		rateLimiter: rateLimiter,
		smtpSender:  smtpSender,
		wsHub:       wsHub,
	}, nil
}

// Start 启动 HTTP + SMTP 服务（阻塞调用）。
//
// 启动顺序：
//  1. 队列 worker（异步）
//  2. SMTP 接收器（异步，独立 goroutine）
//  3. HTTP 服务（阻塞，由 main 在独立 goroutine 启动）
func (s *Server) Start() error {
	// 1. 启动队列 worker
	s.queue.Start()

	// 2. 启动 SMTP 接收器（独立 goroutine）
	go func() {
		if err := s.smtpRecv.Start(); err != nil {
			slog.Error("SMTP 接收器异常", "error", err)
		}
	}()

	// 3. 启动 HTTP 服务（阻塞）
	slog.Info("HTTP 服务监听中", "addr", s.httpSrv.Addr)
	if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown 优雅关闭：停止接收新请求、等待在途请求完成、关闭 DB。
//
// 关闭顺序（与启动相反）：
//  1. HTTP 服务（停止接收新请求）
//  2. SMTP 接收器
//  3. 队列 worker
//  4. 追踪 exporter
//  5. 审计日志器
//  6. 数据库
func (s *Server) Shutdown(ctx context.Context) error {
	// 1. 停止 HTTP 服务
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		slog.Error("HTTP 关闭失败", "error", err)
	}

	// 2. 停止 SMTP 接收器
	if err := s.smtpRecv.Shutdown(ctx); err != nil {
		slog.Error("SMTP 接收器关闭失败", "error", err)
	}

	// 3. 停止队列 worker
	s.queue.Stop()

	// 3.1 停止 SMTP 限流器后台清理 goroutine（幂等）
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}

	// 3.2 阶段 6：优雅关闭 WebSocket Hub（draining 所有连接）
	if s.wsHub != nil {
		s.wsHub.Shutdown()
	}

	// 4. 关闭追踪 exporter
	if s.tracer != nil {
		if err := s.tracer.Shutdown(ctx); err != nil {
			slog.Error("追踪关闭失败", "error", err)
		}
	}

	// 5. 关闭审计日志器（刷盘 JSONL）
	if s.audit != nil {
		if err := s.audit.Close(); err != nil {
			slog.Error("审计日志关闭失败", "error", err)
		}
	}

	// 6. 关闭数据库
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			slog.Error("数据库关闭失败", "error", err)
		}
	}

	return nil
}
