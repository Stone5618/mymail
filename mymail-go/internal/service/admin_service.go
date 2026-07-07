// Package service 实现业务逻辑层。
// admin_service.go 实现管理员业务：用户管理、统计。
//
// 设计：
//   - 管理员操作均记录审计日志（action="admin.*"）
//   - 删除用户采用软删除（SetActive(false)），保留数据完整性
//   - 创建用户时直接设置 storage_limit（P1-14 修复）
package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/sanitize"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// AdminService 管理员业务层。
type AdminService struct {
	userDAO  *dao.UserDAO
	msgDAO   *dao.MessageDAO
	audit    *audit.Logger
	auditDAO *audit.AuditDAO // 审计日志查询/清理（AC-6/AC-8），可能为 nil（未启用审计时）
	domain   string
	cfg      *config.Config
}

// AdminStats 管理员统计数据。
type AdminStats struct {
	TotalUsers     int64 `json:"total_users"`
	ActiveUsers    int64 `json:"active_users"`
	TodayReceived  int64 `json:"today_received"`
	TodaySent      int64 `json:"today_sent"`
}

// CreateUserAdminInput 管理员创建用户输入。
type CreateUserAdminInput struct {
	Username     string
	Email        string
	Password     string // 明文，service 内哈希
	DisplayName  string
	Role         string // 空则 "user"
	StorageLimit int64  // 0 则默认 100MB
}

// UpdateUserAdminInput 管理员更新用户输入（所有字段指针，nil 不更新）。
type UpdateUserAdminInput struct {
	Role         *string
	StorageLimit *int64
	IsActive     *bool
	Password     *string // 明文，非 nil 时哈希
}

// NewAdminService 创建 AdminService。
// auditDAO 可为 nil（未启用审计查询时）；AC-6 查询接口会在 auditDAO=nil 时返回 503。
func NewAdminService(userDAO *dao.UserDAO, msgDAO *dao.MessageDAO, auditLogger *audit.Logger, auditDAO *audit.AuditDAO, domain string, cfg *config.Config) *AdminService {
	return &AdminService{
		userDAO:  userDAO,
		msgDAO:   msgDAO,
		audit:    auditLogger,
		auditDAO: auditDAO,
		domain:   domain,
		cfg:      cfg,
	}
}

// DnsStatus DNS 记录检测结果。
type DnsStatus struct {
	MX    string
	SPF   string
	DMARC string
}

// dnsResolver 返回一个使用公共 DNS 服务器的解析器，避免依赖容器内嵌 DNS 代理。
func dnsResolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			// 优先使用 Google Public DNS，8.8.4.4 作为备用
			conn, err := d.DialContext(ctx, network, "8.8.8.8:53")
			if err != nil {
				return d.DialContext(ctx, network, "8.8.4.4:53")
			}
			return conn, nil
		},
	}
}

// CheckDns 检测当前域名 MX/SPF/DMARC 记录配置。
func (s *AdminService) CheckDns(ctx context.Context) (*DnsStatus, error) {
	domain := s.domain
	if domain == "" {
		return nil, fmt.Errorf("未配置域名")
	}

	resolver := dnsResolver()
	status := &DnsStatus{MX: "missing", SPF: "missing", DMARC: "missing"}

	// MX 记录
	if records, err := resolver.LookupMX(ctx, domain); err == nil && len(records) > 0 {
		status.MX = "ok"
	}

	// SPF 记录
	if records, err := resolver.LookupTXT(ctx, domain); err == nil {
		for _, r := range records {
			if strings.HasPrefix(r, "v=spf1") {
				status.SPF = "ok"
				break
			}
		}
	}

	// DMARC 记录
	if records, err := resolver.LookupTXT(ctx, "_dmarc."+domain); err == nil {
		for _, r := range records {
			if strings.HasPrefix(r, "v=DMARC1") {
				status.DMARC = "ok"
				break
			}
		}
	}

	return status, nil
}

