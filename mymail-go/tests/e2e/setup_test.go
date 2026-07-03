// Package e2e 提供端到端集成测试。
//
// E2E 测试使用 httptest.NewServer 启动真实 HTTP 服务器，
// 通过真实 HTTP 客户端发送请求，验证完整业务流程。
// 与 handler 测试（httptest.NewRecorder）的区别：
//   - 真实 TCP 连接（验证连接处理、超时等）
//   - 支持 WebSocket（需要 ws:// 协议升级）
//   - 完整流程测试（多端点串联，验证业务流转）
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/httpapi"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
	"github.com/mymail/mymail-go/internal/ws"
)

// testEnv E2E 测试环境，包含真实 HTTP 服务器与全部依赖。
type testEnv struct {
	server      *httptest.Server
	db          *db.DB
	cfg         *config.Config
	userDAO     *dao.UserDAO
	msgDAO      *dao.MessageDAO
	jwtMgr      *crypto.JWTManager
	authSvc     *service.AuthService
	mailSvc     *service.MailService
	apiKeySvc   *service.APIKeyService
	adminSvc    *service.AdminService
	ruleSvc     *service.RuleService
	wsHub       *ws.Hub
	attachStore *attachment.Store
}

// newTestEnv 创建 E2E 测试环境：临时 DB + 全部服务 + httptest.Server。
func newTestEnv(t testing.TB) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 1. 临时数据库
	dbPath := filepath.Join(t.TempDir(), "e2e.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// 2. DAO 层
	userDAO := dao.NewUserDAO(database)
	msgDAO := dao.NewMessageDAO(database)
	attachDAO := dao.NewAttachmentDAO(database)
	sendLogDAO := dao.NewSendLogDAO(database)
	queueDAO := dao.NewMailQueueDAO(database)
	apiKeyDAO := dao.NewAPIKeyDAO(database)
	settingsDAO := dao.NewSettingsDAO(database)
	ruleDAO := dao.NewRuleDAO(database)

	// 3. JWT + 审计
	jwtMgr, err := crypto.NewJWTManager("e2e-test-secret-key-must-be-32-chars-min", "24h", "720h")
	if err != nil {
		t.Fatalf("创建 JWT 管理器失败: %v", err)
	}
	auditLogger, err := audit.NewLogger(database, false, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}
	t.Cleanup(func() { auditLogger.Close() })

	// 4. 配置
	maildirPath := filepath.Join(t.TempDir(), "maildir")
	attachPath := filepath.Join(t.TempDir(), "attachments")
	cfg := &config.Config{
		Env:                 "test",
		Domain:              "example.com",
		MaildirPath:         maildirPath,
		AttachmentPath:      attachPath,
		MaxAttachmentSize:   10 * 1024 * 1024,
		CORSAllowedOrigins:  []string{"http://localhost:5173"},
		MetricsEnabled:      false,
		SendRateLimitPerMin: 100,
		FeatureAPIKey:       true,
		FeatureRules:        true,
	}

	// 5. Service 层
	authSvc := service.NewAuthService(userDAO, jwtMgr, auditLogger, cfg.Domain, maildirPath)
	mailSvc := service.NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO,
		database, auditLogger, cfg.Domain, cfg.SendRateLimitPerMin,
	)
	apiKeySvc := service.NewAPIKeyService(apiKeyDAO, auditLogger, 10)
	adminSvc := service.NewAdminService(userDAO, settingsDAO, auditLogger)
	ruleSvc := service.NewRuleService(ruleDAO)

	// 6. 附件存储
	attachStore := attachment.New(cfg)

	// 7. WebSocket Hub
	wsHub := ws.NewHub()
	t.Cleanup(func() { wsHub.Shutdown() })

	// 8. 路由
	router := httpapi.NewRouter(httpapi.Deps{
		Cfg:           cfg,
		DB:            database,
		UserDAO:       userDAO,
		JWTManager:    jwtMgr,
		AuthService:   authSvc,
		MailService:   mailSvc,
		AttachStore:   attachStore,
		APIKeyService: apiKeySvc,
		AdminService:  adminSvc,
		RuleService:   ruleSvc,
		WSHub:         wsHub,
	})

	// 9. 真实 HTTP 服务器
	server := httptest.NewServer(router)
	t.Cleanup(func() { server.Close() })

	return &testEnv{
		server:      server,
		db:          database,
		cfg:         cfg,
		userDAO:     userDAO,
		msgDAO:      msgDAO,
		jwtMgr:      jwtMgr,
		authSvc:     authSvc,
		mailSvc:     mailSvc,
		apiKeySvc:   apiKeySvc,
		adminSvc:    adminSvc,
		ruleSvc:     ruleSvc,
		wsHub:       wsHub,
		attachStore: attachStore,
	}
}

