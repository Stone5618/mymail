// spam 包测试辅助：mock DNS 解析器。
//
// 通过 mockResolver 可控制 DNS 查询返回值，覆盖 SPF/DNSBL 的各种分支。
package spam

import (
	"context"
	"net"
	"sync"
)

// mockResolver mock DNS 解析器，实现 dnsResolver 接口。
//
// 用法：
//   - 设置 txtRecords[key] = []string{...} 控制 LookupTXT 返回
//   - 设置 mxRecords[key] = []*net.MX{...} 控制 LookupMX 返回
//   - 设置 ipAddrs[key] = []net.IPAddr{...} 控制 LookupIPAddr 返回
//   - 未设置的 key 返回错误（模拟 NXDOMAIN）
//   - 设置 txtError/ipError 强制返回错误
type mockResolver struct {
	mu        sync.Mutex
	txtCalls  []string // 记录 LookupTXT 调用的 name 参数
	mxCalls   []string
	ipCalls   []string
	txtRecords map[string][]string
	mxRecords  map[string][]*net.MX
	ipAddrs    map[string][]net.IPAddr
	txtError   error // 强制 TXT 错误
	mxError    error
	ipError    error
}

func newMockResolver() *mockResolver {
	return &mockResolver{
		txtRecords: make(map[string][]string),
		mxRecords:  make(map[string][]*net.MX),
		ipAddrs:    make(map[string][]net.IPAddr),
	}
}

func (m *mockResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txtCalls = append(m.txtCalls, name)
	if m.txtError != nil {
		return nil, m.txtError
	}
	if recs, ok := m.txtRecords[name]; ok {
		return recs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (m *mockResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mxCalls = append(m.mxCalls, name)
	if m.mxError != nil {
		return nil, m.mxError
	}
	if recs, ok := m.mxRecords[name]; ok {
		return recs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (m *mockResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ipCalls = append(m.ipCalls, host)
	if m.ipError != nil {
		return nil, m.ipError
	}
	if recs, ok := m.ipAddrs[host]; ok {
		return recs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// mockSPFChecker mock SPF 校验器，实现 SPFCheckerIface。
type mockSPFChecker struct {
	result SPFResult
	called bool
	ip     string
	domain string
}

func (m *mockSPFChecker) Check(ctx context.Context, ip, senderDomain, heloDomain string) SPFResult {
	m.called = true
	m.ip = ip
	m.domain = senderDomain
	return m.result
}

// mockDNSBLChecker mock DNSBL 查询器，实现 DNSBLCheckerIface。
type mockDNSBLChecker struct {
	result DNSBLResult
	called bool
	ip     string
}

func (m *mockDNSBLChecker) Check(ctx context.Context, ip string) DNSBLResult {
	m.called = true
	m.ip = ip
	return m.result
}
