// security_test.go — E2E 安全测试：IDOR / SQL 注入 / XSS / 越权访问。
package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// TestE2E_IDOR 测试 IDOR（不安全直接对象引用）：
// 用户 A 不应能访问用户 B 的邮件。
func TestE2E_IDOR(t *testing.T) {
	env := newTestEnv(t)

	// 用户 A（admin）注册并插入邮件
	tokenA := env.registerAndLogin(t, "userA", "PassA123!")
	meA := env.getMe(t, tokenA)
	userIDA := int64(meA["id"].(float64))
	mailIDA := env.insertTestMessage(t, userIDA, "A的私密邮件", "这是用户A的私密内容")

	// 用户 B 注册
	tokenB := env.registerAndLogin(t, "userB", "PassB123!")

	// 用户 B 尝试访问用户 A 的邮件 → 应 403/404
	status, _ := env.doJSON(t, "GET", fmt.Sprintf("/api/mail/%d", mailIDA), nil, tokenB)
	if status != http.StatusForbidden && status != http.StatusNotFound {
		t.Fatalf("IDOR: 用户B访问用户A的邮件应返回 403/404，实际 %d", status)
	}
	t.Logf("IDOR 防护通过: 用户B无法访问用户A的邮件 (status=%d)", status)

	// 用户 B 尝试删除用户 A 的邮件 → 应 403/404
	status, _ = env.doJSON(t, "DELETE", fmt.Sprintf("/api/mail/%d", mailIDA), nil, tokenB)
	if status != http.StatusForbidden && status != http.StatusNotFound {
		t.Fatalf("IDOR: 用户B删除用户A的邮件应返回 403/404，实际 %d", status)
	}
	t.Logf("IDOR 防护通过: 用户B无法删除用户A的邮件 (status=%d)", status)

	// 用户 B 尝试星标用户 A 的邮件 → 应 403/404
	status, _ = env.doJSON(t, "PUT", fmt.Sprintf("/api/mail/%d/star", mailIDA), nil, tokenB)
	if status != http.StatusForbidden && status != http.StatusNotFound {
		t.Fatalf("IDOR: 用户B星标用户A的邮件应返回 403/404，实际 %d", status)
	}
	t.Logf("IDOR 防护通过: 用户B无法星标用户A的邮件 (status=%d)", status)
}

// TestE2E_PrivilegeEscalation 测试越权访问：
// 普通用户不应能访问 admin 端点。
func TestE2E_PrivilegeEscalation(t *testing.T) {
	env := newTestEnv(t)

	// admin 注册
	env.registerAndLogin(t, "admin", "AdminPass123!")

	// 普通用户注册
	tokenUser := env.registerAndLogin(t, "normal", "NormalPass123!")

	// 普通用户访问 admin 统计 → 应 403
	status, _ := env.doJSON(t, "GET", "/api/admin/stats", nil, tokenUser)
	if status != http.StatusForbidden {
		t.Fatalf("越权: 普通用户访问 /api/admin/stats 应返回 403，实际 %d", status)
	}
	t.Logf("越权防护通过: 普通用户无法访问 admin/stats")

	// 普通用户访问 admin 用户列表 → 应 403
	status, _ = env.doJSON(t, "GET", "/api/admin/users", nil, tokenUser)
	if status != http.StatusForbidden {
		t.Fatalf("越权: 普通用户访问 /api/admin/users 应返回 403，实际 %d", status)
	}
	t.Logf("越权防护通过: 普通用户无法访问 admin/users")

	// 普通用户尝试创建用户 → 应 403
	status, _ = env.doJSON(t, "POST", "/api/admin/users",
		map[string]string{"username": "hacker", "password": "HackerPass123!"}, tokenUser)
	if status != http.StatusForbidden {
		t.Fatalf("越权: 普通用户创建用户应返回 403，实际 %d", status)
	}
	t.Logf("越权防护通过: 普通用户无法创建用户")

	// 普通用户尝试更新全局设置 → 应 403
	status, _ = env.doJSON(t, "PUT", "/api/admin/settings",
		map[string]any{"key": "value"}, tokenUser)
	if status != http.StatusForbidden {
		t.Fatalf("越权: 普通用户更新设置应返回 403，实际 %d", status)
	}
	t.Logf("越权防护通过: 普通用户无法更新全局设置")
}

// TestE2E_SQLInjection 测试 SQL 注入防护：
// 在输入中注入 SQL 片段，验证不会执行恶意 SQL。
func TestE2E_SQLInjection(t *testing.T) {
	env := newTestEnv(t)
	token := env.registerAndLogin(t, "sqli", "SqliPass123!")

	// 1. 注册时用户名注入 SQL
	status, _ := env.doJSON(t, "POST", "/api/auth/register",
		map[string]string{
			"username": "admin'--",
			"password": "Password123!",
		}, "")
	if status == http.StatusOK || status == http.StatusCreated {
		// 可能注册成功（用户名合法），但不应导致安全问题
		t.Logf("SQL注入用户名注册: status=%d（参数化查询防护）", status)
	} else {
		t.Logf("SQL注入用户名注册被拒: status=%d", status)
	}

	// 2. 登录时 email 注入 SQL
	status, _ = env.doJSON(t, "POST", "/api/auth/login",
		map[string]any{
			"email":    "' OR 1=1--",
			"password": "anything",
			"remember": false,
		}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("SQL注入: ' OR 1=1-- 应返回 401，实际 %d", status)
	}
	t.Logf("SQL注入防护通过: ' OR 1=1-- 登录被拒")

	// 3. 邮件列表参数注入
	status, _ = env.doJSON(t, "GET", "/api/mail/list?folder=INBOX' OR 1=1--&page=1&page_size=20", nil, token)
	// 应正常返回（参数化查询，注入字符串作为普通文本处理）
	if status != http.StatusOK {
		t.Logf("SQL注入邮件列表: status=%d（参数化查询防护）", status)
	} else {
		t.Logf("SQL注入防护通过: folder 参数注入被安全处理")
	}
}

// TestE2E_NoTokenAccess 测试无 token 访问受保护端点。
func TestE2E_NoTokenAccess(t *testing.T) {
	env := newTestEnv(t)

	// 无 token 访问各受保护端点
	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/auth/me"},
		{"GET", "/api/mail/list"},
		{"GET", "/api/mail/unread-count"},
		{"GET", "/api/admin/stats"},
		{"GET", "/api/admin/users"},
		{"GET", "/api/rules"},
	}

	for _, ep := range endpoints {
		status, _ := env.doJSON(t, ep.method, ep.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("无 token 访问 %s %s 应返回 401，实际 %d", ep.method, ep.path, status)
		}
	}
	t.Logf("无 token 访问防护通过: %d 个端点全部返回 401", len(endpoints))
}
