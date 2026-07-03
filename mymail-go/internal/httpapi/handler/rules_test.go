// rules_test.go 测试 RuleHandler 的 HTTP 端点契约与归属校验。
//
// 使用外部测试包 handler_test，复用 auth_test.go 中的 doJSON 辅助。
// 认证上下文通过测试中间件直接注入 ContextKeyUser（模拟 JWT 认证后的状态），
// 避免依赖完整 JWT 登录流程，聚焦 handler 逻辑。
//
// 测试覆盖：
//   - List：创建规则后 List 返回
//   - Create：创建成功
//   - Update：更新成功 + 归属校验（其他用户 403）
//   - Delete：删除成功 + 归属校验
//   - Update 不存在 → 404
//   - Delete 不存在 → 404
package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/httpapi/handler"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// ============ 共享测试辅助（rules_test.go 定义，apiv1_test.go 复用） ============

// newHandlerTestDB 创建临时 SQLite 数据库并执行迁移（handler 测试辅助）。
func newHandlerTestDB(t *testing.T) *db.DB {
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

// createHandlerTestUser 创建测试用户并返回完整 *dao.User（含 ID/Email 等字段）。
func createHandlerTestUser(t *testing.T, database *db.DB, username string) *dao.User {
	t.Helper()
	userDAO := dao.NewUserDAO(database)
	id, err := userDAO.Create(context.Background(), dao.CreateUserInput{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	user, err := userDAO.FindByID(context.Background(), id)
	if err != nil || user == nil {
		t.Fatalf("查询测试用户失败: %v", err)
	}
	return user
}

// injectUser 返回一个向 context 注入当前用户的测试中间件（模拟 JWT 认证后状态）。
func injectUser(user *dao.User) gin.HandlerFunc {
	return func(c *gin.Context) {
		if user != nil {
			c.Set(middleware.ContextKeyUser, user)
		}
		c.Next()
	}
}

// newRulesRouter 构建挂载 RuleHandler 的测试路由（含注入用户中间件）。
func newRulesRouter(database *db.DB, user *dao.User) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ruleH := handler.NewRuleHandler(service.NewRuleService(dao.NewRuleDAO(database)))
	r := gin.New()
	g := r.Group("/api/rules")
	g.Use(injectUser(user))
	g.GET("", ruleH.List)
	g.POST("", ruleH.Create)
	g.PUT("/:id", ruleH.Update)
	g.DELETE("/:id", ruleH.Delete)
	return r
}

// parseRuleID 从创建响应中解析规则 ID。
func parseRuleID(t *testing.T, w *httptest.ResponseRecorder) int64 {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	id, _ := resp["id"].(float64)
	if id == 0 {
		t.Fatalf("响应中无有效 id, body: %s", w.Body.String())
	}
	return int64(id)
}

// ============ List ============

func TestRuleHandler_List(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	router := newRulesRouter(database, alice)

	// 先创建一条规则
	doJSON(t, router, "POST", "/api/rules", map[string]any{
		"name":     "spam-filter",
		"priority": 10,
		"conditions": []map[string]any{
			{"field": "from", "op": "contains", "value": "spam@example.com"},
		},
		"actions": []map[string]any{
			{"type": "move", "folder": "JUNK"},
		},
	}, "")

	// List 应返回 1 条
	w := doJSON(t, router, "GET", "/api/rules", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}

	var rules []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rules); err != nil {
		t.Fatalf("解析列表失败: %v, body: %s", err, w.Body.String())
	}
	if len(rules) != 1 {
		t.Fatalf("期望 1 条规则，实际 %d", len(rules))
	}
	if rules[0]["name"] != "spam-filter" {
		t.Errorf("name 期望 spam-filter，实际 %v", rules[0]["name"])
	}
	if rules[0]["is_active"] != true {
		t.Errorf("新建规则 is_active 期望 true，实际 %v", rules[0]["is_active"])
	}
	// 校验 conditions/actions 序列化正确
	conds, _ := rules[0]["conditions"].([]any)
	if len(conds) != 1 {
		t.Errorf("期望 1 个 condition，实际 %d", len(conds))
	}
	acts, _ := rules[0]["actions"].([]any)
	if len(acts) != 1 {
		t.Errorf("期望 1 个 action，实际 %d", len(acts))
	}
}

// ============ Create ============

func TestRuleHandler_Create(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	router := newRulesRouter(database, alice)

	w := doJSON(t, router, "POST", "/api/rules", map[string]any{
		"name":     "my-rule",
		"priority": 5,
		"conditions": []map[string]any{
			{"field": "subject", "op": "contains", "value": "test"},
		},
		"actions": []map[string]any{
			{"type": "mark_read"},
		},
	}, "")

	if w.Code != http.StatusCreated {
		t.Fatalf("期望 201，实际 %d，body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	id, _ := resp["id"].(float64)
	if id <= 0 {
		t.Errorf("id 应 > 0，实际 %v", resp["id"])
	}
	if resp["name"] != "my-rule" {
		t.Errorf("name 期望 my-rule，实际 %v", resp["name"])
	}
	if resp["user_id"].(float64) != float64(alice.ID) {
		t.Errorf("user_id 期望 %d，实际 %v", alice.ID, resp["user_id"])
	}
	if resp["is_active"] != true {
		t.Errorf("新建规则 is_active 期望 true，实际 %v", resp["is_active"])
	}
}

// ============ Update ============

func TestRuleHandler_Update(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	bob := createHandlerTestUser(t, database, "bob")
	routerA := newRulesRouter(database, alice)
	routerB := newRulesRouter(database, bob)

	// alice 创建规则
	w := doJSON(t, routerA, "POST", "/api/rules", map[string]any{
		"name": "old-name", "priority": 5,
	}, "")
	id := parseRuleID(t, w)

	// bob 尝试更新 alice 的规则 → 403
	w = doJSON(t, routerB, "PUT", "/api/rules/"+itoa(id), map[string]any{
		"name": "hacked",
	}, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("其他用户更新期望 403，实际 %d，body: %s", w.Code, w.Body.String())
	}

	// alice 自己更新 → 200
	w = doJSON(t, routerA, "PUT", "/api/rules/"+itoa(id), map[string]any{
		"name": "new-name", "priority": 20,
	}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("更新期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}

	// 验证更新生效（通过 List）
	w = doJSON(t, routerA, "GET", "/api/rules", nil, "")
	var rules []map[string]any
	json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules) != 1 {
		t.Fatalf("期望 1 条规则，实际 %d", len(rules))
	}
	if rules[0]["name"] != "new-name" {
		t.Errorf("name 期望 new-name，实际 %v", rules[0]["name"])
	}
	if rules[0]["priority"].(float64) != 20 {
		t.Errorf("priority 期望 20，实际 %v", rules[0]["priority"])
	}
}

// ============ Delete ============

func TestRuleHandler_Delete(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	bob := createHandlerTestUser(t, database, "bob")
	routerA := newRulesRouter(database, alice)
	routerB := newRulesRouter(database, bob)

	// alice 创建规则
	w := doJSON(t, routerA, "POST", "/api/rules", map[string]any{
		"name": "to-delete",
	}, "")
	id := parseRuleID(t, w)

	// bob 尝试删除 alice 的规则 → 403
	w = doJSON(t, routerB, "DELETE", "/api/rules/"+itoa(id), nil, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("其他用户删除期望 403，实际 %d，body: %s", w.Code, w.Body.String())
	}

	// alice 自己删除 → 200
	w = doJSON(t, routerA, "DELETE", "/api/rules/"+itoa(id), nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("删除期望 200，实际 %d，body: %s", w.Code, w.Body.String())
	}

	// 验证已删除（List 为空）
	w = doJSON(t, routerA, "GET", "/api/rules", nil, "")
	var rules []map[string]any
	json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules) != 0 {
		t.Errorf("删除后应 0 条规则，实际 %d", len(rules))
	}
}

// ============ Update 不存在 ============

func TestRuleHandler_Update_NotExist(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	router := newRulesRouter(database, alice)

	w := doJSON(t, router, "PUT", "/api/rules/99999", map[string]any{
		"name": "x",
	}, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在期望 404，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// ============ Delete 不存在 ============

func TestRuleHandler_Delete_NotExist(t *testing.T) {
	database := newHandlerTestDB(t)
	alice := createHandlerTestUser(t, database, "alice")
	router := newRulesRouter(database, alice)

	w := doJSON(t, router, "DELETE", "/api/rules/99999", nil, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在期望 404，实际 %d，body: %s", w.Code, w.Body.String())
	}
}

// itoa 将 int64 转为字符串（避免引入 strconv 与其他测试文件冲突）。
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
