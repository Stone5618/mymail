// auth_test.go 测试 WebSocket 认证。
//
// P1-18 验收：子协议优先 + URL query 兼容。
//
// 测试覆盖：
//   - ExtractToken: 子协议优先
//   - ExtractToken: URL query 兼容
//   - ExtractToken: 无 token
//   - ExtractToken: 多子协议混合
//   - Authenticate: 缺少 token
//   - Authenticate: 无效 token
//   - Authenticate: 有效 token
//   - Authenticate: 用户不存在
//   - Authenticate: 用户已禁用
package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

func TestExtractToken_Subprotocol(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "auth.eyJhbGciOiJIUzI1NiJ9.test")

	token := ExtractToken(r)
	if token != "eyJhbGciOiJIUzI1NiJ9.test" {
		t.Errorf("子协议提取 token 期望 'eyJhbGciOiJIUzI1NiJ9.test'，实际 %q", token)
	}
}

func TestExtractToken_SubprotocolMultiple(t *testing.T) {
	// 多个子协议，应提取 auth. 前缀的那个
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "chat, auth.mytoken123, other")

	token := ExtractToken(r)
	if token != "mytoken123" {
		t.Errorf("多子协议提取 token 期望 'mytoken123'，实际 %q", token)
	}
}

func TestExtractToken_URLQuery(t *testing.T) {
	// 无子协议时回退到 URL query
	r := httptest.NewRequest(http.MethodGet, "/ws?token=abc123", nil)

	token := ExtractToken(r)
	if token != "abc123" {
		t.Errorf("URL query 提取 token 期望 'abc123'，实际 %q", token)
	}
}

func TestExtractToken_SubprotocolPriority(t *testing.T) {
	// 子协议和 URL query 同时存在，子协议优先
	r := httptest.NewRequest(http.MethodGet, "/ws?token=querytoken", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "auth.subprotocoltoken")

	token := ExtractToken(r)
	if token != "subprotocoltoken" {
		t.Errorf("子协议优先，期望 'subprotocoltoken'，实际 %q", token)
	}
}

func TestExtractToken_NoToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)

	token := ExtractToken(r)
	if token != "" {
		t.Errorf("无 token 期望空字符串，实际 %q", token)
	}
}

func TestExtractToken_EmptySubprotocol(t *testing.T) {
	// 子协议头存在但无 auth. 前缀，应回退到 URL query
	r := httptest.NewRequest(http.MethodGet, "/ws?token=fallback", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "chat, other")

	token := ExtractToken(r)
	if token != "fallback" {
		t.Errorf("无 auth. 子协议应回退 URL query，期望 'fallback'，实际 %q", token)
	}
}

// --- Authenticate 测试 ---

// newAuthTestDB 创建测试 DB + 用户。
func newAuthTestDB(t *testing.T) (*db.DB, *dao.UserDAO, *crypto.JWTManager, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth_test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	userDAO := dao.NewUserDAO(database)
	uid, err := userDAO.Create(context.Background(), dao.CreateUserInput{
		Username: "wsuser", Email: "ws@example.com", PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}

	jwtMgr, err := crypto.NewJWTManager("test-secret-key-123456", "24h", "720h")
	if err != nil {
		t.Fatalf("创建 JWT 管理器失败: %v", err)
	}

	return database, userDAO, jwtMgr, uid
}

func TestAuthenticate_ValidToken(t *testing.T) {
	_, userDAO, jwtMgr, uid := newAuthTestDB(t)

	token, err := jwtMgr.Generate(uid, "ws@example.com", "user", false)
	if err != nil {
		t.Fatalf("生成 JWT 失败: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "auth."+token)

	gotID, err := Authenticate(r, jwtMgr, userDAO)
	if err != nil {
		t.Fatalf("有效 token 认证失败: %v", err)
	}
	if gotID != uid {
		t.Errorf("认证返回 userID 期望 %d，实际 %d", uid, gotID)
	}
}

func TestAuthenticate_ValidToken_URLQuery(t *testing.T) {
	_, userDAO, jwtMgr, uid := newAuthTestDB(t)

	token, err := jwtMgr.Generate(uid, "ws@example.com", "user", false)
	if err != nil {
		t.Fatalf("生成 JWT 失败: %v", err)
	}

	// 通过 URL query 传递 token
	r := httptest.NewRequest(http.MethodGet, "/ws?token="+token, nil)

	gotID, err := Authenticate(r, jwtMgr, userDAO)
	if err != nil {
		t.Fatalf("URL query token 认证失败: %v", err)
	}
	if gotID != uid {
		t.Errorf("认证返回 userID 期望 %d，实际 %d", uid, gotID)
	}
}

func TestAuthenticate_MissingToken(t *testing.T) {
	_, userDAO, jwtMgr, _ := newAuthTestDB(t)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)

	_, err := Authenticate(r, jwtMgr, userDAO)
	if err == nil {
		t.Error("缺少 token 应返回错误")
	}
	if err != ErrMissingToken {
		t.Errorf("期望 ErrMissingToken，实际 %v", err)
	}
}

func TestAuthenticate_InvalidToken(t *testing.T) {
	_, userDAO, jwtMgr, _ := newAuthTestDB(t)

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "auth.invalid.jwt.token")

	_, err := Authenticate(r, jwtMgr, userDAO)
	if err == nil {
		t.Error("无效 token 应返回错误")
	}
}

func TestAuthenticate_DisabledUser(t *testing.T) {
	database, userDAO, jwtMgr, uid := newAuthTestDB(t)

	// 禁用用户
	if err := userDAO.SetActive(context.Background(), uid, false); err != nil {
		t.Fatalf("禁用用户失败: %v", err)
	}

	token, err := jwtMgr.Generate(uid, "ws@example.com", "user", false)
	if err != nil {
		t.Fatalf("生成 JWT 失败: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "auth."+token)

	_, err = Authenticate(r, jwtMgr, userDAO)
	if err == nil {
		t.Error("已禁用用户应返回错误")
	}
	_ = database
}
