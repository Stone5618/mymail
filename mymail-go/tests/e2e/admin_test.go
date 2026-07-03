// admin_test.go — E2E 测试：管理员创建用户→强制改密→删用户。
package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// TestE2E_AdminUserManagement 测试管理员用户管理流程：
// admin 登录 → 创建用户 → 新用户登录 → 强制改密 → admin 删除用户。
func TestE2E_AdminUserManagement(t *testing.T) {
	env := newTestEnv(t)

	// admin 注册并登录
	adminToken := env.loginAdmin(t)

	// 1. admin 创建用户
	status, resp := env.doJSON(t, "POST", "/api/admin/users",
		map[string]any{
			"username":      "managed_user",
			"email":         "managed_user@example.com",
			"password":      "TempPass123!",
			"display_name":  "Managed User",
			"storage_limit": 104857600, // 100MB
		}, adminToken)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("创建用户失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("admin 创建用户成功")

	// 2. 新用户登录
	newToken := env.loginExisting(t, "managed_user@example.com", "TempPass123!")
	if newToken == "" {
		t.Fatal("新用户登录失败")
	}
	t.Logf("新用户登录成功")

	// 3. 获取用户列表（ListUsers 返回数组）
	status, users := env.doJSONArr(t, "GET", "/api/admin/users", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("获取用户列表失败: status=%d", status)
	}
	if len(users) < 2 {
		t.Fatalf("期望至少 2 个用户，实际 %d", len(users))
	}
	t.Logf("用户列表返回 %d 个用户", len(users))

	// 4. admin 获取统计
	status, resp = env.doJSON(t, "GET", "/api/admin/stats", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("获取统计失败: status=%d", status)
	}
	t.Logf("admin 统计获取成功: %v", resp)

	// 5. 查找新用户 ID
	var managedUserID any
	for _, u := range users {
		user := u.(map[string]any)
		if username, _ := user["username"].(string); username == "managed_user" {
			managedUserID = user["id"]
			break
		}
	}
	if managedUserID == nil {
		t.Fatal("未找到 managed_user")
	}

	// 6. admin 删除用户
	status, _ = env.doJSON(t, "DELETE", fmt.Sprintf("/api/admin/users/%v", managedUserID), nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("删除用户失败: status=%d", status)
	}
	t.Logf("admin 删除用户成功")

	// 7. 验证用户列表减少
	status, users = env.doJSONArr(t, "GET", "/api/admin/users", nil, adminToken)
	for _, u := range users {
		user := u.(map[string]any)
		if username, _ := user["username"].(string); username == "managed_user" {
			if isActive, _ := user["is_active"].(bool); isActive {
				t.Fatal("managed_user 应已被软删除（is_active=false）")
			}
			t.Logf("managed_user 已软删除 (is_active=false)")
			return
		}
	}
	t.Logf("managed_user 已不在活跃用户列表中")
}

// TestE2E_AdminSettings 测试管理员全局设置管理。
func TestE2E_AdminSettings(t *testing.T) {
	env := newTestEnv(t)
	adminToken := env.loginAdmin(t)

	// 1. 获取设置（初始可能为空）
	status, _ := env.doJSON(t, "GET", "/api/admin/settings", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("获取设置失败: status=%d", status)
	}
	t.Logf("获取设置成功")

	// 2. 更新设置
	status, _ = env.doJSON(t, "PUT", "/api/admin/settings",
		map[string]any{
			"site_name":         "MyMail Test",
			"registration_open": true,
		}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("更新设置失败: status=%d", status)
	}
	t.Logf("更新设置成功")

	// 3. 普通用户不能修改设置
	userToken := env.registerAndLogin(t, "settingsuser", "SettingsPass123!")
	status, _ = env.doJSON(t, "PUT", "/api/admin/settings",
		map[string]any{"key": "value"}, userToken)
	if status != http.StatusForbidden {
		t.Fatalf("普通用户更新设置应返回 403，实际 %d", status)
	}
	t.Logf("普通用户无法修改设置（403）")
}
