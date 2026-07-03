// dnsbl.go 实现 DNSBL (DNS-based Blackhole List) 并行查询。
//
// 原理：
//   - 将发件人 IP 反转（如 1.2.3.4 → 4.3.2.1）
//   - 拼接 DNSBL zone（如 4.3.2.1.zen.spamhaus.org）
//   - 查询 A 记录：若存在则命中黑名单
//   - 并行查询多个 zone，提高效率
//
// 默认 zone 列表（与原 Node.js smtp-validator.js 一致）：
//   - zen.spamhaus.org
//   - bl.spamcop.net
//   - b.barracudacentral.org
package spam

import (
	"context"
	"net"
	"sync"
	"time"
)

// DNSBLResult DNSBL 查询结果。
type DNSBLResult struct {
	Listed bool     // 是否命中任一黑名单
	Zones  []string // 命中的 zone 列表
}

// DNSBLChecker DNSBL 查询器。
type DNSBLChecker struct {
	resolver  dnsResolver
	zones     []string
	timeoutMs int
}

// NewDNSBLChecker 创建 DNSBL 查询器。
//
// 参数：
//   - resolver: DNS 解析器（传 nil 则用默认解析器）
//   - zones: DNSBL zone 列表（为空时用默认 3 个 zone）
//   - timeoutMs: 单次查询总超时（<= 0 时默认 3000ms）
func NewDNSBLChecker(resolver *net.Resolver, zones []string, timeoutMs int) *DNSBLChecker {
	if len(zones) == 0 {
		zones = []string{
			"zen.spamhaus.org",
			"bl.spamcop.net",
			"b.barracudacentral.org",
		}
	}
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &DNSBLChecker{
		resolver:  resolver,
		zones:     zones,
		timeoutMs: timeoutMs,
	}
}

// newDNSBLCheckerWithResolver 内部构造器，接受 dnsResolver 接口（用于测试 mock）。
func newDNSBLCheckerWithResolver(resolver dnsResolver, zones []string, timeoutMs int) *DNSBLChecker {
	if len(zones) == 0 {
		zones = []string{
			"zen.spamhaus.org",
			"bl.spamcop.net",
			"b.barracudacentral.org",
		}
	}
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &DNSBLChecker{
		resolver:  resolver,
		zones:     zones,
		timeoutMs: timeoutMs,
	}
}

// Check 并行查询所有 zone。
//
// 参数：
//   - ip: 发件人 IP（仅 IPv4，IPv6 直接返回未命中）
func (c *DNSBLChecker) Check(ctx context.Context, ip string) DNSBLResult {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return DNSBLResult{Listed: false, Zones: nil}
	}
	// 仅支持 IPv4 查询（DNSBL 主要针对 IPv4）
	ipv4 := parsed.To4()
	if ipv4 == nil {
		return DNSBLResult{Listed: false, Zones: nil}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.timeoutMs)*time.Millisecond)
	defer cancel()

	// 反转 IP
	reversed := reverseIPv4(ipv4)

	var (
		mu       sync.Mutex
		listed   []string
		wg       sync.WaitGroup
	)

	for _, zone := range c.zones {
		wg.Add(1)
		go func(z string) {
			defer wg.Done()
			if c.queryZone(ctx, reversed, z) {
				mu.Lock()
				listed = append(listed, z)
				mu.Unlock()
			}
		}(zone)
	}
	wg.Wait()

	return DNSBLResult{
		Listed: len(listed) > 0,
		Zones:  listed,
	}
}

// queryZone 查询单个 zone。
// 返回 true 表示命中（A 记录存在）。
func (c *DNSBLChecker) queryZone(ctx context.Context, reversedIP, zone string) bool {
	query := reversedIP + "." + zone
	// 查询 A 记录：存在即命中
	_, err := c.resolver.LookupIPAddr(ctx, query)
	return err == nil
}

// reverseIPv4 将 IPv4 反转（如 1.2.3.4 → 4.3.2.1）。
func reverseIPv4(ip net.IP) string {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return ""
	}
	return net.IPv4(ipv4[3], ipv4[2], ipv4[1], ipv4[0]).String()
}
