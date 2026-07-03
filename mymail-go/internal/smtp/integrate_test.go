// integrate_test.go 测试阶段 4 集成相关函数与方法。
//
// 覆盖：
//   - extractDomain 辅助函数（handler.go 中用于反垃圾 senderDomain 提取）
//   - Receiver.SetAntiSpam 注入反垃圾组件
//   - Receiver.RateLimiter() getter
package smtp

import (
	"testing"
)

// TestExtractDomain 测试 extractDomain 从邮箱地址提取域名。
func TestExtractDomain(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want string
	}{
		{"标准邮箱", "user@example.com", "example.com"},
		{"另一域名", "alice@example.org", "example.org"},
		{"空字符串", "", ""},
		{"无@符号", "noatdomain", ""},
		{"尾部@无域名", "trailing@", ""},
		{"仅@域名", "@leading.com", "leading.com"},
		{"多@取最后", "a@b@c.com", "c.com"},
		{"含子域名", "user@mail.sub.example.com", "mail.sub.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractDomain(c.addr)
			if got != c.want {
				t.Errorf("extractDomain(%q) = %q, want %q", c.addr, got, c.want)
			}
		})
	}
}

// TestSetAntiSpam 测试 SetAntiSpam 注入反垃圾组件 + RateLimiter getter。
func TestSetAntiSpam(t *testing.T) {
	env := newSMTPTestEnv(t)

	// 初始状态：RateLimiter() 应为 nil（未注入）
	if env.receiver.RateLimiter() != nil {
		t.Fatal("初始 RateLimiter() 应为 nil")
	}

	// 注入限流器（灰名单/验证器传 nil 表示禁用）
	rl := NewRateLimiter(10, 60000)
	env.receiver.SetAntiSpam(nil, rl, nil)

	// 验证 getter 返回注入的实例
	if got := env.receiver.RateLimiter(); got != rl {
		t.Error("SetAntiSpam 后 RateLimiter() 应返回注入的限流器实例")
	}

	// 再次注入新限流器，验证覆盖
	rl2 := NewRateLimiter(5, 30000)
	env.receiver.SetAntiSpam(nil, rl2, nil)
	if got := env.receiver.RateLimiter(); got != rl2 {
		t.Error("再次 SetAntiSpam 后 RateLimiter() 应返回新注入的实例")
	}
}

// TestSetAntiSpam_NilComponents 测试注入全 nil 组件（全部禁用反垃圾）。
func TestSetAntiSpam_NilComponents(t *testing.T) {
	env := newSMTPTestEnv(t)

	// 全部传 nil（等效于禁用所有反垃圾功能）
	env.receiver.SetAntiSpam(nil, nil, nil)

	if env.receiver.RateLimiter() != nil {
		t.Error("注入 nil 限流器后 RateLimiter() 应为 nil")
	}
}
