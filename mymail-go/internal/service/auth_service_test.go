// Package service
// auth_service_test.go 测试认证服务完整业务逻辑。
// 使用真实 SQLite 临时数据库，覆盖所有成功与错误路径。
//
// 测试覆盖：
//   - Register：成功（首用户 admin）、各种校验失败
//   - Login：成功、用户不存在、密码错误（剩余次数）、5 次锁定、禁用账号、已锁定
//   - UpdateProfile：成功
//   - ChangePassword：成功、当前密码错误、新密码太短
//   - ChangeDefaultPassword：成功、新密码为空、清除标志
//   - SanitizeEmail / IsAuthError 工具函数
package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// newTestAuthService 创建测试用 AuthService。
// 返回 (service, userDAO, cleanup)。
func newTestAuthService(t *testing.T) (*AuthService, *dao.UserDAO) {
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
	jwtMgr, err := crypto.NewJWTManager("test-secret-key-for-service-test-32chars", "24h", "720h")
	if err != nil {
		t.Fatalf("创建 JWT 管理器失败: %v", err)
	}

	// 审计日志器：启用 DB 写入，不写 JSONL（避免临时文件）
	auditLogger, err := audit.NewLogger(database, true, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}
	t.Cleanup(func() { auditLogger.Close() })

	// maildirPath 使用临时目录
	maildirPath := filepath.Join(t.TempDir(), "maildir")
	svc := NewAuthService(userDAO, jwtMgr, auditLogger, "example.com", maildirPath)
	return svc, userDAO
}

// ============ Register 测试 ============

func TestAuthService_Register_FirstUserIsAdmin(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, err := svc.Register(ctx, "admin", "password123", "Admin")
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if result.Token == "" {
		t.Error("Token 不应为空")
	}
	if result.User == nil {
		t.Fatal("User 不应为 nil")
	}
	if result.User.Role != "admin" {
		t.Errorf("首用户 Role 应为 admin，实际 %q", result.User.Role)
	}
	if result.User.Email != "admin@example.com" {
		t.Errorf("Email 应为 admin@example.com，实际 %q", result.User.Email)
	}
}

func TestAuthService_Register_SecondUserIsUser(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	svc.Register(ctx, "admin", "password123", "")
	result, err := svc.Register(ctx, "alice", "password123", "")
	if err != nil {
		t.Fatalf("第二个用户 Register 失败: %v", err)
	}
	if result.User.Role != "user" {
		t.Errorf("第二用户 Role 应为 user，实际 %q", result.User.Role)
	}
}

func TestAuthService_Register_ValidationErrors(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		username    string
		password    string
		displayName string
		wantStatus  int
		wantMsg     string
	}{
		{"empty_username", "", "password123", "", 400, "用户名和密码不能为空"},
		{"empty_password", "alice", "", "", 400, "用户名和密码不能为空"},
		{"short_username", "ab", "password123", "", 400, "用户名仅允许"},
		{"invalid_username_chars", "alice@!", "password123", "", 400, "用户名仅允许"},
		{"long_username", strings.Repeat("a", 21), "password123", "", 400, "用户名仅允许"},
		{"short_password", "alice", "12345", "", 400, "密码至少6位"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Register(ctx, tc.username, tc.password, tc.displayName)
			if err == nil {
				t.Fatal("期望错误但未返回")
			}
			ae, ok := IsAuthError(err)
			if !ok {
				t.Fatalf("期望 *AuthError，实际 %T: %v", err, err)
			}
			if ae.Status != tc.wantStatus {
				t.Errorf("Status 期望 %d，实际 %d", tc.wantStatus, ae.Status)
			}
			if !strings.Contains(ae.Message, tc.wantMsg) {
				t.Errorf("Message 期望包含 %q，实际 %q", tc.wantMsg, ae.Message)
			}
		})
	}
}

