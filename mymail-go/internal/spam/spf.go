// Package spam 实现反垃圾邮件模块（阶段 4）。
//
// 包含：
//   - spf.go：SPF (Sender Policy Framework) 校验，支持 ip4/ip6/mx/a/include/exists/all
//   - dnsbl.go：DNSBL (DNS-based Blackhole List) 并行查询
//   - scorer.go：评分规则（SPF + DNSBL 综合评分）
//   - filter.go：评分主流程，整合 SPF + DNSBL + 评分，含失败降级
//
// 设计依据：
//   - RFC 7208 (SPF)
//   - 原 Node.js smtp-validator.js（实际反垃圾逻辑，spam-filter.js 是死代码 P0-2）
//   - Go 重构方案：SPF fail +5、DNSBL 命中 +5/zone
//
// 关键修复：
//   - P0-2：重写 spam 包（原 spam-filter.js 不可用）
//   - 失败降级：DNS 查询失败时邮件仍正常投递（spam_score=0）
package spam

import (
	"context"
	"net"
	"strings"
	"time"
)

// SPF 校验结果类型。
const (
	SPFResultPass      = "pass"      // IP 匹配 SPF 记录
	SPFResultFail      = "fail"      // IP 不匹配且策略为 -all
	SPFResultSoftFail  = "softfail"  // IP 不匹配且策略为 ~all
	SPFResultNeutral   = "neutral"   // 策略为 ?all 或无 all
	SPFResultNone      = "none"      // 无 SPF 记录
	SPFResultError     = "error"     // 查询错误
	SPFResultSkip      = "skip"      // 跳过校验（功能开关关闭）
	SPFResultTempError = "temperror" // 临时错误（DNS 超时等）
)

// dnsResolver DNS 解析器接口（抽象 *net.Resolver 用于 mock 测试）。
// *net.Resolver 自动满足此接口。
type dnsResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SPFResult SPF 校验结果。
type SPFResult struct {
	Result string
	Detail string
}

// SPFChecker SPF 校验器。
//
// 使用 net.Resolver 进行 DNS 查询，支持自定义超时与递归深度。
// 递归深度限制防止 include 机制循环引用（RFC 7208 建议 10）。
type SPFChecker struct {
	resolver  dnsResolver
	maxDepth  int
	timeoutMs int
}

// NewSPFChecker 创建 SPF 校验器。
//
// 参数：
//   - resolver: DNS 解析器（传 nil 则用默认解析器）
//   - maxDepth: include 递归最大深度（<= 0 时默认 10）
//   - timeoutMs: 单次校验总超时（<= 0 时默认 3000ms）
func NewSPFChecker(resolver *net.Resolver, maxDepth, timeoutMs int) *SPFChecker {
	if maxDepth <= 0 {
		maxDepth = 10
	}
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &SPFChecker{
		resolver:  resolver,
		maxDepth:  maxDepth,
		timeoutMs: timeoutMs,
	}
}

// newSPFCheckerWithResolver 内部构造器，接受 dnsResolver 接口（用于测试 mock）。
func newSPFCheckerWithResolver(resolver dnsResolver, maxDepth, timeoutMs int) *SPFChecker {
	if maxDepth <= 0 {
		maxDepth = 10
	}
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &SPFChecker{
		resolver:  resolver,
		maxDepth:  maxDepth,
		timeoutMs: timeoutMs,
	}
}

// Check 执行 SPF 校验。
//
// 参数：
//   - senderIP: 发件人 IP（IPv4 或 IPv6）
//   - senderDomain: 发件人邮箱域名（用于查 TXT 记录）
//   - heloDomain: HELO/EHLO 域名（用于兜底查询，本实现暂未使用）
func (c *SPFChecker) Check(ctx context.Context, senderIP, senderDomain, heloDomain string) SPFResult {
	if senderDomain == "" {
		return SPFResult{Result: SPFResultNone, Detail: "empty sender domain"}
	}
	ipAddr := net.ParseIP(senderIP)
	if ipAddr == nil {
		return SPFResult{Result: SPFResultError, Detail: "invalid sender IP: " + senderIP}
	}

	// 总超时控制
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.timeoutMs)*time.Millisecond)
	defer cancel()

	return c.checkDomain(ctx, ipAddr, senderDomain, 0)
}

// checkDomain 递归查询域名的 SPF 记录。
func (c *SPFChecker) checkDomain(ctx context.Context, ip net.IP, domain string, depth int) SPFResult {
	if depth > c.maxDepth {
		return SPFResult{Result: SPFResultError, Detail: "max recursion depth exceeded"}
	}

	txtRecords, err := c.resolver.LookupTXT(ctx, domain)
	if err != nil {
		// 域名不存在或查询失败：视为 none（降级，不阻断投递）
		return SPFResult{Result: SPFResultNone, Detail: "no TXT record: " + err.Error()}
	}

	// 找 v=spf1 开头的记录
	var spfRecord string
	for _, txt := range txtRecords {
		if strings.HasPrefix(strings.ToLower(txt), "v=spf1") {
			spfRecord = txt
			break
		}
	}
	if spfRecord == "" {
		return SPFResult{Result: SPFResultNone, Detail: "no SPF record"}
	}

	// 拆分机制（跳过 v=spf1）
	fields := strings.Fields(spfRecord)
	if len(fields) <= 1 {
		return SPFResult{Result: SPFResultNeutral, Detail: "empty SPF record"}
	}
	mechanisms := fields[1:]

	// 依次匹配机制
	for _, mech := range mechanisms {
		// 跳过修饰符（如 redirect=、exp=）
		// 注意：ip4:/ip6: 不含 '='，但即使含 '=' 也按机制处理
		if isModifier(mech) {
			// TODO: redirect= 暂未实现，按 neutral 处理
			continue
		}
		result, matched, isAll := c.matchMechanism(ctx, mech, ip, domain, depth)
		if isAll {
			return result
		}
		if matched {
			return result
		}
	}

	// 无 all 机制：默认 neutral
	return SPFResult{Result: SPFResultNeutral, Detail: "no all mechanism"}
}

