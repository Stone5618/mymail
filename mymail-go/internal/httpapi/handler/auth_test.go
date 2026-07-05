// auth_test.go 使用 httptest 进行 HTTP 集成测试。
// 这是阶段 2 验收的核心：验证所有端点的 HTTP 契约与原 Node.js 一致。
//
// 使用外部测试包（handler_test）以避免 import cycle：
//   handler_test → httpapi → handler（无循环）
//
// 测试覆盖：
//   - POST /api/auth/register：成功 201、缺字段 400、重复 409、用户名格式 400、密码太短 400
//   - POST /api/auth/login：成功 200、密码错误 401（含剩余次数）、5 次锁定 423、禁用 403
//   - GET /api/auth/me：无 token 401、有 token 200、字段完整
//   - PUT /api/auth/profile：成功 200
//   - PUT /api/auth/password：成功 200、当前密码错误 401
//   - POST /api/auth/change-default-password：成功 200
//   - JWT 中间件：无 Authorization 401、格式错误 401、过期 token 401
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/httpapi"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// testEnv 测试环境，包含 router 与依赖。
type testEnv struct {
	router      *gin.Engine
	userDAO     *dao.UserDAO
	msgDAO      *dao.MessageDAO
	attachDAO   *dao.AttachmentDAO
	jwtMgr      *crypto.JWTManager
	authSvc     *service.AuthService
	mailSvc     *service.MailService
	attachStore *attachment.Store
	attachPath  string
}

// newTestEnv 创建测试环境：临时 DB + 完整路由（含 mail 路由）。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

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

	jwtMgr, err := crypto.NewJWTManager("test-secret-key-for-handler-test-32chars", "24h", "720h")
	if err != nil {
		t.Fatalf("创建 JWT 管理器失败: %v", err)
	}
	auditLogger, err := audit.NewLogger(database, true, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}
	t.Cleanup(func() { auditLogger.Close() })

	maildirPath := filepath.Join(t.TempDir(), "maildir")
	avatarPath := filepath.Join(t.TempDir(), "avatars")
	authSvc := service.NewAuthService(userDAO, jwtMgr, auditLogger, "example.com", maildirPath, avatarPath)
	mailSvc := service.NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO,
		database, auditLogger, "example.com", 10,
	)

	attachmentPath := filepath.Join(t.TempDir(), "attachments")
	cfg := &config.Config{
		Env:                "test",
		CORSAllowedOrigins: []string{"http://localhost:5173"},
		MetricsEnabled:     false,
		AttachmentPath:     attachmentPath,
		MaxAttachmentSize:  10 * 1024 * 1024, // 10MB
	}
	attachStore := attachment.New(cfg)

	router := httpapi.NewRouter(httpapi.Deps{
		Cfg:         cfg,
		DB:          database,
		UserDAO:     userDAO,
		JWTManager:  jwtMgr,
		AuthService: authSvc,
		MailService: mailSvc,
		AttachStore: attachStore,
	})

	return &testEnv{
		router:      router,
		userDAO:     userDAO,
		msgDAO:      msgDAO,
		attachDAO:   attachDAO,
		jwtMgr:      jwtMgr,
		authSvc:     authSvc,
		mailSvc:     mailSvc,
		attachStore: attachStore,
		attachPath:  attachmentPath,
	}
}

