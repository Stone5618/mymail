// apiv1_test.go 测试 APIV1Handler 的外部发信端点契约。
//
// 使用外部测试包 handler_test，复用 rules_test.go 中的 newHandlerTestDB /
// createHandlerTestUser 辅助。认证上下文通过测试中间件直接注入
// ContextKeyUser + ContextKeyAPIKey（模拟 API Key 认证后的状态）。
//
// 测试覆盖：
//   - Send 成功（本域收件人）
//   - Send 收件人为空 → 400
//   - Send 参数错误（非法 JSON）→ 400
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/httpapi/handler"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// newAPIV1Router 构建挂载 APIV1Handler 的测试路由（含注入 user + apiKey 中间件）。
// 复用 rules_test.go 的 newHandlerTestDB / createHandlerTestUser。
func newAPIV1Router(database *db.DB, user *dao.User, apiKey *dao.APIKey) *gin.Engine {
	gin.SetMode(gin.TestMode)

	msgDAO := dao.NewMessageDAO(database)
	attachDAO := dao.NewAttachmentDAO(database)
	sendLogDAO := dao.NewSendLogDAO(database)
	queueDAO := dao.NewMailQueueDAO(database)
	userDAO := dao.NewUserDAO(database)
	auditLogger, err := audit.NewLogger(database, false, "")
	if err != nil {
		panic("创建审计日志器失败: " + err.Error())
	}
	mailSvc := service.NewMailService(
		msgDAO, attachDAO, sendLogDAO, queueDAO, userDAO,
		database, auditLogger, "example.com", 10,
	)
	apiH := handler.NewAPIV1Handler(mailSvc)

	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(func(c *gin.Context) {
		if user != nil {
			c.Set(middleware.ContextKeyUser, user)
		}
		if apiKey != nil {
			c.Set(middleware.ContextKeyAPIKey, apiKey)
		}
		c.Next()
	})
	g.POST("/send", apiH.Send)
	return r
}

// ============ Send 成功 ============

func TestAPIV1Handler_Send_Success(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	apiKey := &dao.APIKey{ID: 1, UserID: alice.ID, Scopes: []string{"send"}}
	router := newAPIV1Router(database, alice, apiKey)

	w := doJSON(t, router, "POST", "/api/v1/send", map[string]any{
		"to":        []string{"bob@example.com"},
		"subject":   "Hello from API",
		"body_text": "This is a test message.",
	}, "")

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	if resp["message"] != "发送成功" {
		t.Errorf("message 期望 '发送成功'，实际 %v", resp["message"])
	}
	queueID, _ := resp["queue_id"].(float64)
	if queueID <= 0 {
		t.Errorf("queue_id 应 > 0，实际 %v", resp["queue_id"])
	}
}

// ============ Send 收件人为空 ============

func TestAPIV1Handler_Send_EmptyTo(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	apiKey := &dao.APIKey{ID: 1, UserID: alice.ID, Scopes: []string{"send"}}
	router := newAPIV1Router(database, alice, apiKey)

	w := doJSON(t, router, "POST", "/api/v1/send", map[string]any{
		"to":      []string{},
		"subject": "No recipient",
	}, "")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d，body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	if resp["error"] != "收件人不能为空" {
		t.Errorf("error 期望 '收件人不能为空'，实际 %v", resp["error"])
	}
}

// ============ Send 参数错误（非法 JSON） ============

func TestAPIV1Handler_Send_InvalidJSON(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	apiKey := &dao.APIKey{ID: 1, UserID: alice.ID, Scopes: []string{"send"}}
	router := newAPIV1Router(database, alice, apiKey)

	// 手动构造非法 JSON 请求（doJSON 会正确编码，故直接构造）
	req := httptest.NewRequest(http.MethodPost, "/api/v1/send", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d，body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	if resp["error"] != "请求格式错误" {
		t.Errorf("error 期望 '请求格式错误'，实际 %v", resp["error"])
	}
}
