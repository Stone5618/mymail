// admin_test.go 测试 AdminHandler + APIKeyHandler。
//
// 使用内部测试包（package handler）以访问 NewAdminHandler/NewAPIKeyHandler 等。
// 现有 auth_test.go 等为外部测试包（handler_test），两者可共存无冲突。
//
// 测试方法：gin 路由 + httptest.NewRecorder。
//   - AdminHandler 测试不挂认证中间件（聚焦 handler 逻辑，RequireAdmin 由 router 配置）
//   - APIKeyHandler 测试挂一个注入 currentUser 的中间件（模拟 Authenticate）
//
// 测试覆盖：
//   AdminHandler：Stats / ListUsers / GetUser(存在/不存在/无效ID) / CreateUser /
//                 UpdateUser / DeleteUser(软删除) / GetSettings(空/有) / UpdateSettings
//   APIKeyHandler：CreateKey(明文) / ListKeys / DeleteKey(成功/不存在)
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// ============ 测试辅助 ============

// newTestDB 创建临时 SQLite 数据库并执行迁移。
func newTestDB(t *testing.T) *db.DB {
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
	return database
}

// newTestAuditLogger 创建审计日志器（disabled 模式，no-op）。
func newTestAuditLogger(t *testing.T, database *db.DB) *audit.Logger {
	t.Helper()
	logger, err := audit.NewLogger(database, false, "")
	if err != nil {
		t.Fatalf("创建审计日志器失败: %v", err)
	}
	return logger
}

// setupAdminTestRouter 创建 AdminHandler 测试路由。
// 返回路由、handler、service、DB（DB 可用于直接造数据）。
func setupAdminTestRouter(t *testing.T) (*gin.Engine, *AdminHandler, *service.AdminService, *db.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database := newTestDB(t)
	userDAO := dao.NewUserDAO(database)
	settingsDAO := dao.NewSettingsDAO(database)
	msgDAO := dao.NewMessageDAO(database)
	adminSvc := service.NewAdminService(userDAO, settingsDAO, msgDAO, newTestAuditLogger(t, database), "example.com")
	adminHandler := NewAdminHandler(adminSvc)

	r := gin.New()
	g := r.Group("/api/admin")
	g.GET("/stats", adminHandler.Stats)
	g.GET("/users", adminHandler.ListUsers)
	g.GET("/users/:id", adminHandler.GetUser)
	g.POST("/users", adminHandler.CreateUser)
	g.PUT("/users/:id", adminHandler.UpdateUser)
	g.DELETE("/users/:id", adminHandler.DeleteUser)
	g.GET("/settings", adminHandler.GetSettings)
	g.PUT("/settings", adminHandler.UpdateSettings)

	return r, adminHandler, adminSvc, database
}

// setupAPIKeyTestRouter 创建 APIKeyHandler 测试路由。
// database 须已创建好测试用户（userID 对应的记录），handler 与测试共用同一 DB。
// userID 为注入的当前用户 ID（模拟 Authenticate 中间件）。
func setupAPIKeyTestRouter(t *testing.T, database *db.DB, userID int64) (*gin.Engine, *APIKeyHandler, *service.APIKeyService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	apiKeyDAO := dao.NewAPIKeyDAO(database)
	apiKeySvc := service.NewAPIKeyService(apiKeyDAO, newTestAuditLogger(t, database), 10)
	apiKeyHandler := NewAPIKeyHandler(apiKeySvc)

	r := gin.New()
	// 注入当前用户（模拟 Authenticate 中间件设置 context user）
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUser, &dao.User{ID: userID, Role: "user", IsActive: true})
		c.Next()
	})
	g := r.Group("/api/auth/api-keys")
	g.POST("", apiKeyHandler.CreateKey)
	g.GET("", apiKeyHandler.ListKeys)
	g.DELETE("/:id", apiKeyHandler.DeleteKey)

	return r, apiKeyHandler, apiKeySvc
}

