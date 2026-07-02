// health_test.go 测试健康检查端点。
// 覆盖 Liveness / Readiness / Startup 三个端点。
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/httpapi/handler"
	"github.com/mymail/mymail-go/internal/storage/db"
)

func newHealthEnv(t *testing.T) (*gin.Engine, *handler.HealthHandler) {
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

	h := handler.NewHealthHandler(database)
	r := gin.New()
	r.GET("/healthz", h.Liveness)
	r.GET("/readyz", h.Readiness)
	r.GET("/startupz", h.Startup)
	return r, h
}

func TestHealthHandler_Liveness(t *testing.T) {
	r, _ := newHealthEnv(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "alive") {
		t.Errorf("响应应含 'alive'，实际 %s", w.Body.String())
	}
}

func TestHealthHandler_Startup_NotReady(t *testing.T) {
	r, _ := newHealthEnv(t)

	// 刚创建，started 标志为 false（需等 2 秒）
	req := httptest.NewRequest("GET", "/startupz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("启动初期应返回 503，实际 %d", w.Code)
	}
}

func TestHealthHandler_Startup_Ready(t *testing.T) {
	r, h := newHealthEnv(t)

	// 模拟启动完成
	h.SetStarted(true)

	req := httptest.NewRequest("GET", "/startupz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("启动完成后应返回 200，实际 %d", w.Code)
	}
}

func TestHealthHandler_Readiness_Ready(t *testing.T) {
	r, h := newHealthEnv(t)
	h.SetStarted(true)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ready") {
		t.Errorf("响应应含 'ready'，实际 %s", w.Body.String())
	}
}

func TestHealthHandler_Readiness_Starting(t *testing.T) {
	r, _ := newHealthEnv(t)

	// 未启动完成
	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("启动中应返回 503，实际 %d", w.Code)
	}
}
