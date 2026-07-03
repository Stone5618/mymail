// apikey_test.go — E2E 测试：API Key 创建→用 Key 发信。
package e2e

import (
	"net/http"
	"testing"
)

// TestE2E_APIKeyFlow 测试 API Key 完整流程：
// 登录 → 创建 API Key → 列出 Key → 用 API Key 调用 /api/v1/send → 删除 Key。
func TestE2E_APIKeyFlow(t *testing.T) {
	env := newTestEnv(t)
	token := env.registerAndLogin(t, "apikeyuser", "ApiKeyPass123!")

	// 1. 创建 API Key（DTO: name + scopes 数组）
	status, resp := env.doJSON(t, "POST", "/api/auth/api-keys",
		map[string]any{
			"name":   "test-key",
			"scopes": []string{"send"},
		}, token)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("创建 API Key 失败: status=%d, resp=%v", status, resp)
	}
	plainKey, _ := resp["plain_text"].(string)
	if plainKey == "" {
		t.Fatal("创建 API Key 响应中无明文 plain_text")
	}
	keyID, _ := resp["id"].(float64)
	t.Logf("API Key 创建成功: id=%v, key=%s...", keyID, plainKey[:8])

	// 2. 列出 API Key（返回数组，不是 {items:[...]}）
	status, arr := env.doJSONArr(t, "GET", "/api/auth/api-keys", nil, token)
	if status != http.StatusOK {
		t.Fatalf("列出 API Key 失败: status=%d", status)
	}
	if len(arr) < 1 {
		t.Fatalf("期望至少 1 个 API Key，实际 %d", len(arr))
	}
	t.Logf("列出 API Key: %d 个", len(arr))

	// 3. 用 API Key 调用 /api/v1/send（DTO: to 为 []string）
	env.registerAndLogin(t, "recipient", "RecipientPass123!")
	status, resp = env.doJSON(t, "POST", "/api/v1/send",
		map[string]any{
			"to":        []string{"recipient@example.com"},
			"subject":   "API Key 发信测试",
			"body_text": "这是通过 API Key 发送的测试邮件",
		}, plainKey)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("API Key 发信失败: status=%d, resp=%v", status, resp)
	}
	t.Logf("API Key 发信成功: status=%d", status)

	// 4. 删除 API Key
	if keyID > 0 {
		status, _ = env.doJSON(t, "DELETE", "/api/auth/api-keys/"+itoa(int(keyID)), nil, token)
		if status != http.StatusOK {
			t.Fatalf("删除 API Key 失败: status=%d", status)
		}
		t.Logf("API Key 删除成功")
	}

	// 5. 验证已删除的 Key 无法发信
	status, _ = env.doJSON(t, "POST", "/api/v1/send",
		map[string]any{
			"to":        []string{"recipient@example.com"},
			"subject":   "应失败的发信",
			"body_text": "此邮件不应发送成功",
		}, plainKey)
	if status != http.StatusUnauthorized {
		t.Fatalf("已删除的 API Key 发信应返回 401，实际 %d", status)
	}
	t.Logf("已删除的 API Key 正确被拒绝（401）")
}

// itoa 简单整数转字符串。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