// doReq 发送 JSON 请求，返回 ResponseRecorder。
func doReq(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("编码 JSON 失败: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// createTestUser 直接通过 DAO 创建测试用户，返回 user ID。
func createTestUser(t *testing.T, database *db.DB, username string) int64 {
	t.Helper()
	id, err := dao.NewUserDAO(database).Create(context.Background(), dao.CreateUserInput{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return id
}

// parseJSON 解析响应体为 map。
func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("解析 JSON 失败: %v, body: %s", err, w.Body.String())
	}
	return m
}

// ============ AdminHandler 测试 ============

func TestAdminHandler_Stats(t *testing.T) {
	r, _, _, database := setupAdminTestRouter(t)

	// 初始应为 0
	w := doReq(t, r, "GET", "/api/admin/stats", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if int64(resp["total_users"].(float64)) != 0 {
		t.Errorf("初始 total_users 期望 0，实际 %v", resp["total_users"])
	}

	// 创建 2 用户
	createTestUser(t, database, "u1")
	createTestUser(t, database, "u2")

	w = doReq(t, r, "GET", "/api/admin/stats", nil)
	resp = parseJSON(t, w)
	if int64(resp["total_users"].(float64)) != 2 {
		t.Errorf("total_users 期望 2，实际 %v", resp["total_users"])
	}
	if int64(resp["active_users"].(float64)) != 2 {
		t.Errorf("active_users 期望 2，实际 %v", resp["active_users"])
	}
}

func TestAdminHandler_ListUsers(t *testing.T) {
	r, _, _, database := setupAdminTestRouter(t)
	createTestUser(t, database, "u1")
	createTestUser(t, database, "u2")

	w := doReq(t, r, "GET", "/api/admin/users", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析列表 JSON 失败: %v, body: %s", err, w.Body.String())
	}
	if len(list) != 2 {
		t.Errorf("期望 2 个用户，实际 %d", len(list))
	}
	// 验证字段存在（snake_case）
	if _, ok := list[0]["storage_limit"]; !ok {
		t.Error("应包含 storage_limit 字段（snake_case）")
	}
	if _, ok := list[0]["is_active"]; !ok {
		t.Error("应包含 is_active 字段（snake_case）")
	}
}

func TestAdminHandler_GetUser_Exist(t *testing.T) {
	r, _, _, database := setupAdminTestRouter(t)
	id := createTestUser(t, database, "alice")

	w := doReq(t, r, "GET", "/api/admin/users/"+itoa(id), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["username"] != "alice" {
		t.Errorf("username 期望 alice，实际 %v", resp["username"])
	}
	if int64(resp["id"].(float64)) != id {
		t.Errorf("id 期望 %d，实际 %v", id, resp["id"])
	}
}

func TestAdminHandler_GetUser_NotExist(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)

	w := doReq(t, r, "GET", "/api/admin/users/99999", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["error"] != "用户不存在" {
		t.Errorf("error 期望 '用户不存在'，实际 %v", resp["error"])
	}
}

func TestAdminHandler_GetUser_InvalidID(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)

	w := doReq(t, r, "GET", "/api/admin/users/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	resp := parseJSON(t, w)
	if resp["error"] != "无效的用户 ID" {
		t.Errorf("error 期望 '无效的用户 ID'，实际 %v", resp["error"])
	}
}

func TestAdminHandler_CreateUser(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)
	const limit200MB int64 = 200 * 1024 * 1024

	w := doReq(t, r, "POST", "/api/admin/users", map[string]any{
		"username":      "newuser",
		"email":         "new@example.com",
		"password":      "password123",
		"display_name":  "New User",
		"role":          "admin",
		"storage_limit": limit200MB,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("期望 201，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["username"] != "newuser" {
		t.Errorf("username 期望 newuser，实际 %v", resp["username"])
	}
	if resp["role"] != "admin" {
		t.Errorf("role 期望 admin，实际 %v", resp["role"])
	}
	if int64(resp["storage_limit"].(float64)) != limit200MB {
		t.Errorf("storage_limit 期望 %d，实际 %v", limit200MB, resp["storage_limit"])
	}
	if resp["email"] != "new@example.com" {
		t.Errorf("email 期望 new@example.com，实际 %v", resp["email"])
	}
	if resp["display_name"] != "New User" {
		t.Errorf("display_name 期望 'New User'，实际 %v", resp["display_name"])
	}
}

func TestAdminHandler_CreateUser_BadRequest(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)

	// 非 JSON body → bind 失败 → 400
	req := httptest.NewRequest("POST", "/api/admin/users", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	resp := parseJSON(t, w)
	if resp["error"] != "请求参数无效" {
		t.Errorf("error 期望 '请求参数无效'，实际 %v", resp["error"])
	}
}

func TestAdminHandler_UpdateUser(t *testing.T) {
	r, _, _, database := setupAdminTestRouter(t)
	id := createTestUser(t, database, "u")
	ctx := context.Background()

	newRole := "admin"
	newLimit := int64(500 * 1024 * 1024)
	active := false
	w := doReq(t, r, "PUT", "/api/admin/users/"+itoa(id), map[string]any{
		"role":          newRole,
		"storage_limit": newLimit,
		"is_active":     active,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["message"] != "更新成功" {
		t.Errorf("message 期望 '更新成功'，实际 %v", resp["message"])
	}

	// 验证字段已更新
	u, _ := dao.NewUserDAO(database).FindByID(ctx, id)
	if u.Role != "admin" {
		t.Errorf("Role 期望 admin，实际 %q", u.Role)
	}
	if u.StorageLimit != newLimit {
		t.Errorf("StorageLimit 期望 %d，实际 %d", newLimit, u.StorageLimit)
	}
	if u.IsActive {
		t.Error("IsActive 期望 false")
	}
}

func TestAdminHandler_UpdateUser_NotExist(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)

	w := doReq(t, r, "PUT", "/api/admin/users/99999", map[string]any{
		"role": "admin",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

func TestAdminHandler_DeleteUser(t *testing.T) {
	r, _, _, database := setupAdminTestRouter(t)
	id := createTestUser(t, database, "u")
	ctx := context.Background()

	// 删除前应 active
	u, _ := dao.NewUserDAO(database).FindByID(ctx, id)
	if !u.IsActive {
		t.Fatal("删除前 IsActive 应为 true")
	}

	w := doReq(t, r, "DELETE", "/api/admin/users/"+itoa(id), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["message"] != "删除成功" {
		t.Errorf("message 期望 '删除成功'，实际 %v", resp["message"])
	}

	// 软删除：记录仍存在，但 IsActive=false
	u, _ = dao.NewUserDAO(database).FindByID(ctx, id)
	if u == nil {
		t.Fatal("软删除后用户记录应仍存在")
	}
	if u.IsActive {
		t.Error("软删除后 IsActive 应为 false")
	}
}

func TestAdminHandler_DeleteUser_NotExist(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)

	w := doReq(t, r, "DELETE", "/api/admin/users/99999", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d", w.Code)
	}
}

func TestAdminHandler_GetSettings_Empty(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)

	w := doReq(t, r, "GET", "/api/admin/settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	settings, ok := resp["settings"].([]any)
	if !ok {
		t.Fatalf("settings 应为数组，实际 %T", resp["settings"])
	}
	if len(settings) != 0 {
		t.Errorf("初始应 0 个设置，实际 %d", len(settings))
	}
}

func TestAdminHandler_GetSettings_WithValues(t *testing.T) {
	r, _, svc, _ := setupAdminTestRouter(t)
	ctx := context.Background()

	// 直接通过 service 写入设置
	if err := svc.UpdateSettings(ctx, map[string]string{"domain": "example.com"}); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}

	w := doReq(t, r, "GET", "/api/admin/settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	resp := parseJSON(t, w)
	settings := resp["settings"].([]any)
	if len(settings) != 1 {
		t.Errorf("期望 1 个设置，实际 %d", len(settings))
	}
	item := settings[0].(map[string]any)
	if item["key"] != "domain" {
		t.Errorf("key 期望 domain，实际 %v", item["key"])
	}
	if item["value"] != "example.com" {
		t.Errorf("value 期望 example.com，实际 %v", item["value"])
	}
}

func TestAdminHandler_UpdateSettings(t *testing.T) {
	r, _, svc, _ := setupAdminTestRouter(t)
	ctx := context.Background()

	w := doReq(t, r, "PUT", "/api/admin/settings", map[string]any{
		"settings": map[string]string{
			"domain":    "example.com",
			"smtp_port": "25",
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["message"] != "更新成功" {
		t.Errorf("message 期望 '更新成功'，实际 %v", resp["message"])
	}

	// 通过 service 验证已写入
	result, _ := svc.GetSettings(ctx)
	if len(result) != 2 {
		t.Errorf("期望 2 个设置，实际 %d", len(result))
	}
	got := make(map[string]string)
	for _, s := range result {
		got[s.Key] = s.Value
	}
	if got["domain"] != "example.com" {
		t.Errorf("domain 期望 example.com，实际 %q", got["domain"])
	}
	if got["smtp_port"] != "25" {
		t.Errorf("smtp_port 期望 25，实际 %q", got["smtp_port"])
	}

	// 通过 GET 端点再验证一次
	w = doReq(t, r, "GET", "/api/admin/settings", nil)
	resp = parseJSON(t, w)
	settings := resp["settings"].([]any)
	if len(settings) != 2 {
		t.Errorf("GET 后期望 2 个设置，实际 %d", len(settings))
	}
}

func TestAdminHandler_UpdateSettings_BadRequest(t *testing.T) {
	r, _, _, _ := setupAdminTestRouter(t)

	req := httptest.NewRequest("PUT", "/api/admin/settings", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
}

// ============ APIKeyHandler 测试 ============

func TestAPIKeyHandler_CreateKey(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	w := doReq(t, r, "POST", "/api/auth/api-keys", map[string]any{
		"name": "my-key",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("期望 201，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["plain_text"] == nil || resp["plain_text"] == "" {
		t.Error("plain_text 不应为空")
	}
	if resp["name"] != "my-key" {
		t.Errorf("name 期望 my-key，实际 %v", resp["name"])
	}
	if resp["key_prefix"] == nil || resp["key_prefix"] == "" {
		t.Error("key_prefix 不应为空")
	}
	// 默认 scopes ["send"]
	scopes, ok := resp["scopes"].([]any)
	if !ok || len(scopes) != 1 || scopes[0] != "send" {
		t.Errorf("默认 scopes 期望 [send]，实际 %v", resp["scopes"])
	}
	// 默认 rate_limit 60
	if int(resp["rate_limit"].(float64)) != 60 {
		t.Errorf("默认 rate_limit 期望 60，实际 %v", resp["rate_limit"])
	}
}

func TestAPIKeyHandler_CreateKey_BadRequest(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	req := httptest.NewRequest("POST", "/api/auth/api-keys", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	resp := parseJSON(t, w)
	if resp["error"] != "请求参数无效" {
		t.Errorf("error 期望 '请求参数无效'，实际 %v", resp["error"])
	}
}

func TestAPIKeyHandler_CreateKey_EmptyName(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	// Name 为空 → service 返回 AuthError(400)
	w := doReq(t, r, "POST", "/api/auth/api-keys", map[string]any{
		"name": "",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["error"] != "API Key 名称不能为空" {
		t.Errorf("error 期望 'API Key 名称不能为空'，实际 %v", resp["error"])
	}
}

func TestAPIKeyHandler_ListKeys(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	// 创建 2 个 key
	doReq(t, r, "POST", "/api/auth/api-keys", map[string]any{"name": "k1"})
	doReq(t, r, "POST", "/api/auth/api-keys", map[string]any{"name": "k2"})

	w := doReq(t, r, "GET", "/api/auth/api-keys", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析列表 JSON 失败: %v, body: %s", err, w.Body.String())
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个 key，实际 %d，body: %s", len(list), w.Body.String())
	}
	// 列表响应不应包含 plain_text
	if _, ok := list[0]["plain_text"]; ok {
		t.Error("列表响应不应包含 plain_text")
	}
	// 应包含 key_prefix
	if _, ok := list[0]["key_prefix"]; !ok {
		t.Error("应包含 key_prefix 字段")
	}
}

func TestAPIKeyHandler_ListKeys_Empty(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	w := doReq(t, r, "GET", "/api/auth/api-keys", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析列表 JSON 失败: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("初始应 0 个 key，实际 %d", len(list))
	}
}

func TestAPIKeyHandler_DeleteKey_Success(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	// 创建 key
	w := doReq(t, r, "POST", "/api/auth/api-keys", map[string]any{"name": "k1"})
	resp := parseJSON(t, w)
	keyID := int64(resp["id"].(float64))

	// 删除
	w = doReq(t, r, "DELETE", "/api/auth/api-keys/"+itoa(keyID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp = parseJSON(t, w)
	if resp["message"] != "删除成功" {
		t.Errorf("message 期望 '删除成功'，实际 %v", resp["message"])
	}

	// 列表应为空
	w = doReq(t, r, "GET", "/api/auth/api-keys", nil)
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Errorf("删除后应 0 个 key，实际 %d", len(list))
	}
}

func TestAPIKeyHandler_DeleteKey_NotExist(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	w := doReq(t, r, "DELETE", "/api/auth/api-keys/99999", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d，body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["error"] != "API Key 不存在" {
		t.Errorf("error 期望 'API Key 不存在'，实际 %v", resp["error"])
	}
}

func TestAPIKeyHandler_DeleteKey_InvalidID(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database, "alice")
	r, _, _ := setupAPIKeyTestRouter(t, database, uid)

	w := doReq(t, r, "DELETE", "/api/auth/api-keys/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", w.Code)
	}
	resp := parseJSON(t, w)
	if resp["error"] != "无效的 API Key ID" {
		t.Errorf("error 期望 '无效的 API Key ID'，实际 %v", resp["error"])
	}
}

// itoa 将 int64 转为字符串，用于拼接 URL 路径参数。
func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