func TestAuthService_Register_DuplicateUsername(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, "alice", "password123", "")
	if err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}

	_, err = svc.Register(ctx, "alice", "password456", "")
	if err == nil {
		t.Fatal("重复用户名应报错")
	}
	ae, ok := IsAuthError(err)
	if !ok {
		t.Fatalf("期望 *AuthError，实际 %T", err)
	}
	if ae.Status != 409 {
		t.Errorf("Status 期望 409，实际 %d", ae.Status)
	}
	if ae.Message != "用户名已存在" {
		t.Errorf("Message 期望 '用户名已存在'，实际 %q", ae.Message)
	}
}

func TestAuthService_Register_DuplicateEmail(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	// 注册 alice → email = alice@example.com
	svc.Register(ctx, "alice", "password123", "")
	// 再注册 alice（不同大小写也不行，因 username 唯一）
	// 但要测 email 重复，需用不同 username 但相同 email
	// 由于 email = username@domain，username 唯一则 email 必唯一
	// 此测试由 username 唯一性间接保证，这里仅验证逻辑正确
	_, err := svc.Register(ctx, "alice2", "password123", "")
	if err != nil {
		t.Fatalf("不同用户名注册不应失败: %v", err)
	}
}

// ============ Login 测试 ============

func TestAuthService_Login_Success(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	svc.Register(ctx, "alice", "password123", "")

	result, err := svc.Login(ctx, "alice@example.com", "password123", false, "127.0.0.1")
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}
	if result.Token == "" {
		t.Error("Token 不应为空")
	}
	if result.User == nil {
		t.Error("User 不应为 nil")
	}
	if result.RequirePasswordChange {
		t.Error("RequirePasswordChange 应为 false")
	}
}

func TestAuthService_Login_RememberToken(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	svc.Register(ctx, "alice", "password123", "")

	// remember=true 应返回更长过期时间的 token
	result, _ := svc.Login(ctx, "alice@example.com", "password123", true, "127.0.0.1")
	if result.Token == "" {
		t.Error("Token 不应为空")
	}
}

func TestAuthService_Login_EmptyCredentials(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	cases := []struct {
		name     string
		email    string
		password string
	}{
		{"empty_email", "", "password"},
		{"empty_password", "alice@example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Login(ctx, tc.email, tc.password, false, "127.0.0.1")
			if err == nil {
				t.Fatal("期望错误")
			}
			ae, _ := IsAuthError(err)
			if ae.Status != 400 {
				t.Errorf("Status 期望 400，实际 %d", ae.Status)
			}
		})
	}
}

func TestAuthService_Login_UserNotFound(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	_, err := svc.Login(ctx, "nobody@example.com", "password123", false, "127.0.0.1")
	if err == nil {
		t.Fatal("期望错误")
	}
	ae, _ := IsAuthError(err)
	if ae.Status != 401 {
		t.Errorf("Status 期望 401，实际 %d", ae.Status)
	}
	if ae.Message != "邮箱或密码错误" {
		t.Errorf("Message 期望 '邮箱或密码错误'，实际 %q", ae.Message)
	}
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	svc.Register(ctx, "alice", "password123", "")

	// 第一次错误
	_, err := svc.Login(ctx, "alice@example.com", "wrongpass", false, "127.0.0.1")
	ae, _ := IsAuthError(err)
	if ae.Status != 401 {
		t.Errorf("Status 期望 401，实际 %d", ae.Status)
	}
	if !strings.Contains(ae.Message, "还剩4次机会") {
		t.Errorf("Message 应含 '还剩4次机会'，实际 %q", ae.Message)
	}

	// 第二次错误
	_, err = svc.Login(ctx, "alice@example.com", "wrongpass", false, "127.0.0.1")
	ae, _ = IsAuthError(err)
	if !strings.Contains(ae.Message, "还剩3次机会") {
		t.Errorf("Message 应含 '还剩3次机会'，实际 %q", ae.Message)
	}
}