// Stats 返回用户统计数据（含今日邮件收发量）。
func (s *AdminService) Stats(ctx context.Context) (*AdminStats, error) {
	total, err := s.userDAO.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计用户总数失败: %w", err)
	}
	active, err := s.userDAO.ActiveCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计活跃用户数失败: %w", err)
	}

	stats := &AdminStats{TotalUsers: total, ActiveUsers: active}
	if s.msgDAO != nil {
		if rcv, err := s.msgDAO.TodayReceivedCount(ctx); err == nil {
			stats.TodayReceived = rcv
		}
		if sent, err := s.msgDAO.TodaySentCount(ctx); err == nil {
			stats.TodaySent = sent
		}
	}
	return stats, nil
}

// ListUsers 列出所有用户。
func (s *AdminService) ListUsers(ctx context.Context) ([]*dao.User, error) {
	return s.userDAO.ListAll(ctx)
}

// GetUser 按 ID 查询用户，不存在返回 AuthError(404)。
func (s *AdminService) GetUser(ctx context.Context, id int64) (*dao.User, error) {
	u, err := s.userDAO.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	if u == nil {
		return nil, NewAuthError(http.StatusNotFound, "用户不存在")
	}
	return u, nil
}

// CreateUser 管理员创建用户。
// P1-14 修复：直接在 CreateUserInput 中设置 storage_limit，无需后续更新。
func (s *AdminService) CreateUser(ctx context.Context, in CreateUserAdminInput) (*dao.User, error) {
	// 密码哈希（Go bcrypt）
	hash, err := crypto.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("密码哈希失败: %w", err)
	}

	// Dovecot 兼容哈希（IMAP 认证用）
	dovecotHash, err := crypto.HashDovecotPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("生成 Dovecot 哈希失败: %w", err)
	}

	// 创建用户（StorageLimit=0 时由 DAO 默认 100MB）
	id, err := s.userDAO.Create(ctx, dao.CreateUserInput{
		Username:            in.Username,
		Email:               in.Email,
		PasswordHash:        hash,
		DovecotPasswordHash: dovecotHash,
		DisplayName:         in.DisplayName,
		Role:                in.Role,
		StorageLimit:        in.StorageLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	// 审计
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorAdmin,
			Action:       "admin.user.create",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", id),
			Result:       audit.ResultSuccess,
		})
	}

	// 返回完整用户
	u, err := s.userDAO.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("查询新用户失败: %w", err)
	}
	if u == nil {
		return nil, fmt.Errorf("新用户查询返回 nil")
	}
	return u, nil
}

// UpdateUser 管理员更新用户（按字段指针选择性更新）。
// 不存在返回 AuthError(404)。
func (s *AdminService) UpdateUser(ctx context.Context, id int64, in UpdateUserAdminInput) error {
	// 校验存在
	u, err := s.userDAO.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("查询用户失败: %w", err)
	}
	if u == nil {
		return NewAuthError(http.StatusNotFound, "用户不存在")
	}

	// 按字段更新
	if in.Role != nil {
		if err := s.userDAO.UpdateRole(ctx, id, *in.Role); err != nil {
			return fmt.Errorf("更新 role 失败: %w", err)
		}
	}
	if in.StorageLimit != nil {
		if err := s.userDAO.UpdateStorageLimit(ctx, id, *in.StorageLimit); err != nil {
			return fmt.Errorf("更新 storage_limit 失败: %w", err)
		}
	}
	if in.IsActive != nil {
		if err := s.userDAO.SetActive(ctx, id, *in.IsActive); err != nil {
			return fmt.Errorf("更新 is_active 失败: %w", err)
		}
	}
	if in.Password != nil {
		hash, err := crypto.HashPassword(*in.Password)
		if err != nil {
			return fmt.Errorf("密码哈希失败: %w", err)
		}
		dovecotHash, err := crypto.HashDovecotPassword(*in.Password)
		if err != nil {
			return fmt.Errorf("生成 Dovecot 哈希失败: %w", err)
		}
		if err := s.userDAO.UpdatePasswordAndDovecot(ctx, id, hash, dovecotHash); err != nil {
			return fmt.Errorf("更新 password 失败: %w", err)
		}
	}

	// 审计
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorAdmin,
			Action:       "admin.user.update",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", id),
			Result:       audit.ResultSuccess,
		})
	}
	return nil
}

