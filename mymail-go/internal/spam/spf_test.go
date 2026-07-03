// spf_test.go 测试 SPF 校验器。
//
// 覆盖：
//   - 纯函数：matchIPv4CIDR / matchIPv6CIDR / isModifier
//   - Check 方法（用 mock resolver）：各种 SPF 机制 + 错误场景
//   - 递归深度限制
package spam

import (
	"context"
	"net"
	"testing"
)

// ============ 纯函数测试 ============

func TestMatchIPv4CIDR(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		cidr string
		want bool
	}{
		// 精确匹配（无掩码）
		{"exact match", net.ParseIP("1.2.3.4"), "1.2.3.4", true},
		{"exact mismatch", net.ParseIP("1.2.3.5"), "1.2.3.4", false},
		// /24 网段
		{"/24 in range", net.ParseIP("192.168.1.100"), "192.168.1.0/24", true},
		{"/24 out of range", net.ParseIP("192.168.2.100"), "192.168.1.0/24", false},
		// /16 网段
		{"/16 in range", net.ParseIP("10.0.5.10"), "10.0.0.0/16", true},
		{"/16 out of range", net.ParseIP("10.1.5.10"), "10.0.0.0/16", false},
		// /8 网段
		{"/8 in range", net.ParseIP("172.16.0.1"), "172.0.0.0/8", true},
		// /32 显式
		{"/32 exact", net.ParseIP("1.1.1.1"), "1.1.1.1/32", true},
		{"/32 mismatch", net.ParseIP("1.1.1.2"), "1.1.1.1/32", false},
		// 无效 CIDR
		{"invalid cidr", net.ParseIP("1.2.3.4"), "invalid", false},
		// IPv6 地址传给 ip4
		{"ipv6 to ipv4", net.ParseIP("::1"), "1.2.3.4", false},
		// nil IP
		{"nil ip", nil, "1.2.3.4", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchIPv4CIDR(tt.ip, tt.cidr)
			if got != tt.want {
				t.Errorf("matchIPv4CIDR(%v, %q) = %v, want %v", tt.ip, tt.cidr, got, tt.want)
			}
		})
	}
}

func TestMatchIPv6CIDR(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		cidr string
		want bool
	}{
		// 精确匹配
		{"exact match", net.ParseIP("::1"), "::1", true},
		{"exact mismatch", net.ParseIP("::2"), "::1", false},
		// /64 网段
		{"/64 in range", net.ParseIP("2001:db8::1"), "2001:db8::/64", true},
		{"/64 out of range", net.ParseIP("2001:db9::1"), "2001:db8::/64", false},
		// /128 显式
		{"/128 exact", net.ParseIP("::1"), "::1/128", true},
		// 无效 CIDR
		{"invalid cidr", net.ParseIP("::1"), "invalid", false},
		// IPv4 地址传给 ip6（应失败）
		{"ipv4 to ipv6", net.ParseIP("1.2.3.4"), "::1/128", false},
		{"ipv4 exact to ipv6", net.ParseIP("1.2.3.4"), "1.2.3.4", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchIPv6CIDR(tt.ip, tt.cidr)
			if got != tt.want {
				t.Errorf("matchIPv6CIDR(%v, %q) = %v, want %v", tt.ip, tt.cidr, got, tt.want)
			}
		})
	}
}

func TestIsModifier(t *testing.T) {
	tests := []struct {
		mech string
		want bool
	}{
		{"redirect=example.com", true},
		{"exp=explanation.example.com", true},
		{"ip4:192.168.1.0/24", false}, // ip4 机制即使有 = 也不是修饰符
		{"ip6:::1", false},
		{"include:example.com", false},
		{"mx", false},
		{"a:example.com", false},
		{"-all", false},
		{"~all", false},
		{"all", false},
		{"exists:example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.mech, func(t *testing.T) {
			got := isModifier(tt.mech)
			if got != tt.want {
				t.Errorf("isModifier(%q) = %v, want %v", tt.mech, got, tt.want)
			}
		})
	}
}

// ============ SPFChecker.Check 测试（mock resolver） ============