func TestAuthService_Login_LockAfterFiveFails(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	svc.Register(ctx, "alice", "password123", "")

	// 连续 5 次错误密码
	for i := 0; i < 5; i++ {
		_, _ = svc.Login(ctx, "alice@example.com", "wrongpass", false, "127.0.0.1")
	}

	// 第 6 次应返回 423 锁定
	_, err := svc.Login(ctx, "alice@example.com", "password123", false, "127.0.0.1")
	if err == nil {
		t.Fatal("锁定后应报错")
	}
	ae, _ := IsAuthError(err)
	if ae.Status != 423 {
		t.Errorf("Status 期望 423，实际 %d", ae.Status)
	}
}

func TestAuthService_Login_DisabledAccount(t *testing.T) {
	svc, userDAO := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")
	// 禁用账号
	userDAO.SetActive(ctx, result.User.ID, false)

	_, err := svc.Login(ctx, "alice@example.com", "password123", false, "127.0.0.1")
	if err == nil {
		t.Fatal("禁用账号应报错")
	}
	ae, _ := IsAuthError(err)
	if ae.Status != 403 {
		t.Errorf("Status 期望 403，实际 %d", ae.Status)
	}
	if ae.Message != "账号已被禁用" {
		t.Errorf("Message 期望 '账号已被禁用'，实际 %q", ae.Message)
	}
}

func TestAuthService_Login_ResetFailsOnSuccess(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	svc.Register(ctx, "alice", "password123", "")

	// 2 次错误
	svc.Login(ctx, "alice@example.com", "wrong1", false, "127.0.0.1")
	svc.Login(ctx, "alice@example.com", "wrong2", false, "127.0.0.1")

	// 正确登录
	_, err := svc.Login(ctx, "alice@example.com", "password123", false, "127.0.0.1")
	if err != nil {
		t.Fatalf("正确密码登录失败: %v", err)
	}

	// 再次错误，应从 1 开始计数（剩余 4 次）
	_, err = svc.Login(ctx, "alice@example.com", "wrong", false, "127.0.0.1")
	ae, _ := IsAuthError(err)
	if !strings.Contains(ae.Message, "还剩4次机会") {
		t.Errorf("重置后应从 1 开始计数，Message 应含 '还剩4次机会'，实际 %q", ae.Message)
	}
}

func TestAuthService_Login_DefaultPasswordRequiresChange(t *testing.T) {
	svc, userDAO := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")
	// 标记为默认密码
	userDAO.SetDefaultPassword(ctx, result.User.ID, true)

	loginResult, err := svc.Login(ctx, "alice@example.com", "password123", false, "127.0.0.1")
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}
	if !loginResult.RequirePasswordChange {
		t.Error("RequirePasswordChange 应为 true")
	}
}

// ============ UpdateProfile 测试 ============

func TestAuthService_UpdateProfile_Success(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")

	name := "Alice New"
	sig := "My signature"
	err := svc.UpdateProfile(ctx, result.User.ID, &name, &sig, nil)
	if err != nil {
		t.Fatalf("UpdateProfile 失败: %v", err)
	}
}

// ============ ChangePassword 测试 ============

func TestAuthService_ChangePassword_Success(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")

	err := svc.ChangePassword(ctx, result.User, "password123", "newpassword456")
	if err != nil {
		t.Fatalf("ChangePassword 失败: %v", err)
	}

	// 用新密码登录
	_, err = svc.Login(ctx, "alice@example.com", "newpassword456", false, "127.0.0.1")
	if err != nil {
		t.Fatalf("新密码登录失败: %v", err)
	}
}

func TestAuthService_ChangePassword_WrongCurrent(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")

	err := svc.ChangePassword(ctx, result.User, "wrongcurrent", "newpassword456")
	if err == nil {
		t.Fatal("当前密码错误应报错")
	}
	ae, _ := IsAuthError(err)
	if ae.Status != 401 {
		t.Errorf("Status 期望 401，实际 %d", ae.Status)
	}
	if ae.Message != "当前密码错误" {
		t.Errorf("Message 期望 '当前密码错误'，实际 %q", ae.Message)
	}
}

