// validator_test.go 测试 MessageValidator 的反垃圾验证与落库逻辑。
//
// 测试覆盖：
//   - Disabled（功能关闭）
//   - NilFilter（filter 未初始化）
//   - Allow / Mark / Reject 三种判决
//   - spam_log 落库验证
//   - NilSpamLogDAO（不落库但放行）
//   - SpamLogError_Degrade（DB 错误降级）
//   - ShouldReject / ShouldMark 判断
package smtp

import (
	"context"
	"testing"

	"github.com/mymail/mymail-go/internal/spam"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// mockSPFChecker mock SPF 校验器。
type mockSPFChecker struct {
	result spam.SPFResult
}

func (m *mockSPFChecker) Check(ctx context.Context, ip, senderDomain, heloDomain string) spam.SPFResult {
	return m.result
}

// mockDNSBLChecker mock DNSBL 查询器。
type mockDNSBLChecker struct {
	result spam.DNSBLResult
}

func (m *mockDNSBLChecker) Check(ctx context.Context, ip string) spam.DNSBLResult {
	return m.result
}

// newTestValidator 创建测试用 MessageValidator。
// spfResult 和 dnsblResult 控制 spam.Filter 的评估结果。
func newTestValidator(t *testing.T, spfResult spam.SPFResult, dnsblResult spam.DNSBLResult, enabled bool) (*MessageValidator, *dao.SpamLogDAO, *spam.Filter) {
	t.Helper()
	database := newSMTPTestDB(t)
	spamLogDAO := dao.NewSpamLogDAO(database)
	scorer := spam.NewScorer(5, 10) // suspiciousThreshold=5, spamThreshold=10
	spf := &mockSPFChecker{result: spfResult}
	dnsbl := &mockDNSBLChecker{result: dnsblResult}
	filter := spam.NewFilter(spf, dnsbl, scorer, true, true)
	v := NewMessageValidator(filter, spamLogDAO, enabled)
	return v, spamLogDAO, filter
}

func TestMessageValidator_Disabled(t *testing.T) {
	v, _, _ := newTestValidator(t,
		spam.SPFResult{Result: spam.SPFResultFail},
		spam.DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
		false, // 功能关闭
	)

	result := v.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@evil.com",
		SenderDomain: "evil.com",
		HELODomain:   "evil.com",
		Recipient:    "b@y.com",
	})

	if result.ShouldReject {
		t.Error("功能关闭时不应拒绝")
	}
	if result.SpamScore != 0 {
		t.Errorf("功能关闭时 Score 应为 0，实际 %d", result.SpamScore)
	}
	if result.Verdict.Action != spam.ActionAllow {
		t.Errorf("功能关闭时 Action 应为 allow，实际 %s", result.Verdict.Action)
	}
}

func TestMessageValidator_NilFilter(t *testing.T) {
	database := newSMTPTestDB(t)
	spamLogDAO := dao.NewSpamLogDAO(database)
	v := NewMessageValidator(nil, spamLogDAO, true)

	result := v.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@evil.com",
		SenderDomain: "evil.com",
		Recipient:    "b@y.com",
	})

	if result.ShouldReject {
		t.Error("filter=nil 时不应拒绝")
	}
	if result.SpamScore != 0 {
		t.Errorf("filter=nil 时 Score 应为 0，实际 %d", result.SpamScore)
	}
}

func TestMessageValidator_Allow(t *testing.T) {
	v, spamLogDAO, _ := newTestValidator(t,
		spam.SPFResult{Result: spam.SPFResultPass, Detail: "pass"},
		spam.DNSBLResult{Listed: false},
		true,
	)

	result := v.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@x.com",
		SenderDomain: "x.com",
		HELODomain:   "x.com",
		Recipient:    "b@y.com",
	})

	// SPF pass + DNSBL 未命中 = score 0
	if result.SpamScore != 0 {
		t.Errorf("Score 应为 0，实际 %d", result.SpamScore)
	}
	if result.ShouldReject {
		t.Error("不应拒绝")
	}
	if result.ShouldMark {
		t.Error("不应标记")
	}
	if result.Verdict.Action != spam.ActionAllow {
		t.Errorf("Action 应为 allow，实际 %s", result.Verdict.Action)
	}

	// 验证 spam_log 已落库
	count, _ := spamLogDAO.Count(context.Background())
	if count != 1 {
		t.Errorf("spam_log 应有 1 条记录，实际 %d", count)
	}
}

func TestMessageValidator_Mark(t *testing.T) {
	// SPF none(2) + DNSBL 1 zone(5) = 7 → mark（5 <= 7 < 10）
	v, spamLogDAO, _ := newTestValidator(t,
		spam.SPFResult{Result: spam.SPFResultNone, Detail: "no spf record"},
		spam.DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
		true,
	)

	result := v.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@evil.com",
		SenderDomain: "evil.com",
		HELODomain:   "evil.com",
		Recipient:    "b@y.com",
	})

	if result.SpamScore != 7 {
		t.Errorf("Score 应为 7，实际 %d", result.SpamScore)
	}
	if !result.ShouldMark {
		t.Error("应标记 mark")
	}
	if result.ShouldReject {
		t.Error("不应 reject")
	}
	if result.Verdict.Action != spam.ActionMark {
		t.Errorf("Action 应为 mark，实际 %s", result.Verdict.Action)
	}

	// 验证 spam_log action=mark
	stats, _ := spamLogDAO.StatsByAction(context.Background(), "")
	for _, s := range stats {
		if s.Action == spam.ActionMark && s.Count == 1 {
			return
		}
	}
	t.Errorf("spam_log 应有 1 条 mark 记录，stats=%+v", stats)
}

