// bench_test.go — E2E 性能基线测试：测量关键 HTTP 端点的吞吐与延迟。
//
// 运行：go test ./tests/e2e/ -bench=. -benchmem -count=3
//
// 基线指标（验收标准）：
//   - 登录：≥ 100 QPS（bcrypt 成本 10 下单请求 ~80ms）
//   - 邮件列表：≥ 1000 QPS（索引 + 分页）
//   - 邮件详情：≥ 1000 QPS（带归属校验）
//   - 注册：≥ 50 QPS（含 bcrypt + maildir 创建）
package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// BenchmarkLogin 测量登录吞吐（含 bcrypt 校验，预期 ~100 QPS）。
func BenchmarkLogin(b *testing.B) {
	env := newTestEnv(b)
	env.registerAndLogin(b, "benchuser", "BenchPass123!")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		status, _ := env.doJSON(b, "POST", "/api/auth/login",
			map[string]any{
				"email":    "benchuser@example.com",
				"password": "BenchPass123!",
				"remember": false,
			}, "")
		if status != http.StatusOK {
			b.Fatalf("登录失败: status=%d", status)
		}
	}
}

// BenchmarkMailList 测量邮件列表查询吞吐（无 bcrypt，预期 ≥1000 QPS）。
func BenchmarkMailList(b *testing.B) {
	env := newTestEnv(b)
	token := env.registerAndLogin(b, "listuser", "ListPass123!")
	me := env.getMe(b, token)
	userID := int64(me["id"].(float64))

	// 预插入 20 封邮件
	for i := 1; i <= 20; i++ {
		env.insertTestMessage(b, userID, fmt.Sprintf("基准测试邮件 %d", i), "正文")
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		status, _ := env.doJSON(b, "GET", "/api/mail/list?folder=INBOX&page=1&limit=20", nil, token)
		if status != http.StatusOK {
			b.Fatalf("列表查询失败: status=%d", status)
		}
	}
}

// BenchmarkMailDetail 测量邮件详情查询吞吐（含归属校验 + 标记已读副作用）。
func BenchmarkMailDetail(b *testing.B) {
	env := newTestEnv(b)
	token := env.registerAndLogin(b, "detailuser", "DetailPass123!")
	me := env.getMe(b, token)
	userID := int64(me["id"].(float64))

	// 预插入 1 封邮件
	mailID := env.insertTestMessage(b, userID, "详情基准", "正文")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		status, _ := env.doJSON(b, "GET", fmt.Sprintf("/api/mail/%d", mailID), nil, token)
		if status != http.StatusOK {
			b.Fatalf("详情查询失败: status=%d", status)
		}
	}
}

// BenchmarkRegister 测量注册吞吐（含 bcrypt + maildir 创建，预期 ~50 QPS）。
func BenchmarkRegister(b *testing.B) {
	env := newTestEnv(b)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		username := fmt.Sprintf("benchreg%d", i)
		status, _ := env.doJSON(b, "POST", "/api/auth/register",
			map[string]string{
				"username": username,
				"password": "BenchRegPass123!",
			}, "")
		if status != http.StatusCreated {
			b.Fatalf("注册失败: status=%d", status)
		}
	}
}

// BenchmarkUnreadCount 测量未读计数查询吞吐（4 个文件夹 COUNT 查询）。
func BenchmarkUnreadCount(b *testing.B) {
	env := newTestEnv(b)
	token := env.registerAndLogin(b, "unreaduser", "UnreadPass123!")
	me := env.getMe(b, token)
	userID := int64(me["id"].(float64))

	// 预插入 5 封邮件
	for i := 1; i <= 5; i++ {
		env.insertTestMessage(b, userID, fmt.Sprintf("未读 %d", i), "正文")
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		status, _ := env.doJSON(b, "GET", "/api/mail/unread-count", nil, token)
		if status != http.StatusOK {
			b.Fatalf("未读计数失败: status=%d", status)
		}
	}
}