// isModifier 判断是否为修饰符（包含 '=' 且不是 ip4/ip6 机制）。
func isModifier(mech string) bool {
	if strings.HasPrefix(mech, "ip4:") || strings.HasPrefix(mech, "ip6:") {
		return false
	}
	return strings.Contains(mech, "=")
}

// matchMechanism 匹配单个机制。
//
// 返回：
//   - result: 机制匹配时的结果
//   - matched: 是否匹配
//   - isAll: 是否是 all 机制（始终匹配）
func (c *SPFChecker) matchMechanism(ctx context.Context, mech string, ip net.IP, domain string, depth int) (SPFResult, bool, bool) {
	// 解析限定符前缀
	qualifier := "+"
	if len(mech) > 0 && (mech[0] == '+' || mech[0] == '-' || mech[0] == '~' || mech[0] == '?') {
		qualifier = string(mech[0])
		mech = mech[1:]
	}

	resultMap := map[string]string{
		"+": SPFResultPass,
		"-": SPFResultFail,
		"~": SPFResultSoftFail,
		"?": SPFResultNeutral,
	}
	makeResult := func() SPFResult {
		return SPFResult{Result: resultMap[qualifier], Detail: mech}
	}

	// all 机制（始终匹配）
	if mech == "all" {
		return makeResult(), true, true
	}

	// ip4:CIDR
	if strings.HasPrefix(mech, "ip4:") {
		cidr := strings.TrimPrefix(mech, "ip4:")
		if matchIPv4CIDR(ip, cidr) {
			return makeResult(), true, false
		}
		return SPFResult{}, false, false
	}

	// ip6:CIDR
	if strings.HasPrefix(mech, "ip6:") {
		cidr := strings.TrimPrefix(mech, "ip6:")
		if matchIPv6CIDR(ip, cidr) {
			return makeResult(), true, false
		}
		return SPFResult{}, false, false
	}

	// mx[:domain]
	if mech == "mx" || strings.HasPrefix(mech, "mx:") {
		mxDomain := domain
		if strings.HasPrefix(mech, "mx:") {
			mxDomain = strings.TrimPrefix(mech, "mx:")
		}
		if c.matchMX(ctx, ip, mxDomain) {
			return makeResult(), true, false
		}
		return SPFResult{}, false, false
	}

	// a[:domain]
	if mech == "a" || strings.HasPrefix(mech, "a:") {
		aDomain := domain
		if strings.HasPrefix(mech, "a:") {
			aDomain = strings.TrimPrefix(mech, "a:")
		}
		if c.matchA(ctx, ip, aDomain) {
			return makeResult(), true, false
		}
		return SPFResult{}, false, false
	}

	// include:domain
	if strings.HasPrefix(mech, "include:") {
		incDomain := strings.TrimPrefix(mech, "include:")
		subResult := c.checkDomain(ctx, ip, incDomain, depth+1)
		if subResult.Result == SPFResultPass {
			return makeResult(), true, false
		}
		// include 中匹配 fail/softfail/neutral 不直接返回，继续匹配后续机制
		return SPFResult{}, false, false
	}

	// exists:domain
	if strings.HasPrefix(mech, "exists:") {
		exDomain := strings.TrimPrefix(mech, "exists:")
		if c.matchExists(ctx, exDomain) {
			return makeResult(), true, false
		}
		return SPFResult{}, false, false
	}

	// 未知机制：跳过
	return SPFResult{}, false, false
}

// matchIPv4CIDR 检查 IPv4 是否在 CIDR 网段内。
// cidr 不带掩码时视为 /32。
func matchIPv4CIDR(ip net.IP, cidr string) bool {
	if !strings.Contains(cidr, "/") {
		cidr = cidr + "/32"
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	ip = ip.To4()
	if ip == nil {
		return false
	}
	return ipNet.Contains(ip)
}

// matchIPv6CIDR 检查 IPv6 是否在 CIDR 网段内。
// cidr 不带掩码时视为 /128。
func matchIPv6CIDR(ip net.IP, cidr string) bool {
	if !strings.Contains(cidr, "/") {
		cidr = cidr + "/128"
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	// 仅 IPv6
	if ip.To4() != nil {
		return false
	}
	return ipNet.Contains(ip)
}

// matchMX 查询 MX 记录并检查 A/AAAA 记录是否匹配 IP。
func (c *SPFChecker) matchMX(ctx context.Context, ip net.IP, domain string) bool {
	mxs, err := c.resolver.LookupMX(ctx, domain)
	if err != nil {
		return false
	}
	for _, mx := range mxs {
		if c.matchA(ctx, ip, mx.Host) {
			return true
		}
	}
	return false
}

// matchA 查询 A/AAAA 记录并检查是否匹配 IP。
func (c *SPFChecker) matchA(ctx context.Context, ip net.IP, domain string) bool {
	ips, err := c.resolver.LookupIPAddr(ctx, domain)
	if err != nil {
		return false
	}
	for _, resolved := range ips {
		if resolved.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// matchExists 查询 A 记录是否存在。
func (c *SPFChecker) matchExists(ctx context.Context, domain string) bool {
	_, err := c.resolver.LookupIPAddr(ctx, domain)
	return err == nil
}