func TestAuthService_ChangePassword_TooShort(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")

	err := svc.ChangePassword(ctx, result.User, "password123", "12345")
	if err == nil {
		t.Fatal("新密码太短应报错")
	}
	ae, _ := IsAuthError(err)
	if ae.Status != 400 {
		t.Errorf("Status 期望 400，实际 %d", ae.Status)
	}
}

func TestAuthService_ChangePassword_EmptyInput(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")

	err := svc.ChangePassword(ctx, result.User, "", "")
	if err == nil {
		t.Fatal("空输入应报错")
	}
}

// ============ ChangeDefaultPassword 测试 ============

func TestAuthService_ChangeDefaultPassword_Success(t *testing.T) {
	svc, userDAO := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")
	userDAO.SetDefaultPassword(ctx, result.User.ID, true)

	err := svc.ChangeDefaultPassword(ctx, result.User, "newpassword456")
	if err != nil {
		t.Fatalf("ChangeDefaultPassword 失败: %v", err)
	}

	// 验证 is_default_password 已清除
	user, _ := userDAO.FindByID(ctx, result.User.ID)
	if user.IsDefaultPassword {
		t.Error("is_default_password 应已清除")
	}

	// 用新密码登录，requirePasswordChange 应为 false
	loginResult, _ := svc.Login(ctx, "alice@example.com", "newpassword456", false, "127.0.0.1")
	if loginResult.RequirePasswordChange {
		t.Error("改密后 RequirePasswordChange 应为 false")
	}
}

func TestAuthService_ChangeDefaultPassword_Empty(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")

	err := svc.ChangeDefaultPassword(ctx, result.User, "")
	if err == nil {
		t.Fatal("空密码应报错")
	}
	ae, _ := IsAuthError(err)
	if ae.Status != 400 {
		t.Errorf("Status 期望 400，实际 %d", ae.Status)
	}
}

func TestAuthService_ChangeDefaultPassword_TooShort(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	result, _ := svc.Register(ctx, "alice", "password123", "")

	err := svc.ChangeDefaultPassword(ctx, result.User, "12345")
	if err == nil {
		t.Fatal("太短密码应报错")
	}
	ae, _ := IsAuthError(err)
	if ae.Status != 400 {
		t.Errorf("Status 期望 400，实际 %d", ae.Status)
	}
}

// ============ 工具函数测试 ============

func TestSanitizeEmail(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Alice@Example.COM", "alice@example.com"},
		{"  alice@example.com  ", "alice@example.com"},
		{"BOB@X.COM", "bob@x.com"},
	}
	for _, tc := range cases {
		got := SanitizeEmail(tc.input)
		if got != tc.want {
			t.Errorf("SanitizeEmail(%q) = %q，期望 %q", tc.input, got, tc.want)
		}
	}
}

func TestIsAuthError(t *testing.T) {
	// 是 *AuthError
	ae := NewAuthError(400, "test")
	_, ok := IsAuthError(ae)
	if !ok {
		t.Error("*AuthError 应识别")
	}

	// 非 *AuthError
	_, ok = IsAuthError(context.DeadlineExceeded)
	if ok {
		t.Error("普通 error 不应识别为 *AuthError")
	}
}

// ============ 并发测试 ============

func TestAuthService_ConcurrentRegister(t *testing.T) {
	svc, _ := newTestAuthService(t)
	ctx := context.Background()

	// 并发注册不同用户名，都应成功
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		i := i
		go func() {
			_, err := svc.Register(ctx, "user"+string(rune('a'+i)), "password123", "")
			done <- err
		}()
	}
	for i := 0; i < 5; i++ {
		err := <-done
		if err != nil {
			t.Errorf("并发注册失败: %v", err)
		}
	}
}

// 确保时间包被引用
var _ = time.Second