func TestSPFChecker_Check_EmptyDomain(t *testing.T) {
	c := NewSPFChecker(nil, 10, 1000)
	r := c.Check(context.Background(), "1.2.3.4", "", "helo.example.com")
	if r.Result != SPFResultNone {
		t.Errorf("空域名期望 none，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_InvalidIP(t *testing.T) {
	c := NewSPFChecker(nil, 10, 1000)
	r := c.Check(context.Background(), "invalid-ip", "example.com", "helo.example.com")
	if r.Result != SPFResultError {
		t.Errorf("无效 IP 期望 error，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_NoTXTRecord(t *testing.T) {
	mr := newMockResolver()
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	r := c.Check(context.Background(), "1.2.3.4", "noexample.com", "helo.example.com")
	if r.Result != SPFResultNone {
		t.Errorf("无 TXT 记录期望 none，实际 %s (detail: %s)", r.Result, r.Detail)
	}
}

func TestSPFChecker_Check_NoSPFRecord(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"some other txt record"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	r := c.Check(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if r.Result != SPFResultNone {
		t.Errorf("无 SPF 记录期望 none，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_IPv4Match(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 ip4:192.168.1.0/24 -all"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// 在网段内 → pass
	r := c.Check(context.Background(), "192.168.1.100", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("网段内 IP 期望 pass，实际 %s (detail: %s)", r.Result, r.Detail)
	}

	// 不在网段内 → fail（-all）
	r = c.Check(context.Background(), "10.0.0.1", "example.com", "helo.example.com")
	if r.Result != SPFResultFail {
		t.Errorf("网段外 IP 期望 fail，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_IPv4ExactMatch(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 ip4:1.2.3.4 -all"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// 精确匹配
	r := c.Check(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("精确匹配期望 pass，实际 %s", r.Result)
	}

	// 不匹配
	r = c.Check(context.Background(), "1.2.3.5", "example.com", "helo.example.com")
	if r.Result != SPFResultFail {
		t.Errorf("不匹配期望 fail，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_IPv6Match(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 ip6:2001:db8::/32 -all"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// IPv6 在网段内
	r := c.Check(context.Background(), "2001:db8::1", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("IPv6 网段内期望 pass，实际 %s (detail: %s)", r.Result, r.Detail)
	}

	// IPv6 不在网段内
	r = c.Check(context.Background(), "2001:db9::1", "example.com", "helo.example.com")
	if r.Result != SPFResultFail {
		t.Errorf("IPv6 网段外期望 fail，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_AllMechanisms(t *testing.T) {
	tests := []struct {
		name       string
		spf        string
		ip         string
		wantResult string
	}{
		{"-all → fail", "v=spf1 -all", "1.2.3.4", SPFResultFail},
		{"~all → softfail", "v=spf1 ~all", "1.2.3.4", SPFResultSoftFail},
		{"?all → neutral", "v=spf1 ?all", "1.2.3.4", SPFResultNeutral},
		{"all → pass", "v=spf1 all", "1.2.3.4", SPFResultPass},
		{"+all → pass", "v=spf1 +all", "1.2.3.4", SPFResultPass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := newMockResolver()
			mr.txtRecords["example.com"] = []string{tt.spf}
			c := newSPFCheckerWithResolver(mr, 10, 1000)
			r := c.Check(context.Background(), tt.ip, "example.com", "helo.example.com")
			if r.Result != tt.wantResult {
				t.Errorf("期望 %s，实际 %s (detail: %s)", tt.wantResult, r.Result, r.Detail)
			}
		})
	}
}

func TestSPFChecker_Check_IncludeMatch(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 include:spf.example.com -all"}
	mr.txtRecords["spf.example.com"] = []string{"v=spf1 ip4:192.168.1.0/24 -all"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// 通过 include 匹配
	r := c.Check(context.Background(), "192.168.1.50", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("include 匹配期望 pass，实际 %s (detail: %s)", r.Result, r.Detail)
	}

	// include 不匹配 → 主记录 -all → fail
	r = c.Check(context.Background(), "10.0.0.1", "example.com", "helo.example.com")
	if r.Result != SPFResultFail {
		t.Errorf("include 不匹配后期望 fail，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_IncludeMaxDepth(t *testing.T) {
	// 构造循环引用：a → b → a → b → ...
	// 深度限制应中断递归，include 不匹配，继续走 -all → fail（非 error）
	mr := newMockResolver()
	mr.txtRecords["a.example.com"] = []string{"v=spf1 include:b.example.com -all"}
	mr.txtRecords["b.example.com"] = []string{"v=spf1 include:a.example.com -all"}
	c := newSPFCheckerWithResolver(mr, 3, 1000) // 深度限制 3

	r := c.Check(context.Background(), "1.2.3.4", "a.example.com", "helo.example.com")
	// 深度耗尽后 include 返回 error，但 include 机制仅在 pass 时匹配
	// 继续走 -all → fail
	if r.Result != SPFResultFail {
		t.Errorf("循环引用+深度限制后期望 fail（走 -all），实际 %s (detail: %s)", r.Result, r.Detail)
	}
}

func TestSPFChecker_Check_MXMechanism(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 mx -all"}
	mr.mxRecords["example.com"] = []*net.MX{
		{Host: "mail.example.com", Pref: 10},
	}
	mr.ipAddrs["mail.example.com"] = []net.IPAddr{
		{IP: net.ParseIP("192.168.1.1")},
	}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// IP 匹配 MX 记录的 A 记录
	r := c.Check(context.Background(), "192.168.1.1", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("MX 匹配期望 pass，实际 %s (detail: %s)", r.Result, r.Detail)
	}

	// IP 不匹配
	r = c.Check(context.Background(), "10.0.0.1", "example.com", "helo.example.com")
	if r.Result != SPFResultFail {
		t.Errorf("MX 不匹配期望 fail，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_AMechanism(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 a -all"}
	mr.ipAddrs["example.com"] = []net.IPAddr{
		{IP: net.ParseIP("192.168.1.1")},
	}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// IP 匹配 A 记录
	r := c.Check(context.Background(), "192.168.1.1", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("A 匹配期望 pass，实际 %s (detail: %s)", r.Result, r.Detail)
	}

	// IP 不匹配
	r = c.Check(context.Background(), "10.0.0.1", "example.com", "helo.example.com")
	if r.Result != SPFResultFail {
		t.Errorf("A 不匹配期望 fail，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_ExistsMechanism(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 exists:check.example.com -all"}
	mr.ipAddrs["check.example.com"] = []net.IPAddr{
		{IP: net.ParseIP("127.0.0.1")},
	}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// exists: 域名有 A 记录 → 匹配
	r := c.Check(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("exists 匹配期望 pass，实际 %s (detail: %s)", r.Result, r.Detail)
	}
}

func TestSPFChecker_Check_NoAllMechanism(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1 ip4:192.168.1.0/24"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// 无 all 机制：默认 neutral
	r := c.Check(context.Background(), "10.0.0.1", "example.com", "helo.example.com")
	if r.Result != SPFResultNeutral {
		t.Errorf("无 all 期望 neutral，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_EmptySPFRecord(t *testing.T) {
	mr := newMockResolver()
	mr.txtRecords["example.com"] = []string{"v=spf1"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	// 仅有 v=spf1，无机制
	r := c.Check(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if r.Result != SPFResultNeutral {
		t.Errorf("空 SPF 期望 neutral，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_Qualifiers(t *testing.T) {
	// SPF 限定符前缀（+/~/?/-）仅在机制【匹配】时才决定结果。
	// 不匹配时继续走后续机制。
	tests := []struct {
		name       string
		spf        string
		ip         string
		wantResult string
	}{
		// IP 匹配 ip4 但前缀不同
		{"+ip4 match → pass", "v=spf1 +ip4:1.2.3.4 -all", "1.2.3.4", SPFResultPass},
		{"-ip4 match → fail", "v=spf1 -ip4:1.2.3.4 ~all", "1.2.3.4", SPFResultFail},
		{"~ip4 match → softfail", "v=spf1 ~ip4:1.2.3.4 -all", "1.2.3.4", SPFResultSoftFail},
		{"?ip4 match → neutral", "v=spf1 ?ip4:1.2.3.4 -all", "1.2.3.4", SPFResultNeutral},
		// IP 不匹配 ip4，前缀不生效，继续走 -all
		{"~ip4 not match → walk to -all", "v=spf1 ~ip4:10.0.0.1 -all", "1.2.3.4", SPFResultFail},
		{"?ip4 not match → walk to -all", "v=spf1 ?ip4:10.0.0.1 -all", "1.2.3.4", SPFResultFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := newMockResolver()
			mr.txtRecords["example.com"] = []string{tt.spf}
			c := newSPFCheckerWithResolver(mr, 10, 1000)
			r := c.Check(context.Background(), tt.ip, "example.com", "helo.example.com")
			if r.Result != tt.wantResult {
				t.Errorf("期望 %s，实际 %s", tt.wantResult, r.Result)
			}
		})
	}
}

func TestSPFChecker_Check_ModifierSkipped(t *testing.T) {
	mr := newMockResolver()
	// redirect= 修饰符应被跳过，继续匹配后续机制
	mr.txtRecords["example.com"] = []string{"v=spf1 redirect=other.com ip4:1.2.3.4 -all"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	r := c.Check(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("跳过 redirect 后 ip4 匹配期望 pass，实际 %s", r.Result)
	}
}

func TestSPFChecker_Check_UnknownMechanismSkipped(t *testing.T) {
	mr := newMockResolver()
	// 未知机制 ptr 应被跳过
	mr.txtRecords["example.com"] = []string{"v=spf1 ptr ip4:1.2.3.4 -all"}
	c := newSPFCheckerWithResolver(mr, 10, 1000)

	r := c.Check(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if r.Result != SPFResultPass {
		t.Errorf("跳过未知机制后 ip4 匹配期望 pass，实际 %s", r.Result)
	}
}

func TestNewSPFChecker_Defaults(t *testing.T) {
	c := NewSPFChecker(nil, 0, 0)
	if c.maxDepth != 10 {
		t.Errorf("默认 maxDepth 期望 10，实际 %d", c.maxDepth)
	}
	if c.timeoutMs != 3000 {
		t.Errorf("默认 timeoutMs 期望 3000，实际 %d", c.timeoutMs)
	}
	if c.resolver == nil {
		t.Error("默认 resolver 不应为 nil")
	}
}
