// Package service 实现业务逻辑层。
// auth_service.go 实现认证相关业务：注册、登录、改密、强制改密。
//
// 兼容性：所有错误消息与原 Node.js 后端完全一致（前端依赖关键字匹配）。
// 企业级增强：登录失败/成功审计、Prometheus 指标、事务保护。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/metrics"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// AuthError 是认证业务错误，携带 HTTP 状态码与错误消息。
// 与原 Node.js 响应格式完全一致：仅 error 字段。
type AuthError struct {
	Status  int
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

// NewAuthError 构造 AuthError。
func NewAuthError(status int, msg string) *AuthError {
	return &AuthError{Status: status, Message: msg}
}

// 常见错误（与原 Node.js 消息完全一致）
var (
	ErrUsernameRequired      = NewAuthError(http.StatusBadRequest, "用户名和密码不能为空")
	ErrUsernameFormat        = NewAuthError(http.StatusBadRequest, "用户名仅允许字母、数字、点、下划线，3-20字符")
	ErrPasswordTooShort      = NewAuthError(http.StatusBadRequest, "密码至少6位")
	ErrDisplayNameLength     = NewAuthError(http.StatusBadRequest, "显示名称长度1-50字符")
	ErrUsernameExists        = NewAuthError(http.StatusConflict, "用户名已存在")
	ErrEmailExists           = NewAuthError(http.StatusConflict, "邮箱已被注册")
	ErrEmailPasswordRequired = NewAuthError(http.StatusBadRequest, "邮箱和密码不能为空")
	ErrInvalidCredentials    = NewAuthError(http.StatusUnauthorized, "邮箱或密码错误")
	ErrAccountDisabled       = NewAuthError(http.StatusForbidden, "账号已被禁用")
	ErrCurrentPasswordWrong  = NewAuthError(http.StatusUnauthorized, "当前密码错误")
	ErrNewPasswordRequired   = NewAuthError(http.StatusBadRequest, "请填写新密码")
	ErrCurrentNewRequired    = NewAuthError(http.StatusBadRequest, "请填写当前密码和新密码")
)

// 登录失败锁定参数（与原 Node.js 一致）
const (
	MaxLoginFails      = 5
	LockDurationMinutes = 15
)

// usernameRegex 用户名格式校验（与原 Node.js 一致）。
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9._]{3,20}$`)

// AuthService 认证业务服务。
type AuthService struct {
	userDAO     *dao.UserDAO
	jwt         *crypto.JWTManager
	audit       *audit.Logger
	domain      string
	maildirPath string
}

// NewAuthService 创建认证服务。
func NewAuthService(userDAO *dao.UserDAO, jwtMgr *crypto.JWTManager, auditLogger *audit.Logger, domain, maildirPath string) *AuthService {
	return &AuthService{
		userDAO:     userDAO,
		jwt:         jwtMgr,
		audit:       auditLogger,
		domain:      domain,
		maildirPath: maildirPath,
	}
}

// RegisterResult 注册结果。
type RegisterResult struct {
	Token string
	User  *dao.User
}

// Register 注册新用户。
// 业务规则（与原 Node.js 完全一致）：
//  1. 校验 username/password 非空
//  2. 校验 username 格式（正则 ^[a-zA-Z0-9._]{3,20}$）
//  3. 校验 password 长度 >= 6
//  4. 校验 displayName 长度 1-50
//  5. 校验 username 唯一
//  6. 校验 email 唯一
//  7. email 由 username@domain 生成
//  8. 首个用户自动 admin
//  9. 创建 maildir 目录
func (s *AuthService) Register(ctx context.Context, username, password, displayName string) (*RegisterResult, error) {
	// 1. 非空校验
	if username == "" || password == "" {
		return nil, ErrUsernameRequired
	}
	// 2. username 格式
	if !usernameRegex.MatchString(username) {
		return nil, ErrUsernameFormat
	}
	// 3. password 长度
	if len(password) < 6 {
		return nil, ErrPasswordTooShort
	}
	// 4. displayName 长度（仅当传入时校验）
	if displayName != "" && (len(displayName) < 1 || len(displayName) > 50) {
		return nil, ErrDisplayNameLength
	}

	// 5. username 唯一
	existing, err := s.userDAO.FindByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("查询用户名失败: %w", err)
	}
	if existing != nil {
		return nil, ErrUsernameExists
	}

	// 6. email 生成与唯一校验
	// 邮箱标准化为小写（与 Login 的 SanitizeEmail 保持一致，避免大小写差异导致登录失败）
	email := SanitizeEmail(fmt.Sprintf("%s@%s", username, s.domain))
	existing, err = s.userDAO.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("查询邮箱失败: %w", err)
	}
	if existing != nil {
		return nil, ErrEmailExists
	}

	// 7. 密码哈希
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("密码哈希失败: %w", err)
	}

	// 8. 首个用户自动 admin
	count, err := s.userDAO.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计用户数失败: %w", err)
	}
	role := "user"
	if count == 0 {
		role = "admin"
	}

	// 9. 创建用户
	userID, err := s.userDAO.Create(ctx, dao.CreateUserInput{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
		Role:         role,
	})
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	// 10. 创建 maildir 目录（与原 Node.js 一致）
	s.createMaildir(username)

	// 11. 查询完整用户
	user, err := s.userDAO.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("查询新用户失败: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("新用户查询返回 nil")
	}

	// 12. 生成 JWT
	token, err := s.jwt.Generate(user.ID, user.Email, user.Role, false)
	if err != nil {
		return nil, fmt.Errorf("生成 JWT 失败: %w", err)
	}

	// 13. 审计日志
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &user.ID,
			Action:       "auth.register",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", user.ID),
			Result:       audit.ResultSuccess,
			Detail:       fmt.Sprintf(`{"username":"%s","email":"%s","role":"%s"}`, username, email, role),
		})
	}

	slog.Info("用户注册成功", "user_id", user.ID, "username", username, "role", role)
	return &RegisterResult{Token: token, User: user}, nil
}

// LoginResult 登录结果。
type LoginResult struct {
	Token                 string
	User                  *dao.User
	RequirePasswordChange bool
}

// Login 用户登录。
// 业务规则（与原 Node.js 完全一致）：
//  1. 校验 email/password 非空
//  2. 查询用户，不存在返回 401
//  3. is_active=0 返回 403
//  4. locked_until 在未来返回 423（含剩余分钟）
//  5. 密码错误：fails+1，达到 5 次锁定 15 分钟，返回 423；否则返回 401 含剩余次数
//  6. 密码正确：重置 fails
//  7. is_default_password=1 返回 200 + requirePasswordChange=true
//  8. 否则正常返回
func (s *AuthService) Login(ctx context.Context, email, password string, remember bool, clientIP string) (*LoginResult, error) {
	// 1. 非空校验
	if email == "" || password == "" {
		return nil, ErrEmailPasswordRequired
	}

	// 2. 查询用户
	user, err := s.userDAO.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	if user == nil {
		// 用户不存在，仍记审计 + 指标
		s.recordFailedLogin(ctx, email, clientIP, "user_not_found")
		return nil, ErrInvalidCredentials
	}

	// 3. 禁用检查
	if !user.IsActive {
		s.recordFailedLogin(ctx, email, clientIP, "disabled")
		return nil, ErrAccountDisabled
	}

	// 4. 锁定检查
	if user.LockedUntil.Valid && user.LockedUntil.Time.After(time.Now()) {
		remaining := int(math.Ceil(user.LockedUntil.Time.Sub(time.Now()).Minutes()))
		if remaining < 1 {
			remaining = 1
		}
		s.recordFailedLogin(ctx, email, clientIP, "locked")
		return nil, NewAuthError(http.StatusLocked, fmt.Sprintf("账号已锁定，请 %d 分钟后重试", remaining))
	}

	// 5. 密码校验
	if err := crypto.ComparePassword(user.PasswordHash, password); err != nil {
		// 密码错误
		fails := user.LoginFails + 1
		if fails >= MaxLoginFails {
			// 锁定 15 分钟
			until := time.Now().Add(LockDurationMinutes * time.Minute)
			untilStr := until.UTC().Format("2006-01-02 15:04:05")
			_ = s.userDAO.LockUser(ctx, user.ID, untilStr)
			_ = s.userDAO.UpdateLoginFails(ctx, user.ID, fails)
			s.recordFailedLogin(ctx, email, clientIP, "locked")
			metrics.AuthAttemptsTotal.WithLabelValues("login", "locked").Inc()
			return nil, NewAuthError(http.StatusLocked, "连续失败5次，账号已锁定15分钟")
		}
		_ = s.userDAO.UpdateLoginFails(ctx, user.ID, fails)
		s.recordFailedLogin(ctx, email, clientIP, "wrong_password")
		metrics.AuthAttemptsTotal.WithLabelValues("login", "failure").Inc()
		remaining := MaxLoginFails - fails
		return nil, NewAuthError(http.StatusUnauthorized, fmt.Sprintf("邮箱或密码错误（还剩%d次机会）", remaining))
	}

	// 6. 密码正确，重置失败计数
	if err := s.userDAO.ResetLoginFails(ctx, user.ID); err != nil {
		slog.Error("重置登录失败计数失败", "user_id", user.ID, "error", err)
	}

	// 7. 生成 JWT
	token, err := s.jwt.Generate(user.ID, user.Email, user.Role, remember)
	if err != nil {
		return nil, fmt.Errorf("生成 JWT 失败: %w", err)
	}

	// 8. 审计 + 指标
	metrics.AuthAttemptsTotal.WithLabelValues("login", "success").Inc()
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &user.ID,
			ActorIP:      clientIP,
			Action:       "auth.login",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", user.ID),
			Result:       audit.ResultSuccess,
			Detail:       fmt.Sprintf(`{"email":"%s","remember":%v}`, email, remember),
		})
	}

	slog.Info("用户登录成功", "user_id", user.ID, "email", email)
	return &LoginResult{
		Token:                 token,
		User:                  user,
		RequirePasswordChange: user.IsDefaultPassword,
	}, nil
}

// UpdateProfile 更新个人资料。
func (s *AuthService) UpdateProfile(ctx context.Context, userID int64, displayName, signature *string) error {
	in := dao.UpdateProfileInput{
		DisplayName: displayName,
		Signature:   signature,
	}
	if err := s.userDAO.UpdateProfile(ctx, userID, in); err != nil {
		return fmt.Errorf("更新资料失败: %w", err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &userID,
			Action:       "auth.update_profile",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", userID),
			Result:       audit.ResultSuccess,
		})
	}
	return nil
}

// ChangePassword 修改密码（需校验当前密码）。
// 注意：与原 Node.js 一致，此接口不清除 is_default_password 标志。
func (s *AuthService) ChangePassword(ctx context.Context, user *dao.User, currentPassword, newPassword string) error {
	if currentPassword == "" || newPassword == "" {
		return ErrCurrentNewRequired
	}
	if len(newPassword) < 6 {
		return ErrPasswordTooShort
	}
	if err := crypto.ComparePassword(user.PasswordHash, currentPassword); err != nil {
		return ErrCurrentPasswordWrong
	}
	hash, err := crypto.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}
	if err := s.userDAO.UpdatePassword(ctx, user.ID, hash); err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &user.ID,
			Action:       "auth.change_password",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", user.ID),
			Result:       audit.ResultSuccess,
		})
	}
	slog.Info("用户修改密码", "user_id", user.ID)
	return nil
}

// ChangeDefaultPassword 强制改密（不校验当前密码，清除 is_default_password 标志）。
func (s *AuthService) ChangeDefaultPassword(ctx context.Context, user *dao.User, newPassword string) error {
	if newPassword == "" {
		return ErrNewPasswordRequired
	}
	if len(newPassword) < 6 {
		return ErrPasswordTooShort
	}
	hash, err := crypto.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}
	if err := s.userDAO.UpdatePassword(ctx, user.ID, hash); err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}
	if err := s.userDAO.SetDefaultPassword(ctx, user.ID, false); err != nil {
		return fmt.Errorf("清除默认密码标志失败: %w", err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &user.ID,
			Action:       "auth.change_default_password",
			ResourceType: "user",
			ResourceID:   fmt.Sprintf("%d", user.ID),
			Result:       audit.ResultSuccess,
		})
	}
	slog.Info("用户强制改密", "user_id", user.ID)
	return nil
}

// recordFailedLogin 记录登录失败审计。
func (s *AuthService) recordFailedLogin(ctx context.Context, email, ip, reason string) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Entry{
		ActorType: audit.ActorUser,
		ActorIP:   ip,
		Action:    "auth.login",
		Result:    audit.ResultFailure,
		Detail:    fmt.Sprintf(`{"email":"%s","reason":"%s"}`, email, reason),
	})
}

// createMaildir 创建用户 maildir 目录结构。
// 与原 Node.js 一致：{maildirPath}/{domain}/{username}/{cur,new,tmp}
func (s *AuthService) createMaildir(username string) {
	if s.maildirPath == "" {
		return
	}
	domain := s.domain
	if domain == "" {
		domain = "localhost"
	}
	base := filepath.Join(s.maildirPath, domain, username)
	for _, sub := range []string{"cur", "new", "tmp"} {
		dir := filepath.Join(base, sub)
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Error("创建 maildir 失败", "dir", dir, "error", err)
			return
		}
	}
}

// SanitizeEmail 标准化邮箱（小写 + 去空格）。
// 用于登录时兼容大小写差异。
func SanitizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// IsAuthError 判断 error 是否为 *AuthError。
func IsAuthError(err error) (*AuthError, bool) {
	var ae *AuthError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
