// filter_test.go 测试反垃圾主流程。
//
// 覆盖：
//   - Filter.Evaluate 整合逻辑（mock SPFChecker + DNSBLChecker）
//   - 特性开关（spfEnabled/dnsblEnabled）
//   - nil 依赖降级
//   - SpamVerdict 方法（ShouldReject/ShouldMark/FormatReasons）
package spam

import (
	"context"
	"testing"
)

func TestFilter_Evaluate_AllEnabled_AllPass(t *testing.T) {
	spf := &mockSPFChecker{result: SPFResult{Result: SPFResultPass, Detail: "ip4 match"}}
	dnsbl := &mockDNSBLChecker{result: DNSBLResult{Listed: false}}
	scorer := NewScorer(5, 10)
	f := NewFilter(spf, dnsbl, scorer, true, true)

	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 0 {
		t.Errorf("全 pass 期望 score=0，实际 %d", v.Score)
	}
	if v.Action != ActionAllow {
		t.Errorf("期望 action=allow，实际 %s", v.Action)
	}
	if !spf.called {
		t.Error("SPF 应被调用")
	}
	if !dnsbl.called {
		t.Error("DNSBL 应被调用")
	}
}

func TestFilter_Evaluate_SPFFail_Only(t *testing.T) {
	spf := &mockSPFChecker{result: SPFResult{Result: SPFResultFail, Detail: "-all"}}
	dnsbl := &mockDNSBLChecker{result: DNSBLResult{Listed: false}}
	scorer := NewScorer(5, 10)
	f := NewFilter(spf, dnsbl, scorer, true, true)

	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 5 {
		t.Errorf("SPF fail 期望 score=5，实际 %d", v.Score)
	}
	if v.Action != ActionMark {
		t.Errorf("score=5 期望 mark，实际 %s", v.Action)
	}
}

func TestFilter_Evaluate_SPFFail_DNSBLListed(t *testing.T) {
	spf := &mockSPFChecker{result: SPFResult{Result: SPFResultFail, Detail: "-all"}}
	dnsbl := &mockDNSBLChecker{result: DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}}}
	scorer := NewScorer(5, 10)
	f := NewFilter(spf, dnsbl, scorer, true, true)

	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 10 {
		t.Errorf("SPF fail + DNSBL 1 zone 期望 score=10，实际 %d", v.Score)
	}
	if v.Action != ActionReject {
		t.Errorf("score=10 期望 reject，实际 %s", v.Action)
	}
	if len(v.Reasons) != 2 {
		t.Errorf("期望 2 个 reasons，实际 %d (%v)", len(v.Reasons), v.Reasons)
	}
}

func TestFilter_Evaluate_SPFDisabled(t *testing.T) {
	spf := &mockSPFChecker{result: SPFResult{Result: SPFResultFail, Detail: "should not be used"}}
	dnsbl := &mockDNSBLChecker{result: DNSBLResult{Listed: false}}
	scorer := NewScorer(5, 10)
	f := NewFilter(spf, dnsbl, scorer, false, true) // SPF 关闭

	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 0 {
		t.Errorf("SPF 关闭期望 score=0，实际 %d", v.Score)
	}
	if spf.called {
		t.Error("SPF 关闭时不应被调用")
	}
	if !dnsbl.called {
		t.Error("DNSBL 应被调用")
	}
}

func TestFilter_Evaluate_DNSBLDisabled(t *testing.T) {
	spf := &mockSPFChecker{result: SPFResult{Result: SPFResultPass, Detail: "match"}}
	dnsbl := &mockDNSBLChecker{result: DNSBLResult{Listed: true, Zones: []string{"x"}}}
	scorer := NewScorer(5, 10)
	f := NewFilter(spf, dnsbl, scorer, true, false) // DNSBL 关闭

	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 0 {
		t.Errorf("DNSBL 关闭 + SPF pass 期望 score=0，实际 %d", v.Score)
	}
	if dnsbl.called {
		t.Error("DNSBL 关闭时不应被调用")
	}
}

func TestFilter_Evaluate_NilScorer_Degrade(t *testing.T) {
	// scorer 为 nil：降级放行
	f := NewFilter(nil, nil, nil, true, true)
	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 0 {
		t.Errorf("nil scorer 期望 score=0，实际 %d", v.Score)
	}
	if v.Action != ActionAllow {
		t.Errorf("nil scorer 期望 allow，实际 %s", v.Action)
	}
}

func TestFilter_Evaluate_NilCheckers(t *testing.T) {
	// spf/dnsbl 为 nil：跳过校验，评分 0
	scorer := NewScorer(5, 10)
	f := NewFilter(nil, nil, scorer, true, true)
	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 0 {
		t.Errorf("nil checkers 期望 score=0，实际 %d", v.Score)
	}
	if v.Action != ActionAllow {
		t.Errorf("期望 allow，实际 %s", v.Action)
	}
}

func TestFilter_Evaluate_DNSBLOnly_Listed(t *testing.T) {
	spf := &mockSPFChecker{result: SPFResult{Result: SPFResultSkip, Detail: "skip"}}
	dnsbl := &mockDNSBLChecker{result: DNSBLResult{Listed: true, Zones: []string{"zen.spamhaus.org"}}}
	scorer := NewScorer(5, 10)
	f := NewFilter(spf, dnsbl, scorer, true, true)

	v := f.Evaluate(context.Background(), "1.2.3.4", "example.com", "helo.example.com")
	if v.Score != 5 {
		t.Errorf("DNSBL 命中 1 zone 期望 score=5，实际 %d", v.Score)
	}
	if v.Action != ActionMark {
		t.Errorf("score=5 期望 mark，实际 %s", v.Action)
	}
}

func TestSpamVerdict_ShouldReject(t *testing.T) {
	v := SpamVerdict{Action: ActionReject}
	if !v.ShouldReject() {
		t.Error("reject 应返回 true")
	}
	if v.ShouldMark() {
		t.Error("reject 不应 ShouldMark=true")
	}
}

func TestSpamVerdict_ShouldMark(t *testing.T) {
	v := SpamVerdict{Action: ActionMark}
	if !v.ShouldMark() {
		t.Error("mark 应返回 true")
	}
	if v.ShouldReject() {
		t.Error("mark 不应 ShouldReject=true")
	}
}

func TestSpamVerdict_FormatReasons(t *testing.T) {
	tests := []struct {
		name     string
		reasons  []string
		want     string
	}{
		{"empty", nil, ""},
		{"single", []string{"SPF fail"}, "SPF fail"},
		{"multiple", []string{"SPF fail", "DNSBL listed"}, "SPF fail; DNSBL listed"},
		{"three", []string{"a", "b", "c"}, "a; b; c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := SpamVerdict{Reasons: tt.reasons}
			got := v.FormatReasons()
			if got != tt.want {
				t.Errorf("FormatReasons() = %q, want %q", got, tt.want)
			}
		})
	}
}
