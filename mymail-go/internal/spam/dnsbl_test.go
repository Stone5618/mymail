// dnsbl_test.go 测试 DNSBL 查询器。
//
// 覆盖：
//   - reverseIPv4 IP 反转
//   - Check 方法（用 mock resolver）：命中/未命中/多 zone/IPv6/无效 IP
package spam

import (
	"context"
	"net"
	"testing"
)

func TestReverseIPv4(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want string
	}{
		{"1.2.3.4", net.ParseIP("1.2.3.4").To4(), "4.3.2.1"},
		{"192.168.1.100", net.ParseIP("192.168.1.100").To4(), "100.1.168.192"},
		{"10.0.0.1", net.ParseIP("10.0.0.1").To4(), "1.0.0.10"},
		{"0.0.0.0", net.ParseIP("0.0.0.0").To4(), "0.0.0.0"},
		{"255.255.255.255", net.ParseIP("255.255.255.255").To4(), "255.255.255.255"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reverseIPv4(tt.ip)
			if got != tt.want {
				t.Errorf("reverseIPv4(%v) = %q, want %q", tt.ip, got, tt.want)
			}
		})
	}
}

func TestReverseIPv4_NilIP(t *testing.T) {
	got := reverseIPv4(nil)
	if got != "" {
		t.Errorf("reverseIPv4(nil) 期望空字符串，实际 %q", got)
	}
}

func TestDNSBLChecker_Check_NotListed(t *testing.T) {
	mr := newMockResolver()
	// 不设置任何 ipAddrs → 所有查询返回 NXDOMAIN
	c := newDNSBLCheckerWithResolver(mr, []string{"zen.spamhaus.org"}, 1000)

	r := c.Check(context.Background(), "1.2.3.4")
	if r.Listed {
		t.Errorf("未命中黑名单期望 Listed=false，实际 true (zones: %v)", r.Zones)
	}
	if len(r.Zones) != 0 {
		t.Errorf("未命中时 Zones 应为空，实际 %v", r.Zones)
	}
}

func TestDNSBLChecker_Check_Listed(t *testing.T) {
	mr := newMockResolver()
	// 模拟 zen.spamhaus.org 命中
	mr.ipAddrs["4.3.2.1.zen.spamhaus.org"] = []net.IPAddr{
		{IP: net.ParseIP("127.0.0.2")},
	}
	c := newDNSBLCheckerWithResolver(mr, []string{"zen.spamhaus.org"}, 1000)

	r := c.Check(context.Background(), "1.2.3.4")
	if !r.Listed {
		t.Error("期望 Listed=true")
	}
	if len(r.Zones) != 1 || r.Zones[0] != "zen.spamhaus.org" {
		t.Errorf("期望 Zones=[zen.spamhaus.org]，实际 %v", r.Zones)
	}
}

func TestDNSBLChecker_Check_MultipleZonesListed(t *testing.T) {
	mr := newMockResolver()
	mr.ipAddrs["4.3.2.1.zen.spamhaus.org"] = []net.IPAddr{{IP: net.ParseIP("127.0.0.2")}}
	mr.ipAddrs["4.3.2.1.bl.spamcop.net"] = []net.IPAddr{{IP: net.ParseIP("127.0.0.2")}}
	c := newDNSBLCheckerWithResolver(mr, []string{"zen.spamhaus.org", "bl.spamcop.net", "b.barracudacentral.org"}, 1000)

	r := c.Check(context.Background(), "1.2.3.4")
	if !r.Listed {
		t.Error("期望 Listed=true")
	}
	if len(r.Zones) != 2 {
		t.Errorf("期望命中 2 个 zone，实际 %d (%v)", len(r.Zones), r.Zones)
	}
}

func TestDNSBLChecker_Check_InvalidIP(t *testing.T) {
	mr := newMockResolver()
	c := newDNSBLCheckerWithResolver(mr, []string{"zen.spamhaus.org"}, 1000)

	r := c.Check(context.Background(), "invalid-ip")
	if r.Listed {
		t.Error("无效 IP 期望 Listed=false")
	}
}

func TestDNSBLChecker_Check_IPv6NotSupported(t *testing.T) {
	mr := newMockResolver()
	c := newDNSBLCheckerWithResolver(mr, []string{"zen.spamhaus.org"}, 1000)

	// IPv6 应直接返回未命中（当前实现仅支持 IPv4）
	r := c.Check(context.Background(), "::1")
	if r.Listed {
		t.Error("IPv6 期望 Listed=false")
	}
}

func TestDNSBLChecker_Check_EmptyZones(t *testing.T) {
	mr := newMockResolver()
	// 空 zones 列表应使用默认 3 个 zone
	c := newDNSBLCheckerWithResolver(mr, nil, 1000)

	if len(c.zones) != 3 {
		t.Errorf("默认 zones 期望 3 个，实际 %d", len(c.zones))
	}
	r := c.Check(context.Background(), "1.2.3.4")
	if r.Listed {
		t.Error("无 mock 数据期望 Listed=false")
	}
}

func TestNewDNSBLChecker_Defaults(t *testing.T) {
	c := NewDNSBLChecker(nil, nil, 0)
	if len(c.zones) != 3 {
		t.Errorf("默认 zones 期望 3 个，实际 %d", len(c.zones))
	}
	if c.timeoutMs != 3000 {
		t.Errorf("默认 timeoutMs 期望 3000，实际 %d", c.timeoutMs)
	}
	if c.resolver == nil {
		t.Error("默认 resolver 不应为 nil")
	}
}