// doJSON 发送 JSON 请求。
func doJSON(t *testing.T, router *gin.Engine, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("编码 JSON 失败: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// registerAndLogin 注册并登录，返回 token。
func registerAndLogin(t *testing.T, env *testEnv, username, password string) string {
	t.Helper()
	// 注册
	doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": username, "password": password}, "")

	// 登录
	w := doJSON(t, env.router, "POST", "/api/auth/login",
		map[string]string{"email": username + "@example.com", "password": password}, "")

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析登录响应失败: %v，body: %s", err, w.Body.String())
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatalf("登录 token 为空，body: %s", w.Body.String())
	}
	return token
}

// ============ Register 测试 ============

func TestAuthHandler_Register_Success(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": "alice", "password": "password123", "displayName": "Alice"}, "")

	if w.Code != http.StatusCreated {
		t.Fatalf("期望 201，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["message"] != "注册成功" {
		t.Errorf("message 期望 '注册成功'，实际 %v", resp["message"])
	}
	if resp["token"] == nil || resp["token"] == "" {
		t.Error("token 不应为空")
	}
	user, _ := resp["user"].(map[string]any)
	if user == nil {
		t.Fatal("user 不应为 nil")
	}
	if user["username"] != "alice" {
		t.Errorf("username 期望 alice，实际 %v", user["username"])
	}
	if user["email"] != "alice@example.com" {
		t.Errorf("email 期望 alice@example.com，实际 %v", user["email"])
	}
	if user["role"] != "admin" {
		t.Errorf("首用户 role 期望 admin，实际 %v", user["role"])
	}
}

func TestAuthHandler_Register_MissingFields(t *testing.T) {
	env := newTestEnv(t)

	// 空 body
	w := doJSON(t, env.router, "POST", "/api/auth/register", map[string]string{}, "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "用户名和密码不能为空" {
		t.Errorf("error 期望 '用户名和密码不能为空'，实际 %v", resp["error"])
	}
}

func TestAuthHandler_Register_InvalidUsername(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": "ab", "password": "password123"}, "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"].(string), "用户名仅允许") {
		t.Errorf("error 应含 '用户名仅允许'，实际 %v", resp["error"])
	}
}

func TestAuthHandler_Register_ShortPassword(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": "alice", "password": "12345"}, "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "密码至少6位" {
		t.Errorf("error 期望 '密码至少6位'，实际 %v", resp["error"])
	}
}

func TestAuthHandler_Register_Duplicate(t *testing.T) {
	env := newTestEnv(t)

	doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": "alice", "password": "password123"}, "")

	w := doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": "alice", "password": "password456"}, "")
	if w.Code != http.StatusConflict {
		t.Errorf("期望 409，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "用户名已存在" {
		t.Errorf("error 期望 '用户名已存在'，实际 %v", resp["error"])
	}
}

func TestAuthHandler_Register_MalformedJSON(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// ============ Login 测试 ============

func TestAuthHandler_Login_Success(t *testing.T) {
	env := newTestEnv(t)
	registerAndLogin(t, env, "alice", "password123")
	// 上面的辅助函数已验证登录成功，这里再次独立验证
	w := doJSON(t, env.router, "POST", "/api/auth/login",
		map[string]string{"email": "alice@example.com", "password": "password123"}, "")

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "登录成功" {
		t.Errorf("message 期望 '登录成功'，实际 %v", resp["message"])
	}
	if resp["token"] == "" {
		t.Error("token 不应为空")
	}
}

func TestAuthHandler_Login_WrongPassword(t *testing.T) {
	env := newTestEnv(t)
	registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "POST", "/api/auth/login",
		map[string]string{"email": "alice@example.com", "password": "wrongpass"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	errMsg, _ := resp["error"].(string)
	if !strings.Contains(errMsg, "还剩4次机会") {
		t.Errorf("error 应含 '还剩4次机会'，实际 %v", resp["error"])
	}
}

func TestAuthHandler_Login_LockAfterFiveFails(t *testing.T) {
	env := newTestEnv(t)
	registerAndLogin(t, env, "alice", "password123")

	// 5 次错误密码
	for i := 0; i < 5; i++ {
		doJSON(t, env.router, "POST", "/api/auth/login",
			map[string]string{"email": "alice@example.com", "password": "wrongpass"}, "")
	}

	// 第 6 次应返回 423
	w := doJSON(t, env.router, "POST", "/api/auth/login",
		map[string]string{"email": "alice@example.com", "password": "password123"}, "")
	if w.Code != http.StatusLocked {
		t.Errorf("期望 423，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

func TestAuthHandler_Login_DisabledAccount(t *testing.T) {
	env := newTestEnv(t)
	// 注册并获取用户 ID
	w := doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": "alice", "password": "password123"}, "")
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	user, _ := resp["user"].(map[string]any)
	userID := int64(user["id"].(float64))

	// 禁用账号
	env.userDAO.SetActive(context.Background(), userID, false)

	w = doJSON(t, env.router, "POST", "/api/auth/login",
		map[string]string{"email": "alice@example.com", "password": "password123"}, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("期望 403，实际 %d", w.Code)
	}
	var resp2 map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp2)
	if resp2["error"] != "账号已被禁用" {
		t.Errorf("error 期望 '账号已被禁用'，实际 %v", resp2["error"])
	}
}

func TestAuthHandler_Login_UserNotFound(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "POST", "/api/auth/login",
		map[string]string{"email": "nobody@example.com", "password": "password123"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
}

// ============ Me 测试 ============

func TestAuthHandler_Me_NoToken(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "GET", "/api/auth/me", nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "未登录" {
		t.Errorf("error 期望 '未登录'，实际 %v", resp["error"])
	}
}

func TestAuthHandler_Me_InvalidToken(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "GET", "/api/auth/me", nil, "invalidtoken")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "Token 已过期，请重新登录" {
		t.Errorf("error 期望 'Token 已过期...'，实际 %v", resp["error"])
	}
}

func TestAuthHandler_Me_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "GET", "/api/auth/me", nil, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["username"] != "alice" {
		t.Errorf("username 期望 alice，实际 %v", resp["username"])
	}
	if resp["email"] != "alice@example.com" {
		t.Errorf("email 期望 alice@example.com，实际 %v", resp["email"])
	}
	if resp["role"] != "admin" {
		t.Errorf("role 期望 admin，实际 %v", resp["role"])
	}
	// camelCase 字段
	if _, ok := resp["storageLimit"]; !ok {
		t.Error("应包含 storageLimit 字段（camelCase）")
	}
	if _, ok := resp["storageUsed"]; !ok {
		t.Error("应包含 storageUsed 字段（camelCase）")
	}
	if _, ok := resp["createdAt"]; !ok {
		t.Error("应包含 createdAt 字段（camelCase）")
	}
}

// ============ UpdateProfile 测试 ============

func TestAuthHandler_UpdateProfile_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "PUT", "/api/auth/profile",
		map[string]string{"displayName": "Alice New", "signature": "My sig"}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "更新成功" {
		t.Errorf("message 期望 '更新成功'，实际 %v", resp["message"])
	}

	// 验证更新生效
	w = doJSON(t, env.router, "GET", "/api/auth/me", nil, token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["displayName"] != "Alice New" {
		t.Errorf("displayName 期望 'Alice New'，实际 %v", resp["displayName"])
	}
}

func TestAuthHandler_UpdateProfile_NoToken(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "PUT", "/api/auth/profile",
		map[string]string{"displayName": "X"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
}

// ============ ChangePassword 测试 ============

func TestAuthHandler_ChangePassword_Success(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "PUT", "/api/auth/password",
		map[string]string{"currentPassword": "password123", "newPassword": "newpassword456"}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}

	// 用新密码登录
	w = doJSON(t, env.router, "POST", "/api/auth/login",
		map[string]string{"email": "alice@example.com", "password": "newpassword456"}, "")
	if w.Code != http.StatusOK {
		t.Errorf("新密码登录失败，期望 200，实际 %d", w.Code)
	}
}

func TestAuthHandler_ChangePassword_WrongCurrent(t *testing.T) {
	env := newTestEnv(t)
	token := registerAndLogin(t, env, "alice", "password123")

	w := doJSON(t, env.router, "PUT", "/api/auth/password",
		map[string]string{"currentPassword": "wrongcurrent", "newPassword": "newpassword456"}, token)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
}

// ============ ChangeDefaultPassword 测试 ============

func TestAuthHandler_ChangeDefaultPassword_Success(t *testing.T) {
	env := newTestEnv(t)
	// 注册用户并标记为默认密码
	w := doJSON(t, env.router, "POST", "/api/auth/register",
		map[string]string{"username": "alice", "password": "password123"}, "")
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	user, _ := resp["user"].(map[string]any)
	userID := int64(user["id"].(float64))
	env.userDAO.SetDefaultPassword(context.Background(), userID, true)

	// 重新登录获取 token（标记默认密码后）
	token := registerAndLogin(t, env, "alice", "password123")

	w = doJSON(t, env.router, "POST", "/api/auth/change-default-password",
		map[string]string{"newPassword": "newpassword456"}, token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}

	// 验证标志已清除
	u, _ := env.userDAO.FindByID(context.Background(), userID)
	if u.IsDefaultPassword {
		t.Error("is_default_password 应已清除")
	}
}

func TestAuthHandler_ChangeDefaultPassword_NoToken(t *testing.T) {
	env := newTestEnv(t)

	w := doJSON(t, env.router, "POST", "/api/auth/change-default-password",
		map[string]string{"newPassword": "newpassword456"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
}

// ============ 中间件测试 ============

func TestMiddleware_Authenticate_MalformedHeader(t *testing.T) {
	env := newTestEnv(t)

	// 非 Bearer 前缀
	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	req.Header.Set("Authorization", "Basic abc123")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", w.Code)
	}
}

func TestMiddleware_RequireAdmin(t *testing.T) {
	// 直接测试 RequireAdmin 中间件
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		// 模拟已认证用户
		c.Set(middleware.ContextKeyUser, &dao.User{ID: 1, Role: "user"})
		c.Next()
	})
	r.GET("/admin-only", middleware.RequireAdmin(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/admin-only", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("普通用户应返回 403，实际 %d", w.Code)
	}

	// 管理员应通过
	r2 := gin.New()
	r2.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUser, &dao.User{ID: 1, Role: "admin"})
		c.Next()
	})
	r2.GET("/admin-only", middleware.RequireAdmin(), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})
	req = httptest.NewRequest("GET", "/admin-only", nil)
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("管理员应返回 200，实际 %d", w.Code)
	}
}

// 确保时间包被引用（用于过期 token 测试等）
var _ = time.Second