// DeleteUser 软删除用户（SetActive=false，保留数据完整性）。
// 不存在返回 AuthError(404)。
func (s *AdminService) DeleteUser(ctx context.Context, id int64) error {
	// 校验存在
	u, err := s.userDAO.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("查询用户失败: %w", err)
	}
	if u == nil {
		return NewAuthError(http.StatusNotFound, "用户不存在")
	}

	// 软删除
	if err := s.userDAO.SetActive(ctx, id, false); err != nil {
		return fmt.Errorf("禁用用户失败: %w", err)
	}

	// 审计
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorAdmin,
			Action:       "admin.user.delete",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", id),
			Result:       audit.ResultSuccess,
		})
	}
	return nil
}

// SystemConfigGroup 系统配置分组（只读展示用）。
type SystemConfigGroup struct {
	Key   string             `json:"key"`
	Label string             `json:"label"`
	Items []SystemConfigItem `json:"items"`
}

// SystemConfigItem 单个配置项（label + value，敏感字段 value 显示 ***）。
type SystemConfigItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Secret bool   `json:"secret,omitempty"`
}

// GetSystemConfig 返回当前系统配置（按分组、脱敏，只读）。
// 敏感字段（密码、密钥）显示为 ***，不返回明文。
func (s *AdminService) GetSystemConfig() []SystemConfigGroup {
	c := s.cfg
	if c == nil {
		return nil
	}
	boolStr := func(b bool) string {
		if b {
			return "开启"
		}
		return "关闭"
	}
	dnsblZones := "未配置"
	if len(c.DnsblZones) > 0 {
		dnsblZones = strings.Join(c.DnsblZones, ", ")
	}
	corsOrigins := "未配置"
	if len(c.CORSAllowedOrigins) > 0 {
		corsOrigins = strings.Join(c.CORSAllowedOrigins, ", ")
	}
	return []SystemConfigGroup{
		{
			Key:   "system",
			Label: "系统",
			Items: []SystemConfigItem{
				{Key: "env", Label: "运行环境", Value: c.Env},
				{Key: "domain", Label: "域名", Value: c.Domain},
				{Key: "mail_host", Label: "邮件主机", Value: c.MailHost},
				{Key: "port", Label: "服务端口", Value: strconv.Itoa(c.Port)},
				{Key: "debug", Label: "调试模式", Value: boolStr(c.Debug)},
				{Key: "version", Label: "版本", Value: c.Version},
				{Key: "build_time", Label: "构建时间", Value: c.BuildTime},
				{Key: "commit_sha", Label: "提交 SHA", Value: c.CommitSHA},
			},
		},
		{
			Key:   "smtp",
			Label: "SMTP",
			Items: []SystemConfigItem{
				{Key: "smtp_port", Label: "SMTP 收信端口", Value: strconv.Itoa(c.SMTPPort)},
				{Key: "smtp_send_host", Label: "发信主机", Value: c.SMTPSendHost},
				{Key: "smtp_send_port", Label: "发信端口", Value: strconv.Itoa(c.SMTPSendPort)},
				{Key: "smtp_send_username", Label: "发信账号", Value: c.SMTPSendUsername},
				{Key: "smtp_send_password", Label: "发信密码", Value: "***", Secret: true},
				{Key: "smtp_tls_reject_unauthorized", Label: "TLS 严格校验", Value: boolStr(c.SMTPTLSRejectUnauthorized)},
				{Key: "smtp_max_connections_per_ip", Label: "单 IP 最大连接数", Value: strconv.Itoa(c.SMTPMaxConnectionsPerIp)},
			},
		},
		{
			Key:   "security",
			Label: "安全与审计",
			Items: []SystemConfigItem{
				{Key: "jwt_secret", Label: "JWT 密钥", Value: "***", Secret: true},
				{Key: "jwt_expires_in", Label: "JWT 有效期", Value: c.JWTExpiresIn},
				{Key: "jwt_remember_expires_in", Label: "JWT 记住有效期", Value: c.JWTRememberExpiresIn},
				{Key: "admin_password", Label: "管理员密码", Value: "***", Secret: true},
				{Key: "audit_enabled", Label: "审计开关", Value: boolStr(c.AuditEnabled)},
				{Key: "audit_retention_days", Label: "审计保留天数", Value: strconv.Itoa(c.AuditRetentionDays)},
			},
		},
		{
			Key:   "features",
			Label: "特性开关",
			Items: []SystemConfigItem{
				{Key: "feature_spam_filter", Label: "反垃圾", Value: boolStr(c.FeatureSpamFilter)},
				{Key: "feature_ws_notify", Label: "WS 推送", Value: boolStr(c.FeatureWSNotify)},
				{Key: "feature_greylist", Label: "灰名单", Value: boolStr(c.FeatureGreylist)},
				{Key: "feature_api_key", Label: "API Key", Value: boolStr(c.FeatureAPIKey)},
				{Key: "feature_rules", Label: "邮件规则", Value: boolStr(c.FeatureRules)},
				{Key: "feature_audit", Label: "审计", Value: boolStr(c.FeatureAudit)},
				{Key: "feature_registration", Label: "注册", Value: boolStr(c.FeatureRegistration)},
			},
		},
		{
			Key:   "limits",
			Label: "限制",
			Items: []SystemConfigItem{
				{Key: "max_attachment_size", Label: "最大附件", Value: fmt.Sprintf("%d MB", c.MaxAttachmentSize/1024/1024)},
				{Key: "max_avatar_size", Label: "最大头像", Value: fmt.Sprintf("%d MB", c.MaxAvatarSize/1024/1024)},
				{Key: "rate_limit_max", Label: "API 限流/分钟", Value: strconv.Itoa(c.RateLimitMax)},
				{Key: "send_rate_limit_per_min", Label: "发信限速/分钟", Value: strconv.Itoa(c.SendRateLimitPerMin)},
				{Key: "spam_threshold", Label: "垃圾评分阈值（拒收）", Value: strconv.Itoa(c.SpamThreshold)},
				{Key: "spam_suspicious_threshold", Label: "垃圾评分阈值（标记）", Value: strconv.Itoa(c.SpamSuspiciousThreshold)},
			},
		},
		{
			Key:   "anti_spam",
			Label: "反垃圾",
			Items: []SystemConfigItem{
				{Key: "spf_enabled", Label: "SPF 校验", Value: boolStr(c.SpfEnabled)},
				{Key: "spf_max_depth", Label: "SPF 递归深度", Value: strconv.Itoa(c.SpfMaxDepth)},
				{Key: "dnsbl_enabled", Label: "DNSBL 校验", Value: boolStr(c.DnsblEnabled)},
				{Key: "dnsbl_zones", Label: "DNSBL 区", Value: dnsblZones},
			},
		},
		{
			Key:   "storage",
			Label: "存储",
			Items: []SystemConfigItem{
				{Key: "db_path", Label: "数据库路径", Value: c.DBPath},
				{Key: "maildir_path", Label: "邮件目录", Value: c.MaildirPath},
				{Key: "attachment_path", Label: "附件目录", Value: c.AttachmentPath},
				{Key: "avatar_path", Label: "头像目录", Value: c.AvatarPath},
			},
		},
		{
			Key:   "observability",
			Label: "可观测性",
			Items: []SystemConfigItem{
				{Key: "log_level", Label: "日志级别", Value: c.LogLevel},
				{Key: "log_format", Label: "日志格式", Value: c.LogFormat},
				{Key: "metrics_enabled", Label: "Metrics 开关", Value: boolStr(c.MetricsEnabled)},
				{Key: "metrics_path", Label: "Metrics 路径", Value: c.MetricsPath},
				{Key: "tracing_enabled", Label: "链路追踪", Value: boolStr(c.TracingEnabled)},
				{Key: "cors_allowed_origins", Label: "CORS 白名单", Value: corsOrigins},
			},
		},
	}
}