// ===== HTTP 请求辅助 =====

// doJSON 发送 JSON 请求，返回状态码与解析后的响应体。
func (e *testEnv) doJSON(t testing.TB, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("编码 JSON 失败: %v", err)
		}
	}

	req, err := http.NewRequest(method, e.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP 请求失败: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &result)
	}
	return resp.StatusCode, result
}

// doJSONArr 发送 JSON 请求，返回状态码与解析后的数组响应体。
// 用于 ListKeys 等返回 JSON 数组的端点。
func (e *testEnv) doJSONArr(t testing.TB, method, path string, body any, token string) (int, []any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("编码 JSON 失败: %v", err)
		}
	}

	req, err := http.NewRequest(method, e.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP 请求失败: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var arr []any
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &arr)
	}
	return resp.StatusCode, arr
}

// doRaw 发送原始请求，返回 *http.Response（调用方负责关闭 Body）。
func (e *testEnv) doRaw(method, path string, body io.Reader, token string) (*http.Response, error) {
	req, err := http.NewRequest(method, e.server.URL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

// ===== 认证辅助 =====

// registerAndLogin 注册并登录，返回 token。第一个注册的用户自动成为 admin。
func (e *testEnv) registerAndLogin(t testing.TB, username, password string) string {
	t.Helper()
	// 注册
	status, resp := e.doJSON(t, "POST", "/api/auth/register",
		map[string]string{"username": username, "password": password}, "")
	if status != http.StatusCreated {
		t.Fatalf("注册失败: status=%d, resp=%v", status, resp)
	}

	// 登录
	status, resp = e.doJSON(t, "POST", "/api/auth/login",
		map[string]any{"email": username + "@example.com", "password": password, "remember": false}, "")
	if status != http.StatusOK {
		t.Fatalf("登录失败: status=%d, resp=%v", status, resp)
	}

	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatal("登录响应中无 token")
	}
	return token
}

// loginAdmin 注册首个用户（自动 admin）并登录，返回 admin token。
func (e *testEnv) loginAdmin(t testing.TB) string {
	t.Helper()
	return e.registerAndLogin(t, "admin", "AdminPass123!")
}

// loginExisting 登录已存在的用户，返回 token。
func (e *testEnv) loginExisting(t testing.TB, email, password string) string {
	t.Helper()
	status, resp := e.doJSON(t, "POST", "/api/auth/login",
		map[string]any{"email": email, "password": password, "remember": false}, "")
	if status != http.StatusOK {
		t.Fatalf("登录失败: status=%d, resp=%v", status, resp)
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatal("登录响应中无 token")
	}
	return token
}

// getMe 用 token 获取当前用户信息。
func (e *testEnv) getMe(t testing.TB, token string) map[string]any {
	t.Helper()
	status, resp := e.doJSON(t, "GET", "/api/auth/me", nil, token)
	if status != http.StatusOK {
		t.Fatalf("获取 me 失败: status=%d, resp=%v", status, resp)
	}
	return resp
}

// ===== JWT 辅助 =====

// makeTokenForUser 直接为指定用户生成 JWT（绕过登录，用于测试其他用户视角）。
func (e *testEnv) makeTokenForUser(userID int64, email, role string) string {
	token, _ := e.jwtMgr.Generate(userID, email, role, false)
	return token
}

// ===== 邮件辅助 =====

// insertTestMessage 直接通过 DAO 插入测试邮件（绕过 SMTP，用于 mail 流程测试）。
func (e *testEnv) insertTestMessage(t testing.TB, userID int64, subject, body string) int64 {
	t.Helper()
	ctx := context.Background()
	uid := int64(1)
	id, err := e.msgDAO.Create(ctx, dao.CreateMessageInput{
		UserID:    userID,
		Folder:    "INBOX",
		UID:       &uid,
		FromAddr:  "sender@example.com",
		ToAddr:    fmt.Sprintf("user%d@example.com", userID),
		Subject:   subject,
		BodyText:  body,
		SizeBytes: int64(len(body)),
	})
	if err != nil {
		t.Fatalf("插入测试邮件失败: %v", err)
	}
	return id
}
