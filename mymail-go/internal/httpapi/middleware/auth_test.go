// auth_test.go 测试 JWT 认证中间件（Authenticate）与权限中间件（RequireAdmin）。
//
// 验收标准 9.6 第 3 项：非管理员访问 /api/admin/* 返回 403。
//
// 测试覆盖：
//   - RequireAdmin: 无用户上下文 → 401
//   - RequireAdmin: 非管理员用户 → 403
//   - RequireAdmin: 管理员用户 → 放行
//   - CurrentUser: 存在 / 不存在
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// setupRequireAdminRouter 构建挂载 RequireAdmin 的测试路由。
func setupRequireAdminRouter() *gin.Engine {
	r := gin.New()
	r.Use(RequireAdmin())
	r.GET("/admin/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestRequireAdmin_NoUser(t *testing.T) {
	r := setupRequireAdminRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/protected", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无用户上下文期望 401，实际 %d", w.Code)
	}
}

func TestRequireAdmin_NonAdmin(t *testing.T) {
	// 注入非管理员用户
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(ContextKeyUser, &dao.User{ID: 1, Role: "user"})
		c.Next()
	}, RequireAdmin())
	r.GET("/admin/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/protected", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("非管理员期望 403，实际 %d", w.Code)
	}
}

func TestRequireAdmin_Admin(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(ContextKeyUser, &dao.User{ID: 1, Role: "admin"})
		c.Next()
	}, RequireAdmin())
	r.GET("/admin/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/protected", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("管理员用户期望 200，实际 %d", w.Code)
	}
}

func TestCurrentUser_Exists(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	expected := &dao.User{ID: 42, Username: "test"}
	c.Set(ContextKeyUser, expected)
	got := CurrentUser(c)
	if got == nil || got.ID != 42 {
		t.Errorf("CurrentUser 期望 ID=42，实际 %+v", got)
	}
}

func TestCurrentUser_NotExists(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	got := CurrentUser(c)
	if got != nil {
		t.Errorf("无用户上下文 CurrentUser 应返回 nil，实际 %+v", got)
	}
}
