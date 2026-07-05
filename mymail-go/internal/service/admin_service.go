// Package service 实现业务逻辑层。
// admin_service.go 实现管理员业务：用户管理、统计、全局设置。
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
	"strings"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// AdminService 管理员业务层。
type AdminService struct {
	userDAO     *dao.UserDAO
	settingsDAO *dao.SettingsDAO
	msgDAO      *dao.MessageDAO
	audit       *audit.Logger
	domain      string
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
func NewAdminService(userDAO *dao.UserDAO, settingsDAO *dao.SettingsDAO, msgDAO *dao.MessageDAO, auditLogger *audit.Logger, domain string) *AdminService {
	return &AdminService{
		userDAO:     userDAO,
		settingsDAO: settingsDAO,
		msgDAO:      msgDAO,
		audit:       auditLogger,
		domain:      domain,
	}
}

// DnsStatus DNS 记录检测结果。
type DnsStatus struct {
	MX    string
	SPF   string
	DMARC string
}

// CheckDns 检测当前域名 MX/SPF/DMARC 记录配置。
func (s *AdminService) CheckDns(ctx context.Context) (*DnsStatus, error) {
	domain := s.domain
	if domain == "" {
		return nil, fmt.Errorf("未配置域名")
	}

	status := &DnsStatus{MX: "missing", SPF: "missing", DMARC: "missing"}

	// MX 记录
	if records, err := net.LookupMX(domain); err == nil && len(records) > 0 {
		status.MX = "ok"
	}

	// SPF 记录
	if records, err := net.LookupTXT(domain); err == nil {
		for _, r := range records {
			if strings.HasPrefix(r, "v=spf1") {
				status.SPF = "ok"
				break
			}
		}
	}

	// DMARC 记录
	if records, err := net.LookupTXT("_dmarc." + domain); err == nil {
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

// GetSettings 查询所有全局设置。
func (s *AdminService) GetSettings(ctx context.Context) ([]*dao.Setting, error) {
	return s.settingsDAO.GetAll(ctx)
}

// UpdateSettings 批量更新全局设置。
func (s *AdminService) UpdateSettings(ctx context.Context, settings map[string]string) error {
	for k, v := range settings {
		if err := s.settingsDAO.Set(ctx, k, v); err != nil {
			return fmt.Errorf("更新设置 %s 失败: %w", k, err)
		}
	}

	// 审计
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorAdmin,
			Action:       "admin.settings.update",
			ResourceType: "settings",
			Result:       audit.ResultSuccess,
		})
	}
	return nil
}