// ResanitizeResult 重新净化结果统计。
type ResanitizeResult struct {
	Total   int `json:"total"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// ResanitizeAllMails 用当前净化策略对全量 body_html_raw 重新净化并更新 body_html（AC-7）。
//
// 设计：
//   - 单次查询拉取所有 body_html_raw 非空的邮件（仅 ID + BodyHTMLRaw，轻量）
//   - 逐条 sanitize.SanitizeHTML 后 UpdateBodyHTML（无事务，避免大事务锁表）
//   - 记录审计日志（ActorType=admin, Action=admin.maintenance.resanitize）
//
// 性能：邮件量 < 1000 时单次执行 < 1s；> 1000 时建议后续改为分批提交。
func (s *AdminService) ResanitizeAllMails(ctx context.Context) (*ResanitizeResult, error) {
	mails, err := s.msgDAO.ListAllWithRawHTML(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询待净化邮件失败: %w", err)
	}

	result := &ResanitizeResult{Total: len(mails)}
	for _, m := range mails {
		if m.BodyHTMLRaw == "" {
			result.Skipped++
			continue
		}
		sanitized := sanitize.SanitizeHTML(m.BodyHTMLRaw)
		if err := s.msgDAO.UpdateBodyHTML(ctx, m.ID, sanitized); err != nil {
			return result, fmt.Errorf("更新邮件 %d 的 body_html 失败: %w", m.ID, err)
		}
		result.Updated++
	}

	// 审计
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorAdmin,
			Action:       "admin.maintenance.resanitize",
			ResourceType: "maintenance",
			Result:       audit.ResultSuccess,
			Detail:       fmt.Sprintf(`{"total":%d,"updated":%d,"skipped":%d}`, result.Total, result.Updated, result.Skipped),
		})
	}
	return result, nil
}

// AuditLogItem 审计日志列表项（AC-6 响应）。
type AuditLogItem struct {
	ID           int64  `json:"id"`
	Timestamp    string `json:"timestamp"`
	ActorType    string `json:"actor_type"`
	ActorID      *int64 `json:"actor_id,omitempty"`
	ActorIP      string `json:"actor_ip,omitempty"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Result       string `json:"result"`
	Detail       string `json:"detail,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
}

// AuditLogListResult 审计日志列表响应（含分页信息）。
type AuditLogListResult struct {
	Items    []*AuditLogItem `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// ListAuditLogs 分页查询审计日志（AC-6）。
// filter 为过滤条件，page/pageSize 控制分页。
// auditDAO 未注入时返回 503 错误。
func (s *AdminService) ListAuditLogs(ctx context.Context, filter audit.AuditFilter, page, pageSize int) (*AuditLogListResult, error) {
	if s.auditDAO == nil {
		return nil, NewAuthError(http.StatusServiceUnavailable, "审计日志查询未启用")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	rows, total, err := s.auditDAO.List(ctx, filter, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("查询审计日志失败: %w", err)
	}

	items := make([]*AuditLogItem, 0, len(rows))
	for _, r := range rows {
		item := &AuditLogItem{
			ID:           r.ID,
			Timestamp:    r.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			ActorType:    r.ActorType,
			Action:       r.Action,
			Result:       r.Result,
			ResourceType: r.ResourceType.String,
			ResourceID:   r.ResourceID.String,
			Detail:       r.Detail.String,
			RequestID:    r.RequestID.String,
			ActorIP:      r.ActorIP.String,
		}
		if r.ActorID.Valid {
			v := r.ActorID.Int64
			item.ActorID = &v
		}
		items = append(items, item)
	}

	return &AuditLogListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ListAllMails 管理员邮件列表查询（AC-9）。
// 仅返回列表级元数据，不返回正文/HTML/邮件头（隐私保护）。
// filter 为过滤条件，page/pageSize 控制分页。
func (s *AdminService) ListAllMails(ctx context.Context, filter dao.AdminMailFilter, page, pageSize int) (*dao.AdminMailListResult, error) {
	return s.msgDAO.ListAllForAdmin(ctx, filter, page, pageSize)
}
