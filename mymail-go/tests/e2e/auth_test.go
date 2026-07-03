// auth_test.go — E2E 测试：注册→登录→改密→登出 完整流程。
package e2e

import (
	"net/http"
	"testing"
)

// TestE2E_AuthFlow 测试完整认证流程：
// 注册 → 登录 → 获取 me → 修改密码 → 用新密码登录 → 修改资料。
func TestE2E_AuthFlow(t *testing.T) {
	env := newTestEnv(t)

	// 1. 注册
	status, resp := env.doJSON(t, "POST", "/api/auth/register",
		map[string]string{"username": "alice", "password": "AlicePass123!"}, "")
	if status != http.StatusCreated {
		t.Fatalf("注册失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("注册成功: %v", resp)

	// 2. 登录
	status, resp = env.doJSON(t, "POST", "/api/auth/login",
		map[string]any{"email": "alice@example.com", "password": "AlicePass123!", "remember": false}, "")
	if status != http.StatusOK {
		t.Fatalf("登录失败: status=%d, resp=%v", status, resp)
	}
	token, _ := resp["token"].(string)
	if token == "" {
		t.Fatal("登录响应中无 token")
	}
	t.Logf("登录成功，获取到 token")

	// 3. 获取 me
	status, resp = env.doJSON(t, "GET", "/api/auth/me", nil, token)
	if status != http.StatusOK {
		t.Fatalf("获取 me 失败: status=%d, resp=%v", status, resp)
	}
	if username, _ := resp["username"].(string); username != "alice" {
		t.Fatalf("username 期望 alice，实际 %s", username)
	}
	if role, _ := resp["role"].(string); role != "admin" {
		t.Fatalf("首个用户应为 admin，实际 role=%s", role)
	}
	t.Logf("me 验证通过: username=alice, role=admin")

	// 4. 修改密码
	status, resp = env.doJSON(t, "PUT", "/api/auth/password",
		map[string]string{"currentPassword": "AlicePass123!", "newPassword": "NewAlicePass456!"}, token)
	if status != http.StatusOK {
		t.Fatalf("修改密码失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("密码修改成功")

	// 5. 用新密码登录
	status, resp = env.doJSON(t, "POST", "/api/auth/login",
		map[string]any{"email": "alice@example.com", "password": "NewAlicePass456!", "remember": false}, "")
	if status != http.StatusOK {
		t.Fatalf("新密码登录失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("新密码登录成功")

	// 6. 修改资料
	newToken, _ := resp["token"].(string)
	status, resp = env.doJSON(t, "PUT", "/api/auth/profile",
		map[string]string{"display_name": "Alice Updated"}, newToken)
	if status != http.StatusOK {
		t.Fatalf("修改资料失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("资料修改成功")
}

// TestE2E_AuthErrors 测试认证错误场景：
// 重复注册、错误密码、无 token 访问、无效 token。
func TestE2E_AuthErrors(t *testing.T) {
	env := newTestEnv(t)

	// 注册第一个用户
	env.registerAndLogin(t, "bob", "BobPass123!")

	// 1. 重复注册同一用户名
	status, _ := env.doJSON(t, "POST", "/api/auth/register",
		map[string]string{"username": "bob", "password": "AnotherPass123!"}, "")
	if status != http.StatusConflict {
		t.Fatalf("重复注册应返回 409，实际 %d", status)
	}
	t.Logf("重复注册正确返回 409")

	// 2. 错误密码登录
	status, _ = env.doJSON(t, "POST", "/api/auth/login",
		map[string]any{"email": "bob@example.com", "password": "WrongPassword!", "remember": false}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("错误密码应返回 401，实际 %d", status)
	}
	t.Logf("错误密码正确返回 401")

	// 3. 无 token 访问受保护端点
	status, _ = env.doJSON(t, "GET", "/api/auth/me", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("无 token 应返回 401，实际 %d", status)
	}
	t.Logf("无 token 正确返回 401")

	// 4. 无效 token
	status, _ = env.doJSON(t, "GET", "/api/auth/me", nil, "invalid.token.here")
	if status != http.StatusUnauthorized {
		t.Fatalf("无效 token 应返回 401，实际 %d", status)
	}
	t.Logf("无效 token 正确返回 401")
}

// TestE2E_FirstUserIsAdmin 验证首个注册用户自动成为 admin，
// 后续注册用户为普通 user。
func TestE2E_FirstUserIsAdmin(t *testing.T) {
	env := newTestEnv(t)

	// 首个用户 → admin
	token1 := env.registerAndLogin(t, "first", "FirstPass123!")
	me1 := env.getMe(t, token1)
	if role, _ := me1["role"].(string); role != "admin" {
		t.Fatalf("首个用户应为 admin，实际 %s", role)
	}
	t.Logf("首个用户正确成为 admin")

	// 第二个用户 → user
	token2 := env.registerAndLogin(t, "second", "SecondPass123!")
	me2 := env.getMe(t, token2)
	if role, _ := me2["role"].(string); role != "user" {
		t.Fatalf("第二个用户应为 user，实际 %s", role)
	}
	t.Logf("第二个用户正确为 user")
}