func TestMessageValidator_Reject(t *testing.T) {
	// SPF fail(5) + DNSBL 1 zone(5) = 10 → reject（>= 10）
	v, spamLogDAO, _ := newTestValidator(t,
		spam.SPFResult{Result: spam.SPFResultFail, Detail: "fail"},
		spam.DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
		true,
	)

	result := v.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@evil.com",
		SenderDomain: "evil.com",
		HELODomain:   "evil.com",
		Recipient:    "b@y.com",
	})

	if result.SpamScore != 10 {
		t.Errorf("Score 应为 10，实际 %d", result.SpamScore)
	}
	if !result.ShouldReject {
		t.Error("应 reject")
	}
	if result.Verdict.Action != spam.ActionReject {
		t.Errorf("Action 应为 reject，实际 %s", result.Verdict.Action)
	}

	// 验证 spam_log action=reject
	list, _ := spamLogDAO.FindRecent(context.Background(), 10, 0)
	if len(list) != 1 {
		t.Fatalf("spam_log 应有 1 条记录，实际 %d", len(list))
	}
	if list[0].Action != spam.ActionReject {
		t.Errorf("spam_log action 应为 reject，实际 %s", list[0].Action)
	}
	if list[0].Score != 10 {
		t.Errorf("spam_log score 应为 10，实际 %d", list[0].Score)
	}
	if list[0].IP != "1.2.3.4" {
		t.Errorf("spam_log ip 应为 1.2.3.4，实际 %s", list[0].IP)
	}
}

func TestMessageValidator_SpamReasonsFormatted(t *testing.T) {
	v, _, _ := newTestValidator(t,
		spam.SPFResult{Result: spam.SPFResultFail, Detail: "fail"},
		spam.DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
		true,
	)

	result := v.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@evil.com",
		SenderDomain: "evil.com",
		HELODomain:   "evil.com",
		Recipient:    "b@y.com",
	})

	// SpamReasons 应包含 SPF 和 DNSBL 原因
	if result.SpamReasons == "" {
		t.Error("SpamReasons 不应为空")
	}
}

func TestMessageValidator_NilSpamLogDAO(t *testing.T) {
	database := newSMTPTestDB(t)
	scorer := spam.NewScorer(5, 10)
	spf := &mockSPFChecker{result: spam.SPFResult{Result: spam.SPFResultPass}}
	dnsbl := &mockDNSBLChecker{result: spam.DNSBLResult{Listed: false}}
	filter := spam.NewFilter(spf, dnsbl, scorer, true, true)
	v := NewMessageValidator(filter, nil, true) // spamLogDAO=nil

	result := v.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@x.com",
		SenderDomain: "x.com",
		HELODomain:   "x.com",
		Recipient:    "b@y.com",
	})

	// spamLogDAO=nil 时应正常放行，不 panic
	if result.ShouldReject {
		t.Error("不应拒绝")
	}
	_ = database
}

func TestMessageValidator_SpamLogError_Degrade(t *testing.T) {
	// 使用独立 DB 以便关闭模拟写入失败
	v, spamLogDAO, _ := newTestValidator(t,
		spam.SPFResult{Result: spam.SPFResultFail, Detail: "fail"},
		spam.DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}},
		true,
	)

	// 关闭底层 DB 模拟 spam_log 写入失败
	// spamLogDAO 内部持有 db.DB，关闭后 Create 会报错
	// 但 newSMTPTestDB 的 Cleanup 会 Close，我们需要在 Validate 前手动关闭
	// 由于 newSMTPTestDB 已注册 Cleanup，这里直接获取 DB 并关闭
	// 通过 spamLogDAO 无法直接拿到 DB，改用独立构造
	database := newSMTPTestDB(t)
	spamLogDAO2 := dao.NewSpamLogDAO(database)
	scorer := spam.NewScorer(5, 10)
	spf := &mockSPFChecker{result: spam.SPFResult{Result: spam.SPFResultFail}}
	dnsbl := &mockDNSBLChecker{result: spam.DNSBLResult{Listed: true, Zones: []string{"z"}}}
	filter := spam.NewFilter(spf, dnsbl, scorer, true, true)
	v2 := NewMessageValidator(filter, spamLogDAO2, true)

	// 关闭 DB
	database.Close()

	// Validate 应正常返回（spam_log 写入失败不阻断）
	result := v2.Validate(context.Background(), ValidateInput{
		IP:           "1.2.3.4",
		Sender:       "a@evil.com",
		SenderDomain: "evil.com",
		HELODomain:   "evil.com",
		Recipient:    "b@y.com",
	})

	// 验证结果仍返回（降级不阻断）
	if result.SpamScore != 10 {
		t.Errorf("Score 应为 10，实际 %d", result.SpamScore)
	}
	if !result.ShouldReject {
		t.Error("应 reject")
	}
	_ = v
	_ = spamLogDAO
}

func TestMessageValidator_SpamLogMultipleRecords(t *testing.T) {
	v, spamLogDAO, _ := newTestValidator(t,
		spam.SPFResult{Result: spam.SPFResultPass},
		spam.DNSBLResult{Listed: false},
		true,
	)

	// 多次 Validate
	for i := 0; i < 5; i++ {
		v.Validate(context.Background(), ValidateInput{
			IP:           "1.2.3.4",
			Sender:       "a@x.com",
			SenderDomain: "x.com",
			HELODomain:   "x.com",
			Recipient:    "b@y.com",
		})
	}

	count, _ := spamLogDAO.Count(context.Background())
	if count != 5 {
		t.Errorf("spam_log 应有 5 条记录，实际 %d", count)
	}
}
