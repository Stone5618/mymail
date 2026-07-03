// api_auth_test.go 测试 API Key 认证中间件。
//
// 测试覆盖：
//   - APIKeyAuth: 缺失 header / 非 Bearer 格式 / 无效 key / 禁用用户 / 成功
//   - RequireScope: 拥有 scope / 缺少 scope / 无 api_key
//   - CurrentAPIKey: 存在 / 不存在
//
// 通过 mockAPIKeyVerifier 解耦 service 层，避免循环依赖。
// 需要触发 FindByID 的测试用真实 SQLite 临时数据库（newTestDBForMiddleware）。
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockAPIKeyVerifier mock APIKeyVerifier 接口，避免依赖 service 包。
type mockAPIKeyVerifier struct {
	apiKey *dao.APIKey
	err    error
}

func (m *mockAPIKeyVerifier) Verify(ctx context.Context, plaintext string) (*dao.APIKey, error) {
	return m.apiKey, m.err
}

// newTestDBForMiddleware 创建临时 SQLite 数据库并执行迁移。
// middleware 包无法复用 dao 包的 newTestDB，需自行实现。
func newTestDBForMiddleware(t *testing.T) *db.DB {
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

// assertErrorResponse 校验响应状态码与 error 字段。
func assertErrorResponse(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantError string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Errorf("状态码期望 %d，实际 %d", wantStatus, w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应体失败: %v, body: %s", err, w.Body.String())
	}
	if resp["error"] != wantError {
		t.Errorf("error 期望 %q，实际 %q", wantError, resp["error"])
	}
}

func TestAPIKeyAuth_MissingHeader(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	APIKeyAuth(&mockAPIKeyVerifier{}, nil)(c)

	assertErrorResponse(t, w, http.StatusUnauthorized, "缺少 API Key")
	if !c.IsAborted() {
		t.Error("应被 abort")
	}
}

func TestAPIKeyAuth_NotBearer(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Basic abc123def")

	APIKeyAuth(&mockAPIKeyVerifier{}, nil)(c)

	assertErrorResponse(t, w, http.StatusUnauthorized, "缺少 API Key")
	if !c.IsAborted() {
		t.Error("应被 abort")
	}
}

func TestAPIKeyAuth_InvalidKey(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer mk_invalidtoken")

	verifier := &mockAPIKeyVerifier{err: errors.New("key not found")}
	APIKeyAuth(verifier, nil)(c)

	assertErrorResponse(t, w, http.StatusUnauthorized, "无效的 API Key")
	if !c.IsAborted() {
		t.Error("应被 abort")
	}
}

func TestAPIKeyAuth_DisabledUser(t *testing.T) {
	database := newTestDBForMiddleware(t)
	userDAO := dao.NewUserDAO(database)
	ctx := context.Background()

	uid, err := userDAO.Create(ctx, dao.CreateUserInput{
		Username:     "disabled",
		Email:        "disabled@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := userDAO.SetActive(ctx, uid, false); err != nil {
		t.Fatalf("禁用用户失败: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer mk_sometoken")

	verifier := &mockAPIKeyVerifier{apiKey: &dao.APIKey{ID: 1, UserID: uid}}
	APIKeyAuth(verifier, userDAO)(c)

	assertErrorResponse(t, w, http.StatusUnauthorized, "账号已禁用")
	if !c.IsAborted() {
		t.Error("应被 abort")
	}
}

func TestAPIKeyAuth_Success(t *testing.T) {
	database := newTestDBForMiddleware(t)
	userDAO := dao.NewUserDAO(database)
	ctx := context.Background()

	uid, err := userDAO.Create(ctx, dao.CreateUserInput{
		Username:     "active",
		Email:        "active@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer mk_validtoken")

	apiKey := &dao.APIKey{ID: 1, UserID: uid, Scopes: []string{"send"}}
	verifier := &mockAPIKeyVerifier{apiKey: apiKey}
	APIKeyAuth(verifier, userDAO)(c)

	if w.Code != http.StatusOK {
		t.Errorf("状态码期望 200，实际 %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("不应被 abort，应调用 c.Next")
	}

	// 验证 context 注入 user
	v, ok := c.Get(ContextKeyUser)
	if !ok {
		t.Fatal("context 应注入 user")
	}
	user, ok := v.(*dao.User)
	if !ok || user.ID != uid {
		t.Errorf("注入的 user ID 期望 %d，实际 %+v", uid, v)
	}

	// 验证 context 注入 api_key
	v, ok = c.Get(ContextKeyAPIKey)
	if !ok {
		t.Fatal("context 应注入 api_key")
	}
	injected, ok := v.(*dao.APIKey)
	if !ok || injected.ID != apiKey.ID {
		t.Errorf("注入的 api_key ID 期望 %d，实际 %+v", apiKey.ID, v)
	}
}

func TestRequireScope_HasScope(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	c.Set(ContextKeyAPIKey, &dao.APIKey{Scopes: []string{"send"}})

	RequireScope("send")(c)

	if w.Code != http.StatusOK {
		t.Errorf("状态码期望 200，实际 %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("不应被 abort，应调用 c.Next")
	}
}

func TestRequireScope_NoScope(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	c.Set(ContextKeyAPIKey, &dao.APIKey{Scopes: []string{"read"}})

	RequireScope("send")(c)

	assertErrorResponse(t, w, http.StatusForbidden, "API Key 无此操作权限")
	if !c.IsAborted() {
		t.Error("应被 abort")
	}
}

func TestRequireScope_NoAPIKey(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	// 不设置 api_key

	RequireScope("send")(c)

	assertErrorResponse(t, w, http.StatusUnauthorized, "未通过 API Key 认证")
	if !c.IsAborted() {
		t.Error("应被 abort")
	}
}

func TestCurrentAPIKey(t *testing.T) {
	t.Run("has api_key", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

		apiKey := &dao.APIKey{ID: 42, Scopes: []string{"send"}}
		c.Set(ContextKeyAPIKey, apiKey)

		got := CurrentAPIKey(c)
		if got == nil {
			t.Fatal("应返回非 nil")
		}
		if got.ID != 42 {
			t.Errorf("ID 期望 42，实际 %d", got.ID)
		}
	})

	t.Run("no api_key", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

		got := CurrentAPIKey(c)
		if got != nil {
			t.Errorf("应返回 nil，实际 %+v", got)
		}
	})
}
